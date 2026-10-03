//go:build integration

package process_test

import (
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"strings"
	"testing"
)

func TestCentralSecurityMissingStoragePreventsListening(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	m, err := postgres.NewMigrator(db.Config(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if state := m.Migrate(databaseContext(t)); !state.Migrated {
		t.Fatal(state.Fault)
	}
	// A real database with intact migration history but missing Audit storage
	// must fail security initialization instead of advertising availability.
	if _, err := db.Connect(t).Exec(databaseContext(t), `DROP TABLE agenteam_audit.audit_records`); err != nil {
		t.Fatal(err)
	}
	p := launch(t, "agenteam", nil, databaseEnvironment(db))
	p.wait(t, 1)
	if strings.Contains(p.stderr.String(), `"event":"listening"`) || !strings.Contains(p.stderr.String(), `"event":"security","phase":"failed"`) {
		t.Fatal("missing Audit storage reached HTTP or lost security stage")
	}
	noCentralBackends(t, db)
	assertDatabaseLogsSafe(t, p, db)
}
