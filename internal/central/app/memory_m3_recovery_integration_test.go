//go:build integration && linux

package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// The parent owns the database/network/spool; each child owns its real App and
// ProcessGuard. Only already-retired Model calls are covered by this scenario.
func TestMemoryM3RetiredBuildColdRecovery(t *testing.T) {
	network, err := netfixture.Load()
	workAppCheck(t, "memory-cold-network", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	workAppCheck(t, "memory-cold-address", err)
	var environment []string
	var transport *outboundRuntime
	v := newExecutionSchedulerAppWith(t, func(d *dependencies, v *executionSchedulerApp) {
		env := guardEnvironment(t, v.root.db)
		for key, value := range memoryM3Settings() {
			env = append(env, "AGENTEAM_CENTRAL_"+key+"="+value)
		}
		env = append(env, "AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+network.CAFile)
		environment = slices.Clone(env)
		v.root.cfg = guardConfiguration(t, env)
		d.outboundConstruct = func(_ context.Context, _ config.Config, actual *outboundRuntime) error {
			actual.resolver = appFixtureResolver{address}
			transport = actual
			return nil
		}
	})
	stage := "provision"
	defer func() {
		if t.Failed() {
			memoryM3Diagnose(t, v, stage)
		}
	}()
	if transport == nil {
		t.Fatal("Memory cold fixture omitted original outbound")
	}
	m1AllowOwnedPeer(t, v, transport, network)
	tool := func(key string) id.ToolID {
		var raw string
		workAppCheck(t, "memory-cold-tool", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key=$1`, key).Scan(&raw))
		value, e := f.ParseID[id.Tool](raw)
		workAppCheck(t, "memory-cold-tool-id", e)
		return value
	}
	agentScenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		memoryM3ToolReply(t, tool("builtin:retain"), map[string]any{"content": memoryM3Fact}, 1), memoryM3TextReply(t, "Retained the lesson.", 2),
		memoryM3ToolReply(t, tool("builtin:recall"), map[string]any{"query": "schema migration"}, 3), memoryM3TextReply(t, "Recalled the lesson.", 4),
	}})
	workAppCheck(t, "memory-cold-agent-peer", err)
	content := map[string]any{"type": "procedure", "text": memoryM3Fact, "tags": []string{"migration"}, "importance": "normal"}
	extraction := string(workAppJSON(t, map[string]any{"candidates": []any{content}}))
	consolidation := string(workAppJSON(t, map[string]any{"actions": []any{map[string]any{"action": "ADD", "target_memory_id": "", "expected_revision": "", "content": content, "reason": "Reusable migration procedure"}}}))
	memoryScenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{memoryM3TextReply(t, extraction, 1), memoryM3TextReply(t, consolidation, 2)}})
	workAppCheck(t, "memory-cold-memory-peer", err)
	wire := netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{workAppJSON(t, map[string]any{"object": "list", "model": "memory-embedding", "data": []any{map[string]any{"object": "embedding", "index": 0, "embedding": []float64{1, 0}}}, "usage": map[string]any{"prompt_tokens": 10, "total_tokens": 10}})}}
	embeddingScenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_embeddings_wire", Wire: &wire})
	workAppCheck(t, "memory-cold-embedding-peer", err)
	model := memoryM3ConfigureModels(t, v, agentScenario, memoryScenario, embeddingScenario)
	agent := guardID[id.Agent](t)
	disabled := false
	intent, err := ac.NewAgentCreate(ac.AgentCreateFields{AgentID: agent, Name: "memory-recovery-agent", Description: "Memory cold recovery participant", Instructions: "Explicitly use retain or recall as requested.", ModelRef: model, ApprovalPolicy: ac.ApprovalAllow, AllowedToolIDs: []id.ToolID{}, AllowedMountIDs: []id.MountID{}, AllowedSecretVariableIDs: []id.ProjectVariableID{}, AddSkillsEnabled: &disabled, InstallSkillEnabled: &disabled, InjectAgentsMD: false})
	workAppCheck(t, "memory-cold-agent-intent", err)
	_, err = v.assembly.agents.service.CreateAgent(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), v.a, intent)
	workAppCheck(t, "memory-cold-agent", err)
	sprint := memoryM3CreateSprint(t, v)
	firstTask := memoryM3CreateTask(t, v, sprint.ID, agent, "Retain the migration lesson before restart.")
	_, err = v.planning.sprints.StartSprint(v.ctx, v.actor, executionSchedulerAppMeta(t, &sprint.Version), v.a, sprint.ID)
	workAppCheck(t, "memory-cold-start-sprint", err)
	// Retire the provisioning App before releasing discovery. No Execution has
	// borrowed it; the actual business calls will belong to the first child.
	v.runtime.StopAdmission()
	v.requestStop()
	v.store.releaseDirectory()
	usageAppWait(t, v.directory.returned, "memory-cold-provision-directory")
	usageAppWait(t, v.root.done, "memory-cold-provision-app")
	workAppCheck(t, "memory-cold-provision-exit", v.root.err)
	if !v.runtime.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("provisioning App did not join")
	}
	memoryM3GuardReleased(t, v.workAppFixture)
	var executions int
	workAppCheck(t, "memory-cold-no-provision-execution", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1`, v.a.String()).Scan(&executions))
	if executions != 0 {
		t.Fatal("provisioning App escaped original directory barrier")
	}
	stage = "first-child"
	first := memoryM3StartRecoveryChild(t, v, environment, "hold-publish")
	held := memoryM3RecoveryEvent(t, first, "memory_build_held")
	generation, _ := held["generation_id"].(string)
	if _, e := f.ParseID[struct{}](generation); e != nil || held["process_id"] != first.process || held["retired"] != true {
		t.Fatal("Memory crash barrier lacks the exact retired source")
	}
	memoryM3RecoveryGuard(t, v, first, false)
	var canonical, calls, retired, published, longReferences int
	var originalExecution string
	workAppCheck(t, "memory-cold-original-boundary", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_memory.memories WHERE project_id=$1::uuid AND agent_id=$2::uuid AND status='active' AND current_revision=1),
 (SELECT count(*) FROM agenteam_memory.model_calls WHERE project_id=$1::uuid),
 (SELECT count(*) FROM agenteam_memory.model_calls c JOIN agenteam_model.calls m ON m.id=c.call_id JOIN agenteam_secret.secret_leases l ON l.id=m.lease_id WHERE c.project_id=$1::uuid AND c.retired AND m.retired AND l.released),
 (SELECT count(*) FROM agenteam_memory.namespaces WHERE project_id=$1::uuid AND serving_generation_id IS NOT NULL),
 (SELECT count(*) FROM agenteam_model.serving_snapshot_retentions WHERE generation_id=$3::uuid AND state='retained'),
 (SELECT id FROM agenteam_execution.executions WHERE project_id=$1::text AND launch_request->'trigger'->>'task_id'=$4)`, v.a.String(), agent.String(), generation, firstTask.ID.String()).Scan(&canonical, &calls, &retired, &published, &longReferences, &originalExecution))
	if canonical != 1 || calls != 3 || retired != 3 || published != 0 || longReferences != 1 {
		t.Fatal("cold barrier was not after Model retirement and before publication")
	}
	// Queue a second original Task through the live child's Owner HTTP, while
	// the first still owns the Agent. This avoids a fixture ordering assumption.
	var created wc.TaskMutation
	nextTask := wc.TaskCreate{TaskID: guardID[wc.Task](t), SprintID: sprint.ID, Title: "Recall after the original process exits", Plan: "Recall the migration lesson from your own namespace.", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium}
	v.httpSeed(t, "/tasks", map[string]any{"request": nextTask}, &created)
	var moved wc.TaskTransitionMutation
	v.httpSeed(t, "/tasks/"+nextTask.TaskID.String()+"/transfer", map[string]any{"expected_version": created.Task.Version, "request": wc.TaskTransfer{TargetState: wc.TaskStateTodo, AssigneeAgentID: &agent, AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{}}}, &moved)
	stage = "actual-old-process-exit"
	memoryM3KillRecoveryChild(t, v, first)
	stage = "second-child"
	second := memoryM3StartRecoveryChild(t, v, environment, "observe")
	if second.process == first.process {
		t.Fatal("Memory restart reused the old ProcessGuard")
	}
	memoryM3RecoveryEvent(t, second, "memory_build_recovered")
	recoveredExecution := memoryM3AwaitExecution(t, v, firstTask.ID)
	if recoveredExecution.String() != originalExecution {
		t.Fatal("cold lookup replaced the original Execution")
	}
	memoryM3AwaitExecution(t, v, nextTask.TaskID)
	var released, ready, modelCalls, closed, operations, memories, audits int
	workAppCheck(t, "memory-cold-recovered-facts", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_memory.generations g JOIN agenteam_model.serving_snapshot_retentions r ON r.generation_id=g.generation_id WHERE g.generation_id=$3::uuid AND g.status='retired' AND g.owner_version=2 AND g.retention_state='released' AND r.state='released' AND r.released_version=2),
 (SELECT count(*) FROM agenteam_memory.namespaces n JOIN agenteam_memory.generations g ON g.generation_id=n.serving_generation_id JOIN agenteam_model.serving_snapshot_retentions r ON r.generation_id=g.generation_id WHERE n.project_id=$1::uuid AND n.agent_id=$2::uuid AND g.generation_id<>$3::uuid AND g.status='ready' AND g.schema_version=2 AND g.process_id=$4::uuid AND r.state='retained'),
 (SELECT count(*) FROM agenteam_memory.model_calls WHERE project_id=$1::uuid),
 (SELECT count(*) FROM agenteam_memory.model_calls c JOIN agenteam_model.calls m ON m.id=c.call_id JOIN agenteam_secret.secret_leases l ON l.id=m.lease_id JOIN agenteam_model.invocations i ON i.id=m.invocation_id WHERE c.project_id=$1::uuid AND c.retired AND m.retired AND l.released AND i.final_status='succeeded' AND i.dispatch='sent'),
 (SELECT count(*) FROM agenteam_memory.operations WHERE project_id=$1::uuid AND phase='committed'),
 (SELECT count(*) FROM agenteam_memory.memories WHERE project_id=$1::uuid AND status='active' AND current_revision=1),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1::uuid AND action='memory.retain')`, v.a.String(), agent.String(), generation, second.process).Scan(&released, &ready, &modelCalls, &closed, &operations, &memories, &audits))
	if released != 1 || ready != 1 || modelCalls != 5 || closed != 5 || operations != 1 || memories != 1 || audits != 1 {
		t.Fatalf("Memory recovery counts released=%d ready=%d models=%d retired=%d operations=%d memories=%d audits=%d", released, ready, modelCalls, closed, operations, memories, audits)
	}
	stage = "second-child-join"
	memoryM3StopRecoveryChild(t, v, second)
	peer := &m1Workflow{app: v, network: network}
	state := peer.joinedPeer(t, agentScenario, 4, 4)
	peer.joinedPeer(t, memoryScenario, 2, 2)
	peer.joinedPeer(t, embeddingScenario, 3, 3)
	if !strings.Contains(memoryM3LastTool(t, state.Requests[1].Body), `"added"`) || !strings.Contains(memoryM3LastTool(t, state.Requests[3].Body), memoryM3Fact) {
		t.Fatal("recovered Agent did not consume the original receipt and rebuilt recall")
	}
	t.Log("MEMORY_COLD_RECOVERY actual_sigkill_wait=true old_model_calls_retired=true original_retain_lookup=true stopped_process_proof=true old_build_released=true next_execution_rebuilt=true no_replayed_model_call=true original_second_app_joined=true external_model=false")
}

