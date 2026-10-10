//go:build integration

package security_test

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// Each binding owns its immutable controlled D10 issuer and actual Store,
// Secret, native Audit checker and Audit service. No prepared handle crosses
// instances. This does not supply the production D10 Owner or its discovery.
type secretRecoveryBinding struct {
	store     *postgres.Store
	service   *secret.Service
	ports     *secretProjectAuditPorts
	authority *secretVariableStorageAuthority
}

func newSecretRecoveryBinding(t *testing.T, store *postgres.Store, keys secret.Keyring) *secretRecoveryBinding {
	t.Helper()
	checker, err := secret.NewProjectAuditAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	usage := &secretAuthority{auditAuthority: &auditAuthority{store: store}}
	ports := &secretProjectAuditPorts{secretAuthority: usage, checker: checker}
	auditing, err := audit.New(store, auditKeys(t), audit.Authorizations{Sessions: ports, System: ports, Projects: &secretProjectAuditAppendPorts{ports}})
	if err != nil {
		t.Fatal(err)
	}
	authority := &secretVariableStorageAuthority{store: store, ports: ports, issuer: sc.NewPlanIssuer()}
	service, err := secret.New(store, keys, auditing, secret.Authorizations{Sessions: ports, System: ports, Projects: usage, ProjectVariables: authority})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Initialize(auditContext(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.StopMaintenance)
	return &secretRecoveryBinding{store: store, service: service, ports: ports, authority: authority}
}

func secretRecoveryProxyStore(t *testing.T, fixture *secretVariableStorageFixture, address net.Addr) *postgres.Store {
	t.Helper()
	u, err := url.Parse(fixture.db.Fixture.URL(fixture.db.Name))
	if err != nil {
		t.Fatal(err)
	}
	u.Host = address.String()
	return openAuditStore(t, fixture.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
}

func (b *secretRecoveryBinding) plan(t *testing.T, intent sc.ProjectVariableIntent, basis sc.ProjectVariableWriteBasisFields) sc.ProjectVariableWritePlan {
	t.Helper()
	basis.Request = intent.Fields().Request
	locks, err := sc.ProjectVariableWriteLocks(basis.Request, basis.Ref)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := sc.NewProjectVariableWritePlan(b.authority.issuer, basis, locks)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func (b *secretRecoveryBinding) prepare(t *testing.T, intent sc.ProjectVariableIntent, plan sc.ProjectVariableWritePlan) sc.PreparedProjectVariableWrite {
	t.Helper()
	p, err := b.service.PrepareProjectVariableWrite(auditContext(t), intent, plan)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Destroy)
	return p
}

// Capture identity on the original Tx connection, never on another pool lease.
func secretRecoveryBackend(ctx context.Context, store *postgres.Store, tx f.Tx) (int, error) {
	e, err := store.InTx(tx)
	if err != nil {
		return 0, err
	}
	var pid int
	err = e.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid)
	return pid, err
}

func secretRecoveryFirstConflict(held, waiting []f.LockRequest) (f.LockRequest, f.LockRequest, error) {
	// RequiredLocks already has Foundation's complete normalized lock order.
	// For different commands the first collision can be User, not Command.
	for _, next := range waiting {
		for _, original := range held {
			if f.CompareLockKeys(next.Key, original.Key) == 0 && (next.Mode == f.Exclusive || original.Mode == f.Exclusive) {
				return original, next, nil
			}
		}
	}
	return f.LockRequest{}, f.LockRequest{}, errors.New("fixture has no conflicting original lock")
}

func secretRecoveryWaitBlocked(ctx context.Context, store *postgres.Store, database string, ownerPID, waiterPID int, owner, waiter f.LockRequest) error {
	if ownerPID <= 0 || waiterPID <= 0 || ownerPID == waiterPID || owner.Key.Validate() != nil || waiter.Key.Validate() != nil || f.CompareLockKeys(owner.Key, waiter.Key) != 0 || !owner.Mode.Valid() || !waiter.Mode.Valid() {
		return errors.New("invalid original backend or lock binding")
	}
	mode := func(value f.LockMode) string {
		if value == f.Exclusive {
			return "ExclusiveLock"
		}
		return "ShareLock"
	}
	key := uint64(owner.Key.AdvisoryKey())
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		err := store.QueryRow(ctx, `SELECT EXISTS (
 SELECT 1 FROM pg_locks a JOIN pg_locks b
 ON a.locktype=b.locktype AND a.database=b.database AND a.classid=b.classid AND a.objid=b.objid AND a.objsubid=b.objsubid
 JOIN pg_stat_activity aa ON aa.pid=a.pid JOIN pg_stat_activity bb ON bb.pid=b.pid
 WHERE a.pid=$1 AND b.pid=$2 AND a.locktype='advisory' AND a.granted AND NOT b.granted
 AND a.database=(SELECT oid FROM pg_database WHERE datname=$3) AND aa.datname=$3 AND bb.datname=$3
 AND a.classid::bigint=$4 AND a.objid::bigint=$5 AND a.objsubid=1
 AND a.mode=$6 AND b.mode=$7 AND $1=ANY(pg_blocking_pids($2)))`, ownerPID, waiterPID, database, int64(key>>32), int64(uint32(key)), mode(owner.Mode), mode(waiter.Mode)).Scan(&blocked)
		if err != nil {
			return err
		}
		if blocked {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// A closed done channel is the actual goroutine return, separate from a proxy
// barrier, server COMMIT frame, lock rejoin, and listener/forwarder retirement.
type secretRecoveryFlight[T any] struct {
	done   chan struct{}
	result T
}

func startSecretRecoveryFlight[T any](fn func() T) *secretRecoveryFlight[T] {
	flight := &secretRecoveryFlight[T]{done: make(chan struct{})}
	go func() {
		defer close(flight.done)
		flight.result = fn()
	}()
	return flight
}

func (flight *secretRecoveryFlight[T]) wait(ctx context.Context) (T, error) {
	select {
	case <-flight.done:
		return flight.result, nil
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

// The original late proxy has only t.Cleanup. Expose its same once-protected
// retirement here so a test can actually join it before declaring success.
// Existing proxy source and callbacks stay unchanged and remain idempotent.
func closeSecretRecoveryLateProxy(p *outboundLateCommitProxy) {
	p.once.Do(func() {
		close(p.quit)
		_ = p.listener.Close()
		p.mu.Lock()
		for connection := range p.connections {
			_ = connection.Close()
		}
		p.mu.Unlock()
		p.wg.Wait()
	})
}
