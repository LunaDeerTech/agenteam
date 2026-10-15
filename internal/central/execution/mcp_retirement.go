package execution

import (
	"context"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func mcpRetirementRequest(r ec.PreparationRequest, version f.Version) tc.MCPExecutionRetirementRequest {
	return tc.MCPExecutionRetirementRequest{ProjectID: r.Launch.ProjectID, AgentID: r.Launch.AgentID, ExecutionID: r.ExecutionID, TerminalVersion: version}
}

func inputRequiresMCPRetirement(input ec.PreparationInput) bool {
	for _, tool := range input.Fields().Tools {
		b := tool.BindingSnapshot
		if b.MCPBinding.Validate() == nil || b.Binding.HandlerID == "mcp.list-resources" || b.Binding.HandlerID == "mcp.read-resource" {
			return true
		}
	}
	return false
}

func discoverMCPRetirement(ctx context.Context, provider tc.MCPExecutionRetirement, request tc.MCPExecutionRetirementRequest, input ec.PreparationInput) (tc.MCPExecutionRetirementPlan, error) {
	if nilPort(provider) {
		if inputRequiresMCPRetirement(input) {
			return nil, fault(f.DependencyUnbound)
		}
		return nil, nil
	}
	plan, err := provider.DiscoverMCPExecutionRetirement(ctx, request)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(plan) {
		return nil, fault(f.DependencyUnbound)
	}
	return plan, nil
}

func releaseMCPRetirement(ctx context.Context, tx f.Tx, provider tc.MCPExecutionRetirement, request tc.MCPExecutionRetirementRequest, plan tc.MCPExecutionRetirementPlan) error {
	if nilPort(plan) {
		return nil
	}
	if nilPort(provider) {
		return fault(f.DependencyUnbound)
	}
	return portError(provider.ReleaseMCPExecutionInTx(ctx, tx, request, plan))
}

func observeMCPRetirement(ctx context.Context, tx f.Tx, provider tc.MCPExecutionRetirement, request tc.MCPExecutionRetirementRequest, plan tc.MCPExecutionRetirementPlan) (bool, error) {
	if nilPort(plan) {
		return true, nil
	}
	if nilPort(provider) {
		return false, fault(f.DependencyUnbound)
	}
	retired, err := provider.MCPExecutionRetiredInTx(ctx, tx, request, plan)
	return retired, portError(err)
}

func (a *Authority) CheckMCPRetirementPlan(ctx context.Context, request tc.MCPExecutionRetirementRequest) error {
	if request.Validate() != nil {
		return invalid()
	}
	if ctx != nil && ctx.Value(executionRecoveryContextKey{}) != nil {
		w, err := a.preparationRetirementOwner(ctx)
		if err != nil {
			return err
		}
		if request != mcpRetirementRequest(w.request, w.before.Version+1) {
			return fault(f.Forbidden)
		}
		return ctx.Err()
	}
	if _, err := a.directTextModelPlanningScope(ctx, true); err != nil {
		return err
	}
	run, err := a.directTextOwner(ctx)
	if err != nil {
		return err
	}
	if request != mcpRetirementRequest(run.request, run.terminal.version) {
		return fault(f.Forbidden)
	}
	return ctx.Err()
}

func (a *Authority) CheckMCPRetirementInTx(ctx context.Context, tx f.Tx, request tc.MCPExecutionRetirementRequest) error {
	if request.Validate() != nil || !tx.Valid() {
		return invalid()
	}
	if ctx != nil && ctx.Value(executionRecoveryContextKey{}) != nil {
		w, err := a.preparationRetirementOwner(ctx)
		if err != nil {
			return err
		}
		if request != mcpRetirementRequest(w.request, w.before.Version+1) {
			return fault(f.Forbidden)
		}
		return a.requirePreparationRetirementInTx(ctx, tx, w)
	}
	if _, err := a.directTextRetirementScope(ctx, tx); err != nil {
		return err
	}
	run, err := a.directTextOwner(ctx)
	if err != nil {
		return err
	}
	if request != mcpRetirementRequest(run.request, run.terminal.version) {
		return fault(f.Forbidden)
	}
	return ctx.Err()
}

var _ tc.MCPExecutionRetirementAuthority = (*Authority)(nil)
