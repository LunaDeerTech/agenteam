//go:build integration

package database_test

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/LunaDeerTech/agenteam/db/migrations"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func outboxMigrationFiles(t *testing.T, through string) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{}
	names, err := fs.Glob(migrations.SQL, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name[:5] > through {
			continue
		}
		raw, e := fs.ReadFile(migrations.SQL, name)
		if e != nil {
			t.Fatal(e)
		}
		files[name] = &fstest.MapFile{Data: raw}
	}
	return files
}
func TestOutboxUnattemptedMigrationFreshAndUpgrade(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "upgrade-eight"
		}
		t.Run(name, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			cfg := db.Config(t, nil)
			if upgrade {
				src, e := postgres.NewSource(outboxMigrationFiles(t, "00008"), nil)
				if e != nil {
					t.Fatal(e)
				}
				migrate(t, cfg, src)
			} else {
				migrate(t, cfg)
			}
			conn := db.Connect(t)
			const eid = "01900000-0000-7000-8000-000000000001"
			const pid = "01900000-0000-7000-8000-000000000002"
			const did = "01900000-0000-7000-8000-000000000003"
			const claimed = "01900000-0000-7000-8000-000000000004"
			const attempt = "01900000-0000-7000-8000-000000000005"
			_, err := conn.Exec(testContext(t), `INSERT INTO agenteam_outbox.handlers(id,effect,ordering_policy,declaration_digest) VALUES('migration.test','domain_ingress','version_guarded','sha256:'||repeat('0',64));
 INSERT INTO agenteam_outbox.events(id,producer,event_type,schema_version,scope,project_id,aggregate_type,aggregate_id,aggregate_version,occurred_at,header,payload,semantic_digest,stable_actor) VALUES('`+eid+`','test','test.changed',1,'project','`+pid+`','test','`+eid+`',1,clock_timestamp(),'{}','payload','sha256:'||repeat('0',64),'fixture');
 INSERT INTO agenteam_outbox.deliveries(id,event_id,handler_id,scope,project_id) VALUES('`+did+`','`+eid+`','migration.test','project','`+pid+`')`)
			if err != nil {
				t.Fatal(err)
			}
			// An actual persisted claim exercises the other old CHECK branch and
			// its deferred attempt identity while the new migration replaces it.
			_, err = conn.Exec(testContext(t), `BEGIN;
 INSERT INTO agenteam_outbox.handlers(id,effect,ordering_policy,declaration_digest) VALUES('migration.claimed','domain_ingress','version_guarded','sha256:'||repeat('1',64));
 INSERT INTO agenteam_outbox.deliveries(id,event_id,handler_id,scope,project_id) VALUES('`+claimed+`','`+eid+`','migration.claimed','project','`+pid+`');
 INSERT INTO agenteam_outbox.attempts(id,delivery_id,process_id,fence,redrive_cycle,cycle_number,lifetime_number,deadline) VALUES('`+attempt+`','`+claimed+`','`+pid+`',1,0,1,1,clock_timestamp()+interval '30 seconds');
 UPDATE agenteam_outbox.deliveries SET phase='processing',current_attempt_id='`+attempt+`',fence=1,cycle_attempts=1,lifetime_attempts=1 WHERE id='`+claimed+`'; COMMIT;`)
			if err != nil {
				t.Fatal(err)
			}
			var claimBefore, claimAfter string
			claimQuery := `SELECT row_to_json(d)::text||row_to_json(a)::text FROM agenteam_outbox.deliveries d JOIN agenteam_outbox.attempts a ON a.id=d.current_attempt_id WHERE d.id=$1`
			if err = conn.QueryRow(testContext(t), claimQuery, claimed).Scan(&claimBefore); err != nil {
				t.Fatal(err)
			}
			var before string
			if err = conn.QueryRow(testContext(t), `SELECT row_to_json(d)::text FROM agenteam_outbox.deliveries d WHERE id=$1`, did).Scan(&before); err != nil {
				t.Fatal(err)
			}
			if upgrade {
				if _, err = conn.Exec(testContext(t), `UPDATE agenteam_outbox.deliveries SET phase='dead_letter',safe_reason='project_stopped' WHERE id=$1`, did); err == nil {
					t.Fatal("00008 unexpectedly allowed unattempted terminal")
				}
				migrate(t, cfg)
			}
			var after string
			if err = conn.QueryRow(testContext(t), `SELECT row_to_json(d)::text FROM agenteam_outbox.deliveries d WHERE id=$1`, did).Scan(&after); err != nil || after != before {
				t.Fatal("upgrade changed existing row")
			}
			if err = conn.QueryRow(testContext(t), claimQuery, claimed).Scan(&claimAfter); err != nil || claimAfter != claimBefore {
				t.Fatal("upgrade changed existing claim")
			}
			for _, assignment := range []string{
				`phase='dead_letter',safe_reason=NULL`, `phase='dead_letter',safe_reason='source_terminal'`,
				`phase='dead_letter',safe_reason='project_stopped',scope='system',project_id=NULL`,
				`phase='dead_letter',safe_reason='project_stopped',cycle_attempts=1,lifetime_attempts=1`,
				`phase='dead_letter',safe_reason='project_stopped',lifetime_attempts=1`,
				`phase='dead_letter',safe_reason='project_stopped',fence=1`,
				`phase='failed',safe_reason='project_stopped'`, `phase='processing',safe_reason='project_stopped'`,
			} {
				if _, err = conn.Exec(testContext(t), `UPDATE agenteam_outbox.deliveries SET `+assignment+` WHERE id=$1`, did); err == nil {
					t.Fatalf("invalid attempt state accepted: %s", assignment)
				}
			}
			if _, err = conn.Exec(testContext(t), `UPDATE agenteam_outbox.deliveries SET phase='dead_letter',safe_reason='project_stopped' WHERE id=$1`, did); err != nil {
				t.Fatal(err)
			}
			var n int64
			if err = conn.QueryRow(testContext(t), `SELECT count(*) FROM agenteam_outbox.attempts`).Scan(&n); err != nil || n != 1 {
				t.Fatal("migration changed actual attempt count")
			}
			if _, err = conn.Exec(testContext(t), `UPDATE agenteam_outbox.deliveries SET phase='pending',safe_reason=NULL,redrive_cycle=1,version=version+1 WHERE id=$1`, did); err != nil {
				t.Fatal("zero-attempt requeue not representable", err)
			}
		})
	}
}
func TestOutboxUnattemptedMigrationDDLandJournalRollback(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	files := outboxMigrationFiles(t, "00008")
	src, err := postgres.NewSource(files, nil)
	if err != nil {
		t.Fatal(err)
	}
	migrate(t, cfg, src)
	conn := db.Connect(t)
	var before, after string
	query := `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='agenteam_outbox.deliveries'::regclass AND conname='outbox_deliveries_attempt_check'`
	if err = conn.QueryRow(testContext(t), query).Scan(&before); err != nil {
		t.Fatal(err)
	}
	raw, err := fs.ReadFile(migrations.SQL, "00009_outbox_unattempted_stop.sql")
	if err != nil {
		t.Fatal(err)
	}
	files["00009_outbox_unattempted_stop.sql"] = &fstest.MapFile{Data: append(raw, []byte("\nSELECT outbox_fixture_transaction_failure();\n")...)}
	bad, err := postgres.NewSource(files, nil)
	if err != nil {
		t.Fatal(err)
	}
	m, err := postgres.NewMigrator(cfg, bad)
	if err != nil {
		t.Fatal(err)
	}
	if result := m.Migrate(testContext(t)); result.Migrated || result.Fault == nil {
		t.Fatal("intentional transaction failure accepted")
	}
	if err = conn.QueryRow(testContext(t), query).Scan(&after); err != nil || after != before {
		t.Fatal("DDL escaped failed migration")
	}
	var journal, goose int64
	if err = conn.QueryRow(testContext(t), `SELECT (SELECT count(*) FROM agenteam_meta.migration_journal WHERE version=9 AND state='pending'),(SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=9 AND is_applied)`).Scan(&journal, &goose); err != nil || journal != 1 || goose != 0 {
		t.Fatalf("failed migration journal residue: %d/%d %v", journal, goose, err)
	}
	// D03 retains the immutable failed attempt as pending. The SQL and Goose
	// transaction roll back, while retry uses exactly the same source checksum.
	if _, err = conn.Exec(testContext(t), `CREATE FUNCTION public.outbox_fixture_transaction_failure() RETURNS void LANGUAGE SQL AS 'SELECT NULL::void'`); err != nil {
		t.Fatal(err)
	}
	migrate(t, cfg, bad)
}
