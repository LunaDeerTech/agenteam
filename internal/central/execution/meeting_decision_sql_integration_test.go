//go:build integration

package execution

import (
	"context"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// The actual PostgreSQL domain/base inference caused 42P08 before any row
// could be consumed. Exercise the production statement and its no-match gate
// against the real migrated schema before the complete App continuation test.
func TestMeetingDecisionConsumptionSQL(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	migrator, err := postgres.NewMigrator(db.Config(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if result := migrator.Migrate(ctx); !result.Migrated {
		t.Fatal("Meeting Decision schema migration", result.Fault)
	}
	conn := db.Connect(t)
	id := newTestID[struct{}](t).String()
	digest := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	tag, err := conn.Exec(ctx, meetingDecisionResultInsert, id, id, id, id, int64(2), "original-decision-call", digest, int64(3), int64(6), time.Now().UTC(), id, id, digest, digest)
	if err != nil {
		t.Fatal("Meeting Decision SQL must infer all parameters against their actual PostgreSQL types", err)
	}
	if tag.RowsAffected() != 0 {
		t.Fatal("Decision result inserted without its original consumed Tool result")
	}
}
