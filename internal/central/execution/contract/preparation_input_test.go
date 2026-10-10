package contract_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution/prompt"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	mt "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	secret "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	skill "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func inputID[K any](t *testing.T, n int) f.ID[K] {
	t.Helper()
	id, err := f.ParseID[K](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	inputOK(t, err)
	return id
}
func inputOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func inputFixture(t *testing.T) ec.PreparationInputFields {
	t.Helper()
	p, a, e := inputID[i.Project](t, 1), inputID[i.Agent](t, 2), inputID[i.Execution](t, 3)
	now, err := f.ParseInstant("2026-10-10T12:00:00.123456Z")
	inputOK(t, err)
	toolID, secretID := inputID[i.Tool](t, 4), inputID[i.ProjectVariable](t, 5)
	modelID := inputID[mc.Model](t, 6)
	agent, err := ac.NewAgentConfig(ac.AgentConfigFields{Core: ac.AgentCore{ID: a, ProjectID: p, Name: "capture-agent", NormalizedName: "capture-agent", Description: "Agent description", Instructions: "agent-private-canary", InjectAgentsMD: false, ModelRef: modelID, ApprovalPolicy: ac.ApprovalDefault, Lifecycle: ac.AgentActive, Version: 2, CreatedAt: now, UpdatedAt: now}, AllowedToolIDs: []i.ToolID{toolID}, AllowedMountIDs: []i.MountID{}, AllowedSecretVariableIDs: []i.ProjectVariableID{secretID}})
	inputOK(t, err)
	request := ec.PreparationRequest{ExecutionID: e, Launch: ec.LaunchRequest{ProjectID: p, AgentID: a, Trigger: ec.Trigger{Kind: "task", TaskID: inputID[struct{}](t, 7).String()}, Purpose: "task/work", Policy: ec.Policy{SchemaVersion: 1, DeniedToolIDs: []i.ToolID{}, AllowedResourceConstraints: []json.RawMessage{}}, Lineage: ec.Lineage{DispatchID: inputID[struct{}](t, 8).String()}, Meta: f.CommandMeta{RequestID: inputID[f.Request](t, 9), IdempotencyKey: "capture-input-original"}}}
	// Deliberately noncanonical source bytes: the provider's exact digest must
	// survive the outer input's canonical encoding without recanonicalization.
	triggerRaw := []byte("{\"body\":\"trigger-private-canary\", \"version\":\"1\"}\n")
	trigger, err := ec.NewCapturedTriggerInput(ec.TriggerInputRef{ProviderType: "task", SchemaVersion: 1, InputID: inputID[ec.TriggerInput](t, 10), Digest: ec.TriggerInputDigest(triggerRaw)}, triggerRaw)
	inputOK(t, err)
	scope, err := i.InProject(p)
	inputOK(t, err)
	modelRef, err := secret.NewCredentialRef(inputID[secret.Credential](t, 11), scope)
	inputOK(t, err)
	envRef, err := secret.NewCredentialRef(inputID[secret.Credential](t, 12), scope)
	inputOK(t, err)
	owner, err := secret.NewCredentialLeaseOwner(secret.ExecutionOwner, e.String())
	inputOK(t, err)
	model := mc.ResolvedModel{Snapshot: mc.ConfigSnapshot{ID: inputID[mc.Snapshot](t, 13), Identity: mc.ModelIdentity{ProviderID: inputID[mc.Provider](t, 14), ModelID: modelID, ProviderName: "provider", ModelName: "chat", ProviderModelID: "upstream", Protocol: mc.OpenAIChat, Profile: mc.OpenAIChatV1, ModelType: mc.ChatModel, AdapterRevision: "native-v1"}, Endpoint: "https://capture.example/v1", Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), HeaderOverwrite: map[string]string{}, Capabilities: mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"text"}}, CredentialRef: &modelRef}, Consumer: mc.Consumer{Kind: mc.AgentConsumer, Purpose: mc.AgentGeneration, ProjectID: p, AgentID: &a, ExecutionID: &e}, LeaseOwner: owner, CredentialLease: &secret.CredentialLease{LeaseID: inputID[secret.Lease](t, 15), CredentialRef: modelRef}}
	variable, err := pv.NewVariable(pv.VariableFields{ID: inputID[i.ProjectVariable](t, 16), ProjectID: p, Type: pv.VariableType, Name: "EXAMPLE_SETTING", Description: "ordinary setting", Value: "ordinary-private-canary", Version: 1, CreatedAt: now, UpdatedAt: now})
	inputOK(t, err)
	secretVariable, err := pv.NewSecretVariable(pv.SecretVariableFields{ID: secretID, ProjectID: p, Type: pv.SecretVariableType, Name: "EXAMPLE_TOKEN", Description: "secret metadata", Version: 1, CreatedAt: now, UpdatedAt: now})
	inputOK(t, err)
	binding := ec.TriggerInputDigest([]byte("attempt-binding"))
	environment, err := pv.NewEnvironmentCapture(pv.EnvironmentCaptureFields{Request: pv.EnvironmentCaptureRequest{ProjectID: p, AgentID: a, ExecutionID: e}, AttemptBinding: binding, AgentVersion: 2, Variables: []pv.Variable{variable}, Secrets: []pv.ExecutionSecretVariable{{Variable: secretVariable, CredentialRef: envRef, LeaseID: inputID[secret.Lease](t, 17)}}})
	inputOK(t, err)
	result := ec.PreparationInputFields{Request: request, AttemptBinding: binding, CapturedAt: now, Project: pc.ProjectRef{ID: p, OwnerUserID: inputID[i.User](t, 18), Name: "capture-project", NormalizedName: "capture-project", Description: "project description", Lifecycle: pc.Active, Version: 1, CreatedAt: now, UpdatedAt: now}, Agent: agent, Trigger: trigger, Model: model, Tools: []tc.ExecutionTool{{ToolID: toolID, SpecRevision: 1, ModelVisibleName: "tool_" + strings.ReplaceAll(toolID.String(), "-", ""), BindingSnapshot: tc.BuiltinExecutionBinding{Binding: tc.BuiltinBinding{HandlerID: "skill.install", ContractRevision: 1}, ScopeResolverID: "skill.scope", RiskClassifierID: "skill.risk", Class: tc.OrdinaryTool}}}, Skills: skill.InitialSkillBindings{Request: skill.SkillCaptureRequest{ProjectID: p, AgentID: a, ExecutionID: e}, AssignmentSequence: 1, Bindings: []skill.SkillBinding{{SkillID: inputID[pc.Skill](t, 19), RevisionID: inputID[skill.Revision](t, 20), Revision: 1, AssignmentID: inputID[skill.Assignment](t, 21), AssignmentSequence: 1, Name: "Add Skills", Description: "Skill description", PackageSHA256: ec.TriggerInputDigest([]byte("package")), EntryPath: skill.EntryPath}}}, Environment: environment, Mounts: mt.ExecutionMountSet{Request: mt.ExecutionMountCaptureRequest{ProjectID: p, AgentID: a, ExecutionID: e}, AgentVersion: 2, Mounts: []mt.ExecutionMountMetadata{}}, PlatformPrompt: prompt.Current()}
	return result
}

