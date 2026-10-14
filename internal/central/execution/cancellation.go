package execution

import (
	"context"
	"encoding/json"
	"math"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

// CancellationService borrows the original Execution owners. It owns no new
// worker, Model session, Tool dispatcher, or replacement execution.
type CancellationService struct {
	owner *ProjectCallOwnership
	tasks c.TaskCancellationProvider
}

func NewCancellationService(owner *ProjectCallOwnership, tasks c.TaskCancellationProvider) (*CancellationService, error) {
	if owner == nil || owner.state == nil || owner.state.service == nil || owner.state.preparation == nil || owner.state.direct == nil || nilPort(tasks) {
		return nil, fault(f.DependencyUnbound)
	}
	if err := tasks.RequireStore(owner.state.store); err != nil {
		return nil, portError(err)
	}
	return &CancellationService{owner, tasks}, nil
}

func (s *CancellationService) CancelLocks(actor i.Actor, scope c.CancellationScope) ([]f.LockRequest, error) {
	if s == nil || s.owner == nil || s.owner.state == nil {
		return nil, fault(f.DependencyUnbound)
	}
	if scope.Validate() != nil || actor.Validate() != nil {
		return nil, invalid()
	}
	if actor.Details().Kind != i.Human && actor.Details().Kind != i.AgentRun {
		return nil, fault(f.Forbidden)
	}
	schedule, _ := f.ProjectScheduleLock(scope.ProjectID.String())
	task, _ := f.AggregateLock(f.TaskAggregate, scope.TaskID)
	locks := append(scopeLocks(actor, scope.ProjectID, scope.AgentID, f.Shared), f.LockRequest{Key: schedule, Mode: f.Exclusive}, f.LockRequest{Key: task, Mode: f.Exclusive}, executionLock(scope.ExecutionID))
	return oc.NormalizeLocks(locks)
}

func (s *CancellationService) requireCancelScope(ctx context.Context, tx f.Tx, actor i.Actor, scope c.CancellationScope, intent i.AccessIntent) error {
	if actor.Details().Kind == i.Human {
		return s.owner.state.service.deps.Authority.requireScope(ctx, tx, actor, scope.ProjectID, scope.AgentID, intent)
	}
	return portError(s.tasks.RequireTaskCancellationInTx(ctx, tx, actor, scope))
}

// RequireTaskProtectionInTx lends only the original live Human cancel call to
// Work. Possessing this service, a request DTO, or an ordinary project call is
// insufficient to manufacture the protection mutation's lifetime authority.
func (s *CancellationService) RequireTaskProtectionInTx(ctx context.Context, tx f.Tx, actor i.Actor, scope c.CancellationScope) error {
	if s == nil || s.owner == nil || s.owner.state == nil || ctx == nil || scope.Validate() != nil || actor.Validate() != nil {
		return invalid()
	}
	w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness)
	if !ok || w.state != s.owner.state || w.entry == nil || actor.Details().Kind != i.Human {
		return fault(f.Forbidden)
	}
	e := w.entry
	w.state.mu.Lock()
	r := e.cancellation
	valid := r != nil && e.activeMethod == "cancel" && e.actor.Equal(actor) && e.record.Project == scope.ProjectID && e.launchKey.AgentID == scope.AgentID && r.ProjectID == scope.ProjectID && r.TaskID == scope.TaskID && r.ExecutionID == scope.ExecutionID
	w.state.mu.Unlock()
	if !valid {
		return fault(f.Forbidden)
	}
	locks, err := s.CancelLocks(actor, scope)
	if err != nil {
		return err
	}
	if err = w.state.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	if err = requireProjectCallInTx(ctx, tx, w.state.store); err != nil {
		return err
	}
	return s.requireCancelScope(ctx, tx, actor, scope, i.Mutate)
}

