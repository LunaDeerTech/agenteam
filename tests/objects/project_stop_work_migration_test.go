//go:build integration

package objects_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/LunaDeerTech/agenteam/db/migrations"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	workMigrationName      = "00016_object_project_work_maintenance.sql"
	workMigrationProject   = "01900000-0000-7000-8000-000000000001"
	workMigrationSource    = "01900000-0000-7000-8000-000000000301"
	workMigrationOperation = "01900000-0000-7000-8000-000000000302"
	workMigrationSourceOp  = "01900000-0000-7000-8000-000000000303"
	workMigrationProcess   = "01900000-0000-7000-8000-000000000304"
	workMigrationObject    = "01900000-0000-7000-8000-000000000305"
	workMigrationAttempt   = "01900000-0000-7000-8000-000000000307"
	workMigrationCleanup   = "01900000-0000-7000-8000-000000000308"
	workMigrationWorker    = "01900000-0000-7000-8000-000000000309"
)

func workMigrationID(n int) string {
	return fmt.Sprintf("01900000-0000-7000-8000-%012d", n)
}

func workMigrationExec(t *testing.T, ctx context.Context, conn *pgx.Conn, query string, args ...any) {
	t.Helper()
	if _, err := conn.Exec(ctx, query, args...); err != nil {
		t.Fatal("work migration fixture statement", err)
	}
}

// These are schema fixtures, not claims that an Object worker or Project
// lifecycle has run. In particular an old applying cleanup has no new work
// record from which its actual executing process could be reconstructed.
func workMigrationSeed(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	projectMigrationSeed(t, ctx, conn)
	workMigrationExec(t, ctx, conn, `BEGIN;
INSERT INTO agenteam_object.project_stops(project_id,operation_id,action,project_version,state,created_at,stopped_at)
VALUES('01900000-0000-7000-8000-000000000001','01900000-0000-7000-8000-000000000302','archive',8,'stopped','2026-01-01 00:00:00+00','2026-01-02 00:00:00+00');
INSERT INTO agenteam_object.project_stops(project_id,operation_id,action,project_version,scan_kind,scan_after)
VALUES('01900000-0000-7000-8000-000000000301','01900000-0000-7000-8000-000000000303','delete',9,3,'01900000-0000-7000-8000-000000000312');
INSERT INTO agenteam_artifact.project_stops(project_id,operation_id,action,project_version)
VALUES('01900000-0000-7000-8000-000000000001','01900000-0000-7000-8000-000000000302','archive',8),
('01900000-0000-7000-8000-000000000301','01900000-0000-7000-8000-000000000303','delete',9);
INSERT INTO agenteam_object.objects(id,scope,partition_id,project_id,media_type,byte_size,sha256,state,version)
VALUES('01900000-0000-7000-8000-000000000305','project','01900000-0000-7000-8000-000000000001','01900000-0000-7000-8000-000000000001','text/plain',4,decode(repeat('0',64),'hex'),'failed',2);
INSERT INTO agenteam_object.uploads(id,object_id,command_hash,command_key,semantic_digest,owner_kind,owner_id,project_id,stable_actor,initiator_kind,initiator_id,existence,state,disposition,receipt_id)
VALUES('01900000-0000-7000-8000-000000000306','01900000-0000-7000-8000-000000000305',decode(repeat('3',64),'hex'),'maintenance-migration',decode(repeat('4',64),'hex'),'artifact','01900000-0000-7000-8000-000000000006','01900000-0000-7000-8000-000000000001','migration-fixture','human','01900000-0000-7000-8000-000000000002','existing','failed','revoked','01900000-0000-7000-8000-000000000313');
INSERT INTO agenteam_object.upload_attempts(id,upload_id,object_id,ordinal,candidate_key,phase,process_id,spool_payload_id,io_closed,cleanup_gate,byte_size,sha256)
VALUES('01900000-0000-7000-8000-000000000307','01900000-0000-7000-8000-000000000306','01900000-0000-7000-8000-000000000305',1,'candidate/01900000-0000-7000-8000-000000000307','abandoned','01900000-0000-7000-8000-000000000009','01900000-0000-7000-8000-000000000310',true,true,4,decode(repeat('0',64),'hex'));
UPDATE agenteam_object.uploads SET current_attempt_id='01900000-0000-7000-8000-000000000307' WHERE id='01900000-0000-7000-8000-000000000306';
INSERT INTO agenteam_object.cleanup_operations(id,operation_id,object_id,attempt_id,reason,mode,phase,worker_id,fence)
VALUES('01900000-0000-7000-8000-000000000308','01900000-0000-7000-8000-000000000314','01900000-0000-7000-8000-000000000305','01900000-0000-7000-8000-000000000307','abandoned_attempt','zero_marker','applying','01900000-0000-7000-8000-000000000309',3);
INSERT INTO agenteam_object.objects(id,scope,partition_id,project_id,media_type,byte_size,sha256,state,candidate_key)
VALUES('01900000-0000-7000-8000-000000000311','project','01900000-0000-7000-8000-000000000301','01900000-0000-7000-8000-000000000301','text/plain',4,decode(repeat('0',64),'hex'),'available','candidate/01900000-0000-7000-8000-000000000311');
INSERT INTO agenteam_object.object_leases(id,object_id,owner_kind,owner_id,process_id,state)
VALUES('01900000-0000-7000-8000-000000000312','01900000-0000-7000-8000-000000000311','source','01900000-0000-7000-8000-000000000315','01900000-0000-7000-8000-000000000304','active');
INSERT INTO agenteam_artifact.project_work(id,project_id,process_id,kind,resource_id,command_hash,source_project_id,source_lease_id,admission_version,source_revoked_by)
VALUES('01900000-0000-7000-8000-000000000330','01900000-0000-7000-8000-000000000001','01900000-0000-7000-8000-000000000304','creation','01900000-0000-7000-8000-000000000010',decode(repeat('5',64),'hex'),'01900000-0000-7000-8000-000000000301','01900000-0000-7000-8000-000000000312',0,'01900000-0000-7000-8000-000000000303');
COMMIT;`)
	for i, kind := range []string{"preparation", "reader", "source", "download", "transfer_get", "transfer_put"} {
		var object any
		if i != 0 {
			object = workMigrationObject
		}
		workMigrationExec(t, ctx, conn, `INSERT INTO agenteam_object.project_work
(id,project_id,process_id,kind,resource_id,object_id,admission_version,admission_operation,revoked_by,created_at,joined_at)
VALUES($1,$2,$3,$4,$1,$5,CASE WHEN $6::boolean THEN 8 ELSE 0 END,
CASE WHEN $6 THEN $7::agenteam_object.safe_id ELSE NULL END,
CASE WHEN $6 THEN $7::agenteam_object.safe_id ELSE NULL END,'2026-01-01 00:00:00+00',
CASE WHEN $6 THEN '2026-01-02 00:00:00+00'::timestamptz ELSE NULL END)`,
			workMigrationID(320+i), workMigrationProject, workMigrationProcess, kind, object, i%2 != 0, workMigrationOperation)
	}
}

