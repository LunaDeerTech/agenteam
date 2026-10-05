package object

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// CancelUploadWithinBudget has the same authorization and irreversible gate as
// CancelUpload. Recovery may not detach its storage compensation or diagnostic
// checkpoint from the caller's remaining startup/maintenance budget.
func (s *Service) CancelUploadWithinBudget(ctx context.Context, actor identity.Actor, owner oc.ObjectOwner, key foundation.IdempotencyKey) (oc.LookupResult, error) {
	return s.CancelUpload(context.WithValue(ctx, runtimeRecoveryBudgetKey{}, true), actor, owner, key)
}

// DeleteUnreferencedWithinBudget preserves the original caller/force caps while
// retaining the same current cleanup authorization, locks and operation joins.
func (s *Service) DeleteUnreferencedWithinBudget(ctx context.Context, cause oc.ObjectCleanupCause, id oc.ObjectID) (oc.CleanupResult, error) {
	return s.DeleteUnreferenced(context.WithValue(ctx, runtimeRecoveryBudgetKey{}, true), cause, id)
}

// cleanupBudgetIntersection carries the primary context's values and never
// extends either deadline. Cancellation of either source cancels actual work.
func cleanupBudgetIntersection(parent, ceiling context.Context) (context.Context, context.CancelFunc) {
	var ctx context.Context
	var cancel context.CancelFunc
	if deadline, ok := ceiling.Deadline(); ok {
		ctx, cancel = context.WithDeadline(parent, deadline)
	} else {
		ctx, cancel = context.WithCancel(parent)
	}
	stop := context.AfterFunc(ceiling, cancel)
	if ceiling.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }
}

func (s *Service) cleanupCheckpointWithinBudget(ctx context.Context) (context.Context, context.CancelFunc) {
	// The original context owns cleanupRequests, the drained fence and the
	// force cancellation hook. Unregister only after actual checkpoint return.
	registered, finish := s.cleanupContext()
	bounded, cancel := cleanupBudgetIntersection(ctx, registered)
	return bounded, func() { cancel(); finish() }
}

// ReleaseForCleanupInTx authorizes and closes the exact original upload. It
// deliberately issues no ordinary Service owner grant and performs no I/O.
func (s *Service) ReleaseForCleanupInTx(ctx context.Context, tx foundation.Tx, cause oc.ObjectCleanupCause, id oc.ObjectID, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
	d := plan.Details().Request.Details()
	request, err := oc.NewCleanupReleaseAccess(oc.AccessRequestDetails{Operation: oc.ReleaseForCleanupAccess, Cleanup: cause, ObjectID: id, UploadID: d.UploadID})
	if err != nil {
		return invalid()
	}
	if err = s.ValidateAccessPlanInTx(ctx, tx, request, plan, locked); err != nil {
		return err
	}
	if nilPort(s.state().auth.Cleanup) {
		return failure(foundation.DependencyUnbound, nil)
	}
	if err = s.state().auth.Cleanup.CheckCleanupInTx(ctx, tx, cause, id); err != nil {
		return portError(err)
	}
	x, err := executor(s, tx)
	if err != nil {
		return err
	}
	u, found, err := loadUpload(ctx, x, d.UploadID)
	if err != nil {
		return err
	}
	if !found || u.object != id || !u.owner.Equal(cause.Details().Owner) {
		return failure(foundation.Forbidden, nil)
	}
	obj, found, err := loadObject(ctx, x, id)
	if err != nil {
		return err
	}
	if !found || !objectPartition(obj, cause.Details().Owner) {
		return failure(foundation.Forbidden, nil)
	}
	if u.disposition == "revoked" {
		var exact bool
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.cleanup_operations WHERE object_id=$1 AND operation_id=$2 AND reason=$3) AND NOT EXISTS(SELECT 1 FROM agenteam_object.cleanup_operations WHERE object_id=$1 AND (operation_id<>$2 OR reason<>$3))`, id.String(), cause.Details().OperationID.String(), string(cause.Details().Reason)).Scan(&exact)
		if err != nil {
			return unavailable(err)
		}
		if !exact || !obj.cleaning && obj.meta.State != oc.Deleted {
			return failure(foundation.Forbidden, nil)
		}
		return nil
	}
	if obj.cleaning || obj.meta.State == oc.Deleted {
		return deleted(u.state == "committed")
	}
	// The normal Release port may have removed this reference earlier in this
	// same transaction. The exact persisted owning-domain cause is still required.
	owner := cause.Details().Owner
	_, err = x.Exec(ctx, `DELETE FROM agenteam_object.object_references WHERE object_id=$1 AND owner_kind=$2 AND owner_id=$3 AND partition_id=$4 AND upload_id=$5 AND kind IN ('canonical','reserved')`, id.String(), string(owner.Details().Kind), owner.Details().ID, owner.Partition(), u.id.String())
	if err != nil {
		return unavailable(err)
	}
	if _, err = x.Exec(ctx, `UPDATE agenteam_object.uploads SET disposition='revoked' WHERE id=$1`, u.id.String()); err != nil {
		return unavailable(err)
	}
	return s.gateObject(ctx, x, id, cause.Details().Reason, cause.Details().OperationID.String())
}

var _ oc.ReferenceCleanup = (*Service)(nil)
