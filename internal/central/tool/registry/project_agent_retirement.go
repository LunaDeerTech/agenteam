package registry

import (
	"context"
	"slices"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// ProjectAgentRetirementReferences retires the current Agent index only.
// Historical Execution captures and the shared Tool definitions are untouched.
type ProjectAgentRetirementReferences struct {
	store Store
	owner ac.ProjectRetirementOwner
}

func NewProjectAgentRetirementReferences(store Store, owner ac.ProjectRetirementOwner) (*ProjectAgentRetirementReferences, error) {
	if nilPort(store) || nilPort(owner) {
		return nil, fail(f.DependencyUnbound)
	}
	return &ProjectAgentRetirementReferences{store, owner}, nil
}
func (*ProjectAgentRetirementReferences) ProjectRetirementReferenceKind() ac.ProjectRetirementReferenceKind {
	return ac.ProjectRetirementToolReferences
}
func (s *ProjectAgentRetirementReferences) ProjectRetirementLocks(c ac.ProjectRetirementChange) ([]f.LockRequest, error) {
	if s == nil || nilPort(s.store) || nilPort(s.owner) {
		return nil, fail(f.DependencyUnbound)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	locks := append(c.Request.RequiredLocks(), RegistryLock(f.Exclusive))
	for _, tool := range c.Before.Fields().AllowedToolIDs {
		locks = append(locks, toolLock(tool, f.Shared))
	}
	return normalizedLocks(locks)
}
func (s *ProjectAgentRetirementReferences) executor(ctx context.Context, tx f.Tx, c ac.ProjectRetirementChange) (postgres.SQLExecutor, error) {
	locks, err := s.ProjectRetirementLocks(c)
	if err != nil {
		return nil, err
	}
	if ctx == nil || !tx.Valid() {
		return nil, fail(f.InvalidArgument)
	}
	if err = ctx.Err(); err != nil {
		return nil, portError(err)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(x) {
		return nil, fail(f.DependencyUnbound)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, portError(err)
	}
	if err = s.owner.RequireProjectRetirementAppliedInTx(ctx, tx, c.Clone()); err != nil {
		return nil, portError(err)
	}
	return x, nil
}
func (s *ProjectAgentRetirementReferences) RetireProjectAgentReferencesInTx(ctx context.Context, tx f.Tx, c ac.ProjectRetirementChange) error {
	x, err := s.executor(ctx, tx, c)
	if err != nil {
		return err
	}
	r := c.Request
	version, ids, exists, err := loadAgentReferences(ctx, x, r.ProjectID, r.AgentID)
	if err != nil {
		return err
	}
	if !exists || version != r.ExpectedVersion || !slices.Equal(ids, c.Before.Fields().AllowedToolIDs) {
		return fail(f.ResourceBusy)
	}
	var history bool
	if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_tool.execution_configurations WHERE agent_id=$1)`, r.AgentID.String()).Scan(&history); err != nil {
		return portError(err)
	}
	if history {
		return fail(f.ResourceBusy)
	}
	if err = ctx.Err(); err != nil {
		return portError(err)
	}
	tag, err := x.Exec(ctx, `DELETE FROM agenteam_tool.agent_references WHERE agent_id=$1`, r.AgentID.String())
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != int64(len(ids)) {
		return fail(f.ResourceBusy)
	}
	tag, err = x.Exec(ctx, `DELETE FROM agenteam_tool.agent_configurations WHERE agent_id=$1 AND project_id=$2 AND config_version=$3`, r.AgentID.String(), r.ProjectID.String(), int64(r.ExpectedVersion))
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fail(f.ResourceBusy)
	}
	return portError(ctx.Err())
}
func (s *ProjectAgentRetirementReferences) RequireProjectAgentReferencesRetiredInTx(ctx context.Context, tx f.Tx, c ac.ProjectRetirementChange) error {
	x, err := s.executor(ctx, tx, c)
	if err != nil {
		return err
	}
	var remaining bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_tool.agent_configurations WHERE agent_id=$1) OR EXISTS(SELECT 1 FROM agenteam_tool.agent_references WHERE agent_id=$1) OR EXISTS(SELECT 1 FROM agenteam_tool.execution_configurations WHERE agent_id=$1)`, c.Request.AgentID.String()).Scan(&remaining)
	if err != nil {
		return portError(err)
	}
	if remaining {
		return fail(f.ResourceBusy)
	}
	return portError(ctx.Err())
}

var _ ac.ProjectRetirementReferences = (*ProjectAgentRetirementReferences)(nil)
