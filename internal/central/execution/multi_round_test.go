package execution

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// Pure candidate tests exercise complete history, fixed Tools and the terminal
// projection. Durable reservation/Tool/Model authority is verified with PG/App.
func TestMultiRoundSuccessorsPreserveHistoryAndFinalIdentity(t *testing.T) {
	ctx := context.Background()
	facts, consumer := runtimeModelCandidate(t)
	fields := facts.Round.Fields()
	definitions := []tc.Definition{tc.ReadFileDefinition(), tc.WriteFileDefinition(), tc.RunCommandDefinition()}
	tools := make([]mc.Tool, len(definitions))
	for n, def := range definitions {
		tools[n] = mc.Tool{Name: fmt.Sprintf("tool_019000000000700080000000000009%02d", n+1), Description: def.Description, InputSchema: def.InputSchema, OutputSchema: def.OutputSchema}
	}
	digest, err := mc.ToolInputDigest(ctx, fields.Messages, tools, mc.ToolChoice{Kind: "required"})
	if err != nil {
		t.Fatal(err)
	}
	fields.Input.SchemaVersion = mc.ToolInputSchemaVersion
	fields.Input.Digest = digest
	initial, err := c.NewLoopRound(c.LoopRoundFields{Round: fields, Tools: tools, Choice: mc.ToolChoice{Kind: "required"}, Ordinal: 1, TranscriptStart: 1})
	if err != nil {
		t.Fatal(err)
	}
	request := mc.ModelRequest{Actor: consumer.Actor, CallID: fields.CallID, Consumer: consumer.Consumer, Model: facts.Snapshot.Fields().Context.Input().Fields().Model, Input: fields.Input, Messages: fields.Messages, Tools: tools, ToolChoice: mc.ToolChoice{Kind: "required"}, ResponseFormat: mc.ResponseFormat{Kind: "text"}, RetryClass: mc.AgentRetry}
	fixed, err := agentloop.NewLoopRequest(ctx, fields.ContextDigest, request)
	if err != nil {
		t.Fatal(err)
	}
	run := &directTextCall{owner: &directTextState{tools: &toolCallDependencies{}, multi: &multiRoundState{}}, request: facts.Snapshot.Fields().Context.Input().Fields().Request, snapshot: facts.Snapshot, round: loopExecutionRound{initial}, invocation: &directTextInvocation{request: fixed}}
	calls := map[mc.CallID]bool{fields.CallID: true}
	for turn := 0; turn < 4; turn++ {
		call := fmt.Sprintf("provider-call-%d", turn)
		response := mc.ModelResponse{CallID: run.round.Fields().CallID, InvocationID: newTestID[mc.Invocation](t), Message: mc.Message{Role: "assistant", Parts: []mc.MessagePart{{ToolCall: &mc.ToolCallPart{ID: call, Name: tools[turn%3].Name, Arguments: []byte(`{}`)}}}}, FinishReason: "tool_calls", Usage: mc.Usage{Source: mc.UnknownUsage}}
		run.toolFacts = &toolCallCandidate{response: response}
		part := mc.ToolResultPart{CallID: call, Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: fmt.Sprintf("{\"turn\":%d}", turn)}}}}
		message := mc.Message{Role: "tool", Parts: []mc.MessagePart{{ToolResult: &part}}}
		at, _ := f.NewInstant(fields.CreatedAt.Time().Add(time.Duration(turn+1) * time.Second))
		next, err := newLoopSuccessor(ctx, run, tc.RunnerResultBinding{}, message, at, f.Version(4+turn*2))
		if err != nil {
			t.Fatal(err)
		}
		r := next.request.Request()
		v := next.round.LoopRound.Fields()
		if r.Input.SchemaVersion != mc.SuccessorInputSchemaVersion || len(r.Messages) != 4+2*turn || !reflect.DeepEqual(r.Tools, tools) || !r.Model.Consumer.Equal(request.Model.Consumer) || !r.Model.LeaseOwner.Equal(request.Model.LeaseOwner) || r.Model.CredentialLease.LeaseID != request.Model.CredentialLease.LeaseID {
			t.Fatal("successor rewrote captured identity or dropped history/tools")
		}
		if calls[r.CallID] || v.Round.SnapshotID != fields.SnapshotID || *v.Predecessor != run.round.Fields().ID || v.Ordinal != f.Sequence(turn+2) || roundAssistantSequence(next.round) != f.Sequence(4+turn*2) {
			t.Fatal("successor did not reserve distinct monotonic identity")
		}
		calls[r.CallID] = true
		decoded, err := c.DecodeLoopRound(next.round.CanonicalBytes())
		if err != nil || decoded.Digest() != next.round.Digest() {
			t.Fatal("roundtrip", err)
		}
		if _, err = loopModelRequest(ctx, facts.Snapshot, next.round); err != nil {
			t.Fatal("canonical request cannot be consumed", err)
		}
		run.round = next.round
		run.invocation = &directTextInvocation{request: next.request}
	}
	at, _ := f.NewInstant(run.round.Fields().CreatedAt.Time().Add(time.Second))
	response := mc.ModelResponse{CallID: run.round.Fields().CallID, InvocationID: newTestID[mc.Invocation](t), Message: mc.Message{Role: "assistant", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "statistics validated"}}}}, FinishReason: "stop", Usage: mc.Usage{Source: mc.UnknownUsage}}
	raw, err := directTextJSON(response)
	if err != nil {
		t.Fatal(err)
	}
	run.terminal = &directTextTerminal{status: c.Succeeded, reason: "completed", version: 12, through: 10, at: at, response: &response, responseBytes: raw}
	snapshot, err := finalRuntimeSnapshot(run.round, run.terminal)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Schema != c.DurableFinalTextV2 || snapshot.Items[0].ID != run.round.Fields().ID || snapshot.Items[0].SourceReference.ID == fields.ID || snapshot.Items[0].StartedAt == fields.CreatedAt || snapshot.IncludedUpdateProgress != 1 {
		t.Fatal("terminal snapshot lost actual final round")
	}
	sql := &runtimeSnapshotSQL{}
	if err = saveDirectTextRuntime(ctx, sql, run); err != nil || sql.writes != 1 {
		t.Fatal("multi-round terminal snapshot skipped", err)
	}
	if err = confirmDirectTextRuntime(ctx, sql, run); err != nil {
		t.Fatal("terminal unknown observation did not match snapshot", err)
	}
}

