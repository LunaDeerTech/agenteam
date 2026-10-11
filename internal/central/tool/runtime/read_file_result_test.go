package runtime

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// These controls exercise the actual Tool reader/codec with controlled Store
// and Execution ports. They do not stand in for the real PG authorization or
// consumption transaction, which are covered by the separate composite chain.
type resultReaderStore struct {
	*operationStoreControl
	record    readFileOperationRecord
	terminal  readFileTerminal
	payload   []byte
	current   bool
	txs       int
	acquires  int
	badDigest bool
}

func (s *resultReaderStore) WithinTx(ctx context.Context, c f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.txs++
	return s.operationStoreControl.WithinTx(ctx, c, fn)
}
func (s *resultReaderStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.operationStoreControl.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *resultReaderStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.acquires++
	return s.operationStoreControl.AcquireAll(ctx, tx, locks)
}
func (s *resultReaderStore) QueryRow(_ context.Context, query string, _ ...any) postgres.Row {
	if !s.current {
		s.t.Fatal("result SQL preceded the original current-input callback")
	}
	b := s.record.Input.Binding
	switch {
	case strings.HasPrefix(query, "SELECT operation_id,"):
		input, err := encodeReadFileInput(s.record.Input)
		if err != nil {
			s.t.Fatal(err)
		}
		return controlRow{values: []any{s.record.ID.String(), b.Project.String(), b.Agent.String(), b.Execution.String(), b.LogicalCall.String(), b.Invocation.String(), b.Call, b.Spec.ToolID.String(), int64(b.Spec.SpecRevision), input, s.record.InputDigest, "", "", s.record.Fingerprint, s.record.Phase, int64(s.record.Version), s.record.Attempt.String(), readFileOperationKind}}
	case strings.HasPrefix(query, "SELECT o.terminal_data,"):
		raw, err := encode(s.terminal)
		if err != nil {
			s.t.Fatal(err)
		}
		return controlRow{values: []any{raw, "success", bytes.Clone(raw), string(s.terminal.Request), "succeeded", true, int64(1), int64(1)}}
	case strings.HasPrefix(query, "SELECT execution_id,project_id,agent_id,body_sha256,"):
		ref := *s.terminal.Result
		if s.badDigest {
			ref.SHA256 = digest([]byte("different-result"))
		}
		return controlRow{values: []any{b.Execution.String(), b.Project.String(), b.Agent.String(), ref.SHA256, int64(ref.Bytes), bytes.Clone(s.payload)}}
	default:
		s.t.Fatal("unexpected result-reader SQL")
		return controlRow{err: fail(f.InvalidState)}
	}
}

type resultReaderExecution struct {
	readFileLoanControl
	store *resultReaderStore
	deny  bool
}

func (e *resultReaderExecution) DiscoverReadFileCall(context.Context, i.ExecutionID) (tc.ReadFileCallPlan, error) {
	return readFileLookupPlanControl{e.binding}, nil
}
func (e *resultReaderExecution) WithReadFileCallInTx(ctx context.Context, tx f.Tx, plan tc.ReadFileCallPlan, consume func(context.Context, tc.ToolCallBinding, []byte) error) error {
	if e.deny {
		return fail(f.Forbidden)
	}
	e.store.current = true
	defer func() { e.store.current = false }()
	return e.readFileLoanControl.WithReadFileCallInTx(ctx, tx, plan, consume)
}

func resultReaderFixture(t *testing.T) (*ReadFileResultReader, *resultReaderStore, *resultReaderExecution, p.ReadFileResult) {
	t.Helper()
	h := readFileHandoffFixture(t)
	raw := []byte(fmt.Sprintf(`{"mount_id":%q,"path":"private-reader-canary.txt","offset":"0","length":64,"encoding":"utf8"}`, h.arguments.MountID.String()))
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	h.binding.Input.Bytes, h.binding.Input.SHA256, h.binding.CanonicalArguments = f.Progress(len(raw)), digest(raw), digest(canonical)
	h.record.Input.Binding = readFileBindingProjection(h.binding)
	input, err := encodeReadFileInput(h.record.Input)
	if err != nil {
		t.Fatal(err)
	}
	h.record.InputDigest = digest(input)
	h.record.Fingerprint, err = readFileFingerprint(h.record.Input)
	if err != nil {
		t.Fatal(err)
	}
	h.record.Phase, h.record.Version = "succeeded", 3
	file := p.ReadFileResult{Path: h.arguments.Arguments.Path, Offset: 0, BytesRead: 64, EOF: true, Encoding: "utf8", Data: strings.Repeat("x", 64)}
	ref, payload, err := encodeReadFileStoredResult(h.record.ID, h.attempt, file, h.arguments.Arguments)
	if err != nil {
		t.Fatal(err)
	}
	s := &resultReaderStore{operationStoreControl: &operationStoreControl{t: t, rows: make(map[string][]any)}, record: h.record,
		terminal: readFileTerminal{Format: 1, Operation: h.record.ID, Attempt: h.attempt, Request: h.request, Outcome: "success", Attempted: true, Result: &ref}, payload: payload}
	e := &resultReaderExecution{readFileLoanControl: readFileLoanControl{binding: h.binding, raw: raw}, store: s}
	r, err := NewReadFileResultReader(s, e)
	if err != nil {
		t.Fatal(err)
	}
	return r, s, e, file
}

