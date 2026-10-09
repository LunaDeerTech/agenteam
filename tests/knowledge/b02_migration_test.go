//go:build integration

package knowledge_test

import (
	"errors"
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const knowledgeMigrationName = "00025_knowledge.sql"

func TestKnowledgeB02Migration(t *testing.T) {
	for _, kind := range []string{"fresh", "populated_upgrade", "rollback"} {
		t.Run(kind, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			prior := knowledgeMigrationFiles(t, "00024")
			current := knowledgeMigrationFiles(t, "00025")
			if len(current) != 25 || len(prior) != 24 {
				t.Fatal("exact continuous 00024/00025 inputs missing")
			}
			if kind == "fresh" {
				knowledgeMigrate(t, db, knowledgeMigrationSource(t, current))
				conn := db.Connect(t)
				checkKnowledgeSchema(t, conn)
				checkKnowledgeAuditClosed(t, conn)
				checkKnowledgeDocumentShape(t, conn)
				knowledgeMigrate(t, db, knowledgeMigrationSource(t, current))
				return
			}
			knowledgeMigrate(t, db, knowledgeMigrationSource(t, prior))
			conn := db.Connect(t)
			seedPreviousKnowledgeFacts(t, conn)
			before := previousKnowledgeSnapshot(t, conn)
			if kind == "populated_upgrade" {
				knowledgeMigrate(t, db, knowledgeMigrationSource(t, current))
				if previousKnowledgeSnapshot(t, conn) != before {
					t.Fatal("Knowledge upgrade changed previous rows or other closed contracts")
				}
				checkKnowledgeSchema(t, conn)
				seedPreviousKnowledgeFacts(t, conn)
				return
			}
			var checks string
			const allChecks = `SELECT string_agg(conname||pg_get_constraintdef(oid),'' ORDER BY conname) FROM pg_constraint WHERE conrelid='agenteam_audit.audit_records'::regclass`
			if err := conn.QueryRow(knowledgeContext(t), allChecks).Scan(&checks); err != nil {
				t.Fatal(err)
			}
			raw := current[knowledgeMigrationName].Data
			current[knowledgeMigrationName] = &fstest.MapFile{Data: append(append([]byte{}, raw...), []byte("\nSELECT knowledge_fixture_migration_failure();\n")...)}
			source := knowledgeMigrationSource(t, current)
			m, err := postgres.NewMigrator(db.Config(t, nil), source)
			if err != nil {
				t.Fatal(err)
			}
			result := m.Migrate(knowledgeContext(t))
			if result.Migrated || result.Fault == nil {
				t.Fatal("injected final SQL failure accepted")
			}
			var absent bool
			var after string
			if err = conn.QueryRow(knowledgeContext(t), `SELECT to_regnamespace('agenteam_knowledge') IS NULL`).Scan(&absent); err != nil || !absent {
				t.Fatal("Knowledge DDL survived rolled-back migration", err)
			}
			if err = conn.QueryRow(knowledgeContext(t), allChecks).Scan(&after); err != nil || after != checks || previousKnowledgeSnapshot(t, conn) != before {
				t.Fatal("failed Knowledge DDL changed old facts or Audit constraints", err)
			}
			var pending, applied int
			if err = conn.QueryRow(knowledgeContext(t), `SELECT (SELECT count(*) FROM agenteam_meta.migration_journal WHERE version=25 AND state='pending'),(SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=25 AND is_applied)`).Scan(&pending, &applied); err != nil || pending != 1 || applied != 0 {
				t.Fatal("migration failure journal boundary", pending, applied, err)
			}
			// Recover the very same recorded checksum, not a substituted migration.
			if _, err = conn.Exec(knowledgeContext(t), `CREATE FUNCTION public.knowledge_fixture_migration_failure() RETURNS void LANGUAGE SQL AS 'SELECT NULL::void'`); err != nil {
				t.Fatal(err)
			}
			knowledgeMigrate(t, db, source)
			if previousKnowledgeSnapshot(t, conn) != before {
				t.Fatal("same-source recovery changed previous facts")
			}
			checkKnowledgeSchema(t, conn)
		})
	}
}

