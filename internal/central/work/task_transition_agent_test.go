package work

import (
	"context"
	"encoding/json"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"testing"
)

func TestTaskTransitionAgentIdentityAndCurrentAuthority(t *testing.T) {
	r, _, _ := pureTransitionRecord(t)
	actor, err := i.NewAgentRun(r.Input.Project, pureID[i.Agent](t, 61), pureID[i.Execution](t, 62))
	if err != nil {
		t.Fatal(err)
	}
	r.Input.User = i.UserID{}
	r.Input.Agent = pureID[i.Agent](t, 61)
	r.Input.Execution = pureID[i.Execution](t, 62)
	r.Semantic, err = r.Input.semantic(actor, r.Key)
	if err != nil {
		t.Fatal(err)
	}
	for n := range r.Plan.History {
		r.Plan.History[n].Actor = r.Input.historyActor()
	}
	factory, err := c.RegisterTaskTransitionEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	_, ev, err := factory.NewTaskTransitionData(r.Plan.Header, r.Plan.Before, r.Plan.After.Task, r.Input.Request, r.Plan.History, r.Plan.Source, r.Plan.Target)
	if err != nil {
		t.Fatal(err)
	}
	r.Plan.Payload = ev.PayloadBytes()
	if err = validateTransitionRecord(r, actor); err != nil {
		t.Fatal("Agent history lost original execution", err)
	}
	raw, err := json.Marshal(r.Input)
	if err != nil {
		t.Fatal(err)
	}
	var decoded transitionInput
	if err = json.Unmarshal(raw, &decoded); err != nil || !sameValue(decoded, r.Input) {
		t.Fatal("persisted identity", err)
	}
	other, _ := i.NewAgentRun(r.Input.Project, r.Input.Agent, pureID[i.Execution](t, 63))
	if validateTransitionRecord(r, other) == nil {
		t.Fatal("replacement Execution recovered original command")
	}
	store, deps := pureTransitionPorts(t)
	s, err := NewTaskTransition(store, deps)
	if err != nil {
		t.Fatal(err)
	}
	meta := f.CommandMeta{RequestID: pureID[f.Request](t, 64), IdempotencyKey: r.Key, ExpectedVersion: &r.Input.Expected}
	_, err = s.TransferTask(context.Background(), actor, meta, r.Input.Project, r.Input.Task, r.Input.Request)
	pureCode(t, err, f.DependencyUnbound)
	_, err = s.ReadTask(context.Background(), actor, r.Input.Project, r.Input.Task)
	pureCode(t, err, f.DependencyUnbound)
	if store.touches.Load() != 0 {
		t.Fatal("Actor DTO reached SQL without Tool authority")
	}
}

func TestTaskTransitionActiveReviewKeepsOriginalExecution(t *testing.T) {
	r, _, _ := pureTransitionRecord(t)
	store, deps := pureTransitionPorts(t)
	s, err := NewTaskTransition(store, deps)
	if err != nil {
		t.Fatal(err)
	}
	a := pureID[i.Agent](t, 61)
	e := pureID[i.Execution](t, 62)
	actor, _ := i.NewAgentRun(r.Input.Project, a, e)
	before := r.Plan.Before.Clone()
	before.State = c.TaskStateInProgress
	before.AssigneeAgentID = &a
	occupancy := ec.ExecutionOccupancy{Active: []ec.ActiveTaskExecution{{TaskID: before.ID.String(), ExecutionID: e, AgentID: a}}, HistoryTaskIDs: []string{before.ID.String()}}
	request := c.TaskTransfer{TargetState: c.TaskStateInReview}
	if err = s.checkTransitionOccupancy(context.Background(), actor, before, request, occupancy); err != nil {
		t.Fatal(err)
	}
	occupancy.Active[0].ExecutionID = pureID[i.Execution](t, 63)
	pureCode(t, s.checkTransitionOccupancy(context.Background(), actor, before, request, occupancy), f.Forbidden)
	if store.touches.Load() != 0 {
		t.Fatal("pure occupancy check used SQL")
	}
}

func TestTaskTransitionCancellationPlanCannotBeForged(t *testing.T) {
	store, deps := pureTransitionPorts(t)
	s, err := NewTaskTransition(store, deps)
	if err != nil {
		t.Fatal(err)
	}
	_, actor, _ := pureTransitionRecord(t)
	_, err = s.ApplyExecutionCancellationInTx(context.Background(), f.NewTx(), actor, ec.CancellationScope{}, foreignTransitionPlan{})
	pureCode(t, err, f.Forbidden)
	pureCode(t, s.RequireTaskCancellationInTx(context.Background(), f.NewTx(), actor, ec.CancellationScope{}), f.Forbidden)
	if store.touches.Load() != 0 {
		t.Fatal("public scope/foreign plan reached SQL")
	}
}

func TestTaskTransitionLookupCannotAuthorizeMutation(t *testing.T) {
	store, deps := pureTransitionPorts(t)
	s, err := NewTaskTransition(store, deps)
	if err != nil {
		t.Fatal(err)
	}
	r, _, _ := pureTransitionRecord(t)
	actor, _ := i.NewAgentRun(r.Input.Project, pureID[i.Agent](t, 61), pureID[i.Execution](t, 62))
	ctx := context.WithValue(context.Background(), transitionAccessKey{}, &transitionAccess{owner: s.state(), actor: actor.Details(), project: r.Input.Project, task: r.Input.Task, intent: c.TaskToolLookupTransfer})
	_, err = deps.Authority.requireTransitionAccess(ctx, f.NewTx(), actor, r.Input.Project, i.Mutate)
	pureCode(t, err, f.Forbidden)
	if store.touches.Load() != 0 {
		t.Fatal("lookup witness reached mutation SQL")
	}
}
