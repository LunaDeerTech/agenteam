//go:build integration

package account_test

import (
	"github.com/LunaDeerTech/agenteam/db/migrations"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"io/fs"
	"testing"
	"testing/fstest"
)

func migrationFiles(t *testing.T, last string) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	names, e := fs.Glob(migrations.SQL, "*.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range names {
		if name[:5] > last {
			continue
		}
		b, e := fs.ReadFile(migrations.SQL, name)
		if e != nil {
			t.Fatal(e)
		}
		out[name] = &fstest.MapFile{Data: b}
	}
	return out
}
func TestAccountMigrationFreshAndNineUpgrade(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "upgrade-nine"
		}
		t.Run(name, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			cfg := db.Config(t, nil)
			if upgrade {
				source, e := postgres.NewSource(migrationFiles(t, "00009"), nil)
				if e != nil {
					t.Fatal(e)
				}
				migrate(t, cfg, source)
			}
			migrate(t, cfg)
			store := openStore(t, cfg)
			if _, err := store.Check(ctxFor(t)); err != nil {
				t.Fatal(err)
			}
			conn := db.Connect(t)
			var n int
			e := conn.QueryRow(ctxFor(t), `SELECT count(*) FROM information_schema.tables WHERE table_schema='agenteam_account'`).Scan(&n)
			if e != nil || n != 20 {
				t.Fatalf("tables=%d %v", n, e)
			}
			var ownerCheck string
			e = conn.QueryRow(ctxFor(t), `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='agenteam_secret.secret_leases'::regclass AND conname='secret_leases_owner_kind_check'`).Scan(&ownerCheck)
			if e != nil || ownerCheck == "" {
				t.Fatal(e)
			}
			for _, sql := range []string{`INSERT INTO agenteam_account.smtp_settings(singleton,id,version,configured,enabled) VALUES(true,'01900000-0000-7000-8000-000000000001',1,true,true)`, `INSERT INTO agenteam_account.auth_attempts(id,kind,command_id,outcome,phase) VALUES('01900000-0000-7000-8000-000000000001','response_read','01900000-0000-7000-8000-000000000002','planned','active')`} {
				if _, e = conn.Exec(ctxFor(t), sql); e == nil {
					t.Fatal("null-invalid account state accepted")
				}
			}
		})
	}
}
func TestAccountMigrationDDLAndJournalRollback(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	files := migrationFiles(t, "00009")
	old, e := postgres.NewSource(files, nil)
	if e != nil {
		t.Fatal(e)
	}
	migrate(t, cfg, old)
	raw, e := fs.ReadFile(migrations.SQL, "00010_account_session_smtp.sql")
	if e != nil {
		t.Fatal(e)
	}
	files["00010_account_session_smtp.sql"] = &fstest.MapFile{Data: append(raw, []byte("\nSELECT account_fixture_migration_failure();\n")...)}
	bad, e := postgres.NewSource(files, nil)
	if e != nil {
		t.Fatal(e)
	}
	m, e := postgres.NewMigrator(cfg, bad)
	if e != nil {
		t.Fatal(e)
	}
	if r := m.Migrate(ctxFor(t)); r.Migrated {
		t.Fatal("bad migration committed")
	}
	conn := db.Connect(t)
	var n int
	if e = conn.QueryRow(ctxFor(t), `SELECT count(*) FROM pg_namespace WHERE nspname='agenteam_account'`).Scan(&n); e != nil || n != 0 {
		t.Fatal("DDL survived rollback", e)
	}
	var pending, applied int64
	if e = conn.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_meta.migration_journal WHERE version=10 AND state='pending'),(SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=10 AND is_applied)`).Scan(&pending, &applied); e != nil || pending != 1 || applied != 0 {
		t.Fatalf("journal=%d/%d %v", pending, applied, e)
	}
	if _, e = conn.Exec(ctxFor(t), `CREATE FUNCTION public.account_fixture_migration_failure() RETURNS void LANGUAGE SQL AS 'SELECT NULL::void'`); e != nil {
		t.Fatal(e)
	}
	migrate(t, cfg, bad)
}
