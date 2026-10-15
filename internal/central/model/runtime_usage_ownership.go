package model

import (
	"context"
	"reflect"
	"sync"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

type runtimeUsageKey struct{}

// This object starts with the original public admission, before preflight SQL,
// and transfers to its original runtimeCall. It is not an InvocationRequest
// grant. In particular, internal releaseLocal and a Tx callback returning do
// not prove that all public methods and the original Store have returned.
type runtimeUsageCall struct {
	mu                sync.Mutex
	runtime           *runtimeState
	admission         *runtimeAdmission
	call              *runtimeCall
	request           mc.ModelRequest
	binding           f.Digest
	ctx               context.Context
	token             uc.ProjectInvocationCall
	borrows           int
	acquiring         bool
	admissionReturned bool
	closing, returned bool
	changed           chan struct{}
}

func runtimeUsageBorrower(s *runtimeState) uc.ProjectInvocationBorrower {
	if s == nil {
		return nil
	}
	owner, ok := s.deps.Usage.(uc.ProjectInvocationOwnership)
	if !ok {
		return nil
	}
	return owner.ProjectInvocationBorrower()
}

func (r *Runtime) usageOuter(ctx context.Context) (context.Context, *runtimeUsageCall) {
	s := r.state()
	if s == nil || ctx == nil {
		return ctx, nil
	}
	if nilPort(runtimeUsageBorrower(s)) {
		return ctx, nil
	}
	o := &runtimeUsageCall{runtime: s, borrows: 1, changed: make(chan struct{})}
	ctx = context.WithValue(ctx, runtimeUsageKey{}, o)
	o.ctx = ctx
	return ctx, o
}

func runtimeUsageRequestBinding(request mc.ModelRequest) (f.Digest, error) {
	if request.ValidateInvocation() != nil {
		return "", fault(f.InvalidArgument)
	}
	raw, err := encoded(struct {
		Actor   id.ActorDetails
		Request mc.ModelRequest
	}{request.Actor.Details(), request})
	if err != nil {
		return "", err
	}
	return hash(raw), nil
}

func acquireRuntimeUsage(ctx context.Context, s *runtimeState, admission *runtimeAdmission, request mc.ModelRequest) (*runtimeUsageCall, error) {
	borrower := runtimeUsageBorrower(s)
	if nilPort(borrower) {
		return nil, nil
	}
	o, ok := ctx.Value(runtimeUsageKey{}).(*runtimeUsageCall)
	if !ok || o == nil || o.runtime != s {
		return nil, fault(f.DependencyUnbound)
	}
	binding, err := runtimeUsageRequestBinding(request)
	if err != nil {
		return nil, err
	}
	o.mu.Lock()
	if o.admission != nil || o.borrows != 1 || o.returned {
		o.mu.Unlock()
		return nil, fault(f.Forbidden)
	}
	o.admission, o.request, o.binding, o.acquiring = admission, request.Clone(), binding, true
	o.mu.Unlock()
	s.mu.Lock()
	if s.usageCalls == nil {
		s.usageCalls = make(map[*runtimeUsageCall]struct{})
	}
	s.usageCalls[o] = struct{}{}
	s.mu.Unlock()
	// The original token must survive a registration Unknown. Even when there
	// is no runtimeCall, the admission's outer return closes this same owner.
	token, err := borrower.AcquireProjectInvocation(ctx, s.store, request.Clone())
	o.mu.Lock()
	o.token, o.acquiring = token, false
	o.notifyLocked()
	o.mu.Unlock()
	if nilPort(token) && err == nil {
		err = fault(f.DependencyUnbound)
	}
	return o, err
}

func (o *runtimeUsageCall) notifyLocked() {
	close(o.changed)
	o.changed = make(chan struct{})
}

func (o *runtimeUsageCall) admissionDone() {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.admissionReturned = true
	o.notifyLocked()
	o.mu.Unlock()
}

func (o *runtimeUsageCall) transfer(c *runtimeCall) {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.call = c
	o.mu.Unlock()
}

func (c *runtimeCall) borrowUsage(ctx context.Context) (context.Context, *runtimeUsageCall, error) {
	o := c.usage
	if o == nil {
		return ctx, nil, nil
	}
	if err := runtimeContextError(ctx); err != nil {
		return ctx, nil, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closing {
		return ctx, nil, fault(f.ResourceBusy)
	}
	if o.returned {
		// A fully joined handle permits only the existing result observation
		// and idempotent Close paths. It cannot create a new invocation owner.
		return ctx, nil, nil
	}
	o.borrows++
	return context.WithValue(ctx, runtimeUsageKey{}, o), o, nil
}

func (o *runtimeUsageCall) leave(original error) error {
	if o == nil {
		return original
	}
	o.mu.Lock()
	o.borrows--
	o.notifyLocked()
	ready := o.borrows == 0 && o.admissionReturned && !o.acquiring && !o.closing && !o.returned
	o.mu.Unlock()
	if !ready || !o.businessReturned() {
		return original
	}
	// Same private owner, bounded technical return only. Cancellation cannot
	// discard a registered/uncertain Usage owner or authorize another Invoke.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(o.ctx), 3*time.Second)
	defer cancel()
	err := o.closeReturned(ctx)
	if original != nil {
		return original
	}
	return err
}

func (o *runtimeUsageCall) businessReturned() bool {
	o.mu.Lock()
	c := o.call
	o.mu.Unlock()
	if c == nil {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.joined && c.pending == nil && c.retirement == nil && !c.materialOwned && (c.exchange == nil || c.ioRetired) && (!c.handoff || c.startReturned)
}

func (o *runtimeUsageCall) closeReturned(ctx context.Context) error {
	if !o.businessReturned() {
		return fault(f.ResourceBusy)
	}
	o.mu.Lock()
	if o.returned {
		o.mu.Unlock()
		return nil
	}
	if o.closing || o.acquiring || o.borrows != 0 || !o.admissionReturned {
		o.mu.Unlock()
		return fault(f.ResourceBusy)
	}
	o.closing = true
	token := o.token
	o.mu.Unlock()
	var err error
	if !nilPort(token) {
		err = token.Returned(context.WithValue(ctx, runtimeUsageKey{}, o))
	}
	o.mu.Lock()
	o.closing = false
	if err == nil {
		o.returned = true
		o.request.Messages = nil
	}
	o.notifyLocked()
	o.mu.Unlock()
	if err == nil {
		o.runtime.mu.Lock()
		delete(o.runtime.usageCalls, o)
		o.runtime.mu.Unlock()
	}
	return err
}

func (o *runtimeUsageCall) drain(ctx context.Context) error {
	for {
		o.mu.Lock()
		returned := o.returned
		waiting := o.borrows != 0 || o.acquiring || o.closing || !o.admissionReturned
		changed := o.changed
		o.mu.Unlock()
		if returned {
			return nil
		}
		if !waiting {
			return o.closeReturned(ctx)
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return unavailable(ctx.Err())
		}
	}
}

func (c *runtimeCall) usageJoined() bool {
	if c.usage == nil {
		return true
	}
	c.usage.mu.Lock()
	defer c.usage.mu.Unlock()
	return c.usage.returned
}

func (c *runtimeCall) withinUsage(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	if c.usage == nil {
		return c.runtime.store.WithinTx(ctx, cause, fn)
	}
	c.usage.mu.Lock()
	token := c.usage.token
	c.usage.mu.Unlock()
	if nilPort(token) {
		return f.NotCommittedResult(f.NewFault(f.DependencyUnbound, f.NotCommitted))
	}
	return token.WithinTx(context.WithValue(ctx, runtimeUsageKey{}, c.usage), cause, fn)
}

func sameRuntimeUsageToken(a, b uc.ProjectInvocationCall) bool {
	if nilPort(a) || nilPort(b) {
		return false
	}
	x, y := reflect.ValueOf(a), reflect.ValueOf(b)
	return x.Type() == y.Type() && x.Kind() == reflect.Pointer && x.Pointer() == y.Pointer()
}

func (a *RuntimeAuthority) originalUsage(ctx context.Context, store any, request mc.ModelRequest) (*runtimeUsageCall, error) {
	if err := runtimeContextError(ctx); err != nil {
		return nil, err
	}
	originalStore, ok := store.(Store)
	if a.state() == nil || !ok || !sameStore(a.state().store, originalStore) {
		return nil, fault(f.DependencyUnbound)
	}
	o, ok := ctx.Value(runtimeUsageKey{}).(*runtimeUsageCall)
	if !ok || o == nil || o.runtime == nil || o.runtime.authority != a {
		return nil, fault(f.DependencyUnbound)
	}
	a.state().mu.Lock()
	currentRuntime := a.state().runtime == o.runtime
	a.state().mu.Unlock()
	binding, err := runtimeUsageRequestBinding(request)
	if err != nil {
		return nil, err
	}
	o.mu.Lock()
	valid := binding == o.binding && o.admission != nil && !o.returned
	o.mu.Unlock()
	o.runtime.mu.Lock()
	_, registered := o.runtime.usageCalls[o]
	o.runtime.mu.Unlock()
	if !currentRuntime || !valid || !registered {
		return nil, fault(f.Forbidden)
	}
	return o, nil
}

func (a *RuntimeAuthority) RequireOriginalInvocationCall(ctx context.Context, store any, request mc.ModelRequest, token uc.ProjectInvocationCall) error {
	o, err := a.originalUsage(ctx, store, request)
	if err != nil {
		return err
	}
	o.mu.Lock()
	valid := o.borrows > 0 && !o.closing
	if nilPort(token) {
		valid = valid && o.acquiring && nilPort(o.token) && !o.admissionReturned
	} else {
		valid = valid && !o.acquiring && sameRuntimeUsageToken(o.token, token)
	}
	admission := o.admission
	acquiring := o.acquiring
	o.mu.Unlock()
	if acquiring {
		o.runtime.mu.Lock()
		_, current := o.runtime.admissions[admission]
		o.runtime.mu.Unlock()
		valid = valid && current
	}
	if !valid {
		return fault(f.Forbidden)
	}
	return nil
}

func (a *RuntimeAuthority) RequireOriginalInvocationReturned(ctx context.Context, store any, request mc.ModelRequest, token uc.ProjectInvocationCall) error {
	o, err := a.originalUsage(ctx, store, request)
	if err != nil {
		return err
	}
	o.mu.Lock()
	valid := !o.acquiring && o.borrows == 0 && o.admissionReturned && sameRuntimeUsageToken(o.token, token)
	o.mu.Unlock()
	if !valid || !o.businessReturned() {
		return fault(f.ResourceBusy)
	}
	return nil
}

func (a *RuntimeAuthority) CancelOriginalInvocation(ctx context.Context, store any, request mc.ModelRequest, token uc.ProjectInvocationCall) error {
	o, err := a.originalUsage(ctx, store, request)
	if err != nil {
		return err
	}
	o.mu.Lock()
	valid := !o.acquiring && sameRuntimeUsageToken(o.token, token)
	acquiring := o.acquiring && nilPort(o.token) && !nilPort(token) && !o.admissionReturned
	c, admission := o.call, o.admission
	o.mu.Unlock()
	if acquiring {
		o.runtime.mu.Lock()
		_, current := o.runtime.admissions[admission]
		o.runtime.mu.Unlock()
		// During Acquire the original registration Store has not returned its
		// token yet. This private admission can only be cancelled; this branch
		// neither binds that token nor permits WithinTx/Returned to consume it.
		valid = current && c == nil
	}
	if !valid {
		return fault(f.Forbidden)
	}
	if c != nil {
		c.cancel()
	} else {
		admission.cancel()
	}
	return nil
}

var _ uc.ProjectInvocationFacts = (*RuntimeAuthority)(nil)
