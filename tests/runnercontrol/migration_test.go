//go:build integration

package runnercontrol_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/LunaDeerTech/agenteam/db/migrations"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// These are schema oracles, deliberately using SQL to exercise CHECK/trigger/FK
// enforcement. They do not stand in for authenticated service or Audit producer
// acceptance, which require the real Account and Runner services separately.
func migrationContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func migrationID(t *testing.T) string {
	t.Helper()
	id, err := p.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return string(id)
}
func migrationSource(t *testing.T, version int, fail bool) postgres.Source {
	t.Helper()
	files := fstest.MapFS{}
	names, err := fs.Glob(migrations.SQL, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name[:5] > fmt.Sprintf("%05d", version) {
			continue
		}
		raw, err := fs.ReadFile(migrations.SQL, name)
		if err != nil {
			t.Fatal(err)
		}
		if fail && strings.HasPrefix(name, "00026_") {
			raw = append(raw, []byte("\nSELECT 1/0;\n")...)
		}
		files[name] = &fstest.MapFile{Data: raw}
	}
	source, err := postgres.NewSource(files, nil)
	if err != nil || source.Target() != int64(version) {
		t.Fatal("continuous migration source is incomplete", err)
	}
	return source
}
func runnerMigrate(t *testing.T, db *pgfixture.Database, target int) {
	t.Helper()
	m, err := postgres.NewMigrator(db.Config(t, nil), migrationSource(t, target, false))
	if err != nil {
		t.Fatal(err)
	}
	result := m.Migrate(migrationContext(t))
	if !result.Migrated || result.Version != int64(target) {
		t.Fatal("Runner migration failed", result.Fault)
	}
}
func sqlCode(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && len(pg.Code) == 5 {
		for _, b := range []byte(pg.Code) {
			if !(b >= 'A' && b <= 'Z' || b >= '0' && b <= '9') {
				return "other"
			}
		}
		return pg.Code
	}
	return "other"
}
func sqlOK(t *testing.T, conn *pgx.Conn, query string, args ...any) {
	t.Helper()
	if _, err := conn.Exec(migrationContext(t), query, args...); err != nil {
		t.Fatalf("schema stimulus failed (SQLSTATE %s)", sqlCode(err))
	}
}
func sqlReject(t *testing.T, conn *pgx.Conn, code, query string, args ...any) {
	t.Helper()
	_, err := conn.Exec(migrationContext(t), query, args...)
	if err == nil || sqlCode(err) != code {
		t.Fatalf("schema negative control: want %s, got %s (rejected=%t)", code, sqlCode(err), err != nil)
	}
}
func auditSnapshot(t *testing.T, conn *pgx.Conn) string {
	t.Helper()
	var s string
	if err := conn.QueryRow(migrationContext(t), `SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY id),'[]'::jsonb)::text FROM agenteam_audit.audit_records a`).Scan(&s); err != nil {
		t.Fatal("cannot read Audit rows", sqlCode(err))
	}
	return s
}
func auditChecks(t *testing.T, conn *pgx.Conn) string {
	t.Helper()
	var s string
	if err := conn.QueryRow(migrationContext(t), `SELECT jsonb_object_agg(conname,pg_get_expr(conbin,conrelid))::text FROM pg_constraint WHERE conrelid='agenteam_audit.audit_records'::regclass AND contype='c'`).Scan(&s); err != nil {
		t.Fatal("cannot read Audit constraints", sqlCode(err))
	}
	return s
}

type auditRow struct {
	id, scope, project, user, session, action, resource, resourceID, producer, cause, runner, service string
	metadata                                                                                          map[string]any
	ordinal                                                                                           int
}

