//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/.agent-state/project-variables-independent/commitproxy"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// This covers the Found branch of a cancelled pending/unknown Dispatch. The
// original Launch really writes PostgreSQL; only its physical COMMIT response
// is lost. The manager's existing directory barrier excludes unrelated visits
// and all Preparation/Model/Runner work. No domain result is manufactured.
func TestM2PendingUnknownTaskCancellation(t *testing.T) {
	var proxy *commitproxy.Proxy
	var stimulus *m2PendingLaunchStore
	v := newExecutionSchedulerAppWith(t, func(d *dependencies, v *executionSchedulerApp) {
		var err error
		proxy, err = commitproxy.New(v.ctx, net.JoinHostPort("127.0.0.1", v.root.db.Fixture.Port))
		workAppCheck(t, "m2-pending-owned-commit-proxy", err)
		t.Cleanup(func() {
			proxy.Release()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			workAppCheck(t, "m2-pending-proxy-original-join", proxy.Close(ctx))
		})
		address, err := url.Parse(v.root.db.Fixture.URL(v.root.db.Name))
		workAppCheck(t, "m2-pending-owned-database-url", err)
		address.Host = proxy.Address()
		env := slices.DeleteFunc(guardEnvironment(t, v.root.db), func(entry string) bool {
			return strings.HasPrefix(entry, "AGENTEAM_CENTRAL_DATABASE_CA_FILE=")
		})
		// Only this test's nonce-owned PostgreSQL connection traverses the
		// complete-frame fault proxy. All other App dependencies are original.
		v.root.cfg = guardConfiguration(t, append(env,
			"AGENTEAM_CENTRAL_DATABASE_URL="+address.String(), "AGENTEAM_CENTRAL_DATABASE_TLS_MODE=disable",
			"AGENTEAM_CENTRAL_PUBLIC_ORIGIN=http://localhost:8080", "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s",
			"AGENTEAM_CENTRAL_EXECUTION_RUNTIME="+executionSchedulerAppConfig,
			"AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_ATTEMPTS=2", "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_INITIAL_BACKOFF=100ms", "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_BACKOFF=1s"))
		open := d.open
		d.open = func(ctx context.Context, cfg postgres.Config) (database, error) {
			actual, err := open(ctx, cfg)
			if err != nil {
				return actual, err
			}
			if actual != v.store {
				return nil, errors.New("pending test lost original App Store")
			}
			stimulus = &m2PendingLaunchStore{executionSchedulerAppStore: v.store, proxy: proxy, original: make(chan f.CommitResult, 1)}
			return stimulus, nil
		}
	})
	var launchDone chan struct{}
	resolved := false
	// Failure cleanup releases the physical writer and uses only the original
	// Lookup to retire an observed Created result before the App's own cleanup.
	t.Cleanup(func() {
		proxy.Release()
		if launchDone != nil {
			usageAppWait(t, launchDone, "m2-pending-original-launch-return-cleanup")
		}
		if stimulus.armed.Load() && !resolved {
			usageAppWait(t, proxy.Committed(), "m2-pending-original-commit-cleanup")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := v.runtime.schedulerCalls.Handoff().Lookup(ctx, v.a, v.dispatch.Summary().ID)
			workAppCheck(t, "m2-pending-original-lookup-cleanup", err)
		}
	})
	v.seedPendingWithProvider(t, "https://runtime-app.invalid/v1", nil)
	id := v.dispatch.Summary().ID
	command, err := v.launchRequest.Command()
	workAppCheck(t, "m2-pending-original-launch-command", err)
	stimulus.selectLaunch(command)
	initial := m2ReadPendingDispatch(t, v)
	if initial.status != "pending" || initial.outcome != "not_sent" || initial.attempts != 0 || initial.execution != nil || initial.cancelRequests != 0 {
		t.Fatal("original claim did not create a pristine pending Dispatch")
	}
	launchDone = make(chan struct{})
	var launchErr error
	go func() {
		defer close(launchDone)
		_, launchErr = v.runtime.schedulerCalls.Handoff().LaunchOnce(v.ctx, v.a, id)
	}()
	usageAppWait(t, proxy.Reached(), "m2-pending-original-launch-commit-frame")
	var original f.CommitResult
	select {
	case original = <-stimulus.original:
	case <-time.After(5 * time.Second):
		t.Fatal("original Store did not return its physical Unknown")
	}
	if original.State() != f.Unknown || original.AttemptID().Validate() != nil || original.Cause().Details().Primary.Canonical() != command.Canonical() || proxy.WriterPID() <= 0 {
		t.Fatal("Unknown did not belong to the original Launch transaction")
	}
	// The complete original COMMIT is now delivered to PostgreSQL; the client
	// remains disconnected and receives no replacement success response.
	proxy.Release()
	usageAppWait(t, proxy.Committed(), "m2-pending-original-postgres-commit")
	usageAppWait(t, proxy.HeldJoined(), "m2-pending-original-proxy-connection-return")
	usageAppWait(t, launchDone, "m2-pending-original-handoff-return")
	var failure *f.Fault
	if !errors.As(launchErr, &failure) || failure.Code != f.CommitUnknown || failure.CommitState != f.Unknown || failure.RetryHint != "lookup" {
		t.Fatal("original Handoff lost its actual uncertain Launch outcome")
	}
	var execution string
	var pristine bool
	workAppCheck(t, "m2-pending-independent-created", v.admin.QueryRow(v.ctx, `SELECT id,status='created' AND version=1 AND cancel_requested_at IS NULL AND snapshot_id IS NULL AND started_at IS NULL AND completed_at IS NULL AND request_digest=$4 AND launch_request->'lineage'->>'dispatch_id'=$5 FROM agenteam_execution.executions WHERE project_id=$1::text AND agent_id=$2::text AND idempotency_key=$3`, v.a.String(), v.agent.String(), v.launchRequest.Meta.IdempotencyKey.String(), v.digest.String(), id.String()).Scan(&execution, &pristine))
	if !pristine {
		t.Fatal("independent connection did not observe the original committed Created Execution")
	}
	unknown := m2ReadPendingDispatch(t, v)
	if unknown.status != "pending" || unknown.outcome != "unknown" || unknown.attempts != 1 || unknown.execution != nil || unknown.cancelRequests != 0 || unknown.key != initial.key || unknown.request != initial.request || unknown.digest != initial.digest {
		t.Fatal("physical Unknown was replaced by a fabricated Dispatch outcome")
	}
	var originalHandoff string
	workAppCheck(t, "m2-pending-original-retained-handoff", v.admin.QueryRow(v.ctx, `SELECT id::text FROM agenteam_scheduler.project_calls
 WHERE project_id=$1::uuid AND lane='handoff' AND phase='running'
 AND invocation_identity::jsonb->>'command'='launch_once' AND invocation_identity::jsonb->>'key'=$2`, v.a.String(), v.launchRequest.Meta.IdempotencyKey.String()).Scan(&originalHandoff))
	w := &m1Workflow{app: v}
	m2TaskRequest(t, w, http.MethodGet, "/tasks/"+v.launchRequest.Trigger.TaskID, nil, "", &w.task)
	before := w.task
	if before.State != wc.TaskStateInProgress {
		t.Fatal("original Task claim did not persist in_progress")
	}
	m2CancelWholeTask(t, w) // original Owner HTTP, same-key replay and lookup
	m2TaskRequest(t, w, http.MethodGet, "/tasks/"+before.ID.String(), nil, "", &w.task)
	if w.task.State != wc.TaskStateCancelled || w.task.Version != before.Version+1 {
		t.Fatal("Owner cancellation did not change the original Task exactly once")
	}
	cancelled := m2ReadPendingDispatch(t, v)
	if cancelled.cancelRequests != 1 || !bytes.Equal(cancelled.raw, unknown.raw) {
		t.Fatal("Task cancellation rewrote the unresolved Dispatch or lost its reservation")
	}
	var paired, sameTransaction bool
	workAppCheck(t, "m2-pending-cancellation-same-transaction", v.admin.QueryRow(v.ctx, `SELECT count(*)=1,COALESCE(bool_and(r.xmin::text=c.xmin::text AND r.xmin::text=t.xmin::text AND r.xmin::text=e.xmin::text),false)
 FROM agenteam_scheduler.task_cancellation_requests r
 JOIN agenteam_work.task_transition_commands c ON c.project_id::text=r.project_id::text AND c.id::text=r.task_transition_id::text
 JOIN agenteam_work.tasks t ON t.project_id::text=r.project_id::text AND t.id::text=r.task_id::text
 JOIN agenteam_execution.executions e ON e.project_id::text=r.project_id::text AND e.id=$4::text
 WHERE r.project_id=$1::text AND r.dispatch_id=$2::text AND r.task_id=$3::text
 AND c.state='completed' AND c.request#>>'{request,target_state}'='cancelled' AND c.plan->'cancel_dispatches' ? $2::text
 AND t.state='cancelled' AND e.status='created' AND e.version=2 AND e.cancel_requested_at IS NOT NULL`, v.a.String(), id.String(), before.ID.String(), execution).Scan(&paired, &sameTransaction))
	if !paired || !sameTransaction {
		t.Fatal("Task, pending request and original Execution cancellation did not commit atomically")
	}
	queries := stimulus.transactions.Load()
	visited, err := v.runtime.schedulerCalls.Visitor().Visit(v.ctx, v.a, id)
	workAppCheck(t, "m2-pending-original-visitor-lookup", err)
	summary := visited.Dispatch.Summary()
	if !visited.Found || visited.Action != scheduler.PendingVisitLookup || summary.ID != id || summary.Status != scheduler.Launched || summary.LaunchOutcome != scheduler.Created || summary.AttemptCount != 1 || summary.ExecutionID == nil || summary.ExecutionID.String() != execution || stimulus.transactions.Load() != queries+1 {
		t.Fatal("cancelled Unknown did not use the original real Execution Lookup")
	}
	resolved = true
	associated := m2ReadPendingDispatch(t, v)
	if associated.execution == nil || *associated.execution != execution || associated.status != "launched" || associated.outcome != "created" || associated.attempts != 1 || associated.cancelRequests != 1 || !bytes.Equal(unknown.immutable, associated.immutable) {
		t.Fatal("original Lookup changed Dispatch identity, claim, attempt or diagnostics")
	}
	queries = stimulus.transactions.Load()
	again, err := v.runtime.schedulerCalls.Visitor().Visit(v.ctx, v.a, id)
	workAppCheck(t, "m2-pending-second-original-visit", err)
	if again.Found || stimulus.transactions.Load() != queries || !bytes.Equal(m2ReadPendingDispatch(t, v).raw, associated.raw) {
		t.Fatal("a settled cancelled Dispatch issued another Launch or Lookup")
	}
	// Normal Lookup has settled the business outcome. The original launch_once
	// technical entry still owns its immutable Unknown snapshot; only its
	// original Drain may confirm that now-associated Dispatch and close custody.
	var executionLive, schedulerLive, originalLive int
	workAppCheck(t, "m2-pending-exact-technical-custody", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_execution.project_calls WHERE project_id=$1::uuid AND phase<>'business_closed'),
 (SELECT count(*) FROM agenteam_scheduler.project_calls WHERE project_id=$1::uuid AND phase<>'business_closed'),
 (SELECT count(*) FROM agenteam_scheduler.project_calls WHERE project_id=$1::uuid AND id=$2::uuid AND lane='handoff' AND phase='running'
  AND invocation_identity::jsonb->>'command'='launch_once' AND invocation_identity::jsonb->>'key'=$3)`, v.a.String(), originalHandoff, v.launchRequest.Meta.IdempotencyKey.String()).Scan(&executionLive, &schedulerLive, &originalLive))
	if executionLive != 0 || schedulerLive != 1 || originalLive != 1 {
		t.Fatalf("pending cancellation custody execution_live=%d scheduler_live=%d original_handoff=%d", executionLive, schedulerLive, originalLive)
	}
	taskExecutionJoinEnabled(t, v)
	var originalClosed bool
	workAppCheck(t, "m2-pending-original-handoff-closed", v.admin.QueryRow(v.ctx, `SELECT phase='business_closed' AND closed_at IS NOT NULL FROM agenteam_scheduler.project_calls WHERE project_id=$1::uuid AND id=$2::uuid`, v.a.String(), originalHandoff).Scan(&originalClosed))
	if !originalClosed || !bytes.Equal(m2ReadPendingDispatch(t, v).raw, associated.raw) {
		t.Fatal("original Drain lost its custody row or changed the associated Dispatch")
	}
	var executions, launches, origins, liveCalls, claims, preparations, models, tools, blockers int
	workAppCheck(t, "m2-pending-no-resend-facts", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text),
 (SELECT count(*) FROM agenteam_execution.project_calls WHERE project_id=$1::uuid AND invocation_identity::jsonb#>>'{invocation,command}'='launch'),
 (SELECT count(*) FROM agenteam_execution.project_calls WHERE project_id=$1::uuid AND original_result->>'execution_id'=$2::text AND original_result->>'kind'='created'),
 (SELECT count(*) FROM agenteam_execution.project_calls WHERE project_id=$1::uuid AND phase<>'business_closed')+(SELECT count(*) FROM agenteam_scheduler.project_calls WHERE project_id=$1::uuid AND phase<>'business_closed'),
 (SELECT count(*) FROM agenteam_work.task_scheduler_claims WHERE project_id=$1::text::uuid AND task_id=$3::text::uuid),
 (SELECT count(*) FROM agenteam_execution.preparation_attempts),
 (SELECT count(*) FROM agenteam_model.invocations),
 (SELECT count(*) FROM agenteam_tool.operations),
 (SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1::text::uuid AND task_id=$3::text::uuid)`, v.a.String(), execution, before.ID.String()).Scan(&executions, &launches, &origins, &liveCalls, &claims, &preparations, &models, &tools, &blockers))
	if executions != 1 || launches != 1 || origins != 1 || liveCalls != 0 || claims != 1 || preparations != 0 || models != 0 || tools != 0 || blockers != 0 || len(v.runtime.manager.Observation().Projects) != 0 {
		t.Fatalf("pending cancellation facts executions=%d launches=%d origins=%d live=%d claims=%d preparation=%d model=%d tool=%d blockers=%d", executions, launches, origins, liveCalls, claims, preparations, models, tools, blockers)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	workAppCheck(t, "m2-pending-proxy-join", proxy.Close(ctx))
	t.Log("M2_PENDING_UNKNOWN_CANCEL physical_unknown=true original_commit_observed=true task_cancel_same_tx=true original_lookup_found=true original_launches=1 attempts=1 executions=1 original_owners_joined=true not_found_and_cold_not_covered=true")
}

