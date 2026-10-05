// Package adapter implements bounded wire exchanges, not Model business calls.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const OpenAIChatTextRevision = "openai-chat-text-v1"
const OpenAIChatStructuredRevision = "openai-chat-structured-v1"

type ResponseMode string

const (
	JSONResponse ResponseMode = "json"
	SSEResponse  ResponseMode = "sse"
)

type Transport struct {
	Policy   *outbound.PolicyService
	Trust    outbound.TrustStore
	Resolver outbound.Resolver
}
type Request struct {
	Snapshot       mc.ConfigSnapshot
	Messages       []mc.Message
	Tools          []mc.Tool
	ToolChoice     mc.ToolChoice
	ResponseFormat mc.ResponseFormat
	Mode           ResponseMode
}
type CallOptions struct {
	ProjectID  identity.ProjectID
	Context    outbound.CallContext
	Credential sc.SecretMaterial
	AllowHTTP  bool
	Limits     outbound.Limits
}
type End struct {
	FinishReason      mc.FinishReason
	Usage             mc.Usage
	ProviderRequestID string
}
type Result struct {
	Text string
	End  End
}
type EventKind string

const (
	TextDelta   EventKind = "text_delta"
	UsageUpdate EventKind = "usage_update"
	StreamEnd   EventKind = "stream_end"
)

type Event struct {
	Kind  EventKind
	Text  string
	Usage *mc.Usage
	End   *End
}
type Observation struct {
	DoStarted bool
	Decision  *outbound.Decision
	Usage     mc.Usage
}

type OpenAIChat struct {
	transport Transport
	budget    *Budget
}

func NewOpenAIChat(t Transport, b *Budget) (*OpenAIChat, error) {
	if b == nil || b.state == nil || t.Resolver != nil && nilPort(t.Resolver) {
		return nil, invalid()
	}
	if t.Resolver == nil {
		var err error
		t.Resolver, err = outbound.SystemResolver()
		if err != nil {
			return nil, invalid()
		}
	}
	// Use the formal constructor, without inspecting private dependency state.
	probe, err := outbound.NewClient(t.Policy, t.Trust, t.Resolver)
	if err != nil {
		return nil, invalid()
	}
	probe.StopAdmission()
	return &OpenAIChat{transport: t, budget: b}, nil
}

func (a *OpenAIChat) Start(ctx context.Context, r Request, o CallOptions) (*Exchange, error) {
	if a == nil || a.budget == nil || ctx == nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, contextFailure(err)
	}
	req, profile, schema, err := prepareWithSchema(ctx, r, o)
	if err != nil {
		return nil, err
	}
	x, err := a.budget.accept(ctx, o.ProjectID, r.Mode, o.Limits.Overall)
	if err != nil {
		return nil, err
	}
	x.schema = schema
	go x.run(a.transport, req, profile)
	return x, nil
}

func prepare(r Request, o CallOptions) (*http.Request, outbound.Profile, error) {
	req, profile, _, err := prepareWithSchema(context.Background(), r, o)
	return req, profile, err
}

