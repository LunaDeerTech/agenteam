//go:build integration

package database_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

const processSQL = "CREATE TABLE public.guard_once(value integer NOT NULL); INSERT INTO public.guard_once VALUES(1); SELECT pg_advisory_xact_lock(99123);"

func TestMigrationProcessFixture(t *testing.T) {
	database := os.Getenv("AGENTEAM_MIGRATION_DATABASE")
	if database == "" {
		return
	}
	fixture, err := pgfixture.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := fixture.Config(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := fixtureSource(t, processSQL, postgres.Transactional, nil)
	m, err := postgres.NewMigrator(cfg, source)
	if err != nil {
		t.Fatal(err)
	}
	state := m.Migrate(testContext(t))
	if !state.Migrated {
		t.Fatalf("fixture migration: %v", state.Fault)
	}
	fmt.Println("fixture migrated")
}

type migrationProcess struct {
	cmd      *exec.Cmd
	done     chan error
	finished chan struct{}
}

func startMigrationProcess(t *testing.T, db *pgfixture.Database) *migrationProcess {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestMigrationProcessFixture$", "-test.timeout=25s")
	cmd.Env = []string{pgfixture.Env + "=" + os.Getenv(pgfixture.Env), "AGENTEAM_MIGRATION_DATABASE=" + db.Name, "PATH=" + os.Getenv("PATH")}
	p := &migrationProcess{cmd: cmd, done: make(chan error, 1), finished: make(chan struct{})}
	if err := cmd.Start(); err != nil {
		t.Fatal("fixture child start failed")
	}
	go func() { p.done <- cmd.Wait(); close(p.finished) }()
	t.Cleanup(func() {
		select {
		case <-p.finished:
		default:
			_ = cmd.Process.Kill()
			select {
			case <-p.finished:
			case <-time.After(3 * time.Second):
				t.Error("fixture child remains")
			}
		}
	})
	return p
}
func TestTwoMigrationProcessesGuardCancellationAndOwnerDeath(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	admin := db.Connect(t)
	if _, err := admin.Exec(testContext(t), "SELECT pg_advisory_lock(99123)"); err != nil {
		t.Fatal("fixture barrier failed")
	}
	first := startMigrationProcess(t, db)
	const executing = `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a JOIN pg_locks l ON l.pid=a.pid WHERE a.datname=current_database() AND a.query LIKE '%pg_advisory_xact_lock(99123)%' AND a.wait_event_type='Lock' AND l.locktype='advisory' AND l.classid=1095193677 AND l.objid=1 AND l.objsubid=2 AND l.granted)`
	waitDatabase(t, admin, executing)
	second := startMigrationProcess(t, db)
	waitDatabase(t, admin, `SELECT count(*) FILTER(WHERE granted)=1 AND count(*) FILTER(WHERE NOT granted)>=1 FROM pg_locks WHERE locktype='advisory' AND classid=1095193677 AND objid=1 AND objsubid=2 AND database=(SELECT oid FROM pg_database WHERE datname=current_database())`)
	source := fixtureSource(t, processSQL, postgres.Transactional, nil)
	m, _ := postgres.NewMigrator(cfg, source)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	state := m.Migrate(ctx)
	cancel()
	if state.Migrated || state.Fault == nil || state.Fault.Code() != postgres.MigrationGuardFailed {
		t.Fatalf("guard wait did not cancel: %v", state.Fault)
	}
	if err := first.cmd.Process.Kill(); err != nil {
		t.Fatal("fixture child kill failed")
	}
	if err := receive(t, first.done); err == nil {
		t.Fatal("killed migration child succeeded")
	}
	waitDatabase(t, admin, executing)
	if _, err := admin.Exec(testContext(t), "SELECT pg_advisory_unlock(99123)"); err != nil {
		t.Fatal("fixture barrier release failed")
	}
	if err := receive(t, second.done); err != nil {
		t.Fatal("surviving migration child failed")
	}
	var count, versions int
	if err := admin.QueryRow(testContext(t), "SELECT (SELECT count(*) FROM guard_once),(SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=2)").Scan(&count, &versions); err != nil || count != 1 || versions != 1 {
		t.Fatal("migration SQL or version repeated")
	}
}
func TestNonTransactionalInterruptionAndInterruptedRepair(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	migrate(t, cfg)
	admin := db.Connect(t)
	if _, err := admin.Exec(testContext(t), "CREATE TABLE fixture_index_source(value integer NOT NULL); INSERT INTO fixture_index_source SELECT generate_series(1,100)"); err != nil {
		t.Fatal("fixture source table failed")
	}
	plan := &postgres.RecoveryPlan{RestoreSteps: []string{"DROP INDEX CONCURRENTLY IF EXISTS public.fixture_repair_index"}, VerifyRestored: "SELECT to_regclass('public.fixture_repair_index') IS NULL"}
	source := fixtureSource(t, "CREATE INDEX CONCURRENTLY fixture_repair_index ON public.fixture_index_source(value);", postgres.NonTransactional, plan)
	m, _ := postgres.NewMigrator(cfg, source)
	blocker := db.Connect(t)
	tx, err := blocker.Begin(testContext(t))
	if err != nil {
		t.Fatal("fixture begin failed")
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(testContext(t), "INSERT INTO fixture_index_source VALUES(101)"); err != nil {
		t.Fatal("fixture writer barrier failed")
	}
	result := make(chan postgres.MigrationState, 1)
	go func() { result <- m.Migrate(testContext(t)) }()
	waitDatabase(t, admin, `SELECT EXISTS(SELECT 1 FROM pg_index WHERE indexrelid=to_regclass('public.fixture_repair_index') AND NOT indisvalid) AND EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND query LIKE 'CREATE INDEX CONCURRENTLY fixture_repair_index%' AND wait_event_type='Lock')`)
	var pid int32
	if err := admin.QueryRow(testContext(t), `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND query LIKE 'CREATE INDEX CONCURRENTLY fixture_repair_index%' AND wait_event_type='Lock'`).Scan(&pid); err != nil {
		t.Fatal("migration PID missing")
	}
	if err := db.Terminate(testContext(t), pid); err != nil {
		t.Fatal(err)
	}
	if state := receive(t, result); state.Migrated {
		t.Fatal("interrupted non-tx succeeded")
	}
	if err := tx.Rollback(testContext(t)); err != nil {
		t.Fatal("fixture writer release failed")
	}
	state := m.Migrate(testContext(t))
	if state.Migrated || state.Fault.Code() != postgres.MigrationRepairRequired {
		t.Fatalf("partial index rerun allowed: %v", state.Fault)
	}
	var journal string
	if err := admin.QueryRow(testContext(t), "SELECT state FROM agenteam_meta.migration_journal WHERE version=2").Scan(&journal); err != nil || journal != "needs_repair" {
		t.Fatal("needs_repair not persistent")
	}
	checksum := source.Manifest()[1].Checksum
	if repair := m.Repair(testContext(t), 2, foundation.Digest("sha256:0000000000000000000000000000000000000000000000000000000000000000")); repair.RepairedToPending {
		t.Fatal("wrong checksum repair allowed")
	}
	tx, err = blocker.Begin(testContext(t))
	if err != nil {
		t.Fatal("repair blocker begin failed")
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(testContext(t), "LOCK TABLE fixture_index_source IN SHARE UPDATE EXCLUSIVE MODE"); err != nil {
		t.Fatal("repair barrier failed")
	}
	repairs := make(chan postgres.RepairResult, 1)
	go func() { repairs <- m.Repair(testContext(t), 2, checksum) }()
	waitDatabase(t, admin, `SELECT EXISTS(SELECT 1 FROM agenteam_meta.migration_journal WHERE version=2 AND state='repairing') AND EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND query LIKE 'DROP INDEX CONCURRENTLY IF EXISTS public.fixture_repair_index%' AND wait_event_type='Lock')`)
	if err := admin.QueryRow(testContext(t), `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND query LIKE 'DROP INDEX CONCURRENTLY IF EXISTS public.fixture_repair_index%' AND wait_event_type='Lock'`).Scan(&pid); err != nil {
		t.Fatal("repair PID missing")
	}
	if err := db.Terminate(testContext(t), pid); err != nil {
		t.Fatal(err)
	}
	if repair := receive(t, repairs); repair.RepairedToPending {
		t.Fatal("interrupted repair succeeded")
	}
	if err := tx.Rollback(testContext(t)); err != nil {
		t.Fatal("repair blocker release failed")
	}
	if err := admin.QueryRow(testContext(t), "SELECT state FROM agenteam_meta.migration_journal WHERE version=2").Scan(&journal); err != nil || journal != "repairing" {
		t.Fatal("interrupted repair marker lost")
	}
	if state := m.Migrate(testContext(t)); state.Migrated || state.Fault.Code() != postgres.MigrationRepairRequired {
		t.Fatal("interrupted repair auto-ran")
	}
	if repair := m.Repair(testContext(t), 2, checksum); !repair.RepairedToPending {
		t.Fatalf("registered repair failed: %v", repair.Fault)
	}
	if err := admin.QueryRow(testContext(t), "SELECT state FROM agenteam_meta.migration_journal WHERE version=2").Scan(&journal); err != nil || journal != "pending" {
		t.Fatal("repair did not restore pending")
	}
	if state := m.Migrate(testContext(t)); !state.Migrated {
		t.Fatalf("rerun after repair failed: %v", state.Fault)
	}
	var valid bool
	var versions int
	if err := admin.QueryRow(testContext(t), "SELECT (SELECT indisvalid FROM pg_index WHERE indexrelid=to_regclass('public.fixture_repair_index')),(SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=2)").Scan(&valid, &versions); err != nil || !valid || versions != 1 {
		t.Fatal("repaired index or Goose fact invalid")
	}
	production, _ := postgres.NewMigrator(cfg)
	if repair := production.Repair(testContext(t), 1, source.Manifest()[0].Checksum); repair.RepairedToPending || repair.Fault.Code() != postgres.MigrationRepairUnsupported {
		t.Fatal("production tx repair did not reject")
	}
}

func TestRepairVerifierRequiresOneTrueBooleanAndReadOnlyExecution(t *testing.T) {
	for _, verifier := range []struct{ name, sql string }{
		{"false", "SELECT false"},
		{"text", "SELECT 'true'::text"},
		{"null", "SELECT NULL::boolean"},
		{"zero_rows", "SELECT true WHERE false"},
		{"two_rows", "SELECT true UNION ALL SELECT true"},
		{"two_columns", "SELECT true,true"},
		{"write", "WITH inserted AS (INSERT INTO repair_write VALUES(1) RETURNING id) SELECT true FROM inserted"},
	} {
		t.Run(verifier.name, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			cfg := db.Config(t, nil)
			migrate(t, cfg)
			admin := db.Connect(t)
			if _, err := admin.Exec(testContext(t), "CREATE TABLE repair_write(id integer)"); err != nil {
				t.Fatal("verifier fixture table failed")
			}
			plan := &postgres.RecoveryPlan{RestoreSteps: []string{"DROP TABLE IF EXISTS repair_partial"}, VerifyRestored: verifier.sql}
			source := fixtureSource(t, "CREATE TABLE repair_partial(id integer); SELECT private_repair_failure();", postgres.NonTransactional, plan)
			m, _ := postgres.NewMigrator(cfg, source)
			if state := m.Migrate(testContext(t)); state.Migrated || state.Fault.Code() != postgres.MigrationRepairRequired {
				t.Fatal("partial migration not recorded")
			}
			if result := m.Repair(testContext(t), 2, source.Manifest()[1].Checksum); result.RepairedToPending || result.Fault.Code() != postgres.MigrationRepairFailed {
				t.Fatal("invalid verifier accepted")
			}
			var state string
			var writes int
			if err := admin.QueryRow(testContext(t), "SELECT (SELECT state FROM agenteam_meta.migration_journal WHERE version=2),(SELECT count(*) FROM repair_write)").Scan(&state, &writes); err != nil || state != "repairing" || writes != 0 {
				t.Fatal("failed verifier cleared recovery state or wrote data")
			}
		})
	}
}

func TestAppliedGooseFactConvergesInterruptedJournal(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	source := fixtureSource(t, "CREATE TABLE converge_once(id integer); INSERT INTO converge_once VALUES(1);", postgres.Transactional, nil)
	m := migrate(t, cfg, source)
	admin := db.Connect(t)
	for _, interrupted := range []string{"running", "repairing"} {
		if _, err := admin.Exec(testContext(t), "UPDATE agenteam_meta.migration_journal SET state=$1,finished_at=NULL WHERE version=2", interrupted); err != nil {
			t.Fatal("journal fixture mutation failed")
		}
		if state := m.Migrate(testContext(t)); !state.Migrated {
			t.Fatal("applied Goose fact did not converge")
		}
		var count int
		var applied bool
		if err := admin.QueryRow(testContext(t), "SELECT (SELECT count(*) FROM converge_once),(SELECT state='applied' AND finished_at IS NOT NULL FROM agenteam_meta.migration_journal WHERE version=2)").Scan(&count, &applied); err != nil || count != 1 || !applied {
			t.Fatal("migration reran or journal did not converge")
		}
	}
}
