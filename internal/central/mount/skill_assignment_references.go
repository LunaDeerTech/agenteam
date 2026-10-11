package mount

import (
	"context"
	"slices"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

// SkillAssignmentReferences changes neither Mount definitions, Runner mappings
// nor the Agent's selected IDs. It advances only the complete index's version.
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
	return ac.SkillAssignmentMountReferences
}

func (s *SkillAssignmentReferences) SkillAssignmentVersionLocks(change ac.SkillAssignmentVersionChange) ([]f.LockRequest, error) {
	if s == nil || nilPort(s.store) || nilPort(s.owner) {
		return nil, fault(f.DependencyUnbound)
	}
	if err := change.Validate(); err != nil {
		return nil, err
	}
	// All Mount configuration writers hold the same Agent EX; no late per-Mount
	// lock or Runner resolution is needed to retain this exact reference set.
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
	r := change.Request
	before, err := loadReferences(ctx, x, r.ProjectID, r.AgentID)
	if err != nil {
		return err
	}
	if !before.exists || before.version != r.ExpectedVersion || !slices.Equal(before.ids, change.Before.Fields().AllowedMountIDs) {
		return fault(f.VersionConflict)
	}
	tag, err := x.Exec(ctx, `UPDATE agenteam_mount.agent_mount_heads SET owner_version=$3 WHERE agent_id=$1 AND project_id=$2 AND owner_version=$4`, r.AgentID.String(), r.ProjectID.String(), int64(change.ResultVersion), int64(r.ExpectedVersion))
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.VersionConflict)
	}
	return portError(ctx.Err())
}

var _ ac.SkillAssignmentVersionReferences = (*SkillAssignmentReferences)(nil)
