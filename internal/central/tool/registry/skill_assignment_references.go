package registry

import (
	"context"
	"slices"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// SkillAssignmentReferences advances only this domain's unchanged Agent index.
// The injected canonical Agent owner must supply its live same-Tx write witness.
type SkillAssignmentReferences struct {
	store Store
	owner ac.SkillAssignmentVersionOwner
}

func NewSkillAssignmentReferences(store Store, owner ac.SkillAssignmentVersionOwner) (*SkillAssignmentReferences, error) {
	if nilPort(store) || nilPort(owner) {
		return nil, fail(f.DependencyUnbound)
	}
	return &SkillAssignmentReferences{store: store, owner: owner}, nil
}

func (*SkillAssignmentReferences) SkillAssignmentReferenceKind() ac.SkillAssignmentReferenceKind {
	return ac.SkillAssignmentToolReferences
}

func (s *SkillAssignmentReferences) SkillAssignmentVersionLocks(change ac.SkillAssignmentVersionChange) ([]f.LockRequest, error) {
	if s == nil || nilPort(s.store) || nilPort(s.owner) {
		return nil, fail(f.DependencyUnbound)
	}
	if err := change.Validate(); err != nil {
		return nil, err
	}
	locks := append(change.Request.RequiredLocks(), RegistryLock(f.Shared))
	for _, tool := range change.Before.Fields().AllowedToolIDs {
		locks = append(locks, toolLock(tool, f.Shared))
	}
	return normalizedLocks(locks)
}

func (s *SkillAssignmentReferences) AdvanceSkillAssignmentVersionInTx(ctx context.Context, tx f.Tx, change ac.SkillAssignmentVersionChange) error {
	locks, err := s.SkillAssignmentVersionLocks(change)
	if err != nil {
		return err
	}
	if ctx == nil || !tx.Valid() {
		return fail(f.InvalidArgument)
	}
	if err = ctx.Err(); err != nil {
		return portError(err)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if nilPort(x) {
		return fail(f.DependencyUnbound)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	if err = s.owner.RequireSkillAssignmentVersionAppliedInTx(ctx, tx, change.Clone()); err != nil {
		return portError(err)
	}
	r := change.Request
	version, before, exists, err := loadAgentReferences(ctx, x, r.ProjectID, r.AgentID)
	if err != nil {
		return err
	}
	if !exists || version != r.ExpectedVersion || !slices.Equal(before, change.Before.Fields().AllowedToolIDs) {
		return fail(f.VersionConflict)
	}
	// Do not resolve the Tool directory or replace references for a Skill edit.
	tag, err := x.Exec(ctx, `UPDATE agenteam_tool.agent_configurations SET config_version=$3 WHERE agent_id=$1 AND project_id=$2 AND config_version=$4`, r.AgentID.String(), r.ProjectID.String(), int64(change.ResultVersion), int64(r.ExpectedVersion))
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fail(f.VersionConflict)
	}
	return portError(ctx.Err())
}

var _ ac.SkillAssignmentVersionReferences = (*SkillAssignmentReferences)(nil)
