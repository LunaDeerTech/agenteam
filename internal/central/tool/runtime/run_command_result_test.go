package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/jackc/pgx/v5"
)

// These controls drive the actual reader/codecs through explicit Store and
// historical-call ports. They do not prove actual SQL or grant revocation;
// those, the deferred 71 gate, and seq3 atomicity belong to the PG chain.
type commandResultStore struct {
	*operationStoreControl
	record                 runCommandOperationRecord
	terminal               runCommandTerminal
	report, stdout, stderr []byte
	history                bool
	txs, acquires          int
	corrupt                string
	loaded                 [][]byte
	scopes                 []context.Context
}

func (s *commandResultStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.txs++
	s.scopes = append(s.scopes, ctx)
	return s.operationStoreControl.WithinTx(ctx, cause, fn)
}
func (s *commandResultStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.operationStoreControl.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *commandResultStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.acquires++
	return s.operationStoreControl.AcquireAll(ctx, tx, locks)
}
func (s *commandResultStore) QueryRow(ctx context.Context, q string, _ ...any) postgres.Row {
	if !s.history {
		s.t.Fatal("result SQL escaped original historical loan")
	}
	if ctx != s.scopes[len(s.scopes)-1] {
		s.t.Fatal("SQL lost the actual original transaction context")
	}
	b := s.record.Input.Binding
	switch {
	case strings.HasPrefix(q, "SELECT operation_id,"):
		raw, err := encodeRunCommandInput(s.record.Input)
		if err != nil {
			s.t.Fatal(err)
		}
		return controlRow{values: []any{s.record.ID.String(), b.Project.String(), b.Agent.String(), b.Execution.String(), b.LogicalCall.String(), b.Invocation.String(), b.Call, b.Spec.ToolID.String(), int64(b.Spec.SpecRevision), raw, s.record.InputDigest, "", "", s.record.Fingerprint, s.record.Phase, int64(s.record.Version), s.record.Attempt.String(), runCommandOperationKind}}
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
	case strings.HasPrefix(q, "SELECT execution_id,project_id,agent_id,request_id,body_sha256,"):
		if s.corrupt == "missing-report" {
			return controlRow{err: pgx.ErrNoRows}
		}
		ref := *s.terminal.Result
		report, out, errout := bytes.Clone(s.report), bytes.Clone(s.stdout), bytes.Clone(s.stderr)
		s.loaded = append(s.loaded, report, out, errout)
		switch s.corrupt {
		case "report-digest":
			ref.SHA256 = digest([]byte("different-report"))
		case "stdout-digest":
			ref.StdoutSHA256 = digest([]byte("different-out"))
		case "stderr-digest":
			ref.StderrSHA256 = digest([]byte("different-err"))
		case "stdout-bytes":
			out[0] ^= 1
		case "stderr-bytes":
			errout[0] ^= 1
		case "stored-rid":
			ref.Request = p.ID(newOperationID[f.Request](s.t).String())
		}
		return controlRow{values: []any{b.Execution.String(), b.Project.String(), b.Agent.String(), string(ref.Request), ref.SHA256, int64(ref.Bytes), report, ref.StdoutSHA256, int64(ref.StdoutBytes), out, ref.StderrSHA256, int64(ref.StderrBytes), errout}}
	default:
		s.t.Fatal("unexpected historical command result SQL")
		return controlRow{err: fail(f.InvalidState)}
	}
}

type committedCommandPlanControl struct{ binding tc.ToolCallBinding }

func (p committedCommandPlanControl) Binding() tc.ToolCallBinding  { return p.binding }
func (committedCommandPlanControl) RequiredLocks() []f.LockRequest { return nil }

type committedCommandControl struct {
	store       *commandResultStore
	binding     tc.ToolCallBinding
	raw, loan   []byte
	mode        string
	discoverCtx context.Context
}