func TestPreparationInputCanonicalRoundTrip(t *testing.T) {
	fields := inputFixture(t)
	input, err := ec.NewPreparationInput(fields)
	inputOK(t, err)
	raw := input.CanonicalBytes()
	canonical, err := cursor.CanonicalJSON(raw)
	inputOK(t, err)
	if !bytes.Equal(raw, canonical) || input.Digest() != ec.TriggerInputDigest(raw) {
		t.Fatal("not canonical or digest not exact")
	}
	round, err := ec.DecodePreparationInput(raw)
	inputOK(t, err)
	out := round.Fields()
	if !bytes.Equal(round.CanonicalBytes(), raw) || round.Digest() != input.Digest() || !out.Request.Equal(fields.Request) || !bytes.Equal(out.Trigger.Data(), fields.Trigger.Data()) || out.Trigger.Ref() != fields.Trigger.Ref() {
		t.Fatal("original identities or exact trigger bytes changed")
	}
	if !out.Model.CredentialLease.CredentialRef.Equal(fields.Model.CredentialLease.CredentialRef) || out.Model.CredentialLease.LeaseID != fields.Model.CredentialLease.LeaseID || out.Model.LeaseOwner.Details() != fields.Model.LeaseOwner.Details() {
		t.Fatal("model credential projection lost identity")
	}
	gotEnv, wantEnv := out.Environment.Fields(), fields.Environment.Fields()
	if !gotEnv.Secrets[0].CredentialRef.Equal(wantEnv.Secrets[0].CredentialRef) || gotEnv.Secrets[0].LeaseID != wantEnv.Secrets[0].LeaseID || gotEnv.Variables[0].Fields().Value != wantEnv.Variables[0].Fields().Value || out.PlatformPrompt.Content() != prompt.Current().Content() {
		t.Fatal("environment or prompt changed")
	}
	// All source and returned collections remain independent of the opaque input.
	fields.Tools[0].SpecRevision = 99
	fields.Skills.Bindings[0].Name = "changed"
	fields.Model.Snapshot.Parameters[0] = '!'
	out.Tools[0].SpecRevision = 88
	out.Model.Snapshot.HeaderOverwrite["changed"] = "value"
	raw[0] = '!'
	if input.Fields().Tools[0].SpecRevision != 1 || input.Fields().Skills.Bindings[0].Name != "Add Skills" || input.Fields().Model.Snapshot.HeaderOverwrite["changed"] != "" || input.Digest() != ec.TriggerInputDigest(input.CanonicalBytes()) {
		t.Fatal("mutable storage escaped")
	}
	// Secret projections contain no value/material/ciphertext; ordinary values
	// are explicitly persisted and the two distinct purpose leases are retained.
	var wire map[string]json.RawMessage
	inputOK(t, json.Unmarshal(input.CanonicalBytes(), &wire))
	var env map[string]json.RawMessage
	inputOK(t, json.Unmarshal(wire["environment"], &env))
	if bytes.Contains(env["secrets"], []byte(`"value"`)) || bytes.Contains(env["secrets"], []byte("ciphertext")) || !bytes.Contains(env["variables"], []byte("ordinary-private-canary")) {
		t.Fatal("secret/ordinary projection mixed")
	}
}

