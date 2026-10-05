package object

import (
	"context"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestProjectStopUnboundAndExactActor(t *testing.T) {
	project, _ := foundation.NewID[identity.Project]()
	operation, _ := foundation.NewID[oc.ProjectStopOperation]()
	cause, _ := oc.NewProjectStopCause(oc.ProjectStopCauseDetails{ProjectID: project, OperationID: operation, Action: oc.ProjectStopDelete, ProjectVersion: 2})
	scope, _ := identity.InProject(project)
	registration, _ := identity.RegisterService(identity.ProjectLifecycle)
	actor, _ := registration.Actor(operation.String(), scope)
	var service *Service
	report, err := service.RequestProjectStop(context.Background(), actor, cause)
	if !hasCode(err, foundation.DependencyUnbound) || !report.Matches(oc.ObjectStopComponent, cause) || report.Details().State != oc.ProjectStopPending {
		t.Fatal("unbound stop must preserve the exact pending cause")
	}
	other, _ := foundation.NewID[oc.ProjectStopOperation]()
	foreign, _ := registration.Actor(other.String(), scope)
	if _, err = service.InspectProjectStop(context.Background(), foreign, cause); !hasCode(err, foundation.InvalidArgument) {
		t.Fatal("actor/cause mismatch accepted")
	}
}

func TestProjectStopArchiveKinds(t *testing.T) {
	for _, kind := range []string{"preparation", "transfer_put", "verification", "cleanup", "reader", "source", "download", "transfer_get"} {
		want := kind == "preparation" || kind == "transfer_put" || kind == "verification" || kind == "cleanup"
		if stopWorkRelevant(kind, oc.ProjectStopArchive) != want || !stopWorkRelevant(kind, oc.ProjectStopDelete) {
			t.Fatalf("incorrect stop scope: %s", kind)
		}
	}
}
