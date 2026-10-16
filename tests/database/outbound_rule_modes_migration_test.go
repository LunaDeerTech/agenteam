//go:build integration

package database_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

type migrationReadOnlyAudit struct{}

func (migrationReadOnlyAudit) AppendInTx(context.Context, f.Tx, ac.Entry, ac.AppendKey) (ac.AppendReceipt, error) {
	return ac.AppendReceipt{}, errors.New("migration read unexpectedly appended audit")
}

// This is a predecessor-to-current schema test, not an SMTP delivery or Secret
// decryption test. All row material below belongs only to this fresh database.
func TestOutboundRuleModesResetPreservesOtherDomains(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	old, err := postgres.NewSource(outboxMigrationFiles(t, "00144"), nil)
	if err != nil {
		t.Fatal(err)
	}
	migrateVersion := func(source postgres.Source, version int64) *postgres.Migrator {
		t.Helper()
		m, err := postgres.NewMigrator(cfg, source)
		if err != nil {
			t.Fatal(err)
		}
		// Use the migrator's actual configured startup budget for 144 schemas;
		// ordinary per-query testContext remains unchanged.
		if result := m.Migrate(t.Context()); !result.Migrated || result.Version != version {
			t.Fatal("owned migration failed", result.Fault)
		}
		return m
	}
	migrateVersion(old, 144)
	conn := db.Connect(t)
	id := func() string {
		v, err := f.NewID[struct{}]()
		if err != nil {
			t.Fatal(err)
		}
		return v.String()
	}
	user, session, audit, credential, payload, smtp := id(), id(), id(), id(), id(), id()
	const digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(testContext(t), sql, args...); err != nil {
			t.Fatal("owned predecessor seed failed", err)
		}
	}
	exec(`INSERT INTO agenteam_account.users(id,email,username,display_name,role,password_phc,password_version,version,auth_sequence,initial_password_suggestion,theme)
 VALUES($1,'migration@example.test','migration-user','Owned migration','admin','owned-phc-sentinel',3,7,5,false,'dark')`, user)
	exec(`INSERT INTO agenteam_secret.secret_master_registry(version,fingerprint,registration_id) VALUES(1,decode(repeat('01',32),'hex'),$1)`, id())
	exec(`INSERT INTO agenteam_secret.secret_payloads(payload_id,scope,owner_kind,owner_id,format,algorithm,data_nonce,ciphertext,wrap_nonce,wrapped_dek,master_version,wrap_revision)
 VALUES($1,'system',1,$2,1,'AES-256-GCM',decode(repeat('02',12),'hex'),decode(repeat('03',32),'hex'),decode(repeat('04',12),'hex'),decode(repeat('05',48),'hex'),1,1)`, payload, credential)
	exec(`INSERT INTO agenteam_secret.secrets(id,scope,purpose,version,current_payload_id) VALUES($1,'system','smtp',4,$2)`, credential, payload)
	exec(`INSERT INTO agenteam_account.smtp_settings(singleton,id,version,configured,enabled,host,port,tls_mode,auth_username,from_address,password_ref)
 VALUES(true,$1,9,true,false,'smtp.example.test',465,'tls','owned-user','sender@example.test',$2)`, smtp, credential)
	exec(`UPDATE agenteam_outbound.proxy_settings SET version=6,mode='proxy',credential_id=$1,gateway=jsonb_build_object('protocol','http','host','10.11.12.13','port',3128,'username','owned-proxy','credential_present',true)`, credential)
	exec(`INSERT INTO agenteam_audit.audit_records(id,scope,actor_kind,user_id,session_id,action,outcome,resource_kind,metadata,semantic_digest,producer,cause_ref,ordinal)
 VALUES($1,'system','human',$2,$3,'outbound.policy.update','success','outbound_policy','{"version":"8","rule_count":"1"}',$4,'outbound.policy',$4,0)`, audit, user, session, digest)
	exec(`UPDATE agenteam_outbound.outbound_policy SET version=8,rules='[{"cidr":"10.0.0.0/8","ports":"all","allow_http":true}]'`)
	exec(`INSERT INTO agenteam_outbound.outbound_policy_receipts VALUES($1,$1,$2,8,1,$3,clock_timestamp())`, digest, user, audit)
	exec(`INSERT INTO agenteam_outbound.proxy_receipts VALUES($1,'owned',decode(repeat('06',32),'hex'),$2,6,$3,clock_timestamp())`, digest, user, audit)
	exec(`INSERT INTO agenteam_outbound.proxy_credential_operations VALUES($1,$2,$3,$4,6)`, id(), id(), id(), credential)
	// Include xmin to reject accidental UPDATE/re-encryption, not only value loss.
	tables := []string{"agenteam_account.users", "agenteam_account.smtp_settings", "agenteam_secret.secret_master_registry", "agenteam_secret.secret_payloads", "agenteam_secret.secrets", "agenteam_outbound.proxy_settings", "agenteam_outbound.proxy_receipts", "agenteam_outbound.proxy_credential_operations", "agenteam_audit.audit_records"}
	snapshot := func(table string) string {
		t.Helper()
		var value string
		if err := conn.QueryRow(testContext(t), `SELECT coalesce(string_agg(row_to_json(r)::text||r.xmin::text,'' ORDER BY row_to_json(r)::text),'') FROM `+table+` r`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := make(map[string]string)
	for _, table := range tables {
		before[table] = snapshot(table)
	}
	current, err := postgres.NewSource(outboxMigrationFiles(t, "00145"), nil)
	if err != nil {
		t.Fatal(err)
	}
	m := migrateVersion(current, 145)
	var version, receipts int64
	var mode, rules string
	if err := conn.QueryRow(testContext(t), `SELECT version,rule_mode,rules::text,(SELECT count(*) FROM agenteam_outbound.outbound_policy_receipts) FROM agenteam_outbound.outbound_policy`).Scan(&version, &mode, &rules, &receipts); err != nil || version != 1 || mode != "denylist" || receipts != 0 {
		t.Fatal("new rule default/reset differs", err)
	}
	decoded, err := outbound.DecodeRules([]byte(rules))
	if err != nil || string(decoded.JSON()) != string(outbound.DefaultRules().JSON()) {
		t.Fatal("SQL default differs from visible engine default", err)
	}
	for _, table := range tables {
		if snapshot(table) != before[table] {
			t.Fatal("migration rewrote unrelated domain", table)
		}
	}
	// A restart must not reset administrator changes for a second time.
	exec(`UPDATE agenteam_outbound.outbound_policy SET version=2,rule_mode='allowlist',rules='[{"target":"*.example.test","ports":[443]}]'`)
	if result := m.Migrate(t.Context()); !result.Migrated || result.Version != 145 {
		t.Fatal("repeat migration failed", result.Fault)
	}
	if err := conn.QueryRow(testContext(t), `SELECT version,rule_mode FROM agenteam_outbound.outbound_policy`).Scan(&version, &mode); err != nil || version != 2 || mode != "allowlist" {
		t.Fatal("repeat migration reset new rules", err)
	}
	// A legal near-1MiB body becomes larger when jsonb adds spaces. Exercise
	// the real SQL bound and production readPolicy compaction, not a JSON stub.
	ports := make([]uint16, 256)
	for i := range ports {
		ports[i] = uint16(65535 - i)
	}
	selected, err := outbound.SelectedPorts(ports...)
	if err != nil {
		t.Fatal(err)
	}
	large := make([]outbound.Rule, 512)
	for i := range large {
		target := fmt.Sprintf("%d.%s.%s.%s.%s", i, strings.Repeat("a", 63), strings.Repeat("b", 63), strings.Repeat("c", 63), strings.Repeat("d", 56))
		large[i], err = outbound.NewRule(target, selected)
		if err != nil {
			t.Fatal(err)
		}
	}
	ruleSet, err := outbound.NewRules(large...)
	if err != nil || len(ruleSet.JSON()) >= 1<<20 {
		t.Fatal("maximum request fixture invalid", err)
	}
	exec(`UPDATE agenteam_outbound.outbound_policy SET version=3,rules=$1`, ruleSet.JSON())
	var storedBytes int
	if err := conn.QueryRow(testContext(t), `SELECT octet_length(rules::text) FROM agenteam_outbound.outbound_policy`).Scan(&storedBytes); err != nil || storedBytes <= 1<<20 {
		t.Fatal("fixture did not cross formatted-jsonb limit", err)
	}
	store := openStore(t, cfg)
	service, err := outbound.NewPolicyService(store, migrationReadOnlyAudit{}, outbound.Authorizations{})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Reload(testContext(t)); err != nil || !service.Status().Available {
		t.Fatal("production policy read rejected legal maximum rules", err)
	}
}
