//go:build integration

package accountmail_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/LunaDeerTech/agenteam/db/migrations"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
)

func mailMigrationFiles(t *testing.T, last string) fstest.MapFS {
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

// Migration fixtures represent the old schema's immutable committed facts.
// No old producer is invoked using the new schema, and no business success is
// claimed by seeding them. Historical links deliberately need not still exist.
func seedMailOrigin(t *testing.T, conn *pgx.Conn, kind, variant string) string {
	t.Helper()
	root, job, link, user, session, browser, process := id[struct{}](t).String(), id[struct{}](t).String(), id[struct{}](t).String(), id[struct{}](t).String(), id[struct{}](t).String(), id[struct{}](t).String(), id[struct{}](t).String()
	if _, e := conn.Exec(ctxFor(t), `INSERT INTO agenteam_account.account_key_registry(kid,fingerprint) VALUES('fixture',decode(repeat('00',32),'hex')) ON CONFLICT DO NOTHING`); e != nil {
		t.Fatal(e)
	}
	name, actor, initiator, phase := "invite-create", "human", user, "committed"
	var browserID, expires, pver, target any
	target = user
	if kind == "password_reset" {
		name, actor, initiator = "reset-request", "browser", browser
		browserID = browser
		expires = "2026-01-02T00:00:00Z"
		pver = int64(1)
	}
	if kind == "test" {
		name = "smtp-test"
	}
	if variant == "planned" {
		phase = "planned"
	}
	if variant == "null_target" {
		target = nil
	}
	if variant == "null_password_version" {
		pver = nil
	}
	sum := sha256.Sum256([]byte(root))
	hash := "sha256:" + hex.EncodeToString(sum[:])
	if variant != "missing_command" {
		_, e := conn.Exec(ctxFor(t), `INSERT INTO agenteam_account.commands(id,namespace,owner_id,command_key,command_name,identity_digest,semantic_kid,semantic_mac,actor_kind,user_id,session_id,browser_id,browser_expires_at,origin_process_id,password_version,attempt_id,resource_id,phase,result_version,result_code,created_at,completed_at) VALUES($1,'fixture',$2,'owned-key',$3,$4,'fixture',decode(repeat('01',32),'hex'),$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,1,'COMPLETED','2026-01-01T00:00:00Z',CASE WHEN $14='planned' THEN NULL ELSE '2026-01-01T00:00:01Z'::timestamptz END)`, root, initiator, name, hash, actor, target, session, browserID, expires, process, pver, job, link, phase)
		if e != nil {
			t.Fatal(e)
		}
	}
	if variant == "wrong_job" {
		job = id[struct{}](t).String()
	}
	if variant == "wrong_link" {
		link = id[struct{}](t).String()
	}
	if variant == "wrong_initiator" {
		initiator = id[struct{}](t).String()
	}
	var linkValue, recipient any = link, nil
	if kind == "test" {
		linkValue = nil
		recipient = "owned@example.test"
	}
	if _, e := conn.Exec(ctxFor(t), `INSERT INTO agenteam_account.delivery_intents(id,job_id,kind,link_id,initiator_id,recipient,created_at) VALUES($1,$2,$3,$4,$5,$6,'2026-01-01T00:00:00Z')`, root, job, kind, linkValue, initiator, recipient); e != nil {
		t.Fatal(e)
	}
	return root
}

func TestAccountMailMigrationTenOriginsAndStrictRollback(t *testing.T) {
	for _, variant := range []string{"valid", "missing_command", "planned", "wrong_job", "wrong_link", "wrong_initiator", "null_target", "null_password_version", "legacy_test"} {
		t.Run(variant, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			cfg := db.Config(t, nil)
			old, e := postgres.NewSource(mailMigrationFiles(t, "00010"), nil)
			if e != nil {
				t.Fatal(e)
			}
			migrate(t, cfg, old)
			conn := db.Connect(t)
			kind := "password_reset"
			if variant == "legacy_test" {
				kind = "test"
			}
			root := seedMailOrigin(t, conn, kind, variant)
			if variant == "valid" {
				_ = seedMailOrigin(t, conn, "invitation", "valid")
			}
			m, e := postgres.NewMigrator(cfg)
			if e != nil {
				t.Fatal(e)
			}
			r := m.Migrate(ctxFor(t))
			if variant != "valid" {
				if r.Migrated {
					t.Fatal("invalid origin upgraded")
				}
				var columns, applied, pending int
				e = conn.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM information_schema.columns WHERE table_schema='agenteam_account' AND ((table_name='delivery_intents' AND column_name='origin_intent_id') OR (table_name='smtp_settings' AND column_name='sender_name'))),(SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=11 AND is_applied),(SELECT count(*) FROM agenteam_meta.migration_journal WHERE version=11 AND state='pending')`).Scan(&columns, &applied, &pending)
				if e != nil || columns != 0 || applied != 0 || pending != 1 {
					t.Fatal("DDL/journal atomic rollback", columns, applied, pending, e)
				}
				return
			}
			if !r.Migrated {
				t.Fatal("valid consumed/unenqueued roots", r.Fault)
			}
			var count, self int
			if e = conn.QueryRow(ctxFor(t), `SELECT count(*),count(*) FILTER(WHERE id=origin_intent_id) FROM agenteam_account.delivery_intents`).Scan(&count, &self); e != nil || count != 2 || self != 2 {
				t.Fatal(count, self, e)
			}
			var nullable string
			var hasDefault bool
			if e = conn.QueryRow(ctxFor(t), `SELECT is_nullable,column_default IS NOT NULL FROM information_schema.columns WHERE table_schema='agenteam_account' AND table_name='delivery_intents' AND column_name='origin_intent_id'`).Scan(&nullable, &hasDefault); e != nil || nullable != "NO" || hasDefault {
				t.Fatal("origin defaults", nullable, hasDefault, e)
			}
			var fk, index string
			if e = conn.QueryRow(ctxFor(t), `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='agenteam_account.delivery_intents'::regclass AND conname='account_delivery_origin_fk'`).Scan(&fk); e != nil || !strings.Contains(fk, "ON DELETE RESTRICT") {
				t.Fatal("origin FK", fk, e)
			}
			if e = conn.QueryRow(ctxFor(t), `SELECT indexdef FROM pg_indexes WHERE schemaname='agenteam_account' AND indexname='account_delivery_origin'`).Scan(&index); e != nil || !strings.Contains(index, "(origin_intent_id, id)") {
				t.Fatal("origin index", index, e)
			}
			child := id[struct{}](t).String()
			if _, e = conn.Exec(ctxFor(t), `INSERT INTO agenteam_account.delivery_intents(id,origin_intent_id,job_id,kind,link_id,initiator_id,created_at) SELECT $1,id,$2,kind,link_id,initiator_id,created_at FROM agenteam_account.delivery_intents WHERE id=$3`, child, id[struct{}](t).String(), root); e != nil {
				t.Fatal(e)
			}
			if _, e = conn.Exec(ctxFor(t), `DELETE FROM agenteam_account.delivery_intents WHERE id=$1`, root); e == nil {
				t.Fatal("origin deleted before descendant")
			}
		})
	}
}

func TestAccountMailMigrationRetryAuditSQLBranchClosed(t *testing.T) {
	db, _, _ := database(t)
	conn := db.Connect(t)
	for _, variant := range []string{"human", "service", "project", "wrong_producer", "wrong_resource"} {
		scope, actor, producer, resource := "system", "human", "account", "mail_job"
		var project, user, session, service, cause any = nil, id[struct{}](t).String(), id[struct{}](t).String(), nil, nil
		id := id[struct{}](t).String()
		switch variant {
		case "service":
			actor = "service"
			user = nil
			session = nil
			service = "account-mail"
			cause = id
		case "project":
			scope = "project"
			project = id
		case "wrong_producer":
			producer = "account.mail"
		case "wrong_resource":
			resource = "user"
		}
		_, e := conn.Exec(ctxFor(t), `INSERT INTO agenteam_audit.audit_records(id,scope,project_id,actor_kind,user_id,session_id,service_name,service_cause,action,outcome,resource_kind,resource_id,metadata,semantic_digest,producer,cause_ref,ordinal) VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8,'smtp.delivery.retry','success',$9,$1::uuid,'{}','sha256:'||repeat('0',64),$10,$1::text,0)`, id, scope, project, actor, user, session, service, cause, resource, producer)
		if variant == "human" && e != nil {
			t.Fatal("legal SQL branch", e)
		}
		if variant != "human" && e == nil {
			t.Fatal("invalid SQL retry branch", variant)
		}
	}
}
