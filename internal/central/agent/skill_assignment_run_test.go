package agent

import (
	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"strings"
	"testing"
)

type skillAssignmentRunPlanControl []f.LockRequest

func (p skillAssignmentRunPlanControl) RequiredLocks() []f.LockRequest {
	return append([]f.LockRequest(nil), p...)
}
func TestAgentSkillAssignmentRunKeepsTargetIndependentAndHumanClosed(t *testing.T) {
	r, _ := skillAssignmentRequestFixture(t)
	caller := commandID[i.Agent](t, "01900000-0000-7000-8000-000000000099")
	execution := commandID[i.Execution](t, "01900000-0000-7000-8000-000000000098")
	actor, _ := i.NewAgentRun(r.ProjectID, caller, execution)
	const op = "01900000-0000-7000-8000-000000000097"
	run := c.SkillAssignmentRun{CallerAgentID: caller, ExecutionID: execution, OperationID: op, AttemptID: op, RequestID: op, ToolCallID: op, ToolID: commandID[i.Tool](t, op), SpecRevision: 1, InputDigest: f.Digest("sha256:" + strings.Repeat("a", 64))}
	human := r
	r.Actor = actor
	r.Run = &run
	r.RunPlan = skillAssignmentRunPlanControl{}
	if r.AgentID == caller || r.Validate() != nil {
		t.Fatal("same-project different target was incorrectly forbidden")
	}
	human.Run = &run
	human.RunPlan = r.RunPlan
	if human.Validate() == nil {
		t.Fatal("Human mixed with Agent provenance")
	}
	bad := r
	changed := run
	changed.ExecutionID = commandID[i.Execution](t, op)
	bad.Run = &changed
	if bad.Validate() == nil {
		t.Fatal("caller Execution changed")
	}
}
