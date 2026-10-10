package projectvariable

import (
	"context"
	"errors"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func (s *SecretService) CreateSecretVariable(ctx context.Context, actor i.Actor, meta f.CommandMeta, project c.ProjectID, in c.SecretVariableCreate) (c.SecretVariableMutation, error) {
	return s.executeSecret(ctx, actor, meta, project, in.Fields().ID, c.SecretCreateCommand, &in, nil)
}
func (s *SecretService) UpdateSecretVariable(ctx context.Context, actor i.Actor, meta f.CommandMeta, project c.ProjectID, id c.VariableID, in c.SecretVariableUpdate) (c.SecretVariableMutation, error) {
	return s.executeSecret(ctx, actor, meta, project, id, c.SecretUpdateCommand, nil, &in)
}
func (s *SecretService) DeleteSecretVariable(ctx context.Context, actor i.Actor, meta f.CommandMeta, project c.ProjectID, id c.VariableID) (c.SecretVariableMutation, error) {
	return s.executeSecret(ctx, actor, meta, project, id, c.SecretDeleteCommand, nil, nil)
}

type secretPreparedCall struct {
	raw         sc.PreparedProjectVariableWrite
	basis       sc.ProjectVariableWritePlan
	preparation sc.ProjectVariablePreparationFields
	plan        *secretMutationPlan
	event       event.Event
	append      oc.AppendPlan
	locks       []f.LockRequest
	ctx         context.Context
}

func (s *SecretService) prepareSecret(ctx context.Context, intent sc.ProjectVariableIntent) (*secretPreparedCall, error) {
	st := s.state()
	request := intent.Fields().Request
	r := request.Fields()
	basis, err := st.deps.Writes.Discover(ctx, request)
	if err != nil {
		return nil, err
	}
	raw, err := st.deps.Secrets.PrepareProjectVariableWrite(ctx, intent, basis)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(raw) {
		return nil, internal(nil)
	}
	keep := false
	defer func() {
		if !keep {
			raw.Destroy()
		}
	}()
	projection, err := raw.Preparation()
	if err != nil {
		return nil, portError(err)
	}
	preparation, err := projection.Fields()
	if err != nil {
		return nil, portError(err)
	}
	locks, err := raw.RequiredLocks()
	if err != nil {
		return nil, portError(err)
	}
	locks, err = oc.NormalizeLocks(locks)
	if err != nil {
		return nil, portError(err)
	}
	prepared := &secretPreparedCall{raw: raw, basis: basis, preparation: preparation, locks: locks, ctx: ctx}
	// Match in a short read transaction before constructing any new event. It
	// also authenticates the actual private D04 handle before trusting its safe
	// projection for a discovery witness. No durable D10 planned row is written.
	result := st.store.WithinTx(ctx, commandCause(r.Identity), func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		observed, err := st.deps.Secrets.MatchProjectVariableIntentInTx(ctx, tx, raw)
		if err != nil {
			return portError(err)
		}
		x, err := st.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		saved, err := loadSecretCommand(ctx, x, r.ProjectID, secretRequestCommand(request), r.Identity.Key())
		if err != nil {
			return err
		}
		if observed.Observed() {
			if saved == nil || !secretRecordMatchesRequest(saved, request) || !sameSecretObservation(saved.Observation, observed) {
				return internal(nil)
			}
			return nil
		}
		if observed.Validate() != nil || saved != nil {
			return internal(nil)
		}
		if err = st.deps.Writes.CheckInTx(ctx, tx, request, basis, sc.ProjectVariableNewWrite); err != nil {
			return portError(err)
		}
		var before *secretVariableRow
		if r.Kind != sc.Create {
			before, err = loadSecretVariable(ctx, x, r.ProjectID, r.VariableID, false)
			if err != nil {
				return err
			}
		}
		at, err := dbNow(ctx, x)
		if err != nil {
			return err
		}
		operation, err := f.NewID[c.Operation]()
		if err != nil {
			return unavailable(err)
		}
		history, err := f.NewID[c.Operation]()
		if err != nil {
			return unavailable(err)
		}
		plan, changed, err := planSecretVariable(intent, before, at, operation, history)
		if err != nil {
			return err
		}
		if err = checkSecretPreimage(ctx, x, plan); err != nil {
			return err
		}
		if changed {
			plan.Event, err = f.NewID[event.EventIdentity]()
			if err != nil {
				return unavailable(err)
			}
		}
		prepared.plan = plan
		return nil
	})
	if err = txError(result); err != nil {
		return nil, err
	}
	if prepared.plan != nil && len(prepared.plan.Fields) != 0 {
		prepared.ctx, err = secretDiscoveryContext(ctx, st.deps.Authority, prepared.plan, preparation, locks, intent.Fields().ValuePresent)
		if err != nil {
			return nil, err
		}
		prepared.event, err = st.deps.VariableEvents.NewSecretVariableChanged(secretPlanHeader(prepared.plan), secretPlanPayload(prepared.plan))
		if err != nil {
			return nil, internal(err)
		}
		prepared.append, err = st.deps.Events.PrepareAppend(prepared.ctx, r.Actor, prepared.event)
		if err != nil {
			return nil, portError(err)
		}
		prepared.locks, err = oc.NormalizeLocks(append(append([]f.LockRequest{}, locks...), prepared.append.Locks()...))
		if err != nil {
			return nil, portError(err)
		}
	}
	if err = ctx.Err(); err != nil {
		return nil, canceled(err)
	}
	keep = true
	return prepared, nil
}

