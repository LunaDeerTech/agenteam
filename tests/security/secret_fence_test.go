//go:build integration

package security_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestSecretWriteEpochWaitsForOwnedOldWriter(t *testing.T) {
	f := newSecretFixture(t)
	finishSecretRotation(t, f.secret)
	request := f.request(t, sc.Create, []byte("old transaction"))
	prepared, err := f.secret.PrepareWrite(auditContext(t), request)
	if err != nil {
		t.Fatal(err)
	}
	ctx := auditContext(t)
	held, release := make(chan struct{}), make(chan struct{})
	writer := make(chan foundation.CommitResult, 1)
	go func() {
		writer <- f.store.WithinTx(ctx, txCause(t), func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, prepared.RequiredLocks()); err != nil {
				return err
			}
			if _, err := f.secret.ApplyPreparedWriteInTx(ctx, tx, prepared); err != nil {
				return err
			}
			close(held)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-held:
	case <-ctx.Done():
		t.Fatal("writer barrier missing")
	}
	next, err := secret.New(f.store, masterKeys(t, 2, 1, 2), f.service, secret.Authorizations{Sessions: f.auth, System: f.auth, Projects: f.usage, Usage: f.usage})
	if err != nil {
		t.Fatal(err)
	}
	initialized := make(chan error, 1)
	go func() { initialized <- next.Initialize(ctx) }()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err = f.store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=$1 AND wait_event_type='Lock' AND wait_event='advisory')`, f.db.Name).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case <-ticker.C:
		case err = <-initialized:
			t.Fatalf("fence did not wait %v", err)
		case <-ctx.Done():
			t.Fatal("fence wait not visible")
		}
	}
	var current int64
	if err = f.store.QueryRow(ctx, `SELECT current_write_version FROM agenteam_secret.secret_control`).Scan(&current); err != nil || current != 1 {
		t.Fatal("write fence advanced across live writer", err)
	}
	close(release)
	select {
	case r := <-writer:
		if r.State() != foundation.Committed {
			t.Fatal(r.Fault())
		}
	case <-ctx.Done():
		t.Fatal("writer hung")
	}
	select {
	case err = <-initialized:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("initializer hung")
	}
	finishSecretRotation(t, next)
	var old int
	if err = f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_secret.secret_payloads WHERE master_version=1`).Scan(&old); err != nil || old != 0 {
		t.Fatal("committed old writer missed by final scan", err)
	}
}
func TestSecretRotationRealUnknownReconcilesOrRemainsUnknown(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(fmt.Sprint(stop), func(t *testing.T) {
			f := newSecretFixture(t)
			finishSecretRotation(t, f.secret)
			f.create(t, []byte("rotation commit unknown"))
			f.reopen(t, masterKeys(t, 2, 1, 2))
			s, proxy, store := proxySecret(t, f, masterKeys(t, 2, 1, 2), true)
			if _, err := s.PrepareRewrap(auditContext(t)); err != nil {
				t.Fatal(err)
			} // confirmed nonce range before arming
			proxy.armed.Store(true)
			ctx := auditContext(t)
			done := make(chan error, 1)
			go func() { _, err := s.MaintenanceStep(ctx); done <- err }()
			select {
			case <-proxy.reached:
			case <-ctx.Done():
				t.Fatal("rotation COMMIT barrier missing")
			}
			var processed int
			if err := f.store.QueryRow(ctx, `SELECT processed FROM agenteam_secret.secret_rotation_runs WHERE target_version=2`).Scan(&processed); err != nil || processed != 2 {
				t.Fatal("actual batch did not commit", err)
			}
			if stop {
				store.StopAdmission()
			}
			close(proxy.release)
			select {
			case err := <-done:
				if stop {
					requireCode(t, err, foundation.CommitUnknown)
				} else if err != nil {
					t.Fatal("observed progress not reconciled", err)
				}
			case <-ctx.Done():
				t.Fatal("rotation unknown hung")
			}
			restarted := f.reopen(t, masterKeys(t, 2, 1, 2))
			finishSecretRotation(t, restarted)
			if err := f.store.QueryRow(ctx, `SELECT processed FROM agenteam_secret.secret_rotation_runs WHERE target_version=2`).Scan(&processed); err != nil || processed != 2 {
				t.Fatal("unknown batch counted twice", err)
			}
		})
	}
}

type heldResolveAudit struct {
	delegate ac.Appender
	entered  chan struct{}
	release  chan struct{}
}

func (a *heldResolveAudit) AppendInTx(ctx context.Context, tx foundation.Tx, entry ac.Entry, key ac.AppendKey) (ac.AppendReceipt, error) {
	if entry.Fields().Action == ac.SecretResolve {
		close(a.entered)
		select {
		case <-a.release:
		case <-ctx.Done():
			return ac.AppendReceipt{}, ctx.Err()
		}
	}
	return a.delegate.AppendInTx(ctx, tx, entry, key)
}
func TestSecretReadSnapshotHoldsReferenceUntilAuditCommit(t *testing.T) {
	f := newSecretFixture(t)
	created := f.create(t, []byte("old read value"))
	owner, actor := f.bind(t, created.Metadata.CredentialRef, sc.Model)
	lease := f.acquire(t, created.Metadata.CredentialRef, owner, actor)
	held := &heldResolveAudit{f.service, make(chan struct{}), make(chan struct{})}
	s, err := secret.New(f.store, masterKeys(t, 1, 1), held, secret.Authorizations{Sessions: f.auth, System: f.auth, Projects: f.usage, Usage: f.usage})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Initialize(auditContext(t)); err != nil {
		t.Fatal(err)
	}
	ctx := auditContext(t)
	type readResult struct {
		material sc.SecretMaterial
		err      error
	}
	read := make(chan readResult, 1)
	go func() {
		m, err := s.ReadCredentialForUsage(ctx, f.modelReadRequest(t, actor, lease.LeaseID))
		read <- readResult{m, err}
	}()
	select {
	case <-held.entered:
	case <-ctx.Done():
		t.Fatal("read Audit barrier missing")
	}
	update := f.request(t, sc.Update, []byte("later value"))
	update.Ref = created.Metadata.CredentialRef
	update.ExpectedVersion = 1
	done := make(chan error, 1)
	go func() { _, err := f.secret.ExecuteWrite(ctx, update); done <- err }()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err = f.store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=$1 AND wait_event_type='Lock' AND wait_event='advisory')`, f.db.Name).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case <-ticker.C:
		case err = <-done:
			t.Fatalf("value changed across in-flight read %v", err)
		case <-ctx.Done():
			t.Fatal("update wait missing")
		}
	}
	close(held.release)
	select {
	case got := <-read:
		if got.err != nil {
			t.Fatal(got.err)
		}
		defer got.material.Destroy()
		if err = got.material.Use(func(v []byte) error {
			if string(v) != "old read value" {
				t.Fatal("read snapshot changed")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("read hung")
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("update hung")
	}
	if got := readMaterial(t, f, f.secret, actor, lease.LeaseID); string(got) != "later value" {
		t.Fatal("next read did not observe update")
	}
}
