package runtime

import (
	"context"
	"encoding/json"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

func (a *BuiltinAuthority) WithSkillPreparationInTx(ctx context.Context, tx f.Tx, inv sc.PreparationInvocation, plan sc.InvocationPlan, consume func(context.Context, []byte, []sc.SkillBinding, f.Version) error) error {
	if inv.Validate() != nil || consume == nil {
		return fail(f.Forbidden)
	}
	w, err := a.skillWitness(ctx, inv.Invocation)
	if err != nil {
		return err
	}
	if w.call.record.Request != inv.RequestID || w.call.record.Phase != "running" || w.call.record.Version != 2 {
		return fail(f.Forbidden)
	}
	return a.WithSkillInvocationInTx(ctx, tx, inv.Invocation, plan, func(current context.Context, raw []byte, bindings []sc.SkillBinding) error {
		return consume(current, raw, bindings, w.call.record.Version)
	})
}
func (a *BuiltinAuthority) RequireSkillPreparationRetirementInTx(ctx context.Context, tx f.Tx, inv sc.PreparationInvocation, plan sc.InvocationPlan) (f.Version, error) {
	if inv.Validate() != nil {
		return 0, fail(f.Forbidden)
	}
	if err := a.RequireSkillInvocationRetirement(ctx, inv.Invocation); err != nil {
		return 0, err
	}
	w, err := a.witness(ctx, inv.Invocation.Actor, inv.Invocation.ProjectID)
	if err != nil {
		return 0, err
	}
	p, ok := plan.(*skillInvocationPlan)
	if !ok || p == nil || p.issuer != a || p.witness == nil || p.witness.owner != w.owner || p.witness.call != w.call || !sameSkillInvocation(p.invocation, inv.Invocation) || w.call.record.Request != inv.RequestID || w.call.record.Phase != "running" || w.call.record.Version != 2 {
		return 0, fail(f.Forbidden)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return 0, portError(err)
	}
	if err = a.guard.RequireCurrentProcessInTx(ctx, tx, a.process); err != nil {
		return 0, portError(err)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return 0, portError(err)
	}
	if err = requireBuiltinAttempt(ctx, x, a.process, w.call.record); err != nil {
		return 0, err
	}
	return w.call.record.Version, nil
}

var _ sc.PreparationInvocationAuthority = (*BuiltinAuthority)(nil)

type skillPrepareBackend struct{ service *skill.Preparer }

func NewSkillPrepareBackend(service *skill.Preparer) (BuiltinBackend, error) {
	if service == nil {
		return nil, fail(f.DependencyUnbound)
	}
	return &skillPrepareBackend{service}, nil
}
func (b *skillPrepareBackend) Registration() tc.BuiltinRegistration {
	if b == nil || b.service == nil {
		return tc.BuiltinRegistration{}
	}
	definition := tc.Definition{StableKey: "builtin:prepare-skill", Name: "prepare-skill", Description: "Prepare the fixed revision of an effective assigned skill in the selected mount. Returns a relative package directory. Preparation does not execute scripts or install dependencies; use run-command separately when appropriate.", Annotations: &tc.Annotations{Idempotency: tc.Inherent}, InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["skill_id","mount_id"],"properties":{"skill_id":{"type":"string","format":"uuid"},"mount_id":{"type":"string","format":"uuid"}}}`), OutputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["skill_id","revision_id","revision","mount_id","path","reused"],"properties":{"skill_id":{"type":"string","format":"uuid"},"revision_id":{"type":"string","format":"uuid"},"revision":{"type":"string"},"mount_id":{"type":"string","format":"uuid"},"path":{"type":"string"},"reused":{"type":"boolean"}}}`)}
	return tc.BuiltinRegistration{Definition: definition, Binding: tc.BuiltinBinding{HandlerID: string(sc.PrepareSkill), ContractRevision: 1}, ScopeResolverID: "skill.project.v1", RiskClassifierID: "skill.prepare.v1", Class: tc.OrdinaryTool, Active: true}
}
func (b *skillPrepareBackend) RequireBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if b == nil || b.service == nil {
		return fail(f.DependencyUnbound)
	}
	return b.service.RequireBindingInTx(ctx, tx, store)
}
func (b *skillPrepareBackend) invocation(v BuiltinInvocation) (sc.PreparationInvocation, error) {
	if b == nil || b.service == nil || v.Validate() != nil || v.Call.Binding != b.Registration().Binding {
		return sc.PreparationInvocation{}, fail(f.Forbidden)
	}
	inv := sc.PreparationInvocation{Invocation: sc.Invocation{Actor: v.Call.Actor, ProjectID: v.Call.ProjectID, AgentID: v.Call.AgentID, ExecutionID: v.Call.ExecutionID, OperationID: v.OperationID.String(), AttemptID: v.AttemptID.String(), ToolCallID: v.Call.Input.PayloadID, ToolID: v.Call.Spec.ToolID, SpecRevision: v.Call.Spec.SpecRevision, Intent: sc.PrepareSkill}, RequestID: v.RequestID}
	return inv, inv.Validate()
}
func (b *skillPrepareBackend) Execute(ctx context.Context, v BuiltinInvocation, raw []byte) ([]byte, error) {
	inv, err := b.invocation(v)
	if err != nil {
		return nil, err
	}
	return b.service.Execute(ctx, inv, raw)
}
func (b *skillPrepareBackend) Lookup(ctx context.Context, v BuiltinInvocation, raw []byte) ([]byte, bool, error) {
	inv, err := b.invocation(v)
	if err != nil {
		return nil, false, err
	}
	if skillPrepareColdLookup(ctx) {
		return b.service.LookupCold(ctx, inv, raw)
	}
	owned, release, err := skillPrepareReturnedLookup(ctx, v)
	if err != nil {
		return nil, false, err
	}
	defer release()
	return b.service.Lookup(owned, inv)
}
func (b *skillPrepareBackend) JoinBuiltinCall(ctx context.Context, v BuiltinInvocation) error {
	inv, err := b.invocation(v)
	if err != nil {
		return err
	}
	return b.service.Join(ctx, inv)
}

var _ BuiltinBackend = (*skillPrepareBackend)(nil)
var _ BuiltinCallOwner = (*skillPrepareBackend)(nil)
