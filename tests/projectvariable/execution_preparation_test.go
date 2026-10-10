//go:build integration

package projectvariable_test

import (
	"context"
	"errors"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// Preparation prerequisites are deliberately separate from successful
// Execution capture. Project setup uses the existing, disclosed persistent
// test Skills initializer; it does not bind production initialization. No SQL
// candidate from a constraint probe is ever passed to an Execution service.
func TestExecutionPreparation(t *testing.T) {
	t.Run("prefix39-upgrade-and-repeat", func(t *testing.T) {
		for _, upgrade := range []bool{false, true} {
			db := pgfixture.NewDatabase(t)
			var old *variableHTTPFixture
			var before string
			if upgrade {
				migrate(t, db, migrationPrefix(t, "00039"))
				raw := openStore(t, db.Config(t, nil))
				old = assembleVariableHTTPFixture(t, db, raw, &hookStore{fixtureStore: raw})
				before = preparationProjectRow(t, raw, old.project.ID)
			}
			migrate(t, db, migrationPrefix(t, "00040"))
			raw := openStore(t, db.Config(t, nil))
			assertPreparationSchema40(t, raw)
			migrate(t, db, migrationPrefix(t, "00040"))
			assertPreparationSchema40(t, raw)
			if old != nil && before != preparationProjectRow(t, raw, old.project.ID) {
				t.Fatal("migration40 rewrote the actual initialized Project")
			}
			assertNoPreparationFacts(t, raw)
		}
	})
	t.Run("preparation-claim-and-attempt", func(t *testing.T) {
		raw := openStore(t, newDatabase(t).Config(t, nil))
		seed := newRuntimeSchemaSeed(t)
		nextAttempt := id[struct{}](t).String()
		setup := func(ctx context.Context, x postgres.SQLExecutor) error {
			if err := seed.execution(ctx, x); err != nil {
				return err
			}
			if _, err := x.Exec(ctx, `UPDATE agenteam_execution.executions SET status='preparing',version=2,updated_at=updated_at+interval '1 microsecond' WHERE id=$1`, seed.executionID.String()); err != nil {
				return err
			}
			if err := insertPreparationAttempt(ctx, x, seed, seed.attemptID, seed.agent.String(), 1); err != nil {
				return err
			}
			_, err := x.Exec(ctx, `INSERT INTO agenteam_execution.preparation_claims(execution_id,project_id,agent_id,attempt_id,process_id,fence) VALUES($1,$2,$3,$4,$5,1)`, seed.executionID.String(), seed.project.String(), seed.agent.String(), seed.attemptID, seed.process)
			return err
		}
		// These rows exist only inside an explicitly rolled-back constraint
		// probe. Terminal is a retired attempt, never successful capture.
		runtimeSchemaProbe(t, raw, "retired-attempt-and-next-fence", "", "", "", setup, func(ctx context.Context, x postgres.SQLExecutor) error {
			if _, err := x.Exec(ctx, `UPDATE agenteam_execution.preparation_attempts SET phase='terminal',returned_at=started_at+interval '1 microsecond' WHERE execution_id=$1 AND attempt_id=$2`, seed.executionID.String(), seed.attemptID); err != nil {
				return err
			}
			if err := insertPreparationAttempt(ctx, x, seed, nextAttempt, seed.agent.String(), 2); err != nil {
				return err
			}
			if _, err := x.Exec(ctx, `UPDATE agenteam_execution.preparation_claims SET attempt_id=$2,fence=2 WHERE execution_id=$1`, seed.executionID.String(), nextAttempt); err != nil {
				return err
			}
			var exact bool
			if err := x.QueryRow(ctx, `SELECT e.status='preparing' AND e.snapshot_id IS NULL AND c.attempt_id=$2 AND c.fence=2
 AND (SELECT count(*) FROM agenteam_execution.preparation_attempts a WHERE a.execution_id=e.id)=2
 AND (SELECT count(*) FROM agenteam_execution.preparation_attempts a WHERE a.execution_id=e.id AND a.phase='terminal' AND a.returned_at IS NOT NULL)=1
 FROM agenteam_execution.executions e JOIN agenteam_execution.preparation_claims c ON c.execution_id=e.id WHERE e.id=$1`, seed.executionID.String(), nextAttempt).Scan(&exact); err != nil {
				return err
			}
			if !exact {
				return errors.New("attempt retirement changed capture state or lost its exact claim")
			}
			return nil
		})
		for _, probe := range []struct {
			label, code, table, constraint string
			apply                          func(context.Context, postgres.SQLExecutor) error
		}{
			{"attempt-scope", "23503", "preparation_attempts", "", func(ctx context.Context, x postgres.SQLExecutor) error {
				return insertPreparationAttempt(ctx, x, seed, nextAttempt, id[i.Agent](t).String(), 2)
			}},
			{"unique-execution-fence", "23505", "preparation_attempts", "", func(ctx context.Context, x postgres.SQLExecutor) error {
				return insertPreparationAttempt(ctx, x, seed, nextAttempt, seed.agent.String(), 1)
			}},
			{"claim-exact-attempt-process", "23503", "preparation_claims", "", func(ctx context.Context, x postgres.SQLExecutor) error {
				if err := insertPreparationAttempt(ctx, x, seed, nextAttempt, seed.agent.String(), 2); err != nil {
					return err
				}
				_, err := x.Exec(ctx, `UPDATE agenteam_execution.preparation_claims SET attempt_id=$2,process_id=$3,fence=2 WHERE execution_id=$1`, seed.executionID.String(), nextAttempt, id[struct{}](t).String())
				return err
			}},
			{"claim-fence-cannot-skip", "23514", "", "preparation_claim_fence", func(ctx context.Context, x postgres.SQLExecutor) error {
				if err := insertPreparationAttempt(ctx, x, seed, nextAttempt, seed.agent.String(), 3); err != nil {
					return err
				}
				_, err := x.Exec(ctx, `UPDATE agenteam_execution.preparation_claims SET attempt_id=$2,fence=3 WHERE execution_id=$1`, seed.executionID.String(), nextAttempt)
				return err
			}},
			{"attempt-identity-immutable", "23514", "", "preparation_attempt_immutable", func(ctx context.Context, x postgres.SQLExecutor) error {
				_, err := x.Exec(ctx, `UPDATE agenteam_execution.preparation_attempts SET process_id=$2,phase='terminal',returned_at=started_at+interval '1 microsecond' WHERE execution_id=$1`, seed.executionID.String(), id[struct{}](t).String())
				return err
			}},
			{"terminal-needs-original-return", "23514", "", "preparation_attempt_immutable", func(ctx context.Context, x postgres.SQLExecutor) error {
				_, err := x.Exec(ctx, `UPDATE agenteam_execution.preparation_attempts SET phase='terminal' WHERE execution_id=$1`, seed.executionID.String())
				return err
			}},
		} {
			runtimeSchemaProbe(t, raw, probe.label, probe.code, probe.table, probe.constraint, setup, probe.apply)
		}
		assertNoPreparationFacts(t, raw)
	})
	t.Run("project-preparation-gate", func(t *testing.T) {
		v := newVariableHTTPFixture(t)
		// This port checks current Project facts, not an Actor or a private
		// Execution claim. Only the real Execution owner can supply the latter.
		ref := preparationProjectGate(t, v, v.project.ID, true, "")
		if ref.ID != v.project.ID || ref.Version != v.project.Version || ref.Lifecycle != pc.Active {
			t.Fatal("preparation did not return the actual current Project")
		}
		preparationProjectGate(t, v, v.project.ID, false, f.DependencyUnavailable)
		preparationProjectGate(t, v, id[i.Project](t), true, f.NotFound)

		v.skills.setMode("pending")
		pending := id[i.Project](t)
		result, err := v.projects.CreateProject(ctxFor(t), v.ownerBrowser.actor, meta(t, "preparation-pending", nil), pc.CreateProjectRequest{ProjectID: pending, Name: "preparation-pending"})
		v.skills.setMode("")
		if err != nil || result.State == pc.CreationReady {
			t.Fatal("formal Project creation did not remain initialization-pending")
		}
		preparationProjectGate(t, v, pending, true, f.ProjectNotActive)

		// BeginArchive is real; the existing fixture's unavailable participants
		// forbid pretending the archive/cleanup worker has completed.
		if _, err = v.projects.BeginArchive(ctxFor(t), v.ownerBrowser.actor, meta(t, "preparation-archive", &v.project.Version), v.project.ID); err != nil {
			t.Fatal("formal BeginArchive did not accept its original manifest")
		}
		preparationProjectGate(t, v, v.project.ID, true, f.ProjectNotActive)
		assertNoPreparationFacts(t, v.raw)
	})
	t.Run("current-owner-task-input", func(t *testing.T) {
		v := newVariableHTTPFixture(t)
		reader, task := newPreparationTaskInput(t, v)
		input, err := reader.ReadTaskInput(ctxFor(t), v.ownerBrowser.actor, v.project.ID, task.ID, "task/work")
		if err != nil || input.Validate() != nil || input.Purpose() != "task/work" || string(jsonBytes(t, input.Task())) != string(jsonBytes(t, task)) || input.Sprint().ID != task.SprintID || input.Milestone().ID != task.MilestoneID || input.Sprint().State != wc.Planned || task.State != wc.TaskStateBacklog || len(input.UnresolvedBlockers()) != 0 || len(input.RecentTaskEvents()) != 1 {
			t.Fatal("current Owner did not read the complete formally created Task input")
		}
		if raw, err := input.Data(); err != nil || len(raw) == 0 || len(raw) > ec.MaxTriggerInputBytes {
			t.Fatal("Task input did not preserve its bounded complete representation")
		}
		for _, actor := range []i.Actor{v.otherBrowser.actor, v.adminBrowser.actor} {
			denied, err := reader.ReadTaskInput(ctxFor(t), actor, v.project.ID, task.ID, "task/work")
			runtimeSchemaFault(t, err, f.NotFound)
			if denied.Validate() == nil {
				t.Fatal("non-Owner received usable Task input")
			}
		}
		v.revoke(t, v.ownerBrowser)
		denied, err := reader.ReadTaskInput(ctxFor(t), v.ownerBrowser.actor, v.project.ID, task.ID, "task/work")
		runtimeSchemaFault(t, err, f.SessionRevoked)
		if denied.Validate() == nil {
			t.Fatal("revoked original Session received usable Task input")
		}
		// The ordinary read is useful without Execution authority. The same
		// provider still refuses capture discovery, even for the observed Task.
		seed := newRuntimeSchemaSeed(t)
		request := seed.request()
		request.ProjectID, request.Trigger.TaskID = v.project.ID, task.ID.String()
		plan, err := reader.DiscoverCapture(ctxFor(t), seed.executionID, request)
		runtimeSchemaFault(t, err, f.DependencyUnbound)
		if plan != nil {
			t.Fatal("ordinary Owner observation manufactured a capture plan")
		}
		assertNoPreparationFacts(t, v.raw)
	})
}

func insertPreparationAttempt(ctx context.Context, x postgres.SQLExecutor, seed runtimeSchemaSeed, attempt, agent string, fence int64) error {
	_, err := x.Exec(ctx, `INSERT INTO agenteam_execution.preparation_attempts(execution_id,project_id,agent_id,attempt_id,process_id,fence,phase,started_at) VALUES($1,$2,$3,$4,$5,$6,'running',transaction_timestamp())`, seed.executionID.String(), seed.project.String(), agent, attempt, seed.process, fence)
	return err
}

func newPreparationTaskInput(t *testing.T, v *variableHTTPFixture) (*work.TaskTrigger, wc.Task) {
	t.Helper()
	authority, err := work.NewAuthority(v.tracked, v.projectAuthority)
	if err != nil {
		t.Fatal("same-Store Work authority assembly")
	}
	catalog := event.NewCatalog()
	workEvents, err := wc.RegisterWorkEvents(catalog)
	if err != nil {
		t.Fatal("formal Work event types")
	}
	taskEvents, err := wc.RegisterTaskEvents(catalog)
	if err != nil {
		t.Fatal("formal Task event types")
	}
	box, err := outbox.New(v.tracked, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{wc.WorkProducer: authority}, Sessions: v.accounts, System: v.accounts, Projects: v.projectAuthority, Audit: v.audit, Cursors: v.keys, Processes: fixtureProcess{id[oc.Process](t)}})
	if err != nil {
		t.Fatal("same-Store formal Work producer assembly")
	}
	structure, err := work.New(v.tracked, work.Dependencies{Authority: authority, Events: box, WorkEvents: workEvents, Activity: v.accounts})
	if err != nil {
		t.Fatal("formal Work service assembly")
	}
	t.Cleanup(func() {
		structure.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := structure.Drain(ctx); err != nil {
			t.Error("original Work calls did not join")
		}
	})
	structureReader, err := work.NewReader(v.tracked, authority, v.keys)
	if err != nil {
		t.Fatal("same-Store structure reader assembly")
	}
	tasks, err := work.NewTask(v.tracked, work.TaskDependencies{Structure: structureReader, Authority: authority, Events: box, TaskEvents: taskEvents, Activity: v.accounts})
	if err != nil {
		t.Fatal("same-Store formal Task service assembly")
	}
	t.Cleanup(func() {
		tasks.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := tasks.Drain(ctx); err != nil {
			t.Error("original Task calls did not join")
		}
	})
	milestone := wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "Preparation source", Description: "Created by the current Owner"}
	if _, err := structure.CreateMilestone(ctxFor(t), v.ownerBrowser.actor, meta(t, "preparation-milestone", nil), v.project.ID, milestone); err != nil {
		t.Fatal("formal milestone creation")
	}
	sprint := wc.CreateSprintRequest{SprintID: id[pc.Sprint](t), MilestoneID: milestone.MilestoneID, Title: "Preparation sprint", Description: "A planned source is readable, not launchable"}
	if _, err := structure.CreateSprint(ctxFor(t), v.ownerBrowser.actor, meta(t, "preparation-sprint", nil), v.project.ID, sprint); err != nil {
		t.Fatal("formal sprint creation")
	}
	created, err := tasks.CreateTask(ctxFor(t), v.ownerBrowser.actor, meta(t, "preparation-task", nil), v.project.ID, wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: sprint.SprintID, Title: "Read the current input", Description: "Complete source text", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium, Plan: "Observe without publishing an Execution"})
	if err != nil || !created.Changed || created.TaskEventID == nil || len(created.EventIDs) != 1 {
		t.Fatal("formal Task creation did not produce its original event")
	}
	trigger, err := work.NewTaskTrigger(v.tracked, authority, nil)
	if err != nil {
		t.Fatal("same-Store Task input reader assembly")
	}
	return trigger, created.Task
}

