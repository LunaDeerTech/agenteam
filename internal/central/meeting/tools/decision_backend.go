package meetingtools

import (
	"context"
	"encoding/json"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/meeting"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	tr "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
)

type decisionBackend struct{ service *meeting.DecisionService }

func NewDecisionBackend(s *meeting.DecisionService) (tr.BuiltinBackend, error) {
	if s == nil {
		return nil, fault(f.DependencyUnbound)
	}
	return &decisionBackend{s}, nil
}

func (b *decisionBackend) Registration() tc.BuiltinRegistration {
	if b == nil || b.service == nil {
		return tc.BuiltinRegistration{}
	}
	return tc.BuiltinRegistration{
		Definition: tc.Definition{StableKey: "builtin:request-decision", Name: "request-decision", Description: "Ask the Human owner a question during this Meeting contribution. This same execution waits for an explicit answer or skip. Optional choices are suggestions; the owner may supply a different answer. Cancelling the turn cancels the pending question.", InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["question"],"properties":{"question":{"type":"string","minLength":1,"maxLength":8192},"options":{"type":"array","maxItems":32,"uniqueItems":true,"items":{"type":"string","minLength":1,"maxLength":1024}}}}`), OutputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["decision_id","version","status"],"properties":{"decision_id":{"type":"string"},"version":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"status":{"type":"string","enum":["answered","skipped"]},"answer":{"type":"string","minLength":1,"maxLength":32768}}}`), Annotations: &tc.Annotations{Idempotency: tc.Keyed}},
		Binding:    tc.BuiltinBinding{HandlerID: "meeting.request-decision", ContractRevision: 1}, ScopeResolverID: "meeting.current.v1", RiskClassifierID: "meeting.request-decision.v1", Class: tc.CoreTool, Active: true,
	}
}

func (b *decisionBackend) RequireBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if b == nil || b.service == nil || store == nil {
		return fault(f.DependencyUnbound)
	}
	return b.service.RequireBuiltinBindingInTx(ctx, tx, store)
}

func decisionInvocation(inv tr.BuiltinInvocation) (ec.MeetingDecisionCall, error) {
	call := ec.MeetingDecisionCall{Call: inv.Call, OperationID: inv.OperationID, AttemptID: inv.AttemptID, RequestID: inv.RequestID}
	if inv.Validate() != nil || call.Validate() != nil {
		return ec.MeetingDecisionCall{}, fault(f.Forbidden)
	}
	return call, nil
}

func (b *decisionBackend) Execute(ctx context.Context, inv tr.BuiltinInvocation, raw []byte) ([]byte, error) {
	if b == nil || b.service == nil {
		return nil, fault(f.DependencyUnbound)
	}
	call, err := decisionInvocation(inv)
	if err != nil {
		return nil, err
	}
	input, err := meeting.ParseDecisionInput(raw)
	if err != nil {
		return nil, err
	}
	result, err := b.service.Request(ctx, call, input)
	if err != nil {
		return nil, err
	}
	return canonical(result)
}

func (b *decisionBackend) Lookup(ctx context.Context, inv tr.BuiltinInvocation, raw []byte) ([]byte, bool, error) {
	if b == nil || b.service == nil {
		return nil, false, fault(f.DependencyUnbound)
	}
	call, err := decisionInvocation(inv)
	if err != nil {
		return nil, false, err
	}
	input, err := meeting.ParseDecisionInput(raw)
	if err != nil {
		return nil, false, err
	}
	result, found, err := b.service.Lookup(ctx, call, input)
	if err != nil || !found {
		return nil, found, err
	}
	encoded, err := canonical(result)
	return encoded, err == nil, err
}

var _ tr.BuiltinBackend = (*decisionBackend)(nil)

func fault(code f.Code) error { return f.NewFault(code, f.NotStarted) }
func canonical(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fault(f.InvalidArgument)
	}
	return cursor.CanonicalJSON(raw)
}
