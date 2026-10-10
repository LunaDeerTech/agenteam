//go:build integration

package security_test

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestSecretVariableStorageSQLNonceUnknown(t *testing.T) {
	v := newSecretVariableStorageFixture(t)
	ctx, cancel := context.WithCancel(auditContext(t))
	defer cancel()
	wire := newCommitProxy(t, net.JoinHostPort("127.0.0.1", v.db.Fixture.Port), true)
	wire.armed.Store(false)
	store := secretRecoveryProxyStore(t, v, wire.listener.Addr())
	binding := newSecretRecoveryBinding(t, store, masterKeys(t, 1, 1))
	value := "nonce-unknown-caller-material"
	intent := v.intent(t, sc.Create, "nonce-unknown-original", 0, &value)
	ref, err := sc.NewCredentialRef(newID[sc.Credential](t), v.scope)
	if err != nil {
		t.Fatal(err)
	}
	plan := binding.plan(t, intent, sc.ProjectVariableWriteBasisFields{Ref: ref, Receipt: sc.ProjectVariableWriteNotObserved()})
	baseline := v.counts(t)
	var originalHigh int64
	if err = v.store.QueryRow(ctx, `SELECT nonce_high_water FROM agenteam_secret.secret_master_registry WHERE version=1`).Scan(&originalHigh); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		prepared sc.PreparedProjectVariableWrite
		err      error
	}
	var releaseOnce, retireOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(wire.release) }) }
	wire.armed.Store(true) // The new binding has finished Initialize, but has never prepared a value.
	flight := startSecretRecoveryFlight(func() outcome {
		p, err := binding.service.PrepareProjectVariableWrite(ctx, intent, plan)
		return outcome{p, err}
	})
	retire := func() {
		retireOnce.Do(func() {
			release()
			cancel()
			wire.Close()
			cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			out, err := flight.wait(cleanup)
			if err != nil {
				t.Error("nonce Prepare goroutine not joined", err)
			} else if out.prepared != nil {
				out.prepared.Destroy()
			}
			if err := store.ForceClose(cleanup); err != nil {
				t.Error("nonce recovery Store not retired", err)
			}
		})
	}
	t.Cleanup(retire)
	secretRecoveryReceive(t, ctx, wire.reached, "actual committed nonce reservation")
	var unknownHigh int64
	if err = v.store.QueryRow(ctx, `SELECT nonce_high_water FROM agenteam_secret.secret_master_registry WHERE version=1`).Scan(&unknownHigh); err != nil || unknownHigh != originalHigh+1024 || v.counts(t) != baseline {
		t.Fatal("nonce barrier did not bind the actual reservation", err)
	}
	release()
	out := secretRecoveryWait(t, ctx, flight)
	var domain *secret.Error
	var fault *f.Fault
	if out.prepared != nil || !errors.As(out.err, &domain) || domain.Code() != secret.NonceReservationUnknown || !errors.As(out.err, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown || v.counts(t) != baseline {
		t.Fatal("unknown nonce reservation published a prepared candidate or business facts")
	}
	// Reuse the actual original caller intent and same Service, without any
	// synthetic attempt identity: Prepare's public API exposes only the fault.
	prepared := binding.prepare(t, intent, plan)
	var confirmedHigh int64
	if err = v.store.QueryRow(ctx, `SELECT nonce_high_water FROM agenteam_secret.secret_master_registry WHERE version=1`).Scan(&confirmedHigh); err != nil || confirmedHigh != unknownHigh+1024 || v.counts(t) != baseline {
		t.Fatal("successor reused or did not confirm a fresh nonce range", err)
	}
	locks, err := prepared.RequiredLocks()
	if err != nil {
		t.Fatal(err)
	}
	cause, err := f.NewCommandsCause(intent.Fields().Request.Fields().Identity)
	if err != nil {
		t.Fatal(err)
	}
	written := secretRecoveryApply(ctx, binding, prepared, cause, locks, nil, nil)
	if written.callbackErr != nil || written.commit.State() != f.Committed {
		t.Fatal("confirmed successor failed", written.callbackErr)
	}
	result, err := written.observation.Result()
	if err != nil {
		t.Fatal(err)
	}
	counters := map[uint64]bool{}
	for _, target := range []struct {
		kind  int
		owner string
	}{{1, result.Ref.Details().ID.String()}, {3, result.ReceiptID.String()}} {
		var nonce []byte
		if err = v.store.QueryRow(ctx, `SELECT wrap_nonce FROM agenteam_secret.secret_payloads WHERE project_id=$1 AND owner_kind=$2 AND owner_id=$3`, v.project.String(), target.kind, target.owner).Scan(&nonce); err != nil || len(nonce) != 12 {
			t.Fatal("successor target payload not found", err)
		}
		counter := binary.BigEndian.Uint64(nonce[4:])
		if counter <= uint64(unknownHigh) || counter > uint64(confirmedHigh) || counters[counter] {
			t.Fatal("successor payload consumed the unknown range or reused a counter")
		}
		counters[counter] = true
	}
	want := baseline
	want.values++
	want.receipts++
	want.payloads += 2
	want.audits++
	if v.counts(t) != want {
		t.Fatal("successor published duplicate or incomplete D04 facts")
	}
	retire()
}
