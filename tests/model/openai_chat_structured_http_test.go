//go:build integration

package model_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

const structuredMemorySchema = `{"type":"object","properties":{"facts":{"type":"array","items":{"type":"object","properties":{"text":{"type":"string"},"kind":{"type":["string","null"],"enum":["fact",null]},"ordinal":{"type":"integer"},"active":{"type":"boolean"}},"required":["text","kind","ordinal","active"],"additionalProperties":false}},"note":{"type":["string","null"]}},"required":["facts","note"],"additionalProperties":false}`
const structuredMemoryResult = ` {"facts":[{"text":"世界\nquote:\"","kind":"fact","ordinal":9007199254740993,"active":true}],"note":null} `
const structuredScalarSchema = `{"type":"object","properties":{"v":{"type":"integer"}},"required":["v"],"additionalProperties":false}`

func structuredWireInput(t *testing.T, v *wireFixture, key string, mode wire.ResponseMode) (wire.Request, wire.CallOptions) {
	t.Helper()
	r, o := v.input(t, key, mode)
	r.Snapshot.Identity.AdapterRevision = wire.OpenAIChatStructuredRevision
	r.Snapshot.Capabilities.StructuredOutputModes = []string{"json_schema", "text"}
	r.ResponseFormat = mc.ResponseFormat{Kind: "json_schema", Name: "memory_result", Schema: json.RawMessage(structuredMemorySchema)}
	return r, o
}