func workMigrationFacts(t *testing.T, ctx context.Context, conn *pgx.Conn) string {
	t.Helper()
	var value string
	err := conn.QueryRow(ctx, `SELECT jsonb_build_object(
'object_work',(SELECT jsonb_agg(to_jsonb(t)-'cleanup_claim_fence' ORDER BY id) FROM agenteam_object.project_work t),
'object_stops',(SELECT jsonb_agg(to_jsonb(t) ORDER BY project_id,operation_id) FROM agenteam_object.project_stops t),
'artifact_work',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_artifact.project_work t),
'artifact_stops',(SELECT jsonb_agg(to_jsonb(t) ORDER BY project_id,operation_id) FROM agenteam_artifact.project_stops t),
'uploads',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_object.uploads t),
'attempts',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_object.upload_attempts t),
'cleanup',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_object.cleanup_operations t))::text`).Scan(&value)
	if err != nil {
		t.Fatal("snapshot work/native facts", err)
	}
	return projectMigrationFacts(t, ctx, conn) + "\n" + value
}

// Unlike projectMigrationOldConstraints, this includes both domains' work and
// stop tables, domains, columns and indexes. The upgrade comparison excludes
// only 00016's five named changes; the failure comparison excludes nothing.
func workMigrationCatalog(t *testing.T, ctx context.Context, conn *pgx.Conn, excludeDelta bool) string {
	t.Helper()
	var value string
	err := conn.QueryRow(ctx, `SELECT jsonb_build_object(
'constraints',(SELECT jsonb_agg(jsonb_build_array(n.nspname,coalesce(c.relname,''),k.conname,k.convalidated,pg_get_constraintdef(k.oid)) ORDER BY n.nspname,c.relname,k.conname)
 FROM pg_constraint k JOIN pg_namespace n ON n.oid=k.connamespace LEFT JOIN pg_class c ON c.oid=k.conrelid
 WHERE n.nspname LIKE 'agenteam_%' AND NOT ($1 AND n.nspname='agenteam_object' AND coalesce(c.relname,'')='project_work'
 AND k.conname IN ('project_work_kind_check','project_work_cleanup_claim_check','project_work_maintenance_object_check'))),
'indexes',(SELECT jsonb_agg(jsonb_build_array(n.nspname,c.relname,ci.relname,i.indisvalid,i.indisready,pg_get_indexdef(i.indexrelid)) ORDER BY n.nspname,c.relname,ci.relname)
 FROM pg_index i JOIN pg_class c ON c.oid=i.indrelid JOIN pg_class ci ON ci.oid=i.indexrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname LIKE 'agenteam_%' AND NOT ($1 AND n.nspname='agenteam_object' AND c.relname='project_work' AND ci.relname='object_project_work_cleanup_claim')),
'columns',(SELECT jsonb_agg(jsonb_build_array(n.nspname,c.relname,c.relkind,a.attnum,a.attname,format_type(a.atttypid,a.atttypmod),a.attnotnull,a.attidentity,a.attgenerated,pg_get_expr(d.adbin,d.adrelid)) ORDER BY n.nspname,c.relname,a.attnum)
 FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
 WHERE n.nspname LIKE 'agenteam_%' AND c.relkind IN ('r','p') AND a.attnum>0 AND NOT a.attisdropped
 AND NOT ($1 AND n.nspname='agenteam_object' AND c.relname='project_work' AND a.attname='cleanup_claim_fence')))::text`, excludeDelta).Scan(&value)
	if err != nil {
		t.Fatal("snapshot full migration catalog", err)
	}
	return value
}

