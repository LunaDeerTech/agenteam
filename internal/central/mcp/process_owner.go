package mcp

import (
	"context"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// ProcessOptions binds the existing root process. It is construction data,
// never evidence that an earlier request has stopped or a grant to its secrets.
type ProcessOptions struct {
	Guard *object.ProcessGuard
	ID    oc.ProcessID
}

type processOwner struct {
	store    Store
	guard    *object.ProcessGuard
	process  oc.ProcessID
	mu       sync.Mutex
	draining bool
	stopped  bool
	changed  chan struct{}
	calls    map[*processCall]struct{}
}

// A retained call has physically returned, but its original durable result or
// cleanup is still unconfirmed. Only its original observation may retire it.
type processCall struct {
	owner    *processOwner
	cancel   context.CancelFunc
	returned bool
	observe  func(context.Context) (bool, error)
}

func newProcessOwner(store Store, options ProcessOptions) (*processOwner, error) {
	if nilPort(store) || options.Guard == nil || options.ID.Validate() != nil {
		return nil, fail(f.DependencyUnbound)
	}
	// The root may construct this graph before binding the Object service. An
	// actual operation always checks the bound guard again before admission.
	if process, err := options.Guard.CurrentProcess(); err == nil && process != options.ID {
		return nil, fail(f.DependencyUnbound)
	}
	return &processOwner{store: store, guard: options.Guard, process: options.ID, changed: make(chan struct{}), calls: make(map[*processCall]struct{})}, nil
}

func (o *processOwner) signalLocked() {
	close(o.changed)
	o.changed = make(chan struct{})
}

func (o *processOwner) begin(ctx context.Context) (context.Context, *processCall, error) {
	if o == nil || ctx == nil || o.guard == nil {
		return nil, nil, fail(f.DependencyUnbound)
	}
	process, err := o.guard.CurrentProcess()
	if err != nil || process != o.process {
		return nil, nil, fail(f.DependencyUnavailable)
	}
	if err = ctx.Err(); err != nil {
		return nil, nil, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.stopped {
		return nil, nil, fail(f.ShuttingDown)
	}
	child, cancel := context.WithCancel(ctx)
	call := &processCall{owner: o, cancel: cancel}
	o.calls[call] = struct{}{}
	o.signalLocked()
	return child, call, nil
}

func (o *processOwner) requireInTx(ctx context.Context, tx f.Tx, call *processCall) error {
	if o == nil || call == nil || call.owner != o || !tx.Valid() {
		return fail(f.Forbidden)
	}
	o.mu.Lock()
	_, active := o.calls[call]
	o.mu.Unlock()
	if !active {
		return fail(f.Forbidden)
	}
	return safeError(o.guard.RequireCurrentProcessInTx(ctx, tx, o.process))
}

// returned is called only after the original protocol/body/child has returned.
// A nonnil observer retains the original call, including after cancellation.
func (c *processCall) returnedWith(observe func(context.Context) (bool, error)) {
	if c == nil || c.owner == nil {
		return
	}
	o := c.owner
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, active := o.calls[c]; !active || c.returned {
		return
	}
	c.returned, c.observe = true, observe
	c.cancel()
	if observe == nil {
		delete(o.calls, c)
	}
	o.signalLocked()
}

func (o *processOwner) stop() {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stopped = true
	for call := range o.calls {
		call.cancel()
	}
	o.signalLocked()
}

func (o *processOwner) join(ctx context.Context) error {
	if o == nil || ctx == nil {
		return fail(f.DependencyUnbound)
	}
	o.stop()
	for {
		o.mu.Lock()
		if !o.draining {
			o.draining = true
			o.mu.Unlock()
			break
		}
		changed := o.changed
		o.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
	defer func() { o.mu.Lock(); o.draining = false; o.signalLocked(); o.mu.Unlock() }()
	for {
		o.mu.Lock()
		waiting := false
		for call := range o.calls {
			waiting = waiting || !call.returned
		}
		changed := o.changed
		var retained []*processCall
		if !waiting {
			for call := range o.calls {
				retained = append(retained, call)
			}
		}
		o.mu.Unlock()
		if waiting {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-changed:
				continue
			}
		}
		for _, call := range retained {
			if err := ctx.Err(); err != nil {
				return err
			}
			confirmed, err := call.observe(ctx)
			if err != nil {
				return err
			}
			if !confirmed {
				return fail(f.ResourceBusy)
			}
			o.mu.Lock()
			delete(o.calls, call)
			o.signalLocked()
			o.mu.Unlock()
		}
		return nil
	}
}

func (o *processOwner) joined() bool {
	if o == nil {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.stopped && len(o.calls) == 0
}

func (s *Service) StopAdmission() {
	if s != nil {
		s.owner.stop()
	}
}
func (s *Service) Drain(ctx context.Context) error {
	if s == nil {
		return fail(f.DependencyUnbound)
	}
	return s.owner.join(ctx)
}
func (s *Service) Force(ctx context.Context) error { return s.Drain(ctx) }
func (s *Service) Joined() bool                    { return s != nil && s.owner.joined() }
