package audit

import (
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
)

func TestProjectSecretVariableActualRowDecoder(t *testing.T) {
	for _, change := range []string{"create", "update", "delete"} {
		t.Run(change, func(t *testing.T) {
			r := projectReadRow()
			version, field := "2", "value"
			if change == "create" {
				version, field = "1", "created"
			}
			if change == "delete" {
				field = "deleted"
			}
			r.values[12], r.values[14], r.values[15] = "project.secret_variable."+change, "project_variable", systemRecordID
			r.values[16] = `{"variable_id":"` + systemRecordID + `","version":"` + version + `","changed_fields":["` + field + `"]}`
			got, err := scanRecord(r)
			if err != nil || got.Action != c.Action("project.secret_variable."+change) || got.Summary != "Audit event" {
				t.Fatal("decode", err)
			}
			for column, bad := range map[int]string{2: "system", 12: "project.secret_variable.unknown", 13: "denied", 14: "secret", 15: projectReadID, 21: systemRecordID} {
				x := r
				x.values = append([]any(nil), r.values...)
				x.values[column] = bad
				if _, err = scanRecord(x); err == nil {
					t.Fatalf("bad column %d", column)
				}
			}
		})
	}
}