func workMigrationShape(t *testing.T, ctx context.Context, conn *pgx.Conn, installed bool) {
	t.Helper()
	var columns, checks, indexes int
	var kind string
	err := conn.QueryRow(ctx, `SELECT
(SELECT count(*) FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
 WHERE a.attrelid='agenteam_object.project_work'::regclass AND a.attname='cleanup_claim_fence' AND NOT a.attisdropped
 AND a.atttypid='bigint'::regtype AND NOT a.attnotnull AND d.oid IS NULL),
(SELECT count(*) FROM pg_constraint WHERE conrelid='agenteam_object.project_work'::regclass
 AND conname IN ('project_work_cleanup_claim_check','project_work_maintenance_object_check') AND contype='c' AND convalidated),
(SELECT count(*) FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE i.indrelid='agenteam_object.project_work'::regclass
 AND c.relname='object_project_work_cleanup_claim' AND i.indisunique AND i.indisvalid AND i.indisready AND i.indpred IS NOT NULL),
(SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='agenteam_object.project_work'::regclass AND conname='project_work_kind_check' AND convalidated)`).Scan(&columns, &checks, &indexes, &kind)
	want := 0
	if installed {
		want = 1
	}
	if err != nil || columns != want || checks != 2*want || indexes != want || strings.Contains(kind, "'verification'") != installed || strings.Contains(kind, "'cleanup'") != installed {
		t.Fatalf("maintenance schema: columns=%d checks=%d indexes=%d installed=%v err=%v", columns, checks, indexes, installed, err)
	}
}

func workMigrationJournal(t *testing.T, ctx context.Context, conn *pgx.Conn, applied bool, checksum string) {
	t.Helper()
	var count, goose, version int
	var state, actual string
	err := conn.QueryRow(ctx, `SELECT
(SELECT count(*) FROM agenteam_meta.migration_journal WHERE version=16),
(SELECT state FROM agenteam_meta.migration_journal WHERE version=16 AND filename=$1 AND mode='tx'),
(SELECT checksum FROM agenteam_meta.migration_journal WHERE version=16),
(SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=16 AND is_applied),
(SELECT max(version_id) FROM agenteam_meta.goose_db_version WHERE is_applied)`, workMigrationName).Scan(&count, &state, &actual, &goose, &version)
	wantState, wantGoose, wantVersion := "pending", 0, 15
	if applied {
		wantState, wantGoose, wantVersion = "applied", 1, 16
	}
	if err != nil || count != 1 || state != wantState || actual != checksum || goose != wantGoose || version != wantVersion {
		t.Fatalf("migration journal: count=%d state=%s checksum_matches=%v goose=%d version=%d err=%v", count, state, actual == checksum, goose, version, err)
	}
}

