package project

import (
	"strings"
	"testing"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestLifecycleAcceptedAuditUsesOriginalOperationAndTransition(t *testing.T) {
	for _, action := range []c.LifecycleAction{c.Archive, c.Delete} {
		command := lifecycleTestCommand(t, action)
		kind, metadata, err := lifecycleAcceptedMetadata(command)
		if err != nil {
			t.Fatal(err)
		}
		fields, err := metadata.ProjectFields()
		if err != nil || fields.OperationID != command.plan.OperationID.String() || fields.ProjectID != command.project.String() || fields.ProjectVersion != 2 || fields.OperationVersion == nil || *fields.OperationVersion != 1 || fields.InitiatorID != command.user || fields.From != "active" || fields.To != string(command.plan.To) || fields.Action != string(action) {
			t.Fatal("accepted Audit metadata diverged", err)
		}
		if action == c.Archive && kind != audit.ProjectArchiveAccepted || action == c.Delete && kind != audit.ProjectDeleteAccepted || !strings.HasSuffix(string(kind), ".accepted") {
			t.Fatal("acceptance emitted another lifecycle action")
		}
	}
}
