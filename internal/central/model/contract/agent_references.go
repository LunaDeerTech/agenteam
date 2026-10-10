package contract

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// AgentModelSelection is the complete pair of configuration references owned
// by an Agent. Only the primary Model has an Agent-configured effort.
type AgentModelSelection struct {
	ModelID         ModelID
	ReasoningEffort *string
	ApprovalModelID *ModelID
}

func (v AgentModelSelection) Validate() error {
	if v.ModelID.Validate() != nil || v.ApprovalModelID != nil && v.ApprovalModelID.Validate() != nil || v.ReasoningEffort != nil && !safeToken(*v.ReasoningEffort, 32) {
		return bad()
	}
	return nil
}
func (v AgentModelSelection) Clone() AgentModelSelection {
	v.ReasoningEffort, v.ApprovalModelID = copyPtr(v.ReasoningEffort), copyPtr(v.ApprovalModelID)
	return v
}
func (v AgentModelSelection) Equal(other AgentModelSelection) bool {
	return v.ModelID == other.ModelID && equalAgentOptional(v.ReasoningEffort, other.ReasoningEffort) && equalAgentOptional(v.ApprovalModelID, other.ApprovalModelID)
}
func equalAgentOptional[T comparable](a, b *T) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

type AgentReferenceChange struct {
	Actor                id.Actor
	ProjectID            id.ProjectID
	AgentID              id.AgentID
	Command              f.CommandIdentity
	PlanRevision         f.Version
	ExpectedOwnerVersion *f.Version
	ResultOwnerVersion   f.Version
	Before               *AgentModelSelection
	After                AgentModelSelection
}

func (r AgentReferenceChange) Validate() error {
	if r.Actor.Validate() != nil || r.Actor.Details().Kind != id.Human || r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil || r.PlanRevision.Validate() != nil || r.ResultOwnerVersion.Validate() != nil || r.After.Validate() != nil || r.Command.Validate() != nil || r.Command.Namespace() != "project" || !slices.Equal(r.Command.OwnerIDs(), []string{r.ProjectID.String()}) {
		return bad()
	}
	switch r.Command.Command() {
	case "agent.create":
		if r.Before != nil || r.ExpectedOwnerVersion != nil || r.ResultOwnerVersion != 1 {
			return bad()
		}
	case "agent.update":
		if r.Before == nil || r.Before.Validate() != nil || r.ExpectedOwnerVersion == nil || r.ExpectedOwnerVersion.Validate() != nil || r.ResultOwnerVersion < *r.ExpectedOwnerVersion || r.ResultOwnerVersion-*r.ExpectedOwnerVersion > 1 || !r.Before.Equal(r.After) && r.ResultOwnerVersion == *r.ExpectedOwnerVersion {
			return bad()
		}
	default:
		return bad()
	}
	return nil
}
func (r AgentReferenceChange) Clone() AgentReferenceChange {
	r.ExpectedOwnerVersion = copyPtr(r.ExpectedOwnerVersion)
	if r.Before != nil {
		before := r.Before.Clone()
		r.Before = &before
	}
	r.After = r.After.Clone()
	return r
}
func AgentReferenceBinding(r AgentReferenceChange) (f.Digest, error) {
	if r.Validate() != nil {
		return "", bad()
	}
	// Explicit projection: request formatting/JSON remains a safe sentinel.
	return binding(struct {
		Format   string
		Actor    id.ActorDetails
		Project  id.ProjectID
		Agent    id.AgentID
		Command  string
		Revision f.Version
		Expected *f.Version
		Result   f.Version
		Before   *AgentModelSelection
		After    AgentModelSelection
	}{"agent-model-reference-v1", r.Actor.Details(), r.ProjectID, r.AgentID, r.Command.Canonical(), r.PlanRevision, r.ExpectedOwnerVersion, r.ResultOwnerVersion, r.Before, r.After})
}

// Agent owns the command and canonical row. Model owns only its two reference
// index rows. These locks are a minimum; plans include every catalog provider.
func AgentReferenceBaseLocks(r AgentReferenceChange) ([]f.LockRequest, error) {
	if r.Validate() != nil {
		return nil, bad()
	}
	c, _ := f.CommandLock(r.Command)
	u, _ := f.UserLock(r.Actor.Details().UserID)
	p, _ := f.ProjectLock(r.ProjectID.String())
	a, _ := f.AgentLock(r.AgentID.String())
	registry, _ := f.SystemConfigLock("model-references")
	index, err := f.RecordLock(f.ReferenceRecordLock, "model:agent:"+r.AgentID.String())
	if err != nil {
		return nil, bad()
	}
	locks := []f.LockRequest{{Key: c, Mode: f.Exclusive}, {Key: u, Mode: f.Exclusive}, {Key: p, Mode: f.Shared}, {Key: a, Mode: f.Exclusive}, {Key: registry, Mode: f.Shared}, {Key: index, Mode: f.Exclusive}}
	for _, set := range []*AgentModelSelection{r.Before, &r.After} {
		if set == nil {
			continue
		}
		ids := []ModelID{set.ModelID}
		if set.ApprovalModelID != nil {
			ids = append(ids, *set.ApprovalModelID)
		}
		for _, mid := range ids {
			key, _ := f.AggregateLock(f.ModelConfigAggregate, mid.String())
			locks = append(locks, f.LockRequest{Key: key, Mode: f.Shared})
		}
	}
	return locks, nil
}

