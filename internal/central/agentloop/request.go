// Package agentloop assembles model input from immutable Execution context.
// Constructing a request is not evidence of a running Execution, a persisted
// Round or Call, or permission to invoke Model.
package agentloop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// DirectTextRequest is the first-request projection, not a Controller or a
// Transcript. It retains the source Context digest independently of Model's
// message digest. Explicit Request access includes backend-only Model/lease
// references; only its Messages are model-visible input.
type DirectTextRequest struct {
	data func() (f.Digest, mc.ModelRequest)
}

func (v DirectTextRequest) Validate() error {
	if v.data == nil {
		return invalid()
	}
	return nil
}
func (v DirectTextRequest) ContextDigest() f.Digest {
	if v.data == nil {
		return ""
	}
	d, _ := v.data()
	return d
}
func (v DirectTextRequest) Request() mc.ModelRequest {
	if v.data == nil {
		return mc.ModelRequest{}
	}
	_, r := v.data()
	return r
}

// InitialInput is the semantic input candidate for a future Transcript writer.
// It does not contain the fixed System Prompt or establish a committed entry.
func (v DirectTextRequest) InitialInput() mc.Message {
	if v.data == nil {
		return mc.Message{}
	}
	return v.Request().Messages[1]
}
func (DirectTextRequest) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "agent_text_request") }
func (DirectTextRequest) LogValue() slog.Value       { return slog.StringValue("agent_text_request") }
func (DirectTextRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"agent_text_request"`), nil
}
func (*DirectTextRequest) UnmarshalJSON([]byte) error { return invalid() }

// BuildDirectTextRequest builds the initial request solely from captured
// components and initial Skill bindings. Later Turns require a real Transcript
// and the then-effective Skill binding state; this function does not simulate
// either. Actor/RoundID/CallID are checked identities, never authority grants.
// No Store, current source lookup, model invocation, retry or write occurs here.
func BuildDirectTextRequest(ctx context.Context, captured ec.ExecutionContext, actor id.Actor, roundID string, callID mc.CallID) (DirectTextRequest, error) {
	var zero DirectTextRequest
	if err := contextError(ctx); err != nil {
		return zero, err
	}
	if captured.Validate() != nil || actor.Validate() != nil || callID.Validate() != nil {
		return zero, invalid()
	}
	if _, err := f.ParseID[ec.Round](roundID); err != nil {
		return zero, invalid()
	}
	fields := captured.Input().Fields()
	r := fields.Request
	a := actor.Details()
	if a.Kind != id.AgentRun || a.ProjectID != r.Launch.ProjectID.String() || a.AgentID != r.Launch.AgentID.String() || a.ExecutionID != r.ExecutionID.String() {
		return zero, f.NewFault(f.Forbidden, f.NotStarted)
	}
	if !directTextProfile(fields) {
		return zero, unsupported()
	}
	messages, err := projectMessages(ctx, captured)
	if err != nil {
		return zero, err
	}
	// Reuse Model's exact format-1 text projection, including none/text. A
	// second digest format here would not match Model's accepted InputIdentity.
	digest, err := model.TextInputDigest(messages)
	if err != nil {
		return zero, err
	}
	execution := r.ExecutionID
	request := mc.ModelRequest{
		Actor: actor, CallID: callID, Consumer: fields.Model.Consumer.Clone(), Model: fields.Model.Clone(),
		Input:    mc.InputIdentity{ExecutionID: &execution, RoundID: roundID, Digest: digest, SchemaVersion: 1},
		Messages: messages, Tools: []mc.Tool{}, ToolChoice: mc.ToolChoice{Kind: "none"},
		ResponseFormat: mc.ResponseFormat{Kind: "text"}, RetryClass: mc.AgentRetry,
	}
	if err := request.Validate(); err != nil {
		return zero, invalid().WithCause(err)
	}
	if err := contextError(ctx); err != nil {
		return zero, err
	}
	owned, source := request.Clone(), captured.Digest()
	return DirectTextRequest{data: func() (f.Digest, mc.ModelRequest) { return source, owned.Clone() }}, nil
}

func directTextProfile(v ec.PreparationInputFields) bool {
	s, c := v.Model.Snapshot, v.Model.Snapshot.Capabilities
	a := v.Agent.Fields()
	// Tools have already been selected by their real capture provider and the
	// original policy. Never silently drop one to fit this no-tools profile.
	return v.Tools != nil && len(v.Tools) == 0 && a.Core.ReasoningEffort == nil && !a.Core.InjectAgentsMD &&
		v.Mounts.Mounts != nil && len(v.Mounts.Mounts) == 0 && len(v.Request.Launch.Policy.AllowedResourceConstraints) == 0 &&
		s.Identity.Profile == mc.OpenAIChatV1 && s.Identity.Protocol == mc.OpenAIChat && s.Identity.ModelType == mc.ChatModel && s.Identity.AdapterRevision == adapter.OpenAIChatTextRevision &&
		emptyObject(s.Parameters) && emptyObject(s.RequestOverwrite) && len(s.HeaderOverwrite) == 0 &&
		!c.ToolCalls && !c.ParallelToolCalls && !c.Reasoning && len(c.ReasoningEfforts) == 0 && onlyText(c.InputModalities) && onlyText(c.OutputModalities) &&
		(len(c.StructuredOutputModes) == 0 || onlyText(c.StructuredOutputModes)) &&
		s.CredentialRef != nil && v.Model.CredentialLease != nil && v.Model.LeaseOwner.Details().Kind == sc.ExecutionOwner
}
func emptyObject(raw json.RawMessage) bool {
	var value map[string]json.RawMessage
	return len(raw) == 0 || json.Unmarshal(raw, &value) == nil && value != nil && len(value) == 0
}
func onlyText(v []string) bool { return len(v) == 1 && v[0] == "text" }
func invalid() *f.Fault        { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func unsupported() *f.Fault    { return f.NewFault(f.CapabilityUnsupported, f.NotStarted) }
func contextError(ctx context.Context) error {
	if ctx == nil {
		return invalid()
	}
	return ctx.Err()
}
