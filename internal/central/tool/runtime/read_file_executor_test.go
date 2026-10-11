package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	runnerhttp "github.com/LunaDeerTech/agenteam/internal/central/runner/http"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// Private terminal/consumer controls below do not manufacture an actual guard,
// Execution authority, SQL transaction, ticket or WSS dispatch. Those paths are
// covered by the real composite PG fixture, separately from these boundaries.
func readFileHandoffFixture(t *testing.T) *readFileHandoff {
	t.Helper()
	in := readFileProfileFixture(t)
	actor, err := i.NewAgentRun(in.Binding.Project, in.Binding.Agent, in.Binding.Execution)
	if err != nil {
		t.Fatal(err)
	}
	b := in.Binding
	binding := tc.ToolCallBinding{Actor: actor, ProjectID: b.Project, AgentID: b.Agent, ExecutionID: b.Execution, RoundID: b.Round, SnapshotID: b.Snapshot, InputBindingID: b.InputBinding, LogicalCallID: b.LogicalCall, InvocationID: b.Invocation, CallID: b.Call, ModelVisibleName: b.ModelName, Spec: b.Spec, RunnerBinding: tc.ReadFileRegistration().Binding, Input: tc.ToolInputReference{PayloadID: b.Payload, SHA256: b.PayloadDigest, Bytes: b.PayloadBytes}, CanonicalArguments: b.ArgumentsDigest}
	encoded, _ := encodeReadFileInput(in)
	fingerprint, _ := readFileFingerprint(in)
	attempt := newOperationID[tc.Attempt](t)
	return &readFileHandoff{execution: b.Execution, binding: binding, record: readFileOperationRecord{ID: newOperationID[tc.Operation](t), Input: in, InputDigest: digest(encoded), Fingerprint: fingerprint, Phase: "running", Version: 2, Attempt: attempt}, arguments: tc.ReadFileToolArguments{MountID: in.Target.Mount, Arguments: p.ReadFileArguments{Path: "private-reader-canary.txt", Offset: 0, Length: 64, Encoding: "utf8"}}, attempt: attempt, request: p.ID(newOperationID[f.Request](t).String())}
}

func TestReadFileTerminalPreservesActualAttemptAndIdentity(t *testing.T) {
	h := readFileHandoffFixture(t)
	file := p.ReadFileResult{Path: h.arguments.Arguments.Path, Offset: 0, BytesRead: 64, EOF: true, Encoding: "utf8", Data: strings.Repeat("x", 64)}
	body, err := p.EncodeReadFileResult(file)
	if err != nil {
		t.Fatal(err)
	}
	response := p.Response{RequestID: h.request, OperationID: p.ID(h.record.ID.String()), Outcome: p.Success, Payload: body}
	for _, tc := range []struct {
		name          string
		result        runnerhttp.ReadFileDispatchResult
		err           error
		outcome, code string
		attempted     bool
	}{
		{"success", runnerhttp.ReadFileDispatchResult{Attempted: true, Terminal: &response}, nil, "success", "", true},
		{"known-not-sent", runnerhttp.ReadFileDispatchResult{}, context.Canceled, "failure", "not_dispatched", false},
		{"sent-without-terminal", runnerhttp.ReadFileDispatchResult{Attempted: true}, context.Canceled, "unknown", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			terminal, payload := terminalReadFile(h, tc.result, tc.err)
			if terminal.Outcome != tc.outcome || terminal.Code != tc.code || terminal.Attempted != tc.attempted || terminal.validate(h.record) != nil {
				t.Fatal("actual attempt/terminal classification lost")
			}
			if tc.outcome == "success" {
				if terminal.Result == nil {
					t.Fatal("complete result reference missing")
				}
				got, err := decodeReadFileStoredResult(*terminal.Result, payload, h.record.ID, h.attempt, h.arguments.Arguments)
				if err != nil || got != file {
					t.Fatal("terminal bytes not preserved")
				}
			} else if terminal.Result != nil || payload != nil {
				t.Fatal("failure gained file output")
			}
		})
	}
	for _, name := range []string{"request", "operation", "body", "safe-message"} {
		t.Run(name, func(t *testing.T) {
			changed := response
			switch name {
			case "request":
				changed.RequestID = p.ID(newOperationID[f.Request](t).String())
			case "operation":
				changed.OperationID = p.ID(newOperationID[struct{}](t).String())
			case "body":
				changed.Payload = []byte(`{}`)
			case "safe-message":
				message := "unexpected"
				changed.SafeMessage = &message
			}
			terminal, body := terminalReadFile(h, runnerhttp.ReadFileDispatchResult{Attempted: true, Terminal: &changed}, nil)
			if terminal.Outcome != "unknown" || terminal.Code != "backend_contract_violation" || terminal.Result != nil || body != nil {
				t.Fatal("foreign/malformed terminal became known success")
			}
		})
	}
	code := p.CancelledCode
	message := code.SafeMessage()
	cancelled := response
	cancelled.Payload = nil
	cancelled.Outcome = p.Cancelled
	cancelled.Code = &code
	cancelled.SafeMessage = &message
	terminal, _ := terminalReadFile(h, runnerhttp.ReadFileDispatchResult{Attempted: true, Terminal: &cancelled}, nil)
	if terminal.Outcome != "cancelled" || terminal.validate(h.record) != nil {
		t.Fatal("actual cancelled terminal lost")
	}
}