type AgentReferencePlanDetails struct {
	Binding, Mapping f.Digest
	Locks            []f.LockRequest
}

func (d AgentReferencePlanDetails) clone() AgentReferencePlanDetails {
	d.Locks = slices.Clone(d.Locks)
	return d
}

type agentReferencePlanData struct {
	issuer  PlanIssuer
	details AgentReferencePlanDetails
}

// The real Agent authority issues and validates this plan. Its mapping must
// cover the original planned command/revision and complete canonical pre/post
// images. A public constructor cannot mint that authority's private witness.
type AgentReferenceOwnerPlan struct{ data func() agentReferencePlanData }

func NewAgentReferenceOwnerPlan(issuer PlanIssuer, r AgentReferenceChange, mapping f.Digest, locks []f.LockRequest) (AgentReferenceOwnerPlan, error) {
	b, err := AgentReferenceBinding(r)
	if err != nil {
		return AgentReferenceOwnerPlan{}, err
	}
	if len(locks) > MaxAgentConfigurationLocks {
		return AgentReferenceOwnerPlan{}, bad()
	}
	normal, err := planLocks(issuer, b, mapping, locks)
	// The owner need not acquire Model's index/catalog locks during Discover.
	c, _ := f.CommandLock(r.Command)
	u, _ := f.UserLock(r.Actor.Details().UserID)
	p, _ := f.ProjectLock(r.ProjectID.String())
	a, _ := f.AgentLock(r.AgentID.String())
	want := []f.LockRequest{{Key: c, Mode: f.Exclusive}, {Key: u, Mode: f.Exclusive}, {Key: p, Mode: f.Shared}, {Key: a, Mode: f.Exclusive}}
	if err != nil || !covers(normal, want) {
		return AgentReferenceOwnerPlan{}, bad()
	}
	d := agentReferencePlanData{issuer, AgentReferencePlanDetails{b, mapping, normal}}
	return AgentReferenceOwnerPlan{func() agentReferencePlanData { return d }}, nil
}
func (p AgentReferenceOwnerPlan) Validate() error {
	if p.data == nil {
		return bad()
	}
	return nil
}
func (p AgentReferenceOwnerPlan) Details() AgentReferencePlanDetails {
	if p.data == nil {
		return AgentReferencePlanDetails{}
	}
	return p.data().details.clone()
}
func (p AgentReferenceOwnerPlan) RequiredLocks() []f.LockRequest { return p.Details().Locks }
func (p AgentReferenceOwnerPlan) Matches(i PlanIssuer, b, m f.Digest) bool {
	return i.Valid() && p.data != nil && p.data().issuer == i && p.data().details.Binding == b && p.data().details.Mapping == m
}

type agentReferenceData struct {
	plan     agentReferencePlanData
	owner    AgentReferenceOwnerPlan
	primary  ConfigurationSelectionPlan
	approval *ConfigurationSelectionPlan
}
type AgentReferencePlan struct{ data func() agentReferenceData }

