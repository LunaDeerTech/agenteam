package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func fresh[K any](t *testing.T) foundation.ID[K] {
	t.Helper()
	v, e := foundation.NewID[K]()
	if e != nil {
		t.Fatal(e)
	}
	return v
}

type payload struct {
	Value string `json:"value"`
}

func sample(t *testing.T) (event.Event, identity.Actor) {
	t.Helper()
	c := event.NewCatalog()
	et, e := event.DefineEvent(c, event.Definition[payload]{Schema: event.Schema{Producer: "fixture", EventType: "fixture.changed", AggregateType: "fixture", Version: 1}, Codec: event.JSONCodec[payload]{}, Validate: func(payload) error { return nil }})
	if e != nil {
		t.Fatal(e)
	}
	at, _ := foundation.NewInstant(time.Now())
	v := foundation.Version(1)
	ev, e := event.NewEvent(et, event.Header{EventID: fresh[event.EventIdentity](t), EventType: "fixture.changed", SchemaVersion: 1, OccurredAt: at, Scope: event.Scope{Kind: event.SystemScope}, AggregateType: "fixture", AggregateID: fresh[event.Aggregate](t), AggregateVersion: &v}, payload{"private-canary"})
	if e != nil {
		t.Fatal(e)
	}
	actor, _ := identity.NewHuman(fresh[identity.User](t), fresh[identity.Session](t))
	return ev, actor
}
func TestPlanBindingCopiesAndSafeProjection(t *testing.T) {
	e, a := sample(t)
	issuer := NewPlanIssuer()
	sum, _ := SemanticDigest(a, e)
	key, _ := foundation.SystemConfigLock("fixture")
	locks := []foundation.LockRequest{{Key: key, Mode: foundation.Shared}, {Key: key, Mode: foundation.Exclusive}}
	raw := []byte("private-canary")
	deps, err := NewDependencies(issuer, sum, locks, raw)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewAppendPlan(issuer, AppendPlanDetails{Event: e, Semantic: sum, Producer: deps, Locks: locks})
	if err != nil {
		t.Fatal(err)
	}
	locks[1].Mode = foundation.Shared
	raw[0] = '!'
	returned := deps.Locks()
	returned[0].Mode = foundation.Shared
	deps.Opaque()[0] = '!'
	plan.Locks()[0].Mode = foundation.Shared
	if len(plan.Locks()) != 1 || plan.Locks()[0].Mode != foundation.Exclusive || string(deps.Opaque()) != "private-canary" || !plan.Matches(issuer, a, e) || plan.Matches(NewPlanIssuer(), a, e) {
		t.Fatal("issuer/mode/copy binding")
	}
	user, _ := foundation.ParseID[identity.User](a.Details().UserID)
	newSession, _ := identity.NewHuman(user, fresh[identity.Session](t))
	if !plan.Matches(issuer, newSession, e) {
		t.Fatal("session changed stable command identity")
	}
	_, other := sample(t)
	if plan.Matches(issuer, other, e) {
		t.Fatal("different actor accepted")
	}
	hp, err := NewHandlerPlan("fixture.handler", e, deps)
	if err != nil || !hp.Matches("fixture.handler", e) || hp.Matches("other.handler", e) {
		t.Fatal("handler plan binding")
	}
	for _, v := range []any{plan, deps, hp, struct {
		plan    AppendPlan
		deps    Dependencies
		handler HandlerPlan
	}{plan, deps, hp}} {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if strings.Contains(fmt.Sprintf(verb, v), "private-canary") {
				t.Fatal("fmt disclosure")
			}
		}
		var b bytes.Buffer
		slog.New(slog.NewTextHandler(&b, nil)).Info("safe", "value", v)
		if strings.Contains(b.String(), "private-canary") {
			t.Fatal("slog disclosure")
		}
		b.Reset()
		slog.New(slog.NewJSONHandler(&b, nil)).Info("safe", "value", v)
		if strings.Contains(b.String(), "private-canary") {
			t.Fatal("JSON log disclosure")
		}
	}
	if json.Unmarshal([]byte(`{}`), &plan) == nil || json.Unmarshal([]byte(`{}`), &deps) == nil || json.Unmarshal([]byte(`{}`), &hp) == nil {
		t.Fatal("JSON forged plan")
	}
}
func TestProjectVariantsRejectPartialOrCrossPurposeIdentity(t *testing.T) {
	e, a := sample(t)
	h := e.Header()
	project := fresh[identity.Project](t)
	ep, _ := foundation.ParseID[event.Project](project.String())
	h.Scope = event.Scope{Kind: event.ProjectScope, ProjectID: ep}
	summary := e.Summary()
	summary.Header = h
	appendRequest := ProjectRequestDetails{Kind: AppendProject, ProjectID: project, Actor: a, Stage: CurrentAccess, Event: summary}
	if _, err := NewProjectRequest(appendRequest); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*ProjectRequestDetails){func(d *ProjectRequestDetails) { d.Delivery.DeliveryID = fresh[Delivery](t) }, func(d *ProjectRequestDetails) { d.Event.Header.Scope = event.Scope{Kind: event.SystemScope} }, func(d *ProjectRequestDetails) { d.Stage = "true" }, func(d *ProjectRequestDetails) { d.Kind = InspectProject }} {
		v := appendRequest
		mutate(&v)
		if _, err := NewProjectRequest(v); err == nil {
			t.Fatal("variant admitted extra/partial fields")
		}
	}
	delivery := DeliveryIdentity{EventID: h.EventID, DeliveryID: fresh[Delivery](t), AttemptID: fresh[Attempt](t), Fence: 1, HandlerID: "fixture.handler", Scope: h.Scope, Effect: CanonicalConverge}
	scope, _ := ScopeIdentity(h.Scope)
	registration, _ := identity.RegisterService(identity.OutboxDelivery)
	ref, _ := delivery.CauseRef()
	actor, _ := registration.Actor(ref, scope)
	d := ProjectRequestDetails{Kind: DeliverProject, ProjectID: project, Actor: actor, Event: summary, Delivery: delivery}
	request, err := NewProjectRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	*summary.Header.AggregateVersion = 4
	if *request.Details().Event.Header.AggregateVersion != 1 {
		t.Fatal("mutable summary reached request")
	}
	for _, mutate := range []func(*ProjectRequestDetails){func(d *ProjectRequestDetails) { d.Delivery.AttemptID = fresh[Attempt](t) }, func(d *ProjectRequestDetails) { d.Delivery.Fence++ }, func(d *ProjectRequestDetails) { d.Delivery.Effect = DomainIngress }, func(d *ProjectRequestDetails) { d.Actor = a }, func(d *ProjectRequestDetails) { d.Event.Header.EventID = fresh[event.EventIdentity](t) }} {
		v := request.Details()
		mutate(&v)
		if _, err := NewProjectRequest(v); err == nil {
			t.Fatal("delivery cause mismatched")
		}
	}
}
