package object

import (
	"context"
	"crypto/sha256"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func (s *Service) CancelUpload(ctx context.Context, actor identity.Actor, owner oc.ObjectOwner, key foundation.IdempotencyKey) (oc.LookupResult, error) {
	op, finish, err := s.begin(ctx)
	if err != nil {
		return oc.LookupResult{}, err
	}
	defer finish()
	ctx = op.ctx
	command, err := commandIdentity(owner, key)
	if err != nil {
		return oc.LookupResult{}, err
	}
	maintenance := actor.Details().Kind == identity.Service
	if actor.Validate() != nil || maintenance && (actor.Details().ServiceName != identity.ObjectMaintenance || actor.Details().ProjectID != owner.Details().ProjectID) {
		return oc.LookupResult{}, failure(foundation.Forbidden, nil)
	}
	var object oc.ObjectID
	result := s.withinAccess(ctx, recoveryCause(), ownerRequest(actor, owner, oc.CancelAccess, oc.AccessRequestDetails{Key: key}), func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		if !maintenance {
			if _, err := s.authorize(ctx, tx, actor, owner, identity.Converge); err != nil {
				return err
			}
		}
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		u, found, err := loadCommand(ctx, e, command)
		if err != nil {
			return err
		}
		if !found {
			if maintenance {
				return failure(foundation.Forbidden, nil)
			}
			return failure(foundation.NotFound, nil)
		}
		object = u.object
		// A maintenance lookup is only discovery. No result or existence is
		// exposed until the formal persisted cleanup cause has authorized it.
		if maintenance {
			if !u.owner.Equal(owner) {
				return failure(foundation.Forbidden, nil)
			}
			if _, err := s.checkCancelMaintenance(ctx, tx, actor, owner, object); err != nil {
				return err
			}
		}
		if !maintenance {
			if _, err = s.authorize(ctx, tx, actor, owner, identity.Converge); err != nil {
				return err
			}
		}
		u, found, err = loadCommand(ctx, e, command)
		if err != nil {
			return err
		}
		if !found || u.object != object || !u.owner.Equal(owner) {
			if maintenance {
				return failure(foundation.Forbidden, nil)
			}
			return failure(foundation.ResourceBusy, nil)
		}
		causeID, err := foundation.ParseID[oc.CleanupOperation](u.id.String())
		if err != nil {
			return unavailable(err)
		}
		if maintenance {
			obj, ok, err := loadObject(ctx, e, object)
			if err != nil {
				return err
			}
			if !ok || !objectPartition(obj, owner) {
				return failure(foundation.Forbidden, nil)
			}
			causeID, err = s.checkCancelMaintenance(ctx, tx, actor, owner, object)
			if err != nil {
				return err
			}
		} else if err = checkOriginal(u, actor, owner); err != nil {
			return err
		}
		if err = s.gate(ctx, tx, actor, owner, identity.Converge); err != nil {
			return err
		}
		if u.disposition == "attached" {
			return failure(foundation.InvalidState, nil)
		}
		_, err = e.Exec(ctx, `UPDATE agenteam_object.uploads SET disposition='revoked' WHERE id=$1`, u.id.String())
		if err != nil {
			return unavailable(err)
		}
		_, err = e.Exec(ctx, `DELETE FROM agenteam_object.object_references WHERE object_id=$1 AND upload_id=$2 AND kind='reserved'`, object.String(), u.id.String())
		if err != nil {
			return unavailable(err)
		}
		return s.gateObject(ctx, e, object, oc.CancelledUpload, causeID.String())
	})
	if err = commitError(result); err != nil {
		return oc.LookupResult{}, err
	}
	if err = s.stopWriters(ctx, object); err != nil {
		return oc.LookupResult{State: oc.UploadRevoked, ObjectID: &object, Cleanup: oc.CleanupPending}, err
	}
	state, err := s.cleanObject(ctx, object)
	return oc.LookupResult{State: oc.UploadRevoked, ObjectID: &object, Cleanup: state}, err
}