func AgentReferenceSelectionRequests(r AgentReferenceChange) (ConfigurationSelectionRequest, *ConfigurationSelectionRequest, error) {
	if r.Validate() != nil {
		return ConfigurationSelectionRequest{}, nil, bad()
	}
	primary := ConfigurationSelectionRequest{Actor: r.Actor, ProjectID: r.ProjectID, Command: r.Command, ModelID: r.After.ModelID, Purpose: AgentModelConfiguration, ReasoningEffort: copyPtr(r.After.ReasoningEffort)}
	if r.After.ApprovalModelID == nil {
		return primary, nil, nil
	}
	approval := primary.Clone()
	approval.ModelID = *r.After.ApprovalModelID
	approval.Purpose = ApprovalModelConfiguration
	approval.ReasoningEffort = nil
	return primary, &approval, nil
}
func NewAgentReferencePlan(issuer PlanIssuer, r AgentReferenceChange, mapping f.Digest, locks []f.LockRequest, owner AgentReferenceOwnerPlan, primary ConfigurationSelectionPlan, approval *ConfigurationSelectionPlan) (AgentReferencePlan, error) {
	b, err := AgentReferenceBinding(r)
	if err != nil {
		return AgentReferencePlan{}, err
	}
	if len(locks) > MaxAgentConfigurationLocks {
		return AgentReferencePlan{}, bad()
	}
	normal, err := planLocks(issuer, b, mapping, locks)
	want, _ := AgentReferenceBaseLocks(r)
	if err != nil || owner.Validate() != nil || owner.Details().Binding != b || !covers(normal, want) || !covers(normal, owner.RequiredLocks()) {
		return AgentReferencePlan{}, bad()
	}
	pr, ar, _ := AgentReferenceSelectionRequests(r)
	pb, _ := ConfigurationSelectionBinding(pr)
	if primary.Validate() != nil || primary.Details().Binding != pb || !covers(normal, primary.RequiredLocks()) {
		return AgentReferencePlan{}, bad()
	}
	if ar == nil {
		if approval != nil {
			return AgentReferencePlan{}, bad()
		}
	} else {
		ab, _ := ConfigurationSelectionBinding(*ar)
		if approval == nil || approval.Validate() != nil || approval.Details().Binding != ab || !covers(normal, approval.RequiredLocks()) {
			return AgentReferencePlan{}, bad()
		}
	}
	d := agentReferenceData{agentReferencePlanData{issuer, AgentReferencePlanDetails{b, mapping, normal}}, owner, primary, copyPtr(approval)}
	return AgentReferencePlan{func() agentReferenceData { return d }}, nil
}
func (p AgentReferencePlan) Validate() error {
	if p.data == nil {
		return bad()
	}
	return nil
}
func (p AgentReferencePlan) Details() AgentReferencePlanDetails {
	if p.data == nil {
		return AgentReferencePlanDetails{}
	}
	return p.data().plan.details.clone()
}
func (p AgentReferencePlan) RequiredLocks() []f.LockRequest { return p.Details().Locks }
func (p AgentReferencePlan) Matches(i PlanIssuer, b, m f.Digest) bool {
	return i.Valid() && p.data != nil && p.data().plan.issuer == i && p.data().plan.details.Binding == b && p.data().plan.details.Mapping == m
}
func (p AgentReferencePlan) OwnerPlan() AgentReferenceOwnerPlan {
	if p.data == nil {
		return AgentReferenceOwnerPlan{}
	}
	return p.data().owner
}
func (p AgentReferencePlan) PrimarySelection() ConfigurationSelectionPlan {
	if p.data == nil {
		return ConfigurationSelectionPlan{}
	}
	return p.data().primary
}
func (p AgentReferencePlan) ApprovalSelection() *ConfigurationSelectionPlan {
	if p.data == nil {
		return nil
	}
	return copyPtr(p.data().approval)
}

type AgentReferences interface {
	DiscoverAgentReferences(context.Context, AgentReferenceChange) (AgentReferencePlan, error)
	ApplyAgentReferencesInTx(context.Context, f.Tx, AgentReferenceChange, AgentReferencePlan) error
}
type AgentReferenceOwnerAuthority interface {
	DiscoverAgentReferenceOwner(context.Context, AgentReferenceChange) (AgentReferenceOwnerPlan, error)
	CheckAgentReferenceOwnerAppliedInTx(context.Context, f.Tx, AgentReferenceChange, AgentReferenceOwnerPlan) error
}

func (AgentReferenceChange) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("agent_model_reference_change"))
}
func (AgentReferenceChange) LogValue() slog.Value {
	return slog.StringValue("agent_model_reference_change")
}
func (AgentReferenceChange) MarshalJSON() ([]byte, error) {
	return []byte(`"agent_model_reference_change"`), nil
}
func (*AgentReferenceChange) UnmarshalJSON([]byte) error { return bad() }
func (AgentReferenceOwnerPlan) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("agent_model_reference_owner_plan"))
}
func (AgentReferenceOwnerPlan) LogValue() slog.Value {
	return slog.StringValue("agent_model_reference_owner_plan")
}
func (AgentReferenceOwnerPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"agent_model_reference_owner_plan"`), nil
}
func (*AgentReferenceOwnerPlan) UnmarshalJSON([]byte) error { return bad() }
func (AgentReferencePlan) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("agent_model_reference_plan"))
}
func (AgentReferencePlan) LogValue() slog.Value {
	return slog.StringValue("agent_model_reference_plan")
}
func (AgentReferencePlan) MarshalJSON() ([]byte, error) {
	return []byte(`"agent_model_reference_plan"`), nil
}
func (*AgentReferencePlan) UnmarshalJSON([]byte) error { return bad() }
