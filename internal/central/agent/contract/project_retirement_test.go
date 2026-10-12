package contract_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func projectRetirementPlan(t *testing.T) ac.ProjectRetirementPlan {
	t.Helper()
	core := coreFixture(t)
	before, err := ac.NewAgentConfig(ac.AgentConfigFields{Core: core, AllowedToolIDs: []i.ToolID{}, AllowedMountIDs: []i.MountID{}, AllowedSecretVariableIDs: []i.ProjectVariableID{}})
	if err != nil {
		t.Fatal(err)
	}
	cause := pc.LifecycleCause{OperationID: mustID[pc.Operation](t, "01900000-0000-7000-8000-000000000030"), Action: pc.Delete, ProjectVersion: 3}
	registration, err := i.RegisterService(i.ProjectLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := i.InProject(core.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := registration.Actor(cause.OperationID.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := ac.ProjectRetirementBeforeDigest(before)
	if err != nil {
		t.Fatal(err)
	}
	return ac.ProjectRetirementPlan{Change: ac.ProjectRetirementChange{Request: ac.ProjectRetirementRequest{Actor: actor, ProjectID: core.ProjectID, AgentID: core.ID, Cause: cause, ExpectedVersion: core.Version}, Before: before}, CommandID: mustID[ac.AgentCommand](t, "01900000-0000-7000-8000-000000000031"), BeforeDigest: digest}
}

func TestProjectRetirementContractBindings(t *testing.T) {
	p := projectRetirementPlan(t)
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	locks := p.Change.Request.RequiredLocks()
	if len(locks) != 3 {
		t.Fatal("incomplete original lock union")
	}
	for _, lock := range locks {
		if lock.Mode != f.Exclusive {
			t.Fatal("retirement weakened a gate")
		}
	}
	for _, tc := range []struct {
		name   string
		change func(*ac.ProjectRetirementPlan)
	}{
		{"archive", func(p *ac.ProjectRetirementPlan) { p.Change.Request.Cause.Action = pc.Archive }},
		{"wrong-operation", func(p *ac.ProjectRetirementPlan) {
			p.Change.Request.Cause.OperationID = mustID[pc.Operation](t, "01900000-0000-7000-8000-000000000032")
		}},
		{"wrong-agent", func(p *ac.ProjectRetirementPlan) {
			p.Change.Request.AgentID = mustID[i.Agent](t, "01900000-0000-7000-8000-000000000033")
		}},
		{"wrong-version", func(p *ac.ProjectRetirementPlan) { p.Change.Request.ExpectedVersion++ }},
		{"missing-command", func(p *ac.ProjectRetirementPlan) { p.CommandID = ac.AgentCommandID{} }},
		{"changed-full-before", func(p *ac.ProjectRetirementPlan) {
			fields := p.Change.Before.Fields()
			fields.Core.Instructions = "different original instructions"
			p.Change.Before, _ = ac.NewAgentConfig(fields)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := p.Clone()
			tc.change(&bad)
			if bad.Validate() == nil {
				t.Fatal("changed retirement binding accepted")
			}
		})
	}
}

func TestProjectRetirementPlanKeepsPrivateBody(t *testing.T) {
	p := projectRetirementPlan(t)
	for _, value := range []any{p.Change.Request, p.Change, p} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{string(raw), fmt.Sprintf("%#v", value)} {
			if strings.Contains(text, p.Change.Before.Fields().Core.Instructions) || strings.Contains(text, p.CommandID.String()) {
				t.Fatal("retirement diagnostic exposed private original body or binding")
			}
		}
	}
	fields := p.Change.Before.Fields()
	fields.Core.Instructions = "mutated detached projection"
	if p.Clone().Change.Before.Fields().Core.Instructions == fields.Core.Instructions {
		t.Fatal("original Before alias escaped")
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
}
