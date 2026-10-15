package skill

import (
	"context"
	"testing"

	e "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type assignmentEventRunPlan struct{}

func (assignmentEventRunPlan) RequiredLocks() []f.LockRequest { return nil }

func TestAssignmentProjectEventRequiresExactPrivateSource(t *testing.T) {
	service, store, _, _, _ := readFixture(t)
	in, run := assignmentRunControl(t)
	events, err := sc.RegisterAssignmentEvents(e.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	facts, err := NewAssignmentAuthority(service.state().authority, events)
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := NewAssignmentAuditFacts(store)
	if err != nil {
		t.Fatal(err)
	}
	commands := &AssignmentCommands{deps: AssignmentDependencies{Service: service}, facts: facts}
	owner := &RuntimeAssignmentCommands{commands: commands}
	w := &assignmentRunWitness{owner: owner, actor: in.Actor, project: in.Request.ProjectID, run: run, input: in, plan: assignmentEventRunPlan{}}
	w.active.Store(true)
	ctx := context.WithValue(context.Background(), assignmentRunKey{}, w)
	at := stateRow(t).created
	version := f.Version(2)
	project, _ := f.ParseID[e.Project](in.Request.ProjectID.String())
	target, _ := f.ParseID[e.Aggregate](in.Request.AgentID.String())
	summary := e.Summary{Producer: sc.AssignmentProducer, Header: e.Header{EventID: stateID[e.EventIdentity](650), EventType: sc.AssignmentAddedEvent, SchemaVersion: 1, OccurredAt: at, Scope: e.Scope{Kind: e.ProjectScope, ProjectID: project}, AggregateType: sc.AssignmentAggregate, AggregateID: target, AggregateVersion: &version}, PayloadDigest: in.Digest}
	if got, err := bridge.assignmentProjectEventOwner(ctx, in.Actor, summary); err != nil || got != facts {
		t.Fatal("original source rejected", err)
	}
	if _, err := bridge.DiscoverAssignmentProjectEvent(context.Background(), in.Actor, summary); err == nil {
		t.Fatal("public summary created a grant")
	}
	w.active.Store(false)
	if _, err := bridge.assignmentProjectEventOwner(ctx, in.Actor, summary); err == nil {
		t.Fatal("expired source accepted")
	}
	w.active.Store(true)
	changed := summary
	changed.Header.AggregateID = stateID[e.Aggregate](651)
	if _, err := bridge.assignmentProjectEventOwner(ctx, in.Actor, changed); err == nil {
		t.Fatal("source rebound target")
	}
	other := *bridge
	_, foreignStore, _, _, _ := readFixture(t)
	other.store = foreignStore
	if _, err := other.assignmentProjectEventOwner(ctx, in.Actor, summary); err == nil {
		t.Fatal("source crossed fixed Store")
	}
	// Even the same Agent/Execution cannot borrow another command's event.
	foreign := in
	changedRun := run
	changedRun.AttemptID = stateID[struct{}](652).String()
	foreign, err = newAssignmentRunInput(in.Actor, in.Meta, in.Request, &changedRun)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := facts.eventBinding(ctx, in.Actor, summary, &assignmentCommand{Input: foreign}); err == nil {
		t.Fatal("event plan rebound original attempt")
	}
}