func TestObjectProjectStopWorkMigrationFreshAndPopulatedFifteen(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "populated-fifteen"
		}
		t.Run(name, func(t *testing.T) {
			ctx := projectMigrationContext(t)
			db := pgfixture.NewDatabase(t)
			conn := db.Connect(t)
			var facts, catalog string
			if upgrade {
				projectMigrate(t, ctx, projectMigrationRunner(t, db, projectMigrationFiles(t, "00015")), 15)
				workMigrationSeed(t, ctx, conn)
				facts = workMigrationFacts(t, ctx, conn)
				catalog = workMigrationCatalog(t, ctx, conn, true)
			}
			files := projectMigrationFiles(t, "00016")
			runner := projectMigrationRunner(t, db, files)
			projectMigrate(t, ctx, runner, 16)
			workMigrationShape(t, ctx, conn, true)
			if upgrade && (facts != workMigrationFacts(t, ctx, conn) || catalog != workMigrationCatalog(t, ctx, conn, true)) {
				t.Fatal("00016 changed prior facts, work/stops, or unrelated catalog")
			}
			var work, newFences int
			if err := conn.QueryRow(ctx, `SELECT count(*),count(cleanup_claim_fence) FROM agenteam_object.project_work`).Scan(&work, &newFences); err != nil || newFences != 0 || upgrade && work != 6 || !upgrade && work != 0 {
				t.Fatal("upgrade fabricated work or cleanup claim evidence", work, newFences, err)
			}
			checksum := fmt.Sprintf("sha256:%x", sha256.Sum256(files[workMigrationName].Data))
			workMigrationJournal(t, ctx, conn, true, checksum)
			facts, catalog = workMigrationFacts(t, ctx, conn), workMigrationCatalog(t, ctx, conn, false)
			projectMigrate(t, ctx, runner, 16)
			workMigrationJournal(t, ctx, conn, true, checksum)
			if facts != workMigrationFacts(t, ctx, conn) || catalog != workMigrationCatalog(t, ctx, conn, false) {
				t.Fatal("repeated migration changed retained data or installed schema")
			}
		})
	}
}

const workMigrationInsert = `INSERT INTO agenteam_object.project_work
(id,project_id,process_id,kind,resource_id,object_id,admission_version,cleanup_claim_fence)
VALUES($1,$2,$3,$4,$5,$6,0,$7)`

func workMigrationRow(t *testing.T, ctx context.Context, conn *pgx.Conn, id string) string {
	t.Helper()
	var row string
	if err := conn.QueryRow(ctx, `SELECT to_jsonb(w)::text FROM agenteam_object.project_work w WHERE id=$1`, id).Scan(&row); err != nil {
		t.Fatal("read exact work identity", err)
	}
	return row
}

