package runtime

import (
	"context"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func meetingDecisionCallFixture(t *testing.T) (*builtinCall, ec.MeetingDecisionCall) {
	t.Helper()
	h := readFileHandoffFixture(t)
	b := h.binding
	b.RunnerBinding = tc.RunnerBinding{}
	b.Binding = tc.BuiltinBinding{HandlerID: "meeting.request-decision", ContractRevision: 1}
	in := ec.MeetingDecisionCall{Call: b, OperationID: h.record.ID, AttemptID: h.attempt, RequestID: h.request}
	if err := in.Validate(); err != nil {
		t.Fatal(err)
	}
	return &builtinCall{binding: b, record: builtinRecord{ID: in.OperationID, Attempt: in.AttemptID, Request: in.RequestID}}, in
}

func TestMeetingDecisionCallRequiresOriginalAttemptAndLookupMode(t *testing.T) {
	call, in := meetingDecisionCallFixture(t)
	if !sameMeetingDecisionCall(call, in, false) || sameMeetingDecisionCall(call, in, true) {
		t.Fatal("live call and readonly lookup were not distinguished")
	}
	for name, change := range map[string]func(*ec.MeetingDecisionCall){
		"operation": func(v *ec.MeetingDecisionCall) { v.OperationID = newOperationID[tc.Operation](t) },
		"attempt":   func(v *ec.MeetingDecisionCall) { v.AttemptID = newOperationID[tc.Attempt](t) },
		"request":   func(v *ec.MeetingDecisionCall) { v.RequestID = p.ID(newOperationID[f.Request](t).String()) },
		"call":      func(v *ec.MeetingDecisionCall) { v.Call.CallID += "-foreign" },
		"handler":   func(v *ec.MeetingDecisionCall) { v.Call.Binding.HandlerID = "meeting.comment" },
	} {
		t.Run(name, func(t *testing.T) {
			foreign := in
			change(&foreign)
			if sameMeetingDecisionCall(call, foreign, false) {
				t.Fatal("foreign tuple admitted")
			}
		})
	}
	call.lookup = true
	if sameMeetingDecisionCall(call, in, false) || !sameMeetingDecisionCall(call, in, true) {
		t.Fatal("committed lookup became live invocation")
	}
}

func TestMeetingDecisionDTOCannotBorrowExecution(t *testing.T) {
	call, in := meetingDecisionCallFixture(t)
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
			_, err := a.DiscoverMeetingDecisionTool(ctx, in, false)
			schemaFault(t, err, f.Forbidden)
			consume := func(context.Context, []byte) error { t.Fatal("unissued invocation reached callback"); return nil }
			schemaFault(t, a.WithMeetingDecisionToolInTx(ctx, f.Tx{}, in, in.OperationID.String(), false, consume), f.Forbidden)
			schemaFault(t, a.SetMeetingDecisionToolWaitingInTx(ctx, f.Tx{}, in, in.OperationID.String(), true), f.Forbidden)
			schemaFault(t, a.WithCommittedMeetingDecisionToolInTx(ctx, f.Tx{}, in, consume), f.Forbidden)
		})
	}
}
