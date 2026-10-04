package contract

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func testHeader(name event.StableName, version foundation.Version) event.Header {
	return event.Header{EventID: testID[event.EventIdentity](10), EventType: name, SchemaVersion: 1, OccurredAt: testTime(), Scope: event.Scope{Kind: event.ProjectScope, ProjectID: testID[event.Project](1)}, AggregateType: ProjectAggregate, AggregateID: testID[event.Aggregate](1), AggregateVersion: &version}
}

func TestProjectTypedEventsRoundTripAndSafePayloads(t *testing.T) {
	catalog := event.NewCatalog()
	events, err := RegisterProjectEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err = catalog.Seal(); err != nil {
		t.Fatal(err)
	}
	created, err := events.NewCreated(testHeader(CreatedEventName, 1), CreatedPayload{OwnerUserID: testID[identity.User](2), CreationID: testID[Creation](3)})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := events.NewUpdated(testHeader(UpdatedEventName, 2), UpdatedPayload{ChangedFields: []ChangedField{NameChanged, DescriptionChanged}})
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := events.NewLifecycleChanged(testHeader(LifecycleChangedEventName, 3), LifecycleChangedPayload{OperationID: ptr(testID[Operation](5)), From: Active, To: Archiving, Action: Archive})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []event.Event{created, updated, lifecycle} {
		restored, err := events.Restore(value.Header(), value.PayloadBytes())
		if err != nil || restored.Summary().PayloadDigest != value.Summary().PayloadDigest {
			t.Fatalf("restore %v", err)
		}
		raw := string(value.PayloadBytes())
		for _, forbidden := range []string{"Build.API", "keep", `"description":`, `"normalized_name":`, `"path":`} {
			if strings.Contains(raw, forbidden) {
				t.Fatalf("event contains business content: %s", raw)
			}
		}
	}
	if payload, err := events.DecodeCreated(created); err != nil || payload.CreationID != testID[Creation](3) {
		t.Fatal("created decode")
	}
	if _, err := events.DecodeUpdated(created); err == nil {
		t.Fatal("cross-type decode")
	}
	if _, err := events.DecodeLifecycleChanged(lifecycle); err != nil {
		t.Fatal(err)
	}
	payload, err := events.DecodeUpdated(updated)
	if err != nil {
		t.Fatal(err)
	}
	payload.ChangedFields[0] = "mutated"
	again, err := events.DecodeUpdated(updated)
	if err != nil || again.ChangedFields[0] != NameChanged {
		t.Fatal("typed decode aliases payload")
	}
}

func TestProjectEventHeaderAndSchemaBoundary(t *testing.T) {
	catalog := event.NewCatalog()
	events, err := RegisterProjectEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*event.Header){func(h *event.Header) { h.Scope = event.Scope{Kind: event.SystemScope} }, func(h *event.Header) { h.Scope.ProjectID = testID[event.Project](99) }, func(h *event.Header) { h.AggregateType = "account" }, func(h *event.Header) { h.AggregateVersion = nil; h.AggregateSequence = ptr(foundation.Sequence(1)) }, func(h *event.Header) { h.AggregateSequence = ptr(foundation.Sequence(1)) }, func(h *event.Header) { h.SchemaVersion = 2 }, func(h *event.Header) { h.EventType = "project.deleted" }, func(h *event.Header) { h.AggregateVersion = ptr(foundation.Version(2)) }} {
		header := testHeader(CreatedEventName, 1)
		mutate(&header)
		if _, err := events.NewCreated(header, CreatedPayload{testID[identity.User](2), testID[Creation](3)}); err == nil {
			t.Fatal("invalid Project header accepted")
		}
	}
	if _, err := RegisterProjectEvents(catalog); err == nil {
		t.Fatal("duplicate registration")
	}
	if len(catalog.Schemas()) != 3 {
		t.Fatal("partial duplicate registration")
	}
	foreignCatalog := event.NewCatalog()
	foreign, _ := RegisterProjectEvents(foreignCatalog)
	value, err := foreign.NewCreated(testHeader(CreatedEventName, 1), CreatedPayload{testID[identity.User](2), testID[Creation](3)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := events.DecodeCreated(value); err == nil {
		t.Fatal("foreign catalog event accepted")
	}
	empty := event.NewCatalog()
	_ = empty.Seal()
	if _, err := RegisterProjectEvents(empty); err == nil || len(empty.Schemas()) != 0 {
		t.Fatal("sealed registration mutated catalog")
	}
	// Neutral Catalog knows the generic schema, not Project header semantics.
	// The Project adapter must reject even an event restored by that lower layer.
	wrong := testHeader(CreatedEventName, 1)
	wrong.Scope.ProjectID = testID[event.Project](99)
	neutral, err := catalog.Restore(ProjectProducer, wrong, []byte(`{"owner_user_id":"01960000-0000-7000-8000-000000000002","creation_id":"01960000-0000-7000-8000-000000000003"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := events.DecodeCreated(neutral); err == nil {
		t.Fatal("neutral restore bypassed Project scope check")
	}
}

func TestProjectPayloadStrictCodecsAndInvalidVariants(t *testing.T) {
	catalog := event.NewCatalog()
	events, _ := RegisterProjectEvents(catalog)
	for _, raw := range []string{`{}`, `{"changed_fields":[]}`, `{"changed_fields":null}`, `{"changed_fields":["name","name"]}`, `{"changed_fields":["owner_user_id"]}`, `{"Changed_Fields":["name"]}`, `{"changed_fields":["name"],"changed_fields":["description"]}`, `{"changed_fields":["name"],"description":"secret"}`, `{"changed_fields":["name"]} {}`} {
		if _, err := events.Restore(testHeader(UpdatedEventName, 2), []byte(raw)); err == nil {
			t.Fatalf("accepted invalid event payload %s", raw)
		}
	}
	for _, value := range []LifecycleChangedPayload{{From: Archived, To: Active, Action: Restore, OperationID: ptr(testID[Operation](5))}, {From: Active, To: Deleting, Action: Delete}, {From: Archiving, To: Deleting, Action: Delete, OperationID: ptr(testID[Operation](5))}, {From: Deleting, To: Active, Action: Restore}} {
		if _, err := events.NewLifecycleChanged(testHeader(LifecycleChangedEventName, 3), value); err == nil {
			t.Fatal("invalid lifecycle event")
		}
	}
	if _, err := events.Restore(testHeader(LifecycleChangedEventName, 3), []byte(`{"from":"archived","to":"active","action":"restore","operation_id":null}`)); err == nil {
		t.Fatal("null operation passed omitted-only variant")
	}
	for _, raw := range []string{`"soft-deleted"`, `null`, `1`} {
		var state Lifecycle
		if json.Unmarshal([]byte(raw), &state) == nil {
			t.Fatal("invalid enum decoded")
		}
	}
}

func TestProjectEventConcurrentImmutableUse(t *testing.T) {
	catalog := event.NewCatalog()
	events, _ := RegisterProjectEvents(catalog)
	_ = catalog.Seal()
	value, err := events.NewUpdated(testHeader(UpdatedEventName, 2), UpdatedPayload{[]ChangedField{NameChanged}})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 50 {
				restored, err := events.Restore(value.Header(), value.PayloadBytes())
				if err != nil {
					t.Error(err)
					return
				}
				payload, err := events.DecodeUpdated(restored)
				if err != nil {
					t.Error(err)
					return
				}
				payload.ChangedFields[0] = "local"
			}
		})
	}
	wg.Wait()
}
