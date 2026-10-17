package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestModelChoiceClosedReasoning(t *testing.T) {
	for _, raw := range []string{`{"mode":"default"}`, `{"mode":"on"}`, `{"mode":"off"}`, `{"mode":"effort","value":"vendor:depth"}`} {
		var r ReasoningChoice
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			t.Fatalf("valid choice: %v", err)
		}
		data, err := json.Marshal(r)
		if err != nil || string(data) != raw {
			t.Fatalf("roundtrip %s", data)
		}
	}
	for _, raw := range []string{`{}`, `null`, `{"mode":"effort"}`, `{"mode":"effort","value":null}`, `{"mode":"default","value":""}`, `{"mode":"off","value":null}`, `{"mode":"on","parameter":"evil"}`, `{"mode":"effort","value":"a b"}`} {
		var r ReasoningChoice
		if json.Unmarshal([]byte(raw), &r) == nil {
			t.Fatalf("invalid accepted %s", raw)
		}
	}
	mode := "enable_thinking"
	m := ModelInput{Type: ChatModel, Capabilities: Capabilities{Reasoning: true, ReasoningToggleMode: &mode}}
	for _, choice := range []ReasoningChoice{{Mode: "default"}, {Mode: "on"}, {Mode: "off"}} {
		if choice.ValidateFor(m) != nil {
			t.Fatal(choice)
		}
	}
	m.Capabilities.ReasoningToggleMode = nil
	if (ReasoningChoice{Mode: "off"}).ValidateFor(m) == nil {
		t.Fatal("absence is not off")
	}
	m.Capabilities.ReasoningEfforts = []ReasoningEffort{{Name: "Custom", Value: "vendor:depth"}}
	if (ReasoningChoice{Mode: "effort", Value: "vendor:depth"}).ValidateFor(m) != nil {
		t.Fatal("declared effort")
	}
	for _, choice := range []ReasoningChoice{{Mode: "on"}, {Mode: "off"}, {Mode: "effort", Value: "none"}} {
		if choice.ValidateFor(m) == nil {
			t.Fatal("invented choice")
		}
	}
	m.Type = EmbeddingModel
	if (ReasoningChoice{Mode: "effort", Value: "vendor:depth"}).ValidateFor(m) == nil {
		t.Fatal("nonchat choice")
	}
}
func TestModelNotesAndSnapshotToggle(t *testing.T) {
	x := setup(t)
	m := ModelInput{Notes: "", ProviderModelID: strings.Repeat("m", 256), Type: ChatModel, Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), Capabilities: x.snapshot.Capabilities}
	if m.Validate() != nil {
		t.Fatal("empty notes/canonical max ID")
	}
	m.Notes = strings.Repeat("🧠", 2000)
	if m.Validate() != nil {
		t.Fatal("Unicode notes max")
	}
	m.Notes += "a"
	if m.Validate() == nil {
		t.Fatal("notes max")
	}
	m.Notes = "a\x00"
	if m.Validate() == nil {
		t.Fatal("NUL notes")
	}
	for _, mode := range []string{"enable_thinking", "thinking_type"} {
		s := x.snapshot.Clone()
		s.Capabilities.Reasoning = true
		s.Capabilities.ReasoningToggleMode = &mode
		disabled := false
		s.ReasoningEnabled = &disabled
		if s.Validate() != nil {
			t.Fatal("explicit false")
		}
		clone := s.Clone()
		*clone.ReasoningEnabled = true
		*clone.Capabilities.ReasoningToggleMode = "invalid"
		if *s.ReasoningEnabled || *s.Capabilities.ReasoningToggleMode != mode {
			t.Fatal("snapshot alias")
		}
		raw, _ := json.Marshal(s)
		if !strings.Contains(string(raw), `"reasoning_enabled":false`) {
			t.Fatal("false was omitted")
		}
		s.ReasoningEffort = "depth"
		if s.Validate() == nil {
			t.Fatal("two controls")
		}
		s.ReasoningEffort = ""
		s.Capabilities.ReasoningToggleMode = nil
		if s.Validate() == nil {
			t.Fatal("undeclared toggle")
		}
	}
	x.snapshot.Identity.ModelName = strings.Repeat("m", 256)
	if x.snapshot.Validate() != nil {
		t.Fatal("canonical identity ID")
	}
	x.snapshot.Identity.ProviderName = strings.Repeat("p", 129)
	if x.snapshot.Validate() == nil {
		t.Fatal("provider name loosened")
	}
}
