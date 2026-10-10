//go:build integration

package security_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type secretRecoveryPayload struct {
	receipt, credential                       string
	master, revision                          int64
	cipher, dataNonce, wrapped, wrappingNonce []byte
}

func secretRecoveryPayloads(t *testing.T, v *secretVariableStorageFixture) map[string]secretRecoveryPayload {
	t.Helper()
	rows, err := v.store.Query(auditContext(t), `SELECT p.payload_id::text,r.id::text,r.credential_id::text,p.master_version,p.wrap_revision,p.ciphertext,p.data_nonce,p.wrapped_dek,p.wrap_nonce
 FROM agenteam_secret.project_variable_receipts r JOIN agenteam_secret.secret_payloads p ON p.payload_id=r.digest_payload_id
 WHERE r.project_id=$1 AND p.project_id=r.project_id AND p.owner_kind=3 AND p.owner_id=r.id ORDER BY p.payload_id`, v.project.String())
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	result := map[string]secretRecoveryPayload{}
	for rows.Next() {
		var id string
		var record secretRecoveryPayload
		if err = rows.Scan(&id, &record.receipt, &record.credential, &record.master, &record.revision, &record.cipher, &record.dataNonce, &record.wrapped, &record.wrappingNonce); err != nil {
			t.Fatal(err)
		}
		if _, exists := result[id]; exists {
			t.Fatal("duplicate payload identity in maintenance snapshot")
		}
		result[id] = record
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	return result
}

func secretRecoveryAssertRewrapped(t *testing.T, before, after map[string]secretRecoveryPayload) {
	t.Helper()
	if len(before) == 0 || len(before) != len(after) {
		t.Fatal("rotation changed the actual producer payload set")
	}
	for id, old := range before {
		now, exists := after[id]
		if !exists || old.receipt != now.receipt || old.credential != now.credential || old.master != 1 || now.master != 2 || now.revision != old.revision+1 || !bytes.Equal(old.cipher, now.cipher) || !bytes.Equal(old.dataNonce, now.dataNonce) || bytes.Equal(old.wrapped, now.wrapped) || bytes.Equal(old.wrappingNonce, now.wrappingNonce) {
			t.Fatal("target kind3 tuple was not exactly rewrapped")
		}
	}
}

func secretRecoveryAssertSamePayloads(t *testing.T, before, after map[string]secretRecoveryPayload) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatal("recovery changed the payload set")
	}
	for id, old := range before {
		now, exists := after[id]
		if !exists || old.receipt != now.receipt || old.credential != now.credential || old.master != now.master || old.revision != now.revision || !bytes.Equal(old.cipher, now.cipher) || !bytes.Equal(old.dataNonce, now.dataNonce) || !bytes.Equal(old.wrapped, now.wrapped) || !bytes.Equal(old.wrappingNonce, now.wrappingNonce) {
			t.Fatal("recovery repeated or changed an already committed rewrap")
		}
	}
}

