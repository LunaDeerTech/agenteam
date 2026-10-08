package contract

import (
	"bytes"
	"encoding/json"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func eventFixture(t *testing.T) (WorkEvents, event.Header, MilestoneChanged) {
	t.Helper()
	catalog := event.NewCatalog()
	work, err := RegisterWorkEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	m := testMilestone(t)
	version := f.Version(1)
	h := event.Header{EventID: testID[event.EventIdentity](t, 10), EventType: MilestoneChangedName, SchemaVersion: 1, OccurredAt: m.UpdatedAt, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: testID[event.Project](t, 5)}, AggregateType: MilestoneAggregate, AggregateID: testID[event.Aggregate](t, 4), AggregateVersion: &version}
	p := MilestoneChanged{CommandID: testID[StructureCommand](t, 12), ActorUserID: testID[i.User](t, 1), Change: CreatedChange, ChangedFields: []ChangedField{DescriptionChanged, RankChanged, TitleChanged}, Position: &MilestonePosition{OrderGeneration: 2}}
	return work, h, p
}
func TestWorkTypedEventsClosedSchemaAndHistory(t *testing.T) {
	w, h, p := eventFixture(t)
	ev, err := w.NewMilestoneChanged(h, p)
	if err != nil {
		t.Fatal(err)
	}
	p.ChangedFields[0] = TitleChanged
	if _, err = w.DecodeMilestoneChanged(ev); err != nil {
		t.Fatal("event borrowed mutable payload", err)
	}
	restored, err := w.Restore(ev.Header(), ev.PayloadBytes())
	if err != nil || restored.Summary().PayloadDigest != ev.Summary().PayloadDigest {
		t.Fatal("typed restore changed payload", err)
	}
	other, _, _ := eventFixture(t)
	if _, err = other.DecodeMilestoneChanged(ev); err == nil {
		t.Fatal("foreign catalog accepted")
	}
	if _, err = w.DecodeSprintChanged(ev); err == nil {
		t.Fatal("wrong typed schema accepted")
	}
	for _, alter := range []func(*event.Header){func(v *event.Header) { v.SchemaVersion = 2 }, func(v *event.Header) { v.EventType = SprintChangedName }, func(v *event.Header) { v.AggregateType = SprintAggregate }, func(v *event.Header) { v.Scope = event.Scope{Kind: event.SystemScope} }, func(v *event.Header) { v.AggregateVersion = nil }, func(v *event.Header) { n := f.Sequence(1); v.AggregateSequence = &n }} {
		header := ev.Header()
		alter(&header)
		if _, err = w.Restore(header, ev.PayloadBytes()); err == nil {
			t.Fatal("replacement header accepted")
		}
	}
	var payload MilestoneChanged
	if err = json.Unmarshal(ev.PayloadBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	self := testID[Milestone](t, 4)
	payload.Position.PreviousID = &self
	if _, err = w.NewMilestoneChanged(h, payload); err == nil {
		t.Fatal("self neighbor accepted")
	}
	catalog := event.NewCatalog()
	if _, err = RegisterWorkEvents(catalog); err != nil {
		t.Fatal(err)
	}
	if _, err = RegisterWorkEvents(catalog); err == nil {
		t.Fatal("duplicate registration accepted")
	}
	if err = catalog.Seal(); err != nil {
		t.Fatal(err)
	}
	if _, err = RegisterWorkEvents(catalog); err == nil {
		t.Fatal("sealed registration accepted")
	}
}
func TestWorkEventPayloadExactChangesAndNoBody(t *testing.T) {
	w, h, p := eventFixture(t)
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, wire := range [][]byte{bytes.Replace(raw, []byte(`"created"`), []byte(`"deleted"`), 1), bytes.Replace(raw, []byte(`"changed_fields":["description","manual_rank","title"]`), []byte(`"changed_fields":["title","manual_rank","description"]`), 1), bytes.Replace(raw, []byte(`"position":{`), []byte(`"POSITION":{`), 1), append(bytes.Clone(raw[:len(raw)-1]), []byte(`,"title":"secret"}`)...), bytes.Replace(raw, []byte(`"order_generation":"2"`), []byte(`"order_generation":"0"`), 1)} {
		if json.Unmarshal(wire, new(MilestoneChanged)) == nil {
			t.Fatal("invalid payload accepted")
		}
	}
	version := f.Version(2)
	h.AggregateVersion = &version
	p.Change = UpdatedChange
	p.ChangedFields = []ChangedField{TitleChanged}
	p.Position = nil
	if _, err = w.NewMilestoneChanged(h, p); err != nil {
		t.Fatal(err)
	}
	p.ChangedFields = []ChangedField{RankChanged}
	if _, err = w.NewMilestoneChanged(h, p); err == nil {
		t.Fatal("rank admitted as content update")
	}
	sprintPayload := SprintChanged{CommandID: p.CommandID, ActorUserID: p.ActorUserID, MilestoneID: testID[Milestone](t, 4), Change: ReorderedChange, ChangedFields: []ChangedField{RankChanged}, Position: &SprintPosition{OrderGeneration: 8}}
	h.EventType = SprintChangedName
	h.AggregateType = SprintAggregate
	h.AggregateID = testID[event.Aggregate](t, 6)
	before := testID[pc.Sprint](t, 7)
	sprintPayload.Position.NextID = &before
	sprintEvent, err := w.NewSprintChanged(h, sprintPayload)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := w.DecodeSprintChanged(sprintEvent)
	if err != nil || decoded.Position.NextID == nil || *decoded.Position.NextID != before {
		t.Fatal("sprint position lost", err)
	}
	for _, secret := range [][]byte{[]byte("description\":"), []byte("session_id"), []byte("manual_rank\":"), []byte("idempotency_key"), []byte("semantic")} {
		if bytes.Contains(sprintEvent.PayloadBytes(), secret) {
			t.Fatal("forbidden body field in event")
		}
	}
}
