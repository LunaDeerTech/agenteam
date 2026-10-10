package project

import (
	"context"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestSecretVariableProjectEventExactRouteCurrentOwnerAndBothStages(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	ordinary := variableProjectRequest(t, x)
	details := ordinary.Details()
	details.Event.Header.EventType = "project.secret_variable_changed"
	request, err := oc.NewProjectRequest(details)
	if err != nil {
		t.Fatal(err)
	}
	deps, err := x.a.Discover(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(x.order) != 0 {
		t.Fatal("discovery performed IO")
	}
	for _, phase := range []c.Lifecycle{c.Active, c.Archiving, c.Archived, c.Deleting} {
		x.lifecycle = phase
		for _, stage := range []oc.Stage{oc.CurrentAccess, oc.NewFact} {
			d := details
			d.Stage = stage
			r, err := oc.NewProjectRequest(d)
			if err != nil {
				t.Fatal(err)
			}
			err = x.a.ValidateInTx(context.Background(), x.store.tx, r, deps)
			if phase == c.Active || (phase == c.Archiving || phase == c.Archived) && stage == oc.CurrentAccess {
				if err != nil {
					t.Fatalf("%s/%s: %v", phase, stage, err)
				}
			} else {
				hasCode(t, err, f.ProjectNotActive)
			}
		}
	}
	x.lifecycle = c.Active
	x.sessionErr = fault(f.SessionRevoked)
	hasCode(t, x.a.ValidateInTx(context.Background(), x.store.tx, request, deps), f.SessionRevoked)
	x.sessionErr = nil
	if err = x.a.ValidateInTx(context.Background(), x.store.tx, ordinary, deps); err == nil {
		t.Fatal("Secret dependencies authorize ordinary event")
	}
	if _, _, err = projectVariableEventBinding(request); err == nil {
		t.Fatal("ordinary predicate was widened")
	}
	if err = x.a.ValidateInTx(context.Background(), f.NewTx(), request, deps); err == nil {
		t.Fatal("foreign transaction")
	}
	for _, change := range []func(*oc.ProjectRequestDetails){func(d *oc.ProjectRequestDetails) { d.Event.Producer = "work" }, func(d *oc.ProjectRequestDetails) { d.Event.Header.AggregateType = "project.project" }, func(d *oc.ProjectRequestDetails) { d.Event.Header.SchemaVersion = 2 }, func(d *oc.ProjectRequestDetails) { d.Event.Header.EventType = "project.secret_variable_other" }, func(d *oc.ProjectRequestDetails) { d.Stage = oc.NewFact }} {
		d := details
		change(&d)
		r, err := oc.NewProjectRequest(d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = x.a.Discover(context.Background(), r); err == nil {
			t.Fatal("wrong closed event admitted")
		}
	}
	d := details
	role, _ := i.RegisterService(i.SecretService)
	scope, _ := i.InProject(x.project)
	d.Actor, _ = role.Actor(testID[struct{}](t).String(), scope)
	r, err := oc.NewProjectRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = x.a.Discover(context.Background(), r); err == nil {
		t.Fatal("Service actor borrowed Human gate")
	}
	d = details
	d.Event.Header.EventID = testID[event.EventIdentity](t)
	r, err = oc.NewProjectRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	if err = x.a.ValidateInTx(context.Background(), x.store.tx, r, deps); err == nil {
		t.Fatal("different event reused dependencies")
	}
}
