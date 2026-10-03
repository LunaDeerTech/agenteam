//go:build integration

package security_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type rejectSecretAudit struct{}

func (rejectSecretAudit) AppendInTx(context.Context, foundation.Tx, ac.Entry, ac.AppendKey) (ac.AppendReceipt, error) {
	return ac.AppendReceipt{}, errors.New("test-only-sensitive-audit-failure")
}
func TestSecretBindingsAuthorizationAndReadAuditBoundary(t *testing.T) {
	f := newSecretFixture(t)
	created := f.create(t, []byte("private value"))
	ref := created.Metadata.CredentialRef
	owner, actor := f.bind(t, ref, sc.Model)
	lease := f.acquire(t, ref, owner, actor)
	// Model's retained owner can use the stable current reference after removal
	// from live configuration. This deliberately does not grant MCP semantics.
	if _, err := f.store.Exec(auditContext(t), `UPDATE audit_fixture.secret_bindings SET active=false,retained=true WHERE owner_id=$1`, owner.Details().ID); err != nil {
		t.Fatal(err)
	}
	if got := readMaterial(t, f.secret, actor, lease.LeaseID); string(got) != "private value" {
		t.Fatal("retained Model read")
	}
	r := f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
		_, err := f.secret.AcquireCredentialLeaseInTx(ctx, tx, actor, ref, owner)
		return err
	})
	requireCode(t, r.Fault(), foundation.Forbidden)
	mcpRequest := f.request(t, sc.Create, []byte("mcp material"))
	mcpRequest.Purpose = sc.MCP
	mcp, err := f.secret.ExecuteWrite(auditContext(t), mcpRequest)
	if err != nil {
		t.Fatal(err)
	}
	mcpOwner, mcpActor := f.bind(t, mcp.Metadata.CredentialRef, sc.MCP)
	mcpLease := f.acquire(t, mcp.Metadata.CredentialRef, mcpOwner, mcpActor)
	if _, err = f.store.Exec(auditContext(t), `UPDATE audit_fixture.secret_bindings SET active=false,retained=true,mcp_valid=false WHERE owner_id=$1`, mcpOwner.Details().ID); err != nil {
		t.Fatal(err)
	}
	_, err = f.secret.ReadCredentialForRequest(auditContext(t), mcpActor, mcpLease.LeaseID)
	requireCode(t, err, foundation.Forbidden)
	if _, err = f.store.Exec(auditContext(t), `UPDATE audit_fixture.projects SET state='archived'`); err != nil {
		t.Fatal(err)
	}
	_, err = f.secret.ReadCredentialForRequest(auditContext(t), actor, lease.LeaseID)
	requireCode(t, err, foundation.ProjectNotActive)
	if _, err = f.store.Exec(auditContext(t), `UPDATE audit_fixture.projects SET state='active'`); err != nil {
		t.Fatal(err)
	}
	// Registry already exists, so this initializer needs no new maintenance
	// append. A failing real read Audit must roll back without issuing material.
	denied, err := secret.New(f.store, masterKeys(t, 1, 1), rejectSecretAudit{}, secret.Authorizations{Sessions: f.auth, System: f.auth, Projects: f.usage, Usage: f.usage})
	if err != nil {
		t.Fatal(err)
	}
	if err = denied.Initialize(auditContext(t)); err != nil {
		t.Fatal(err)
	}
	material, err := denied.ReadCredentialForRequest(auditContext(t), actor, lease.LeaseID)
	if err == nil {
		t.Fatal("failed Audit returned material")
	}
	if material.Use(func([]byte) error { return nil }) == nil {
		t.Fatal("failed Audit material accessible")
	}
	var before, after int
	if err = f.store.QueryRow(auditContext(t), `SELECT count(*) FROM agenteam_secret.secret_command_receipts`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err = denied.ExecuteWrite(auditContext(t), f.request(t, sc.Create, []byte("atomic audit rollback"))); err == nil {
		t.Fatal("mutation ignored Audit failure")
	}
	if err = f.store.QueryRow(auditContext(t), `SELECT count(*) FROM agenteam_secret.secret_command_receipts`).Scan(&after); err != nil || after != before {
		t.Fatal("failed Audit left a success receipt", err)
	}
	unbound, err := secret.New(f.store, masterKeys(t, 1, 1), f.service, secret.Authorizations{})
	if err != nil {
		t.Fatal(err)
	}
	if err = unbound.Initialize(auditContext(t)); err != nil {
		t.Fatal(err)
	}
	_, err = unbound.ReadCredentialForRequest(auditContext(t), actor, lease.LeaseID)
	requireCode(t, err, foundation.DependencyUnbound)
	_, err = unbound.ExecuteWrite(auditContext(t), f.request(t, sc.Create, []byte("unbound")))
	requireCode(t, err, foundation.DependencyUnbound)
	_, err = f.secret.ReadCredentialForRequest(auditContext(t), actor, newID[sc.Lease](t))
	requireCode(t, err, foundation.NotFound)
	// Wrong scope fails both metadata and the credential lock/lease authority.
	systemRef, _ := sc.NewCredentialRef(ref.Details().ID, identity.SystemScope())
	_, err = f.secret.Metadata(auditContext(t), f.actor, systemRef)
	requireCode(t, err, foundation.NotFound)
}
func TestSecretReadUnknownNeverReturnsMaterial(t *testing.T) {
	f := newSecretFixture(t)
	created := f.create(t, []byte("never leave on unknown"))
	owner, actor := f.bind(t, created.Metadata.CredentialRef, sc.Model)
	lease := f.acquire(t, created.Metadata.CredentialRef, owner, actor)
	service, proxy, _ := proxySecret(t, f, masterKeys(t, 1, 1), true)
	proxy.armed.Store(true)
	ctx := auditContext(t)
	type result struct {
		material sc.SecretMaterial
		err      error
	}
	done := make(chan result, 1)
	go func() {
		material, err := service.ReadCredentialForRequest(ctx, actor, lease.LeaseID)
		done <- result{material, err}
	}()
	select {
	case <-proxy.reached:
	case <-ctx.Done():
		t.Fatal("resolve COMMIT barrier missing")
	}
	var count int
	if err := f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_audit.audit_records WHERE action='secret.resolve'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("read Audit not actually committed", err)
	}
	close(proxy.release)
	select {
	case got := <-done:
		requireCode(t, got.err, foundation.CommitUnknown)
		if got.material.Use(func([]byte) error { return nil }) == nil {
			t.Fatal("unknown read issued material")
		}
	case <-ctx.Done():
		t.Fatal("unknown read hung")
	}
	readMaterial(t, service, actor, lease.LeaseID)
	if err := f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_audit.audit_records WHERE action='secret.resolve'`).Scan(&count); err != nil || count != 2 {
		t.Fatal("new resolution reused unknown Audit identity", err)
	}
}
func TestSecretPreparedAndDatabaseSensitiveProjection(t *testing.T) {
	f := newSecretFixture(t)
	value := []byte("sensitive-low-entropy-unique-42")
	request := f.request(t, sc.Create, value)
	prepared, err := f.secret.PrepareWrite(auditContext(t), request)
	if err != nil {
		t.Fatal(err)
	}
	var result sc.MutationResult
	r := f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
		var err error
		result, err = f.secret.ApplyPreparedWriteInTx(ctx, tx, prepared)
		return err
	})
	if r.State() != foundation.Committed {
		t.Fatal(r.Fault())
	}
	var cipher, nonce, wrapped []byte
	if err = f.store.QueryRow(auditContext(t), `SELECT p.ciphertext,p.wrap_nonce,p.wrapped_dek FROM agenteam_secret.secret_payloads p JOIN agenteam_secret.secrets s ON s.current_payload_id=p.payload_id WHERE s.id=$1`, result.Metadata.CredentialRef.Details().ID.String()).Scan(&cipher, &nonce, &wrapped); err != nil {
		t.Fatal(err)
	}
	for _, v := range []any{prepared, struct{ p secret.PreparedWrite }{prepared}, f.secret, struct{ s *secret.Service }{f.secret}, request} {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			text := fmt.Sprintf(verb, v)
			for _, forbidden := range []string{string(value), hex.EncodeToString(cipher), fmt.Sprint(cipher), fmt.Sprint(nonce), fmt.Sprint(wrapped)} {
				if strings.Contains(text, forbidden) {
					t.Fatalf("sensitive fmt projection for %T", v)
				}
			}
		}
		raw, err := json.Marshal(v)
		if err != nil && strings.Contains(err.Error(), string(value)) {
			t.Fatal("plaintext in serialization rejection")
		}
		if strings.Contains(string(raw), string(value)) {
			t.Fatal("plaintext JSON")
		}
		var log strings.Builder
		slog.New(slog.NewTextHandler(&log, nil)).Info("projection", "value", v)
		if strings.Contains(log.String(), string(value)) || strings.Contains(log.String(), fmt.Sprint(cipher)) {
			t.Fatal("sensitive slog")
		}
	}
	// Receipts keep only a separate sealed 32-byte digest, with distinct owner
	// kind/AAD. No plaintext value or naked semantic-digest column is present.
	var count int
	if err = f.store.QueryRow(auditContext(t), `SELECT count(*) FROM agenteam_secret.secret_payloads WHERE owner_kind=2 AND octet_length(ciphertext)=48 AND ciphertext<>$1`, value).Scan(&count); err != nil || count != 1 {
		t.Fatal("encrypted receipt digest", err)
	}
	var serialized string
	if err = f.store.QueryRow(auditContext(t), `SELECT row_to_json(r)::text FROM agenteam_secret.secret_command_receipts r`).Scan(&serialized); err != nil || strings.Contains(serialized, string(value)) {
		t.Fatal("receipt plaintext", err)
	}
}
