package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"mime"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type TextPart struct {
	Text string `json:"text"`
}
type MediaPart struct {
	Ref       oc.BusinessFileRef `json:"ref"`
	MediaType string             `json:"media_type"`
}
type ToolCallPart struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}
type ToolResultPart struct {
	CallID  string       `json:"call_id"`
	Parts   []ResultPart `json:"parts"`
	IsError bool         `json:"is_error"`
}
type ProviderMetadataPart struct {
	AdapterID string `json:"adapter_id"`
	Version   string `json:"version"`
	OpaqueRef string `json:"opaque_ref"`
}
type ResultPart struct {
	Text  *TextPart  `json:"text,omitempty"`
	Image *MediaPart `json:"image,omitempty"`
	File  *MediaPart `json:"file,omitempty"`
}
type MessagePart struct {
	Text       *TextPart             `json:"text,omitempty"`
	Image      *MediaPart            `json:"image,omitempty"`
	File       *MediaPart            `json:"file,omitempty"`
	ToolCall   *ToolCallPart         `json:"tool_call,omitempty"`
	ToolResult *ToolResultPart       `json:"tool_result,omitempty"`
	Metadata   *ProviderMetadataPart `json:"metadata,omitempty"`
}

func (p TextPart) Validate() error {
	if len(p.Text) > 16<<20 || !utf8.ValidString(p.Text) {
		return bad()
	}
	return nil
}
func (p MediaPart) Validate() error {
	m, _, e := mime.ParseMediaType(p.MediaType)
	if e != nil || m != p.MediaType || p.Ref.Validate() != nil {
		return bad()
	}
	return nil
}
func (p ToolCallPart) Validate() error {
	if !text(p.ID, 256) || !text(p.Name, 256) || objectJSON(p.Arguments, 1<<20) != nil {
		return bad()
	}
	return nil
}
func (p ToolCallPart) Clone() ToolCallPart { p.Arguments = bytes.Clone(p.Arguments); return p }
func (p ToolResultPart) Validate() error {
	if !text(p.CallID, 256) || len(p.Parts) > 256 {
		return bad()
	}
	for _, v := range p.Parts {
		if v.Validate() != nil {
			return bad()
		}
	}
	return nil
}
func (p ToolResultPart) Clone() ToolResultPart {
	p.Parts = append([]ResultPart(nil), p.Parts...)
	for n := range p.Parts {
		p.Parts[n] = p.Parts[n].Clone()
	}
	return p
}
func (p ProviderMetadataPart) Validate() error {
	if !safeToken(p.AdapterID, 128) || !safeToken(p.Version, 128) || !safeToken(p.OpaqueRef, 256) {
		return bad()
	}
	return nil
}
func (p ResultPart) Validate() error {
	n := 0
	if p.Text != nil {
		n++
		if p.Text.Validate() != nil {
			return bad()
		}
	}
	for _, v := range []*MediaPart{p.Image, p.File} {
		if v != nil {
			n++
			if v.Validate() != nil {
				return bad()
			}
		}
	}
	if n != 1 {
		return bad()
	}
	return nil
}
func (p ResultPart) Clone() ResultPart {
	p.Text = copyPtr(p.Text)
	p.Image = copyPtr(p.Image)
	p.File = copyPtr(p.File)
	return p
}
func (p MessagePart) Validate() error {
	n := 0
	if p.Text != nil {
		n++
		if p.Text.Validate() != nil {
			return bad()
		}
	}
	for _, v := range []*MediaPart{p.Image, p.File} {
		if v != nil {
			n++
			if v.Validate() != nil {
				return bad()
			}
		}
	}
	if p.ToolCall != nil {
		n++
		if p.ToolCall.Validate() != nil {
			return bad()
		}
	}
	if p.ToolResult != nil {
		n++
		if p.ToolResult.Validate() != nil {
			return bad()
		}
	}
	if p.Metadata != nil {
		n++
		if p.Metadata.Validate() != nil {
			return bad()
		}
	}
	if n != 1 {
		return bad()
	}
	return nil
}
func (p MessagePart) Clone() MessagePart {
	p.Text = copyPtr(p.Text)
	p.Image = copyPtr(p.Image)
	p.File = copyPtr(p.File)
	if p.ToolCall != nil {
		x := p.ToolCall.Clone()
		p.ToolCall = &x
	}
	if p.ToolResult != nil {
		x := p.ToolResult.Clone()
		p.ToolResult = &x
	}
	p.Metadata = copyPtr(p.Metadata)
	return p
}

