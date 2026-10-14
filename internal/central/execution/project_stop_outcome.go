package execution

import (
	"context"
	"errors"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// These are the original owners, not reusable authorization DTOs. A returned
// preparation/model error cannot be retired by merely closing its call row.
type projectCallOutcome struct {
	preparation     *preparationCall
	direct          *directTextCall
	parentOnly      bool
	associationRead bool
	readOnly        bool
}

func uncertainProjectVisit(err error) bool {
	var problem *f.Fault
	return err != nil && (errors.As(err, &problem) && (problem.CommitState == f.Unknown || problem.Code == f.CommitUnknown))
}
func (s *projectCallState) captureProjectOutcome(e *projectCallEntry, original error) (*projectCallOutcome, error) {
	v := &projectCallOutcome{}
	switch e.record.Lane {
	case projectLaneService:
		v.readOnly = e.read != nil || e.identity.Command() == "lookup_launch" || e.identity.Command() == "lookup_cancel"
		if e.cancellation != nil {
			if originalResult, ok := UnknownAttempt(original); ok && originalResult.Cause().Details().Owner == "execution.cancel.lookup" {
				v.readOnly = true
			}
		}
	case projectLanePreparation:
		p := s.preparation.state
		p.mu.Lock()
		run := p.calls[e.execution]
		if run != nil && run.returned && run.unresolved != nil {
			v.preparation = run
			original = run.unresolved
		}
		p.mu.Unlock()
		if v.preparation != nil {
			return v, original
		}
	case projectLaneDirect:
		d := s.direct.state
		d.mu.Lock()
		run := d.calls[e.execution]
		if run != nil && run.returned && run.unresolved != nil {
			v.direct = run
			original = run.unresolved
		}
		d.mu.Unlock()
		if v.direct != nil {
			return v, original
		}
	case projectLaneAssociated:
		v.parentOnly = s.parentOutcomeKnown(e, original)
		if raw, ok := UnknownAttempt(original); ok {
			detail := raw.Cause().Details()
			v.associationRead = detail.Kind == f.RecoveryCause && detail.Owner == "execution.associated-read" && detail.RecoveryRunID == e.execution.String()
		}
	}
	if uncertainProjectVisit(original) || v.parentOnly && original != nil {
		return v, original
	}
	return nil, nil
}
func (s *projectCallState) parentOutcomeKnown(e *projectCallEntry, original error) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, saved := range e.childOutcomes {
		if sameProjectOutcomeError(original, saved) {
			return true
		}
	}
	return false
}
func normalizeProjectCallLocks(p i.ProjectID, extra []f.LockRequest) ([]f.LockRequest, error) {
	return oc.NormalizeLocks(append(projectCallLocks(p), extra...))
}

