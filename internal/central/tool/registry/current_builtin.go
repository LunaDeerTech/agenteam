package registry

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// RequireCurrentBuiltinInTx checks an ordinary Builtin's exact current code
// binding in the caller's live Store/Tx. The caller pre-collects Registry SH and
// this ToolSpec SH alongside its complete authorization/operation lock plan.
// This method neither acquires locks nor begins a transaction.
//
// A persisted registration is insufficient: currentTool also compares the
// immutable canonical definition with the configured Source and invokes that
// Source's actual backend/resolver/classifier binding check in this same Tx.
// The result is metadata/code-binding validation, never Actor, scope, risk,
// approval, Execution or general Tool execution authority.
func (r *Registry) RequireCurrentBuiltinInTx(ctx context.Context, tx f.Tx, spec tc.SpecRef, binding tc.BuiltinBinding, scope tc.ScopeResolverID, risk tc.RiskClassifierID) error {
	if r.data() == nil {
		return fail(f.DependencyUnbound)
	}
	if ctx == nil || spec.Validate() != nil || binding.ContractRevision.Validate() != nil || binding.HandlerID == "" || scope == "" || risk == "" {
		return fail(f.InvalidArgument)
	}
	x, err := r.executor(ctx, tx, []f.LockRequest{RegistryLock(f.Shared), toolLock(spec.ToolID, f.Shared)})
	if err != nil {
		return err
	}
	current, err := r.currentTool(ctx, tx, x, spec.ToolID)
	if err != nil {
		return err
	}
	if current.Ref != spec || current.Binding != binding || current.Scope != scope || current.Risk != risk {
		return fail(f.InvalidState)
	}
	return portError(ctx.Err())
}
