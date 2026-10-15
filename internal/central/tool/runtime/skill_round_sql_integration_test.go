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

// Probe the migrated successor guard after an original Tool result. Only the
// schema names are redirected to a temporary ledger: the production function
// must evaluate its empty, applied and skipped disposition branches itself.
func TestSkillRoundSQLPair(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	migrator, err := postgres.NewMigrator(db.Config(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if r := migrator.Migrate(ctx); !r.Migrated {
		t.Fatal("migrate production successor guard", r.Fault)
	}
	conn := db.Connect(t)
	var production string
	if err := conn.QueryRow(ctx, `SELECT pg_get_functiondef('agenteam_execution.require_loop_skill_pair(text,text)'::regprocedure)`).Scan(&production); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TEMP TABLE loop_skill_inputs(execution_id text,round_id text,source_round_id text,snapshot_id text,assignment_cutoff bigint,content bytea,control_sequence bigint,created_at timestamptz)`,
		`CREATE TEMP TABLE rounds(execution_id text,id text,snapshot_id text,predecessor_round_id text,schema_version bigint,round_ordinal bigint,skill_input_required boolean,transcript_start bigint,created_at timestamptz)`,
		`CREATE TEMP TABLE loop_tool_results(execution_id text,round_id text,transcript_sequence bigint,created_at timestamptz)`,
		`CREATE TEMP TABLE loop_approval_results(LIKE loop_tool_results)`,
		`CREATE TEMP TABLE snapshots(execution_id text,id text,content bytea)`,
		`CREATE TEMP TABLE skill_assignment_ingress(execution_id text,change_id text,outcome text,change_data bytea)`,
		`CREATE TEMP TABLE transcript_entries(execution_id text,round_id text,sequence bigint,loop_entry boolean,kind text,round_input_id text,created_at timestamptz,message bytea)`,
		strings.ReplaceAll(production, "agenteam_execution.", "pg_temp."),
	} {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatal("install exact successor guard against probe ledger", err)
		}
	}
	for _, name := range []string{"unchanged", "applied", "skipped", "missing-disposition", "foreign-addition", "wrong-control"} {
		t.Run(name, func(t *testing.T) {
			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			exec := func(statement string, args ...any) {
				t.Helper()
				if _, err := tx.Exec(ctx, statement, args...); err != nil {
					t.Fatal("prepare successor facts", err)
				}
			}
			encode := func(v any) []byte {
				t.Helper()
				b, err := json.Marshal(v)
				if err != nil {
					t.Fatal(err)
				}
				return b
			}
			at := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
			bindings, additions, dispositions := []any{}, []any{}, []any{}
			cutoff, transcriptStart := int64(1), int64(4)
			var sequence any
			var controlValue any
			if name != "unchanged" {
				cutoff = 2
				binding := map[string]any{"skill_id": "skill", "assignment_id": "assignment", "assignment_sequence": "2"}
				exec(`INSERT INTO skill_assignment_ingress VALUES('execution','change','queued',$1)`, encode(map[string]any{"assignment_sequence": "2", "binding": binding}))
				state, reason := "applied", ""
				if name == "skipped" {
					state, reason = "skipped", "removed"
				} else {
					sequence, controlValue, transcriptStart = int64(4), "4", 5
					if name == "foreign-addition" {
						binding = map[string]any{"skill_id": "foreign", "assignment_id": "assignment", "assignment_sequence": "2"}
					}
					bindings = append(bindings, binding)
					additions = append(additions, map[string]any{"change_id": "change", "binding": binding})
				}
				if name != "missing-disposition" {
					dispositions = append(dispositions, map[string]any{"change_id": "change", "assignment_sequence": "2", "state": state, "reason": reason})
				}
			}
			cutoffText := "1"
			if cutoff == 2 {
				cutoffText = "2"
			}
			input := map[string]any{"schema_version": "1", "execution_id": "execution", "snapshot_id": "snapshot", "round_id": "next", "source_round_id": "previous", "previous_cutoff": "1", "assignment_cutoff": cutoffText, "previous_bindings": []any{}, "bindings": bindings, "dispositions": dispositions, "additions": additions, "control_sequence": controlValue, "created_at": at}
			exec(`INSERT INTO rounds VALUES('execution','previous','snapshot',NULL,4,1,true,1,$1),('execution','next','snapshot','previous',4,2,true,$2,$1)`, at, transcriptStart)
			exec(`INSERT INTO loop_tool_results VALUES('execution','previous',3,$1)`, at)
			exec(`INSERT INTO snapshots VALUES('execution','snapshot',$1)`, encode(map[string]any{"context": map[string]any{"preparation_input": map[string]any{"skills": map[string]any{"assignment_sequence": "1", "bindings": []any{}}}}}))
			exec(`INSERT INTO loop_skill_inputs VALUES('execution','next','previous','snapshot',$1,$2,$3,$4)`, cutoff, encode(input), sequence, at)
			if sequence != nil {
				control := map[string]any{"kind": "skills_added", "schema_version": "1", "execution_id": "execution", "round_id": "next", "source_round_id": "previous", "assignment_cutoff": cutoffText, "payload": additions, "transcript_position": "4"}
				if name == "wrong-control" {
					control["assignment_cutoff"] = "1"
				}
				exec(`INSERT INTO transcript_entries VALUES('execution','next',4,true,'control',NULL,$1,$2)`, at, encode(control))
			}
			_, err = tx.Exec(ctx, `SELECT pg_temp.require_loop_skill_pair('execution','next')`)
			if name == "unchanged" || name == "applied" || name == "skipped" {
				if err != nil {
					t.Fatal("valid original Tool successor rejected", err)
				}
			} else {
				var pg *pgconn.PgError
				if !errors.As(err, &pg) || pg.Code != "23514" {
					t.Fatalf("invalid original Tool successor did not reach its constraint: %v", err)
				}
			}
		})
	}
}
