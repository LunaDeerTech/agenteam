//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/app"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
)

// Setup uses formal Owner services and never claims or launches the todo.
// Its original owners retire before the public App starts with its own Store,
// process guard and configuration. The App's default Manager is the only
// consumer allowed to discover, claim, prepare and execute this task.
func TestAppExecutionRuntime(t *testing.T) {
	for _, test := range []struct {
		name string
		held bool
	}{
		{"discovers-existing-project-and-completes", false},
		{"stop-joins-held-model-and-leases", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			wire := &firstRoundFixture{}
			capture := newModelEnvironmentFixtureWithSource(t, nil, func(v *taskTransitionFixture, models *model.Service, policy *ec.Policy) {
				t.Cleanup(func() {
					if t.Failed() {
						wire.close(t)
					}
				})
				wire.configureWire(t, test.held)
				wire.audit = v.base.audit
				// This formal System command persists the exact owned D04 IP/port rule.
				// The App subsequently constructs and reloads its own real PolicyService.
				_ = wire.transport(t, v)
				current, err := models.GetProjectModel(ctxFor(t), v.base.ownerBrowser.actor, v.base.project.ID, v.agent.modelID)
				firstRoundRequire(t, err)
				provider, err := models.GetProjectProvider(ctxFor(t), v.base.ownerBrowser.actor, v.base.project.ID, current.ProviderID)
				firstRoundRequire(t, err)
				input := provider.Input.Clone()
				input.BaseURL = "https://fixture.test:8443/case/" + wire.scenario
				scope, err := i.InProject(v.base.project.ID)
				firstRoundRequire(t, err)
				_, err = models.UpdateProvider(ctxFor(t), mc.UpdateProviderRequest{CommandMeta: mc.CommandMeta{Actor: v.base.ownerBrowser.actor, Scope: scope, Key: "app-runtime-real-wire"}, ID: provider.ID, ExpectedVersion: provider.Version, Input: input})
				firstRoundRequire(t, err)
				policy.DeniedToolIDs = []i.ToolID{v.agent.tool.ToolID}
				firstRoundRequire(t, policy.Validate())
			}, true)
			wire.capture = capture
			v := capture.v
			oldProcess, err := v.agent.guard.CurrentProcess()
			firstRoundRequire(t, err)
			var before int64
			firstRoundRequire(t, v.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1`, v.base.project.ID.String()).Scan(&before))
			if before != 0 || v.task.State != wc.TaskStateTodo {
				t.Fatal("setup admitted work before the production App")
			}
			if v.base.afterOwners != nil {
				t.Fatal("fixture already owns a completion handoff")
			}
			v.base.afterOwners = func() {
				// Testing's cleanup LIFO already ran the real Stop/Drain callbacks. These
				// additional facts reject a still-live original owner before any App call.
				if t.Failed() || !v.base.core.Joined() || !v.agent.agents.Joined() || !capture.claims.Joined() {
					t.Fatal("setup owners did not actually join")
				}
				if _, err := v.agent.guard.CurrentProcess(); err == nil {
					t.Fatal("setup still holds its original guard")
				}
				var stopped bool
				firstRoundRequire(t, v.base.raw.QueryRow(ctxFor(t), `SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1`, oldProcess.String()).Scan(&stopped))
				if !stopped {
					t.Fatal("setup process claim did not retire")
				}
				defer wire.close(t)
				runAppExecutionRuntime(t, wire, test.held, oldProcess.String())
			}
		})
	}
}

type appRuntimeLog struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (l *appRuntimeLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.data.Write(p)
}
func (l *appRuntimeLog) bytes() []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	return bytes.Clone(l.data.Bytes())
}

func runAppExecutionRuntime(t *testing.T, wire *firstRoundFixture, held bool, oldProcess string) {
	t.Helper()
	v := wire.capture.v
	// A private mount namespace supplied by the original runner changes only
	// name resolution. This is the production SystemResolver and real TLS CA.
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, "fixture.test")
	firstRoundRequire(t, err)
	if os.Getenv("AGENTEAM_APP_RUNTIME_PRIVATE_HOSTS") != "1" || len(addresses) != 1 || addresses[0].IP.String() != wire.network.PrivateIP {
		t.Fatal("original private hosts binding is absent")
	}
	// This handoff runs after the setup's testing.TempDir cleanup. Own a new
	// directory explicitly until the actual public App call has returned.
	directory, err := os.MkdirTemp("", "app-runtime-")
	firstRoundRequire(t, err)
	defer func() { firstRoundRequire(t, os.RemoveAll(directory)) }()
	cfg := appExecutionConfig(t, wire, directory)
	logs := &appRuntimeLog{}
	logger, err := logging.New(logging.Central, slog.LevelInfo, logs)
	firstRoundRequire(t, err)
	signals := make(chan os.Signal, 1)
	done := make(chan struct{})
	var runErr error
	go func() { runErr = app.Run(ctx, cfg, logger, signals); close(done) }()
	joined := false
	defer func() {
		if joined {
			return
		}
		cancel()
		select {
		case <-done:
			joined = true
		case <-time.After(15 * time.Second):
			t.Error("original public App call has not returned")
			// Keep the database, spool and wire owned until the physical call
			// returns. The original whole-test deadline remains the outer bound.
			<-done
		}
	}()
	await := func(check func() bool) {
		t.Helper()
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			if check() {
				return
			}
			select {
			case <-done:
				t.Fatalf("App returned before its required runtime fact: %v; safe log=%s", runErr, logs.bytes())
			case <-ctx.Done():
				t.Fatal("App runtime observation deadline")
			case <-tick.C:
			}
		}
	}
	var executionID, processID string
	if held {
		await(func() bool {
			state, e := wire.network.State(ctx, wire.scenario)
			if e != nil {
				t.Fatal(e)
			}
			return len(state.Requests) == 1 && state.ActiveHandlers == 1
		})
		firstRoundRequire(t, v.base.raw.QueryRow(ctx, `SELECT e.id,s.process_id FROM agenteam_execution.executions e JOIN agenteam_execution.snapshots s ON(s.id,s.execution_id)=(e.snapshot_id,e.id) WHERE e.project_id=$1 AND e.agent_id=$2 AND e.status='running'`, v.base.project.ID.String(), v.agentID.String()).Scan(&executionID, &processID))
		var live bool
		firstRoundRequire(t, v.base.raw.QueryRow(ctx, `SELECT p.state='claimed' AND p.stopped_at IS NULL AND NOT l.released AND NOT e.released
   FROM agenteam_object.process_claims p JOIN agenteam_model.calls c ON c.process_id::text=p.process_id
   JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id
   JOIN agenteam_secret.project_variable_execution_leases e ON e.execution_id=$1
   WHERE p.process_id=$2 AND c.request_data#>>'{Initiator,ExecutionID}'=$1`, executionID, processID).Scan(&live))
		if !live || processID == oldProcess {
			t.Fatal("held Model does not own the new App guard and both live leases")
		}
	} else {
		await(func() bool {
			var count int64
			firstRoundRequire(t, v.base.raw.QueryRow(ctx, `SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1 AND agent_id=$2 AND status='succeeded'`, v.base.project.ID.String(), v.agentID.String()).Scan(&count))
			return count == 1
		})
	}
	signals <- syscall.SIGTERM
	select {
	case <-done:
		joined = true
	case <-time.After(15 * time.Second):
		t.Fatal("public App did not join its original stop")
	}
	firstRoundRequire(t, runErr)
	status := ec.Succeeded
	if held {
		status = ec.Cancelled
	}
	appExecutionTerminal(t, wire, status, oldProcess)
	wire.requireWire(t, 1, true)
	var graceful bool
	for _, line := range bytes.Split(logs.bytes(), []byte{'\n'}) {
		var entry struct {
			Event   string `json:"event"`
			Outcome string `json:"outcome"`
		}
		if json.Unmarshal(line, &entry) == nil && entry.Event == "shutdown_complete" && entry.Outcome == "drained" {
			graceful = true
		}
	}
	if !graceful {
		t.Fatal("App returned without its original graceful shutdown fact")
	}
}

func appExecutionConfig(t *testing.T, wire *firstRoundFixture, directory string) config.Config {
	t.Helper()
	v := wire.capture.v
	remote, err := objectfixture.Load()
	firstRoundRequire(t, err)
	recovery := filepath.Join(directory, "account-recovery.log")
	firstRoundRequire(t, os.WriteFile(recovery, nil, 0600))
	key := func(k string, b byte) string {
		return fmt.Sprintf(`{"format":1,"current_kid":%q,"keys":[{"kid":%q,"key_b64":%q}]}`, k, k, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)))
	}
	values := map[string]string{
		"HTTP_ADDR": "127.0.0.1:0", "PUBLIC_ORIGIN": variableHTTPOrigin, "SHUTDOWN_TIMEOUT": "10s",
		"DATABASE_URL": v.base.db.Fixture.URL(v.base.db.Name), "DATABASE_CA_FILE": v.base.db.Fixture.CAFile, "DATABASE_TLS_MODE": "verify-full",
		"CURSOR_KEYRING": key("c", 1), "SECRET_KEYRING": fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))),
		"OBJECT_DOWNLOAD_KEYRING": key("d", 3), "ACCOUNT_KEYRING": key("a", 4), "KNOWLEDGE_CONFIRMATION_KEYRING": key("k", 5), "ACCOUNT_RECOVERY_LOG": recovery,
		"OBJECT_ENDPOINT": remote.Endpoint(), "OBJECT_BUCKET": v.agent.p2.bucket, "OBJECT_ACCESS_KEY": remote.AccessKey, "OBJECT_SECRET_KEY": remote.SecretKey, "OBJECT_TLS_MODE": "verify-full", "OBJECT_CA_FILE": remote.CAFile, "OBJECT_SPOOL_DIR": filepath.Join(directory, "app-spool"),
		"OUTBOUND_CA_FILE": wire.network.CAFile, "SCHEDULER_LAUNCH_RETRY_MAX_ATTEMPTS": "2", "SCHEDULER_LAUNCH_RETRY_INITIAL_BACKOFF": "20ms", "SCHEDULER_LAUNCH_RETRY_MAX_BACKOFF": "100ms",
	}
	policy, err := json.Marshal(wire.capture.launchPolicy)
	firstRoundRequire(t, err)
	// The explicit large visit count prevents this two-case single-turn fixture
	// from authorizing a subsequent work-phase relaunch while it observes stop.
	values["EXECUTION_RUNTIME"] = fmt.Sprintf(`{"profile":"direct-text-v1","project_runners":{"max_projects":2,"project_page_size":2,"execution_page_size":1,"discovery_interval":"50ms","tick_interval":"50ms","relaunch_skip_count":1000000,"launch_policy":%s},"associated_executor":{"max_owned":1,"recovery_interval":"20ms"},"model_agent_retry":{"initial_request_timeout":"30s","max_request_timeout":"60s","timeout_multiplier":2,"initial_backoff":"100ms","max_backoff":"1s"}}`, policy)
	cfg, err := config.Load(func(name string) (string, bool) {
		value, ok := values[strings.TrimPrefix(name, config.Prefix)]
		return value, ok
	}, nil)
	firstRoundRequire(t, err)
	return cfg
}

