package model

import (
	"context"
	"encoding/json"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const runtimeTextLimit = 16 << 20

// TextInputDigest defines this Runtime's version-1 text input projection. The
// digest binds bytes; only the consumer's current authority can bind ownership.
func TextInputDigest(messages []mc.Message) (f.Digest, error) {
	return runtimeInputDigest(context.Background(), messages)
}

func runtimeInputDigest(ctx context.Context, messages []mc.Message) (f.Digest, error) {
	return runtimeStructuredInputDigest(ctx, messages, mc.ResponseFormat{Kind: "text"})
}

// Check allocation bounds before Validate/Clone/Marshal. The accepted wire
// profile has exactly one text part per message and no tool role.
func runtimeTextShape(ctx context.Context, messages []mc.Message) error {
	if err := runtimeContextError(ctx); err != nil {
		return err
	}
	if len(messages) == 0 || len(messages) > 256 {
		return fault(f.InvalidArgument)
	}
	// Count the exact fixed projection before allocating its JSON encoding.
	encodedSize := len(`{"format":1,"messages":[],"tool_choice":{"kind":"none"},"response_format":{"kind":"text"}}`)
	for n, m := range messages {
		if err := runtimeContextError(ctx); err != nil {
			return err
		}
		if m.Role != "system" && m.Role != "user" && m.Role != "assistant" || len(m.Parts) != 1 || m.Parts[0].Text == nil {
			return fault(f.CapabilityUnsupported)
		}
		p := m.Parts[0]
		if p.Image != nil || p.File != nil || p.ToolCall != nil || p.ToolResult != nil || p.Metadata != nil {
			return fault(f.InvalidArgument)
		}
		text := p.Text.Text
		if len(text) > runtimeTextLimit {
			return fault(f.PayloadTooLarge)
		}
		if !utf8.ValidString(text) {
			return fault(f.InvalidArgument)
		}
		encodedSize += len(`{"role":"","parts":[{"text":{"text":""}}]}`) + len(m.Role)
		if n != 0 {
			encodedSize++
		}
		for i := 0; i < len(text); i++ {
			if i&4095 == 0 {
				if err := runtimeContextError(ctx); err != nil {
					return err
				}
			}
			switch b := text[i]; {
			case b == '"' || b == '\\' || b == '\b' || b == '\f' || b == '\n' || b == '\r' || b == '\t':
				encodedSize += 2
			case b < 0x20 || b == '<' || b == '>' || b == '&':
				encodedSize += 6
			case b == 0xe2 && i+2 < len(text) && text[i+1] == 0x80 && (text[i+2] == 0xa8 || text[i+2] == 0xa9):
				encodedSize += 6
				i += 2
			default:
				encodedSize++
			}
			if encodedSize > runtimeTextLimit {
				return fault(f.PayloadTooLarge)
			}
		}
	}
	return runtimeContextError(ctx)
}

func prepareRuntimeInput(ctx context.Context, request mc.ModelRequest, mode wire.ResponseMode) (mc.ModelRequest, error) {
	if request.Model.Snapshot.Identity.AdapterRevision == wire.OpenAIChatToolsRevision {
		return prepareRuntimeToolInput(ctx, request, mode)
	}
	if err := runtimeTextShape(ctx, request.Messages); err != nil {
		return mc.ModelRequest{}, err
	}
	if mode != wire.JSONResponse && mode != wire.SSEResponse || request.Validate() != nil {
		return mc.ModelRequest{}, fault(f.InvalidArgument)
	}
	s, c := request.Model.Snapshot, request.Model.Snapshot.Capabilities
	structured := s.Identity.AdapterRevision == wire.OpenAIChatStructuredRevision
	formatSupported := request.ResponseFormat.Kind == "text" || structured && request.ResponseFormat.Kind == "json_schema"
	modesSupported := len(c.StructuredOutputModes) == 0 || runtimeOnlyText(c.StructuredOutputModes)
	if structured {
		modesSupported = false
		for _, mode := range c.StructuredOutputModes {
			if mode == "json_schema" {
				modesSupported = true
			} else if mode != "text" {
				return mc.ModelRequest{}, fault(f.CapabilityUnsupported)
			}
		}
	}
	if !runtimeSupportedOwner(request) || len(request.Tools) != 0 || request.ToolChoice.Kind != "none" || !formatSupported || s.Identity.Profile != mc.OpenAIChatV1 || s.Identity.Protocol != mc.OpenAIChat || s.Identity.ModelType != mc.ChatModel || s.Identity.AdapterRevision != wire.OpenAIChatTextRevision && !structured || !runtimeEmptyObject(s.Parameters) || !runtimeEmptyObject(s.RequestOverwrite) || len(s.HeaderOverwrite) != 0 || c.ToolCalls || c.ParallelToolCalls || !runtimeOnlyText(c.InputModalities) || !runtimeOnlyText(c.OutputModalities) || !modesSupported || mode == wire.SSEResponse && !c.Streaming || s.CredentialRef == nil || request.Model.CredentialLease == nil {
		return mc.ModelRequest{}, fault(f.CapabilityUnsupported)
	}
	if request.Input.SchemaVersion != 1 {
		return mc.ModelRequest{}, fault(f.SchemaUnsupported)
	}
	digest, err := runtimeStructuredInputDigest(ctx, request.Messages, request.ResponseFormat)
	if err != nil {
		return mc.ModelRequest{}, err
	}
	if digest != request.Input.Digest {
		return mc.ModelRequest{}, fault(f.InvalidArgument)
	}
	copy := request.Clone()
	if err := runtimeContextError(ctx); err != nil {
		return mc.ModelRequest{}, err
	}
	return copy, nil
}

func runtimeOnlyText(values []string) bool { return len(values) == 1 && values[0] == "text" }

func runtimeEmptyObject(raw json.RawMessage) bool {
	var value map[string]json.RawMessage
	return len(raw) == 0 || json.Unmarshal(raw, &value) == nil && value != nil && len(value) == 0
}

func runtimeConsumerRequest(request mc.ModelRequest, action mc.ConsumerAction, attempt *mc.AttemptIdentity, terminal *f.Version) mc.ConsumerRequest {
	call := request.CallID
	input := request.Input.Clone()
	r := mc.ConsumerRequest{Action: action, Actor: request.Actor, Consumer: request.Consumer.Clone(), SnapshotID: request.Model.Snapshot.ID, LeaseOwner: request.Model.LeaseOwner, CallID: &call}
	if request.Model.CredentialLease != nil {
		lease := request.Model.CredentialLease.LeaseID
		r.LeaseID = &lease
	}
	if action != mc.RetireConsumer {
		r.Input = &input
	}
	if action == mc.ReadCredentialConsumer || action == mc.FinalizeConsumer {
		r.Attempt, r.TerminalVersion = attempt, terminal
	}
	return r.Clone()
}

func runtimeContextError(ctx context.Context) error {
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return unavailable(err)
	}
	return nil
}

// This only recognizes a shape. Every admission still requires the original
// ConsumerAuthority and the persisted resolution; Actor construction grants none.
func runtimeSupportedOwner(r mc.ModelRequest) bool {
	o := r.Model.LeaseOwner.Details()
	if r.RetryClass == mc.BoundedRetry {
		return o.Kind == sc.ModelCallOwner
	}
	return r.RetryClass == mc.AgentRetry && r.Consumer.Kind == mc.AgentConsumer && r.Consumer.Purpose == mc.AgentGeneration && r.Actor.Details().Kind == id.AgentRun && o.Kind == sc.ExecutionOwner && r.Consumer.ExecutionID != nil && o.ID == r.Consumer.ExecutionID.String()
}
