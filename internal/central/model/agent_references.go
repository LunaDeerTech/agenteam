package model

import (
	"context"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// AgentReferenceService maintains only Model's complete Agent reference index.
// The injected Agent owner must prove its canonical write in the caller's Tx;
// neither a public plan nor configuration selection is that proof.
type AgentReferenceService struct{ data func() *agentReferenceState }
type agentReferenceState struct {
	store     Store
	selection *AgentConfiguration
	owner     mc.AgentReferenceOwnerAuthority
	issuer    mc.PlanIssuer
}

func NewAgentReferences(store Store, selection *AgentConfiguration, owner mc.AgentReferenceOwnerAuthority) (*AgentReferenceService, error) {
	if nilPort(store) || selection.state() == nil || !sameStore(store, selection.state().store) || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	s := &agentReferenceState{store, selection, owner, mc.NewPlanIssuer()}
	return &AgentReferenceService{func() *agentReferenceState { return s }}, nil
}
func (s *AgentReferenceService) state() *agentReferenceState {
	if s == nil || s.data == nil {
		return nil
	}
	return s.data()
}
func (s *AgentReferenceService) validate(ctx context.Context, r mc.AgentReferenceChange) error {
	if s.state() == nil {
		return fault(f.DependencyUnbound)
	}
	if err := agentConfigurationContext(ctx); err != nil {
		return err
	}
	if err := human(r.Actor); err != nil {
		return err
	}
	if r.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	return nil
}

func (s *AgentReferenceService) DiscoverAgentReferences(ctx context.Context, request mc.AgentReferenceChange) (mc.AgentReferencePlan, error) {
	zero := mc.AgentReferencePlan{}
	if err := s.validate(ctx, request); err != nil {
		return zero, err
	}
	r, state := request.Clone(), s.state()
	owner, err := state.owner.DiscoverAgentReferenceOwner(ctx, r.Clone())
	if err != nil {
		return zero, portError(err)
	}
	binding, _ := mc.AgentReferenceBinding(r)
	if owner.Validate() != nil || owner.Details().Binding != binding {
		return zero, fault(f.Forbidden)
	}
	pr, ar, _ := mc.AgentReferenceSelectionRequests(r)
	primary, err := state.selection.DiscoverConfigurationSelection(ctx, pr)
	if err != nil {
		return zero, err
	}
	var approval *mc.ConfigurationSelectionPlan
	if ar != nil {
		p, err := state.selection.DiscoverConfigurationSelection(ctx, *ar)
		if err != nil {
			return zero, err
		}
		approval = &p
	}
	locks, _ := mc.AgentReferenceBaseLocks(r)
	locks = append(locks, owner.RequiredLocks()...)
	locks = append(locks, primary.RequiredLocks()...)
	if approval != nil {
		locks = append(locks, approval.RequiredLocks()...)
	}
	locks, err = oc.NormalizeLocks(locks)
	if err != nil {
		return zero, portError(err)
	}
	cause, err := readCause("agent-references")
	if err != nil {
		return zero, err
	}
	var snapshot agentReferenceSnapshot
	result := state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := state.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		scope, _ := id.InProject(r.ProjectID)
		if err := state.selection.state().authority.currentScope(ctx, tx, r.Actor, scope, id.Read); err != nil {
			return err
		}
		x, err := state.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		snapshot, err = readAgentReferenceSnapshot(ctx, x, r)
		return err
	})
	if err = commitError(result); err != nil {
		return zero, err
	}
	// Old provider identities are candidates until final re-read under this
	// complete union. Discover never acquires an unknown late provider lock.
	for _, old := range snapshot.Models {
		locks = append(locks, aggregateLock(f.ProviderAggregate, old.Provider, f.Shared))
	}
	mapping, err := agentReferenceMapping(snapshot)
	if err != nil {
		return zero, err
	}
	if err = agentConfigurationContext(ctx); err != nil {
		return zero, err
	}
	return mc.NewAgentReferencePlan(state.issuer, r, mapping, locks, owner, primary, approval)
}

func (s *AgentReferenceService) ApplyAgentReferencesInTx(ctx context.Context, tx f.Tx, request mc.AgentReferenceChange, plan mc.AgentReferencePlan) error {
	if err := s.validate(ctx, request); err != nil {
		return err
	}
	r, state := request.Clone(), s.state()
	x, err := state.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	binding, _ := mc.AgentReferenceBinding(r)
	details := plan.Details()
	if plan.Validate() != nil || !plan.Matches(state.issuer, binding, details.Mapping) {
		return fault(f.Forbidden)
	}
	if err = state.store.RequireHeldLocks(ctx, tx, plan.RequiredLocks()); err != nil {
		return portError(err)
	}
	scope, _ := id.InProject(r.ProjectID)
	if err = state.selection.state().authority.currentScope(ctx, tx, r.Actor, scope, id.Mutate); err != nil {
		return err
	}
	if err = state.owner.CheckAgentReferenceOwnerAppliedInTx(ctx, tx, r.Clone(), plan.OwnerPlan()); err != nil {
		return portError(err)
	}
	snapshot, err := readAgentReferenceSnapshot(ctx, x, r)
	if err != nil {
		return err
	}
	mapping, err := agentReferenceMapping(snapshot)
	if err != nil {
		return err
	}
	if mapping != details.Mapping {
		return fault(f.ResourceBusy)
	}
	pr, ar, _ := mc.AgentReferenceSelectionRequests(r)
	if _, err = state.selection.RequireConfigurationSelectionInTx(ctx, tx, pr, plan.PrimarySelection()); err != nil {
		return err
	}
	if ar != nil {
		approval := plan.ApprovalSelection()
		if approval == nil {
			return fault(f.Forbidden)
		}
		if _, err = state.selection.RequireConfigurationSelectionInTx(ctx, tx, *ar, *approval); err != nil {
			return err
		}
	}
	if err = agentConfigurationContext(ctx); err != nil {
		return err
	}
	if r.Before != nil && r.Before.Equal(r.After) && *r.ExpectedOwnerVersion == r.ResultOwnerVersion {
		return nil
	}
	// Replace the whole pair, including retained roles' owner_version. Agent
	// owns Commit/rollback/Unknown; this participant never opens a write Tx.
	tag, err := x.Exec(ctx, `DELETE FROM agenteam_model.references WHERE owner_kind='agent' AND owner_id=$1`, r.AgentID.String())
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != int64(len(snapshot.References)) {
		return fault(f.ResourceBusy)
	}
	for _, ref := range agentReferenceRecords(r, &r.After, r.ResultOwnerVersion) {
		tag, err = x.Exec(ctx, `INSERT INTO agenteam_model.references(owner_kind,owner_id,role,project_id,model_id,owner_version,reasoning_effort) VALUES('agent',$1,$2,$3,$4,$5,$6)`, ref.Owner, ref.Role, ref.Project, ref.Model, int64(ref.Version), ref.Effort)
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return unavailable(nil)
		}
	}
	return agentConfigurationContext(ctx)
}

