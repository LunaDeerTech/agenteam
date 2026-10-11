package execution

import (
	"context"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

func validateAssociated(project i.ProjectID, dispatch c.AssociatedDispatch) error {
	if project.Validate() != nil || dispatch.ExecutionID.Validate() != nil || dispatch.AgentID.Validate() != nil || dispatch.Key.Validate() != nil || dispatch.Digest.Validate() != nil {
		return invalid()
	}
	if _, err := f.ParseID[struct{}](dispatch.DispatchID); err != nil {
		return invalid()
	}
	return nil
}

// inspect never reads Scheduler tables or treats a public tuple as an Actor
// grant. Its trusted caller supplied the association; these are independently
// checked Execution facts. Driver-specific current authority is still checked
// by the original preparation and DirectText methods before their writes/I/O.
func (s *associatedExecutorState) inspect(ctx context.Context, project i.ProjectID, dispatch c.AssociatedDispatch) (associatedFacts, error) {
	if ctx == nil || validateAssociated(project, dispatch) != nil {
		return associatedFacts{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return associatedFacts{}, err
	}
	key := c.LaunchLookupKey{ProjectID: project, AgentID: dispatch.AgentID, IdempotencyKey: dispatch.Key}
	command, err := key.Command()
	if err != nil {
		return associatedFacts{}, err
	}
	locks, err := oc.NormalizeLocks(append(dispatchObservationLocks(project, dispatch.AgentID), commandLock(command), executionLock(dispatch.ExecutionID)))
	if err != nil {
		return associatedFacts{}, portError(err)
	}
	id, err := f.NewID[struct{}]()
	if err != nil {
		return associatedFacts{}, unavailable(err)
	}
	cause, err := f.NewRecoveryCause("execution.associated-read", dispatch.ExecutionID.String(), id.String())
	if err != nil {
		return associatedFacts{}, err
	}
	store := s.preparation.state.store
	var facts associatedFacts
	result := store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		row, err := loadLaunch(ctx, x, key)
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if row == nil {
			return fault(f.NotFound)
		}
		if row.digest != dispatch.Digest {
			return fault(f.IdempotencyKeyReused)
		}
		if row.summary.ID != dispatch.ExecutionID || row.summary.ProjectID != project || row.summary.AgentID != dispatch.AgentID || row.launch.ProjectID != project || row.launch.AgentID != dispatch.AgentID || row.launch.Meta.IdempotencyKey != dispatch.Key || row.launch.Trigger.Kind != "task" || row.launch.Lineage.DispatchID != dispatch.DispatchID {
			return fault(f.ConfirmationStale)
		}
		facts.status = row.summary.Status
		if row.summary.Status.Terminal() || row.summary.Status == c.Running || row.summary.Status == c.Waiting {
			return ctx.Err()
		}
		input, err := loadPreparationInput(ctx, x, dispatch.ExecutionID)
		if err != nil {
			return err
		}
		claim, err := loadPreparationClaim(ctx, x, dispatch.ExecutionID)
		if err != nil {
			return err
		}
		if claim != nil && (claim.project != project || claim.agent != dispatch.AgentID || claim.execution != dispatch.ExecutionID) {
			return unavailable(nil)
		}
		facts.claimTerminal = claim != nil && claim.phase == "terminal"
		if input != nil {
			request := c.PreparationRequest{ExecutionID: dispatch.ExecutionID, Launch: row.launch.Clone()}
			if row.summary.Status != c.Preparing || !input.input.Fields().Request.Equal(request) || claim == nil || input.claim.execution != claim.execution || input.claim.project != claim.project || input.claim.agent != claim.agent || input.claim.attempt != claim.attempt || input.claim.process != claim.process || input.claim.fence != claim.fence {
				return unavailable(nil)
			}
			// A valid foreign-process input is historical evidence, not this
			// process's permission to adopt another live preparation/Loop.
			facts.inputReady = facts.claimTerminal && input.claim.process == s.preparation.state.process
		}
		return ctx.Err()
	})
	if err = commitError(result); err != nil {
		return associatedFacts{}, err
	}
	if err = ctx.Err(); err != nil {
		return associatedFacts{}, err
	}
	return facts, nil
}

// Only the original private context token can associate a retained driver
// call with this executor. Matching an EID, request or Actor alone cannot do
// that. No foreign call is cancelled, recovered or counted as our ownership.
func (s *associatedExecutorState) ownedCalls(job *associatedCall) associatedOwners {
	var result associatedOwners
	p := s.preparation.state
	p.mu.Lock()
	if run := p.calls[job.dispatch.ExecutionID]; run != nil {
		if run.ctx != nil && run.ctx.Value(associatedContextKey{}) == job {
			result.preparation, result.err = true, run.unresolved
		} else {
			result.foreign = true
		}
	}
	p.mu.Unlock()
	d := s.direct.state
	d.mu.Lock()
	if run := d.calls[job.dispatch.ExecutionID]; run != nil {
		if run.ctx != nil && run.ctx.Value(associatedContextKey{}) == job {
			result.direct, result.uncertainty = true, run.uncertainty
			if run.unresolved != nil {
				result.err = run.unresolved
			}
		} else {
			result.foreign = true
		}
	}
	d.mu.Unlock()
	return result
}
