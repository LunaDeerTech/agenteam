package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// These controls use explicit SQL/Store outcomes and candidate codec material.
// They prove repository identity and original-call ownership, not a committed
// RoundInput or a PostgreSQL chain; independent PG validation must supply that chain.
func roundModelRequestCandidateControl(t *testing.T) (c.NextRoundModelRequest, c.NextRoundModelRequestInput) {
	t.Helper()
	facts, _ := runtimeModelCandidate(t)
	sf := facts.Snapshot.Fields()
	input := sf.Context.Input().Fields()
	project, agent, execution := input.Request.Launch.ProjectID, input.Request.Launch.AgentID, input.Request.ExecutionID
	actor, err := i.NewAgentRun(project, agent, execution)
	if err != nil {
		t.Fatal(err)
	}
	model := input.Model.Clone()
	model.Snapshot.Capabilities.ToolCalls = true
	tool := mc.Tool{Name: "controlled_tool", Description: "controlled tool", InputSchema: json.RawMessage(`{"type":"object"}`)}
	text := func(role, value string) mc.Message {
		return mc.Message{Role: role, Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: value}}}}
	}
	parts := c.NextRoundModelRequestInput{
		ProjectID: project, AgentID: agent, ExecutionID: execution,
		RoundInputID: newTestID[c.Round](t), RoundInputDigest: c.TriggerInputDigest([]byte("controlled successor input")),
		SnapshotID: sf.ID, SnapshotDigest: facts.Snapshot.Digest(), ProjectionDigest: c.TriggerInputDigest([]byte("controlled projection")),
		CallID: newTestID[mc.Call](t), Actor: actor, Model: model,
		Messages: []mc.Message{text("system", "original system"), text("user", "original user"),
			{Role: "assistant", Parts: []mc.MessagePart{{ToolCall: &mc.ToolCallPart{ID: "original_call", Name: tool.Name, Arguments: json.RawMessage(`{}`)}}}},
			{Role: "tool", Parts: []mc.MessagePart{{ToolResult: &mc.ToolResultPart{CallID: "original_call", Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: "original result"}}}}}}},
			text("user", "fixed catalog")}, Tools: []mc.Tool{tool},
	}
	v, err := c.NewNextRoundModelRequest(context.Background(), parts)
	if err != nil {
		t.Fatal("controlled codec candidate", err)
	}
	return v, parts
}

type roundModelRecordStore struct {
	Store
	row []any
	err error
}

func (s *roundModelRecordStore) QueryRow(context.Context, string, ...any) postgres.Row {
	values := append([]any(nil), s.row...)
	if len(values) > 9 {
		values[9] = bytes.Clone(values[9].([]byte))
	}
	return launchScan{values: values, err: s.err}
}
func roundModelRecordRow(v c.NextRoundModelRequest, at f.Instant) []any {
	q := v.Fields()
	raw := v.CanonicalBytes()
	return []any{q.ProjectID.String(), q.AgentID.String(), q.SnapshotID.String(), string(q.RoundInputDigest), string(q.SnapshotDigest), string(q.ProjectionDigest), q.CallID.String(), int64(q.SchemaVersion), string(v.InputDigest()), raw, string(v.Digest()), int64(len(raw)), at.Time()}
}