func TestMultiRoundResultIdentityUsesActorSemantics(t *testing.T) {
	_, consumer := runtimeModelCandidate(t)
	a := tc.RunnerResultBinding{Call: tc.ToolCallBinding{Actor: consumer.Actor}, OperationID: newTestID[tc.Operation](t), AttemptID: newTestID[tc.Attempt](t), ResultDigest: c.TriggerInputDigest([]byte("result"))}
	if !sameLoopResultBinding(a, a) {
		t.Fatal("actual Agent Actor function was compared by reflect")
	}
	b := a
	b.Call.Actor = consumer.Actor
	if !sameLoopResultBinding(a, b) {
		t.Fatal("equal copied Actor rejected")
	}
	b.ResultDigest = c.TriggerInputDigest([]byte("other"))
	if sameLoopResultBinding(a, b) {
		t.Fatal("changed result provenance admitted")
	}
	b = a
	b.AttemptID = newTestID[tc.Attempt](t)
	if sameLoopResultBinding(a, b) {
		t.Fatal("changed Attempt admitted")
	}
}

type loopJoinControl struct {
	cancelled, joined int
	joinErr           error
}

func (*loopJoinControl) Execute(context.Context, tc.ToolCallBinding) (tc.RunnerOutcome, error) {
	panic("must not redispatch")
}
func (*loopJoinControl) Lookup(context.Context, tc.ToolCallBinding) (tc.RunnerOutcome, bool, error) {
	panic("join cannot invent durable result")
}
func (*loopJoinControl) Stop()                       { panic("must not stop another Execution") }
func (*loopJoinControl) Drain(context.Context) error { panic("must not drain another Execution") }
func (*loopJoinControl) Joined() bool                { panic("global Joined is not a call proof") }
func (v *loopJoinControl) CancelCall(ctx context.Context, _ tc.ToolCallBinding) error {
	v.cancelled++
	return ctx.Err()
}
func (v *loopJoinControl) JoinCall(ctx context.Context, _ tc.ToolCallBinding) error {
	v.joined++
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return v.joinErr
}

