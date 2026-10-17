//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mt "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// The first child stops after the real Meeting call/source transaction and
// before Resolve. The replacement App must prove that original process dead,
// settle the original absence, and fail honestly without a Summary HTTP call.
func TestMeetingM3AppPreparedSummaryColdRecovery(t *testing.T) {
	meetingM3ConfigurationPreflight(t)
	w, _ := newM1WorkflowWithOptions(t, nil, false)
	v := w.app
	meetingM3VerifyOwnedDatabase(t, v)
	scenario, err := w.network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		meetingM3Reply(t, 1, "A supplied the original contribution."),
		meetingM3Reply(t, 2, "B supplied the original contribution."),
	}})
	workAppCheck(t, "meeting-cold-owned-peer", err)
	defer func() {
		if t.Failed() {
			meetingM3FailureObservation(t, w, scenario)
		}
	}()
	model := meetingM3ConfigureModel(t, v, "https://fixture.test:8443/case/"+scenario)
	agents := meetingM3CreateAgents(t, v, model)
	taskExecutionJoinEnabled(t, v)
	first := meetingM3StartPreparedChild(t, w)
	var receipt mt.Receipt
	meetingM3ColdHTTP(t, v, http.MethodPost, "/meetings", map[string]any{
		"agents": agents, "mode": "sequential", "content_nodes": []mt.InlineNode{{Type: "text", Text: "Discuss the original request."}},
	}, &receipt)
	if receipt.MeetingID.Validate() != nil || receipt.TurnID == nil {
		t.Fatal("cold stimulus did not return its original Meeting and Turn")
	}
	m2RecoveryEvent(t, first, "meeting_summary_prepared_held", nil)
	var call, generation, process, binding string
	var originalSource []byte
	var prepared bool
	err = v.admin.QueryRow(v.ctx, `SELECT c.call_id,c.generation_id,c.prepared_process_id,c.resolve_source_data,c.resolve_binding,
 c.status='prepared' AND c.snapshot_id IS NULL AND c.retired_at IS NULL AND g.status='generating' AND g.attempt=1
 FROM agenteam_meeting.summary_model_calls c JOIN agenteam_meeting.summary_generations g ON g.id=c.generation_id
 WHERE c.project_id=$1 AND c.meeting_id=$2 AND g.turn_id=$3`, v.a.String(), receipt.MeetingID.String(), receipt.TurnID.String()).Scan(&call, &generation, &process, &originalSource, &binding, &prepared)
	meetingM3ColdSQL(t, "meeting-cold-original-prepared-facts", err)
	var source struct {
		Source    mc.ResolutionSource `json:"source"`
		Selection *mc.SelectionRef    `json:"selection"`
	}
	if !prepared || process != first.process || json.Unmarshal(originalSource, &source) != nil || source.Source != mc.CurrentSelectionSource || source.Selection == nil || source.Selection.Kind != "platform" || source.Selection.Selector != mc.SummarySelector || source.Selection.Version != nil || f.Digest(binding).Validate() != nil {
		t.Fatal("prepared source lost its original selection or process")
	}
	meetingM3ColdNoSummaryAdmission(t, v, call, generation)
	m2RecoveryGuardHeld(t, w, first.process)
	m2KillRecoveryChild(t, w, first)
	w.joinedPeer(t, scenario, 2, 2)
	// The existing observation child starts the ordinary complete App with the
	// same database and spool. It does not inject a Meeting recovery substitute.
	second := m2StartRecoveryChild(t, w, false)
	if second.process == first.process {
		t.Fatal("Meeting restart reused the original ProcessGuard")
	}
	meetingM3AwaitColdFailure(t, v, second, call, generation)
	var afterSource []byte
	var afterBinding, afterProcess string
	err = v.admin.QueryRow(v.ctx, `SELECT resolve_source_data,resolve_binding,prepared_process_id FROM agenteam_meeting.summary_model_calls WHERE call_id=$1 AND generation_id=$2`, call, generation).Scan(&afterSource, &afterBinding, &afterProcess)
	meetingM3ColdSQL(t, "meeting-cold-source-preserved", err)
	if !bytes.Equal(originalSource, afterSource) || binding != afterBinding || process != afterProcess {
		t.Fatal("recovery replaced the original Resolve source")
	}
	meetingM3ColdNoSummaryAdmission(t, v, call, generation)
	var detail mt.Detail
	base := "/meetings/" + receipt.MeetingID.String()
	meetingM3ColdHTTP(t, v, http.MethodGet, base, nil, &detail)
	if len(detail.Turns) != 1 || detail.Turns[0].Status != "finalizing" || detail.Turns[0].SummaryError == "" || detail.Session.Summary != nil || detail.Session.Title != nil {
		t.Fatal("cold Summary invented an output or hid its failed finalization")
	}
	var cancelled mt.Receipt
	meetingM3ColdHTTP(t, v, http.MethodPost, base+"/controls", map[string]any{
		"action": "cancel-turn", "turn_id": receipt.TurnID, "expected_version": detail.Session.Version,
	}, &cancelled)
	if cancelled.MeetingID != receipt.MeetingID || cancelled.TurnID == nil || *cancelled.TurnID != *receipt.TurnID {
		t.Fatal("Owner cancellation changed the original source")
	}
	ctx, cancel := context.WithTimeout(v.ctx, 15*time.Second)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		var done bool
		err = v.admin.QueryRow(ctx, `SELECT g.status='cancelled' AND t.status='cancelled' AND s.summary IS NULL AND s.title IS NULL FROM agenteam_meeting.summary_generations g JOIN agenteam_meeting.turns t ON t.id=g.turn_id JOIN agenteam_meeting.sessions s ON s.id=g.meeting_id WHERE g.id=$1 AND s.project_id=$2`, generation, v.a.String()).Scan(&done)
		meetingM3ColdSQL(t, "meeting-cold-owner-cancelled", err)
		if done {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("original cold Turn cancellation did not converge")
		case <-second.done:
			m2RecoveryChildWaitLog(t, second, "meeting-cancel")
			t.Fatal("replacement App exited before Owner cancellation")
		case <-tick.C:
		}
	}
	meetingM3ColdNoSummaryAdmission(t, v, call, generation)
	m2StopRecoveryChild(t, w, second)
	w.joinedPeer(t, scenario, 2, 2)
	t.Log("MEETING_M3_PREPARED_COLD actual_sigkill_wait=true original_source_version_nil=true no_resolve_or_summary_dispatch=true honest_failure=true owner_cancelled_original_turn=true replacement_app_joined=true external_model=false")
}