func (s *SecretService) executeSecret(ctx context.Context, actor i.Actor, meta f.CommandMeta, project c.ProjectID, id c.VariableID, command c.SecretCommandName, create *c.SecretVariableCreate, update *c.SecretVariableUpdate) (c.SecretVariableMutation, error) {
	empty := c.SecretVariableMutation{}
	ctx, call, done, err := s.beginProject(ctx, project, mutationCall)
	if err != nil {
		return empty, err
	}
	defer done()
	if err = readInput(actor, project); err != nil {
		return empty, err
	}
	if err = c.ValidateSecretCommandMeta(command, meta); err != nil {
		return empty, err
	}
	if id.Validate() != nil {
		return empty, fault(f.InvalidArgument)
	}
	identity, err := c.SecretVariableCommandIdentity(project, command, meta.IdempotencyKey)
	if err != nil {
		return empty, err
	}
	kind, err := secretMutationKind(command)
	if err != nil {
		return empty, err
	}
	request, err := sc.NewProjectVariableWriteRequest(sc.ProjectVariableWriteFields{Actor: actor, ProjectID: project, VariableID: id, Identity: identity, Kind: kind, ExpectedVersion: meta.ExpectedVersion})
	if err != nil {
		return empty, portError(err)
	}
	intent, err := secretIntent(request, create, update)
	if err != nil {
		return empty, err
	}
	defer intent.Destroy()
	for attempt := 0; attempt < 2; attempt++ {
		prepared, err := s.prepareSecret(ctx, intent)
		if err != nil {
			var changed secretPreparationChanged
			var problem *f.Fault
			if attempt == 0 && errors.As(err, &changed) && errors.As(err, &problem) && problem.CommitState == f.NotCommitted && ctx.Err() == nil {
				continue
			}
			return empty, err
		}
		out, result := s.applySecret(prepared, intent)
		if result.State() == f.Unknown {
			out, err = s.confirmSecretUnknown(ctx, call, request, prepared.raw, result)
			prepared.raw.Destroy()
			return out, err
		}
		err = txError(result)
		prepared.raw.Destroy()
		if err == nil {
			if out.Validate() != nil {
				return empty, internal(nil)
			}
			return out, nil
		}
		var changed secretPreparationChanged
		if attempt == 0 && result.State() == f.NotCommitted && errors.As(err, &changed) && ctx.Err() == nil {
			continue
		}
		return empty, err
	}
	return empty, fault(f.ResourceBusy)
}
func (s *SecretService) applySecret(prepared *secretPreparedCall, intent sc.ProjectVariableIntent) (c.SecretVariableMutation, f.CommitResult) {
	st := s.state()
	request := intent.Fields().Request
	r := request.Fields()
	var out c.SecretVariableMutation
	result := st.store.WithinTx(prepared.ctx, commandCause(r.Identity), func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, prepared.locks); err != nil {
			return portError(err)
		}
		observation, err := st.deps.Secrets.MatchProjectVariableIntentInTx(ctx, tx, prepared.raw)
		if err != nil {
			return portError(err)
		}
		x, err := st.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		saved, err := loadSecretCommand(ctx, x, r.ProjectID, secretRequestCommand(request), r.Identity.Key())
		if err != nil {
			return err
		}
		if observation.Observed() {
			if saved == nil || !secretRecordMatchesRequest(saved, request) || !sameSecretObservation(saved.Observation, observation) {
				return internal(nil)
			}
			out = saved.Receipt
			return nil
		}
		if observation.Validate() != nil || saved != nil || prepared.plan == nil {
			return internal(nil)
		}
		if err = st.deps.Writes.CheckInTx(ctx, tx, request, prepared.basis, sc.ProjectVariableNewWrite); err != nil {
			return portError(err)
		}
		if err = checkSecretPreimage(ctx, x, prepared.plan); err != nil {
			return err
		}
		observation, err = st.deps.Secrets.ApplyProjectVariableWriteInTx(ctx, tx, prepared.raw)
		if err != nil {
			return portError(err)
		}
		if err = validateSecretApplied(prepared.plan, prepared.preparation, observation, intent.Fields().ValuePresent); err != nil {
			return err
		}
		if len(prepared.plan.Fields) == 0 {
			record, err := secretCompletedRecord(prepared.plan, observation, intent.Fields(), nil)
			if err != nil {
				return err
			}
			if err = insertSecretCommand(ctx, x, record); err != nil {
				return err
			}
			out = record.Receipt
			return nil
		}
		if err = applySecretPlan(ctx, x, prepared.plan, observation); err != nil {
			return err
		}
		entry, key, err := secretPlanAudit(prepared.plan)
		if err != nil {
			return err
		}
		mutationCtx, err := secretMutationContext(ctx, st.deps.Authority, tx, observation, entry, key)
		if err != nil {
			return err
		}
		auditReceipt, err := st.deps.Audit.AppendInTx(mutationCtx, tx, entry, key)
		if err != nil {
			return portError(err)
		}
		completedCtx, err := secretAuditCompletedContext(mutationCtx, auditReceipt)
		if err != nil {
			return err
		}
		record, err := secretCompletedRecord(prepared.plan, observation, intent.Fields(), &auditReceipt.AuditID)
		if err != nil {
			return err
		}
		if err = insertSecretCommand(ctx, x, record); err != nil {
			return err
		}
		receipt, err := st.deps.Events.AppendEventInTx(completedCtx, tx, r.Actor, prepared.event, prepared.append)
		if err != nil {
			return portError(err)
		}
		if receipt.EventID != prepared.plan.Event || receipt.Sequence.Validate() != nil {
			return internal(nil)
		}
		if err = st.deps.Activity.TouchActivityInTx(ctx, tx, r.Actor); err != nil {
			return portError(err)
		}
		out = record.Receipt
		return nil
	})
	if result.State() != f.Committed {
		return c.SecretVariableMutation{}, result
	}
	return out, result
}

