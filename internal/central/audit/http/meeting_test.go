package audithttp

import (
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestMeetingAuditProjectProjectionPreservesOriginalService(t *testing.T) {
	p, _ := f.NewID[i.Project]()
	scope, _ := i.InProject(p)
	id, _ := f.NewID[struct{}]()
	user, _ := f.NewID[i.User]()
	auditID, _ := f.NewID[ac.Record]()
	at, _ := f.NewInstant(time.Now().UTC())
	metadata, err := ac.MeetingDeletionMetadata(ac.MeetingDeleteCompleted, ac.MeetingDeletionMetadataFields{MeetingID: id.String(), DeletionID: id.String(), InitiatorID: user.String(), Version: 2, DeletedRows: 3})
	if err != nil {
		t.Fatal(err)
	}
	resource, _ := ac.NewResource(ac.MeetingResource, id.String())
	r := ac.SafeRecord{AuditID: auditID, CreatedAt: at, Scope: scope, Actor: ac.ActorSummary{Kind: i.Service, Service: i.MeetingRuntime, CauseRef: id.String(), ProjectID: p.String()}, Action: ac.MeetingDeleteCompleted, Outcome: ac.Success, Resource: resource, Metadata: metadata, Associations: ac.Associations{OperationID: id.String()}, Summary: "Audit event"}
	if _, err := projectAuditProjection(p, r); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*ac.SafeRecord){func(v *ac.SafeRecord) { v.Actor.Service = i.ProjectLifecycle }, func(v *ac.SafeRecord) { v.Actor.CauseRef = user.String() }, func(v *ac.SafeRecord) { v.Actor = ac.ActorSummary{Kind: i.Human, ID: user.String()} }, func(v *ac.SafeRecord) { v.Associations.OperationID = user.String() }} {
		bad := r
		change(&bad)
		if _, err := projectAuditProjection(p, bad); err == nil {
			t.Fatal("forged clipped audit accepted")
		}
	}
	if systemResource(ac.MeetingResource) {
		t.Fatal("Project deletion leaked into system audit")
	}
}
