//go:build integration

package database_test

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/LunaDeerTech/agenteam/db/migrations"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func migrate(t *testing.T, cfg postgres.Config, sources ...postgres.Source) *postgres.Migrator {
	t.Helper()
	m, err := postgres.NewMigrator(cfg, sources...)
	if err != nil {
		t.Fatal(err)
	}
	state := m.Migrate(testContext(t))
	if !state.Migrated {
		t.Fatalf("migration failed: %v", state.Fault)
	}
	return m
}
func openStore(t *testing.T, cfg postgres.Config) *postgres.Store {
	t.Helper()
	s, err := postgres.Open(testContext(t), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.ForceClose(ctx); err != nil {
			t.Error(err)
		}
	})
	return s
}
func fixtureSource(t *testing.T, sql string, mode postgres.MigrationMode, plan *postgres.RecoveryPlan) postgres.Source {
	t.Helper()
	first, err := fs.ReadFile(migrations.SQL, "00001_database_foundation.sql")
	if err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{"00001_database_foundation.sql": {Data: first}}
	plans := map[int64]postgres.RecoveryPlan{}
	if sql != "" {
		header := "-- agenteam:transaction " + string(mode) + "\n"
		if mode == postgres.NonTransactional {
			header += "-- +goose NO TRANSACTION\n"
		}
		files["00002_fixture.sql"] = &fstest.MapFile{Data: []byte(header + "-- +goose Up\n" + sql + "\n")}
		if plan != nil {
			plans[2] = *plan
		}
	}
	source, err := postgres.NewSource(files, plans)
	if err != nil {
		t.Fatal(err)
	}
	return source
}
func TestEmptyRepeatUpgradeAndHealth(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	store := openStore(t, cfg)
	if _, err := store.Check(testContext(t)); err == nil {
		t.Fatal("unmigrated database reported healthy")
	}
	m := migrate(t, cfg)
	if state := m.Migrate(testContext(t)); !state.Migrated || state.Version != 1 {
		t.Fatalf("repeat migration: %v", state.Fault)
	}
	health, err := store.Check(testContext(t))
	if err != nil || !health.PostgreSQL || !health.PGVector || !health.Migrations || !health.ReadWrite || health.ExtensionVersion != "0.8.1" {
		t.Fatalf("health failed: %v", err)
	}
	conn := db.Connect(t)
	var probes, versions int
	if err := conn.QueryRow(testContext(t), "SELECT (SELECT count(*) FROM agenteam_meta.health_probe),(SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=1)").Scan(&probes, &versions); err != nil || probes != 0 || versions != 1 {
		t.Fatal("probe residue or repeated migration")
	}
	upgraded := fixtureSource(t, "CREATE TABLE public.fixture_once(value integer NOT NULL); INSERT INTO public.fixture_once VALUES(1);", postgres.Transactional, nil)
	m = migrate(t, cfg, upgraded)
	if state := m.Migrate(testContext(t)); !state.Migrated {
		t.Fatalf("repeat upgraded: %v", state.Fault)
	}
	if err := conn.QueryRow(testContext(t), "SELECT count(*) FROM fixture_once").Scan(&versions); err != nil || versions != 1 {
		t.Fatal("migration executed twice")
	}
	older, err := postgres.NewMigrator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if state := older.Migrate(testContext(t)); state.Migrated || state.Fault.Code() != postgres.MigrationHistoryDiverged {
		t.Fatalf("newer database accepted: %v", state.Fault)
	}
	if _, err := store.Check(testContext(t)); err == nil {
		t.Fatal("health accepted newer schema")
	}
}
func TestMigrationRollbackAndHistoryMismatch(t *testing.T) {
	for _, scenario := range []string{"bad_sql", "checksum", "filename", "missing_journal", "missing_goose", "version_hole", "metadata_format", "metadata_default"} {
		t.Run(scenario, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			cfg := db.Config(t, nil)
			migrate(t, cfg)
			conn := db.Connect(t)
			source := fixtureSource(t, "CREATE TABLE public.must_rollback(id integer); SELECT private_sql_sentinel_invalid();", postgres.Transactional, nil)
			switch scenario {
			case "bad_sql":
				m, _ := postgres.NewMigrator(cfg, source)
				result := m.Migrate(testContext(t))
				if result.Migrated || result.Fault == nil {
					t.Fatal("bad SQL accepted")
				}
				assertSafe(t, result.Fault, "private_sql_sentinel_invalid", db.Fixture.Password)
				var absent bool
				if err := conn.QueryRow(testContext(t), "SELECT to_regclass('public.must_rollback') IS NULL").Scan(&absent); err != nil || !absent {
					t.Fatal("transaction DDL not rolled back")
				}
			case "checksum":
				if _, err := conn.Exec(testContext(t), "UPDATE agenteam_meta.migration_journal SET checksum='sha256:' || repeat('0',64) WHERE version=1"); err != nil {
					t.Fatal("fixture mutation failed")
				}
			case "filename":
				if _, err := conn.Exec(testContext(t), "UPDATE agenteam_meta.migration_journal SET filename='00001_changed.sql' WHERE version=1"); err != nil {
					t.Fatal("fixture mutation failed")
				}
			case "missing_journal":
				if _, err := conn.Exec(testContext(t), "DELETE FROM agenteam_meta.migration_journal WHERE version=1"); err != nil {
					t.Fatal("fixture mutation failed")
				}
			case "missing_goose":
				if _, err := conn.Exec(testContext(t), "DELETE FROM agenteam_meta.goose_db_version WHERE version_id=1"); err != nil {
					t.Fatal("fixture mutation failed")
				}
			case "version_hole":
				if _, err := conn.Exec(testContext(t), "INSERT INTO agenteam_meta.goose_db_version(version_id,is_applied) VALUES(3,true)"); err != nil {
					t.Fatal("fixture mutation failed")
				}
			case "metadata_format":
				if _, err := conn.Exec(testContext(t), "ALTER TABLE agenteam_meta.migration_journal ADD COLUMN unexpected integer"); err != nil {
					t.Fatal("fixture mutation failed")
				}
			case "metadata_default":
				if _, err := conn.Exec(testContext(t), "ALTER TABLE agenteam_meta.goose_db_version ALTER COLUMN tstamp DROP DEFAULT"); err != nil {
					t.Fatal("fixture mutation failed")
				}
			}
			if scenario != "bad_sql" {
				m, _ := postgres.NewMigrator(cfg)
				result := m.Migrate(testContext(t))
				if result.Migrated || result.Fault == nil || result.Fault.Code() != postgres.MigrationHistoryDiverged {
					t.Fatalf("history mismatch accepted: %v", result.Fault)
				}
			}
		})
	}
}
