package mount

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// This first retirement path supports the actual empty initialized index.
// Managed Mounts, Runner workspaces and historical uses remain their owners'
// responsibility; no filesystem operation or resource deletion is attempted.
type ProjectAgentRetirementReferences struct {
	store Store
	owner ac.ProjectRetirementOwner
}

func NewProjectAgentRetirementReferences(store Store, owner ac.ProjectRetirementOwner) (*ProjectAgentRetirementReferences, error) {
	if nilPort(store) || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ProjectAgentRetirementReferences{store, owner}, nil
}
func (*ProjectAgentRetirementReferences) ProjectRetirementReferenceKind() ac.ProjectRetirementReferenceKind {
	return ac.ProjectRetirementMountReferences
}
func (s *ProjectAgentRetirementReferences) ProjectRetirementLocks(c ac.ProjectRetirementChange) ([]f.LockRequest, error) {
	if s == nil || nilPort(s.store) || nilPort(s.owner) {
		return nil, fault(f.DependencyUnbound)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c.Request.RequiredLocks(), nil
}
func (s *ProjectAgentRetirementReferences) executor(ctx context.Context, tx f.Tx, c ac.ProjectRetirementChange) (postgres.SQLExecutor, error) {
	locks, err := s.ProjectRetirementLocks(c)
	if err != nil {
		return nil, err
	}
	if ctx == nil || !tx.Valid() {
		return nil, fault(f.InvalidArgument)
	}
	if err = ctx.Err(); err != nil {
		return nil, portError(err)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(x) {
		return nil, fault(f.DependencyUnbound)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, portError(err)
	}
	if err = s.owner.RequireProjectRetirementAppliedInTx(ctx, tx, c.Clone()); err != nil {
		return nil, portError(err)
	}
	return x, nil
}
func noProjectAgentMountResources(ctx context.Context, x postgres.SQLExecutor, c ac.ProjectRetirementChange) error {
	var resources bool
	err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mount.mounts WHERE agent_id=$1::text::uuid) OR EXISTS(SELECT 1 FROM agenteam_mount.runner_workspaces WHERE agent_id=$1::text::uuid) OR EXISTS(SELECT 1 FROM agenteam_mount.execution_captures WHERE agent_id=$1::text) OR EXISTS(SELECT 1 FROM agenteam_mount.execution_uses WHERE agent_id=$1::text)`, c.Request.AgentID.String()).Scan(&resources)
	if err != nil {
		return portError(err)
	}
	if resources || len(c.Before.Fields().AllowedMountIDs) != 0 {
		return fault(f.ResourceBusy)
	}
	return portError(ctx.Err())
}
func (s *ProjectAgentRetirementReferences) RetireProjectAgentReferencesInTx(ctx context.Context, tx f.Tx, c ac.ProjectRetirementChange) error {
	x, err := s.executor(ctx, tx, c)
	if err != nil {
		return err
	}
	r := c.Request
	before, err := loadReferences(ctx, x, r.ProjectID, r.AgentID)
	if err != nil {
		return err
	}
	if !before.exists || before.version != r.ExpectedVersion || len(before.ids) != 0 {
		return fault(f.ResourceBusy)
	}
	if err = noProjectAgentMountResources(ctx, x, c); err != nil {
		return err
	}
	tag, err := x.Exec(ctx, `DELETE FROM agenteam_mount.agent_mount_heads WHERE agent_id=$1 AND project_id=$2 AND owner_version=$3`, r.AgentID.String(), r.ProjectID.String(), int64(r.ExpectedVersion))
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ResourceBusy)
	}
	return portError(ctx.Err())
}
func (s *ProjectAgentRetirementReferences) RequireProjectAgentReferencesRetiredInTx(ctx context.Context, tx f.Tx, c ac.ProjectRetirementChange) error {
	x, err := s.executor(ctx, tx, c)
	if err != nil {
		return err
	}
	if err = noProjectAgentMountResources(ctx, x, c); err != nil {
		return err
	}
	var remaining bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mount.agent_mount_heads WHERE agent_id=$1) OR EXISTS(SELECT 1 FROM agenteam_mount.agent_mount_refs WHERE agent_id=$1)`, c.Request.AgentID.String()).Scan(&remaining)
	if err != nil {
		return portError(err)
	}
	if remaining {
		return fault(f.ResourceBusy)
	}
	return portError(ctx.Err())
}

var _ ac.ProjectRetirementReferences = (*ProjectAgentRetirementReferences)(nil)
