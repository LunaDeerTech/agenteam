//go:build integration

package security_test

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func secretOnStore(t *testing.T, f *secretFixture, store *postgres.Store, keys secret.Keyring) *secret.Service {
	t.Helper()
	auth := &auditAuthority{store: store}
	usage := &secretAuthority{auth}
	auditing, err := audit.New(store, auditKeys(t), audit.Authorizations{Sessions: auth, System: auth, Projects: auth})
	if err != nil {
		t.Fatal(err)
	}
	service, err := secret.New(store, keys, auditing, secret.Authorizations{Sessions: auth, System: auth, Projects: usage, Usage: usage})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Initialize(auditContext(t)); err != nil {
		t.Fatal(err)
	}
	return service
}
func proxySecret(t *testing.T, f *secretFixture, keys secret.Keyring, after bool) (*secret.Service, *commitProxy, *postgres.Store) {
	t.Helper()
	proxy := newCommitProxy(t, net.JoinHostPort("127.0.0.1", f.db.Fixture.Port), after)
	proxy.armed.Store(false)
	u, _ := url.Parse(f.db.Fixture.URL(f.db.Name))
	u.Host = proxy.listener.Addr().String()
	store := openAuditStore(t, f.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
	return secretOnStore(t, f, store, keys), proxy, store
}
func TestSecretNonceConcurrentRangesAndRollbackBurn(t *testing.T) {
	f := newSecretFixture(t)
	services := make([]*secret.Service, 4)
	for i := range services {
		services[i] = f.reopen(t, masterKeys(t, 1, 1))
	}
	requests := make([]sc.WriteRequest, 40)
	for i := range requests {
		requests[i] = f.request(t, sc.Create, []byte(fmt.Sprintf("value %d", i)))
	}
	errors := make(chan error, len(requests))
	var wg sync.WaitGroup
	for i, r := range requests {
		wg.Add(1)
		go func(i int, r sc.WriteRequest) {
			defer wg.Done()
			_, err := services[i%len(services)].ExecuteWrite(auditContext(t), r)
			errors <- err
		}(i, r)
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	var total, unique int
	var high int64
	if err := f.store.QueryRow(auditContext(t), `SELECT count(*),count(DISTINCT nonce) FROM (SELECT wrap_nonce AS nonce FROM agenteam_secret.secret_payloads UNION ALL SELECT canary_nonce FROM agenteam_secret.secret_master_registry) n`).Scan(&total, &unique); err != nil {
		t.Fatal(err)
	}
	if total != 81 || unique != total {
		t.Fatalf("nonce collision %d/%d", unique, total)
	}
	if err := f.store.QueryRow(auditContext(t), `SELECT nonce_high_water FROM agenteam_secret.secret_master_registry WHERE version=1`).Scan(&high); err != nil || high != 5*1024 {
		t.Fatalf("high water %d %v", high, err)
	}
	prepared, err := f.secret.PrepareWrite(auditContext(t), f.request(t, sc.Create, []byte("roll back")))
	if err != nil {
		t.Fatal(err)
	}
	var burned []byte
	result := f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
		result, err := f.secret.ApplyPreparedWriteInTx(ctx, tx, prepared)
		if err != nil {
			return err
		}
		e, _ := f.store.InTx(tx)
		if err = e.QueryRow(ctx, `SELECT wrap_nonce FROM agenteam_secret.secret_payloads WHERE payload_id=(SELECT current_payload_id FROM agenteam_secret.secrets WHERE id=$1)`, result.Metadata.CredentialRef.Details().ID.String()).Scan(&burned); err != nil {
			return err
		}
		return deny(foundation.InvalidState)
	})
	if result.State() != foundation.NotCommitted {
		t.Fatal("rollback not enforced")
	}
	f.create(t, []byte("after rollback"))
	var reused bool
	if err = f.store.QueryRow(auditContext(t), `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secret_payloads WHERE wrap_nonce=$1)`, burned).Scan(&reused); err != nil || reused {
		t.Fatal("rollback nonce reused", err)
	}
	// The supported minimum pool is two. Holding one Rows checkout leaves only
	// one for Apply: borrowing a second connection inside that Tx would stall.
	one := openAuditStore(t, f.db.Config(t, map[string]string{"MAX_CONNS": "2"}))
	s := secretOnStore(t, f, one, masterKeys(t, 1, 1))
	p, err := s.PrepareWrite(auditContext(t), f.request(t, sc.Create, []byte("one connection")))
	if err != nil {
		t.Fatal(err)
	}
	held, err := one.Query(auditContext(t), `SELECT 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if !held.Next() {
		t.Fatal("fixture did not hold its spare checkout")
	}
	result = one.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
		_, err := s.ApplyPreparedWriteInTx(ctx, tx, p)
		return err
	})
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
}
func TestSecretNonceUnknownRangeNeverConsumed(t *testing.T) {
	f := newSecretFixture(t)
	service, proxy, _ := proxySecret(t, f, masterKeys(t, 1, 1), true)
	proxy.armed.Store(true)
	ctx := auditContext(t)
	request := f.request(t, sc.Create, []byte("unknown reservation"))
	done := make(chan error, 1)
	go func() { _, err := service.PrepareWrite(ctx, request); done <- err }()
	select {
	case <-proxy.reached:
	case <-ctx.Done():
		t.Fatal("nonce COMMIT barrier missing")
	}
	var high int64
	if err := f.store.QueryRow(ctx, `SELECT nonce_high_water FROM agenteam_secret.secret_master_registry WHERE version=1`).Scan(&high); err != nil || high != 2048 {
		t.Fatalf("real reserved high=%d %v", high, err)
	}
	close(proxy.release)
	select {
	case err := <-done:
		requireCode(t, err, foundation.CommitUnknown)
	case <-ctx.Done():
		t.Fatal("unknown reserve hung")
	}
	created, err := service.ExecuteWrite(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	var nonce []byte
	if err = f.store.QueryRow(ctx, `SELECT wrap_nonce FROM agenteam_secret.secret_payloads WHERE payload_id=(SELECT current_payload_id FROM agenteam_secret.secrets WHERE id=$1)`, created.Metadata.CredentialRef.Details().ID.String()).Scan(&nonce); err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint64(nonce[4:]) <= uint64(high) {
		t.Fatal("unconfirmed nonce range consumed")
	}
}
func TestSecretRegistryStartupAndNonceCeiling(t *testing.T) {
	t.Run("wrong_canary_and_history", func(t *testing.T) {
		f := newSecretFixture(t)
		finishSecretRotation(t, f.secret)
		f.create(t, []byte("must retain old key"))
		initWith := func(keys secret.Keyring) error {
			s, err := secret.New(f.store, keys, f.service, secret.Authorizations{})
			if err != nil {
				return err
			}
			return s.Initialize(auditContext(t))
		}
		if err := initWith(masterKeys(t, 2, 2)); err == nil {
			t.Fatal("missing live old key accepted")
		}
		material := make([]byte, 32)
		for i := range material {
			material[i] = byte(32 + i)
		}
		historical := fmt.Sprintf(`{"format":1,"current_version":"3","keys":[{"version":"3","key_b64":"%s"}]}`, base64.StdEncoding.EncodeToString(material))
		keys, err := secret.LoadKeyring(historical, auditKeys(t))
		if err != nil {
			t.Fatal(err)
		}
		if err = initWith(keys); err == nil {
			t.Fatal("same material registered under different historical version")
		}
		wrong := fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"%s"}]}`, base64.StdEncoding.EncodeToString(make([]byte, 32)))
		keys, err = secret.LoadKeyring(wrong, auditKeys(t))
		if err != nil {
			t.Fatal(err)
		}
		if err = initWith(keys); err == nil {
			t.Fatal("version label substituted for key validation")
		}
		if _, err = f.store.Exec(auditContext(t), `UPDATE agenteam_secret.secret_master_registry SET canary_ciphertext=set_byte(canary_ciphertext,0,get_byte(canary_ciphertext,0)#1) WHERE version=1`); err != nil {
			t.Fatal(err)
		}
		if err = initWith(masterKeys(t, 1, 1)); err == nil {
			t.Fatal("bad canary accepted")
		}
	})
	t.Run("last_partial_range", func(t *testing.T) {
		f := newSecretFixture(t)
		// Move only this task-owned registry forward, never backwards. This tests
		// the last partial interval without billions of encryptions.
		if _, err := f.store.Exec(auditContext(t), `UPDATE agenteam_secret.secret_master_registry SET nonce_high_water=4294967292 WHERE version=1`); err != nil {
			t.Fatal(err)
		}
		s := f.reopen(t, masterKeys(t, 1, 1))
		if _, err := s.ExecuteWrite(auditContext(t), f.request(t, sc.Create, []byte("last pair"))); err != nil {
			t.Fatal(err)
		}
		if _, err := s.PrepareWrite(auditContext(t), f.request(t, sc.Create, []byte("overflow"))); err == nil {
			t.Fatal("nonce ceiling exceeded")
		}
		var high int64
		if err := f.store.QueryRow(auditContext(t), `SELECT nonce_high_water FROM agenteam_secret.secret_master_registry WHERE version=1`).Scan(&high); err != nil || high != 4294967295 {
			t.Fatal("ceiling registry", err)
		}
	})
}

