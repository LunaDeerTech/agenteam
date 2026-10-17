package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func jsonToolsInput(t *testing.T) (Request, CallOptions) {
	r, o := unitInput(t)
	r.Snapshot.Identity.AdapterRevision = OpenAIChatToolsRevision
	r.Snapshot.Capabilities.Streaming = false
	r.Snapshot.Capabilities.ToolCalls = true
	r.Tools = []mc.Tool{{Name: "read_file", Description: "read exact file", InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"string"}`)}}
	r.ToolChoice = mc.ToolChoice{Kind: "required"}
	return r, o
}
func jsonToolsReply(name, arguments, finish string) []byte {
	v := map[string]any{"id": "chat-1", "object": "chat.completion", "created": 1, "model": "native-model", "usage": map[string]any{"prompt_tokens": json.Number("9007199254740993"), "completion_tokens": 0}, "choices": []any{map[string]any{"index": 0, "finish_reason": finish, "message": map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "call_1", "type": "function", "function": map[string]any{"name": name, "arguments": arguments}}}}}}}
	raw, _ := json.Marshal(v)
	return raw
}
func TestJSONToolsRequestHistoryAndChoice(t *testing.T) {
	r, o := jsonToolsInput(t)
	r.Snapshot.Capabilities.Streaming = true
	r.Snapshot.Capabilities.StructuredOutputModes = []string{"text", "json_schema"}
	r.Messages = append(r.Messages,
		mc.Message{Role: "assistant", Parts: []mc.MessagePart{{ToolCall: &mc.ToolCallPart{ID: "previous", Name: "old_tool", Arguments: json.RawMessage(`{"x":9007199254740993}`)}}}},
		mc.Message{Role: "tool", Parts: []mc.MessagePart{{ToolResult: &mc.ToolResultPart{CallID: "previous", IsError: true, Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: "original error\u0000text"}}}}}}})
	for _, choice := range []mc.ToolChoice{{Kind: "none"}, {Kind: "auto"}, {Kind: "required"}, {Kind: "named", Name: "read_file"}} {
		r.ToolChoice = choice
		req, _, err := prepare(r, o)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields["response_format"] != nil {
			t.Fatal("unused schema capability changed the requested response format")
		}
		var body struct {
			Messages []struct {
				Role       string
				Content    json.RawMessage
				ToolCallID string                                          `json:"tool_call_id"`
				ToolCalls  []struct{ Function struct{ Arguments string } } `json:"tool_calls"`
			}
			Tools []struct {
				Function struct {
					Name       string
					Strict     bool
					Parameters json.RawMessage
					Output     json.RawMessage `json:"output_schema"`
				}
			}
			Parallel *bool           `json:"parallel_tool_calls"`
			Choice   json.RawMessage `json:"tool_choice"`
		}
		if json.Unmarshal(raw, &body) != nil || len(body.Messages) != 3 || body.Messages[1].ToolCalls[0].Function.Arguments != `{"x":9007199254740993}` || body.Messages[2].ToolCallID != "previous" || body.Parallel == nil || *body.Parallel || len(body.Tools) != 1 || body.Tools[0].Function.Strict || body.Tools[0].Function.Output != nil || strings.Contains(string(raw), "is_error") || strings.Contains(string(raw), "wire-material-canary") {
			t.Fatal("tool protocol or secret mapping changed")
		}
	}
	req, _, err := prepare(r, o)
	if err != nil {
		t.Fatal(err)
	}
	r.Tools[0].InputSchema[0] = '!'
	r.Messages[2].Parts[0].ToolResult.Parts[0].Text.Text = "mutated"
	raw, _ := io.ReadAll(req.Body)
	req.Body.Close()
	if strings.Contains(string(raw), "mutated") || strings.Contains(string(raw), "!\"type") {
		t.Fatal("request retained aliases")
	}
}
func TestJSONToolsRequestRejectsIncompleteHistoryAndUnsupportedMode(t *testing.T) {
	for name, mutate := range map[string]func(*Request){
		"orphan": func(r *Request) {
			r.Messages = append(r.Messages, mc.Message{Role: "tool", Parts: []mc.MessagePart{{ToolResult: &mc.ToolResultPart{CallID: "absent"}}}})
		},
		"unmatched": func(r *Request) {
			r.Messages = append(r.Messages, mc.Message{Role: "assistant", Parts: []mc.MessagePart{{ToolCall: &mc.ToolCallPart{ID: "pending", Name: "read_file", Arguments: json.RawMessage(`{}`)}}}})
		},
		"name":         func(r *Request) { r.Tools[0].Name = "bad.name" },
		"duplicate":    func(r *Request) { r.Tools = append(r.Tools, r.Tools[0]) },
		"named-absent": func(r *Request) { r.ToolChoice = mc.ToolChoice{Kind: "named", Name: "absent"} },
		"parallel":     func(r *Request) { r.Snapshot.Capabilities.ParallelToolCalls = true },
		"stream":       func(r *Request) { r.Mode = SSEResponse; r.Snapshot.Capabilities.Streaming = true },
		"schema-response": func(r *Request) {
			r.Snapshot.Capabilities.StructuredOutputModes = []string{"text", "json_schema"}
			r.ResponseFormat = mc.ResponseFormat{Kind: "json_schema", Name: "result", Schema: json.RawMessage(`{"type":"object"}`)}
		},
		"no-text-response": func(r *Request) {
			r.Snapshot.Capabilities.StructuredOutputModes = []string{"json_schema"}
		},
		"old-revision": func(r *Request) { r.Snapshot.Identity.AdapterRevision = OpenAIChatTextRevision },
		"oversize":     func(r *Request) { r.Messages[0].Parts[0].Text.Text = strings.Repeat("<", 3<<20) },
	} {
		t.Run(name, func(t *testing.T) {
			r, o := jsonToolsInput(t)
			mutate(&r)
			req, _, err := prepare(r, o)
			if err == nil || req != nil {
				t.Fatal("rejected request produced HTTP request")
			}
			if name == "schema-response" || name == "no-text-response" {
				requireModel(t, err, "unsupported_feature", "wire_unsupported_feature")
			}
		})
	}
	r, _ := jsonToolsInput(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if raw, err := prepareToolsBody(ctx, r); err == nil || raw != nil {
		t.Fatal("cancelled encoding returned bytes")
	}
}
func TestJSONToolsResponsesRetainExactArgumentsAndUsage(t *testing.T) {
	r, _ := jsonToolsInput(t)
	p := newToolPolicy(r)
	v, err := decodeNativeProfile(jsonToolsReply("read_file", `{"integer":9007199254740993,"path":"世界"}`, "tool_calls"), false, p)
	if err != nil || len(v.tools) != 1 || v.tools[0].Validate() != nil || string(v.tools[0].Arguments) != `{"integer":9007199254740993,"path":"世界"}` || !v.usageValid || v.usage.InputTokens == nil || *v.usage.InputTokens != 9007199254740993 || v.usage.OutputTokens == nil || *v.usage.OutputTokens != 0 {
		t.Fatal("tool result or exact usage lost", err)
	}
	r.Tools[0].Name = "changed"
	if _, err := decodeNativeProfile(jsonToolsReply("read_file", `{}`, "tool_calls"), false, p); err != nil {
		t.Fatal("caller altered accepted policy", err)
	}
	for index, arguments := range []string{`[]`, `{"x":1,"x":2}`, `{"x":1e400}`, `{} {}`, `{"x":"\ud800"}`, strings.Repeat(`{"x":`, 32) + `0` + strings.Repeat(`}`, 32)} {
		got, err := decodeNativeProfile(jsonToolsReply("read_file", arguments, "tool_calls"), false, p)
		if err == nil || len(got.tools) != 0 || !got.usageValid || got.usage.InputTokens == nil {
			t.Fatalf("invalid parameter case %d: err=%v tools=%d usage=%v", index, err, len(got.tools), got.usageValid)
		}
		if strings.Contains(fmt.Sprint(err), arguments) {
			t.Fatal("unsafe error")
		}
	}
	for _, name := range []string{"unknown_tool", ""} {
		if got, err := decodeNativeProfile(jsonToolsReply(name, `{}`, "tool_calls"), false, p); err == nil || len(got.tools) > 0 {
			t.Fatal("undeclared tool accepted")
		}
	}
	for _, finish := range []string{"stop", "length", "content_filter"} {
		if got, err := decodeNativeProfile(jsonToolsReply("read_file", `{}`, finish), false, p); err == nil || len(got.tools) > 0 {
			t.Fatal("incomplete tool completion accepted")
		}
	}
	if _, err := decodeNative(jsonToolsReply("read_file", `{}`, "tool_calls"), false); err == nil {
		t.Fatal("old revision accepted tools")
	}
	p.choice = mc.ToolChoice{Kind: "auto"}
	got, err := decodeNativeProfile([]byte(nativeReply("final result", "stop")), false, p)
	if err != nil || got.text != "final result" || len(got.tools) > 0 {
		t.Fatal("final text rejected", err)
	}
	p.choice = mc.ToolChoice{Kind: "required"}
	if _, err := decodeNativeProfile([]byte(nativeReply("premature", "stop")), false, p); err == nil {
		t.Fatal("required tool silently completed")
	}
}

func TestJSONToolsEmptyAndRefusalCannotPublishTools(t *testing.T) {
	r, _ := jsonToolsInput(t)
	p := newToolPolicy(r)
	p.choice = mc.ToolChoice{Kind: "auto"}
	for _, content := range []string{`null`, `""`} {
		raw := []byte(`{"id":"chat-1","object":"chat.completion","created":1,"model":"native-model","usage":{"completion_tokens":0},"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":` + content + `,"tool_calls":[]}}]}`)
		v, err := decodeNativeProfile(raw, false, p)
		if content == `null` && err == nil {
			t.Fatal("null content and zero tool calls became success")
		}
		if content == `""` && (err != nil || v.text != "") {
			t.Fatal("explicit empty text should retain text semantics", err)
		}
	}
	raw := []byte(`{"id":"chat-1","object":"chat.completion","created":1,"model":"native-model","usage":{"completion_tokens":0},"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","refusal":"refusal-canary"}}]}`)
	v, err := decodeNativeProfile(raw, false, p)
	if err != nil || !v.refusal || !v.usageValid || v.usage.OutputTokens == nil || len(v.tools) > 0 {
		t.Fatal("refusal envelope lost reliable usage", err)
	}
}

func TestJSONToolsPreserves128DefinitionsAndParameterLimit(t *testing.T) {
	r, o := jsonToolsInput(t)
	for n := 1; n < 128; n++ {
		r.Tools = append(r.Tools, mc.Tool{Name: fmt.Sprintf("core_tool_%d", n), InputSchema: json.RawMessage(`{}`)})
	}
	req, _, err := prepare(r, o)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(req.Body)
	req.Body.Close()
	var body struct{ Tools []json.RawMessage }
	if json.Unmarshal(raw, &body) != nil || len(body.Tools) != 128 || !strings.Contains(string(body.Tools[127]), "core_tool_127") {
		t.Fatal("full ToolSet was not sent")
	}
	policy := newToolPolicy(r)
	arguments := `{"value":"` + strings.Repeat("x", (1<<20)-12) + `"}`
	if len(arguments) != 1<<20 {
		t.Fatal("invalid boundary fixture")
	}
	value, err := decodeNativeProfile(jsonToolsReply("read_file", arguments, "tool_calls"), false, policy)
	if err != nil || len(value.tools) != 1 || value.tools[0].Validate() != nil || string(value.tools[0].Arguments) != arguments {
		t.Fatal("valid exact argument boundary rejected", err)
	}
	arguments = arguments[:len(arguments)-2] + `x"}`
	if got, err := decodeNativeProfile(jsonToolsReply("read_file", arguments, "tool_calls"), false, policy); err == nil || len(got.tools) > 0 || !got.usageValid {
		t.Fatal("oversize tool output published or usage lost")
	}
}
