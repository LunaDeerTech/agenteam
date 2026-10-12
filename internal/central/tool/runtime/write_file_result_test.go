package runtime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/jackc/pgx/v5"
)

// These controls exercise the real reader and stored-result codecs with explicit
// Store/history ports. Real SQL, grant revocation, and seq3 atomicity belong to
// the composite PG chain. There is deliberately no current-authority port here.
type writeResultStore struct {
	*operationStoreControl
	record        writeFileOperationRecord
	terminal      writeFileTerminal
	payload       []byte
	history       bool
	txs, acquires int
	corrupt       string
}

func (s *writeResultStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.txs++
	return s.operationStoreControl.WithinTx(ctx, cause, fn)
}
func (s *writeResultStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.operationStoreControl.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *writeResultStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.acquires++
	return s.operationStoreControl.AcquireAll(ctx, tx, locks)
}
func (s *writeResultStore) QueryRow(_ context.Context, q string, _ ...any) postgres.Row {
	if !s.history {
		s.t.Fatal("result SQL escaped the original historical loan")
	}
	b := s.record.Input.Binding
	switch {
	case strings.HasPrefix(q, "SELECT operation_id,"):
		raw, err := encodeWriteFileInput(s.record.Input)
		if err != nil {
			s.t.Fatal(err)
		}
		return controlRow{values: []any{s.record.ID.String(), b.Project.String(), b.Agent.String(), b.Execution.String(), b.LogicalCall.String(), b.Invocation.String(), b.Call, b.Spec.ToolID.String(), int64(b.Spec.SpecRevision), raw, s.record.InputDigest, "", "", s.record.Fingerprint, s.record.Phase, int64(s.record.Version), s.record.Attempt.String(), writeFileOperationKind}}
	case strings.HasPrefix(q, "SELECT o.terminal_data,"):
		raw, err := encode(s.terminal)
		if err != nil {
			s.t.Fatal(err)
		}
		attemptRaw := bytes.Clone(raw)
		request := string(s.terminal.Request)
		if s.corrupt == "request" {
			request = string(p.ID(newOperationID[f.Request](s.t).String()))
		}
		if s.corrupt == "attempt-terminal" {
			attemptRaw = append(attemptRaw, ' ')
		}
		_, phase, outcome := s.terminal.phases()
		return controlRow{values: []any{raw, outcome, attemptRaw, request, phase, s.corrupt != "not-retired", int64(1), int64(1)}}
	case strings.HasPrefix(q, "SELECT execution_id,project_id,agent_id,body_sha256,"):
		if s.corrupt == "missing-result" {
			return controlRow{err: pgx.ErrNoRows}
		}
		ref := *s.terminal.Result
		if s.corrupt == "result-digest" {
			ref.SHA256 = digest([]byte("different-receipt"))
		}
		return controlRow{values: []any{b.Execution.String(), b.Project.String(), b.Agent.String(), ref.SHA256, int64(ref.Bytes), bytes.Clone(s.payload)}}
	default:
		s.t.Fatal("unexpected write-result SQL")
		return controlRow{err: fail(f.InvalidState)}
	}
}

type committedWritePlanControl struct{ binding tc.ToolCallBinding }

func (p committedWritePlanControl) Binding() tc.ToolCallBinding  { return p.binding }
func (committedWritePlanControl) RequiredLocks() []f.LockRequest { return nil }

type committedWriteControl struct {
	store     *writeResultStore
	binding   tc.ToolCallBinding
	raw, loan []byte
	mode      string
}

func (c *committedWriteControl) DiscoverCommittedWriteFileCall(context.Context, i.ExecutionID) (tc.CommittedWriteFileCallPlan, error) {
	return committedWritePlanControl{c.binding}, nil
}
func (c *committedWriteControl) WithCommittedWriteFileCallInTx(ctx context.Context, tx f.Tx, _ tc.CommittedWriteFileCallPlan, consume func(context.Context, tc.ToolCallBinding, []byte) error) error {
	if _, err := c.store.InTx(tx); err != nil {
		return err
	}
	if c.mode == "omit" {
		return nil
	}
	c.store.history = true
	defer func() { c.store.history = false }()
	c.loan = bytes.Clone(c.raw)
	defer clear(c.loan)
	err := consume(ctx, c.binding, c.loan)
	switch c.mode {
	case "duplicate":
		return consume(ctx, c.binding, c.loan)
	case "swallow":
		return nil
	case "return-error":
		return fail(f.DependencyUnavailable)
	}
	return err
}