type Message struct {
	Role  string        `json:"role"`
	Parts []MessagePart `json:"parts"`
}

func (m Message) Validate() error {
	if !one(m.Role, "system", "user", "assistant", "tool") || len(m.Parts) == 0 || len(m.Parts) > 256 {
		return bad()
	}
	for _, p := range m.Parts {
		if p.Validate() != nil || p.ToolCall != nil && m.Role != "assistant" || p.Metadata != nil && m.Role != "assistant" || p.ToolResult != nil && m.Role != "tool" {
			return bad()
		}
	}
	return nil
}
func (m Message) Clone() Message {
	m.Parts = append([]MessagePart(nil), m.Parts...)
	for n := range m.Parts {
		m.Parts[n] = m.Parts[n].Clone()
	}
	return m
}

type Tool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
}

func (t Tool) Validate() error {
	if !text(t.Name, 256) || len(t.Description) > 64<<10 || !utf8.ValidString(t.Description) || objectJSON(t.InputSchema, 64<<10) != nil || len(t.OutputSchema) > 0 && objectJSON(t.OutputSchema, 64<<10) != nil {
		return bad()
	}
	return nil
}
func (t Tool) Clone() Tool {
	t.InputSchema = bytes.Clone(t.InputSchema)
	t.OutputSchema = bytes.Clone(t.OutputSchema)
	return t
}

type ToolChoice struct {
	Kind string `json:"kind"`
	Name string `json:"name,omitempty"`
}

func (t ToolChoice) Validate() error {
	if t.Kind == "named" {
		if !text(t.Name, 256) {
			return bad()
		}
	} else if !one(t.Kind, "auto", "none", "required") || t.Name != "" {
		return bad()
	}
	return nil
}

type ResponseFormat struct {
	Kind   string          `json:"kind"`
	Name   string          `json:"name,omitempty"`
	Schema json.RawMessage `json:"schema,omitempty"`
}

func (r ResponseFormat) Validate() error {
	if r.Kind == "text" {
		if r.Name != "" || len(r.Schema) > 0 {
			return bad()
		}
	} else if r.Kind != "json_schema" || !safeToken(r.Name, 64) || objectJSON(r.Schema, 64<<10) != nil {
		return bad()
	}
	return nil
}
func (r ResponseFormat) Clone() ResponseFormat { r.Schema = bytes.Clone(r.Schema); return r }

type RetryClass string

const (
	AgentRetry   RetryClass = "agent"
	BoundedRetry RetryClass = "bounded_consumer"
)

type ModelRequest struct {
	Actor          id.Actor       `json:"-"`
	CallID         CallID         `json:"call_id"`
	Consumer       Consumer       `json:"consumer"`
	Model          ResolvedModel  `json:"model"`
	Input          InputIdentity  `json:"input"`
	Messages       []Message      `json:"messages"`
	Tools          []Tool         `json:"tools"`
	ToolChoice     ToolChoice     `json:"tool_choice"`
	ResponseFormat ResponseFormat `json:"response_format"`
	RetryClass     RetryClass     `json:"retry_class"`
}

func (r ModelRequest) Validate() error {
	if !actorMatches(r.Actor, r.Consumer) || r.CallID.Validate() != nil || r.Consumer.Validate() != nil || r.Model.Validate() != nil || !r.Consumer.Equal(r.Model.Consumer) || !ownerMatches(r.Model.LeaseOwner, r.Consumer, &r.CallID) || r.Consumer.Purpose.ModelType() != ChatModel || r.Input.ValidateFor(r.Consumer) != nil || len(r.Messages) == 0 || r.ToolChoice.Validate() != nil || r.ResponseFormat.Validate() != nil || len(r.Tools) > 128 {
		return bad()
	}
	if r.Consumer.Kind == AgentConsumer {
		if r.RetryClass != AgentRetry {
			return bad()
		}
	} else if r.RetryClass != BoundedRetry {
		return bad()
	}
	parts := 0
	for _, m := range r.Messages {
		if m.Validate() != nil {
			return bad()
		}
		parts += len(m.Parts)
		if parts > 256 {
			return bad()
		}
	}
	seen := map[string]bool{}
	for _, t := range r.Tools {
		if t.Validate() != nil || seen[t.Name] {
			return bad()
		}
		seen[t.Name] = true
	}
	if r.ToolChoice.Kind == "named" && !seen[r.ToolChoice.Name] || r.ToolChoice.Kind == "required" && len(r.Tools) == 0 {
		return bad()
	}
	return nil
}
func (r ModelRequest) Clone() ModelRequest {
	r.Consumer = r.Consumer.Clone()
	r.Model = r.Model.Clone()
	r.Input = r.Input.Clone()
	r.Messages = append([]Message(nil), r.Messages...)
	for n := range r.Messages {
		r.Messages[n] = r.Messages[n].Clone()
	}
	r.Tools = append([]Tool(nil), r.Tools...)
	for n := range r.Tools {
		r.Tools[n] = r.Tools[n].Clone()
	}
	r.ResponseFormat = r.ResponseFormat.Clone()
	return r
}