// A registered maintenance actor is a separate convergence port, not an
// Avatar OwnerAuthorization grant. It confers no ordinary reading capability.
func (s *Service) checkCancelMaintenance(ctx context.Context, tx foundation.Tx, actor identity.Actor, owner oc.ObjectOwner, object oc.ObjectID) (oc.CleanupID, error) {
	id, err := foundation.ParseID[oc.CleanupOperation](actor.Details().CauseRef)
	if err != nil {
		return oc.CleanupID{}, failure(foundation.Forbidden, nil)
	}
	if nilPort(s.state().auth.Cleanup) {
		return oc.CleanupID{}, failure(foundation.DependencyUnbound, nil)
	}
	cause, err := oc.NewObjectCleanupCause(oc.CleanupDetails{OperationID: id, Owner: owner, Reason: oc.CancelledUpload})
	if err != nil {
		return oc.CleanupID{}, invalid()
	}
	if err = s.state().auth.Cleanup.CheckCleanupInTx(ctx, tx, cause, object); err != nil {
		return oc.CleanupID{}, portError(err)
	}
	return id, nil
}
func (s *Service) gateObject(ctx context.Context, e postgres.SQLExecutor, id oc.ObjectID, reason oc.CleanupReason, cause string) error {
	var references int64
	if err := e.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.object_references WHERE object_id=$1`, id.String()).Scan(&references); err != nil {
		return unavailable(err)
	}
	if references != 0 {
		return failure(foundation.ResourceBusy, nil)
	}
	_, err := e.Exec(ctx, `UPDATE agenteam_object.objects SET cleaning=true WHERE id=$1 AND state<>'deleted'`, id.String())
	if err != nil {
		return unavailable(err)
	}
	ids, err := attemptIDs(ctx, e, id)
	if err != nil {
		return err
	}
	for _, attemptID := range ids {
		a, found, err := loadAttempt(ctx, e, attemptID)
		if err != nil {
			return err
		}
		if !found {
			return unavailable(nil)
		}
		if err = s.gateAttempt(ctx, e, a, reason, cause); err != nil {
			return err
		}
	}
	return nil
}
func attemptIDs(ctx context.Context, e postgres.SQLExecutor, object oc.ObjectID) ([]oc.AttemptID, error) {
	rows, err := e.Query(ctx, `SELECT id::text FROM agenteam_object.upload_attempts WHERE object_id=$1 ORDER BY ordinal`, object.String())
	if err != nil {
		return nil, unavailable(err)
	}
	defer rows.Close()
	var ids []oc.AttemptID
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, unavailable(err)
		}
		id, err := foundation.ParseID[oc.Attempt](raw)
		if err != nil {
			return nil, unavailable(err)
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		return nil, unavailable(err)
	}
	return ids, nil
}
func (s *Service) stopWriters(ctx context.Context, object oc.ObjectID) error {
	ids, err := attemptIDs(ctx, s.state().store, object)
	if err != nil {
		return err
	}
	r := s.state()
	var active []*writer
	r.mu.Lock()
	for _, id := range ids {
		if w := r.writers[id]; w != nil {
			active = append(active, w)
			w.operation.cancel()
		}
	}
	r.mu.Unlock()
	for _, w := range active {
		select {
		case <-w.done:
		case <-ctx.Done():
			return unavailable(ctx.Err())
		}
	}
	for _, id := range ids {
		a, found, err := loadAttempt(ctx, r.store, id)
		if err != nil {
			return err
		}
		if !found || a.kind != "private_candidate" || !a.cleaning || a.process != r.process || a.closed {
			continue
		}
		r.mu.Lock()
		joined := r.closedAttempts[id] || (!a.late && r.writers[id] == nil)
		r.mu.Unlock()
		if joined {
			if err = s.joinedAttempt(ctx, id, r.process); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Service) DeleteUnreferenced(ctx context.Context, cause oc.ObjectCleanupCause, id oc.ObjectID) (oc.CleanupResult, error) {
	if cause.Validate() != nil || id.Validate() != nil {
		return oc.CleanupResult{}, invalid()
	}
	if nilPort(s.state().auth.Cleanup) {
		return oc.CleanupResult{}, failure(foundation.DependencyUnbound, nil)
	}
	if skillProjectCleanup(cause) {
		return s.deleteSkillObject(ctx, cause, id)
	}
	op, finish, err := s.begin(ctx)
	if err != nil {
		return oc.CleanupResult{}, err
	}
	defer finish()
	ctx = op.ctx
	out := oc.CleanupResult{State: oc.CleanupPending, OperationID: cause.Details().OperationID}
	request, _ := oc.NewObjectCleanupAccess(oc.AccessRequestDetails{Operation: oc.CleanupObjectAccess, Cleanup: cause, ObjectID: id})
	result := s.withinAccess(ctx, recoveryCause(), request, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		if err := s.state().auth.Cleanup.CheckCleanupInTx(ctx, tx, cause, id); err != nil {
			return portError(err)
		}
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		obj, found, err := loadObject(ctx, e, id)
		if err != nil {
			return err
		}
		if !found {
			return failure(foundation.NotFound, nil)
		}
		if !objectPartition(obj, cause.Details().Owner) {
			return failure(foundation.Forbidden, nil)
		}
		out.Remaining, err = inspect(ctx, e, id)
		if err != nil {
			return err
		}
		if len(out.Remaining.References) > 0 {
			return nil
		}
		return s.gateObject(ctx, e, id, cause.Details().Reason, cause.Details().OperationID.String())
	})
	if err = commitError(result); err != nil {
		return out, err
	}
	if len(out.Remaining.References) > 0 {
		return out, nil
	}
	if err = s.stopWriters(ctx, id); err != nil {
		return out, err
	}
	out.State, err = s.cleanObject(ctx, id)
	if err != nil {
		return out, err
	}
	out.Remaining, err = s.inspectExisting(ctx, id)
	return out, err
}
func (s *Service) inspectExisting(ctx context.Context, id oc.ObjectID) (oc.ReferenceInspection, error) {
	var out oc.ReferenceInspection
	result := s.withinAccess(ctx, recoveryCause(), s.maintenanceRequest(oc.InspectAccess, id, oc.AccessRequestDetails{}), func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		out, err = inspect(ctx, e, id)
		return err
	})
	return out, commitError(result)
}

type cleanupClaim struct {
	id, mode, phase, worker string
	fence                   int64
	attempt                 attemptRow
	allowed                 bool
}

func (s *Service) claimCleanup(ctx context.Context, object oc.ObjectID, attemptID oc.AttemptID) (cleanupClaim, error) {
	var out cleanupClaim
	worker, err := foundation.NewID[oc.CleanupOperation]()
	if err != nil {
		return out, unavailable(err)
	}
	result := s.withinAccess(ctx, recoveryCause(), s.maintenanceRequest(oc.ClaimCleanupAccess, object, oc.AccessRequestDetails{AttemptID: attemptID}), func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		a, found, err := loadAttempt(ctx, e, attemptID)
		if err != nil {
			return err
		}
		if !found || !a.cleaning {
			return nil
		}
		out.attempt = a
		obj, found, err := loadObject(ctx, e, object)
		if err != nil {
			return err
		}
		if !found {
			return unavailable(nil)
		}
		bounded := s.boundedCleanup(ctx, object) != nil
		remaining, err := inspectWithLimit(ctx, e, object, bounded)
		if err != nil {
			return err
		}
		if a.kind == "runner_staging" {
			for _, lease := range remaining.ActiveLeases {
				if lease.Owner.Details().Kind == oc.SourceOwner || lease.Owner.Details().Kind == oc.ReaderOwner {
					return nil
				}
			}
		}
		if obj.cleaning || obj.key == a.key {
			if len(remaining.References) > 0 {
				return nil
			}
			for _, lease := range remaining.ActiveLeases {
				if a.kind == "runner_staging" && lease.Owner.Details().Kind == oc.TransferOwner && lease.Owner.Details().ID == a.transfer.String() {
					continue // Only this gated staging body; the external lease remains.
				}
				if lease.Owner.Details().Kind != oc.WriterOwner {
					return nil
				}
			}
		}
		err = e.QueryRow(ctx, `SELECT id::text,mode,phase,fence FROM agenteam_object.cleanup_operations WHERE attempt_id=$1`, attemptID.String()).Scan(&out.id, &out.mode, &out.phase, &out.fence)
		if err != nil {
			return unavailable(err)
		}
		if out.phase == "completed" {
			return nil
		}
		if bounded {
			if a.kind == "private_candidate" && !a.closed {
				return failure(foundation.ResourceBusy, nil)
			}
			var live bool
			if err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.project_work WHERE object_id=$1 AND kind='cleanup' AND resource_id=$2 AND joined_at IS NULL)`, object.String(), out.id).Scan(&live); err != nil {
				return unavailable(err)
			}
			if live {
				return failure(foundation.ResourceBusy, nil)
			}
		}
		out.worker = worker.String()
		out.fence++
		_, err = e.Exec(ctx, `UPDATE agenteam_object.cleanup_operations SET phase='applying',worker_id=$2,fence=$3,fault_code=NULL WHERE id=$1`, out.id, out.worker, out.fence)
		if err != nil {
			return unavailable(err)
		}
		out.allowed = true
		return nil
	})
	if err = commitError(result); err != nil {
		return cleanupClaim{}, err
	}
	return out, nil
}
func (s *Service) cleanupIOContext(parent context.Context) (context.Context, context.CancelFunc) {
	r := s.state()
	r.mu.Lock()
	force := r.forceContext
	r.mu.Unlock()
	if parent.Value(runtimeRecoveryBudgetKey{}) == true {
		bounded, cancel := context.WithTimeout(parent, 15*time.Second)
		if force == nil {
			return bounded, cancel
		}
		joined, finish := cleanupBudgetIntersection(bounded, force)
		return joined, func() { finish(); cancel() }
	}
	if force != nil {
		return context.WithTimeout(force, 15*time.Second)
	}
	// A caller may cancel after the durable cleanup gate. Compensating storage
	// I/O uses a fresh bounded context; force always substitutes its shared cap.
	return context.WithTimeout(context.WithoutCancel(parent), 15*time.Second)
}
func (s *Service) cleanObject(ctx context.Context, object oc.ObjectID) (oc.CleanupState, error) {
	cause, bounded, err := canonicalSkillCleanup(ctx, s.state().store, object)
	if err != nil {
		return oc.CleanupPending, err
	}
	if bounded {
		out, err := s.deleteSkillObject(ctx, cause, object)
		return out.State, err
	}
	ids, err := attemptIDs(ctx, s.state().store, object)
	if err != nil {
		return oc.CleanupPending, err
	}
	return s.cleanObjectAttempts(ctx, object, ids)
}

