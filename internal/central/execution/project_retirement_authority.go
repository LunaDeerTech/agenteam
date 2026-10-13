package execution

import (
	"context"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func (a *Authority) retirementWitnessInTx(ctx context.Context, tx f.Tx, request ec.ProjectExecutionRetirementRequest, applied bool) (*executionRetirementWitness, error) {
	if a == nil || a.state == nil {
		return nil, fault(f.DependencyUnbound)
	}
	if ctx == nil || request.Validate() != nil || !tx.Valid() {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w, ok := ctx.Value(executionRetirementWitnessKey{}).(*executionRetirementWitness)
	if !ok || w == nil || !w.active.Load() || w.tx != tx || w.applied != applied || w.owner == nil || w.plan == nil || w.control == nil || w.owner.pair.service.deps.Authority != a || !sameProjectCallPort(w.owner.pair.store, a.state.store) {
		return nil, fault(f.Forbidden)
	}
	p := w.plan
	if p.owner != w.owner || p.control != w.control || !p.request.Actor.Equal(request.Actor) || p.request.Cause != request.Cause || p.request.Binding != request.Binding {
		return nil, fault(f.Forbidden)
	}
	// Discovery has not collected the resource locks yet. Its Prepared witness
	// covers the already held base locks. Each provider checks its extra locks
	// for mutation after the consumer acquires the complete final union.
	base, err := normalizeProjectCallLocks(request.Binding.ProjectID, []f.LockRequest{agentLock(request.Binding.AgentID, f.Exclusive), executionLock(request.Binding.ExecutionID)})
	if err != nil {
		return nil, err
	}
	if _, err = w.owner.requireTx(ctx, tx, w.control, base); err != nil {
		return nil, err
	}
	if applied && !p.commit.Matches(request) {
		return nil, fault(f.Forbidden)
	}
	return w, ctx.Err()
}

func (a *Authority) RequireProjectExecutionRetirementPreparedInTx(ctx context.Context, tx f.Tx, request ec.ProjectExecutionRetirementRequest) error {
	_, err := a.retirementWitnessInTx(ctx, tx, request, false)
	return err
}
func (a *Authority) RequireProjectExecutionRetirementAppliedInTx(ctx context.Context, tx f.Tx, request ec.ProjectExecutionRetirementRequest) error {
	_, err := a.retirementWitnessInTx(ctx, tx, request, true)
	return err
}
func (a *Authority) ProjectExecutionRetirementProofInTx(ctx context.Context, tx f.Tx, request ec.ProjectExecutionRetirementRequest) (ec.ProjectExecutionRetirementCommit, error) {
	w, err := a.retirementWitnessInTx(ctx, tx, request, true)
	if err != nil {
		return ec.ProjectExecutionRetirementCommit{}, err
	}
	return w.plan.commit, nil
}

var _ ec.ProjectExecutionRetirementOwner = (*Authority)(nil)
