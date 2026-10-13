package project

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

// UsageStopAuthority is bound to the original lifecycle authority and Store.
// Only a current stop driver's private invocation can lend a Usage control;
// public cause values and stopped database rows do not issue this permission.
type UsageStopAuthority struct{ authority *LifecycleAuthority }

// Runtime admission already carries the original Model's private whole-call
// witness. This independent Project fact prevents a fresh call from registering
// after Delete has sealed the Project, even in another process. It is neither
// a Human Owner grant nor permission for Model business operations.
func (a *UsageStopAuthority) RequireProjectInvocationAdmissionInTx(ctx context.Context, tx f.Tx, project c.ProjectID) error {
	if a == nil || !a.authority.bound() {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() || project.Validate() != nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return portError(err)
	}
	x, err := a.authority.lifecycleExecutor(ctx, tx, project)
	if err != nil {
		return err
	}
	if nilPort(x) {
		return fault(f.DependencyUnbound)
	}
	p, err := loadProject(ctx, x, project)
	if err != nil {
		return err
	}
	if p == nil {
		return fault(f.NotFound)
	}
	initialization := c.InitializationPending
	if p.initialized {
		initialization = c.Initialized
	}
	if err = c.CheckOwnerGate(p.ref.Lifecycle, initialization, i.Mutate); err != nil {
		return portError(err)
	}
	return portError(ctx.Err())
}

func NewUsageStopAuthority(authority *LifecycleAuthority, executorStore Store) (*UsageStopAuthority, error) {
	if !authority.bound() || nilPort(executorStore) || !sameStore(authority.store, executorStore) {
		return nil, fault(f.DependencyUnbound)
	}
	return &UsageStopAuthority{authority: authority}, nil
}

type usageStopLoanKey struct{}
type usageStopLoan struct{ scope *agentPreparationScope }

func (s *agentPreparationScope) borrowUsageStop(ctx context.Context) (*usageStopLoan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active || s.ctx.Err() != nil {
		return nil, fault(f.InvalidState)
	}
	if ctx.Value(agentPreparationLoanKey{}) != nil || ctx.Value(schedulerStopLoanKey{}) != nil || ctx.Value(executionStopLoanKey{}) != nil || ctx.Value(artifactSourceStopLoanKey{}) != nil || ctx.Value(usageStopLoanKey{}) != nil || ctx.Value(knowledgeStopLoanKey{}) != nil || ctx.Value(runnerManagedStopLoanKey{}) != nil || ctx.Value(knowledgeSourceStopLoanKey{}) != nil || ctx.Value(workStopLoanKey{}) != nil {
		return nil, fault(f.Forbidden)
	}
	if s.usageStops == nil {
		s.usageStops = make(map[*usageStopLoan]struct{})
	}
	loan := &usageStopLoan{scope: s}
	s.usageStops[loan] = struct{}{}
	return loan, nil
}
func (s *agentPreparationScope) releaseUsageStop(loan *usageStopLoan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.usageStops, loan)
	close(s.changed)
	s.changed = make(chan struct{})
}
func (s *agentPreparationScope) usageStopActive(loan *usageStopLoan) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, present := s.usageStops[loan]
	return present && loan.scope == s && s.active && s.ctx.Err() == nil
}

func (a *UsageStopAuthority) usageScope(ctx context.Context, actor i.Actor, cause c.LifecycleCause, target c.ScopeRef) (*agentPreparationScope, error) {
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
	if project != target.ProjectID || cause.Action != c.Delete {
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
func (a *UsageStopAuthority) WithProjectStop(ctx context.Context, actor i.Actor, cause c.LifecycleCause, target c.ScopeRef, fn func(context.Context) error) error {
	if fn == nil {
		return invalid()
	}
	scope, err := a.usageScope(ctx, actor, cause, target)
	if err != nil {
		return err
	}
	loan, err := scope.borrowUsageStop(ctx)
	if err != nil {
		return err
	}
	defer scope.releaseUsageStop(loan)
	run, cancel := context.WithCancel(ctx)
	stopCancel := context.AfterFunc(scope.ctx, cancel)
	defer func() { stopCancel(); cancel() }()
	run = context.WithValue(run, usageStopLoanKey{}, loan)
	txCause, err := f.NewRecoveryCause("usage-project-stop", cause.OperationID.String(), target.ProjectID.String())
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
	if !scope.usageStopActive(loan) {
		return fault(f.InvalidState)
	}
	if err = fn(run); err != nil {
		return err
	}
	if err = run.Err(); err != nil {
		return err
	}
	if !scope.usageStopActive(loan) {
		return fault(f.InvalidState)
	}
	return nil
}

func (a *UsageStopAuthority) RequireProjectStopInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause c.LifecycleCause, target c.ScopeRef) error {
	if !tx.Valid() {
		return invalid()
	}
	scope, err := a.usageScope(ctx, actor, cause, target)
	if err != nil {
		return err
	}
	loan, ok := ctx.Value(usageStopLoanKey{}).(*usageStopLoan)
	if !ok || loan == nil || loan.scope != scope || !scope.usageStopActive(loan) {
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
	fact, err := a.authority.lifecycleFact(ctx, x, target.ProjectID, cause, c.UsageParticipant)
	if err != nil {
		return err
	}
	if err = fact.authorize(false); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if !scope.usageStopActive(loan) {
		return fault(f.InvalidState)
	}
	return nil
}

var _ uc.ProjectStopAuthority = (*UsageStopAuthority)(nil)
var _ uc.ProjectInvocationAdmissionAuthority = (*UsageStopAuthority)(nil)
