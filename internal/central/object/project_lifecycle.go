package object

import (
	"context"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

var _ oc.ObjectProjectStop = (*Service)(nil)

type projectStopAdmissionKey struct{}

type capturedProjectWork struct {
	identity projectWork
	handle   *projectWorkHandle
}

func (s *Service) RequestProjectStop(ctx context.Context, actor identity.Actor, cause oc.ProjectStopCause) (oc.ObjectStopReport, error) {
	return s.projectStop(ctx, actor, cause, oc.RequestProjectStopStep)
}
func (s *Service) InspectProjectStop(ctx context.Context, actor identity.Actor, cause oc.ProjectStopCause) (oc.ObjectStopReport, error) {
	return s.projectStop(ctx, actor, cause, oc.InspectProjectStopStep)
}

func stopReport(cause oc.ProjectStopCause, state oc.ProjectStopState, reason oc.ProjectStopReason, refs []oc.ProjectStopRef) oc.ObjectStopReport {
	r, _ := oc.NewObjectStopReport(oc.ObjectStopComponent, cause, oc.ObjectStopDetails{State: state, SafeReason: reason, ActiveRefs: refs})
	return r
}
func stopFailure(cause oc.ProjectStopCause, err error) (oc.ObjectStopReport, error) {
	reason := oc.ProjectStopDependencyUnavailable
	if hasCode(err, foundation.CommitUnknown) {
		reason = oc.ProjectStopOutcomeUnknown
	}
	if hasCode(err, foundation.DependencyUnbound) {
		reason = oc.ProjectStopDependencyUnbound
	}
	return stopReport(cause, oc.ProjectStopPending, reason, nil), err
}

func (s *Service) projectStop(ctx context.Context, actor identity.Actor, cause oc.ProjectStopCause, step oc.ProjectStopStep) (oc.ObjectStopReport, error) {
	request, err := oc.NewProjectStopRequest(oc.ProjectStopRequestDetails{Actor: actor, Cause: cause, Component: oc.ObjectStopComponent, Step: step})
	if err != nil {
		return oc.ObjectStopReport{}, invalid()
	}
	if s == nil || s.data == nil || nilPort(s.state().auth.ProjectStop) {
		return stopFailure(cause, failure(foundation.DependencyUnbound, nil))
	}
	op, done, err := s.begin(context.WithValue(ctx, projectStopAdmissionKey{}, true))
	if err != nil {
		return stopFailure(cause, err)
	}
	defer done()
	ctx = op.ctx
	row, err := readProjectStop(ctx, s.state().store, cause)
	if err != nil {
		return stopFailure(cause, err)
	}
	lane, after := 0, ""
	if row != nil {
		lane, after = row.lane, row.after
	}
	preflight, err := s.discoverProjectStop(ctx, request, lane, after)
	if err != nil {
		return stopFailure(cause, err)
	}
	var captured []capturedProjectWork
	var readOnly, stopped bool
	checked := s.projectStopTransaction(ctx, preflight, func(ctx context.Context, _ foundation.Tx, e postgres.SQLExecutor, a oc.ProjectStopAuthorization, f projectStopFacts) error {
		current, err := readProjectStop(ctx, e, cause)
		if err != nil {
			return err
		}
		if current == nil && step == oc.InspectProjectStopStep {
			return failure(foundation.InvalidState, nil)
		}
		readOnly = a.Mode() == oc.ReadProjectStop
		if readOnly {
			if current == nil || current.state != "stopped" {
				return failure(foundation.InvalidState, nil)
			}
			stopped = true
			return nil
		}
		if a.Mode() != oc.ContinueProjectStop {
			return failure(foundation.Forbidden, nil)
		}
		if current != nil && current.state == "stopped" {
			stopped = true
			return nil
		}
		r := s.state()
		r.mu.Lock()
		defer r.mu.Unlock()
		for _, w := range f.works {
			if !w.joined && stopWorkRelevant(w.kind, cause.Details().Action) {
				h := r.projectWork[w.id]
				if h != nil && !workIdentityEqual(w, h.work) {
					return accessChanged()
				}
				captured = append(captured, capturedProjectWork{w, h})
			}
		}
		return nil
	})
	if err = commitError(checked); err != nil {
		return stopFailure(cause, err)
	}
	if readOnly || stopped {
		return stopReport(cause, oc.ProjectStopped, "", nil), nil
	}
	// Creating the gate and revocation is a separate, freshly authorized phase.
	// An unknown reply from either transaction means no cancellation at all.
	gatePlan, err := s.discoverProjectStop(ctx, request, lane, after)
	if err != nil {
		return stopFailure(cause, err)
	}
	gated := s.projectStopTransaction(ctx, gatePlan, func(ctx context.Context, tx foundation.Tx, e postgres.SQLExecutor, a oc.ProjectStopAuthorization, f projectStopFacts) error {
		if a.Mode() != oc.ContinueProjectStop {
			return failure(foundation.Forbidden, nil)
		}
		d := cause.Details()
		current, err := readProjectStop(ctx, e, cause)
		if err != nil {
			return err
		}
		if current == nil {
			if step != oc.RequestProjectStopStep {
				return failure(foundation.InvalidState, nil)
			}
			_, err = e.Exec(ctx, `INSERT INTO agenteam_object.project_stops(project_id,operation_id,action,project_version) VALUES($1,$2,$3,$4)`, d.ProjectID.String(), d.OperationID.String(), string(d.Action), int64(d.ProjectVersion))
			if err != nil {
				return unavailable(err)
			}
		}
		if !f.overflow {
			if err = s.revokeProjectBatch(ctx, tx, e, cause, f); err != nil {
				return err
			}
		}
		return nil
	})
	if err = commitError(gated); err != nil {
		return stopFailure(cause, err)
	}
	if !gatePlan.facts.overflow {
		cancelCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		for _, c := range captured {
			if c.handle == nil {
				continue
			}
			r := s.state()
			r.mu.Lock()
			same := r.projectWork[c.identity.id] == c.handle && workIdentityEqual(c.identity, c.handle.work)
			stop, stopFn := c.handle.cancel, c.handle.stop
			r.mu.Unlock()
			if same {
				if stop != nil {
					stop()
				}
				if stopFn != nil {
					_ = stopFn(cancelCtx)
				}
			}
		}
		cancel()
	}
	// A checkpoint owns fresh mapping locks; all original native writers shared
	// the Project/Object/command union. Local handles prove actual return, while
	// external processes require the exact configured ProcessGuard proof.
	checkpoint, err := s.discoverProjectStop(ctx, request, lane, after)
	if err != nil {
		return stopFailure(cause, err)
	}
	dead := map[oc.ProcessID]bool{}
	if !nilPort(s.state().auth.Processes) {
		for _, w := range checkpoint.facts.works {
			if w.process != s.state().process && !w.joined && !dead[w.process] {
				dead[w.process] = s.state().auth.Processes.ConfirmStopped(ctx, w.process) == nil
			}
		}
	}
	// Native rows from before work registration still need exact process proof.
	nativeProcesses, err := queryStopIDs(ctx, s.state().store, `SELECT DISTINCT process_id::text FROM (SELECT process_id FROM agenteam_object.upload_attempts WHERE object_id=ANY($1::uuid[]) AND kind='private_candidate' AND NOT io_closed UNION SELECT process_id FROM agenteam_object.object_leases WHERE object_id=ANY($1::uuid[]) AND state='active' AND process_id IS NOT NULL) p ORDER BY process_id LIMIT 1001`, checkpoint.facts.objects)
	if err != nil {
		return stopFailure(cause, err)
	}
	if len(nativeProcesses) > stopFanoutLimit {
		return stopReport(cause, oc.ProjectStopPending, oc.ProjectStopWorkPending, nil), nil
	}
	if !nilPort(s.state().auth.Processes) {
		for _, raw := range nativeProcesses {
			id, err := foundation.ParseID[oc.Process](raw)
			if err != nil {
				return stopFailure(cause, unavailable(err))
			}
			if id != s.state().process && !dead[id] {
				dead[id] = s.state().auth.Processes.ConfirmStopped(ctx, id) == nil
			}
		}
	}
	var pending bool
	finished := s.projectStopTransaction(ctx, checkpoint, func(ctx context.Context, _ foundation.Tx, e postgres.SQLExecutor, a oc.ProjectStopAuthorization, f projectStopFacts) error {
		if a.Mode() != oc.ContinueProjectStop {
			return failure(foundation.Forbidden, nil)
		}
		current, err := readProjectStop(ctx, e, cause)
		if err != nil {
			return err
		}
		if current == nil {
			return failure(foundation.InvalidState, nil)
		}
		if !f.overflow {
			for _, w := range f.works {
				if !stopWorkRelevant(w.kind, cause.Details().Action) {
					continue
				}
				joined := w.joined || dead[w.process]
				s.state().mu.Lock()
				h := s.state().projectWork[w.id]
				if h != nil && workIdentityEqual(w, h.work) && h.ended {
					joined = true
				}
				s.state().mu.Unlock()
				if !joined {
					continue
				}
				if _, err = e.Exec(ctx, `UPDATE agenteam_object.project_work SET joined_at=COALESCE(joined_at,clock_timestamp()) WHERE id=$1`, w.id); err != nil {
					return unavailable(err)
				}
				if err = checkpointWorkNative(ctx, e, w); err != nil {
					return err
				}
			}
		}
		if !f.overflow {
			for process, proved := range dead {
				if !proved {
					continue
				}
				if _, err = e.Exec(ctx, `UPDATE agenteam_object.upload_attempts SET io_closed=true,phase=CASE WHEN phase IN ('reserved','sending') THEN 'unknown' ELSE phase END WHERE object_id=ANY($1::uuid[]) AND process_id=$2 AND kind='private_candidate'`, f.objects, process.String()); err != nil {
					return unavailable(err)
				}
				if _, err = e.Exec(ctx, `UPDATE agenteam_object.object_leases SET state='released',released_at=clock_timestamp() WHERE object_id=ANY($1::uuid[]) AND process_id=$2 AND state='active' AND (owner_kind='writer' OR $3 AND owner_kind IN ('reader','source'))`, f.objects, process.String(), cause.Details().Action == oc.ProjectStopDelete); err != nil {
					return unavailable(err)
				}
			}
		}
		nextLane, nextAfter := lane, ""
		if len(f.ids) == stopBatchLimit {
			nextAfter = f.ids[len(f.ids)-1]
		} else {
			nextLane = (lane + 1) % 5
		}
		d := cause.Details()
		_, err = e.Exec(ctx, `UPDATE agenteam_object.project_stops SET scan_kind=$3,scan_after=$4 WHERE project_id=$1 AND operation_id=$2 AND state='stopping'`, d.ProjectID.String(), d.OperationID.String(), nextLane, null(nextAfter))
		if err != nil {
			return unavailable(err)
		}
		pending, err = projectStopPending(ctx, e, cause)
		pending = pending || f.overflow
		return err
	})
	if err = commitError(finished); err != nil {
		return stopFailure(cause, err)
	}
	if pending {
		return stopReport(cause, oc.ProjectStopPending, oc.ProjectStopWorkPending, nil), nil
	}
	// Completion is its own atomic authorization/existence check, independent of
	// the cursor and every bounded diagnostic collected above.
	final, err := s.discoverProjectStop(ctx, request, 0, "")
	if err != nil {
		return stopFailure(cause, err)
	}
	result := s.projectStopTransaction(ctx, final, func(ctx context.Context, _ foundation.Tx, e postgres.SQLExecutor, a oc.ProjectStopAuthorization, facts projectStopFacts) error {
		if a.Mode() != oc.ContinueProjectStop {
			return failure(foundation.Forbidden, nil)
		}
		row, err := readProjectStop(ctx, e, cause)
		if err != nil {
			return err
		}
		if row == nil {
			return failure(foundation.InvalidState, nil)
		}
		pending, err = projectStopPending(ctx, e, cause)
		pending = pending || facts.overflow
		if err != nil || pending {
			return err
		}
		d := cause.Details()
		_, err = e.Exec(ctx, `UPDATE agenteam_object.project_stops SET state='stopped',stopped_at=COALESCE(stopped_at,clock_timestamp()),scan_kind=0,scan_after=NULL WHERE project_id=$1 AND operation_id=$2`, d.ProjectID.String(), d.OperationID.String())
		return unavailableIf(err)
	})
	if err = commitError(result); err != nil {
		return stopFailure(cause, err)
	}
	if pending {
		return stopReport(cause, oc.ProjectStopPending, oc.ProjectStopWorkPending, nil), nil
	}
	return stopReport(cause, oc.ProjectStopped, "", nil), nil
}

func (s *Service) revokeProjectBatch(ctx context.Context, tx foundation.Tx, e postgres.SQLExecutor, cause oc.ProjectStopCause, f projectStopFacts) error {
	d := cause.Details()
	for _, w := range f.works {
		if stopWorkRelevant(w.kind, d.Action) {
			if _, err := e.Exec(ctx, `UPDATE agenteam_object.project_work SET revoked_by=COALESCE(revoked_by,$2) WHERE id=$1 AND admission_version<$3`, w.id, d.OperationID.String(), int64(d.ProjectVersion)); err != nil {
				return unavailable(err)
			}
		}
	}
	for _, object := range f.objects {
		if _, err := e.Exec(ctx, `UPDATE agenteam_object.uploads SET disposition='revoked' WHERE object_id=$1 AND disposition='reserved'`, object); err != nil {
			return unavailable(err)
		}
		if _, err := e.Exec(ctx, `DELETE FROM agenteam_object.object_references r USING agenteam_object.uploads u WHERE r.upload_id=u.id AND u.object_id=$1 AND u.disposition='revoked' AND r.kind='reserved'`, object); err != nil {
			return unavailable(err)
		}
	}
	if d.Action == oc.ProjectStopDelete {
		if _, err := e.Exec(ctx, `UPDATE agenteam_download.grants SET revoked=true WHERE project_id=$1 AND (id=ANY($2::uuid[]) OR object_id=ANY($3::uuid[]))`, d.ProjectID.String(), f.ids, f.objects); err != nil {
			return unavailable(err)
		}
	}
	ids, err := queryStopIDs(ctx, e, `SELECT id::text FROM agenteam_object.object_transfers WHERE project_id=$1 AND (id=ANY($2::uuid[]) OR object_id=ANY($3::uuid[])) AND ($4 OR direction='put') AND revoked_at IS NULL ORDER BY id LIMIT 1001`, d.ProjectID.String(), f.ids, f.objects, d.Action == oc.ProjectStopDelete)
	if err != nil {
		return err
	}
	if len(ids) > stopFanoutLimit {
		return failure(foundation.ResourceBusy, nil)
	}
	for _, raw := range ids {
		id, _ := foundation.ParseID[oc.Transfer](raw)
		r, ok, err := loadTransfer(ctx, e, id)
		if err != nil {
			return err
		}
		if !ok {
			return accessChanged()
		}
		s.state().mu.Lock()
		transfers := s.state().transfers
		s.state().mu.Unlock()
		if transfers == nil || nilPort(transfers.state().authority) {
			return failure(foundation.DependencyUnbound, nil)
		}
		if _, err = e.Exec(ctx, `UPDATE agenteam_object.object_transfers SET revoked_at=clock_timestamp(),version=version+1 WHERE id=$1 AND revoked_at IS NULL`, raw); err != nil {
			return unavailable(err)
		}
		if err = transfers.appendAudit(ctx, tx, r, ac.ObjectTransferRevoke); err != nil {
			return err
		}
	}
	return nil
}

func checkpointWorkNative(ctx context.Context, e postgres.SQLExecutor, w projectWork) error {
	if w.kind == "preparation" {
		if _, err := e.Exec(ctx, `UPDATE agenteam_object.upload_attempts SET io_closed=true,phase=CASE WHEN phase IN ('reserved','sending') THEN 'unknown' ELSE phase END WHERE id=$1 AND process_id=$2 AND kind='private_candidate'`, w.resource, w.process.String()); err != nil {
			return unavailable(err)
		}
		if _, err := e.Exec(ctx, `UPDATE agenteam_object.object_leases SET state='released',released_at=clock_timestamp() WHERE attempt_id=$1 AND process_id=$2 AND owner_kind='writer' AND state='active'`, w.resource, w.process.String()); err != nil {
			return unavailable(err)
		}
	}
	if w.kind == "reader" || w.kind == "source" {
		if _, err := e.Exec(ctx, `UPDATE agenteam_object.object_leases SET state='released',released_at=clock_timestamp() WHERE id=$1 AND process_id=$2 AND owner_kind IN ('reader','source') AND state='active'`, w.resource, w.process.String()); err != nil {
			return unavailable(err)
		}
	}
	return nil
}
