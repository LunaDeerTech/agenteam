package object

import (
	"context"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// recoveryFailures preserves the first observable failure while independent
// items advance. A failed item keeps its existing durable/in-memory checkpoint.
type recoveryFailures struct{ first error }

func (f *recoveryFailures) remember(err error) error {
	if f.first == nil {
		f.first = err
	}
	return f.first
}

// Recover advances storage verification and already-authorized cleanup only.
// It never uses a background Service actor to publish an Owner's business row.
func (s *Service) Recover(ctx context.Context) error {
	if nilPort(s.state().auth.Planner) {
		return failure(foundation.DependencyUnbound, nil)
	}
	op, finish, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer finish()
	ctx = op.ctx
	r := s.state()
	var failures recoveryFailures
	r.mu.Lock()
	closed := make(map[oc.AttemptID]bool, len(r.closedAttempts))
	for id, v := range r.closedAttempts {
		closed[id] = v
	}
	leases := make(map[oc.LeaseID]oc.ObjectID, len(r.closedLeases))
	for id, obj := range r.closedLeases {
		leases[id] = obj
	}
	r.mu.Unlock()
	for id, obj := range leases {
		if err = ctx.Err(); err != nil {
			return failures.remember(unavailable(err))
		}
		failures.remember(s.releaseInternal(id, obj))
	}
	for id := range closed {
		if err = ctx.Err(); err != nil {
			return failures.remember(unavailable(err))
		}
		if err = s.joinedAttempt(ctx, id, r.process); err != nil {
			failures.remember(err)
			continue
		}
		r.mu.Lock()
		delete(r.closedAttempts, id)
		r.mu.Unlock()
	}
	rows, err := r.store.Query(ctx, `SELECT DISTINCT process_id::text FROM agenteam_object.object_leases WHERE state='active' AND process_id IS NOT NULL ORDER BY process_id LIMIT 100`)
	if err != nil {
		return failures.remember(unavailable(err))
	}
	var processes []oc.ProcessID
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return failures.remember(unavailable(err))
		}
		id, err := foundation.ParseID[oc.Process](raw)
		if err != nil {
			rows.Close()
			return failures.remember(unavailable(err))
		}
		if id != r.process {
			processes = append(processes, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return failures.remember(unavailable(err))
	}
	for _, process := range processes {
		if err = ctx.Err(); err != nil {
			return failures.remember(unavailable(err))
		}
		if nilPort(r.auth.Processes) {
			failures.remember(failure(foundation.DependencyUnbound, nil))
			break
		}
		if err = r.auth.Processes.ConfirmStopped(ctx, process); err != nil {
			// A different instance may legitimately still own live I/O. Preserve
			// all its leases, but do not block this instance's independent work.
			failures.remember(portError(err))
			continue
		}
		failures.remember(s.releaseStopped(ctx, process))
	}
	rows, err = r.store.Query(ctx, `SELECT id::text FROM agenteam_object.upload_attempts WHERE io_closed AND NOT cleanup_gate AND phase IN ('reserved','sending','unknown') ORDER BY created_at,id LIMIT 100`)
	if err != nil {
		return failures.remember(unavailable(err))
	}
	var attempts []oc.AttemptID
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return failures.remember(unavailable(err))
		}
		id, err := foundation.ParseID[oc.Attempt](raw)
		if err != nil {
			rows.Close()
			return failures.remember(unavailable(err))
		}
		attempts = append(attempts, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return failures.remember(unavailable(err))
	}
	for _, id := range attempts {
		if err = ctx.Err(); err != nil {
			return failures.remember(unavailable(err))
		}
		failures.remember(s.recoverAttempt(ctx, id))
	}
	rows, err = r.store.Query(ctx, `SELECT id::text FROM agenteam_object.objects o WHERE cleaning AND state<>'deleted' OR EXISTS(SELECT 1 FROM agenteam_object.cleanup_operations c WHERE c.object_id=o.id AND c.phase<>'completed') ORDER BY created_at,id LIMIT 100`)
	if err != nil {
		return failures.remember(unavailable(err))
	}
	var objects []oc.ObjectID
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return failures.remember(unavailable(err))
		}
		id, err := foundation.ParseID[oc.StoredObject](raw)
		if err != nil {
			rows.Close()
			return failures.remember(unavailable(err))
		}
		objects = append(objects, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return failures.remember(unavailable(err))
	}
	for _, id := range objects {
		if err = ctx.Err(); err != nil {
			return failures.remember(unavailable(err))
		}
		_, err = s.cleanObject(ctx, id)
		failures.remember(err)
	}
	if err = ctx.Err(); err != nil {
		return failures.remember(unavailable(err))
	}
	return failures.remember(r.spool.RecoverOrphans(ctx, spoolRecoveryAuthority{s}))
}
func (s *Service) joinedAttempt(ctx context.Context, id oc.AttemptID, process oc.ProcessID) error {
	a, found, err := loadAttempt(ctx, s.state().store, id)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	result := s.withinAccess(ctx, recoveryCause(), s.maintenanceRequest(oc.JoinAttemptAccess, a.object, oc.AccessRequestDetails{AttemptID: id, ProcessID: process}), func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		_, err = e.Exec(ctx, `UPDATE agenteam_object.upload_attempts SET io_closed=true,phase=CASE WHEN phase IN ('reserved','sending') THEN 'unknown' ELSE phase END WHERE id=$1 AND process_id=$2`, id.String(), process.String())
		if err != nil {
			return unavailable(err)
		}
		_, err = e.Exec(ctx, `UPDATE agenteam_object.object_leases SET state='released',released_at=clock_timestamp() WHERE attempt_id=$1 AND process_id=$2 AND owner_kind='writer' AND state='active'`, id.String(), process.String())
		return unavailableIf(err)
	})
	return commitError(result)
}
func (s *Service) releaseStopped(ctx context.Context, process oc.ProcessID) error {
	rows, err := s.state().store.Query(ctx, `SELECT DISTINCT object_id::text FROM agenteam_object.object_leases WHERE process_id=$1 AND state='active' ORDER BY object_id LIMIT 100`, process.String())
	if err != nil {
		return unavailable(err)
	}
	var objects []oc.ObjectID
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return unavailable(err)
		}
		id, err := foundation.ParseID[oc.StoredObject](raw)
		if err != nil {
			rows.Close()
			return unavailable(err)
		}
		objects = append(objects, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return unavailable(err)
	}
	var failures recoveryFailures
	for _, id := range objects {
		if err = ctx.Err(); err != nil {
			return failures.remember(unavailable(err))
		}
		result := s.withinAccess(ctx, recoveryCause(), s.maintenanceRequest(oc.ReleaseProcessAccess, id, oc.AccessRequestDetails{ProcessID: process}), func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
			e, err := executor(s, tx)
			if err != nil {
				return err
			}
			_, err = e.Exec(ctx, `UPDATE agenteam_object.upload_attempts SET io_closed=true,phase=CASE WHEN phase IN ('reserved','sending') THEN 'unknown' ELSE phase END WHERE object_id=$1 AND process_id=$2`, id.String(), process.String())
			if err != nil {
				return unavailable(err)
			}
			_, err = e.Exec(ctx, `UPDATE agenteam_object.object_leases SET state='released',released_at=clock_timestamp() WHERE object_id=$1 AND process_id=$2 AND owner_kind IN ('reader','source','writer') AND state='active'`, id.String(), process.String())
			return unavailableIf(err)
		})
		failures.remember(commitError(result))
	}
	return failures.first
}
func (s *Service) recoverAttempt(ctx context.Context, id oc.AttemptID) error {
	a, found, err := loadAttempt(ctx, s.state().store, id)
	if err != nil {
		return err
	}
	if !found || !a.closed || a.cleaning {
		return nil
	}
	verification := s.state().backend.verify(ctx, a.key, a.size, a.digest)
	if verification != nil && !hasCode(verification, foundation.ObjectPayloadMissing) && !hasCode(verification, foundation.ObjectIntegrityMismatch) {
		return verification
	}
	_, found, err = loadUpload(ctx, s.state().store, a.upload)
	if err != nil {
		return err
	}
	if !found {
		return unavailable(nil)
	}
	result := s.withinAccess(ctx, recoveryCause(), s.maintenanceRequest(oc.RecoverAttemptAccess, a.object, oc.AccessRequestDetails{AttemptID: id}), func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		current, found, err := loadAttempt(ctx, e, id)
		if err != nil {
			return err
		}
		if !found || current.cleaning || current.phase == "published" || current.phase == "verified" {
			return nil
		}
		upload, found, err := loadUpload(ctx, e, a.upload)
		if err != nil {
			return err
		}
		if !found {
			return unavailable(nil)
		}
		if verification == nil && upload.attempt == id && upload.disposition != "revoked" {
			_, err = e.Exec(ctx, `UPDATE agenteam_object.upload_attempts SET phase='verified',fault_code=NULL WHERE id=$1`, id.String())
			return unavailableIf(err)
		}
		if err = s.gateAttempt(ctx, e, current, oc.AbandonedAttempt, a.upload.String()); err != nil {
			return err
		}
		obj, found, err := loadObject(ctx, e, a.object)
		if err != nil {
			return err
		}
		if !found {
			return unavailable(nil)
		}
		_, err = s.appendAudit(ctx, tx, upload, obj.meta, ac.ObjectUploadFailed, ac.Unknown, ac.FailedPhase, reasonFor(verification), a.id.String(), 1)
		return err
	})
	return commitError(result)
}

