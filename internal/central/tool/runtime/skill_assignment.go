package runtime

import (
	"context"
	"encoding/json"
	"slices"

	ag "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type skillAssignmentRunPlan struct {
	owner   *BuiltinAuthority
	witness *builtinWitness
	actor   i.Actor
	project i.ProjectID
	run     ag.SkillAssignmentRun
	locks   []f.LockRequest
}

func (p *skillAssignmentRunPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}
func (a *BuiltinAuthority) assignmentWitness(ctx context.Context, actor i.Actor, project i.ProjectID, run ag.SkillAssignmentRun) (*builtinWitness, error) {
	if run.Validate(actor, project) != nil {
		return nil, fail(f.Forbidden)
	}
	w, err := a.witness(ctx, actor, project)
	if err != nil {
		return nil, err
	}
	b := w.call.binding
	if w.cleanupOnly.Load() || b.Binding != (tc.BuiltinBinding{HandlerID: "skill.assign", ContractRevision: 1}) || b.AgentID != run.CallerAgentID || b.ExecutionID != run.ExecutionID || b.Input.PayloadID != run.ToolCallID || b.Spec.ToolID != run.ToolID || b.Spec.SpecRevision != run.SpecRevision || b.Input.SHA256 != run.InputDigest || w.call.record.ID.String() != run.OperationID || w.call.record.Attempt.String() != run.AttemptID || string(w.call.record.Request) != run.RequestID {
		return nil, fail(f.Forbidden)
	}
	return w, nil
}
func (a *BuiltinAuthority) DiscoverSkillAssignmentRun(ctx context.Context, actor i.Actor, project i.ProjectID, run ag.SkillAssignmentRun) (ag.SkillAssignmentRunPlan, error) {
	w, err := a.assignmentWitness(ctx, actor, project, run)
	if err != nil {
		return nil, err
	}
	schedule, _ := f.ProjectScheduleLock(project.String())
	caller, _ := f.AgentLock(run.CallerAgentID.String())
	locks, err := ob.NormalizeLocks(append(slices.Clone(w.call.locks), f.LockRequest{Key: schedule, Mode: f.Exclusive}, f.LockRequest{Key: caller, Mode: f.Exclusive}))
	if err != nil {
		return nil, err
	}
	return &skillAssignmentRunPlan{a, w, actor, project, run, locks}, nil
}
func (a *BuiltinAuthority) RequireSkillAssignmentRunInTx(ctx context.Context, tx f.Tx, r ag.SkillAssignmentRequest, intent i.AccessIntent) error {
	if r.Validate() != nil || r.Run == nil || (intent != i.Read && intent != i.Mutate) {
		return fail(f.Forbidden)
	}
	w, err := a.assignmentWitness(ctx, r.Actor, r.ProjectID, *r.Run)
	if err != nil {
		return err
	}
	p, ok := r.RunPlan.(*skillAssignmentRunPlan)
	if !ok || p == nil || p.owner != a || p.witness != w || !p.actor.Equal(r.Actor) || p.project != r.ProjectID || p.run != *r.Run || (w.call.lookup && intent != i.Read) {
		return fail(f.Forbidden)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, r.RequiredLocks()); err != nil {
		return portError(err)
	}
	return a.withCurrent(ctx, tx, w, func(_ context.Context, raw []byte) error {
		args, err := sc.DecodeAssignmentToolArguments(raw)
		if err != nil {
			return err
		}
		if args.AgentID != r.AgentID || args.ExpectedVersion != r.ExpectedVersion || r.Command.Command() != "skill."+args.Action || string(r.Command.Key()) != r.Run.OperationID {
			return fail(f.Forbidden)
		}
		return nil
	})
}

var _ ag.SkillAssignmentRunAuthority = (*BuiltinAuthority)(nil)

type skillAssignmentBackend struct {
	commands *skill.RuntimeAssignmentCommands
}

