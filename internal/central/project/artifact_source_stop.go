package project

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// ArtifactSourceStopAuthority consumes the original stop driver's private
// invoke scope. Binding the original LifecycleAuthority avoids a mutable
// registration cycle; each loan still belongs to exactly one actual driver,
// current claim and invocation, never to a public Actor or Cause DTO.
type ArtifactSourceStopAuthority struct{ authority *LifecycleAuthority }

func NewArtifactSourceStopAuthority(authority *LifecycleAuthority, executorStore Store) (*ArtifactSourceStopAuthority, error) {
	if !authority.bound() || nilPort(executorStore) || !sameStore(authority.store, executorStore) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ArtifactSourceStopAuthority{authority: authority}, nil
}

type artifactSourceStopLoanKey struct{}
type artifactSourceStopLoan struct {
	scope   *agentPreparationScope
	request oc.ProjectSourceStopRequest
}

func (s *agentPreparationScope) borrowArtifactSourceStop(ctx context.Context, request oc.ProjectSourceStopRequest) (*artifactSourceStopLoan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active || s.ctx.Err() != nil {
		return nil, fault(f.InvalidState)
	}
	if ctx.Value(agentPreparationLoanKey{}) != nil || ctx.Value(schedulerStopLoanKey{}) != nil || ctx.Value(executionStopLoanKey{}) != nil || ctx.Value(artifactSourceStopLoanKey{}) != nil || ctx.Value(workStopLoanKey{}) != nil || ctx.Value(usageStopLoanKey{}) != nil || ctx.Value(knowledgeStopLoanKey{}) != nil || ctx.Value(knowledgeSourceStopLoanKey{}) != nil {
		return nil, fault(f.Forbidden)
	}
	if s.artifactSources == nil {
		s.artifactSources = make(map[*artifactSourceStopLoan]struct{})
	}
	loan := &artifactSourceStopLoan{scope: s, request: request}
	s.artifactSources[loan] = struct{}{}
	return loan, nil
}
func (s *agentPreparationScope) releaseArtifactSourceStop(loan *artifactSourceStopLoan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.artifactSources, loan)
	close(s.changed)
	s.changed = make(chan struct{})
}
func (s *agentPreparationScope) artifactSourceStopActive(loan *artifactSourceStopLoan) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, present := s.artifactSources[loan]
	return present && loan.scope == s && s.active && s.ctx.Err() == nil
}

func (a *ArtifactSourceStopAuthority) sourceScope(ctx context.Context, request oc.ProjectSourceStopRequest) (*agentPreparationScope, error) {
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
	return scope, nil
}

func (a *ArtifactSourceStopAuthority) WithProjectSourceStop(ctx context.Context, request oc.ProjectSourceStopRequest, fn func(context.Context) error) error {
	if fn == nil {
		return invalid()
	}
	scope, err := a.sourceScope(ctx, request)
	if err != nil {
		return err
	}
	loan, err := scope.borrowArtifactSourceStop(ctx, request)
	if err != nil {
		return err
	}
	defer scope.releaseArtifactSourceStop(loan)
	run, cancel := context.WithCancel(ctx)
	stopCancel := context.AfterFunc(scope.ctx, cancel)
	defer func() { stopCancel(); cancel() }()
	run = context.WithValue(run, artifactSourceStopLoanKey{}, loan)
	d := request.Details().Cause.Details()
	txCause, err := f.NewRecoveryCause("artifact-source-stop", d.OperationID.String(), request.Details().Lease.ID().String())
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
		if err := a.RequireProjectSourceStopInTx(ctx, tx, request); err != nil {
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
	if !scope.artifactSourceStopActive(loan) {
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
	if !scope.artifactSourceStopActive(loan) {
		return fault(f.InvalidState)
	}
	return nil
}

func (a *ArtifactSourceStopAuthority) RequireProjectSourceStopInTx(ctx context.Context, tx f.Tx, request oc.ProjectSourceStopRequest) error {
	if !tx.Valid() {
		return invalid()
	}
	scope, err := a.sourceScope(ctx, request)
	if err != nil {
		return err
	}
	loan, ok := ctx.Value(artifactSourceStopLoanKey{}).(*artifactSourceStopLoan)
	if !ok || loan == nil || loan.scope != scope || !loan.request.Equal(request) || !scope.artifactSourceStopActive(loan) {
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
	// Unlike a generic stop observation, only the still-active Artifact/Object
	// participant may issue source convergence within this stopping operation.
	fact, err := a.authority.lifecycleFact(ctx, x, scope.claim.project, cause, c.ArtifactObjectParticipant)
	if err != nil {
		return err
	}
	if err = fact.authorize(false); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if !scope.artifactSourceStopActive(loan) {
		return fault(f.Forbidden)
	}
	return nil
}

var _ oc.ProjectSourceStopAuthority = (*ArtifactSourceStopAuthority)(nil)
