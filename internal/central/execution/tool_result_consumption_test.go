package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// This controls reader timing and metadata only. Durable constraints, current
// owner checks and bytes from a real Operation are covered by the PG chain.
type toolResultReaderControl struct {
	discover func(context.Context, i.ExecutionID) (tc.ReadFileResultPlan, error)
}

func (v *toolResultReaderControl) DiscoverReadFileResult(ctx context.Context, e i.ExecutionID) (tc.ReadFileResultPlan, error) {
	return v.discover(ctx, e)
}
func (*toolResultReaderControl) WithReadFileResultInTx(context.Context, f.Tx, tc.ReadFileResultPlan, func(context.Context, tc.ReadFileResultBinding, []byte) error) error {
	return fault(f.InvalidState)
}

func toolResultCandidateControl(t *testing.T) (tc.ReadFileResultBinding, *toolResultConsumption) {
	t.Helper()
	x := newReadFileAuthorityControl(t)
	call, err := toolBinding(&x.stored)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.EncodeReadFileResult(p.ReadFileResult{Path: "private-file-canary.txt", BytesRead: 4, EOF: true, Encoding: "utf8", Data: "body"})
	if err != nil {
		t.Fatal(err)
	}
	digest := c.TriggerInputDigest([]byte("controlled-result-identity"))
	b := tc.ReadFileResultBinding{Call: call, OperationID: newTestID[tc.Operation](t), AttemptID: newTestID[tc.Attempt](t),
		RequestID: p.ID(newTestID[struct{}](t).String()), InputDigest: digest, Fingerprint: digest, TerminalDigest: digest,
		Outcome: "success", Attempted: true, ResultDigest: c.TriggerInputDigest(raw), ResultBytes: f.Progress(len(raw))}
	projection, err := agentloop.ProjectReadFileToolResult(context.Background(), b, raw)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := toolResultBindingJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	round, err := f.ParseID[c.Round](call.RoundID)
	if err != nil {
		t.Fatal(err)
	}
	v := &toolResultConsumption{binding: bound, message: projection.CanonicalBytes(), receipt: c.ToolResultConsumptionReceipt{
		ExecutionID: call.ExecutionID, RoundID: round, CallID: call.CallID, OperationID: b.OperationID, AttemptID: b.AttemptID,
		Version: 4, TranscriptThrough: 3, MessageDigest: projection.Digest(), ResultDigest: b.ResultDigest, CreatedAt: x.stored.toolPending.at,
	}}
	if v.receipt.Validate() != nil {
		t.Fatal("controlled receipt invalid")
	}
	return b, v
}

func TestExecutionToolResultObservationBindsWholeCandidate(t *testing.T) {
	_, original := toolResultCandidateControl(t)
	for name, alter := range map[string]func(*toolResultConsumption){
		"call":    func(v *toolResultConsumption) { v.receipt.CallID += "-other" },
		"op":      func(v *toolResultConsumption) { v.receipt.OperationID = newTestID[tc.Operation](t) },
		"attempt": func(v *toolResultConsumption) { v.receipt.AttemptID = newTestID[tc.Attempt](t) },
		"version": func(v *toolResultConsumption) { v.receipt.Version++ },
		"time": func(v *toolResultConsumption) {
			v.receipt.CreatedAt, _ = f.NewInstant(v.receipt.CreatedAt.Time().Add(time.Microsecond))
		},
		"binding": func(v *toolResultConsumption) { v.binding[len(v.binding)-2] ^= 1 },
		"message": func(v *toolResultConsumption) { v.message[len(v.message)-2] ^= 1 },
		"digest":  func(v *toolResultConsumption) { v.receipt.ResultDigest = c.TriggerInputDigest([]byte("other")) },
	} {
		t.Run(name, func(t *testing.T) {
			copy := *original
			copy.binding, copy.message = bytes.Clone(original.binding), bytes.Clone(original.message)
			if !sameToolResultConsumption(&copy, original) {
				t.Fatal("exact original copy rejected")
			}
			alter(&copy)
			if sameToolResultConsumption(&copy, original) {
				t.Fatal("different candidate accepted")
			}
		})
	}
}