func appExecutionTerminal(t *testing.T, wire *firstRoundFixture, status ec.Status, oldProcess string) {
	t.Helper()
	v := wire.capture.v
	var inputRaw, snapshotRaw, roundRaw []byte
	var actual, process, taskState, modelPhase, usageFinal string
	var executions, claims, dispatches, attempts, invocations, transcripts, events, taskVersion int64
	var retired, modelReleased, environmentReleased, guardStopped bool
	var completed, guardAt time.Time
	err := v.base.raw.QueryRow(ctxFor(t), `SELECT e.status,e.completed_at,s.process_id,p.input,s.content,r.content,t.state,t.version,c.phase,c.retired,i.final_status,l.released,env.released,g.state='stopped',g.stopped_at,
 (SELECT count(*) FROM agenteam_execution.executions n WHERE n.project_id=e.project_id),
 (SELECT count(*) FROM agenteam_work.task_scheduler_claims n WHERE n.project_id=e.project_id::uuid),
 (SELECT count(*) FROM agenteam_scheduler.dispatches n WHERE n.project_id=e.project_id),
 (SELECT count(*) FROM agenteam_model.runtime_attempts n WHERE n.call_id=c.id),
 (SELECT count(*) FROM agenteam_model.invocations n WHERE n.call_id=c.id),
 (SELECT count(*) FROM agenteam_execution.transcript_entries n WHERE n.execution_id=e.id),
 (SELECT count(*) FROM agenteam_outbox.events n WHERE n.aggregate_type='execution.execution' AND n.aggregate_id=e.id::uuid)
 FROM agenteam_execution.executions e
 JOIN agenteam_execution.preparation_inputs p ON(p.execution_id,p.project_id,p.agent_id)=(e.id,e.project_id,e.agent_id)
 JOIN agenteam_execution.snapshots s ON(s.execution_id,s.id)=(e.id,e.snapshot_id)
 JOIN agenteam_execution.rounds r ON(r.execution_id,r.snapshot_id,r.start_id)=(e.id,s.id,s.start_id)
 JOIN agenteam_model.calls c ON c.id=r.call_id::uuid
 JOIN agenteam_model.invocations i ON i.id=c.invocation_id
 JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id
 JOIN agenteam_secret.project_variable_execution_leases env ON env.execution_id=e.id
 JOIN agenteam_object.process_claims g ON g.process_id=s.process_id
 JOIN agenteam_work.tasks t ON t.id=(e.launch_request#>>'{trigger,task_id}')::uuid
 WHERE e.project_id=$1 AND e.agent_id=$2 AND r.terminal_status=e.status AND r.terminal_version=e.version
 AND r.finished_at=e.completed_at AND e.launch_request->>'purpose'='task/work'`, v.base.project.ID.String(), v.agentID.String()).Scan(&actual, &completed, &process, &inputRaw, &snapshotRaw, &roundRaw, &taskState, &taskVersion, &modelPhase, &retired, &usageFinal, &modelReleased, &environmentReleased, &guardStopped, &guardAt, &executions, &claims, &dispatches, &attempts, &invocations, &transcripts, &events)
	firstRoundRequire(t, err)
	input, err := ec.DecodePreparationInput(inputRaw)
	firstRoundRequire(t, err)
	snapshot, err := ec.DecodeDirectTextSnapshot(snapshotRaw)
	firstRoundRequire(t, err)
	round, err := ec.DecodeDirectTextRound(roundRaw)
	firstRoundRequire(t, err)
	captured, err := work.DecodeTaskContext(snapshot.Fields().Context.Trigger())
	firstRoundRequire(t, err)
	wantEntries := int64(2)
	if status == ec.Cancelled {
		wantEntries = 1
	}
	if actual != string(status) || modelPhase != actual || usageFinal != actual || !retired || !modelReleased || !environmentReleased || !guardStopped || guardAt.Before(completed) || process == oldProcess || taskState != string(wc.TaskStateInProgress) || taskVersion != int64(captured.Task().Version) || executions != 1 || claims != 1 || dispatches != 1 || attempts != 1 || invocations != 1 || transcripts != wantEntries || events != 2 {
		t.Fatal("App runtime lost its exact single turn, independent Task, lease or guard retirement facts")
	}
	if !bytes.Equal(snapshot.Fields().Context.Input().CanonicalBytes(), inputRaw) || input.Digest() != snapshot.Fields().Context.InputDigest() || round.Fields().CallID.Validate() != nil || round.Fields().ExecutionID != input.Fields().Request.ExecutionID || len(input.Fields().Tools) != 0 || len(input.Fields().Environment.Fields().Secrets) != 1 {
		t.Fatal("App runtime did not capture the original real supported inputs")
	}
	if input.Fields().Request.Launch.Trigger.TaskID != v.task.ID.String() {
		t.Fatal("App runtime executed a different task")
	}
}
