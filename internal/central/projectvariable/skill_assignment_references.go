package projectvariable

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

// SkillAssignmentReferences advances only the Agent's unchanged Secret IDs.
// It has no material/value/credential reader and cannot grant runtime access.
type SkillAssignmentReferences struct {
	store Store
	owner ac.SkillAssignmentVersionOwner
}

func NewSkillAssignmentReferences(store Store, owner ac.SkillAssignmentVersionOwner) (*SkillAssignmentReferences, error) {
	if nilPort(store) || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	return &SkillAssignmentReferences{store: store, owner: owner}, nil
}

func (*SkillAssignmentReferences) SkillAssignmentReferenceKind() ac.SkillAssignmentReferenceKind {
	return ac.SkillAssignmentSecretReferences
}

func (s *SkillAssignmentReferences) SkillAssignmentVersionLocks(change ac.SkillAssignmentVersionChange) ([]f.LockRequest, error) {
	if s == nil || nilPort(s.store) || nilPort(s.owner) {
		return nil, fault(f.DependencyUnbound)
	}
	if err := change.Validate(); err != nil {
		return nil, err
	}
	// The Agent EX protects its complete index. Project SH excludes all variable
	// writers; no credential mapping/material is read for a version-only advance.
	return oc.NormalizeLocks(change.Request.RequiredLocks())
}

func (s *SkillAssignmentReferences) AdvanceSkillAssignmentVersionInTx(ctx context.Context, tx f.Tx, change ac.SkillAssignmentVersionChange) error {
	locks, err := s.SkillAssignmentVersionLocks(change)
	if err != nil {
		return err
	}
	if ctx == nil || !tx.Valid() {
		return fault(f.InvalidArgument)
	}
	if err = ctx.Err(); err != nil {
		return portError(err)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if nilPort(x) {
		return fault(f.DependencyUnbound)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	if err = s.owner.RequireSkillAssignmentVersionAppliedInTx(ctx, tx, change.Clone()); err != nil {
		return portError(err)
	}
	r, expected := change.Request, change.Before.Fields().AllowedSecretVariableIDs
	var projects, ids []string
	var versions []int64
	// Agent IDs are global. Include foreign Project rows rather than hiding them
	// behind a scope filter; the extra bounded entry rejects oversized state.
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
	tag, err := x.Exec(ctx, `UPDATE agenteam_projectvariable.secret_references SET owner_version=$3 WHERE owner_kind='agent' AND owner_id=$1 AND project_id=$2 AND owner_version=$4`, r.AgentID.String(), r.ProjectID.String(), int64(change.ResultVersion), int64(r.ExpectedVersion))
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != int64(len(expected)) {
		return fault(f.ResourceBusy)
	}
	return portError(ctx.Err())
}

var _ ac.SkillAssignmentVersionReferences = (*SkillAssignmentReferences)(nil)
