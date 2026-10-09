package contract

import (
	"encoding/json"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"strings"
	"testing"
)

func TestProjectVariableAuditClosedIdentityAndSafeMetadata(t *testing.T) {
	id := "01900000-0000-7000-8000-000000000006"
	base := projectEntry(t, ProjectUpdate)
	resource, err := NewResource(ProjectVariableResource, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []Action{ProjectVariableCreate, ProjectVariableUpdate, ProjectVariableDelete} {
		version := f.Version(2)
		fields := []string{"description", "value"}
		if a == ProjectVariableCreate {
			version = 1
			fields = []string{"created"}
		}
		if a == ProjectVariableDelete {
			fields = []string{"deleted"}
		}
		m, err := ProjectVariableMetadata(a, ProjectVariableMetadataFields{id, version, fields})
		if err != nil {
			t.Fatal(err)
		}
		entry := base
		entry.Action = a
		entry.Resource = resource
		entry.Metadata = m
		if _, err = NewEntry(entry); err != nil {
			t.Fatal(err)
		}
		if !a.Valid() || ProducerFor(a) != ProjectVariableProducer {
			t.Fatal("registration")
		}
		copy, err := DecodeMetadata(a, m.JSON())
		if err != nil || string(copy.JSON()) != string(m.JSON()) {
			t.Fatal("decode")
		}
		for _, bad := range [][]byte{[]byte(`null`), []byte(strings.Replace(string(m.JSON()), `"variable_id":`, `"Variable_ID":`, 1)), []byte(strings.Replace(string(m.JSON()), `"version":`, `"version":"1","version":`, 1)), []byte(strings.Replace(string(m.JSON()), `"changed_fields":`, `"value":"must-not-store","changed_fields":`, 1))} {
			if _, err = DecodeMetadata(a, bad); err == nil {
				t.Fatal("bad metadata")
			}
		}
		for _, modify := range []func(*EntryFields){func(v *EntryFields) { v.Scope = i.SystemScope() }, func(v *EntryFields) { v.Outcome = Denied }, func(v *EntryFields) { v.Associations.RequestID = id }, func(v *EntryFields) {
			v.Resource, _ = NewResource(ProjectVariableResource, "01900000-0000-7000-8000-000000000007")
		}} {
			bad := entry
			modify(&bad)
			if _, err = NewEntry(bad); err == nil {
				t.Fatal("invalid relation")
			}
		}
		fields[0] = "mutated"
		if _, err = DecodeMetadata(a, m.JSON()); err != nil {
			t.Fatal("metadata alias")
		}
		raw, _ := json.Marshal(copy)
		if strings.Contains(string(raw), "must-not-store") {
			t.Fatal("payload leak")
		}
	}
	if _, err = NewAppendKey(ProjectVariableProducer, "sha256:"+strings.Repeat("a", 64), 0); err != nil {
		t.Fatal(err)
	}
	if _, err = NewAppendKey(ProjectVariableProducer, id, 0); err == nil {
		t.Fatal("raw id cause")
	}
	if _, err = NewAppendKey(ProjectVariableProducer, "sha256:"+strings.Repeat("a", 64), 1); err == nil {
		t.Fatal("ordinal")
	}
}
