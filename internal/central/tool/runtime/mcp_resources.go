package runtime

import (
	"context"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type resourceInvocationPlan struct {
	owner       *BuiltinAuthority
	witness     *builtinWitness
	invocation  tc.MCPResourceInvocation
	connections []tc.MCPResourceConnection
	locks       []f.LockRequest
}

func (p *resourceInvocationPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}
func (p *resourceInvocationPlan) RuntimeConnections() []tc.MCPResourceConnection {
	if p == nil {
		return nil
	}
	return slices.Clone(p.connections)
}

func (a *BuiltinAuthority) resourceWitness(ctx context.Context, v tc.MCPResourceInvocation) (*builtinWitness, error) {
	if v.Validate() != nil {
		return nil, fail(f.Forbidden)
	}
	w, err := a.witness(ctx, v.Call.Actor, v.Call.ProjectID)
	if err != nil {
		return nil, err
	}
	if w.call.lookup || !sameRunnerCallBinding(w.call.binding, v.Call) || w.call.record.ID != v.OperationID || w.call.record.Attempt != v.AttemptID || string(w.call.record.Request) != v.RequestID {
		return nil, fail(f.Forbidden)
	}
	return w, nil
}
func (a *BuiltinAuthority) DiscoverMCPResourceInvocation(ctx context.Context, v tc.MCPResourceInvocation) (tc.MCPResourceInvocationPlan, error) {
	w, err := a.resourceWitness(ctx, v)
	if err != nil {
		return nil, err
	}
	p := &resourceInvocationPlan{owner: a, witness: w, invocation: v, locks: slices.Clone(w.call.locks)}
	err = w.owner.transaction(ctx, w.call, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		return a.withCurrent(ctx, tx, w, func(current context.Context, _ []byte) error {
			var err error
			p.connections, err = w.owner.registry.ReadMCPResourceConnectionsInTx(current, tx, v.Call)
			return err
		})
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}
func (a *BuiltinAuthority) RequireMCPResourceInvocationInTx(ctx context.Context, tx f.Tx, v tc.MCPResourceInvocation, plan tc.MCPResourceInvocationPlan) error {
	p, ok := plan.(*resourceInvocationPlan)
	if !ok || p == nil || p.owner != a || p.invocation.OperationID != v.OperationID || p.invocation.AttemptID != v.AttemptID || p.invocation.RequestID != v.RequestID || !sameRunnerCallBinding(p.invocation.Call, v.Call) {
		return fail(f.Forbidden)
	}
	w, err := a.resourceWitness(ctx, v)
	if err != nil {
		return err
	}
	if w != p.witness {
		return fail(f.Forbidden)
	}
	return a.withCurrent(ctx, tx, w, func(current context.Context, _ []byte) error {
		connections, err := w.owner.registry.ReadMCPResourceConnectionsInTx(current, tx, v.Call)
		if err != nil {
			return err
		}
		if !slices.Equal(connections, p.connections) {
			return fail(f.ConfirmationStale)
		}
		return nil
	})
}

// The backend port carries only the fixed resource invocation. It cannot be
// selected by a remote MCPBinding or used to dispatch ordinary Builtin tools.
type MCPResourceBackend interface {
	RequireBindingInTx(context.Context, f.Tx, any) error
	Execute(context.Context, tc.MCPResourceInvocation, []byte) (tc.MCPBackendResult, error)
}
type MCPResourceBuiltin struct {
	registration tc.BuiltinRegistration
	backend      MCPResourceBackend
}

func NewMCPResourceBuiltin(registration tc.BuiltinRegistration, backend MCPResourceBackend) (*MCPResourceBuiltin, error) {
	b := registration.Binding
	if nilPort(backend) || registration.Validate(context.Background()) != nil || !registration.Active || b.ContractRevision != 1 || (b.HandlerID != "mcp.list-resources" && b.HandlerID != "mcp.read-resource") {
		return nil, fail(f.DependencyUnbound)
	}
	return &MCPResourceBuiltin{registration, backend}, nil
}
func (b *MCPResourceBuiltin) Registration() tc.BuiltinRegistration {
	if b == nil {
		return tc.BuiltinRegistration{}
	}
	return b.registration
}
func (b *MCPResourceBuiltin) RequireBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if b == nil || nilPort(b.backend) {
		return fail(f.DependencyUnbound)
	}
	return b.backend.RequireBindingInTx(ctx, tx, store)
}
func (b *MCPResourceBuiltin) Execute(ctx context.Context, in BuiltinInvocation, raw []byte) ([]byte, error) {
	if b == nil || in.Validate() != nil || in.Call.Binding != b.registration.Binding {
		return nil, fail(f.Forbidden)
	}
	result, err := b.backend.Execute(ctx, tc.MCPResourceInvocation{Call: in.Call, OperationID: in.OperationID, AttemptID: in.AttemptID, RequestID: string(in.RequestID)}, raw)
	if err != nil {
		return nil, err
	}
	encoded, err := EncodeMCPResourceResult(in, result)
	if err == nil {
		return encoded, nil
	}
	fallback := tc.MCPBackendResult{OutcomeKnown: result.OutcomeKnown, MayHaveExecuted: result.MayHaveExecuted, FailureCode: "mcp_invalid_response", Result: mc.ToolResultPart{CallID: in.Call.CallID, IsError: true, Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: "mcp_invalid_response"}}}}}
	if !fallback.OutcomeKnown {
		fallback.MayHaveExecuted = true
	}
	return EncodeMCPResourceResult(in, fallback)
}
func (*MCPResourceBuiltin) Lookup(context.Context, BuiltinInvocation, []byte) ([]byte, bool, error) {
	return nil, false, nil
}

var _ tc.MCPResourceInvocationAuthority = (*BuiltinAuthority)(nil)
var _ BuiltinBackend = (*MCPResourceBuiltin)(nil)
