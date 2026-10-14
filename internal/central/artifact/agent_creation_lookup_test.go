package artifact

import (
	"context"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestArtifactAgentCreationLookupOriginalCommandAndCurrentGrant(t *testing.T) {
	for _, mode := range []string{"committed", "changed-operation", "current-denied"} {
		t.Run(mode, func(t *testing.T) {
			v := newCreationLookupFixture(t)
			d := v.invocation.Details()
			execution := newTestID[i.Execution](t)
			d.Actor, _ = i.NewAgentRun(d.ProjectID, newTestID[i.Agent](t), execution)
			d.ExecutionID, d.OperationID, d.ToolID, d.ToolCallID = execution.String(), newTestID[struct{}](t).String(), newTestID[struct{}](t).String(), newTestID[struct{}](t).String()
			var err error
			v.invocation, err = ac.NewInvocation(d)
			if err != nil {
				t.Fatal(err)
			}
			rows := v.store.rows
			m := rows.meta.Details()
			m.CreatedByUserID = ""
			m.CreatedByAgentID = d.Actor.Details().AgentID
			m.ExecutionID = d.ExecutionID
			m.OperationID = d.OperationID
			rows.meta, err = ac.NewMetadata(m)
			if err != nil {
				t.Fatal(err)
			}
			rows.storedMeta = rows.meta
			rows.request, err = makeContentRequest(v.invocation, v.meta, v.display, v.media, v.content)
			if err != nil {
				t.Fatal(err)
			}
			rows.command, err = newCommand(rows.request, m.Reference, oc.ResolvedSource{})
			if err != nil {
				t.Fatal(err)
			}
			rows.command.created, rows.command.object, rows.command.state = m.CreatedAt, m.Object.ID, "completed"
			rows.storedCommand, rows.rowCause = rows.command, rows.command.cause
			switch mode {
			case "changed-operation":
				d.OperationID = newTestID[struct{}](t).String()
				v.invocation, err = ac.NewInvocation(d)
				if err != nil {
					t.Fatal(err)
				}
			case "current-denied":
				v.authority.deny = true
			}
			result, err := v.lookup(context.Background())
			switch mode {
			case "committed":
				if err != nil || result.State != ac.CreationCommitted || result.Artifact == nil || result.Artifact.Details().ExecutionID != d.ExecutionID || v.store.ownerReads != 1 {
					t.Fatal("original AgentRun result not observed", err)
				}
			case "changed-operation":
				projectAuditCode(t, err, f.IdempotencyKeyReused)
			case "current-denied":
				projectAuditCode(t, err, f.Forbidden)
			}
			v.assertReadOnly(t)
		})
	}
}
