package projectvariable

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// Only current Agent Secret-variable references are retired. No value,
// credential, execution environment or runtime lease is read or released.
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
	return ac.ProjectRetirementSecretReferences
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
func (s *ProjectAgentRetirementReferences) RetireProjectAgentReferencesInTx(ctx context.Context, tx f.Tx, c ac.ProjectRetirementChange) error {
	x, err := s.executor(ctx, tx, c)
	if err != nil {
		return err
	}
	r, expected := c.Request, c.Before.Fields().AllowedSecretVariableIDs
	var projects, ids []string
	var versions []int64
	err = x.QueryRow(ctx, `SELECT COALESCE(array_agg(project_id::text ORDER BY project_id,variable_id),'{}'::text[]),COALESCE(array_agg(variable_id::text ORDER BY project_id,variable_id),'{}'::text[]),COALESCE(array_agg(owner_version ORDER BY project_id,variable_id),'{}'::bigint[]) FROM (SELECT project_id,variable_id,owner_version FROM agenteam_projectvariable.secret_references WHERE owner_kind='agent' AND owner_id=$1 ORDER BY project_id,variable_id LIMIT 129) AS owned`, r.AgentID.String()).Scan(&projects, &ids, &versions)
	if err != nil {
		return unavailable(err)
	}
	if len(ids) != len(expected) || len(projects) != len(ids) || len(versions) != len(ids) {
		return fault(f.ResourceBusy)
	}
	for n, id := range expected {
		if ids[n] != id.String() || projects[n] != r.ProjectID.String() || versions[n] != int64(r.ExpectedVersion) {
			return fault(f.ResourceBusy)
		}
	}
	var history bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_projectvariable.execution_environments WHERE agent_id=$1)`, r.AgentID.String()).Scan(&history)
	if err != nil {
		return unavailable(err)
	}
	if history {
		return fault(f.ResourceBusy)
	}
	if err = ctx.Err(); err != nil {
		return portError(err)
	}
	tag, err := x.Exec(ctx, `DELETE FROM agenteam_projectvariable.secret_references WHERE owner_kind='agent' AND owner_id=$1 AND project_id=$2 AND owner_version=$3`, r.AgentID.String(), r.ProjectID.String(), int64(r.ExpectedVersion))
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != int64(len(expected)) {
		return fault(f.ResourceBusy)
	}
	return portError(ctx.Err())
}
func (s *ProjectAgentRetirementReferences) RequireProjectAgentReferencesRetiredInTx(ctx context.Context, tx f.Tx, c ac.ProjectRetirementChange) error {
	x, err := s.executor(ctx, tx, c)
	if err != nil {
		return err
	}
	var remaining bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_projectvariable.secret_references WHERE owner_kind='agent' AND owner_id=$1) OR EXISTS(SELECT 1 FROM agenteam_projectvariable.execution_environments WHERE agent_id=$1)`, c.Request.AgentID.String()).Scan(&remaining)
	if err != nil {
		return unavailable(err)
	}
	if remaining {
		return fault(f.ResourceBusy)
	}
	return portError(ctx.Err())
}

var _ ac.ProjectRetirementReferences = (*ProjectAgentRetirementReferences)(nil)
