package model

import (
	"context"
	"slices"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

// SkillAssignmentReferences preserves the original Model/approval selection,
// including reasoning effort. It neither resolves nor leases a Model.
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
	return ac.SkillAssignmentModelReferences
}

func (s *SkillAssignmentReferences) SkillAssignmentVersionLocks(change ac.SkillAssignmentVersionChange) ([]f.LockRequest, error) {
	if s == nil || nilPort(s.store) || nilPort(s.owner) {
		return nil, fault(f.DependencyUnbound)
	}
	if err := change.Validate(); err != nil {
		return nil, err
	}
	registry, _ := f.SystemConfigLock("model-references")
	index, _ := f.RecordLock(f.ReferenceRecordLock, "model:agent:"+change.Request.AgentID.String())
	locks := append(change.Request.RequiredLocks(), f.LockRequest{Key: registry, Mode: f.Shared}, f.LockRequest{Key: index, Mode: f.Exclusive})
	core := change.Before.Fields().Core
	model, _ := f.AggregateLock(f.ModelConfigAggregate, core.ModelRef.String())
	locks = append(locks, f.LockRequest{Key: model, Mode: f.Shared})
	if core.ApprovalModelRef != nil {
		model, _ := f.AggregateLock(f.ModelConfigAggregate, core.ApprovalModelRef.String())
		locks = append(locks, f.LockRequest{Key: model, Mode: f.Shared})
	}
	return oc.NormalizeLocks(locks)
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
	r, core := change.Request, change.Before.Fields().Core
	// Read every role and Project of this owner. A filtered subset could hide
	// a foreign, stale or extra row and falsely accept a partial configuration.
	var roles, projects, models, efforts []string
	var versions []int64
	err = x.QueryRow(ctx, `SELECT array_agg(role ORDER BY role),array_agg(project_id::text ORDER BY role),array_agg(model_id::text ORDER BY role),array_agg(owner_version ORDER BY role),array_agg(reasoning_effort ORDER BY role) FROM agenteam_model.references WHERE owner_kind='agent' AND owner_id=$1`, r.AgentID.String()).Scan(&roles, &projects, &models, &versions, &efforts)
	if err != nil {
		return unavailable(err)
	}
	expectedRoles, expectedModels, expectedEfforts := []string{}, []string{}, []string{}
	if core.ApprovalModelRef != nil {
		expectedRoles = append(expectedRoles, "approval_model")
		expectedModels = append(expectedModels, core.ApprovalModelRef.String())
		expectedEfforts = append(expectedEfforts, "")
	}
	effort := ""
	if core.ReasoningEffort != nil {
		effort = *core.ReasoningEffort
	}
	// SQL orders agent_model before approval_model.
	expectedRoles = append([]string{"agent_model"}, expectedRoles...)
	expectedModels = append([]string{core.ModelRef.String()}, expectedModels...)
	expectedEfforts = append([]string{effort}, expectedEfforts...)
	if !slices.Equal(roles, expectedRoles) || !slices.Equal(models, expectedModels) || !slices.Equal(efforts, expectedEfforts) || len(projects) != len(roles) || len(versions) != len(roles) {
		return fault(f.ResourceBusy)
	}
	for n := range roles {
		if projects[n] != r.ProjectID.String() || versions[n] != int64(r.ExpectedVersion) {
			return fault(f.ResourceBusy)
		}
	}
	tag, err := x.Exec(ctx, `UPDATE agenteam_model.references SET owner_version=$3 WHERE owner_kind='agent' AND owner_id=$1 AND project_id=$2 AND owner_version=$4`, r.AgentID.String(), r.ProjectID.String(), int64(change.ResultVersion), int64(r.ExpectedVersion))
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != int64(len(expectedRoles)) {
		return fault(f.ResourceBusy)
	}
	return portError(ctx.Err())
}

var _ ac.SkillAssignmentVersionReferences = (*SkillAssignmentReferences)(nil)
