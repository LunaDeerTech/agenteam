package project

import (
	"context"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"testing"
	"time"
)

func meetingProjectRequest(t *testing.T, x *auditGateFixture, agent bool) oc.ProjectRequest {
	t.Helper()
	at, _ := f.NewInstant(time.Now())
	p, _ := f.ParseID[event.Project](x.project.String())
	version := f.Version(2)
	name := mc.ActivatedName
	actor := x.actor
	if agent {
		version = 1
		name = mc.ProposedName
		actor, _ = i.NewAgentRun(x.project, testID[i.Agent](t), testID[i.Execution](t))
	}
	r, err := oc.NewProjectRequest(oc.ProjectRequestDetails{Kind: oc.AppendProject, ProjectID: x.project, Actor: actor, Stage: oc.CurrentAccess, Event: event.Summary{Producer: mc.MeetingProducer, Header: event.Header{EventID: testID[event.EventIdentity](t), EventType: name, SchemaVersion: 1, OccurredAt: at, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: p}, AggregateType: mc.MeetingAggregate, AggregateID: testID[event.Aggregate](t), AggregateVersion: &version}, PayloadDigest: digest([]byte("original private Meeting plan"))}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestMeetingProjectEventGatePreservesOwnerAndAgentBoundaries(t *testing.T) {
	for _, agent := range []bool{false, true} {
		x := newAuditGateFixture(t, nil)
		request := meetingProjectRequest(t, x, agent)
		deps, err := x.a.Discover(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		if err = x.a.ValidateInTx(context.Background(), x.store.tx, request, deps); err != nil {
			t.Fatal(err)
		}
		x.lifecycle = pc.Deleting
		details := request.Details()
		details.Stage = oc.NewFact
		next, err := oc.NewProjectRequest(details)
		if err != nil {
			t.Fatal(err)
		}
		hasCode(t, x.a.ValidateInTx(context.Background(), x.store.tx, next, deps), f.ProjectNotActive)
		x.lifecycle = pc.Active
		details = request.Details()
		details.Event.Header.EventType = "meeting.rejected"
		foreign, err := oc.NewProjectRequest(details)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = x.a.Discover(context.Background(), foreign); err == nil {
			t.Fatal("unknown lifecycle admitted")
		}
		details = request.Details()
		if agent {
			details.Actor = x.actor
		} else {
			details.Actor, _ = i.NewAgentRun(x.project, testID[i.Agent](t), testID[i.Execution](t))
		}
		foreign, err = oc.NewProjectRequest(details)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = x.a.Discover(context.Background(), foreign); err == nil {
			t.Fatal("wrong lifecycle actor admitted")
		}
	}
}

func meetingDeliveryRequest(t *testing.T, x *auditGateFixture) oc.ProjectRequest {
	t.Helper()
	d := meetingProjectRequest(t, x, true).Details()
	d.Kind = oc.DeliverProject
	d.Stage = ""
	d.Delivery = oc.DeliveryIdentity{EventID: d.Event.Header.EventID, DeliveryID: testID[oc.Delivery](t), AttemptID: testID[oc.Attempt](t), Fence: 1, HandlerID: "humaninbox.meeting_proposal", Scope: d.Event.Header.Scope, Effect: oc.CanonicalConverge}
	return remakeMeetingDelivery(t, d)
}
func remakeMeetingDelivery(t *testing.T, d oc.ProjectRequestDetails) oc.ProjectRequest {
	t.Helper()
	cause, err := d.Delivery.CauseRef()
	if err != nil {
		t.Fatal(err)
	}
	role, _ := i.RegisterService(i.OutboxDelivery)
	scope, _ := i.InProject(d.ProjectID)
	d.Actor, err = role.Actor(cause, scope)
	if err != nil {
		t.Fatal(err)
	}
	r, err := oc.NewProjectRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestMeetingProjectDeliveryGateRejectsBareAndForeignDelivery(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	r := meetingDeliveryRequest(t, x)
	binding, locks, err := meetingDeliveryBinding(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = x.a.Discover(context.Background(), r); err == nil {
		t.Fatal("bare exact-looking delivery granted discovery")
	}
	deps, err := oc.NewDependencies(x.a.state().projectIssuer, binding, locks, []byte(meetingDeliveryPurpose))
	if err != nil {
		t.Fatal(err)
	}
	if err = x.a.ValidateInTx(context.Background(), x.store.tx, r, deps); err == nil {
		t.Fatal("bare delivery and dependency IDs granted same-Tx authority")
	}
	for _, kind := range []string{"producer", "handler", "effect", "schema", "aggregate", "event"} {
		d := r.Details()
		switch kind {
		case "producer":
			d.Event.Producer = "other"
		case "handler":
			d.Delivery.HandlerID = "another.handler"
		case "effect":
			d.Delivery.Effect = oc.DomainIngress
		case "schema":
			d.Event.Header.SchemaVersion = 2
		case "aggregate":
			d.Event.Header.AggregateType = "other"
		case "event":
			d.Event.Header.EventType = "meeting.rejected"
		}
		if _, _, err = meetingDeliveryBinding(remakeMeetingDelivery(t, d)); err == nil {
			t.Fatalf("foreign %s admitted", kind)
		}
	}
	for _, kind := range []string{"attempt", "fence", "payload"} {
		d := r.Details()
		switch kind {
		case "attempt":
			d.Delivery.AttemptID = testID[oc.Attempt](t)
		case "fence":
			d.Delivery.Fence++
		case "payload":
			d.Event.PayloadDigest = digest([]byte("changed"))
		}
		changed, _, err := meetingDeliveryBinding(remakeMeetingDelivery(t, d))
		if err != nil || changed == binding {
			t.Fatalf("original %s was not bound", kind)
		}
	}
}
func TestMeetingProjectDeliveryCanonicalConvergenceLifecycle(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	locks := []f.LockRequest{projectLock(x.project, f.Shared)}
	for _, state := range []pc.Lifecycle{pc.Active, pc.Archiving, pc.Archived} {
		x.lifecycle = state
		if err := x.a.validateMeetingDeliveryProjectInTx(context.Background(), x.store.tx, x.project, locks); err != nil {
			t.Fatal(state, err)
		}
	}
	x.lifecycle = pc.Deleting
	hasCode(t, x.a.validateMeetingDeliveryProjectInTx(context.Background(), x.store.tx, x.project, locks), f.ProjectNotActive)
	x.lifecycle = pc.Active
	x.initialized = false
	hasCode(t, x.a.validateMeetingDeliveryProjectInTx(context.Background(), x.store.tx, x.project, locks), f.ProjectNotActive)
}

func TestMeetingDecisionProjectEventActorAndAggregate(t *testing.T) {
	for _, name := range []event.StableName{mc.DecisionRequestedName, mc.DecisionAnsweredName, mc.DecisionSkippedName, mc.DecisionCancelledName} {
		x := newAuditGateFixture(t, nil)
		d := meetingProjectRequest(t, x, name == mc.DecisionRequestedName || name == mc.DecisionCancelledName).Details()
		d.Event.Header.AggregateType = mc.DecisionAggregate
		d.Event.Header.EventType = name
		r, err := oc.NewProjectRequest(d)
		if err != nil {
			t.Fatal(err)
		}
		deps, err := x.a.Discover(context.Background(), r)
		if err != nil {
			t.Fatal(name, err)
		}
		if err = x.a.ValidateInTx(context.Background(), x.store.tx, r, deps); err != nil {
			t.Fatal(name, err)
		}
		if name == mc.DecisionCancelledName {
			for _, state := range []pc.Lifecycle{pc.Archiving, pc.Archived, pc.Deleting} {
				x.lifecycle = state
				if err = x.a.ValidateInTx(context.Background(), x.store.tx, r, deps); err != nil {
					t.Fatal("original cancellation convergence", state, err)
				}
			}
		}
		d.Event.Header.AggregateType = mc.MeetingAggregate
		wrong, err := oc.NewProjectRequest(d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = x.a.Discover(context.Background(), wrong); err == nil {
			t.Fatal("Decision paired with Meeting aggregate")
		}
		d = r.Details()
		if name == mc.DecisionRequestedName {
			d.Actor = x.actor
		} else if name != mc.DecisionCancelledName {
			d.Actor, _ = i.NewAgentRun(x.project, testID[i.Agent](t), testID[i.Execution](t))
		} else {
			continue
		}
		wrong, err = oc.NewProjectRequest(d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = x.a.Discover(context.Background(), wrong); err == nil {
			t.Fatal("wrong Decision transition actor")
		}
	}
}

func TestMeetingDecisionProjectDeliveryKeepsExactHandlerAndPrivateWitness(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	for _, name := range []event.StableName{mc.DecisionRequestedName, mc.DecisionAnsweredName, mc.DecisionSkippedName, mc.DecisionCancelledName} {
		d := meetingDeliveryRequest(t, x).Details()
		d.Event.Header.AggregateType = mc.DecisionAggregate
		d.Event.Header.EventType = name
		d.Delivery.HandlerID = "humaninbox.meeting_decision"
		r := remakeMeetingDelivery(t, d)
		if _, _, err := meetingDeliveryBinding(r); err != nil {
			t.Fatal(name, err)
		}
		if _, err := x.a.Discover(context.Background(), r); err == nil {
			t.Fatal("bare Decision delivery bypassed private live witness")
		}
		d.Delivery.HandlerID = "humaninbox.meeting_proposal"
		if _, _, err := meetingDeliveryBinding(remakeMeetingDelivery(t, d)); err == nil {
			t.Fatal("Decision entered Proposal handler")
		}
		d.Delivery.HandlerID = "humaninbox.meeting_decision"
		d.Event.Header.EventType = mc.ProposedName
		d.Event.Header.AggregateType = mc.MeetingAggregate
		if _, _, err := meetingDeliveryBinding(remakeMeetingDelivery(t, d)); err == nil {
			t.Fatal("Proposal entered Decision handler")
		}
	}
}
