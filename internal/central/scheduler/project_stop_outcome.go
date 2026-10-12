package scheduler

import (
	"context"
	"errors"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// Only the original owner can supply these snapshots. No public dispatch or
// invocation DTO authorizes confirming a physical business attempt.
type projectCallOutcome struct {
	claim      *claimCall
	launch     *launchCall
	busy       *busyCall
	failure    *failureCall
	relaunch   *relaunchCall
	record     *dispatchRecord
	parentOnly bool
}

func projectEntryDispatch(e *projectCallEntry) (DispatchID, bool) {
	key := string(e.identity.Key())
	for _, prefix := range []string{"scheduler_claim:", "scheduler_dispatch:", "scheduler_relaunch:"} {
		if strings.HasPrefix(key, prefix) {
			id, err := f.ParseID[DispatchIdentity](strings.TrimPrefix(key, prefix))
			return id, err == nil
		}
	}
	return DispatchID{}, false
}
func (s *projectCallState) captureProjectOutcome(e *projectCallEntry, original error) *projectCallOutcome {
	v := &projectCallOutcome{}
	id, ok := projectEntryDispatch(e)
	switch e.record.Lane {
	case projectLaneCoordinator:
		if ok {
			s.coordinator.mu.Lock()
			v.claim = s.coordinator.unknown[id]
			s.coordinator.mu.Unlock()
		}
	case projectLaneHandoff:
		if ok {
			s.handoff.mu.Lock()
			v.launch = s.handoff.calls[id]
			if v.launch != nil && v.launch.record != nil {
				r := *v.launch.record
				v.record = &r
			}
			s.handoff.mu.Unlock()
		}
	case projectLaneBusy:
		if ok {
			s.busy.mu.Lock()
			v.busy = s.busy.calls[id]
			if v.busy != nil && v.busy.record != nil {
				r := *v.busy.record
				v.record = &r
			}
			s.busy.mu.Unlock()
		}
	case projectLaneFailure:
		if ok {
			s.failure.mu.Lock()
			v.failure = s.failure.calls[id]
			if v.failure != nil && v.failure.record != nil {
				r := *v.failure.record
				v.record = &r
			}
			s.failure.mu.Unlock()
		}
	case projectLaneRelaunch:
		if ok {
			s.relaunch.mu.Lock()
			v.relaunch = s.relaunch.unknown[id]
			s.relaunch.mu.Unlock()
		}
	case projectLaneVisitor:
		v.parentOnly = s.parentOutcomeKnown(e, original)
	case projectLaneRunner:
		var run *ProjectRunError
		if errors.As(original, &run) {
			for _, execution := range run.Observation().Executions {
				if uncertainProjectVisit(execution.Err) {
					return v
				}
			}
		}
		v.parentOnly = s.parentOutcomeKnown(e, original)
	}
	return v
}

// Only a known read-only original transaction, or this exact nested child's
// original error, can establish a parent's stop-confirmation branch.
func (s *projectCallState) parentOutcomeKnown(e *projectCallEntry, original error) bool {
	if r, ok := UnknownAttempt(original); ok {
		d := r.Cause().Details()
		owners := d.Primary.OwnerIDs()
		if d.Kind == f.CommandsCause && d.Primary.Namespace() == "scheduler" && len(owners) == 1 && owners[0] == e.record.Project.String() {
			switch d.Primary.Command() {
			case "capture_traversal", "visit_traversal", "capture_launched", "observe_launched", "visit_pending":
				return true
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, originalChild := range e.childOutcomes {
		if sameProjectOutcomeError(original, originalChild) {
			return true
		}
	}
	return false
}

func normalizeProjectCallLocks(project i.ProjectID, extra []f.LockRequest) ([]f.LockRequest, error) {
	return oc.NormalizeLocks(append(projectCallLocks(project), extra...))
}

// A later normal Resolve/Lookup may have already retired the original owner.
// The saved exact snapshot still permits a read-only positive observation;
// it never turns absence into rollback or invokes a writing lookup itself.
func (s *projectCallState) reserveProjectOutcome(v *projectCallOutcome) (func(bool), bool) {
	if v == nil {
		return nil, false
	}
	if c := v.claim; c != nil {
		owner := s.coordinator
		id, _ := f.ParseID[DispatchIdentity](c.request.DispatchID)
		owner.mu.Lock()
		current := owner.unknown[id]
		if current != nil && (current != c || c.resolving) {
			owner.mu.Unlock()
			return nil, false
		}
		if current == c {
			c.resolving = true
		}
		owner.mu.Unlock()
		return func(success bool) {
			owner.mu.Lock()
			remove := success && owner.unknown[id] == c
			if remove {
				delete(owner.unknown, id)
			}
			c.resolving = false
			owner.mu.Unlock()
			if remove {
				owner.release(c)
			}
		}, true
	}
	if c := v.relaunch; c != nil {
		owner := s.relaunch
		id, _ := f.ParseID[DispatchIdentity](c.request.DispatchID)
		owner.mu.Lock()
		current := owner.unknown[id]
		if current != nil && (current != c || c.resolving) {
			owner.mu.Unlock()
			return nil, false
		}
		if current == c {
			c.resolving = true
		}
		owner.mu.Unlock()
		return func(success bool) {
			owner.mu.Lock()
			remove := success && owner.unknown[id] == c
			if remove {
				delete(owner.unknown, id)
			}
			c.resolving = false
			owner.mu.Unlock()
			if remove {
				owner.releaseRelaunch(c)
			}
		}, true
	}
	if c := v.launch; c != nil {
		owner := s.handoff
		owner.mu.Lock()
		current := owner.calls[c.id]
		if current != nil && (current != c || c.running) {
			owner.mu.Unlock()
			return nil, false
		}
		if current == c {
			c.running = true
		}
		owner.mu.Unlock()
		return func(success bool) {
			owner.mu.Lock()
			present := owner.calls[c.id] == c
			if present && !success {
				c.running = false
			}
			owner.mu.Unlock()
			if present && success {
				owner.finish(c, false, nil)
			}
		}, true
	}
	if c := v.busy; c != nil {
		owner := s.busy
		owner.mu.Lock()
		current := owner.calls[c.id]
		if current != nil && (current != c || c.running) {
			owner.mu.Unlock()
			return nil, false
		}
		if current == c {
			c.running = true
		}
		owner.mu.Unlock()
		return func(success bool) {
			owner.mu.Lock()
			present := owner.calls[c.id] == c
			if present && !success {
				c.running = false
			}
			owner.mu.Unlock()
			if present && success {
				owner.finish(c, false, nil)
			}
		}, true
	}
	if c := v.failure; c != nil {
		owner := s.failure
		owner.mu.Lock()
		current := owner.calls[c.id]
		if current != nil && (current != c || c.running) {
			owner.mu.Unlock()
			return nil, false
		}
		if current == c {
			c.running = true
		}
		owner.mu.Unlock()
		return func(success bool) {
			owner.mu.Lock()
			present := owner.calls[c.id] == c
			if present && !success {
				c.running = false
			}
			owner.mu.Unlock()
			if present && success {
				owner.finish(c, false, nil)
			}
		}, true
	}
	if v.parentOnly {
		return func(bool) {}, true
	}
	return nil, false
}

func (s *projectCallState) confirmBusinessOutcome(ctx context.Context, e *projectCallEntry, stop *ownedProjectStop) error {
	s.mu.Lock()
	original, v := e.outcomeErr, e.outcome
	childrenDone := true
	for _, child := range e.children {
		if !child.returned || !child.closed && !child.absent && child.registration.State() != f.NotCommitted {
			childrenDone = false
		}
	}
	s.mu.Unlock()
	if original == nil {
		return nil
	}
	if v == nil || v.parentOnly && !childrenDone {
		return original
	}
	release, ok := s.reserveProjectOutcome(v)
	if !ok {
		return original
	}
	success := false
	defer func() { release(success) }()
	cause, _ := f.NewRecoveryCause("scheduler-call-outcome", e.record.ID.String(), e.record.Project.String())
	if result, ok := UnknownAttempt(original); ok {
		cause = result.Cause()
	}
	var extra []f.LockRequest
	var err error
	switch {
	case v.claim != nil:
		extra, err = claimLocks(v.claim.request, v.claim.launch)
	case v.relaunch != nil:
		extra, err = relaunchLocks(v.relaunch.request, v.relaunch.launch)
	case v.launch != nil:
		extra, err = handoffLocks(e.record.Project, v.launch.id, "lookup_launch", v.record)
	case v.busy != nil:
		extra, err = handoffLocks(e.record.Project, v.busy.id, "lookup_busy_compensation", v.record)
	case v.failure != nil:
		extra, err = handoffLocks(e.record.Project, v.failure.id, "lookup_launch_failure", v.record)
	}
	if err != nil {
		return original
	}
	found := false
	result := s.callTx(ctx, cause, e.record, stop, extra, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		technical, err := loadProjectCall(ctx, x, e.record.ID)
		if err != nil {
			return err
		}
		if technical == nil || !e.record.sameOwner(*technical) || technical.Phase != "running" {
			return fault(f.ConfirmationStale)
		}
		if v.parentOnly {
			found = true
			return ctx.Err()
		}
		if v.relaunch != nil {
			receipt, err := loadRelaunchReceipt(ctx, x, v.relaunch.request)
			if err != nil || receipt == nil {
				return err
			}
			if _, err = relaunchReceiptResult(ctx, x, v.relaunch, receipt, true); err != nil {
				return err
			}
			found = true
			return ctx.Err()
		}
		id, ok := projectEntryDispatch(e)
		if !ok {
			return fault(f.InvalidState)
		}
		row, err := loadDispatch(ctx, x, e.record.Project, id)
		if err != nil || row == nil {
			return err
		}
		switch {
		case v.claim != nil:
			if row.retryPolicy != v.claim.retryPolicy {
				return fault(f.ConfirmationStale)
			}
			if err = sameClaim(row, v.claim.request, v.claim.launch); err != nil {
				return err
			}
			found = true
		case v.launch != nil:
			// A marker without its original full snapshot remains unresolved. An
			// unassociated Execution observation is not a Scheduler receipt.
			if v.record == nil || !sameDispatch(v.record, row) || v.record.attempts != row.attempts {
				return fault(f.ConfirmationStale)
			}
			found = row.status == Launched && row.outcome == Created && row.execution != nil || row.outcome == KnownNotCreated && row.execution == nil
		case v.busy != nil:
			found = sameDispatch(v.record, row) && v.record.busyAttempt == row.busyAttempt && v.record.attempts == row.attempts && completedBusy(row)
		case v.failure != nil:
			found = sameDispatch(v.record, row) && sameFinalFailure(v.record, row) && v.record.attempts == row.attempts && completedFinalFailure(row)
		}
		return ctx.Err()
	})
	if result.State() != f.Committed || !found || ctx.Err() != nil {
		return original
	}
	success = true
	s.mu.Lock()
	e.outcomeErr = nil
	s.signalLocked()
	s.mu.Unlock()
	return nil
}

// These original entry points already own normal recovery. Reborrowing the
// same retained technical entry lets that original path execute, without
// manufacturing a new owner or permitting the Stop path to write an outcome.
func projectCallCanResume(e *projectCallEntry) bool {
	if e.outcomeErr == nil || e.unknown != nil {
		return false
	}
	switch e.identity.Command() {
	case "managed_project_run", "project_run", "project_traversal", "visit", "visit_next", "resolve_claim", "resolve_relaunch", "lookup_launch", "lookup_busy_compensation", "lookup_launch_failure":
		return true
	}
	return false
}

// Same-identity retries first advance the old custody, instead of allocating
// another owner or blocking the original manager's recovery forever. Each
// explicit call performs at most one original confirmation or close.
func (s *projectCallState) resumePrevious(ctx context.Context, p i.ProjectID, lane projectCallLane, identity f.CommandIdentity) error {
	s.mu.Lock()
	if s.projectKeyBusyLocked(p, lane, identity.Key(), nil) {
		s.mu.Unlock()
		return fault(f.ResourceBusy)
	}
	var prior *projectCallEntry
	for _, e := range s.owners {
		if e.record.Project == p && e.record.Lane == lane && e.record.Identity == identity.Canonical() && e.returned {
			prior = e
			break
		}
	}
	if prior == nil || projectCallCanResume(prior) {
		s.mu.Unlock()
		return nil
	}
	next := s.projectLeafLocked(prior)
	original := prior.outcomeErr
	if original == nil && prior.unknown != nil {
		original = commitError(*prior.unknown)
	}
	s.mu.Unlock()
	if next == nil {
		if original != nil {
			return original
		}
		return fault(f.ResourceBusy)
	}
	_, err := s.settle(ctx, next, nil, false)
	if err != nil {
		return err
	}
	// Confirmation and retry are different explicit actions.
	if original != nil {
		return original
	}
	return fault(f.ResourceBusy)
}
func (s *projectCallState) projectLeafLocked(e *projectCallEntry) *projectCallEntry {
	for _, child := range e.children {
		if s.owners[child.record.ID] == child {
			if next := s.projectLeafLocked(child); next != nil {
				return next
			}
			return nil
		}
	}
	if e.returned && !e.settling {
		return e
	}
	return nil
}

// This mutex protects the entire public return, not just an owner's inner
// method. A normal Resolve cannot remove its snapshot before capture finishes.
func (s *projectCallState) projectKeyBusyLocked(p i.ProjectID, lane projectCallLane, key f.IdempotencyKey, except *projectCallEntry) bool {
	for _, e := range s.owners {
		if e != except && e.record.Project == p && e.record.Lane == lane && e.identity.Key() == key && (!e.returned || e.settling) {
			return true
		}
	}
	return false
}

// Only the closed original recovery entry points call this hook, after their
// original operation actually returned successfully. The manager supplies its
// exact prior observation; a Stop inspection can never mint this witness.
func (s *projectCallState) recoveredProjectCall(e *projectCallEntry, prior error) {
	if s == nil || e == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.owners[e.record.ID] != e || e.bodyReturned || e.returned || e.settling || e.outcomeErr == nil {
		return
	}
	switch e.activeMethod {
	case "managed_project_run":
		if prior == nil || !sameProjectOutcomeError(prior, e.outcomeErr) {
			return
		}
	case "resolve_claim", "resolve_relaunch", "lookup_launch", "lookup_busy_compensation", "lookup_launch_failure":
		if prior != nil {
			return
		}
		prior = e.outcomeErr
	default:
		return
	}
	// Retire only this exact old outcome before a new traversal may begin.
	// Technical Unknown is separate and can only use its read-only confirmation.
	var clearOutcome func(*projectCallEntry)
	clearOutcome = func(current *projectCallEntry) {
		if current != e && (!current.returned || current.settling) {
			return
		}
		if current.outcomeErr != nil && sameProjectOutcomeError(prior, current.outcomeErr) {
			current.outcomeErr = nil
			current.outcome = nil
		}
		for _, child := range current.children {
			clearOutcome(child)
		}
	}
	clearOutcome(e)
	s.signalLocked()
}
func sameProjectOutcomeError(original, target error) bool {
	if original == nil || target == nil {
		return false
	}
	if errors.Is(original, target) {
		return true
	}
	a, ok := UnknownAttempt(original)
	b, other := UnknownAttempt(target)
	return ok && other && a.AttemptID().Validate() == nil && a.AttemptID() == b.AttemptID()
}

// Publish the exact error before the child can return or be confirmed. Its
// parent must not later infer that a cleared mutable child never had Unknown.
func (s *projectCallState) rememberParentErrorLocked(e *projectCallEntry, original error) {
	if e.parent == nil || !uncertainProjectVisit(original) {
		return
	}
	for _, prior := range e.parent.childOutcomes {
		if sameProjectOutcomeError(prior, original) {
			return
		}
	}
	e.parent.childOutcomes = append(e.parent.childOutcomes, original)
}