func writeResultFixture(t *testing.T, durable bool, code *p.Code) (*WriteFileResultReader, *writeResultStore, *committedWriteControl, p.WriteFileResult) {
	t.Helper()
	h := writeFileHandoffFixture(t)
	raw := []byte(fmt.Sprintf(`{"mount_id":%q,"path":%q,"data_base64":%q,"overwrite":true}`, h.arguments.MountID.String(), h.arguments.Arguments.Path, base64.StdEncoding.EncodeToString(h.arguments.Arguments.Data)))
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	h.binding.Input.Bytes, h.binding.Input.SHA256, h.binding.CanonicalArguments = f.Progress(len(raw)), digest(raw), digest(canonical)
	h.record.Input.Binding = writeFileBindingProjection(h.binding)
	encoded, err := encodeWriteFileInput(h.record.Input)
	if err != nil {
		t.Fatal(err)
	}
	h.record.InputDigest = digest(encoded)
	h.record.Fingerprint, err = writeFileFingerprint(h.record.Input)
	if err != nil {
		t.Fatal(err)
	}
	h.record.Phase, h.record.Version = "succeeded", 3
	receipt := p.WriteFileResult{Path: h.arguments.Arguments.Path, State: "committed", BytesWritten: len(h.arguments.Arguments.Data), Durable: durable, CompletionCode: code}
	ref, payload, err := encodeWriteFileStoredResult(h.record.ID, h.attempt, receipt, h.arguments.Arguments)
	if err != nil {
		t.Fatal(err)
	}
	s := &writeResultStore{operationStoreControl: &operationStoreControl{t: t, rows: make(map[string][]any)}, record: h.record, terminal: writeFileTerminal{Format: 1, Operation: h.record.ID, Attempt: h.attempt, Request: h.request, Outcome: "success", Attempted: true, Result: &ref}, payload: payload}
	e := &committedWriteControl{store: s, binding: h.binding, raw: raw}
	r, err := NewWriteFileResultReader(s, e)
	if err != nil {
		t.Fatal(err)
	}
	return r, s, e, receipt
}

func TestWriteFileResultLoanIsSameTxCompleteAndCleared(t *testing.T) {
	for _, name := range []string{"durable", "nondurable-late-io", "durable-late-cancel"} {
		t.Run(name, func(t *testing.T) {
			var code *p.Code
			if name == "nondurable-late-io" {
				code = writeFileTestCode(p.InternalError)
			}
			if name == "durable-late-cancel" {
				code = writeFileTestCode(p.CancelledCode)
			}
			r, s, e, receipt := writeResultFixture(t, name != "nondurable-late-io", code)
			plan, err := r.DiscoverWriteFileResult(context.Background(), e.binding.ExecutionID)
			if err != nil || plan == nil || plan.Binding().Validate() != nil {
				t.Fatal("historical result discovery failed", err)
			}
			locks := plan.RequiredLocks()
			opLock, err := f.AggregateLock(f.OperationAggregate, s.record.ID.String())
			if err != nil {
				t.Fatal(err)
			}
			operationIndex := -1
			for n, lock := range locks {
				if f.CompareLockKeys(lock.Key, opLock) == 0 && lock.Mode == f.Exclusive {
					operationIndex = n
				}
			}
			if operationIndex < 0 {
				t.Fatal("plan omitted original operation lock")
			}
			locks[operationIndex].Mode = f.Shared
			if plan.RequiredLocks()[operationIndex].Mode != f.Exclusive {
				t.Fatal("plan exposed mutable lock slice")
			}
			if tc.ReadFileResultBinding(plan.Binding()).Validate() == nil {
				t.Fatal("write publication accepted as read receipt")
			}
			wrongProfile := plan.Binding()
			wrongProfile.Call.RunnerBinding = tc.ReadFileRegistration().Binding
			if wrongProfile.Validate() == nil {
				t.Fatal("read profile accepted as write receipt")
			}
			// The controlled producer has no current-grant methods or dispatch owner.
			cause, _ := inputCause(e.binding)
			var borrowed []byte
			calls := 0
			result := s.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
				if err := s.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
					return err
				}
				beforeTx, beforeLocks := s.txs, s.acquires
				err := r.WithWriteFileResultInTx(ctx, tx, plan, func(authorized context.Context, b tc.WriteFileResultBinding, raw []byte) error {
					calls++
					borrowed = raw
					if authorized != ctx || !s.history || b.Validate() != nil || b.ResultDigest != digest(raw) || b.ResultBytes != f.Progress(len(raw)) {
						t.Fatal("loan lost original transaction or complete binding")
					}
					got, err := p.DecodeWriteFileResult(raw)
					if err != nil || got.Path != receipt.Path || got.State != receipt.State || got.BytesWritten != receipt.BytesWritten || got.Durable != receipt.Durable || (got.CompletionCode == nil) != (code == nil) || code != nil && *got.CompletionCode != *code || !bytes.Equal(raw, s.payload) {
						t.Fatal("known publication receipt changed")
					}
					if bytes.Contains(raw, []byte("protected-write-value")) || bytes.Contains(raw, []byte("data_base64")) {
						t.Fatal("receipt leaked original write data")
					}
					return nil
				})
				if s.txs != beforeTx || s.acquires != beforeLocks {
					t.Fatal("reader opened another transaction or added locks")
				}
				return err
			})
			if result.State() != f.Committed || calls != 1 || len(borrowed) == 0 || !bytes.Equal(borrowed, make([]byte, len(borrowed))) || !bytes.Equal(e.loan, make([]byte, len(e.loan))) {
				t.Fatal("loan did not finish once with both borrowed buffers cleared")
			}
			safe, _ := json.Marshal(plan.Binding())
			if string(safe) != `"tool_write_file_result_binding"` || strings.Contains(fmt.Sprintf("%+v", plan), receipt.Path) {
				t.Fatal("default result metadata exposed protected values")
			}
		})
	}
}

