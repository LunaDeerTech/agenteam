package outbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

// Options only tightens the fixed production bounds. Zero selects the bound;
// there is no environment setting or option that increases it.
type Options struct {
	Concurrent, PerHandler, Batch                                      int
	PollInterval, AttemptTimeout, QueryTimeout, ItemTimeout, RetryBase time.Duration
}

func (o Options) normalized() (Options, error) {
	ints := []struct {
		value *int
		max   int
	}{{&o.Concurrent, 4}, {&o.PerHandler, 2}, {&o.Batch, 64}}
	for _, v := range ints {
		if *v.value == 0 {
			*v.value = v.max
		}
		if *v.value < 1 || *v.value > v.max {
			return o, invalid()
		}
	}
	durations := []struct {
		value *time.Duration
		max   time.Duration
	}{
		{&o.PollInterval, time.Second}, {&o.AttemptTimeout, 30 * time.Second},
		{&o.QueryTimeout, 2 * time.Second}, {&o.ItemTimeout, 250 * time.Millisecond}, {&o.RetryBase, time.Second},
	}
	for _, v := range durations {
		if *v.value == 0 {
			*v.value = v.max
		}
		if *v.value < time.Millisecond || *v.value > v.max {
			return o, invalid()
		}
	}
	return o, nil
}

// Runtime is the single scheduling and join owner for one Service. Outbox does
// not own its ProcessAuthority; the composition root retains that authority
// until Joined reports a local fact, or the OS terminates the process.
type Runtime struct{ data func() *runtimeState }

func (r Runtime) Format(w fmt.State, _ rune)            { _, _ = io.WriteString(w, "outbox_runtime") }
func (r Runtime) MarshalJSON() ([]byte, error)          { return []byte(`"outbox_runtime"`), nil }
func (*Runtime) UnmarshalJSON([]byte) error             { return invalid() }
func (r Runtime) LogValue() slog.Value                  { return slog.StringValue("outbox_runtime") }
func (r *Runtime) Initialize(ctx context.Context) error { return r.data().Initialize(ctx) }
func (r *Runtime) Start(ctx context.Context) error      { return r.data().Start(ctx) }
func (r *Runtime) Check(ctx context.Context) error      { return r.data().Check(ctx) }
func (r *Runtime) Recover(ctx context.Context) error    { return r.data().Recover(ctx) }
func (r *Runtime) StopClaims()                          { r.data().StopClaims() }
func (r *Runtime) Drain(ctx context.Context) error      { return r.data().Drain(ctx) }
func (r *Runtime) Force(ctx context.Context) error      { return r.data().Force(ctx) }
func (r *Runtime) Joined() bool                         { return r.data().Joined() }

type runtimeState struct {
	svc                                                 *Service
	options                                             Options
	definitions                                         []oc.HandlerDefinition
	mu                                                  sync.Mutex
	changed                                             chan struct{}
	wake                                                chan struct{}
	initialized, initializing, started, stopped, forced bool
	controls                                            int
	controlCancels                                      map[uint64]context.CancelFunc
	nextControl                                         uint64
	workerCancel                                        context.CancelFunc
	workerDone                                          chan struct{}
	forceContext                                        context.Context
	active                                              map[oc.AttemptID]*execution
	err                                                 error
	dueAfter                                            duePosition
	recoveryAfter                                       recoveryPosition
	lifecycleAfter                                      string
	recoverSlot                                         chan struct{}
}

func NewRuntime(s *Service, definitions []oc.HandlerDefinition, options Options) (*Runtime, error) {
	if s == nil || s.data == nil || nilPort(s.state().auth.Processes) || s.state().auth.Processes.CurrentProcess().Validate() != nil || len(definitions) > 128 {
		return nil, failure(foundation.DependencyUnbound, nil)
	}
	o, err := options.normalized()
	if err != nil {
		return nil, err
	}
	defs := make([]oc.HandlerDefinition, 0, len(definitions))
	seen := map[event.StableName]bool{}
	for _, d := range definitions {
		n, _, err := normalizeDefinition(d)
		if err != nil {
			return nil, err
		}
		if seen[n.ID] {
			return nil, invalid()
		}
		seen[n.ID] = true
		defs = append(defs, n)
	}
	r := &runtimeState{svc: s, options: o, definitions: defs, changed: make(chan struct{}), wake: make(chan struct{}, 1), active: make(map[oc.AttemptID]*execution), controlCancels: make(map[uint64]context.CancelFunc), recoverSlot: make(chan struct{}, 1)}
	s.state().mu.Lock()
	defer s.state().mu.Unlock()
	if s.state().runtime != nil || s.state().claimsStopped {
		return nil, failure(foundation.InvalidState, nil)
	}
	runtime := &Runtime{data: func() *runtimeState { return r }}
	s.state().runtime = runtime
	return runtime, nil
}