func (c *committedCommandControl) DiscoverCommittedRunCommandCall(ctx context.Context, _ i.ExecutionID) (tc.CommittedRunCommandCallPlan, error) {
	c.discoverCtx = ctx
	return committedCommandPlanControl{c.binding}, nil
}
func (c *committedCommandControl) WithCommittedRunCommandCallInTx(ctx context.Context, tx f.Tx, _ tc.CommittedRunCommandCallPlan, consume func(context.Context, tc.ToolCallBinding, []byte) error) error {
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

func commandResultFixture(t *testing.T, outcome string) (*RunCommandResultReader, *commandResultStore, *committedCommandControl, p.RunCommandResult) {
	t.Helper()
	h := runCommandHandoffFixture(t)
	raw, err := json.Marshal(struct {
		Mount   string   `json:"mount_id"`
		Mode    string   `json:"mode"`
		Program string   `json:"program"`
		Args    []string `json:"args"`
		CWD     string   `json:"cwd"`
	}{h.arguments.MountID.String(), h.arguments.Arguments.Mode, h.arguments.Arguments.Program, h.arguments.Arguments.Args, h.arguments.Arguments.CWD})
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	h.binding.Input.Bytes, h.binding.Input.SHA256, h.binding.CanonicalArguments = f.Progress(len(raw)), digest(raw), digest(canonical)
	h.record.Input.Binding = runCommandBindingProjection(h.binding)
	input, err := encodeRunCommandInput(h.record.Input)
	if err != nil {
		t.Fatal(err)
	}
	h.record.InputDigest = digest(input)
	h.record.Fingerprint, err = runCommandFingerprint(h.record.Input)
	if err != nil {
		t.Fatal(err)
	}
	out, errout := []byte("private-out\x00\"\\\n中文"), []byte("private-error\x00\t")
	report := runCommandReport(out, errout)
	wantCode := ""
	switch outcome {
	case "failure":
		report.ExitCode = 23
		wantCode = "command_exit_nonzero"
	case "cancelled":
		report.Cancelled = true
		report.OutputComplete = false
		report.CompletionCode = runCommandTestCode(p.CancelledCode)
		wantCode = string(p.CancelledCode)
	case "success":
	default:
		t.Fatal("invalid fixture outcome")
	}
	ref, payload, err := encodeRunCommandStoredResult(h.record.ID, h.attempt, h.request, report, out, errout)
	if err != nil {
		t.Fatal(err)
	}
	terminal := runCommandTerminal{Format: 1, Operation: h.record.ID, Attempt: h.attempt, Request: h.request, Outcome: outcome, Code: wantCode, Attempted: true, ReportAvailable: true, Result: &ref}
	h.record.Phase, _, _ = terminal.phases()
	h.record.Version = 3
	s := &commandResultStore{operationStoreControl: &operationStoreControl{t: t, rows: make(map[string][]any)}, record: h.record, terminal: terminal, report: payload, stdout: out, stderr: errout}
	e := &committedCommandControl{store: s, binding: h.binding, raw: raw}
	r, err := NewRunCommandResultReader(s, e)
	if err != nil {
		t.Fatal(err)
	}
	return r, s, e, report
}

func commandResultAssertCleared(t *testing.T, values ...[]byte) {
	t.Helper()
	for _, v := range values {
		if !bytes.Equal(v, make([]byte, len(v))) {
			t.Fatal("borrowed original bytes survived reader return")
		}
	}
}

func TestRunCommandResultLoanPreservesKnownReportAndStreams(t *testing.T) {
	for _, outcome := range []string{"success", "failure", "cancelled"} {
		t.Run(outcome, func(t *testing.T) {
			r, s, e, report := commandResultFixture(t, outcome)
			parent, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			plan, err := r.DiscoverRunCommandResult(parent, e.binding.ExecutionID)
			if err != nil || plan == nil || plan.Binding().Validate() != nil || plan.Binding().Outcome != outcome {
				t.Fatal("known historical report discovery failed", err)
			}
			if e.discoverCtx != parent || len(s.scopes) != 2 || s.scopes[0] == s.scopes[1] {
				t.Fatal("history discovery shortened caller or reused transaction context")
			}
			for _, scope := range s.scopes {
				d, ok := scope.Deadline()
				if !ok || d.After(time.Now().Add(2*time.Second)) || scope.Err() != context.Canceled {
					t.Fatal("short historical transaction was not bounded and retired")
				}
			}
			locks := plan.RequiredLocks()
			opKey, _ := f.AggregateLock(f.OperationAggregate, s.record.ID.String())
			index := -1
			for n, l := range locks {
				if f.CompareLockKeys(l.Key, opKey) == 0 && l.Mode == f.Exclusive {
					index = n
				}
			}
			if index < 0 {
				t.Fatal("original operation lock missing")
			}
			locks[index].Mode = f.Shared
			if plan.RequiredLocks()[index].Mode != f.Exclusive {
				t.Fatal("mutable plan locks escaped")
			}
			if tc.ReadFileResultBinding(plan.Binding()).Validate() == nil || tc.WriteFileResultBinding(plan.Binding()).Validate() == nil {
				t.Fatal("command report passed a different result profile")
			}
			cause, _ := inputCause(e.binding)
			calls := 0
			var borrowed []byte
			result := s.WithinTx(parent, cause, func(ctx context.Context, tx f.Tx) error {
				if err := s.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
					return err
				}
				beforeTx, beforeLocks := s.txs, s.acquires
				err := r.WithRunCommandResultInTx(ctx, tx, plan, func(authorized context.Context, b tc.RunCommandResultBinding, raw []byte) error {
					calls++
					borrowed = raw
					if authorized != ctx || !s.history || b.ResultDigest != digest(raw) || b.ResultBytes != f.Progress(len(raw)) {
						t.Fatal("loan lost original context or complete byte binding")
					}
					got, err := tc.DecodeRunCommandResultBundle(authorized, raw)
					if err != nil || !bytes.Equal(got.Stdout, s.stdout) || !bytes.Equal(got.Stderr, s.stderr) {
						t.Fatal("historical output changed", err)
					}
					defer clear(got.Stdout)
					defer clear(got.Stderr)
					a, _ := p.EncodeRunCommandResult(got.Report)
					want, _ := p.EncodeRunCommandResult(report)
					if !bytes.Equal(a, want) || b.Outcome != outcome || b.Code != s.terminal.Code {
						t.Fatal("known failure/cancellation report was normalized away")
					}
					return nil
				})
				if s.txs != beforeTx || s.acquires != beforeLocks {
					t.Fatal("historical loan created a transaction or acquired late locks")
				}
				return err
			})
			if result.State() != f.Committed || calls != 1 || len(borrowed) == 0 {
				t.Fatal("original result loan did not commit once")
			}
			commandResultAssertCleared(t, borrowed, e.loan)
			commandResultAssertCleared(t, s.loaded...)
			safe, _ := json.Marshal(plan.Binding())
			if string(safe) != `"tool_run_command_result_binding"` || fmt.Sprintf("%+v", plan) != "tool_run_command_result_plan" {
				t.Fatal("default formatting exposed output")
			}
		})
	}
}

