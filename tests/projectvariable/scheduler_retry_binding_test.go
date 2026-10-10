//go:build integration

package projectvariable_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/config"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/tests/testsupport/accountenv"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	"github.com/jackc/pgx/v5/pgconn"
)

// This slice binds an explicit policy and proves one real temporary rejection.
// It does not schedule another attempt or manufacture a retryable Fault.
func TestSchedulerRetryBinding(t *testing.T) {
	t.Run("config-bound-claim-and-real-lock-timeout", func(t *testing.T) {
		cfg := schedulerRetryConfig(t, "2", "100ms", "1s")
		policy, configured := cfg.SchedulerLaunchRetryPolicy()
		if !configured || policy.Validate() != nil || policy.MaxAttempts() != 2 || policy.InitialBackoff() != 100*time.Millisecond || policy.MaxBackoff() != time.Second {
			t.Fatal("explicit loader did not preserve the three policy settings")
		}
		v := prepareSchedulerClaimTask(t)
		startSchedulerSprint(t, v)
		coordinator := newSchedulerClaimCoordinator(t, v, &policy)
		claim, launchPolicy := schedulerClaimRequest(t, v)
		dispatch, err := coordinator.ClaimTask(ctxFor(t), claim, launchPolicy)
		if err != nil || dispatch.Summary().Status != scheduler.Pending || dispatch.Summary().AttemptCount != 0 {
			t.Fatal("real policy-bound Claim", err)
		}
		requireStoredRetryPolicy(t, v, dispatch, &policy)
		beforeReplay, err := schedulerClaimSnapshot(ctxFor(t), v.base.raw, v)
		if err != nil {
			t.Fatal("original Claim snapshot", err)
		}
		// A new service graph/configuration cannot rewrite the old key's policy.
		changedConfig := schedulerRetryConfig(t, "4", "200ms", "2s")
		changedPolicy, configured := changedConfig.SchedulerLaunchRetryPolicy()
		if !configured {
			t.Fatal("replacement explicit policy is absent")
		}
		restarted := newSchedulerClaimCoordinator(t, v, &changedPolicy)
		replay, err := restarted.ClaimTask(ctxFor(t), claim, launchPolicy)
		if err != nil || !reflect.DeepEqual(replay.Summary(), dispatch.Summary()) {
			t.Fatal("Claim replay under changed config lost its original result", err)
		}
		requireStoredRetryPolicy(t, v, replay, &policy)
		afterReplay, err := schedulerClaimSnapshot(ctxFor(t), v.base.raw, v)
		if err != nil || beforeReplay != afterReplay {
			t.Fatal("Claim replay changed persisted facts", err)
		}

		handoff, counter := newBusyLaunchHandoff(t, v)
		original, err := dispatch.LaunchRequest()
		if err != nil {
			t.Fatal("original complete Launch input", err)
		}
		taskBefore := v.databaseSnapshot(t)
		command, err := original.Command()
		if err != nil {
			t.Fatal("original Launch command", err)
		}
		agentKey, err := f.AgentLock(v.agentID.String())
		if err != nil {
			t.Fatal("actual Agent lock", err)
		}
		var observedMu sync.Mutex
		var finalAcquire bool
		var physical []f.CommitResult
		store := v.base.tracked
		store.mu.Lock()
		store.beforeLocks = func(_ context.Context, _ f.Tx, locks []f.LockRequest) error {
			for _, lock := range locks {
				if lock.Key.Canonical() == agentKey.Canonical() && lock.Mode == f.Exclusive {
					observedMu.Lock()
					finalAcquire = true
					observedMu.Unlock()
				}
			}
			return nil // Observe the batch; the actual Store still acquires it.
		}
		store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
			observedMu.Lock()
			defer observedMu.Unlock()
			if finalAcquire && cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == command.Canonical() {
				physical = append(physical, result)
			}
		}
		store.mu.Unlock()
		clearObservers := func() {
			store.mu.Lock()
			store.beforeLocks, store.afterResult = nil, nil
			store.mu.Unlock()
		}
		defer clearObservers()
		// The real fixture sets lock_timeout=1s. Agent SH permits historical
		// lookup and Work discovery, but conflicts with final Launch's Agent EX.
		release := holdSchedulerRetryAgent(t, v, agentKey)
		rejected, launchErr := handoff.LaunchOnce(ctxFor(t), v.base.project.ID, dispatch.Summary().ID)
		release()
		clearObservers()
		observedMu.Lock()
		seen, results := finalAcquire, append([]f.CommitResult(nil), physical...)
		observedMu.Unlock()
		var pgError *pgconn.PgError
		if !seen || len(results) != 1 || results[0].State() != f.NotCommitted || !errors.As(results[0].Fault(), &pgError) || pgError.Code != "55P03" {
			t.Fatal("original final AcquireAll did not physically roll back its PostgreSQL lock timeout")
		}
		reason, matched := ec.MatchLaunchTemporaryRejection(launchErr, original)
		if launchErr == nil || !matched || reason != ec.LaunchTemporaryLockTimeout || errors.Is(launchErr, context.Canceled) || errors.Is(launchErr, context.DeadlineExceeded) {
			t.Fatal("original synchronous Launch did not retain its request-bound temporary proof")
		}
		summary := rejected.Summary()
		if summary.ID != dispatch.Summary().ID || summary.Status != scheduler.Pending || summary.LaunchOutcome != scheduler.KnownNotCreated || summary.AttemptCount != 1 || summary.ExecutionID != nil {
			t.Fatal("real temporary rejection was not checkpointed as pending known-not-created")
		}
		requestAfter, err := rejected.LaunchRequest()
		if err != nil || !reflect.DeepEqual(requestAfter, original) || taskBefore != v.databaseSnapshot(t) {
			t.Fatal("temporary rejection changed original Launch identity or Task facts", err)
		}
		requireStoredRetryPolicy(t, v, rejected, &policy)
		var executions, slots int64
		var noRetry bool
		err = v.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text),
 (SELECT count(*) FROM agenteam_execution.executions WHERE agent_id=$2::text AND status IN ('created','preparing','running','waiting')),
 (SELECT next_retry_at IS NULL FROM agenteam_scheduler.dispatches WHERE project_id=$1::text AND id=$3::text)`, v.base.project.ID.String(), v.agentID.String(), summary.ID.String()).Scan(&executions, &slots, &noRetry)
		if err != nil || executions != 0 || slots != 0 || !noRetry {
			t.Fatal("temporary rejection created an Execution, occupied a slot or scheduled a retry", err)
		}
		looked, err := handoff.Lookup(ctxFor(t), v.base.project.ID, summary.ID)
		launches, _, created := counter.observed()
		if err != nil || !reflect.DeepEqual(looked.Summary(), summary) || launches != 1 || created.ID.Validate() == nil {
			t.Fatal("read-only original handoff lookup resent Launch or changed the rejection", err)
		}
	})
	t.Run("legacy-null-policy-stays-unbound", func(t *testing.T) {
		v, legacy := newSchedulerClaimFixture(t)
		claim, launchPolicy := schedulerClaimRequest(t, v)
		original, err := legacy.ClaimTask(ctxFor(t), claim, launchPolicy)
		if err != nil {
			t.Fatal("real legacy-constructor Claim", err)
		}
		requireStoredRetryPolicy(t, v, original, nil)
		before, err := schedulerClaimSnapshot(ctxFor(t), v.base.raw, v)
		if err != nil {
			t.Fatal("legacy Claim snapshot", err)
		}
		cfg := schedulerRetryConfig(t, "2", "100ms", "1s")
		policy, configured := cfg.SchedulerLaunchRetryPolicy()
		if !configured {
			t.Fatal("new explicit policy is absent")
		}
		restarted := newSchedulerClaimCoordinator(t, v, &policy)
		replay, err := restarted.ClaimTask(ctxFor(t), claim, launchPolicy)
		if err != nil || !reflect.DeepEqual(replay.Summary(), original.Summary()) {
			t.Fatal("configured service did not replay the original unbound Claim", err)
		}
		requireStoredRetryPolicy(t, v, replay, nil)
		after, err := schedulerClaimSnapshot(ctxFor(t), v.base.raw, v)
		if err != nil || before != after {
			t.Fatal("new configuration backfilled legacy policy or changed Claim facts", err)
		}
	})
}

func requireStoredRetryPolicy(t *testing.T, v *taskTransitionFixture, dispatch scheduler.Dispatch, expected *scheduler.LaunchRetryPolicy) {
	t.Helper()
	actual, bound := dispatch.RetryPolicy()
	var raw []byte
	var digest *string
	err := v.base.raw.QueryRow(ctxFor(t), `SELECT retry_policy,retry_policy_digest FROM agenteam_scheduler.dispatches WHERE project_id=$1::text AND id=$2::text`, v.base.project.ID.String(), dispatch.Summary().ID.String()).Scan(&raw, &digest)
	if err != nil {
		t.Fatal("actual persisted retry policy", err)
	}
	if expected == nil {
		if bound || raw != nil || digest != nil {
			t.Fatal("unbound Claim acquired a retry policy")
		}
		return
	}
	want, err := expected.Identity()
	got, actualErr := actual.Identity()
	sum := sha256.Sum256(raw)
	if err != nil || actualErr != nil || !bound || len(raw) == 0 || digest == nil || *digest != string(want) || got != want || fmt.Sprintf("sha256:%x", sum) != string(want) {
		t.Fatal("stored immutable retry policy bytes/digest differ from the explicit configuration")
	}
}

// Only the policy projection is used from this configuration. Config-only
// endpoints are parsed, never connected; the real fixture owns DB/Account/Object.
func schedulerRetryConfig(t *testing.T, attempts, initial, maximum string) config.Config {
	t.Helper()
	values := map[string]string{
		config.Prefix + "DATABASE_URL": "postgresql://config:config@127.0.0.1:1/config_only", config.Prefix + "DATABASE_TLS_MODE": "disable",
		config.Prefix + "CURSOR_KEYRING":                         `{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`,
		config.Prefix + "SECRET_KEYRING":                         `{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="}]}`,
		config.Prefix + "KNOWLEDGE_CONFIRMATION_KEYRING":         `{"format":1,"current_kid":"knowledge","keys":[{"kid":"knowledge","key_b64":"gIGCg4SFhoeIiYqLjI2Oj5CRkpOUlZaXmJmam5ydnp8="}]}`,
		config.Prefix + "SCHEDULER_LAUNCH_RETRY_MAX_ATTEMPTS":    attempts,
		config.Prefix + "SCHEDULER_LAUNCH_RETRY_INITIAL_BACKOFF": initial,
		config.Prefix + "SCHEDULER_LAUNCH_RETRY_MAX_BACKOFF":     maximum,
	}
	for key, value := range objectfixture.ConfigOnlyValues() {
		values[key] = value
	}
	for key, value := range accountenv.New(t).Values() {
		values[key] = value
	}
	var environment []string
	for key, value := range values {
		environment = append(environment, key+"="+value)
	}
	cfg, err := config.Load(func(key string) (string, bool) { value, ok := values[key]; return value, ok }, environment)
	if err != nil {
		t.Fatal("explicit Central retry configuration", err)
	}
	return cfg
}

func holdSchedulerRetryAgent(t *testing.T, v *taskTransitionFixture, key f.LockKey) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	release, ready, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var result f.CommitResult
	transactionCause := cause(t)
	go func() {
		result = v.base.raw.WithinTx(ctx, transactionCause, func(ctx context.Context, tx f.Tx) error {
			if err := v.base.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Shared}}); err != nil {
				return err
			}
			close(ready)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		close(done)
	}()
	var once sync.Once
	join := func() {
		once.Do(func() {
			close(release)
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				cancel()
				<-done // The actual original Store return owns the holder's tail.
			}
			cancel()
			if result.State() != f.Committed {
				t.Error("original shared-lock holder did not return committed after release")
			}
		})
	}
	t.Cleanup(join)
	select {
	case <-ready:
	case <-done:
		t.Fatal("real shared Agent lock was not acquired")
	case <-ctx.Done():
		t.Fatal("real shared Agent lock barrier expired")
	}
	return join
}
