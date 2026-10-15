package runtime

import (
	"context"
	ag "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"strings"
	"testing"
)

func TestSkillAssignmentRequiresOriginalPrivateToolWitness(t *testing.T) {
	const raw = "01900000-0000-7000-8000-000000000077"
	p, _ := f.ParseID[i.Project](raw)
	a, _ := f.ParseID[i.Agent](raw)
	e, _ := f.ParseID[i.Execution](raw)
	tool, _ := f.ParseID[i.Tool](raw)
	actor, _ := i.NewAgentRun(p, a, e)
	run := ag.SkillAssignmentRun{CallerAgentID: a, ExecutionID: e, OperationID: raw, AttemptID: raw, RequestID: raw, ToolCallID: raw, ToolID: tool, SpecRevision: 1, InputDigest: f.Digest("sha256:" + strings.Repeat("a", 64))}
	authority := &BuiltinAuthority{}
	if _, err := authority.DiscoverSkillAssignmentRun(context.Background(), actor, p, run); err == nil {
		t.Fatal("plausible tuple created private assignment authority")
	}
	w := &builtinWitness{}
	w.active.Store(false)
	if _, err := authority.DiscoverSkillAssignmentRun(context.WithValue(context.Background(), builtinWitnessKey{}, w), actor, p, run); err == nil {
		t.Fatal("expired witness accepted")
	}
}

type skillCatalogPlanControl struct{}

func (skillCatalogPlanControl) RequiredLocks() []f.LockRequest { return nil }
func TestSkillCatalogRejectsBareOrExpiredAssignmentSource(t *testing.T) {
	const raw = "01900000-0000-7000-8000-000000000077"
	p, _ := f.ParseID[i.Project](raw)
	a, _ := f.ParseID[i.Agent](raw)
	e, _ := f.ParseID[i.Execution](raw)
	tool, _ := f.ParseID[i.Tool](raw)
	actor, _ := i.NewAgentRun(p, a, e)
	run := ag.SkillAssignmentRun{CallerAgentID: a, ExecutionID: e, OperationID: raw, AttemptID: raw, RequestID: raw, ToolCallID: raw, ToolID: tool, SpecRevision: 1, InputDigest: f.Digest("sha256:" + strings.Repeat("a", 64))}
	r := ag.SkillAssignmentCatalogRequest{Actor: actor, ProjectID: p, Run: run, Plan: skillCatalogPlanControl{}, Limit: 20}
	if r.Validate() != nil {
		t.Fatal("fixture malformed")
	}
	authority := &BuiltinAuthority{}
	if authority.RequireSkillAssignmentCatalogInTx(context.Background(), f.Tx{}, r) == nil {
		t.Fatal("DTO granted metadata")
	}
	w := &builtinWitness{}
	if authority.RequireSkillAssignmentCatalogInTx(context.WithValue(context.Background(), builtinWitnessKey{}, w), f.Tx{}, r) == nil {
		t.Fatal("expired loan granted metadata")
	}
}
