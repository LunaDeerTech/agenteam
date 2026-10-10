package contract_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	a "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	e "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func configFixture(t *testing.T) a.AgentConfig {
	t.Helper()
	v, err := a.NewAgentConfig(a.AgentConfigFields{Core: coreFixture(t), AllowedToolIDs: []i.ToolID{}, AllowedMountIDs: []i.MountID{}, AllowedSecretVariableIDs: []i.ProjectVariableID{}})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func createFixture(t *testing.T) a.AgentCreate {
	t.Helper()
	c := coreFixture(t)
	v, err := a.NewAgentCreate(a.AgentCreateFields{AgentID: c.ID, Name: c.Name, ModelRef: c.ModelRef, InjectAgentsMD: true, ApprovalPolicy: a.ApprovalDefault, AllowedToolIDs: []i.ToolID{}, AllowedMountIDs: []i.MountID{}, AllowedSecretVariableIDs: []i.ProjectVariableID{}})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestAgentConfigurationReferencesAndStrictRaw(t *testing.T) {
	v := configFixture(t)
	fields := v.Fields()
	fields.AllowedToolIDs = []i.ToolID{mustID[i.Tool](t, "01900000-0000-7000-8000-00000000000e")}
	v, err := a.NewAgentConfig(fields)
	if err != nil {
		t.Fatal(err)
	}
	fields.AllowedToolIDs[0] = i.ToolID{}
	if v.Fields().AllowedToolIDs[0].Validate() != nil {
		t.Fatal("constructor shared references")
	}
	copy := v.Fields()
	copy.AllowedToolIDs[0] = i.ToolID{}
	*copy.Core.DisplayName = "other"
	if v.Fields().AllowedToolIDs[0].Validate() != nil || *v.Fields().Core.DisplayName == "other" {
		t.Fatal("projection shared storage")
	}
	raw := mustJSON(t, v)
	round, err := a.DecodeAgentConfig(raw)
	if err != nil || !reflect.DeepEqual(v.Fields(), round.Fields()) {
		t.Fatal("config round trip", err)
	}
	bad := []string{
		strings.Replace(string(raw), `"allowed_tool_ids":[`, `"allowed_tool_ids":null,"unused":[`, 1),
		strings.Replace(string(raw), `"version":"1"`, `"version":"1","version":"1"`, 1),
		strings.Replace(string(raw), `"allowed_tool_ids"`, `"Allowed_tool_ids"`, 1),
		string(raw) + ` {}`, strings.Repeat(" ", a.MaxAgentCoreBytes) + string(raw),
	}
	for n, wire := range bad {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			if _, err := a.DecodeAgentConfig([]byte(wire)); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
	max := configFixture(t).Fields()
	max.Core.Description = strings.Repeat("<", a.MaxAgentDescriptionBytes)
	max.Core.Instructions = strings.Repeat(">", a.MaxAgentInstructionsBytes)
	for n := range 128 {
		max.AllowedToolIDs = append(max.AllowedToolIDs, mustID[i.Tool](t, fmt.Sprintf("01900000-0000-7000-8000-%012x", n+1)))
		max.AllowedMountIDs = append(max.AllowedMountIDs, mustID[i.Mount](t, fmt.Sprintf("01900000-0000-7000-8000-%012x", n+1)))
	}
	v, err = a.NewAgentConfig(max)
	if err != nil {
		t.Fatal(err)
	}
	if raw := mustJSON(t, v); len(raw) >= a.MaxAgentCoreBytes {
		t.Fatal("legal worst escaping exceeds cap")
	}
	max.AllowedSecretVariableIDs = []i.ProjectVariableID{mustID[i.ProjectVariable](t, "01900000-0000-7000-8000-000000000001")}
	if _, err := a.NewAgentConfig(max); err == nil {
		t.Fatal("combined limit ignored")
	}
}

func TestAgentCreateDefaultsAndUpdatePresence(t *testing.T) {
	r := createFixture(t)
	raw := string(mustJSON(t, r))
	if !*r.Fields().AddSkillsEnabled || !*r.Fields().InstallSkillEnabled {
		t.Fatal("missing default true")
	}
	omitted := strings.ReplaceAll(strings.ReplaceAll(raw, `"add_skills_enabled":true,`, ""), `,"install_skill_enabled":true`, "")
	decoded, err := a.DecodeAgentCreate([]byte(omitted))
	if err != nil || string(mustJSON(t, decoded)) != raw {
		t.Fatal("omission/default semantics", err)
	}
	for _, name := range []string{"add_skills_enabled", "install_skill_enabled", "inject_agents_md", "allowed_tool_ids"} {
		t.Run(name, func(t *testing.T) {
			var m map[string]json.RawMessage
			if json.Unmarshal([]byte(raw), &m) != nil {
				t.Fatal("fixture")
			}
			m[name] = json.RawMessage("null")
			if _, err := a.DecodeAgentCreate(mustJSON(t, m)); err == nil {
				t.Fatal("null silently defaulted")
			}
		})
	}
	for _, wire := range []string{`{}`, `{"name":null}`, `{"allowed_tool_ids":null}`, `{"version":"2"}`, `{"name":"Alpha","name":"Beta"}`, `{"display_name":"\ud800"}`} {
		if _, err := a.DecodeAgentUpdate([]byte(wire)); err == nil {
			t.Fatal("bad update accepted")
		}
	}
	patch, err := a.DecodeAgentUpdate([]byte(`{"display_name":null,"inject_agents_md":false,"allowed_tool_ids":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	base := configFixture(t)
	next, err := patch.Proposed(base)
	if err != nil {
		t.Fatal(err)
	}
	n, b := next.Fields(), base.Fields()
	if n.Core.DisplayName != nil || n.Core.InjectAgentsMD || n.Core.ModelRef != b.Core.ModelRef || n.Core.Version != b.Core.Version || !n.Core.UpdatedAt.Time().Equal(b.Core.UpdatedAt.Time()) || n.AllowedToolIDs == nil || len(n.AllowedToolIDs) != 0 {
		t.Fatal("patch lost presence or manufactured defaults/version")
	}
	if string(mustJSON(t, patch)) != `{"allowed_tool_ids":[],"display_name":null,"inject_agents_md":false}` {
		t.Fatal("patch expanded omission")
	}
	bad := a.AgentUpdateFields{DisplayName: a.NullableChange[string]{Value: ptr("hidden")}}
	if _, err := a.NewAgentUpdate(bad); err == nil {
		t.Fatal("absent field retained hidden payload")
	}
}

func TestAgentCommandSemanticIdentity(t *testing.T) {
	c := coreFixture(t)
	user := mustID[i.User](t, "01900000-0000-7000-8000-000000000010")
	session := mustID[i.Session](t, "01900000-0000-7000-8000-000000000011")
	actor, err := i.NewHuman(user, session)
	if err != nil {
		t.Fatal(err)
	}
	meta := f.CommandMeta{RequestID: mustID[f.Request](t, "01900000-0000-7000-8000-000000000012"), IdempotencyKey: "agent-fixture-one"}
	request := createFixture(t)
	first, err := a.AgentCommandDigest(actor, meta, c.ProjectID, c.ID, a.CreateAgentCommand, request)
	if err != nil {
		t.Fatal(err)
	}
	other, err := i.NewHuman(user, mustID[i.Session](t, "01900000-0000-7000-8000-000000000013"))
	if err != nil {
		t.Fatal(err)
	}
	meta.RequestID = mustID[f.Request](t, "01900000-0000-7000-8000-000000000014")
	meta.IdempotencyKey = "agent-fixture-two"
	second, err := a.AgentCommandDigest(other, meta, c.ProjectID, c.ID, a.CreateAgentCommand, request)
	if err != nil || first != second {
		t.Fatal("transport/session affected semantic")
	}
	d := request.Fields()
	d.AddSkillsEnabled = ptr(false)
	disabled, err := a.NewAgentCreate(d)
	if err != nil {
		t.Fatal(err)
	}
	third, err := a.AgentCommandDigest(actor, meta, c.ProjectID, c.ID, a.CreateAgentCommand, disabled)
	if err != nil || third == first {
		t.Fatal("default flag not bound")
	}
	identity, err := a.AgentCommandIdentity(c.ProjectID, a.CreateAgentCommand, meta.IdempotencyKey)
	if err != nil || identity.Namespace() != "project" || !reflect.DeepEqual(identity.OwnerIDs(), []string{c.ProjectID.String()}) {
		t.Fatal("command namespace changed")
	}
	meta.ExpectedVersion = ptr(f.Version(1))
	if _, err := a.AgentCommandDigest(actor, meta, c.ProjectID, c.ID, a.CreateAgentCommand, request); err == nil {
		t.Fatal("create accepted expected version")
	}
}

func TestAgentMutationAndLookupUnion(t *testing.T) {
	config := configFixture(t)
	id := mustID[e.EventIdentity](t, "01900000-0000-7000-8000-000000000015")
	changed, err := a.NewAgentMutation(a.AgentMutationFields{Agent: config, Changed: true, EventIDs: []e.EventID{id}})
	if err != nil {
		t.Fatal(err)
	}
	noop, err := a.NewAgentMutation(a.AgentMutationFields{Agent: config, EventIDs: []e.EventID{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, receipt := range []a.AgentMutation{changed, noop} {
		lookup, err := a.NewAgentCommandLookup(a.AgentLookupCommitted, &receipt)
		if err != nil {
			t.Fatal(err)
		}
		round, err := a.DecodeAgentCommandLookup(mustJSON(t, lookup))
		if err != nil || round.Status() != a.AgentLookupCommitted || round.Receipt() == nil {
			t.Fatal("lookup round trip", err)
		}
	}
	for _, status := range []a.AgentLookupStatus{a.AgentLookupInProgress, a.AgentLookupNotObserved} {
		if _, err := a.NewAgentCommandLookup(status, &changed); err == nil {
			t.Fatal("uncommitted receipt accepted")
		}
		if _, err := a.NewAgentCommandLookup(status, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.NewAgentMutation(a.AgentMutationFields{Agent: config, Changed: true, EventIDs: []e.EventID{}}); err == nil {
		t.Fatal("changed without event")
	}
	if _, err := a.NewAgentMutation(a.AgentMutationFields{Agent: config, EventIDs: []e.EventID{id}}); err == nil {
		t.Fatal("noop published event")
	}
}
