//go:build integration

package outbox_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type stopRun struct {
	ctx   context.Context
	event event.EventID
}
type stopCaptureHandler struct {
	*handler
	runs    chan stopRun
	release [2]chan struct{}
	once    [2]sync.Once
	count   atomic.Int32
}

func captureHandler(f *fixture) *stopCaptureHandler {
	return &stopCaptureHandler{handler: f.handler("stop.capture"), runs: make(chan stopRun, 2), release: [2]chan struct{}{make(chan struct{}), make(chan struct{})}}
}
func (h *stopCaptureHandler) unblock(i int) { h.once[i].Do(func() { close(h.release[i]) }) }
func (h *stopCaptureHandler) HandleInTx(ctx context.Context, tx foundation.Tx, e event.Event, p oc.HandlerPlan) oc.Result {
	i := int(h.count.Add(1)) - 1
	if i >= len(h.release) {
		return oc.Reject(oc.InvalidEvent)
	}
	h.runs <- stopRun{ctx, e.Header().EventID}
	<-h.release[i]
	return h.handler.HandleInTx(ctx, tx, e, p)
}
func (h *stopCaptureHandler) definition() oc.HandlerDefinition {
	d := h.handler.definition(1)
	d.Handler = h
	d.Effect = oc.DomainIngress
	return d
}
func waitStopRun(t *testing.T, h *stopCaptureHandler) stopRun {
	t.Helper()
	select {
	case r := <-h.runs:
		return r
	case <-ctxFor(t).Done():
		t.Fatal("callback did not enter")
		return stopRun{}
	}
}

type postAuthorizationStore struct {
	*postgres.Store
	committed, release chan struct{}
	once               atomic.Bool
}

