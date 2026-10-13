package mount

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	obc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type projectLifecycleProof interface {
	RequireCurrentProcessInTx(context.Context, f.Tx, oc.ProcessID) error
	ConfirmStopped(context.Context, oc.ProcessID) error
}
type projectManagedCalls interface {
	RequestProjectStop(context.Context, i.Actor, pc.LifecycleCause, pc.ScopeRef) (pc.StopReport, error)
	InspectProjectStop(context.Context, i.Actor, pc.LifecycleCause, pc.ScopeRef) (pc.StopReport, error)
	RequireProjectCallsReturnedInTx(context.Context, f.Tx, i.Actor, pc.LifecycleCause, pc.ScopeRef) error
	ProjectControlJoined(i.ProjectID) bool
	Joined() bool
}
type projectManagedAgents interface {
	projectManagedCalls
	ReadRunnerManagedConfigurationInTx(context.Context, f.Tx, i.Actor, pc.LifecycleCause, pc.ScopeRef, i.AgentID) (ac.AgentConfig, error)
}
type projectLifecycleInstance struct{}
type projectLifecycleControlID struct{}

// This owner retains only Runner-managed lifecycle controls. Business calls
// remain on the exact original Agent/Execution pairs; no second invocation or
// independently reconstructed business service is created here.
type ProjectLifecycleOwnership struct {
	store      Store
	agents     projectManagedAgents
	executions projectManagedCalls
	proof      projectLifecycleProof
	process    oc.ProcessID
	instance   f.ID[projectLifecycleInstance]
	stop       mc.ProjectStopAuthority
	mu         sync.Mutex
	stopped    bool
	controls   map[i.ProjectID]*projectLifecycleControl
	changed    chan struct{}
}
type projectLifecycleControl struct {
	id          f.ID[projectLifecycleControlID]
	actor       i.Actor
	cause       pc.LifecycleCause
	scope       pc.ScopeRef
	cleanup     mc.ProjectCleanupAuthority
	checkpoint  []byte
	active      bool
	cancel      context.CancelFunc
	unknown     *f.CommitResult
	firstError  error
	kind        string
	observation string
	observed    bool
	candidate   i.AgentID
	batch       *projectMountBatch
}

