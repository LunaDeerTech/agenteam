package object

import (
	"context"
	"sort"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type boundedCleanupKey struct{}
type boundedCleanupBudgetKey struct{}

// A call owns one immutable set of at most 32 physical candidates. Its private
// completed map is filled only after I/O actually returned AND its exact
// checkpoint committed. It does not assert that the enclosing call has joined.
type boundedCleanupCall struct {
	service   *Service
	object    oc.ObjectID
	cause     oc.ObjectCleanupCause
	operation *operation
	completed map[string]cleanupClaim
}

func (s *Service) boundedCleanup(ctx context.Context, object oc.ObjectID) *boundedCleanupCall {
	c, _ := ctx.Value(boundedCleanupKey{}).(*boundedCleanupCall)
	if c == nil || c.service != s || c.object != object || c.operation == nil {
		return nil
	}
	op, _ := ctx.Value(operationKey{}).(*operation)
	if op != c.operation {
		return nil
	}
	return c
}

func (s *Service) gateSkillObjectBatch(ctx context.Context, e postgres.SQLExecutor, cause oc.ObjectCleanupCause, object oc.ObjectID) ([]oc.AttemptID, error) {
	u, found, err := scanUpload(e.QueryRow(ctx, `SELECT `+uploadColumns+` FROM agenteam_object.uploads WHERE object_id=$1`, object.String()))
	if err != nil {
		return nil, err
	}
	if !skillProjectCleanup(cause) || !found || !u.owner.Equal(cause.Details().Owner) || u.state != "committed" || u.disposition != "revoked" || u.attempt.Validate() != nil {
		return nil, failure(foundation.Forbidden, nil)
	}
	var protected bool
	if err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_references WHERE object_id=$1)`, object.String()).Scan(&protected); err != nil {
		return nil, unavailable(err)
	}
	if protected {
		return nil, failure(foundation.ResourceBusy, nil)
	}
	if _, err = e.Exec(ctx, `UPDATE agenteam_object.objects SET cleaning=true WHERE id=$1 AND state<>'deleted'`, object.String()); err != nil {
		return nil, unavailable(err)
	}
	// Always include the original published anchor. Its canonical cause is
	// required for post-Stop admission; unrelated old causes remain unchanged.
	ids := []oc.AttemptID{u.attempt}
	old, err := metadataIDs(ctx, e, `SELECT a.id::text FROM agenteam_object.upload_attempts a WHERE a.object_id=$1 AND a.id<>$2 AND (a.phase<>'cleaned' OR (a.kind='private_candidate' AND NOT a.io_closed) OR EXISTS(SELECT 1 FROM agenteam_object.cleanup_operations c WHERE c.attempt_id=a.id AND c.phase<>'completed')) ORDER BY a.id LIMIT 31`, object.String(), u.attempt.String())
	if err != nil {
		return nil, err
	}
	for _, raw := range old {
		id, _ := foundation.ParseID[oc.Attempt](raw)
		ids = append(ids, id)
	}
	for _, id := range ids {
		a, found, err := loadAttempt(ctx, e, id)
		if err != nil {
			return nil, err
		}
		if !found || a.object != object || a.upload != u.id {
			return nil, unavailable(nil)
		}
		if !a.cleaning {
			if err = s.gateAttempt(ctx, e, a, cause.Details().Reason, cause.Details().OperationID.String()); err != nil {
				return nil, err
			}
		}
	}
	return ids, nil
}

func (s *Service) deleteSkillObject(ctx context.Context, cause oc.ObjectCleanupCause, id oc.ObjectID) (oc.CleanupResult, error) {
	out := oc.CleanupResult{State: oc.CleanupPending, OperationID: cause.Details().OperationID}
	// Retain the caller's original remaining budget for the operation's actual
	// work-retirement tail too. The operation cancel itself is not that budget.
	op, finish, err := s.begin(context.WithValue(ctx, boundedCleanupBudgetKey{}, ctx))
	if err != nil {
		return out, err
	}
	defer finish()
	call := &boundedCleanupCall{service: s, object: id, cause: cause, operation: op, completed: make(map[string]cleanupClaim)}
	ctx = context.WithValue(op.ctx, runtimeRecoveryBudgetKey{}, true)
	ctx = context.WithValue(ctx, boundedCleanupKey{}, call)
	request, _ := oc.NewObjectCleanupAccess(oc.AccessRequestDetails{Operation: oc.CleanupObjectAccess, Cleanup: cause, ObjectID: id})
	var ids []oc.AttemptID
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
		if !found || !objectPartition(obj, cause.Details().Owner) {
			return failure(foundation.Forbidden, nil)
		}
		out.Remaining, err = inspectWithLimit(ctx, e, id, true)
		if err != nil || len(out.Remaining.References) != 0 {
			return err
		}
		ids, err = s.gateSkillObjectBatch(ctx, e, cause, id)
		return err
	})
	if err = commitError(result); err != nil || len(out.Remaining.References) != 0 {
		return out, err
	}
	if err = s.stopSelectedWriters(ctx, ids); err != nil {
		return out, err
	}
	out.State, err = s.cleanObjectAttempts(ctx, id, ids)
	if err != nil {
		return out, err
	}
	// Skills permits the original ObjectCleanup request, not a newly invented
	// generic Inspect maintenance grant after its lifecycle gate has closed.
	result = s.withinAccess(ctx, recoveryCause(), request, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		if err := s.state().auth.Cleanup.CheckCleanupInTx(ctx, tx, cause, id); err != nil {
			return portError(err)
		}
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		out.Remaining, err = inspectWithLimit(ctx, e, id, true)
		return err
	})
	if err = commitError(result); err != nil {
		out.State = oc.CleanupPending
	}
	return out, err
}

func (s *Service) stopSelectedWriters(ctx context.Context, ids []oc.AttemptID) error {
	r := s.state()
	for _, id := range ids {
		r.mu.Lock()
		w := r.writers[id]
		if w != nil {
			w.operation.cancel()
		}
		r.mu.Unlock()
		if w != nil {
			select {
			case <-w.done:
			case <-ctx.Done():
				return unavailable(ctx.Err())
			}
		}
		a, found, err := loadAttempt(ctx, r.store, id)
		if err != nil {
			return err
		}
		if !found {
			return unavailable(nil)
		}
		// Stop owns technical join and exact ProcessGuard recovery under the
		// original locks. Missing local writers and cancel are never substitutes.
		if a.kind == "private_candidate" && !a.closed {
			return failure(foundation.ResourceBusy, nil)
		}
	}
	return nil
}

func (s *Service) completedCleanupWorkers(ctx context.Context, object oc.ObjectID) ([]string, error) {
	call := s.boundedCleanup(ctx, object)
	if call == nil {
		return nil, nil
	}
	if len(call.completed) > oc.ObjectMetadataPurgeBatchLimit {
		return nil, invalid()
	}
	workers := make([]string, 0, len(call.completed))
	r := s.state()
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, claim := range call.completed {
		h := r.projectWork[id]
		if h == nil || h.operation != call.operation || h.work.object != object || h.work.resource != claim.id || h.work.fence != claim.fence {
			return nil, failure(foundation.ResourceBusy, nil)
		}
		workers = append(workers, id)
	}
	sort.Strings(workers)
	return workers, nil
}

func boundedCleanupPending(ctx context.Context, e postgres.SQLExecutor, object oc.ObjectID, workers []string) (bool, error) {
	if workers == nil {
		workers = []string{}
	}
	var pending bool
	err := e.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM agenteam_object.object_references WHERE object_id=$1)
 OR EXISTS(SELECT 1 FROM agenteam_object.object_leases WHERE object_id=$1 AND state='active')
 OR EXISTS(SELECT 1 FROM agenteam_object.upload_attempts WHERE object_id=$1 AND (phase<>'cleaned' OR NOT cleanup_gate OR (kind='private_candidate' AND NOT io_closed)))
 OR EXISTS(SELECT 1 FROM agenteam_object.cleanup_operations WHERE object_id=$1 AND phase<>'completed')
 OR EXISTS(SELECT 1 FROM agenteam_object.project_work w WHERE w.object_id=$1 AND w.joined_at IS NULL AND NOT (w.id=ANY($2::uuid[]) AND w.kind='cleanup' AND EXISTS(SELECT 1 FROM agenteam_object.cleanup_operations c WHERE c.id=w.resource_id AND c.object_id=w.object_id AND c.worker_id=w.id AND c.fence=w.cleanup_claim_fence AND c.phase='completed')))
 OR EXISTS(SELECT 1 FROM agenteam_object.object_transfers WHERE object_id=$1 AND (revoked_at IS NULL OR retirement_evidence IS NULL))`, object.String(), workers).Scan(&pending)
	return pending, unavailableIf(err)
}
