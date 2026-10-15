package contract

import (
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"testing"
	"time"
)

func TestProposalLifecycleEventsUseCanonicalStateNames(t *testing.T) {
	catalog := event.NewCatalog()
	types, err := RegisterEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	m := fresh[Meeting](t)
	aggregate, _ := f.ParseID[event.Aggregate](m.String())
	at, _ := f.NewInstant(time.Now())
	version := f.Version(1)
	for name, status := range map[event.StableName]string{ProposedName: "proposed", ActivatedName: "active", RestoredName: "active", ArchivedName: "archive", DeletedName: "deleted"} {
		header := event.Header{EventID: fresh[event.EventIdentity](t), EventType: name, SchemaVersion: 1, OccurredAt: at, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: fresh[event.Project](t)}, AggregateType: MeetingAggregate, AggregateID: aggregate, AggregateVersion: &version}
		e, err := types.New(header, LifecyclePayload{MeetingID: m, Status: status})
		if err != nil {
			t.Fatal(name, err)
		}
		decoded, err := types.Decode(e)
		if err != nil || decoded.MeetingID != m || decoded.Status != status {
			t.Fatal("lost canonical payload", err)
		}
		if _, err = types.New(header, LifecyclePayload{MeetingID: m, Status: "rejected"}); err == nil {
			t.Fatal("invented rejected lifecycle")
		}
		if _, err = types.New(header, LifecyclePayload{MeetingID: fresh[Meeting](t), Status: status}); err == nil {
			t.Fatal("foreign aggregate accepted")
		}
	}
}
