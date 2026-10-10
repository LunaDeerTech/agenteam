//go:build integration

package security_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestSecretVariableStorageSQLConcurrency(t *testing.T) {
	for _, mode := range []string{"same-intent", "changed-value", "stale-credential-version"} {
		t.Run(mode, func(t *testing.T) {
			v := newSecretVariableStorageFixture(t)
			ctx, cancel := context.WithCancel(auditContext(t))
			defer cancel()
			left := newSecretRecoveryBinding(t, openAuditStore(t, v.db.Config(t, nil)), masterKeys(t, 1, 1))
			right := newSecretRecoveryBinding(t, openAuditStore(t, v.db.Config(t, nil)), masterKeys(t, 1, 1))
			value, other := "concurrent-original-canary", "concurrent-other-canary"
			kind, version := sc.Create, f.Version(0)
			firstKey, secondKey := "concurrent-command", "concurrent-command"
			ref, err := sc.NewCredentialRef(newID[sc.Credential](t), v.scope)
			if err != nil {
				t.Fatal(err)
			}
			basis := sc.ProjectVariableWriteBasisFields{Ref: ref, Receipt: sc.ProjectVariableWriteNotObserved()}
			want := secretVariableStorageCounts{1, 1, 2, 1}
			if mode == "stale-credential-version" {
				seed := v.prepare(t, v.intent(t, sc.Create, "concurrent-seed", 0, &value), sc.ProjectVariableWriteNotObserved(), false)
				observed, commit := v.apply(t, seed, nil)
				created := requireSecretVariableStored(t, observed, commit, sc.ProjectVariableCreated, 1)
				kind, version = sc.Update, 7 // External Variable version is deliberately distinct.
				firstKey, secondKey = "concurrent-update-a", "concurrent-update-b"
				basis.Ref, basis.CredentialVersion, basis.VariableVersion = created.Ref, 1, version
				want = secretVariableStorageCounts{1, 2, 3, 2}
			}
			first := v.intent(t, kind, firstKey, version, &value)
			secondValue := &value
			if mode != "same-intent" {
				secondValue = &other
			}
			second := v.intent(t, kind, secondKey, version, secondValue)
			leftPrepared := left.prepare(t, first, left.plan(t, first, basis))
			rightPrepared := right.prepare(t, second, right.plan(t, second, basis))
			leftLocks, err := leftPrepared.RequiredLocks()
			if err != nil {
				t.Fatal(err)
			}
			rightLocks, err := rightPrepared.RequiredLocks()
			if err != nil {
				t.Fatal(err)
			}
			leftCause, err := f.NewCommandsCause(first.Fields().Request.Fields().Identity)
			if err != nil {
				t.Fatal(err)
			}
			rightCause, err := f.NewCommandsCause(second.Fields().Request.Fields().Identity)
			if err != nil {
				t.Fatal(err)
			}
			release, held := make(chan struct{}), make(chan string, 1)
			var releaseOnce, retireOnce sync.Once
			unlock := func() { releaseOnce.Do(func() { close(release) }) }
			var joins []func(context.Context) error
			retire := func() {
				retireOnce.Do(func() {
					unlock()
					cancel()
					cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
					defer stop()
					for _, join := range joins {
						if err := join(cleanup); err != nil {
							t.Error("owned competing writer not joined", err)
						}
					}
					if err := left.store.ForceClose(cleanup); err != nil {
						t.Error("left Store did not retire", err)
					}
					if err := right.store.ForceClose(cleanup); err != nil {
						t.Error("right Store did not retire", err)
					}
				})
			}
			t.Cleanup(retire)
			leftEntered, rightEntered := make(chan int, 1), make(chan int, 1)
			writer := startSecretRecoveryFlight(func() secretRecoveryWrite {
				return secretRecoveryApply(ctx, left, leftPrepared, leftCause, leftLocks, leftEntered, func(ctx context.Context, tx f.Tx, observation sc.ProjectVariableWriteObservation) error {
					result, err := observation.Result()
					if err != nil {
						return err
					}
					e, err := left.store.InTx(tx)
					if err != nil {
						return err
					}
					var payload string
					var actualVersion int64
					var receiptCount, auditCount int
					action := "secret.create"
					if kind == sc.Update {
						action = "secret.update"
					}
					if err = e.QueryRow(ctx, `SELECT s.current_payload_id::text,s.version,
 (SELECT count(*) FROM agenteam_secret.project_variable_receipts r JOIN agenteam_secret.secret_payloads p ON p.payload_id=r.digest_payload_id WHERE r.id=$2 AND r.credential_id=s.id AND p.owner_kind=3 AND p.owner_id=r.id),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=s.project_id AND resource_id=s.id AND action=$3 AND producer='secret' AND outcome='success')
 FROM agenteam_secret.secrets s WHERE s.id=$1 AND s.purpose='project_variable'`, result.Ref.Details().ID.String(), result.ReceiptID.String(), action).Scan(&payload, &actualVersion, &receiptCount, &auditCount); err != nil {
						return err
					}
					if payload == "" || actualVersion != int64(result.Version) || receiptCount != 1 || auditCount != 1 {
						return errors.New("original writer did not reach native facts and Audit")
					}
					held <- payload
					select {
					case <-release:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				})
			})
			joins = append(joins, func(ctx context.Context) error { _, err := writer.wait(ctx); return err })
			ownerPID := secretRecoveryReceive(t, ctx, leftEntered, "original A backend")
			originalPayload := secretRecoveryReceive(t, ctx, held, "A native facts before holding original Tx")
			waiter := startSecretRecoveryFlight(func() secretRecoveryWrite {
				return secretRecoveryApply(ctx, right, rightPrepared, rightCause, rightLocks, rightEntered, nil)
			})
			joins = append(joins, func(ctx context.Context) error { _, err := waiter.wait(ctx); return err })
			waiterPID := secretRecoveryReceive(t, ctx, rightEntered, "original B backend")
			ownerLock, waitingLock, err := secretRecoveryFirstConflict(leftLocks, rightLocks)
			if err != nil {
				t.Fatal(err)
			}
			if err = secretRecoveryWaitBlocked(ctx, v.store, v.db.Name, ownerPID, waiterPID, ownerLock, waitingLock); err != nil {
				t.Fatal("exact original A-to-B lock wait not proven", err)
			}
			unlock()
			one := secretRecoveryWait(t, ctx, writer)
			two := secretRecoveryWait(t, ctx, waiter)
			if one.callbackErr != nil || one.commit.State() != f.Committed {
				t.Fatal("original writer did not commit", one.callbackErr)
			}
			original, err := one.observation.Result()
			if err != nil {
				t.Fatal(err)
			}
			if mode == "same-intent" {
				replayed, err := two.observation.Result()
				if err != nil || two.callbackErr != nil || two.commit.State() != f.Committed || replayed.ReceiptID != original.ReceiptID || !replayed.Ref.Equal(original.Ref) || replayed.Version != original.Version {
					t.Fatal("serialized same-intent writer did not replay original receipt")
				}
			} else {
				code, domainCode := f.IdempotencyKeyReused, secret.KeyReused
				if mode == "stale-credential-version" {
					code, domainCode = f.VersionConflict, secret.Conflict
				}
				requireCode(t, two.callbackErr, code)
				var domain *secret.Error
				if !errors.As(two.callbackErr, &domain) || domain.Code() != domainCode || two.commit.State() != f.NotCommitted || two.observation.Observed() {
					t.Fatal("competing writer did not fail at the expected native target")
				}
			}
			var actualPayload string
			if err = v.store.QueryRow(ctx, `SELECT current_payload_id::text FROM agenteam_secret.secrets WHERE id=$1`, original.Ref.Details().ID.String()).Scan(&actualPayload); err != nil || actualPayload != originalPayload || v.counts(t) != want {
				t.Fatal("competing writer changed winner payload or facts", err)
			}
			if left.ports.checks != 1 || right.ports.checks != 0 {
				t.Fatal("native Audit did not have exactly the original append")
			}
			retire()
		})
	}
}