func TestPreparationInputRejectsPartialOrCrossCapture(t *testing.T) {
	tests := []struct {
		name   string
		change func(*ec.PreparationInputFields)
	}{
		{"model-other-execution", func(v *ec.PreparationInputFields) {
			e := inputID[i.Execution](t, 90)
			v.Model.Consumer.ExecutionID = &e
			o, err := secret.NewCredentialLeaseOwner(secret.ExecutionOwner, e.String())
			inputOK(t, err)
			v.Model.LeaseOwner = o
		}},
		{"model-other-selection", func(v *ec.PreparationInputFields) { v.Model.Snapshot.Identity.ModelID = inputID[mc.Model](t, 90) }},
		{"model-foreign-credential", func(v *ec.PreparationInputFields) {
			scope, err := i.InProject(inputID[i.Project](t, 90))
			inputOK(t, err)
			r, err := secret.NewCredentialRef(inputID[secret.Credential](t, 91), scope)
			inputOK(t, err)
			v.Model.Snapshot.CredentialRef = &r
			v.Model.CredentialLease.CredentialRef = r
		}},
		{"environment-other-attempt", func(v *ec.PreparationInputFields) {
			d := v.Environment.Fields()
			d.AttemptBinding = ec.TriggerInputDigest([]byte("other"))
			var err error
			v.Environment, err = pv.NewEnvironmentCapture(d)
			inputOK(t, err)
		}},
		{"environment-lease-collision", func(v *ec.PreparationInputFields) {
			d := v.Environment.Fields()
			d.Secrets[0].LeaseID = v.Model.CredentialLease.LeaseID
			var err error
			v.Environment, err = pv.NewEnvironmentCapture(d)
			inputOK(t, err)
		}},
		{"missing-secret-selection", func(v *ec.PreparationInputFields) {
			d := v.Environment.Fields()
			d.Secrets = []pv.ExecutionSecretVariable{}
			var err error
			v.Environment, err = pv.NewEnvironmentCapture(d)
			inputOK(t, err)
		}},
		{"skill-other-execution", func(v *ec.PreparationInputFields) { v.Skills.Request.ExecutionID = inputID[i.Execution](t, 90) }},
		{"mount-stale-agent", func(v *ec.PreparationInputFields) { v.Mounts.AgentVersion = 1 }},
		{"mount-nil-is-not-empty", func(v *ec.PreparationInputFields) { v.Mounts.Mounts = nil }},
		{"missing-tool", func(v *ec.PreparationInputFields) { v.Tools = []tc.ExecutionTool{} }},
		{"denied-tool", func(v *ec.PreparationInputFields) {
			v.Request.Launch.Policy.DeniedToolIDs = []i.ToolID{v.Tools[0].ToolID}
		}},
		{"unbound-agents-md", func(v *ec.PreparationInputFields) {
			d := v.Agent.Fields()
			d.Core.InjectAgentsMD = true
			var err error
			v.Agent, err = ac.NewAgentConfig(d)
			inputOK(t, err)
		}},
		{"uninterpreted-policy", func(v *ec.PreparationInputFields) {
			v.Request.Launch.Policy.AllowedResourceConstraints = []json.RawMessage{json.RawMessage(`{"scope":"unknown"}`)}
		}},
		{"missing-platform", func(v *ec.PreparationInputFields) { v.PlatformPrompt = ec.PlatformPrompt{} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			v := inputFixture(t)
			test.change(&v)
			if _, err := ec.NewPreparationInput(v); err == nil {
				t.Fatal("partial or cross-capture input accepted")
			}
		})
	}
	// A real provider may legitimately return no ordinary tools after denial;
	// this shape is distinct from nil or silently retaining the denied tool.
	v := inputFixture(t)
	v.Request.Launch.Policy.DeniedToolIDs = []i.ToolID{v.Tools[0].ToolID}
	v.Tools = []tc.ExecutionTool{}
	_, err := ec.NewPreparationInput(v)
	inputOK(t, err)
}

