package postgres

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgconn/ctxwatch"
)

const poolCancelTargetKey = "agenteam.postgres.pool-cancellation"
const poolCancelLimit = 100 * time.Millisecond

// A target belongs to one physical, authenticated connection. CustomData is
// written once by the factory, then read only by its checkout owner. Idle
// targets live with that connection, not in the Store's active-owner registry.
// No cancellation credential or driver connection escapes this package.
type poolCancelTarget struct {
	owner *storeState
	pg    *pgconn.PgConn
	data  net.Conn
	refs  int // Store.mu
	work  *poolCancelWork
}

type poolCancelOutcome uint8

const (
	poolCancelAttempted poolCancelOutcome = iota // local attempt; NOT server-stopped evidence
	poolCancelRequestError
	poolCancelBudgetExhausted
)

type poolCancelWork struct {
	target  *poolCancelTarget
	budget  *poolCancelBudget
	once    sync.Once
	done    chan struct{}
	outcome poolCancelOutcome // published with done, also protected by Store.mu
	err     error
}

type poolForcePhase struct {
	total, request *poolCancelBudget
	sealed         bool
	finished       bool
	err            error
}

// The deadlines only move earlier. A single context and timer allow Force to
// shorten an already running dial/read without replacing its context or giving
// each caller a new timeout. Context cancellation is always observed by pgx's
// control-socket watcher; neither this context nor its completion proves that
// a remote backend stopped.
type poolCancelBudget struct {
	context.Context
	mu       sync.Mutex
	deadline time.Time
	cancel   context.CancelCauseFunc
	timer    *time.Timer
}

func newPoolCancelBudget(deadline time.Time) *poolCancelBudget {
	ctx, cancel := context.WithCancelCause(context.Background())
	b := &poolCancelBudget{Context: ctx, deadline: deadline, cancel: cancel}
	b.mu.Lock()
	b.armLocked()
	b.mu.Unlock()
	return b
}
func (b *poolCancelBudget) Deadline() (time.Time, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.deadline, true
}
func (b *poolCancelBudget) Err() error {
	if b.Context.Err() == nil {
		return nil
	}
	if context.Cause(b.Context) == context.DeadlineExceeded {
		return context.DeadlineExceeded
	}
	return context.Canceled
}
func (b *poolCancelBudget) armLocked() {
	if time.Until(b.deadline) <= 0 {
		b.cancel(context.DeadlineExceeded)
		return
	}
	b.timer = time.AfterFunc(time.Until(b.deadline), func() { b.stop(context.DeadlineExceeded) })
}
func (b *poolCancelBudget) shorten(deadline time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !deadline.Before(b.deadline) || b.Context.Err() != nil {
		return
	}
	b.deadline = deadline
	if b.timer != nil {
		b.timer.Stop()
	}
	b.armLocked()
}
func (b *poolCancelBudget) stop(reason error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.timer != nil {
		b.timer.Stop()
	}
	b.cancel(reason)
}
func (b *poolCancelBudget) expired() bool {
	deadline, _ := b.Deadline()
	return b.Err() != nil || !time.Now().Before(deadline)
}

func (s *storeState) signalLocked() {
	close(s.changed)
	s.changed = make(chan struct{})
}
func (s *storeState) poolWatcher(pg *pgconn.PgConn) ctxwatch.Handler {
	t := &poolCancelTarget{owner: s, pg: pg, data: pg.Conn()}
	pg.CustomData()[poolCancelTargetKey] = t
	return &poolCancelWatcher{target: t}
}
func (t *poolCancelTarget) retainLocked() {
	s := t.owner
	if s.targets == nil {
		s.targets = make(map[*poolCancelTarget]struct{})
	}
	s.targets[t] = struct{}{}
	t.refs++
}
func (t *poolCancelTarget) releaseLocked() {
	t.refs--
	if t.refs == 0 {
		delete(t.owner.targets, t)
	}
	t.owner.signalLocked()
}
func (t *poolCancelTarget) workLocked() *poolCancelWork {
	if t.work != nil {
		return t.work
	}
	s := t.owner
	deadline := time.Now().Add(poolCancelLimit)
	if s.force != nil {
		limit, _ := s.force.request.Deadline()
		if limit.Before(deadline) {
			deadline = limit
		}
		if s.force.sealed || s.force.request.expired() {
			deadline = time.Now()
		}
	}
	w := &poolCancelWork{target: t, budget: newPoolCancelBudget(deadline), done: make(chan struct{})}
	t.work = w
	t.retainLocked() // work ownership survives a bounded Force return
	if s.works == nil {
		s.works = make(map[*poolCancelWork]struct{})
	}
	s.works[w] = struct{}{}
	return w
}

func (w *poolCancelWork) start() { w.once.Do(func() { go w.run() }) }
func (w *poolCancelWork) runOrJoin() {
	w.start()
	<-w.done
}
func (w *poolCancelWork) run() {
	var err error
	if !w.budget.expired() {
		err = w.target.pg.CancelRequest(w.budget)
	}
	// Read the outcome before our cleanup cancel. pgx intentionally ignores
	// the ACK read's error, so nil alone is not proof of a timely attempt.
	outcome := poolCancelAttempted
	if w.budget.expired() {
		outcome, err = poolCancelBudgetExhausted, failure(DrainTimeout, w.budget.Err())
	} else if err != nil {
		outcome, err = poolCancelRequestError, failure(ConnectionFailed, err)
	}
	w.budget.stop(context.Canceled)
	// CancelRequest has returned and closed its control socket. Close the
	// stable data socket before publishing local join, even if SQL completed.
	_ = w.target.data.SetDeadline(time.Now())
	_ = w.target.data.Close()
	s := w.target.owner
	s.mu.Lock()
	w.outcome, w.err = outcome, err
	if s.force != nil && s.force.err == nil && err != nil {
		s.force.err = err
	}
	delete(s.works, w)
	w.target.releaseLocked()
	close(w.done)
	s.mu.Unlock()
}

