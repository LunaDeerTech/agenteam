package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	runnerhttp "github.com/LunaDeerTech/agenteam/internal/central/runner/http"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// These controls use explicit loan/Store substitutes. They do not stand in for
// the separate true Execution/PG/Claim/WSS first chain.
func runCommandHandoffFixture(t *testing.T) *runCommandHandoff {
	t.Helper()
	in := runCommandProfileFixture(t)
	actor, err := i.NewAgentRun(in.Binding.Project, in.Binding.Agent, in.Binding.Execution)
	if err != nil {
		t.Fatal(err)
	}
	b := in.Binding
	binding := tc.ToolCallBinding{Actor: actor, ProjectID: b.Project, AgentID: b.Agent, ExecutionID: b.Execution, RoundID: b.Round, SnapshotID: b.Snapshot, InputBindingID: b.InputBinding, LogicalCallID: b.LogicalCall, InvocationID: b.Invocation, CallID: b.Call, ModelVisibleName: b.ModelName, Spec: b.Spec, RunnerBinding: tc.RunCommandRegistration().Binding, Input: tc.ToolInputReference{PayloadID: b.Payload, SHA256: b.PayloadDigest, Bytes: b.PayloadBytes}, CanonicalArguments: b.ArgumentsDigest}
	raw, err := encodeRunCommandInput(in)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := runCommandFingerprint(in)
	if err != nil {
		t.Fatal(err)
	}
	attempt := newOperationID[tc.Attempt](t)
	return &runCommandHandoff{execution: b.Execution, binding: binding, record: runCommandOperationRecord{ID: newOperationID[tc.Operation](t), Input: in, InputDigest: digest(raw), Fingerprint: fingerprint, Phase: "running", Version: 2, Attempt: attempt}, arguments: tc.RunCommandToolArguments{MountID: in.Target.Mount, Arguments: p.RunCommandArguments{Mode: "argv", Program: "/bin/private-command", Args: []string{"private-argv-canary"}, CWD: "."}}, attempt: attempt, request: p.ID(newOperationID[f.Request](t).String())}
}
func runCommandTestCode(code p.Code) *p.Code { return &code }
func runCommandResponse(t *testing.T, h *runCommandHandoff, report p.RunCommandResult) p.Response {
	t.Helper()
	raw, err := p.EncodeRunCommandResult(report)
	if err != nil {
		t.Fatal(err)
	}
	return p.Response{RequestID: h.request, OperationID: p.ID(h.record.ID.String()), Outcome: p.Success, Payload: raw}
}

