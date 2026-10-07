//go:build integration

package model_test

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"strings"
	"testing"
	"time"
)

// An ordinary single-lock holder isolates the new selector lock from refs.
// The release function always observes the actual transaction completion.
func (v *meetingResolutionFixture) hold(t *testing.T, lock f.LockRequest) func() {
	t.Helper()
	held := make(chan struct{})
	release := make(chan struct{})
	done := make(chan f.CommitResult, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	go func() {
		done <- v.raw.WithinTx(ctx, recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
			if e := v.raw.AcquireAll(ctx, tx, []f.LockRequest{lock}); e != nil {
				return e
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
	released := false
	finish := func() {
		if released {
			return
		}
		released = true
		close(release)
		result := <-done
		cancel()
		if result.State() != f.Committed {
			t.Error("ordinary holder failed actual completion", result.Fault())
		}
	}
	t.Cleanup(finish)
	waitSignal(t, held)
	return finish
}
func TestModelMeetingSummaryResolutionAtomicity(t *testing.T) {
	v := newMeetingResolutionFixture(t, true)
	_, m, _ := v.config(t, false, true)
	v.choose(t, m.ID)
	t.Run("simulated-input-snapshot-binding-lease-commit-or-rollback", func(t *testing.T) {
		r := v.request(t, mc.MeetingSummaryInitial)
		p := v.discover(t, r)
		_, result := v.final(t, r, p, true, true)
		if result.State() != f.NotCommitted {
			t.Fatal("outer rollback escaped")
		}
		v.atomicFacts(t, r, p, 0)
		out, result := v.final(t, r, p, true, false)
		if result.State() != f.Committed || out.Validate() != nil {
			t.Fatal("atomic commit", result.Fault())
		}
		v.atomicFacts(t, r, p, 1)
		if v.strict.validated.Load() < 4 {
			t.Fatal("consumer omitted a current transaction stage")
		}
	})
	t.Run("secret-failure-and-missing-summary-sh-poison", func(t *testing.T) {
		r := v.request(t, mc.MeetingSummaryUpdate)
		p := v.discover(t, r)
		v.tap.mu.Lock()
		v.tap.fail = true
		v.tap.mu.Unlock()
		_, result := v.final(t, r, p, true, false)
		v.tap.mu.Lock()
		v.tap.fail = false
		v.tap.mu.Unlock()
		if result.State() != f.NotCommitted {
			t.Fatal("Secret failure committed")
		}
		v.atomicFacts(t, r, p, 0)
		var locks []f.LockRequest
		removed := false
		for _, l := range p.RequiredLocks() {
			if strings.Contains(l.Key.Canonical(), "model-meeting-summary-selection") {
				removed = true
				continue
			}
			locks = append(locks, l)
		}
		if !removed {
			t.Fatal("new Summary lock absent from plan")
		}
		var inner error
		result = v.observed.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
			if e := v.observed.AcquireAll(ctx, tx, locks); e != nil {
				return e
			}
			_, inner = v.resolving.ResolveModelInTx(ctx, tx, r, p)
			return nil
		})
		if inner == nil || result.State() != f.NotCommitted {
			t.Fatal("missing Summary SH did not poison swallowed failure", inner, result.State())
		}
		v.atomicFacts(t, r, p, 0)
	})
	t.Run("only-new-selector-ex-blocks-select-and-resolve", func(t *testing.T) {
		key, e := f.SystemConfigLock("model-meeting-summary-selection")
		if e != nil {
			t.Fatal(e)
		}
		release := v.hold(t, f.LockRequest{Key: key, Mode: f.Exclusive})
		r := v.request(t, mc.MeetingSummaryInitial)
		ctx, cancel := context.WithTimeout(testContext(t), 150*time.Millisecond)
		out, e := v.resolving.SelectModel(ctx, v.owner, mc.SelectionRequest{Consumer: r.Consumer, Selection: *r.Selection})
		cancel()
		if e == nil || out.Selected != nil {
			t.Fatal("Select omitted new SH")
		}
		ctx, cancel = context.WithTimeout(testContext(t), 150*time.Millisecond)
		p, e := v.resolving.DiscoverResolve(ctx, r)
		cancel()
		resolutionZeroPlan(t, p, e)
		release()
		v.resolve(t, r)
	})
	t.Run("real-switch-and-cross-provider-delete-invalidate-prepared", func(t *testing.T) {
		_, next, _ := v.config(t, false, true)
		r := v.request(t, mc.MeetingSummaryUpdate)
		p := v.discover(t, r)
		v.choose(t, next.ID)
		_, result := v.final(t, r, p, true, false)
		if result.State() != f.NotCommitted {
			t.Fatal("old selection plan accepted")
		}
		v.atomicFacts(t, r, p, 0)
		v.choose(t, m.ID)
		v.selectMemory(t, m)
		r = v.request(t, mc.MeetingSummaryInitial)
		p = v.discover(t, r)
		key, e := f.SystemConfigLock("model-meeting-summary-selection")
		if e != nil {
			t.Fatal(e)
		}
		release := v.hold(t, f.LockRequest{Key: key, Mode: f.Shared})
		request := mc.DeleteModelRequest{CommandMeta: v.meta(t, "both-owner-delete"), ID: m.ID, ExpectedVersion: m.Version, Replacement: &next.ID}
		ctx, cancel := context.WithTimeout(testContext(t), 150*time.Millisecond)
		_, e = v.service.DeleteModel(ctx, request)
		cancel()
		if e == nil {
			t.Fatal("delete passed held Summary SH")
		}
		release()
		receipt, e := v.service.DeleteModel(testContext(t), request)
		if e != nil || receipt.AffectedReferences != 2 {
			t.Fatal("cross-provider double owner replacement", e)
		}
		_, result = v.final(t, r, p, true, false)
		if result.State() != f.NotCommitted {
			t.Fatal("deleted prepared target accepted")
		}
		v.atomicFacts(t, r, p, 0)
		fresh := v.discover(t, r)
		out, result := v.final(t, r, fresh, true, false)
		if result.State() != f.Committed || out.Snapshot.Identity.ModelID != next.ID {
			t.Fatal("replacement replan", result.Fault())
		}
		v.atomicFacts(t, r, fresh, 1)
	})
	t.Run("same-call-canonical-different-call-independent-no-ref-zero-secret", func(t *testing.T) {
		var leases []sc.LeaseID
		for range 2 {
			r := v.request(t, mc.MeetingSummaryInitial)
			a := v.resolve(t, r)
			b := v.resolve(t, r)
			if a.Snapshot.ID != b.Snapshot.ID || a.CredentialLease.LeaseID != b.CredentialLease.LeaseID {
				t.Fatal("same Call lease changed")
			}
			leases = append(leases, a.CredentialLease.LeaseID)
		}
		if leases[0] == leases[1] {
			t.Fatal("different Call shared lease")
		}
		_, plain, _ := v.config(t, false, false)
		v.choose(t, plain.ID)
		before := v.tap.calls.Load()
		out := v.resolve(t, v.request(t, mc.MeetingSummaryUpdate))
		if out.CredentialLease != nil || before != v.tap.calls.Load() {
			t.Fatal("no ref called Secret")
		}
	})
}
