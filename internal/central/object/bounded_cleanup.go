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
	old, err := metadataIDs(ctx, e, `SELECT id::text FROM (
 (SELECT id FROM agenteam_object.upload_attempts WHERE object_id=$1 AND id<>$2 AND (phase<>'cleaned' OR (kind='private_candidate' AND NOT io_closed)) ORDER BY id LIMIT 31)
 UNION
 (SELECT attempt_id AS id FROM agenteam_object.cleanup_operations WHERE object_id=$1 AND attempt_id<>$2 AND phase<>'completed' ORDER BY attempt_id LIMIT 31)
) pending ORDER BY id LIMIT 31`, object.String(), u.attempt.String())
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

func (s *Service) beginBoundedCleanup(ctx context.Context) (*operation, func(), error) {
	// Retain the caller's original remaining budget for the operation's actual
	// work-retirement tail too. Always own a separate lifetime when called from
	// Recovery: reusing its parent would defer this batch's join until all other
	// objects have run, and would discard this original-budget context value.
	budget := context.WithValue(ctx, boundedCleanupBudgetKey{}, ctx)
	if parent, _ := ctx.Value(operationKey{}).(*operation); parent != nil && parent.service == s {
		return s.childOperation(budget)
	}
	return s.begin(budget)
}

// canonicalSkillCleanup only chooses the bounded recovery path. It grants no
// permission: deleteSkillObject re-discovers the original union and checks the
// current CleanupAuthority in the caller's Store/Tx before every change.
func canonicalSkillCleanup(ctx context.Context, e postgres.SQLExecutor, object oc.ObjectID) (oc.ObjectCleanupCause, bool, error) {
	u, found, err := scanUpload(e.QueryRow(ctx, `SELECT `+uploadColumns+` FROM agenteam_object.uploads WHERE object_id=$1`, object.String()))
	if err != nil || !found {
		return oc.ObjectCleanupCause{}, false, err
	}
	if u.owner.Details().Kind != oc.SkillRevision || u.state != "committed" || u.disposition != "revoked" {
		return oc.ObjectCleanupCause{}, false, nil
	}
	if u.attempt.Validate() != nil {
		return oc.ObjectCleanupCause{}, true, unavailable(nil)
	}
	var raw string
	err = e.QueryRow(ctx, `SELECT c.operation_id::text FROM agenteam_object.cleanup_operations c JOIN agenteam_object.upload_attempts a ON a.id=c.attempt_id JOIN agenteam_object.objects o ON o.id=c.object_id WHERE c.object_id=$1 AND c.attempt_id=$2 AND c.reason='project_deleted' AND a.upload_id=$3 AND a.object_id=$1 AND a.cleanup_gate AND o.cleaning`, object.String(), u.attempt.String(), u.id.String()).Scan(&raw)
	if err != nil {
		// Missing/corrupt canonical anchors must not fall back to the old
		// all-history recovery path or borrow an older attempt's cause.
		return oc.ObjectCleanupCause{}, true, unavailable(err)
	}
	id, err := foundation.ParseID[oc.CleanupOperation](raw)
	if err != nil {
		return oc.ObjectCleanupCause{}, true, unavailable(err)
	}
	cause, err := oc.NewObjectCleanupCause(oc.CleanupDetails{OperationID: id, Owner: u.owner, Reason: oc.ProjectDeleted})
	return cause, true, unavailableIf(err)
}

func (s *Service) deleteSkillObject(ctx context.Context, cause oc.ObjectCleanupCause, id oc.ObjectID) (oc.CleanupResult, error) {
	out := oc.CleanupResult{State: oc.CleanupPending, OperationID: cause.Details().OperationID}
	op, finish, err := s.beginBoundedCleanup(ctx)
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