func TestMultiRoundCancelRetainsExactUnknownJoin(t *testing.T) {
	run := runtimeCandidate(t, "unused")
	run.terminal = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	run.ctx = ctx
	original := f.NewFault(f.CommitUnknown, f.Unknown)
	runner := &loopJoinControl{joinErr: original}
	state := &directTextState{multi: &multiRoundState{dispatch: runner}, calls: map[i.ExecutionID]*directTextCall{run.request.ExecutionID: run}}
	run.owner = state
	_, err := state.finishLoopDispatch(run, tc.ToolCallBinding{}, context.Canceled)
	if !directTextUnknown(err) || runner.cancelled != 1 || runner.joined != 1 || run.unresolved == nil || run.uncertainty != "loop-dispatch" || run.terminal != nil || state.calls[run.request.ExecutionID] != run {
		t.Fatal("cancel signal retired an unresolved Tool owner")
	}
	_, err = state.finishLoopDispatch(run, tc.ToolCallBinding{}, context.Canceled)
	if !directTextUnknown(err) || runner.cancelled != 2 || runner.joined != 2 || run.terminal != nil {
		t.Fatal("repeated observation converted Unknown to retirement")
	}
}

func TestMultiRoundKnownReceiptCannotBypassOriginalJoin(t *testing.T) {
	run := runtimeCandidate(t, "unused")
	run.terminal = nil
	run.ctx = context.Background()
	runner := &loopJoinControl{joinErr: f.NewFault(f.CommitUnknown, f.Unknown)}
	state := &directTextState{multi: &multiRoundState{dispatch: runner}, calls: map[i.ExecutionID]*directTextCall{run.request.ExecutionID: run}}
	run.owner = state
	// No ResultAuthority/Store is bound: reaching either would panic or prove
	// that a durable receipt was mistaken for actual owner retirement.
	_, err := state.continueLoopTool(context.Background(), run, tc.ToolCallBinding{}, tc.RunnerOutcome{Phase: "succeeded", Outcome: "success"})
	if !directTextUnknown(err) || runner.joined != 1 || runner.cancelled != 0 || run.unresolved == nil || run.next != nil || run.terminal != nil {
		t.Fatal("known receipt bypassed original retained owner")
	}
}

func TestMultiRoundClassifiesOnlyActualPayloadAvailability(t *testing.T) {
	for _, tc := range []struct {
		name, phase, outcome                            string
		available, wantPayload, wantCancel, wantUnknown bool
	}{
		{"success payload", "succeeded", "success", true, true, false, false},
		{"command nonzero with report", "failed", "failure", true, true, false, false},
		{"command cancelled with report", "cancelled", "cancelled", true, true, false, false},
		{"read failure without payload", "failed", "failure", false, false, false, false},
		{"write failure without payload", "failed", "failure", false, false, false, false},
		{"command no report", "failed", "failure", false, false, false, false},
		{"cancel no report", "cancelled", "cancelled", false, false, true, false},
		{"unknown result never retires", "failed", "unknown", false, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := loopPayloadGate(toolOutcome(tc.phase, tc.outcome, tc.available))
			if (err == nil) != tc.wantPayload || errors.Is(err, context.Canceled) != tc.wantCancel || directTextUnknown(err) != tc.wantUnknown {
				t.Fatal("terminal classification lost actual payload/cancel/Unknown facts")
			}
		})
	}
}
func toolOutcome(phase, outcome string, available bool) tc.RunnerOutcome {
	return tc.RunnerOutcome{Phase: phase, Outcome: outcome, ResultAvailable: available}
}
