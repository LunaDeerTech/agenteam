package agentloop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// Controlled historical source exercises only the pure request/response seam.
// No committed-source authority, actual Invocation, Model or Runner is faked.
func successorResponseFixture(t *testing.T) (mc.ModelRequest, mc.ModelResponse) {
	t.Helper()
	source := roundProjectionFixture(t, false)
	prepared, err := BuildNextRoundModelRequest(context.Background(), source, requestID[mc.Call](t, 2601))
	requestOK(t, err)
	request := prepared.Request()
	input, output := mc.TokenCount(17), mc.TokenCount(9)
	response := mc.ModelResponse{
		CallID: request.CallID, InvocationID: requestID[mc.Invocation](t, 2602),
		Message: mc.Message{Role: "assistant", Parts: []mc.MessagePart{
			{Text: &mc.TextPart{Text: "private-response-before"}},
			{ToolCall: &mc.ToolCallPart{ID: "private-call", Name: request.Tools[0].Name, Arguments: bytes.Clone(source.Arguments)}},
			{Text: &mc.TextPart{Text: "private-response-after"}},
		}},
		FinishReason: "tool_calls", Usage: mc.Usage{Source: mc.ProviderUsage, InputTokens: &input, OutputTokens: &output},
		ProviderRequestID: "private-provider-request",
	}
	requestOK(t, response.Validate())
	return request, response
}

// Rebind deliberate valid request changes through the one production semantic
// codec. This makes capability/schema controls reach the response boundary,
// instead of merely failing because the original input digest was left stale.
func successorResponseRebind(t *testing.T, request mc.ModelRequest) mc.ModelRequest {
	t.Helper()
	semantic, err := mc.NewSuccessorSemantic(context.Background(), mc.SuccessorSemanticParts{
		Actor: request.Actor, Model: request.Model, Messages: request.Messages, Tools: request.Tools,
		ToolChoice: request.ToolChoice, ResponseFormat: request.ResponseFormat, RetryClass: request.RetryClass,
	})
	requestOK(t, err)
	request.Input.Digest = semantic.Digest()
	_, err = mc.ValidateSuccessorRequest(context.Background(), request)
	requestOK(t, err)
	return request
}

const successorNumberSchema = `{"type":"object","required":["n"],"properties":{"n":{"$ref":"#/$defs/n"}},"$defs":{"n":{"type":"number","minimum":1}},"additionalProperties":false}`

// These opaque identities contain non-nil functions, which DeepEqual cannot
// compare even with themselves. Compare their complete identities first; then
// exclude only those already checked handles from two independent copies.
func successorResponseSameRequest(a, b mc.ModelRequest) bool {
	if !a.Actor.Equal(b.Actor) || !a.Model.LeaseOwner.Equal(b.Model.LeaseOwner) ||
		(a.Model.Snapshot.CredentialRef == nil) != (b.Model.Snapshot.CredentialRef == nil) ||
		(a.Model.CredentialLease == nil) != (b.Model.CredentialLease == nil) {
		return false
	}
	if a.Model.Snapshot.CredentialRef != nil && !a.Model.Snapshot.CredentialRef.Equal(*b.Model.Snapshot.CredentialRef) {
		return false
	}
	if a.Model.CredentialLease != nil && !a.Model.CredentialLease.CredentialRef.Equal(b.Model.CredentialLease.CredentialRef) {
		return false
	}
	a, b = a.Clone(), b.Clone()
	a.Actor, b.Actor = i.Actor{}, i.Actor{}
	a.Model.LeaseOwner, b.Model.LeaseOwner = sc.CredentialLeaseOwner{}, sc.CredentialLeaseOwner{}
	a.Model.Snapshot.CredentialRef, b.Model.Snapshot.CredentialRef = nil, nil
	if a.Model.CredentialLease != nil {
		a.Model.CredentialLease.CredentialRef, b.Model.CredentialLease.CredentialRef = sc.CredentialRef{}, sc.CredentialRef{}
	}
	return reflect.DeepEqual(a, b)
}