func (s *Service) cleanObjectAttempts(ctx context.Context, object oc.ObjectID, ids []oc.AttemptID) (oc.CleanupState, error) {
	for _, id := range ids {
		claim, err := s.claimCleanup(ctx, object, id)
		if err != nil {
			return oc.CleanupPending, err
		}
		if !claim.allowed {
			continue
		}
		ioCtx, cancel := s.cleanupIOContext(ctx)
		if claim.mode == "zero_marker" {
			err = s.state().backend.zero(ioCtx, claim.attempt.key)
		} else {
			err = s.state().backend.remove(ioCtx, claim.attempt.key)
		}
		cancel()
		if err != nil {
			if ctx.Value(runtimeRecoveryBudgetKey{}) != true || ctx.Err() == nil {
				_ = s.noteCleanupFailure(ctx, claim, err)
			}
			return oc.CleanupPending, err
		}
		result := s.withinAccess(ctx, recoveryCause(), s.checkpointRequest(claim), func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
			e, err := executor(s, tx)
			if err != nil {
				return err
			}
			tag, err := e.Exec(ctx, `UPDATE agenteam_object.cleanup_operations SET phase='completed',fault_code=NULL WHERE id=$1 AND worker_id=$2 AND fence=$3 AND phase='applying'`, claim.id, claim.worker, claim.fence)
			if err != nil {
				return unavailable(err)
			}
			if tag.RowsAffected() != 1 {
				return failure(foundation.ResourceBusy, nil)
			}
			_, err = e.Exec(ctx, `UPDATE agenteam_object.upload_attempts SET phase='cleaned' WHERE id=$1 AND cleanup_gate`, id.String())
			return unavailableIf(err)
		})
		if err = commitError(result); err != nil {
			return oc.CleanupPending, err
		}
		if call := s.boundedCleanup(ctx, object); call != nil {
			call.completed[claim.worker] = claim
		}
	}
	return s.finalizeCleanup(ctx, object)
}
func (s *Service) noteCleanupFailure(ctx context.Context, c cleanupClaim, reason error) error {
	var bounded context.Context
	var cancel context.CancelFunc
	if ctx.Value(runtimeRecoveryBudgetKey{}) == true {
		bounded, cancel = s.cleanupCheckpointWithinBudget(ctx)
	} else {
		bounded, cancel = s.cleanupContext()
	}
	defer cancel()
	result := s.withinAccess(bounded, recoveryCause(), s.checkpointRequest(c), func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		_, err = e.Exec(ctx, `UPDATE agenteam_object.cleanup_operations SET fault_code=$4 WHERE id=$1 AND worker_id=$2 AND fence=$3`, c.id, c.worker, c.fence, errorCode(reason))
		return unavailableIf(err)
	})
	return commitError(result)
}
func (s *Service) checkpointRequest(c cleanupClaim) oc.AccessRequest {
	id, _ := foundation.ParseID[oc.CleanupOperation](c.id)
	worker, _ := foundation.ParseID[oc.CleanupOperation](c.worker)
	return s.maintenanceRequest(oc.CheckpointCleanupAccess, c.attempt.object, oc.AccessRequestDetails{AttemptID: c.attempt.id, CleanupID: id, WorkerID: worker, Fence: foundation.Version(c.fence)})
}