func TestPreparationInputClosedEncodingAndSafeDefaults(t *testing.T) {
	input, err := ec.NewPreparationInput(inputFixture(t))
	inputOK(t, err)
	raw := input.CanonicalBytes()
	mutations := []struct {
		name   string
		change func(map[string]json.RawMessage)
	}{
		{"unknown-version", func(w map[string]json.RawMessage) { w["schema_version"] = json.RawMessage(`"2"`) }},
		{"missing-required", func(w map[string]json.RawMessage) { delete(w, "captured_at") }},
		{"wrong-command", func(w map[string]json.RawMessage) { w["command_identity"] = json.RawMessage(`"other"`) }},
		{"wrong-launch-digest", func(w map[string]json.RawMessage) {
			w["launch_digest"], _ = json.Marshal(ec.TriggerInputDigest([]byte("other")))
		}},
		{"case-alias", func(w map[string]json.RawMessage) {
			w["Schema_version"] = w["schema_version"]
			delete(w, "schema_version")
		}},
		{"unexpected-field", func(w map[string]json.RawMessage) { w["unexpected"] = json.RawMessage(`true`) }},
		{"prompt-digest", func(w map[string]json.RawMessage) {
			var p map[string]json.RawMessage
			inputOK(t, json.Unmarshal(w["platform_prompt"], &p))
			p["content"] = json.RawMessage(`"different"`)
			w["platform_prompt"], _ = json.Marshal(p)
		}},
		{"secret-value", func(w map[string]json.RawMessage) {
			w["environment"] = bytes.Replace(w["environment"], []byte(`"secrets":[{`), []byte(`"secrets":[{"value":"must-not-store",`), 1)
		}},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			var w map[string]json.RawMessage
			inputOK(t, json.Unmarshal(raw, &w))
			test.change(w)
			b, err := json.Marshal(w)
			inputOK(t, err)
			b, err = cursor.CanonicalJSON(b)
			inputOK(t, err)
			if _, err := ec.DecodePreparationInput(b); err == nil {
				t.Fatal("nonclosed encoding accepted")
			}
		})
	}
	for _, b := range [][]byte{append([]byte(" "), raw...), append(bytes.Clone(raw), []byte(" {}")...), bytes.Replace(raw, []byte(`"schema_version":"1"`), []byte(`"schema_version":"1","schema_version":"1"`), 1), bytes.Repeat([]byte(" "), ec.MaxPreparationInputBytes+1)} {
		if _, err := ec.DecodePreparationInput(b); err == nil {
			t.Fatal("invalid encoding accepted")
		}
	}
	for _, v := range []any{input, input.Fields(), input.Fields().PlatformPrompt} {
		encoded, err := json.Marshal(v)
		inputOK(t, err)
		output := fmt.Sprintf("%v %+v %#v", v, v, v) + string(encoded) + slog.AnyValue(v).Resolve().String()
		for _, canary := range []string{"agent-private-canary", "trigger-private-canary", "ordinary-private-canary", "Knowledge Retrieval", input.Fields().Request.ExecutionID.String()} {
			if strings.Contains(output, canary) {
				t.Fatal("default projection exposed captured input")
			}
		}
	}
	var out ec.PreparationInput
	if json.Unmarshal(raw, &out) == nil || out.Validate() == nil {
		t.Fatal("generic JSON decode bypassed explicit codec")
	}
}