type readFileLoanControl struct {
	binding tc.ToolCallBinding
	raw     []byte
	mode    string
}

func (c *readFileLoanControl) DiscoverReadFileCall(context.Context, i.ExecutionID) (tc.ReadFileCallPlan, error) {
	return nil, fail(f.DependencyUnbound)
}
func (c *readFileLoanControl) WithReadFileCallInTx(ctx context.Context, _ f.Tx, _ tc.ReadFileCallPlan, consume func(context.Context, tc.ToolCallBinding, []byte) error) error {
	if c.mode == "omit" {
		return nil
	}
	b := c.binding
	raw := c.raw
	if c.mode == "wrong-binding" {
		b.CallID = "other-call"
	}
	if c.mode == "substituted-bytes" {
		raw = []byte(`{}`)
	}
	err := consume(ctx, b, raw)
	if c.mode == "duplicate" {
		return consume(ctx, b, raw)
	}
	if c.mode == "swallow-error" {
		return nil
	}
	return err
}

func TestReadFileConsumerBindsOriginalBorrowedArguments(t *testing.T) {
	for _, mode := range []string{"valid", "wrong-binding", "substituted-bytes", "omit", "duplicate", "swallow-error"} {
		t.Run(mode, func(t *testing.T) {
			h := readFileHandoffFixture(t)
			raw := []byte(fmt.Sprintf(`{"mount_id":%q,"path":"private-reader-canary.txt","offset":"0","length":64,"encoding":"utf8"}`, h.arguments.MountID.String()))
			canonical, err := cursor.CanonicalJSON(raw)
			if err != nil {
				t.Fatal(err)
			}
			h.binding.Input.SHA256 = digest(raw)
			h.binding.Input.Bytes = f.Progress(len(raw))
			h.binding.CanonicalArguments = digest(canonical)
			port := &readFileLoanControl{binding: h.binding, raw: raw, mode: mode}
			d := &readFileAuthorityState{executions: port}
			calls := 0
			err = consumeReadFileInput(context.Background(), f.NewTx(), d, h, func(context.Context, []byte) error {
				calls++
				if mode == "swallow-error" {
					return fail(f.Forbidden)
				}
				return nil
			})
			if mode == "valid" {
				if err != nil || calls != 1 {
					t.Fatal("valid original loan rejected")
				}
			} else if err == nil {
				t.Fatal("incomplete/foreign consumer boundary accepted")
			}
			if mode != "valid" && mode != "duplicate" && mode != "swallow-error" && calls != 0 {
				t.Fatal("unauthorized input reached consumer")
			}
		})
	}
}

func TestReadFileAuthorityStopRequiresOriginalReturnAndPersistence(t *testing.T) {
	h := readFileHandoffFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.ctx, h.cancel, h.done = ctx, cancel, make(chan struct{})
	d := &readFileAuthorityState{active: map[i.ExecutionID]*readFileHandoff{h.execution: h}}
	h.issuer = d
	a := &ReadFileAuthority{data: func() *readFileAuthorityState { return d }}
	if err := a.WithReadFileInTx(context.Background(), f.NewTx(), h.intent(), func(context.Context) error { t.Fatal("public intent created a handoff"); return nil }); err == nil {
		t.Fatal("forged handoff accepted")
	}
	a.Stop()
	if ctx.Err() == nil || a.Joined() {
		t.Fatal("signal became retirement")
	}
	deadline, stop := context.WithCancel(context.Background())
	stop()
	if err := a.Drain(deadline); !errors.Is(err, context.Canceled) {
		t.Fatal("drain did not keep actual caller boundary")
	}
	h.retirement = f.NewFault(f.CommitUnknown, f.Unknown)
	close(h.done)
	if err := a.Drain(context.Background()); err == nil || a.Joined() {
		t.Fatal("returned call hid unresolved persistence")
	}
	if err := retireReadFileOwner(d, h); err != nil {
		t.Fatal(err)
	}
	if err := a.Drain(context.Background()); err != nil || !a.Joined() {
		t.Fatal("resolved owner did not retire")
	}
}

func TestReadFileOutcomeDefaultFormattingHidesFileBytes(t *testing.T) {
	h := readFileHandoffFixture(t)
	value := p.ReadFileResult{Path: h.arguments.Arguments.Path, Offset: 0, BytesRead: 6, EOF: true, Encoding: "utf8", Data: "secret"}
	out := ReadFileOutcome{data: func() readFileObservation {
		return readFileObservation{record: h.record, terminal: &readFileTerminal{Outcome: "success"}, result: value}
	}}
	got, ok := out.Result()
	if !ok || got != value {
		t.Fatal("explicit result missing")
	}
	if text := fmt.Sprintf("%+v", out); strings.Contains(text, "secret") || strings.Contains(text, value.Path) {
		t.Fatal("default log exposed file bytes")
	}
	raw, err := out.MarshalJSON()
	if err != nil || strings.Contains(string(raw), "secret") || strings.Contains(string(raw), value.Path) {
		t.Fatal("default JSON exposed file bytes")
	}
}
