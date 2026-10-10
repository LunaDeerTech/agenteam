//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	toolruntime "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// SQL candidates below are rollback-only constraint material. In particular,
// their Execution, Tool definition, Agent provenance and terminal bytes are
// never supplied to a service as authorization or a successful Runtime receipt.
// The only committed installation is made by the real Human/Object service.
// Real Trigger, Snapshot, ToolCall authority and Agent installation stay unbound.
func TestAgentRuntimeSchema(t *testing.T) {
	t.Run("prefix36-upgrade-and-repeat", func(t *testing.T) {
		for _, upgrade := range []bool{false, true} {
			db := pgfixture.NewDatabase(t)
			if upgrade {
				migrate(t, db, migrationPrefix(t, "00036"))
			}
			migrate(t, db, migrationPrefix(t, "00039"))
			raw := openStore(t, db.Config(t, nil))
			assertRuntimeSchema39(t, raw)
			migrate(t, db, migrationPrefix(t, "00039"))
			assertRuntimeSchema39(t, raw)
			if runtimeSchemaCounts(t, raw) != ([3]int64{}) {
				t.Fatal("migration manufactured execution or Runtime facts")
			}
		}
	})
	t.Run("runtime-attempt-and-terminal", func(t *testing.T) {
		raw := openStore(t, newDatabase(t).Config(t, nil))
		seed := newRuntimeSchemaSeed(t)
		terminal := []byte(`{"format":1,"status":"success","receipt":{"schema_probe":true}}`)
		runtimeSchemaProbe(t, raw, "terminal-roundtrip", "", "", "", seed.operation, func(ctx context.Context, x postgres.SQLExecutor) error {
			if err := seed.attempt(ctx, x, seed.attemptID, 1, seed.agent.String()); err != nil {
				return err
			}
			if _, err := x.Exec(ctx, `UPDATE agenteam_tool.operations SET active_attempt_id=$2,phase='running',version=2 WHERE operation_id=$1`, seed.operationID, seed.attemptID); err != nil {
				return err
			}
			if _, err := x.Exec(ctx, `UPDATE agenteam_tool.attempts SET phase='succeeded',completed_at=clock_timestamp(),outcome_data=$2,retired=true WHERE attempt_id=$1`, seed.attemptID, terminal); err != nil {
				return err
			}
			if _, err := x.Exec(ctx, `UPDATE agenteam_tool.operations SET phase='succeeded',version=3,completed_at=clock_timestamp(),outcome='success',terminal_data=$2 WHERE operation_id=$1`, seed.operationID, terminal); err != nil {
				return err
			}
			var operationBytes, attemptBytes []byte
			var retired bool
			if err := x.QueryRow(ctx, `SELECT o.terminal_data,a.outcome_data,a.retired FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id WHERE o.operation_id=$1`, seed.operationID).Scan(&operationBytes, &attemptBytes, &retired); err != nil {
				return err
			}
			if !retired || !bytes.Equal(operationBytes, terminal) || !bytes.Equal(attemptBytes, terminal) {
				return errors.New("terminal bytes did not roundtrip")
			}
			return nil
		})
		for _, p := range []struct {
			label, code, table, constraint string
			apply                          func(context.Context, postgres.SQLExecutor) error
		}{
			{"one-active-attempt", "23505", "attempts", "tool_attempt_active", func(ctx context.Context, x postgres.SQLExecutor) error {
				if err := seed.attempt(ctx, x, seed.attemptID, 1, seed.agent.String()); err != nil {
					return err
				}
				return seed.attempt(ctx, x, id[struct{}](t).String(), 2, seed.agent.String())
			}},
			{"attempt-scope", "23503", "attempts", "", func(ctx context.Context, x postgres.SQLExecutor) error {
				return seed.attempt(ctx, x, seed.attemptID, 1, id[i.Agent](t).String())
			}},
			{"operation-execution-parent", "23503", "operations", "tool_operations_execution_scope", func(ctx context.Context, x postgres.SQLExecutor) error {
				next := id[struct{}](t).String()
				_, err := x.Exec(ctx, `INSERT INTO agenteam_tool.operations(operation_id,project_id,agent_id,execution_id,logical_call_id,invocation_id,call_id,tool_id,spec_revision,input_data,input_digest,skill_id,backend_key,fingerprint,phase,version)
 SELECT $1,project_id,agent_id,$2,logical_call_id,invocation_id,call_id,tool_id,spec_revision,input_data,input_digest,skill_id,$3,fingerprint,'created',1 FROM agenteam_tool.operations WHERE operation_id=$4`, next, id[i.Execution](t).String(), "tool.skill.install:"+next, seed.operationID)
				return err
			}},
			{"active-parent-deferred", "23503", "operations", "tool_active_attempt_parent", func(ctx context.Context, x postgres.SQLExecutor) error {
				_, err := x.Exec(ctx, `UPDATE agenteam_tool.operations SET active_attempt_id=$2,phase='running',version=2 WHERE operation_id=$1`, seed.operationID, seed.attemptID)
				return err
			}},
			{"success-needs-terminal", "23514", "operations", "", func(ctx context.Context, x postgres.SQLExecutor) error {
				_, err := x.Exec(ctx, `UPDATE agenteam_tool.operations SET phase='succeeded',version=2,completed_at=clock_timestamp(),outcome='success' WHERE operation_id=$1`, seed.operationID)
				return err
			}},
			{"input-immutable", "23514", "", "", func(ctx context.Context, x postgres.SQLExecutor) error {
				_, err := x.Exec(ctx, `UPDATE agenteam_tool.operations SET input_data=$2,version=2 WHERE operation_id=$1`, seed.operationID, []byte(`{"changed":true}`))
				return err
			}},
		} {
			runtimeSchemaProbe(t, raw, p.label, p.code, p.table, p.constraint, seed.operation, p.apply)
		}
		if runtimeSchemaCounts(t, raw) != ([3]int64{}) {
			t.Fatal("Runtime probes escaped rollback")
		}
		service, err := toolruntime.New(raw, nil)
		if service != nil {
			t.Fatal("missing Execution Tool authority constructed a Runtime")
		}
		runtimeSchemaFault(t, err, f.DependencyUnbound)
	})
	t.Run("execution-slot-and-unbound-launch", func(t *testing.T) {
		v := newVariableHTTPFixture(t)
		seed := newRuntimeSchemaSeed(t)
		runtimeSchemaProbe(t, v.raw, "slot-release", "", "", "", seed.execution, func(ctx context.Context, x postgres.SQLExecutor) error {
			for _, query := range []string{
				`UPDATE agenteam_execution.executions SET status='preparing',version=2,updated_at=updated_at+interval '1 microsecond' WHERE id=$1`,
				`UPDATE agenteam_execution.executions SET status='running',version=3,snapshot_id=$1,started_at=updated_at+interval '1 microsecond',updated_at=updated_at+interval '1 microsecond' WHERE id=$1`,
				`UPDATE agenteam_execution.executions SET status='waiting',version=4,updated_at=updated_at+interval '1 microsecond' WHERE id=$1`,
				`UPDATE agenteam_execution.executions SET status='cancelled',version=5,completed_at=updated_at+interval '1 microsecond',updated_at=updated_at+interval '1 microsecond' WHERE id=$1`,
			} {
				if _, err := x.Exec(ctx, query, seed.executionID.String()); err != nil {
					return err
				}
			}
			next := newRuntimeSchemaSeed(t)
			next.agent = seed.agent
			return next.execution(ctx, x)
		})
		for _, p := range []struct {
			label, code, constraint string
			apply                   func(context.Context, postgres.SQLExecutor) error
		}{
			{"global-active-slot", "23505", "executions_active_agent", func(ctx context.Context, x postgres.SQLExecutor) error {
				next := newRuntimeSchemaSeed(t)
				next.agent = seed.agent // Another Project must not bypass the Agent slot.
				return next.execution(ctx, x)
			}},
			{"launch-immutable", "23514", "execution_immutable", func(ctx context.Context, x postgres.SQLExecutor) error {
				_, err := x.Exec(ctx, `UPDATE agenteam_execution.executions SET request_digest=$2,version=2,updated_at=updated_at+interval '1 microsecond' WHERE id=$1`, seed.executionID.String(), "sha256:"+strings.Repeat("1", 64))
				return err
			}},
			{"created-not-running", "23514", "execution_transition", func(ctx context.Context, x postgres.SQLExecutor) error {
				_, err := x.Exec(ctx, `UPDATE agenteam_execution.executions SET status='running',version=2,snapshot_id=$1,started_at=updated_at+interval '1 microsecond',updated_at=updated_at+interval '1 microsecond' WHERE id=$1`, seed.executionID.String())
				return err
			}},
			{"cancel-wins", "23514", "execution_cancel_wins", func(ctx context.Context, x postgres.SQLExecutor) error {
				if _, err := x.Exec(ctx, `UPDATE agenteam_execution.executions SET status='preparing',version=2,cancel_requested_at=updated_at+interval '1 microsecond',updated_at=updated_at+interval '1 microsecond' WHERE id=$1`, seed.executionID.String()); err != nil {
					return err
				}
				_, err := x.Exec(ctx, `UPDATE agenteam_execution.executions SET status='running',version=3,snapshot_id=$1,started_at=updated_at+interval '1 microsecond',updated_at=updated_at+interval '1 microsecond' WHERE id=$1`, seed.executionID.String())
				return err
			}},
		} {
			runtimeSchemaProbe(t, v.raw, p.label, p.code, "", p.constraint, seed.execution, p.apply)
		}
		checkUnboundExecutionLaunch(t, v)
		if runtimeSchemaCounts(t, v.raw) != ([3]int64{}) {
			t.Fatal("Execution probe or missing dependency published identity")
		}
	})
	t.Run("human-compatibility-and-agent-origin", func(t *testing.T) {
		v := newSkillInstallationFixture(t)
		request, err := skill.NewInstallRequest(ctxFor(t), id[pc.Skill](t), skillInstallationPackage(t))
		if err != nil {
			t.Fatal("real Human input construction")
		}
		command := meta(t, id[struct{}](t).String(), nil)
		receipt, err := v.service.Install(ctxFor(t), v.base.ownerBrowser.actor, command, v.project.ID, request)
		if err != nil || receipt.Validate() != nil {
			t.Fatal("migration39 rejected real Human installation")
		}
		var human bool
		if err = v.base.raw.QueryRow(ctxFor(t), `SELECT actor_kind='human' AND actor_user_id=$2 AND actor_agent_id IS NULL AND actor_execution_id IS NULL AND tool_operation_id IS NULL FROM agenteam_skill.installations WHERE id=$1`, receipt.InstallationID.String(), v.base.ownerBrowser.actor.Details().UserID).Scan(&human); err != nil || !human {
			t.Fatal("Human origin changed under migration39")
		}
		checkSkillActorSchema(t, v, receipt)
		observed, err := v.service.LookupInstall(ctxFor(t), v.base.ownerBrowser.actor, v.project.ID, command.IdempotencyKey, request)
		if err != nil || observed != receipt {
			t.Fatal("rollback probes changed actual Human receipt")
		}
		var total int
		if err = v.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_skill.installations WHERE project_id=$1`, v.project.ID.String()).Scan(&total); err != nil || total != 1 || runtimeSchemaCounts(t, v.base.raw) != ([3]int64{}) {
			t.Fatal("actor probes left durable facts")
		}
	})
}

func assertRuntimeSchema39(t *testing.T, raw *postgres.Store) {
	t.Helper()
	var journal, goose, constraints, indexes int
	err := raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_meta.migration_journal WHERE version>0 AND state='applied'),
 (SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id>0 AND is_applied),
 (SELECT count(*) FROM pg_constraint WHERE conname IN ('tool_operations_execution_scope','tool_active_attempt_parent','installations_actor_origin','installation_attempt_actor_parent') AND convalidated),
 (SELECT count(*) FROM pg_indexes WHERE indexname IN ('executions_active_agent','tool_attempt_active','installations_tool_operation'))`).Scan(&journal, &goose, &constraints, &indexes)
	if err != nil || journal != 39 || goose != 39 || constraints != 4 || indexes != 3 {
		t.Fatal("continuous 39 prefix or exact new constraints missing")
	}
}

func runtimeSchemaCounts(t *testing.T, raw *postgres.Store) [3]int64 {
	t.Helper()
	var out [3]int64
	if err := raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_execution.executions),(SELECT count(*) FROM agenteam_tool.operations),(SELECT count(*) FROM agenteam_tool.attempts)`).Scan(&out[0], &out[1], &out[2]); err != nil {
		t.Fatal("runtime schema count failed")
	}
	return out
}

var runtimeSchemaRollback = errors.New("runtime schema probe rollback")

// SET CONSTRAINTS is deliberate: deferred parent failures must occur inside
// the observed original transaction, never be hidden by our final rollback.
func runtimeSchemaProbe(t *testing.T, raw *postgres.Store, label, code, table, constraint string, setup, apply func(context.Context, postgres.SQLExecutor) error) {
	t.Helper()
	reached, accepted := false, false
	result := raw.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx f.Tx) error {
		x, err := raw.InTx(tx)
		if err != nil {
			return err
		}
		if setup != nil {
			if err = setup(ctx, x); err != nil {
				return err
			}
		}
		reached = true
		if err = apply(ctx, x); err != nil {
			return err
		}
		if _, err = x.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
			return err
		}
		accepted = true
		return runtimeSchemaRollback
	})
	if !reached || result.State() != f.NotCommitted {
		t.Fatalf("probe %s did not reach its target and physically roll back", label)
	}
	if code == "" {
		if !accepted || !errors.Is(result.Fault(), runtimeSchemaRollback) {
			t.Fatalf("probe %s did not reach explicit rollback", label)
		}
		return
	}
	var pg *pgconn.PgError
	if accepted || !errors.As(result.Fault(), &pg) || pg.Code != code || table != "" && pg.TableName != table || constraint != "" && pg.ConstraintName != constraint {
		t.Fatalf("probe %s did not reject at its exact SQL constraint", label)
	}
}

type runtimeSchemaSeed struct {
	project                                                                      i.ProjectID
	agent                                                                        i.AgentID
	executionID                                                                  i.ExecutionID
	requestID                                                                    f.ID[f.Request]
	tool, operationID, attemptID, skillID, logical, invocation, process, backend string
}

func newRuntimeSchemaSeed(t *testing.T) runtimeSchemaSeed {
	t.Helper()
	return runtimeSchemaSeed{id[i.Project](t), id[i.Agent](t), id[i.Execution](t), id[f.Request](t), id[struct{}](t).String(), id[struct{}](t).String(), id[struct{}](t).String(), id[struct{}](t).String(), id[struct{}](t).String(), id[struct{}](t).String(), id[struct{}](t).String(), id[struct{}](t).String()}
}

func (s runtimeSchemaSeed) request() ec.LaunchRequest {
	return ec.LaunchRequest{ProjectID: s.project, AgentID: s.agent, Trigger: ec.Trigger{Kind: "task", TaskID: s.logical}, Purpose: "task/work", Policy: ec.Policy{SchemaVersion: 1, DeniedToolIDs: []i.ToolID{}, AllowedResourceConstraints: []json.RawMessage{}}, Meta: f.CommandMeta{RequestID: s.requestID, IdempotencyKey: f.IdempotencyKey(s.executionID.String())}}
}

func (s runtimeSchemaSeed) execution(ctx context.Context, x postgres.SQLExecutor) error {
	r := s.request()
	digest, err := r.Digest()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = x.Exec(ctx, `INSERT INTO agenteam_execution.executions(id,project_id,agent_id,status,version,idempotency_key,request_id,request_digest,trigger_reference_digest,launch_request,initiator,created_at,updated_at) VALUES($1,$2,$3,'created',1,$4,$5,$6,$6,$7::jsonb,'{}'::jsonb,transaction_timestamp(),transaction_timestamp())`, s.executionID.String(), s.project.String(), s.agent.String(), r.Meta.IdempotencyKey.String(), s.requestID.String(), string(digest), string(raw))
	return err
}

func (s runtimeSchemaSeed) operation(ctx context.Context, x postgres.SQLExecutor) error {
	if err := s.execution(ctx, x); err != nil {
		return err
	}
	if _, err := x.Exec(ctx, `INSERT INTO agenteam_tool.identities(tool_id,stable_key,latest_revision) VALUES($1,$2,1)`, s.tool, "builtin:schema-"+s.tool); err != nil {
		return err
	}
	if _, err := x.Exec(ctx, `INSERT INTO agenteam_tool.spec_revisions(tool_id,spec_revision,definition) VALUES($1,1,$2)`, s.tool, []byte(`{}`)); err != nil {
		return err
	}
	_, err := x.Exec(ctx, `INSERT INTO agenteam_tool.operations(operation_id,project_id,agent_id,execution_id,logical_call_id,invocation_id,call_id,tool_id,spec_revision,input_data,input_digest,skill_id,backend_key,fingerprint,phase,version) VALUES($1,$2,$3,$4,$5,$6,'schema-call',$7,1,$8,$9,$10,$11,$9,'created',1)`, s.operationID, s.project.String(), s.agent.String(), s.executionID.String(), s.logical, s.invocation, s.tool, []byte(`{}`), "sha256:"+strings.Repeat("0", 64), s.skillID, "tool.skill.install:"+s.operationID)
	return err
}

func (s runtimeSchemaSeed) attempt(ctx context.Context, x postgres.SQLExecutor, attempt string, ordinal int, agentID string) error {
	_, err := x.Exec(ctx, `INSERT INTO agenteam_tool.attempts(attempt_id,operation_id,execution_id,project_id,agent_id,ordinal,process_id,fence,backend_request_id,phase) VALUES($1,$2,$3,$4,$5,$6,$7,1,$8,'prepared')`, attempt, s.operationID, s.executionID.String(), s.project.String(), agentID, ordinal, s.process, s.backend)
	return err
}

func runtimeSchemaFault(t *testing.T, err error, code f.Code) {
	t.Helper()
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != code {
		t.Fatal("unexpected safe domain refusal")
	}
}

func checkUnboundExecutionLaunch(t *testing.T, v *variableHTTPFixture) {
	t.Helper()
	authority, err := execution.NewAuthority(v.tracked, v.projectAuthority, nil)
	if err != nil {
		t.Fatal("Execution authority assembly")
	}
	agents, err := agent.NewAuthority(v.tracked, v.projectAuthority)
	if err != nil {
		t.Fatal("Agent authority assembly")
	}
	configuration, err := agent.NewExecutionConfiguration(agents, authority)
	if err != nil {
		t.Fatal("same-Store execution configuration assembly")
	}
	service, err := execution.New(v.tracked, execution.Dependencies{Authority: authority, Agents: configuration})
	if err != nil {
		t.Fatal("Execution service assembly")
	}
	t.Cleanup(func() {
		service.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := service.Drain(ctx); err != nil || !service.Joined() {
			t.Error("original Execution calls not joined")
		}
	})
	seed := newRuntimeSchemaSeed(t)
	seed.project = v.project.ID
	request := seed.request()
	result, err := service.Launch(ctxFor(t), v.ownerBrowser.actor, request)
	runtimeSchemaFault(t, err, f.DependencyUnbound)
	if result.Execution.ID != (i.ExecutionID{}) || result.Replayed {
		t.Fatal("missing Trigger returned usable identity")
	}
	digest, err := request.Digest()
	if err != nil {
		t.Fatal("launch request digest")
	}
	lookup, err := service.LookupLaunch(ctxFor(t), v.ownerBrowser.actor, ec.LaunchLookupKey{ProjectID: request.ProjectID, AgentID: request.AgentID, IdempotencyKey: request.Meta.IdempotencyKey}, digest)
	if err != nil || lookup.Found || lookup.Execution != nil {
		t.Fatal("unbound Launch created a lookup fact")
	}
	_, err = service.Launch(ctxFor(t), v.otherBrowser.actor, request)
	runtimeSchemaFault(t, err, f.NotFound)
}

func checkSkillActorSchema(t *testing.T, v *skillInstallationFixture, receipt skill.InstallReceipt) {
	t.Helper()
	seed := newRuntimeSchemaSeed(t)
	installation, revision := id[struct{}](t).String(), id[struct{}](t).String()
	insert := func(ctx context.Context, x postgres.SQLExecutor, kind string, user any) error {
		tag, err := x.Exec(ctx, `INSERT INTO agenteam_skill.installations(id,project_id,actor_user_id,command_key,skill_id,revision_id,semantic_digest,package_sha256,manifest_sha256,byte_size,manifest,name,normalized_name,description,phase,version,created_at,updated_at,actor_kind,actor_agent_id,actor_execution_id,tool_operation_id,tool_id,tool_spec_revision,operation_fingerprint,first_tool_attempt_id,first_backend_request_id)
 SELECT $1,project_id,$2,$3,$4,$5,semantic_digest,package_sha256,manifest_sha256,byte_size,manifest,$6,$6,description,'planned',1,transaction_timestamp(),transaction_timestamp(),$7,$8,$9,$10,$11,1,$12,$13,$14 FROM agenteam_skill.installations WHERE id=$15`, installation, user, "tool.skill.install:"+seed.operationID, seed.skillID, revision, "schema-"+seed.skillID, kind, seed.agent.String(), seed.executionID.String(), seed.operationID, seed.tool, "sha256:"+strings.Repeat("0", 64), seed.attemptID, seed.backend, receipt.InstallationID.String())
		if err == nil && tag.RowsAffected() != 1 {
			return errors.New("actor probe template missing")
		}
		return err
	}
	valid := func(ctx context.Context, x postgres.SQLExecutor) error { return insert(ctx, x, "agent_run", nil) }
	runtimeSchemaProbe(t, v.base.raw, "complete-agent-origin", "", "", "", nil, valid)
	for _, p := range []struct {
		label, kind string
		user        any
	}{
		{"agent-cannot-have-user", "agent_run", v.base.ownerBrowser.actor.Details().UserID},
		{"human-cannot-have-agent", "human", v.base.ownerBrowser.actor.Details().UserID},
	} {
		runtimeSchemaProbe(t, v.base.raw, p.label, "23514", "installations", "installations_actor_origin", nil, func(ctx context.Context, x postgres.SQLExecutor) error { return insert(ctx, x, p.kind, p.user) })
	}
	runtimeSchemaProbe(t, v.base.raw, "origin-immutable", "23514", "", "installations_immutable", valid, func(ctx context.Context, x postgres.SQLExecutor) error {
		_, err := x.Exec(ctx, `UPDATE agenteam_skill.installations SET operation_fingerprint=$2,version=2,updated_at=clock_timestamp() WHERE id=$1`, installation, "sha256:"+strings.Repeat("1", 64))
		return err
	})
	for _, kind := range []string{"agent_run", "human"} {
		code, constraint := "", ""
		if kind == "human" {
			code, constraint = "23503", "installation_attempt_actor_parent"
		}
		runtimeSchemaProbe(t, v.base.raw, "attempt-parent-"+kind, code, "", constraint, valid, func(ctx context.Context, x postgres.SQLExecutor) error {
			object, upload, attempt := id[struct{}](t).String(), id[struct{}](t).String(), id[struct{}](t).String()
			if _, err := x.Exec(ctx, `UPDATE agenteam_skill.installations SET phase='reserved',version=2,object_id=$2,upload_id=$3,current_attempt_id=$4,updated_at=clock_timestamp() WHERE id=$1`, installation, object, upload, attempt); err != nil {
				return err
			}
			var toolAttempt, backend any
			if kind == "agent_run" {
				toolAttempt, backend = seed.attemptID, seed.backend
			}
			_, err := x.Exec(ctx, `INSERT INTO agenteam_skill.installation_attempts(attempt_id,project_id,installation_id,skill_id,revision_id,object_id,upload_id,process_id,created_at,actor_kind,tool_attempt_id,backend_request_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,clock_timestamp(),$9,$10,$11)`, attempt, v.project.ID.String(), installation, seed.skillID, revision, object, upload, seed.process, kind, toolAttempt, backend)
			return err
		})
	}
}
