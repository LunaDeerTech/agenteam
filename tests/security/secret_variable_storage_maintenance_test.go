//go:build integration

package security_test

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestSecretVariableStorageSQLRotationDeletedOwnerAndCleanup(t *testing.T) {
	v := newSecretVariableStorageFixture(t)
	finishSecretRotation(t, v.secret)
	value := "historical-value-canary"
	originalIntent := v.intent(t, sc.Create, "rotation-create", 0, &value)
	p := v.prepare(t, originalIntent, sc.ProjectVariableWriteNotObserved(), false)
	created, commit := v.apply(t, p, nil)
	original := requireSecretVariableStored(t, created, commit, sc.ProjectVariableCreated, 1)
	current := created
	// One create + 99 metadata receipts + one delete = 101 kind3 receipts.
	// The Credential is gone before rotation; all lock ownership is historical.
	for n := range 99 {
		intent := v.intent(t, sc.Update, fmt.Sprintf("rotation-metadata-%d", n), f.Version(n+2), nil)
		prepared := v.prepare(t, intent, current, false)
		current, commit = v.apply(t, prepared, nil)
		requireSecretVariableStored(t, current, commit, sc.ProjectVariableUnchanged, 1)
		prepared.Destroy()
	}
	remove := v.prepare(t, v.intent(t, sc.Delete, "rotation-delete", 101, nil), current, false)
	deleted, commit := v.apply(t, remove, nil)
	requireSecretVariableStored(t, deleted, commit, sc.ProjectVariableDeleted, 2)
	if got := v.counts(t); got != (secretVariableStorageCounts{0, 101, 101, 2}) {
		t.Fatal("historical payload population", got)
	}
	var payload string
	var ciphertext, nonce []byte
	if err := v.store.QueryRow(auditContext(t), `SELECT p.payload_id::text,p.ciphertext,p.data_nonce FROM agenteam_secret.secret_payloads p JOIN agenteam_secret.project_variable_receipts r ON r.digest_payload_id=p.payload_id WHERE r.id=$1`, original.ReceiptID.String()).Scan(&payload, &ciphertext, &nonce); err != nil {
		t.Fatal(err)
	}
	reopen := func(versions ...int64) *secret.Service {
		s, err := secret.New(v.store, masterKeys(t, 2, versions...), v.service, secret.Authorizations{Sessions: v.ports, System: v.ports, Projects: v.usage, ProjectVariables: v.authority})
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Initialize(auditContext(t)); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.StopMaintenance)
		return s
	}
	v.secret = reopen(1, 2)
	finishSecretRotation(t, v.secret)
	var old int
	var retired bool
	if err := v.store.QueryRow(auditContext(t), `SELECT (SELECT count(*) FROM agenteam_secret.secret_payloads WHERE master_version<>2),(SELECT retireable FROM agenteam_secret.secret_master_registry WHERE version=1)`).Scan(&old, &retired); err != nil || old != 0 || !retired {
		t.Fatal("kind3 rotation fence", err)
	}
	var after, afterNonce []byte
	var revision int64
	if err := v.store.QueryRow(auditContext(t), `SELECT ciphertext,data_nonce,wrap_revision FROM agenteam_secret.secret_payloads WHERE payload_id=$1`, payload).Scan(&after, &afterNonce, &revision); err != nil || !bytes.Equal(after, ciphertext) || !bytes.Equal(afterNonce, nonce) || revision != 2 {
		t.Fatal("rewrap changed immutable receipt", err)
	}
	// Startup validates a real kind3 sample and canaries without retired key1.
	v.secret = reopen(2)
	replay := v.prepare(t, originalIntent, created, true)
	got, commit := v.apply(t, replay, nil)
	replayed := requireSecretVariableStored(t, got, commit, sc.ProjectVariableCreated, 1)
	if replayed.ReceiptID != original.ReceiptID {
		t.Fatal("key retirement lost original receipt")
	}
	operation := newID[sc.LifecycleOperation](t)
	cause, _ := sc.NewLifecycleCause(operation, 2, true)
	registration, _ := i.RegisterService(i.ProjectLifecycle)
	actor, _ := registration.Actor(operation.String(), v.scope)
	if _, err := v.store.Exec(auditContext(t), `UPDATE audit_fixture.projects SET state='deleting',stopped=true,operation_id=$2,version=2 WHERE id=$1`, v.project.String(), operation.String()); err != nil {
		t.Fatal(err)
	}
	report, err := v.secret.CleanupProject(auditContext(t), actor, cause, v.project, nil)
	if err != nil || report.Completed {
		t.Fatal("first 100 of 101 cleanup", err)
	}
	if got := v.counts(t); got != (secretVariableStorageCounts{0, 1, 1, 2}) {
		t.Fatal("first bounded cleanup", got)
	}
	report, err = v.secret.CleanupProject(auditContext(t), actor, cause, v.project, &report.Checkpoint)
	if err != nil || !report.Completed {
		t.Fatal("final cleanup", err)
	}
	if got := v.counts(t); got != (secretVariableStorageCounts{0, 0, 0, 2}) {
		t.Fatal("final receipt/payload cleanup", got)
	}
	if report, err = v.secret.CleanupProject(auditContext(t), actor, cause, v.project, &report.Checkpoint); err != nil || !report.Completed {
		t.Fatal("cleanup replay", err)
	}
}

