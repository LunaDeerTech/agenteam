//go:build integration

package security_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func finishSecretRotation(t *testing.T, s *secret.Service) {
	t.Helper()
	for range 20 {
		done, err := s.MaintenanceStep(auditContext(t))
		if err != nil {
			t.Fatal(err)
		}
		if done {
			return
		}
	}
	t.Fatal("rotation did not converge")
}
func (f *secretFixture) reopen(t *testing.T, keys secret.Keyring) *secret.Service {
	t.Helper()
	s, err := secret.New(f.store, keys, f.service, secret.Authorizations{Sessions: f.auth, System: f.auth, Projects: f.usage, Usage: f.usage})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Initialize(auditContext(t)); err != nil {
		t.Fatal(err)
	}
	return s
}
func TestSecretRotationCASReopenAndRetirement(t *testing.T) {
	f := newSecretFixture(t)
	finishSecretRotation(t, f.secret)
	refs := make([]sc.CredentialRef, 51)
	for i := range refs {
		refs[i] = f.create(t, []byte("rotating value")).Metadata.CredentialRef
	}
	lateRequest := f.request(t, sc.Create, []byte("late old key"))
	late, err := f.secret.PrepareWrite(auditContext(t), lateRequest)
	if err != nil {
		t.Fatal(err)
	}
	var originalPayload string
	var ciphertext, nonce []byte
	if err = f.store.QueryRow(auditContext(t), `SELECT p.payload_id::text,p.ciphertext,p.data_nonce FROM agenteam_secret.secret_payloads p JOIN agenteam_secret.secrets s ON s.current_payload_id=p.payload_id WHERE s.id=$1`, refs[50].Details().ID.String()).Scan(&originalPayload, &ciphertext, &nonce); err != nil {
		t.Fatal(err)
	}
	next := f.reopen(t, masterKeys(t, 2, 1, 2))
	staleResult := f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
		if err := f.store.AcquireAll(ctx, tx, late.RequiredLocks()); err != nil {
			return err
		}
		_, err := f.secret.ApplyPreparedWriteInTx(ctx, tx, late)
		return err
	})
	requireCode(t, staleResult.Fault(), foundation.InvalidState)
	if _, err = f.secret.PrepareWrite(auditContext(t), f.request(t, sc.Create, []byte("stale"))); err == nil {
		t.Fatal("old process prepared a new old-key write")
	}
	prepared, err := next.PrepareRewrap(auditContext(t))
	if err != nil {
		t.Fatal(err)
	}
	// Replace a value after preparation. Its old candidate is discarded while
	// immutable receipt digests and other live values still migrate.
	update := f.request(t, sc.Update, []byte("new current value"))
	update.Ref = refs[0]
	update.ExpectedVersion = 1
	if _, err = next.ExecuteWrite(auditContext(t), update); err != nil {
		t.Fatal(err)
	}
	var report secret.RewrapReport
	result := f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
		var err error
		report, err = next.ApplyPreparedRewrapInTx(ctx, tx, prepared)
		return err
	})
	if result.State() != foundation.Committed || report.Applied != 99 {
		t.Fatalf("CAS count %d state %s", report.Applied, result.State())
	}
	// Applying the same sealed batch twice is safe and cannot double count.
	result = f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
		var err error
		report, err = next.ApplyPreparedRewrapInTx(ctx, tx, prepared)
		return err
	})
	if result.State() != foundation.Committed || report.Applied != 0 {
		t.Fatal("duplicate CAS changed progress")
	}
	// A restarted instance resumes only persisted checkpoint/current row state.
	restarted := f.reopen(t, masterKeys(t, 2, 1, 2))
	finishSecretRotation(t, restarted)
	var old, count int
	var retired bool
	if err = f.store.QueryRow(auditContext(t), `SELECT count(*) FROM agenteam_secret.secret_payloads WHERE master_version<>2`).Scan(&old); err != nil || old != 0 {
		t.Fatal("old payloads remain", err)
	}
	if err = f.store.QueryRow(auditContext(t), `SELECT retireable FROM agenteam_secret.secret_master_registry WHERE version=1`).Scan(&retired); err != nil || !retired {
		t.Fatal("old key not fenced", err)
	}
	if err = f.store.QueryRow(auditContext(t), `SELECT processed FROM agenteam_secret.secret_rotation_runs WHERE target_version=2`).Scan(&count); err != nil || count != 101 {
		t.Fatalf("durable count=%d %v", count, err)
	}
	var after, afterNonce []byte
	var revision int64
	if err = f.store.QueryRow(auditContext(t), `SELECT ciphertext,data_nonce,wrap_revision FROM agenteam_secret.secret_payloads WHERE payload_id=$1`, originalPayload).Scan(&after, &afterNonce, &revision); err != nil || !bytes.Equal(after, ciphertext) || !bytes.Equal(afterNonce, nonce) || revision != 2 {
		t.Fatal("rewrap altered business encryption", err)
	}
	withoutOld := f.reopen(t, masterKeys(t, 2, 2))
	owner, actor := f.bind(t, refs[0], sc.Model)
	lease := f.acquire(t, refs[0], owner, actor)
	if got := readMaterial(t, withoutOld, actor, lease.LeaseID); string(got) != "new current value" {
		t.Fatal("removed-key restart value")
	}
	// Old command receipts also rewrap and remain replayable without key 1.
	if _, err = withoutOld.ExecuteWrite(auditContext(t), update); err != nil {
		t.Fatal("receipt not rewrapped", err)
	}
}
func TestSecretRotationCorruptionStopsWritesButKeepsUnrelatedAuthorizedRead(t *testing.T) {
	f := newSecretFixture(t)
	finishSecretRotation(t, f.secret)
	first := f.create(t, []byte("first good value"))
	bad := f.create(t, []byte("will be corrupted"))
	other := f.create(t, []byte("unrelated"))
	_ = first
	next := f.reopen(t, masterKeys(t, 2, 1, 2))
	// Startup already verified this version. A later bad row must fail the
	// running rotation, preserve ciphertext, and never become a retired key.
	if _, err := f.store.Exec(auditContext(t), `UPDATE agenteam_secret.secret_payloads SET wrapped_dek=set_byte(wrapped_dek,0,get_byte(wrapped_dek,0)#1) WHERE payload_id=(SELECT current_payload_id FROM agenteam_secret.secrets WHERE id=$1)`, bad.Metadata.CredentialRef.Details().ID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := next.MaintenanceStep(auditContext(t)); err == nil {
		t.Fatal("corrupted payload skipped")
	}
	if next.Status().Available || next.Status().Rotation != "failed" {
		t.Fatal("failed component reports availability")
	}
	if _, err := next.ExecuteWrite(auditContext(t), f.request(t, sc.Create, []byte("must reject"))); err == nil {
		t.Fatal("failed component writes")
	}
	owner, actor := f.bind(t, other.Metadata.CredentialRef, sc.Model)
	lease := f.acquire(t, other.Metadata.CredentialRef, owner, actor)
	if got := readMaterial(t, next, actor, lease.LeaseID); string(got) != "unrelated" {
		t.Fatal("unrelated authorized read")
	}
	owner, actor = f.bind(t, bad.Metadata.CredentialRef, sc.Model)
	lease = f.acquire(t, bad.Metadata.CredentialRef, owner, actor)
	if _, err := next.ReadCredentialForRequest(auditContext(t), actor, lease.LeaseID); err == nil {
		t.Fatal("corrupted value read")
	}
	var retired bool
	var code, state string
	if err := f.store.QueryRow(auditContext(t), `SELECT retireable FROM agenteam_secret.secret_master_registry WHERE version=1`).Scan(&retired); err != nil || retired {
		t.Fatal("failed key retired", err)
	}
	if err := f.store.QueryRow(auditContext(t), `SELECT state,safe_error FROM agenteam_secret.secret_rotation_runs WHERE target_version=2`).Scan(&state, &code); err != nil || state != "failed" || code != "SECRET_DECRYPT_FAILED" {
		t.Fatalf("failure journal %s %s %v", state, code, err)
	}
}