func (r *runtimeState) signalLocked() {
	close(r.changed)
	r.changed = make(chan struct{})
	select {
	case r.wake <- struct{}{}:
	default:
	}
}
func (r *runtimeState) beginControl(ctx context.Context) (context.Context, func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return nil, nil, failure(foundation.ShuttingDown, nil)
	}
	ctx, cancel := context.WithCancel(ctx)
	r.nextControl++
	id := r.nextControl
	r.controls++
	r.controlCancels[id] = cancel
	var once sync.Once
	return ctx, func() {
		once.Do(func() {
			cancel()
			r.mu.Lock()
			delete(r.controlCancels, id)
			r.controls--
			r.signalLocked()
			r.mu.Unlock()
		})
	}, nil
}

func (r *runtimeState) Initialize(ctx context.Context) error {
	ctx, done, err := r.beginControl(ctx)
	if err != nil {
		return err
	}
	defer done()
	r.mu.Lock()
	already := r.initialized || r.initializing || r.started
	if !already {
		r.initializing = true
	}
	r.mu.Unlock()
	if already {
		return failure(foundation.InvalidState, nil)
	}
	defer func() { r.mu.Lock(); r.initializing = false; r.signalLocked(); r.mu.Unlock() }()
	if err = r.svc.CheckStorage(ctx); err != nil {
		return err
	}
	for _, d := range r.definitions {
		if _, err = r.svc.RegisterHandler(ctx, d); err != nil {
			return err
		}
	}
	if err = r.checkBindings(ctx); err != nil {
		return err
	}
	if err = r.checkStructure(ctx); err != nil {
		return err
	}
	if err = r.recoverAll(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped || ctx.Err() != nil {
		return failure(foundation.ShuttingDown, ctx.Err())
	}
	r.initialized = true
	r.err = nil
	r.signalLocked()
	return nil
}

func (r *runtimeState) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ctx.Err() != nil {
		return portError(ctx.Err())
	}
	if !r.initialized || r.started || r.stopped {
		return failure(foundation.InvalidState, nil)
	}
	serving, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r.started = true
	r.workerCancel = cancel
	r.workerDone = make(chan struct{})
	go r.run(serving)
	return nil
}

func (r *runtimeState) StopClaims() {
	// This is also the publication barrier for a late RegisterHandler. It does
	// not close Append admission for an already admitted HTTP transaction.
	r.svc.state().mu.Lock()
	r.svc.state().claimsStopped = true
	r.svc.state().mu.Unlock()
	r.mu.Lock()
	r.stopped = true
	r.signalLocked()
	r.mu.Unlock()
}

func (r *runtimeState) Joined() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.joinedLocked()
}
func (r *runtimeState) joinedLocked() bool {
	if r.controls != 0 {
		return false
	}
	if r.workerDone != nil {
		select {
		case <-r.workerDone:
		default:
			return false
		}
	}
	for _, a := range r.active {
		select {
		case <-a.done:
		default:
			return false
		}
	}
	return true
}
func (r *runtimeState) Drain(ctx context.Context) error {
	r.StopClaims()
	for {
		r.mu.Lock()
		if r.joinedLocked() {
			r.mu.Unlock()
			return nil
		}
		changed := r.changed
		r.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return failure(foundation.ResourceBusy, ctx.Err())
		}
	}
}
func (r *runtimeState) Force(ctx context.Context) error {
	r.StopClaims()
	r.mu.Lock()
	r.forced = true
	if r.forceContext == nil {
		r.forceContext = ctx
	} else if old, ok := r.forceContext.Deadline(); ok {
		if next, has := ctx.Deadline(); has && next.Before(old) {
			r.forceContext = ctx
		}
	} else if _, has := ctx.Deadline(); has {
		r.forceContext = ctx
	}
	cancels := make([]context.CancelFunc, 0, len(r.controlCancels)+len(r.active)+1)
	for _, c := range r.controlCancels {
		cancels = append(cancels, c)
	}
	for _, a := range r.active {
		cancels = append(cancels, a.cancel)
	}
	if r.workerCancel != nil {
		cancels = append(cancels, r.workerCancel)
	}
	r.signalLocked()
	r.mu.Unlock()
	for _, c := range cancels {
		c()
	}
	return r.Drain(ctx)
}

