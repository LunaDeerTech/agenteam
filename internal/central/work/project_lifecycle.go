package work

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
const projectStopBatch = 100

type projectStopCall struct {
	actor   i.Actor
	cause   pc.LifecycleCause
	scope   pc.ScopeRef
	active  bool
	unknown *f.CommitResult
	before  []projectCallRecord
}

// RequestProjectStop cancels only this project's selected original calls.
// Archive selects mutations; Delete also selects reads. Persisted closed rows
// still require the original local instance or real foreign-process death.
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
	if ctx == nil || actor.Validate() != nil || cause.Validate() != nil || pc.RequireProjectScope(scope) != nil || cause.Action != pc.Archive && cause.Action != pc.Delete {
		return pc.StopReport{}, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return pc.StopReport{}, err
	}
	s := p.state
	s.mu.Lock()
	// Cleanup must exclude a new Stop's original Store tail as well as
	// business calls. Both controls register under this same project lock.
	if s.cleanups[scope.ProjectID] != nil {
		s.mu.Unlock()
		return pc.StopReport{}, fault(f.ResourceBusy)
	}
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
		stop = &projectStopCall{actor: actor, cause: cause, scope: scope}
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
			s.cancelProjectCalls(scope.ProjectID, cause.Action == pc.Delete)
		}
		if stop.unknown != nil {
			return s.confirmProjectStop(run, stop)
		}
		pending, next := s.projectCallsPending(scope.ProjectID, cause.Action == pc.Delete)
		if pending {
			if next != nil {
				_, err := s.settle(run, next, stop, !request)
				return err
			}
			return nil
		}
		stopped, err := s.convergeProjectStop(run, stop, request)
		if err != nil {
			return err
		}
		if stopped {
			details = pc.StopDetails{State: pc.Stopped}
		}
		return nil
	})
	if err != nil {
		// A failed later authorization/confirmation cannot replace the first
		// ambiguous technical attempt retained by this exact stop call.
		if stop.unknown != nil {
			return pc.StopReport{}, txError(*stop.unknown)
		}
		return pc.StopReport{}, err
	}
	return pc.NewStopReport(pc.WorkParticipant, cause, scope, details)
}

func (s *projectCallOwnershipState) cancelProjectCalls(project i.ProjectID, reads bool) {
	s.mu.Lock()
	var cancels []context.CancelFunc
	for _, o := range s.owners {
		if o.record.Project != project || !reads && o.record.Class != projectCallMutation {
			continue
		}
		// Before the original P-locked Read gate returns, an Archive cannot
		// know whether this entry is an allowed historical replay. It remains
		// Pending until classified, without cancelling that possible read.
		if !reads && !o.classified {
			continue
		}
		o.stopping = true
		cancels = append(cancels, o.entry.cancel)
		for token := range o.confirmations {
			cancels = append(cancels, token.cancel)
		}
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		if cancel != nil {
			cancel()
		}
	}
}

func (s *projectCallOwnershipState) projectCallsPending(project i.ProjectID, reads bool) (bool, *projectCallOwner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := false
	var next *projectCallOwner
	for _, o := range s.owners {
		if o.record.Project != project || !reads && o.record.Class != projectCallMutation {
			continue
		}
		pending = true
		if o.returned && !o.settling && (next == nil || o.record.ID.String() < next.record.ID.String()) {
			next = o
		}
	}
	return pending, next
}

func (s *projectCallOwnershipState) stopTx(ctx context.Context, stop *projectStopCall, cause f.TransactionCause, fn func(context.Context, postgres.SQLExecutor) error) f.CommitResult {
	return s.callTx(ctx, cause, projectCallRecord{Project: stop.scope.ProjectID}, stop, fn)
}

func (s *projectCallOwnershipState) convergeProjectStop(ctx context.Context, stop *projectStopCall, request bool) (bool, error) {
	reads := stop.cause.Action == pc.Delete
	cause, _ := f.NewRecoveryCause("work-project-stop", stop.cause.OperationID.String(), stop.scope.ProjectID.String())
	var before []projectCallRecord
	validated := false
	result := s.stopTx(ctx, stop, cause, func(ctx context.Context, x postgres.SQLExecutor) error {
		var err error
		before, err = listProjectCalls(ctx, x, stop.scope.ProjectID, reads)
		if err != nil {
			return err
		}
		validated = true
		return ctx.Err()
	})
	if err := txError(result); err != nil {
		return false, err
	}
	if !validated || ctx.Err() != nil {
		return false, unavailable(ctx.Err())
	}
	foreign := map[oc.ProcessID]struct{}{}
	for _, row := range before {
		if row.Process == s.process {
			if row.Instance != s.instance || row.Phase != "business_closed" {
				return false, nil
			}
		} else {
			foreign[row.Process] = struct{}{}
		}
	}
	// Proof calls occur only after the original read transaction actually
	// returns. The following transaction rechecks the complete identity set.
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
	remaining := false
	result = s.stopTx(ctx, stop, cause, func(ctx context.Context, x postgres.SQLExecutor) error {
		current, err := listProjectCalls(ctx, x, stop.scope.ProjectID, reads)
		if err != nil {
			return err
		}
		if !slices.Equal(before, current) {
			return fault(f.VersionConflict)
		}
		closed := 0
		for _, row := range current {
			if row.Phase == "business_closed" {
				continue
			}
			if _, ok := foreign[row.Process]; !ok {
				return fault(f.InvalidState)
			}
			if !request || closed == projectStopBatch {
				remaining = true
				continue
			}
			if err = closeProjectCall(ctx, x, row); err != nil {
				return err
			}
			closed++
		}
		validated = true
		return ctx.Err()
	})
	if result.State() == f.Unknown {
		s.mu.Lock()
		original := result
		stop.unknown = &original
		stop.before = append([]projectCallRecord(nil), before...)
		s.signalLocked()
		s.mu.Unlock()
		return false, txError(result)
	}
	if err := txError(result); err != nil {
		return false, err
	}
	if !validated || ctx.Err() != nil {
		return false, unavailable(ctx.Err())
	}
	pending, _ := s.projectCallsPending(stop.scope.ProjectID, reads)
	return !remaining && !pending, nil
}

// Confirmation consumes only the original exact identity set and permits only
// its monotonic running -> business_closed transition. It performs no writes;
// a subsequent explicit request performs any remaining closure.
func (s *projectCallOwnershipState) confirmProjectStop(ctx context.Context, stop *projectStopCall) error {
	original := *stop.unknown
	validated := false
	result := s.stopTx(ctx, stop, original.Cause(), func(ctx context.Context, x postgres.SQLExecutor) error {
		current, err := listProjectCalls(ctx, x, stop.scope.ProjectID, stop.cause.Action == pc.Delete)
		if err != nil {
			return err
		}
		if len(current) != len(stop.before) {
			return fault(f.InvalidState)
		}
		for n, row := range current {
			old := stop.before[n]
			if !old.sameOwner(row) || old.Phase == "business_closed" && row.Phase != "business_closed" {
				return fault(f.InvalidState)
			}
		}
		validated = true
		return ctx.Err()
	})
	if result.State() != f.Committed || !validated || ctx.Err() != nil {
		return txError(original)
	}
	s.mu.Lock()
	stop.unknown = nil
	stop.before = nil
	s.signalLocked()
	s.mu.Unlock()
	return nil
}
