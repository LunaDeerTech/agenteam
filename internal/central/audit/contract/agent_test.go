package contract

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func agentAuditFixture(t *testing.T) (AgentMetadataFields, EntryFields) {
	t.Helper()
	_, base := knowledgeAuditFixture(t)
	value := AgentMetadataFields{AgentID: "01900000-0000-7000-8000-000000000005", Version: 2, CommandID: "01900000-0000-7000-8000-000000000006", ChangedFields: []string{"display_name", "instructions"}}
	metadata, err := AgentMetadata(AgentUpdate, value)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := NewResource(AgentResource, value.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	return value, EntryFields{Scope: base.Scope, Actor: base.Actor, Action: AgentUpdate, Outcome: Success, Resource: resource, Metadata: metadata}
}

func TestAgentAuditMetadataIsClosedAndCopied(t *testing.T) {
	value, fields := agentAuditFixture(t)
	original := fields.Metadata.JSON()
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(original, &wire); err != nil || len(wire) != 4 || string(wire["version"]) != `"2"` {
		t.Fatal("unsafe metadata shape", err)
	}
	for _, name := range []string{"agent_id", "version", "command_id", "changed_fields"} {
		if _, ok := wire[name]; !ok {
			t.Fatal("missing safe field", name)
		}
	}
	value.ChangedFields[0] = "name"
	first, err := fields.Metadata.AgentFields()
	if err != nil || !slices.Equal(first.ChangedFields, []string{"display_name", "instructions"}) {
		t.Fatal("caller slice aliases metadata", err)
	}
	first.ChangedFields[0] = "description"
	if !bytes.Equal(original, fields.Metadata.JSON()) {
		t.Fatal("projection aliases metadata")
	}
	round, err := DecodeMetadata(AgentUpdate, original)
	if err != nil || !bytes.Equal(round.JSON(), original) {
		t.Fatal("strict roundtrip", err)
	}
	if _, err = NewEntry(fields); err != nil {
		t.Fatal(err)
	}
	// This list is the independently specified complete creation projection.
	created := AgentMetadataFields{AgentID: value.AgentID, CommandID: value.CommandID, Version: 1, ChangedFields: []string{"allowed_mount_ids", "allowed_secret_variable_ids", "allowed_tool_ids", "approval_model_ref", "approval_policy", "description", "display_name", "inject_agents_md", "instructions", "model_ref", "name", "reasoning_effort", "tag_color"}}
	if _, err = AgentMetadata(AgentCreate, created); err != nil {
		t.Fatal("complete creation", err)
	}
	created.ChangedFields = created.ChangedFields[:12]
	if _, err = AgentMetadata(AgentCreate, created); err == nil {
		t.Fatal("incomplete creation accepted")
	}
	if !AgentCreate.Valid() || !AgentUpdate.Valid() || ProducerFor(AgentCreate) != AgentProducer || ProducerFor(AgentUpdate) != AgentProducer || !AgentProducer.Valid() || Action("agent.delete").Valid() {
		t.Fatal("Agent dispatch is not closed")
	}
}

func TestAgentAuditRejectsUnsafeMetadataAndNoopShape(t *testing.T) {
	value, fields := agentAuditFixture(t)
	raw := string(fields.Metadata.JSON())
	for name, candidate := range map[string]string{
		"input":     strings.TrimSuffix(raw, "}") + `,"instructions":"private"}`,
		"refs":      strings.TrimSuffix(raw, "}") + `,"allowed_tool_ids":[]}`,
		"duplicate": strings.TrimSuffix(raw, "}") + `,"command\u005fid":"` + value.CommandID + `"}`,
		"missing":   strings.Replace(raw, `"agent_id":"`+value.AgentID+`",`, "", 1),
		"null":      strings.Replace(raw, `"version":"2"`, `"version":null`, 1),
		"number":    strings.Replace(raw, `"version":"2"`, `"version":2`, 1),
		"trailing":  raw + `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeMetadata(AgentUpdate, []byte(candidate)); err == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
	for _, changed := range [][]string{nil, {}, {"instructions", "display_name"}, {"name", "name"}, {"tool_ids"}, {"private-value"}} {
		value.ChangedFields = changed
		if _, err := AgentMetadata(AgentUpdate, value); err == nil {
			t.Fatal("invalid change set accepted")
		}
	}
	value.ChangedFields = []string{"name"}
	for _, version := range []f.Version{0, 1} {
		value.Version = version
		if _, err := AgentMetadata(AgentUpdate, value); err == nil {
			t.Fatal("no-op/initial update accepted")
		}
	}
	value.Version = 2
	if _, err := AgentMetadata(AgentCreate, value); err == nil {
		t.Fatal("create has update version")
	}
	value.AgentID = "not-an-id"
	if _, err := AgentMetadata(AgentUpdate, value); err == nil {
		t.Fatal("invalid AgentID")
	}
	if _, err := fields.Metadata.KnowledgeFields(); err == nil {
		t.Fatal("Agent metadata projected as Knowledge")
	}
}

func TestAgentAuditRequiresHumanProjectAndCanonicalAppend(t *testing.T) {
	value, fields := agentAuditFixture(t)
	for _, mutate := range []func(*EntryFields){
		func(e *EntryFields) { e.Scope = id.SystemScope() },
		func(e *EntryFields) { e.Outcome = Unknown },
		func(e *EntryFields) { e.Action = AgentCreate },
		func(e *EntryFields) { e.Resource, _ = NewResource(AgentResource, value.CommandID) },
		func(e *EntryFields) { e.Resource, _ = NewResource(ObjectResource, value.AgentID) },
		func(e *EntryFields) { e.Associations.RequestID = value.CommandID },
		func(e *EntryFields) {
			r, _ := id.RegisterService(id.SecretService)
			e.Actor, _ = r.Actor(value.CommandID, e.Scope)
		},
		func(e *EntryFields) {
			p, _ := f.ParseID[id.Project](e.Scope.Details().ProjectID)
			a, _ := f.ParseID[id.Agent](value.AgentID)
			x, _ := f.ParseID[id.Execution](value.CommandID)
			e.Actor, _ = id.NewAgentRun(p, a, x)
		},
	} {
		bad := fields
		mutate(&bad)
		if _, err := NewEntry(bad); err == nil {
			t.Fatal("wrong Agent entry accepted")
		}
	}
	cause := "sha256:" + strings.Repeat("1", 64)
	if _, err := NewAppendKey(AgentProducer, cause, 0); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		cause   string
		ordinal int64
	}{{cause, 1}, {cause, -1}, {value.CommandID, 0}, {"raw-key", 0}} {
		if _, err := NewAppendKey(AgentProducer, v.cause, v.ordinal); err == nil {
			t.Fatal("noncanonical Agent append identity")
		}
	}
	if _, err := NewAppendKey(ModelProducer, value.CommandID, 2); err != nil {
		t.Fatal("legacy Model key changed", err)
	}
}
