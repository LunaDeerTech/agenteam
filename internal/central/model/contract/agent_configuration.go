package contract

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// ConfigurationPurpose describes a configuration reference, not a runtime
// Consumer or permission to invoke a provider.
type ConfigurationPurpose string

const (
	AgentModelConfiguration    ConfigurationPurpose = "agent_model"
	ApprovalModelConfiguration ConfigurationPurpose = "approval_model"
	MaxAgentConfigurationLocks                      = 512
)

type ConfigurationSelectionRequest struct {
	Actor     id.Actor
	ProjectID id.ProjectID
	Command   f.CommandIdentity
	ModelID   ModelID
	Purpose   ConfigurationPurpose
	// Only the primary Agent model has a configurable reasoning effort.
	// Approval references must leave this nil; they do not inherit that choice.
	ReasoningEffort *string
}

func (r ConfigurationSelectionRequest) Validate() error {
	if r.Actor.Validate() != nil || r.Actor.Details().Kind != id.Human || r.ProjectID.Validate() != nil ||
		r.Command.Validate() != nil || r.Command.Namespace() != "project" ||
		!slices.Equal(r.Command.OwnerIDs(), []string{r.ProjectID.String()}) ||
		!one(r.Command.Command(), "agent.create", "agent.update") || r.ModelID.Validate() != nil ||
		(r.Purpose != AgentModelConfiguration && r.Purpose != ApprovalModelConfiguration) ||
		(r.Purpose == ApprovalModelConfiguration && r.ReasoningEffort != nil) ||
		(r.ReasoningEffort != nil && !safeToken(*r.ReasoningEffort, 32)) {
		return bad()
	}
	return nil
}

func (r ConfigurationSelectionRequest) Clone() ConfigurationSelectionRequest {
	r.ReasoningEffort = copyPtr(r.ReasoningEffort)
	return r
}

func ConfigurationSelectionBinding(r ConfigurationSelectionRequest) (f.Digest, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return binding(struct {
		Format  string
		Actor   id.ActorDetails
		Project id.ProjectID
		Command string
		Model   ModelID
		Purpose ConfigurationPurpose
		Effort  *string
	}{"agent-model-selection-v1", r.Actor.Details(), r.ProjectID, r.Command.Canonical(), r.ModelID, r.Purpose, r.ReasoningEffort})
}

// ConfigurationSelectionFacts contains safe catalog metadata. It is a read
// result in the caller's current Tx, never a reusable authority grant.
type ConfigurationSelectionFacts struct {
	ModelID         ModelID
	ProviderID      ProviderID
	Scope           id.Scope
	ModelVersion    f.Version
	ProviderVersion f.Version
	Capabilities    Capabilities
}

func (v ConfigurationSelectionFacts) Validate() error {
	if v.ModelID.Validate() != nil || v.ProviderID.Validate() != nil || !configScope(v.Scope) ||
		v.ModelVersion.Validate() != nil || v.ProviderVersion.Validate() != nil || v.Capabilities.Validate() != nil {
		return bad()
	}
	return nil
}

func (v ConfigurationSelectionFacts) Clone() ConfigurationSelectionFacts {
	v.Capabilities = v.Capabilities.Clone()
	return v
}

type ConfigurationSelectionPlanDetails struct {
	Binding, Mapping f.Digest
	Locks            []f.LockRequest
	Candidate        ConfigurationSelectionFacts
}

func (v ConfigurationSelectionPlanDetails) clone() ConfigurationSelectionPlanDetails {
	v.Locks = slices.Clone(v.Locks)
	v.Candidate = v.Candidate.Clone()
	return v
}

type configurationSelectionPlanData struct {
	issuer  PlanIssuer
	details ConfigurationSelectionPlanDetails
}

// Plans are issued by a particular Model catalog instance. Public constructors
// and scalar validity cannot impersonate that instance or its live Store.
type ConfigurationSelectionPlan struct {
	data func() configurationSelectionPlanData
}