func prepareWithSchema(ctx context.Context, r Request, o CallOptions) (*http.Request, outbound.Profile, *structuredSchema, error) {
	fail := func(err error) (*http.Request, outbound.Profile, *structuredSchema, error) {
		return nil, outbound.Profile{}, nil, err
	}
	if r.Mode != JSONResponse && r.Mode != SSEResponse || r.Snapshot.Validate() != nil || r.ToolChoice.Validate() != nil || r.ResponseFormat.Validate() != nil || len(r.Messages) == 0 || len(r.Messages) > 256 || len(r.Tools) > 128 || o.ProjectID.Validate() != nil {
		return fail(invalid())
	}
	for _, m := range r.Messages {
		if m.Validate() != nil {
			return fail(invalid())
		}
	}
	for _, t := range r.Tools {
		if t.Validate() != nil {
			return fail(invalid())
		}
	}
	s, c := r.Snapshot.Identity, r.Snapshot.Capabilities
	if s.Profile != mc.OpenAIChatV1 || s.Protocol != mc.OpenAIChat || s.ModelType != mc.ChatModel || !emptyObject(r.Snapshot.Parameters) || !emptyObject(r.Snapshot.RequestOverwrite) || len(r.Snapshot.HeaderOverwrite) != 0 || c.ToolCalls || c.ParallelToolCalls || c.Reasoning || len(c.ReasoningEfforts) != 0 || !onlyText(c.InputModalities) || !onlyText(c.OutputModalities) || r.Mode == SSEResponse && !c.Streaming || len(r.Tools) != 0 || r.ToolChoice.Kind != "none" {
		return fail(unsupported())
	}
	var schema *structuredSchema
	var format *structuredWireFormat
	switch s.AdapterRevision {
	case OpenAIChatTextRevision:
		if len(c.StructuredOutputModes) > 0 && !onlyText(c.StructuredOutputModes) || r.ResponseFormat.Kind != "text" {
			return fail(unsupported())
		}
	case OpenAIChatStructuredRevision:
		hasSchema := false
		for _, mode := range c.StructuredOutputModes {
			hasSchema = hasSchema || mode == "json_schema"
		}
		if !hasSchema {
			return fail(unsupported())
		}
		if r.ResponseFormat.Kind == "json_schema" {
			var err error
			schema, format, err = compileStructured(ctx, r.ResponseFormat)
			if err != nil {
				return fail(err)
			}
		}
	default:
		return fail(unsupported())
	}
	// Explicit wire tags prevent either public-contract JSON or logging methods
	// from becoming an alternative encoding of the provider request.
	type wireMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	messages := make([]wireMessage, 0, len(r.Messages))
	encodedSize := 128 + 6*len(s.ProviderModelID)
	for _, m := range r.Messages {
		if m.Role == "tool" || len(m.Parts) != 1 || m.Parts[0].Text == nil {
			return fail(unsupported())
		}
		// Bound allocation before json.Marshal; up to 256 individually valid
		// 16 MiB parts must not allocate a multi-gigabyte temporary encoding.
		encodedSize += quotedSize(m.Parts[0].Text.Text) + len(m.Role) + 32
		if encodedSize > maxTextBytes+8192 {
			return fail(limitFailure())
		}
		messages = append(messages, wireMessage{m.Role, m.Parts[0].Text.Text})
	}
	var maxOutput *int64
	if c.MaxOutput != nil {
		n := int64(*c.MaxOutput)
		maxOutput = &n
	}
	body := struct {
		Model     string                `json:"model"`
		Messages  []wireMessage         `json:"messages"`
		Stream    bool                  `json:"stream"`
		Options   map[string]bool       `json:"stream_options,omitempty"`
		MaxOutput *int64                `json:"max_completion_tokens,omitempty"`
		Format    *structuredWireFormat `json:"response_format,omitempty"`
	}{Model: s.ProviderModelID, Messages: messages, Stream: r.Mode == SSEResponse, MaxOutput: maxOutput, Format: format}
	if body.Stream {
		body.Options = map[string]bool{"include_usage": true}
	}
	raw, err := json.Marshal(body)
	if err != nil || len(raw) > maxTextBytes {
		return fail(limitFailure())
	}
	u, err := url.Parse(r.Snapshot.Endpoint)
	if err != nil || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return fail(invalid())
	}
	path := strings.TrimSuffix(u.EscapedPath(), "/") + "/chat/completions"
	u.Path, err = url.PathUnescape(path)
	if err != nil {
		return fail(invalid())
	}
	u.RawPath = path
	target, err := outbound.ParseTarget(u.String())
	if err != nil {
		return fail(invalid())
	}
	if o.Limits.Overall <= 0 || o.Limits.Overall > 120*time.Second {
		return fail(invalid())
	}
	idle := min(60*time.Second, o.Limits.Overall)
	if o.Limits.ReadIdle == 0 {
		o.Limits.ReadIdle = idle
	} else if o.Limits.ReadIdle < 0 || o.Limits.ReadIdle > idle {
		return fail(invalid())
	}
	// Validate availability without retaining or converting borrowed plaintext.
	if o.Credential.Use(func([]byte) error { return nil }) != nil {
		return fail(invalid())
	}
	field, err := outbound.HeaderCredential("Authorization", "Bearer ", o.Credential)
	if err != nil {
		return fail(invalid())
	}
	binding, err := outbound.NewCredentialBinding(target.Origin(), field)
	if err != nil {
		return fail(invalid())
	}
	profile, err := outbound.NewProfile(outbound.ProfileOptions{Consumer: ac.Model, AllowHTTP: o.AllowHTTP, Streaming: body.Stream, Limits: o.Limits, Context: o.Context, Credentials: binding})
	if err != nil {
		return fail(invalid())
	}
	req, err := http.NewRequest(http.MethodPost, target.URL().String(), bytes.NewReader(raw))
	if err != nil {
		return fail(invalid())
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if body.Stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	req.Close = true
	return req, profile, schema, nil
}