func (r auditRow) insert(ctx context.Context, conn *pgx.Conn) error {
	raw, err := json.Marshal(r.metadata)
	if err != nil {
		return err
	}
	actor := "human"
	var user, session, service, serviceCause any = r.user, r.session, nil, nil
	if r.service != "" {
		actor, user, session, service, serviceCause = "service", nil, nil, r.service, r.cause
	}
	_, err = conn.Exec(ctx, `INSERT INTO agenteam_audit.audit_records
 (id,scope,project_id,actor_kind,user_id,session_id,service_name,service_cause,action,outcome,resource_kind,resource_id,metadata,semantic_digest,producer,cause_ref,ordinal,runner_id)
 VALUES($1::uuid,$2,NULLIF($3::text,'')::uuid,$4,$5::uuid,$6::uuid,$7,$8,$9,'success',$10,$11::uuid,$12::jsonb,$13,$14,$15,$16,NULLIF($17::text,'')::uuid)`,
		r.id, r.scope, r.project, actor, user, session, service, serviceCause, r.action, r.resource, r.resourceID, string(raw), "sha256:"+strings.Repeat("a", 64), r.producer, r.cause, r.ordinal, r.runner)
	return err
}
func legacyAuditRows(t *testing.T) []auditRow {
	t.Helper()
	project, user, session := migrationID(t), migrationID(t), migrationID(t)
	var rows []auditRow
	for _, action := range []string{"create", "update", "delete"} {
		id := migrationID(t)
		fields, version := []string{"created"}, "1"
		if action == "update" {
			fields, version = []string{"value"}, "2"
		}
		if action == "delete" {
			fields, version = []string{"deleted"}, "3"
		}
		rows = append(rows, auditRow{id: migrationID(t), scope: "project", project: project, user: user, session: session, action: "project.variable." + action, resource: "project_variable", resourceID: id, producer: "projectvariable", cause: "sha256:" + strings.ReplaceAll(migrationID(t), "-", "") + strings.Repeat("b", 32), metadata: map[string]any{"variable_id": id, "version": version, "changed_fields": fields}})
	}
	root := migrationID(t)
	rows = append(rows, auditRow{id: migrationID(t), scope: "project", project: project, user: user, session: session, action: "knowledge.delete_subtree", resource: "knowledge_document", resourceID: root, producer: "knowledge", cause: "sha256:" + strings.Repeat("c", 64), metadata: map[string]any{"project_id": project, "root_id": root, "initiator_id": user, "scope_digest": "sha256:" + strings.Repeat("d", 64), "deleted_count": "2"}})
	rows = append(rows, auditRow{id: migrationID(t), scope: "project", project: project, user: user, session: session, action: "project.update", resource: "project", resourceID: project, producer: "project", cause: migrationID(t), metadata: map[string]any{"project_id": project, "initiator_id": user, "project_version": "2", "changed_fields": []string{"name"}}})
	return rows
}
func insertAudit(t *testing.T, conn *pgx.Conn, row auditRow) {
	t.Helper()
	if err := row.insert(migrationContext(t), conn); err != nil {
		t.Fatalf("Audit schema %s rejected (SQLSTATE %s)", row.action, sqlCode(err))
	}
}
func checkRunnerAudit(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	for _, action := range []string{"runner.create", "runner.update", "runner.enrollment.issue", "runner.enroll", "runner.revoke"} {
		id := migrationID(t)
		row := auditRow{id: migrationID(t), scope: "system", user: migrationID(t), session: migrationID(t), action: action, resource: "runner", resourceID: id, runner: id, producer: "runner", cause: migrationID(t), metadata: map[string]any{"runner_id": id, "version": "2", "credential_generation": "1", "changed_fields": []string{"credential"}}}
		switch action {
		case "runner.create":
			row.metadata["version"], row.metadata["changed_fields"] = "1", []string{"created"}
		case "runner.update":
			row.metadata["changed_fields"] = []string{"description", "tags"}
		case "runner.enroll":
			row.service = "runner-identity"
			row.metadata["public_key_fingerprint"] = "sha256:" + strings.Repeat("e", 64)
		}
		insertAudit(t, conn, row)
		for _, negative := range []string{"producer", "resource", "association", "metadata", "actor"} {
			bad := row
			bad.id, bad.cause = migrationID(t), migrationID(t)
			bad.metadata = make(map[string]any, len(row.metadata))
			for key, value := range row.metadata {
				bad.metadata[key] = value
			}
			switch negative {
			case "producer":
				bad.producer = "secret"
			case "resource":
				bad.resource = "secret"
			case "association":
				bad.runner = migrationID(t)
			case "metadata":
				bad.metadata["token"] = "must-never-be-persisted"
			case "actor":
				bad.service = "secret"
			}
			if err := bad.insert(migrationContext(t), conn); err == nil || sqlCode(err) != "23514" {
				t.Fatalf("Audit %s/%s failed closed check (SQLSTATE %s)", action, negative, sqlCode(err))
			}
		}
	}
}

