package runtime

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func (a *BuiltinAuthority) knowledgeWitness(ctx context.Context, inv kc.Invocation) (*builtinWitness, error) {
	if inv.Validate() != nil {
		return nil, fail(f.Forbidden)
	}
	d := inv.Details()
	w, err := a.witness(ctx, d.Actor, d.ProjectID)
	if err != nil {
		return nil, err
	}
	b := w.call.binding
	if w.call.lookup || b.Binding.HandlerID != "knowledge.query" && b.Binding.HandlerID != "knowledge.read" ||
		d.ExecutionID != b.ExecutionID.String() || d.OperationID != w.call.record.ID.String() ||
		d.ToolID != b.Spec.ToolID.String() || d.ToolCallID != b.Input.PayloadID || d.AttemptID.String() != w.call.record.Attempt.String() {
		return nil, fail(f.Forbidden)
	}
	return w, nil
}

func (a *BuiltinAuthority) Discover(ctx context.Context, inv kc.Invocation) (oc.AccessDependencies, error) {
	w, err := a.knowledgeWitness(ctx, inv)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	raw, err := encode(struct {
		Input     f.Digest
		Operation tc.OperationID
		Attempt   tc.AttemptID
	}{w.call.record.InputDigest, w.call.record.ID, w.call.record.Attempt})
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return oc.NewAccessDependencies(digest(raw), w.call.locks)
}

func (a *BuiltinAuthority) DiscoverInTx(ctx context.Context, tx f.Tx, inv kc.Invocation) (oc.AccessDependencies, error) {
	if err := a.AuthorizeInTx(ctx, tx, inv, i.Read); err != nil {
		return oc.AccessDependencies{}, err
	}
	return a.Discover(ctx, inv)
}

func (a *BuiltinAuthority) AuthorizeInTx(ctx context.Context, tx f.Tx, inv kc.Invocation, intent i.AccessIntent) error {
	if intent != i.Read {
		return fail(f.Forbidden)
	}
	w, err := a.knowledgeWitness(ctx, inv)
	if err != nil {
		return err
	}
	return a.withCurrent(ctx, tx, w, func(context.Context, []byte) error { return nil })
}

func (a *BuiltinAuthority) WithKnowledgeInputInTx(ctx context.Context, tx f.Tx, inv kc.Invocation, consume func(context.Context, []byte) error) error {
	w, err := a.knowledgeWitness(ctx, inv)
	if err != nil {
		return err
	}
	return a.withCurrent(ctx, tx, w, consume)
}

var _ kc.KnowledgeInvocationAuthority = (*BuiltinAuthority)(nil)