// Observe the original callbacks and results; the only stimulus is arming the
// physical proxy for the writer PID after its actual Created row is visible in
// that transaction. The initial historical lookup cannot arm the proxy.
type m2PendingLaunchStore struct {
	*executionSchedulerAppStore
	proxy        *commitproxy.Proxy
	mu           sync.Mutex
	command      string
	armed        atomic.Bool
	transactions atomic.Int64
	original     chan f.CommitResult
}

func (s *m2PendingLaunchStore) selectLaunch(command f.CommandIdentity) {
	s.mu.Lock()
	s.command = command.Canonical()
	s.mu.Unlock()
}

func (s *m2PendingLaunchStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	target := s.command != "" && cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == s.command
	s.mu.Unlock()
	if !target {
		return s.executionSchedulerAppStore.WithinTx(ctx, cause, fn)
	}
	s.transactions.Add(1)
	selected := false
	result := s.executionSchedulerAppStore.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := fn(ctx, tx); err != nil {
			return err
		}
		if s.armed.Load() {
			return nil
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var pid int32
		var created bool
		owners := cause.Details().Primary.OwnerIDs()
		if len(owners) != 2 {
			return errors.New("original Launch scope differs")
		}
		if err = x.QueryRow(ctx, `SELECT pg_backend_pid(),EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE project_id=$1 AND agent_id=$2 AND idempotency_key=$3 AND status='created' AND version=1)`, owners[0], owners[1], cause.Details().Primary.Key().String()).Scan(&pid, &created); err != nil {
			return err
		}
		if created {
			if err = s.proxy.Arm(pid); err != nil {
				return err
			}
			s.armed.Store(true)
			selected = true
		}
		return nil
	})
	if selected {
		s.original <- result
	}
	return result
}