func (s *SecretService) confirmSecretUnknown(ctx context.Context, entry *call, request sc.ProjectVariableWriteRequest, prepared sc.PreparedProjectVariableWrite, original f.CommitResult) (c.SecretVariableMutation, error) {
	empty := c.SecretVariableMutation{}
	confirm, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	token := &confirmation{cancel: cancel}
	st := s.state()
	st.mu.Lock()
	entry.confirmations[token] = struct{}{}
	if st.stopped || entry.stopRequested {
		cancel()
	}
	st.mu.Unlock()
	defer func() { cancel(); st.mu.Lock(); delete(entry.confirmations, token); st.mu.Unlock() }()
	locks, err := prepared.RequiredLocks()
	if err != nil {
		return empty, txError(original)
	}
	r := request.Fields()
	var out c.SecretVariableMutation
	result := st.store.WithinTx(confirm, commandCause(r.Identity), func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		// Retain the original prepared intent. Identity-only Lookup could match
		// another semantic command committed after an unobserved rollback.
		observation, err := st.deps.Secrets.MatchProjectVariableIntentInTx(ctx, tx, prepared)
		if err != nil {
			return portError(err)
		}
		if !observation.Observed() {
			return fault(f.NotFound)
		}
		x, err := st.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		record, err := loadSecretCommand(ctx, x, r.ProjectID, secretRequestCommand(request), r.Identity.Key())
		if err != nil {
			return err
		}
		if record == nil || !secretRecordMatchesRequest(record, request) || !sameSecretObservation(record.Observation, observation) {
			return internal(nil)
		}
		out = record.Receipt
		return nil
	})
	if result.State() == f.Committed && confirm.Err() == nil && out.Validate() == nil {
		return out, nil
	}
	return empty, txError(original)
}

var _ c.SecretCommands = (*SecretService)(nil)
