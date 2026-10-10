//go:build integration

package projectvariable_test

import (
	"context"
	"errors"
	"net"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

func proxySecretOwner(t *testing.T, v *secretOwnerFixture, forwarded bool) (*secretOwnerFixture, *commitProxy) {
	t.Helper()
	proxy := newCommitProxy(t, net.JoinHostPort("127.0.0.1", v.db.Fixture.Port), forwarded)
	u, err := url.Parse(v.db.Fixture.URL(v.db.Name))
	if err != nil {
		t.Fatal(err)
	}
	u.Host = proxy.listener.Addr().String()
	raw := openStore(t, v.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable", "LOCK_TIMEOUT": "5s"}))
	store := &hookStore{fixtureStore: raw}
	ak, _ := keys(t)
	accounts, err := account.NewAuthority(store, ak)
	if err != nil {
		t.Fatal(err)
	}
	if err = accounts.Initialize(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	facts, err := pv.NewAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	// Account Session authority, D10 facts, D04, Audit, Outbox and Activity
	// all share the new Store. Existing setup services remain independent.
	base := *v.variableHTTPFixture
	base.tracked, base.accounts, base.authority = store, accounts, facts
	return assembleSecretOwnerFixture(t, &base), proxy
}

func armSecretCommit(t *testing.T, v *secretOwnerFixture, proxy *commitProxy, identity f.CommandIdentity) (<-chan lockAttempt, <-chan f.CommitResult) {
	t.Helper()
	confirm := make(chan lockAttempt, 1)
	original := make(chan f.CommitResult, 1)
	var armed, observed atomic.Bool
	v.tracked.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if cause.Kind() != f.CommandsCause || cause.Details().Primary.Canonical() != identity.Canonical() || armed.Load() {
			return nil
		}
		x, err := v.tracked.InTx(tx)
		if err != nil {
			return err
		}
		var complete bool
		// A real completed result must already have both receipt owners,
		// canonical/history, actual D10 Audit and Outbox inside this same Tx.
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_projectvariable.secret_commands c
 JOIN agenteam_projectvariable.variables v ON v.project_id=c.project_id AND v.id=c.target_id
 JOIN agenteam_projectvariable.secret_history h ON h.project_id=c.project_id AND h.operation_id=c.id AND h.event_id=c.event_id
 JOIN agenteam_secret.project_variable_receipts r ON r.id=c.d04_receipt_id AND r.project_id=c.project_id AND r.variable_id=c.target_id AND r.credential_id=v.credential_id
 JOIN agenteam_audit.audit_records a ON a.id=c.audit_id AND a.project_id=c.project_id AND a.resource_id=c.target_id AND a.action='project.secret_variable.create'
 JOIN agenteam_outbox.events e ON e.id=c.event_id AND e.project_id=c.project_id AND e.event_type='project.secret_variable_changed'
 WHERE c.project_id=$1 AND c.command_name=$2 AND c.idempotency_key=$3)`, v.project.ID.String(), identity.Command(), string(identity.Key())).Scan(&complete)
		if err != nil {
			return err
		}
		if complete && armed.CompareAndSwap(false, true) {
			var pid int32
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			proxy.targetPID.Store(pid)
		}
		return nil
	})
	v.tracked.mu.Lock()
	v.tracked.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == identity.Canonical() && result.State() == f.Unknown {
			select {
			case original <- result:
			default:
			}
		}
	}
	v.tracked.beforeLocks = func(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
		select {
		case <-proxy.reached:
		default:
			return nil
		}
		if !observed.CompareAndSwap(false, true) {
			return nil
		}
		normalized, err := oc.NormalizeLocks(locks)
		if err != nil {
			return err
		}
		if len(normalized) == 0 {
			return errors.New("confirmation requested no locks")
		}
		x, err := v.tracked.InTx(tx)
		if err != nil {
			return err
		}
		var pid int32
		if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			return err
		}
		confirm <- lockAttempt{pid, normalized[0]}
		return nil
	}
	v.tracked.mu.Unlock()
	return confirm, original
}

func TestSecretVariableOwnerCommitRecovery(t *testing.T) {
	v := newSecretOwnerFixture(t)
	for _, mode := range []string{"before-forward", "after-forward", "pending-outlives-confirmation", "stop-confirms-actual-join"} {
		t.Run(mode, func(t *testing.T) {
			forwarded := mode != "before-forward"
			remote, proxy := proxySecretOwner(t, v, forwarded)
			in := secretCreateInput(t, "RECOVERY_"+id[struct{}](t).String()[24:], []byte("recovery-private-value"))
			m := meta(t, "recovery-"+mode, nil)
			identity, err := vc.SecretVariableCommandIdentity(v.project.ID, vc.SecretCreateCommand, m.IdempotencyKey)
			if err != nil {
				t.Fatal(err)
			}
			attempts, original := armSecretCommit(t, remote, proxy, identity)
			var once sync.Once
			release := func() { once.Do(func() { close(proxy.release) }) }
			defer release()
			baseline := v.secretSnapshot(t)
			counts := v.secretCounts(t)
			writer := asyncSecretOwner(t, release, func(ctx context.Context) (vc.SecretVariableMutation, error) {
				return remote.owner.CreateSecretVariable(ctx, v.ownerBrowser.actor, m, v.project.ID, in)
			})
			awaitSecretStage(t, proxy.reached, writer)
			attempt := waitSecretAttempt(t, attempts, writer)
			if proxy.backendPID.Load() <= 0 || attempt.pid == proxy.backendPID.Load() {
				t.Fatal("confirmation is not a new physical Tx")
			}
			v.waitLock(t, attempt, false, proxy.backendPID.Load())
			if baseline != v.secretSnapshot(t) {
				t.Fatal("held real COMMIT published early")
			}
			if mode == "before-forward" || mode == "after-forward" {
				release()
				await(t, proxy.completed)
			}
			if mode == "stop-confirms-actual-join" {
				remote.owner.Stop()
			}
			result := secretReply(t, writer)
			var physical f.CommitResult
			select {
			case physical = <-original:
			default:
				t.Fatal("no actual Store Unknown")
			}
			if physical.AttemptID().Validate() != nil || physical.Cause().Details().Primary.Canonical() != identity.Canonical() {
				t.Fatal("original physical cause missing")
			}
			if mode == "after-forward" {
				if result.err != nil || result.value.Validate() != nil {
					t.Fatal("confirmed actual commit did not return success", result.err)
				}
			} else {
				requireCode(t, result.err, f.CommitUnknown)
				var problem *f.Fault
				if !errors.As(result.err, &problem) || problem.CommitState != f.Unknown || problem.CauseID != physical.AttemptID().String() || problem.RetryHint != "lookup" || result.value.Validate() == nil {
					t.Fatal("original Unknown replaced")
				}
			}
			remote.owner.Stop()
			joined, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			err = remote.owner.Drain(joined)
			cancel()
			if err != nil {
				t.Fatal("original call/confirmation did not actually return", err)
			}
			_, err = remote.owner.CreateSecretVariable(ctxFor(t), v.ownerBrowser.actor, m, v.project.ID, in)
			requireCode(t, err, f.ShuttingDown)
			release()
			await(t, proxy.completed)
			fresh := v.login(t, v.ownerBrowser.email).actor
			query := secretLookup(t, v.project.ID, in.Fields().ID, vc.SecretCreateCommand, m)
			looked, err := v.owner.LookupSecretVariableCommand(ctxFor(t), fresh, query)
			want := vc.SecretLookupNotObserved
			if forwarded {
				want = vc.SecretLookupCommitted
			}
			if err != nil || looked.Status() != want {
				t.Fatal("serialized real late outcome", err, looked.Status())
			}
			recovered, err := v.owner.CreateSecretVariable(ctxFor(t), fresh, m, v.project.ID, in)
			if err != nil {
				t.Fatal("explicit original-intent recovery", err)
			}
			if looked.Receipt() != nil {
				sameSecretReceipt(t, *looked.Receipt(), recovered)
			}
			if mode == "after-forward" {
				sameSecretReceipt(t, result.value, recovered)
			}
			after := v.secretCounts(t)
			for n := range counts {
				if after[n] != counts[n]+1 {
					t.Fatal("Unknown recovery duplicated/lost facts", counts, after)
				}
			}
			stable := v.secretSnapshot(t)
			again, err := v.owner.CreateSecretVariable(ctxFor(t), fresh, m, v.project.ID, in)
			if err != nil {
				t.Fatal(err)
			}
			sameSecretReceipt(t, recovered, again)
			if stable != v.secretSnapshot(t) {
				t.Fatal("historical replay changed facts")
			}
			// newCommitProxy's original cleanup closes the exact listener and
			// connections and waits its actual wg before this subtest can pass.
		})
	}
}
