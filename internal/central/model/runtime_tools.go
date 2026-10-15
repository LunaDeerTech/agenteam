package model

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func prepareRuntimeToolInput(ctx context.Context, request mc.ModelRequest, mode wire.ResponseMode) (mc.ModelRequest, error) {
	if err := runtimeContextError(ctx); err != nil {
		return mc.ModelRequest{}, err
	}
	if request.Validate() != nil {
		return mc.ModelRequest{}, fault(f.InvalidArgument)
	}
	s, c := request.Model.Snapshot, request.Model.Snapshot.Capabilities
	if mode != wire.JSONResponse || !runtimeSupportedOwner(request) || request.RetryClass != mc.AgentRetry || request.ResponseFormat.Kind != "text" || s.Identity.Profile != mc.OpenAIChatV1 || s.Identity.Protocol != mc.OpenAIChat || s.Identity.ModelType != mc.ChatModel || !runtimeEmptyObject(s.Parameters) || !runtimeEmptyObject(s.RequestOverwrite) || len(s.HeaderOverwrite) != 0 || !c.ToolCalls || c.ParallelToolCalls || c.Reasoning || len(c.ReasoningEfforts) != 0 || !runtimeOnlyText(c.InputModalities) || !runtimeOnlyText(c.OutputModalities) || len(c.StructuredOutputModes) > 0 && !runtimeOnlyText(c.StructuredOutputModes) || s.CredentialRef == nil || request.Model.CredentialLease == nil {
		return mc.ModelRequest{}, fault(f.CapabilityUnsupported)
	}
	// This profile consumes text tool results. Preserve canonical media parts
	// and their business references in the caller's history, but reject the
	// unsupported input before consumer admission, Usage or wire preparation.
	for _, message := range request.Messages {
		for _, part := range message.Parts {
			if part.ToolResult == nil {
				continue
			}
			for _, result := range part.ToolResult.Parts {
				if result.Text == nil {
					return mc.ModelRequest{}, fault(f.CapabilityUnsupported)
				}
			}
		}
	}
	switch request.Input.SchemaVersion {
	case mc.ToolCallInputSchemaVersion:
		if len(request.Tools) != 1 || request.ToolChoice != (mc.ToolChoice{Kind: "required"}) {
			return mc.ModelRequest{}, fault(f.InvalidArgument)
		}
		digest, err := mc.ToolCallInputDigest(ctx, request.Messages, request.Tools[0])
		if err != nil {
			return mc.ModelRequest{}, runtimePortError(err)
		}
		if digest != request.Input.Digest {
			return mc.ModelRequest{}, fault(f.InvalidArgument)
		}
	case mc.ToolInputSchemaVersion:
		if err := mc.ValidateToolInputRequest(ctx, request); err != nil {
			return mc.ModelRequest{}, runtimePortError(err)
		}
	case mc.SuccessorInputSchemaVersion:
		if _, err := mc.ValidateSuccessorRequest(ctx, request); err != nil {
			return mc.ModelRequest{}, runtimePortError(err)
		}
	default:
		return mc.ModelRequest{}, fault(f.SchemaUnsupported)
	}
	// The adapter performs native shape and total request-byte checks before any
	// wire admission; the canonical input above is also independently bounded.
	if err := runtimeContextError(ctx); err != nil {
		return mc.ModelRequest{}, err
	}
	return request.Clone(), nil
}

func runtimeResponseMessage(result wire.Result) mc.Message {
	parts := make([]mc.MessagePart, 0, 1+len(result.ToolCalls))
	if result.Text != "" || len(result.ToolCalls) == 0 {
		parts = append(parts, mc.MessagePart{Text: &mc.TextPart{Text: result.Text}})
	}
	for _, call := range result.ToolCalls {
		owned := call.Clone()
		parts = append(parts, mc.MessagePart{ToolCall: &owned})
	}
	return mc.Message{Role: "assistant", Parts: parts}
}
