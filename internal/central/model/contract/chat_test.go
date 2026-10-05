package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestChatClosedPartsAndImmutableCopy(t *testing.T) {
	x := setup(t)
	r := ModelRequest{Actor: x.actor, CallID: x.call, Consumer: x.c, Model: ResolvedModel{Snapshot: x.snapshot, Consumer: x.c, LeaseOwner: x.owner}, Input: x.input(t), Messages: []Message{{Role: "user", Parts: []MessagePart{{Text: &TextPart{Text: "hello"}}}}}, ToolChoice: ToolChoice{Kind: "none"}, ResponseFormat: ResponseFormat{Kind: "text"}, RetryClass: AgentRetry}
	must(t, r.Validate())
	c := r.Clone()
	c.Messages[0].Parts[0].Text.Text = "changed"
	if r.Messages[0].Parts[0].Text.Text != "hello" {
		t.Fatal("message aliases")
	}
	c = r.Clone()
	c.Messages[0].Parts[0].ToolCall = &ToolCallPart{ID: "call", Name: "tool", Arguments: json.RawMessage(`{}`)}
	reject(t, c.Validate())
	c.Messages[0].Parts[0].Text = nil
	reject(t, c.Validate())
	c.Messages[0].Role = "assistant"
	must(t, c.Validate())
	c.Messages[0].Parts[0].ToolCall.Arguments = json.RawMessage(`{"a":1,"a":2}`)
	reject(t, c.Validate())
	c = r.Clone()
	c.RetryClass = BoundedRetry
	reject(t, c.Validate())
	c = r.Clone()
	c.Model.LeaseOwner, _ = sc.NewCredentialLeaseOwner(sc.ModelCallOwner, fresh[Call](t).String())
	reject(t, c.Validate())
	c = r.Clone()
	c.ToolChoice = ToolChoice{Kind: "named", Name: "missing"}
	reject(t, c.Validate())
	c.Tools = []Tool{{Name: "missing", InputSchema: json.RawMessage(`{}`)}}
	must(t, c.Validate())
	c.Tools = append(c.Tools, c.Tools[0])
	reject(t, c.Validate())
	reject(t, (ResponseFormat{Kind: "text", Schema: json.RawMessage(`{}`)}).Validate())
	reject(t, (ToolChoice{Kind: "auto", Name: "extra"}).Validate())
}
func TestFramePayloadAndExactAttemptIdentity(t *testing.T) {
	x := setup(t)
	r := ModelFrame{CallID: x.call, InvocationID: x.inv, AttemptIndex: 1, Sequence: 1, Kind: "attempt_started", Start: &FrameStart{StartedAt: &x.now}}
	must(t, r.Validate())
	c := r.Clone()
	c.Text = &FrameText{Text: "extra"}
	reject(t, c.Validate())
	c = r.Clone()
	c.Start.MessageID = "extra"
	reject(t, c.Validate())
	r.Kind = "tool_call_delta"
	r.Start = nil
	r.Tool = &FrameTool{CallID: "tool", Fragment: `{"a":`}
	must(t, r.Validate())
	c = r.Clone()
	c.Tool.Arguments = json.RawMessage(`{}`)
	reject(t, c.Validate())
	r.Kind = "tool_call_end"
	r.Tool.Fragment = ""
	r.Tool.Arguments = json.RawMessage(`{}`)
	must(t, r.Validate())
	r.Kind = "message_end"
	r.Tool = nil
	r.Response = &ModelResponse{CallID: x.call, InvocationID: x.inv, Message: Message{Role: "assistant", Parts: []MessagePart{{Text: &TextPart{Text: "done"}}}}, FinishReason: "stop", Usage: Usage{Source: UnknownUsage}}
	must(t, r.Validate())
	c = r.Clone()
	c.Response.InvocationID = fresh[Invocation](t)
	reject(t, c.Validate())
	c = r.Clone()
	c.Response.FinishReason = "error"
	reject(t, c.Validate())
	c = r.Clone()
	c.Partial = true
	reject(t, c.Validate())
	r.Kind = "attempt_aborted"
	r.Response = nil
	r.Error = &ModelError{Category: "network", Dispatched: true, PartialOutput: true}
	r.Partial = true
	must(t, r.Validate())
	r.Error.Dispatched = false
	reject(t, r.Validate())
}
func TestSensitiveDTOFormattingDoesNotExposePayload(t *testing.T) {
	const canary = "sensitive-secret-canary"
	x := setup(t)
	x.snapshot.Endpoint = "https://" + canary + ".example"
	x.snapshot.HeaderOverwrite = map[string]string{"X-Custom": canary}
	x.snapshot.Parameters = json.RawMessage(`{"opaque":"` + canary + `"}`)
	values := []any{x.snapshot, TextPart{Text: canary}, ToolCallPart{ID: "c", Name: "n", Arguments: json.RawMessage(`{"v":"` + canary + `"}`)}, ProviderInput{Name: canary}, ProviderMetadataPart{OpaqueRef: canary}, ModelError{Category: "unknown", Code: canary}, ImageRequest{Prompt: canary}, ModelRequest{Messages: []Message{{Parts: []MessagePart{{Text: &TextPart{Text: canary}}}}}}}
	for _, v := range values {
		for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
			if strings.Contains(fmt.Sprintf(format, v), canary) {
				t.Fatalf("format leaked %T", v)
			}
		}
		var out bytes.Buffer
		l := slog.New(slog.NewJSONHandler(&out, nil))
		l.Info("test", "value", v)
		if strings.Contains(out.String(), canary) {
			t.Fatalf("slog leaked %T", v)
		}
	}
}

func TestEveryFrameVariantRequiresItsOwnPayload(t *testing.T) {
	x := setup(t)
	base := ModelFrame{CallID: x.call, InvocationID: x.inv, AttemptIndex: 1, Sequence: 1}
	frames := []ModelFrame{
		{Kind: "attempt_started", Start: &FrameStart{StartedAt: &x.now}},
		{Kind: "message_start", Start: &FrameStart{MessageID: "message"}},
		{Kind: "text_delta", Text: &FrameText{Text: "日", OffsetUTF8: 3}},
		{Kind: "reasoning_delta", Text: &FrameText{Text: "reasoning", OffsetUTF8: 0}},
		{Kind: "tool_call_start", Tool: &FrameTool{CallID: "tool-id", Name: "tool"}},
		{Kind: "tool_call_delta", Tool: &FrameTool{CallID: "tool-id", Fragment: `{"key":`}},
		{Kind: "tool_call_end", Tool: &FrameTool{CallID: "tool-id", Arguments: json.RawMessage(`{"key":1}`)}},
		{Kind: "usage_update", Usage: &Usage{Source: ProviderUsage, TotalTokens: ptr(TokenCount(0))}},
		{Kind: "message_end", Response: &ModelResponse{CallID: x.call, InvocationID: x.inv, Message: Message{Role: "assistant", Parts: []MessagePart{{Text: &TextPart{Text: ""}}}}, FinishReason: "length", Usage: Usage{Source: UnknownUsage}}},
		{Kind: "attempt_aborted", Error: &ModelError{Category: "timeout"}},
		{Kind: "call_failed", Error: &ModelError{Category: "authentication"}},
		{Kind: "call_cancelled", CancelReason: "watchdog"},
	}
	for _, r := range frames {
		r.CallID = base.CallID
		r.InvocationID = base.InvocationID
		r.AttemptIndex = base.AttemptIndex
		r.Sequence = base.Sequence
		must(t, r.Validate())
		r.Kind = "unknown"
		reject(t, r.Validate())
	}
}
