package contract

import (
	"encoding/json"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"testing"
)

func TestVariableTypedProducerExactEnvelope(t *testing.T) {
	cat := event.NewCatalog()
	factory, err := RegisterVariableEvents(cat)
	if err != nil || !factory.Valid() {
		t.Fatal(err)
	}
	if _, err = RegisterVariableEvents(cat); err == nil {
		t.Fatal("duplicate schema")
	}
	v := testVariable(t)
	version := f.Version(1)
	h := event.Header{EventID: testID[event.EventIdentity](10), EventType: VariableChangedName, SchemaVersion: 1, OccurredAt: v.Fields().CreatedAt, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: testID[event.Project](2)}, AggregateType: VariableAggregate, AggregateID: testID[event.Aggregate](1), AggregateVersion: &version}
	payload := VariableChanged{testID[i.ProjectVariable](1), testID[Operation](11), Created, []string{"created"}}
	ev, err := factory.NewVariableChanged(h, payload)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := factory.Decode(ev)
	if err != nil || decoded.VariableID != payload.VariableID {
		t.Fatal("typed decode")
	}
	raw, _ := json.Marshal(payload)
	if _, err = factory.Restore(h, raw); err != nil {
		t.Fatal(err)
	}
	other, _ := RegisterVariableEvents(event.NewCatalog())
	if _, err = other.Decode(ev); err == nil {
		t.Fatal("foreign catalog accepted")
	}
	bad := h
	bad.AggregateID = testID[event.Aggregate](12)
	if _, err = factory.NewVariableChanged(bad, payload); err == nil {
		t.Fatal("cross target")
	}
	bad = h
	bad.Scope = event.Scope{Kind: event.SystemScope}
	if _, err = factory.NewVariableChanged(bad, payload); err == nil {
		t.Fatal("system scope")
	}
	for _, fields := range [][]string{nil, {"value", "name"}, {"name", "name"}, {"secret"}, {"created"}} {
		if ValidateChangedFields(Updated, fields) == nil {
			t.Fatal("unclosed changes")
		}
	}
}
