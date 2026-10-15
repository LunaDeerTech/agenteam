//go:build integration

package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// Execute the migrated production trigger body in an isolated temporary
// ledger. This isolates JSONB evaluation and its positive/negative envelope
// gates; the App test proves the real authorized Operation/Attempt chain.
func TestMCPResultSQLTypedEnvelope(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	migrator, err := postgres.NewMigrator(db.Config(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if r := migrator.Migrate(ctx); !r.Migrated {
		t.Fatal("migrate production result guard", r.Fault)
	}
	conn := db.Connect(t)
	var production string
	if err := conn.QueryRow(ctx, `SELECT pg_get_functiondef('agenteam_tool.check_builtin_result()'::regprocedure)`).Scan(&production); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TEMP TABLE operations(operation_id text,operation_kind text,phase text,active_attempt_id text,version bigint,outcome text,terminal_data bytea,fingerprint text,call_id text,project_id text)`,
		`CREATE TEMP TABLE attempts(attempt_id text,operation_id text,execution_id text,project_id text,agent_id text,backend_request_id text,phase text,retired boolean)`,
		`CREATE TEMP TABLE builtin_results(operation_id text,attempt_id text,execution_id text,project_id text,agent_id text,request_id text,body_data bytea)`,
		`CREATE TEMP TABLE approval_denials(operation_id text,fingerprint text,request_id text)`,
		strings.ReplaceAll(production, "agenteam_tool.", "pg_temp."),
		`CREATE TRIGGER result_gate AFTER INSERT ON builtin_results FOR EACH ROW EXECUTE FUNCTION pg_temp.check_builtin_result()`,
	} {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatal("install exact production body against probe ledger", err)
		}
	}
	const project = "01900000-0000-7000-8000-000000000001"
	file := func(p string) map[string]any {
		return map[string]any{"kind": "file", "media_type": "text/plain", "ref": map[string]any{"kind": "mcp_file", "project_id": p, "file_id": "01900000-0000-7000-8000-000000000002"}}
	}
	tests := []struct {
		name, kind string
		unknown    bool
		parts      []any
		extra      bool
		valid      bool
	}{
		{"remote-text", "mcp.call", false, []any{map[string]any{"kind": "text", "text": "safe"}}, false, true},
		{"resource-list-text", "mcp.list-resources", false, []any{map[string]any{"kind": "text", "text": "safe"}}, false, true},
		{"resource-read-text", "mcp.read-resource", false, []any{map[string]any{"kind": "text", "text": "safe"}}, false, true},
		{"canonical-file", "mcp.call", false, []any{file(project)}, false, true},
		{"known-unknown", "mcp.call", true, []any{map[string]any{"kind": "text", "text": "unknown"}}, false, true},
		{"foreign-file", "mcp.call", false, []any{file("01900000-0000-7000-8000-000000000003")}, false, false},
		{"unimplemented-part", "mcp.call", false, []any{map[string]any{"kind": "audio"}}, false, false},
		{"empty-parts", "mcp.call", false, []any{}, false, false},
		{"unknown-envelope-field", "mcp.call", false, []any{map[string]any{"kind": "text", "text": "safe"}}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			phase, outcome, terminal, attempt := "succeeded", "success", "success", "succeeded"
			if tt.unknown {
				phase, outcome, terminal, attempt = "failed", "unknown", "unknown", "unknown"
			}
			terminalData, _ := json.Marshal(map[string]any{"Outcome": terminal})
			body := map[string]any{"format": 1, "call_id": "original-call", "is_error": tt.unknown, "outcome_known": !tt.unknown, "may_have_executed": true, "failure_code": "", "parts": tt.parts}
			if tt.extra {
				body["unexpected"] = true
			}
			raw, _ := json.Marshal(body)
			if _, err = tx.Exec(ctx, `INSERT INTO operations VALUES('operation',$1,$2,'attempt',3,$3,$4,'fingerprint','original-call',$5)`, tt.kind, phase, outcome, terminalData, project); err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(ctx, `INSERT INTO attempts VALUES('attempt','operation','execution',$1,'agent','request',$2,true)`, project, attempt); err != nil {
				t.Fatal(err)
			}
			_, err = tx.Exec(ctx, `INSERT INTO builtin_results VALUES('operation','attempt','execution',$1,'agent','request',$2)`, project, raw)
			if tt.valid {
				if err != nil {
					t.Fatal("valid typed result rejected", err)
				}
			} else {
				var pg *pgconn.PgError
				if !errors.As(err, &pg) || pg.Code != "23514" || pg.ConstraintName != "tool_mcp_result_envelope" {
					t.Fatalf("invalid typed result did not reach its closed constraint: %v", err)
				}
			}
		})
	}
}
