package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestDatabaseEventsExposeOnlyTechnicalState(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(Central, slog.LevelInfo, &output)
	if err != nil {
		t.Fatal(err)
	}
	logger.Database(DatabaseMigrated, "", "", 1)
	logger.Database(DatabaseUnavailable, "MIGRATION_FAILED", "42501", 0)
	logger.Database(DatabasePhase("SQL-SENTINEL"), "password-SENTINEL", "42SENTINEL", -1)
	logger.InvalidConfig("AGENTEAM_CENTRAL_DATABASE_CA_FILE", "invalid")
	logger.InvalidConfig("PG*", "invalid")
	if strings.Contains(output.String(), "SENTINEL") {
		t.Fatal("raw database input reached the log")
	}
	decoder := json.NewDecoder(&output)
	var records []map[string]any
	for decoder.More() {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		if record["service"] != "central" || record["ready"] != false {
			t.Fatal("database log claimed product readiness")
		}
		records = append(records, record)
	}
	if len(records) != 5 || records[0]["database_phase"] != "migrated" || records[0]["migration_version"] != float64(1) || records[1]["sqlstate"] != "42501" || records[1]["code"] != "MIGRATION_FAILED" {
		t.Fatal("technical database state was lost")
	}
	if records[2]["database_phase"] != "unavailable" || records[2]["code"] != "DATABASE_FAILED" || records[2]["sqlstate"] != nil || records[2]["migration_version"] != nil {
		t.Fatal("untrusted database state was not bounded")
	}
	if records[3]["field"] != "AGENTEAM_CENTRAL_DATABASE_CA_FILE" || records[4]["field"] != "PG*" {
		t.Fatal("safe configuration field classification was lost")
	}
}
