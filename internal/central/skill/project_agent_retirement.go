package skill

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// Only an unchanged initialization head and its optional initial grant can
// retire here. Later assignments, commands and runtime history stay protected.
// The initial grant's narrow Project-only database exception is not authority;
// both shapes consume the same original private Applied witness below.
type ProjectAgentRetirementReferences struct {
	store   Store
	owner   ac.ProjectRetirementOwner
	ingress sc.ProjectAgentIngressRetirement
}

func NewProjectAgentRetirementReferences(store Store, owner ac.ProjectRetirementOwner) (*ProjectAgentRetirementReferences, error) {
	if nilPort(store) || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ProjectAgentRetirementReferences{store: store, owner: owner}, nil
}

// This explicit composition also retires completed assignment history whose
// original Execution owner confirms only no-active ingress. The old constructor
// retains its initialization-only contract when this dependency is absent.
func NewProjectAgentRetirementReferencesWithIngress(store Store, owner ac.ProjectRetirementOwner, ingress sc.ProjectAgentIngressRetirement) (*ProjectAgentRetirementReferences, error) {
	if nilPort(ingress) {
		return nil, fault(f.DependencyUnbound)
	}
	s, err := NewProjectAgentRetirementReferences(store, owner)
	if err != nil {
		return nil, err
	}
	s.ingress = ingress
	return s, nil
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
	return projectAgentSkillsHistory(ctx, x, c, false)
}
func projectAgentSkillsHistory(ctx context.Context, x postgres.SQLExecutor, c ac.ProjectRetirementChange, allowInitial bool) error {
	grant := `EXISTS(SELECT 1 FROM agenteam_skill.agent_assignments WHERE agent_id=$1)`
	if allowInitial {
		// No Project predicate may hide an extra or foreign Agent grant. Only
		// the exact unchanged initialization grant is excluded from history.
		grant = `EXISTS(SELECT 1 FROM agenteam_skill.agent_assignments a WHERE a.agent_id=$1 AND NOT EXISTS (
 SELECT 1 FROM agenteam_skill.agent_assignment_heads h WHERE h.project_id=a.project_id AND h.agent_id=a.agent_id
 AND h.add_skills_enabled AND h.assignment_sequence=1 AND h.initial_assignment_id=a.id
 AND h.initial_skill_id=a.skill_id AND h.created_at=a.created_at
 AND a.assignment_sequence=1 AND a.change_id IS NULL AND a.enabled AND a.removed_at IS NULL))`
	}
	var history bool
	err := x.QueryRow(ctx, `SELECT `+grant+` OR EXISTS(SELECT 1 FROM agenteam_skill.execution_binding_heads WHERE agent_id=$1) OR EXISTS(SELECT 1 FROM agenteam_skill.execution_bindings WHERE agent_id=$1) OR EXISTS(SELECT 1 FROM agenteam_skill.assignment_commands WHERE agent_id=$1) OR EXISTS(SELECT 1 FROM agenteam_skill.assignment_changes WHERE agent_id=$1) OR EXISTS(SELECT 1 FROM agenteam_skill.assignment_current WHERE agent_id=$1) OR EXISTS(SELECT 1 FROM agenteam_skill.assignment_pending_refs WHERE agent_id=$1) OR EXISTS(SELECT 1 FROM agenteam_skill.assignment_execution_retirement_proofs WHERE agent_id=$1)`, c.Request.AgentID.String()).Scan(&history)
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
	if s.ingress != nil {
		return s.retireAssignmentHistory(ctx, tx, x, c)
	}
	if err = projectAgentSkillsHistory(ctx, x, c, true); err != nil {
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
	if before == nil || before.sequence != 1 {
		return fault(f.ResourceBusy)
	}
	if err = ctx.Err(); err != nil {
		return portError(err)
	}
	headQuery := `DELETE FROM agenteam_skill.agent_assignment_heads WHERE project_id=$1 AND agent_id=$2 AND creation_command=$3 AND plan_revision=$4 AND request_binding=$5 AND NOT add_skills_enabled AND assignment_sequence=1 AND initial_assignment_id IS NULL`
	headArgs := []any{r.ProjectID.String(), r.AgentID.String(), before.command, int64(before.planRevision), string(before.binding)}
	if before.enabled {
		// The complete reader validated the head's original grant identity.
		// Delete while that head still exists for the database exception; both
		// directions of the initialization FK remain deferred to the same Tx.
		tag, err := x.Exec(ctx, `DELETE FROM agenteam_skill.agent_assignments WHERE project_id=$1 AND agent_id=$2 AND id=$3 AND skill_id=$4 AND created_at=$5 AND enabled AND assignment_sequence=1 AND change_id IS NULL AND removed_at IS NULL`, r.ProjectID.String(), r.AgentID.String(), before.assignment.String(), before.skill.String(), before.created.Time())
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.ResourceBusy)
		}
		if err = ctx.Err(); err != nil {
			return portError(err)
		}
		headQuery = `DELETE FROM agenteam_skill.agent_assignment_heads WHERE project_id=$1 AND agent_id=$2 AND creation_command=$3 AND plan_revision=$4 AND request_binding=$5 AND add_skills_enabled AND assignment_sequence=1 AND initial_assignment_id=$6`
		headArgs = append(headArgs, before.assignment.String())
	}
	tag, err := x.Exec(ctx, headQuery, headArgs...)
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
	if s.ingress != nil {
		if err = s.ingress.RequireProjectAgentIngressRetiredInTx(ctx, tx, c.Clone()); err != nil {
			return portError(err)
		}
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