func TestRunCommandTerminalClassifiesActualCompletedCodec(t *testing.T) {
	stdout, stderr := []byte("output\n"), []byte("private-error-text\n")
	for _, test := range []struct {
		name          string
		mutate        func(*p.RunCommandResult)
		outcome, code string
	}{
		{"exit-zero", func(*p.RunCommandResult) {}, "success", ""},
		{"exit-23", func(r *p.RunCommandResult) { r.ExitCode = 23 }, "failure", "command_exit_nonzero"},
		{"signal", func(r *p.RunCommandResult) { r.ExitCode = -1; r.Signal = 15 }, "failure", "command_signaled"},
		{"cancelled", func(r *p.RunCommandResult) {
			r.Cancelled = true
			r.OutputComplete = false
			r.CompletionCode = runCommandTestCode(p.CancelledCode)
		}, "cancelled", string(p.CancelledCode)},
		{"timeout", func(r *p.RunCommandResult) {
			r.Cancelled = true
			r.TimedOut = true
			r.OutputComplete = false
			r.CompletionCode = runCommandTestCode(p.Timeout)
		}, "cancelled", string(p.Timeout)},
		{"local-io", func(r *p.RunCommandResult) {
			r.OutputComplete = false
			r.CompletionCode = runCommandTestCode(p.InternalError)
		}, "failure", string(p.InternalError)},
		{"declared-truncation", func(r *p.RunCommandResult) { r.StdoutTruncated = true }, "success", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := runCommandHandoffFixture(t)
			report := runCommandReport(stdout, stderr)
			test.mutate(&report)
			response := runCommandResponse(t, h, report)
			header := p.Header{ProtocolVersion: p.CurrentVersion(), MessageID: p.ID(newOperationID[f.Request](t).String()), Timestamp: p.Instant("2026-10-12T00:00:00Z"), RequestID: response.RequestID, OperationID: response.OperationID}
			msg, err := p.NewMessage(header, response)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := p.Encode(msg)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := p.Decode(wire)
			if err != nil || decoded.AllowedFrom(p.Runner) != nil {
				t.Fatal("actual response codec", err)
			}
			received, ok := decoded.Payload().(p.Response)
			if !ok {
				t.Fatal("not a response")
			}
			terminal, body, out, errout := terminalRunCommand(h, runnerhttp.CommandDispatchResult{Attempted: true, Terminal: &received, Stdout: stdout, Stderr: stderr}, nil)
			if terminal.Outcome != test.outcome || terminal.Code != test.code || !terminal.ReportAvailable || terminal.validate(h.record) != nil {
				t.Fatal("known invocation classification lost", terminal.Outcome, terminal.Code)
			}
			got, err := decodeRunCommandStoredResult(*terminal.Result, body, out, errout, h.record.ID, h.attempt, h.request)
			expected, _ := p.EncodeRunCommandResult(report)
			actual, _ := p.EncodeRunCommandResult(got)
			if err != nil || !bytes.Equal(expected, actual) || !bytes.Equal(out, stdout) || !bytes.Equal(errout, stderr) {
				t.Fatal("known report/streams lost", err)
			}
			out[0] = '!'
			errout[0] = '!'
			if stdout[0] == '!' || stderr[0] == '!' {
				t.Fatal("borrowed stream was retained without copy")
			}
			raw, err := encode(terminal)
			if err != nil || !bytes.Contains(raw, []byte(`"report_available":true`)) || bytes.Contains(raw, stderr) {
				t.Fatal("terminal must contain availability/ref only")
			}
		})
	}
}

func TestRunCommandTerminalRejectsUntrustedOrMissingReport(t *testing.T) {
	h := runCommandHandoffFixture(t)
	stdout, stderr := []byte("out"), []byte("err")
	report := runCommandReport(stdout, stderr)
	for _, name := range []string{"wrong-rid", "wrong-operation", "invalid-body", "wrong-count", "missing-stream", "invalid-utf8", "wire-error", "missing-terminal", "explicit-unknown", "not-dispatched", "prestart-failure"} {
		t.Run(name, func(t *testing.T) {
			response := runCommandResponse(t, h, report)
			result := runnerhttp.CommandDispatchResult{Attempted: true, Terminal: &response, Stdout: bytes.Clone(stdout), Stderr: bytes.Clone(stderr)}
			var err error
			want, code, attempted := "unknown", "backend_contract_violation", true
			switch name {
			case "wrong-rid":
				response.RequestID = p.ID(newOperationID[f.Request](t).String())
			case "wrong-operation":
				response.OperationID = p.ID(newOperationID[tc.Operation](t).String())
			case "invalid-body":
				response.Payload = []byte(`{}`)
			case "wrong-count":
				bad := report
				bad.StdoutBytes = "1"
				response = runCommandResponse(t, h, bad)
			case "missing-stream":
				result.Stdout = nil
			case "invalid-utf8":
				result.Stdout[0] = 0xff
			case "wire-error":
				err = errors.New("controlled transport failure")
			case "missing-terminal":
				result.Terminal = nil
				code = ""
			case "explicit-unknown":
				response.Outcome = p.Unknown
				response.Payload = nil
				message := p.UnknownMessage
				response.SafeMessage = &message
				code = ""
			case "not-dispatched":
				result = runnerhttp.CommandDispatchResult{}
				err = context.Canceled
				want, code, attempted = "failure", "not_dispatched", false
			case "prestart-failure":
				response.Outcome = p.Failure
				response.Payload = nil
				c := p.InternalError
				message := c.SafeMessage()
				response.Code = &c
				response.SafeMessage = &message
				result.Stdout, result.Stderr = nil, nil
				want, code = "failure", string(c)
			}
			terminal, body, out, errout := terminalRunCommand(h, result, err)
			if terminal.Outcome != want || terminal.Code != code || terminal.Attempted != attempted || terminal.ReportAvailable || terminal.Result != nil || body != nil || out != nil || errout != nil || terminal.validate(h.record) != nil {
				t.Fatal("untrusted output became a known complete report", terminal.Outcome, terminal.Code)
			}
		})
	}
}

