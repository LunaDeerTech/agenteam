package project

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// KnowledgeProjectSourceStopAuthority consumes the original stop driver's private
// invoke scope. Binding the original LifecycleAuthority avoids a mutable
// registration cycle; each loan still belongs to exactly one actual driver,
// current claim and invocation, never to a public Actor or Cause DTO.
type KnowledgeProjectSourceStopAuthority struct{ authority *LifecycleAuthority }

func NewKnowledgeProjectSourceStopAuthority(authority *LifecycleAuthority, executorStore Store) (*KnowledgeProjectSourceStopAuthority, error) {
	if !authority.bound() || nilPort(executorStore) || !sameStore(authority.store, executorStore) {
		return nil, fault(f.DependencyUnbound)
	}
	return &KnowledgeProjectSourceStopAuthority{authority: authority}, nil
}

type knowledgeSourceStopLoanKey struct{}
type knowledgeSourceStopLoan struct {
	scope   *agentPreparationScope
	request oc.KnowledgeProjectSourceStopRequest
	parent  *knowledgeStopLoan
}

func (s *agentPreparationScope) borrowKnowledgeProjectSourceStop(ctx context.Context, request oc.KnowledgeProjectSourceStopRequest) (*knowledgeSourceStopLoan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active || s.ctx.Err() != nil {
		return nil, fault(f.InvalidState)
	}
	if ctx.Value(agentPreparationLoanKey{}) != nil || ctx.Value(schedulerStopLoanKey{}) != nil || ctx.Value(executionStopLoanKey{}) != nil || ctx.Value(knowledgeSourceStopLoanKey{}) != nil || ctx.Value(artifactSourceStopLoanKey{}) != nil || ctx.Value(workStopLoanKey{}) != nil || ctx.Value(usageStopLoanKey{}) != nil {
		return nil, fault(f.Forbidden)
	}
	parent, _ := ctx.Value(knowledgeStopLoanKey{}).(*knowledgeStopLoan)
	if parent == nil || parent.scope != s || parent.closing {
		return nil, fault(f.Forbidden)
	}
	if _, ok := s.knowledgeStops[parent]; !ok {
		return nil, fault(f.Forbidden)
	}
	if s.knowledgeSources == nil {
		s.knowledgeSources = make(map[*knowledgeSourceStopLoan]struct{})
	}
	loan := &knowledgeSourceStopLoan{scope: s, request: request, parent: parent}
	s.knowledgeSources[loan] = struct{}{}
	return loan, nil
}
func (s *agentPreparationScope) releaseKnowledgeProjectSourceStop(loan *knowledgeSourceStopLoan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.knowledgeSources, loan)
	close(s.changed)
	s.changed = make(chan struct{})
}
func (s *agentPreparationScope) knowledgeSourceStopActive(loan *knowledgeSourceStopLoan) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, present := s.knowledgeSources[loan]
	_, parentPresent := s.knowledgeStops[loan.parent]
	return present && parentPresent && loan.scope == s && s.active && s.ctx.Err() == nil
}

func (a *KnowledgeProjectSourceStopAuthority) sourceScope(ctx context.Context, request oc.KnowledgeProjectSourceStopRequest) (*agentPreparationScope, error) {
	if a == nil || !a.authority.bound() {
		return nil, fault(f.DependencyUnbound)
	}
	if ctx == nil || request.Validate() != nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d := request.Details()
	cause := d.Cause.Details()
	scope, ok := ctx.Value(agentPreparationScopeKey{}).(*agentPreparationScope)
	if !ok || scope == nil || scope.driver == nil || scope.driver.authority != a.authority || !sameStore(scope.driver.store, a.authority.store) ||
		!scope.actor.Equal(d.Actor) || scope.claim.project != cause.ProjectID || scope.cause.OperationID.String() != cause.OperationID.String() ||
		string(scope.cause.Action) != string(cause.Action) || scope.cause.ProjectVersion != cause.ProjectVersion || scope.claim.phase != "running" {
		return nil, fault(f.Forbidden)
	}
	parent, _ := ctx.Value(knowledgeStopLoanKey{}).(*knowledgeStopLoan)
	if parent == nil || parent.scope != scope || !scope.knowledgeStopActive(parent) || cause.Action != oc.ProjectStopDelete {
		return nil, fault(f.Forbidden)
	}
	return scope, nil
}

