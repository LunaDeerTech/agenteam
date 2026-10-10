package project

import (
	"context"
	"errors"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func agentProjectRequest(t *testing.T, x *auditGateFixture) oc.ProjectRequest {
	t.Helper()
	at, _ := f.NewInstant(time.Now())
	project, _ := f.ParseID[event.Project](x.project.String())
	version := f.Version(2)
	r, err := oc.NewProjectRequest(oc.ProjectRequestDetails{Kind: oc.AppendProject, ProjectID: x.project, Actor: x.actor, Stage: oc.CurrentAccess, Event: event.Summary{Producer: "agent", Header: event.Header{EventID: testID[event.EventIdentity](t), EventType: "agent.config_changed", SchemaVersion: 1, OccurredAt: at, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: project}, AggregateType: "agent.config", AggregateID: testID[event.Aggregate](t), AggregateVersion: &version}, PayloadDigest: digest([]byte("original typed Agent payload"))}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAgentProjectEventGateUsesCurrentOwnerAndHeldTransaction(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	r := agentProjectRequest(t, x)
	deps, err := x.a.Discover(context.Background(), r)
	if err != nil || len(x.order) != 0 {
		t.Fatal("discovery queried Project/Agent", err, x.order)
	}
	locks := deps.Locks()
	if len(locks) != 2 || locks[0].Key.Canonical() != userLock(x.actor.Details().UserID, f.Shared).Key.Canonical() || locks[1].Key.Canonical() != projectLock(x.project, f.Shared).Key.Canonical() || locks[0].Mode != f.Shared || locks[1].Mode != f.Shared {
		t.Fatal("wrong current-Project minimum lock set")
	}
	for _, state := range []pc.Lifecycle{pc.Active, pc.Archived, pc.Deleting} {
		x.lifecycle = state
		for _, stage := range []oc.Stage{oc.CurrentAccess, oc.NewFact} {
			d := r.Details()
			d.Stage = stage
			next, e := oc.NewProjectRequest(d)
			if e != nil {
				t.Fatal(e)
			}
			e = x.a.ValidateInTx(context.Background(), x.store.tx, next, deps)
			if state == pc.Active || state == pc.Archived && stage == oc.CurrentAccess {
				if e != nil {
					t.Fatal("valid Project gate", state, stage, e)
				}
			} else {
				hasCode(t, e, f.ProjectNotActive)
			}
		}
	}
	x.lifecycle, x.initialized = pc.Active, false
	hasCode(t, x.a.ValidateInTx(context.Background(), x.store.tx, r, deps), f.ProjectNotActive)
	x.initialized, x.sessionErr = true, fault(f.SessionRevoked)
	hasCode(t, x.a.ValidateInTx(context.Background(), x.store.tx, r, deps), f.SessionRevoked)
	x.sessionErr = nil
	hasCode(t, x.a.ValidateInTx(context.Background(), f.NewTx(), r, deps), f.DependencyUnavailable)
	x.store.lockErr = errors.New("missing held union")
	hasCode(t, x.a.ValidateInTx(context.Background(), x.store.tx, r, deps), f.DependencyUnavailable)
	x.store.lockErr, x.order = nil, nil
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = x.a.Discover(canceled, r); !errors.Is(err, context.Canceled) {
		t.Fatal("discovery lost cancellation", err)
	}
	if err = x.a.ValidateInTx(canceled, x.store.tx, r, deps); !errors.Is(err, context.Canceled) || len(x.order) != 0 {
		t.Fatal("canceled validation reached store", err)
	}
}

func TestAgentProjectEventPlanRejectsChangedSummaryAndForeignIssuer(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	request := agentProjectRequest(t, x)
	deps, err := x.a.Discover(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*oc.ProjectRequestDetails){
		"session": func(d *oc.ProjectRequestDetails) {
			u, _ := f.ParseID[id.User](x.actor.Details().UserID)
			d.Actor, _ = id.NewHuman(u, testID[id.Session](t))
		},
		"event":          func(d *oc.ProjectRequestDetails) { d.Event.Header.EventID = testID[event.EventIdentity](t) },
		"aggregate":      func(d *oc.ProjectRequestDetails) { d.Event.Header.AggregateID = testID[event.Aggregate](t) },
		"digest":         func(d *oc.ProjectRequestDetails) { d.Event.PayloadDigest = digest([]byte("other payload")) },
		"version":        func(d *oc.ProjectRequestDetails) { v := f.Version(3); d.Event.Header.AggregateVersion = &v },
		"schema":         func(d *oc.ProjectRequestDetails) { d.Event.Header.SchemaVersion = 2 },
		"type":           func(d *oc.ProjectRequestDetails) { d.Event.Header.EventType = "agent.executed" },
		"aggregate-type": func(d *oc.ProjectRequestDetails) { d.Event.Header.AggregateType = "agent.run" },
		"sequence":       func(d *oc.ProjectRequestDetails) { v := f.Sequence(1); d.Event.Header.AggregateSequence = &v },
		"project": func(d *oc.ProjectRequestDetails) {
			d.ProjectID = testID[id.Project](t)
			d.Event.Header.Scope.ProjectID, _ = f.ParseID[event.Project](d.ProjectID.String())
		},
		"agent-actor": func(d *oc.ProjectRequestDetails) {
			d.Actor, _ = id.NewAgentRun(x.project, testID[id.Agent](t), testID[id.Execution](t))
		},
		"service": func(d *oc.ProjectRequestDetails) {
			reg, _ := id.RegisterService(id.SecretService)
			scope, _ := id.InProject(x.project)
			d.Actor, _ = reg.Actor(testID[struct{}](t).String(), scope)
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := request.Details()
			mutate(&d)
			r, e := oc.NewProjectRequest(d)
			if e != nil {
				t.Fatal("invalid controlled request", e)
			}
			x.order = nil
			if e = x.a.ValidateInTx(context.Background(), x.store.tx, r, deps); e == nil || len(x.order) != 0 {
				t.Fatal("changed request reached current facts", e)
			}
			switch name {
			case "schema", "type", "aggregate-type", "sequence", "agent-actor", "service":
				if _, e = x.a.Discover(context.Background(), r); e == nil {
					t.Fatal("unsupported event shape admitted")
				}
			}
		})
	}
	other, err := NewAuthority(x.store, AuthorityDependencies{Sessions: x.a.state().sessions})
	if err != nil {
		t.Fatal(err)
	}
	hasCode(t, other.ValidateInTx(context.Background(), x.store.tx, request, deps), f.Forbidden)
	binding, _, err := agentEventBinding(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"missing-lock", "other-project", "other-purpose"} {
		locks, purpose := deps.Locks(), []byte(agentEventPurpose)
		switch change {
		case "missing-lock":
			locks = locks[:1]
		case "other-project":
			locks[1] = projectLock(testID[id.Project](t), f.Shared)
		case "other-purpose":
			purpose = []byte("project.knowledge.append-v1")
		}
		bad, e := oc.NewDependencies(x.a.state().projectIssuer, binding, locks, purpose)
		if e != nil {
			t.Fatal(e)
		}
		hasCode(t, x.a.ValidateInTx(context.Background(), x.store.tx, request, bad), f.Forbidden)
	}
	d := request.Details()
	d.Stage = oc.NewFact
	next, err := oc.NewProjectRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	_, err = x.a.Discover(context.Background(), next)
	hasCode(t, err, f.Forbidden)
}