func TestRunCommandCandidatePreservesKnownErrorAcrossMetadataFailure(t *testing.T) {
	for _, mode := range []string{"dependency", "cancelled", "schema-mismatch"} {
		t.Run(mode, func(t *testing.T) {
			v, s := schemaAdapterFixture(t)
			definition := builtin.SkillInstallDefinition()
			definition.OutputSchema = tc.RunCommandDefinition().OutputSchema
			var err error
			s.versions[1], err = tc.CanonicalDefinition(s.ctx, definition)
			if err != nil {
				t.Fatal(err)
			}
			h := runCommandHandoffFixture(t)
			stdout, stderr := []byte("out"), []byte("private-error-canary")
			report := runCommandReport(stdout, stderr)
			report.ExitCode = 23
			response := runCommandResponse(t, h, report)
			original, body, out, errout := terminalRunCommand(h, runnerhttp.CommandDispatchResult{Attempted: true, Terminal: &response, Stdout: stdout, Stderr: stderr}, nil)
			before := bytes.Clone(body)
			ctx := s.ctx
			switch mode {
			case "dependency":
				s.readError = fail(f.DependencyUnavailable)
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				s.ctx, s.readHook = ctx, cancel
				defer cancel()
			case "schema-mismatch":
				definition.OutputSchema = []byte(`{"const":"different"}`)
				s.versions[1], err = tc.CanonicalDefinition(s.ctx, definition)
				if err != nil {
					t.Fatal(err)
				}
			}
			candidate, err := runCommandOutputCandidate(ctx, s.tx, v, s.ref, original, body, out, errout)
			if mode == "schema-mismatch" {
				if err != nil || candidate.Outcome != "unknown" || candidate.ReportAvailable || candidate.Result != nil {
					t.Fatal("actual output mismatch not separated", err)
				}
			} else if err == nil || candidate.Outcome != "" {
				t.Fatal("metadata error became protocol verdict")
			}
			if original.Outcome != "failure" || !original.ReportAvailable || original.Result == nil || !bytes.Equal(body, before) || !bytes.Equal(out, stdout) || !bytes.Equal(errout, stderr) {
				t.Fatal("uncommitted observation destroyed original known error")
			}
			s.ctx, s.readHook, s.readError = context.Background(), nil, nil
			definition.OutputSchema = tc.RunCommandDefinition().OutputSchema
			s.versions[1], err = tc.CanonicalDefinition(s.ctx, definition)
			if err != nil {
				t.Fatal(err)
			}
			candidate, err = runCommandOutputCandidate(s.ctx, s.tx, v, s.ref, original, body, out, errout)
			if err != nil || !sameRunCommandTerminal(candidate, original) {
				t.Fatal("same original error report failed historical schema", err)
			}
		})
	}
}

type runCommandLookupPlanControl struct{ binding tc.ToolCallBinding }

func (p runCommandLookupPlanControl) Binding() tc.ToolCallBinding  { return p.binding }
func (runCommandLookupPlanControl) RequiredLocks() []f.LockRequest { return nil }

type runCommandLookupDiscoveryControl struct{ binding tc.ToolCallBinding }

func (c runCommandLookupDiscoveryControl) DiscoverRunCommandCall(context.Context, i.ExecutionID) (tc.RunCommandCallPlan, error) {
	return runCommandLookupPlanControl{c.binding}, nil
}
func (runCommandLookupDiscoveryControl) WithRunCommandCallInTx(context.Context, f.Tx, tc.RunCommandCallPlan, func(context.Context, tc.ToolCallBinding, []byte) error) error {
	panic("held Store must not enter the callback")
}

type runCommandLookupStoreBarrier struct {
	*operationStoreControl
	entered chan context.Context
	release chan struct{}
	calls   atomic.Int32
}