func TestExecutionRoundModelRequestStoredIdentityAndObservation(t *testing.T) {
	v, parts := roundModelRequestCandidateControl(t)
	q := v.Fields()
	at, _ := f.NewInstant(time.Now())
	s := &roundModelRecordStore{row: roundModelRecordRow(v, at)}
	stored, err := loadRoundModelRequest(context.Background(), s, q.ExecutionID, q.RoundInputID)
	if err != nil || stored == nil || stored.request.Digest() != v.Digest() || stored.at != at {
		t.Fatal("complete stored candidate", err)
	}
	if !bytes.Equal(s.row[9].([]byte), v.CanonicalBytes()) {
		t.Fatal("SQL source aliased loan cleanup")
	}
	for name, alter := range map[string]func([]any){
		"project":         func(r []any) { r[0] = newTestID[i.Project](t).String() },
		"agent":           func(r []any) { r[1] = newTestID[i.Agent](t).String() },
		"snapshot":        func(r []any) { r[2] = newTestID[c.Snapshot](t).String() },
		"input":           func(r []any) { r[3] = string(c.TriggerInputDigest([]byte("other input"))) },
		"projection":      func(r []any) { r[5] = string(c.TriggerInputDigest([]byte("other projection"))) },
		"call":            func(r []any) { r[6] = newTestID[mc.Call](t).String() },
		"schema":          func(r []any) { r[7] = int64(2) },
		"semantic-digest": func(r []any) { r[8] = string(c.TriggerInputDigest([]byte("other semantic"))) },
		"noncanonical": func(r []any) {
			r[9] = append([]byte(" "), r[9].([]byte)...)
			r[10] = string(c.TriggerInputDigest(r[9].([]byte)))
			r[11] = int64(len(r[9].([]byte)))
		},
		"request-digest": func(r []any) { r[10] = string(c.TriggerInputDigest([]byte("other request"))) },
		"length":         func(r []any) { r[11] = r[11].(int64) - 1 },
	} {
		t.Run(name, func(t *testing.T) {
			x := &roundModelRecordStore{row: roundModelRecordRow(v, at)}
			alter(x.row)
			got, err := loadRoundModelRequest(context.Background(), x, q.ExecutionID, q.RoundInputID)
			if err == nil || got != nil {
				t.Fatal("changed persisted identity accepted")
			}
		})
	}
	original := &roundModelRequestUnknown{round: q.RoundInputID, call: q.CallID, candidate: v}
	if !sameRoundModelRequestObservation(original, v) {
		t.Fatal("original observation rejected")
	}
	parts.Messages[1].Parts[0].Text.Text = "changed request but same logical identity"
	changed, err := c.NewNextRoundModelRequest(context.Background(), parts)
	if err != nil {
		t.Fatal(err)
	}
	if sameRoundModelRequestObservation(original, changed) {
		t.Fatal("whole candidate was not bound")
	}
	original.call = newTestID[mc.Call](t)
	if sameRoundModelRequestObservation(original, v) {
		t.Fatal("changed logical call resolved original Unknown")
	}
}

type roundModelOutcomeStore struct {
	*readFileAuthorityControl
	entered, release chan struct{}
	jobCalls         int
	status           c.Status
}

func (s *roundModelOutcomeStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.launchStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *roundModelOutcomeStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if strings.Contains(query, "FROM agenteam_execution.round_model_requests") {
		return launchScan{}
	}
	r := s.readFileAuthorityControl.QueryRow(ctx, query, args...)
	if s.status != "" && strings.Contains(query, "FROM agenteam_execution.executions WHERE") {
		v := r.(launchScan)
		v.values[3] = string(s.status)
		return v
	}
	return r
}
func (s *roundModelOutcomeStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	if cause.Details().JobType == "execution.round-model-request.prepare" {
		s.jobCalls++
		close(s.entered)
		<-s.release
		// Simulate an original transaction result whose callback/publication
		// cannot be proved. The owner must not turn it into another Prepare.
		return f.UnknownResult(newTestID[f.TransactionAttempt](s.t), cause)
	}
	return s.launchStore.WithinTx(ctx, cause, fn)
}
func roundModelBinderControl(t *testing.T) (*roundModelOutcomeStore, *RoundModelRequestBinder) {
	t.Helper()
	x := newReadFileAuthorityControl(t)
	s := &roundModelOutcomeStore{readFileAuthorityControl: x, entered: make(chan struct{}), release: make(chan struct{})}
	a := &Authority{state: &authorityState{store: s}}
	b, err := NewRoundModelRequestBinder(s, a, x)
	if err != nil {
		t.Fatal(err)
	}
	return s, b
}

func TestExecutionRoundModelRequestTransactionAndCurrentGates(t *testing.T) {
	s, b := roundModelBinderControl(t)
	request := s.input.Fields().Request
	e, next, call := request.ExecutionID, newTestID[c.Round](t), newTestID[mc.Call](t)
	scope := &executionRecord{summary: c.Summary{ID: e, ProjectID: request.Launch.ProjectID, AgentID: request.Launch.AgentID}}
	locks := roundInputLocks(scope.summary.ProjectID, scope.summary.AgentID, e)
	if _, err := NewRoundModelRequestBinder(s, &Authority{state: &authorityState{store: s.launchStore}}, s); err == nil {
		t.Fatal("foreign authority Store accepted")
	}
	if _, _, err := b.requestInTx(context.Background(), f.NewTx(), scope, next, call, locks, true); err == nil {
		t.Fatal("foreign transaction accepted")
	}
	for index, name := range []string{"project", "schedule", "agent", "execution"} {
		t.Run("missing-"+name, func(t *testing.T) {
			cause, _ := f.NewRecoveryCause("controlled.round-model", e.String(), next.String())
			result := s.launchStore.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
				partial := append([]f.LockRequest(nil), locks[:index]...)
				partial = append(partial, locks[index+1:]...)
				if err := s.AcquireAll(ctx, tx, partial); err != nil {
					return err
				}
				if _, _, err := b.requestInTx(ctx, tx, scope, next, call, partial, true); err == nil {
					t.Fatal("self-reported partial locks accepted")
				}
				return nil
			})
			if result.State() != f.Committed {
				t.Fatal("controlled gate transaction", result.State())
			}
		})
	}
	for _, mode := range []string{"not-running", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "not-running" {
				s.status = c.Waiting
			} else {
				s.status = ""
				s.cancelled = true
			}
			cause, _ := f.NewRecoveryCause("controlled.round-model", e.String(), next.String())
			result := s.launchStore.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
				if err := s.AcquireAll(ctx, tx, locks); err != nil {
					return err
				}
				_, _, err := b.requestInTx(ctx, tx, scope, next, call, locks, true)
				var known *f.Fault
				if !errors.As(err, &known) || known.Code != f.ConfirmationStale {
					t.Fatal("current gate did not reject before history", err)
				}
				return nil
			})
			if result.State() != f.Committed {
				t.Fatal("controlled current gate")
			}
		})
	}
}

