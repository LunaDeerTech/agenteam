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

func variableProjectRequest(t *testing.T, x *auditGateFixture) oc.ProjectRequest {
	t.Helper()
	at, _ := f.NewInstant(time.Now())
	project, _ := f.ParseID[event.Project](x.project.String())
	version := f.Version(2)
	r, e := oc.NewProjectRequest(oc.ProjectRequestDetails{Kind: oc.AppendProject, ProjectID: x.project, Actor: x.actor, Stage: oc.CurrentAccess, Event: event.Summary{Producer: "projectvariable", Header: event.Header{EventID: testID[event.EventIdentity](t), EventType: "project.variable_changed", SchemaVersion: 1, OccurredAt: at, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: project}, AggregateType: "project.variable", AggregateID: testID[event.Aggregate](t), AggregateVersion: &version}, PayloadDigest: digest([]byte("variable payload"))}})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestProjectVariableProjectGateBothStagesAndExactTriple(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	request := variableProjectRequest(t, x)
	deps, e := x.a.Discover(context.Background(), request)
	if e != nil {
		t.Fatal(e)
	}
	if len(x.order) != 0 {
		t.Fatal("discovery queried")
	}
	for _, phase := range []c.Lifecycle{c.Active, c.Archiving, c.Archived, c.Deleting} {
		x.lifecycle = phase
		for _, stage := range []oc.Stage{oc.CurrentAccess, oc.NewFact} {
			d := request.Details()
			d.Stage = stage
			r, e := oc.NewProjectRequest(d)
			if e != nil {
				t.Fatal(e)
			}
			e = x.a.ValidateInTx(context.Background(), x.store.tx, r, deps)
			if phase == c.Active || (phase == c.Archiving || phase == c.Archived) && stage == oc.CurrentAccess {
				if e != nil {
					t.Fatalf("%s/%s: %v", phase, stage, e)
				}
			} else {
				hasCode(t, e, f.ProjectNotActive)
			}
		}
	}
	x.lifecycle = c.Active
	x.sessionErr = fault(f.SessionRevoked)
	hasCode(t, x.a.ValidateInTx(context.Background(), x.store.tx, request, deps), f.SessionRevoked)
	x.sessionErr = nil
	for _, kind := range []event.StableName{"work.task_transitioned", "work.task_blocker_changed"} {
		d := request.Details()
		d.Event.Header.EventType = kind
		r, e := oc.NewProjectRequest(d)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = x.a.Discover(context.Background(), r); e == nil {
			t.Fatal("unbound triple admitted")
		}
	}
	d := request.Details()
	role, _ := i.RegisterService(i.SecretService)
	scope, _ := i.InProject(x.project)
	d.Actor, _ = role.Actor(testID[struct{}](t).String(), scope)
	r, e := oc.NewProjectRequest(d)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = x.a.Discover(context.Background(), r); e == nil {
		t.Fatal("Service admitted")
	}
	hasCode(t, x.a.ValidateInTx(context.Background(), f.NewTx(), request, deps), f.DependencyUnavailable)
}
