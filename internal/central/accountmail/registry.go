// Package accountmail drives durable account delivery through current account,
// Secret and outbound ports. It exposes no HTTP route or protocol bypass.
package accountmail

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"sync"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func fail(code foundation.Code, cause error) error {
	f := foundation.NewFault(code, foundation.NotStarted)
	if cause != nil {
		return f.WithCause(cause)
	}
	return f
}
func invalid() error { return fail(foundation.InvalidArgument, nil) }
func nilPort(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Func, reflect.Map, reflect.Slice, reflect.Interface, reflect.Chan:
		return r.IsNil()
	}
	return false
}

type registryState struct {
	mu      sync.Mutex
	process c.ProcessAuthority
	issuer  c.DeliveryIssuer
	stopped bool
	force   context.Context
	works   map[*deliveryWork]bool
	active  map[string]*deliveryWork
	changed chan struct{}
}
type WorkRegistry struct{ data func() *registryState }
type deliveryWork struct {
	registry         *registryState
	ctx              context.Context
	cancel           context.CancelFunc
	attempt          c.DeliveryAttempt
	cleanupCancel    context.CancelFunc
	joined, finished bool
	completion       c.DeliveryCompletion
}

func NewWorkRegistry(p c.ProcessAuthority) (*WorkRegistry, error) {
	if nilPort(p) || p.CurrentProcess().Validate() != nil {
		return nil, invalid()
	}
	s := &registryState{process: p, issuer: c.NewDeliveryIssuer(), works: map[*deliveryWork]bool{}, active: map[string]*deliveryWork{}, changed: make(chan struct{})}
	return &WorkRegistry{func() *registryState { return s }}, nil
}
func (r *WorkRegistry) CurrentProcess() c.ProcessID {
	if r == nil || r.data == nil {
		return c.ProcessID{}
	}
	return r.data().process.CurrentProcess()
}
func (r *WorkRegistry) begin(ctx context.Context) (*deliveryWork, error) {
	s := r.data()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil, fail(foundation.ShuttingDown, nil)
	}
	if len(s.works) >= 2 {
		return nil, fail(foundation.ResourceBusy, nil)
	}
	ctx, cancel := context.WithCancel(ctx)
	w := &deliveryWork{registry: s, ctx: ctx, cancel: cancel}
	s.works[w] = true
	return w, nil
}
func (w *deliveryWork) accept(a c.DeliveryAttempt) error {
	s := w.registry
	s.mu.Lock()
	defer s.mu.Unlock()
	if w.finished || w.attempt.Validate() == nil || a.Validate() != nil || a.Details().ProcessID != s.process.CurrentProcess() {
		return invalid()
	}
	if s.active[a.Details().AttemptID.String()] != nil {
		return fail(foundation.ResourceBusy, nil)
	}
	// Already handed off: register even after Stop. The worker's finally now
	// owns cleanup, but cancelled work cannot regain sending admission.
	w.attempt = a
	s.active[a.Details().AttemptID.String()] = w
	return nil
}
func (r *WorkRegistry) RequireActive(a c.DeliveryAttempt) error {
	if a.Validate() != nil {
		return invalid()
	}
	s := r.data()
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.active[a.Details().AttemptID.String()]
	if w == nil || !w.attempt.Same(a) || w.joined || w.finished || w.ctx.Err() != nil {
		return fail(foundation.InvalidState, nil)
	}
	return nil
}
func (r *WorkRegistry) RequireJoined(a c.DeliveryAttempt, cpl c.DeliveryCompletion) error {
	if !cpl.Matches(r.data().issuer, a) {
		return fail(foundation.Forbidden, nil)
	}
	return nil
}

// Only Worker calls this after socket closure / ticket.Done and all material
// borrowers have joined. A request timeout is never a call to this method.
func (w *deliveryWork) seal(out c.DeliveryOutcome) (c.DeliveryCompletion, error) {
	s := w.registry
	s.mu.Lock()
	defer s.mu.Unlock()
	if w.joined {
		return w.completion, nil
	}
	if w.finished || w.attempt.Validate() != nil {
		return c.DeliveryCompletion{}, invalid()
	}
	cpl, e := c.NewDeliveryCompletion(s.issuer, w.attempt, out)
	if e != nil {
		return cpl, e
	}
	w.joined = true
	w.completion = cpl
	return cpl, nil
}
func (w *deliveryWork) finish() {
	s := w.registry
	s.mu.Lock()
	defer s.mu.Unlock()
	if w.finished {
		return
	}
	w.finished = true
	w.cancel()
	if w.cleanupCancel != nil {
		w.cleanupCancel()
	}
	delete(s.works, w)
	if w.attempt.Validate() == nil {
		delete(s.active, w.attempt.Details().AttemptID.String())
	}
	close(s.changed)
	s.changed = make(chan struct{})
}
func (r *WorkRegistry) StopAdmission() { s := r.data(); s.mu.Lock(); s.stopped = true; s.mu.Unlock() }
func (r *WorkRegistry) Drain(ctx context.Context) error {
	r.StopAdmission()
	s := r.data()
	for {
		s.mu.Lock()
		if len(s.works) == 0 {
			s.mu.Unlock()
			return nil
		}
		ch := s.changed
		s.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return fail(foundation.DependencyUnavailable, ctx.Err())
		}
	}
}
func (r *WorkRegistry) requestForce(ctx context.Context) {
	s := r.data()
	s.mu.Lock()
	s.stopped = true
	if s.force == nil {
		s.force = ctx
	}
	for w := range s.works {
		w.cancel()
		if w.cleanupCancel != nil {
			w.cleanupCancel()
		}
	}
	s.mu.Unlock()
}
func (r *WorkRegistry) Force(ctx context.Context) error {
	r.requestForce(ctx)
	return r.Drain(ctx)
}
func (r *WorkRegistry) Joined() bool {
	s := r.data()
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.works) == 0
}
func (r WorkRegistry) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "mail_work_registry") }
func (r WorkRegistry) MarshalJSON() ([]byte, error) { return []byte(`"mail_work_registry"`), nil }
func (r WorkRegistry) LogValue() slog.Value         { return slog.StringValue("mail_work_registry") }

var _ c.DeliveryRuntime = (*WorkRegistry)(nil)
