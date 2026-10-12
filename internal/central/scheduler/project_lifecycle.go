package scheduler

import (
	"context"
	"errors"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

const maxProjectStopControls = 4

type ownedProjectStop struct {
	actor    i.Actor
	cause    pc.LifecycleCause
	scope    pc.ScopeRef
	active   bool
	unknown  *f.CommitResult
	before   []projectCallRecord
	observed bool
}

func (p *ProjectCallOwnership) RequestProjectStop(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (pc.StopReport, error) {
	return p.projectStop(ctx, actor, cause, scope, true)
}
func (p *ProjectCallOwnership) InspectProjectStop(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (pc.StopReport, error) {
	return p.projectStop(ctx, actor, cause, scope, false)
}
func (p *ProjectCallOwnership) projectStop(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, request bool) (pc.StopReport, error) {
	if p == nil || p.state == nil {
		return pc.StopReport{}, fault(f.DependencyUnbound)
	}
	if ctx == nil || actor.Validate() != nil || cause.Validate() != nil || cause.Action != pc.Delete || pc.RequireProjectScope(scope) != nil {
		return pc.StopReport{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return pc.StopReport{}, err
	}
	s := p.state
	s.mu.Lock()
	stop := s.stops[scope.ProjectID]
	if stop != nil && (stop.active || !stop.actor.Equal(actor) || stop.cause != cause || !stop.scope.Equal(scope)) {
		s.mu.Unlock()
		return pc.StopReport{}, fault(f.ResourceBusy)
	}
	if stop == nil {
		if s.stopped || len(s.stops) >= maxProjectStopControls {
			s.mu.Unlock()
			return pc.StopReport{}, fault(f.ResourceBusy)
		}
		stop = &ownedProjectStop{actor: actor, cause: cause, scope: scope}
		s.stops[scope.ProjectID] = stop
	}
	stop.active = true
	s.signalLocked()
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		stop.active = false
		if stop.unknown == nil && s.stops[scope.ProjectID] == stop {
			delete(s.stops, scope.ProjectID)
		}
		s.signalLocked()
		s.mu.Unlock()
	}()
	details := pc.StopDetails{State: pc.StopPending, SafeReason: pc.ReasonWorkPending}
	err := s.authority.WithProjectStop(ctx, actor, cause, scope, func(run context.Context) error {
		if request {
			s.cancelProjectCalls(scope.ProjectID)
		}
		if stop.unknown != nil {
			return s.confirmProjectStop(run, stop)
		}
		pending, next := s.projectCallsPending(scope.ProjectID)
		if pending {
			if next != nil {
				_, err := s.settle(run, next, stop, !request)
				return err
			}
			return nil
		}
		stopped, err := s.observeProjectStopped(run, stop)
		if err == nil && stopped {
			details = pc.StopDetails{State: pc.Stopped}
		}
		return err
	})
	if err != nil {
		if stop.unknown != nil {
			return pc.StopReport{}, commitError(*stop.unknown)
		}
		return pc.StopReport{}, err
	}
	return pc.NewStopReport(pc.SchedulerParticipant, cause, scope, details)
}
func (s *projectCallState) cancelProjectCalls(project i.ProjectID) {
	s.mu.Lock()
	var cancels []context.CancelFunc
	for _, e := range s.owners {
		if e.record.Project == project {
			cancels = append(cancels, e.cancel)
		}
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}
func (s *projectCallState) projectCallsPending(project i.ProjectID) (bool, *projectCallEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := false
	var next *projectCallEntry
	for _, e := range s.owners {
		if e.record.Project != project {
			continue
		}
		pending = true
		leaf := s.projectLeafLocked(e)
		if leaf != nil && (next == nil || leaf.record.ID.String() < next.record.ID.String()) {
			next = leaf
		}
	}
	return pending, next
}
func (s *projectCallState) stopTx(ctx context.Context, stop *ownedProjectStop, cause f.TransactionCause, fn func(context.Context, postgres.SQLExecutor) error) f.CommitResult {
	return s.callTx(ctx, cause, projectCallRecord{Project: stop.scope.ProjectID}, stop, nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error { return fn(ctx, x) })
}
func (s *projectCallState) retainStopRead(stop *ownedProjectStop, result f.CommitResult, rows []projectCallRecord, observed bool) {
	s.mu.Lock()
	saved := result
	stop.unknown = &saved
	stop.before = append([]projectCallRecord(nil), rows...)
	stop.observed = observed
	s.signalLocked()
	s.mu.Unlock()
}

// A closed foreign row also needs real process death. A running foreign row
// has no original local outcome snapshot, so this slice cannot retire it by
// treating process death or missing Dispatch data as a negative business proof.
func (s *projectCallState) observeProjectStopped(ctx context.Context, stop *ownedProjectStop) (bool, error) {
	cause, _ := f.NewRecoveryCause("scheduler-project-stop", stop.cause.OperationID.String(), stop.scope.ProjectID.String())
	var before []projectCallRecord
	validated := false
	result := s.stopTx(ctx, stop, cause, func(ctx context.Context, x postgres.SQLExecutor) error {
		var err error
		before, err = listProjectCalls(ctx, x, stop.scope.ProjectID)
		if err != nil {
			return err
		}
		validated = true
		return ctx.Err()
	})
	if result.State() == f.Unknown {
		s.retainStopRead(stop, result, before, validated)
		return false, commitError(result)
	}
	if err := commitError(result); err != nil {
		return false, err
	}
	if !validated || ctx.Err() != nil {
		return false, unavailable(ctx.Err())
	}
	foreign := map[oc.ProcessID]struct{}{}
	for _, row := range before {
		if row.Phase != "business_closed" {
			return false, nil
		}
		if row.Process == s.process {
			if row.Instance != s.instance {
				return false, nil
			}
		} else {
			foreign[row.Process] = struct{}{}
		}
	}
	for process := range foreign {
		if err := s.proof.ConfirmStopped(ctx, process); err != nil {
			var problem *f.Fault
			if errors.As(err, &problem) && (problem.Code == f.ResourceBusy || problem.Code == f.NotFound) {
				return false, nil
			}
			return false, portError(err)
		}
	}
	validated = false
	result = s.stopTx(ctx, stop, cause, func(ctx context.Context, x postgres.SQLExecutor) error {
		current, err := listProjectCalls(ctx, x, stop.scope.ProjectID)
		if err != nil {
			return err
		}
		if !slices.Equal(before, current) {
			return fault(f.VersionConflict)
		}
		validated = true
		return ctx.Err()
	})
	if result.State() == f.Unknown {
		s.retainStopRead(stop, result, before, true)
		return false, commitError(result)
	}
	if err := commitError(result); err != nil {
		return false, err
	}
	if !validated || ctx.Err() != nil {
		return false, unavailable(ctx.Err())
	}
	pending, _ := s.projectCallsPending(stop.scope.ProjectID)
	return !pending, nil
}
func (s *projectCallState) confirmProjectStop(ctx context.Context, stop *ownedProjectStop) error {
	original := *stop.unknown
	validated := false
	result := s.stopTx(ctx, stop, original.Cause(), func(ctx context.Context, x postgres.SQLExecutor) error {
		current, err := listProjectCalls(ctx, x, stop.scope.ProjectID)
		if err != nil {
			return err
		}
		if stop.observed && !slices.Equal(stop.before, current) {
			return fault(f.ConfirmationStale)
		}
		validated = true
		return ctx.Err()
	})
	if result.State() != f.Committed || !validated || ctx.Err() != nil {
		return commitError(original)
	}
	s.mu.Lock()
	stop.unknown = nil
	stop.before = nil
	stop.observed = false
	s.signalLocked()
	s.mu.Unlock()
	return nil
}
