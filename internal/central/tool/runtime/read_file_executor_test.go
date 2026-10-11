package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	runnerhttp "github.com/LunaDeerTech/agenteam/internal/central/runner/http"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func TestReadFileSuccessCandidateSurvivesMetadataFailure(t *testing.T) {
	for _, mode := range []string{"dependency", "metadata-invalid-argument", "cancelled", "deadline", "schema-mismatch"} {
		t.Run(mode, func(t *testing.T) {
			v, s := schemaAdapterFixture(t)
			definition := builtin.SkillInstallDefinition()
			definition.OutputSchema = tc.ReadFileDefinition().OutputSchema
			var err error
			s.versions[1], err = tc.CanonicalDefinition(s.ctx, definition)
			if err != nil {
				t.Fatal(err)
			}
			h := readFileHandoffFixture(t)
			file := p.ReadFileResult{Path: h.arguments.Arguments.Path, Offset: 0, BytesRead: 64, EOF: true, Encoding: "utf8", Data: strings.Repeat("x", 64)}
			ref, payload, err := encodeReadFileStoredResult(h.record.ID, h.attempt, file, h.arguments.Arguments)
			if err != nil {
				t.Fatal(err)
			}
			defer clear(payload)
			before := bytes.Clone(payload)
			defer clear(before)
			original := readFileTerminal{Format: 1, Operation: h.record.ID, Attempt: h.attempt, Request: h.request, Outcome: "success", Attempted: true, Result: &ref}
			ctx := s.ctx
			switch mode {
			case "dependency":
				s.readError = fail(f.DependencyUnavailable)
			case "metadata-invalid-argument":
				s.readError = fail(f.InvalidArgument)
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(s.ctx)
				s.ctx, s.readHook = ctx, cancel
				defer cancel()
			case "deadline":
				s.readError = context.DeadlineExceeded
			case "schema-mismatch":
				definition.OutputSchema = []byte(`{"const":"different-output"}`)
				s.versions[1], err = tc.CanonicalDefinition(s.ctx, definition)
				if err != nil {
					t.Fatal(err)
				}
			}
			candidate, err := readFileOutputCandidate(ctx, s.tx, v, s.ref, original, payload)
			if mode == "schema-mismatch" {
				if err != nil || candidate.Outcome != "unknown" || candidate.Code != "backend_contract_violation" || candidate.Result != nil {
					t.Fatal("actual schema mismatch was not distinguished")
				}
			} else if err == nil || candidate.Outcome != "" {
				t.Fatal("metadata/cancellation error became a Runner contract verdict")
			}
			if original.Outcome != "success" || original.Code != "" || original.Result != &ref || !bytes.Equal(before, payload) {
				t.Fatal("unsuccessful publication destroyed the original result")
			}
			// A fresh caller and the same historical schema permit an explicit
			// retry of the original candidate; no Runner dispatch is involved.
			s.ctx, s.readError, s.readHook = context.Background(), nil, nil
			definition.OutputSchema = tc.ReadFileDefinition().OutputSchema
			s.versions[1], err = tc.CanonicalDefinition(s.ctx, definition)
			if err != nil {
				t.Fatal(err)
			}
			candidate, err = readFileOutputCandidate(s.ctx, s.tx, v, s.ref, original, payload)
			if err != nil || !sameReadFileTerminal(candidate, original) {
				t.Fatal("recovery lost the original successful candidate", err)
			}
			got, err := decodeReadFileStoredResult(*candidate.Result, payload, h.record.ID, h.attempt, h.arguments.Arguments)
			if err != nil || got != file {
				t.Fatal("recovered candidate lost complete file bytes")
			}
		})
	}
}

type readFileLookupPlanControl struct{ binding tc.ToolCallBinding }

func (p readFileLookupPlanControl) Binding() tc.ToolCallBinding  { return p.binding }
func (readFileLookupPlanControl) RequiredLocks() []f.LockRequest { return nil }

type readFileLookupDiscoveryControl struct{ binding tc.ToolCallBinding }

func (c readFileLookupDiscoveryControl) DiscoverReadFileCall(context.Context, i.ExecutionID) (tc.ReadFileCallPlan, error) {
	return readFileLookupPlanControl{c.binding}, nil
}
func (readFileLookupDiscoveryControl) WithReadFileCallInTx(context.Context, f.Tx, tc.ReadFileCallPlan, func(context.Context, tc.ToolCallBinding, []byte) error) error {
	panic("held Store must not enter the callback")
}

