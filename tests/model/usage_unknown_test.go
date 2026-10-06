//go:build integration

package model_test

import (
	"context"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

func TestUsageInvocationUnknownConfirmation(t *testing.T) {
	v := newUsageFixture(t, false)
	for _, rollback := range []bool{false, true} {
		name := "committed"
		if rollback {
			name = "rolled-back"
		}
		t.Run(name, func(t *testing.T) {
			c := v.newCase(t, nil, nil)
			r := v.stage(t, c.event)
			plan, e := v.ledger.DiscoverInvocation(testContext(t), r)
			if e != nil {
				t.Fatal(e)
			}
			marker := newID[f.TransactionAttempt](t)
			v.storeView.mu.Lock()
			v.storeView.armed = true
			v.storeView.rollback = rollback
			v.storeView.marker = marker
			v.storeView.mu.Unlock()
			_, result := v.applyPlan(t, r, plan, false, false)
			v.storeView.mu.Lock()
			actual := v.storeView.actual
			v.storeView.armed = false
			v.storeView.mu.Unlock()
			want := f.Committed
			if rollback {
				want = f.NotCommitted
			}
			if actual.State() != want || result.State() != f.Unknown || result.AttemptID() != marker || result.Cause().Details().Primary.Canonical() != plan.Cause().Details().Primary.Canonical() {
				t.Fatal("decorated actual outcome/cause lost")
			}
			confirm := r
			confirm.Access = uc.ConfirmInvocation
			confirm.Actor = v.technical(t, r.Identity)
			lookup, e := v.ledger.LookupInvocation(testContext(t), confirm)
			if e != nil || lookup.Observed == rollback {
				t.Fatal("exact receipt confirmation", e)
			}
			if lookup.Observed && lookup.Receipt.Value.Dispatch != uc.Reserved {
				t.Fatal("DB Unknown became dispatch unknown")
			}
			if rollback {
				v.apply(t, r)
			}
			t.Run("original-writer-lock-is-required", func(t *testing.T) {
				held, release, done := make(chan struct{}), make(chan struct{}), make(chan f.CommitResult, 1)
				go func() {
					done <- v.raw.WithinTx(testContext(t), plan.Cause(), func(ctx context.Context, tx f.Tx) error {
						if e := v.raw.AcquireAll(ctx, tx, []f.LockRequest{usageWriterKey(r.Identity)}); e != nil {
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
				waitSignal(t, held)
				ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
				lookup, e := v.ledger.LookupInvocation(ctx, confirm)
				cancel()
				if e == nil || lookup.Observed || lookup.Receipt != nil {
					t.Fatal("blocked writer reported absence")
				}
				close(release)
				if (<-done).State() != f.Committed {
					t.Fatal("writer lock owner failed")
				}
				lookup, e = v.ledger.LookupInvocation(testContext(t), confirm)
				if e != nil || !lookup.Observed {
					t.Fatal("receipt not observable after writer joined", e)
				}
			})
			v.storeView.mu.Lock()
			v.storeView.armed = true
			v.storeView.rollback = false
			v.storeView.mu.Unlock()
			lookup, e = v.ledger.LookupInvocation(testContext(t), confirm)
			v.storeView.mu.Lock()
			v.storeView.armed = false
			v.storeView.mu.Unlock()
			if usageErrorCode(e) != f.CommitUnknown || lookup.Observed || lookup.Receipt != nil {
				t.Fatal("unknown read Tx published receipt", e)
			}
		})
	}
}
