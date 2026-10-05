package project

import (
	"context"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func modelProjectRequest(t *testing.T, f *auditGateFixture) oc.ProjectRequest {
	t.Helper()
	now, _ := foundation.NewInstant(time.Now())
	p, _ := foundation.ParseID[event.Project](f.project.String())
	version := foundation.Version(1)
	r, err := oc.NewProjectRequest(oc.ProjectRequestDetails{Kind: oc.AppendProject, ProjectID: f.project, Actor: f.actor, Stage: oc.CurrentAccess, Event: event.Summary{Producer: "model", Header: event.Header{EventID: testID[event.EventIdentity](t), EventType: "model.configuration_changed", SchemaVersion: 1, OccurredAt: now, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: p}, AggregateType: "model.configuration", AggregateID: testID[event.Aggregate](t), AggregateVersion: &version}, PayloadDigest: digest([]byte("exact prepared payload"))}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestModelProjectEventUsesSameCompletePlanForReadAndMutate(t *testing.T) {
	f := newAuditGateFixture(t, nil)
	request := modelProjectRequest(t, f)
	deps, err := f.a.Discover(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.order) != 0 {
		t.Fatal("discovery authorized or queried")
	}
	for _, phase := range []c.Lifecycle{c.Active, c.Archiving, c.Archived, c.Deleting} {
		f.lifecycle = phase
		for _, stage := range []oc.Stage{oc.CurrentAccess, oc.NewFact} {
			d := request.Details()
			d.Stage = stage
			r, err := oc.NewProjectRequest(d)
			if err != nil {
				t.Fatal(err)
			}
			err = f.a.ValidateInTx(context.Background(), f.store.tx, r, deps)
			if phase == c.Active || (phase == c.Archiving || phase == c.Archived) && stage == oc.CurrentAccess {
				if err != nil {
					t.Fatalf("%s/%s: %v", phase, stage, err)
				}
			} else {
				hasCode(t, err, foundation.ProjectNotActive)
			}
		}
	}
	f.lifecycle = c.Active
	f.sessionErr = fault(foundation.SessionRevoked)
	hasCode(t, f.a.ValidateInTx(context.Background(), f.store.tx, request, deps), foundation.SessionRevoked)
	f.sessionErr = nil
	hasCode(t, f.a.ValidateInTx(context.Background(), foundation.NewTx(), request, deps), foundation.DependencyUnavailable)
	f.store.lockErr = fault(foundation.InvalidState)
	hasCode(t, f.a.ValidateInTx(context.Background(), f.store.tx, request, deps), foundation.DependencyUnavailable)
}
func TestModelProjectEventPlanClosesActorSummaryIssuerAndPurpose(t *testing.T) {
	f := newAuditGateFixture(t, nil)
	request := modelProjectRequest(t, f)
	deps, err := f.a.Discover(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"session", "user", "event", "digest", "time", "aggregate", "version", "schema", "type", "producer", "project", "service", "sequence"} {
		t.Run(change, func(t *testing.T) {
			d := request.Details()
			switch change {
			case "session":
				user, _ := foundation.ParseID[identity.User](f.actor.Details().UserID)
				d.Actor, _ = identity.NewHuman(user, testID[identity.Session](t))
			case "user":
				d.Actor = testActor(t)
			case "event":
				d.Event.Header.EventID = testID[event.EventIdentity](t)
			case "digest":
				d.Event.PayloadDigest = digest([]byte("changed"))
			case "time":
				d.Event.Header.OccurredAt, _ = foundation.NewInstant(time.Now().Add(time.Hour))
			case "aggregate":
				d.Event.Header.AggregateID = testID[event.Aggregate](t)
			case "version":
				v := foundation.Version(2)
				d.Event.Header.AggregateVersion = &v
			case "schema":
				d.Event.Header.SchemaVersion = 2
			case "type":
				d.Event.Header.EventType = "model.embedding_selection_changed"
			case "producer":
				d.Event.Producer = "unknown"
			case "project":
				d.ProjectID = testID[identity.Project](t)
				d.Event.Header.Scope.ProjectID, _ = foundation.ParseID[event.Project](d.ProjectID.String())
			case "service":
				role, _ := identity.RegisterService(identity.SecretService)
				scope, _ := identity.InProject(f.project)
				d.Actor, _ = role.Actor(testID[struct{}](t).String(), scope)
			case "sequence":
				v := foundation.Sequence(1)
				d.Event.Header.AggregateSequence = &v
			}
			r, err := oc.NewProjectRequest(d)
			if err != nil {
				t.Fatal(err)
			}
			if err = f.a.ValidateInTx(context.Background(), f.store.tx, r, deps); err == nil {
				t.Fatal("changed bound field accepted")
			}
		})
	}
	other, err := NewAuthority(f.store, AuthorityDependencies{Sessions: f.a.state().sessions})
	if err != nil {
		t.Fatal(err)
	}
	hasCode(t, other.ValidateInTx(context.Background(), f.store.tx, request, deps), foundation.Forbidden)
	d := request.Details()
	d.Stage = oc.NewFact
	next, _ := oc.NewProjectRequest(d)
	_, err = f.a.Discover(context.Background(), next)
	hasCode(t, err, foundation.Forbidden)
}