type memoryM3RecoveryChild struct {
	*fixtureProcess
	process    string
	pending    []map[string]any
	waitLogged sync.Once
}

func memoryM3StartRecoveryChild(t *testing.T, v *executionSchedulerApp, environment []string, mode string) *memoryM3RecoveryChild {
	t.Helper()
	executable, err := os.Executable()
	workAppCheck(t, "memory-cold-executable", err)
	address, err := url.Parse(v.address)
	workAppCheck(t, "memory-cold-listener", err)
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, "AGENTEAM_") {
			env = append(env, entry)
		}
	}
	for _, entry := range environment {
		if strings.HasPrefix(entry, "AGENTEAM_CENTRAL_HTTP_ADDR=") {
			entry = "AGENTEAM_CENTRAL_HTTP_ADDR=" + address.Host
		}
		env = append(env, entry)
	}
	env = append(env, pgfixture.Env+"="+os.Getenv(pgfixture.Env), objectfixture.Env+"="+os.Getenv(objectfixture.Env), netfixture.Env+"="+os.Getenv(netfixture.Env), "AGENTEAM_MEMORY_RECOVERY_CHILD="+mode, "AGENTEAM_MEMORY_RECOVERY_DATABASE="+v.root.db.Name)
	p := &memoryM3RecoveryChild{fixtureProcess: &fixtureProcess{cmd: exec.Command(executable, "-test.run=^TestMemoryM3RecoveryChild$", "-test.timeout=80s"), stdout: newEventLog(), stderr: newEventLog(), done: make(chan struct{})}}
	p.cmd.Dir = t.TempDir()
	p.cmd.Env = env
	p.cmd.Stdout = p.stdout
	p.cmd.Stderr = p.stderr
	workAppCheck(t, "memory-cold-child-start", p.cmd.Start())
	t.Logf("MEMORY_CHILD_START mode=%s pid=%d", mode, p.cmd.Process.Pid)
	go func() { p.err = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		select {
		case <-p.done:
		default:
			_ = p.cmd.Process.Kill()
			usageAppWait(t, p.done, "memory-cold-child-cleanup")
		}
		memoryM3RecoveryWait(t, p)
	})
	ready := memoryM3RecoveryEvent(t, p, "memory_recovery_ready")
	p.process, _ = ready["process_id"].(string)
	_, err = f.ParseID[oc.Process](p.process)
	workAppCheck(t, "memory-cold-child-process", err)
	return p
}
func memoryM3RecoveryEvent(t *testing.T, p *memoryM3RecoveryChild, name string) map[string]any {
	t.Helper()
	accept := func(event map[string]any) bool {
		if event["event"] == "memory_recovery_failure" {
			code, _ := event["code"].(string)
			t.Fatalf("Memory child failed fault=%s", f.Code(code).Safe())
		}
		return event["event"] == name
	}
	for n, event := range p.pending {
		if accept(event) {
			p.pending = append(p.pending[:n], p.pending[n+1:]...)
			return event
		}
	}
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-p.stdout.events:
			if accept(event) {
				return event
			}
			p.pending = append(p.pending, event)
		case <-p.done:
			memoryM3RecoveryWait(t, p)
			for {
				select {
				case event := <-p.stdout.events:
					if accept(event) {
						return event
					}
					p.pending = append(p.pending, event)
				default:
					t.Fatalf("Memory child exited before stage=%s", name)
				}
			}
		case <-timer.C:
			t.Fatalf("Memory child did not reach stage=%s", name)
		}
	}
}
func memoryM3RecoveryWait(t *testing.T, p *memoryM3RecoveryChild) {
	t.Helper()
	select {
	case <-p.done:
		p.waitLogged.Do(func() {
			code := -1
			signal := 0
			if p.cmd.ProcessState != nil {
				code = p.cmd.ProcessState.ExitCode()
				if status, ok := p.cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
					signal = int(status.Signal())
				}
			}
			t.Logf("MEMORY_CHILD_WAIT pid=%d actual_wait=true exit=%d signal=%d", p.cmd.Process.Pid, code, signal)
		})
	default:
		t.Fatal("Memory child has no original Wait")
	}
}
func memoryM3RecoveryGuard(t *testing.T, v *executionSchedulerApp, p *memoryM3RecoveryChild, stopped bool) {
	t.Helper()
	claim, err := os.OpenFile(filepath.Join(v.root.cfg.Objects().SpoolDirectory()+".processes", p.process+".claim"), os.O_RDWR, 0)
	workAppCheck(t, "memory-cold-claim", err)
	defer claim.Close()
	err = syscall.Flock(int(claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if !stopped {
		if err == nil {
			_ = syscall.Flock(int(claim.Fd()), syscall.LOCK_UN)
			t.Fatal("old Memory process no longer held its original flock")
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			t.Fatal("Memory flock observation did not prove a live owner")
		}
		return
	}
	workAppCheck(t, "memory-cold-released-flock", err)
	workAppCheck(t, "memory-cold-flock-unlock", syscall.Flock(int(claim.Fd()), syscall.LOCK_UN))
}
func memoryM3KillRecoveryChild(t *testing.T, v *executionSchedulerApp, p *memoryM3RecoveryChild) {
	t.Helper()
	workAppCheck(t, "memory-cold-exact-sigkill", p.cmd.Process.Kill())
	usageAppWait(t, p.done, "memory-cold-killed-wait")
	memoryM3RecoveryWait(t, p)
	var exit *exec.ExitError
	if !errors.As(p.err, &exit) {
		t.Fatal("old Memory child did not exit from SIGKILL")
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatal("old Memory child death was not the original SIGKILL")
	}
	memoryM3RecoveryGuard(t, v, p, true)
}
func memoryM3StopRecoveryChild(t *testing.T, v *executionSchedulerApp, p *memoryM3RecoveryChild) {
	t.Helper()
	workAppCheck(t, "memory-cold-stop-child", p.cmd.Process.Signal(syscall.SIGTERM))
	event := memoryM3RecoveryEvent(t, p, "memory_recovery_returned")
	usageAppWait(t, p.done, "memory-cold-stopped-wait")
	memoryM3RecoveryWait(t, p)
	if p.err != nil || event["joined"] != true {
		t.Fatal("recovered Memory App did not join its original owners")
	}
	memoryM3RecoveryGuard(t, v, p, true)
	ctx, cancel := context.WithTimeout(v.ctx, 2*time.Second)
	defer cancel()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		var stopped bool
		var connections int
		err := v.admin.QueryRow(ctx, `SELECT coalesce((SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1::uuid),false),(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam')`, p.process).Scan(&stopped, &connections)
		workAppCheck(t, "memory-cold-original-exit-facts", err)
		if stopped && connections == 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Memory recovered exit stopped=%t connections=%d", stopped, connections)
		case <-tick.C:
		}
	}
}