func TestObjectProjectStopWorkMigrationMaintenanceConstraints(t *testing.T) {
	ctx := projectMigrationContext(t)
	db := pgfixture.NewDatabase(t)
	projectMigrate(t, ctx, projectMigrationRunner(t, db, projectMigrationFiles(t, "00016")), 16)
	conn := db.Connect(t)
	workMigrationSeed(t, ctx, conn)
	for i := range 6 {
		workMigrationExec(t, ctx, conn, `UPDATE agenteam_object.project_work SET object_id=NULL WHERE id=$1`, workMigrationID(320+i))
	}
	verifier := workMigrationID(340)
	workMigrationExec(t, ctx, conn, workMigrationInsert, verifier, workMigrationProject, workMigrationProcess, "verification", workMigrationAttempt, workMigrationObject, nil)
	workMigrationExec(t, ctx, conn, workMigrationInsert, workMigrationWorker, workMigrationProject, workMigrationProcess, "cleanup", workMigrationCleanup, workMigrationObject, int64(3))
	verificationBefore := workMigrationRow(t, ctx, conn, verifier)
	cleanupBefore := workMigrationRow(t, ctx, conn, workMigrationWorker)
	for _, fence := range []any{nil, int64(0), int64(-1)} {
		projectMigrationSQLState(t, ctx, conn, "23514", `UPDATE agenteam_object.project_work SET cleanup_claim_fence=$1 WHERE id=$2`, fence, workMigrationWorker)
	}
	for _, kind := range []string{"preparation", "reader", "source", "download", "transfer_get", "transfer_put", "verification"} {
		projectMigrationSQLState(t, ctx, conn, "23514", `UPDATE agenteam_object.project_work SET kind=$1,cleanup_claim_fence=1 WHERE id=$2`, kind, verifier)
	}
	for _, id := range []string{verifier, workMigrationWorker} {
		projectMigrationSQLState(t, ctx, conn, "23514", `UPDATE agenteam_object.project_work SET object_id=NULL WHERE id=$1`, id)
	}
	projectMigrationSQLState(t, ctx, conn, "23514", `UPDATE agenteam_object.project_work SET kind='invented' WHERE id=$1`, verifier)
	// The same worker cannot be inserted with another resource, fence or
	// process. S2 must also reject identity changes through UPDATE/replay.
	for _, change := range []struct {
		resource, process string
		fence             int64
	}{
		{workMigrationID(341), workMigrationProcess, 3},
		{workMigrationCleanup, workMigrationProcess, 4},
		{workMigrationCleanup, workMigrationID(342), 3},
	} {
		projectMigrationSQLState(t, ctx, conn, "23505", workMigrationInsert, workMigrationWorker, workMigrationProject, change.process, "cleanup", change.resource, workMigrationObject, change.fence)
	}
	projectMigrationSQLState(t, ctx, conn, "23505", workMigrationInsert, workMigrationID(343), workMigrationProject, workMigrationProcess, "cleanup", workMigrationCleanup, workMigrationObject, int64(3))
	if cleanupBefore != workMigrationRow(t, ctx, conn, workMigrationWorker) || verificationBefore != workMigrationRow(t, ctx, conn, verifier) {
		t.Fatal("rejected identity/constraint changes mutated an existing work")
	}

	// A new native claim can overwrite its current worker, while the old work
	// remains recoverable. No join/death is inferred by this schema test.
	newWorker, newProcess := workMigrationID(344), workMigrationID(345)
	workMigrationExec(t, ctx, conn, workMigrationInsert, newWorker, workMigrationProject, newProcess, "cleanup", workMigrationCleanup, workMigrationObject, int64(4))
	workMigrationExec(t, ctx, conn, `UPDATE agenteam_object.cleanup_operations SET worker_id=$1,fence=4 WHERE id=$2`, newWorker, workMigrationCleanup)
	if cleanupBefore != workMigrationRow(t, ctx, conn, workMigrationWorker) {
		t.Fatal("reclaim replaced the old worker/fence/process evidence")
	}
	var currentMatches bool
	var liveClaims int
	if err := conn.QueryRow(ctx, `SELECT
(SELECT worker_id=$1 AND fence=4 FROM agenteam_object.cleanup_operations WHERE id=$2),
(SELECT count(*) FROM agenteam_object.project_work WHERE kind='cleanup' AND resource_id=$2 AND joined_at IS NULL)`, newWorker, workMigrationCleanup).Scan(&currentMatches, &liveClaims); err != nil || !currentMatches || liveClaims != 2 {
		t.Fatal("new and old claims did not remain distinct", currentMatches, liveClaims, err)
	}
	workMigrationExec(t, ctx, conn, `UPDATE agenteam_object.project_work SET joined_at=clock_timestamp() WHERE id=$1`, workMigrationWorker)
	projectMigrationSQLState(t, ctx, conn, "23505", workMigrationInsert, workMigrationID(346), workMigrationProject, newProcess, "cleanup", workMigrationCleanup, workMigrationObject, int64(3))
	projectMigrationSQLState(t, ctx, conn, "23505", workMigrationInsert, workMigrationWorker, workMigrationProject, newProcess, "cleanup", workMigrationID(347), workMigrationObject, int64(5))
	// Verification can repeat with a distinct work identity without abusing a
	// cleanup fence. Both original verifier and old six kinds remain legal.
	workMigrationExec(t, ctx, conn, workMigrationInsert, workMigrationID(348), workMigrationProject, newProcess, "verification", workMigrationAttempt, workMigrationObject, nil)
	if verificationBefore != workMigrationRow(t, ctx, conn, verifier) {
		t.Fatal("new verifier changed the original verifier identity")
	}
}

