package project

import (
	"context"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func (a *Authority) checkMCPRuntimeProjectInTx(ctx context.Context, tx f.Tx, actor id.Actor, project id.ProjectID) error {
	if a == nil || a.state() == nil || ctx == nil || actor.Validate() != nil || project.Validate() != nil {
		return invalid()
	}
	d := actor.Details()
	if d.Kind != id.Service || d.ServiceName != id.MCPRuntime || d.ProjectID != project.String() {
		return fault(f.Forbidden)
	}
	if _, err := f.ParseID[struct{}](d.CauseRef); err != nil {
		return fault(f.Forbidden)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.state().store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Shared)}); err != nil {
		return unavailable(err)
	}
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return unavailable(err)
	}
	p, err := loadProject(ctx, x, project)
	if err != nil {
		return err
	}
	if p == nil {
		return fault(f.NotFound)
	}
	initialized := c.InitializationPending
	if p.initialized {
		initialized = c.Initialized
	}
	return c.CheckOwnerGate(p.ref.Lifecycle, initialized, id.Mutate)
}

func (a *Authority) RequireMCPRuntimeProjectInTx(ctx context.Context, tx f.Tx, actor id.Actor, project id.ProjectID) error {
	if err := a.checkMCPRuntimeProjectInTx(ctx, tx, actor, project); err != nil {
		return err
	}
	proof, ok := a.state().auditFacts[ac.MCPProducer].(c.MCPRuntimeProof)
	if !ok || nilPort(proof) {
		return fault(f.DependencyUnbound)
	}
	return portError(proof.RequireMCPRuntimeInTx(ctx, tx, actor, project))
}

var _ c.MCPRuntimeProjectAuthority = (*Authority)(nil)