func TestSecretVariableStorageSQLClosedConstraints(t *testing.T) {
	v := newSecretVariableStorageFixture(t)
	value := "constraint-fixture"
	one := v.prepare(t, v.intent(t, sc.Create, "constraint-one", 0, &value), sc.ProjectVariableWriteNotObserved(), false)
	first, commit := v.apply(t, one, nil)
	oneResult := requireSecretVariableStored(t, first, commit, sc.ProjectVariableCreated, 1)
	v.variable = newID[i.ProjectVariable](t)
	two := v.prepare(t, v.intent(t, sc.Create, "constraint-two", 0, &value), sc.ProjectVariableWriteNotObserved(), false)
	second, commit := v.apply(t, two, nil)
	twoResult := requireSecretVariableStored(t, second, commit, sc.ProjectVariableCreated, 1)
	before := v.counts(t)
	for _, test := range []struct {
		name, sql, state string
		args             []any
	}{
		{"v4-variable", `UPDATE agenteam_secret.project_variable_receipts SET variable_id='01900000-0000-4000-8000-000000000001' WHERE id=$1`, "23514", []any{oneResult.ReceiptID.String()}},
		{"effect-create-mismatch", `UPDATE agenteam_secret.project_variable_receipts SET effect='replace',result_version=2 WHERE id=$1`, "23514", []any{oneResult.ReceiptID.String()}},
		{"create-expected-present", `UPDATE agenteam_secret.project_variable_receipts SET external_expected_version=1 WHERE id=$1`, "23514", []any{oneResult.ReceiptID.String()}},
		{"command-digest", `UPDATE agenteam_secret.project_variable_receipts SET command_digest='not-a-digest' WHERE id=$1`, "23514", []any{oneResult.ReceiptID.String()}},
		{"duplicate-command", `UPDATE agenteam_secret.project_variable_receipts SET command_digest=(SELECT command_digest FROM agenteam_secret.project_variable_receipts WHERE id=$2) WHERE id=$1`, "23505", []any{twoResult.ReceiptID.String(), oneResult.ReceiptID.String()}},
		{"duplicate-payload", `UPDATE agenteam_secret.project_variable_receipts SET digest_payload_id=(SELECT digest_payload_id FROM agenteam_secret.project_variable_receipts WHERE id=$2) WHERE id=$1`, "23505", []any{twoResult.ReceiptID.String(), oneResult.ReceiptID.String()}},
		{"kind3-system", `UPDATE agenteam_secret.secret_payloads SET scope='system',project_id=NULL WHERE payload_id=(SELECT digest_payload_id FROM agenteam_secret.project_variable_receipts WHERE id=$1)`, "23514", []any{oneResult.ReceiptID.String()}},
		{"kind3-length", `UPDATE agenteam_secret.secret_payloads SET ciphertext=decode(repeat('00',47),'hex') WHERE payload_id=(SELECT digest_payload_id FROM agenteam_secret.project_variable_receipts WHERE id=$1)`, "23514", []any{oneResult.ReceiptID.String()}},
		{"purpose-system", `UPDATE agenteam_secret.secrets SET scope='system',project_id=NULL WHERE id=$1`, "23514", []any{oneResult.Ref.Details().ID.String()}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := v.store.Exec(auditContext(t), test.sql, test.args...)
			var safe *postgres.Error
			if !errors.As(err, &safe) || safe.SQLState() != test.state {
				t.Fatalf("safe SQLSTATE %v, expected %s", err, test.state)
			}
			if v.counts(t) != before {
				t.Fatal("rejected constraint changed rows")
			}
		})
	}
}
