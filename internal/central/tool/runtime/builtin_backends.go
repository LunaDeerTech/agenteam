package runtime

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type taskBuiltinBackend struct {
	adapter *builtin.TaskAdapter
	name    string
	handler string
	risk    tc.RiskClassifierID
}

// Each backend retains the actual adapter. Registry publication also checks
// the adapter's real domain binding inside the Registry transaction.
func NewTaskBackends(adapter *builtin.TaskAdapter) ([]BuiltinBackend, error) {
	if adapter == nil {
		return nil, fail(f.DependencyUnbound)
	}
	return []BuiltinBackend{
		&taskBuiltinBackend{adapter, builtin.TaskReadName, builtin.TaskReadHandlerID, builtin.TaskReadRiskID},
		&taskBuiltinBackend{adapter, builtin.TaskTransferName, builtin.TaskTransferHandlerID, builtin.TaskTransferRiskID},
	}, nil
}

func (b *taskBuiltinBackend) Registration() tc.BuiltinRegistration {
	if b == nil || b.adapter == nil {
		return tc.BuiltinRegistration{}
	}
	definition, _ := builtin.TaskDefinition(b.name)
	return tc.BuiltinRegistration{Definition: definition, Binding: tc.BuiltinBinding{HandlerID: b.handler, ContractRevision: 1}, ScopeResolverID: builtin.TaskScopeID, RiskClassifierID: b.risk, Class: tc.CoreTool, Active: true}
}
func (b *taskBuiltinBackend) RequireBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if b == nil || b.adapter == nil {
		return fail(f.DependencyUnbound)
	}
	return b.adapter.RequireBindingInTx(ctx, tx, store)
}
func (b *taskBuiltinBackend) request(ctx context.Context, v BuiltinInvocation) (f.ID[f.Request], error) {
	if b == nil || b.adapter == nil {
		return f.ID[f.Request]{}, fail(f.DependencyUnbound)
	}
	if err := contextError(ctx); err != nil {
		return f.ID[f.Request]{}, err
	}
	if v.Validate() != nil || v.Call.Binding != b.Registration().Binding {
		return f.ID[f.Request]{}, fail(f.Forbidden)
	}
	return f.ParseID[f.Request](string(v.RequestID))
}
func (b *taskBuiltinBackend) Execute(ctx context.Context, v BuiltinInvocation, raw []byte) ([]byte, error) {
	request, err := b.request(ctx, v)
	if err != nil {
		return nil, err
	}
	return b.adapter.Execute(ctx, v.Call, v.OperationID, request, raw)
}
func (b *taskBuiltinBackend) Lookup(ctx context.Context, v BuiltinInvocation, raw []byte) ([]byte, bool, error) {
	request, err := b.request(ctx, v)
	if err != nil {
		return nil, false, err
	}
	if b.name == builtin.TaskReadName {
		return nil, false, nil
	}
	return b.adapter.Lookup(ctx, v.Call, v.OperationID, request, raw)
}

type artifactBuiltinBackend struct {
	adapter *builtin.ArtifactAdapter
	name    string
	handler string
	risk    tc.RiskClassifierID
}

func NewArtifactBackends(adapter *builtin.ArtifactAdapter) ([]BuiltinBackend, error) {
	if adapter == nil {
		return nil, fail(f.DependencyUnbound)
	}
	return []BuiltinBackend{
		&artifactBuiltinBackend{adapter, builtin.ArtifactCreateName, builtin.ArtifactCreateHandlerID, builtin.ArtifactCreateRiskID},
		&artifactBuiltinBackend{adapter, builtin.ArtifactReadName, builtin.ArtifactReadHandlerID, builtin.ArtifactReadRiskID},
		&artifactBuiltinBackend{adapter, builtin.ArtifactListName, builtin.ArtifactListHandlerID, builtin.ArtifactReadRiskID},
	}, nil
}
func (b *artifactBuiltinBackend) Registration() tc.BuiltinRegistration {
	if b == nil || b.adapter == nil {
		return tc.BuiltinRegistration{}
	}
	definition, _ := builtin.ArtifactDefinition(b.name)
	return tc.BuiltinRegistration{Definition: definition, Binding: tc.BuiltinBinding{HandlerID: b.handler, ContractRevision: 1}, ScopeResolverID: builtin.ArtifactScopeID, RiskClassifierID: b.risk, Class: tc.CoreTool, Active: true}
}
func (b *artifactBuiltinBackend) RequireBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if b == nil || b.adapter == nil {
		return fail(f.DependencyUnbound)
	}
	return b.adapter.RequireBindingInTx(ctx, tx, store)
}
func (b *artifactBuiltinBackend) invocation(ctx context.Context, v BuiltinInvocation) (ac.Invocation, f.CommandMeta, error) {
	if b == nil || b.adapter == nil {
		return ac.Invocation{}, f.CommandMeta{}, fail(f.DependencyUnbound)
	}
	if err := contextError(ctx); err != nil {
		return ac.Invocation{}, f.CommandMeta{}, err
	}
	if v.Validate() != nil || v.Call.Binding != b.Registration().Binding {
		return ac.Invocation{}, f.CommandMeta{}, fail(f.Forbidden)
	}
	attempt, err := f.ParseID[ac.CallAttempt](v.AttemptID.String())
	if err != nil {
		return ac.Invocation{}, f.CommandMeta{}, err
	}
	invocation, err := ac.NewInvocation(ac.InvocationDetails{Actor: v.Call.Actor, ProjectID: v.Call.ProjectID, ExecutionID: v.Call.ExecutionID.String(), OperationID: v.OperationID.String(), ToolID: v.Call.Spec.ToolID.String(), ToolCallID: v.Call.Input.PayloadID, AttemptID: attempt})
	if err != nil {
		return ac.Invocation{}, f.CommandMeta{}, err
	}
	request, err := f.ParseID[f.Request](string(v.RequestID))
	if err != nil {
		return ac.Invocation{}, f.CommandMeta{}, err
	}
	key, err := builtin.ArtifactCreateKey(v.OperationID.String())
	if err != nil {
		return ac.Invocation{}, f.CommandMeta{}, err
	}
	return invocation, f.CommandMeta{RequestID: request, IdempotencyKey: key}, nil
}
func (b *artifactBuiltinBackend) Execute(ctx context.Context, v BuiltinInvocation, raw []byte) ([]byte, error) {
	invocation, command, err := b.invocation(ctx, v)
	if err != nil {
		return nil, err
	}
	return b.adapter.Execute(ctx, invocation, command, b.name, raw)
}
func (b *artifactBuiltinBackend) Lookup(ctx context.Context, v BuiltinInvocation, raw []byte) ([]byte, bool, error) {
	invocation, command, err := b.invocation(ctx, v)
	if err != nil {
		return nil, false, err
	}
	if b.name != builtin.ArtifactCreateName {
		return nil, false, nil
	}
	return b.adapter.LookupCreate(ctx, invocation, command, raw)
}

var _ BuiltinBackend = (*taskBuiltinBackend)(nil)
var _ BuiltinBackend = (*artifactBuiltinBackend)(nil)
