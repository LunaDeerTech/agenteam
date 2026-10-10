package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

func retryBindingPolicy(t *testing.T, attempts int64) LaunchRetryPolicy {
	t.Helper()
	p, err := NewLaunchRetryPolicy(attempts, 2*time.Second, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func retryBindingClaimRow(t *testing.T, req wc.TaskClaimRequest, policy LaunchRetryPolicy) *dispatchRecord {
	t.Helper()
	r := dispatchTestRecord(t, 1, Pending)
	r.outcome, r.attempts, r.retryPolicy = NotSent, 0, policy
	r.guard = &ClaimGuard{TaskID: req.TaskID.String(), ClaimedVersion: req.ExpectedTaskVersion + 1, SourceState: "todo", SourceAssigneeID: req.AgentID, SourcePriority: "high", SourceSprintID: req.CurrentSprintID.String(), SourceOrderGeneration: 1}
	return r
}

func TestSchedulerRetryBindingStrictStoredIdentity(t *testing.T) {
	p := retryBindingPolicy(t, 6)
	raw, digest, err := encodeRetryPolicy(p)
	const canonical = "agenteam.scheduler.launch_retry\nalgorithm=capped_exponential_v1\nmax_attempts=6\ninitial_backoff_ns=2000000000\nmax_backoff_ns=10000000000\n"
	const identity = "sha256:15c902076008d31a88b82ff8c437160aec11d9e97389c9a209f8b20d3b8ad07f"
	if err != nil || string(raw) != canonical || digest == nil || *digest != identity {
		t.Fatal("stored representation changed existing identity")
	}
	decoded, err := decodeRetryPolicy(raw, digest)
	if err != nil || decoded != p {
		t.Fatal("valid bound policy did not round trip", err)
	}
	if got, err := decodeRetryPolicy(nil, nil); err != nil || got != (LaunchRetryPolicy{}) {
		t.Fatal("legacy NULL policy acquired settings", err)
	}
	if got, ok := (Dispatch{}).RetryPolicy(); ok || got != (LaunchRetryPolicy{}) {
		t.Fatal("empty receipt acquired settings")
	}
	for _, name := range []string{"missing-bytes", "missing-digest", "empty-bytes", "wrong-digest", "other-algorithm", "leading-zero", "leading-plus", "no-final-lf", "extra-line", "zero-attempts", "overflow", "zero-duration", "reversed-backoff", "oversized", "non-utf8"} {
		t.Run(name, func(t *testing.T) {
			bad := append([]byte(nil), raw...)
			sum := *digest
			var badDigest *string = &sum
			switch name {
			case "missing-bytes":
				bad = nil
			case "missing-digest":
				badDigest = nil
			case "empty-bytes":
				bad = []byte{}
			case "wrong-digest":
				sum = "sha256:" + strings.Repeat("0", 64)
			case "other-algorithm":
				bad = []byte(strings.Replace(canonical, "v1", "v2", 1))
			case "leading-zero":
				bad = []byte(strings.Replace(canonical, "attempts=6", "attempts=06", 1))
			case "leading-plus":
				bad = []byte(strings.Replace(canonical, "attempts=6", "attempts=+6", 1))
			case "no-final-lf":
				bad = bad[:len(bad)-1]
			case "extra-line":
				bad = append(bad, '\n')
			case "zero-attempts":
				bad = []byte(strings.Replace(canonical, "attempts=6", "attempts=0", 1))
			case "overflow":
				bad = []byte(strings.Replace(canonical, "attempts=6", "attempts=9223372036854775808", 1))
			case "zero-duration":
				bad = []byte(strings.Replace(canonical, "initial_backoff_ns=2000000000", "initial_backoff_ns=0", 1))
			case "reversed-backoff":
				bad = []byte(strings.Replace(canonical, "max_backoff_ns=10000000000", "max_backoff_ns=1", 1))
			case "oversized":
				bad = []byte(strings.Repeat("x", retryPolicyByteLimit+1))
			case "non-utf8":
				bad[0] = 0xff
			}
			// Except for pair/hash failures, provide the correct hash of the
			// malformed bytes: hash agreement alone is not canonical policy.
			if name != "wrong-digest" && badDigest != nil {
				hash := sha256.Sum256(bad)
				sum = "sha256:" + hex.EncodeToString(hash[:])
			}
			row := dispatchTestRecord(t, 1, Pending)
			values := recordValues(t, row)
			values[29], values[30] = bad, badDigest
			got, err := scanDispatch(dispatchTestRow{values: values})
			var issue *f.Fault
			if got != nil || !errors.As(err, &issue) || issue.Code != f.DependencyUnavailable {
				t.Fatal("invalid storage returned a partial Dispatch or unsafe error", err)
			}
		})
	}
	row := dispatchTestRecord(t, 1, Pending)
	row.retryPolicy = p
	values := recordValues(t, row)
	read, err := scanDispatch(dispatchTestRow{values: values})
	if err != nil {
		t.Fatal(err)
	}
	receipt := snapshot(read)
	values[29].([]byte)[0] = 'X'
	read.retryPolicy = retryBindingPolicy(t, 9)
	copy, bound := receipt.RetryPolicy()
	if !bound || copy != p {
		t.Fatal("receipt retained mutable row/byte identity")
	}
	copy = retryBindingPolicy(t, 10)
	if again, ok := receipt.RetryPolicy(); !ok || again != p || copy == again {
		t.Fatal("caller replaced immutable receipt settings")
	}
}

type retryBindingInsertExecutor struct {
	pendingTestStore
	sql  string
	args []any
}

func (s *retryBindingInsertExecutor) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	s.sql, s.args = sql, append([]any(nil), args...)
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func TestSchedulerRetryBindingInsertPreservesExplicitAndLegacyPair(t *testing.T) {
	for _, name := range []string{"explicit", "legacy"} {
		t.Run(name, func(t *testing.T) {
			_, _, _, req := testClaimSetup(t)
			var policy LaunchRetryPolicy
			if name == "explicit" {
				policy = retryBindingPolicy(t, 6)
			}
			row := retryBindingClaimRow(t, req, policy)
			x := new(retryBindingInsertExecutor)
			// This checks the real repository's parameter projection only;
			// it does not pretend to authorize Work or execute PostgreSQL.
			if err := insertDispatch(context.Background(), x, row); err != nil {
				t.Fatal(err)
			}
			if len(x.args) != 16 || !strings.Contains(x.sql, "retry_policy,retry_policy_digest)") || !strings.Contains(x.sql, "$14,$14,$15,$16)") {
				t.Fatal("binding omitted from original insert")
			}
			raw, ok := x.args[14].([]byte)
			digest, digestOK := x.args[15].(*string)
			if !ok || !digestOK {
				t.Fatal("policy parameter types changed")
			}
			decoded, err := decodeRetryPolicy(raw, digest)
			if err != nil || decoded != policy || name == "legacy" && (raw != nil || digest != nil) {
				t.Fatal("insert did not preserve explicit/NULL pair", err)
			}
		})
	}
}

func TestSchedulerRetryBindingConstructorAndStoredReplay(t *testing.T) {
	old, _, _, req := testClaimSetup(t)
	if got, err := NewCoordinatorWithRetryPolicy(old.authority, old.deps, LaunchRetryPolicy{}); err == nil || got != nil {
		t.Fatal("opt-in constructor supplied implicit policy")
	}
	p := retryBindingPolicy(t, 6)
	configured, err := NewCoordinatorWithRetryPolicy(old.authority, old.deps, p)
	if err != nil {
		t.Fatal(err)
	}
	p = retryBindingPolicy(t, 9)
	_, call, err := configured.admit(context.Background(), req, emptyClaimPolicy())
	if err != nil || call.retryPolicy.MaxAttempts() != 6 || configured.retryPolicy == p || old.retryPolicy != (LaunchRetryPolicy{}) {
		t.Fatal("constructor changed legacy behavior or retained caller settings", err)
	}
	configured.release(call)
	configured.Stop()
	old.Stop()
	for _, name := range []string{"bound-different-deployment", "legacy-under-opt-in", "bound-under-legacy-constructor"} {
		t.Run(name, func(t *testing.T) {
			s, store, work, request := testClaimSetup(t)
			stored := retryBindingPolicy(t, 6)
			if name == "legacy-under-opt-in" {
				stored = LaunchRetryPolicy{}
			}
			if name != "bound-under-legacy-constructor" {
				s, err = NewCoordinatorWithRetryPolicy(s.authority, s.deps, retryBindingPolicy(t, 9))
				if err != nil {
					t.Fatal(err)
				}
			}
			row := retryBindingClaimRow(t, request, stored)
			store.row = dispatchTestRow{values: recordValues(t, row)}
			transactions := 0
			store.within = func(ctx context.Context, _ f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
				transactions++
				if err := fn(ctx, store.tx); err != nil {
					return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err))
				}
				return f.CommittedResult()
			}
			out, err := s.ClaimTask(context.Background(), request, emptyClaimPolicy())
			got, bound := out.RetryPolicy()
			if err != nil || got != stored || bound != (stored != (LaunchRetryPolicy{})) || work.called != 0 || transactions != 1 {
				t.Fatal("replay replaced binding or re-applied Work", err)
			}
			s.Stop()
			if !s.Joined() || s.Drain(context.Background()) != nil {
				t.Fatal("replay did not retire its original call")
			}
		})
	}
}

