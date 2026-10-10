package agent

import (
	"context"
	"slices"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func (s *Service) CreateAgent(ctx context.Context, actor i.Actor, meta f.CommandMeta, project i.ProjectID, request c.AgentCreate) (c.AgentMutation, error) {
	return s.execute(ctx, commandInput{actor: actor, meta: meta, project: project, target: request.Fields().AgentID, name: c.CreateAgentCommand, create: &request})
}
func (s *Service) UpdateAgent(ctx context.Context, actor i.Actor, meta f.CommandMeta, project i.ProjectID, target i.AgentID, request c.AgentUpdate) (c.AgentMutation, error) {
	return s.execute(ctx, commandInput{actor: actor, meta: meta, project: project, target: target, name: c.UpdateAgentCommand, update: &request})
}

func (s *Service) execute(ctx context.Context, in commandInput) (c.AgentMutation, error) {
	empty := c.AgentMutation{}
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return empty, err
	}
	defer done()
	if err = in.validate(); err != nil {
		return empty, err
	}
	r, replay, result := s.prepareCommand(ctx, in)
	if result.State() == f.Unknown {
		return s.confirmUnknown(ctx, in, result)
	}
	if err = commitError(result); err != nil {
		return empty, err
	}
	if replay != nil {
		return replay.Clone(), nil
	}
	if r == nil {
		return empty, unavailable(nil)
	}
	tools, err := s.state.deps.Tools.DiscoverConfigurationTools(ctx, toolRequest(in, r))
	if err != nil {
		return empty, portError(err)
	}
	if nilPort(tools) {
		return empty, unavailable(nil)
	}
	resolved := slices.Clone(tools.ResolvedToolIDs())
	// Even no-op updates must resolve and validate their current references.
	// Updates cannot regain a creation default that the user previously removed.
	if in.create == nil && !slices.Equal(resolved, r.After.Fields().AllowedToolIDs) {
		return empty, fault(f.InvalidState)
	}
	r, replay, result = s.freezeTools(ctx, in, r, resolved)
	if result.State() == f.Unknown {
		return s.confirmUnknown(ctx, in, result)
	}
	if err = commitError(result); err != nil {
		return empty, err
	}
	if replay != nil {
		return replay.Clone(), nil
	}
	if r == nil {
		return empty, unavailable(nil)
	}
	deps, err := s.discoverDependencies(ctx, in, r, tools)
	if err != nil {
		return empty, err
	}
	out, result := s.commitCommand(ctx, in, r, deps)
	if result.State() == f.Unknown {
		return s.confirmUnknown(ctx, in, result)
	}
	if err = commitError(result); err != nil {
		return empty, err
	}
	if out == nil {
		return empty, unavailable(nil)
	}
	// A committed business result survives cancellation during delivery.
	return out.Clone(), nil
}

func (s *Service) commitCommand(ctx context.Context, in commandInput, prepared *commandRecord, deps *commandDependencies) (*c.AgentMutation, f.CommitResult) {
	var out *c.AgentMutation
	cause, _ := f.NewCommandsCause(in.identity)
	result := s.state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.state.store.AcquireAll(ctx, tx, deps.locks); err != nil {
			return portError(err)
		}
		x, r, replay, err := s.currentCommand(ctx, tx, in)
		if err != nil {
			return err
		}
		if replay != nil {
			out = replay
			return nil
		}
		if err = s.requireMutation(ctx, tx, in); err != nil {
			return err
		}
		expected, err := recordMapping(prepared)
		if err != nil {
			return err
		}
		actual, err := recordMapping(r)
		if err != nil || actual != expected {
			return fault(f.VersionConflict)
		}
		if err = checkPreimage(ctx, x, r); err != nil {
			return err
		}
		if err = s.requireDependencies(ctx, tx, deps); err != nil {
			return err
		}
		witnessCtx, err := s.state.deps.Authority.writeCanonical(ctx, tx, in.actor, r)
		if err != nil {
			return err
		}
		if err = s.applyDependencies(witnessCtx, tx, in, deps); err != nil {
			return err
		}
		eventIDs := []event.EventID{}
		if len(r.ChangedFields) > 0 {
			entry, key, err := recordAudit(r, in.actor)
			if err != nil {
				return err
			}
			auditReceipt, err := s.state.deps.Audit.AppendInTx(witnessCtx, tx, entry, key)
			if err != nil {
				return portError(err)
			}
			if auditReceipt.AuditID.Validate() != nil || auditReceipt.CreatedAt.Validate() != nil {
				return unavailable(nil)
			}
			eventReceipt, err := s.state.deps.Events.AppendEventInTx(witnessCtx, tx, in.actor, deps.event, deps.append)
			if err != nil {
				return portError(err)
			}
			if eventReceipt.EventID != deps.event.Header().EventID || eventReceipt.Sequence.Validate() != nil {
				return unavailable(nil)
			}
			eventIDs = append(eventIDs, eventReceipt.EventID)
		}
		receipt, err := c.NewAgentMutation(c.AgentMutationFields{Agent: r.After, Changed: len(r.ChangedFields) > 0, EventIDs: eventIDs})
		if err != nil {
			return unavailable(err)
		}
		if err = s.state.deps.Activity.TouchActivityInTx(witnessCtx, tx, in.actor); err != nil {
			return portError(err)
		}
		if err = completeCommand(ctx, x, r, receipt); err != nil {
			return err
		}
		out = &receipt
		return nil
	})
	return out, result
}

// At most one confirmation for one execute call: every Unknown immediately
// returns through this function. It never starts another writer. Register the
// independent <=3s context with the same call owner so Stop cancels it and Drain
// still waits for the original physical Tx/rows and confirmation to return.
func (s *Service) confirmUnknown(ctx context.Context, in commandInput, original f.CommitResult) (c.AgentMutation, error) {
	empty := c.AgentMutation{}
	confirmation, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	confirmation, done, err := s.begin(confirmation)
	if err != nil {
		return empty, commitError(original)
	}
	defer done()
	var receipt *c.AgentMutation
	var absent bool
	cause, _ := f.NewCommandsCause(in.identity)
	result := s.state.store.WithinTx(confirmation, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.state.store.AcquireAll(ctx, tx, []f.LockRequest{commandLock(in.identity), userLock(in.actor, f.Shared), projectLock(in.project, f.Shared)}); err != nil {
			return portError(err)
		}
		_, r, replay, err := s.currentCommand(ctx, tx, in)
		if err != nil {
			return err
		}
		receipt = replay
		absent = r == nil
		return nil
	})
	if result.State() == f.Committed {
		if receipt != nil {
			return receipt.Clone(), nil
		}
		if absent {
			// This is an actual serialized no-row observation under the original
			// Command EX, not a timeout, canceled lookup or inferred absence.
			err := f.NewFault(f.DependencyUnavailable, f.NotCommitted)
			err.RetryHint = "retry_same_key"
			if original.AttemptID().Validate() == nil {
				err.CauseID = original.AttemptID().String()
			}
			return empty, err.WithCause(commitFailure{original})
		}
	}
	return empty, commitError(original)
}
