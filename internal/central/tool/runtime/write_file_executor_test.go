package runtime

import (
	"bytes"
	"context"
	"encoding/base64"
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

func TestWriteFileSuccessCandidateSurvivesMetadataFailure(t *testing.T) {
	for _, mode := range []string{"dependency", "metadata-invalid-argument", "cancelled", "deadline", "schema-mismatch"} {
		t.Run(mode, func(t *testing.T) {
			v, s := schemaAdapterFixture(t)
			definition := builtin.SkillInstallDefinition()
			definition.OutputSchema = tc.WriteFileDefinition().OutputSchema
			var err error
			s.versions[1], err = tc.CanonicalDefinition(s.ctx, definition)
			if err != nil {
				t.Fatal(err)
			}
			h := writeFileHandoffFixture(t)
			file := p.WriteFileResult{Path: h.arguments.Arguments.Path, State: "committed", BytesWritten: len(h.arguments.Arguments.Data), Durable: true}
			ref, payload, err := encodeWriteFileStoredResult(h.record.ID, h.attempt, file, h.arguments.Arguments)
			if err != nil {
				t.Fatal(err)
			}
			defer clear(payload)
			before := bytes.Clone(payload)
			defer clear(before)
			original := writeFileTerminal{Format: 1, Operation: h.record.ID, Attempt: h.attempt, Request: h.request, Outcome: "success", Attempted: true, Result: &ref}
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
			candidate, err := writeFileOutputCandidate(ctx, s.tx, v, s.ref, original, payload)
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
			definition.OutputSchema = tc.WriteFileDefinition().OutputSchema
			s.versions[1], err = tc.CanonicalDefinition(s.ctx, definition)
			if err != nil {
				t.Fatal(err)
			}
			candidate, err = writeFileOutputCandidate(s.ctx, s.tx, v, s.ref, original, payload)
			if err != nil || !sameWriteFileTerminal(candidate, original) {
				t.Fatal("recovery lost the original successful candidate", err)
			}
			got, err := decodeWriteFileStoredResult(*candidate.Result, payload, h.record.ID, h.attempt, h.arguments.Arguments)
			if err != nil || got != file {
				t.Fatal("recovered candidate lost complete file bytes")
			}
		})
	}
}

type writeFileLookupPlanControl struct{ binding tc.ToolCallBinding }

func (p writeFileLookupPlanControl) Binding() tc.ToolCallBinding  { return p.binding }
func (writeFileLookupPlanControl) RequiredLocks() []f.LockRequest { return nil }

type writeFileLookupDiscoveryControl struct{ binding tc.ToolCallBinding }

func (c writeFileLookupDiscoveryControl) DiscoverWriteFileCall(context.Context, i.ExecutionID) (tc.WriteFileCallPlan, error) {
	return writeFileLookupPlanControl{c.binding}, nil
}
func (writeFileLookupDiscoveryControl) WithWriteFileCallInTx(context.Context, f.Tx, tc.WriteFileCallPlan, func(context.Context, tc.ToolCallBinding, []byte) error) error {
	panic("held Store must not enter the callback")
}

type writeFileLookupStoreBarrier struct {
	*operationStoreControl
	entered chan context.Context
	release chan struct{}
	calls   atomic.Int32
}

func (s *writeFileLookupStoreBarrier) WithinTx(ctx context.Context, _ f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
	s.calls.Add(1)
	s.entered <- ctx
	<-s.release
	return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
}

func TestWriteFileLookupStopWaitsForOriginalStoreReturn(t *testing.T) {
	h := writeFileHandoffFixture(t)
	store := &writeFileLookupStoreBarrier{operationStoreControl: &operationStoreControl{t: t}, entered: make(chan context.Context, 1), release: make(chan struct{})}
	var once sync.Once
	release := func() { once.Do(func() { close(store.release) }) }
	defer release()
	d := &writeFileAuthorityState{store: store, executions: writeFileLookupDiscoveryControl{h.binding}, active: make(map[i.ExecutionID]*writeFileHandoff)}
	a := &WriteFileAuthority{data: func() *writeFileAuthorityState { return d }}
	e := &WriteFileExecutor{data: func() *writeFileExecutorData { return &writeFileExecutorData{authority: d} }}
	returned := make(chan error, 1)
	go func() { _, _, err := e.LookupWriteFile(context.Background(), h.execution); returned <- err }()
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
	_, _, err = e.LookupWriteFile(context.Background(), h.execution)
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
func writeFileHandoffFixture(t *testing.T) *writeFileHandoff {
	t.Helper()
	in := writeFileProfileFixture(t)
	actor, err := i.NewAgentRun(in.Binding.Project, in.Binding.Agent, in.Binding.Execution)
	if err != nil {
		t.Fatal(err)
	}
	b := in.Binding
	binding := tc.ToolCallBinding{Actor: actor, ProjectID: b.Project, AgentID: b.Agent, ExecutionID: b.Execution, RoundID: b.Round, SnapshotID: b.Snapshot, InputBindingID: b.InputBinding, LogicalCallID: b.LogicalCall, InvocationID: b.Invocation, CallID: b.Call, ModelVisibleName: b.ModelName, Spec: b.Spec, RunnerBinding: tc.WriteFileRegistration().Binding, Input: tc.ToolInputReference{PayloadID: b.Payload, SHA256: b.PayloadDigest, Bytes: b.PayloadBytes}, CanonicalArguments: b.ArgumentsDigest}
	encoded, _ := encodeWriteFileInput(in)
	fingerprint, _ := writeFileFingerprint(in)
	attempt := newOperationID[tc.Attempt](t)
	return &writeFileHandoff{execution: b.Execution, binding: binding, record: writeFileOperationRecord{ID: newOperationID[tc.Operation](t), Input: in, InputDigest: digest(encoded), Fingerprint: fingerprint, Phase: "running", Version: 2, Attempt: attempt}, arguments: tc.WriteFileToolArguments{MountID: in.Target.Mount, Arguments: p.WriteFileArguments{Path: "private-writer-canary.bin", Data: []byte("protected-write-value"), Overwrite: true}}, attempt: attempt, request: p.ID(newOperationID[f.Request](t).String())}
}

func TestWriteFileTerminalAcceptsActualMessageCodec(t *testing.T) {
	h := writeFileHandoffFixture(t)
	for _, code := range []*p.Code{nil, writeFileTestCode(p.InternalError), writeFileTestCode(p.CancelledCode), writeFileTestCode(p.Timeout)} {
		for _, durable := range []bool{false, true} {
			if code == nil && !durable {
				continue
			}
			value := p.WriteFileResult{Path: h.arguments.Arguments.Path, State: "committed", BytesWritten: len(h.arguments.Arguments.Data), Durable: durable, CompletionCode: code}
			body, err := p.EncodeWriteFileResult(value)
			if err != nil {
				t.Fatal(err)
			}
			response := p.Response{RequestID: h.request, OperationID: p.ID(h.record.ID.String()), Outcome: p.Success, Payload: body}
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
				t.Fatal("actual message codec rejected")
			}
			observed, ok := decoded.Payload().(p.Response)
			if !ok {
				t.Fatal("response absent")
			}
			terminal, raw := terminalWriteFile(h, runnerhttp.WriteFileDispatchResult{Attempted: true, Terminal: &observed}, nil)
			if terminal.Outcome != "success" || terminal.Result == nil || terminal.validate(h.record) != nil {
				t.Fatal("actual codec result demoted")
			}
			got, err := decodeWriteFileStoredResult(*terminal.Result, raw, h.record.ID, h.attempt, h.arguments.Arguments)
			gotRaw, _ := p.EncodeWriteFileResult(got)
			if err != nil || !bytes.Equal(raw, body) || !bytes.Equal(raw, gotRaw) {
				t.Fatal("durability or completion details changed")
			}
		}
	}
}
func writeFileTestCode(code p.Code) *p.Code { return &code }

func TestWriteFileTerminalPreservesActualAttemptAndIdentity(t *testing.T) {
	h := writeFileHandoffFixture(t)
	file := p.WriteFileResult{Path: h.arguments.Arguments.Path, State: "committed", BytesWritten: len(h.arguments.Arguments.Data), Durable: true}
	body, err := p.EncodeWriteFileResult(file)
	if err != nil {
		t.Fatal(err)
	}
	response := p.Response{RequestID: h.request, OperationID: p.ID(h.record.ID.String()), Outcome: p.Success, Payload: body}
	for _, tc := range []struct {
		name          string
		result        runnerhttp.WriteFileDispatchResult
		err           error
		outcome, code string
		attempted     bool
	}{
		{"success", runnerhttp.WriteFileDispatchResult{Attempted: true, Terminal: &response}, nil, "success", "", true},
		{"known-not-sent", runnerhttp.WriteFileDispatchResult{}, context.Canceled, "failure", "not_dispatched", false},
		{"sent-without-terminal", runnerhttp.WriteFileDispatchResult{Attempted: true}, context.Canceled, "unknown", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			terminal, payload := terminalWriteFile(h, tc.result, tc.err)
			if terminal.Outcome != tc.outcome || terminal.Code != tc.code || terminal.Attempted != tc.attempted || terminal.validate(h.record) != nil {
				t.Fatal("actual attempt/terminal classification lost")
			}
			if tc.outcome == "success" {
				if terminal.Result == nil {
					t.Fatal("complete result reference missing")
				}
				got, err := decodeWriteFileStoredResult(*terminal.Result, payload, h.record.ID, h.attempt, h.arguments.Arguments)
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
			terminal, body := terminalWriteFile(h, runnerhttp.WriteFileDispatchResult{Attempted: true, Terminal: &changed}, nil)
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
	terminal, _ := terminalWriteFile(h, runnerhttp.WriteFileDispatchResult{Attempted: true, Terminal: &cancelled}, nil)
	if terminal.Outcome != "cancelled" || terminal.validate(h.record) != nil {
		t.Fatal("actual cancelled terminal lost")
	}
}

type writeFileLoanControl struct {
	binding tc.ToolCallBinding
	raw     []byte
	mode    string
}

func (c *writeFileLoanControl) DiscoverWriteFileCall(context.Context, i.ExecutionID) (tc.WriteFileCallPlan, error) {
	return nil, fail(f.DependencyUnbound)
}
func (c *writeFileLoanControl) WithWriteFileCallInTx(ctx context.Context, _ f.Tx, _ tc.WriteFileCallPlan, consume func(context.Context, tc.ToolCallBinding, []byte) error) error {
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

func TestWriteFileConsumerBindsOriginalBorrowedArguments(t *testing.T) {
	for _, mode := range []string{"valid", "wrong-binding", "substituted-bytes", "omit", "duplicate", "swallow-error"} {
		t.Run(mode, func(t *testing.T) {
			h := writeFileHandoffFixture(t)
			raw := []byte(fmt.Sprintf(`{"mount_id":%q,"path":"private-writer-canary.bin","data_base64":%q,"overwrite":true}`, h.arguments.MountID.String(), base64.StdEncoding.EncodeToString(h.arguments.Arguments.Data)))
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
				clearWriteFileArguments(h)
				h.arguments = tc.WriteFileToolArguments{}
			}
			port := &writeFileLoanControl{binding: h.binding, raw: raw, mode: mode}
			d := &writeFileAuthorityState{executions: port}
			calls := 0
			err = consumeWriteFileInput(context.Background(), f.NewTx(), d, h, func(context.Context, []byte) error {
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
				if string(h.arguments.Arguments.Data) != "protected-write-value" {
					t.Fatal("borrowed decode cleanup erased retained intent")
				}
				clearWriteFileArguments(h)
			} else if err == nil {
				t.Fatal("incomplete/foreign consumer boundary accepted")
			}
			if mode != "valid" && mode != "duplicate" && mode != "swallow-error" && calls != 0 {
				t.Fatal("unauthorized input reached consumer")
			}
		})
	}
}

func TestWriteFileAuthorityRejectsMissingAndWrongRID(t *testing.T) {
	h := writeFileHandoffFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.ctx, h.cancel, h.done = ctx, cancel, make(chan struct{})
	d := &writeFileAuthorityState{active: map[i.ExecutionID]*writeFileHandoff{h.execution: h}}
	h.issuer = d
	h.executor = &writeFileExecutorData{authority: d}
	a := &WriteFileAuthority{data: func() *writeFileAuthorityState { return d }}
	owned := context.WithValue(ctx, writeFileHandoffKey{}, h)
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
			err := a.WithWriteFileInTx(callctx, f.NewTx(), h.intent(), request, func(context.Context) error { t.Fatal("unbound admission reached consumer"); return nil })
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

func TestWriteFileOutcomeDefaultFormattingHidesFileBytes(t *testing.T) {
	h := writeFileHandoffFixture(t)
	value := p.WriteFileResult{Path: h.arguments.Arguments.Path, State: "committed", BytesWritten: len(h.arguments.Arguments.Data), Durable: false, CompletionCode: writeFileTestCode(p.InternalError)}
	out := WriteFileOutcome{data: func() writeFileObservation {
		return writeFileObservation{record: h.record, terminal: &writeFileTerminal{Outcome: "success"}, result: value}
	}}
	got, ok := out.Result()
	if !ok || got.Path != value.Path || got.State != value.State || got.Durable != value.Durable || got.CompletionCode == nil || *got.CompletionCode != *value.CompletionCode {
		t.Fatal("explicit result missing")
	}
	*got.CompletionCode = p.Timeout
	again, _ := out.Result()
	if *again.CompletionCode != p.InternalError {
		t.Fatal("caller mutated durable observation")
	}
	if text := fmt.Sprintf("%+v", out); strings.Contains(text, "secret") || strings.Contains(text, value.Path) {
		t.Fatal("default log exposed file bytes")
	}
	raw, err := out.MarshalJSON()
	if err != nil || strings.Contains(string(raw), "secret") || strings.Contains(string(raw), value.Path) {
		t.Fatal("default JSON exposed file bytes")
	}
}