func (a *KnowledgeProjectSourceStopAuthority) WithKnowledgeProjectSourceStop(ctx context.Context, request oc.KnowledgeProjectSourceStopRequest, fn func(context.Context) error) error {
	if fn == nil {
		return invalid()
	}
	scope, err := a.sourceScope(ctx, request)
	if err != nil {
		return err
	}
	loan, err := scope.borrowKnowledgeProjectSourceStop(ctx, request)
	if err != nil {
		return err
	}
	defer scope.releaseKnowledgeProjectSourceStop(loan)
	run, cancel := context.WithCancel(ctx)
	stopCancel := context.AfterFunc(scope.ctx, cancel)
	defer func() { stopCancel(); cancel() }()
	run = context.WithValue(run, knowledgeSourceStopLoanKey{}, loan)
	d := request.Details().Cause.Details()
	txCause, err := f.NewRecoveryCause("knowledge-source-stop", d.OperationID.String(), request.Details().Lease.ID().String())
	if err != nil {
		return portError(err)
	}
	locks, err := request.RequiredLocks()
	if err != nil {
		return err
	}
	validated := false
	result := scope.driver.store.WithinTx(run, txCause, func(ctx context.Context, tx f.Tx) error {
		if err := scope.driver.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := a.RequireKnowledgeProjectSourceStopInTx(ctx, tx, request); err != nil {
			return err
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
	if !scope.knowledgeSourceStopActive(loan) {
		return fault(f.InvalidState)
	}
	// The callback joins the original source tail synchronously, including its
	// own Store returns. Cancellation does not release this loan early.
	if err = fn(run); err != nil {
		return err
	}
	if err = run.Err(); err != nil {
		return err
	}
	if !scope.knowledgeSourceStopActive(loan) {
		return fault(f.InvalidState)
	}
	return nil
}

func (a *KnowledgeProjectSourceStopAuthority) RequireKnowledgeProjectSourceStopInTx(ctx context.Context, tx f.Tx, request oc.KnowledgeProjectSourceStopRequest) error {
	if !tx.Valid() {
		return invalid()
	}
	scope, err := a.sourceScope(ctx, request)
	if err != nil {
		return err
	}
	loan, ok := ctx.Value(knowledgeSourceStopLoanKey{}).(*knowledgeSourceStopLoan)
	if !ok || loan == nil || loan.scope != scope || !loan.request.Equal(request) || !scope.knowledgeSourceStopActive(loan) {
		return fault(f.Forbidden)
	}
	locks, err := request.RequiredLocks()
	if err != nil {
		return err
	}
	st := scope.driver
	if err = st.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	x, err := a.authority.lifecycleExecutor(ctx, tx, scope.claim.project)
	if err != nil {
		return err
	}
	if nilPort(x) {
		return fault(f.DependencyUnbound)
	}
	actual, err := loadLifecycleStopClaim(ctx, x, scope.claim.operation)
	if err != nil {
		return err
	}
	if actual == nil || *actual != scope.claim {
		return fault(f.Forbidden)
	}
	cause, record, err := st.current(ctx, x, scope.claim.project, scope.claim.operation)
	if err != nil {
		return err
	}
	if cause != scope.cause || record.operation.State != c.OperationStopping {
		return fault(f.InvalidState)
	}
	// Unlike a generic stop observation, only the still-active Knowledge
	// participant may issue source convergence within this stopping operation.
	fact, err := a.authority.lifecycleFact(ctx, x, scope.claim.project, cause, c.KnowledgeParticipant)
	if err != nil {
		return err
	}
	if err = fact.authorize(false); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if !scope.knowledgeSourceStopActive(loan) {
		return fault(f.Forbidden)
	}
	return nil
}

var _ oc.KnowledgeProjectSourceStopAuthority = (*KnowledgeProjectSourceStopAuthority)(nil)