// Only the SQL/checkout owner calls discard. HandleCancel and Force touch the
// concurrency-safe net.Conn, never PgConn's mutable driver state. The exact
// Background branch avoids recursive Unwatch; data is already closed, so the
// driver's Terminate write cannot add another network wait.
func (t *poolCancelTarget) discard() { _ = t.pg.Close(context.Background()) }

type poolCancelWatcher struct{ target *poolCancelTarget }

func (h *poolCancelWatcher) HandleCancel(context.Context) {
	s := h.target.owner
	s.mu.Lock()
	h.target.retainLocked() // includes pre-checkout pool Ping/ValidateConnect
	w := h.target.workLocked()
	s.mu.Unlock()
	w.runOrJoin()
}
func (h *poolCancelWatcher) HandleUnwatchAfterCancel() {
	h.target.discard()
	s := h.target.owner
	s.mu.Lock()
	h.target.releaseLocked()
	s.mu.Unlock()
}

func earlierDeadline(ctx context.Context, limit time.Time) time.Time {
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(limit) {
		limit = deadline
	}
	if now := time.Now(); ctx.Err() != nil && now.Before(limit) {
		limit = now
	}
	return limit
}

// beginForce publishes the phase before any operation is cancelled. Work is
// selected under the same lock as normal release/late Acquire, then run outside
// it. Repeated callers share the original phase and may only shorten it.
func (s *storeState) beginForce(ctx context.Context) (*poolForcePhase, []*poolCancelWork, []context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	now := time.Now()
	if s.force == nil {
		s.force = &poolForcePhase{
			total:   newPoolCancelBudget(earlierDeadline(ctx, now.Add(time.Second))),
			request: newPoolCancelBudget(earlierDeadline(ctx, now.Add(poolCancelLimit))),
		}
	} else if !s.force.finished {
		s.force.total.shorten(earlierDeadline(ctx, now.Add(time.Second)))
		s.force.request.shorten(earlierDeadline(ctx, now.Add(poolCancelLimit)))
	}
	f := s.force
	var work []*poolCancelWork
	var cancels []context.CancelFunc
	if f.finished {
		return f, work, cancels
	}
	deadline, _ := f.request.Deadline()
	for target := range s.targets {
		w := target.workLocked()
		w.budget.shorten(deadline)
		if f.err == nil && w.err != nil {
			f.err = w.err
		}
		work = append(work, w)
	}
	for op := range s.operations {
		cancels = append(cancels, op.cancel)
	}
	return f, work, cancels
}
func (s *storeState) cancelForceParent() {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.force
	if f.finished {
		return
	}
	now := time.Now()
	f.total.shorten(now)
	f.request.shorten(now)
	for work := range s.works {
		work.budget.shorten(now)
	}
	s.signalLocked()
}
func (s *storeState) forceClose(ctx context.Context) error {
	f, work, cancels := s.beginForce(ctx)
	stopParent := context.AfterFunc(ctx, func() { s.cancelForceParent() })
	defer stopParent()
	for _, w := range work {
		w.start()
	}
	for _, cancel := range cancels {
		cancel()
	}
	for {
		s.mu.Lock()
		if f.finished {
			err := f.err
			s.mu.Unlock()
			return err
		}
		if f.sealed || len(s.works) == 0 || f.request.expired() {
			if !f.sealed && f.request.expired() && f.err == nil {
				f.err = failure(DrainTimeout, f.request.Err())
			}
			f.sealed = true
			for w := range s.works {
				w.budget.shorten(time.Now())
			}
			s.mu.Unlock()
			// These effects are performed even with an expired total budget.
			// Dial checks this seal both before and after the actual dial.
			s.sockets.close()
			s.startClose()
			break
		}
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-changed:
		case <-f.request.Done():
		}
	}
	for {
		s.mu.Lock()
		if f.finished {
			err := f.err
			s.mu.Unlock()
			return err
		}
		poolJoined := false
		select {
		case <-s.closed:
			poolJoined = true
		default:
		}
		// A repeated call may observe completed local work without waiting.
		// This grants no new budget and never erases a known cancellation error.
		if poolJoined && len(s.operations) == 0 && len(s.targets) == 0 && len(s.works) == 0 {
			f.finished = true
			err := f.err
			s.mu.Unlock()
			f.request.stop(context.Canceled)
			f.total.stop(context.Canceled)
			return err
		}
		if f.total.expired() {
			s.mu.Unlock()
			return failure(DrainTimeout, f.total.Err())
		}
		changed := s.changed
		s.mu.Unlock()
		closed := s.closed
		if poolJoined {
			closed = nil
		}
		select {
		case <-changed:
		case <-closed:
		case <-f.total.Done():
			// Another Force caller may have completed and stopped the shared
			// context for cleanup. Recheck finished before classifying timeout.
		}
	}
}
