package project

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// Each provider transaction consumes the current private Variables driver
// scope. It cannot borrow an Agent/Skills callback or a saved public DTO.
func (a *Authority) RequireProjectVariablesCleanupInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause c.LifecycleCause, project i.ProjectID) error {
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
	if err = s.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Exclusive)}); err != nil {
		return portError(err)
	}
	if err = lifecycle.validateSkillsCleanupFactsInTx(ctx, tx, project, cause); err != nil {
		return err
	}
	if err = lifecycle.requireVariablesCleanupClaimInTx(ctx, tx, cause, project); err != nil {
		return err
	}
	return portError(ctx.Err())
}

var _ vc.ProjectCleanupAuthority = (*Authority)(nil)
