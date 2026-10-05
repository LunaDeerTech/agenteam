package project

import (
	"context"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	obj "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestProjectStopAuthorityRejectsRebuiltWrongPlans(t *testing.T) {
	a := lifecycleTestAuthority(t)
	actor, project, cause := lifecycleTestActor(t)
	op, _ := foundation.ParseID[obj.ProjectStopOperation](cause.OperationID.String())
	stop, _ := obj.NewProjectStopCause(obj.ProjectStopCauseDetails{ProjectID: project, OperationID: op, Action: obj.ProjectStopArchive, ProjectVersion: cause.ProjectVersion})
	request, _ := obj.NewProjectStopRequest(obj.ProjectStopRequestDetails{Actor: actor, Cause: stop, Component: obj.ObjectStopComponent, Step: obj.RequestProjectStopStep})
	plan, err := a.DiscoverProjectStop(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	locks := plan.Locks()
	locks[0].Mode = foundation.Exclusive
	if plan.Locks()[0].Mode != foundation.Shared {
		t.Fatal("returned locks alias plan")
	}
	mapping, _ := obj.ProjectStopBinding(request)
	for _, badLocks := range [][]foundation.LockRequest{nil, locks, append(plan.Locks(), projectLock(testID[identity.Project](t), foundation.Shared))} {
		bad, err := obj.NewAccessDependencies(mapping, badLocks)
		if err != nil && badLocks != nil {
			t.Fatal(err)
		}
		grant, err := a.ValidateProjectStopInTx(context.Background(), foundation.NewTx(), request, bad)
		hasCode(t, err, foundation.Forbidden)
		if grant.Validate() == nil {
			t.Fatal("grant on rejection")
		}
	}
	d := request.Details()
	d.Component = obj.ArtifactStopComponent
	changed, _ := obj.NewProjectStopRequest(d)
	_, err = a.ValidateProjectStopInTx(context.Background(), foundation.NewTx(), changed, plan)
	hasCode(t, err, foundation.Forbidden)
	d = request.Details()
	d.Step = obj.InspectProjectStopStep
	changed, _ = obj.NewProjectStopRequest(d)
	_, err = a.ValidateProjectStopInTx(context.Background(), foundation.NewTx(), changed, plan)
	hasCode(t, err, foundation.Forbidden)
}
