package postgres

import (
	"context"
	"errors"
	"net"
	"reflect"
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
	owner    *storeState
	pg       *pgconn.PgConn
	data     net.Conn
	refs     int // Store.mu
	work     *poolCancelWork
	sql      *poolSQLContext    // current SQL owner; Store.mu, cleared before owner release
	closedBy *poolSQLCloseProof // immutable local terminal fact; Store.mu
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
		t.closedBy = nil
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

// Each pre-COMMIT SQL call carries its own private identity to the driver's
// watcher. A later deadline, a different query, or a closed physical connection
// alone cannot turn a hard driver error into cancellation. In particular,
// internal Rows cleanup and Force are not caller cancellation provenance.
type poolSQLContext struct {
	context.Context
	operation *operation
	target    *poolCancelTarget
	caller    context.Context
	business  bool  // transaction business SQL; never initialization or COMMIT
	reason    error // Store.mu; captured by the actual watcher, not on error return
	cause     error
	discarded bool // the SQL owner's matching Unwatch changed driver state to closed
	internal  bool // an internal Rows/transaction abort preceded error normalization
}

func (o *operation) sqlContext(ctx, caller context.Context) *poolSQLContext {
	return o.newSQLContext(ctx, caller, false)
}

func (o *operation) businessSQLContext(ctx, caller context.Context) *poolSQLContext {
	return o.newSQLContext(ctx, caller, true)
}

func (o *operation) newSQLContext(ctx, caller context.Context, business bool) *poolSQLContext {
	call := &poolSQLContext{Context: ctx, operation: o, target: o.target, caller: caller, business: business}
	o.owner.mu.Lock()
	o.target.sql = call
	o.owner.mu.Unlock()
	return call
}

// A completed SQL can return nil after its Unwatch discarded the connection.
// Retain that terminal local fact independently of the completed call's scope.
// It grants no transaction access and is never server-stopped evidence.
type poolSQLCloseProof struct {
	target    *poolCancelTarget
	operation *operation
	caller    context.Context
	reason    error
	cause     error
}

// Contexts and explicit causes can contain slices, maps or interfaces holding
// them. Unknown identities are not transferable; never compare them blindly.
func poolSameIdentity(left, right any) bool {
	return left != nil && right != nil &&
		reflect.ValueOf(left).Comparable() && reflect.ValueOf(right).Comparable() && left == right
}

func (p *poolSQLCloseProof) matchesLocked(c *poolSQLContext) bool {
	s := c.operation.owner
	_, owned := s.operations[c.operation]
	return p != nil && c.business && !c.internal && owned && s.force == nil &&
		p.target == c.target && p.operation == c.operation && c.operation.target == c.target && c.target.sql == c &&
		poolSameIdentity(p.caller, c.caller) && poolSameIdentity(p.reason, c.caller.Err()) &&
		poolSameIdentity(p.reason, c.Err()) && poolSameIdentity(p.cause, context.Cause(c.caller)) &&
		poolSameIdentity(p.cause, context.Cause(c)) &&
		(c.operation.ctx.Err() == nil || c.operation.caller.Err() != nil)
}

func (c *poolSQLContext) finish() {
	c.operation.owner.mu.Lock()
	if c.target.sql == c {
		c.target.sql = nil
	}
	c.operation.owner.mu.Unlock()
}

func (c *poolSQLContext) cancelInternal(cancel context.CancelFunc) {
	c.operation.owner.mu.Lock()
	c.internal = true
	c.operation.owner.mu.Unlock()
	cancel()
}

// Rows misuse and an escaped transaction callback cancel the operation for
// local cleanup. Record that before cancelling, so a later caller deadline
// cannot retroactively turn this abort into caller-origin cancellation.
func (o *operation) cancelSQL() {
	o.owner.mu.Lock()
	if o.target != nil && o.target.sql != nil && o.target.sql.operation == o {
		o.target.sql.internal = true
	}
	o.owner.mu.Unlock()
	o.cancel()
}

func (c *poolSQLContext) sqlError(err error) error {
	if c == nil || !errors.Is(err, pgconn.ErrConnClosed) {
		return err
	}
	s := c.operation.owner
	s.mu.Lock()
	reason, cause, discarded, internal := c.reason, c.cause, c.discarded, c.internal
	if !internal && (!discarded || reason == nil) && c.target.closedBy.matchesLocked(c) {
		reason, cause, discarded = c.target.closedBy.reason, c.target.closedBy.cause, true
	}
	s.mu.Unlock()
	if !discarded || reason == nil || internal {
		return err
	}
	// Keep the original driver identity and any explicit cancellation cause.
	// The existing safe Error wrapper owns their diagnostic projection.
	return errors.Join(err, reason, cause)
}

type poolCancelWatcher struct {
	target *poolCancelTarget
	call   *poolSQLContext // Store.mu; only the actual Watch argument can supply it
}

func (h *poolCancelWatcher) HandleCancel(ctx context.Context) {
	s := h.target.owner
	s.mu.Lock()
	h.call = nil
	if call, ok := ctx.(*poolSQLContext); ok && call.target == h.target && call.operation.owner == s && call.operation.target == h.target {
		_, owned := s.operations[call.operation]
		// An already selected work belongs to Force/release or another watcher.
		// op.ctx may also be cancelled internally to close escaped/concurrent
		// Rows. Its original caller must have cancelled before we credit that.
		if owned && !call.internal && s.force == nil && h.target.work == nil && ctx.Err() != nil && call.caller.Err() != nil &&
			(call.operation.ctx.Err() == nil || call.operation.caller.Err() != nil) {
			call.reason, call.cause = ctx.Err(), context.Cause(ctx)
			h.call = call
		}
	}
	h.target.retainLocked() // includes pre-checkout pool Ping/ValidateConnect
	w := h.target.workLocked()
	s.mu.Unlock()
	w.runOrJoin()
}
func (h *poolCancelWatcher) HandleUnwatchAfterCancel() {
	// Only this SQL-owner callback may inspect driver state. If pgx already
	// closed it (for example after an independent read failure), our discard is
	// not evidence that cancellation caused ErrConnClosed.
	wasOpen := !h.target.pg.IsClosed()
	h.target.discard()
	closed := h.target.pg.IsClosed()
	s := h.target.owner
	s.mu.Lock()
	if h.call != nil && wasOpen && closed && s.force == nil {
		h.call.discarded = true
		call := h.call
		_, owned := s.operations[call.operation]
		if h.target.closedBy == nil && call.business && !call.internal && owned && h.target.sql == call &&
			poolSameIdentity(call.caller, call.caller) && poolSameIdentity(call.reason, call.caller.Err()) &&
			poolSameIdentity(call.cause, context.Cause(call.caller)) {
			h.target.closedBy = &poolSQLCloseProof{target: h.target, operation: call.operation,
				caller: call.caller, reason: call.reason, cause: call.cause}
		}
	}
	h.call = nil
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
