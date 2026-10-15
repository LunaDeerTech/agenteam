package agentloop

import (
	"context"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/schema"
)

// BuildLoopRequest projects every captured tool. Missing definitions fail the
// whole request; the caller cannot shrink the immutable set to fit a backend.
func BuildLoopRequest(ctx context.Context, captured ec.ExecutionContext, actor i.Actor, round string, call mc.CallID, definitions []tc.Definition) (DirectTextRequest, error) {
	if ctx == nil || captured.Validate() != nil || actor.Validate() != nil || call.Validate() != nil {
		return DirectTextRequest{}, invalid()
	}
	v := captured.Input().Fields()
	want, err := i.NewAgentRun(v.Request.Launch.ProjectID, v.Request.Launch.AgentID, v.Request.ExecutionID)
	if err != nil || !actor.Equal(want) || len(definitions) != len(v.Tools) || len(definitions) == 0 {
		return DirectTextRequest{}, invalid()
	}
	tools := make([]mc.Tool, len(definitions))
	for n, d := range definitions {
		if _, err = tc.CanonicalDefinition(ctx, d); err != nil {
			return DirectTextRequest{}, err
		}
		tools[n] = mc.Tool{Name: v.Tools[n].ModelVisibleName, Description: d.Description, InputSchema: d.InputSchema, OutputSchema: d.OutputSchema}
	}
	messages, err := projectMessages(ctx, captured)
	if err != nil {
		return DirectTextRequest{}, err
	}
	choice := mc.ToolChoice{Kind: "required"}
	if v.Request.Launch.Trigger.Kind == "meeting" {
		// A contribution may directly answer the frozen conversation. Its
		// available tools remain exactly the captured set; only the initial
		// choice differs from Task's existing required-tool policy.
		choice.Kind = "auto"
	}
	digest, err := mc.ToolInputDigest(ctx, messages, tools, choice)
	if err != nil {
		return DirectTextRequest{}, err
	}
	e := v.Request.ExecutionID
	request := mc.ModelRequest{Actor: actor, CallID: call, Consumer: v.Model.Consumer.Clone(), Model: v.Model.Clone(), Input: mc.InputIdentity{ExecutionID: &e, RoundID: round, Digest: digest, SchemaVersion: mc.ToolInputSchemaVersion}, Messages: messages, Tools: tools, ToolChoice: choice, ResponseFormat: mc.ResponseFormat{Kind: "text"}, RetryClass: mc.AgentRetry}
	return NewLoopRequest(ctx, captured.Digest(), request)
}

// NewLoopRequest accepts a complete fixed request. Execution still has to
// commit this exact request and supply a private live invocation owner.
func NewLoopRequest(ctx context.Context, contextDigest f.Digest, request mc.ModelRequest) (DirectTextRequest, error) {
	if ctx == nil || contextDigest.Validate() != nil || request.Validate() != nil {
		return DirectTextRequest{}, invalid()
	}
	switch request.Input.SchemaVersion {
	case mc.ToolInputSchemaVersion:
		if err := mc.ValidateToolInputRequest(ctx, request); err != nil {
			return DirectTextRequest{}, err
		}
		digest, err := mc.ToolInputDigest(ctx, request.Messages, request.Tools, request.ToolChoice)
		if err != nil || digest != request.Input.Digest {
			return DirectTextRequest{}, invalid()
		}
	case mc.SuccessorInputSchemaVersion:
		if _, err := mc.ValidateSuccessorRequest(ctx, request); err != nil {
			return DirectTextRequest{}, err
		}
	default:
		return DirectTextRequest{}, unsupported()
	}
	owned := request.Clone()
	return DirectTextRequest{data: func() (f.Digest, mc.ModelRequest) { return contextDigest, owned.Clone() }, loop: true}, nil
}

func validateLoopResponse(ctx context.Context, request mc.ModelRequest, response mc.ModelResponse) error {
	if response.Validate() != nil || response.CallID != request.CallID {
		return invalid()
	}
	if response.FinishReason == "stop" {
		if request.ToolChoice.Kind == "required" {
			return invalid()
		}
		return validateTextResponse(request.CallID, response)
	}
	if response.FinishReason != "tool_calls" {
		return invalid()
	}
	var call *mc.ToolCallPart
	for _, part := range response.Message.Parts {
		if part.ToolCall != nil {
			if call != nil {
				return unsupported()
			}
			call = part.ToolCall
		} else if part.Text == nil {
			return unsupported()
		}
	}
	if call == nil {
		return invalid()
	}
	for _, tool := range request.Tools {
		if tool.Name == call.Name {
			compiled, err := schema.Compile(ctx, tool.InputSchema)
			if err != nil {
				return err
			}
			return compiled.Validate(ctx, call.Arguments)
		}
	}
	return invalid()
}
