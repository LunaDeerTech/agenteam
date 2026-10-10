package contract

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func configurationRequest(t *testing.T) ConfigurationSelectionRequest {
	t.Helper()
	a, err := id.NewHuman(fresh[id.User](t), fresh[id.Session](t))
	must(t, err)
	r := ConfigurationSelectionRequest{Actor: a, ProjectID: fresh[id.Project](t), ModelID: fresh[Model](t), Purpose: AgentModelConfiguration}
	r.Command, err = f.NewCommandIdentity("project", []string{r.ProjectID.String()}, "agent.create", "configuration-command-canary")
	must(t, err)
	return r
}

func TestConfigurationSelectionBindingIncludesCurrentIdentityAndIntent(t *testing.T) {
	r := configurationRequest(t)
	b, err := ConfigurationSelectionBinding(r)
	must(t, err)
	for _, change := range []func(*ConfigurationSelectionRequest){
		func(v *ConfigurationSelectionRequest) {
			user, _ := f.ParseID[id.User](v.Actor.Details().UserID)
			v.Actor, _ = id.NewHuman(user, fresh[id.Session](t))
		},
		func(v *ConfigurationSelectionRequest) {
			v.ProjectID = fresh[id.Project](t)
			v.Command, _ = f.NewCommandIdentity("project", []string{v.ProjectID.String()}, "agent.create", "configuration-command-canary")
		},
		func(v *ConfigurationSelectionRequest) {
			v.Command, _ = f.NewCommandIdentity("project", []string{v.ProjectID.String()}, "agent.update", "configuration-command-canary")
		},
		func(v *ConfigurationSelectionRequest) {
			v.Command, _ = f.NewCommandIdentity("project", []string{v.ProjectID.String()}, "agent.create", "another-key")
		},
		func(v *ConfigurationSelectionRequest) { v.ModelID = fresh[Model](t) },
		func(v *ConfigurationSelectionRequest) { v.Purpose = ApprovalModelConfiguration },
		func(v *ConfigurationSelectionRequest) { v.ReasoningEffort = ptr("medium") },
	} {
		v := r.Clone()
		change(&v)
		other, err := ConfigurationSelectionBinding(v)
		must(t, err)
		if other == b {
			t.Fatal("binding omitted original intent or current Session")
		}
	}
	r.Purpose, r.ReasoningEffort = ApprovalModelConfiguration, ptr("medium")
	reject(t, r.Validate())
	r = configurationRequest(t)
	r.Command, _ = f.NewCommandIdentity("project", []string{fresh[id.Project](t).String()}, "agent.create", "wrong-owner")
	reject(t, r.Validate())
}

func TestConfigurationSelectionOpaquePlanRequiredLocksAndCopies(t *testing.T) {
	r := configurationRequest(t)
	provider := fresh[Provider](t)
	locks, err := ConfigurationSelectionLocks(r, provider)
	must(t, err)
	b, err := ConfigurationSelectionBinding(r)
	must(t, err)
	d := ConfigurationSelectionPlanDetails{Binding: b, Mapping: digest("a"), Locks: locks,
		Candidate: ConfigurationSelectionFacts{ModelID: r.ModelID, ProviderID: provider, Scope: id.SystemScope(), ModelVersion: 1, ProviderVersion: 2, Capabilities: Capabilities{InputModalities: []string{"text"}}}}
	issuer := NewPlanIssuer()
	p, err := NewConfigurationSelectionPlan(issuer, r, d)
	must(t, err)
	if !p.Matches(issuer, b, d.Mapping) || p.Matches(NewPlanIssuer(), b, d.Mapping) {
		t.Fatal("issuer forgery")
	}
	d.Candidate.Capabilities.InputModalities[0] = "image"
	got := p.Details()
	got.Candidate.Capabilities.InputModalities[0] = "file"
	got.Locks[0].Mode = f.Shared
	if p.Details().Candidate.Capabilities.InputModalities[0] != "text" {
		t.Fatal("plan capabilities alias caller")
	}
	for i := range locks {
		bad := p.Details()
		bad.Locks = append(bad.Locks[:i:i], bad.Locks[i+1:]...)
		_, err = NewConfigurationSelectionPlan(issuer, r, bad)
		reject(t, err)
	}
	bad := p.Details()
	user, _ := f.UserLock(r.Actor.Details().UserID)
	for i := range bad.Locks {
		if bad.Locks[i].Key.Canonical() == user.Canonical() {
			bad.Locks[i].Mode = f.Shared
		}
	}
	_, err = NewConfigurationSelectionPlan(issuer, r, bad)
	reject(t, err)
	bad = p.Details()
	bad.Candidate.Scope, _ = id.InProject(fresh[id.Project](t))
	_, err = NewConfigurationSelectionPlan(issuer, r, bad)
	reject(t, err)
	var zero ConfigurationSelectionPlan
	reject(t, zero.Validate())
	reject(t, json.Unmarshal([]byte(`{}`), &zero))
}

func TestConfigurationSelectionSafeFormattingAndClone(t *testing.T) {
	r := configurationRequest(t)
	r.ReasoningEffort = ptr("medium")
	c := r.Clone()
	*c.ReasoningEffort = "high"
	if *r.ReasoningEffort != "medium" {
		t.Fatal("request effort aliases")
	}
	raw, err := json.Marshal(r)
	must(t, err)
	for _, value := range []string{string(raw), fmt.Sprintf("%+v", r), r.LogValue().String()} {
		if strings.Contains(value, "canary") || strings.Contains(value, r.Actor.Details().SessionID) || strings.Contains(value, "medium") {
			t.Fatal("selection request escaped safe formatting")
		}
	}
	reject(t, json.Unmarshal([]byte(`{}`), &r))
}
