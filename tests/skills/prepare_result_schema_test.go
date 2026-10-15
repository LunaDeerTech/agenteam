//go:build integration

package skill_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// This is a narrow SQL-function test after the real continuous migration chain.
// Session-local tables copy the production columns and CHECK constraints. Only
// the schema qualifier of the deployed trigger function changes. Cross-domain
// foreign keys and in-process return/Join authority remain App-test obligations.
func TestSkillPrepareReturnedUnknownSchema(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	source := skillMigrationSource(t, skillMigrationFiles(t, "00138"))
	if source.Target() != 138 {
		t.Fatal("requires the continuous source through migration 138")
	}
	migrateSkillDB(t, db, source)
	conn := db.Connect(t)
	ctx := testContext(t)
	for _, table := range []string{"operations", "attempts", "builtin_results"} {
		if _, err := conn.Exec(ctx, "CREATE TEMP TABLE "+table+" (LIKE agenteam_tool."+table+" INCLUDING DEFAULTS INCLUDING CONSTRAINTS)"); err != nil {
			t.Fatal("copy original result table shape", err)
		}
	}
	var definition string
	if err := conn.QueryRow(ctx, `SELECT pg_get_functiondef('agenteam_tool.check_builtin_result()'::regprocedure)`).Scan(&definition); err != nil {
		t.Fatal("read deployed result function", err)
	}
	if !strings.Contains(definition, "tool_skill_prepare_unknown_result") {
		t.Fatal("deployed function lacks the forward preparation constraint")
	}
	if _, err := conn.Exec(ctx, strings.ReplaceAll(definition, "agenteam_tool.", "pg_temp.")); err != nil {
		t.Fatal("install session-local original result function", err)
	}
	for _, table := range []string{"operations", "attempts", "builtin_results"} {
		if _, err := conn.Exec(ctx, "CREATE CONSTRAINT TRIGGER prepare_result_complete AFTER INSERT ON "+table+" DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pg_temp.check_builtin_result()"); err != nil {
			t.Fatal("bind original deferred result function", err)
		}
	}
	for _, test := range []struct {
		name, kind, phase, outcome, attempt, constraint string
		path                                            bool
	}{
		{name: "original_returned_unknown", kind: "skill.prepare", phase: "failed", outcome: "unknown", attempt: "unknown"},
		{name: "unknown_cannot_be_success", kind: "skill.prepare", phase: "succeeded", outcome: "success", attempt: "succeeded", constraint: "tool_skill_prepare_unknown_result"},
		{name: "unknown_cannot_publish_path", kind: "skill.prepare", phase: "failed", outcome: "unknown", attempt: "unknown", constraint: "tool_skill_prepare_unknown_result", path: true},
		{name: "other_handler_keeps_original_phase_rule", kind: "skill.read", phase: "failed", outcome: "unknown", attempt: "unknown", constraint: "tool_builtin_result_complete"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := tx.Rollback(ctx); err != nil {
					t.Error("rollback isolated result case", err)
				}
			}()
			operation, attempt, request := testID[struct{}](t), testID[struct{}](t), testID[struct{}](t)
			project, agent, execution := testID[id.Project](t), testID[id.Agent](t), testID[id.Execution](t)
			terminal, err := json.Marshal(map[string]any{"Format": "1", "Operation": operation.String(), "Attempt": attempt.String(), "Request": request.String(), "Outcome": "unknown", "Code": string(f.DependencyUnavailable)})
			if err != nil {
				t.Fatal(err)
			}
			result := sc.PrepareUnknownResult{SkillID: testID[pc.Skill](t), RevisionID: testID[sc.Revision](t), Revision: 1, MountID: testID[id.Mount](t), Outcome: "unknown", MayHavePrepared: true}
			result.Error.Code = f.DependencyUnavailable
			body, err := sc.EncodePrepareUnknownResult(result)
			if err != nil {
				t.Fatal(err)
			}
			if test.path {
				var fields map[string]any
				if err = json.Unmarshal(body, &fields); err != nil {
					t.Fatal(err)
				}
				fields["path"] = "skills/unproven"
				body, err = json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
			}
			_, err = tx.Exec(ctx, `INSERT INTO pg_temp.operations
 (operation_id,project_id,agent_id,execution_id,logical_call_id,invocation_id,call_id,tool_id,spec_revision,input_data,input_digest,skill_id,backend_key,fingerprint,phase,version,active_attempt_id,created_at,completed_at,outcome,terminal_data,operation_kind)
 VALUES($1,$2,$3,$4,$5,$6,'prepare-call',$7,1,convert_to('{}','UTF8'),'sha256:'||repeat('a',64),NULL,NULL,'sha256:'||repeat('b',64),$8,3,$9,now(),now(),$10,$11,$12)`, operation.String(), project.String(), agent.String(), execution.String(), testID[struct{}](t).String(), testID[struct{}](t).String(), testID[id.Tool](t).String(), test.phase, attempt.String(), test.outcome, terminal, test.kind)
			if err != nil {
				t.Fatal("original operation shape", err)
			}
			_, err = tx.Exec(ctx, `INSERT INTO pg_temp.attempts
 (attempt_id,operation_id,execution_id,project_id,agent_id,ordinal,process_id,fence,backend_request_id,phase,created_at,completed_at,retired,outcome_data)
 VALUES($1,$2,$3,$4,$5,1,$6,1,$7,$8,now(),now(),true,$9)`, attempt.String(), operation.String(), execution.String(), project.String(), agent.String(), testID[struct{}](t).String(), request.String(), test.attempt, terminal)
			if err != nil {
				t.Fatal("original retired attempt shape", err)
			}
			_, err = tx.Exec(ctx, `INSERT INTO pg_temp.builtin_results
 (operation_id,attempt_id,execution_id,project_id,agent_id,request_id,body_sha256,body_bytes,body_data)
 VALUES($1,$2,$3,$4,$5,$6,'sha256:'||encode(sha256($7::bytea),'hex'),octet_length($7::bytea),$7::bytea)`, operation.String(), attempt.String(), execution.String(), project.String(), agent.String(), request.String(), body)
			if err != nil {
				t.Fatal("original result checksum shape", err)
			}
			_, err = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`)
			if test.constraint == "" {
				if err != nil {
					t.Fatal("legal original returned unknown rejected", err)
				}
				return
			}
			var constraint *pgconn.PgError
			if !errors.As(err, &constraint) || constraint.Code != "23514" || constraint.ConstraintName != test.constraint {
				t.Fatalf("expected precise constraint %s, got %v", test.constraint, err)
			}
		})
	}
}
