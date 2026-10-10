package execution

import (
	"context"
	"encoding/json"
	"reflect"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	object "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// The matcher consumes only facts supplied by this Authority's original live
// DirectText owner. It cannot turn a valid request or a preparing input into a
// running grant, and never reads Model/Agent owner tables itself.
func runtimeModelRequestMatches(request mc.ConsumerRequest, facts directTextModelFacts, final bool) error {
	if request.Validate() != nil || facts.Snapshot.Validate() != nil || facts.Round.Validate() != nil {
		return invalid()
	}
	fields := facts.Snapshot.Fields().Context.Input().Fields()
	model, round := fields.Model, facts.Round.Fields()
	consumer := request.Consumer
	if consumer.Kind != mc.AgentConsumer || consumer.Purpose != mc.AgentGeneration || !consumer.Equal(model.Consumer) || !request.LeaseOwner.Equal(model.LeaseOwner) || request.LeaseOwner.Details().Kind != sc.ExecutionOwner || request.SnapshotID != model.Snapshot.ID || request.LeaseID == nil || model.CredentialLease == nil || *request.LeaseID != model.CredentialLease.LeaseID || round.ExecutionID != fields.Request.ExecutionID || round.ContextDigest != facts.Snapshot.Fields().Context.Digest() {
		return fault(f.Forbidden)
	}
	actor, err := i.NewAgentRun(consumer.ProjectID, *consumer.AgentID, *consumer.ExecutionID)
	if err != nil {
		return invalid()
	}
	switch request.Action {
	case mc.InvokeConsumer, mc.ReadCredentialConsumer:
		if !request.Actor.Equal(actor) {
			return fault(f.Forbidden)
		}
	case mc.FinalizeConsumer:
		d := request.Actor.Details()
		if request.Attempt == nil || d.Kind != i.Service || d.ServiceName != i.ModelRuntime || d.ProjectID != consumer.ProjectID.String() || d.CauseRef != request.Attempt.InvocationID.String() {
			return fault(f.Forbidden)
		}
	case mc.RetireConsumer:
		if !request.Actor.Equal(actor) || request.TerminalVersion == nil || final && (*request.TerminalVersion != facts.Summary.Version || !facts.Summary.Status.Terminal() || facts.Summary.CompletedAt == nil) {
			return fault(f.Forbidden)
		}
		return nil
	default:
		return fault(f.DependencyUnbound)
	}
	if request.TerminalVersion != nil {
		return fault(f.Forbidden)
	}
	if request.CallID == nil || *request.CallID != round.CallID || request.Input == nil || !reflect.DeepEqual(request.Input.Clone(), round.Input.Clone()) {
		return fault(f.Forbidden)
	}
	if request.Attempt != nil {
		process, err := f.ParseID[object.Process](facts.ProcessID.String())
		if err != nil || request.Attempt.ProcessID != process || request.Attempt.CallID != round.CallID {
			return fault(f.Forbidden)
		}
	}
	return nil
}

func runtimeModelMapping(facts directTextModelFacts) (f.Digest, error) {
	raw, err := json.Marshal(struct {
		Format, Process string
		Snapshot, Round f.Digest
	}{"execution-model-runtime-v1", facts.ProcessID.String(), facts.Snapshot.Digest(), facts.Round.Digest()})
	if err != nil {
		return "", unavailable(nil)
	}
	return ec.TriggerInputDigest(raw), nil
}

func (a *Authority) discoverRuntimeModel(ctx context.Context, request mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
	if ctx.Value(directTextContextKey{}) == nil {
		// The old preparation-only consumer remains explicitly unbound for
		// runtime actions until an actual startup owner supplies this context.
		return mc.ConsumerDependencies{}, fault(f.DependencyUnbound)
	}
	facts, err := a.directTextModelPlanningScope(ctx, request.Action == mc.RetireConsumer)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	if err = runtimeModelRequestMatches(request, facts, false); err != nil {
		return mc.ConsumerDependencies{}, err
	}
	binding, err := mc.ConsumerBinding(request)
	if err != nil {
		return mc.ConsumerDependencies{}, portError(err)
	}
	mapping, err := runtimeModelMapping(facts)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	details := mc.ConsumerDependencyDetails{Binding: binding, Mapping: mapping, Locks: facts.Locks}
	if request.Action == mc.InvokeConsumer {
		details.RetryPolicy = &mc.RetryPolicy{Class: mc.AgentRetry, Categories: []mc.ErrorCategory{"rate_limited", "provider_unavailable", "timeout", "network", "provider_error"}}
	}
	if err = ctx.Err(); err != nil {
		return mc.ConsumerDependencies{}, err
	}
	return mc.NewConsumerDependencies(a.state.modelIssuer, details)
}

func (a *Authority) validateRuntimeModelInTx(ctx context.Context, tx f.Tx, request mc.ConsumerRequest, plan mc.ConsumerDependencies) error {
	if request.Validate() != nil || plan.Validate() != nil {
		return invalid()
	}
	if _, err := a.state.store.InTx(tx); err != nil {
		return portError(err)
	}
	if err := a.state.store.RequireHeldLocks(ctx, tx, plan.RequiredLocks()); err != nil {
		return portError(err)
	}
	var facts directTextModelFacts
	var err error
	if request.Action == mc.RetireConsumer {
		facts, err = a.directTextRetirementScope(ctx, tx)
	} else {
		facts, err = a.directTextModelScope(ctx, tx, request.Action == mc.FinalizeConsumer)
	}
	if err != nil {
		return err
	}
	if err = runtimeModelRequestMatches(request, facts, true); err != nil {
		return err
	}
	binding, err := mc.ConsumerBinding(request)
	if err != nil {
		return portError(err)
	}
	mapping, err := runtimeModelMapping(facts)
	if err != nil || !plan.Matches(a.state.modelIssuer, binding, mapping) {
		return fault(f.ConfirmationStale)
	}
	if request.Action == mc.InvokeConsumer || request.Action == mc.ReadCredentialConsumer {
		// Current Project and Agent checks are independent of frozen Model
		// selection. Changing a selection does not rewrite this Snapshot.
		if _, err = a.directTextCurrentAgent(ctx, tx); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (a *Authority) requireModelCurrentConfiguration(ctx context.Context, tx f.Tx, request ac.ExecutionConfigurationRequest) error {
	facts, err := a.directTextConfigurationScope(ctx, tx)
	if err != nil {
		return err
	}
	original := facts.Snapshot.Fields().Context.Input().Fields().Request
	actor, err := i.NewAgentRun(original.Launch.ProjectID, original.Launch.AgentID, original.ExecutionID)
	if err != nil || !actor.Equal(request.Actor) || request.Stage != ac.ExecutionConfigurationCurrent || request.ProjectID != original.Launch.ProjectID || request.AgentID != original.Launch.AgentID || request.ExecutionID != original.ExecutionID {
		return fault(f.Forbidden)
	}
	return ctx.Err()
}