func TestReadFileResultLoanIsSameTxCompleteAndCleared(t *testing.T) {
	r, s, e, file := resultReaderFixture(t)
	plan, err := r.DiscoverReadFileResult(context.Background(), e.binding.ExecutionID)
	if err != nil || plan.Binding().Validate() != nil {
		t.Fatal("actual result discovery failed", err)
	}
	cause, _ := inputCause(e.binding)
	var borrowed []byte
	calls := 0
	result := s.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return err
		}
		beforeTx, beforeAcquire := s.txs, s.acquires
		err := r.WithReadFileResultInTx(ctx, tx, plan, func(authorized context.Context, b tc.ReadFileResultBinding, raw []byte) error {
			calls++
			if authorized != ctx || !s.current || b.Validate() != nil || digest(raw) != b.ResultDigest || f.Progress(len(raw)) != b.ResultBytes {
				t.Fatal("result loan lost the current original transaction/binding")
			}
			value, err := p.DecodeReadFileResult(raw)
			if err != nil || value != file || !bytes.Equal(raw, s.payload) {
				t.Fatal("loan changed the durable result")
			}
			borrowed = raw
			return nil
		})
		if s.txs != beforeTx || s.acquires != beforeAcquire {
			t.Fatal("WithResult opened a transaction or acquired more locks")
		}
		return err
	})
	if result.State() != f.Committed || calls != 1 || len(borrowed) == 0 || !bytes.Equal(borrowed, make([]byte, len(borrowed))) {
		t.Fatal("result loan did not complete once and clear its original bytes")
	}
}

func TestReadFileResultLoanRejectsForeignStaleAndNonSuccess(t *testing.T) {
	for _, mode := range []string{"foreign-reader", "foreign-tx", "missing-locks", "denied", "result-digest", "failed", "callback-error", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			r, s, e, _ := resultReaderFixture(t)
			plan, err := r.DiscoverReadFileResult(context.Background(), e.binding.ExecutionID)
			if err != nil {
				t.Fatal(err)
			}
			cause, _ := inputCause(e.binding)
			var borrowed []byte
			calls := 0
			result := s.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
				if mode != "missing-locks" {
					if err := s.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
						return err
					}
				}
				switch mode {
				case "foreign-reader":
					r, err = NewReadFileResultReader(s, e)
					if err != nil {
						return err
					}
				case "foreign-tx":
					tx = f.NewTx()
				case "denied":
					e.deny = true
				case "result-digest":
					s.badDigest = true
				case "failed":
					s.record.Phase = "failed"
				}
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()
				return r.WithReadFileResultInTx(ctx, tx, plan, func(_ context.Context, _ tc.ReadFileResultBinding, raw []byte) error {
					calls++
					borrowed = raw
					if mode == "callback-error" {
						return fail(f.Forbidden)
					}
					if mode == "cancelled" {
						cancel()
					}
					return nil
				})
			})
			if mode == "callback-error" || mode == "cancelled" {
				if calls != 1 || len(borrowed) == 0 || !bytes.Equal(borrowed, make([]byte, len(borrowed))) {
					t.Fatal("failed/cancelled consumer retained the original borrowed bytes")
				}
			} else if calls != 0 {
				t.Fatal("foreign/stale/non-success result reached consumer")
			}
			if result.State() == f.Committed {
				t.Fatal("foreign/stale/non-success result reached a committed consumer")
			}
		})
	}
}
