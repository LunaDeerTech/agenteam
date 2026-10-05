//go:build integration

package security_test

import (
	"bytes"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestSecretStorageReceiptAndCurrentLease(t *testing.T) {
	f := newSecretFixture(t)
	original := []byte{' ', 0, 255, 'x', ' '}
	request := f.request(t, sc.Create, original)
	created, err := f.secret.ExecuteWrite(auditContext(t), request)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f.secret.ExecuteWrite(auditContext(t), request)
	if err != nil || !replay.Metadata.CredentialRef.Equal(created.Metadata.CredentialRef) || replay.Metadata.Version != 1 {
		t.Fatalf("replay %v", err)
	}
	other, _ := sc.NewSecretMaterial([]byte("another"))
	defer other.Destroy()
	request.Value = other
	_, err = f.secret.ExecuteWrite(auditContext(t), request)
	requireCode(t, err, foundation.IdempotencyKeyReused)
	owner, actor := f.bind(t, created.Metadata.CredentialRef, sc.Model)
	lease := f.acquire(t, created.Metadata.CredentialRef, owner, actor)
	if again := f.acquire(t, created.Metadata.CredentialRef, owner, actor); again.LeaseID != lease.LeaseID {
		t.Fatal("lease duplicated")
	}
	material, err := f.secret.ReadCredentialForUsage(auditContext(t), f.modelReadRequest(t, actor, lease.LeaseID))
	if err != nil {
		t.Fatal(err)
	}
	defer material.Destroy()
	update := f.request(t, sc.Update, []byte("next value"))
	update.Ref = created.Metadata.CredentialRef
	update.ExpectedVersion = 1
	changed, err := f.secret.ExecuteWrite(auditContext(t), update)
	if err != nil || changed.Metadata.Version != 2 {
		t.Fatalf("update %v", err)
	}
	if got := readMaterial(t, f, f.secret, actor, lease.LeaseID); string(got) != "next value" {
		t.Fatal("lease pinned obsolete value")
	}
	if err = material.Use(func(v []byte) error {
		if !bytes.Equal(v, original) {
			t.Fatal("issued material changed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.secret.ExecuteWrite(auditContext(t), update); err != nil {
		t.Fatal("old expected version replay", err)
	}
	del := f.request(t, sc.Delete, nil)
	del.Ref = update.Ref
	del.ExpectedVersion = 2
	_, err = f.secret.ExecuteWrite(auditContext(t), del)
	requireCode(t, err, foundation.ResourceBusy)
	registration, _ := identity.RegisterService(identity.SecretService)
	wrong, _ := registration.Actor(newID[struct{}](t).String(), f.scope)
	_, result := f.applyModelUsage(t, f.secret, f.modelLeaseRequest(wrong, lease, owner, sc.ReleaseLeaseUsage))
	requireCode(t, result.Fault(), foundation.Forbidden)
	for range 2 {
		_, result = f.applyModelUsage(t, f.secret, f.modelLeaseRequest(actor, lease, owner, sc.ReleaseLeaseUsage))
		if result.State() != foundation.Committed {
			t.Fatal(result.Fault())
		}
	}
	if _, err = f.secret.ReadCredentialForUsage(auditContext(t), f.modelReadRequest(t, actor, lease.LeaseID)); err == nil {
		t.Fatal("released lease read")
	}
	deleted, err := f.secret.ExecuteWrite(auditContext(t), del)
	if err != nil || !deleted.Deleted || deleted.Metadata.Version != 3 {
		t.Fatal("delete", err)
	}
	if _, err = f.secret.ExecuteWrite(auditContext(t), del); err != nil {
		t.Fatal("deleted replay", err)
	}
	var secrets, receipts, payloads, audits int
	for query, dst := range map[string]*int{
		`SELECT count(*) FROM agenteam_secret.secrets`:                                    &secrets,
		`SELECT count(*) FROM agenteam_secret.secret_command_receipts`:                    &receipts,
		`SELECT count(*) FROM agenteam_secret.secret_payloads`:                            &payloads,
		`SELECT count(*) FROM agenteam_audit.audit_records WHERE action='secret.resolve'`: &audits,
	} {
		if err = f.store.QueryRow(auditContext(t), query).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	if secrets != 0 || receipts != 3 || payloads != 3 || audits != 2 {
		t.Fatalf("counts %d %d %d %d", secrets, receipts, payloads, audits)
	}
}
