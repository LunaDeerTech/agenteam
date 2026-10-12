package project

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// WorkStopAuthority is bound to the original lifecycle authority and Store.
// Only a current stop driver's private invocation can lend a Work control;
// public cause values and stopped database rows do not issue this permission.
type WorkStopAuthority struct{ authority *LifecycleAuthority }

func NewWorkStopAuthority(authority *LifecycleAuthority, executorStore Store) (*WorkStopAuthority, error) {
	if !authority.bound() || nilPort(executorStore) || !sameStore(authority.store, executorStore) {
		return nil, fault(f.DependencyUnbound)
	}
	return &WorkStopAuthority{authority: authority}, nil
}

type workStopLoanKey struct{}
type workStopLoan struct{ scope *agentPreparationScope }

func (s *agentPreparationScope) borrowWorkStop(ctx context.Context) (*workStopLoan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active || s.ctx.Err() != nil {
		return nil, fault(f.InvalidState)
	}
	if ctx.Value(agentPreparationLoanKey{}) != nil || ctx.Value(schedulerStopLoanKey{}) != nil || ctx.Value(executionStopLoanKey{}) != nil || ctx.Value(artifactSourceStopLoanKey{}) != nil || ctx.Value(workStopLoanKey{}) != nil {
		return nil, fault(f.Forbidden)
	}
	if s.workStops == nil {
		s.workStops = make(map[*workStopLoan]struct{})
	}
	loan := &workStopLoan{scope: s}
	s.workStops[loan] = struct{}{}
	return loan, nil
}
func (s *agentPreparationScope) releaseWorkStop(loan *workStopLoan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.workStops, loan)
	close(s.changed)
	s.changed = make(chan struct{})
}
func (s *agentPreparationScope) workStopActive(loan *workStopLoan) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, present := s.workStops[loan]
	return present && loan.scope == s && s.active && s.ctx.Err() == nil
}

func (a *WorkStopAuthority) workScope(ctx context.Context, actor i.Actor, cause c.LifecycleCause, target c.ScopeRef) (*agentPreparationScope, error) {
	if a == nil || !a.authority.bound() {
		return nil, fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, err := lifecycleActorProject(actor, cause)
	if err != nil {
		return nil, err
	}
	if err = c.RequireProjectScope(target); err != nil {
		return nil, err
	}
	if project != target.ProjectID || cause.Action != c.Archive && cause.Action != c.Delete {
		return nil, fault(f.Forbidden)
	}
	scope, ok := ctx.Value(agentPreparationScopeKey{}).(*agentPreparationScope)
	if !ok || scope == nil || scope.driver == nil || scope.driver.authority != a.authority || !sameStore(scope.driver.store, a.authority.store) ||
		!scope.actor.Equal(actor) || scope.cause != cause || scope.claim.project != project || scope.claim.operation != cause.OperationID || scope.claim.phase != "running" {
		return nil, fault(f.Forbidden)
	}
	return scope, nil
}

// WithProjectStop waits for the original authorization transaction and the
// complete callback return. Closing the parent scope seals and cancels this
// loan, then waits for its actual release; cancellation cannot fabricate join.
func (a *WorkStopAuthority) WithProjectStop(ctx context.Context, actor i.Actor, cause c.LifecycleCause, target c.ScopeRef, fn func(context.Context) error) error {
	if fn == nil {
		return invalid()
	}
	scope, err := a.workScope(ctx, actor, cause, target)
	if err != nil {
		return err
	}
	loan, err := scope.borrowWorkStop(ctx)
	if err != nil {
		return err
	}
	defer scope.releaseWorkStop(loan)
	run, cancel := context.WithCancel(ctx)
	stopCancel := context.AfterFunc(scope.ctx, cancel)
	defer func() { stopCancel(); cancel() }()
	run = context.WithValue(run, workStopLoanKey{}, loan)
	txCause, err := f.NewRecoveryCause("work-project-stop", cause.OperationID.String(), target.ProjectID.String())
	if err != nil {
		return portError(err)
	}
	validated := false
	result := scope.driver.store.WithinTx(run, txCause, func(ctx context.Context, tx f.Tx) error {
		if err := scope.driver.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(target.ProjectID, f.Exclusive)}); err != nil {
			return portError(err)
		}
		if err := a.RequireProjectStopInTx(ctx, tx, actor, cause, target); err != nil {
			return err
		}
		validated = true
		return ctx.Err()
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
	if !scope.workStopActive(loan) {
		return fault(f.InvalidState)
	}
	if err = fn(run); err != nil {
		return err
	}
	if err = run.Err(); err != nil {
		return err
	}
	if !scope.workStopActive(loan) {
		return fault(f.InvalidState)
	}
	return nil
}

func (a *WorkStopAuthority) RequireProjectStopInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause c.LifecycleCause, target c.ScopeRef) error {
	if !tx.Valid() {
		return invalid()
	}
	scope, err := a.workScope(ctx, actor, cause, target)
	if err != nil {
		return err
	}
	loan, ok := ctx.Value(workStopLoanKey{}).(*workStopLoan)
	if !ok || loan == nil || loan.scope != scope || !scope.workStopActive(loan) {
		return fault(f.Forbidden)
	}
	st := scope.driver
	if err = st.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(target.ProjectID, f.Exclusive)}); err != nil {
		return portError(err)
	}
	x, err := a.authority.lifecycleExecutor(ctx, tx, target.ProjectID)
	if err != nil {
		return err
	}
	if nilPort(x) {
		return fault(f.DependencyUnbound)
	}
	actual, err := loadLifecycleStopClaim(ctx, x, cause.OperationID)
	if err != nil {
		return err
	}
	if actual == nil || *actual != scope.claim {
		return fault(f.Forbidden)
	}
	current, record, err := st.current(ctx, x, target.ProjectID, cause.OperationID)
	if err != nil {
		return err
	}
	if current != cause || record.operation.State != c.OperationStopping {
		return fault(f.InvalidState)
	}
	fact, err := a.authority.lifecycleFact(ctx, x, target.ProjectID, cause, c.WorkParticipant)
	if err != nil {
		return err
	}
	if err = fact.authorize(false); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if !scope.workStopActive(loan) {
		return fault(f.InvalidState)
	}
	return nil
}

var _ wc.ProjectStopAuthority = (*WorkStopAuthority)(nil)
