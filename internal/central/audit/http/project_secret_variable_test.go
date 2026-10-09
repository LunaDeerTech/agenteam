package audithttp

import (
	"context"
	"encoding/json"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestProjectSecretVariableAuditWireAndSchema(t *testing.T) {
	p, _ := foundation.ParseID[identity.Project](projectAuditWireID)
	var vectors []projectAuditSchemaVector
	for _, change := range []string{"create", "update", "delete"} {
		version, fields := "2", `["description","value"]`
		if change == "create" {
			version, fields = "1", `["created"]`
		}
		if change == "delete" {
			fields = `["deleted"]`
		}
		tc := typedCase{"project.secret_variable." + change, "project_variable", `{"variable_id":"` + wireID + `","version":"` + version + `","changed_fields":` + fields + `}`, "", c.Success}
		r := projectAuditTestRecord(t, tc, identity.Human, false)
		body, err := projectAuditEncodeRecord(context.Background(), p, r.AuditID, r)
		if err != nil {
			t.Fatal(change, err)
		}
		vectors = append(vectors, projectAuditSchemaVector{change, "ProjectAuditRecord", body, true})
		if change == "update" {
			for _, fields := range []string{`["description"]`, `["name"]`, `["value"]`, `["description","name"]`, `["description","value"]`, `["name","value"]`, `["description","name","value"]`} {
				variant := tc
				variant.metadata = `{"variable_id":"` + wireID + `","version":"2","changed_fields":` + fields + `}`
				row := projectAuditTestRecord(t, variant, identity.Human, false)
				encoded, err := projectAuditEncodeRecord(context.Background(), p, row.AuditID, row)
				if err != nil {
					t.Fatal("canonical field subset", err)
				}
				vectors = append(vectors, projectAuditSchemaVector{change + "/" + fields, "ProjectAuditRecord", encoded, true})
			}
		}
		for name, modify := range map[string]func(map[string]any){
			"unknown-action": func(v map[string]any) { v["action"] = "project.secret_variable.unknown" },
			"value":          func(v map[string]any) { v["metadata"].(map[string]any)["value"] = "private-canary" },
			"credential":     func(v map[string]any) { v["metadata"].(map[string]any)["credential_id"] = wireID },
			"null":           func(v map[string]any) { v["metadata"] = nil },
			"version":        func(v map[string]any) { v["metadata"].(map[string]any)["version"] = "0" },
			"unordered":      func(v map[string]any) { v["metadata"].(map[string]any)["changed_fields"] = []string{"value", "name"} },
			"duplicate":      func(v map[string]any) { v["metadata"].(map[string]any)["changed_fields"] = []string{"value", "value"} },
			"associations":   func(v map[string]any) { v["associations"] = map[string]any{"request_id": wireID} },
			"outcome":        func(v map[string]any) { v["outcome"] = "denied" },
		} {
			var wire map[string]any
			if json.Unmarshal(body, &wire) != nil {
				t.Fatal("wire")
			}
			modify(wire)
			bad, _ := json.Marshal(wire)
			vectors = append(vectors, projectAuditSchemaVector{change + "/" + name, "ProjectAuditRecord", bad, false})
		}
		for _, modify := range []func(*c.SafeRecord){
			func(v *c.SafeRecord) { v.Outcome = c.Denied },
			func(v *c.SafeRecord) { v.Associations.RequestID = wireID },
			func(v *c.SafeRecord) { v.Resource, _ = c.NewResource(c.SecretResource, wireID) },
			func(v *c.SafeRecord) {
				v.Actor.Kind = identity.Service
				v.Actor.Service = identity.ProjectInitialization
			},
		} {
			bad := r
			modify(&bad)
			if b, e := projectAuditEncodeRecord(context.Background(), p, bad.AuditID, bad); e == nil || len(b) != 0 {
				t.Fatal("projection accepted bad relation")
			}
		}
	}
	projectAuditStandardSchema(t, vectors)
}
