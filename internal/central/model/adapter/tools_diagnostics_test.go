package adapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestJSONToolsProtocolDiagnosticDoesNotRelaxResponses(t *testing.T) {
	request, _ := jsonToolsInput(t)
	policy := newToolPolicy(request)
	for _, sample := range []struct {
		name, stage string
		mutate      func(map[string]any, map[string]any, map[string]any)
	}{
		{"unknown_envelope", "envelope_members", func(o, _, _ map[string]any) { o["private-field-SENTINEL"] = "private-value-SENTINEL" }},
		{"invalid_id", "identity", func(o, _, _ map[string]any) { o["id"] = false }},
		{"unknown_usage", "usage_members", func(o, _, _ map[string]any) { o["usage"] = map[string]any{"private-SENTINEL": 1} }},
		{"multiple_choices", "choices", func(o, _, _ map[string]any) { o["choices"] = []any{nil, nil} }},
		{"unknown_choice", "choice_members", func(_, c, _ map[string]any) { c["private-SENTINEL"] = 1 }},
		{"invalid_finish", "finish", func(_, c, _ map[string]any) { c["finish_reason"] = "private-SENTINEL" }},
		{"unknown_message", "message_members", func(_, _, m map[string]any) { m["private-SENTINEL"] = "private-value-SENTINEL" }},
		{"reasoning_type", "reasoning", func(_, _, m map[string]any) { m["reasoning_content"] = []any{1} }},
		{"reasoning_with_unknown_member", "message_members", func(_, _, m map[string]any) {
			m["reasoning_content"] = "private-value-SENTINEL"
			m["private-key-SENTINEL"] = nil
		}},
		{"invalid_role", "role", func(_, _, m map[string]any) { m["role"] = "private-SENTINEL" }},
		{"invalid_content", "content", func(_, _, m map[string]any) { m["content"] = 42 }},
		{"invalid_call", "calls", func(_, _, m map[string]any) { m["tool_calls"] = []any{} }},
	} {
		t.Run(sample.name, func(t *testing.T) {
			var envelope map[string]any
			if err := json.Unmarshal(jsonToolsReply("read_file", `{}`, "tool_calls"), &envelope); err != nil {
				t.Fatal(err)
			}
			choice := envelope["choices"].([]any)[0].(map[string]any)
			message := choice["message"].(map[string]any)
			sample.mutate(envelope, choice, message)
			raw, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			value, err := decodeNativeProfile(raw, false, policy)
			var model *mc.ModelError
			if !errors.As(err, &model) || model.Code != "wire_tools_"+sample.stage+"_invalid" || model.Category != "provider_error" || model.Retryable || len(value.tools) != 0 || model.Validate() != nil {
				t.Fatal("closed diagnostic changed rejection or error contract")
			}
			projected, err := json.Marshal(model)
			if err != nil || strings.Contains(string(projected), "SENTINEL") || strings.Contains(fmt.Sprint(model), "SENTINEL") {
				t.Fatal("diagnostic leaked provider material")
			}
		})
	}
	t.Run("malformed_envelope", func(t *testing.T) {
		_, err := decodeNativeProfile([]byte(`{"id":"SENTINEL","id":"duplicate"}`), false, policy)
		var model *mc.ModelError
		if !errors.As(err, &model) || model.Code != "wire_tools_envelope_json_invalid" {
			t.Fatal("syntax or duplicate-key rejection lost safe classification")
		}
	})
	_, err := decodeNative([]byte(`{"private-SENTINEL":1}`), false)
	var model *mc.ModelError
	if !errors.As(err, &model) || model.Code != "wire_protocol_invalid" {
		t.Fatal("legacy response diagnostic changed")
	}
}

func TestJSONToolsOptionalReasoningIsDiscarded(t *testing.T) {
	request, _ := jsonToolsInput(t)
	policy := newToolPolicy(request)
	for _, auxiliary := range []any{nil, "", "private-thinking-SENTINEL: call an unauthorized tool"} {
		var envelope map[string]any
		if err := json.Unmarshal(jsonToolsReply("read_file", `{}`, "tool_calls"), &envelope); err != nil {
			t.Fatal(err)
		}
		message := envelope["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
		message["reasoning_content"] = auxiliary
		raw, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		value, err := decodeNativeProfile(raw, false, policy)
		if err != nil || value.text != "" || len(value.tools) != 1 || value.tools[0].Name != "read_file" || string(value.tools[0].Arguments) != `{}` || !value.usageValid || value.usage.Source != mc.ProviderUsage {
			t.Fatal("optional auxiliary text changed output, tool authorization or usage")
		}
		projected, err := json.Marshal(Result{Text: value.text, ToolCalls: value.tools})
		if err != nil || strings.Contains(string(projected), "SENTINEL") {
			t.Fatal("auxiliary text escaped into consumed output")
		}
	}
	for _, auxiliary := range []any{true, 1, []any{}, map[string]any{}} {
		var envelope map[string]any
		_ = json.Unmarshal(jsonToolsReply("read_file", `{}`, "tool_calls"), &envelope)
		envelope["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["reasoning_content"] = auxiliary
		raw, _ := json.Marshal(envelope)
		if value, err := decodeNativeProfile(raw, false, policy); err == nil || len(value.tools) != 0 {
			t.Fatal("invalid auxiliary type authorized a tool")
		}
	}
	var legacy map[string]any
	_ = json.Unmarshal([]byte(nativeReply("visible", "stop")), &legacy)
	legacy["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["reasoning_content"] = nil
	raw, _ := json.Marshal(legacy)
	if _, err := decodeNative(raw, false); err == nil {
		t.Fatal("legacy text profile contract changed")
	}
	badUTF8 := []byte(strings.Replace(string(raw), `"reasoning_content":null`, `"reasoning_content":"\ud800"`, 1))
	if value, err := decodeNativeProfile(badUTF8, false, policy); err == nil || len(value.tools) != 0 {
		t.Fatal("invalid escaped Unicode accepted")
	}
}