// Keep SQL diagnostics closed: the existing safe observer records only fault
// code/SQLSTATE/position and technical identifiers, never SQL, args or content.
func meetingM3ColdSQL(t *testing.T, stage string, err error) {
	t.Helper()
	if err != nil {
		meetingM3SafeError(t, stage, err)
	}
	workAppCheck(t, stage, err)
}

func meetingM3ColdNoSummaryAdmission(t *testing.T, v *executionSchedulerApp, call, generation string) {
	t.Helper()
	var calls, resolutions, leases, domainCalls, retained int
	err := v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_model.calls WHERE id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_model.resolution_preparations WHERE owner_kind='model_call' AND owner_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_kind='model_call' AND owner_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_meeting.summary_model_calls WHERE generation_id=$2::text),
 (SELECT count(*) FROM agenteam_model.serving_snapshot_retentions WHERE consumer_kind='meeting' AND generation_id=$2::text::uuid)`, call, generation).Scan(&calls, &resolutions, &leases, &domainCalls, &retained)
	meetingM3ColdSQL(t, "meeting-cold-no-new-admission", err)
	if calls != 0 || resolutions != 0 || leases != 0 || domainCalls != 1 || retained != 0 {
		t.Fatalf("prepared recovery admitted new work calls=%d resolutions=%d leases=%d domain=%d retained=%d", calls, resolutions, leases, domainCalls, retained)
	}
}

func meetingM3AwaitColdFailure(t *testing.T, v *executionSchedulerApp, child *m2RecoveryChild, call, generation string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(v.ctx, 15*time.Second)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		var done bool
		err := v.admin.QueryRow(ctx, `SELECT g.status='failed' AND g.error_code<>'' AND c.returned_at IS NOT NULL AND c.retired_at IS NOT NULL AND c.snapshot_id IS NULL AND NOT c.resolution_unknown FROM agenteam_meeting.summary_generations g JOIN agenteam_meeting.summary_model_calls c ON c.generation_id=g.id WHERE c.call_id=$1 AND g.id=$2`, call, generation).Scan(&done)
		meetingM3ColdSQL(t, "meeting-cold-original-call-retirement", err)
		if done {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("original prepared Summary did not settle after actual process death")
		case <-child.done:
			m2RecoveryChildWaitLog(t, child, "meeting-recovery")
			t.Fatal("replacement App exited before original Summary recovery")
		case <-tick.C:
		}
	}
}

func meetingM3ColdHTTP(t *testing.T, v *executionSchedulerApp, method, suffix string, body, out any) {
	t.Helper()
	var raw []byte
	if body != nil {
		raw = workAppJSON(t, body)
	}
	response, err := v.request(v.ctx, method, "/api/v1/projects/"+v.a.String()+suffix, raw, guardID[struct{}](t).String())
	clear(raw)
	workAppCheck(t, "meeting-cold-owner-http", err)
	defer clear(response.body)
	if response.status != http.StatusOK && response.status != http.StatusCreated {
		var problem struct {
			Code f.Code `json:"code"`
		}
		_ = json.Unmarshal(response.body, &problem)
		t.Fatalf("Meeting cold Owner HTTP status=%d code=%s", response.status, problem.Code.Safe())
	}
	if out != nil {
		workAppCheck(t, "meeting-cold-owner-response", json.Unmarshal(response.body, out))
	}
}

func meetingM3StartPreparedChild(t *testing.T, w *m1Workflow) *m2RecoveryChild {
	t.Helper()
	executable, err := os.Executable()
	workAppCheck(t, "meeting-cold-test-executable", err)
	address, err := url.Parse(w.app.address)
	workAppCheck(t, "meeting-cold-listener", err)
	env := slices.Clone(w.environment)
	for n, value := range env {
		if strings.HasPrefix(value, "AGENTEAM_CENTRAL_HTTP_ADDR=") {
			env[n] = "AGENTEAM_CENTRAL_HTTP_ADDR=" + address.Host
		}
	}
	p := &m2RecoveryChild{fixtureProcess: &fixtureProcess{cmd: exec.Command(executable, "-test.run=^TestMeetingM3PreparedSummaryChild$", "-test.timeout=80s"), stdout: newEventLog(), stderr: newEventLog(), done: make(chan struct{})}}
	p.cmd.Dir = t.TempDir()
	p.cmd.Env = append(env, "PATH="+os.Getenv("PATH"), pgfixture.Env+"="+os.Getenv(pgfixture.Env), objectfixture.Env+"="+os.Getenv(objectfixture.Env), netfixture.Env+"="+os.Getenv(netfixture.Env), "AGENTEAM_M3_MEETING_PREPARED_CHILD=1", "AGENTEAM_M3_MEETING_DATABASE="+w.app.root.db.Name)
	p.cmd.Stdout, p.cmd.Stderr = p.stdout, p.stderr
	workAppCheck(t, "meeting-cold-child-start", p.cmd.Start())
	t.Logf("MEETING_PREPARED_CHILD_START pid=%d", p.cmd.Process.Pid)
	go func() { p.err = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		defer m2RecoveryChildWaitLog(t, p, "meeting-cleanup")
		select {
		case <-p.done:
		default:
			_ = p.cmd.Process.Kill()
			usageAppWait(t, p.done, "meeting-cold-owned-child-cleanup-wait")
		}
	})
	ready := m2RecoveryEvent(t, p, "m2_recovery_ready", nil)
	p.process, _ = ready["process_id"].(string)
	_, err = f.ParseID[oc.Process](p.process)
	workAppCheck(t, "meeting-cold-child-process", err)
	return p
}

type meetingM3PreparedStore struct {
	*postgres.Store
	held atomic.Bool
}

func (s *meetingM3PreparedStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	result := s.Store.WithinTx(ctx, cause, fn)
	d := cause.Details()
	if result.State() == f.Committed && d.Kind == f.RecoveryCause && d.Owner == "meeting.summary-model.prepare" && s.held.CompareAndSwap(false, true) {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "meeting_summary_prepared_held"})
		select {} // The parent proves actual SIGKILL + original Wait.
	}
	return result
}

func TestMeetingM3PreparedSummaryChild(t *testing.T) {
	if os.Getenv("AGENTEAM_M3_MEETING_PREPARED_CHILD") == "" {
		return
	}
	if os.Getenv("AGENTEAM_M3_MEETING_PREPARED_CHILD") != "1" {
		t.Fatal("unknown owned Meeting child mode")
	}
	fixture, err := pgfixture.Load()
	m2RecoveryCheck(t, "meeting-database-fixture", err)
	_, err = fixture.Config(os.Getenv("AGENTEAM_M3_MEETING_DATABASE"), nil)
	m2RecoveryCheck(t, "meeting-database-config", err)
	network, err := netfixture.Load()
	m2RecoveryCheck(t, "meeting-network-fixture", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	m2RecoveryCheck(t, "meeting-network-address", err)
	env := os.Environ()
	values := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	cfg, err := config.Load(func(key string) (string, bool) { value, ok := values[key]; return value, ok }, env)
	m2RecoveryCheck(t, "meeting-child-config", err)
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	var owned *resources
	store := &meetingM3PreparedStore{}
	d := dependencies{
		open: func(ctx context.Context, cfg postgres.Config) (database, error) {
			var err error
			store.Store, err = postgres.Open(ctx, cfg)
			return store, err
		},
		bind: func(ctx context.Context, cfg config.Config, db database, actual *resources, deps *dependencies) error {
			owned = actual
			return bindAccounts(ctx, cfg, db, actual, deps)
		},
		outboundConstruct: func(_ context.Context, _ config.Config, actual *outboundRuntime) error {
			actual.resolver = appFixtureResolver{address}
			return nil
		},
	}
	logs := newEventLog()
	logger, err := logging.New(logging.Central, slog.LevelInfo, io.MultiWriter(os.Stderr, logs))
	m2RecoveryCheck(t, "meeting-child-logger", err)
	done := make(chan struct{})
	var runErr error
	go func() { runErr = run(context.Background(), cfg, logger, signals, d); close(done) }()
	startup := time.NewTimer(40 * time.Second)
	defer startup.Stop()
listening:
	for {
		select {
		case event := <-logs.events:
			if event["event"] == "listening" {
				break listening
			}
		case <-done:
			m2RecoveryFailure("meeting-startup-return", runErr)
			t.Fatal("Meeting child returned before listening")
		case <-startup.C:
			m2RecoveryCheck(t, "meeting-child-startup", context.DeadlineExceeded)
		}
	}
	objects, ok := owned.objects().(*objectAssembly)
	if !ok || objects == nil {
		m2RecoveryCheck(t, "meeting-child-guard", f.NewFault(f.DependencyUnbound, f.NotStarted))
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m2_recovery_ready", "process_id": objects.process.String()})
	<-done
	m2RecoveryCheck(t, "meeting-child-return", runErr)
	joined := owned.producersJoined()
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m2_recovery_returned", "joined": joined})
	if !joined {
		t.Fatal("Meeting child returned before its original owners")
	}
}
