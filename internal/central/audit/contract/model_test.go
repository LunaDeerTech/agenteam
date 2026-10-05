package contract

import (
	"bytes"
	"encoding/json"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestModelAuditClosedActionsMetadataAndResourceBinding(t *testing.T) {
	provider := "01900000-0000-7000-8000-000000000001"
	model := "01900000-0000-7000-8000-000000000002"
	selection := "01900000-0000-7000-8000-000000000003"
	replacement := "01900000-0000-7000-8000-000000000004"
	zero := f.Progress(0)
	u, _ := f.ParseID[id.User](provider)
	session, _ := f.ParseID[id.Session](model)
	actor, _ := id.NewHuman(u, session)
	for _, action := range []Action{ProviderCreate, ProviderUpdate, ProviderDelete, ModelCreate, ModelUpdate, ModelDelete, ModelSelectionUpdate} {
		t.Run(string(action), func(t *testing.T) {
			fields := ModelMetadataFields{ProviderID: provider, Version: 1}
			kind, resource := ModelProviderResource, provider
			switch action {
			case ProviderCreate, ModelCreate:
				fields.ChangedFields = []string{"created"}
			case ProviderDelete, ModelDelete:
				fields.ChangedFields = []string{"deleted"}
			case ProviderUpdate, ModelUpdate:
				fields.ChangedFields = []string{"name", "enabled"}
			case ModelSelectionUpdate:
				fields = ModelMetadataFields{SelectionID: selection, Version: 1, SelectorKind: "platform", ChangedFields: []string{"selection"}}
				kind, resource = ModelSelectionResource, selection
			}
			if action == ModelCreate || action == ModelUpdate || action == ModelDelete {
				fields.ModelID = model
				kind, resource = ModelConfigResource, model
			}
			if action == ModelDelete {
				fields.AffectedCount = &zero
			}
			metadata, e := ModelMetadata(action, fields)
			if e != nil {
				t.Fatal(e)
			}
			round, e := DecodeMetadata(action, metadata.JSON())
			if e != nil || !bytes.Equal(round.JSON(), metadata.JSON()) {
				t.Fatal("typed metadata did not roundtrip", e)
			}
			r, e := NewResource(kind, resource)
			if e != nil {
				t.Fatal(e)
			}
			entry, e := NewEntry(EntryFields{Scope: id.SystemScope(), Actor: actor, Action: action, Outcome: Success, Resource: r, Metadata: metadata})
			if e != nil || entry.Validate() != nil {
				t.Fatal(e)
			}
			if !action.Valid() || ProducerFor(action) != ModelProducer || !ModelProducer.Valid() {
				t.Fatal("closed dispatch missing")
			}
			wrong, _ := NewResource(kind, replacement)
			if _, e = NewEntry(EntryFields{Scope: id.SystemScope(), Actor: actor, Action: action, Outcome: Success, Resource: wrong, Metadata: metadata}); e == nil {
				t.Fatal("wrong resource accepted")
			}
			bad := fields
			bad.ChangedFields = []string{"endpoint-secret"}
			if _, e = ModelMetadata(action, bad); e == nil {
				t.Fatal("free metadata accepted")
			}
			var object map[string]json.RawMessage
			_ = json.Unmarshal(metadata.JSON(), &object)
			object["base_url"] = json.RawMessage(`"https://private.example"`)
			raw, _ := json.Marshal(object)
			if _, e = DecodeMetadata(action, raw); e == nil {
				t.Fatal("unknown field accepted")
			}
			for _, suffix := range []string{`,"version":"1"}`, `,"model_id":null}`, `,"replacement_id":""}`} {
				raw := append([]byte(nil), metadata.JSON()...)
				raw = append(raw[:len(raw)-1], []byte(suffix)...)
				if _, e = DecodeMetadata(action, raw); e == nil {
					t.Fatal("duplicate/null/forbidden explicit zero accepted")
				}
			}
		})
	}
}

func TestModelAuditDeleteCountExactZeroAndImmutableMetadata(t *testing.T) {
	count := f.Progress(9007199254740993)
	fields := ModelMetadataFields{ProviderID: "01900000-0000-7000-8000-000000000001", ModelID: "01900000-0000-7000-8000-000000000002", Version: 1, ChangedFields: []string{"deleted"}, AffectedCount: &count}
	m, e := ModelMetadata(ModelDelete, fields)
	if e != nil {
		t.Fatal(e)
	}
	count = 0
	fields.ChangedFields[0] = "leak"
	v, e := m.ModelFields()
	if e != nil || *v.AffectedCount != 9007199254740993 || v.ChangedFields[0] != "deleted" {
		t.Fatal("metadata aliased caller")
	}
	raw := m.JSON()
	if !bytes.Contains(raw, []byte(`"9007199254740993"`)) {
		t.Fatal("count lost integer precision")
	}
}