func TestExecutionRoundModelRequestUnknownAndStopWaitForOriginalReturn(t *testing.T) {
	s, b := roundModelBinderControl(t)
	e, next := s.input.Fields().Request.ExecutionID, newTestID[c.Round](t)
	returned := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-s.release:
		default:
			close(s.release)
		}
		b.Stop()
		<-returned
	})
	done := make(chan error, 1)
	go func() {
		defer close(returned)
		value, err := b.Prepare(context.Background(), e, next)
		if value.Validate() == nil {
			done <- errors.New("Unknown published a request")
			return
		}
		done <- err
	}()
	<-s.entered
	b.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := b.Drain(ctx); !errors.Is(err, context.DeadlineExceeded) || b.Joined() {
		t.Fatal("Stop/Drain skipped original transaction return", err)
	}
	select {
	case <-done:
		t.Fatal("returned before original Store")
	default:
	}
	close(s.release)
	err := <-done
	if _, ok := UnknownAttempt(err); !ok {
		t.Fatal("actual Unknown lost", err)
	}
	if b.Joined() {
		t.Fatal("Unknown was silently retired")
	}
	if _, err = b.Prepare(context.Background(), e, next); err == nil || s.jobCalls != 1 {
		t.Fatal("Unknown created a second write")
	}
	value, found, err := b.Lookup(context.Background(), e, next)
	if err != nil || found || value.Validate() == nil {
		t.Fatal("absence became a stored request", err)
	}
	if b.Joined() || len(b.pending) != 1 {
		t.Fatal("absence was treated as rollback")
	}
	if _, _, err := b.Lookup(context.Background(), e, newTestID[c.Round](t)); err == nil {
		t.Fatal("stopped owner accepted a different key")
	}
	if err := b.Drain(context.Background()); err == nil {
		t.Fatal("unresolved Unknown joined")
	}
}

func TestExecutionRoundModelRequestReadWaitsForActualCommit(t *testing.T) {
	for _, mode := range []string{"committed", "unknown", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			s := &roundProjectionReturnStore{launchStore: &launchStore{t: t, rows: map[string][]any{}}, entered: make(chan struct{}), release: make(chan struct{}), unknown: mode == "unknown"}
			b := &RoundModelRequestBinder{store: s}
			e, next := newTestID[i.Execution](t), newTestID[c.Round](t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			returned := make(chan struct{})
			t.Cleanup(func() {
				select {
				case <-s.release:
				default:
					close(s.release)
				}
				<-returned
			})
			calls := 0
			go func() {
				defer close(returned)
				done <- b.readTx(ctx, "lookup", e, next, []f.LockRequest{executionLock(e)}, func(context.Context, f.Tx) error { calls++; return nil })
			}()
			<-s.entered
			if calls != 1 {
				t.Fatal("original callback missing")
			}
			if mode == "cancelled" {
				cancel()
			}
			select {
			case <-done:
				t.Fatal("callback substituted original return")
			default:
			}
			close(s.release)
			err := <-done
			if mode == "committed" && err != nil || mode != "committed" && err == nil {
				t.Fatal("actual read outcome lost", err)
			}
			if mode == "unknown" {
				if _, ok := UnknownAttempt(err); !ok {
					t.Fatal("original read Unknown identity lost")
				}
			}
			if mode == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal("late cancellation published read", err)
			}
		})
	}
}
