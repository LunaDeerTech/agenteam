package meetingtools

import (
	"context"
	"encoding/json"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/meeting"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	tr "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
)

type agentBackend struct {
	service *meeting.AgentService
	handler string
}

func NewAgentBackends(s *meeting.AgentService) ([]tr.BuiltinBackend, error) {
	if s == nil {
		return nil, fault(f.DependencyUnbound)
	}
	out := []tr.BuiltinBackend{}
	for _, handler := range []string{"meeting.list", "meeting.request", "meeting.add-agent", "meeting.remove-agent"} {
		out = append(out, &agentBackend{s, handler})
	}
	return out, nil
}
func (b *agentBackend) Registration() tc.BuiltinRegistration {
	if b == nil || b.service == nil {
		return tc.BuiltinRegistration{}
	}
	var name, description, input string
	switch b.handler {
	case "meeting.list":
		name = "list-meetings"
		description = "List meetings in this execution's project. Optional status is active (default), proposed or archive. Returns a bounded snapshot."
		input = `{"type":"object","additionalProperties":false,"properties":{"status":{"type":"string","enum":["active","proposed","archive"]},"limit":{"type":"integer","minimum":1,"maximum":100}}}`
	case "meeting.request":
		name = "request-meeting"
		description = "Propose a meeting for the Human owner to review. Supply a reason and suggested agents from this project; the requesting agent is included. The meeting stays proposed until approved and no turn starts. Source execution and origin are derived by the server."
		input = `{"type":"object","additionalProperties":false,"required":["proposal_content"],"properties":{"proposal_content":{"type":"string","minLength":1,"maxLength":32768},"agents":{"type":"array","maxItems":32,"uniqueItems":true,"items":{"type":"string"}}}}`
	case "meeting.add-agent":
		name = "add-agent-to-meeting"
		description = "Add a current project Agent to a proposed or active meeting in which this Agent participates. Requires the current meeting version. Affects future turns only."
		input = `{"type":"object","additionalProperties":false,"required":["meeting_id","expected_version","agent_id"],"properties":{"meeting_id":{"type":"string"},"expected_version":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"agent_id":{"type":"string"}}}`
	case "meeting.remove-agent":
		name = "remove-agent-from-meeting"
		description = "Remove an Agent from future turns of a proposed or active meeting in which this Agent participates. Requires the current meeting version. Existing turn snapshots and messages are retained."
		input = `{"type":"object","additionalProperties":false,"required":["meeting_id","expected_version","agent_id"],"properties":{"meeting_id":{"type":"string"},"expected_version":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"agent_id":{"type":"string"}}}`
	default:
		return tc.BuiltinRegistration{}
	}
	return tc.BuiltinRegistration{Definition: tc.Definition{StableKey: "builtin:" + name, Name: name, Description: description, InputSchema: json.RawMessage(input), OutputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"oneOf":[{"required":["receipt"]},{"required":["meetings"]}],"properties":{"receipt":{"type":"object","additionalProperties":false,"required":["meeting_id","version","replayed"],"properties":{"meeting_id":{"type":"string"},"version":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"replayed":{"type":"boolean"},"turn_id":{"type":"string"}}},"meetings":{"type":"array","items":{"type":"object"}}}}`), Annotations: &tc.Annotations{Idempotency: tc.Keyed}}, Binding: tc.BuiltinBinding{HandlerID: b.handler, ContractRevision: 1}, ScopeResolverID: "meeting.project.v1", RiskClassifierID: tc.RiskClassifierID(b.handler + ".v1"), Class: tc.CoreTool, Active: true}
}
func (b *agentBackend) RequireBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if b == nil || b.service == nil {
		return fault(f.DependencyUnbound)
	}
	return b.service.RequireBuiltinBindingInTx(ctx, tx, store)
}
func (b *agentBackend) invocation(inv tr.BuiltinInvocation) (c.AgentToolCall, error) {
	v := c.AgentToolCall{Call: inv.Call, OperationID: inv.OperationID, AttemptID: inv.AttemptID, RequestID: inv.RequestID}
	if inv.Validate() != nil || v.Validate() != nil || v.Call.Binding.HandlerID != b.handler {
		return c.AgentToolCall{}, fault(f.Forbidden)
	}
	return v, nil
}
func (b *agentBackend) Execute(ctx context.Context, inv tr.BuiltinInvocation, raw []byte) ([]byte, error) {
	if b == nil || b.service == nil {
		return nil, fault(f.DependencyUnbound)
	}
	call, err := b.invocation(inv)
	if err != nil {
		return nil, err
	}
	in, err := meeting.ParseAgentInput(b.handler, raw)
	if err != nil {
		return nil, err
	}
	out, err := b.service.Execute(ctx, call, in)
	if err != nil {
		return nil, err
	}
	return canonical(out)
}
func (b *agentBackend) Lookup(ctx context.Context, inv tr.BuiltinInvocation, raw []byte) ([]byte, bool, error) {
	if b == nil || b.service == nil {
		return nil, false, fault(f.DependencyUnbound)
	}
	call, err := b.invocation(inv)
	if err != nil {
		return nil, false, err
	}
	in, err := meeting.ParseAgentInput(b.handler, raw)
	if err != nil {
		return nil, false, err
	}
	out, found, err := b.service.Lookup(ctx, call, in)
	if err != nil || !found {
		return nil, found, err
	}
	encoded, err := canonical(out)
	return encoded, err == nil, err
}

var _ tr.BuiltinBackend = (*agentBackend)(nil)
