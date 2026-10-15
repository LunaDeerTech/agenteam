//go:build integration

package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// Use the real migrated CHECK expression. The temporary table isolates this
// routing closed set; it does not substitute for the original Execution FK,
// captured binding authority, or operation ledger exercised by the App test.
func TestSkillToolCallRoutingSQL(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	migrator, err := postgres.NewMigrator(db.Config(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if result := migrator.Migrate(ctx); !result.Migrated {
		t.Fatal("migrate skill routing", result.Fault)
	}
	conn := db.Connect(t)
	var check string
	if err := conn.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='agenteam_execution.tool_calls'::regclass AND conname='tool_calls_operation_name_check'`).Scan(&check); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `CREATE TEMP TABLE skill_routing_probe(operation_name text NOT NULL, CONSTRAINT original_routing_check `+check+`)`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"skill.install", "skill.list", "skill.read", "skill.assign", "skill.prepare", "read-file", "write-file", "run-command", "mcp.call", "mcp.read-resource", "meeting.request-decision"} {
		if _, err := conn.Exec(ctx, `INSERT INTO skill_routing_probe VALUES($1)`, name); err != nil {
			t.Fatalf("registered routing %s: %v", name, err)
		}
	}
	for _, name := range []string{"", "skill.delete", "skill.install.v2", "skill.list-all"} {
		_, err := conn.Exec(ctx, `INSERT INTO skill_routing_probe VALUES($1)`, name)
		var fault *pgconn.PgError
		if !errors.As(err, &fault) || fault.Code != "23514" || fault.ConstraintName != "original_routing_check" {
			t.Fatalf("unregistered routing %q was not rejected by the original check: %v", name, err)
		}
	}
}
