//go:build integration

package outbox_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

func ownedRuntime(t *testing.T, svc *outbox.Service, defs []oc.HandlerDefinition) *outbox.Runtime {
	t.Helper()
	r, err := outbox.NewRuntime(svc, defs, outbox.Options{PollInterval: 3 * time.Millisecond, RetryBase: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Initialize(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := r.Force(ctx); err != nil {
			t.Error(err)
		}
	})
	return r
}
func TestOutboxRuntimeClaimUnknownNeverEntersCallbackBeforeTerminal(t *testing.T) {
	for _, commit := range []bool{true, false} {
		name := "commit"
		if !commit {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			svc, store, proxy := proxyService(t, f, commit)
			h := f.handler("unknown.claim")
			h.store = store
			r := ownedRuntime(t, svc, []oc.HandlerDefinition{h.definition(1)})
			e := f.event(t, false, 1, "claim fact")
			_, result := f.append(t, svc, store, f.actor, e)
			state(t, result, foundation.Committed)
			proxy.armed.Store(true)
			if err := r.Start(ctxFor(t)); err != nil {
				t.Fatal(err)
			}
			reached(t, proxy.reached)
			waitDB(t, f.db.Connect(t), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a WHERE datname=current_database() AND cardinality(pg_blocking_pids(a.pid))>0 AND a.query LIKE '%pg_advisory_xact_lock%')`)
			observer := f.db.Connect(t)
			var firstWaiter int32
			if err := observer.QueryRow(ctxFor(t), `SELECT pid FROM pg_stat_activity a WHERE datname=current_database() AND cardinality(pg_blocking_pids(a.pid))>0 AND a.query LIKE '%pg_advisory_xact_lock%' ORDER BY query_start LIMIT 1`).Scan(&firstWaiter); err != nil {
				t.Fatal(err)
			}
			// The claim's bounded confirmation must return before the joined
			// task's retirement pass starts a different owned DB operation.
			waitDB(t, observer, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a WHERE datname=current_database() AND a.pid<>$1 AND cardinality(pg_blocking_pids(a.pid))>0 AND a.query LIKE '%pg_advisory_xact_lock%')`, firstWaiter)
			if h.calls.Load() != 0 || f.count(t, `SELECT count(*) FROM agenteam_outbox.attempts`) != 0 {
				t.Fatal("unconfirmed claim invoked callback or exposed uncommitted attempt")
			}
			close(proxy.release)
			reached(t, proxy.completed)
			waitOutbox(t, func() bool {
				return f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries WHERE phase='succeeded'`) == 1
			})
			r.StopClaims()
			if err := r.Drain(ctxFor(t)); err != nil {
				t.Fatal(err)
			}
			if h.calls.Load() != 1 || f.count(t, `SELECT count(*) FROM agenteam_outbox.processed`) != 1 || f.count(t, `SELECT count(*) FROM outbox_fixture.effects`) != 1 {
				t.Fatal("claim unknown replayed/dropped business effect")
			}
			want := int64(1)
			if commit {
				want = 2
			}
			if f.count(t, `SELECT count(*) FROM agenteam_outbox.attempts`) != want {
				t.Fatal("claim history did not match real commit/rollback")
			}
			if commit && f.count(t, `SELECT count(*) FROM agenteam_outbox.attempts WHERE handler_returned_at IS NULL AND checkpoint='not_committed'`) != 1 {
				t.Fatal("uninvoked callback acquired fictitious latency")
			}
		})
	}
}

type armCommitHandler struct {
	base  *handler
	proxy *commitProxy
	once  atomic.Bool
}

func (h *armCommitHandler) Prepare(ctx context.Context, e event.Event) (oc.HandlerPlan, error) {
	return h.base.Prepare(ctx, e)
}
func (h *armCommitHandler) ValidateInTx(ctx context.Context, tx foundation.Tx, e event.Event, p oc.HandlerPlan) error {
	return h.base.ValidateInTx(ctx, tx, e, p)
}
func (h *armCommitHandler) HandleInTx(ctx context.Context, tx foundation.Tx, e event.Event, p oc.HandlerPlan) oc.Result {
	result := h.base.HandleInTx(ctx, tx, e, p)
	if h.once.CompareAndSwap(false, true) {
		h.proxy.armed.Store(true)
	}
	return result
}
func TestOutboxRuntimeApplyUnknownWaitsOriginalWriterAndRecognizesMarker(t *testing.T) {
	for _, commit := range []bool{true, false} {
		name := "commit"
		if !commit {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			svc, store, proxy := proxyService(t, f, commit)
			h := f.handler("unknown.apply")
			h.store = store
			wrapped := &armCommitHandler{base: h, proxy: proxy}
			def := h.definition(1)
			def.Handler = wrapped
			r := ownedRuntime(t, svc, []oc.HandlerDefinition{def})
			e := f.event(t, false, 1, "late apply fact")
			_, result := f.append(t, svc, store, f.actor, e)
			state(t, result, foundation.Committed)
			if err := r.Start(ctxFor(t)); err != nil {
				t.Fatal(err)
			}
			reached(t, proxy.reached)
			waitDB(t, f.db.Connect(t), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a WHERE datname=current_database() AND cardinality(pg_blocking_pids(a.pid))>0 AND a.query LIKE '%pg_advisory_xact_lock%')`)
			if h.calls.Load() != 1 || f.count(t, `SELECT count(*) FROM agenteam_outbox.processed`) != 0 || f.count(t, `SELECT count(*) FROM outbox_fixture.effects`) != 0 {
				t.Fatal("held original apply repeated or leaked commit")
			}
			close(proxy.release)
			reached(t, proxy.completed)
			waitOutbox(t, func() bool {
				return f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries WHERE phase='succeeded'`) == 1
			})
			r.StopClaims()
			if err := r.Drain(ctxFor(t)); err != nil {
				t.Fatal(err)
			}
			calls := int64(2)
			if commit {
				calls = 1
			}
			if h.calls.Load() != calls || f.count(t, `SELECT count(*) FROM agenteam_outbox.processed`) != 1 || f.count(t, `SELECT count(*) FROM outbox_fixture.effects`) != 1 {
				t.Fatal("unknown terminal/marker did not fence callback")
			}
			if f.count(t, `SELECT count(*) FROM agenteam_outbox.attempts WHERE handler_returned_at IS NULL`) != 0 {
				t.Fatal("actual returned callback latency was lost")
			}
		})
	}
}
