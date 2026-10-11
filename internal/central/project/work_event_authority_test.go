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

func blockerProjectRequest(t *testing.T, x *auditGateFixture) oc.ProjectRequest {
	t.Helper()
	at, _ := f.NewInstant(time.Now())
	project, _ := f.ParseID[event.Project](x.project.String())
	version := f.Version(2)
	r, e := oc.NewProjectRequest(oc.ProjectRequestDetails{Kind: oc.AppendProject, ProjectID: x.project, Actor: x.actor, Stage: oc.CurrentAccess, Event: event.Summary{Producer: "work", Header: event.Header{EventID: testID[event.EventIdentity](t), EventType: "work.task_blockers_changed", SchemaVersion: 1, OccurredAt: at, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: project}, AggregateType: "work.task", AggregateID: testID[event.Aggregate](t), AggregateVersion: &version}, PayloadDigest: digest([]byte("blocker payload"))}})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestTaskBlockerProjectGateBothStagesAndExactTriple(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	request := blockerProjectRequest(t, x)
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
	for _, kind := range []event.StableName{"work.task_transitions_changed", "work.task_blocker_changed"} {
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

func TestTaskTransitionProjectGateBothStages(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	d := blockerProjectRequest(t, x).Details()
	d.Event.Header.EventType = "work.task_transitioned"
	request, err := oc.NewProjectRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	deps, err := x.a.Discover(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []oc.Stage{oc.CurrentAccess, oc.NewFact} {
		d.Stage = stage
		r, err := oc.NewProjectRequest(d)
		if err != nil {
			t.Fatal(err)
		}
		if err = x.a.ValidateInTx(context.Background(), x.store.tx, r, deps); err != nil {
			t.Fatal(err)
		}
	}
	x.lifecycle = c.Archived
	d.Stage = oc.NewFact
	r, err := oc.NewProjectRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	hasCode(t, x.a.ValidateInTx(context.Background(), x.store.tx, r, deps), f.ProjectNotActive)
	d.Stage = oc.CurrentAccess
	r, err = oc.NewProjectRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	if err = x.a.ValidateInTx(context.Background(), x.store.tx, r, deps); err != nil {
		t.Fatal(err)
	}
	x.sessionErr = fault(f.SessionRevoked)
	hasCode(t, x.a.ValidateInTx(context.Background(), x.store.tx, r, deps), f.SessionRevoked)
}

func TestSchedulerClaimProjectEventExactGate(t *testing.T) {
	x, config := schedulerFactsFixture(t)
	config.Enabled = true
	reg, err := i.RegisterService(i.Scheduler)
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := i.InProject(x.project)
	actor, err := reg.Actor(testID[struct{}](t).String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	project, _ := f.ParseID[event.Project](x.project.String())
	at, _ := f.NewInstant(time.Now())
	version := f.Version(3)
	details := oc.ProjectRequestDetails{Kind: oc.AppendProject, ProjectID: x.project, Actor: actor, Stage: oc.CurrentAccess, Event: event.Summary{Producer: "work", Header: event.Header{EventID: testID[event.EventIdentity](t), EventType: "work.task_transitioned", SchemaVersion: 2, OccurredAt: at, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: project}, AggregateType: "work.task", AggregateID: testID[event.Aggregate](t), AggregateVersion: &version}, PayloadDigest: digest([]byte("claim"))}}
	request, err := oc.NewProjectRequest(details)
	if err != nil {
		t.Fatal(err)
	}
	deps, err := x.authority.Discover(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []oc.Stage{oc.CurrentAccess, oc.NewFact} {
		details.Stage = stage
		request, err = oc.NewProjectRequest(details)
		if err != nil {
			t.Fatal(err)
		}
		if err = x.authority.ValidateInTx(context.Background(), x.store.tx, request, deps); err != nil {
			t.Fatal("current scheduler Project gate", err)
		}
	}
	config.Enabled = false
	hasCode(t, x.authority.ValidateInTx(context.Background(), x.store.tx, request, deps), f.InvalidState)
	config.Enabled = true
	x.lifecycle = c.Archived
	hasCode(t, x.authority.ValidateInTx(context.Background(), x.store.tx, request, deps), f.ProjectNotActive)
	x.lifecycle = c.Active
	for _, schema := range []uint32{1, 6} {
		bad := details
		bad.Stage = oc.CurrentAccess
		bad.Event.Header.SchemaVersion = schema
		r, e := oc.NewProjectRequest(bad)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = x.authority.Discover(context.Background(), r); e == nil {
			t.Fatal("Scheduler broadened another schema")
		}
	}
	// A different stable cause cannot reuse the original dependency plan.
	different, _ := reg.Actor(testID[struct{}](t).String(), scope)
	bad := details
	bad.Actor = different
	r, err := oc.NewProjectRequest(bad)
	if err != nil {
		t.Fatal(err)
	}
	hasCode(t, x.authority.ValidateInTx(context.Background(), x.store.tx, r, deps), f.Forbidden)
}

func TestTaskBusyCompensationProjectGatePausedAndExactSchema(t *testing.T) {
	x, config := schedulerFactsFixture(t)
	config.Enabled = true
	reg, _ := i.RegisterService(i.Scheduler)
	scope, _ := i.InProject(x.project)
	actor, _ := reg.Actor(testID[struct{}](t).String(), scope)
	project, _ := f.ParseID[event.Project](x.project.String())
	at, _ := f.NewInstant(time.Now())
	version := f.Version(4)
	d := oc.ProjectRequestDetails{Kind: oc.AppendProject, ProjectID: x.project, Actor: actor, Stage: oc.CurrentAccess, Event: event.Summary{Producer: "work", Header: event.Header{EventID: testID[event.EventIdentity](t), EventType: "work.task_transitioned", SchemaVersion: 3, OccurredAt: at, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: project}, AggregateType: "work.task", AggregateID: testID[event.Aggregate](t), AggregateVersion: &version}, PayloadDigest: digest([]byte("busy"))}}
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
	// Even another accepted schema cannot borrow schema3's bound dependency.
	d.Event.Header.SchemaVersion = 2
	request, err = oc.NewProjectRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	hasCode(t, x.authority.ValidateInTx(context.Background(), x.store.tx, request, deps), f.Forbidden)
	d.Event.Header.SchemaVersion = 6
	d.Stage = oc.CurrentAccess
	request, err = oc.NewProjectRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = x.authority.Discover(context.Background(), request); err == nil {
		t.Fatal("unknown Scheduler schema accepted")
	}
}