func onlyText(v []string) bool { return len(v) == 1 && v[0] == "text" }
func quotedSize(s string) int {
	n := 2
	for _, c := range s {
		switch {
		case c == '"' || c == '\\' || c == '\n' || c == '\r' || c == '\t' || c == '\b' || c == '\f':
			n += 2
		case c < 32 || c == '<' || c == '>' || c == '&' || c == '\u2028' || c == '\u2029':
			n += 6
		default:
			if c < 0x80 {
				n++
			} else if c < 0x800 {
				n += 2
			} else if c < 0x10000 {
				n += 3
			} else {
				n += 4
			}
		}
	}
	return n
}
func emptyObject(raw []byte) bool {
	var v map[string]json.RawMessage
	return json.Unmarshal(raw, &v) == nil && v != nil && len(v) == 0
}
func invalid() error { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func nilPort(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}

func safeFormat(w fmt.State, label string)       { _, _ = io.WriteString(w, label) }
func (Request) Format(w fmt.State, _ rune)       { safeFormat(w, "model_wire_request") }
func (Request) MarshalJSON() ([]byte, error)     { return []byte(`"model_wire_request"`), nil }
func (Request) LogValue() slog.Value             { return slog.StringValue("model_wire_request") }
func (CallOptions) Format(w fmt.State, _ rune)   { safeFormat(w, "model_wire_options") }
func (CallOptions) MarshalJSON() ([]byte, error) { return []byte(`"model_wire_options"`), nil }
func (CallOptions) LogValue() slog.Value         { return slog.StringValue("model_wire_options") }
func (Result) Format(w fmt.State, _ rune)        { safeFormat(w, "model_wire_result") }
func (Result) MarshalJSON() ([]byte, error)      { return []byte(`"model_wire_result"`), nil }
func (Result) LogValue() slog.Value              { return slog.StringValue("model_wire_result") }
func (Event) Format(w fmt.State, _ rune)         { safeFormat(w, "model_wire_event") }
func (Event) MarshalJSON() ([]byte, error)       { return []byte(`"model_wire_event"`), nil }
func (Event) LogValue() slog.Value               { return slog.StringValue("model_wire_event") }
func (Observation) Format(w fmt.State, _ rune)   { safeFormat(w, "model_wire_observation") }
func (Observation) MarshalJSON() ([]byte, error) { return []byte(`"model_wire_observation"`), nil }
func (Observation) LogValue() slog.Value         { return slog.StringValue("model_wire_observation") }
func (End) Format(w fmt.State, _ rune)           { safeFormat(w, "model_wire_end") }
func (End) MarshalJSON() ([]byte, error)         { return []byte(`"model_wire_end"`), nil }
func (End) LogValue() slog.Value                 { return slog.StringValue("model_wire_end") }