type m2PendingDispatch struct {
	raw, immutable                        []byte
	status, outcome, key, request, digest string
	execution                             *string
	attempts, cancelRequests              int
}

func m2ReadPendingDispatch(t *testing.T, v *executionSchedulerApp) m2PendingDispatch {
	t.Helper()
	var r m2PendingDispatch
	workAppCheck(t, "m2-pending-durable-dispatch", v.admin.QueryRow(v.ctx, `SELECT to_jsonb(d),to_jsonb(d)-ARRAY['status','launch_outcome','execution_id','version','updated_at'],status,launch_outcome,execution_id,attempt_count,idempotency_key,request_id,launch_digest,
 (SELECT count(*) FROM agenteam_scheduler.task_cancellation_requests c WHERE c.project_id=d.project_id AND c.dispatch_id=d.id)
 FROM agenteam_scheduler.dispatches d WHERE project_id=$1::text AND id=$2::text`, v.a.String(), v.dispatch.Summary().ID.String()).Scan(&r.raw, &r.immutable, &r.status, &r.outcome, &r.execution, &r.attempts, &r.key, &r.request, &r.digest, &r.cancelRequests))
	if r.key != v.launchRequest.Meta.IdempotencyKey.String() || r.request != v.launchRequest.Meta.RequestID.String() || r.digest != v.digest.String() {
		t.Fatal("durable Dispatch differs from original claimed Launch")
	}
	return r
}