func (s *projectCallState) reserveProjectOutcome(v *projectCallOutcome) (func(bool), bool) {
	if v == nil {
		return nil, false
	}
	if run := v.preparation; run != nil {
		p := s.preparation.state
		p.mu.Lock()
		current := p.calls[run.request.ExecutionID]
		if current != nil && (current != run || !run.returned || run.resolving) {
			p.mu.Unlock()
			return nil, false
		}
		run.resolving = true
		p.mu.Unlock()
		return func(success bool) {
			p.mu.Lock()
			if success && p.calls[run.request.ExecutionID] == run {
				run.unresolved = nil
				delete(p.calls, run.request.ExecutionID)
				if run.claim != nil {
					delete(p.returned, run.request.ExecutionID)
				}
			}
			run.resolving = false
			close(p.changed)
			p.changed = make(chan struct{})
			p.mu.Unlock()
			if success {
				run.cancel()
			}
		}, true
	}
	if run := v.direct; run != nil {
		d := s.direct.state
		d.mu.Lock()
		current := d.calls[run.request.ExecutionID]
		if current != nil && (current != run || !run.returned || run.resolving) {
			d.mu.Unlock()
			return nil, false
		}
		run.resolving = true
		d.mu.Unlock()
		return func(success bool) {
			d.mu.Lock()
			if success && d.calls[run.request.ExecutionID] == run {
				run.unresolved = nil
				run.uncertainty = ""
				delete(d.calls, run.request.ExecutionID)
			}
			run.resolving = false
			close(d.changed)
			d.changed = make(chan struct{})
			d.mu.Unlock()
			if success {
				run.cancel()
			}
		}, true
	}
	return func(bool) {}, true
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
	cause, _ := f.NewRecoveryCause("execution-call-outcome", e.record.ID.String(), e.record.Project.String())
	if raw, ok := UnknownAttempt(original); ok {
		cause = raw.Cause()
	}
	var extra []f.LockRequest
	switch {
	case e.cancellation != nil:
		command, err := e.cancellation.Identity()
		if err != nil {
			return original
		}
		extra = append(scopeLocks(e.actor, e.record.Project, e.launchKey.AgentID, f.Shared), commandLock(command), executionLock(e.execution))
	case e.read != nil:
		// The original business transaction was read-only. Its uncertain ACK
		// cannot have changed Execution or Work; only this exact technical
		// owner is confirmed, without authorizing a fresh public read.
		if !v.readOnly || s.reader == nil {
			return original
		}
	case e.record.Lane == projectLaneService:
		command, err := e.launchKey.Command()
		if err != nil {
			return original
		}
		extra = append(scopeLocks(e.actor, e.record.Project, e.launchKey.AgentID, f.Shared), commandLock(command))
	case v.preparation != nil:
		var err error
		extra, err = preparationLocks(v.preparation.request)
		if err != nil {
			return original
		}
	case v.direct != nil:
		extra = append(extra, v.direct.locks...)
	case v.associationRead:
		if e.job == nil {
			return original
		}
		key := e.job.dispatch
		extra = append(dispatchObservationLocks(e.record.Project, key.AgentID), executionLock(e.execution))
	case !v.parentOnly:
		return original
	}
	found := false
	result := s.callTx(ctx, cause, e.record, stop, extra, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		technical, err := loadProjectCall(ctx, x, e.record.ID)
		if err != nil {
			return err
		}
		if technical == nil || !e.record.sameOwner(*technical) || technical.Phase != "running" {
			return fault(f.ConfirmationStale)
		}
		switch {
		case e.cancellation != nil:
			receipt, readErr := loadCancellation(ctx, x, *e.cancellation, e.actor.Details().UserID, e.launchDigest)
			if readErr != nil {
				return readErr
			}
			found = receipt != nil || v.readOnly
		case e.read != nil:
			found = v.readOnly && s.reader != nil
		case e.record.Lane == projectLaneService:
			row, err := loadLaunch(ctx, x, e.launchKey)
			if err != nil {
				return err
			}
			if row == nil {
				found = v.readOnly
				return nil
			}
			found = row.digest == e.launchDigest && row.launch.ProjectID == e.record.Project && row.launch.AgentID == e.launchKey.AgentID && row.launch.Meta.IdempotencyKey == e.launchKey.IdempotencyKey
		case v.preparation != nil:
			found, err = s.confirmPreparationInTx(ctx, tx, x, v.preparation)
			if err != nil {
				return err
			}
		case v.direct != nil:
			found, err = s.confirmDirectInTx(ctx, tx, x, v.direct)
			if err != nil {
				return err
			}
		case v.parentOnly:
			found = true
		case v.associationRead:
			row, err := loadExecution(ctx, x, e.execution)
			if err != nil {
				return err
			}
			d := e.job.dispatch
			found = row != nil && row.launch.ProjectID == e.record.Project && row.launch.AgentID == d.AgentID && row.launch.Meta.IdempotencyKey == d.Key && row.digest == d.Digest && row.launch.Lineage.DispatchID == d.DispatchID
		}
		return ctx.Err()
	})
	if result.State() != f.Committed || !found || ctx.Err() != nil {
		return original
	}
	success = true
	s.mu.Lock()
	e.outcomeErr = nil
	e.outcome = nil
	s.signalLocked()
	s.mu.Unlock()
	return nil
}

// A terminal exact attempt is a positive fact. Missing or still-running
// preparation and unconfirmed Model capture retain their original owner.
func (s *projectCallState) confirmPreparationInTx(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, run *preparationCall) (bool, error) {
	if run.claim == nil || run.modelUnknown != nil {
		return false, nil
	}
	claim := *run.claim
	actual, err := loadPreparationAttempt(ctx, x, claim)
	if err != nil {
		return false, err
	}
	if actual == nil || actual.phase != "terminal" {
		return false, nil
	}
	expected := claim
	expected.phase = actual.phase
	if *actual != expected {
		return false, fault(f.ConfirmationStale)
	}
	if run.input != nil {
		input, err := loadPreparationInput(ctx, x, run.request.ExecutionID)
		if err != nil {
			return false, err
		}
		if !samePreparationInput(input, run.input) {
			return false, fault(f.ConfirmationStale)
		}
		if err = s.preparation.state.verifyMountMaterial(ctx, tx, input); err != nil {
			return false, err
		}
	}
	return true, ctx.Err()
}