func seedPreviousKnowledgeFacts(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	variable, project, user, session, record := knowledgeID(t), knowledgeID(t), knowledgeID(t), knowledgeID(t), knowledgeID(t)
	if _, err := conn.Exec(knowledgeContext(t), `INSERT INTO agenteam_projectvariable.variables
 (id,project_id,type,name,description,value,version,created_at,updated_at)
 VALUES($1,$2,'variable','ordinary_name','preserve description','preserve value',1,clock_timestamp(),clock_timestamp())`, variable, project); err != nil {
		t.Fatal("previous variable fixture rejected", err)
	}
	if _, err := conn.Exec(knowledgeContext(t), `INSERT INTO agenteam_audit.audit_records
 (id,scope,project_id,actor_kind,user_id,session_id,action,outcome,resource_kind,resource_id,metadata,semantic_digest,producer,cause_ref,ordinal)
 VALUES($1::uuid,'project',$2::uuid,'human',$3::uuid,$4::uuid,'project.variable.create','success','project_variable',$5::uuid,
 jsonb_build_object('variable_id',$5::text,'version','1','changed_fields',jsonb_build_array('created')),
 'sha256:'||repeat('1',64),'projectvariable','sha256:'||repeat('2',64),0)`, record, project, user, session, variable); err != nil {
		t.Fatal("previous variable Audit closed contract rejected", err)
	}
}
func previousKnowledgeSnapshot(t *testing.T, conn *pgx.Conn) string {
	t.Helper()
	var result string
	err := conn.QueryRow(knowledgeContext(t), `SELECT
 (SELECT coalesce(string_agg(row_to_json(v)::text,'' ORDER BY id),'') FROM agenteam_projectvariable.variables v)||
 (SELECT coalesce(string_agg(row_to_json(a)::text,'' ORDER BY id),'') FROM agenteam_audit.audit_records a)||
 (SELECT string_agg(conname||pg_get_constraintdef(oid),'' ORDER BY conname) FROM pg_constraint
 WHERE conrelid='agenteam_audit.audit_records'::regclass AND conname NOT IN
 ('audit_records_action_check','audit_records_resource_kind_check','audit_records_producer_check','audit_records_check2','audit_records_knowledge_contract'))`).Scan(&result)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func checkKnowledgeSchema(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	var tables []string
	err := conn.QueryRow(knowledgeContext(t), `SELECT array_agg(table_name::text ORDER BY table_name) FROM information_schema.tables WHERE table_schema='agenteam_knowledge' AND table_type='BASE TABLE'`).Scan(&tables)
	if err != nil || fmt.Sprint(tables) != "[command_events commands documents object_cleanup publications work_claims]" {
		t.Fatal("Knowledge exact table shape", tables, err)
	}
	var foreign int
	err = conn.QueryRow(knowledgeContext(t), `SELECT count(*) FROM pg_constraint c JOIN pg_class a ON a.oid=c.conrelid JOIN pg_namespace an ON an.oid=a.relnamespace JOIN pg_class b ON b.oid=c.confrelid JOIN pg_namespace bn ON bn.oid=b.relnamespace WHERE c.contype='f' AND an.nspname='agenteam_knowledge' AND bn.nspname<>'agenteam_knowledge'`).Scan(&foreign)
	if err != nil || foreign != 0 {
		t.Fatal("Knowledge migration crossed schema ownership", foreign, err)
	}
}

func requireKnowledgeCheck(t *testing.T, err error, constraint string) {
	t.Helper()
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23514" || pg.ConstraintName != constraint {
		t.Fatalf("expected closed CHECK %s; got %T", constraint, err)
	}
}
func checkKnowledgeAuditClosed(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	project, user, session, root := knowledgeID(t), knowledgeID(t), knowledgeID(t), knowledgeID(t)
	insert := `INSERT INTO agenteam_audit.audit_records
 (id,scope,project_id,actor_kind,user_id,session_id,action,outcome,resource_kind,resource_id,metadata,semantic_digest,producer,cause_ref,ordinal)
 VALUES($1::uuid,'project',$2::uuid,'human',$3::uuid,$4::uuid,'knowledge.delete_subtree','success','knowledge_document',$5::uuid,
 jsonb_build_object('project_id',$2::text,'root_id',$5::text,'initiator_id',$3::text,'scope_digest','sha256:'||repeat('3',64),'deleted_count','2'),
 'sha256:'||repeat('4',64),'knowledge','sha256:'||repeat('5',64),0)`
	record := knowledgeID(t)
	if _, err := conn.Exec(knowledgeContext(t), insert, record, project, user, session, root); err != nil {
		t.Fatal("valid Knowledge Audit fixture rejected", err)
	}
	for _, field := range []string{"tool_id", "execution_id", "tool_call_id", "operation_id", "request_id", "approval_id", "runner_id", "correlation_id", "http_trace_id"} {
		_, err := conn.Exec(knowledgeContext(t), `UPDATE agenteam_audit.audit_records SET `+pgx.Identifier{field}.Sanitize()+`=$2::uuid WHERE id=$1::uuid`, record, knowledgeID(t))
		requireKnowledgeCheck(t, err, "audit_records_knowledge_contract")
	}
	for _, change := range []string{
		`metadata=metadata-'scope_digest'`,
		`metadata=jsonb_set(metadata,'{deleted_count}','"0"')`,
		`metadata=metadata||'{"extra":"not allowed"}'`,
		`metadata=jsonb_set(metadata,'{project_id}',to_jsonb('wrong-project'::text))`,
		`producer='model'`,
	} {
		_, err := conn.Exec(knowledgeContext(t), `UPDATE agenteam_audit.audit_records SET `+change+` WHERE id=$1::uuid`, record)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "23514" {
			t.Fatal("mismatched Knowledge Audit accepted")
		}
	}
}

