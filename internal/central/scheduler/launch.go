package scheduler

import (
	"context"
	"errors"
	"sync"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// ExecutionLauncher is the original synchronous Execution service. Its return
// ends the physical call; a timeout or an unknown result is not a negative
// creation proof. The observer separately verifies canonical association.
type ExecutionLauncher interface {
	Launch(context.Context, i.Actor, ec.LaunchRequest) (ec.LaunchResult, error)
	LookupLaunch(context.Context, i.Actor, ec.LaunchLookupKey, f.Digest) (ec.LaunchLookup, error)
}
type LaunchHandoffDependencies struct {
	Executions   ExecutionLauncher
	Observations ec.DispatchObserver
}

// LaunchHandoff implements one initial send and original-key observation. It
// is not the Scheduler traversal/retry/compensation worker. In particular a
// known rejection leaves pending/known_not_created for the future Work-owned
// compensation; it never silently marks that Task skipped or failed.
type LaunchHandoff struct {
	authority *PendingAuthority
	deps      LaunchHandoffDependencies
	mu        sync.Mutex
	stopped   bool
	calls     map[DispatchID]*launchCall
	drained   chan struct{}
}
type launchContextKey struct{}
type launchCall struct {
	owner   *LaunchHandoff
	project i.ProjectID
	id      DispatchID
	actor   i.Actor
	cancel  context.CancelCauseFunc
	// All fields below are protected by owner.mu. record is an immutable
	// snapshot, never the mutable repository update buffer.
	record  *dispatchRecord
	live    bool
	send    bool
	running bool
	unknown error
}

func NewLaunchHandoff(a *PendingAuthority, deps LaunchHandoffDependencies) (*LaunchHandoff, error) {
	if a == nil || nilPort(a.store) || nilPort(deps.Executions) || nilPort(deps.Observations) {
		return nil, fault(f.DependencyUnbound)
	}
	return &LaunchHandoff{authority: a, deps: deps, calls: make(map[DispatchID]*launchCall), drained: make(chan struct{})}, nil
}

func (s *LaunchHandoff) begin(ctx context.Context, p i.ProjectID, id DispatchID, lookup bool) (context.Context, *launchCall, error) {
	if ctx == nil || p.Validate() != nil || id.Validate() != nil {
		return nil, nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if s == nil || s.authority == nil {
		return nil, nil, fault(f.DependencyUnbound)
	}
	actor, err := schedulerActor(p, id.String())
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	call := s.calls[id]
	if call != nil {
		if call.project != p || call.running {
			return nil, nil, fault(f.ResourceBusy)
		}
		if !lookup {
			return nil, nil, call.unknown
		}
	} else {
		if s.stopped {
			return nil, nil, fault(f.ShuttingDown)
		}
		call = &launchCall{owner: s, project: p, id: id, actor: actor}
		s.calls[id] = call
	}
	owned, cancel := context.WithCancelCause(ctx)
	call.cancel, call.running = cancel, true
	return context.WithValue(owned, launchContextKey{}, call), call, nil
}
func (s *LaunchHandoff) finish(call *launchCall, retain bool, err error) {
	s.mu.Lock()
	call.live, call.send, call.running = false, false, false
	if retain {
		if call.unknown == nil {
			call.unknown = err
		}
	} else {
		delete(s.calls, call.id)
	}
	cancel := call.cancel
	if s.stopped && len(s.calls) == 0 {
		select {
		case <-s.drained:
		default:
			close(s.drained)
		}
	}
	s.mu.Unlock()
	cancel(nil)
}
func (s *LaunchHandoff) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	cancels := make([]context.CancelCauseFunc, 0, len(s.calls))
	for _, call := range s.calls {
		if call.running {
			cancels = append(cancels, call.cancel)
		}
	}
	if len(s.calls) == 0 {
		close(s.drained)
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel(fault(f.ShuttingDown))
	}
}
func (s *LaunchHandoff) Drain(ctx context.Context) error {
	if ctx == nil {
		return invalid()
	}
	if s == nil {
		return nil
	}
	select {
	case <-s.drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *LaunchHandoff) Joined() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped && len(s.calls) == 0
}

// LaunchOnce permits a send only to the caller whose not_sent -> unknown
// marker was known committed. A marker CommitUnknown issues no handoff and no
// Launch. A crash in that gap is intentionally recoverable by lookup only.
func (s *LaunchHandoff) LaunchOnce(ctx context.Context, p i.ProjectID, id DispatchID) (out Dispatch, err error) {
	ctx, call, err := s.begin(ctx, p, id, false)
	if err != nil {
		return Dispatch{}, err
	}
	retain := false
	defer func() { s.finish(call, retain, err) }()
	r, sent, err := s.markSending(ctx, call)
	if err != nil {
		_, retain = UnknownAttempt(err)
		return Dispatch{}, err
	}
	if !sent {
		switch {
		case r.status == Launched:
			return snapshot(r), nil
		case r.status == Pending && r.outcome == Unknown:
			retain = true
			return Dispatch{}, pendingUnknown()
		default:
			return snapshot(r), fault(f.InvalidState)
		}
	}
	s.activate(call, r, true)
	result, launchErr := s.deps.Executions.Launch(ctx, call.actor, r.launch.Clone())
	// Retire the permission only after the original synchronous call returns.
	s.deactivate(call)
	if launchErr != nil {
		// Classify the original result, before safe wrapping can introduce a
		// NotStarted fault around an opaque transport/dependency error.
		rejected := knownNotCreated(launchErr)
		launchErr = portError(launchErr)
		if !rejected {
			retain = true
			return Dispatch{}, uncertainLaunch(launchErr)
		}
		observed, checkpointErr := s.recordRejected(ctx, call, r)
		if checkpointErr != nil {
			retain = true
			return Dispatch{}, uncertainLaunch(errors.Join(launchErr, checkpointErr))
		}
		return snapshot(observed), launchErr
	}
	if !matchesExecution(r, result.Execution) {
		retain = true
		return Dispatch{}, uncertainLaunch(unavailable(nil))
	}
	associated, err := s.associate(ctx, call, r, result.Execution.ID)
	if err != nil {
		retain = true
		return Dispatch{}, uncertainLaunch(err)
	}
	return snapshot(associated), nil
}

// Lookup never dispatches, retries, discovers Work or mints a Launch permit.
// It also resumes this driver's retained unknown after Stop. A fresh driver
// can observe a crash survivor using its durable original Dispatch identity.
// Not observed is deliberately inconclusive, including a crash before send.
func (s *LaunchHandoff) Lookup(ctx context.Context, p i.ProjectID, id DispatchID) (out Dispatch, err error) {
	ctx, call, err := s.begin(ctx, p, id, true)
	if err != nil {
		return Dispatch{}, err
	}
	s.mu.Lock()
	prior := call.unknown
	s.mu.Unlock()
	retain := prior != nil
	defer func() { s.finish(call, retain, err) }()
	r, err := s.readOriginal(ctx, call)
	if err != nil {
		return Dispatch{}, err
	}
	if r.status == Launched || r.outcome == KnownNotCreated {
		retain = false
		return snapshot(r), nil
	}
	if r.outcome != Unknown {
		if prior != nil {
			return Dispatch{}, prior
		}
		return snapshot(r), nil
	}
	retain = true
	s.activate(call, r, false)
	observed, err := s.deps.Executions.LookupLaunch(ctx, call.actor, lookupKey(r), r.digest)
	s.deactivate(call)
	if err != nil {
		return Dispatch{}, portError(err)
	}
	if !observed.Found {
		if observed.Execution != nil || observed.RequestDigest != "" {
			return Dispatch{}, unavailable(nil)
		}
		if prior != nil {
			return Dispatch{}, prior
		}
		return Dispatch{}, pendingUnknown()
	}
	if observed.Execution == nil || observed.RequestDigest != r.digest || !matchesExecution(r, *observed.Execution) {
		return Dispatch{}, unavailable(nil)
	}
	r, err = s.associate(ctx, call, r, observed.Execution.ID)
	if err != nil {
		return Dispatch{}, err
	}
	retain = false
	return snapshot(r), nil
}
func (s *LaunchHandoff) activate(c *launchCall, r *dispatchRecord, send bool) {
	s.mu.Lock()
	c.record, c.live, c.send = r, true, send
	s.mu.Unlock()
}
func (s *LaunchHandoff) deactivate(c *launchCall) {
	s.mu.Lock()
	c.live, c.send = false, false
	s.mu.Unlock()
}
func pendingUnknown() error {
	err := f.NewFault(f.CommitUnknown, f.Unknown)
	err.RetryHint = "lookup"
	return err
}
func uncertainLaunch(cause error) error {
	err := f.NewFault(f.CommitUnknown, f.Unknown)
	err.RetryHint = "lookup"
	return err.WithCause(portError(cause))
}
func knownNotCreated(err error) bool {
	var v *f.Fault
	return errors.As(err, &v) && v.Code != f.CommitUnknown && (v.CommitState == f.NotStarted || v.CommitState == f.NotCommitted)
}
func matchesExecution(r *dispatchRecord, e ec.Summary) bool {
	return e.ID.Validate() == nil && e.ProjectID == r.project && e.AgentID == r.agent && e.Trigger == r.launch.Trigger && e.Purpose == r.launch.Purpose && e.Status.Valid() && e.Version.Validate() == nil
}
