//go:build integration

package security_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type secretRecoveryWrite struct {
	commit      f.CommitResult
	observation sc.ProjectVariableWriteObservation
	callbackErr error
}

func secretRecoveryApply(ctx context.Context, b *secretRecoveryBinding, p sc.PreparedProjectVariableWrite, cause f.TransactionCause, locks []f.LockRequest, entered chan<- int, after func(context.Context, f.Tx, sc.ProjectVariableWriteObservation) error) secretRecoveryWrite {
	var result secretRecoveryWrite
	result.commit = b.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if entered != nil {
			pid, err := secretRecoveryBackend(ctx, b.store, tx)
			if err != nil {
				result.callbackErr = err
				return err
			}
			entered <- pid
		}
		if err := b.store.AcquireAll(ctx, tx, locks); err != nil {
			result.callbackErr = err
			return err
		}
		result.observation, result.callbackErr = b.service.ApplyProjectVariableWriteInTx(ctx, tx, p)
		if result.callbackErr == nil && after != nil {
			result.callbackErr = after(ctx, tx, result.observation)
		}
		return result.callbackErr
	})
	return result
}

func secretRecoveryProof(ctx context.Context, store *postgres.Store, tx f.Tx, observation sc.ProjectVariableWriteObservation) error {
	result, err := observation.Result()
	if err != nil {
		return err
	}
	e, err := store.InTx(tx)
	if err != nil {
		return err
	}
	var canonical, receipt, audit int
	err = e.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_secret.secrets s JOIN agenteam_secret.secret_payloads p ON p.payload_id=s.current_payload_id
  WHERE s.id=$1 AND s.project_id=$2 AND s.purpose='project_variable' AND s.version=1 AND p.owner_kind=1 AND p.owner_id=s.id),
 (SELECT count(*) FROM agenteam_secret.project_variable_receipts r JOIN agenteam_secret.secret_payloads p ON p.payload_id=r.digest_payload_id
  WHERE r.id=$3 AND r.project_id=$2 AND r.credential_id=$1 AND r.effect='create' AND p.owner_kind=3 AND p.owner_id=r.id),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$2 AND resource_id=$1 AND producer='secret' AND action='secret.create' AND outcome='success')`, result.Ref.Details().ID.String(), result.ProjectID.String(), result.ReceiptID.String()).Scan(&canonical, &receipt, &audit)
	if err != nil {
		return err
	}
	if canonical != 1 || receipt != 1 || audit != 1 {
		return errors.New("target Apply and native Audit facts were not reached")
	}
	return nil
}

func secretRecoveryReceive[T any](t *testing.T, ctx context.Context, values <-chan T, label string) T {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-ctx.Done():
		t.Fatal(label + " did not arrive")
		var zero T
		return zero
	}
}

func secretRecoveryWait[T any](t *testing.T, ctx context.Context, flight *secretRecoveryFlight[T]) T {
	t.Helper()
	result, err := flight.wait(ctx)
	if err != nil {
		t.Fatal("original goroutine did not return", err)
	}
	return result
}

func secretRecoveryAssertUnknown(t *testing.T, result secretRecoveryWrite, cause f.TransactionCause) {
	t.Helper()
	actual, expected := result.commit.Cause().Details(), cause.Details()
	if result.callbackErr != nil || !result.observation.Observed() || result.commit.State() != f.Unknown || result.commit.AttemptID().Validate() != nil || actual.Kind != expected.Kind || actual.Primary.Canonical() != expected.Primary.Canonical() || len(actual.Related) != len(expected.Related) {
		t.Fatal("original final CommitResult/cause or reached callback evidence differs")
	}
}

func secretRecoveryRead(ctx context.Context, b *secretRecoveryBinding, intent sc.ProjectVariableIntent, plan sc.ProjectVariableWritePlan, p sc.PreparedProjectVariableWrite, locks []f.LockRequest) (sc.ProjectVariableWriteObservation, error, f.CommitResult) {
	var observation sc.ProjectVariableWriteObservation
	var readErr error
	cause, err := f.NewCommandsCause(intent.Fields().Request.Fields().Identity)
	if err != nil {
		return observation, err, f.CommitResult{}
	}
	commit := b.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if readErr = b.store.AcquireAll(ctx, tx, locks); readErr != nil {
			return readErr
		}
		observation, readErr = b.service.LookupProjectVariableWriteInTx(ctx, tx, intent.Fields().Request, plan)
		if readErr != nil {
			return readErr
		}
		matched, err := b.service.MatchProjectVariableIntentInTx(ctx, tx, p)
		if err != nil {
			readErr = err
			return err
		}
		if matched.Observed() != observation.Observed() {
			readErr = errors.New("dedicated Lookup and Match observation differ")
			return readErr
		}
		if matched.Observed() {
			one, _ := observation.Result()
			two, _ := matched.Result()
			if one.ReceiptID != two.ReceiptID || !one.Ref.Equal(two.Ref) || one.Effect != two.Effect || one.Version != two.Version {
				readErr = errors.New("dedicated Lookup and Match result differ")
				return readErr
			}
		}
		return nil
	})
	return observation, readErr, commit
}

func TestSecretVariableStorageSQLCommitUnknown(t *testing.T) {
	for _, mode := range []string{"before", "after", "pending"} {
		t.Run(mode, func(t *testing.T) {
			v := newSecretVariableStorageFixture(t)
			ctx, cancel := context.WithCancel(auditContext(t))
			defer cancel()
			upstream := net.JoinHostPort("127.0.0.1", v.db.Fixture.Port)
			var address net.Addr
			var reached, completed <-chan struct{}
			var arm, release, closeWire func()
			var releaseOnce sync.Once
			if mode == "pending" {
				wire := newOutboundLateCommitProxy(t, upstream)
				address, reached, completed = wire.listener.Addr(), wire.reached, wire.completed
				arm = func() { wire.armed.Store(true) }
				release = func() { releaseOnce.Do(func() { close(wire.release) }) }
				closeWire = func() { closeSecretRecoveryLateProxy(wire) }
			} else {
				wire := newCommitProxy(t, upstream, mode == "after")
				wire.armed.Store(false)
				address, reached = wire.listener.Addr(), wire.reached
				arm = func() { wire.armed.Store(true) }
				release = func() { releaseOnce.Do(func() { close(wire.release) }) }
				closeWire = wire.Close
			}
			store := secretRecoveryProxyStore(t, v, address)
			binding := newSecretRecoveryBinding(t, store, masterKeys(t, 1, 1))
			value := "unknown-original-intent-canary"
			intent := v.intent(t, sc.Create, "unknown-original", 0, &value)
			ref, err := sc.NewCredentialRef(newID[sc.Credential](t), v.scope)
			if err != nil {
				t.Fatal(err)
			}
			plan := binding.plan(t, intent, sc.ProjectVariableWriteBasisFields{Ref: ref, Receipt: sc.ProjectVariableWriteNotObserved()})
			prepared := binding.prepare(t, intent, plan)
			locks, err := prepared.RequiredLocks()
			if err != nil {
				t.Fatal(err)
			}
			cause, err := f.NewCommandsCause(intent.Fields().Request.Fields().Identity)
			if err != nil {
				t.Fatal(err)
			}
			var joins []func(context.Context) error
			var retireOnce sync.Once
			retire := func() {
				retireOnce.Do(func() {
					release()
					cancel()
					closeWire() // Actual original accept/forwarder wg join, also on assertion failure.
					cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
					defer stop()
					for _, join := range joins {
						if err := join(cleanup); err != nil {
							t.Error("owned recovery goroutine not joined", err)
						}
					}
					if err := store.ForceClose(cleanup); err != nil {
						t.Error("owned recovery Store not retired", err)
					}
				})
			}
			t.Cleanup(retire)
			entered, applied := make(chan int, 1), make(chan struct{}, 1)
			arm() // Initialization and both Prepare seals/nonce work have actually finished.
			writer := startSecretRecoveryFlight(func() secretRecoveryWrite {
				return secretRecoveryApply(ctx, binding, prepared, cause, locks, entered, func(ctx context.Context, tx f.Tx, observation sc.ProjectVariableWriteObservation) error {
					if err := secretRecoveryProof(ctx, store, tx, observation); err != nil {
						return err
					}
					applied <- struct{}{}
					return nil
				})
			})
			joins = append(joins, func(ctx context.Context) error { _, err := writer.wait(ctx); return err })
			ownerPID := secretRecoveryReceive(t, ctx, entered, "original backend")
			secretRecoveryReceive(t, ctx, applied, "actual Apply/native Audit proof")
			secretRecoveryReceive(t, ctx, reached, "target COMMIT wire barrier")
			want := secretVariableStorageCounts{}
			if mode == "after" {
				want = secretVariableStorageCounts{1, 1, 2, 1}
			}
			if got := v.counts(t); got != want {
				t.Fatal("barrier intercepted a different transaction", got)
			}
			var original secretRecoveryWrite
			if mode == "pending" {
				original = secretRecoveryWait(t, ctx, writer)
				secretRecoveryAssertUnknown(t, original, cause)
				probeCtx, stopProbe := context.WithCancel(ctx)
				defer stopProbe()
				probeEntered := make(chan int, 1)
				probe := startSecretRecoveryFlight(func() f.CommitResult {
					return v.store.WithinTx(probeCtx, cause, func(ctx context.Context, tx f.Tx) error {
						pid, err := secretRecoveryBackend(ctx, v.store, tx)
						if err != nil {
							return err
						}
						probeEntered <- pid
						return v.store.AcquireAll(ctx, tx, locks)
					})
				})
				joins = append(joins, func(ctx context.Context) error { _, err := probe.wait(ctx); return err })
				waiterPID := secretRecoveryReceive(t, ctx, probeEntered, "original probe backend")
				ownerLock, waitingLock, err := secretRecoveryFirstConflict(locks, locks)
				if err != nil {
					t.Fatal(err)
				}
				if err = secretRecoveryWaitBlocked(ctx, v.store, v.db.Name, ownerPID, waiterPID, ownerLock, waitingLock); err != nil {
					t.Fatal("original pending backend/lock not proven", err)
				}
				stopProbe() // Cancel only after proving the exact live owner's blocking edge.
				if r := secretRecoveryWait(t, ctx, probe); r.State() != f.NotCommitted {
					t.Fatal("cancelled original-lock probe became committed or unknown")
				}
			}
			release()
			if mode == "pending" {
				secretRecoveryReceive(t, ctx, completed, "original server COMMIT/idle")
			} else {
				original = secretRecoveryWait(t, ctx, writer)
				secretRecoveryAssertUnknown(t, original, cause)
			}
			projectAuditJoinWriter(t, v.store, locks)
			observed, err, readCommit := secretRecoveryRead(ctx, binding, intent, plan, prepared, locks)
			if err != nil || readCommit.State() != f.Committed || observed.Observed() != (mode != "before") {
				t.Fatal("dedicated observation after actual writer join differs", err)
			}
			if observed.Observed() {
				before, _ := original.observation.Result()
				after, _ := observed.Result()
				if before.ReceiptID != after.ReceiptID || !before.Ref.Equal(after.Ref) || before.Version != after.Version {
					t.Fatal("committed original result changed")
				}
			}
			if _, err = v.store.Exec(ctx, `UPDATE audit_fixture.sessions SET active=false`); err != nil {
				t.Fatal(err)
			}
			_, denied, deniedCommit := secretRecoveryRead(ctx, binding, intent, plan, prepared, locks)
			requireCode(t, denied, f.SessionRevoked)
			if deniedCommit.State() != f.NotCommitted {
				t.Fatal("current Session denial did not stop original lookup")
			}
			if _, err = v.store.Exec(ctx, `UPDATE audit_fixture.sessions SET active=true`); err != nil {
				t.Fatal(err)
			}
			// Explicit caller recovery after original-owner retirement; D04 has no retry loop.
			recovered := secretRecoveryApply(ctx, binding, prepared, cause, locks, nil, nil)
			if recovered.callbackErr != nil || recovered.commit.State() != f.Committed || !recovered.observation.Observed() {
				t.Fatal("explicit original-command recovery failed", recovered.callbackErr)
			}
			before, _ := original.observation.Result()
			after, _ := recovered.observation.Result()
			if before.ReceiptID != after.ReceiptID || !before.Ref.Equal(after.Ref) || v.counts(t) != (secretVariableStorageCounts{1, 1, 2, 1}) {
				t.Fatal("explicit recovery duplicated or changed original facts")
			}
			retire()
		})
	}
}