func TestExecutionToolResultUnknownCannotBecomeRetry(t *testing.T) {
	b, candidate := toolResultCandidateControl(t)
	reads := 0
	reader := &toolResultReaderControl{discover: func(context.Context, i.ExecutionID) (tc.ReadFileResultPlan, error) {
		reads++
		return nil, fault(f.DependencyUnavailable)
	}}
	owner, err := NewToolResultConsumer(&ToolCallDriver{driver: &DirectTextDriver{state: &directTextState{}}}, reader)
	if err != nil {
		t.Fatal(err)
	}
	cause, _ := f.NewJobCause("execution.tool-result.consume", b.Call.ExecutionID.String(), b.AttemptID.String())
	attempt := newTestID[f.TransactionAttempt](t)
	original := commitError(f.UnknownResult(attempt, cause))
	owner.pending[b.Call.ExecutionID] = &toolResultUnknown{candidate: candidate, err: original}
	if _, err = owner.Consume(context.Background(), b.Call.ExecutionID); err != original || reads != 0 {
		t.Fatal("unknown retried a physical consume", err)
	}
	if _, err = owner.ResolveUnknown(context.Background(), b.Call.ExecutionID); err == nil || reads != 1 || owner.pending[b.Call.ExecutionID].err != original {
		t.Fatal("failed observation lost original uncertainty")
	}
	actual, ok := UnknownAttempt(owner.pending[b.Call.ExecutionID].err)
	if !ok || actual.AttemptID() != attempt {
		t.Fatal("original transaction identity lost")
	}
	if err = owner.Drain(context.Background()); err != nil || !owner.Joined() {
		t.Fatal("completed observation did not join", err)
	}
}

func TestExecutionToolResultStopWaitsForActualReaderReturn(t *testing.T) {
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	reader := &toolResultReaderControl{discover: func(ctx context.Context, _ i.ExecutionID) (tc.ReadFileResultPlan, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return nil, ctx.Err()
	}}
	owner, err := NewToolResultConsumer(&ToolCallDriver{driver: &DirectTextDriver{state: &directTextState{}}}, reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		owner.Stop()
		select {
		case <-release:
		default:
			close(release)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := owner.Drain(ctx); err != nil {
			t.Error("original reader cleanup", err)
		}
	})
	e := newTestID[i.Execution](t)
	done := make(chan error, 1)
	go func() { _, err := owner.Consume(context.Background(), e); done <- err }()
	<-entered
	if _, _, err = owner.Lookup(context.Background(), e); err == nil {
		t.Fatal("concurrent call entered the same execution")
	}
	owner.Stop()
	<-cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = owner.Drain(ctx); !errors.Is(err, context.Canceled) || owner.Joined() {
		t.Fatal("cancel request falsely joined original reader", err)
	}
	close(release)
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("original cancelled call outcome changed", err)
	}
	if err = owner.Drain(context.Background()); err != nil || !owner.Joined() {
		t.Fatal("actual returned call remained active", err)
	}
	if _, err = owner.Consume(context.Background(), e); err == nil {
		t.Fatal("stopped consumer reopened")
	}
}

func TestExecutionToolResultPersistenceIsExplicitAndComplete(t *testing.T) {
	b, candidate := toolResultCandidateControl(t)
	var explicit map[string]any
	if json.Unmarshal(candidate.binding, &explicit) != nil || len(explicit) != 30 {
		t.Fatal("explicit binding profile changed")
	}
	for key, expected := range map[string]string{"request_id": string(b.RequestID), "operation_id": b.OperationID.String(), "attempt_id": b.AttemptID.String(), "invocation_id": b.Call.InvocationID.String(), "call_id": b.Call.CallID, "payload_digest": string(b.Call.Input.SHA256), "terminal_digest": string(b.TerminalDigest)} {
		if explicit[key] != expected {
			t.Fatal("original identity omitted", key)
		}
	}
	changed := b
	changed.RequestID = p.ID(newTestID[struct{}](t).String())
	other, err := toolResultBindingJSON(changed)
	if err != nil || bytes.Equal(candidate.binding, other) {
		t.Fatal("different wire request reused binding", err)
	}
	changed = b
	changed.Outcome = "unknown"
	if _, err = toolResultBindingJSON(changed); err == nil {
		t.Fatal("unknown result minted consumption binding")
	}
	for _, value := range []any{b, candidate.receipt, &ToolResultConsumer{}} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{string(encoded), fmt.Sprintf("%+v", value)} {
			if strings.Contains(text, "private-file-canary") || strings.Contains(text, "body") {
				t.Fatal("default representation leaked result")
			}
		}
	}
}
