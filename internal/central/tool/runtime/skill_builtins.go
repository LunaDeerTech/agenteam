package runtime

import (
	"context"
	"encoding/json"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// Implemented by the actual Execution ToolCallDriver. It reads this call's
// immutable round input, not the current Agent catalog or a caller DTO.
type skillToolBindings interface {
	ReadSkillToolBindingsInTx(context.Context, f.Tx, tc.ToolCallBinding, tc.ToolCallPlan) ([]sc.SkillBinding, error)
}

type skillInvocationPlan struct {
	issuer     *BuiltinAuthority
	witness    *builtinWitness
	invocation sc.Invocation
	locks      []f.LockRequest
}

func (p *skillInvocationPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}

func sameSkillInvocation(a, b sc.Invocation) bool {
	return a.Actor.Equal(b.Actor) && a.ProjectID == b.ProjectID && a.AgentID == b.AgentID && a.ExecutionID == b.ExecutionID &&
		a.OperationID == b.OperationID && a.AttemptID == b.AttemptID && a.ToolCallID == b.ToolCallID && a.ToolID == b.ToolID &&
		a.SpecRevision == b.SpecRevision && a.Intent == b.Intent
}

func (a *BuiltinAuthority) skillWitness(ctx context.Context, inv sc.Invocation) (*builtinWitness, error) {
	if inv.Validate() != nil {
		return nil, fail(f.Forbidden)
	}
	w, err := a.witness(ctx, inv.Actor, inv.ProjectID)
	if err != nil {
		return nil, err
	}
	b := w.call.binding
	if w.call.lookup || w.cleanupOnly.Load() || b.Binding.HandlerID != string(inv.Intent) || b.Binding.ContractRevision != 1 ||
		b.AgentID != inv.AgentID || b.ExecutionID != inv.ExecutionID || b.Spec.ToolID != inv.ToolID || b.Spec.SpecRevision != inv.SpecRevision ||
		b.Input.PayloadID != inv.ToolCallID || w.call.record.ID.String() != inv.OperationID || w.call.record.Attempt.String() != inv.AttemptID {
		return nil, fail(f.Forbidden)
	}
	if _, ok := w.owner.executions.(skillToolBindings); !ok {
		return nil, fail(f.DependencyUnbound)
	}
	return w, nil
}

func (a *BuiltinAuthority) DiscoverSkillInvocation(ctx context.Context, inv sc.Invocation) (sc.InvocationPlan, error) {
	w, err := a.skillWitness(ctx, inv)
	if err != nil {
		return nil, err
	}
	schedule, _ := f.ProjectScheduleLock(inv.ProjectID.String())
	agent, _ := f.AgentLock(inv.AgentID.String())
	locks, err := ob.NormalizeLocks(append(slices.Clone(w.call.locks), f.LockRequest{Key: schedule, Mode: f.Exclusive}, f.LockRequest{Key: agent, Mode: f.Exclusive}))
	if err != nil {
		return nil, err
	}
	return &skillInvocationPlan{a, w, inv, locks}, nil
}

func (a *BuiltinAuthority) WithSkillInvocationInTx(ctx context.Context, tx f.Tx, inv sc.Invocation, plan sc.InvocationPlan, consume func(context.Context, []byte, []sc.SkillBinding) error) error {
	w, err := a.skillWitness(ctx, inv)
	if err != nil {
		return err
	}
	p, ok := plan.(*skillInvocationPlan)
	if !ok || p == nil || p.issuer != a || p.witness != w || !sameSkillInvocation(p.invocation, inv) || consume == nil {
		return fail(f.Forbidden)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return portError(err)
	}
	return a.withCurrent(ctx, tx, w, func(current context.Context, raw []byte) error {
		bindings, err := w.owner.executions.(skillToolBindings).ReadSkillToolBindingsInTx(current, tx, w.call.binding, w.call.plan)
		if err != nil {
			return err
		}
		if bindings == nil {
			return fail(f.InvalidState)
		}
		for n, b := range bindings {
			if b.Validate() != nil || n > 0 && bindings[n-1].SkillID.String() >= b.SkillID.String() {
				return fail(f.InvalidState)
			}
		}
		return consume(current, raw, slices.Clone(bindings))
	})
}

func (a *BuiltinAuthority) RequireSkillRuntimeBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	return a.RequireAgentRunBindingInTx(ctx, tx, store)
}

var _ sc.InvocationAuthority = (*BuiltinAuthority)(nil)

type skillReadBackend struct {
	service *skill.Service
	intent  sc.InvocationIntent
	name    string
}

