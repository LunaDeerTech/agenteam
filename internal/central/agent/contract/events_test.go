package contract_test

import (
	"encoding/json"
	"strings"
	"testing"

	a "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	e "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestAgentTypedEventSafeClosedPayload(t *testing.T) {
	catalog := e.NewCatalog()
	events, err := a.RegisterAgentEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.RegisterAgentEvents(catalog); err == nil {
		t.Fatal("duplicate schema")
	}
	if err := catalog.Seal(); err != nil {
		t.Fatal(err)
	}
	core := coreFixture(t)
	version := f.Version(1)
	h := e.Header{EventID: mustID[e.EventIdentity](t, "01900000-0000-7000-8000-000000000021"), EventType: a.AgentConfigChangedEvent, SchemaVersion: 1, OccurredAt: core.CreatedAt, Scope: e.Scope{Kind: e.ProjectScope, ProjectID: mustID[e.Project](t, core.ProjectID.String())}, AggregateType: a.AgentAggregate, AggregateID: mustID[e.Aggregate](t, core.ID.String()), AggregateVersion: &version}
	p := a.ConfigChangedPayload{CommandID: mustID[a.AgentCommand](t, "01900000-0000-7000-8000-000000000021"), ActorUserID: mustID[i.User](t, "01900000-0000-7000-8000-000000000022"), Operation: a.ConfigCreated, ChangedFields: a.EditableFields()}
	value, err := events.NewConfigChanged(h, p)
	if err != nil {
		t.Fatal(err)
	}
	p.ChangedFields[0] = "body-sentinel"
	round, err := events.DecodeConfigChanged(value)
	if err != nil || round.ChangedFields[0] == "body-sentinel" {
		t.Fatal("event payload aliases input")
	}
	raw := string(mustJSON(t, round))
	if strings.Contains(raw, core.Instructions) || strings.Contains(raw, core.Description) {
		t.Fatal("event contains configuration text")
	}
	for _, bad := range []string{strings.Replace(raw, `"operation":"created"`, `"operation":"created","operation":"updated"`, 1), strings.Replace(raw, `"changed_fields":[`, `"body":"sentinel","changed_fields":[`, 1)} {
		var out a.ConfigChangedPayload
		if json.Unmarshal([]byte(bad), &out) == nil {
			t.Fatal("unsafe event shape accepted")
		}
	}
	version = 2
	if _, err := events.NewConfigChanged(h, round); err == nil {
		t.Fatal("created version mismatch")
	}
	round.Operation = a.ConfigUpdated
	round.ChangedFields = []string{"name"}
	if _, err := events.NewConfigChanged(h, round); err != nil {
		t.Fatal(err)
	}
}