// RequestCancelInTx never commits or signals an in-memory owner. A successful
// return is tentative until the caller's real commit. An already requested or
// terminal execution is unchanged, so an old request cannot resurrect it.
func (s *CancellationService) RequestCancelInTx(ctx context.Context, tx f.Tx, actor i.Actor, scope c.CancellationScope, expected *f.Version) (c.Summary, error) {
	if ctx == nil {
		return c.Summary{}, invalid()
	}
	locks, err := s.CancelLocks(actor, scope)
	if err != nil {
		return c.Summary{}, err
	}
	store := s.owner.state.store
	if err = store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return c.Summary{}, portError(err)
	}
	// Human entrypoints either borrow this exact cancel command or Work's
	// whole-Task transition. A bare current Owner/scope cannot omit protection.
	if actor.Details().Kind == i.Human {
		if witness, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness); ok && witness.entry != nil && witness.entry.cancellation != nil {
			err = s.RequireTaskProtectionInTx(ctx, tx, actor, scope)
		} else {
			err = portError(s.tasks.RequireTaskCancellationInTx(ctx, tx, actor, scope))
		}
		if err != nil {
			return c.Summary{}, err
		}
	}
	if err = s.requireCancelScope(ctx, tx, actor, scope, i.Mutate); err != nil {
		return c.Summary{}, err
	}
	x, err := store.InTx(tx)
	if err != nil {
		return c.Summary{}, portError(err)
	}
	row, err := loadExecution(ctx, x, scope.ExecutionID)
	if err != nil {
		return c.Summary{}, err
	}
	if !cancellationScopeMatches(row, scope) {
		return c.Summary{}, fault(f.NotFound)
	}
	if expected != nil && (expected.Validate() != nil || row.summary.Version != *expected) {
		return c.Summary{}, fault(f.VersionConflict)
	}
	if row.summary.Status.Terminal() || row.summary.CancelRequestedAt != nil {
		return row.summary.Clone(), nil
	}
	if row.summary.Version == f.Version(math.MaxInt64) {
		return c.Summary{}, fault(f.ResourceBusy)
	}
	at, err := captureTime(ctx, x)
	if err != nil {
		return c.Summary{}, err
	}
	tag, err := x.Exec(ctx, `UPDATE agenteam_execution.executions SET cancel_requested_at=$3,version=version+1,updated_at=GREATEST($3,updated_at+interval '1 microsecond') WHERE id=$1 AND version=$2 AND cancel_requested_at IS NULL AND status NOT IN('succeeded','failed','cancelled')`, scope.ExecutionID.String(), int64(row.summary.Version), at.Time())
	if err != nil {
		return c.Summary{}, unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return c.Summary{}, fault(f.VersionConflict)
	}
	row.summary.Version++
	row.summary.CancelRequestedAt = &at
	return row.summary.Clone(), nil
}

func cancellationScopeMatches(row *executionRecord, scope c.CancellationScope) bool {
	return row != nil && row.summary.ID == scope.ExecutionID && row.summary.ProjectID == scope.ProjectID && row.summary.AgentID == scope.AgentID && row.launch.Trigger.Kind == "task" && row.launch.Trigger.TaskID == scope.TaskID
}

func (s *CancellationService) scope(ctx context.Context, r c.CancelRequest) (c.CancellationScope, error) {
	row, err := loadExecution(ctx, s.owner.state.store, r.ExecutionID)
	if err != nil {
		return c.CancellationScope{}, err
	}
	if row == nil || row.summary.ProjectID != r.ProjectID || row.launch.Trigger.Kind != "task" || row.launch.Trigger.TaskID != r.TaskID {
		return c.CancellationScope{}, fault(f.NotFound)
	}
	return c.CancellationScope{ProjectID: r.ProjectID, TaskID: r.TaskID, ExecutionID: r.ExecutionID, AgentID: row.summary.AgentID}, nil
}

