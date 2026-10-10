package model

import (
	"context"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// AgentConfiguration is a configuration catalog. Construction performs no I/O
// and does not bind Agent runtime authority or claim the presence of an Agent.
type AgentConfiguration struct {
	data func() *agentConfigurationState
}
type agentConfigurationState struct {
	store     Store
	authority *Authority
	issuer    mc.PlanIssuer
}

func NewAgentConfiguration(store Store, authority *Authority) (*AgentConfiguration, error) {
	if nilPort(store) || authority.state() == nil || !sameStore(store, authority.state().store) || nilPort(authority.state().auth.Projects) {
		return nil, fault(f.DependencyUnbound)
	}
	s := &agentConfigurationState{store, authority, mc.NewPlanIssuer()}
	return &AgentConfiguration{func() *agentConfigurationState { return s }}, nil
}
func (s *AgentConfiguration) state() *agentConfigurationState {
	if s == nil || s.data == nil {
		return nil
	}
	return s.data()
}
func agentConfigurationContext(ctx context.Context) error {
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return unavailable(err)
	}
	return nil
}
func (s *AgentConfiguration) validate(ctx context.Context, r mc.ConfigurationSelectionRequest) error {
	if err := agentConfigurationContext(ctx); err != nil {
		return err
	}
	if err := human(r.Actor); err != nil {
		return err
	}
	if r.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if s.state() == nil {
		return fault(f.DependencyUnbound)
	}
	return nil
}

func (s *AgentConfiguration) DiscoverConfigurationSelection(ctx context.Context, request mc.ConfigurationSelectionRequest) (mc.ConfigurationSelectionPlan, error) {
	if err := s.validate(ctx, request); err != nil {
		return mc.ConfigurationSelectionPlan{}, err
	}
	r := request.Clone()
	scope, _ := id.InProject(r.ProjectID)
	cause, err := readCause("agent-model-selection")
	if err != nil {
		return mc.ConfigurationSelectionPlan{}, err
	}
	var candidate mc.ConfigurationSelectionFacts
	// Provider identity is not known until this discovery read. These are only
	// candidate facts; the returned union includes Provider SH for final re-read.
	user := userLock(r.Actor.Details().UserID)
	user.Mode = f.Exclusive
	locks := []f.LockRequest{commandLock(r.Command), user,
		projectLock(r.ProjectID.String()), systemLock("model-references", f.Shared),
		aggregateLock(f.ModelConfigAggregate, r.ModelID.String(), f.Shared)}
	state := s.state()
	result := state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := state.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := state.authority.currentScope(ctx, tx, r.Actor, scope, id.Read); err != nil {
			return err
		}
		x, err := state.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		candidate, err = readConfigurationSelection(ctx, x, r)
		return err
	})
	if err = commitError(result); err != nil {
		return mc.ConfigurationSelectionPlan{}, err
	}
	if err = agentConfigurationContext(ctx); err != nil {
		return mc.ConfigurationSelectionPlan{}, err
	}
	locks, err = mc.ConfigurationSelectionLocks(r, candidate.ProviderID)
	if err != nil {
		return mc.ConfigurationSelectionPlan{}, portError(err)
	}
	binding, _ := mc.ConfigurationSelectionBinding(r)
	mapping, err := configurationSelectionMapping(candidate)
	if err != nil {
		return mc.ConfigurationSelectionPlan{}, err
	}
	return mc.NewConfigurationSelectionPlan(state.issuer, r, mc.ConfigurationSelectionPlanDetails{Binding: binding, Mapping: mapping, Locks: locks, Candidate: candidate})
}

func (s *AgentConfiguration) RequireConfigurationSelectionInTx(ctx context.Context, tx f.Tx, request mc.ConfigurationSelectionRequest, plan mc.ConfigurationSelectionPlan) (mc.ConfigurationSelectionFacts, error) {
	zero := mc.ConfigurationSelectionFacts{}
	if err := s.validate(ctx, request); err != nil {
		return zero, err
	}
	r := request.Clone()
	state := s.state()
	x, err := state.store.InTx(tx)
	if err != nil {
		return zero, portError(err)
	}
	binding, _ := mc.ConfigurationSelectionBinding(r)
	details := plan.Details()
	if plan.Validate() != nil || !plan.Matches(state.issuer, binding, details.Mapping) {
		return zero, fault(f.Forbidden)
	}
	if err = state.store.RequireHeldLocks(ctx, tx, plan.RequiredLocks()); err != nil {
		return zero, portError(err)
	}
	scope, _ := id.InProject(r.ProjectID)
	if err = state.authority.currentScope(ctx, tx, r.Actor, scope, id.Mutate); err != nil {
		return zero, err
	}
	facts, err := readConfigurationSelection(ctx, x, r)
	if err != nil {
		return zero, err
	}
	mapping, err := configurationSelectionMapping(facts)
	if err != nil {
		return zero, err
	}
	if mapping != details.Mapping {
		return zero, fault(f.ResourceBusy)
	}
	if err = agentConfigurationContext(ctx); err != nil {
		return zero, err
	}
	return facts.Clone(), nil
}

func readConfigurationSelection(ctx context.Context, x postgres.SQLExecutor, r mc.ConfigurationSelectionRequest) (mc.ConfigurationSelectionFacts, error) {
	zero := mc.ConfigurationSelectionFacts{}
	m, err := loadModel(ctx, x, r.ModelID.String())
	if err == nil && m == nil {
		scope, _ := id.InProject(r.ProjectID)
		m, err = loadModelScope(ctx, x, r.ModelID.String(), scope)
	}
	if err != nil {
		return zero, err
	}
	if m == nil {
		return zero, fault(f.NotFound)
	}
	p, err := loadProviderScope(ctx, x, m.ProviderID, configurationScope(m.Project))
	if err != nil {
		return zero, err
	}
	if p == nil {
		return zero, unavailable(nil)
	}
	if !m.Input.Enabled || !p.Input.Enabled {
		return zero, fault(f.InvalidState)
	}
	if m.Input.Type != mc.ChatModel || !p.Input.Protocol.Supports(mc.ChatModel) {
		return zero, fault(f.CapabilityUnsupported)
	}
	if r.Purpose == mc.AgentModelConfiguration {
		if len(m.Input.Capabilities.ReasoningEfforts) == 0 {
			if r.ReasoningEffort != nil {
				return zero, fault(f.CapabilityUnsupported)
			}
		} else if r.ReasoningEffort == nil || !slices.Contains(m.Input.Capabilities.ReasoningEfforts, *r.ReasoningEffort) {
			return zero, fault(f.CapabilityUnsupported)
		}
	}
	providerID, err := f.ParseID[mc.Provider](p.ID)
	if err != nil {
		return zero, unavailable(err)
	}
	facts := mc.ConfigurationSelectionFacts{ModelID: r.ModelID, ProviderID: providerID, Scope: configurationScope(m.Project), ModelVersion: m.Version, ProviderVersion: p.Version, Capabilities: m.Input.Capabilities.Clone()}
	if err = facts.Validate(); err != nil {
		return zero, unavailable(err)
	}
	return facts, nil
}

func configurationSelectionMapping(v mc.ConfigurationSelectionFacts) (f.Digest, error) {
	raw, err := encoded(v)
	if err != nil {
		return "", err
	}
	return hash(raw), nil
}

var _ mc.AgentConfigurationSelections = (*AgentConfiguration)(nil)
