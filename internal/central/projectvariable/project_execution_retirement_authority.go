package projectvariable

import (
	"context"
	"sync/atomic"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// This separate bridge cannot substitute old terminal or Agent retirement
// authority for the original Execution project-cleanup Prepared scope.
type ProjectExecutionRetirementAuthority struct {
	data func() *projectExecutionRetirementAuthorityState
}
type projectExecutionRetirementAuthorityState struct {
	store Store
	owner ec.ProjectExecutionRetirementOwner
}

func NewProjectExecutionRetirementAuthority(original *Authority, owner ec.ProjectExecutionRetirementOwner) (*ProjectExecutionRetirementAuthority, error) {
	if original.state() == nil || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	s := &projectExecutionRetirementAuthorityState{original.state().store, owner}
	return &ProjectExecutionRetirementAuthority{func() *projectExecutionRetirementAuthorityState { return s }}, nil
}
func (a *ProjectExecutionRetirementAuthority) state() *projectExecutionRetirementAuthorityState {
	if a == nil || a.data == nil {
		return nil
	}
	return a.data()
}

type projectExecutionRetirementStage uint8

const (
	projectExecutionDiscover projectExecutionRetirementStage = iota
	projectExecutionRetire
	projectExecutionObserve
)

type projectExecutionRetirementWitnessKey struct{}
type projectExecutionRetirementWitness struct {
	issuer *projectExecutionRetirementAuthorityState
	plan   *projectExecutionVariablePlan
	tx     f.Tx
	stage  projectExecutionRetirementStage
	live   atomic.Bool
}

func (a *ProjectExecutionRetirementAuthority) check(ctx context.Context, tx f.Tx, r sc.ProjectExecutionLeaseRetirementRequest, stage projectExecutionRetirementStage) error {
	if err := environmentContext(ctx); err != nil {
		return err
	}
	s := a.state()
	if s == nil {
		return fault(f.DependencyUnbound)
	}
	w, ok := ctx.Value(projectExecutionRetirementWitnessKey{}).(*projectExecutionRetirementWitness)
	if !ok || w == nil || !w.live.Load() || w.issuer != s || w.plan == nil || w.tx != tx || w.stage != stage || r.Validate() != nil || !w.plan.environment.Equal(r) {
		return fault(f.Forbidden)
	}
	x, err := s.store.InTx(tx)
	if err != nil || nilPort(x) {
		return fault(f.Forbidden)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, w.plan.locks); err != nil {
		return environmentError(err)
	}
	if stage == projectExecutionObserve {
		err = s.owner.RequireProjectExecutionRetirementAppliedInTx(ctx, tx, w.plan.request)
	} else {
		err = s.owner.RequireProjectExecutionRetirementPreparedInTx(ctx, tx, w.plan.request)
	}
	if err != nil {
		return environmentError(err)
	}
	return environmentContext(ctx)
}
func (a *ProjectExecutionRetirementAuthority) CheckProjectExecutionLeaseDiscoveryInTx(ctx context.Context, tx f.Tx, r sc.ProjectExecutionLeaseRetirementRequest) error {
	return a.check(ctx, tx, r, projectExecutionDiscover)
}
func (a *ProjectExecutionRetirementAuthority) CheckProjectExecutionLeaseRetirementInTx(ctx context.Context, tx f.Tx, r sc.ProjectExecutionLeaseRetirementRequest) error {
	return a.check(ctx, tx, r, projectExecutionRetire)
}
func (a *ProjectExecutionRetirementAuthority) CheckProjectExecutionLeasesRetiredInTx(ctx context.Context, tx f.Tx, r sc.ProjectExecutionLeaseRetirementRequest) error {
	return a.check(ctx, tx, r, projectExecutionObserve)
}

func (p *ProjectExecutionRetirementReferences) withLeaseScope(ctx context.Context, tx f.Tx, plan *projectExecutionVariablePlan, stage projectExecutionRetirementStage, fn func(context.Context) error) error {
	w := &projectExecutionRetirementWitness{issuer: p.authority.state(), plan: plan, tx: tx, stage: stage}
	w.live.Store(true)
	defer w.live.Store(false)
	return fn(context.WithValue(ctx, projectExecutionRetirementWitnessKey{}, w))
}

var _ sc.ProjectExecutionLeaseRetirementAuthority = (*ProjectExecutionRetirementAuthority)(nil)
