package contract_test

import (
	"encoding/json"
	"strings"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	k "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func eventHeader(d k.DocumentRef, name event.StableName) event.Header {
	return event.Header{EventID: keyID[event.EventIdentity](70), EventType: name, SchemaVersion: 1, OccurredAt: d.UpdatedAt, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: must(f.ParseID[event.Project](d.ProjectID.String()))}, AggregateType: k.KnowledgeAggregate, AggregateID: must(f.ParseID[event.Aggregate](d.ID.String())), AggregateVersion: ptr(d.ContentVersion)}
}
func TestTypedEventsAreCanonicalFactsWithoutBodyOrMove(t *testing.T) {
	d := doc(10)
	catalog := event.NewCatalog()
	events := must(k.RegisterKnowledgeEvents(catalog))
	if len(catalog.Schemas()) != 2 {
		t.Fatal("unexpected event family")
	}
	payload := k.ContentChangedPayload{DocumentID: d.ID, ContentVersion: d.ContentVersion, Changes: []k.ContentChange{k.ContentCreated}, ObjectID: d.ObjectID}
	evt := must(events.ContentChanged(eventHeader(d, k.ContentChangedEvent), payload))
	if evt.Summary().Producer != k.KnowledgeProducer || !catalog.Owns(evt) {
		t.Fatal("wrong producer/catalog")
	}
	var restored k.ContentChangedPayload
	requireOK(t, json.Unmarshal(evt.PayloadBytes(), &restored))
	if restored.DocumentID != d.ID || restored.ObjectID != d.ObjectID {
		t.Fatal("lost canonical fact")
	}
	payload.Changes[0] = k.SourceChanged
	if strings.Contains(string(evt.PayloadBytes()), `"source"`) {
		t.Fatal("event aliases caller payload")
	}
	deleted := must(events.Deleted(eventHeader(d, k.DeletedEvent), k.DeletedPayload{DocumentID: d.ID, ContentVersion: d.ContentVersion}))
	for _, forbidden := range []string{"title", "text", "body", "object_id", "parent"} {
		if strings.Contains(string(deleted.PayloadBytes()), forbidden) {
			t.Fatal("deleted payload retained", forbidden)
		}
	}
	requireOK(t, catalog.Seal())
	_, e := k.RegisterKnowledgeEvents(catalog)
	requireError(t, e)
	emptySealed := event.NewCatalog()
	requireOK(t, emptySealed.Seal())
	_, e = k.RegisterKnowledgeEvents(emptySealed)
	requireError(t, e)
}
func TestEventHeaderPayloadAgreementAndClosedChanges(t *testing.T) {
	d := doc(10)
	catalog := event.NewCatalog()
	events := must(k.RegisterKnowledgeEvents(catalog))
	payload := k.ContentChangedPayload{DocumentID: d.ID, ContentVersion: d.ContentVersion, Changes: []k.ContentChange{k.TitleChanged, k.SourceChanged}, ObjectID: d.ObjectID}
	h := eventHeader(d, k.ContentChangedEvent)
	_, e := events.ContentChanged(h, payload)
	requireOK(t, e)
	for _, change := range []func(*event.Header){
		func(x *event.Header) { x.Scope = event.Scope{Kind: event.SystemScope} },
		func(x *event.Header) { x.AggregateID = keyID[event.Aggregate](99) },
		func(x *event.Header) { x.AggregateVersion = ptr(f.Version(2)) },
		func(x *event.Header) { x.AggregateSequence = ptr(f.Sequence(1)) },
		func(x *event.Header) { x.EventType = k.DeletedEvent },
		func(x *event.Header) { x.AggregateType = "other" },
		func(x *event.Header) { x.SchemaVersion = 2 },
	} {
		bad := h
		change(&bad)
		_, e := events.ContentChanged(bad, payload)
		requireError(t, e)
	}
	for _, changes := range [][]k.ContentChange{nil, {}, {k.ContentCreated, k.TitleChanged}, {k.SourceChanged, k.TitleChanged}, {k.TitleChanged, k.TitleChanged}, {"move"}} {
		bad := payload
		bad.Changes = changes
		requireError(t, bad.Validate())
	}
	duplicate := event.NewCatalog()
	_, e = k.RegisterKnowledgeEvents(duplicate)
	requireOK(t, e)
	_, e = k.RegisterKnowledgeEvents(duplicate)
	requireError(t, e)
	var zero k.KnowledgeEvents
	_, e = zero.ContentChanged(h, payload)
	requireError(t, e)
}
func TestTypedEventDecoderRejectsExtraAndImpreciseFields(t *testing.T) {
	payload := k.ContentChangedPayload{DocumentID: keyID[k.Document](10), ContentVersion: f.Version(9007199254740993), Changes: []k.ContentChange{k.SourceChanged}, ObjectID: keyID[oc.StoredObject](20)}
	var decoded k.ContentChangedPayload
	roundTrip(t, payload, &decoded)
	original := string(must(json.Marshal(decoded)))
	for _, raw := range []string{
		strings.TrimSuffix(original, "}") + `,"body":"FORBIDDEN"}`,
		strings.Replace(original, `"content_version":"9007199254740993"`, `"content_version":9007199254740993`, 1),
		strings.Replace(original, `"changes":["source"]`, `"changes":["source","source"]`, 1),
		strings.TrimSuffix(original, "}") + `,"changes":["source"]}`,
		strings.Replace(original, `"changes":["source"]`, `"changes":null`, 1),
	} {
		requireError(t, json.Unmarshal([]byte(raw), &decoded))
		if string(must(json.Marshal(decoded))) != original {
			t.Fatal("event decode partially mutated")
		}
	}
	deleted := k.DeletedPayload{DocumentID: payload.DocumentID, ContentVersion: payload.ContentVersion}
	var out k.DeletedPayload
	roundTrip(t, deleted, &out)
	requireError(t, json.Unmarshal(must(json.Marshal(payload)), &out))
}