func preparationProjectRow(t *testing.T, raw *postgres.Store, project i.ProjectID) string {
	t.Helper()
	var row string
	if err := raw.QueryRow(ctxFor(t), `SELECT to_jsonb(p)::text FROM agenteam_project.projects p WHERE id=$1`, project.String()).Scan(&row); err != nil {
		t.Fatal("actual Project row unavailable")
	}
	return row
}

func assertPreparationSchema40(t *testing.T, raw *postgres.Store) {
	t.Helper()
	var journal, goose, tables, indexes, triggers int
	err := raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_meta.migration_journal WHERE version>0 AND state='applied'),
 (SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id>0 AND is_applied),
 (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='agenteam_execution' AND c.relkind='r' AND c.relname IN ('preparation_attempts','preparation_claims')),
 (SELECT count(*) FROM pg_indexes WHERE schemaname='agenteam_execution' AND indexname IN ('preparation_attempts_project','preparation_claims_project')),
 (SELECT count(*) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='agenteam_execution' AND NOT t.tgisinternal AND t.tgenabled='O' AND t.tgname IN ('preparation_attempt_immutable','preparation_claim_fence'))`).Scan(&journal, &goose, &tables, &indexes, &triggers)
	if err != nil || journal != 40 || goose != 40 || tables != 2 || indexes != 2 || triggers != 2 {
		t.Fatal("continuous migration40 or actual preparation schema missing")
	}
}

func assertNoPreparationFacts(t *testing.T, raw *postgres.Store) {
	t.Helper()
	var attempts, claims int64
	if err := raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_execution.preparation_attempts),(SELECT count(*) FROM agenteam_execution.preparation_claims)`).Scan(&attempts, &claims); err != nil || attempts != 0 || claims != 0 || runtimeSchemaCounts(t, raw) != ([3]int64{}) {
		t.Fatal("prerequisite observation manufactured Execution or preparation facts")
	}
}

func preparationProjectGate(t *testing.T, v *variableHTTPFixture, project i.ProjectID, held bool, code f.Code) pc.ProjectRef {
	t.Helper()
	var ref pc.ProjectRef
	var observed error
	reached := false
	result := v.tracked.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx f.Tx) error {
		if held {
			key, err := f.ProjectLock(project.String())
			if err != nil {
				return err
			}
			if err = v.tracked.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Shared}}); err != nil {
				return err
			}
		}
		reached = true
		ref, observed = v.projectAuthority.RequirePreparingProjectInTx(ctx, tx, project)
		return observed
	})
	if !reached {
		t.Fatal("preparation gate did not reach its original transaction")
	}
	if code == "" {
		if observed != nil || result.State() != f.Committed {
			t.Fatal("original preparation Project read did not commit")
		}
	} else {
		runtimeSchemaFault(t, observed, code)
		if ref.ID.Validate() == nil || result.State() != f.NotCommitted {
			t.Fatal("rejected preparation leaked a Project or committed")
		}
	}
	return ref
}