func TestSuccessorResponsePreservesCompleteTextAndSingleCall(t *testing.T) {
	for _, name := range []string{"text", "empty-text", "call-with-text", "numeric-lexeme"} {
		t.Run(name, func(t *testing.T) {
			request, response := successorResponseFixture(t)
			want := SuccessorToolCall
			switch name {
			case "text", "empty-text":
				want, response.FinishReason = SuccessorText, "stop"
				text := "complete 中文\x00🙂"
				if name == "empty-text" {
					text = ""
				}
				response.Message.Parts = []mc.MessagePart{{Text: &mc.TextPart{Text: text}}, {Text: &mc.TextPart{Text: ""}}}
			case "numeric-lexeme":
				request.Tools[0].InputSchema = json.RawMessage(successorNumberSchema)
				request = successorResponseRebind(t, request)
				response.Message.Parts[1].ToolCall.Arguments = json.RawMessage(`{ "n" : 1.20e+1 }`)
			}
			originalRequest, originalResponse := request.Clone(), response.Clone()
			got, err := ValidateSuccessorResponse(context.Background(), request, response)
			requestOK(t, err)
			requestOK(t, got.Validate())
			full, ok := got.Response()
			if !ok || got.Kind() != want || got.FinishReason() != response.FinishReason || got.CallID() != response.CallID || got.InvocationID() != response.InvocationID || !reflect.DeepEqual(got.Usage(), response.Usage) || !reflect.DeepEqual(full, originalResponse) {
				t.Fatal("complete response lost original parts, arguments, identity or Usage")
			}
			if !successorResponseSameRequest(request, originalRequest) || !reflect.DeepEqual(response, originalResponse) {
				t.Fatal("pure validation changed the original request/response")
			}
		})
	}
}

func TestSuccessorResponseRejectsIncompleteOrUnsupportedShapes(t *testing.T) {
	for _, name := range []string{"wrong-call", "missing-invocation", "wrong-name", "missing-tool-call-id", "stop-with-call", "calls-without-call", "metadata-part", "tool-result-part", "unknown-finish", "half-json", "duplicate-key", "trailing-json", "array-arguments", "invalid-utf8", "excess-depth", "excess-arguments", "schema-required", "schema-additional", "schema-ref-minimum", "external-schema"} {
		t.Run(name, func(t *testing.T) {
			request, response := successorResponseFixture(t)
			switch name {
			case "wrong-call":
				response.CallID = requestID[mc.Call](t, 2603)
			case "missing-invocation":
				response.InvocationID = mc.InvocationID{}
			case "wrong-name":
				response.Message.Parts[1].ToolCall.Name = "other-tool"
			case "missing-tool-call-id":
				response.Message.Parts[1].ToolCall.ID = ""
			case "stop-with-call":
				response.FinishReason = "stop"
			case "calls-without-call":
				response.Message.Parts = response.Message.Parts[:1]
			case "metadata-part":
				response.Message.Parts = append(response.Message.Parts, mc.MessagePart{Metadata: &mc.ProviderMetadataPart{AdapterID: "openai", Version: "v1", OpaqueRef: "continuation"}})
				requestOK(t, response.Validate()) // Valid C0, unsupported success profile.
			case "tool-result-part":
				response.Message.Parts = append(response.Message.Parts, mc.MessagePart{ToolResult: &mc.ToolResultPart{CallID: "private-call", Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: "forged result"}}}}})
			case "unknown-finish":
				response.FinishReason = "invented"
			case "half-json":
				response.Message.Parts[1].ToolCall.Arguments = json.RawMessage(`{"n":`)
			case "duplicate-key":
				response.Message.Parts[1].ToolCall.Arguments = json.RawMessage(`{"n":1,"n":2}`)
			case "trailing-json":
				response.Message.Parts[1].ToolCall.Arguments = json.RawMessage(`{} {}`)
			case "array-arguments":
				response.Message.Parts[1].ToolCall.Arguments = json.RawMessage(`[]`)
			case "invalid-utf8":
				response.Message.Parts[1].ToolCall.Arguments = []byte{'{', '"', 'n', '"', ':', '"', 255, '"', '}'}
			case "excess-depth":
				response.Message.Parts[1].ToolCall.Arguments = []byte(`{"n":` + strings.Repeat("[", 33) + "0" + strings.Repeat("]", 33) + "}")
			case "excess-arguments":
				response.Message.Parts[1].ToolCall.Arguments = []byte(`{"n":"` + strings.Repeat("x", 1<<20) + `"}`)
			case "schema-required":
				response.Message.Parts[1].ToolCall.Arguments = json.RawMessage(`{}`)
			case "schema-additional":
				args := response.Message.Parts[1].ToolCall.Arguments
				response.Message.Parts[1].ToolCall.Arguments = append([]byte(`{"extra":true,`), args[1:]...)
			case "schema-ref-minimum":
				request.Tools[0].InputSchema = json.RawMessage(successorNumberSchema)
				request = successorResponseRebind(t, request)
				response.Message.Parts[1].ToolCall.Arguments = json.RawMessage(`{"n":0.5}`)
				requestOK(t, response.Validate()) // The actual schema must reject it.
			case "external-schema":
				request.Tools[0].InputSchema = json.RawMessage(`{"$ref":"https://external.invalid/not-loaded"}`)
				request = successorResponseRebind(t, request)
			}
			got, err := ValidateSuccessorResponse(context.Background(), request, response)
			if err == nil || got.Validate() == nil {
				t.Fatal("invalid or unsupported response produced a classification")
			}
			if full, ok := got.Response(); ok || full.Validate() == nil {
				t.Fatal("rejected response exposed a candidate")
			}
			if name == "external-schema" {
				loopCode(t, err, f.SchemaUnsupported)
			}
		})
	}
}

