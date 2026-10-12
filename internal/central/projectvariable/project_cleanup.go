package projectvariable

import (
	"context"
	"errors"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// ProjectCleanup retires this pair's Project metadata. It does not retire D04,
// immutable Execution environments, project_calls, or the whole participant.
// Both original service owners retain every synchronous cleanup call, including
// an already committed transaction whose original Store return is still held.
type ProjectCleanup struct {
	pair      *projectCallOwnershipState
	authority c.ProjectCleanupAuthority
	owners    *projectCleanupOwnership
}

type projectCleanupOwnership struct {
	mu    sync.Mutex
	calls map[i.ProjectID]*projectCleanupCall
}
type projectCleanupCall struct {
	actor    i.Actor
	cause    pc.LifecycleCause
	active   bool
	unknown  f.CommitResult
	returned func()
}

func NewProjectCleanup(pair *ProjectCallOwnership, authority c.ProjectCleanupAuthority) (*ProjectCleanup, error) {
	if pair == nil || pair.state == nil || nilPort(authority) {
		return nil, fault(f.DependencyUnbound)
	}
	s := pair.state
	a, b := s.service.state(), s.secrets.state()
	if a == nil || b == nil || a.ownership != s || b.ownership != s || !sameStore(s.store, a.store) || !sameStore(s.store, b.store) {
		return nil, fault(f.DependencyUnbound)
	}
	s.mu.Lock()
	if s.cleanup == nil {
		s.cleanup = &projectCleanupOwnership{calls: make(map[i.ProjectID]*projectCleanupCall)}
	}
	owners := s.cleanup
	s.mu.Unlock()
	return &ProjectCleanup{pair: s, authority: authority, owners: owners}, nil
}

// Progress is deliberately remaining-first: there is no cursor that could skip
// an original row after an Unknown outcome. Only the Project driver can issue
// another call, with its still-current private claim checked in every Tx.
func (s *ProjectCleanup) Cleanup(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, checkpoint *pc.CleanupCheckpoint) (pc.CleanupReport, error) {
	var zero pc.CleanupReport
	if err := projectStopArguments(ctx, actor, cause, scope); err != nil {
		return zero, err
	}
	if cause.Action != pc.Delete || checkpoint != nil {
		return zero, fault(f.InvalidArgument)
	}
	if s == nil || s.pair == nil || s.owners == nil || nilPort(s.authority) {
		return zero, fault(f.DependencyUnbound)
	}
	owner, err := s.borrow(actor, cause, scope.ProjectID)
	if err != nil {
		return zero, err
	}
	defer s.release(scope.ProjectID, owner)
	run, _, ordinaryDone, err := s.pair.service.begin(ctx)
	if err != nil {
		return zero, err
	}
	run, _, secretDone, err := s.pair.secrets.begin(run)
	if err != nil {
		ordinaryDone()
		return zero, err
	}
	returned := func() { secretDone(); ordinaryDone() }
	retained := false
	defer func() {
		if !retained {
			returned()
		}
	}()
	transaction, err := f.NewRecoveryCause("projectvariable-project-cleanup", cause.OperationID.String(), scope.ProjectID.String())
	if err != nil {
		return zero, portError(err)
	}
	validated, completed, confirmed := false, false, false
	result := s.pair.store.WithinTx(run, transaction, func(ctx context.Context, tx f.Tx) error {
		if err := s.pair.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(scope.ProjectID, f.Exclusive)}); err != nil {
			return portError(err)
		}
		x, err := s.pair.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if nilPort(x) {
			return fault(f.DependencyUnbound)
		}
		if err = s.authority.RequireProjectVariablesCleanupInTx(ctx, tx, actor, cause, scope.ProjectID); err != nil {
			return portError(err)
		}
		if err = ctx.Err(); err != nil {
			return canceled(err)
		}
		if s.pair.cleanupPending(scope.ProjectID) {
			validated = true
			return nil
		}
		var running bool
		if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_projectvariable.project_calls WHERE project_id=$1 AND phase='running')`, scope.ProjectID.String()).Scan(&running); err != nil {
			return unavailable(err)
		}
		if err = ctx.Err(); err != nil {
			return canceled(err)
		}
		if !running {
			if err = requireProjectCleanupUnreferenced(ctx, x, scope.ProjectID); err != nil {
				return err
			}
			if owner.returned != nil {
				// Resolving a retained original never starts another deletion. This
				// observes remaining facts, not the physical attempt's commit bit.
				completed, err = projectCleanupEmpty(ctx, x, scope.ProjectID)
				confirmed = err == nil
			} else {
				completed, err = cleanupProjectMetadata(ctx, x, scope.ProjectID)
			}
			if err != nil {
				return err
			}
		}
		validated = true
		return nil
	})
	if owner.returned != nil {
		if result.State() != f.Committed || !validated || !confirmed {
			return zero, errors.Join(txError(owner.unknown), txError(result))
		}
		// Original ownership survives until this authorized, read-only
		// confirmation's Store return. Cancellation does not erase Unknown.
		if err = run.Err(); err != nil {
			return zero, errors.Join(txError(owner.unknown), canceled(err))
		}
		owner.returned()
		owner.returned = nil
	} else if result.State() == f.Unknown {
		owner.unknown, owner.returned = result, returned
		retained = true
		return zero, txError(result)
	}
	if err = txError(result); err != nil {
		return zero, err
	}
	if !validated {
		return zero, unavailable(nil)
	}
	if err = run.Err(); err != nil {
		return zero, canceled(err)
	}
	// Late admissions cannot pass the current deleting Project gate. Until their
	// original returns are observed, however, this call reports only Pending.
	if s.pair.cleanupPending(scope.ProjectID) {
		completed = false
	}
	details := pc.CleanupDetails{State: pc.CleanupPending, SafeReason: pc.ReasonWorkPending}
	if completed {
		details.State = pc.CleanupCompleted
		details.SafeReason = ""
	}
	return pc.NewCleanupReport(pc.SkillsParticipant, cause, scope, details)
}

// One original owner per Project is shared by all cleaners of the same pair.
// Retained Unknown controls remain in both original lane maps until confirmed;
// a second constructor or a different cause cannot bypass them.
func (s *ProjectCleanup) borrow(actor i.Actor, cause pc.LifecycleCause, project i.ProjectID) (*projectCleanupCall, error) {
	s.owners.mu.Lock()
	defer s.owners.mu.Unlock()
	if call := s.owners.calls[project]; call != nil {
		if call.active {
			return nil, fault(f.ResourceBusy)
		}
		if !call.actor.Equal(actor) || call.cause != cause {
			return nil, fault(f.InvalidState)
		}
		call.active = true
		return call, nil
	}
	if len(s.owners.calls) >= maxProjectCallOwners {
		return nil, fault(f.ResourceBusy)
	}
	call := &projectCleanupCall{actor: actor, cause: cause, active: true}
	s.owners.calls[project] = call
	return call, nil
}
func (s *ProjectCleanup) release(project i.ProjectID, call *projectCleanupCall) {
	s.owners.mu.Lock()
	defer s.owners.mu.Unlock()
	call.active = false
	if call.returned == nil {
		delete(s.owners.calls, project)
	}
}

// The lock order is the pair's existing ordinary -> Secret -> ownership order.
// No mutex is held across SQL. Control calls have no Project identity and do
// not wait for themselves; all business classes and retained originals count.
func (s *projectCallOwnershipState) cleanupPending(project i.ProjectID) bool {
	a, b := s.service.state(), s.secrets.state()
	a.mu.Lock()
	b.mu.Lock()
	s.mu.Lock()
	defer a.mu.Unlock()
	defer b.mu.Unlock()
	defer s.mu.Unlock()
	for _, calls := range []map[*call]struct{}{a.calls, b.calls} {
		for call := range calls {
			if call.project == project && (call.kind == readCall || call.kind == mutationCall) {
				return true
			}
		}
	}
	for _, owner := range s.owners {
		if owner.record.Project == project {
			return true
		}
	}
	return false
}

var _ c.ProjectCleanupDomain = (*ProjectCleanup)(nil)
