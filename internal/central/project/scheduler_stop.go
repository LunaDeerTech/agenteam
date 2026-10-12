package project

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// SchedulerStopAuthority is fixed to the original driver, directory and
// manager Store. Neither an Actor DTO nor a visible stopping row is its issuer.
type SchedulerStopAuthority struct {
	driver    *lifecycleStopState
	directory *SchedulerProjects
}

func NewSchedulerStopAuthority(driver *LifecycleStopDriver, directory *SchedulerProjects, managerStore Store) (*SchedulerStopAuthority, error) {
	if driver == nil || driver.state == nil || directory == nil || directory.authority.state() == nil || nilPort(managerStore) {
		return nil, fault(f.DependencyUnbound)
	}
	st := driver.state
	if nilPort(st.store) || !st.authority.bound() || !sameStore(st.store, st.authority.store) || !sameStore(st.store, directory.authority.state().store) || !sameStore(st.store, managerStore) {
		return nil, fault(f.DependencyUnbound)
	}
	return &SchedulerStopAuthority{driver: st, directory: directory}, nil
}

type schedulerStopLoanKey struct{}
type schedulerStopLoan struct {
	scope *agentPreparationScope
	// Only the full Scheduler binding issues transaction-consumable loans.
	// The existing local ProjectRunners authority leaves issuer nil.
	issuer *SchedulerProjectStopAuthority
}

func (s *agentPreparationScope) borrowSchedulerStop(ctx context.Context) (*schedulerStopLoan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active || s.ctx.Err() != nil {
		return nil, fault(f.InvalidState)
	}
	// Only the original step may start an independent control. A saved loan
	// cannot revive its ancestor scope, even through context.WithoutCancel.
	if ctx.Value(agentPreparationLoanKey{}) != nil || ctx.Value(schedulerStopLoanKey{}) != nil || ctx.Value(executionStopLoanKey{}) != nil || ctx.Value(artifactSourceStopLoanKey{}) != nil || ctx.Value(workStopLoanKey{}) != nil {
		return nil, fault(f.Forbidden)
	}
	if s.schedulerStops == nil {
		s.schedulerStops = make(map[*schedulerStopLoan]struct{})
	}
	loan := &schedulerStopLoan{scope: s}
	s.schedulerStops[loan] = struct{}{}
	return loan, nil
}

func (s *agentPreparationScope) releaseSchedulerStop(loan *schedulerStopLoan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.schedulerStops, loan)
	close(s.changed)
	s.changed = make(chan struct{})
}

func (s *agentPreparationScope) schedulerStopActive(loan *schedulerStopLoan) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, present := s.schedulerStops[loan]
	return present && loan.scope == s && s.active && s.ctx.Err() == nil
}

// WithConfirmedSchedulerStop owns the authorization transaction's ORIGINAL
// return and the complete synchronous callback. Only invoke closes the parent
// scope; the control releases itself without ever waiting on its own loan.
func (a *SchedulerStopAuthority) WithConfirmedSchedulerStop(ctx context.Context, actor i.Actor, cause c.LifecycleCause, target c.ScopeRef, fn func(context.Context) error) error {
	if a == nil || a.driver == nil || a.directory == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || fn == nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	project, err := lifecycleActorProject(actor, cause)
	if err != nil {
		return err
	}
	if err = c.RequireProjectScope(target); err != nil {
		return err
	}
	if cause.Action != c.Delete || project != target.ProjectID {
		return fault(f.Forbidden)
	}
	scope, ok := ctx.Value(agentPreparationScopeKey{}).(*agentPreparationScope)
	if !ok || scope == nil || scope.driver != a.driver || scope.cause != cause || scope.claim.project != project || !scope.actor.Equal(actor) {
		return fault(f.Forbidden)
	}
	loan, err := scope.borrowSchedulerStop(ctx)
	if err != nil {
		return err
	}
	defer scope.releaseSchedulerStop(loan)
	run, cancel := context.WithCancel(ctx)
	stopCancel := context.AfterFunc(scope.ctx, cancel)
	defer func() { stopCancel(); cancel() }()
	run = context.WithValue(run, schedulerStopLoanKey{}, loan)
	if !scope.schedulerStopActive(loan) {
		return fault(f.InvalidState)
	}
	txCause, err := f.NewRecoveryCause("scheduler-project-stop", cause.OperationID.String(), project.String())
	if err != nil {
		return portError(err)
	}
	validated := false
	st := a.driver
	result := st.store.WithinTx(run, txCause, func(ctx context.Context, tx f.Tx) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := st.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(project, f.Shared)}); err != nil {
			return portError(err)
		}
		x, err := st.authority.lifecycleExecutor(ctx, tx, project)
		if err != nil {
			return err
		}
		actual, err := loadLifecycleStopClaim(ctx, x, cause.OperationID)
		if err != nil {
			return err
		}
		if actual == nil || *actual != scope.claim {
			return fault(f.Forbidden)
		}
		current, record, err := st.current(ctx, x, project, cause.OperationID)
		if err != nil {
			return err
		}
		if current != cause || record.operation.State != c.OperationStopping {
			return fault(f.InvalidState)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !scope.schedulerStopActive(loan) {
			return fault(f.InvalidState)
		}
		validated = true
		return nil
	})
	if err = commitError(result); err != nil {
		return err
	}
	if !validated {
		return unavailable(nil)
	}
	if err = run.Err(); err != nil {
		return err
	}
	if !scope.schedulerStopActive(loan) {
		return fault(f.InvalidState)
	}
	if err = fn(run); err != nil {
		return err
	}
	if err = run.Err(); err != nil {
		return err
	}
	if !scope.schedulerStopActive(loan) {
		return fault(f.InvalidState)
	}
	return nil
}
