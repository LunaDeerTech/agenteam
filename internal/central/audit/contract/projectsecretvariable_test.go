package contract

import (
	"encoding/json"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestProjectSecretVariableAuditClosedReadContract(t *testing.T) {
	const id = "01900000-0000-7000-8000-000000000006"
	base := projectEntry(t, ProjectUpdate)
	resource, _ := NewResource(ProjectVariableResource, id)
	for _, action := range []Action{ProjectSecretVariableCreate, ProjectSecretVariableUpdate, ProjectSecretVariableDelete} {
		t.Run(string(action), func(t *testing.T) {
			version, fields := f.Version(2), []string{"description", "value"}
			if action == ProjectSecretVariableCreate {
				version, fields = 1, []string{"created"}
			}
			if action == ProjectSecretVariableDelete {
				fields = []string{"deleted"}
			}
			m, err := ProjectSecretVariableMetadata(action, ProjectSecretVariableMetadataFields{id, version, fields})
			if err != nil {
				t.Fatal(err)
			}
			v := base
			v.Action, v.Resource, v.Metadata = action, resource, m
			if _, err = NewEntry(v); err != nil {
				t.Fatal(err)
			}
			if !action.Valid() || !ProjectSecretVariableAction(action) || ProjectVariableAction(action) || ProducerFor(action) != ProjectVariableProducer {
				t.Fatal("closed action/provenance")
			}
			if _, err = m.ProjectVariableFields(); err == nil {
				t.Fatal("ordinary projection accepted Secret")
			}
			copy, err := DecodeMetadata(action, m.JSON())
			if err != nil || string(copy.JSON()) != string(m.JSON()) {
				t.Fatal("roundtrip", err)
			}
			for _, raw := range []string{
				`null`, string(m.JSON()) + `{}`, strings.Replace(string(m.JSON()), `"version":`, `"version":"1","version":`, 1),
				strings.Replace(string(m.JSON()), `"changed_fields":`, `"changed_fields":null,"unused":`, 1),
				strings.Replace(string(m.JSON()), `"variable_id":`, `"Variable_ID":`, 1),
				strings.Replace(string(m.JSON()), `"version":"`+version.String()+`"`, `"version":2`, 1),
				strings.Replace(string(m.JSON()), `"changed_fields":`, `"private-canary/key":"private-canary","changed_fields":`, 1),
			} {
				_, err := DecodeMetadata(action, []byte(raw))
				if err == nil {
					t.Fatal("bad metadata accepted")
				}
				public, _ := json.Marshal(err)
				if strings.Contains(string(public), "private-canary") {
					t.Fatal("unsafe error path")
				}
			}
			for _, mutate := range []func(*EntryFields){
				func(v *EntryFields) { v.Scope = i.SystemScope() },
				func(v *EntryFields) { v.Outcome = Denied },
				func(v *EntryFields) { v.Associations.RequestID = id },
				func(v *EntryFields) { v.Resource, _ = NewResource(SecretResource, id) },
				func(v *EntryFields) {
					v.Resource, _ = NewResource(ProjectVariableResource, "01900000-0000-7000-8000-000000000007")
				},
				func(v *EntryFields) {
					registration, _ := i.RegisterService(i.ProjectInitialization)
					v.Actor, _ = registration.Actor(id, v.Scope)
				},
			} {
				bad := v
				mutate(&bad)
				if _, err = NewEntry(bad); err == nil {
					t.Fatal("invalid relation accepted")
				}
			}
			fields[0] = "private-canary"
			projected, _ := copy.ProjectSecretVariableFields()
			projected.ChangedFields[0] = "private-canary"
			if strings.Contains(string(m.JSON()), "private-canary") || strings.Contains(string(copy.JSON()), "private-canary") {
				t.Fatal("mutable metadata alias")
			}
		})
	}
	for _, action := range []Action{ProjectVariableCreate, SecretCreate, "project.secret_variable.unknown"} {
		if _, err := ProjectSecretVariableMetadata(action, ProjectSecretVariableMetadataFields{id, 1, []string{"created"}}); err == nil {
			t.Fatal("foreign action")
		}
	}
	for _, fields := range [][]string{nil, {"value", "name"}, {"value", "value"}, {"credential_id"}, {"created"}} {
		if _, err := ProjectSecretVariableMetadata(ProjectSecretVariableUpdate, ProjectSecretVariableMetadataFields{id, 2, fields}); err == nil {
			t.Fatal("bad fields")
		}
	}
	if _, err := ProjectSecretVariableMetadata(ProjectSecretVariableUpdate, ProjectSecretVariableMetadataFields{id, 1, []string{"value"}}); err == nil {
		t.Fatal("update version")
	}
}
