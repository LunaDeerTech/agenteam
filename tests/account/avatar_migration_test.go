//go:build integration

package account_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/LunaDeerTech/agenteam/db/migrations"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// These rows exercise DDL retention, not a fabricated authorization decision.
// No account/object operation consumes the fixture-only PHC or semantic MAC.
func seedAvatarMigrationRows(t *testing.T, db *pgfixture.Database) {
	t.Helper()
	conn := db.Connect(t)
	uid := id[struct{}](t).String()
	process := id[struct{}](t).String()
	if _, e := conn.Exec(ctxFor(t), `INSERT INTO agenteam_account.account_key_registry(kid,fingerprint) VALUES('migration-fixture',$1)`, make([]byte, 32)); e != nil {
		t.Fatal(e)
	}
	if _, e := conn.Exec(ctxFor(t), `INSERT INTO agenteam_account.users(id,email,username,display_name,role,password_phc,password_version,version,auth_sequence,initial_password_suggestion,theme) VALUES($1,'fixture@example.test','fixture-user','fixture','user','unused-migration-fixture',1,7,1,false,'system')`, uid); e != nil {
		t.Fatal(e)
	}
	for i, phase := range []string{"preparing", "published", "applied", "cancelled", "cleanup_pending", "completed"} {
		change := id[struct{}](t).String()
		object := id[struct{}](t).String()
		previous := id[struct{}](t).String()
		upload := id[struct{}](t).String()
		attempt := id[struct{}](t).String()
		identity, e := foundation.NewCommandIdentity("account.profile", []string{uid}, "avatar-update", foundation.IdempotencyKey(change))
		if e != nil {
			t.Fatal(e)
		}
		h := sha256.Sum256([]byte(identity.Canonical()))
		if _, e = conn.Exec(ctxFor(t), `INSERT INTO agenteam_account.commands(id,namespace,owner_id,command_key,command_name,identity_digest,semantic_kid,semantic_mac,actor_kind,user_id,origin_process_id,expected_version,phase) VALUES($1,'account.profile',$2,$6,'avatar-update',$3,'migration-fixture',$4,'human',$2,$5,7,'planned')`, change, uid, "sha256:"+hex.EncodeToString(h[:]), make([]byte, 32), process, change); e != nil {
			t.Fatal(e)
		}
		if _, e = conn.Exec(ctxFor(t), `INSERT INTO agenteam_account.avatar_changes(id,command_id,user_id,previous_object_id,new_object_id,upload_id,attempt_id,origin_process_id,cleanup_cause,phase,pass,version) VALUES($1,$1,$2,$3,$4,$5,$6,$7,$1,$8,$9,7)`, change, uid, previous, object, upload, attempt, process, phase, i); e != nil {
			t.Fatal(e)
		}
		cleanupPhase := "pending"
		if i%2 == 1 {
			cleanupPhase = "completed"
		}
		if _, e = conn.Exec(ctxFor(t), `INSERT INTO agenteam_account.avatar_cleanup(id,user_id,object_id,change_id,phase,pass) VALUES($1,$2,$3,$1,$4,$5)`, change, uid, previous, cleanupPhase, i); e != nil {
			t.Fatal(e)
		}
	}
}
func avatarMigrationFacts(t *testing.T, db *pgfixture.Database) string {
	t.Helper()
	var value string
	e := db.Connect(t).QueryRow(ctxFor(t), `SELECT md5((SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id)::text,'') FROM agenteam_account.avatar_changes t)||(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id)::text,'') FROM agenteam_account.avatar_cleanup t)||(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id)::text,'') FROM agenteam_account.commands t)||(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id)::text,'') FROM agenteam_account.users t))`).Scan(&value)
	if e != nil {
		t.Fatal(e)
	}
	return value
}
func checkAvatarRecoveryIndex(t *testing.T, db *pgfixture.Database) {
	t.Helper()
	var def, predicate string
	e := db.Connect(t).QueryRow(ctxFor(t), `SELECT pg_get_indexdef(i.indexrelid),pg_get_expr(i.indpred,i.indrelid) FROM pg_index i WHERE i.indexrelid='agenteam_account.account_avatar_changes_fair'::regclass`).Scan(&def, &predicate)
	if e != nil || !strings.Contains(def, "(pass, id)") {
		t.Fatal("recovery index", e)
	}
	for _, phase := range []string{"preparing", "published", "cancelled", "cleanup_pending"} {
		if !strings.Contains(predicate, "'"+phase+"'::text") {
			t.Fatal("missing recovering phase", phase)
		}
	}
	if strings.Contains(predicate, "'applied'") || strings.Contains(predicate, "'completed'") {
		t.Fatal("terminal phase included in recovery index")
	}
}
func TestAccountAvatarMigrationFreshAndPopulatedEleven(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "eleven"
		}
		t.Run(name, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			cfg := db.Config(t, nil)
			before := ""
			if upgrade {
				src, e := postgres.NewSource(migrationFiles(t, "00011"), nil)
				if e != nil {
					t.Fatal(e)
				}
				migrate(t, cfg, src)
				seedAvatarMigrationRows(t, db)
				before = avatarMigrationFacts(t, db)
			}
			migrate(t, cfg)
			checkAvatarRecoveryIndex(t, db)
			if upgrade && before != avatarMigrationFacts(t, db) {
				t.Fatal("index migration changed existing commands, phases or mappings")
			}
		})
	}
}
func TestAccountAvatarMigrationAtomicIndexAndJournalRollback(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	files := migrationFiles(t, "00011")
	old, e := postgres.NewSource(files, nil)
	if e != nil {
		t.Fatal(e)
	}
	migrate(t, cfg, old)
	seedAvatarMigrationRows(t, db)
	before := avatarMigrationFacts(t, db)
	raw, e := fs.ReadFile(migrations.SQL, "00012_account_avatar_recovery.sql")
	if e != nil {
		t.Fatal(e)
	}
	files["00012_account_avatar_recovery.sql"] = &fstest.MapFile{Data: append(raw, []byte("\nSELECT avatar_fixture_fail_migration();\n")...)}
	bad, e := postgres.NewSource(files, nil)
	if e != nil {
		t.Fatal(e)
	}
	m, e := postgres.NewMigrator(cfg, bad)
	if e != nil {
		t.Fatal(e)
	}
	if r := m.Migrate(ctxFor(t)); r.Migrated {
		t.Fatal("injected migration failure committed")
	}
	var absent bool
	var pending, applied int
	e = db.Connect(t).QueryRow(ctxFor(t), `SELECT to_regclass('agenteam_account.account_avatar_changes_fair') IS NULL,(SELECT count(*) FROM agenteam_meta.migration_journal WHERE version=12 AND state='pending'),(SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=12 AND is_applied)`).Scan(&absent, &pending, &applied)
	if e != nil || !absent || pending != 1 || applied != 0 || before != avatarMigrationFacts(t, db) {
		t.Fatal("migration was not atomic", absent, pending, applied, e)
	}
	// Retrying the identical pending checksum after fixing only the owned
	// fixture dependency exercises Goose's real transaction and journal path.
	if _, e = db.Connect(t).Exec(ctxFor(t), `CREATE FUNCTION public.avatar_fixture_fail_migration() RETURNS void LANGUAGE SQL AS 'SELECT NULL::void'`); e != nil {
		t.Fatal(e)
	}
	migrate(t, cfg, bad)
	checkAvatarRecoveryIndex(t, db)
	if before != avatarMigrationFacts(t, db) {
		t.Fatal("retry changed existing business facts")
	}
}
