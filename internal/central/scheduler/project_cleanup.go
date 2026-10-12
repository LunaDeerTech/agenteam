package scheduler

import (
	"bytes"
	"context"
	"errors"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/scheduler/contract"
)

const maxProjectCleanupControls = 4

type projectCleanupID struct{}
type projectCleanupParticipant struct {
	pair      *ProjectCallOwnership
	authority sc.ProjectCleanupAuthority
}

// A cleanup control belongs to the original pair, including when a caller
// constructs another adapter. Neither a checkpoint nor a closed SQL row can
// replace that pair's actual returned calls.
type ownedProjectCleanup struct {
	id         f.ID[projectCleanupID]
	actor      i.Actor
	cause      pc.LifecycleCause
	scope      pc.ScopeRef
	authority  sc.ProjectCleanupAuthority
	checkpoint []byte
	active     bool
	unknown    *f.CommitResult
	firstError error
	before     []projectCallRecord
	observed   bool
	batch      *projectCleanupBatch
}

// NewProjectCleanupParticipant binds the original seven-lane owner. Every
// transaction still consumes the Project driver's private current cleanup
// round through authority; construction and checkpoint values grant nothing.
func NewProjectCleanupParticipant(original *ProjectCallOwnership, authority sc.ProjectCleanupAuthority) (pc.ProjectLifecycleParticipant, error) {
	if original == nil || original.state == nil || nilPort(authority) {
		return nil, fault(f.DependencyUnbound)
	}
	return &projectCleanupParticipant{original, authority}, nil
}
func (*projectCleanupParticipant) Name() pc.ParticipantName { return pc.SchedulerParticipant }
func (p *projectCleanupParticipant) RequestStop(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (pc.StopReport, error) {
	return p.pair.RequestProjectStop(ctx, actor, cause, scope)
}
func (p *projectCleanupParticipant) InspectStop(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (pc.StopReport, error) {
	return p.pair.InspectProjectStop(ctx, actor, cause, scope)
}

func (p *projectCleanupParticipant) Cleanup(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, cp *pc.CleanupCheckpoint) (pc.CleanupReport, error) {
	if p == nil || p.pair == nil || p.pair.state == nil || nilPort(p.authority) {
		return pc.CleanupReport{}, fault(f.DependencyUnbound)
	}
	if ctx == nil || actor.Validate() != nil || cause.Validate() != nil || cause.Action != pc.Delete || pc.RequireProjectScope(scope) != nil {
		return pc.CleanupReport{}, invalid()
	}
	d := actor.Details()
	if d.Kind != i.Service || d.ServiceName != i.ProjectLifecycle || d.ProjectID != scope.ProjectID.String() || d.CauseRef != cause.OperationID.String() {
		return pc.CleanupReport{}, fault(f.Forbidden)
	}
	progress, err := sc.DecodeProjectCleanupCheckpoint(cp, cause, scope)
	if err != nil {
		return pc.CleanupReport{}, err
	}
	s := p.pair.state
	call, err := s.borrowProjectCleanup(ctx, actor, cause, scope, cp, p.authority)
	if err != nil {
		return pc.CleanupReport{}, err
	}
	defer s.returnProjectCleanup(call)
	if call.unknown != nil {
		if err = s.confirmProjectCleanup(ctx, call); err != nil {
			return pc.CleanupReport{}, err
		}
		// Confirmation never performs the next batch, even if it observes that
		// the previous physical transaction removed the last row.
		return s.projectCleanupPending(call, progress)
	}
	if err = ctx.Err(); err != nil {
		return pc.CleanupReport{}, err
	}
	ready, err := s.prepareProjectCleanup(ctx, call, progress)
	if err != nil {
		return pc.CleanupReport{}, err
	}
	if !ready {
		return s.projectCleanupPending(call, progress)
	}
	stage := "runtimes"
	validated := false
	result := s.cleanupTx(ctx, call, s.projectCleanupCause(call), func(run context.Context, x postgres.SQLExecutor) error {
		current, err := listProjectCalls(run, x, scope.ProjectID)
		if err != nil {
			return err
		}
		if !slices.Equal(current, call.before) || !s.projectCleanupReturned(call) {
			return fault(f.ResourceBusy)
		}
		batch, err := readProjectCleanupBatch(run, x, scope.ProjectID)
		if err != nil {
			return err
		}
		call.batch = batch
		if batch == nil {
			stage = "completed"
		} else {
			stage = batch.stage
			if err = deleteProjectCleanupBatch(run, x, scope.ProjectID, batch); err != nil {
				return err
			}
			stage, err = nextProjectCleanupStage(run, x, scope.ProjectID)
			if err != nil {
				return err
			}
		}
		validated = true
		return run.Err()
	})
	if result.State() == f.Unknown {
		return pc.CleanupReport{}, s.retainProjectCleanup(call, result)
	}
	if err = commitError(result); err != nil {
		return pc.CleanupReport{}, err
	}
	if !validated || ctx.Err() != nil {
		return pc.CleanupReport{}, unavailable(ctx.Err())
	}
	return s.projectCleanupReport(call, stage)
}

func (s *projectCallState) borrowProjectCleanup(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, cp *pc.CleanupCheckpoint, authority sc.ProjectCleanupAuthority) (*ownedProjectCleanup, error) {
	var raw []byte
	if cp != nil {
		raw = cp.Bytes()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old := s.cleanups[scope.ProjectID]; old != nil {
		if old.active {
			return nil, fault(f.ResourceBusy)
		}
		if !old.actor.Equal(actor) || old.cause != cause || !old.scope.Equal(scope) || !bytes.Equal(old.checkpoint, raw) {
			return nil, fault(f.ConfirmationStale)
		}
		if err := ctx.Err(); err != nil {
			return nil, old.firstError
		}
		old.active = true
		s.signalLocked()
		return old, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.stopped || len(s.cleanups) >= maxProjectCleanupControls {
		return nil, fault(f.ResourceBusy)
	}
	id, err := f.NewID[projectCleanupID]()
	if err != nil {
		return nil, portError(err)
	}
	call := &ownedProjectCleanup{id: id, actor: actor, cause: cause, scope: scope, authority: authority, checkpoint: append([]byte(nil), raw...), active: true}
	if s.cleanups == nil {
		s.cleanups = map[i.ProjectID]*ownedProjectCleanup{}
	}
	s.cleanups[scope.ProjectID] = call
	s.signalLocked()
	return call, nil
}
func (s *projectCallState) returnProjectCleanup(call *ownedProjectCleanup) {
	s.mu.Lock()
	defer s.mu.Unlock()
	call.active = false
	if call.unknown == nil && s.cleanups[call.scope.ProjectID] == call {
		delete(s.cleanups, call.scope.ProjectID)
	}
	s.signalLocked()
}
func (s *projectCallState) retainProjectCleanup(call *ownedProjectCleanup, result f.CommitResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if call.unknown == nil {
		saved := result
		call.unknown = &saved
		call.firstError = commitError(result)
	}
	s.signalLocked()
	return call.firstError
}
func (s *projectCallState) projectCleanupCause(call *ownedProjectCleanup) f.TransactionCause {
	cause, _ := f.NewRecoveryCause("scheduler-project-cleanup", call.id.String(), call.scope.ProjectID.String())
	return cause
}
func (s *projectCallState) cleanupTx(ctx context.Context, call *ownedProjectCleanup, cause f.TransactionCause, fn func(context.Context, postgres.SQLExecutor) error) f.CommitResult {
	return s.store.WithinTx(ctx, cause, func(run context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(run, tx, projectCallLocks(call.scope.ProjectID)); err != nil {
			return portError(err)
		}
		if err := s.proof.RequireCurrentProcessInTx(run, tx, s.process); err != nil {
			return portError(err)
		}
		if err := call.authority.RequireProjectCleanupInTx(run, tx, call.actor, call.cause, call.scope); err != nil {
			return portError(err)
		}
		s.mu.Lock()
		valid := call.active && s.cleanups[call.scope.ProjectID] == call
		s.mu.Unlock()
		if !valid {
			return fault(f.Forbidden)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if nilPort(x) {
			return fault(f.DependencyUnbound)
		}
		return fn(run, x)
	})
}

// This excludes the current cleanup control, not another Stop, original
// business call, or retained Unknown. Project B never participates in this test.
func (s *projectCallState) projectCleanupReturned(call *ownedProjectCleanup) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !call.active || s.cleanups[call.scope.ProjectID] != call || s.stops[call.scope.ProjectID] != nil {
		return false
	}
	for _, e := range s.owners {
		if e.record.Project == call.scope.ProjectID {
			return false
		}
	}
	return true
}

func (s *projectCallState) prepareProjectCleanup(ctx context.Context, call *ownedProjectCleanup, progress sc.ProjectCleanupProgress) (bool, error) {
	ready := false
	result := s.cleanupTx(ctx, call, s.projectCleanupCause(call), func(run context.Context, x postgres.SQLExecutor) error {
		var err error
		call.before, err = listProjectCalls(run, x, call.scope.ProjectID)
		if err != nil {
			return err
		}
		call.observed = true
		ready = s.projectCleanupReturned(call)
		return run.Err()
	})
	if result.State() == f.Unknown {
		return false, s.retainProjectCleanup(call, result)
	}
	if err := commitError(result); err != nil {
		return false, err
	}
	if !call.observed || ctx.Err() != nil {
		return false, unavailable(ctx.Err())
	}
	if !ready {
		return false, nil
	}
	foreign := map[oc.ProcessID]struct{}{}
	for _, row := range call.before {
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
	if progress.ProcessID.Validate() == nil {
		if progress.ProcessID == s.process {
			if progress.InstanceID != s.instance.String() {
				return false, nil
			}
		} else {
			foreign[progress.ProcessID] = struct{}{}
		}
	}
	// Every proof is obtained after the preflight's original Store return.
	// The mutation re-reads the complete exact ledger while holding both locks.
	for process := range foreign {
		if err := s.proof.ConfirmStopped(ctx, process); err != nil {
			var problem *f.Fault
			if errors.As(err, &problem) && (problem.Code == f.ResourceBusy || problem.Code == f.NotFound) {
				return false, nil
			}
			return false, portError(err)
		}
	}
	return true, ctx.Err()
}

func (s *projectCallState) confirmProjectCleanup(ctx context.Context, call *ownedProjectCleanup) error {
	original := *call.unknown
	validated := false
	result := s.cleanupTx(ctx, call, original.Cause(), func(run context.Context, x postgres.SQLExecutor) error {
		current, err := listProjectCalls(run, x, call.scope.ProjectID)
		if err != nil {
			return err
		}
		if !s.projectCleanupReturned(call) {
			return fault(f.ResourceBusy)
		}
		if call.batch == nil {
			if call.observed && !slices.Equal(current, call.before) {
				return fault(f.ConfirmationStale)
			}
		} else {
			applied, err := confirmProjectCleanupBatch(run, x, call.scope.ProjectID, call.batch)
			if err != nil {
				return err
			}
			expected := call.before
			if applied && call.batch.stage == "calls" {
				expected = make([]projectCallRecord, 0, len(call.before))
				for _, row := range call.before {
					if !call.batch.contains(row.ID.String()) {
						expected = append(expected, row)
					}
				}
			}
			if !slices.Equal(current, expected) {
				return fault(f.ConfirmationStale)
			}
		}
		validated = true
		return run.Err()
	})
	if result.State() != f.Committed || !validated || ctx.Err() != nil {
		return call.firstError
	}
	s.mu.Lock()
	call.unknown = nil
	s.signalLocked()
	s.mu.Unlock()
	return nil
}
func (s *projectCallState) projectCleanupReport(call *ownedProjectCleanup, stage string) (pc.CleanupReport, error) {
	return makeProjectCleanupReport(call, sc.ProjectCleanupProgress{ProjectID: call.scope.ProjectID, OperationID: call.cause.OperationID, ProjectVersion: call.cause.ProjectVersion, ProcessID: s.process, InstanceID: s.instance.String(), Stage: stage})
}
func (s *projectCallState) projectCleanupPending(call *ownedProjectCleanup, prior sc.ProjectCleanupProgress) (pc.CleanupReport, error) {
	if prior.Validate() != nil {
		return s.projectCleanupReport(call, "runtimes")
	}
	// A conservative Pending observation must not replace an unproved old
	// process/instance with this pair, including after a read-only Unknown.
	prior.Stage = "runtimes"
	return makeProjectCleanupReport(call, prior)
}
func makeProjectCleanupReport(call *ownedProjectCleanup, progress sc.ProjectCleanupProgress) (pc.CleanupReport, error) {
	cp, err := sc.NewProjectCleanupCheckpoint(progress, call.cause, call.scope)
	if err != nil {
		return pc.CleanupReport{}, err
	}
	d := pc.CleanupDetails{State: pc.CleanupPending, Checkpoint: &cp, SafeReason: pc.ReasonWorkPending}
	if progress.Stage == "completed" {
		d.State, d.SafeReason = pc.CleanupCompleted, ""
	}
	return pc.NewCleanupReport(pc.SchedulerParticipant, call.cause, call.scope, d)
}

var _ pc.ProjectLifecycleParticipant = (*projectCleanupParticipant)(nil)
