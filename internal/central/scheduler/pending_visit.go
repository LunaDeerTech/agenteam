package scheduler

import (
	"context"
	"sync"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type PendingVisitAction string

const (
	PendingVisitDeferred PendingVisitAction = "deferred"
	PendingVisitLookup   PendingVisitAction = "lookup"
	PendingVisitLaunch   PendingVisitAction = "launch"
	PendingVisitBusy     PendingVisitAction = "busy_compensation"
	PendingVisitFailure  PendingVisitAction = "final_failure"
	PendingVisitRetry    PendingVisitAction = "retry"
)

// Action records the attempted path, not successful mutation. On a downstream
// error Found/After still identify the visited row; Dispatch is only the
// original driver's returned result. A scan/commit error returns no cursor.
type PendingVisitResult struct {
	Found    bool
	After    *DispatchID
	Action   PendingVisitAction
	Dispatch Dispatch
}

// PendingVisitor performs at most one synchronous visit. It has no timer,
// background traversal, retry policy or replacement Dispatch writer. The
// caller supplies normal serial pacing and starts a new pass with nil After
// after Found=false. An unresolved early row cannot starve later identities.
//
// The original handoff/compensator are borrowed, including their retained
// Unknown ownership. Stop/Drain/Joined here cover this visitor's actual calls;
// they do not retire or replace either driver's own lifecycle or Lookup.
type PendingVisitor struct {
	authority *PendingAuthority
	handoff   *LaunchHandoff
	busy      *BusyCompensator
	failure   *LaunchFailureFinalizer
	mu        sync.Mutex
	stopped   bool
	calls     map[i.ProjectID]*pendingVisitCall
	drained   chan struct{}
}
type pendingVisitContextKey struct{}
type pendingVisitCall struct {
	owner   *PendingVisitor
	project i.ProjectID
	id      DispatchID // owner.mu; set only after the committed scan
	cancel  context.CancelCauseFunc
}

func NewPendingVisitor(a *PendingAuthority, handoff *LaunchHandoff, busy *BusyCompensator) (*PendingVisitor, error) {
	if a == nil || nilPort(a.store) || handoff == nil || busy == nil || handoff.authority != a || busy.authority != a || nilPort(busy.deps.Projects) {
		return nil, fault(f.DependencyUnbound)
	}
	return &PendingVisitor{authority: a, handoff: handoff, busy: busy, calls: make(map[i.ProjectID]*pendingVisitCall), drained: make(chan struct{})}, nil
}

// The original constructor remains a bounded Launch/Busy visitor. This
// immutable composition additionally binds the actual final-failure owner;
// there is no late setter or replacement of retained Unknown ownership.
func NewPendingVisitorWithFailure(a *PendingAuthority, handoff *LaunchHandoff, busy *BusyCompensator, failure *LaunchFailureFinalizer) (*PendingVisitor, error) {
	s, err := NewPendingVisitor(a, handoff, busy)
	if err != nil {
		return nil, err
	}
	if failure == nil || failure.authority != a || nilPort(failure.deps.Work) || nilPort(failure.deps.Projects) {
		return nil, fault(f.DependencyUnbound)
	}
	s.failure = failure
	return s, nil
}
func (s *PendingVisitor) begin(ctx context.Context, p i.ProjectID) (context.Context, *pendingVisitCall, error) {
	if ctx == nil || p.Validate() != nil {
		return nil, nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if s == nil || s.authority == nil {
		return nil, nil, fault(f.DependencyUnbound)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil, nil, fault(f.ShuttingDown)
	}
	if s.calls[p] != nil {
		return nil, nil, fault(f.ResourceBusy)
	}
	owned, cancel := context.WithCancelCause(ctx)
	call := &pendingVisitCall{owner: s, project: p, cancel: cancel}
	s.calls[p] = call
	return context.WithValue(owned, pendingVisitContextKey{}, call), call, nil
}
func (s *PendingVisitor) finish(call *pendingVisitCall) {
	s.mu.Lock()
	delete(s.calls, call.project)
	if s.stopped && len(s.calls) == 0 {
		close(s.drained)
	}
	s.mu.Unlock()
	call.cancel(nil)
}
func (s *PendingVisitor) Stop() {
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
		cancels = append(cancels, call.cancel)
	}
	if len(s.calls) == 0 {
		close(s.drained)
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel(fault(f.ShuttingDown))
	}
}
func (s *PendingVisitor) Drain(ctx context.Context) error {
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
func (s *PendingVisitor) Joined() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped && len(s.calls) == 0
}

func (s *PendingVisitor) VisitNext(ctx context.Context, p i.ProjectID, after *DispatchID) (PendingVisitResult, error) {
	// Copy caller-owned cursor before any callback. It selects rows, never
	// grants permission or substitutes for their canonical Dispatch identity.
	cursor := ""
	if after != nil {
		id := *after
		if id.Validate() != nil {
			return PendingVisitResult{}, invalid()
		}
		cursor = id.String()
	}
	ctx, call, err := s.begin(ctx, p)
	if err != nil {
		return PendingVisitResult{}, err
	}
	defer s.finish(call)
	r, project, err := s.readNext(ctx, p, cursor)
	if err != nil || r == nil {
		return PendingVisitResult{}, err
	}
	return s.visit(ctx, call, r, project)
}

// Visit visits one original identity selected by a bounded traversal. It uses
// the same lifecycle, current scan and downstream owners as VisitNext; it
// does not reuse a snapshot row as a current scheduling permit.
func (s *PendingVisitor) Visit(ctx context.Context, p i.ProjectID, id DispatchID) (PendingVisitResult, error) {
	if id.Validate() != nil {
		return PendingVisitResult{}, invalid()
	}
	ctx, call, err := s.begin(ctx, p)
	if err != nil {
		return PendingVisitResult{}, err
	}
	defer s.finish(call)
	r, project, err := s.readIdentity(ctx, p, id)
	if err != nil || r == nil {
		return PendingVisitResult{}, err
	}
	return s.visit(ctx, call, r, project)
}

func (s *PendingVisitor) visit(ctx context.Context, call *pendingVisitCall, r *dispatchRecord, project pc.SchedulerProject) (PendingVisitResult, error) {
	p, id := call.project, r.id
	var err error
	out := PendingVisitResult{Found: true, After: &id, Action: PendingVisitDeferred, Dispatch: snapshot(r)}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	s.mu.Lock()
	call.id = id
	s.mu.Unlock()
	// The current Lookup may associate, so a paused visit only enumerates.
	// The actual write paths recheck pause in their own Tx below; this scan
	// is not a permit that survives a concurrent pause.
	if !project.Config.Enabled {
		return out, nil
	}
	switch {
	case r.outcome == Unknown:
		out.Action = PendingVisitLookup
		out.Dispatch, err = s.handoff.Lookup(ctx, p, id)
	case pendingBusy(r):
		out.Action = PendingVisitBusy
		// Resolve this exact compensator's retained Unknown before deciding
		// whether any Work call is permitted. No new owner or key is minted.
		out.Dispatch, err = s.busy.Lookup(ctx, p, id)
		if err == nil && out.Dispatch.Summary().Status == Pending {
			out.Dispatch, err = s.busy.CompensateAgentBusy(ctx, p, id)
		}
	case pendingFinalFailure(r) && s.failure != nil:
		out.Action = PendingVisitFailure
		// First retire any original rejection-checkpoint Unknown against its
		// now-observed durable marker. Then resolve the settlement owner's
		// physical Unknown before permitting another Work call.
		out.Dispatch, err = s.handoff.Lookup(ctx, p, id)
		if err == nil {
			out.Dispatch, err = s.failure.Lookup(ctx, p, id)
		}
		if err == nil && out.Dispatch.Summary().Status == Pending {
			out.Dispatch, err = s.failure.FinalizeLaunchFailure(ctx, p, id)
		}
	case r.outcome == NotSent && project.Project.CurrentSprintID != nil && project.Project.CurrentSprintID.String() == r.sprint:
		out.Action = PendingVisitLaunch
		out.Dispatch, err = s.handoff.LaunchOnce(ctx, p, id)
	case retryableTemporary(r) && s.handoff.retryProjects != nil && !r.nextRetry.Time().After(time.Now().UTC()) && project.Project.CurrentSprintID != nil && project.Project.CurrentSprintID.String() == r.sprint:
		// A durable checkpoint may resolve the same driver's retained
		// physical Unknown. Observe it before requesting another send;
		// RetryDue independently rechecks due/current gates in its own Tx.
		out.Action = PendingVisitRetry
		out.Dispatch, err = s.handoff.Lookup(ctx, p, id)
		if err == nil && out.Dispatch.Summary().Status == Pending {
			out.Dispatch, err = s.handoff.RetryDue(ctx, p, id)
		}
		// Untyped known_not_created remains pending. A legacy retry date or
		// previous attempt's temporary diagnostic cannot enable this branch.
	}
	return out, err
}
