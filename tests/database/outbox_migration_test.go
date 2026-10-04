//go:build integration

package database_test

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/LunaDeerTech/agenteam/db/migrations"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func TestOutboxMigrationPreservesAuditAndExtendsOnlyTypedRequeue(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	files := fstest.MapFS{}
	names, err := fs.Glob(migrations.SQL, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name >= "00008" {
			continue
		}
		raw, err := fs.ReadFile(migrations.SQL, name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: raw}
	}
	source, err := postgres.NewSource(files, nil)
	if err != nil {
		t.Fatal(err)
	}
	migrate(t, cfg, source)
	conn := db.Connect(t)
	const record = "01900000-0000-7000-8000-000000000031"
	const user = "01900000-0000-7000-8000-000000000032"
	const session = "01900000-0000-7000-8000-000000000033"
	const digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	// A legal pre-00008 Audit event must survive the CHECK extension byte-for-byte.
	if _, err = conn.Exec(testContext(t), `INSERT INTO agenteam_audit.audit_records(id,scope,actor_kind,user_id,session_id,action,outcome,resource_kind,resource_id,metadata,semantic_digest,producer,cause_ref,ordinal) VALUES($1,'system','human',$2,$3,'secret.create','success','secret',$1,'{"name":"historical"}',$4,'secret',$4,0)`, record, user, session, digest); err != nil {
		t.Fatal("old audit fixture rejected")
	}
	var before string
	if err = conn.QueryRow(testContext(t), `SELECT row_to_json(r)::text FROM agenteam_audit.audit_records r WHERE id=$1`, record).Scan(&before); err != nil {
		t.Fatal(err)
	}
	migrate(t, cfg)
	var after string
	if err = conn.QueryRow(testContext(t), `SELECT row_to_json(r)::text FROM agenteam_audit.audit_records r WHERE id=$1`, record).Scan(&after); err != nil || after != before {
		t.Fatal("old Audit row changed")
	}
	var cycle bool
	var cache int64
	if err = conn.QueryRow(testContext(t), `SELECT seqcycle,seqcache FROM pg_sequence WHERE seqrelid='agenteam_outbox.outbox_sequence'::regclass`).Scan(&cycle, &cache); err != nil || cycle || cache != 1 {
		t.Fatal("sequence contract")
	}
	id, _ := foundation.NewID[struct{}]()
	event, _ := foundation.NewID[struct{}]()
	metadata := `{"delivery_id":"` + id.String() + `","event_id":"` + event.String() + `","handler_id":"fixture.handler","from_state":"failed","redrive_cycle":"1","reason_code":"operator_retry"}`
	insert := `INSERT INTO agenteam_audit.audit_records(id,scope,actor_kind,user_id,session_id,action,outcome,resource_kind,resource_id,metadata,semantic_digest,producer,cause_ref,ordinal) VALUES($1,'system','human',$2,$3,'outbox.delivery.requeue',$5,'outbox_delivery',$1,$6::jsonb,$4,'outbox',$4,$7)`
	if _, err = conn.Exec(testContext(t), insert, id.String(), user, session, digest, "success", metadata, 1); err != nil {
		t.Fatal("new typed Audit row rejected")
	}
	for _, bad := range []string{`{}`, `{"delivery_id":"` + id.String() + `","event_id":"` + event.String() + `","handler_id":"fixture.handler","from_state":"failed","redrive_cycle":1,"reason_code":"operator_retry"}`, metadata[:len(metadata)-1] + `,"message":"private-canary"}`, metadata} {
		next, _ := foundation.NewID[struct{}]()
		// The final case keeps valid metadata but attempts to attach another resource.
		if _, err = conn.Exec(testContext(t), insert, next.String(), user, session, digest, "success", bad, 2); err == nil {
			t.Fatal("invalid new metadata/resource passed CHECK")
		}
	}
	// Role registration is not permission to impersonate the Human admin actor.
	next, _ := foundation.NewID[struct{}]()
	if _, err = conn.Exec(testContext(t), `INSERT INTO agenteam_audit.audit_records(id,scope,actor_kind,service_name,service_cause,action,outcome,resource_kind,resource_id,metadata,semantic_digest,producer,cause_ref,ordinal) VALUES($1,'system','service','outbox-delivery',$2,'outbox.delivery.requeue','success','outbox_delivery',$1,$3::jsonb,$2,'outbox',$2,3)`, next.String(), digest, metadata); err == nil {
		t.Fatal("service Audit privilege widened")
	}
}
