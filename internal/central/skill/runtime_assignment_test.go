package skill

import (
	"context"
	"strings"
	"testing"

	ag "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func assignmentRunControl(t *testing.T) (assignmentInput, ag.SkillAssignmentRun) {
	t.Helper()
	human, _ := i.NewHuman(stateID[i.User](111), stateID[i.Session](112))
	in, _ := assignmentReceiptControl(t, human)
	run := ag.SkillAssignmentRun{CallerAgentID: stateID[i.Agent](501), ExecutionID: stateID[i.Execution](502), OperationID: stateID[struct{}](503).String(), AttemptID: stateID[struct{}](504).String(), RequestID: stateID[struct{}](505).String(), ToolCallID: stateID[struct{}](506).String(), ToolID: stateID[i.Tool](507), SpecRevision: 1, InputDigest: f.Digest("sha256:" + strings.Repeat("b", 64))}
	actor, _ := i.NewAgentRun(in.Request.ProjectID, run.CallerAgentID, run.ExecutionID)
	in.Meta.IdempotencyKey = f.IdempotencyKey(run.OperationID)
	in.Meta.RequestID = stateID[f.Request](505)
	in, err := newAssignmentRunInput(actor, in.Meta, in.Request, &run)
	if err != nil {
		t.Fatal(err)
	}
	return in, run
}
func TestRuntimeAssignmentOriginAndPrivateAdmission(t *testing.T) {
	in, run := assignmentRunControl(t)
	if in.Request.AgentID == run.CallerAgentID {
		t.Fatal("fixture must target another agent")
	}
	if _, err := newAssignmentInput(in.Actor, in.Meta, in.Request); err == nil {
		t.Fatal("Human entry accepted Agent DTO")
	}
	s := &AssignmentCommands{}
	if _, err := s.input(context.Background(), in.Actor, in.Meta, in.Request); err == nil {
		t.Fatal("bare Agent origin became authority")
	}
	for _, mutate := range []func(*ag.SkillAssignmentRun){
		func(r *ag.SkillAssignmentRun) { r.AttemptID = stateID[struct{}](508).String() },
		func(r *ag.SkillAssignmentRun) { r.ToolCallID = stateID[struct{}](509).String() },
		func(r *ag.SkillAssignmentRun) { r.SpecRevision++ },
		func(r *ag.SkillAssignmentRun) { r.InputDigest = f.Digest("sha256:" + strings.Repeat("c", 64)) },
	} {
		changed := run
		mutate(&changed)
		next, err := newAssignmentRunInput(in.Actor, in.Meta, in.Request, &changed)
		if err != nil || next.Digest == in.Digest {
			t.Fatal("original invocation lost from immutable intent")
		}
	}
	owner := &RuntimeAssignmentCommands{commands: s}
	w := &assignmentRunWitness{owner: owner, actor: in.Actor, project: in.Request.ProjectID, run: run, input: in}
	w.active.Store(true)
	ctx := context.WithValue(context.Background(), assignmentRunKey{}, w)
	if _, err := s.input(ctx, in.Actor, in.Meta, in.Request); err != nil {
		t.Fatal(err)
	}
	altered := in.Request.clone()
	altered.AgentID = stateID[i.Agent](510)
	if _, err := s.input(ctx, in.Actor, in.Meta, altered); err == nil {
		t.Fatal("source loan rebound target")
	}
	w.active.Store(false)
	if _, err := s.input(ctx, in.Actor, in.Meta, in.Request); err == nil {
		t.Fatal("expired source reused")
	}
	var intent assignmentIntent
	if assignmentDecode(in.Raw, &intent, 16384) != nil || intent.Run == nil || *intent.Run != run {
		t.Fatal("immutable source did not round-trip")
	}
}