func TestSchedulerRetryBindingUnknownRetainsOriginalObservedPolicy(t *testing.T) {
	for _, name := range []string{"new-bound", "new-legacy", "final-replay-bound", "final-replay-legacy"} {
		t.Run(name, func(t *testing.T) {
			s, store, work, req := testClaimSetup(t)
			configured := retryBindingPolicy(t, 9)
			if name == "new-legacy" {
				configured = LaunchRetryPolicy{}
			} else {
				var err error
				s, err = NewCoordinatorWithRetryPolicy(s.authority, s.deps, configured)
				if err != nil {
					t.Fatal(err)
				}
			}
			observed := configured
			finalReplay := strings.HasPrefix(name, "final-replay-")
			if name == "final-replay-bound" {
				observed = retryBindingPolicy(t, 6)
			} else if name == "final-replay-legacy" {
				observed = LaunchRetryPolicy{}
			}
			row := retryBindingClaimRow(t, req, observed)
			dispatch, _ := f.ParseID[DispatchIdentity](req.DispatchID)
			command, _ := claimCommand(req.ProjectID, dispatch)
			cause, _ := f.NewCommandsCause(command)
			attempt := dispatchTestID[f.TransactionAttempt](t, 777)
			phase := 0
			store.within = func(ctx context.Context, _ f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
				phase++
				if phase == 1 {
					// Execute the original first read: no receipt exists yet.
					if err := fn(ctx, store.tx); err != nil {
						t.Fatal(err)
					}
					return f.CommittedResult()
				}
				if phase == 2 {
					if finalReplay {
						store.row = dispatchTestRow{values: recordValues(t, row)}
						if err := fn(ctx, store.tx); err != nil {
							t.Fatal(err)
						}
					}
					// Controlled physical Unknown. The new-claim cases do not
					// execute or claim successful Work SQL; real SQL is separate.
					return f.UnknownResult(attempt, cause)
				}
				if err := fn(ctx, store.tx); err != nil {
					return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err))
				}
				return f.CommittedResult()
			}
			out, original := s.ClaimTask(context.Background(), req, emptyClaimPolicy())
			unknown, ok := UnknownAttempt(original)
			if !ok || unknown.AttemptID() != attempt || out.data != nil || work.called != 1 {
				t.Fatal("original Unknown identity lost")
			}
			s.Stop()
			wrong := observed
			if wrong == (LaunchRetryPolicy{}) {
				wrong = retryBindingPolicy(t, 6)
			} else {
				wrong = LaunchRetryPolicy{}
			}
			row.retryPolicy = wrong
			store.row = dispatchTestRow{values: recordValues(t, row)}
			if out, err := s.ResolveClaim(context.Background(), req); err != original || out.data != nil || s.Joined() || work.called != 1 {
				t.Fatal("different policy resolved or replaced original Unknown")
			}
			row.retryPolicy = observed
			store.row = dispatchTestRow{values: recordValues(t, row)}
			out, err := s.ResolveClaim(context.Background(), req)
			got, bound := out.RetryPolicy()
			if err != nil || got != observed || bound != (observed != (LaunchRetryPolicy{})) || !s.Joined() || work.called != 1 {
				t.Fatal("original observed policy failed to resolve", err)
			}
			if s.Drain(context.Background()) != nil {
				t.Fatal("resolved original call did not drain")
			}
		})
	}
}