func (r *runtimeState) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, r.options.QueryTimeout)
	defer cancel()
	r.mu.Lock()
	ok := r.initialized && !r.stopped
	last := r.err
	done := r.workerDone
	r.mu.Unlock()
	if !ok {
		return failure(foundation.DependencyUnavailable, nil)
	}
	if done != nil {
		select {
		case <-done:
			return failure(foundation.DependencyUnavailable, nil)
		default:
		}
	}
	if err := r.svc.CheckStorage(ctx); err != nil {
		return err
	}
	if err := r.checkBindings(ctx); err != nil {
		return err
	}
	if err := r.checkStructure(ctx); err != nil {
		return err
	}
	if err := r.checkLifecycleResolver(ctx); err != nil {
		return err
	}
	if last != nil {
		return last
	}
	return nil
}

func (r *runtimeState) checkpointContext(parent context.Context) (context.Context, context.CancelFunc) {
	r.mu.Lock()
	if r.forced {
		parent = r.forceContext
	}
	r.mu.Unlock()
	return context.WithTimeout(parent, r.options.QueryTimeout)
}
func (r *runtimeState) recordError(err error) { r.mu.Lock(); r.err = err; r.mu.Unlock() }
func safeFaultCode(err error) foundation.Code {
	var f *foundation.Fault
	if errors.As(err, &f) && f != nil {
		return f.Code
	}
	return foundation.DependencyUnavailable
}

func (r *runtimeState) checkBindings(ctx context.Context) error {
	rows, err := r.svc.state().store.Query(ctx, `SELECT h.id,h.effect,h.ordering_policy,h.declaration_digest,CASE WHEN EXISTS(SELECT 1 FROM agenteam_outbox.deliveries d WHERE d.handler_id=h.id AND d.scope='project' AND d.phase IN ('pending','processing','retry_wait')) THEN 'project' ELSE 'system' END FROM agenteam_outbox.handlers h`)
	if err != nil {
		return unavailable(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, effect, ordering, digest, scope string
		if err = rows.Scan(&name, &effect, &ordering, &digest, &scope); err != nil {
			return unavailable(err)
		}
		r.svc.state().mu.RLock()
		d, ok := r.svc.state().handlers[event.StableName(name)]
		r.svc.state().mu.RUnlock()
		if !ok || scope == "project" && nilPort(r.svc.state().auth.Projects) {
			return failure(foundation.DependencyUnbound, nil)
		}
		_, sum, e := normalizeDefinition(d)
		if e != nil || string(d.Effect) != effect || string(d.Ordering) != ordering || sum.String() != digest {
			return unavailable(e)
		}
	}
	return portResult(rows.Err())
}
func portResult(err error) error {
	if err != nil {
		return unavailable(err)
	}
	return nil
}
func (r *runtimeState) checkStructure(ctx context.Context) error {
	var bad bool
	err := r.svc.state().store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_outbox.deliveries d JOIN agenteam_outbox.events e ON e.id=d.event_id LEFT JOIN agenteam_outbox.attempts a ON a.id=d.current_attempt_id WHERE d.scope<>e.scope OR d.project_id IS DISTINCT FROM e.project_id OR (d.phase='processing' AND (a.id IS NULL OR a.delivery_id<>d.id OR a.fence<>d.fence)) OR (d.phase='succeeded' AND NOT EXISTS(SELECT 1 FROM agenteam_outbox.processed p WHERE p.delivery_id=d.id AND p.attempt_id=d.current_attempt_id AND p.fence=d.fence)))`).Scan(&bad)
	if err != nil || bad {
		return unavailable(err)
	}
	return nil
}