func TestSuccessorResponseKeepsInterruptedFactsWithoutCandidate(t *testing.T) {
	for _, name := range []string{"multi-call", "length", "content_filter", "cancelled", "error", "unknown", "unknown-usage"} {
		t.Run(name, func(t *testing.T) {
			request, response := successorResponseFixture(t)
			want := SuccessorInterrupted
			switch name {
			case "multi-call":
				second := response.Message.Parts[1].Clone()
				second.ToolCall.ID = "second-private-call"
				response.Message.Parts = append(response.Message.Parts, second)
				want = SuccessorUnsupported
			case "unknown-usage":
				response.FinishReason = "length"
				response.Usage = mc.Usage{Source: mc.UnknownUsage}
			default:
				response.FinishReason = mc.FinishReason(name)
				if name == "cancelled" {
					want = SuccessorCancelled
				} else if name == "error" {
					want = SuccessorError
				} else if name == "unknown" {
					want = SuccessorUnknown
				}
			}
			original := response.Clone()
			requestOK(t, response.Validate())
			if name == "length" || name == "content_filter" {
				// C0 permits these message_end frames, but a complete frame is
				// not a complete executable successor response.
				frame := mc.ModelFrame{CallID: response.CallID, InvocationID: response.InvocationID, AttemptIndex: 1, Sequence: 1, Kind: "message_end", Response: &response}
				requestOK(t, frame.Validate())
			}
			got, err := ValidateSuccessorResponse(context.Background(), request, response)
			requestOK(t, err)
			requestOK(t, got.Validate())
			full, ok := got.Response()
			if ok || full.Validate() == nil || got.Kind() != want || got.FinishReason() != original.FinishReason || got.CallID() != original.CallID || got.InvocationID() != original.InvocationID || !reflect.DeepEqual(got.Usage(), original.Usage) || !reflect.DeepEqual(response, original) {
				t.Fatal("non-success was rewritten, lost Usage, or exposed a call candidate")
			}
			usage := got.Usage()
			if usage.InputTokens != nil {
				*usage.InputTokens = 999
				*response.Usage.InputTokens = 888
				if !reflect.DeepEqual(got.Usage(), original.Usage) {
					t.Fatal("non-success Usage retained a mutable borrowed pointer")
				}
			}
		})
	}
}

