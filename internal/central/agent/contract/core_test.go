package contract_test

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	agent "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	model "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func ptr[T any](v T) *T { return &v }
func mustID[K any](t *testing.T, s string) foundation.ID[K] {
	t.Helper()
	v, err := foundation.ParseID[K](s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func coreFixture(t *testing.T) agent.AgentCore {
	t.Helper()
	when, err := foundation.ParseInstant("2026-10-09T12:00:00.123456Z")
	if err != nil {
		t.Fatal(err)
	}
	return agent.AgentCore{
		ID:        mustID[identity.Agent](t, "01900000-0000-7000-8000-00000000000a"),
		ProjectID: mustID[identity.Project](t, "01900000-0000-7000-8000-00000000000b"),
		Name:      "Agent-One", NormalizedName: "agent-one", DisplayName: ptr("独立 Agent"), TagColor: ptr("#12abef"),
		Description: "description sentinel\t\n\r", Instructions: "instruction sentinel <>&", InjectAgentsMD: true,
		ModelRef:        mustID[model.Model](t, "01900000-0000-7000-8000-00000000000c"),
		ReasoningEffort: ptr("medium.v1:mode-2_test"), ApprovalPolicy: agent.ApprovalAuto,
		ApprovalModelRef: ptr(mustID[model.Model](t, "01900000-0000-7000-8000-00000000000d")),
		Lifecycle:        agent.AgentActive, Version: 1, CreatedAt: when, UpdatedAt: when,
	}
}
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestAgentCoreCanonicalAndClone(t *testing.T) {
	v := coreFixture(t)
	if err := v.Validate(); err != nil {
		t.Fatal(err)
	}
	raw := mustJSON(t, v)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{"id", "project_id", "name", "normalized_name", "display_name", "tag_color", "description", "instructions", "inject_agents_md", "model_ref", "reasoning_effort", "approval_policy", "approval_model_ref", "lifecycle", "version", "created_at", "updated_at"}
	if len(fields) != len(wantKeys) {
		t.Fatalf("got %d keys", len(fields))
	}
	for _, key := range wantKeys {
		if _, ok := fields[key]; !ok {
			t.Fatalf("missing %s", key)
		}
	}
	if string(fields["version"]) != `"1"` || string(fields["created_at"]) != `"2026-10-09T12:00:00.123456Z"` {
		t.Fatal("noncanonical version/time")
	}
	round, err := agent.DecodeAgentCore(raw)
	if err != nil || !reflect.DeepEqual(round, v) {
		t.Fatalf("round trip: %v", err)
	}
	clone := v.Clone()
	*clone.DisplayName = "changed"
	*clone.TagColor = "#000000"
	*clone.ReasoningEffort = "other"
	*clone.ApprovalModelRef = v.ModelRef
	if *v.DisplayName != "独立 Agent" || *v.TagColor != "#12abef" || *v.ReasoningEffort != "medium.v1:mode-2_test" || *v.ApprovalModelRef == v.ModelRef {
		t.Fatal("Clone shares pointer storage")
	}
	for _, policy := range []agent.ApprovalPolicy{agent.ApprovalDefault, agent.ApprovalAllow} {
		v.ApprovalPolicy, v.ApprovalModelRef = policy, nil
		v.DisplayName, v.TagColor, v.ReasoningEffort = nil, nil, nil
		v.InjectAgentsMD = false
		data := mustJSON(t, v)
		if !strings.Contains(string(data), `"inject_agents_md":false`) {
			t.Fatal("false must be present")
		}
		for _, key := range []string{"display_name", "tag_color", "reasoning_effort", "approval_model_ref"} {
			if !strings.Contains(string(data), `"`+key+`":null`) {
				t.Fatalf("nullable %s omitted", key)
			}
		}
		cloned := v.Clone()
		if !reflect.DeepEqual(v, cloned) {
			t.Fatal("nil clone changed shape")
		}
	}
}

func TestAgentCoreValidationBoundaries(t *testing.T) {
	tests := []struct {
		name   string
		change func(*agent.AgentCore)
	}{
		{"zero-agent", func(v *agent.AgentCore) { v.ID = identity.AgentID{} }},
		{"zero-project", func(v *agent.AgentCore) { v.ProjectID = identity.ProjectID{} }},
		{"zero-model", func(v *agent.AgentCore) { v.ModelRef = model.ModelID{} }},
		{"zero-version", func(v *agent.AgentCore) { v.Version = 0 }},
		{"negative-version", func(v *agent.AgentCore) { v.Version = -1 }},
		{"reversed-time", func(v *agent.AgentCore) {
			v.CreatedAt, _ = foundation.NewInstant(v.UpdatedAt.Time().Add(time.Microsecond))
		}},
		{"name-short", func(v *agent.AgentCore) { v.Name, v.NormalizedName = "ab", "ab" }},
		{"name-long", func(v *agent.AgentCore) { v.Name = strings.Repeat("a", 33); v.NormalizedName = v.Name }},
		{"leading-hyphen", func(v *agent.AgentCore) { v.Name, v.NormalizedName = "-abc", "-abc" }},
		{"trailing-hyphen", func(v *agent.AgentCore) { v.Name, v.NormalizedName = "abc-", "abc-" }},
		{"name-underscore", func(v *agent.AgentCore) { v.Name, v.NormalizedName = "a_b", "a_b" }},
		{"name-unicode", func(v *agent.AgentCore) { v.Name, v.NormalizedName = "中文名", "中文名" }},
		{"normalized-mismatch", func(v *agent.AgentCore) { v.NormalizedName = v.Name }},
		{"display-empty", func(v *agent.AgentCore) { v.DisplayName = ptr("") }},
		{"display-whitespace", func(v *agent.AgentCore) { v.DisplayName = ptr(" \u2003\u3000") }},
		{"display-control", func(v *agent.AgentCore) { v.DisplayName = ptr("name\n") }},
		{"display-byte-limit", func(v *agent.AgentCore) { v.DisplayName = ptr(strings.Repeat("😀", 257)) }},
		{"display-scalar-limit", func(v *agent.AgentCore) { v.DisplayName = ptr(strings.Repeat("a", 257)) }},
		{"display-invalid-utf8", func(v *agent.AgentCore) { v.DisplayName = ptr("a\xff") }},
		{"tag-uppercase", func(v *agent.AgentCore) { v.TagColor = ptr("#12ABef") }},
		{"tag-short", func(v *agent.AgentCore) { v.TagColor = ptr("#fff") }},
		{"tag-invalid", func(v *agent.AgentCore) { v.TagColor = ptr("#gg0000") }},
		{"description-large", func(v *agent.AgentCore) { v.Description = strings.Repeat("x", 8193) }},
		{"instructions-large", func(v *agent.AgentCore) { v.Instructions = strings.Repeat("x", 32769) }},
		{"description-control", func(v *agent.AgentCore) { v.Description = "x\x00" }},
		{"instructions-control", func(v *agent.AgentCore) { v.Instructions = "x\u0085" }},
		{"body-invalid-utf8", func(v *agent.AgentCore) { v.Instructions = "\xff" }},
		{"effort-empty", func(v *agent.AgentCore) { v.ReasoningEffort = ptr("") }},
		{"effort-large", func(v *agent.AgentCore) { v.ReasoningEffort = ptr(strings.Repeat("a", 33)) }},
		{"effort-space", func(v *agent.AgentCore) { v.ReasoningEffort = ptr("a b") }},
		{"effort-unicode", func(v *agent.AgentCore) { v.ReasoningEffort = ptr("高") }},
		{"policy-unknown", func(v *agent.AgentCore) { v.ApprovalPolicy = "AUTO" }},
		{"auto-missing-model", func(v *agent.AgentCore) { v.ApprovalModelRef = nil }},
		{"default-model", func(v *agent.AgentCore) { v.ApprovalPolicy = agent.ApprovalDefault }},
		{"allow-model", func(v *agent.AgentCore) { v.ApprovalPolicy = agent.ApprovalAllow }},
		{"approval-zero-model", func(v *agent.AgentCore) { v.ApprovalModelRef = ptr(model.ModelID{}) }},
		{"lifecycle-unknown", func(v *agent.AgentCore) { v.Lifecycle = "idle" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := coreFixture(t)
			tc.change(&v)
			if v.Validate() == nil {
				t.Fatal("invalid core accepted")
			}
			if _, err := json.Marshal(v); err == nil {
				t.Fatal("invalid core serialized")
			}
		})
	}
	for _, name := range strings.Split("api assets auth login logout invite reset settings system personal diagnostics livez readyz debug support root admin", " ") {
		t.Run("reserved-"+name, func(t *testing.T) {
			v := coreFixture(t)
			v.Name, v.NormalizedName = strings.ToUpper(name), name
			if v.Validate() == nil {
				t.Fatal("reserved normalized name accepted")
			}
		})
	}
	for _, name := range []string{"a-b", "A0z", "a--b", strings.Repeat("a", 32)} {
		v := coreFixture(t)
		v.Name, v.NormalizedName = name, strings.ToLower(name)
		v.DisplayName = ptr(" x ")
		v.Description, v.Instructions = "", ""
		v.Lifecycle, v.Version = agent.AgentDeleting, math.MaxInt64
		if v.Validate() != nil {
			t.Fatal("valid boundary rejected")
		}
	}
}

func TestAgentCoreSimultaneousMaximaAndTextPreservation(t *testing.T) {
	v := coreFixture(t)
	v.Name, v.NormalizedName = strings.Repeat("a", 32), strings.Repeat("a", 32)
	v.DisplayName = ptr(strings.Repeat("😀", 256)) // 256 scalars and 1024 bytes.
	v.Description = strings.Repeat("<", 8192)
	v.Instructions = strings.Repeat(">", 32768)
	v.ReasoningEffort = ptr(strings.Repeat("A", 32))
	v.Version = math.MaxInt64
	raw := mustJSON(t, v)
	if len(raw) >= agent.MaxAgentCoreBytes || strings.Count(string(raw), `\u003c`) != 8192 || strings.Count(string(raw), `\u003e`) != 32768 {
		t.Fatal("sixfold escaping or maximum size assumption failed")
	}
	round, err := agent.DecodeAgentCore(raw)
	if err != nil || !reflect.DeepEqual(v, round) {
		t.Fatalf("maxima not lossless: %v", err)
	}
	v.DisplayName = ptr("e\u0301")
	v.Instructions = "literal \\ud800 \"quote\" \t\n\r e\u0301 \u2028 😀"
	v.Description = "\ufffd"
	round, err = agent.DecodeAgentCore(mustJSON(t, v))
	if err != nil || !reflect.DeepEqual(v, round) {
		t.Fatal("valid Unicode/escapes changed")
	}
}