func NewSkillAssignmentBackend(commands *skill.RuntimeAssignmentCommands) (BuiltinBackend, error) {
	if commands == nil {
		return nil, fail(f.DependencyUnbound)
	}
	return &skillAssignmentBackend{commands}, nil
}
func (b *skillAssignmentBackend) Registration() tc.BuiltinRegistration {
	if b == nil || b.commands == nil {
		return tc.BuiltinRegistration{}
	}
	return tc.BuiltinRegistration{Definition: tc.Definition{StableKey: "builtin:assign-skill", Name: "assign-skill", Description: "Inspect assignable project skill metadata and target agent versions with action=catalog. Optionally supply agent_id to inspect its assignments; pass next_skill_id as after_skill_id and the next_agent fields to page. Assign, enable, disable or remove a project skill using the target expected_version. Enabled assignments enter its next model round; this does not grant the caller permission to read the skill.", Annotations: &tc.Annotations{Idempotency: tc.Keyed}, InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["action"],"properties":{"action":{"type":"string","enum":["catalog","assign","remove"]},"agent_id":{"type":"string","format":"uuid"},"expected_version":{"type":"string"},"skill_id":{"type":"string","format":"uuid"},"assignment_id":{"type":"string","format":"uuid"},"enabled":{"type":"boolean"},"after_skill_id":{"type":"string","format":"uuid"},"after_agent_id":{"type":"string","format":"uuid"},"after_agent_created_at":{"type":"string"},"limit":{"type":"integer"}}}`), OutputSchema: json.RawMessage(`{"type":"object"}`)}, Binding: tc.BuiltinBinding{HandlerID: "skill.assign", ContractRevision: 1}, ScopeResolverID: "skill.project.v1", RiskClassifierID: "skill.assign.v1", Class: tc.OrdinaryTool, Active: true}
}
func (b *skillAssignmentBackend) RequireBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if b == nil || b.commands == nil {
		return fail(f.DependencyUnbound)
	}
	return b.commands.RequireBindingInTx(ctx, tx, store)
}
func (b *skillAssignmentBackend) origin(v BuiltinInvocation) (ag.SkillAssignmentRun, error) {
	if b == nil || b.commands == nil || v.Validate() != nil || v.Call.Binding != b.Registration().Binding {
		return ag.SkillAssignmentRun{}, fail(f.Forbidden)
	}
	r := ag.SkillAssignmentRun{CallerAgentID: v.Call.AgentID, ExecutionID: v.Call.ExecutionID, OperationID: v.OperationID.String(), AttemptID: v.AttemptID.String(), RequestID: string(v.RequestID), ToolCallID: v.Call.Input.PayloadID, ToolID: v.Call.Spec.ToolID, SpecRevision: v.Call.Spec.SpecRevision, InputDigest: v.Call.Input.SHA256}
	return r, r.Validate(v.Call.Actor, v.Call.ProjectID)
}
func (b *skillAssignmentBackend) Execute(ctx context.Context, v BuiltinInvocation, raw []byte) ([]byte, error) {
	r, err := b.origin(v)
	if err != nil {
		return nil, err
	}
	return b.commands.Execute(ctx, v.Call.Actor, v.Call.ProjectID, r, raw)
}
func (b *skillAssignmentBackend) Lookup(ctx context.Context, v BuiltinInvocation, raw []byte) ([]byte, bool, error) {
	r, err := b.origin(v)
	if err != nil {
		return nil, false, err
	}
	return b.commands.Lookup(ctx, v.Call.Actor, v.Call.ProjectID, r, raw)
}

var _ BuiltinBackend = (*skillAssignmentBackend)(nil)

func (a *BuiltinAuthority) RequireSkillAssignmentCatalogInTx(ctx context.Context, tx f.Tx, r ag.SkillAssignmentCatalogRequest) error {
	if r.Validate() != nil {
		return fail(f.Forbidden)
	}
	w, err := a.assignmentWitness(ctx, r.Actor, r.ProjectID, r.Run)
	if err != nil {
		return err
	}
	p, ok := r.Plan.(*skillAssignmentRunPlan)
	if !ok || p == nil || p.owner != a || p.witness != w || !p.actor.Equal(r.Actor) || p.project != r.ProjectID || p.run != r.Run || w.call.lookup {
		return fail(f.Forbidden)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, r.RequiredLocks()); err != nil {
		return portError(err)
	}
	return a.withCurrent(ctx, tx, w, func(_ context.Context, raw []byte) error {
		args, e := sc.DecodeAssignmentToolArguments(raw)
		if e != nil || args.Action != "catalog" || args.Limit != r.Limit {
			return fail(f.Forbidden)
		}
		if r.Target == nil {
			if args.AgentID.Validate() == nil {
				return fail(f.Forbidden)
			}
		} else if args.AgentID != *r.Target {
			return fail(f.Forbidden)
		}
		if (args.AfterAgentID == nil) != (r.AfterID == nil) || (args.AfterAgentCreated == nil) != (r.AfterCreated == nil) {
			return fail(f.Forbidden)
		}
		if r.AfterID != nil && (*args.AfterAgentID != *r.AfterID || *args.AfterAgentCreated != *r.AfterCreated) {
			return fail(f.Forbidden)
		}
		return nil
	})
}

var _ ag.SkillAssignmentCatalogRunAuthority = (*BuiltinAuthority)(nil)