func NewSkillReadBackends(service *skill.Service) ([]BuiltinBackend, error) {
	if service == nil {
		return nil, fail(f.DependencyUnbound)
	}
	return []BuiltinBackend{&skillReadBackend{service, sc.ListSkills, "list-skills"}, &skillReadBackend{service, sc.ReadSkill, "read-skill"}}, nil
}
func (b *skillReadBackend) Registration() tc.BuiltinRegistration {
	if b == nil || b.service == nil {
		return tc.BuiltinRegistration{}
	}
	d := tc.Definition{StableKey: "builtin:" + b.name, Name: b.name, Annotations: &tc.Annotations{ReadOnly: true, Idempotency: tc.Inherent}}
	if b.intent == sc.ListSkills {
		d.Description = "List skills effective for this execution's current model round and still assigned to this agent. Newly assigned skills become available in the next model round."
		d.InputSchema = json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{}}`)
		d.OutputSchema = json.RawMessage(`{"type":"object","required":["skills"],"properties":{"skills":{"type":"array","items":{"type":"object","required":["skill_id","revision","name","description"],"properties":{"skill_id":{"type":"string","format":"uuid"},"revision":{"type":"string"},"name":{"type":"string"},"description":{"type":"string"}}}}}}`)
	} else {
		d.Description = "Read text from the fixed revision of an effective assigned skill. Start with SKILL.md. Paths are relative to the skill package; byte_offset continues a truncated file."
		d.InputSchema = json.RawMessage(`{"type":"object","additionalProperties":false,"required":["skill_id","path"],"properties":{"skill_id":{"type":"string","format":"uuid"},"path":{"type":"string","minLength":1,"maxLength":1024},"byte_offset":{"type":"integer","minimum":0,"maximum":8388608},"max_bytes":{"type":"integer","minimum":1,"maximum":262144}}}`)
		d.OutputSchema = json.RawMessage(`{"type":"object","required":["skill_id","revision","path","text","next_byte_offset","truncated"],"properties":{"skill_id":{"type":"string","format":"uuid"},"revision":{"type":"string"},"path":{"type":"string"},"text":{"type":"string"},"next_byte_offset":{"type":"integer"},"truncated":{"type":"boolean"}}}`)
	}
	return tc.BuiltinRegistration{Definition: d, Binding: tc.BuiltinBinding{HandlerID: string(b.intent), ContractRevision: 1}, ScopeResolverID: "skill.project.v1", RiskClassifierID: "skill.read.v1", Class: tc.OrdinaryTool, Active: true}
}
func (b *skillReadBackend) RequireBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if b == nil || b.service == nil {
		return fail(f.DependencyUnbound)
	}
	return b.service.RequireSkillBuiltinBindingInTx(ctx, tx, store)
}
func (b *skillReadBackend) invocation(v BuiltinInvocation) (sc.Invocation, error) {
	if b == nil || b.service == nil || v.Validate() != nil || v.Call.Binding != b.Registration().Binding {
		return sc.Invocation{}, fail(f.Forbidden)
	}
	r := sc.Invocation{Actor: v.Call.Actor, ProjectID: v.Call.ProjectID, AgentID: v.Call.AgentID, ExecutionID: v.Call.ExecutionID, OperationID: v.OperationID.String(), AttemptID: v.AttemptID.String(), ToolCallID: v.Call.Input.PayloadID, ToolID: v.Call.Spec.ToolID, SpecRevision: v.Call.Spec.SpecRevision, Intent: b.intent}
	return r, r.Validate()
}
func (b *skillReadBackend) Execute(ctx context.Context, v BuiltinInvocation, raw []byte) ([]byte, error) {
	inv, err := b.invocation(v)
	if err != nil {
		return nil, err
	}
	return b.service.ExecuteSkillReadTool(ctx, inv, raw)
}
func (b *skillReadBackend) Lookup(ctx context.Context, v BuiltinInvocation, _ []byte) ([]byte, bool, error) {
	_, err := b.invocation(v)
	return nil, false, err
}

var _ BuiltinBackend = (*skillReadBackend)(nil)

func (a *BuiltinAuthority) RequireSkillInvocationRetirement(ctx context.Context, inv sc.Invocation) error {
	if inv.Validate() != nil {
		return fail(f.Forbidden)
	}
	w, err := a.witness(ctx, inv.Actor, inv.ProjectID)
	if err != nil {
		return err
	}
	b := w.call.binding
	if w.call.lookup || b.Binding.HandlerID != string(inv.Intent) || b.Binding.ContractRevision != 1 || b.AgentID != inv.AgentID || b.ExecutionID != inv.ExecutionID || b.Spec.ToolID != inv.ToolID || b.Spec.SpecRevision != inv.SpecRevision || b.Input.PayloadID != inv.ToolCallID || w.call.record.ID.String() != inv.OperationID || w.call.record.Attempt.String() != inv.AttemptID {
		return fail(f.Forbidden)
	}
	// The ordinary synchronous join occurs immediately after Execute returned;
	// recovery receives a cleanup-only witness tied to the same retained call.
	if !w.cleanupOnly.Load() {
		return fail(f.ResourceBusy)
	}
	return nil
}
func (b *skillReadBackend) JoinBuiltinCall(ctx context.Context, v BuiltinInvocation) error {
	inv, err := b.invocation(v)
	if err != nil {
		return err
	}
	return b.service.JoinSkillReadTool(ctx, inv)
}

var _ sc.InvocationRetirementAuthority = (*BuiltinAuthority)(nil)
var _ BuiltinCallOwner = (*skillReadBackend)(nil)
