package contract

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func agentChange(t *testing.T) AgentReferenceChange {
	t.Helper()
	r := configurationRequest(t)
	return AgentReferenceChange{Actor: r.Actor, ProjectID: r.ProjectID, AgentID: fresh[id.Agent](t), Command: r.Command, PlanRevision: 1, ResultOwnerVersion: 1, After: AgentModelSelection{ModelID: r.ModelID}}
}

func TestAgentReferenceChangeCreateUpdateAndBinding(t *testing.T) {
	r := agentChange(t)
	must(t, r.Validate())
	badCreate := r.Clone()
	badCreate.ExpectedOwnerVersion = ptr(f.Version(1))
	reject(t, badCreate.Validate())
	badCreate = r.Clone()
	badCreate.Before = &r.After
	reject(t, badCreate.Validate())
	b, err := AgentReferenceBinding(r)
	must(t, err)
	for _, change := range []func(*AgentReferenceChange){
		func(v *AgentReferenceChange) { v.AgentID = fresh[id.Agent](t) },
		func(v *AgentReferenceChange) { v.PlanRevision++ },
		func(v *AgentReferenceChange) { v.After.ReasoningEffort = ptr("medium") },
		func(v *AgentReferenceChange) { v.After.ApprovalModelID = ptr(fresh[Model](t)) },
		func(v *AgentReferenceChange) {
			user, _ := f.ParseID[id.User](v.Actor.Details().UserID)
			v.Actor, _ = id.NewHuman(user, fresh[id.Session](t))
		},
		func(v *AgentReferenceChange) {
			v.Command, _ = f.NewCommandIdentity("project", []string{v.ProjectID.String()}, "agent.create", "new-key")
		},
	} {
		v := r.Clone()
		change(&v)
		next, err := AgentReferenceBinding(v)
		must(t, err)
		if next == b {
			t.Fatal("binding omitted canonical owner input")
		}
	}
	r.Command, _ = f.NewCommandIdentity("project", []string{r.ProjectID.String()}, "agent.update", "update-key")
	before := r.After.Clone()
	r.Before, r.ExpectedOwnerVersion, r.ResultOwnerVersion = &before, ptr(f.Version(5)), 5
	must(t, r.Validate())
	r.ResultOwnerVersion = 6 // Body-only update still advances every reference.
	must(t, r.Validate())
	r.After.ApprovalModelID = ptr(fresh[Model](t))
	must(t, r.Validate())
	r.ResultOwnerVersion = 5
	reject(t, r.Validate())
	r.ResultOwnerVersion = 7
	reject(t, r.Validate())
	r.ResultOwnerVersion, r.Before = 6, nil
	reject(t, r.Validate())
}

func referenceSelectionPlan(t *testing.T, r ConfigurationSelectionRequest) ConfigurationSelectionPlan {
	t.Helper()
	provider := fresh[Provider](t)
	locks, err := ConfigurationSelectionLocks(r, provider)
	must(t, err)
	b, err := ConfigurationSelectionBinding(r)
	must(t, err)
	p, err := NewConfigurationSelectionPlan(NewPlanIssuer(), r, ConfigurationSelectionPlanDetails{Binding: b, Mapping: digest("b"), Locks: locks, Candidate: ConfigurationSelectionFacts{ModelID: r.ModelID, ProviderID: provider, Scope: id.SystemScope(), ModelVersion: 1, ProviderVersion: 1, Capabilities: Capabilities{InputModalities: []string{"text"}}}})
	must(t, err)
	return p
}

func TestAgentReferencePlansBindBothRolesOwnerAndLocks(t *testing.T) {
	r := agentChange(t)
	r.After.ReasoningEffort, r.After.ApprovalModelID = ptr("medium"), ptr(fresh[Model](t))
	pr, ar, err := AgentReferenceSelectionRequests(r)
	must(t, err)
	if ar == nil || ar.ReasoningEffort != nil || pr.ReasoningEffort == nil || *pr.ReasoningEffort != "medium" {
		t.Fatal("approval inherited primary effort")
	}
	primary, approval := referenceSelectionPlan(t, pr), referenceSelectionPlan(t, *ar)
	locks, err := AgentReferenceBaseLocks(r)
	must(t, err)
	ownerIssuer, issuer := NewPlanIssuer(), NewPlanIssuer()
	owner, err := NewAgentReferenceOwnerPlan(ownerIssuer, r, digest("c"), locks)
	must(t, err)
	locks = append(locks, primary.RequiredLocks()...)
	locks = append(locks, approval.RequiredLocks()...)
	p, err := NewAgentReferencePlan(issuer, r, digest("d"), locks, owner, primary, &approval)
	must(t, err)
	b, _ := AgentReferenceBinding(r)
	if !p.Matches(issuer, b, digest("d")) || p.Matches(NewPlanIssuer(), b, digest("d")) || !p.OwnerPlan().Matches(ownerIssuer, b, digest("c")) {
		t.Fatal("lost issuer or owner binding")
	}
	for i := range p.RequiredLocks() {
		bad := p.RequiredLocks()
		bad = append(bad[:i:i], bad[i+1:]...)
		_, err = NewAgentReferencePlan(issuer, r, digest("d"), bad, owner, primary, &approval)
		reject(t, err)
	}
	_, err = NewAgentReferencePlan(issuer, r, digest("d"), locks, owner, primary, nil)
	reject(t, err)
	_, err = NewAgentReferencePlan(issuer, r, digest("d"), locks, owner, approval, &primary)
	reject(t, err)
	wrong := r.Clone()
	wrong.PlanRevision++
	_, err = NewAgentReferencePlan(issuer, wrong, digest("d"), locks, owner, primary, &approval)
	reject(t, err)
	copy := p.Details()
	copy.Locks[0].Mode = f.Shared
	returned := p.ApprovalSelection()
	*returned = ConfigurationSelectionPlan{}
	if p.ApprovalSelection().Validate() != nil || !p.Matches(issuer, b, digest("d")) {
		t.Fatal("mutable plan alias")
	}
}

func TestAgentReferenceChangeCloneAndSafeFormatting(t *testing.T) {
	r := agentChange(t)
	r.After.ReasoningEffort, r.After.ApprovalModelID = ptr("medium"), ptr(fresh[Model](t))
	copy := r.Clone()
	*copy.After.ReasoningEffort = "high"
	*copy.After.ApprovalModelID = fresh[Model](t)
	if *r.After.ReasoningEffort != "medium" || *r.After.ApprovalModelID == *copy.After.ApprovalModelID {
		t.Fatal("selection alias")
	}
	raw, err := json.Marshal(r)
	must(t, err)
	for _, text := range []string{string(raw), fmt.Sprintf("%+v", r), r.LogValue().String()} {
		if strings.Contains(text, r.Actor.Details().SessionID) || strings.Contains(text, "canary") || strings.Contains(text, "medium") {
			t.Fatal("reference material escaped safe representation")
		}
	}
	reject(t, json.Unmarshal([]byte(`{}`), &r))
	var plan AgentReferencePlan
	var owner AgentReferenceOwnerPlan
	reject(t, plan.Validate())
	reject(t, owner.Validate())
	reject(t, json.Unmarshal([]byte(`{}`), &plan))
	reject(t, json.Unmarshal([]byte(`{}`), &owner))
}
