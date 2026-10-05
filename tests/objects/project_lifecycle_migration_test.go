//go:build integration

package objects_test

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/LunaDeerTech/agenteam/db/migrations"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const projectStopMigration = "00014_object_artifact_project_stop.sql"

func projectMigrationContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func projectMigrationFiles(t *testing.T, through string) fstest.MapFS {
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
		raw, err := fs.ReadFile(migrations.SQL, name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: raw}
	}
	return files
}

func projectMigrationRunner(t *testing.T, db *pgfixture.Database, files fstest.MapFS) *postgres.Migrator {
	t.Helper()
	source, err := postgres.NewSource(files, nil)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := postgres.NewMigrator(db.Config(t, nil), source)
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func projectMigrate(t *testing.T, ctx context.Context, runner *postgres.Migrator, version int64) {
	t.Helper()
	result := runner.Migrate(ctx)
	if !result.Migrated || result.Fault != nil || result.Version != version {
		t.Fatalf("migration result: migrated=%v version=%d fault=%v", result.Migrated, result.Version, result.Fault)
	}
}

// These are valid pre-existing D05/D08 persistence fixtures, not authorization
// grants or fabricated I/O completion. In particular the source lease remains
// active, the Artifact command remains pending, and Project creation is not
// initialized while migration runs.
func projectMigrationSeed(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	_, err := conn.Exec(ctx, `BEGIN;
INSERT INTO agenteam_project.projects(id,owner_user_id,name,normalized_name,description,lifecycle,version,created_at,updated_at,creation_id)
VALUES('01900000-0000-7000-8000-000000000001','01900000-0000-7000-8000-000000000002','Migration.Project','migration.project','retained','active',1,'2026-01-01 00:00:00+00','2026-01-01 00:00:00+00','01900000-0000-7000-8000-000000000003');
INSERT INTO agenteam_project.creations(id,project_id,owner_user_id,command_key,semantic_digest,request_name,request_description,state,initialization_key,version,created_at,updated_at,event_id)
VALUES('01900000-0000-7000-8000-000000000003','01900000-0000-7000-8000-000000000001','01900000-0000-7000-8000-000000000002','migration-create','sha256:'||repeat('1',64),'Migration.Project','retained','accepted','migration-initialize',1,'2026-01-01 00:00:00+00','2026-01-01 00:00:00+00','01900000-0000-7000-8000-000000000004');
INSERT INTO agenteam_project.work_claims(work_kind,work_id,project_id,process_id,attempt_id,fence,phase)
VALUES('creation','01900000-0000-7000-8000-000000000003','01900000-0000-7000-8000-000000000001','01900000-0000-7000-8000-000000000009','01900000-0000-7000-8000-000000000012',2,'running');
INSERT INTO agenteam_object.objects(id,scope,partition_id,project_id,media_type,byte_size,sha256,state,version,candidate_key)
VALUES('01900000-0000-7000-8000-000000000005','project','01900000-0000-7000-8000-000000000001','01900000-0000-7000-8000-000000000001','text/plain',4,decode(repeat('0',64),'hex'),'available',3,'candidate/01900000-0000-7000-8000-000000000005');
INSERT INTO agenteam_object.object_references(object_id,owner_kind,owner_id,partition_id,kind)
VALUES('01900000-0000-7000-8000-000000000005','artifact','01900000-0000-7000-8000-000000000006','01900000-0000-7000-8000-000000000001','canonical');
INSERT INTO agenteam_object.object_leases(id,object_id,owner_kind,owner_id,process_id,state)
VALUES('01900000-0000-7000-8000-000000000008','01900000-0000-7000-8000-000000000005','source','01900000-0000-7000-8000-000000000010','01900000-0000-7000-8000-000000000009','active');
INSERT INTO agenteam_artifact.artifacts(id,file_id,project_id,object_id,kind,name,description,media_type,byte_size,sha256,object_version,object_created_at,creator_kind,creator_id,creation_cause)
VALUES('01900000-0000-7000-8000-000000000006','01900000-0000-7000-8000-000000000007','01900000-0000-7000-8000-000000000001','01900000-0000-7000-8000-000000000005','generated','retained.txt','retained','text/plain',4,decode(repeat('0',64),'hex'),3,'2026-01-01 00:00:00+00','human','01900000-0000-7000-8000-000000000002','01900000-0000-7000-8000-000000000003');
INSERT INTO agenteam_artifact.commands(command_hash,command_key,project_id,artifact_id,file_id,stable_actor,request_digest,creation_cause,path,kind,state,name,description,media_type,byte_size,sha256,source,source_lease_id,creator_kind,creator_id)
VALUES(decode(repeat('1',64),'hex'),'retained-source-command','01900000-0000-7000-8000-000000000001','01900000-0000-7000-8000-000000000010','01900000-0000-7000-8000-000000000011','migration-fixture',decode(repeat('2',64),'hex'),'01900000-0000-7000-8000-000000000003','source','generated','pending','pending.txt','retained source fact','text/plain',4,decode(repeat('0',64),'hex'),'{"migration_fixture":true}','01900000-0000-7000-8000-000000000008','human','01900000-0000-7000-8000-000000000002');
COMMIT;`)
	if err != nil {
		t.Fatal("seed pre-00014 facts", err)
	}
}

func projectMigrationFacts(t *testing.T, ctx context.Context, conn *pgx.Conn) string {
	t.Helper()
	var value string
	err := conn.QueryRow(ctx, `SELECT jsonb_build_object(
'project',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_project.projects t),
'creation',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_project.creations t),
'claim',(SELECT jsonb_agg(to_jsonb(t) ORDER BY work_id) FROM agenteam_project.work_claims t),
'object',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_object.objects t),
'reference',(SELECT jsonb_agg(to_jsonb(t) ORDER BY object_id,owner_id) FROM agenteam_object.object_references t),
'lease',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_object.object_leases t),
'artifact',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_artifact.artifacts t),
'command',(SELECT jsonb_agg(to_jsonb(t) ORDER BY command_hash) FROM agenteam_artifact.commands t))::text`).Scan(&value)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func projectMigrationOldConstraints(t *testing.T, ctx context.Context, conn *pgx.Conn) string {
	t.Helper()
	var value string
	// Compare definitions, not OIDs; include all existing domain/table checks,
	// FKs, uniqueness, and indexes, including those of the accepted 00013.
	err := conn.QueryRow(ctx, `SELECT jsonb_build_object(
'constraints',(SELECT jsonb_agg(jsonb_build_array(n.nspname,coalesce(c.relname,''),k.conname,pg_get_constraintdef(k.oid)) ORDER BY n.nspname,c.relname,k.conname)
 FROM pg_constraint k JOIN pg_namespace n ON n.oid=k.connamespace LEFT JOIN pg_class c ON c.oid=k.conrelid
 WHERE n.nspname LIKE 'agenteam_%' AND NOT (n.nspname IN ('agenteam_object','agenteam_artifact') AND coalesce(c.relname,'') IN ('project_work','project_stops'))),
'indexes',(SELECT jsonb_agg(jsonb_build_array(n.nspname,c.relname,ci.relname,pg_get_indexdef(i.indexrelid)) ORDER BY n.nspname,c.relname,ci.relname)
 FROM pg_index i JOIN pg_class c ON c.oid=i.indrelid JOIN pg_class ci ON ci.oid=i.indexrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname LIKE 'agenteam_%' AND NOT (n.nspname IN ('agenteam_object','agenteam_artifact') AND c.relname IN ('project_work','project_stops'))))::text`).Scan(&value)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func projectMigrationTables(t *testing.T, ctx context.Context, conn *pgx.Conn, want int) {
	t.Helper()
	var tables, indexes int
	err := conn.QueryRow(ctx, `SELECT
(SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname IN ('agenteam_object','agenteam_artifact') AND c.relname IN ('project_work','project_stops') AND c.relkind='r'),
(SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname IN ('agenteam_object','agenteam_artifact') AND c.relname IN ('object_project_work_scan','object_project_work_process','object_project_work_resource','artifact_project_work_scan','artifact_project_work_source','artifact_project_work_process') AND c.relkind='i')`).Scan(&tables, &indexes)
	if err != nil || tables != want || (want == 0 && indexes != 0) || (want == 4 && indexes != 6) {
		t.Fatalf("technical DDL: tables=%d indexes=%d want tables=%d err=%v", tables, indexes, want, err)
	}
}

func TestProjectLifecycleMigrationFreshAndPopulatedThirteen(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "populated-thirteen"
		}
		t.Run(name, func(t *testing.T) {
			ctx := projectMigrationContext(t)
			db := pgfixture.NewDatabase(t)
			var facts, constraints string
			conn := db.Connect(t)
			if upgrade {
				projectMigrate(t, ctx, projectMigrationRunner(t, db, projectMigrationFiles(t, "00013")), 13)
				projectMigrationSeed(t, ctx, conn)
				facts = projectMigrationFacts(t, ctx, conn)
				constraints = projectMigrationOldConstraints(t, ctx, conn)
			}
			runner := projectMigrationRunner(t, db, projectMigrationFiles(t, "00014"))
			projectMigrate(t, ctx, runner, 14)
			projectMigrationTables(t, ctx, conn, 4)
			if upgrade && (facts != projectMigrationFacts(t, ctx, conn) || constraints != projectMigrationOldConstraints(t, ctx, conn)) {
				t.Fatal("00014 changed existing Project/Object/Artifact facts or prior constraints/indexes")
			}
			var work, stops, journal, goose int
			err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_object.project_work)+(SELECT count(*) FROM agenteam_artifact.project_work),
(SELECT count(*) FROM agenteam_object.project_stops)+(SELECT count(*) FROM agenteam_artifact.project_stops),
(SELECT count(*) FROM agenteam_meta.migration_journal WHERE version=14 AND state='applied'),
(SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=14 AND is_applied)`).Scan(&work, &stops, &journal, &goose)
			if err != nil || work != 0 || stops != 0 || journal != 1 || goose != 1 {
				t.Fatal("migration fabricated work/stopped proof or did not apply once", work, stops, journal, goose, err)
			}
			// A second ordinary migration must not alter retained facts or DDL.
			projectMigrate(t, ctx, runner, 14)
			if upgrade && (facts != projectMigrationFacts(t, ctx, conn) || constraints != projectMigrationOldConstraints(t, ctx, conn)) {
				t.Fatal("idempotent migration changed existing facts or constraints")
			}
		})
	}
}

func projectMigrationSQLState(t *testing.T, ctx context.Context, conn *pgx.Conn, state, query string, args ...any) {
	t.Helper()
	_, err := conn.Exec(ctx, query, args...)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != state {
		t.Fatalf("expected SQLSTATE %s; got %T %v", state, err, err)
	}
}

func TestProjectLifecycleMigrationClosedStatesAndProjectIsolation(t *testing.T) {
	ctx := projectMigrationContext(t)
	db := pgfixture.NewDatabase(t)
	projectMigrate(t, ctx, projectMigrationRunner(t, db, projectMigrationFiles(t, "00014")), 14)
	conn := db.Connect(t)
	const project = "01900000-0000-7000-8000-000000000101"
	const source = "01900000-0000-7000-8000-000000000102"
	const operation = "01900000-0000-7000-8000-000000000103"
	const sourceOperation = "01900000-0000-7000-8000-000000000104"
	const work = "01900000-0000-7000-8000-000000000105"
	const process = "01900000-0000-7000-8000-000000000106"
	for _, schema := range []string{"agenteam_object", "agenteam_artifact"} {
		insert := `INSERT INTO ` + schema + `.project_stops(project_id,operation_id,action,project_version) VALUES($1,$2,'archive',7),($3,$4,'delete',8)`
		if _, err := conn.Exec(ctx, insert, project, operation, source, sourceOperation); err != nil {
			t.Fatal(err)
		}
		max := 4
		if schema == "agenteam_artifact" {
			max = 3
		}
		for kind := 0; kind <= max; kind++ {
			if _, err := conn.Exec(ctx, `UPDATE `+schema+`.project_stops SET scan_kind=$1 WHERE project_id=$2`, kind, project); err != nil {
				t.Fatalf("valid %s scan kind %d: %v", schema, kind, err)
			}
		}
		// 5 and 6 used to pass Object's CHECK despite no matching scanner.
		for _, kind := range []int{-1, max + 1, 5, 6} {
			projectMigrationSQLState(t, ctx, conn, "23514", `UPDATE `+schema+`.project_stops SET scan_kind=$1 WHERE project_id=$2`, kind, project)
		}
		for _, assignment := range []string{
			`action='restore'`, `project_version=0`, `state='unknown'`,
			`state='stopped'`, `stopped_at=clock_timestamp()`,
			`state='stopped',stopped_at=clock_timestamp(),scan_kind=1`,
			`state='stopped',stopped_at=clock_timestamp(),scan_kind=0,scan_after=operation_id`,
		} {
			projectMigrationSQLState(t, ctx, conn, "23514", `UPDATE `+schema+`.project_stops SET `+assignment+` WHERE project_id=$1`, project)
		}
		projectMigrationSQLState(t, ctx, conn, "23505", `INSERT INTO `+schema+`.project_stops(project_id,operation_id,action,project_version) VALUES($1,$2,'archive',7)`, project, sourceOperation)
		kind := "source"
		if schema == "agenteam_artifact" {
			kind = "creation"
		}
		if _, err := conn.Exec(ctx, `INSERT INTO `+schema+`.project_work(id,project_id,process_id,kind,resource_id,admission_version) VALUES($1,$2,$3,$4,$1,0)`, work, project, process, kind); err != nil {
			t.Fatal(err)
		}
		for _, assignment := range []string{`admission_version=-1`, `admission_version=7`, `admission_operation=resource_id`, `kind='unbound'`} {
			projectMigrationSQLState(t, ctx, conn, "23514", `UPDATE `+schema+`.project_work SET `+assignment+` WHERE id=$1`, work)
		}
		projectMigrationSQLState(t, ctx, conn, "23502", `UPDATE `+schema+`.project_work SET process_id=NULL WHERE id=$1`, work)
		projectMigrationSQLState(t, ctx, conn, "23514", `UPDATE `+schema+`.project_work SET process_id='01900000-0000-4000-8000-000000000106' WHERE id=$1`, work)
		projectMigrationSQLState(t, ctx, conn, "23503", `UPDATE `+schema+`.project_work SET revoked_by=$1 WHERE id=$2`, sourceOperation, work)
		if _, err := conn.Exec(ctx, `UPDATE `+schema+`.project_work SET admission_version=7,admission_operation=$1,revoked_by=$1 WHERE id=$2`, operation, work); err != nil {
			t.Fatal(err)
		}
		// Receipts cannot cascade away an unresolved technical work record.
		projectMigrationSQLState(t, ctx, conn, "23503", `DELETE FROM `+schema+`.project_stops WHERE project_id=$1`, project)
		if _, err := conn.Exec(ctx, `UPDATE `+schema+`.project_stops SET state='stopped',stopped_at=clock_timestamp(),scan_kind=0,scan_after=NULL WHERE project_id=$1`, project); err != nil {
			t.Fatal(err)
		}
	}
	// Source-only stop is a distinct relation. Its join does not fabricate a
	// target join, and a stop from another Project cannot be substituted.
	projectMigrationSQLState(t, ctx, conn, "23514", `UPDATE agenteam_artifact.project_work SET source_lease_id=$1 WHERE id=$2`, process, work)
	projectMigrationSQLState(t, ctx, conn, "23514", `UPDATE agenteam_artifact.project_work SET source_joined_at=clock_timestamp() WHERE id=$1`, work)
	projectMigrationSQLState(t, ctx, conn, "23514", `UPDATE agenteam_artifact.project_work SET command_hash=decode('01','hex') WHERE id=$1`, work)
	projectMigrationSQLState(t, ctx, conn, "23503", `UPDATE agenteam_artifact.project_work SET source_project_id=$1,source_revoked_by=$2 WHERE id=$3`, source, operation, work)
	if _, err := conn.Exec(ctx, `UPDATE agenteam_artifact.project_work SET source_project_id=$1,source_revoked_by=$2,source_lease_id=$3,source_joined_at=clock_timestamp() WHERE id=$4`, source, sourceOperation, process, work); err != nil {
		t.Fatal(err)
	}
	var targetLive, sourceJoined bool
	if err := conn.QueryRow(ctx, `SELECT joined_at IS NULL,source_joined_at IS NOT NULL FROM agenteam_artifact.project_work WHERE id=$1`, work).Scan(&targetLive, &sourceJoined); err != nil || !targetLive || !sourceJoined {
		t.Fatal("source join incorrectly implied target join", err)
	}
	projectMigrationSQLState(t, ctx, conn, "23503", `DELETE FROM agenteam_artifact.project_stops WHERE project_id=$1`, source)
	if _, err := conn.Exec(ctx, `DELETE FROM agenteam_artifact.project_work WHERE id=$1`, work); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `DELETE FROM agenteam_artifact.project_stops WHERE project_id=$1`, source); err != nil {
		t.Fatal("retired source relation prevented receipt cleanup", err)
	}
}

func TestProjectLifecycleMigrationAtomicDDLandJournalRetry(t *testing.T) {
	ctx := projectMigrationContext(t)
	db := pgfixture.NewDatabase(t)
	files := projectMigrationFiles(t, "00013")
	projectMigrate(t, ctx, projectMigrationRunner(t, db, files), 13)
	conn := db.Connect(t)
	projectMigrationSeed(t, ctx, conn)
	facts := projectMigrationFacts(t, ctx, conn)
	constraints := projectMigrationOldConstraints(t, ctx, conn)
	raw, err := fs.ReadFile(migrations.SQL, projectStopMigration)
	if err != nil {
		t.Fatal(err)
	}
	// Only this private migration Source appends a deliberate dependency
	// failure. Production 00014 bytes and its ordinary upgrade stay unchanged.
	files[projectStopMigration] = &fstest.MapFile{Data: append(raw, []byte("\nSELECT project_stop_fixture_migration_dependency();\n")...)}
	runner := projectMigrationRunner(t, db, files)
	result := runner.Migrate(ctx)
	var pg *pgconn.PgError
	if result.Migrated || !errors.As(result.Fault, &pg) || pg.Code != "42883" {
		t.Fatalf("injected DDL dependency did not fail as expected: migrated=%v fault=%v", result.Migrated, result.Fault)
	}
	projectMigrationTables(t, ctx, conn, 0)
	var pending, applied, goose, maxVersion int
	var pendingChecksum string
	err = conn.QueryRow(ctx, `SELECT
(SELECT count(*) FROM agenteam_meta.migration_journal WHERE version=14 AND state='pending'),
(SELECT count(*) FROM agenteam_meta.migration_journal WHERE version=14 AND state='applied'),
(SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=14 AND is_applied),
(SELECT max(version_id) FROM agenteam_meta.goose_db_version WHERE is_applied),
(SELECT checksum FROM agenteam_meta.migration_journal WHERE version=14)`).Scan(&pending, &applied, &goose, &maxVersion, &pendingChecksum)
	if err != nil || pending != 1 || applied != 0 || goose != 0 || maxVersion != 13 {
		t.Fatal("failed DDL/journal did not roll back", pending, applied, goose, maxVersion, err)
	}
	if facts != projectMigrationFacts(t, ctx, conn) || constraints != projectMigrationOldConstraints(t, ctx, conn) {
		t.Fatal("failed migration changed prior facts or constraints")
	}
	if _, err = conn.Exec(ctx, `CREATE FUNCTION public.project_stop_fixture_migration_dependency() RETURNS void LANGUAGE SQL AS 'SELECT NULL::void'`); err != nil {
		t.Fatal(err)
	}
	// Retry the exact same source, checksum, Migrator and journal entry. Do not
	// replace the pending migration with the production checksum after failure.
	projectMigrate(t, ctx, runner, 14)
	projectMigrationTables(t, ctx, conn, 4)
	var checksum string
	err = conn.QueryRow(ctx, `SELECT
(SELECT count(*) FROM agenteam_meta.migration_journal WHERE version=14 AND state='applied'),
(SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=14 AND is_applied),
(SELECT checksum FROM agenteam_meta.migration_journal WHERE version=14)`).Scan(&applied, &goose, &checksum)
	if err != nil || applied != 1 || goose != 1 || checksum != pendingChecksum {
		t.Fatal("same-byte retry did not apply the original pending checksum once", err)
	}
	if facts != projectMigrationFacts(t, ctx, conn) || constraints != projectMigrationOldConstraints(t, ctx, conn) {
		t.Fatal("same-byte retry changed prior facts or constraints")
	}
	t.Log("four technical tables and six indexes recovered at version 14; prior facts/constraints retained")
}