func TestModelOpenAIChatStructuredHTTP(t *testing.T) {
	v := newWireFixture(t)
	v.allow(t, true)
	t.Run("strict_native_request_and_immutable_schema", func(t *testing.T) {
		cfg := wireJSON(wireReply(structuredMemoryResult, "stop", json.RawMessage(`{"prompt_tokens":9007199254740993,"completion_tokens":0,"total_tokens":null}`), nil))
		hold := 0
		cfg.HoldAfter = &hold
		key := v.scenario(t, cfg)
		r, o := structuredWireInput(t, v, key, wire.JSONResponse)
		x := v.start(t, testContext(t), v.adapter, r, o)
		wireWait(t, "structured request did not reach held response", func() bool { return len(v.state(t, key).Requests) == 1 })
		for i := range r.ResponseFormat.Schema {
			r.ResponseFormat.Schema[i] = 'x'
		}
		r.Messages[0].Parts[0].Text.Text = "changed after Start"
		r.Snapshot.Capabilities.StructuredOutputModes[0] = "text"
		if err := v.net.Release(testContext(t), key); err != nil {
			t.Fatal(err)
		}
		result, err := x.Result(testContext(t))
		if err != nil || result.Text != structuredMemoryResult || result.End.FinishReason != "stop" || !x.Joined() {
			t.Fatal("strict result", err)
		}
		u := result.End.Usage
		if u.Source != mc.ProviderUsage || u.InputTokens == nil || *u.InputTokens != 9007199254740993 || u.OutputTokens == nil || *u.OutputTokens != 0 || u.TotalTokens != nil {
			t.Fatal("structured usage precision/unknown changed")
		}
		state := v.state(t, key)
		if len(state.Requests) != 1 {
			t.Fatal("structured request retried")
		}
		request := state.Requests[0]
		if request.Method != "POST" || request.Headers.Get("Authorization") != "Bearer "+wireCredentialCanary || request.RequestURI != "/case/"+key+"/chat/completions" {
			t.Fatal("structured request authority/origin/path")
		}
		var body map[string]json.RawMessage
		if json.Unmarshal([]byte(request.Body), &body) != nil || len(body) != 4 || string(body["model"]) != `"fixture-native-model"` || string(body["stream"]) != "false" || strings.Contains(request.Body, "changed after Start") {
			t.Fatal("unexpected structured native fields")
		}
		var format struct {
			Type   string `json:"type"`
			Schema struct {
				Name   string          `json:"name"`
				Schema json.RawMessage `json:"schema"`
				Strict bool            `json:"strict"`
			} `json:"json_schema"`
		}
		if json.Unmarshal(body["response_format"], &format) != nil || format.Type != "json_schema" || format.Schema.Name != "memory_result" || !format.Schema.Strict || !bytes.Equal(format.Schema.Schema, []byte(structuredMemorySchema)) {
			t.Fatal("schema rewritten/aliased")
		}
		wireNoLeak(t, result)
		wireNoLeak(t, x.Observe())
		wireClose(t, x)
		v.settled(t, key)
	})
	t.Run("schema_errors_are_pre_admission_zero_send", func(t *testing.T) {
		key := v.scenario(t, wireJSON(wireReply("unused", "stop", nil, nil)))
		b := wire.NewBudget()
		a := v.newAdapter(t, b)
		b.StopAdmission()
		for _, kind := range []string{"old_revision", "unknown_keyword", "optional_property", "bad_name", "malformed_required", "too_many_properties", "cancelled"} {
			r, o := structuredWireInput(t, v, key, wire.JSONResponse)
			ctx := testContext(t)
			switch kind {
			case "old_revision":
				r.Snapshot.Identity.AdapterRevision = wire.OpenAIChatTextRevision
			case "unknown_keyword":
				r.ResponseFormat.Schema = json.RawMessage(`{"type":"object","$ref":"https://example.invalid/schema"}`)
			case "optional_property":
				r.ResponseFormat.Schema = json.RawMessage(`{"type":"object","properties":{"v":{"type":"string"}},"required":[],"additionalProperties":false}`)
			case "bad_name":
				r.ResponseFormat.Name = "native.invalid:format"
			case "malformed_required":
				r.ResponseFormat.Schema = json.RawMessage(`{"type":"object","properties":{"":{"type":"string"}},"required":[null],"additionalProperties":false}`)
			case "too_many_properties":
				// Keep below C0's 64 KiB so this observes the profile bound.
				properties := map[string]any{}
				var required []string
				for i := 0; i < 257; i++ {
					name := string(rune(0x1000 + i))
					properties[name] = map[string]any{"type": "string"}
					required = append(required, name)
				}
				raw, err := json.Marshal(map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false})
				if err != nil {
					t.Fatal(err)
				}
				r.ResponseFormat.Schema = raw
			case "cancelled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			if err := r.Snapshot.Validate(); err != nil {
				t.Fatal("invalid fixture snapshot", err)
			}
			x, err := a.Start(ctx, r, o)
			if x != nil || !b.Joined() {
				t.Fatal("schema rejection acquired slot")
			}
			switch kind {
			case "bad_name", "malformed_required":
				wireFault(t, err, f.InvalidArgument)
			case "cancelled":
				wireModelError(t, err, "cancelled", "wire_cancelled", false, false)
			default:
				wireModelError(t, err, "unsupported_feature", "wire_unsupported_feature", false, false)
			}
		}
		if len(v.state(t, key).Requests) != 0 {
			t.Fatal("preflight failure reached server")
		}
	})
	t.Run("valid_native_envelope_never_bypasses_schema", func(t *testing.T) {
		for _, tc := range []struct {
			name, body, finish, category string
			refusal                      any
		}{
			{"missing", `{"facts":[]}`, "stop", "provider_error", nil},
			{"extra", `{"facts":[],"note":null,"private":"` + wireBodyCanary + `"}`, "stop", "provider_error", nil},
			{"nested_type", strings.Replace(structuredMemoryResult, `"active":true`, `"active":0`, 1), "stop", "provider_error", nil},
			{"enum", strings.Replace(structuredMemoryResult, `"kind":"fact"`, `"kind":"other"`, 1), "stop", "provider_error", nil},
			{"duplicate", `{"facts":[],"note":null,"note":"x"}`, "stop", "provider_error", nil},
			{"trailing", structuredMemoryResult + `{}`, "stop", "provider_error", nil},
			{"malformed", `{"facts":`, "stop", "provider_error", nil},
			{"length_even_valid", structuredMemoryResult, "length", "provider_error", nil},
			{"content_filter", structuredMemoryResult, "content_filter", "content_filter", nil},
			{"refusal", wireBodyCanary, "stop", "content_filter", wireBodyCanary},
		} {
			t.Run(tc.name, func(t *testing.T) {
				key := v.scenario(t, wireJSON(wireReply(tc.body, tc.finish, json.RawMessage(`{"prompt_tokens":0,"completion_tokens":2}`), tc.refusal)))
				r, o := structuredWireInput(t, v, key, wire.JSONResponse)
				x := v.start(t, testContext(t), v.adapter, r, o)
				result, err := x.Result(testContext(t))
				code := "wire_protocol_invalid"
				if tc.category == "content_filter" {
					code = ""
				}
				m := wireModelError(t, err, mc.ErrorCategory(tc.category), code, true, false)
				if result.Text != "" || result.End.FinishReason != "" || m.Retryable || x.Observe().Usage.InputTokens == nil || *x.Observe().Usage.InputTokens != 0 || x.Observe().Usage.OutputTokens == nil || *x.Observe().Usage.OutputTokens != 2 {
					t.Fatal("false success or dropped usage")
				}
				wireClose(t, x)
				if len(v.state(t, key).Requests) != 1 {
					t.Fatal("schema error retried")
				}
			})
		}
	})
	t.Run("new_revision_text_path_and_unknown_usage", func(t *testing.T) {
		for _, structured := range []bool{false, true} {
			body, finish := "not JSON; old text semantics", "length"
			if structured {
				body, finish = structuredMemoryResult, "stop"
			}
			key := v.scenario(t, wireJSON(wireReply(body, finish, nil, nil)))
			r, o := structuredWireInput(t, v, key, wire.JSONResponse)
			if !structured {
				r.ResponseFormat = mc.ResponseFormat{Kind: "text"}
			}
			x := v.start(t, testContext(t), v.adapter, r, o)
			result, err := x.Result(testContext(t))
			if err != nil || result.Text != body || string(result.End.FinishReason) != finish || result.End.Usage.Source != mc.UnknownUsage {
				t.Fatal("new revision text/unknown compatibility", err)
			}
			var native map[string]json.RawMessage
			if json.Unmarshal([]byte(v.state(t, key).Requests[0].Body), &native) != nil {
				t.Fatal("request JSON")
			}
			_, format := native["response_format"]
			if format != structured {
				t.Fatal("response format appeared on text")
			}
			wireClose(t, x)
		}
	})
}