func TestRunnerControlMigration(t *testing.T) {
	t.Run("continuous-23-24-25-26-and-repeat-retains-Audit", func(t *testing.T) {
		db := pgfixture.NewDatabase(t)
		runnerMigrate(t, db, 23)
		conn := db.Connect(t)
		rows := legacyAuditRows(t)
		insertAudit(t, conn, rows[len(rows)-1])
		runnerMigrate(t, db, 24)
		for _, row := range rows[:3] {
			insertAudit(t, conn, row)
		}
		runnerMigrate(t, db, 25)
		insertAudit(t, conn, rows[3])
		before := auditSnapshot(t, conn)
		runnerMigrate(t, db, 26)
		runnerMigrate(t, db, 26)
		if auditSnapshot(t, conn) != before {
			t.Fatal("migration rewrote predecessor Audit")
		}
		for _, row := range legacyAuditRows(t) {
			insertAudit(t, conn, row)
		}
		checkRunnerAudit(t, conn)
	})
	t.Run("fresh-schema-credential-and-history-guards", func(t *testing.T) {
		db := pgfixture.NewDatabase(t)
		runnerMigrate(t, db, 26)
		conn := db.Connect(t)
		var tables int
		if err := conn.QueryRow(migrationContext(t), `SELECT count(*) FROM pg_tables WHERE schemaname='agenteam_runner'`).Scan(&tables); err != nil || tables != 6 {
			t.Fatal("wrong Runner table count", tables, sqlCode(err))
		}
		checkRunnerRows(t, conn)
	})
	t.Run("failure-rolls-back-full-26-and-Audit-alterations", func(t *testing.T) {
		db := pgfixture.NewDatabase(t)
		runnerMigrate(t, db, 25)
		conn := db.Connect(t)
		for _, row := range legacyAuditRows(t) {
			insertAudit(t, conn, row)
		}
		before, constraints := auditSnapshot(t, conn), auditChecks(t, conn)
		m, err := postgres.NewMigrator(db.Config(t, nil), migrationSource(t, 26, true))
		if err != nil {
			t.Fatal(err)
		}
		result := m.Migrate(migrationContext(t))
		if result.Migrated || result.Fault == nil {
			t.Fatal("injected end-of-DDL failure accepted")
		}
		var leaks int
		if err := conn.QueryRow(migrationContext(t), `SELECT (SELECT count(*) FROM pg_namespace WHERE nspname='agenteam_runner')+(SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=26 AND is_applied)`).Scan(&leaks); err != nil || leaks != 0 {
			t.Fatal("DDL escaped original transaction", leaks, sqlCode(err))
		}
		if auditSnapshot(t, conn) != before || auditChecks(t, conn) != constraints {
			t.Fatal("failed DDL changed predecessor rows or CHECKs")
		}
	})
}

