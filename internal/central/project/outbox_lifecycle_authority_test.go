package project

import (
	"context"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

func TestOutboxLifecyclePlansRequireIssuerAndExactDependencies(t *testing.T) {
	a := lifecycleTestAuthority(t)
	actor, project, cause := lifecycleTestActor(t)
	op, _ := foundation.ParseID[oc.LifecycleOperation](cause.OperationID.String())
	lifecycle, _ := oc.NewLifecycleCause(oc.LifecycleDetails{ProjectID: project, OperationID: op, Action: oc.ArchiveProject, ProjectVersion: cause.ProjectVersion})
	request, _ := oc.NewProjectRequest(oc.ProjectRequestDetails{Kind: oc.LifecycleProject, ProjectID: project, Actor: actor, Lifecycle: lifecycle, LifecycleStep: oc.LifecycleInspect})
	plan, err := a.Discover(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	lock := plan.Locks()[0]
	lock.Mode = foundation.Exclusive
	for _, tc := range []struct {
		issuer oc.PlanIssuer
		locks  []foundation.LockRequest
		opaque []byte
	}{
		{oc.NewPlanIssuer(), plan.Locks(), nil}, {a.issuer, nil, nil}, {a.issuer, []foundation.LockRequest{lock}, nil}, {a.issuer, plan.Locks(), []byte("extra")},
	} {
		bad, err := oc.NewDependencies(tc.issuer, plan.Binding(), tc.locks, tc.opaque)
		if err != nil {
			t.Fatal(err)
		}
		hasCode(t, a.ValidateInTx(context.Background(), foundation.NewTx(), request, bad), foundation.Forbidden)
	}
	d := request.Details()
	d.LifecycleStep = oc.LifecycleStop
	changed, _ := oc.NewProjectRequest(d)
	hasCode(t, a.ValidateInTx(context.Background(), foundation.NewTx(), changed, plan), foundation.Forbidden)
	var absent Authority
	_, err = absent.ResolveLifecycleActor(context.Background(), lifecycle)
	hasCode(t, err, foundation.DependencyUnbound)
}
