//go:build integration

package database_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
)

func migrationWaiterPID(t *testing.T, conn *pgx.Conn) int32 {
	t.Helper()
	waitDatabase(t, conn, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam' AND wait_event_type='Lock')")
	var pid int32
	if err := conn.QueryRow(testContext(t), "SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam' AND wait_event_type='Lock'").Scan(&pid); err != nil {
		t.Fatal("owned migration waiter missing")
	}
	return pid
}

func assertMigrationBackendExited(t *testing.T, conn *pgx.Conn, pid int32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	timer := time.NewTicker(10 * time.Millisecond)
	defer timer.Stop()
	for {
		var absent bool
		if err := conn.QueryRow(ctx, "SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid=$1)", pid).Scan(&absent); err != nil {
			t.Fatal("canceled migration backend did not exit within its cleanup bound")
		}
		if absent {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("canceled migration backend remains")
		case <-timer.C:
		}
	}
}

func TestMigrationCancellationClosesGuardAndSQLBackends(t *testing.T) {
	for _, mode := range []string{"guard", "transactional_sql", "non_transactional_sql_and_repair"} {
		t.Run(mode, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			cfg := db.Config(t, nil)
			admin := db.Connect(t)
			migrate(t, cfg)
			var source postgres.Source
			var lockSQL string
			switch mode {
			case "guard":
				source, _ = postgres.EmbeddedSource()
				lockSQL = "SELECT pg_advisory_lock(1095193677,1)"
			case "transactional_sql":
				source = fixtureSource(t, "CREATE TABLE fixture_canceled_migration(value integer); SELECT pg_advisory_xact_lock(99125);", postgres.Transactional, nil)
				lockSQL = "SELECT pg_advisory_lock(99125)"
			default:
				source = fixtureSource(t, "CREATE TABLE fixture_canceled_migration(value integer); SELECT pg_advisory_lock(99126);", postgres.NonTransactional, &postgres.RecoveryPlan{RestoreSteps: []string{"DROP TABLE IF EXISTS fixture_canceled_migration", "SELECT pg_advisory_lock(99127)"}, VerifyRestored: "SELECT to_regclass('fixture_canceled_migration') IS NULL"})
				lockSQL = "SELECT pg_advisory_lock(99126)"
			}
			if _, err := admin.Exec(testContext(t), lockSQL); err != nil {
				t.Fatal("owned migration barrier failed")
			}
			m, err := postgres.NewMigrator(cfg, source)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(testContext(t))
			defer cancel()
			done := make(chan postgres.MigrationState, 1)
			go func() { done <- m.Migrate(ctx) }()
			pid := migrationWaiterPID(t, admin)
			started := time.Now()
			cancel()
			state := receive(t, done)
			if state.Migrated || state.Fault == nil || time.Since(started) > time.Second {
				t.Fatal("canceled migration did not fail within its bound")
			}
			assertMigrationBackendExited(t, admin, pid)
			if mode == "transactional_sql" {
				var absent bool
				if err := admin.QueryRow(testContext(t), "SELECT to_regclass('fixture_canceled_migration') IS NULL").Scan(&absent); err != nil || !absent {
					t.Fatal("canceled transactional DDL persisted")
				}
			}
			if mode != "non_transactional_sql_and_repair" {
				return
			}
			// A new guard owner must preserve the non-transactional evidence.
			if state := m.Migrate(testContext(t)); state.Migrated || state.Fault == nil || state.Fault.Code() != postgres.MigrationRepairRequired {
				t.Fatal("canceled non-transactional migration reran without repair")
			}
			if _, err := admin.Exec(testContext(t), "SELECT pg_advisory_lock(99127)"); err != nil {
				t.Fatal("owned repair barrier failed")
			}
			repairContext, cancelRepair := context.WithCancel(testContext(t))
			defer cancelRepair()
			repairs := make(chan postgres.RepairResult, 1)
			checksum := source.Manifest()[len(source.Manifest())-1].Checksum
			go func() { repairs <- m.Repair(repairContext, fixtureVersion(t), checksum) }()
			pid = migrationWaiterPID(t, admin)
			started = time.Now()
			cancelRepair()
			repair := receive(t, repairs)
			if repair.RepairedToPending || repair.Fault == nil || time.Since(started) > time.Second {
				t.Fatal("canceled repair did not fail within its bound")
			}
			assertMigrationBackendExited(t, admin, pid)
			var journal string
			if err := admin.QueryRow(testContext(t), fmt.Sprintf("SELECT state FROM agenteam_meta.migration_journal WHERE version=%d", fixtureVersion(t))).Scan(&journal); err != nil || journal != "repairing" {
				t.Fatal("repair interruption evidence was lost")
			}
			if _, err := admin.Exec(testContext(t), "SELECT pg_advisory_unlock(99126),pg_advisory_unlock(99127)"); err != nil {
				t.Fatal("owned barriers did not release")
			}
			if repair := m.Repair(testContext(t), fixtureVersion(t), checksum); !repair.RepairedToPending {
				t.Fatal("interrupted repair could not resume")
			}
			if state := m.Migrate(testContext(t)); !state.Migrated {
				t.Fatal("repaired migration could not run")
			}
		})
	}
}