func checkRunnerRows(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	id, command := migrationID(t), migrationID(t)
	sqlOK(t, conn, `INSERT INTO agenteam_runner.runners(id,name,description,tags,root_path,version,credential_generation,created_at,updated_at) VALUES($1::uuid,'Runner','','[]','/workspace',1,1,clock_timestamp(),clock_timestamp())`, id)
	sqlReject(t, conn, "23514", `UPDATE agenteam_runner.runners SET root_path='/other',version=2 WHERE id=$1::uuid`, id)
	sqlOK(t, conn, `UPDATE agenteam_runner.runners SET device_public_key=decode(repeat('11',32),'hex'),enrolled_at=clock_timestamp(),version=2,updated_at=clock_timestamp() WHERE id=$1::uuid`, id)
	sqlReject(t, conn, "23514", `UPDATE agenteam_runner.runners SET device_public_key=NULL,enrolled_at=NULL,version=3 WHERE id=$1::uuid`, id)
	sqlReject(t, conn, "23514", `UPDATE agenteam_runner.runners SET enrolled_at=enrolled_at+interval '1 second',version=3 WHERE id=$1::uuid`, id)
	sqlReject(t, conn, "23514", `UPDATE agenteam_runner.runners SET device_public_key=decode(repeat('22',32),'hex'),version=3 WHERE id=$1::uuid`, id)
	sqlOK(t, conn, `UPDATE agenteam_runner.runners SET name='renamed',version=3,updated_at=clock_timestamp() WHERE id=$1::uuid`, id)
	sqlOK(t, conn, `UPDATE agenteam_runner.runners SET device_public_key=NULL,enrolled_at=NULL,credential_generation=2,version=4,updated_at=clock_timestamp() WHERE id=$1::uuid`, id)
	sqlOK(t, conn, `UPDATE agenteam_runner.runners SET device_public_key=decode(repeat('22',32),'hex'),enrolled_at=clock_timestamp(),version=5,updated_at=clock_timestamp() WHERE id=$1::uuid`, id)
	sqlOK(t, conn, `UPDATE agenteam_runner.runners SET connection_generation=1,last_seen_at=clock_timestamp() WHERE id=$1::uuid`, id)
	sqlOK(t, conn, `INSERT INTO agenteam_runner.connections(runner_id,id,credential_generation,generation,owner_id,authenticated_at,last_seen_at,lease_expires_at) VALUES($1::uuid,$2::uuid,2,1,$3::uuid,clock_timestamp(),clock_timestamp(),clock_timestamp()+interval '30 seconds')`, id, migrationID(t), migrationID(t))
	sqlReject(t, conn, "23503", `UPDATE agenteam_runner.connections SET generation=2 WHERE runner_id=$1::uuid`, id)
	sqlOK(t, conn, `INSERT INTO agenteam_runner.commands(id,runner_id,actor_user_id,actor_session_id,command_name,idempotency_key,semantic_digest,request,receipt,created_at,committed_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,'runner.update','fixture-key','sha256:'||repeat('a',64),'{}','{}',clock_timestamp(),clock_timestamp())`, command, id, migrationID(t), migrationID(t))
	sqlReject(t, conn, "23514", `UPDATE agenteam_runner.commands SET receipt='{"changed":true}' WHERE id=$1::uuid`, command)
	sqlReject(t, conn, "23514", `DELETE FROM agenteam_runner.commands WHERE id=$1::uuid`, command)
	sqlOK(t, conn, `INSERT INTO agenteam_runner.identity_events(id,runner_id,kind,credential_generation,version,public_key_fingerprint,occurred_at) VALUES($1::uuid,$2::uuid,'enrolled',2,5,'sha256:'||repeat('a',64),clock_timestamp())`, migrationID(t), id)
	sqlReject(t, conn, "23505", `INSERT INTO agenteam_runner.identity_events(id,runner_id,kind,credential_generation,version,public_key_fingerprint,occurred_at) VALUES($1::uuid,$2::uuid,'enrolled',2,5,'sha256:'||repeat('a',64),clock_timestamp())`, migrationID(t), id)
	sqlReject(t, conn, "23514", `DELETE FROM agenteam_runner.identity_events WHERE runner_id=$1::uuid`, id)
	for _, table := range []string{"challenges", "enrollment_tokens"} {
		hashColumn, extraColumns, extraValues, ttl := "nonce_hash", "", "", "30 seconds"
		if table == "enrollment_tokens" {
			hashColumn, extraColumns, extraValues, ttl = "token_hash", ",issued_command_id", ",$2::uuid", "10 minutes"
		}
		query := `INSERT INTO agenteam_runner.` + table + `(` + hashColumn + `,runner_id,credential_generation,issued_at,expires_at` + extraColumns + `) SELECT decode(repeat('33',32),'hex'),$1::uuid,2,at,at+interval '` + ttl + `'` + extraValues + ` FROM (SELECT clock_timestamp() AS at) q`
		args := []any{id}
		if extraColumns != "" {
			args = append(args, command)
		}
		sqlOK(t, conn, query, args...)
		sqlOK(t, conn, `UPDATE agenteam_runner.`+table+` SET consumed_at=clock_timestamp() WHERE runner_id=$1::uuid`, id)
		sqlReject(t, conn, "23514", `UPDATE agenteam_runner.`+table+` SET consumed_at=NULL WHERE runner_id=$1::uuid`, id)
		sqlReject(t, conn, "23514", `UPDATE agenteam_runner.`+table+` SET credential_generation=3 WHERE runner_id=$1::uuid`, id)
		sqlOK(t, conn, `UPDATE agenteam_runner.`+table+` SET revoked_at=clock_timestamp() WHERE runner_id=$1::uuid`, id)
		sqlReject(t, conn, "23514", `UPDATE agenteam_runner.`+table+` SET revoked_at=NULL WHERE runner_id=$1::uuid`, id)
	}
}
