//go:build integration

package database_test

import (
	"encoding/json"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// This checks the actual deployed SQL contract, not permission to append an
// Audit event. The separate domain test must supply the private live-Tx proof.
func TestMeetingDeletionAuditMigrationPreservesOldRowsAndClosesNewShape(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	previous, err := postgres.NewSource(outboxMigrationFiles(t, "00139"), nil)
	if err != nil {
		t.Fatal(err)
	}
	migrate(t, cfg, previous)
	conn := db.Connect(t)
	id := func() string {
		v, err := f.NewID[struct{}]()
		if err != nil {
			t.Fatal(err)
		}
		return v.String()
	}
	old, user, session, project, meeting, deletion := id(), id(), id(), id(), id(), id()
	const digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if _, err = conn.Exec(testContext(t), `INSERT INTO agenteam_audit.audit_records
 (id,scope,actor_kind,user_id,session_id,action,outcome,resource_kind,resource_id,metadata,semantic_digest,producer,cause_ref,ordinal)
 VALUES($1,'system','human',$2,$3,'secret.create','success','secret',$1,'{"name":"historical"}',$4,'secret',$4,0)`, old, user, session, digest); err != nil {
		t.Fatal("predecessor audit row rejected", err)
	}
	read := func(record string) string {
		t.Helper()
		var value string
		if err := conn.QueryRow(testContext(t), `SELECT row_to_json(r)::text||r.xmin::text FROM agenteam_audit.audit_records r WHERE id=$1`, record).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := read(old)
	target, err := postgres.NewSource(outboxMigrationFiles(t, "00140"), nil)
	if err != nil {
		t.Fatal(err)
	}
	m := migrate(t, cfg, target)
	if repeat := m.Migrate(testContext(t)); !repeat.Migrated || repeat.Version != 140 {
		t.Fatal("repeat current migration failed", repeat.Fault)
	}
	if read(old) != before {
		t.Fatal("migration rewrote the predecessor audit row")
	}
	metadata := func(rows string) []byte {
		value, err := json.Marshal(map[string]string{"meeting_id": meeting, "deletion_id": deletion, "initiator_id": user, "version": "7", "deleted_rows": rows})
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	const insert = `INSERT INTO agenteam_audit.audit_records
 (id,scope,project_id,actor_kind,user_id,session_id,actor_project_id,service_name,service_cause,action,outcome,resource_kind,resource_id,metadata,semantic_digest,producer,cause_ref,ordinal,operation_id)
 VALUES($1,'project',$2,$3,$4::uuid,$5::uuid,$6::uuid,$7,$8,$9,'success','meeting',$10,$11::jsonb,$12,'meeting',$13::text,$14,$13::uuid)`
	accepted, completed := id(), id()
	if _, err = conn.Exec(testContext(t), insert, accepted, project, "human", user, session, nil, nil, nil, "meeting.delete.accepted", meeting, metadata("0"), digest, deletion, int64(0)); err != nil {
		t.Fatal("accepted shape rejected", err)
	}
	if _, err = conn.Exec(testContext(t), insert, completed, project, "service", nil, nil, project, "meeting-runtime", deletion, "meeting.delete.completed", meeting, metadata("9007199254740993"), digest, deletion, int64(1)); err != nil {
		t.Fatal("completed shape rejected", err)
	}
	rejected := func(record, assignment string) {
		t.Helper()
		_, err := conn.Exec(testContext(t), `UPDATE agenteam_audit.audit_records SET `+assignment+` WHERE id=$1`, record)
		var check *pgconn.PgError
		if !errors.As(err, &check) || check.Code != "23514" {
			t.Fatalf("expected CHECK rejection for %s: %v", assignment, err)
		}
	}
	for _, record := range []string{accepted, completed} {
		original := read(record)
		for _, assignment := range []string{
			`scope='system',project_id=NULL,actor_project_id=NULL`,
			`producer='secret'`, `resource_kind='secret'`, `outcome='failed'`,
			`operation_id=NULL`, `request_id=id`, `tool_id=id`, `resource_id=id`,
			`cause_ref='sha256:'||repeat('0',64)`,
			`metadata=metadata-'initiator_id'`, `metadata=metadata||'{"private":"canary"}'::jsonb`,
			`metadata=jsonb_set(metadata,'{initiator_id}','null'::jsonb)`,
			`metadata=jsonb_set(metadata,'{version}','"0"'::jsonb)`,
			`metadata=jsonb_set(metadata,'{version}','7'::jsonb)`,
			`metadata=jsonb_set(metadata,'{deleted_rows}','1'::jsonb)`,
			`metadata=jsonb_set(metadata,'{deleted_rows}','"01"'::jsonb)`,
			`metadata=jsonb_set(metadata,'{deleted_rows}','"9223372036854775808"'::jsonb)`,
		} {
			rejected(record, assignment)
		}
		if read(record) != original {
			t.Fatal("rejected update changed the audit row")
		}
	}
	for _, scenario := range []struct{ record, assignment string }{
		{accepted, `metadata=jsonb_set(metadata,'{deleted_rows}','"1"'::jsonb)`},
		{accepted, `metadata=jsonb_set(metadata,'{initiator_id}',to_jsonb(id::text))`},
		{accepted, `ordinal=2`},
		{completed, `metadata=jsonb_set(metadata,'{deleted_rows}','"0"'::jsonb)`},
		{completed, `service_name='project-lifecycle'`},
		{completed, `service_cause=id::text`},
		{completed, `actor_kind='human',user_id=resource_id,session_id=resource_id,actor_project_id=NULL,service_name=NULL,service_cause=NULL`},
		{completed, `ordinal=2`},
		{old, `resource_kind='meeting'`},
		{old, `producer='meeting'`},
	} {
		rejected(scenario.record, scenario.assignment)
	}
	if read(old) != before {
		t.Fatal("new vocabulary changed predecessor semantics")
	}
}
