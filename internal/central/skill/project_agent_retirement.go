package skill

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// Only an actual disabled initialization head with no assignments or runtime
// history can retire here. Existing immutable-grant triggers remain intact;
// enabled initial grants and later commands require their own future cleanup.
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
	return ac.ProjectRetirementSkillReferences
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
		return nil, invalid()
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
func projectAgentSkillsNoHistory(ctx context.Context, x postgres.SQLExecutor, c ac.ProjectRetirementChange) error {
	var history bool
	err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_skill.agent_assignments WHERE agent_id=$1) OR EXISTS(SELECT 1 FROM agenteam_skill.execution_binding_heads WHERE agent_id=$1) OR EXISTS(SELECT 1 FROM agenteam_skill.execution_bindings WHERE agent_id=$1) OR EXISTS(SELECT 1 FROM agenteam_skill.assignment_commands WHERE agent_id=$1) OR EXISTS(SELECT 1 FROM agenteam_skill.assignment_changes WHERE agent_id=$1) OR EXISTS(SELECT 1 FROM agenteam_skill.assignment_current WHERE agent_id=$1) OR EXISTS(SELECT 1 FROM agenteam_skill.assignment_pending_refs WHERE agent_id=$1)`, c.Request.AgentID.String()).Scan(&history)
	if err != nil {
		return unavailable(err)
	}
	if history {
		return fault(f.ResourceBusy)
	}
	return portError(ctx.Err())
}
func (s *ProjectAgentRetirementReferences) RetireProjectAgentReferencesInTx(ctx context.Context, tx f.Tx, c ac.ProjectRetirementChange) error {
	x, err := s.executor(ctx, tx, c)
	if err != nil {
		return err
	}
	if err = projectAgentSkillsNoHistory(ctx, x, c); err != nil {
		return err
	}
	r := c.Request
	// Inspect the globally unique Agent head before the scoped complete reader;
	// a foreign Project head cannot disappear behind a Project predicate.
	var project string
	if err = x.QueryRow(ctx, `SELECT project_id::text FROM agenteam_skill.agent_assignment_heads WHERE agent_id=$1`, r.AgentID.String()).Scan(&project); err != nil {
		return unavailable(err)
	}
	if project != r.ProjectID.String() {
		return fault(f.ResourceBusy)
	}
	before, err := loadAgentInitialization(ctx, x, r.ProjectID, r.AgentID)
	if err != nil {
		return err
	}
	if before == nil || before.enabled || before.assignment != nil || before.sequence != 1 {
		return fault(f.ResourceBusy)
	}
	if err = ctx.Err(); err != nil {
		return portError(err)
	}
	tag, err := x.Exec(ctx, `DELETE FROM agenteam_skill.agent_assignment_heads WHERE project_id=$1 AND agent_id=$2 AND creation_command=$3 AND plan_revision=$4 AND request_binding=$5 AND NOT add_skills_enabled AND assignment_sequence=1 AND initial_assignment_id IS NULL`, r.ProjectID.String(), r.AgentID.String(), before.command, int64(before.planRevision), string(before.binding))
	if err != nil {
		return unavailable(err)
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
	if err = projectAgentSkillsNoHistory(ctx, x, c); err != nil {
		return err
	}
	var remaining bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_skill.agent_assignment_heads WHERE agent_id=$1)`, c.Request.AgentID.String()).Scan(&remaining)
	if err != nil {
		return unavailable(err)
	}
	if remaining {
		return fault(f.ResourceBusy)
	}
	return portError(ctx.Err())
}

var _ ac.ProjectRetirementReferences = (*ProjectAgentRetirementReferences)(nil)
