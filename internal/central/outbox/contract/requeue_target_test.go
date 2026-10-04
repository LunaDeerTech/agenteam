package contract

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func requeueRequest(t *testing.T) ProjectRequestDetails {
	t.Helper()
	project := fresh[identity.Project](t)
	eventProject, _ := foundation.ParseID[event.Project](project.String())
	actor, _ := identity.NewHuman(fresh[identity.User](t), fresh[identity.Session](t))
	return ProjectRequestDetails{Kind: RequeueProject, ProjectID: project, Actor: actor, Stage: CurrentAccess, Requeue: RequeueTarget{EventID: fresh[event.EventIdentity](t), DeliveryID: fresh[Delivery](t), HandlerID: "private-handler", Scope: event.Scope{Kind: event.ProjectScope, ProjectID: eventProject}, Effect: DomainIngress}}
}
func TestRequeueTargetNoClaimAndCompleteBinding(t *testing.T) {
	d := requeueRequest(t)
	r, err := NewProjectRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	sum, err := RequeueBinding(r)
	if err != nil {
		t.Fatal(err)
	}
	issuer := NewPlanIssuer()
	plan, err := NewDependencies(issuer, sum, nil, []byte("private-data"))
	if err != nil {
		t.Fatal(err)
	}
	for i, change := range []func(*ProjectRequestDetails){
		func(d *ProjectRequestDetails) { d.Requeue.EventID = fresh[event.EventIdentity](t) },
		func(d *ProjectRequestDetails) { d.Requeue.DeliveryID = fresh[Delivery](t) },
		func(d *ProjectRequestDetails) { d.Requeue.HandlerID = "another" },
		func(d *ProjectRequestDetails) { d.Requeue.Effect = CanonicalConverge },
		func(d *ProjectRequestDetails) { d.Stage = NewFact },
		func(d *ProjectRequestDetails) {
			d.Actor, _ = identity.NewHuman(fresh[identity.User](t), fresh[identity.Session](t))
		},
		func(d *ProjectRequestDetails) {
			d.ProjectID = fresh[identity.Project](t)
			d.Requeue.Scope.ProjectID, _ = foundation.ParseID[event.Project](d.ProjectID.String())
		},
	} {
		next := d
		change(&next)
		request, err := NewProjectRequest(next)
		if err != nil {
			t.Fatal(err)
		}
		binding, err := RequeueBinding(request)
		if err != nil || plan.Matches(issuer, binding) {
			t.Fatalf("replacement %d reused plan", i)
		}
	}
	// Session is checked by the authority on every call, not a stable command or
	// target identity. Neither a known hash nor a target is an access grant.
	user, _ := foundation.ParseID[identity.User](d.Actor.Details().UserID)
	again := d
	again.Actor, _ = identity.NewHuman(user, fresh[identity.Session](t))
	changed, _ := NewProjectRequest(again)
	binding, _ := RequeueBinding(changed)
	if binding != sum || plan.Matches(NewPlanIssuer(), sum) {
		t.Fatal("stable actor/issuer boundary")
	}
	detached := r.Details()
	detached.Requeue.HandlerID = "changed"
	if r.Details().Requeue.HandlerID != d.Requeue.HandlerID {
		t.Fatal("Details aliased request")
	}
	for _, output := range []string{fmt.Sprintf("%+v", r), fmt.Sprintf("%#v", struct{ private ProjectRequest }{r})} {
		if strings.Contains(output, "private-handler") {
			t.Fatal("request projection leaked target")
		}
	}
	raw, _ := json.Marshal(r)
	var restored ProjectRequest
	if json.Unmarshal(raw, &restored) == nil {
		t.Fatal("request deserialized into authority")
	}
	identityWithoutClaim := DeliveryIdentity{EventID: d.Requeue.EventID, DeliveryID: d.Requeue.DeliveryID, HandlerID: d.Requeue.HandlerID, Scope: d.Requeue.Scope, Effect: d.Requeue.Effect}
	if identityWithoutClaim.Valid() {
		t.Fatal("callback accepted absent claim")
	}
	if _, err = identityWithoutClaim.CauseRef(); err == nil {
		t.Fatal("callback cause without actual attempt")
	}
}
func TestRequeueTargetVariantIsolation(t *testing.T) {
	d := requeueRequest(t)
	for _, change := range []func(*ProjectRequestDetails){
		func(d *ProjectRequestDetails) { d.Requeue = RequeueTarget{} },
		func(d *ProjectRequestDetails) { d.Requeue.EventID = event.EventID{} },
		func(d *ProjectRequestDetails) { d.Requeue.DeliveryID = DeliveryID{} },
		func(d *ProjectRequestDetails) { d.Requeue.Scope = event.Scope{Kind: event.SystemScope} },
		func(d *ProjectRequestDetails) { d.Requeue.Scope.ProjectID = fresh[event.Project](t) },
		func(d *ProjectRequestDetails) { d.Requeue.Effect = "other" },
		func(d *ProjectRequestDetails) { d.Requeue.HandlerID = "not stable" },
		func(d *ProjectRequestDetails) { d.Stage = "" },
		func(d *ProjectRequestDetails) { d.LifecycleStep = LifecycleStop },
		func(d *ProjectRequestDetails) { d.Delivery = DeliveryIdentity{EventID: d.Requeue.EventID} },
		func(d *ProjectRequestDetails) { d.Kind = InspectProject; d.Stage = "" },
	} {
		bad := d
		change(&bad)
		if _, err := NewProjectRequest(bad); err == nil {
			t.Fatal("invalid variant accepted")
		}
	}
	lifecycle := lifecycleRequest(t)
	lifecycle.Requeue = d.Requeue
	if _, err := NewProjectRequest(lifecycle); err == nil {
		t.Fatal("lifecycle smuggled requeue target")
	}
	r, _ := NewProjectRequest(lifecycleRequest(t))
	if _, err := RequeueBinding(r); err == nil {
		t.Fatal("wrong variant canonicalized")
	}
}