func TestRunCommandResultLoanRejectsForeignStaleAndIncomplete(t *testing.T) {
	for _, mode := range []string{"foreign-reader", "foreign-tx", "missing-locks", "changed-call", "read-profile", "write-profile", "report-digest", "stdout-digest", "stderr-digest", "stdout-bytes", "stderr-bytes", "not-retired", "request", "stored-rid", "attempt-terminal", "missing-report", "classification", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			r, s, e, _ := commandResultFixture(t, "failure")
			plan, err := r.DiscoverRunCommandResult(context.Background(), e.binding.ExecutionID)
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
					r, err = NewRunCommandResultReader(s, e)
					if err != nil {
						return err
					}
				case "foreign-tx":
					tx = f.NewTx()
				case "changed-call":
					e.binding.CallID = "different-original-call"
				case "read-profile":
					e.binding.RunnerBinding = tc.ReadFileRegistration().Binding
				case "write-profile":
					e.binding.RunnerBinding = tc.WriteFileRegistration().Binding
				case "classification":
					s.terminal.Code = "command_signaled"
				case "unknown":
					s.record.Phase = "failed"
					s.terminal.Outcome = "unknown"
					s.terminal.Code = "backend_contract_violation"
					s.terminal.ReportAvailable = false
					s.terminal.Result = nil
				default:
					s.corrupt = mode
				}
				return r.WithRunCommandResultInTx(ctx, tx, plan, func(context.Context, tc.RunCommandResultBinding, []byte) error { calls++; return nil })
			})
			if result.State() == f.Committed || calls != 0 {
				t.Fatal("changed, foreign or incomplete report reached consumer")
			}
			commandResultAssertCleared(t, e.loan)
			commandResultAssertCleared(t, s.loaded...)
		})
	}
}

func TestRunCommandResultLoanRequiresOriginalReturnAndClearsBorrow(t *testing.T) {
	for _, mode := range []string{"omit", "duplicate", "swallow", "return-error", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			r, s, e, _ := commandResultFixture(t, "failure")
			plan, err := r.DiscoverRunCommandResult(context.Background(), e.binding.ExecutionID)
			if err != nil {
				t.Fatal(err)
			}
			cause, _ := inputCause(e.binding)
			e.mode = mode
			calls := 0
			var loan []byte
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := s.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
				if err := s.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
					return err
				}
				return r.WithRunCommandResultInTx(ctx, tx, plan, func(_ context.Context, _ tc.RunCommandResultBinding, raw []byte) error {
					calls++
					loan = raw
					if mode == "swallow" {
						return fail(f.Forbidden)
					}
					if mode == "cancelled" {
						cancel()
					}
					return nil
				})
			})
			want := 1
			if mode == "omit" {
				want = 0
			}
			if result.State() == f.Committed || calls != want {
				t.Fatal("original port return/cancellation lost")
			}
			commandResultAssertCleared(t, loan, e.loan)
			commandResultAssertCleared(t, s.loaded...)
			// Only this controlled history port owns the raw loan. The reader owns
			// its decoded argv slice and clears it even after a late port error.
			e.mode = "return-error"
			var argv []string
			result = s.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
				return borrowCommittedRunCommandArguments(ctx, tx, e, committedCommandPlanControl{e.binding}, e.binding, func(_ context.Context, v tc.RunCommandToolArguments) error { argv = v.Arguments.Args; return nil })
			})
			if result.State() == f.Committed || len(argv) == 0 {
				t.Fatal("decoded argument loan not exercised")
			}
			for _, v := range argv {
				if v != "" {
					t.Fatal("decoded argv slice retained after original port return")
				}
			}
		})
	}
	r, s, e, _ := commandResultFixture(t, "failure")
	s.unknown = true
	s.attempt = newOperationID[f.TransactionAttempt](t)
	if plan, err := r.DiscoverRunCommandResult(context.Background(), e.binding.ExecutionID); err == nil || plan != nil {
		t.Fatal("Unknown original transaction published a plan")
	}
}