type agentReferenceModel struct {
	Model, Provider, Project      string
	ModelVersion, ProviderVersion f.Version
}
type agentReferenceSnapshot struct {
	References []referenceRecord
	Models     []agentReferenceModel
}

func agentReferenceRecords(r mc.AgentReferenceChange, selection *mc.AgentModelSelection, version f.Version) []referenceRecord {
	out := []referenceRecord{}
	if selection == nil {
		return out
	}
	effort := ""
	if selection.ReasoningEffort != nil {
		effort = *selection.ReasoningEffort
	}
	out = append(out, referenceRecord{Kind: "agent", Owner: r.AgentID.String(), Role: "agent_model", Project: r.ProjectID.String(), Model: selection.ModelID.String(), Version: version, Effort: effort})
	if selection.ApprovalModelID != nil {
		out = append(out, referenceRecord{Kind: "agent", Owner: r.AgentID.String(), Role: "approval_model", Project: r.ProjectID.String(), Model: selection.ApprovalModelID.String(), Version: version})
	}
	return out
}

func readAgentReferenceSnapshot(ctx context.Context, x postgres.SQLExecutor, r mc.AgentReferenceChange) (agentReferenceSnapshot, error) {
	zero := agentReferenceSnapshot{}
	// The schema permits at most these two rows per Agent. Read every role and
	// Project together: filtering to the requested Project could hide a corrupt
	// or foreign index and falsely authorize creation. Aggregate arrays retain
	// one ordinary QueryRow lifetime without leaving a streaming reader open.
	var roles, projects, models, efforts []string
	var versions []int64
	err := x.QueryRow(ctx, `SELECT array_agg(role ORDER BY role),array_agg(project_id::text ORDER BY role),array_agg(model_id::text ORDER BY role),array_agg(owner_version ORDER BY role),array_agg(reasoning_effort ORDER BY role) FROM agenteam_model.references WHERE owner_kind='agent' AND owner_id=$1`, r.AgentID.String()).Scan(&roles, &projects, &models, &versions, &efforts)
	if err != nil {
		return zero, unavailable(err)
	}
	if len(roles) > 2 || len(roles) != len(projects) || len(roles) != len(models) || len(roles) != len(versions) || len(roles) != len(efforts) {
		return zero, unavailable(nil)
	}
	snapshot := agentReferenceSnapshot{References: []referenceRecord{}, Models: []agentReferenceModel{}}
	for i, role := range roles {
		snapshot.References = append(snapshot.References, referenceRecord{Kind: "agent", Owner: r.AgentID.String(), Role: role, Project: projects[i], Model: models[i], Version: f.Version(versions[i]), Effort: efforts[i]})
	}
	var expected f.Version
	if r.ExpectedOwnerVersion != nil {
		expected = *r.ExpectedOwnerVersion
	}
	if !slices.Equal(snapshot.References, agentReferenceRecords(r, r.Before, expected)) {
		return zero, fault(f.ResourceBusy)
	}
	for _, ref := range snapshot.References {
		m, err := loadModel(ctx, x, ref.Model)
		if err == nil && m == nil {
			scope, _ := id.InProject(r.ProjectID)
			m, err = loadModelScope(ctx, x, ref.Model, scope)
		}
		if err != nil {
			return zero, err
		}
		if m == nil {
			return zero, fault(f.ResourceBusy)
		}
		p, err := loadProviderScope(ctx, x, m.ProviderID, configurationScope(m.Project))
		if err != nil {
			return zero, err
		}
		if p == nil {
			return zero, fault(f.ResourceBusy)
		}
		// Old references may point to disabled configurations. Removing one
		// must remain possible; eligibility is enforced on both After roles.
		snapshot.Models = append(snapshot.Models, agentReferenceModel{m.ID, p.ID, m.Project, m.Version, p.Version})
	}
	return snapshot, nil
}
func agentReferenceMapping(snapshot agentReferenceSnapshot) (f.Digest, error) {
	raw, err := encoded(snapshot)
	if err != nil {
		return "", err
	}
	return hash(raw), nil
}

var _ mc.AgentReferences = (*AgentReferenceService)(nil)