func NewProjectLifecycleOwnership(store Store, agents *agent.RunnerManagedProjectCalls, executions *execution.RunnerManagedProjectCalls, guard *object.ProcessGuard, expected oc.ProcessID, stop mc.ProjectStopAuthority) (*ProjectLifecycleOwnership, error) {
	if agents == nil || executions == nil || guard == nil {
		return nil, fault(f.DependencyUnbound)
	}
	if err := agents.RequireBinding(store, guard, expected); err != nil {
		return nil, portError(err)
	}
	if err := executions.RequireBinding(store, guard, expected); err != nil {
		return nil, portError(err)
	}
	return newProjectLifecycleOwnership(store, agents, executions, guard, expected, stop)
}
func newProjectLifecycleOwnership(store Store, agents projectManagedAgents, executions projectManagedCalls, proof projectLifecycleProof, process oc.ProcessID, stop mc.ProjectStopAuthority) (*ProjectLifecycleOwnership, error) {
	if nilPort(store) || nilPort(agents) || nilPort(executions) || nilPort(proof) || nilPort(stop) || process.Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	instance, err := f.NewID[projectLifecycleInstance]()
	if err != nil {
		return nil, portError(err)
	}
	return &ProjectLifecycleOwnership{store: store, agents: agents, executions: executions, proof: proof, process: process, instance: instance, stop: stop, controls: map[i.ProjectID]*projectLifecycleControl{}, changed: make(chan struct{})}, nil
}
func projectLifecycleArguments(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
	if ctx == nil || actor.Validate() != nil || cause.Validate() != nil || cause.Action != pc.Delete || pc.RequireProjectScope(scope) != nil {
		return fault(f.InvalidArgument)
	}
	d := actor.Details()
	if d.Kind != i.Service || d.ServiceName != i.ProjectLifecycle || d.ProjectID != scope.ProjectID.String() || d.CauseRef != cause.OperationID.String() {
		return fault(f.Forbidden)
	}
	return nil
}
func (s *ProjectLifecycleOwnership) signalLocked() { close(s.changed); s.changed = make(chan struct{}) }
func (s *ProjectLifecycleOwnership) borrow(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, kind string, cp *pc.CleanupCheckpoint, cleanup mc.ProjectCleanupAuthority) (context.Context, *projectLifecycleControl, error) {
	if s == nil {
		return nil, nil, fault(f.DependencyUnbound)
	}
	if err := projectLifecycleArguments(ctx, actor, cause, scope); err != nil {
		return nil, nil, err
	}
	var raw []byte
	if cp != nil {
		raw = cp.Bytes()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.controls[scope.ProjectID]
	if c != nil {
		if c.active {
			return nil, nil, fault(f.ResourceBusy)
		}
		if !c.actor.Equal(actor) || c.cause != cause || !c.scope.Equal(scope) || c.kind != kind || !bytes.Equal(c.checkpoint, raw) {
			return nil, nil, fault(f.ConfirmationStale)
		}
		if err := ctx.Err(); err != nil {
			if c.firstError != nil {
				return nil, nil, c.firstError
			}
			return nil, nil, err
		}
	} else {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if s.stopped || len(s.controls) >= 4 {
			return nil, nil, fault(f.ResourceBusy)
		}
		id, err := f.NewID[projectLifecycleControlID]()
		if err != nil {
			return nil, nil, portError(err)
		}
		c = &projectLifecycleControl{id: id, actor: actor, cause: cause, scope: scope, kind: kind, checkpoint: bytes.Clone(raw), cleanup: cleanup}
		s.controls[scope.ProjectID] = c
	}
	run, cancel := context.WithTimeout(ctx, 2*time.Second)
	c.active, c.cancel = true, cancel
	s.signalLocked()
	return run, c, nil
}
func (s *ProjectLifecycleOwnership) returned(c *projectLifecycleControl) {
	// The original context cancellation returns before publishing the owner as
	// absent; the caller's Store and both original adapters have returned first.
	s.mu.Lock()
	cancel := c.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c.cancel = nil
	c.active = false
	if c.unknown == nil && s.agents.ProjectControlJoined(c.scope.ProjectID) && s.executions.ProjectControlJoined(c.scope.ProjectID) && s.controls[c.scope.ProjectID] == c {
		delete(s.controls, c.scope.ProjectID)
	}
	s.signalLocked()
}
func (s *ProjectLifecycleOwnership) retain(c *projectLifecycleControl, result f.CommitResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.unknown == nil {
		saved := result
		c.unknown = &saved
		c.firstError = commitError(result)
	}
	s.signalLocked()
	return c.firstError
}
func (s *ProjectLifecycleOwnership) clearUnknown(c *projectLifecycleControl) {
	s.mu.Lock()
	c.unknown = nil
	s.signalLocked()
	s.mu.Unlock()
}
func projectLifecycleCause(c *projectLifecycleControl) f.TransactionCause {
	cause, _ := f.NewRecoveryCause("runner-managed-project-"+c.kind, c.id.String(), c.scope.ProjectID.String())
	return cause
}
func projectLifecycleLocks(p i.ProjectID, a i.AgentID) []f.LockRequest {
	pk, _ := f.ProjectLock(p.String())
	schedule, _ := f.ProjectScheduleLock(p.String())
	locks := []f.LockRequest{{Key: pk, Mode: f.Exclusive}, {Key: schedule, Mode: f.Exclusive}}
	if a.Validate() == nil {
		ak, _ := f.AgentLock(a.String())
		locks = append(locks, f.LockRequest{Key: ak, Mode: f.Exclusive})
	}
	normalized, _ := obc.NormalizeLocks(locks)
	return normalized
}
func (s *ProjectLifecycleOwnership) transaction(ctx context.Context, c *projectLifecycleControl, cause f.TransactionCause, a i.AgentID, fn func(context.Context, f.Tx, postgres.SQLExecutor) error) f.CommitResult {
	return s.store.WithinTx(ctx, cause, func(run context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(run, tx, projectLifecycleLocks(c.scope.ProjectID, a)); err != nil {
			return portError(err)
		}
		if err := s.proof.RequireCurrentProcessInTx(run, tx, s.process); err != nil {
			return portError(err)
		}
		var err error
		if c.kind == "stop" {
			err = s.stop.RequireProjectStopInTx(run, tx, c.actor, c.cause, c.scope)
		} else {
			err = c.cleanup.RequireProjectCleanupInTx(run, tx, c.actor, c.cause, c.scope)
		}
		if err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if nilPort(x) {
			return fault(f.DependencyUnbound)
		}
		return fn(run, tx, x)
	})
}
func (s *ProjectLifecycleOwnership) RequestProjectStop(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (pc.StopReport, error) {
	return s.projectStop(ctx, actor, cause, scope, true)
}
func (s *ProjectLifecycleOwnership) InspectProjectStop(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (pc.StopReport, error) {
	return s.projectStop(ctx, actor, cause, scope, false)
}
func (s *ProjectLifecycleOwnership) projectStop(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, request bool) (pc.StopReport, error) {
	run, c, err := s.borrow(ctx, actor, cause, scope, "stop", nil, nil)
	if err != nil {
		return pc.StopReport{}, err
	}
	defer s.returned(c)
	details := pc.StopDetails{State: pc.StopPending, SafeReason: pc.ReasonWorkPending}
	err = s.stop.WithProjectStop(run, actor, cause, scope, func(authorized context.Context) error {
		if c.unknown != nil {
			valid := false
			result := s.transaction(authorized, c, c.unknown.Cause(), i.AgentID{}, func(context.Context, f.Tx, postgres.SQLExecutor) error { valid = true; return nil })
			if result.State() != f.Committed || !valid || authorized.Err() != nil {
				return c.firstError
			}
			s.clearUnknown(c)
			return nil
		}
		for _, owner := range []projectManagedCalls{s.agents, s.executions} {
			var report pc.StopReport
			var err error
			if request {
				report, err = owner.RequestProjectStop(authorized, actor, cause, scope)
			} else {
				report, err = owner.InspectProjectStop(authorized, actor, cause, scope)
			}
			if err != nil {
				if c.firstError == nil {
					c.firstError = err
				}
				return err
			}
			if !report.Matches(pc.RunnerManagedParticipant, cause, scope) || report.Details().State != pc.Stopped {
				return nil
			}
		}
		validated, unsupported := false, false
		result := s.transaction(authorized, c, projectLifecycleCause(c), i.AgentID{}, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
			var err error
			unsupported, err = projectRunnerOperationsPresent(ctx, x, scope.ProjectID)
			if err != nil {
				return err
			}
			validated = true
			return ctx.Err()
		})
		if result.State() == f.Unknown {
			return s.retain(c, result)
		}
		if err := commitError(result); err != nil {
			return err
		}
		if !validated || authorized.Err() != nil {
			return fault(f.ResourceBusy)
		}
		if unsupported {
			return nil
		}
		details = pc.StopDetails{State: pc.Stopped}
		return nil
	})
	if err != nil {
		return pc.StopReport{}, err
	}
	return pc.NewStopReport(pc.RunnerManagedParticipant, cause, scope, details)
}
func (s *ProjectLifecycleOwnership) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.stopped = true
	var cancels []context.CancelFunc
	for _, c := range s.controls {
		if c.cancel != nil {
			cancels = append(cancels, c.cancel)
		}
	}
	s.signalLocked()
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}
func (s *ProjectLifecycleOwnership) Joined() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.controls) == 0 && s.agents.Joined() && s.executions.Joined()
}
func (s *ProjectLifecycleOwnership) Drain(ctx context.Context) error {
	if s == nil || ctx == nil {
		return fault(f.DependencyUnbound)
	}
	s.Stop()
	for {
		s.mu.Lock()
		localJoined := len(s.controls) == 0
		joined := localJoined && s.agents.Joined() && s.executions.Joined()
		changed := s.changed
		s.mu.Unlock()
		if joined {
			return nil
		}
		if localJoined {
			return fault(f.ResourceBusy)
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