func TestSecretVariableStorageSQLMaintenanceUnknown(t *testing.T) {
	for _, mode := range []string{"rotation-refresh", "rotation-no-refresh", "cleanup"} {
		t.Run(mode, func(t *testing.T) {
			v := newSecretVariableStorageFixture(t)
			finishSecretRotation(t, v.secret)
			value := "maintenance-history-canary"
			create := v.prepare(t, v.intent(t, sc.Create, "maintenance-create", 0, &value), sc.ProjectVariableWriteNotObserved(), false)
			created, commit := v.apply(t, create, nil)
			first := requireSecretVariableStored(t, created, commit, sc.ProjectVariableCreated, 1)
			remove := v.prepare(t, v.intent(t, sc.Delete, "maintenance-delete", 2, nil), created, false)
			deleted, commit := v.apply(t, remove, nil)
			last := requireSecretVariableStored(t, deleted, commit, sc.ProjectVariableDeleted, 2)
			baseline := v.counts(t)
			before := secretRecoveryPayloads(t, v)
			if baseline != (secretVariableStorageCounts{0, 2, 2, 2}) || len(before) != 2 || first.ReceiptID == last.ReceiptID {
				t.Fatal("actual create/delete receipt history missing")
			}
			for _, p := range before {
				if p.credential != first.Ref.Details().ID.String() || p.receipt != first.ReceiptID.String() && p.receipt != last.ReceiptID.String() {
					t.Fatal("historical reverse ownership differs from actual producer")
				}
			}
			ctx, cancel := context.WithCancel(auditContext(t))
			defer cancel()
			wire := newCommitProxy(t, net.JoinHostPort("127.0.0.1", v.db.Fixture.Port), true)
			wire.armed.Store(false)
			store := secretRecoveryProxyStore(t, v, wire.listener.Addr())
			keys := masterKeys(t, 1, 1)
			if mode != "cleanup" {
				keys = masterKeys(t, 2, 1, 2)
			}
			binding := newSecretRecoveryBinding(t, store, keys)
			var processedBefore int64
			var actor i.Actor
			var cause sc.LifecycleCause
			var operation f.ID[sc.LifecycleOperation]
			if mode == "cleanup" {
				operation = newID[sc.LifecycleOperation](t)
				cause, _ = sc.NewLifecycleCause(operation, 2, true)
				registration, _ := i.RegisterService(i.ProjectLifecycle)
				actor, _ = registration.Actor(operation.String(), v.scope)
				if _, err := v.store.Exec(ctx, `UPDATE audit_fixture.projects SET state='deleting',stopped=true,operation_id=$2,version=2 WHERE id=$1`, v.project.String(), operation.String()); err != nil {
					t.Fatal(err)
				}
			} else {
				// Complete all reservation work before arming; these candidates are
				// not a committed batch and do not supply the target proof below.
				if _, err := binding.service.PrepareRewrap(ctx); err != nil {
					t.Fatal(err)
				}
				if err := v.store.QueryRow(ctx, `SELECT processed FROM agenteam_secret.secret_rotation_runs WHERE target_version=2`).Scan(&processedBefore); err != nil || processedBefore != 0 {
					t.Fatal("new rotation baseline differs", err)
				}
				secretRecoveryAssertSamePayloads(t, before, secretRecoveryPayloads(t, v))
			}
			type outcome struct {
				completed bool
				report    sc.CleanupReport
				err       error
			}
			var releaseOnce, retireOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(wire.release) }) }
			wire.armed.Store(true)
			flight := startSecretRecoveryFlight(func() outcome {
				if mode == "cleanup" {
					report, err := binding.service.CleanupProject(ctx, actor, cause, v.project, nil)
					return outcome{report: report, err: err}
				}
				done, err := binding.service.MaintenanceStep(ctx)
				return outcome{completed: done, err: err}
			})
			retire := func() {
				retireOnce.Do(func() {
					release()
					cancel()
					wire.Close()
					cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
					defer stop()
					if _, err := flight.wait(cleanup); err != nil {
						t.Error("owned maintenance call not joined", err)
					}
					if err := store.ForceClose(cleanup); err != nil {
						t.Error("maintenance proxy Store not retired", err)
					}
				})
			}
			t.Cleanup(retire)
			secretRecoveryReceive(t, ctx, wire.reached, "target maintenance server COMMIT/idle")
			committedPayloads := secretRecoveryPayloads(t, v)
			if mode == "cleanup" {
				if len(committedPayloads) != 0 || v.counts(t) != (secretVariableStorageCounts{0, 0, 0, baseline.audits}) {
					t.Fatal("cleanup target did not actually delete original kind3 rows")
				}
			} else {
				secretRecoveryAssertRewrapped(t, before, committedPayloads)
				var processed int64
				if err := v.store.QueryRow(ctx, `SELECT processed FROM agenteam_secret.secret_rotation_runs WHERE target_version=2`).Scan(&processed); err != nil || processed != processedBefore+int64(len(before)) || v.counts(t) != baseline {
					t.Fatal("target batch did not commit exact tuple progress", err)
				}
				if mode == "rotation-no-refresh" {
					store.StopAdmission() // Only the subsequent observation/failure journal is unavailable.
				}
			}
			release()
			out := secretRecoveryWait(t, ctx, flight)
			if mode == "rotation-refresh" {
				if out.err != nil || out.completed || binding.service.Status().Remaining != 0 {
					t.Fatal("actual progress refresh did not reconcile the batch", out.err)
				}
			} else {
				var fault *f.Fault
				if !errors.As(out.err, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown || out.completed || out.report != (sc.CleanupReport{}) {
					t.Fatal("unresolved public maintenance result lost Unknown/empty report")
				}
			}
			if mode == "cleanup" {
				wrongOperation := newID[sc.LifecycleOperation](t)
				if _, err := v.store.Exec(ctx, `UPDATE audit_fixture.projects SET operation_id=$2 WHERE id=$1`, v.project.String(), wrongOperation.String()); err != nil {
					t.Fatal(err)
				}
				report, err := binding.service.CleanupProject(ctx, actor, cause, v.project, nil)
				requireCode(t, err, f.InvalidState)
				if report != (sc.CleanupReport{}) {
					t.Fatal("empty database bypassed current operation gate")
				}
				if _, err = v.store.Exec(ctx, `UPDATE audit_fixture.projects SET operation_id=$2,state='active' WHERE id=$1`, v.project.String(), operation.String()); err != nil {
					t.Fatal(err)
				}
				report, err = binding.service.CleanupProject(ctx, actor, cause, v.project, nil)
				requireCode(t, err, f.InvalidState)
				if report != (sc.CleanupReport{}) {
					t.Fatal("empty database bypassed current lifecycle state")
				}
				if _, err = v.store.Exec(ctx, `UPDATE audit_fixture.projects SET state='deleting' WHERE id=$1`, v.project.String()); err != nil {
					t.Fatal(err)
				}
				report, err = binding.service.CleanupProject(ctx, actor, cause, v.project, nil)
				if err != nil || !report.Completed || report.Checkpoint.ProjectID != v.project || report.Checkpoint.OperationID != operation || v.counts(t) != (secretVariableStorageCounts{0, 0, 0, baseline.audits}) {
					t.Fatal("explicit cleanup reconciliation failed", err)
				}
			} else {
				restarted := newSecretRecoveryBinding(t, v.store, masterKeys(t, 2, 1, 2))
				finishSecretRotation(t, restarted.service)
				var processed int64
				if err := v.store.QueryRow(ctx, `SELECT processed FROM agenteam_secret.secret_rotation_runs WHERE target_version=2`).Scan(&processed); err != nil || processed != processedBefore+int64(len(before)) {
					t.Fatal("restarted rotation counted the original batch twice", err)
				}
				secretRecoveryAssertSamePayloads(t, committedPayloads, secretRecoveryPayloads(t, v))
			}
			retire()
		})
	}
}