func TestObjectProjectStopWorkMigrationPreservesPriorConstraints(t *testing.T) {
	ctx := projectMigrationContext(t)
	db := pgfixture.NewDatabase(t)
	projectMigrate(t, ctx, projectMigrationRunner(t, db, projectMigrationFiles(t, "00016")), 16)
	conn := db.Connect(t)
	workMigrationSeed(t, ctx, conn)
	for _, schema := range []string{"agenteam_object", "agenteam_artifact"} {
		work := workMigrationID(320)
		if schema == "agenteam_artifact" {
			work = workMigrationID(330)
		}
		for _, assignment := range []string{`admission_version=-1`, `admission_version=8`, `admission_operation=resource_id`} {
			projectMigrationSQLState(t, ctx, conn, "23514", `UPDATE `+schema+`.project_work SET `+assignment+` WHERE id=$1`, work)
		}
		for _, column := range []string{"id", "project_id", "process_id", "resource_id"} {
			projectMigrationSQLState(t, ctx, conn, "23514", `UPDATE `+schema+`.project_work SET `+column+`='01900000-0000-4000-8000-000000000399' WHERE id=$1`, work)
			projectMigrationSQLState(t, ctx, conn, "23502", `UPDATE `+schema+`.project_work SET `+column+`=NULL WHERE id=$1`, work)
		}
		projectMigrationSQLState(t, ctx, conn, "23503", `UPDATE `+schema+`.project_work SET revoked_by=$1 WHERE id=$2`, workMigrationSourceOp, work)
		workMigrationExec(t, ctx, conn, `UPDATE `+schema+`.project_work SET admission_version=8,admission_operation=$1,revoked_by=$1 WHERE id=$2`, workMigrationOperation, work)
		projectMigrationSQLState(t, ctx, conn, "23503", `DELETE FROM `+schema+`.project_stops WHERE project_id=$1`, workMigrationProject)

		max := 4
		if schema == "agenteam_artifact" {
			max = 3
		}
		for kind := 0; kind <= max; kind++ {
			workMigrationExec(t, ctx, conn, `UPDATE `+schema+`.project_stops SET scan_kind=$1 WHERE project_id=$2`, kind, workMigrationSource)
		}
		for _, assignment := range []string{
			`action='restore'`, `project_version=0`, `state='unknown'`, `scan_kind=-1`, fmt.Sprintf("scan_kind=%d", max+1),
			`state='stopped'`, `stopped_at=clock_timestamp()`,
			`state='stopped',stopped_at=clock_timestamp(),scan_kind=1`,
			`state='stopped',stopped_at=clock_timestamp(),scan_kind=0,scan_after=operation_id`,
		} {
			projectMigrationSQLState(t, ctx, conn, "23514", `UPDATE `+schema+`.project_stops SET `+assignment+` WHERE project_id=$1`, workMigrationSource)
		}
		projectMigrationSQLState(t, ctx, conn, "23505", `INSERT INTO `+schema+`.project_stops(project_id,operation_id,action,project_version) VALUES($1,$2,'delete',9)`, workMigrationSource, workMigrationID(350))
		workMigrationExec(t, ctx, conn, `UPDATE `+schema+`.project_stops SET state='stopped',stopped_at=clock_timestamp(),scan_kind=0,scan_after=NULL WHERE project_id=$1`, workMigrationSource)
	}
	for _, kind := range []string{"verification", "cleanup", "reader", "invented"} {
		projectMigrationSQLState(t, ctx, conn, "23514", `UPDATE agenteam_artifact.project_work SET kind=$1 WHERE id=$2`, kind, workMigrationID(330))
	}
	for _, kind := range []string{"upload", "creation"} {
		workMigrationExec(t, ctx, conn, `UPDATE agenteam_artifact.project_work SET kind=$1 WHERE id=$2`, kind, workMigrationID(330))
	}
	for _, assignment := range []string{`source_project_id=NULL`, `command_hash=decode('01','hex')`} {
		projectMigrationSQLState(t, ctx, conn, "23514", `UPDATE agenteam_artifact.project_work SET `+assignment+` WHERE id=$1`, workMigrationID(330))
	}
	projectMigrationSQLState(t, ctx, conn, "23503", `UPDATE agenteam_artifact.project_work SET source_revoked_by=$1 WHERE id=$2`, workMigrationOperation, workMigrationID(330))
	workMigrationExec(t, ctx, conn, `UPDATE agenteam_artifact.project_work SET source_joined_at=clock_timestamp() WHERE id=$1`, workMigrationID(330))
	var separateJoins bool
	if err := conn.QueryRow(ctx, `SELECT joined_at IS NULL AND source_joined_at IS NOT NULL FROM agenteam_artifact.project_work WHERE id=$1`, workMigrationID(330)).Scan(&separateJoins); err != nil || !separateJoins {
		t.Fatal("Artifact source join changed the target join", err)
	}
	projectMigrationSQLState(t, ctx, conn, "23503", `DELETE FROM agenteam_artifact.project_stops WHERE project_id=$1`, workMigrationSource)
	workMigrationExec(t, ctx, conn, `DELETE FROM agenteam_artifact.project_work WHERE id=$1`, workMigrationID(330))
	workMigrationExec(t, ctx, conn, `DELETE FROM agenteam_artifact.project_stops WHERE project_id=$1`, workMigrationSource)
}