type spoolRecoveryAuthority struct{ s *Service }

func (a spoolRecoveryAuthority) ConfirmStopped(ctx context.Context, id oc.ProcessID) error {
	if nilPort(a.s.state().auth.Processes) {
		return failure(foundation.DependencyUnbound, nil)
	}
	if err := a.s.state().auth.Processes.ConfirmStopped(ctx, id); err != nil {
		return portError(err)
	}
	// The manifest's process and payload IDs are persisted on each physical
	// attempt. A pending, still-needed candidate prevents removal of that
	// process's files; no expiry or failed socket is substituted for evidence.
	var pending int64
	if err := a.s.state().store.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.upload_attempts WHERE process_id=$1 AND phase NOT IN ('verified','published','cleaned')`, id.String()).Scan(&pending); err != nil {
		return unavailable(err)
	}
	if pending != 0 {
		return failure(foundation.ResourceBusy, nil)
	}
	return nil
}

type MaintenanceStatus struct {
	Running   bool               `json:"running"`
	Healthy   bool               `json:"healthy"`
	CheckedAt foundation.Instant `json:"checked_at"`
	Code      foundation.Code    `json:"code,omitempty"`
}

func (s *Service) MaintenanceStatus() MaintenanceStatus {
	r := s.state()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.maintenance
}
func (s *Service) StartMaintenance(ctx context.Context) error {
	r := s.state()
	r.mu.Lock()
	if r.stopped || r.forced || !r.initialized {
		r.mu.Unlock()
		return unavailable(nil)
	}
	if r.workerDone != nil {
		r.mu.Unlock()
		return failure(foundation.InvalidState, nil)
	}
	r.workerDone = make(chan struct{})
	r.maintenance.Running = true
	r.mu.Unlock()
	go func() {
		defer func() {
			r.mu.Lock()
			r.maintenance.Running = false
			close(r.workerDone)
			close(r.changed)
			r.changed = make(chan struct{})
			r.mu.Unlock()
		}()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-r.workerStop:
				return
			default:
			}
			attempt, cancel := context.WithTimeout(ctx, 15*time.Second)
			err := s.Recover(attempt)
			cancel()
			at, _ := foundation.NewInstant(time.Now())
			r.mu.Lock()
			r.maintenance.CheckedAt = at
			r.maintenance.Healthy = err == nil
			r.maintenance.Code = ""
			if err != nil {
				r.maintenance.Code = foundation.DependencyUnavailable
			}
			r.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-r.workerStop:
				return
			case <-ticker.C:
			}
		}
	}()
	return nil
}
