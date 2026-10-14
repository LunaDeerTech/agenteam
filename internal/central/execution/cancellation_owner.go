package execution

import (
	"context"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

func (s *CancellationService) begin(ctx context.Context, actor i.Actor, r c.CancelRequest, lookup bool) (context.Context, *projectCallEntry, error) {
	if s == nil || s.owner == nil || s.owner.state == nil {
		return ctx, nil, fault(f.DependencyUnbound)
	}
	if r.Validate() != nil {
		return ctx, nil, invalid()
	}
	if err := taskReadInput(ctx, actor, r.ProjectID, r.TaskID); err != nil {
		return ctx, nil, err
	}
	digest, _ := r.Digest(actor)
	state := s.owner.state
	method := "cancel"
	if lookup {
		method = "lookup_cancel"
	}
	identity, err := f.NewCommandIdentity("execution", []string{r.ProjectID.String(), r.TaskID, r.ExecutionID.String()}, method, r.Meta.IdempotencyKey)
	if err != nil {
		return ctx, nil, err
	}
	if lookup {
		state.mu.Lock()
		for _, old := range state.owners {
			if old.cancellation != nil && sameProjectInvocationKey(old.identity, identity) && old.launchDigest == digest && old.actor.Equal(actor) && old.returned && !old.settling && old.registrationKnown && old.unknown == nil && old.outcomeErr != nil {
				if state.projectKeyBusyLocked(r.ProjectID, projectLaneService, identity, old) {
					state.mu.Unlock()
					return ctx, nil, fault(f.ResourceBusy)
				}
				state.borrowProjectCallLocked(ctx, old, method)
				state.mu.Unlock()
				return old.ctx, old, nil
			}
		}
		state.mu.Unlock()
	}
	run, done, err := state.service.begin(ctx)
	if err != nil {
		return ctx, nil, err
	}
	scope, err := s.scope(run, r)
	if err != nil {
		done()
		return run, nil, err
	}
	key := c.LaunchLookupKey{ProjectID: r.ProjectID, AgentID: scope.AgentID, IdempotencyKey: r.Meta.IdempotencyKey}
	owned := r.Clone()
	template := &projectCallEntry{actor: actor, launchKey: key, launchDigest: digest, cancellation: &owned, execution: r.ExecutionID}
	run, entry, err := state.admit(run, r.ProjectID, projectLaneService, identity, template, lookup)
	if entry == nil {
		done()
		return run, nil, err
	}
	state.mu.Lock()
	entry.serviceDone = done
	state.mu.Unlock()
	return run, entry, err
}

func (s *CancellationService) lookup(ctx context.Context, actor i.Actor, r c.CancelRequest) (out c.CancelLookup, err error) {
	scope, err := s.scope(ctx, r)
	if err != nil {
		return out, err
	}
	locks, err := s.CancelLocks(actor, scope)
	if err != nil {
		return out, err
	}
	identity, _ := r.Identity()
	locks, err = oc.NormalizeLocks(append(locks, commandLock(identity)))
	if err != nil {
		return out, err
	}
	digest, _ := r.Digest(actor)
	cause, _ := f.NewRecoveryCause("execution.cancel.lookup", r.ExecutionID.String(), digest.String())
	store := s.owner.state.store
	result := store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := requireProjectCallInTx(ctx, tx, store); err != nil {
			return err
		}
		if err := s.requireCancelScope(ctx, tx, actor, scope, i.Read); err != nil {
			return err
		}
		x, err := store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		row, err := loadExecution(ctx, x, scope.ExecutionID)
		if err != nil {
			return err
		}
		if !cancellationScopeMatches(row, scope) {
			return fault(f.NotFound)
		}
		out.Receipt, err = loadCancellation(ctx, x, r, actor.Details().UserID, digest)
		out.Found = out.Receipt != nil
		return err
	})
	if err = commitError(result); err != nil {
		return c.CancelLookup{}, err
	}
	return out, nil
}

func (s *CancellationService) LookupCancel(ctx context.Context, actor i.Actor, r c.CancelRequest) (out c.CancelLookup, err error) {
	ctx, entry, err := s.begin(ctx, actor, r, true)
	if entry != nil {
		defer finishProjectCall(s.owner.state, entry, &err)
	}
	if err != nil {
		return out, err
	}
	out, err = s.lookup(ctx, actor, r)
	if err == nil && out.Found {
		s.owner.state.recoveredProjectCall(entry, nil)
		s.signal(r.ExecutionID)
	}
	return out, err
}

// Notify is post-commit acceleration. It observes the exact canonical binding
// again, including ProcessGuard, before signalling any original runtime owner.
func (s *CancellationService) Notify(ctx context.Context, scope c.CancellationScope) error {
	if s == nil || s.owner == nil || ctx == nil || scope.Validate() != nil {
		return invalid()
	}
	state := s.owner.state
	ctx, done, err := state.service.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	locks := []f.LockRequest{projectLock(scope.ProjectID), agentLock(scope.AgentID, f.Shared), executionLock(scope.ExecutionID)}
	cause, _ := f.NewRecoveryCause("execution.cancel.notify", scope.ExecutionID.String(), scope.ProjectID.String())
	requested := false
	result := state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := state.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := state.proof.RequireCurrentProcessInTx(ctx, tx, state.process); err != nil {
			return portError(err)
		}
		x, err := state.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		row, err := loadExecution(ctx, x, scope.ExecutionID)
		if err != nil {
			return err
		}
		if !cancellationScopeMatches(row, scope) {
			return fault(f.NotFound)
		}
		requested = row.summary.CancelRequestedAt != nil && !row.summary.Status.Terminal()
		return ctx.Err()
	})
	if err := commitError(result); err != nil {
		return err
	}
	if requested {
		s.signal(scope.ExecutionID)
	}
	return nil
}

func (s *CancellationService) signal(execution i.ExecutionID) {
	state := s.owner.state
	state.direct.state.mu.Lock()
	if run := state.direct.state.calls[execution]; run != nil {
		run.cancel()
	}
	state.direct.state.mu.Unlock()
	state.preparation.state.mu.Lock()
	if run := state.preparation.state.calls[execution]; run != nil {
		run.cancel()
	}
	state.preparation.state.mu.Unlock()
	if state.associated != nil && state.associated.state != nil {
		state.associated.state.notifyCancellation(execution)
	}
}
