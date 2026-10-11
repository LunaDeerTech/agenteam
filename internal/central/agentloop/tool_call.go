package agentloop

import (
	"context"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// ToolCallController is an explicit, separately constructed first-turn owner.
// It shares actual handle retirement with DirectText, never its no-tools gate.
// No default initializer constructs this controller.
type ToolCallController struct{ owner *DirectTextController }

func NewToolCallController(models mc.JSONCaller) (*ToolCallController, error) {
	owner, err := NewDirectTextController(models)
	if err != nil {
		return nil, err
	}
	return &ToolCallController{owner}, nil
}
func (c *ToolCallController) Accept(request DirectTextRequest) (*DirectTextSession, error) {
	if c == nil || c.owner == nil {
		return nil, loopFault(f.DependencyUnbound)
	}
	if !request.toolCall {
		return nil, unsupported()
	}
	return c.owner.accept(request)
}
func (c *ToolCallController) Stop() {
	if c != nil && c.owner != nil {
		c.owner.Stop()
	}
}
func (c *ToolCallController) Drain(ctx context.Context) error {
	if c == nil || c.owner == nil {
		return loopFault(f.DependencyUnbound)
	}
	return c.owner.Drain(ctx)
}
func (c *ToolCallController) Joined() bool { return c != nil && c.owner != nil && c.owner.Joined() }

// The caller obtained definition in its real current Tool transaction. This
// builder binds exact bytes to the captured name/spec; it grants no authority.
func BuildToolCallRequest(ctx context.Context, captured ec.ExecutionContext, actor i.Actor, roundID string, callID mc.CallID, definition tc.Definition) (DirectTextRequest, error) {
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
	v := captured.Input().Fields()
	a := actor.Details()
	if a.Kind != i.AgentRun || a.ProjectID != v.Request.Launch.ProjectID.String() || a.AgentID != v.Request.Launch.AgentID.String() || a.ExecutionID != v.Request.ExecutionID.String() {
		return zero, loopFault(f.Forbidden)
	}
	if len(v.Tools) != 1 || v.Tools[0].BindingSnapshot.RunnerBinding.Validate() != nil || definition.StableKey != "runner:read-file" || !v.Model.Snapshot.Capabilities.ToolCalls || v.Model.Snapshot.Capabilities.ParallelToolCalls || v.Agent.Fields().Core.InjectAgentsMD || v.Agent.Fields().Core.ReasoningEffort != nil || len(v.Request.Launch.Policy.AllowedResourceConstraints) != 0 {
		return zero, unsupported()
	}
	canonical, err := tc.CanonicalDefinition(ctx, definition)
	if err != nil {
		return zero, err
	}
	_ = canonical // Validation includes complete schemas, not a keyword subset.
	tool := mc.Tool{Name: v.Tools[0].ModelVisibleName, Description: definition.Description, InputSchema: definition.InputSchema, OutputSchema: definition.OutputSchema}
	messages, err := projectMessages(ctx, captured)
	if err != nil {
		return zero, err
	}
	digest, err := ec.ToolCallInputDigest(ctx, messages, tool)
	if err != nil {
		return zero, err
	}
	execution := v.Request.ExecutionID
	r := mc.ModelRequest{Actor: actor, CallID: callID, Consumer: v.Model.Consumer.Clone(), Model: v.Model.Clone(), Input: mc.InputIdentity{ExecutionID: &execution, RoundID: roundID, Digest: digest, SchemaVersion: ec.ToolCallRoundSchemaVersion}, Messages: messages, Tools: []mc.Tool{tool}, ToolChoice: mc.ToolChoice{Kind: "required"}, ResponseFormat: mc.ResponseFormat{Kind: "text"}, RetryClass: mc.AgentRetry}
	if r.Validate() != nil {
		return zero, invalid()
	}
	owned := r.Clone()
	return DirectTextRequest{data: func() (f.Digest, mc.ModelRequest) { return captured.Digest(), owned.Clone() }, toolCall: true}, nil
}

func validateSessionResponse(request DirectTextRequest, response mc.ModelResponse) error {
	if !request.toolCall {
		return validateTextResponse(request.Request().CallID, response)
	}
	r := request.Request()
	if response.Validate() != nil || response.CallID != r.CallID || response.FinishReason != "tool_calls" || len(r.Tools) != 1 {
		return invalid()
	}
	count := 0
	for _, part := range response.Message.Parts {
		if part.ToolCall != nil {
			count++
			if part.ToolCall.Name != r.Tools[0].Name {
				return invalid()
			}
			if _, err := tc.DecodeReadFileToolArguments(context.Background(), part.ToolCall.Arguments); err != nil {
				return err
			}
		} else if part.Text == nil {
			return unsupported()
		}
	}
	if count != 1 {
		return unsupported()
	}
	return nil
}
