package runtime

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"testing"
)

func meetingAgentCallFixture(t *testing.T) (*builtinCall, c.AgentToolCall) {
	t.Helper()
	h := readFileHandoffFixture(t)
	b := h.binding
	b.RunnerBinding = tc.RunnerBinding{}
	b.Binding = tc.BuiltinBinding{HandlerID: "meeting.request", ContractRevision: 1}
	in := c.AgentToolCall{Call: b, OperationID: h.record.ID, AttemptID: h.attempt, RequestID: h.request}
	if err := in.Validate(); err != nil {
		t.Fatal(err)
	}
	return &builtinCall{binding: b, record: builtinRecord{ID: in.OperationID, Attempt: in.AttemptID, Request: in.RequestID}}, in
}
func TestMeetingAgentAuthorityBindsOriginalAttemptAndLookupMode(t *testing.T) {
	call, in := meetingAgentCallFixture(t)
	if !sameMeetingToolCall(call, in, false) || sameMeetingToolCall(call, in, true) {
		t.Fatal("mode not bound")
	}
	for name, change := range map[string]func(*c.AgentToolCall){
		"operation": func(v *c.AgentToolCall) { v.OperationID = newOperationID[tc.Operation](t) },
		"attempt":   func(v *c.AgentToolCall) { v.AttemptID = newOperationID[tc.Attempt](t) },
		"request":   func(v *c.AgentToolCall) { v.RequestID = p.ID(newOperationID[f.Request](t).String()) },
		"call":      func(v *c.AgentToolCall) { v.Call.CallID += "-foreign" },
		"handler":   func(v *c.AgentToolCall) { v.Call.Binding.HandlerID = "meeting.list" },
		"decision":  func(v *c.AgentToolCall) { v.Call.Binding.HandlerID = "meeting.request-decision" },
	} {
		t.Run(name, func(t *testing.T) {
			foreign := in
			change(&foreign)
			if sameMeetingToolCall(call, foreign, false) {
				t.Fatal("foreign invocation accepted")
			}
		})
	}
	call.lookup = true
	if sameMeetingToolCall(call, in, false) || !sameMeetingToolCall(call, in, true) {
		t.Fatal("lookup became mutation")
	}
}
func TestMeetingAgentDTOCannotBorrowExecutionInput(t *testing.T) {
	call, in := meetingAgentCallFixture(t)
	a := &BuiltinAuthority{}
	for _, mode := range []string{"bare", "inactive", "foreign-owner"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			if mode != "bare" {
				owner := a
				if mode == "foreign-owner" {
					owner = &BuiltinAuthority{}
				}
				w := &builtinWitness{owner: &BuiltinExecutor{authority: owner}, call: call}
				w.active.Store(mode == "foreign-owner")
				ctx = context.WithValue(ctx, builtinWitnessKey{}, w)
			}
			_, err := a.DiscoverMeetingTool(ctx, in, false)
			schemaFault(t, err, f.Forbidden)
			schemaFault(t, a.WithMeetingToolInputInTx(ctx, f.Tx{}, in, false, func(context.Context, []byte, c.AgentSource) error {
				t.Fatal("bare DTO reached protected input")
				return nil
			}), f.Forbidden)
		})
	}
}
