package project

import (
	"context"
	"errors"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// RequirePreparingProjectInTx supplies only current Project facts to
// execution/contract.PreparationProjectGate. The Execution owner must first
// verify its private preparing claim/fence and original transaction. This
// method grants no Actor or Execution authority and deliberately never calls
// back into Execution, Agent, or the launch caller's Human Session.
func (a *Authority) RequirePreparingProjectInTx(ctx context.Context, tx f.Tx, project i.ProjectID) (c.ProjectRef, error) {
	if a.state() == nil {
		return c.ProjectRef{}, fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() || project.Validate() != nil {
		return c.ProjectRef{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return c.ProjectRef{}, err
	}
	// InTx rejects foreign or retired handles before any Project read. The
	// caller already acquired the complete union; this gate cannot add locks.
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return c.ProjectRef{}, preparationProjectError(err)
	}
	if err = a.state().store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Shared)}); err != nil {
		return c.ProjectRef{}, preparationProjectError(err)
	}
	p, err := loadProject(ctx, x, project)
	// Do not publish facts after cancellation, and do not return before the
	// original SQL call actually returns even when its driver ignores cancel.
	if cancelled := ctx.Err(); cancelled != nil {
		return c.ProjectRef{}, cancelled
	}
	if err != nil {
		return c.ProjectRef{}, preparationProjectError(err)
	}
	if p == nil {
		return c.ProjectRef{}, fault(f.NotFound)
	}
	if p.ref.ID != project {
		return c.ProjectRef{}, unavailable(nil)
	}
	initialized := c.InitializationPending
	if p.initialized {
		initialized = c.Initialized
	}
	if err = c.CheckOwnerGate(p.ref.Lifecycle, initialized, i.Launch); err != nil {
		return c.ProjectRef{}, err
	}
	return p.ref, nil
}

func preparationProjectError(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return portError(err)
}

var _ ec.PreparationProjectGate = (*Authority)(nil)
