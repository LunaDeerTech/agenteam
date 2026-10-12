package model

import (
	"context"
	"slices"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// ProjectAgentRetirementReferences removes only Model's complete Agent index
// in the original Agent retirement transaction. The Agent owner must confirm
// its applied private witness; neither the public change nor an empty index
// supplies Project lifecycle authority or confirms a prior transaction.
type ProjectAgentRetirementReferences struct {
	store Store
	owner ac.ProjectRetirementOwner
}

func NewProjectAgentRetirementReferences(store Store, owner ac.ProjectRetirementOwner) (*ProjectAgentRetirementReferences, error) {
	if nilPort(store) || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ProjectAgentRetirementReferences{store: store, owner: owner}, nil
}

func (*ProjectAgentRetirementReferences) ProjectRetirementReferenceKind() ac.ProjectRetirementReferenceKind {
	return ac.ProjectRetirementModelReferences
}

func (s *ProjectAgentRetirementReferences) ProjectRetirementLocks(change ac.ProjectRetirementChange) ([]f.LockRequest, error) {
	if s == nil || nilPort(s.store) || nilPort(s.owner) {
		return nil, fault(f.DependencyUnbound)
	}
	if err := change.Validate(); err != nil {
		return nil, err
	}
	registry, _ := f.SystemConfigLock("model-references")
	index, _ := f.RecordLock(f.ReferenceRecordLock, "model:agent:"+change.Request.AgentID.String())
	locks := append(change.Request.RequiredLocks(), f.LockRequest{Key: registry, Mode: f.Exclusive}, f.LockRequest{Key: index, Mode: f.Exclusive})
	core := change.Before.Fields().Core
	primary, _ := f.AggregateLock(f.ModelConfigAggregate, core.ModelRef.String())
	locks = append(locks, f.LockRequest{Key: primary, Mode: f.Shared})
	if core.ApprovalModelRef != nil {
		approval, _ := f.AggregateLock(f.ModelConfigAggregate, core.ApprovalModelRef.String())
		locks = append(locks, f.LockRequest{Key: approval, Mode: f.Shared})
	}
	return oc.NormalizeLocks(locks)
}

func (s *ProjectAgentRetirementReferences) original(ctx context.Context, tx f.Tx, change ac.ProjectRetirementChange) (postgres.SQLExecutor, error) {
	locks, err := s.ProjectRetirementLocks(change)
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
	// The concrete Agent consumer checks this same live Tx, the original
	// Project Delete claim, and its exact applied preimage or recovery record.
	if err = s.owner.RequireProjectRetirementAppliedInTx(ctx, tx, change.Clone()); err != nil {
		return nil, portError(err)
	}
	if err = ctx.Err(); err != nil {
		return nil, portError(err)
	}
	return x, nil
}

func readProjectRetirementModelReferences(ctx context.Context, x postgres.SQLExecutor, change ac.ProjectRetirementChange) ([]referenceRecord, error) {
	var roles, projects, models, efforts []string
	var versions []int64
	// Read every role and Project for this owner. Filtering by the requested
	// Project would hide an unexpected foreign row or partial index.
	err := x.QueryRow(ctx, `SELECT array_agg(role ORDER BY role),array_agg(project_id::text ORDER BY role),array_agg(model_id::text ORDER BY role),array_agg(owner_version ORDER BY role),array_agg(reasoning_effort ORDER BY role) FROM agenteam_model.references WHERE owner_kind='agent' AND owner_id=$1`, change.Request.AgentID.String()).Scan(&roles, &projects, &models, &versions, &efforts)
	if err != nil {
		return nil, unavailable(err)
	}
	if len(roles) > 2 || len(roles) != len(projects) || len(roles) != len(models) || len(roles) != len(versions) || len(roles) != len(efforts) {
		return nil, unavailable(nil)
	}
	refs := make([]referenceRecord, 0, len(roles))
	for n, role := range roles {
		refs = append(refs, referenceRecord{Kind: "agent", Owner: change.Request.AgentID.String(), Role: role, Project: projects[n], Model: models[n], Version: f.Version(versions[n]), Effort: efforts[n]})
	}
	return refs, nil
}

func (s *ProjectAgentRetirementReferences) RetireProjectAgentReferencesInTx(ctx context.Context, tx f.Tx, change ac.ProjectRetirementChange) error {
	change = change.Clone()
	x, err := s.original(ctx, tx, change)
	if err != nil {
		return err
	}
	refs, err := readProjectRetirementModelReferences(ctx, x, change)
	if err != nil {
		return err
	}
	core := change.Before.Fields().Core
	effort := ""
	if core.ReasoningEffort != nil {
		effort = *core.ReasoningEffort
	}
	expected := []referenceRecord{{Kind: "agent", Owner: core.ID.String(), Role: "agent_model", Project: core.ProjectID.String(), Model: core.ModelRef.String(), Version: core.Version, Effort: effort}}
	if core.ApprovalModelRef != nil {
		expected = append(expected, referenceRecord{Kind: "agent", Owner: core.ID.String(), Role: "approval_model", Project: core.ProjectID.String(), Model: core.ApprovalModelRef.String(), Version: core.Version})
	}
	if !slices.Equal(refs, expected) {
		return fault(f.ResourceBusy)
	}
	if err = ctx.Err(); err != nil {
		return portError(err)
	}
	// This participant does not start or commit a transaction. Agent owns the
	// canonical removal, all other reference owners and the final checkpoint.
	tag, err := x.Exec(ctx, `DELETE FROM agenteam_model.references WHERE owner_kind='agent' AND owner_id=$1 AND project_id=$2 AND owner_version=$3`, change.Request.AgentID.String(), change.Request.ProjectID.String(), int64(change.Request.ExpectedVersion))
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != int64(len(expected)) {
		return fault(f.ResourceBusy)
	}
	return portError(ctx.Err())
}

func (s *ProjectAgentRetirementReferences) RequireProjectAgentReferencesRetiredInTx(ctx context.Context, tx f.Tx, change ac.ProjectRetirementChange) error {
	change = change.Clone()
	x, err := s.original(ctx, tx, change)
	if err != nil {
		return err
	}
	refs, err := readProjectRetirementModelReferences(ctx, x, change)
	if err != nil {
		return err
	}
	if len(refs) != 0 {
		return fault(f.ResourceBusy)
	}
	return portError(ctx.Err())
}

var _ ac.ProjectRetirementReferences = (*ProjectAgentRetirementReferences)(nil)
