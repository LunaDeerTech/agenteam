package project

import (
	"context"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// AgentRetirementPreparation is fixed to one real stop driver. Only that
// driver's current invoke can supply the private scope consumed by this owner.
type AgentRetirementPreparation struct{ driver *lifecycleStopState }

func NewAgentRetirementPreparation(driver *LifecycleStopDriver) (*AgentRetirementPreparation, error) {
	if driver == nil || driver.state == nil || nilPort(driver.state.store) || !driver.state.authority.bound() || !sameStore(driver.state.store, driver.state.authority.store) {
		return nil, fault(f.DependencyUnbound)
	}
	return &AgentRetirementPreparation{driver: driver.state}, nil
}

type agentPreparationClaimKey struct{}
type agentPreparationScopeKey struct{}
type agentPreparationLoanKey struct{}

type agentPreparationClaim struct {
	driver *lifecycleStopState
	claim  lifecycleStopClaim
	cause  c.LifecycleCause
}

type agentPreparationScope struct {
	knowledgeStops     map[*knowledgeStopLoan]struct{}
	runnerManagedStops map[*runnerManagedStopLoan]struct{}
	knowledgeSources   map[*knowledgeSourceStopLoan]struct{}
	driver             *lifecycleStopState
	claim              lifecycleStopClaim
	cause              c.LifecycleCause
	actor              i.Actor
	ctx                context.Context
	cancel             context.CancelFunc
	mu                 sync.Mutex
	active             bool
	loans              map[*agentPreparationLoan]struct{}
	schedulerStops     map[*schedulerStopLoan]struct{}
	executionStops     map[*executionStopLoan]struct{}
	artifactSources    map[*artifactSourceStopLoan]struct{}
	workStops          map[*workStopLoan]struct{}
	usageStops         map[*usageStopLoan]struct{}
	changed            chan struct{}
}

type agentPreparationLoan struct {
	scope   *agentPreparationScope
	parent  *agentPreparationLoan
	tx      f.Tx
	request c.AgentRetirementPreparationRequest
	active  bool // Protected, including ancestors, by scope.mu.
}

// Called only after start's original Store return is known Committed. This is
// not a public context constructor and cannot be reconstructed from claim DTOs.
func agentPreparationClaimContext(ctx context.Context, st *lifecycleStopState, claim lifecycleStopClaim, cause c.LifecycleCause) context.Context {
	return context.WithValue(ctx, agentPreparationClaimKey{}, agentPreparationClaim{st, claim, cause})
}

func (st *lifecycleStopState) agentPreparationStep(ctx context.Context, actor i.Actor, cause c.LifecycleCause, project c.ProjectID) (context.Context, func(), error) {
	p, present := ctx.Value(agentPreparationClaimKey{}).(agentPreparationClaim)
	if !present {
		// Older private invoke callers have no preparation authority. Public Run
		// always supplies the original committed claim before entering invoke.
		return ctx, func() {}, nil
	}
	if p.driver != st || p.cause != cause || p.claim.project != project || p.claim.operation != cause.OperationID || p.claim.phase != "running" {
		return nil, nil, fault(f.Forbidden)
	}
	run, cancel := context.WithCancel(ctx)
	scope := &agentPreparationScope{driver: st, claim: p.claim, cause: cause, actor: actor, ctx: run, cancel: cancel, active: true, loans: make(map[*agentPreparationLoan]struct{}), changed: make(chan struct{})}
	return context.WithValue(run, agentPreparationScopeKey{}, scope), scope.close, nil
}

func sameAgentPreparationRequest(a, b c.AgentRetirementPreparationRequest) bool {
	return a.Actor.Equal(b.Actor) && a.ProjectID == b.ProjectID && a.Cause == b.Cause && a.AgentID == b.AgentID && a.ExpectedConfigVersion == b.ExpectedConfigVersion
}

func (s *agentPreparationScope) ancestorsActive(loan *agentPreparationLoan) bool {
	for current := loan; current != nil; current = current.parent {
		if current.scope != s || !current.active {
			return false
		}
	}
	return true
}

func (s *agentPreparationScope) borrow(ctx context.Context, tx f.Tx, request c.AgentRetirementPreparationRequest) (*agentPreparationLoan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active || s.ctx.Err() != nil {
		return nil, fault(f.InvalidState)
	}
	// A stop control is a different synchronous loan. Its saved context cannot
	// inherit this scope to create an Agent read after that control ends.
	if ctx.Value(schedulerStopLoanKey{}) != nil || ctx.Value(executionStopLoanKey{}) != nil || ctx.Value(artifactSourceStopLoanKey{}) != nil || ctx.Value(workStopLoanKey{}) != nil || ctx.Value(usageStopLoanKey{}) != nil || ctx.Value(knowledgeStopLoanKey{}) != nil || ctx.Value(runnerManagedStopLoanKey{}) != nil || ctx.Value(knowledgeSourceStopLoanKey{}) != nil {
		return nil, fault(f.Forbidden)
	}
	parent, _ := ctx.Value(agentPreparationLoanKey{}).(*agentPreparationLoan)
	if parent != nil && (!s.ancestorsActive(parent) || parent.tx != tx || !sameAgentPreparationRequest(parent.request, request)) {
		return nil, fault(f.Forbidden)
	}
	loan := &agentPreparationLoan{scope: s, parent: parent, tx: tx, request: request, active: true}
	s.loans[loan] = struct{}{}
	return loan, nil
}

func (s *agentPreparationScope) release(loan *agentPreparationLoan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	loan.active = false
	delete(s.loans, loan)
	close(s.changed)
	s.changed = make(chan struct{})
}

// Only invoke closes a scope, after the fixed step returned. With releases its
// own loan without waiting for children, so a callback never waits on itself.
// Closing cancels and seals admission before waiting for actual Agent read,
// Scheduler control and Execution control returns. No loan closes its own scope.
func (s *agentPreparationScope) close() {
	s.mu.Lock()
	s.active = false
	s.mu.Unlock()
	s.cancel()
	for {
		s.mu.Lock()
		if len(s.loans) == 0 && len(s.schedulerStops) == 0 && len(s.executionStops) == 0 && len(s.artifactSources) == 0 && len(s.workStops) == 0 && len(s.usageStops)+len(s.knowledgeStops)+len(s.knowledgeSources)+len(s.runnerManagedStops) == 0 {
			s.mu.Unlock()
			return
		}
		changed := s.changed
		s.mu.Unlock()
		<-changed
	}
}

// WithAgentRetirementPreparationInTx owns the synchronous callback's actual
// return, not the surrounding Store.WithinTx return or arbitrary goroutines.
// Nested readers are independently counted and must use the exact live parent
// loan, Tx and target. A saved closed loan context cannot borrow from its still
// active ancestor step. No caller-owned transaction is opened or committed here.
func (a *AgentRetirementPreparation) WithAgentRetirementPreparationInTx(ctx context.Context, tx f.Tx, request c.AgentRetirementPreparationRequest, fn func(context.Context) error) error {
	if a == nil || a.driver == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() || fn == nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := request.Validate(); err != nil {
		return err
	}
	scope, ok := ctx.Value(agentPreparationScopeKey{}).(*agentPreparationScope)
	if !ok || scope == nil || scope.driver != a.driver || scope.cause != request.Cause || scope.claim.project != request.ProjectID || !scope.actor.Equal(request.Actor) {
		return fault(f.Forbidden)
	}
	loan, err := scope.borrow(ctx, tx, request)
	if err != nil {
		return err
	}
	defer scope.release(loan)
	run, cancel := context.WithCancel(ctx)
	stopCancel := context.AfterFunc(scope.ctx, cancel)
	defer func() { stopCancel(); cancel() }()
	run = context.WithValue(run, agentPreparationLoanKey{}, loan)
	if err := scope.ctx.Err(); err != nil {
		return err
	}
	st := a.driver
	x, err := st.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if nilPort(x) {
		return fault(f.DependencyUnbound)
	}
	locks, err := request.RequiredLocks()
	if err != nil {
		return err
	}
	if err = st.store.RequireHeldLocks(run, tx, locks); err != nil {
		return portError(err)
	}
	actual, err := loadLifecycleStopClaim(run, x, request.Cause.OperationID)
	if err != nil {
		return err
	}
	if actual == nil || *actual != scope.claim {
		return fault(f.Forbidden)
	}
	cause, record, err := st.current(run, x, request.ProjectID, request.Cause.OperationID)
	if err != nil {
		return err
	}
	if cause != request.Cause || record.operation.State != c.OperationStopping {
		return fault(f.InvalidState)
	}
	if err = run.Err(); err != nil {
		return err
	}
	if err = fn(run); err != nil {
		return err
	}
	if err = run.Err(); err != nil {
		return err
	}
	if err = scope.ctx.Err(); err != nil {
		return err
	}
	scope.mu.Lock()
	valid := scope.active && scope.ancestorsActive(loan)
	scope.mu.Unlock()
	if !valid {
		return fault(f.InvalidState)
	}
	return nil
}