func ConfigurationSelectionLocks(r ConfigurationSelectionRequest, provider ProviderID) ([]f.LockRequest, error) {
	if r.Validate() != nil || provider.Validate() != nil {
		return nil, bad()
	}
	c, _ := f.CommandLock(r.Command)
	u, _ := f.UserLock(r.Actor.Details().UserID)
	p, _ := f.ProjectLock(r.ProjectID.String())
	registry, _ := f.SystemConfigLock("model-references")
	m, _ := f.AggregateLock(f.ModelConfigAggregate, r.ModelID.String())
	providerLock, _ := f.AggregateLock(f.ProviderAggregate, provider.String())
	return []f.LockRequest{{Key: c, Mode: f.Exclusive}, {Key: u, Mode: f.Exclusive},
		{Key: p, Mode: f.Shared}, {Key: registry, Mode: f.Shared},
		{Key: m, Mode: f.Shared}, {Key: providerLock, Mode: f.Shared}}, nil
}

func NewConfigurationSelectionPlan(issuer PlanIssuer, request ConfigurationSelectionRequest, details ConfigurationSelectionPlanDetails) (ConfigurationSelectionPlan, error) {
	b, err := ConfigurationSelectionBinding(request)
	if err != nil || details.Binding != b || details.Candidate.Validate() != nil ||
		details.Candidate.ModelID != request.ModelID || len(details.Locks) > MaxAgentConfigurationLocks ||
		(details.Candidate.Scope.Details().Kind == id.ProjectScope && details.Candidate.Scope.Details().ProjectID != request.ProjectID.String()) {
		return ConfigurationSelectionPlan{}, bad()
	}
	locks, err := planLocks(issuer, details.Binding, details.Mapping, details.Locks)
	want, _ := ConfigurationSelectionLocks(request, details.Candidate.ProviderID)
	if err != nil || !covers(locks, want) {
		return ConfigurationSelectionPlan{}, bad()
	}
	details = details.clone()
	details.Locks = locks
	d := configurationSelectionPlanData{issuer, details}
	return ConfigurationSelectionPlan{func() configurationSelectionPlanData { return d }}, nil
}

func (p ConfigurationSelectionPlan) Validate() error {
	if p.data == nil {
		return bad()
	}
	return nil
}
func (p ConfigurationSelectionPlan) Details() ConfigurationSelectionPlanDetails {
	if p.data == nil {
		return ConfigurationSelectionPlanDetails{}
	}
	return p.data().details.clone()
}
func (p ConfigurationSelectionPlan) RequiredLocks() []f.LockRequest { return p.Details().Locks }
func (p ConfigurationSelectionPlan) Matches(issuer PlanIssuer, binding, mapping f.Digest) bool {
	return issuer.Valid() && p.data != nil && p.data().issuer == issuer &&
		p.data().details.Binding == binding && p.data().details.Mapping == mapping
}

// Discover reads current Owner-visible candidates in its own short transaction.
// Require consumes only the caller's live Tx, acquires no locks, and checks
// current Owner Mutate before returning catalog facts. Neither operation reads
// credential material, creates a runtime snapshot/lease, or invokes a provider.
type AgentConfigurationSelections interface {
	DiscoverConfigurationSelection(context.Context, ConfigurationSelectionRequest) (ConfigurationSelectionPlan, error)
	RequireConfigurationSelectionInTx(context.Context, f.Tx, ConfigurationSelectionRequest, ConfigurationSelectionPlan) (ConfigurationSelectionFacts, error)
}

func (ConfigurationSelectionRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_configuration_selection"))
}
func (ConfigurationSelectionRequest) LogValue() slog.Value {
	return slog.StringValue("model_configuration_selection")
}
func (ConfigurationSelectionRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"model_configuration_selection"`), nil
}
func (*ConfigurationSelectionRequest) UnmarshalJSON([]byte) error { return bad() }
func (ConfigurationSelectionPlan) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_configuration_plan"))
}
func (ConfigurationSelectionPlan) LogValue() slog.Value {
	return slog.StringValue("model_configuration_plan")
}
func (ConfigurationSelectionPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"model_configuration_plan"`), nil
}
func (*ConfigurationSelectionPlan) UnmarshalJSON([]byte) error { return bad() }
