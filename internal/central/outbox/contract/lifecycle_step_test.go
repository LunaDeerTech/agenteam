package contract

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func lifecycleRequest(t *testing.T) ProjectRequestDetails {
	t.Helper()
	project := fresh[identity.Project](t)
	scope, _ := identity.InProject(project)
	role, _ := identity.RegisterService(identity.ProjectLifecycle)
	actor, err := role.Actor(DigestBytes([]byte("private-lifecycle-cause")).String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	cause, err := NewLifecycleCause(LifecycleDetails{ProjectID: project, OperationID: fresh[LifecycleOperation](t), Action: DeleteProject, ProjectVersion: 3})
	if err != nil {
		t.Fatal(err)
	}
	return ProjectRequestDetails{Kind: LifecycleProject, ProjectID: project, Actor: actor, Lifecycle: cause, LifecycleStep: LifecycleStop}
}
func TestLifecycleStepAndFullBinding(t *testing.T) {
	d := lifecycleRequest(t)
	request, err := NewProjectRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := LifecycleBinding(request)
	if err != nil {
		t.Fatal(err)
	}
	issuer := NewPlanIssuer()
	deps, err := NewDependencies(issuer, binding, nil, []byte("private-dependency"))
	if err != nil {
		t.Fatal(err)
	}
	changes := []func(*ProjectRequestDetails){
		func(d *ProjectRequestDetails) { d.LifecycleStep = LifecycleInspect },
		func(d *ProjectRequestDetails) { d.LifecycleStep = LifecycleCleanup },
		func(d *ProjectRequestDetails) {
			v := d.Lifecycle.Details()
			v.OperationID = fresh[LifecycleOperation](t)
			d.Lifecycle, _ = NewLifecycleCause(v)
		},
		func(d *ProjectRequestDetails) {
			v := d.Lifecycle.Details()
			v.ProjectVersion++
			d.Lifecycle, _ = NewLifecycleCause(v)
		},
		func(d *ProjectRequestDetails) {
			v := d.Lifecycle.Details()
			v.Action = ArchiveProject
			d.Lifecycle, _ = NewLifecycleCause(v)
		},
		func(d *ProjectRequestDetails) {
			scope, _ := identity.InProject(d.ProjectID)
			role, _ := identity.RegisterService(identity.ProjectLifecycle)
			d.Actor, _ = role.Actor(DigestBytes([]byte("different-current-operation")).String(), scope)
		},
	}
	for _, change := range changes {
		next := d
		change(&next)
		r, e := NewProjectRequest(next)
		if e != nil {
			t.Fatal(e)
		}
		sum, e := LifecycleBinding(r)
		if e != nil || sum == binding || deps.Matches(issuer, sum) {
			t.Fatal("lifecycle authorization plan reused across request facts")
		}
	}
	for _, step := range []LifecycleStep{"", "STOP", "archive", LifecycleStep(" stop")} {
		next := d
		next.LifecycleStep = step
		if _, err = NewProjectRequest(next); err == nil {
			t.Fatal("invalid lifecycle step accepted")
		}
	}
	for _, stage := range []Stage{CurrentAccess, NewFact} {
		next := d
		next.Stage = stage
		if _, err = NewProjectRequest(next); err == nil {
			t.Fatal("generic Stage used as lifecycle step")
		}
	}
	for _, kind := range []ProjectAction{AppendProject, DeliverProject, InspectProject, RequeueProject} {
		next := d
		next.Kind = kind
		if _, err = NewProjectRequest(next); err == nil {
			t.Fatal("lifecycle step accepted on another variant")
		}
	}
	role, _ := identity.RegisterService(identity.OutboxDelivery)
	scope, _ := identity.InProject(d.ProjectID)
	next := d
	next.Actor, _ = role.Actor(DigestBytes([]byte("private-lifecycle-cause")).String(), scope)
	if _, err = NewProjectRequest(next); err == nil {
		t.Fatal("different service became lifecycle authority")
	}
	other := fresh[identity.Project](t)
	otherScope, _ := identity.InProject(other)
	role, _ = identity.RegisterService(identity.ProjectLifecycle)
	next = d
	next.Actor, _ = role.Actor(DigestBytes([]byte("private-lifecycle-cause")).String(), otherScope)
	if _, err = NewProjectRequest(next); err == nil {
		t.Fatal("cross Project lifecycle actor accepted")
	}
	// Safe projections intentionally collapse to a fixed label. The binding
	// above must still distinguish every explicit semantic fact.
	raw, _ := json.Marshal(request)
	if string(raw) != `"outbox_project_request"` {
		t.Fatal("unsafe request JSON")
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		v := struct {
			request      ProjectRequest
			dependencies Dependencies
		}{request, deps}
		if strings.Contains(fmt.Sprintf(verb, v), "private-") || strings.Contains(fmt.Sprintf(verb, v), d.Actor.Details().CauseRef) {
			t.Fatal("recursive projection leaked lifecycle/private plan")
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Go(func() {
			for j := 0; j < 20; j++ {
				copy := request.Details()
				copy.LifecycleStep = LifecycleCleanup
				b, e := LifecycleBinding(request)
				if e != nil || b != binding {
					t.Error("request mutated through Details")
				}
			}
		})
	}
	wg.Wait()
	if json.Unmarshal([]byte(`{}`), &request) == nil {
		t.Fatal("request forged through JSON")
	}
	if _, err = LifecycleBinding(ProjectRequest{}); err == nil {
		t.Fatal("zero request accepted")
	}
}