func TestObjectProjectStopWorkMigrationAtomicFailureAndRetry(t *testing.T) {
	ctx := projectMigrationContext(t)
	db := pgfixture.NewDatabase(t)
	files := projectMigrationFiles(t, "00015")
	projectMigrate(t, ctx, projectMigrationRunner(t, db, files), 15)
	conn := db.Connect(t)
	workMigrationSeed(t, ctx, conn)
	facts := workMigrationFacts(t, ctx, conn)
	catalog := workMigrationCatalog(t, ctx, conn, false)
	stableCatalog := workMigrationCatalog(t, ctx, conn, true)
	raw, err := fs.ReadFile(migrations.SQL, workMigrationName)
	if err != nil {
		t.Fatal(err)
	}
	// Keep this failed source/checksum for the retry. Production SQL is never
	// replaced or edited, and the independent normal upgrade runs above.
	failedSource := append(raw, []byte("\nSELECT object_work_fixture_migration_dependency();\n")...)
	files[workMigrationName] = &fstest.MapFile{Data: failedSource}
	checksum := fmt.Sprintf("sha256:%x", sha256.Sum256(failedSource))
	runner := projectMigrationRunner(t, db, files)
	result := runner.Migrate(ctx)
	var pg *pgconn.PgError
	if result.Migrated || !errors.As(result.Fault, &pg) || pg.Code != "42883" {
		t.Fatalf("injected DDL dependency failure: migrated=%v fault=%v", result.Migrated, result.Fault)
	}
	workMigrationShape(t, ctx, conn, false)
	workMigrationJournal(t, ctx, conn, false, checksum)
	if facts != workMigrationFacts(t, ctx, conn) || catalog != workMigrationCatalog(t, ctx, conn, false) {
		t.Fatal("failed migration did not restore all old data and catalog")
	}
	for _, kind := range []string{"verification", "cleanup"} {
		projectMigrationSQLState(t, ctx, conn, "23514", `UPDATE agenteam_object.project_work SET kind=$1 WHERE id=$2`, kind, workMigrationID(320))
	}
	workMigrationExec(t, ctx, conn, `CREATE FUNCTION public.object_work_fixture_migration_dependency() RETURNS void LANGUAGE SQL AS 'SELECT NULL::void'`)
	projectMigrate(t, ctx, runner, 16)
	workMigrationShape(t, ctx, conn, true)
	workMigrationJournal(t, ctx, conn, true, checksum)
	if facts != workMigrationFacts(t, ctx, conn) || stableCatalog != workMigrationCatalog(t, ctx, conn, true) {
		t.Fatal("same-byte retry changed old facts or unrelated catalog")
	}
	installed := workMigrationCatalog(t, ctx, conn, false)
	projectMigrate(t, ctx, runner, 16)
	workMigrationJournal(t, ctx, conn, true, checksum)
	if facts != workMigrationFacts(t, ctx, conn) || installed != workMigrationCatalog(t, ctx, conn, false) {
		t.Fatal("already-applied retry changed data or schema")
	}
}