type memoryM3RecoveryStore struct {
	*postgres.Store
	hold        bool
	stoppedMode string
	held        atomic.Bool
}

func (s *memoryM3RecoveryStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	d := cause.Details()
	if s.hold && d.Kind == f.RecoveryCause && d.Owner == "memory" && d.CheckpointRef == "index-publish" && s.held.CompareAndSwap(false, true) {
		var generation, process string
		var retired bool
		err := s.Store.QueryRow(ctx, `SELECT g.generation_id::text,g.process_id::text,c.retired AND m.retired AND l.released FROM agenteam_memory.generations g JOIN agenteam_memory.model_calls c ON c.call_id=g.source_call_id JOIN agenteam_model.calls m ON m.id=c.call_id JOIN agenteam_secret.secret_leases l ON l.id=m.lease_id JOIN agenteam_model.serving_snapshot_retentions r ON r.generation_id=g.generation_id WHERE g.status='building' AND g.retention_state='retained' AND r.state='retained' AND NOT EXISTS(SELECT 1 FROM agenteam_memory.namespaces n WHERE n.serving_generation_id=g.generation_id)`).Scan(&generation, &process, &retired)
		if err != nil || !retired {
			memoryM3RecoveryFailure("publish-barrier", err)
			select {}
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "memory_build_held", "generation_id": generation, "process_id": process, "retired": retired})
		// No transaction has started here. Only the original child death unblocks
		// the parent; cancellation does not fabricate a completed owner.
		select {}
	}
	result := s.Store.WithinTx(ctx, cause, fn)
	s.observeStoppedRetain(ctx, cause, result)
	if d.Kind == f.RecoveryCause && d.Owner == "memory" && d.CheckpointRef == "generation-recover" {
		if result.State() != f.Committed {
			memoryM3RecoveryFailure("recovery-release", result.Fault())
		} else {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "memory_build_recovered"})
		}
	}
	return result
}
func memoryM3RecoveryFailure(stage string, err error) {
	event := map[string]any{"event": "memory_recovery_failure", "stage": stage}
	var fault *f.Fault
	if errors.As(err, &fault) {
		event["code"] = fault.Code.Safe()
		event["commit_state"] = fault.CommitState.Safe()
	}
	_ = json.NewEncoder(os.Stdout).Encode(event)
}
func memoryM3RecoveryCheck(t *testing.T, stage string, err error) {
	t.Helper()
	if err != nil {
		memoryM3RecoveryFailure(stage, err)
		t.Fatalf("Memory recovery child stage=%s failed", stage)
	}
}
func TestMemoryM3RecoveryChild(t *testing.T) {
	mode := os.Getenv("AGENTEAM_MEMORY_RECOVERY_CHILD")
	if mode == "" {
		return
	}
	if mode != "hold-publish" && mode != "observe" && mode != "hold-model-authorized" && mode != "hold-resolved" {
		t.Fatal("unknown owned Memory child mode")
	}
	fixture, err := pgfixture.Load()
	memoryM3RecoveryCheck(t, "database-fixture", err)
	_, err = fixture.Config(os.Getenv("AGENTEAM_MEMORY_RECOVERY_DATABASE"), nil)
	memoryM3RecoveryCheck(t, "database-config", err)
	network, err := netfixture.Load()
	memoryM3RecoveryCheck(t, "network-fixture", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	memoryM3RecoveryCheck(t, "network-address", err)
	env := os.Environ()
	values := map[string]string{}
	for _, entry := range env {
		k, v, _ := strings.Cut(entry, "=")
		values[k] = v
	}
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := values[k]; return v, ok }, env)
	memoryM3RecoveryCheck(t, "config", err)
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	var owned *resources
	store := &memoryM3RecoveryStore{hold: mode == "hold-publish", stoppedMode: mode}
	deps := dependencies{open: func(ctx context.Context, cfg postgres.Config) (database, error) {
		var e error
		store.Store, e = postgres.Open(ctx, cfg)
		return store, e
	}, bind: func(ctx context.Context, cfg config.Config, db database, actual *resources, d *dependencies) error {
		owned = actual
		return bindAccounts(ctx, cfg, db, actual, d)
	}, outboundConstruct: func(_ context.Context, _ config.Config, actual *outboundRuntime) error {
		actual.resolver = appFixtureResolver{address}
		return nil
	}}
	logs := newEventLog()
	logger, err := logging.New(logging.Central, slog.LevelInfo, io.MultiWriter(os.Stderr, logs))
	memoryM3RecoveryCheck(t, "logger", err)
	done := make(chan struct{})
	var runErr error
	go func() { runErr = run(context.Background(), cfg, logger, signals, deps); close(done) }()
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
			memoryM3RecoveryFailure("startup-return", runErr)
			t.Fatal("Memory child returned before listening")
		case <-startup.C:
			memoryM3RecoveryCheck(t, "startup-timeout", context.DeadlineExceeded)
		}
	}
	objects, ok := owned.objects().(*objectAssembly)
	if !ok || objects == nil {
		memoryM3RecoveryCheck(t, "guard", f.NewFault(f.DependencyUnbound, f.NotStarted))
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "memory_recovery_ready", "process_id": objects.process.String()})
	<-done
	memoryM3RecoveryCheck(t, "root-return", runErr)
	joined := owned.producersJoined()
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "memory_recovery_returned", "joined": joined})
	if !joined {
		t.Fatal("Memory child returned before original owners joined")
	}
}
