package project

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Discovery and restoration use this same original Project authority. A
// public Service actor plus a deleting row is insufficient: only the active
// Agent substep of its fixed cleanup driver supplies the private claim scope.
func (a *Authority) RequireAgentRetirementScopeInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause c.LifecycleCause, project i.ProjectID) error {
	if ctx == nil || !tx.Valid() || actor.Validate() != nil || cause.Validate() != nil || project.Validate() != nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return portError(err)
	}
	d := actor.Details()
	if cause.Action != c.Delete || d.Kind != i.Service || d.ServiceName != i.ProjectLifecycle || d.ProjectID != project.String() || d.CauseRef != cause.OperationID.String() {
		return fault(f.Forbidden)
	}
	s := a.state()
	if s == nil {
		return fault(f.DependencyUnbound)
	}
	lifecycle, ok := s.lifecycle.(*LifecycleAuthority)
	if !ok || !lifecycle.bound() || !sameStore(s.store, lifecycle.store) {
		return fault(f.DependencyUnbound)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if nilPort(x) {
		return fault(f.DependencyUnbound)
	}
	if err := s.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Exclusive)}); err != nil {
		return portError(err)
	}
	// Retain the complete current Project/operation/manifest and domains gate.
	// The private check additionally distinguishes Agent from the Skills call.
	if err := lifecycle.validateSkillsCleanupFactsInTx(ctx, tx, project, cause); err != nil {
		return err
	}
	if err := lifecycle.requireAgentCleanupStepInTx(ctx, tx, cause, project); err != nil {
		return err
	}
	return portError(ctx.Err())
}

func (a *Authority) RequireAgentRetirementInTx(ctx context.Context, tx f.Tx, request ac.ProjectRetirementRequest) error {
	if err := request.Validate(); err != nil {
		return portError(err)
	}
	if ctx == nil || !tx.Valid() {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return portError(err)
	}
	s := a.state()
	if s == nil {
		return fault(f.DependencyUnbound)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if nilPort(x) {
		return fault(f.DependencyUnbound)
	}
	// A targeted call requires its complete union before the first canonical
	// query. Discovery has only the Project target and uses the scope entry.
	if err := s.store.RequireHeldLocks(ctx, tx, request.RequiredLocks()); err != nil {
		return portError(err)
	}
	return a.RequireAgentRetirementScopeInTx(ctx, tx, request.Actor, request.Cause, request.ProjectID)
}

// Recovery requires the exact persisted applied anchor loaded by this active
// Agent round. Neither missing rows nor a caller's reconstructed plan can
// replace the original atomic Project checkpoint and immutable Agent command.
func (a *Authority) RequireAgentRetirementAppliedInTx(ctx context.Context, tx f.Tx, plan ac.ProjectRetirementPlan) error {
	if err := plan.Validate(); err != nil {
		return portError(err)
	}
	if err := a.RequireAgentRetirementInTx(ctx, tx, plan.Change.Request); err != nil {
		return err
	}
	lifecycle := a.state().lifecycle.(*LifecycleAuthority)
	if err := lifecycle.requireAgentCleanupAppliedInTx(ctx, tx, plan); err != nil {
		return err
	}
	return portError(ctx.Err())
}
