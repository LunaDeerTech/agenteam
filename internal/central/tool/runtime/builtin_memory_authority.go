package runtime

import (
	"context"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func (a *BuiltinAuthority) memoryWitness(ctx context.Context, actor i.Actor, project i.ProjectID, agent i.AgentID, operation tc.OperationID, intent string) (*builtinWitness, error) {
	w, err := a.witness(ctx, actor, project)
	if err != nil {
		return nil, err
	}
	if agent.Validate() != nil || operation.Validate() != nil || w.call.binding.AgentID != agent || w.call.record.ID != operation {
		return nil, fail(f.Forbidden)
	}
	handler := ""
	switch intent {
	case "retain", "lookup-retain":
		handler = "memory.retain"
	case "recall":
		handler = "memory.recall"
	case "reflect":
		handler = "memory.reflect"
	}
	if handler == "" || w.call.binding.Binding.HandlerID != handler || w.call.lookup != (intent == "lookup-retain") {
		return nil, fail(f.Forbidden)
	}
	return w, nil
}

func (a *BuiltinAuthority) DiscoverMemoryTool(ctx context.Context, actor i.Actor, project i.ProjectID, agent i.AgentID, operation tc.OperationID, intent string) ([]f.LockRequest, error) {
	w, err := a.memoryWitness(ctx, actor, project, agent, operation, intent)
	if err != nil {
		return nil, err
	}
	return slices.Clone(w.call.locks), nil
}

func (a *BuiltinAuthority) RequireMemoryToolInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, agent i.AgentID, operation tc.OperationID, intent string) error {
	w, err := a.memoryWitness(ctx, actor, project, agent, operation, intent)
	if err != nil {
		return err
	}
	return a.withCurrent(ctx, tx, w, func(context.Context, []byte) error { return nil })
}

// RequireStoppedMemoryToolInTx lends no transferable death proof. The exact
// process is checked against this original synchronous cold Lookup and all
// current Tool/Execution facts are revalidated under its complete lock union.
func (a *BuiltinAuthority) RequireStoppedMemoryToolInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, agent i.AgentID, operation tc.OperationID, original oc.ProcessID) error {
	w, err := a.memoryWitness(ctx, actor, project, agent, operation, "lookup-retain")
	if err != nil {
		return err
	}
	proof := w.call.cold
	if original.Validate() != nil || proof == nil || proof.process != original || !proof.permits(a.process, w.call.record) {
		return fail(f.Forbidden)
	}
	return a.withCurrent(ctx, tx, w, func(context.Context, []byte) error {
		if !proof.permits(a.process, w.call.record) {
			return fail(f.Forbidden)
		}
		return nil
	})
}

// RequireAgentRunBindingInTx validates only the fixed composition identity.
// Actual calls still require a private invocation witness and its full plan.
func (a *BuiltinAuthority) RequireAgentRunBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if a == nil || ctx == nil || !sameInstallInstance(store, a.store) {
		return fail(f.DependencyUnbound)
	}
	if _, err := a.store.InTx(tx); err != nil {
		return portError(err)
	}
	return a.guard.RequireCurrentProcessInTx(ctx, tx, a.process)
}