func (s *Service) finalizeCleanup(ctx context.Context, object oc.ObjectID) (oc.CleanupState, error) {
	state := oc.CleanupPending
	result := s.withinAccess(ctx, recoveryCause(), s.maintenanceRequest(oc.FinalizeCleanupAccess, object, oc.AccessRequestDetails{}), func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		u, found, err := scanUpload(e.QueryRow(ctx, `SELECT `+uploadColumns+` FROM agenteam_object.uploads WHERE object_id=$1`, object.String()))
		if err != nil {
			return err
		}
		if !found {
			return failure(foundation.NotFound, nil)
		}
		obj, found, err := loadObject(ctx, e, object)
		if err != nil {
			return err
		}
		if !found {
			return failure(foundation.NotFound, nil)
		}
		if s.boundedCleanup(ctx, object) != nil {
			workers, err := s.completedCleanupWorkers(ctx, object)
			if err != nil {
				return err
			}
			pending, err := boundedCleanupPending(ctx, e, object, workers)
			if err != nil || pending {
				return err
			}
		}
		if obj.meta.State == oc.Deleted {
			state = oc.CleanupCompleted
			return nil
		}
		if !obj.cleaning {
			return nil
		}
		refs, err := inspectWithLimit(ctx, e, object, s.boundedCleanup(ctx, object) != nil)
		if err != nil {
			return err
		}
		if len(refs.References) > 0 || len(refs.ActiveLeases) > 0 {
			return nil
		}
		var remaining bool
		if err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.upload_attempts WHERE object_id=$1 AND phase<>'cleaned')`, object.String()).Scan(&remaining); err != nil {
			return unavailable(err)
		}
		if remaining {
			return nil
		}
		u, found, err = scanUpload(e.QueryRow(ctx, `SELECT `+uploadColumns+` FROM agenteam_object.uploads WHERE object_id=$1`, object.String()))
		if err != nil {
			return err
		}
		if !found {
			return unavailable(nil)
		}
		var cause string
		if err = e.QueryRow(ctx, `SELECT operation_id::text FROM agenteam_object.cleanup_operations WHERE object_id=$1 ORDER BY created_at,id LIMIT 1`, object.String()).Scan(&cause); err != nil {
			return unavailable(err)
		}
		hash := sha256.Sum256([]byte("object.delete.v1\x00" + cause + "\x00" + object.String()))
		if _, err = s.appendAudit(ctx, tx, u, obj.meta, ac.ObjectDelete, ac.Success, ac.DeletedPhase, "", newDigest(hash[:]).String(), 1); err != nil {
			return err
		}
		_, err = e.Exec(ctx, `UPDATE agenteam_object.objects SET state='deleted',deleted_at=clock_timestamp(),version=version+1 WHERE id=$1`, object.String())
		if err != nil {
			return unavailable(err)
		}
		state = oc.CleanupCompleted
		return nil
	})
	if err := commitError(result); err != nil {
		return oc.CleanupPending, err
	}
	return state, nil
}

func (s *Service) CleanupProject(ctx context.Context, actor identity.Actor, cause oc.ProjectCleanupCause) (oc.ProjectCleanupResult, error) {
	if actor.Validate() != nil || cause.Validate() != nil {
		return oc.ProjectCleanupResult{}, invalid()
	}
	if nilPort(s.state().auth.Cleanup) {
		return oc.ProjectCleanupResult{}, failure(foundation.DependencyUnbound, nil)
	}
	op, finish, err := s.begin(ctx)
	if err != nil {
		return oc.ProjectCleanupResult{}, err
	}
	defer finish()
	ctx = op.ctx
	d := cause.Details()
	out := oc.ProjectCleanupResult{State: oc.CleanupPending, ProjectID: d.ProjectID, OperationID: d.OperationID}
	var objects []oc.ObjectID
	request, _ := oc.NewProjectCleanupAccess(oc.AccessRequestDetails{Operation: oc.GateProjectAccess, Actor: actor, ProjectCleanup: cause})
	result := s.withinAccess(ctx, recoveryCause(), request, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		if err = s.state().auth.Cleanup.CheckProjectCleanupInTx(ctx, tx, actor, cause); err != nil {
			return portError(err)
		}
		objects = plan.Details().Objects
		if err = s.state().auth.Cleanup.CheckProjectCleanupInTx(ctx, tx, actor, cause); err != nil {
			return portError(err)
		}
		for _, id := range objects {
			obj, found, err := loadObject(ctx, e, id)
			if err != nil {
				return err
			}
			if !found {
				continue
			}
			if obj.meta.Scope.Details().ProjectID != d.ProjectID.String() {
				return failure(foundation.Forbidden, nil)
			}
			// Persist the scheduling turn even when a real lease prevents deletion.
			// Repeated calls (including after restart) visit the whole project,
			// rather than starving later rows behind a protected first batch.
			if _, err = e.Exec(ctx, `UPDATE agenteam_object.objects SET project_cleanup_pass=project_cleanup_pass+1 WHERE id=$1`, id.String()); err != nil {
				return unavailable(err)
			}
			// Project destruction is the formal authority to release its references,
			// but never evidence that execution/history/transfer streams have stopped.
			_, err = e.Exec(ctx, `DELETE FROM agenteam_object.object_references WHERE object_id=$1 AND partition_id=$2`, id.String(), d.ProjectID.String())
			if err != nil {
				return unavailable(err)
			}
			_, err = e.Exec(ctx, `UPDATE agenteam_object.uploads SET disposition='revoked' WHERE object_id=$1`, id.String())
			if err != nil {
				return unavailable(err)
			}
			if err = s.gateObject(ctx, e, id, oc.ProjectDeleted, d.OperationID.String()); err != nil {
				return err
			}
			if err = gateProjectTransfers(ctx, s, tx, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err = commitError(result); err != nil {
		return out, err
	}
	for _, id := range objects {
		if err = s.stopWriters(ctx, id); err != nil {
			return out, err
		}
		if _, err = s.cleanObject(ctx, id); err != nil {
			return out, err
		}
	}
	request, _ = oc.NewProjectCleanupAccess(oc.AccessRequestDetails{Operation: oc.FinishProjectAccess, Actor: actor, ProjectCleanup: cause, Objects: objects})
	result = s.withinAccess(ctx, recoveryCause(), request, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		if err := s.state().auth.Cleanup.CheckProjectCleanupInTx(ctx, tx, actor, cause); err != nil {
			return portError(err)
		}
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		for _, id := range objects {
			obj, found, err := loadObject(ctx, e, id)
			if err != nil {
				return err
			}
			if !found || obj.meta.State != oc.Deleted {
				continue
			}
			remaining, err := inspect(ctx, e, id)
			if err != nil {
				return err
			}
			if len(remaining.References) > 0 || len(remaining.ActiveLeases) > 0 {
				continue
			}
			if err = purgeObjectTransfers(ctx, e, id); err != nil {
				return err
			}
			for _, query := range []string{
				`UPDATE agenteam_object.uploads SET current_attempt_id=NULL WHERE object_id=$1`,
				`DELETE FROM agenteam_object.cleanup_operations WHERE object_id=$1`,
				`DELETE FROM agenteam_object.object_leases WHERE object_id=$1`,
				`DELETE FROM agenteam_object.upload_attempts WHERE object_id=$1`,
				`DELETE FROM agenteam_object.uploads WHERE object_id=$1`,
				`DELETE FROM agenteam_object.objects WHERE id=$1`,
			} {
				if _, err = e.Exec(ctx, query, id.String()); err != nil {
					return unavailable(err)
				}
			}
		}
		var n int64
		if err = e.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.objects WHERE project_id=$1`, d.ProjectID.String()).Scan(&n); err != nil {
			return unavailable(err)
		}
		out.Remaining = foundation.Progress(n)
		if n == 0 {
			out.State = oc.CleanupCompleted
		}
		return nil
	})
	if err = commitError(result); err != nil {
		return out, err
	}
	return out, nil
}
