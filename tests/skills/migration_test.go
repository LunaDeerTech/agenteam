//go:build integration

package skill_test

import (
	"errors"
	"io/fs"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/LunaDeerTech/agenteam/db/migrations"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const skillMigrationName = "00027_skills.sql"

func skillMigrationFiles(t *testing.T, through string) fstest.MapFS {
	t.Helper()
	names, err := fs.Glob(migrations.SQL, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{}
	for _, name := range names {
		if name[:5] > through {
			continue
		}
		body, err := fs.ReadFile(migrations.SQL, name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: body}
	}
	return files
}

func skillMigrationSource(t *testing.T, files fstest.MapFS) postgres.Source {
	t.Helper()
	source, err := postgres.NewSource(files, nil)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func migrateSkillDB(t *testing.T, db *pgfixture.Database, source postgres.Source) {
	t.Helper()
	migrator, err := postgres.NewMigrator(db.Config(t, nil), source)
	if err != nil {
		t.Fatal(err)
	}
	result := migrator.Migrate(testContext(t))
	if !result.Migrated || result.Version != source.Target() {
		t.Fatal("Skill migration did not reach exact source target", result.Fault)
	}
}

// These are historical, test-owned Account/Audit rows, not a live login or an
// authorization substitute. The snapshot stays in memory and is never logged.
func seedBeforeSkill(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	user, session, record := testID[id.User](t), testID[id.Session](t), testID[struct{}](t)
	if _, err := conn.Exec(testContext(t), `INSERT INTO agenteam_account.users
 (id,email,username,display_name,role,password_phc,password_version,version,auth_sequence,initial_password_suggestion,theme)
 VALUES($1,'skills-migration@example.test','skills-migration','before skills','user','fixture-not-a-login-hash',1,1,1,false,'system')`, user.String()); err != nil {
		t.Fatal("legacy Account fixture rejected")
	}
	if _, err := conn.Exec(testContext(t), `INSERT INTO agenteam_audit.audit_records
 (id,scope,actor_kind,user_id,session_id,action,outcome,resource_kind,resource_id,metadata,semantic_digest,producer,cause_ref,ordinal)
 VALUES($1,'system','human',$2,$3,'secret.create','success','secret',$1,'{"name":"historical"}','sha256:'||repeat('0',64),'secret','sha256:'||repeat('0',64),0)`, record.String(), user.String(), session.String()); err != nil {
		t.Fatal("legacy Audit fixture rejected")
	}
}

func beforeSkillSnapshot(t *testing.T, conn *pgx.Conn) string {
	t.Helper()
	var value string
	err := conn.QueryRow(testContext(t), `SELECT jsonb_build_array(
 (SELECT coalesce(jsonb_agg(to_jsonb(u) ORDER BY id),'[]') FROM agenteam_account.users u),
 (SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY id),'[]') FROM agenteam_audit.audit_records a),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(n.nspname,r.relname,c.conname,pg_get_constraintdef(c.oid)) ORDER BY n.nspname,r.relname,c.conname),'[]')
 FROM pg_constraint c JOIN pg_class r ON r.oid=c.conrelid JOIN pg_namespace n ON n.oid=r.relnamespace
 WHERE n.nspname LIKE 'agenteam\_%' ESCAPE '\' AND n.nspname NOT IN ('agenteam_meta','agenteam_skill')),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(schemaname,tablename,indexname,indexdef) ORDER BY schemaname,tablename,indexname),'[]')
 FROM pg_indexes WHERE schemaname LIKE 'agenteam\_%' ESCAPE '\' AND schemaname NOT IN ('agenteam_meta','agenteam_skill'))
 )::text`).Scan(&value)
	if err != nil {
		t.Fatal("legacy snapshot unavailable")
	}
	return value
}

func assertSkillSchema(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	var tables []string
	if err := conn.QueryRow(testContext(t), `SELECT array_agg(table_name::text ORDER BY table_name) FROM information_schema.tables WHERE table_schema='agenteam_skill' AND table_type='BASE TABLE'`).Scan(&tables); err != nil || !reflect.DeepEqual(tables, []string{"cleanup", "initializations", "object_attempts", "revisions", "skills", "work"}) {
		t.Fatal("unexpected Skill table manifest")
	}
	var foreignKeys, indexes, applied, completed int
	err := conn.QueryRow(testContext(t), `SELECT
 (SELECT count(*) FROM pg_constraint c JOIN pg_class a ON a.oid=c.conrelid JOIN pg_namespace an ON an.oid=a.relnamespace JOIN pg_class b ON b.oid=c.confrelid JOIN pg_namespace bn ON bn.oid=b.relnamespace WHERE c.contype='f' AND an.nspname='agenteam_skill' AND bn.nspname<>'agenteam_skill'),
 (SELECT count(*) FROM pg_indexes WHERE schemaname='agenteam_skill' AND indexname IN ('skill_attempts_original_object','skill_work_live_project','skill_work_recovery')),
 (SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=27 AND is_applied),
 (SELECT count(*) FROM agenteam_meta.migration_journal WHERE version=27 AND state='applied')`).Scan(&foreignKeys, &indexes, &applied, &completed)
	if err != nil || foreignKeys != 0 || indexes != 3 || applied != 1 || completed != 1 {
		t.Fatal("Skill ownership/index/migration history invariant failed", err)
	}
}

func TestSkillMigration(t *testing.T) {
	t.Run("fresh_and_repeat", func(t *testing.T) {
		db := pgfixture.NewDatabase(t)
		source := skillMigrationSource(t, skillMigrationFiles(t, "00027"))
		migrateSkillDB(t, db, source)
		conn := db.Connect(t)
		assertSkillSchema(t, conn)
		seedBeforeSkill(t, conn)
		before := beforeSkillSnapshot(t, conn)
		migrateSkillDB(t, db, source)
		assertSkillSchema(t, conn)
		if beforeSkillSnapshot(t, conn) != before {
			t.Fatal("repeat migration changed previous domain facts or constraints")
		}
	})
	t.Run("populated_00026_upgrade", func(t *testing.T) {
		db := pgfixture.NewDatabase(t)
		migrateSkillDB(t, db, skillMigrationSource(t, skillMigrationFiles(t, "00026")))
		conn := db.Connect(t)
		seedBeforeSkill(t, conn)
		before := beforeSkillSnapshot(t, conn)
		migrateSkillDB(t, db, skillMigrationSource(t, skillMigrationFiles(t, "00027")))
		assertSkillSchema(t, conn)
		if beforeSkillSnapshot(t, conn) != before {
			t.Fatal("Skill upgrade changed previous Account/Audit facts or another domain's constraints")
		}
	})
	t.Run("six_table_constraints", skillMigrationConstraints)
	t.Run("transactional_failure_and_same_source_recovery", func(t *testing.T) {
		db := pgfixture.NewDatabase(t)
		files := skillMigrationFiles(t, "00026")
		migrateSkillDB(t, db, skillMigrationSource(t, files))
		conn := db.Connect(t)
		seedBeforeSkill(t, conn)
		before := beforeSkillSnapshot(t, conn)
		raw, err := fs.ReadFile(migrations.SQL, skillMigrationName)
		if err != nil {
			t.Fatal(err)
		}
		files[skillMigrationName] = &fstest.MapFile{Data: append(append([]byte{}, raw...), []byte("\nSELECT public.skill_fixture_migration_failure();\n")...)}
		failedSource := skillMigrationSource(t, files)
		migrator, err := postgres.NewMigrator(db.Config(t, nil), failedSource)
		if err != nil {
			t.Fatal(err)
		}
		result := migrator.Migrate(testContext(t))
		if result.Migrated || result.Fault == nil {
			t.Fatal("deliberate final DDL failure was accepted")
		}
		var absent bool
		var pending, applied int
		err = conn.QueryRow(testContext(t), `SELECT to_regnamespace('agenteam_skill') IS NULL,
 (SELECT count(*) FROM agenteam_meta.migration_journal WHERE version=27 AND state='pending'),
 (SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=27 AND is_applied)`).Scan(&absent, &pending, &applied)
		if err != nil || !absent || pending != 1 || applied != 0 || beforeSkillSnapshot(t, conn) != before {
			t.Fatal("failed Skill migration escaped its transaction", err)
		}
		// The failed checksum is durable. A different source is not a repair.
		original, err := postgres.NewMigrator(db.Config(t, nil), skillMigrationSource(t, skillMigrationFiles(t, "00027")))
		if err != nil {
			t.Fatal(err)
		}
		if r := original.Migrate(testContext(t)); r.Migrated || r.Fault == nil || r.Fault.Code() != postgres.MigrationHistoryDiverged {
			t.Fatal("a different checksum replaced failed migration history")
		}
		if _, err := conn.Exec(testContext(t), `CREATE FUNCTION public.skill_fixture_migration_failure() RETURNS void LANGUAGE SQL AS 'SELECT NULL::void'`); err != nil {
			t.Fatal("same-source repair precondition failed")
		}
		migrateSkillDB(t, db, failedSource)
		assertSkillSchema(t, conn)
		if beforeSkillSnapshot(t, conn) != before {
			t.Fatal("same-source recovery changed previous domain facts")
		}
	})
}

// This is a SQL-constraint fixture, not a service-published Skill or an Object
// authorization. No other domain row is inserted to satisfy a hidden FK.
func skillMigrationConstraints(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	migrateSkillDB(t, db, skillMigrationSource(t, skillMigrationFiles(t, "00027")))
	conn := db.Connect(t)
	args := make([]any, 11)
	for i := range args {
		args[i] = testID[struct{}](t).String()
	}
	tx, err := conn.Begin(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(testContext(t)) })
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO agenteam_skill.initializations(project_id,creation_id,initialization_key,skill_id,revision_id,semantic_digest,bundle_id,revision,package_sha256,manifest_sha256,byte_size,manifest,name,description,phase,version,object_id,upload_id,current_attempt_id,created_at,updated_at)
 VALUES($1,$2,'migration-original',$3,$4,'sha256:'||repeat('1',64),'builtin.add-skills.v1',1,'sha256:'||repeat('2',64),'sha256:'||repeat('3',64),100,'{}','Add Skills','constraint fixture','published',1,$5,$6,$7,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, args[:7]},
		{`INSERT INTO agenteam_skill.object_attempts(attempt_id,project_id,creation_id,skill_id,revision_id,object_id,upload_id,process_id,created_at) VALUES($7,$1,$2,$3,$4,$5,$6,$8,'2026-01-01T00:00:00Z')`, args[:8]},
		{`INSERT INTO agenteam_skill.skills(id,project_id,creation_id,revision_id,name,normalized_name,description,protected,current_revision,version,serving) VALUES($3,$1,$2,$4,'Add Skills','add-skills','constraint fixture',true,1,1,true)`, args[:4]},
		{`INSERT INTO agenteam_skill.revisions(id,project_id,skill_id,revision,object_id,object_version,object_created_at,published_at) VALUES($1,$2,$3,1,$4,1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, []any{args[3], args[0], args[2], args[4]}},
		{`INSERT INTO agenteam_skill.work(id,project_id,skill_id,process_id,kind,phase,fence,created_at,joined_at) VALUES($1,$2,$3,$4,'initialization','joined',1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, []any{args[8], args[0], args[2], args[7]}},
		{`INSERT INTO agenteam_skill.cleanup(id,project_id,lifecycle_operation_id,project_version,action,skill_id,revision_id,object_id,upload_id,phase,version,created_at,updated_at) VALUES($1,$2,$3,1,'delete',$4,$5,$6,$7,'gated',1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, []any{args[9], args[0], args[10], args[2], args[3], args[4], args[5]}},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(testContext(t), statement.sql, statement.args...); err != nil {
			t.Fatal("valid deferred Skill graph could not be inserted")
		}
	}
	if err := tx.Commit(testContext(t)); err != nil {
		t.Fatal("valid cyclic Skill facts failed deferred commit")
	}
	constraints := []struct{ name, sql, code string }{
		{"uuid7_only", `UPDATE agenteam_skill.work SET process_id='00000000-0000-4000-8000-000000000000'`, "23514"},
		{"initialization_key", `UPDATE agenteam_skill.initializations SET initialization_key=''`, "23514"},
		{"builtin_closed", `UPDATE agenteam_skill.initializations SET bundle_id='foreign.bundle'`, "23514"},
		{"revision_one", `UPDATE agenteam_skill.initializations SET revision=2`, "23514"},
		{"size_positive", `UPDATE agenteam_skill.initializations SET byte_size=0`, "23514"},
		{"size_cap", `UPDATE agenteam_skill.initializations SET byte_size=262145`, "23514"},
		{"manifest_object", `UPDATE agenteam_skill.initializations SET manifest='[]'`, "23514"},
		{"digest_shape", `UPDATE agenteam_skill.initializations SET package_sha256='not-a-digest'`, "23514"},
		{"closed_phase", `UPDATE agenteam_skill.initializations SET phase='complete'`, "23514"},
		{"paired_object_upload", `UPDATE agenteam_skill.initializations SET upload_id=NULL`, "23514"},
		{"planned_has_no_attempt", `UPDATE agenteam_skill.initializations SET phase='planned'`, "23514"},
		{"failed_requires_reason", `UPDATE agenteam_skill.initializations SET phase='failed'`, "23514"},
		{"closed_safe_reason", `UPDATE agenteam_skill.initializations SET phase='failed',safe_reason='raw-private-error'`, "23514"},
		{"exact_current_attempt", `UPDATE agenteam_skill.initializations SET current_attempt_id='00000000-0000-7000-8000-000000000000'`, "23503"},
		{"attempt_exact_parent", `UPDATE agenteam_skill.object_attempts SET creation_id='00000000-0000-7000-8000-000000000000'`, "23503"},
		{"protected_builtin", `UPDATE agenteam_skill.skills SET protected=false`, "23514"},
		{"canonical_name", `UPDATE agenteam_skill.skills SET normalized_name='other'`, "23514"},
		{"current_revision_one", `UPDATE agenteam_skill.skills SET current_revision=2`, "23514"},
		{"skill_exact_parent", `UPDATE agenteam_skill.skills SET creation_id='00000000-0000-7000-8000-000000000000'`, "23503"},
		{"object_version_positive", `UPDATE agenteam_skill.revisions SET object_version=0`, "23514"},
		{"publication_chronology", `UPDATE agenteam_skill.revisions SET published_at='2025-01-01T00:00:00Z'`, "23514"},
		{"revision_exact_object", `UPDATE agenteam_skill.revisions SET object_id='00000000-0000-7000-8000-000000000000'`, "23503"},
		{"closed_work_kind", `UPDATE agenteam_skill.work SET kind='foreign'`, "23514"},
		{"joined_requires_time", `UPDATE agenteam_skill.work SET joined_at=NULL`, "23514"},
		{"live_has_no_join_time", `UPDATE agenteam_skill.work SET phase='running'`, "23514"},
		{"positive_fence", `UPDATE agenteam_skill.work SET fence=0`, "23514"},
		{"nonnegative_recovery_pass", `UPDATE agenteam_skill.work SET recovery_pass=-1`, "23514"},
		{"cleanup_delete_only", `UPDATE agenteam_skill.cleanup SET action='archive'`, "23514"},
		{"closed_cleanup_phase", `UPDATE agenteam_skill.cleanup SET phase='done'`, "23514"},
		{"cleanup_exact_upload", `UPDATE agenteam_skill.cleanup SET upload_id='00000000-0000-7000-8000-000000000000'`, "23503"},
	}
	for _, check := range constraints {
		t.Run(check.name, func(t *testing.T) {
			tx, err := conn.Begin(testContext(t))
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(testContext(t))
			_, rejected := tx.Exec(testContext(t), check.sql)
			if rejected == nil {
				_, rejected = tx.Exec(testContext(t), "SET CONSTRAINTS ALL IMMEDIATE")
			}
			var pgError *pgconn.PgError
			if !errors.As(rejected, &pgError) || pgError.Code != check.code {
				t.Fatal("database did not reject invalid Skill fact at its expected constraint")
			}
		})
	}
	assertSkillSchema(t, conn)
}