type ModelResponse struct {
	CallID            CallID       `json:"call_id"`
	InvocationID      InvocationID `json:"invocation_id"`
	Message           Message      `json:"message"`
	FinishReason      FinishReason `json:"finish_reason"`
	Usage             Usage        `json:"usage"`
	ProviderRequestID string       `json:"provider_request_id,omitempty"`
}

func (r ModelResponse) Validate() error {
	if r.CallID.Validate() != nil || r.InvocationID.Validate() != nil || r.Message.Validate() != nil || r.Message.Role != "assistant" || !r.FinishReason.Valid() || r.Usage.Validate() != nil || r.ProviderRequestID != "" && !safeToken(r.ProviderRequestID, 256) {
		return bad()
	}
	return nil
}
func (r ModelResponse) Clone() ModelResponse {
	r.Message = r.Message.Clone()
	r.Usage = r.Usage.Clone()
	return r
}

type FrameKind string
type FrameText struct {
	PartIndex  int        `json:"part_index"`
	OffsetUTF8 TokenCount `json:"offset_utf8"`
	Text       string     `json:"text"`
}
type FrameTool struct {
	PartIndex int             `json:"part_index"`
	CallID    string          `json:"call_id"`
	Name      string          `json:"name,omitempty"`
	Fragment  string          `json:"fragment,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}
type FrameStart struct {
	StartedAt *f.Instant `json:"started_at,omitempty"`
	MessageID string     `json:"message_id,omitempty"`
}
type ModelFrame struct {
	CallID       CallID         `json:"call_id"`
	InvocationID InvocationID   `json:"invocation_id"`
	AttemptIndex f.Sequence     `json:"attempt_index"`
	Sequence     f.Sequence     `json:"sequence"`
	Kind         FrameKind      `json:"kind"`
	Start        *FrameStart    `json:"start,omitempty"`
	Text         *FrameText     `json:"text,omitempty"`
	Tool         *FrameTool     `json:"tool,omitempty"`
	Usage        *Usage         `json:"usage,omitempty"`
	Response     *ModelResponse `json:"response,omitempty"`
	Error        *ModelError    `json:"error,omitempty"`
	Partial      bool           `json:"partial,omitempty"`
	CancelReason string         `json:"cancel_reason,omitempty"`
}

func (r ModelFrame) Validate() error {
	if r.CallID.Validate() != nil || r.InvocationID.Validate() != nil || r.AttemptIndex.Validate() != nil || r.Sequence.Validate() != nil {
		return bad()
	}
	n := 0
	for _, v := range []bool{r.Start != nil, r.Text != nil, r.Tool != nil, r.Usage != nil, r.Response != nil, r.Error != nil, r.CancelReason != ""} {
		if v {
			n++
		}
	}
	if n != 1 || r.Partial && r.Kind != "attempt_aborted" {
		return bad()
	}
	switch r.Kind {
	case "attempt_started":
		if r.Start == nil || r.Start.StartedAt == nil || r.Start.StartedAt.Validate() != nil || r.Start.MessageID != "" {
			return bad()
		}
	case "message_start":
		if r.Start == nil || r.Start.StartedAt != nil || !text(r.Start.MessageID, 256) {
			return bad()
		}
	case "text_delta", "reasoning_delta":
		if r.Text == nil || r.Text.PartIndex < 0 || r.Text.PartIndex >= 256 || r.Text.OffsetUTF8.Validate() != nil || len(r.Text.Text) > 16<<20 || !utf8.ValidString(r.Text.Text) {
			return bad()
		}
	case "tool_call_start", "tool_call_delta", "tool_call_end":
		if r.Tool == nil || r.Tool.PartIndex < 0 || r.Tool.PartIndex >= 256 || !text(r.Tool.CallID, 256) {
			return bad()
		}
		switch r.Kind {
		case "tool_call_start":
			if !text(r.Tool.Name, 256) || r.Tool.Fragment != "" || len(r.Tool.Arguments) > 0 {
				return bad()
			}
		case "tool_call_delta":
			if r.Tool.Name != "" || len(r.Tool.Arguments) > 0 || len(r.Tool.Fragment) > 1<<20 || !utf8.ValidString(r.Tool.Fragment) {
				return bad()
			}
		case "tool_call_end":
			if r.Tool.Name != "" || r.Tool.Fragment != "" || objectJSON(r.Tool.Arguments, 1<<20) != nil {
				return bad()
			}
		}
	case "usage_update":
		if r.Usage == nil || r.Usage.Validate() != nil {
			return bad()
		}
	case "message_end":
		if r.Response == nil || r.Response.Validate() != nil || r.Response.CallID != r.CallID || r.Response.InvocationID != r.InvocationID || one(string(r.Response.FinishReason), "error", "unknown", "cancelled") {
			return bad()
		}
	case "attempt_aborted", "call_failed":
		if r.Error == nil || r.Error.Validate() != nil {
			return bad()
		}
	case "call_cancelled":
		if !one(r.CancelReason, "cancelled", "watchdog", "project_stopping", "shutdown") {
			return bad()
		}
	default:
		return bad()
	}
	return nil
}
func (r ModelFrame) Clone() ModelFrame {
	r.Start = copyPtr(r.Start)
	if r.Start != nil {
		r.Start.StartedAt = copyPtr(r.Start.StartedAt)
	}
	r.Text = copyPtr(r.Text)
	r.Tool = copyPtr(r.Tool)
	if r.Tool != nil {
		r.Tool.Arguments = bytes.Clone(r.Tool.Arguments)
	}
	if r.Usage != nil {
		x := r.Usage.Clone()
		r.Usage = &x
	}
	if r.Response != nil {
		x := r.Response.Clone()
		r.Response = &x
	}
	r.Error = copyPtr(r.Error)
	return r
}

type ModelStream interface {
	Next(context.Context) (ModelFrame, error)
	Close(context.Context) error
	Joined() bool
}
type Chat interface {
	Chat(context.Context, ModelRequest) (ModelResponse, error)
	Stream(context.Context, ModelRequest) (ModelStream, error)
}

func (TextPart) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_text_part")) }
func (TextPart) LogValue() slog.Value       { return slog.StringValue("model_text_part") }

func (MediaPart) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_media_part")) }
func (MediaPart) LogValue() slog.Value       { return slog.StringValue("model_media_part") }

func (ToolCallPart) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_tool_call_part")) }
func (ToolCallPart) LogValue() slog.Value       { return slog.StringValue("model_tool_call_part") }

func (ToolResultPart) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_tool_result_part")) }
func (ToolResultPart) LogValue() slog.Value       { return slog.StringValue("model_tool_result_part") }

func (ProviderMetadataPart) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_provider_metadata_part"))
}
func (ProviderMetadataPart) LogValue() slog.Value {
	return slog.StringValue("model_provider_metadata_part")
}

func (ResultPart) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_result_part")) }
func (ResultPart) LogValue() slog.Value       { return slog.StringValue("model_result_part") }

func (MessagePart) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_message_part")) }
func (MessagePart) LogValue() slog.Value       { return slog.StringValue("model_message_part") }

func (Message) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_message")) }
func (Message) LogValue() slog.Value       { return slog.StringValue("model_message") }

func (Tool) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_tool")) }
func (Tool) LogValue() slog.Value       { return slog.StringValue("model_tool") }

func (ToolChoice) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_tool_choice")) }
func (ToolChoice) LogValue() slog.Value       { return slog.StringValue("model_tool_choice") }

func (ResponseFormat) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_response_format")) }
func (ResponseFormat) LogValue() slog.Value       { return slog.StringValue("model_response_format") }

func (ModelRequest) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_model_request")) }
func (ModelRequest) LogValue() slog.Value       { return slog.StringValue("model_model_request") }

func (ModelResponse) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_model_response")) }
func (ModelResponse) LogValue() slog.Value       { return slog.StringValue("model_model_response") }

func (FrameText) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_frame_text")) }
func (FrameText) LogValue() slog.Value       { return slog.StringValue("model_frame_text") }

func (FrameTool) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_frame_tool")) }
func (FrameTool) LogValue() slog.Value       { return slog.StringValue("model_frame_tool") }

func (FrameStart) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_frame_start")) }
func (FrameStart) LogValue() slog.Value       { return slog.StringValue("model_frame_start") }

func (ModelFrame) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_model_frame")) }
func (ModelFrame) LogValue() slog.Value       { return slog.StringValue("model_model_frame") }