func (s *CancellationService) Cancel(ctx context.Context, actor i.Actor, r c.CancelRequest) (out c.CancelResult, err error) {
	ctx, entry, err := s.begin(ctx, actor, r, false)
	if entry != nil {
		defer finishProjectCall(s.owner.state, entry, &err)
	}
	if err != nil {
		return out, err
	}
	prior, err := s.lookup(ctx, actor, r)
	if err != nil {
		return out, err
	}
	if prior.Found {
		out = c.CancelResult{Receipt: *prior.Receipt, Replayed: true}
		s.signal(prior.Receipt.Execution.ID)
		return out, nil
	}
	scope, err := s.scope(ctx, r)
	if err != nil {
		return out, err
	}
	plan, err := s.tasks.PrepareExecutionCancellation(ctx, actor, r.Meta, scope, r.ExpectedTaskVersion, r.Reason)
	if err != nil {
		return out, portError(err)
	}
	if nilPort(plan) {
		return out, fault(f.DependencyUnbound)
	}
	locks, err := s.CancelLocks(actor, scope)
	if err != nil {
		return out, err
	}
	identity, _ := r.Identity()
	locks, err = oc.NormalizeLocks(append(append(locks, commandLock(identity)), plan.RequiredLocks()...))
	if err != nil {
		return out, err
	}
	digest, _ := r.Digest(actor)
	cause, _ := f.NewCommandsCause(identity)
	result := s.owner.state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		store := s.owner.state.store
		if err := store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := requireProjectCallInTx(ctx, tx, store); err != nil {
			return err
		}
		if err := s.requireCancelScope(ctx, tx, actor, scope, i.Mutate); err != nil {
			return err
		}
		x, err := store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		old, err := loadCancellation(ctx, x, r, actor.Details().UserID, digest)
		if err != nil {
			return err
		}
		if old != nil {
			out = c.CancelResult{Receipt: *old, Replayed: true}
			return nil
		}
		row, err := loadExecution(ctx, x, scope.ExecutionID)
		if err != nil {
			return err
		}
		if !cancellationScopeMatches(row, scope) {
			return fault(f.NotFound)
		}
		if row.summary.Status.Terminal() || row.summary.CancelRequestedAt != nil {
			return fault(f.InvalidState)
		}
		if row.summary.Version != r.ExpectedVersion {
			return fault(f.VersionConflict)
		}
		protection, err := s.tasks.ApplyExecutionCancellationInTx(ctx, tx, actor, scope, plan)
		if err != nil {
			return portError(err)
		}
		if protection.Validate() != nil || protection.ID != scope.TaskID || protection.Version != r.ExpectedTaskVersion+1 {
			return fault(f.InvalidState)
		}
		summary, err := s.RequestCancelInTx(ctx, tx, actor, scope, &r.ExpectedVersion)
		if err != nil {
			return err
		}
		out.Receipt = c.CancelReceipt{Execution: taskExecutionSummary(summary), Task: protection}
		if err = loadTaskWaiting(ctx, x, &out.Receipt.Execution); err != nil {
			return err
		}
		raw, err := json.Marshal(out.Receipt)
		if err != nil {
			return unavailable(err)
		}
		tag, err := x.Exec(ctx, `INSERT INTO agenteam_execution.cancel_commands(project_id,task_id,execution_id,idempotency_key,user_id,request_digest,receipt,receipt_digest,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,clock_timestamp())`, r.ProjectID.String(), r.TaskID, r.ExecutionID.String(), r.Meta.IdempotencyKey.String(), actor.Details().UserID, digest.String(), raw, c.TriggerInputDigest(raw).String())
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return unavailable(nil)
		}
		return ctx.Err()
	})
	if err = commitError(result); err != nil {
		return c.CancelResult{}, err
	}
	// The canonical request is committed. Signalling merely accelerates the
	// original owner; every subsequent dispatch also rechecks durable state.
	s.signal(scope.ExecutionID)
	return out, nil
}

func taskExecutionSummary(s c.Summary) c.TaskExecution {
	return c.TaskExecution{ID: s.ID, ProjectID: s.ProjectID, TaskID: s.Trigger.TaskID, AgentID: s.AgentID, Purpose: s.Purpose, Status: s.Status, Version: s.Version, CreatedAt: s.CreatedAt, StartedAt: s.StartedAt, CompletedAt: s.CompletedAt, CancelRequestedAt: s.CancelRequestedAt, SnapshotID: s.SnapshotID}
}

var _ c.CancellationInTx = (*CancellationService)(nil)
var _ c.TaskExecutionCancellation = (*CancellationService)(nil)
