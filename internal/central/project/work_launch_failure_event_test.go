package project

import (
	"context"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestTaskLaunchFailureProjectGatePausedAndExactSchema(t *testing.T) {
	x, config := schedulerFactsFixture(t)
	config.Enabled = true
	reg, _ := i.RegisterService(i.Scheduler)
	scope, _ := i.InProject(x.project)
	actor, _ := reg.Actor(testID[struct{}](t).String(), scope)
	project, _ := f.ParseID[event.Project](x.project.String())
	at, _ := f.NewInstant(time.Now())
	version := f.Version(4)
	d := oc.ProjectRequestDetails{Kind: oc.AppendProject, ProjectID: x.project, Actor: actor, Stage: oc.CurrentAccess, Event: event.Summary{Producer: "work", Header: event.Header{EventID: testID[event.EventIdentity](t), EventType: "work.task_transitioned", SchemaVersion: 4, OccurredAt: at, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: project}, AggregateType: "work.task", AggregateID: testID[event.Aggregate](t), AggregateVersion: &version}, PayloadDigest: digest([]byte("final failure"))}}
	request, err := oc.NewProjectRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	deps, err := x.authority.Discover(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []oc.Stage{oc.CurrentAccess, oc.NewFact} {
		d.Stage = stage
		request, err = oc.NewProjectRequest(d)
		if err != nil {
			t.Fatal(err)
		}
		if err = x.authority.ValidateInTx(context.Background(), x.store.tx, request, deps); err != nil {
			t.Fatal(err)
		}
		config.Enabled = false
		hasCode(t, x.authority.ValidateInTx(context.Background(), x.store.tx, request, deps), f.InvalidState)
		config.Enabled = true
	}
	x.lifecycle = c.Archived
	hasCode(t, x.authority.ValidateInTx(context.Background(), x.store.tx, request, deps), f.ProjectNotActive)
	x.lifecycle = c.Active
	// Even another accepted schema cannot borrow schema4's bound dependency.
	d.Event.Header.SchemaVersion = 2
	request, err = oc.NewProjectRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	hasCode(t, x.authority.ValidateInTx(context.Background(), x.store.tx, request, deps), f.Forbidden)
	d.Event.Header.SchemaVersion = 5
	d.Stage = oc.CurrentAccess
	request, err = oc.NewProjectRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = x.authority.Discover(context.Background(), request); err == nil {
		t.Fatal("unknown Scheduler schema accepted")
	}
}
