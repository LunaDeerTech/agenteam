package execution

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func (a *Authority) RequireModelCaptureDiscoveryInTx(ctx context.Context, tx f.Tx, request mc.ExecutionModelCaptureRequest) (mc.ExecutionModelCaptureScope, error) {
	project, binding, err := a.requireResourceDiscovery(ctx, tx, request.ProjectID, request.AgentID, request.ExecutionID, "model")
	if err != nil {
		return mc.ExecutionModelCaptureScope{}, err
	}
	// Only this successful source proof lets the original Model discovery mint
	// its Consumer plan. That plan grants nothing until the live Tx rechecks it.
	d := ctx.Value(preparationDiscoveryKey{}).(*preparationDiscovery)
	d.modelMu.Lock()
	d.modelBinding = binding
	d.modelMu.Unlock()
	scope := mc.ExecutionModelCaptureScope{Project: project, AttemptBinding: binding}
	if !d.recovering {
		d.driver.mu.Lock()
		run := d.driver.calls[request.ExecutionID]
		if run == nil || run.returned || run.unresolved != nil || run.claim == nil || *run.claim != d.claim {
			d.driver.mu.Unlock()
			return mc.ExecutionModelCaptureScope{}, fault(f.Forbidden)
		}
		owned := scope.Clone()
		run.modelScope = &owned
		d.driver.mu.Unlock()
	}
	return scope, nil
}

func (a *Authority) RequireModelCaptureInTx(ctx context.Context, tx f.Tx, request mc.ExecutionModelCaptureRequest) (mc.ExecutionModelCaptureFacts, error) {
	w, binding, err := a.requireResourceCapture(ctx, tx, request.ProjectID, request.AgentID, request.ExecutionID)
	if err != nil {
		return mc.ExecutionModelCaptureFacts{}, err
	}
	core := w.agent.Fields().Core
	return mc.ExecutionModelCaptureFacts{Scope: mc.ExecutionModelCaptureScope{Project: clonePreparationProject(w.project), AttemptBinding: binding}, AgentVersion: core.Version, Selection: mc.AgentModelSelection{ModelID: core.ModelRef, ReasoningEffort: core.ReasoningEffort, ApprovalModelID: core.ApprovalModelRef}.Clone()}, nil
}

// Preparing permits resolution of the direct primary Model only. Invocation,
// credential reads and retirement require their actual runtime owner, not an
// AgentRun value or this preparation's capture witness.
func preparationModelConsumer(request mc.ConsumerRequest) (mc.ExecutionModelCaptureRequest, error) {
	if request.Validate() != nil {
		return mc.ExecutionModelCaptureRequest{}, invalid()
	}
	c := request.Consumer
	if request.Action != mc.ResolveConsumer || c.Kind != mc.AgentConsumer || c.Purpose != mc.AgentGeneration || c.MeetingID != "" || c.OperationID != "" || c.AgentID == nil || c.ExecutionID == nil || request.Resolve == nil || request.CallID != nil {
		return mc.ExecutionModelCaptureRequest{}, fault(f.DependencyUnbound)
	}
	r := request.Resolve
	owner := request.LeaseOwner.Details()
	if r.Source != mc.CurrentSelectionSource || r.Selection == nil || r.Selection.Kind != "direct" || r.ModelRef == nil || owner.Kind != sc.ExecutionOwner || owner.ID != c.ExecutionID.String() {
		return mc.ExecutionModelCaptureRequest{}, fault(f.DependencyUnbound)
	}
	actor, err := i.NewAgentRun(c.ProjectID, *c.AgentID, *c.ExecutionID)
	if err != nil || !actor.Equal(request.Actor) {
		return mc.ExecutionModelCaptureRequest{}, fault(f.Forbidden)
	}
	return mc.ExecutionModelCaptureRequest{ProjectID: c.ProjectID, AgentID: *c.AgentID, ExecutionID: *c.ExecutionID}, nil
}

func (a *Authority) Discover(ctx context.Context, request mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
	if a == nil || a.state == nil {
		return mc.ConsumerDependencies{}, fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return mc.ConsumerDependencies{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return mc.ConsumerDependencies{}, err
	}
	source, err := preparationModelConsumer(request)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	d, ok := ctx.Value(preparationDiscoveryKey{}).(*preparationDiscovery)
	if !ok || d == nil || !d.active.Load() || d.recovering || d.kind != "model" || d.driver == nil || d.driver.authority.state != a.state || d.request.ExecutionID != source.ExecutionID || d.request.Launch.ProjectID != source.ProjectID || d.request.Launch.AgentID != source.AgentID {
		return mc.ConsumerDependencies{}, fault(f.Forbidden)
	}
	d.modelMu.Lock()
	mapping := d.modelBinding
	d.modelMu.Unlock()
	if mapping.Validate() != nil {
		return mc.ConsumerDependencies{}, fault(f.Forbidden)
	}
	binding, err := mc.ConsumerBinding(request)
	if err != nil {
		return mc.ConsumerDependencies{}, portError(err)
	}
	return mc.NewConsumerDependencies(a.state.modelIssuer, mc.ConsumerDependencyDetails{Binding: binding, Mapping: mapping, Locks: preparationDiscoveryLocks(source.ProjectID, source.AgentID, source.ExecutionID)})
}

func (a *Authority) ValidateInTx(ctx context.Context, tx f.Tx, request mc.ConsumerRequest, plan mc.ConsumerDependencies) error {
	if a == nil || a.state == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	source, err := preparationModelConsumer(request)
	if err != nil {
		return err
	}
	if plan.Validate() != nil {
		return fault(f.Forbidden)
	}
	if _, err = a.state.store.InTx(tx); err != nil {
		return portError(err)
	}
	if err = a.state.store.RequireHeldLocks(ctx, tx, plan.RequiredLocks()); err != nil {
		return portError(err)
	}
	var mapping f.Digest
	if _, capture := ctx.Value(preparationWitnessKey{}).(*preparationWitness); capture {
		var w *preparationWitness
		w, mapping, err = a.requireResourceCapture(ctx, tx, source.ProjectID, source.AgentID, source.ExecutionID)
		if err == nil {
			core := w.agent.Fields().Core
			effort := ""
			if core.ReasoningEffort != nil {
				effort = *core.ReasoningEffort
			}
			if *request.Resolve.ModelRef != core.ModelRef || request.Resolve.ReasoningEffort != effort {
				return fault(f.ConfirmationStale)
			}
		}
	} else {
		if d, ok := ctx.Value(preparationDiscoveryKey{}).(*preparationDiscovery); ok && d != nil && d.recovering {
			return fault(f.Forbidden)
		}
		_, mapping, err = a.requireResourceDiscovery(ctx, tx, source.ProjectID, source.AgentID, source.ExecutionID, "model")
	}
	if err != nil {
		return err
	}
	binding, err := mc.ConsumerBinding(request)
	if err != nil || !plan.Matches(a.state.modelIssuer, binding, mapping) {
		return fault(f.ConfirmationStale)
	}
	return ctx.Err()
}

var _ mc.ExecutionModelCaptureAuthority = (*Authority)(nil)
var _ mc.ConsumerAuthority = (*Authority)(nil)