type readFileLookupStoreBarrier struct {
	*operationStoreControl
	entered chan context.Context
	release chan struct{}
	calls   atomic.Int32
}

func (s *readFileLookupStoreBarrier) WithinTx(ctx context.Context, _ f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
	s.calls.Add(1)
	s.entered <- ctx
	<-s.release
	return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
}

func TestReadFileLookupStopWaitsForOriginalStoreReturn(t *testing.T) {
	h := readFileHandoffFixture(t)
	store := &readFileLookupStoreBarrier{operationStoreControl: &operationStoreControl{t: t}, entered: make(chan context.Context, 1), release: make(chan struct{})}
	var once sync.Once
	release := func() { once.Do(func() { close(store.release) }) }
	defer release()
	d := &readFileAuthorityState{store: store, executions: readFileLookupDiscoveryControl{h.binding}, active: make(map[i.ExecutionID]*readFileHandoff)}
	a := &ReadFileAuthority{data: func() *readFileAuthorityState { return d }}
	e := &ReadFileExecutor{data: func() *readFileExecutorData { return &readFileExecutorData{authority: d} }}
	returned := make(chan error, 1)
	go func() { _, _, err := e.LookupReadFile(context.Background(), h.execution); returned <- err }()
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
	_, _, err = e.LookupReadFile(context.Background(), h.execution)
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

func TestReadFileTerminalAcceptsActualMessageCodec(t *testing.T) {
	h := readFileHandoffFixture(t)
	h.arguments.Arguments.Length = p.MaxReadFileBytes
	file := p.ReadFileResult{Path: h.arguments.Arguments.Path, Offset: 0, BytesRead: p.MaxReadFileBytes, EOF: true, Encoding: "utf8", Data: strings.Repeat("λ\r\n", p.MaxReadFileBytes/4)}
	body, err := p.EncodeReadFileResult(file)
	if err != nil {
		t.Fatal("original full result encoding failed")
	}
	defer clear(body)
	response := p.Response{RequestID: h.request, OperationID: p.ID(h.record.ID.String()), Outcome: p.Success, Payload: body}
	header := p.Header{
		ProtocolVersion: p.CurrentVersion(),
		MessageID:       p.ID(newOperationID[f.Request](t).String()),
		Timestamp:       p.Instant("2026-10-11T00:00:00Z"),
		RequestID:       response.RequestID,
		OperationID:     response.OperationID,
	}
	// Cross the real wire codec, whose validated JSON tree can reorder object
	// members. A directly constructed Response does not exercise this boundary.
	message, err := p.NewMessage(header, response)
	if err != nil {
		t.Fatal("original Response message construction failed")
	}
	wire, err := p.Encode(message)
	if err != nil {
		t.Fatal("original Response wire encoding failed")
	}
	defer clear(wire)
	decoded, err := p.Decode(wire)
	if err != nil || decoded.AllowedFrom(p.Runner) != nil {
		t.Fatal("original Response wire decoding failed")
	}
	observed, ok := decoded.Payload().(p.Response)
	if !ok {
		t.Fatal("original decoded Response missing")
	}
	defer clear(observed.Payload)
	actual, err := p.DecodeReadFileResult(observed.Payload)
	if err != nil || actual != file || bytes.Equal(observed.Payload, body) {
		t.Fatal("real protocol did not preserve the full result with reordered JSON")
	}
	terminal, payload := terminalReadFile(h, runnerhttp.ReadFileDispatchResult{Attempted: true, Terminal: &observed}, nil)
	defer clear(payload)
	if terminal.Outcome != "success" || terminal.Code != "" || !terminal.Attempted || terminal.Result == nil || terminal.validate(h.record) != nil {
		t.Fatal("valid protocol result was rejected after actual wire decoding")
	}
	stored, err := decodeReadFileStoredResult(*terminal.Result, payload, h.record.ID, h.attempt, h.arguments.Arguments)
	if err != nil || stored != file || !bytes.Equal(payload, body) || len(payload) <= 32768 {
		t.Fatal("complete protocol result did not retain its stable stored representation")
	}
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