func TestSecretMutationUnknownLookupAndCurrentAuthorization(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(fmt.Sprint(after), func(t *testing.T) {
			f := newSecretFixture(t)
			service, proxy, store := proxySecret(t, f, masterKeys(t, 1, 1), after)
			ctx := auditContext(t)
			request := f.request(t, sc.Create, []byte("uncertain"))
			prepared, err := service.PrepareWrite(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			proxy.armed.Store(true)
			done := make(chan foundation.CommitResult, 1)
			go func() {
				done <- store.WithinTx(ctx, txCause(t), func(ctx context.Context, tx foundation.Tx) error {
					_, err := service.ApplyPreparedWriteInTx(ctx, tx, prepared)
					return err
				})
			}()
			select {
			case <-proxy.reached:
			case <-time.After(5 * time.Second):
				t.Fatal("write barrier missing")
			}
			lookup, err := service.LookupWrite(ctx, prepared)
			if err != nil || lookup.Observed != after {
				t.Fatalf("observation before resolution %t %v", lookup.Observed, err)
			}
			close(proxy.release)
			select {
			case result := <-done:
				if result.State() != foundation.Unknown {
					t.Fatal("lost COMMIT misclassified", result.State())
				}
			case <-ctx.Done():
				t.Fatal("write hung")
			}
			if _, err = f.store.Exec(ctx, `UPDATE audit_fixture.sessions SET active=false`); err != nil {
				t.Fatal(err)
			}
			_, err = service.LookupWrite(ctx, prepared)
			requireCode(t, err, foundation.SessionRevoked)
			if _, err = f.store.Exec(ctx, `UPDATE audit_fixture.sessions SET active=true`); err != nil {
				t.Fatal(err)
			}
			if _, err = service.ExecuteWrite(ctx, request); err != nil {
				t.Fatal("reconcile by command", err)
			}
			var count int
			if err = f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_secret.secrets`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("duplicate secret %d %v", count, err)
			}
		})
	}
}