func TestWriteFileResultLoanRejectsForeignStaleAndNonPublication(t *testing.T) {
	for _, mode := range []string{"foreign-reader", "foreign-tx", "missing-locks", "changed-call", "read-profile", "result-digest", "not-retired", "request", "attempt-terminal", "missing-result", "wrong-path", "wrong-length", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			r, s, e, receipt := writeResultFixture(t, true, nil)
			plan, err := r.DiscoverWriteFileResult(context.Background(), e.binding.ExecutionID)
			if err != nil {
				t.Fatal(err)
			}
			cause, _ := inputCause(e.binding)
			calls := 0
			result := s.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
				if mode != "missing-locks" {
					if err := s.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
						return err
					}
				}
				switch mode {
				case "foreign-reader":
					r, err = NewWriteFileResultReader(s, e)
					if err != nil {
						return err
					}
				case "foreign-tx":
					tx = f.NewTx()
				case "changed-call":
					e.binding.CallID = "other-original-call"
				case "read-profile":
					e.binding.RunnerBinding = tc.ReadFileRegistration().Binding
				case "wrong-path", "wrong-length":
					if mode == "wrong-path" {
						receipt.Path = "different.bin"
					} else {
						receipt.BytesWritten++
					}
					s.payload, err = p.EncodeWriteFileResult(receipt)
					if err != nil {
						t.Fatal(err)
					}
					s.terminal.Result.SHA256 = digest(s.payload)
					s.terminal.Result.Bytes = f.Progress(len(s.payload))
				case "unknown":
					s.record.Phase = "failed"
					s.terminal.Outcome = "unknown"
					s.terminal.Result = nil
				default:
					s.corrupt = mode
				}
				return r.WithWriteFileResultInTx(ctx, tx, plan, func(context.Context, tc.WriteFileResultBinding, []byte) error { calls++; return nil })
			})
			if result.State() == f.Committed || calls != 0 {
				t.Fatal("foreign, changed, incomplete or non-publication receipt reached consumer")
			}
		})
	}
}

func TestWriteFileResultLoanWaitsForOriginalReturnAndClearsFailure(t *testing.T) {
	for _, mode := range []string{"omit", "duplicate", "swallow", "return-error", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			r, s, e, _ := writeResultFixture(t, true, nil)
			plan, err := r.DiscoverWriteFileResult(context.Background(), e.binding.ExecutionID)
			if err != nil {
				t.Fatal(err)
			}
			cause, _ := inputCause(e.binding)
			e.mode = mode
			calls := 0
			var receipt, decodedData []byte
			result := s.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
				if err := s.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
					return err
				}
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()
				return r.WithWriteFileResultInTx(ctx, tx, plan, func(_ context.Context, _ tc.WriteFileResultBinding, raw []byte) error {
					calls++
					receipt = raw
					if mode == "swallow" {
						return fail(f.Forbidden)
					}
					if mode == "cancelled" {
						cancel()
					}
					return nil
				})
			})
			expected := 1
			if mode == "omit" {
				expected = 0
			}
			if result.State() == f.Committed || calls != expected || !bytes.Equal(receipt, make([]byte, len(receipt))) || !bytes.Equal(e.loan, make([]byte, len(e.loan))) {
				t.Fatal("original port failure/cancel escaped or retained a receipt loan")
			}
			// Directly observe the private decoded-data loan; it is never part of the
			// public result API. The port may return an error after a successful callback.
			e.mode = "return-error"
			result = s.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
				return borrowCommittedWriteFileArguments(ctx, tx, e, committedWritePlanControl{e.binding}, e.binding, func(_ context.Context, v tc.WriteFileToolArguments) error { decodedData = v.Arguments.Data; return nil })
			})
			if result.State() == f.Committed || len(decodedData) == 0 || !bytes.Equal(decodedData, make([]byte, len(decodedData))) {
				t.Fatal("decoded private write bytes survived history-port return")
			}
		})
	}
	// Even after successful loan callbacks, failed discovery's original transaction
	// must return no public plan. Unknown is observation-only, never success.
	r, s, e, _ := writeResultFixture(t, true, nil)
	s.unknown = true
	s.attempt = newOperationID[f.TransactionAttempt](t)
	if plan, err := r.DiscoverWriteFileResult(context.Background(), e.binding.ExecutionID); err == nil || plan != nil {
		t.Fatal("Unknown original transaction published a discovered result")
	}
}