func checkKnowledgeDocumentShape(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	doc, project := knowledgeID(t), knowledgeID(t)
	if _, err := conn.Exec(knowledgeContext(t), `INSERT INTO agenteam_knowledge.documents(id,project_id,content_version,status,deleted_at) VALUES($1,$2,1,'deleted',clock_timestamp())`, doc, project); err != nil {
		t.Fatal("minimal tombstone rejected", err)
	}
	_, err := conn.Exec(knowledgeContext(t), `UPDATE agenteam_knowledge.documents SET title='retained content' WHERE id=$1`, doc)
	requireKnowledgeCheck(t, err, "documents_current_shape")
	_, err = conn.Exec(knowledgeContext(t), `INSERT INTO agenteam_knowledge.documents(id,project_id,content_version,status,deleted_at) VALUES($1,$2,1,'deleted',clock_timestamp())`, doc, knowledgeID(t))
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23505" {
		t.Fatal("global document identity reused across projects")
	}
	_, err = conn.Exec(knowledgeContext(t), `INSERT INTO agenteam_knowledge.documents(id,project_id,content_version,status,deleted_at) VALUES($1,$2,1,'deleted',clock_timestamp())`, knowledgeID(t), project)
	if err != nil {
		t.Fatal("distinct canonical identity control failed", err)
	}
	// A real active row supplies all canonical columns; the same legal row
	// cannot point at a parent in another Project through a UUID-only FK.
	active := knowledgeID(t)
	insert := `INSERT INTO agenteam_knowledge.documents
 (id,project_id,parent_document_id,title,content_version,source_kind,media_type,current_object_id,current_upload_id,status,indexing_status,creator_user_id,created_at,updated_at)
 VALUES($1::uuid,$2::uuid,$3::uuid,'title',1,'text','text/plain',$4::uuid,$5::uuid,'active','pending',$6::uuid,clock_timestamp(),clock_timestamp())`
	if _, err = conn.Exec(knowledgeContext(t), insert, active, project, nil, knowledgeID(t), knowledgeID(t), knowledgeID(t)); err != nil {
		t.Fatal("complete active shape rejected", err)
	}
	_, err = conn.Exec(knowledgeContext(t), insert, knowledgeID(t), knowledgeID(t), active, knowledgeID(t), knowledgeID(t), knowledgeID(t))
	if !errors.As(err, &pg) || pg.Code != "23503" || pg.ConstraintName != "documents_parent_fk" {
		t.Fatal("cross-project parent key accepted")
	}
	if _, err = conn.Exec(knowledgeContext(t), insert, knowledgeID(t), project, active, knowledgeID(t), knowledgeID(t), knowledgeID(t)); err != nil {
		t.Fatal("same-project parent control rejected", err)
	}
}
