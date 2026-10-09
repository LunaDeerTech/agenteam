package project

import (
	"context"
	"errors"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func knowledgeProjectRequest(t *testing.T, x *auditGateFixture, kind event.StableName) oc.ProjectRequest {
	t.Helper()
	at, _ := f.NewInstant(time.Now())
	project, _ := f.ParseID[event.Project](x.project.String())
	version := f.Version(2)
	r, err := oc.NewProjectRequest(oc.ProjectRequestDetails{Kind: oc.AppendProject, ProjectID: x.project, Actor: x.actor, Stage: oc.CurrentAccess, Event: event.Summary{Producer: "knowledge", Header: event.Header{EventID: testID[event.EventIdentity](t), EventType: kind, SchemaVersion: 1, OccurredAt: at, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: project}, AggregateType: "knowledge_document", AggregateID: testID[event.Aggregate](t), AggregateVersion: &version}, PayloadDigest: digest([]byte("original Knowledge payload"))}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestKnowledgeProjectGateKeepsReadAndNewFactDistinct(t *testing.T) {
	for _, kind := range []event.StableName{"knowledge.content_changed", "knowledge.deleted"} {
		t.Run(string(kind), func(t *testing.T) {
			x := newAuditGateFixture(t, nil)
			r := knowledgeProjectRequest(t, x, kind)
			deps, err := x.a.Discover(context.Background(), r)
			if err != nil || len(x.order) != 0 {
				t.Fatal("discovery queried or rejected", err, x.order)
			}
			locks := deps.Locks()
			if len(locks) != 2 || locks[0].Key.Canonical() != userLock(x.actor.Details().UserID, f.Shared).Key.Canonical() || locks[1].Key.Canonical() != projectLock(x.project, f.Shared).Key.Canonical() || locks[0].Mode != f.Shared || locks[1].Mode != f.Shared {
				t.Fatal("wrong Project lock union")
			}
			for _, lifecycle := range []c.Lifecycle{c.Active, c.Archiving, c.Archived, c.Deleting} {
				x.lifecycle = lifecycle
				for _, stage := range []oc.Stage{oc.CurrentAccess, oc.NewFact} {
					d := r.Details()
					d.Stage = stage
					next, err := oc.NewProjectRequest(d)
					if err != nil {
						t.Fatal(err)
					}
					err = x.a.ValidateInTx(context.Background(), x.store.tx, next, deps)
					if lifecycle == c.Active || stage == oc.CurrentAccess && (lifecycle == c.Archiving || lifecycle == c.Archived) {
						if err != nil {
							t.Fatalf("%s/%s: %v", lifecycle, stage, err)
						}
					} else {
						hasCode(t, err, f.ProjectNotActive)
					}
				}
			}
			x.lifecycle, x.initialized = c.Active, false
			hasCode(t, x.a.ValidateInTx(context.Background(), x.store.tx, r, deps), f.ProjectNotActive)
			x.initialized, x.sessionErr = true, fault(f.SessionRevoked)
			hasCode(t, x.a.ValidateInTx(context.Background(), x.store.tx, r, deps), f.SessionRevoked)
			x.sessionErr = nil
			hasCode(t, x.a.ValidateInTx(context.Background(), f.NewTx(), r, deps), f.DependencyUnavailable)
			x.store.lockErr = errors.New("missing held lock")
			hasCode(t, x.a.ValidateInTx(context.Background(), x.store.tx, r, deps), f.DependencyUnavailable)
		})
	}
}

func TestKnowledgeProjectPlanBindsEntireRequestAndClosedTypes(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	request := knowledgeProjectRequest(t, x, "knowledge.content_changed")
	deps, err := x.a.Discover(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"session", "user", "event", "digest", "time", "aggregate-id", "aggregate-type", "version", "no-version", "schema", "type", "producer", "project", "service", "agent", "sequence"} {
		t.Run(change, func(t *testing.T) {
			d := request.Details()
			switch change {
			case "session":
				user, _ := f.ParseID[identity.User](x.actor.Details().UserID)
				d.Actor, _ = identity.NewHuman(user, testID[identity.Session](t))
			case "user":
				d.Actor = testActor(t)
			case "event":
				d.Event.Header.EventID = testID[event.EventIdentity](t)
			case "digest":
				d.Event.PayloadDigest = digest([]byte("different payload"))
			case "time":
				d.Event.Header.OccurredAt, _ = f.NewInstant(time.Now().Add(time.Hour))
			case "aggregate-id":
				d.Event.Header.AggregateID = testID[event.Aggregate](t)
			case "aggregate-type":
				d.Event.Header.AggregateType = "knowledge.other"
			case "version":
				v := f.Version(3)
				d.Event.Header.AggregateVersion = &v
			case "no-version":
				d.Event.Header.AggregateVersion = nil
			case "schema":
				d.Event.Header.SchemaVersion = 2
			case "type":
				d.Event.Header.EventType = "knowledge.indexed"
			case "producer":
				d.Event.Producer = "unbound"
			case "project":
				d.ProjectID = testID[identity.Project](t)
				d.Event.Header.Scope.ProjectID, _ = f.ParseID[event.Project](d.ProjectID.String())
			case "service":
				role, _ := identity.RegisterService(identity.SecretService)
				scope, _ := identity.InProject(x.project)
				d.Actor, _ = role.Actor(testID[struct{}](t).String(), scope)
			case "agent":
				d.Actor, _ = identity.NewAgentRun(x.project, testID[identity.Agent](t), testID[identity.Execution](t))
			case "sequence":
				v := f.Sequence(1)
				d.Event.Header.AggregateSequence = &v
			}
			r, err := oc.NewProjectRequest(d)
			if err != nil {
				if change == "no-version" {
					// The public Header constructor already rejects an absent
					// version and sequence; no forged request reaches Project.
					return
				}
				t.Fatal("invalid test stimulus", err)
			}
			x.order = nil
			if err = x.a.ValidateInTx(context.Background(), x.store.tx, r, deps); err == nil || len(x.order) != 0 {
				t.Fatal("changed request reached authorization", err, x.order)
			}
			switch change {
			case "aggregate-type", "no-version", "schema", "type", "producer", "service", "agent", "sequence":
				if _, err = x.a.Discover(context.Background(), r); err == nil {
					t.Fatal("unbound event/actor shape discovered")
				}
			}
		})
	}
	other, err := NewAuthority(x.store, AuthorityDependencies{Sessions: x.a.state().sessions})
	if err != nil {
		t.Fatal(err)
	}
	hasCode(t, other.ValidateInTx(context.Background(), x.store.tx, request, deps), f.Forbidden)
	d := request.Details()
	d.Stage = oc.NewFact
	next, _ := oc.NewProjectRequest(d)
	_, err = x.a.Discover(context.Background(), next)
	hasCode(t, err, f.Forbidden)
	// Even an internally issued plan with a changed lock set/purpose cannot
	// authorize this consumer. Public callers cannot obtain this issuer.
	binding, _, err := knowledgeEventBinding(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"missing-lock", "different-project", "wrong-purpose"} {
		locks, purpose := deps.Locks(), []byte(knowledgeEventPurpose)
		switch change {
		case "missing-lock":
			locks = locks[:1]
		case "different-project":
			locks[1] = projectLock(testID[identity.Project](t), f.Shared)
		case "wrong-purpose":
			purpose = []byte("project.model.append-v1")
		}
		bad, err := oc.NewDependencies(x.a.state().projectIssuer, binding, locks, purpose)
		if err != nil {
			t.Fatal(err)
		}
		hasCode(t, x.a.ValidateInTx(context.Background(), x.store.tx, request, bad), f.Forbidden)
	}
}
