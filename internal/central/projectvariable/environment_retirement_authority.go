package projectvariable

import (
	"context"
	"sync/atomic"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// EnvironmentRetirementAuthority bridges the real Execution owner and Secret
// without a public Bind or a grant made from an EnvironmentCapture value.
type EnvironmentRetirementAuthority struct {
	data func() *environmentRetirementAuthorityState
}
type environmentRetirementAuthorityState struct {
	store Store
	owner c.ExecutionEnvironmentRetirementAuthority
}

func NewEnvironmentRetirementAuthority(original *Authority, owner c.ExecutionEnvironmentRetirementAuthority) (*EnvironmentRetirementAuthority, error) {
	if original.state() == nil || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	s := &environmentRetirementAuthorityState{original.state().store, owner}
	return &EnvironmentRetirementAuthority{func() *environmentRetirementAuthorityState { return s }}, nil
}
func (a *EnvironmentRetirementAuthority) state() *environmentRetirementAuthorityState {
	if a == nil || a.data == nil {
		return nil
	}
	return a.data()
}

type environmentRetirementStage uint8

const (
	environmentRetirementPlanning environmentRetirementStage = iota
	environmentRetirementWriting
	environmentRetirementObserving
)

type environmentRetirementWitnessKey struct{}
type environmentRetirementWitness struct {
	issuer    *environmentRetirementAuthorityState
	candidate *environmentRetirementCandidate
	tx        f.Tx
	stage     environmentRetirementStage
	live      atomic.Bool
}

func sameEnvironmentRetirementRequest(a, b sc.ProjectVariableLeaseRetirementRequest) bool {
	return a.ProjectID == b.ProjectID && a.AgentID == b.AgentID && a.ExecutionID == b.ExecutionID && a.VariableID == b.VariableID && a.VariableVersion == b.VariableVersion && a.AgentVersion == b.AgentVersion && a.AttemptBinding == b.AttemptBinding && a.LeaseID == b.LeaseID && a.Ref.Equal(b.Ref)
}
func (a *EnvironmentRetirementAuthority) retirementWitness(ctx context.Context, tx f.Tx, r sc.ProjectVariableLeaseRetirementRequest, stage environmentRetirementStage) error {
	if err := environmentContext(ctx); err != nil {
		return err
	}
	s := a.state()
	if s == nil {
		return fault(f.DependencyUnbound)
	}
	w, ok := ctx.Value(environmentRetirementWitnessKey{}).(*environmentRetirementWitness)
	if !ok || w == nil || !w.live.Load() || w.issuer != s || w.stage != stage || w.candidate == nil || r.Validate() != nil {
		return fault(f.Forbidden)
	}
	found := false
	for _, request := range w.candidate.requests {
		if sameEnvironmentRetirementRequest(request, r) {
			found = true
			break
		}
	}
	if !found {
		return fault(f.Forbidden)
	}
	if stage != environmentRetirementPlanning {
		if tx != w.tx {
			return fault(f.Forbidden)
		}
		x, err := s.store.InTx(tx)
		if err != nil || nilPort(x) {
			return fault(f.Forbidden)
		}
		if err = s.store.RequireHeldLocks(ctx, tx, w.candidate.locks); err != nil {
			return environmentError(err)
		}
	}
	return environmentContext(ctx)
}
func (a *EnvironmentRetirementAuthority) CheckProjectVariableLeaseRetirementPlan(ctx context.Context, r sc.ProjectVariableLeaseRetirementRequest) error {
	return a.retirementWitness(ctx, f.Tx{}, r, environmentRetirementPlanning)
}
func (a *EnvironmentRetirementAuthority) CheckProjectVariableLeaseRetirementInTx(ctx context.Context, tx f.Tx, r sc.ProjectVariableLeaseRetirementRequest) error {
	return a.retirementWitness(ctx, tx, r, environmentRetirementWriting)
}
func (a *EnvironmentRetirementAuthority) CheckProjectVariableLeaseRetiredInTx(ctx context.Context, tx f.Tx, r sc.ProjectVariableLeaseRetirementRequest) error {
	return a.retirementWitness(ctx, tx, r, environmentRetirementObserving)
}

var _ sc.ProjectVariableLeaseRetirementAuthority = (*EnvironmentRetirementAuthority)(nil)