func TestSuccessorResponseOwnsCopiesAndKeepsProfileSeparate(t *testing.T) {
	request, response := successorResponseFixture(t)
	original := response.Clone()
	got, err := ValidateSuccessorResponse(context.Background(), request, response)
	requestOK(t, err)
	clear(response.Message.Parts[1].ToolCall.Arguments)
	response.Message.Parts[0].Text.Text = "changed borrowed text"
	*response.Usage.InputTokens = 333
	first, ok := got.Response()
	if !ok || !reflect.DeepEqual(first, original) {
		t.Fatal("response did not own its original data")
	}
	clear(first.Message.Parts[1].ToolCall.Arguments)
	first.Message.Parts[2].Text.Text = "changed explicit copy"
	*first.Usage.OutputTokens = 444
	usage := got.Usage()
	*usage.InputTokens = 555
	second, ok := got.Response()
	if !ok || !reflect.DeepEqual(second, original) || !reflect.DeepEqual(got.Usage(), original.Usage) {
		t.Fatal("explicit copy modified the classification")
	}
	encoded, err := json.Marshal(got)
	requestOK(t, err)
	var log bytes.Buffer
	slog.New(slog.NewJSONHandler(&log, nil)).Info("response", "value", got)
	for _, output := range []string{fmt.Sprintf("%#v", got), string(encoded), got.LogValue().String(), log.String()} {
		for _, secret := range []string{"private-response", "README.md", "private-call", original.CallID.String(), original.InvocationID.String(), "private-provider-request"} {
			if strings.Contains(output, secret) {
				t.Fatal("default formatting disclosed protected response material")
			}
		}
	}
	var fabricated SuccessorResponse
	if json.Unmarshal(encoded, &fabricated) == nil || fabricated.Validate() == nil {
		t.Fatal("default JSON constructed a classification")
	}
	if full, ok := fabricated.Response(); ok || full.Validate() == nil || fabricated.Kind() != "" {
		t.Fatal("zero classification exposed a response")
	}
	for _, name := range []string{"input-digest", "required", "no-tools-capability", "parallel-capability", "extra-tool", "nil-context", "cancelled-context", "expired-context", "old-required"} {
		t.Run(name, func(t *testing.T) {
			r, response := successorResponseFixture(t)
			ctx := context.Background()
			switch name {
			case "input-digest":
				r.Input.Digest = toolResultTestDigest([]byte("different semantic"))
			case "required":
				r.ToolChoice.Kind = "required"
			case "no-tools-capability":
				r.Model.Snapshot.Capabilities.ToolCalls = false
				r = successorResponseRebind(t, r)
			case "parallel-capability":
				r.Model.Snapshot.Capabilities.ParallelToolCalls = true
				r = successorResponseRebind(t, r)
			case "extra-tool":
				other := r.Tools[0].Clone()
				other.Name = "other-tool"
				r.Tools = append(r.Tools, other)
			case "nil-context":
				ctx = nil
			case "cancelled-context":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "expired-context":
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Unix(0, 0))
				defer cancel()
			case "old-required":
				old := firstToolRequest(t)
				r, response = old.Request(), firstToolResponse(t, old)
				requestOK(t, validateSessionResponse(old, response))
				text := response.Clone()
				text.FinishReason, text.Message.Parts = "stop", text.Message.Parts[:1]
				if validateSessionResponse(old, text) == nil {
					t.Fatal("new auto profile relaxed the original required session")
				}
			}
			out, err := ValidateSuccessorResponse(ctx, r, response)
			if err == nil || out.Validate() == nil {
				t.Fatal("invalid/cancelled/old profile became a new auto classification")
			}
			if _, ok := out.Response(); ok {
				t.Fatal("rejected profile published a candidate")
			}
			if name == "no-tools-capability" || name == "parallel-capability" {
				loopCode(t, err, f.CapabilityUnsupported)
			}
			if name == "cancelled-context" && !errors.Is(err, context.Canceled) || name == "expired-context" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("original context cancellation changed")
			}
		})
	}
}
