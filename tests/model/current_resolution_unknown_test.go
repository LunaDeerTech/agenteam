//go:build integration

package model_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func (v *currentResolutionFixture) arm(t *testing.T, target int, rollback, unknown bool, after func()) {
	t.Helper()
	v.observed.mu.Lock()
	defer v.observed.mu.Unlock()
	v.observed.seen = 0
	v.observed.target = target
	v.observed.rollback = rollback
	v.observed.after = after
	v.observed.marker = f.ID[f.TransactionAttempt]{}
	if unknown {
		v.observed.marker = newID[f.TransactionAttempt](t)
	}
}
func TestModelCurrentResolutionUnknown(t *testing.T) {
	v := newCurrentResolutionFixture(t)
	_, m, _ := v.config(t, false, true)
	for _, stage := range []int{2, 3} {
		for _, rollback := range []bool{false, true} {
			t.Run(fmt.Sprintf("result-decoration-stage-%d-rollback-%v", stage, rollback), func(t *testing.T) {
				r := v.request(t, m, false)
				v.arm(t, stage, rollback, true, nil)
				var e error
				if stage == 2 {
					p, err := v.resolving.DiscoverResolve(testContext(t), r)
					e = err
					resolutionZeroPlan(t, p, e)
				} else {
					out, err := v.resolving.ResolveModel(testContext(t), r)
					e = err
					resolutionNoResult(t, out, e)
				}
				var unknown *model.UnknownCommandError
				if !errors.As(e, &unknown) {
					t.Fatal("original Unknown projection lost", e)
				}
				v.observed.mu.Lock()
				actual, returned := v.observed.actual, v.observed.returned
				v.observed.target = 0
				v.observed.mu.Unlock()
				if unknown.AttemptID() != returned.AttemptID() || unknown.Cause().Details().Primary.Canonical() != returned.Cause().Details().Primary.Canonical() || returned.State() != f.Unknown {
					t.Fatal("returned attempt/cause replaced")
				}
				expected := f.Committed
				if rollback {
					expected = f.NotCommitted
				}
				if actual.State() != expected {
					t.Fatal("decoration actual Store mismatch", actual.State())
				}
				var snapshots int
				if e = v.raw.QueryRow(testContext(t), `SELECT count(*) FROM agenteam_model.snapshot_bindings WHERE owner_id=$1`, r.LeaseOwner.Details().ID).Scan(&snapshots); e != nil {
					t.Fatal(e)
				}
				want := 0
				if stage == 3 && !rollback {
					want = 1
				}
				if snapshots != want {
					t.Fatal("actual PG outcome differs", snapshots, want)
				}
				out := v.resolve(t, r)
				if out.Validate() != nil {
					t.Fatal("correct reentry failed")
				}
				t.Logf("formal result decoration only: stage=%d actual=%s returned=%s; canonical bindings before retry=%d", stage, actual.State(), returned.State(), snapshots)
			})
		}
	}
	t.Run("committed-then-caller-cancelled-zero-publication", func(t *testing.T) {
		r := v.request(t, m, false)
		ctx, cancel := context.WithCancel(testContext(t))
		defer cancel()
		v.arm(t, 3, false, false, cancel)
		out, e := v.resolving.ResolveModel(ctx, r)
		resolutionNoResult(t, out, e)
		if !errors.Is(e, context.Canceled) {
			t.Fatal("caller cancellation lost", e)
		}
		v.observed.mu.Lock()
		actual := v.observed.actual
		v.observed.target = 0
		v.observed.after = nil
		v.observed.mu.Unlock()
		var count int
		if e = v.raw.QueryRow(testContext(t), `SELECT count(*) FROM agenteam_model.snapshot_bindings WHERE owner_id=$1`, r.LeaseOwner.Details().ID).Scan(&count); e != nil || count != 1 || actual.State() != f.Committed {
			t.Fatal("committed facts rewritten on delivery cancellation", count, actual.State(), e)
		}
	})
	t.Run("ordinary-original-writer-mutex-before-reentry", func(t *testing.T) {
		r := v.request(t, m, false)
		p := v.discover(t, r)
		var original f.LockRequest
		for _, l := range p.RequiredLocks() {
			if l.Key.Canonical()[:8] == "command:" {
				original = l
				break
			}
		}
		if original.Key.Validate() != nil {
			t.Fatal("no original command mutex")
		}
		held := make(chan struct{})
		release := make(chan struct{})
		done := make(chan f.CommitResult, 1)
		go func() {
			done <- v.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
				if e := v.raw.AcquireAll(ctx, tx, []f.LockRequest{original}); e != nil {
					return e
				}
				close(held)
				<-release
				return nil
			})
		}()
		waitSignal(t, held)
		ctx, cancel := context.WithTimeout(testContext(t), 150*time.Millisecond)
		out, e := v.resolving.ResolveModel(ctx, r)
		cancel()
		resolutionNoResult(t, out, e)
		close(release)
		if result := <-done; result.State() != f.Committed {
			t.Fatal("ordinary holder did not end", result.Fault())
		}
		v.resolve(t, r)
		t.Log("normal second Tx held exact original Command EX; no missing-row confirmation until released; no network fault")
	})
	t.Run("old-writer-completed-before-preflight-replans", func(t *testing.T) {
		provider, modelView, _ := v.config(t, false, false)
		r := v.request(t, modelView, false)
		p := v.discover(t, r)
		read := make(chan struct{})
		resume := make(chan struct{})
		v.observed.mu.Lock()
		v.observed.afterPreparationRead = func() { close(read); <-resume }
		v.observed.mu.Unlock()
		type result struct {
			plan mc.ResolutionPlan
			err  error
		}
		done := make(chan result, 1)
		go func() { plan, e := v.resolving.DiscoverResolve(testContext(t), r); done <- result{plan, e} }()
		waitSignal(t, read)
		_, commit := v.final(t, r, p, false, false)
		if commit.State() != f.Committed {
			close(resume)
			t.Fatal("original completion", commit.Fault())
		}
		_, e := v.service.DeleteModel(testContext(t), mc.DeleteModelRequest{CommandMeta: v.meta(t, "barrier-delete-model"), ID: modelView.ID, ExpectedVersion: modelView.Version})
		if e != nil {
			close(resume)
			t.Fatal(e)
		}
		_, e = v.service.DeleteProvider(testContext(t), mc.DeleteProviderRequest{CommandMeta: v.meta(t, "barrier-delete-provider"), ID: provider.ID, ExpectedVersion: provider.Version})
		if e != nil {
			close(resume)
			t.Fatal(e)
		}
		close(resume)
		got := <-done
		resolutionZeroPlan(t, got.plan, got.err)
		requireCode(t, got.err, f.ResourceBusy)
		out := v.resolve(t, r)
		if out.Snapshot.ID != p.Details().SnapshotID {
			t.Fatal("replan lost original committed snapshot")
		}
		t.Log("candidate read prepared; original completion and formal live delete precede preflight; Busy then original committed snapshot")
	})
}