// Stop observes only an already committed terminal candidate and the original
// resources' read-only retirement ports. It never calls Result, Start, Advance
// or a normal Resolve that could write a new terminal transition.
func (s *projectCallState) confirmDirectInTx(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, run *directTextCall) (bool, error) {
	if run.uncertainty != "terminal" || run.terminal == nil || !run.allJoined || !joinedLoopCheckpoint(run) || run.input == nil {
		return false, nil
	}
	row, err := loadExecution(ctx, x, run.request.ExecutionID)
	if err != nil {
		return false, err
	}
	stored, err := loadDirectText(ctx, x, run.request.ExecutionID)
	if err != nil {
		return false, err
	}
	if !sameDirectText(stored, run.snapshot, run.round) || !directTextTerminalMatches(row, stored, run.terminal) {
		return false, nil
	}
	d := s.direct.state
	d.mu.Lock()
	prior := run.phase
	run.phase = "terminal"
	run.tx = tx
	d.mu.Unlock()
	defer func() { d.mu.Lock(); run.tx = f.Tx{}; run.phase = prior; d.mu.Unlock() }()
	ctx = context.WithValue(ctx, directTextContextKey{}, run)
	model, err := d.deps.Models.ExecutionModelRetiredInTx(ctx, tx, run.invocation.modelRetirement, run.invocation.modelPlan)
	if err != nil {
		return false, portError(err)
	}
	environment, err := d.deps.Environment.EnvironmentRetiredInTx(ctx, tx, run.input.input.Fields().Environment, run.invocation.environmentPlan)
	if err != nil {
		return false, portError(err)
	}
	if !model || !environment {
		return false, nil
	}
	if err = d.verifyDirectTextMountUse(ctx, tx, run, row, stored, run.input); err != nil {
		return false, err
	}
	return true, ctx.Err()
}

func (s *projectCallState) resumePrevious(ctx context.Context, p i.ProjectID, lane projectCallLane, identity f.CommandIdentity, recovery bool, template *projectCallEntry) error {
	s.mu.Lock()
	if s.projectKeyBusyLocked(p, lane, identity, nil) {
		s.mu.Unlock()
		return fault(f.ResourceBusy)
	}
	var prior *projectCallEntry
	for _, e := range s.owners {
		if e.record.Project == p && e.record.Lane == lane && sameProjectInvocationKey(e.identity, identity) && e.returned {
			prior = e
			break
		}
	}
	if prior != nil && lane == projectLaneService && (template == nil || prior.launchKey != template.launchKey || prior.launchDigest != template.launchDigest || !prior.actor.Equal(template.actor)) {
		s.mu.Unlock()
		return fault(f.IdempotencyKeyReused)
	}
	if prior == nil || recovery && prior.registrationKnown && prior.unknown == nil && prior.outcomeErr != nil {
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
	if _, err := s.settle(ctx, next, nil, false); err != nil {
		return err
	}
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
func (s *projectCallState) projectKeyBusyLocked(p i.ProjectID, lane projectCallLane, identity f.CommandIdentity, except *projectCallEntry) bool {
	for _, e := range s.owners {
		if e != except && e.record.Project == p && e.record.Lane == lane && sameProjectInvocationKey(e.identity, identity) && (!e.returned || e.settling) {
			return true
		}
	}
	return false
}
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
	case "advance":
		if prior == nil || !sameProjectOutcomeError(prior, e.outcomeErr) {
			return
		}
	case "lookup_launch", "lookup_cancel", "resolve_preparation", "resolve_direct":
		if prior != nil {
			return
		}
		prior = e.outcomeErr
	default:
		return
	}
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
func (s *projectCallState) rememberParentErrorLocked(e *projectCallEntry, original error) {
	if e.parent == nil || original == nil || !uncertainProjectVisit(original) && e.outcomeErr == nil {
		return
	}
	for _, saved := range e.parent.childOutcomes {
		if sameProjectOutcomeError(saved, original) {
			return
		}
	}
	e.parent.childOutcomes = append(e.parent.childOutcomes, original)
}

func normalizeProjectRegistrationLocks(locks []f.LockRequest) ([]f.LockRequest, error) {
	return oc.NormalizeLocks(locks)
}

// This witness refers to original child outcomes which actually returned and
// were retired by their own normal recovery or exact read-only confirmation.
// An empty owner map by itself cannot establish this permission.
func (s *projectCallState) parentRecovered(e *projectCallEntry) bool {
	if s == nil || e == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.owners[e.record.ID] != e || e.bodyReturned || e.outcomeErr == nil || e.outcome == nil || !e.outcome.parentOnly {
		return false
	}
	for _, child := range e.children {
		if !child.returned || child.settling || child.outcomeErr != nil || child.unknown != nil {
			return false
		}
	}
	return true
}

func sameProjectInvocationKey(a, b f.CommandIdentity) bool {
	return a.Validate() == nil && b.Validate() == nil && a.Key() == b.Key() && a.Namespace() == b.Namespace() && slices.Equal(a.OwnerIDs(), b.OwnerIDs())
}
