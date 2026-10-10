package projectvariable

import (
	"context"
	"errors"
	"sync/atomic"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// EnvironmentAuthority is constructed before the dedicated D04 lease provider.
// Its private context witnesses break the construction dependency without a
// mutable Bind method or a grant minted from a public request.
type EnvironmentAuthority struct {
	data func() *environmentAuthorityState
}
type environmentAuthorityState struct {
	store Store
	owner c.ExecutionEnvironmentAuthority
}

func NewEnvironmentAuthority(original *Authority, owner c.ExecutionEnvironmentAuthority) (*EnvironmentAuthority, error) {
	if original.state() == nil || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	s := &environmentAuthorityState{original.state().store, owner}
	return &EnvironmentAuthority{func() *environmentAuthorityState { return s }}, nil
}
func (a *EnvironmentAuthority) state() *environmentAuthorityState {
	if a == nil || a.data == nil {
		return nil
	}
	return a.data()
}

type environmentLeaseWitnessKey struct{}
type environmentLeaseWitness struct {
	issuer    *environmentAuthorityState
	candidate *environmentCandidate
	tx        f.Tx
	final     bool
	live      atomic.Bool
}

func sameEnvironmentLeaseRequest(a, b sc.ProjectVariableLeaseRequest) bool {
	return a.ProjectID == b.ProjectID && a.AgentID == b.AgentID && a.ExecutionID == b.ExecutionID && a.VariableID == b.VariableID && a.VariableVersion == b.VariableVersion && a.AgentVersion == b.AgentVersion && a.CredentialVersion == b.CredentialVersion && a.AttemptBinding == b.AttemptBinding && a.Ref.Equal(b.Ref)
}
func environmentError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return portError(err)
}
func environmentContext(ctx context.Context) error {
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	return environmentError(ctx.Err())
}
func (a *EnvironmentAuthority) leaseWitness(ctx context.Context, r sc.ProjectVariableLeaseRequest, final bool) (*environmentLeaseWitness, error) {
	if err := environmentContext(ctx); err != nil {
		return nil, err
	}
	s := a.state()
	if s == nil {
		return nil, fault(f.DependencyUnbound)
	}
	w, ok := ctx.Value(environmentLeaseWitnessKey{}).(*environmentLeaseWitness)
	if !ok || w == nil || !w.live.Load() || w.issuer != s || w.final != final || w.candidate == nil || r.Validate() != nil {
		return nil, fault(f.Forbidden)
	}
	for _, request := range w.candidate.requests {
		if sameEnvironmentLeaseRequest(request, r) {
			return w, nil
		}
	}
	return nil, fault(f.Forbidden)
}
func (a *EnvironmentAuthority) CheckProjectVariableLeasePlan(ctx context.Context, r sc.ProjectVariableLeaseRequest) error {
	_, err := a.leaseWitness(ctx, r, false)
	return err
}
func (a *EnvironmentAuthority) CheckProjectVariableLeaseInTx(ctx context.Context, tx f.Tx, r sc.ProjectVariableLeaseRequest) error {
	w, err := a.leaseWitness(ctx, r, true)
	if err != nil {
		return err
	}
	// Foundation Tx is an opaque comparable original transaction token.
	if tx != w.tx {
		return fault(f.Forbidden)
	}
	x, err := w.issuer.store.InTx(tx)
	if err != nil || nilPort(x) {
		return fault(f.Forbidden)
	}
	if err = w.issuer.store.RequireHeldLocks(ctx, tx, w.candidate.locks); err != nil {
		return environmentError(err)
	}
	return environmentContext(ctx)
}

var _ sc.ProjectVariableLeaseAuthority = (*EnvironmentAuthority)(nil)