func (s *postAuthorizationStore) WithinTx(ctx context.Context, c foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	r := s.Store.WithinTx(ctx, c, fn)
	if c.Details().Owner == "outbox.stop-authorize" && r.State() == foundation.Committed && s.once.CompareAndSwap(false, true) {
		close(s.committed)
		select {
		case <-s.release:
		case <-ctx.Done():
		}
	}
	return r
}
func TestOutboxStopCapturedRunDoesNotCancelRestoredAdmission(t *testing.T) {
	f := newFixture(t)
	store := &postAuthorizationStore{Store: f.store, committed: make(chan struct{}), release: make(chan struct{})}
	s, _ := lifecycleService(t, f, f.auth, store)
	h := captureHandler(f)
	defer h.unblock(0)
	defer h.unblock(1)
	r := ownedRuntime(t, s, []oc.HandlerDefinition{h.definition()})
	if err := r.Start(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	_, result := f.append(t, s, f.store, f.actor, f.event(t, true, 1, "old admission"))
	state(t, result, foundation.Committed)
	old := waitStopRun(t, h)
	actor, cause := lifecycleCause(t, f, oc.ArchiveProject, 2)
	done := make(chan error, 1)
	go func() { _, e := s.RequestStop(ctxFor(t), actor, cause); done <- e }()
	reached(t, store.committed)
	// The old authorization is now committed, but the service has not yet
	// received that return. Its exact captured run completes and Restore admits
	// another real handler under the same Project before cancellation executes.
	h.unblock(0)
	waitOutbox(t, func() bool {
		return f.count(t, `SELECT count(*) FROM agenteam_outbox.processed WHERE event_id=$1`, old.event.String()) == 1
	})
	f.sql(t, `UPDATE outbox_fixture.authority SET active=true WHERE id=$1`, f.project.String())
	f.sql(t, `UPDATE outbox_fixture.lifecycle SET version=3 WHERE project_id=$1`, f.project.String())
	_, result = f.append(t, s, f.store, f.actor, f.event(t, true, 1, "restored admission"))
	state(t, result, foundation.Committed)
	current := waitStopRun(t, h)
	close(store.release)
	// The later EX transition now really waits behind the new SH callback.
	// Releasing that callback, not an unauthorized broadcast cancel, permits
	// the original stop to revalidate and reject its now-stale cause.
	waitDB(t, f.db.Connect(t), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND cardinality(pg_blocking_pids(pid))>0)`)
	if current.ctx.Err() != nil || current.event == old.event {
		t.Fatal("old authorization cancelled a new admission")
	}
	h.unblock(1)
	select {
	case err := <-done:
		code(t, err, foundation.Forbidden)
	case <-ctxFor(t).Done():
		t.Fatal("stale stop did not finish")
	}
	waitOutbox(t, func() bool { return f.count(t, `SELECT count(*) FROM agenteam_outbox.processed`) == 2 })
	r.StopClaims()
	if err := r.Drain(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
}
func TestOutboxStopUnknownAuthorizationDoesNotCancel(t *testing.T) {
	f := newFixture(t)
	copy, store, proxy := administrationProxy(t, f, true, "outbox.stop-authorize")
	s, _ := lifecycleService(t, copy, copy.auth, store)
	h := captureHandler(copy)
	defer h.unblock(0)
	defer h.unblock(1)
	r := ownedRuntime(t, s, []oc.HandlerDefinition{h.definition()})
	if err := r.Start(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	_, result := f.append(t, s, copy.store, f.actor, f.event(t, true, 1, "unknown stop authorization"))
	state(t, result, foundation.Committed)
	run := waitStopRun(t, h)
	actor, cause := lifecycleCause(t, f, oc.ArchiveProject, 2)
	bad := cause.Details()
	bad.OperationID = id[oc.LifecycleOperation](t)
	wrong, _ := oc.NewLifecycleCause(bad)
	if _, err := s.RequestStop(ctxFor(t), actor, wrong); err == nil {
		t.Fatal("wrong cause accepted")
	}
	if run.ctx.Err() != nil {
		t.Fatal("refused authorization cancelled callback")
	}
	done := make(chan error, 1)
	go func() { _, e := s.RequestStop(ctxFor(t), actor, cause); done <- e }()
	reached(t, proxy.reached)
	select {
	case err := <-done:
		code(t, err, foundation.CommitUnknown)
	case <-ctxFor(t).Done():
		t.Fatal("unknown authorization did not return")
	}
	if run.ctx.Err() != nil {
		t.Fatal("unconfirmed read authorization cancelled callback")
	}
	close(proxy.release)
	reached(t, proxy.completed)
	h.unblock(0)
	r.StopClaims()
	if err := r.Drain(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
}

type exclusiveStopAuthority struct {
	*lifecycleAuthority
	key foundation.LockKey
}

func (a exclusiveStopAuthority) Discover(ctx context.Context, r oc.ProjectRequest) (oc.Dependencies, error) {
	d, err := a.lifecycleAuthority.Discover(ctx, r)
	if err != nil || r.Details().Kind != oc.LifecycleProject {
		return d, err
	}
	return oc.NewDependencies(a.issuer, d.Binding(), append(d.Locks(), foundation.LockRequest{Key: a.key, Mode: foundation.Exclusive}), d.Opaque())
}
func (a exclusiveStopAuthority) ValidateInTx(ctx context.Context, tx foundation.Tx, r oc.ProjectRequest, d oc.Dependencies) error {
	if r.Details().Kind == oc.LifecycleProject {
		if err := a.base.store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{{Key: a.key, Mode: foundation.Exclusive}}); err != nil {
			return err
		}
	}
	return a.lifecycleAuthority.ValidateInTx(ctx, tx, r, d)
}
func TestOutboxStopPreservesProviderExclusiveDependency(t *testing.T) {
	f := newFixture(t)
	_, base := lifecycleService(t, f, f.auth)
	key, _ := foundation.AggregateLock(foundation.ExecutionAggregate, f.aggregate.String())
	provider := exclusiveStopAuthority{base, key}
	s, err := outbox.New(f.store, f.cat, outbox.Authorizations{Projects: provider, Processes: f.auth, Producers: map[event.StableName]oc.ProducerAuthority{"fixture": lifecycleProducer{base}}})
	if err != nil {
		t.Fatal(err)
	}
	h := captureHandler(f)
	defer h.unblock(0)
	defer h.unblock(1)
	r := ownedRuntime(t, s, []oc.HandlerDefinition{h.definition()})
	if err = r.Start(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	_, result := f.append(t, s, f.store, f.actor, f.event(t, true, 1, "EX dependency"))
	state(t, result, foundation.Committed)
	run := waitStopRun(t, h)
	actor, cause := lifecycleCause(t, f, oc.ArchiveProject, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, e := s.RequestStop(ctx, actor, cause); done <- e }()
	waitDB(t, f.db.Connect(t), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND cardinality(pg_blocking_pids(pid))>0)`)
	if err = <-done; err == nil {
		t.Fatal("provider EX dependency was weakened")
	}
	if run.ctx.Err() != nil {
		t.Fatal("blocked authorization cancelled unproved target")
	}
	h.unblock(0)
	r.StopClaims()
	if err = r.Drain(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
}