func (s *runCommandLookupStoreBarrier) WithinTx(ctx context.Context, _ f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
	s.calls.Add(1)
	s.entered <- ctx
	<-s.release
	return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
}

func (runCommandLookupDiscoveryControl) RequireRunCommandAdmissionInTx(context.Context, f.Tx, tc.RunCommandCallPlan) error {
	panic("Lookup must not invoke dispatch admission")
}

func TestRunCommandLookupStopWaitsForOriginalStoreReturn(t *testing.T) {
	h := runCommandHandoffFixture(t)
	store := &runCommandLookupStoreBarrier{operationStoreControl: &operationStoreControl{t: t}, entered: make(chan context.Context, 1), release: make(chan struct{})}
	var once sync.Once
	release := func() { once.Do(func() { close(store.release) }) }
	defer release()
	d := &runCommandAuthorityState{store: store, executions: runCommandLookupDiscoveryControl{h.binding}, active: make(map[i.ExecutionID]*runCommandHandoff)}
	a := &RunCommandAuthority{data: func() *runCommandAuthorityState { return d }}
	e := &RunCommandExecutor{data: func() *runCommandExecutorData { return &runCommandExecutorData{authority: d} }}
	returned := make(chan error, 1)
	go func() { _, _, err := e.LookupRunCommand(context.Background(), h.execution); returned <- err }()
	var original context.Context
	select {
	case original = <-store.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("public Lookup did not reach original readonly Store")
	}
	a.Stop()
	if original.Err() == nil || a.Joined() {
		t.Fatal("Lookup cancellation was mistaken for actual return")
	}
	short, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	err := a.Drain(short)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) || a.Joined() {
		t.Fatal("Drain passed while original Lookup Store was held")
	}
	_, _, err = e.LookupRunCommand(context.Background(), h.execution)
	schemaFault(t, err, f.ShuttingDown)
	if store.calls.Load() != 1 {
		t.Fatal("stopped Lookup entered another transaction")
	}
	release()
	select {
	case err := <-returned:
		if err == nil {
			t.Fatal("failed original read published an observation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("original Lookup did not return after Store release")
	}
	if err := a.Drain(context.Background()); err != nil || !a.Joined() {
		t.Fatal("actual Lookup return failed to retire")
	}
}

type runCommandLoanControl struct {
	binding tc.ToolCallBinding
	raw     []byte
	mode    string
}

func (c *runCommandLoanControl) DiscoverRunCommandCall(context.Context, i.ExecutionID) (tc.RunCommandCallPlan, error) {
	return nil, fail(f.DependencyUnbound)
}
func (c *runCommandLoanControl) WithRunCommandCallInTx(ctx context.Context, _ f.Tx, _ tc.RunCommandCallPlan, consume func(context.Context, tc.ToolCallBinding, []byte) error) error {
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

func (*runCommandLoanControl) RequireRunCommandAdmissionInTx(context.Context, f.Tx, tc.RunCommandCallPlan) error {
	return fail(f.Forbidden)
}

func TestRunCommandConsumerBindsOriginalBorrowedArguments(t *testing.T) {
	for _, mode := range []string{"valid", "wrong-binding", "substituted-bytes", "omit", "duplicate", "swallow-error"} {
		t.Run(mode, func(t *testing.T) {
			h := runCommandHandoffFixture(t)
			raw := []byte(fmt.Sprintf(`{"mount_id":%q,"mode":"argv","program":"/bin/private-command","args":["private-argv-canary"],"cwd":"."}`, h.arguments.MountID.String()))
			canonical, err := cursor.CanonicalJSON(raw)
			if err != nil {
				t.Fatal(err)
			}
			h.binding.Input.SHA256 = digest(raw)
			h.binding.Input.Bytes = f.Progress(len(raw))
			h.binding.CanonicalArguments = digest(canonical)
			// Exercise first ownership: decoder scratch is cleared on return,
			// while the executor must retain its independent typed copy.
			if mode == "valid" {
				clearRunCommandArguments(h)
				h.arguments = tc.RunCommandToolArguments{}
			}
			port := &runCommandLoanControl{binding: h.binding, raw: raw, mode: mode}
			d := &runCommandAuthorityState{executions: port}
			calls := 0
			err = consumeRunCommandInput(context.Background(), f.NewTx(), d, h, func(context.Context, []byte) error {
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
				if len(h.arguments.Arguments.Args) != 1 || h.arguments.Arguments.Args[0] != "private-argv-canary" {
					t.Fatal("borrowed decode cleanup erased retained intent")
				}
				clearRunCommandArguments(h)
			} else if err == nil {
				t.Fatal("incomplete/foreign consumer boundary accepted")
			}
			if mode != "valid" && mode != "duplicate" && mode != "swallow-error" && calls != 0 {
				t.Fatal("unauthorized input reached consumer")
			}
		})
	}
}

func TestRunCommandAuthorityRejectsMissingAndWrongRID(t *testing.T) {
	h := runCommandHandoffFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.ctx, h.cancel, h.done = ctx, cancel, make(chan struct{})
	d := &runCommandAuthorityState{active: map[i.ExecutionID]*runCommandHandoff{h.execution: h}}
	h.issuer = d
	h.executor = &runCommandExecutorData{authority: d}
	a := &RunCommandAuthority{data: func() *runCommandAuthorityState { return d }}
	owned := context.WithValue(ctx, runCommandHandoffKey{}, h)
	for _, mode := range []string{"public", "created-with-rid", "running-without-rid", "running-other-rid"} {
		t.Run(mode, func(t *testing.T) {
			h.admission = "running"
			request := h.request
			callctx := owned
			switch mode {
			case "public":
				callctx = ctx
			case "created-with-rid":
				h.admission = "created"
			case "running-without-rid":
				request = ""
			case "running-other-rid":
				request = p.ID(newOperationID[f.Request](t).String())
			}
			err := a.WithCommandInTx(callctx, f.NewTx(), h.intent(), request, func(context.Context) error { t.Fatal("unbound admission reached consumer"); return nil })
			schemaFault(t, err, f.Forbidden)
		})
	}
	a.Stop()
	if ctx.Err() == nil || a.Joined() {
		t.Fatal("cancel became actual return")
	}
	h.retirement = f.NewFault(f.CommitUnknown, f.Unknown)
	close(h.done)
	if err := a.Drain(context.Background()); err == nil || a.Joined() {
		t.Fatal("returned DBUnknown owner falsely retired")
	}
}

func TestRunCommandOutcomeDefaultsRedactAndExplicitResultCopies(t *testing.T) {
	h := runCommandHandoffFixture(t)
	stdout, stderr := []byte("stdout-private-canary"), []byte("stderr-private-canary")
	report := runCommandReport(stdout, stderr)
	report.CompletionCode = runCommandTestCode(p.InternalError)
	report.OutputComplete = false
	response := runCommandResponse(t, h, report)
	terminal, _, out, errout := terminalRunCommand(h, runnerhttp.CommandDispatchResult{Attempted: true, Terminal: &response, Stdout: stdout, Stderr: stderr}, nil)
	h.record.Phase, h.record.Version = "failed", 3
	value := RunCommandOutcome{data: func() runCommandObservation {
		return runCommandObservation{record: h.record, terminal: &terminal, result: report, stdout: out, stderr: errout}
	}}
	got, gotout, goterr, ok := value.Result()
	if !ok || !bytes.Equal(gotout, stdout) || !bytes.Equal(goterr, stderr) || got.CompletionCode == nil {
		t.Fatal("explicit complete report lost")
	}
	gotout[0] = '!'
	goterr[0] = '!'
	*got.CompletionCode = p.CancelledCode
	again, aout, aerr, _ := value.Result()
	if *again.CompletionCode != p.InternalError || !bytes.Equal(aout, stdout) || !bytes.Equal(aerr, stderr) {
		t.Fatal("caller mutated stored observation")
	}
	raw, err := value.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{fmt.Sprintf("%+v", value), string(raw), value.LogValue().String()} {
		if strings.Contains(text, "canary") || strings.Contains(text, string(h.request)) {
			t.Fatal("default observation exposed data")
		}
	}
	if phase, outcome, attempted := value.Status(); phase != "failed" || outcome != "failure" || !attempted || value.Code() != string(p.InternalError) {
		t.Fatal("public known error status lost")
	}
}
