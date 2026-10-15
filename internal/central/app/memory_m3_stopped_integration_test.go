//go:build integration && linux

package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"slices"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestMemoryM3StoppedConfiguration(t *testing.T) {
	TestMemoryM3AppConfiguration(t)
	source, err := postgres.EmbeddedSource()
	memoryM3StoppedCheck(t, "embedded-source", err)
	if source.Target() < 132 {
		t.Fatal("Memory stopped fixture needs the real continuous source through 132")
	}
}

// Both barriers stop the actual original child after a committed production
// transaction. No fixture writes a call, operation, lease or recovery result.
func TestMemoryM3StoppedRetainRecovery(t *testing.T) {
	for _, mode := range []string{"hold-model-authorized", "hold-resolved"} {
		t.Run(mode, func(t *testing.T) { memoryM3StoppedRetain(t, mode) })
	}
}
func memoryM3StoppedRetain(t *testing.T, mode string) {
	TestMemoryM3StoppedConfiguration(t)
	network, err := netfixture.Load()
	memoryM3StoppedCheck(t, "network", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	memoryM3StoppedCheck(t, "peer-address", err)
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
		t.Fatal("original outbound missing")
	}
	m1AllowOwnedPeer(t, v, transport, network)
	var rawTool string
	memoryM3StoppedCheck(t, "retain-tool", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key='builtin:retain'`).Scan(&rawTool))
	tool, err := f.ParseID[id.Tool](rawTool)
	memoryM3StoppedCheck(t, "retain-tool-id", err)
	agentPeer, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		memoryM3ToolReply(t, tool, map[string]any{"content": memoryM3Fact}, 1), memoryM3TextReply(t, "The interrupted retain failed; no memory was saved.", 2),
	}})
	memoryM3StoppedCheck(t, "agent-peer", err)
	memoryWire := memoryM3TextReply(t, `{"candidates":[]}`, 1)
	memoryPeer, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", Wire: &memoryWire})
	memoryM3StoppedCheck(t, "memory-peer", err)
	wire := netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{workAppJSON(t, map[string]any{"object": "list", "model": "memory-embedding", "data": []any{map[string]any{"object": "embedding", "index": 0, "embedding": []float64{1, 0}}}, "usage": map[string]any{"prompt_tokens": 10, "total_tokens": 10}})}}
	embeddingPeer, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_embeddings_wire", Wire: &wire})
	memoryM3StoppedCheck(t, "embedding-peer", err)
	model := memoryM3ConfigureModels(t, v, agentPeer, memoryPeer, embeddingPeer)
	agent := guardID[id.Agent](t)
	disabled := false
	intent, err := ac.NewAgentCreate(ac.AgentCreateFields{AgentID: agent, Name: "stopped-memory-agent", Description: "Interrupted retain recovery", Instructions: "Use retain once. Report any tool failure without retrying.", ModelRef: model, ApprovalPolicy: ac.ApprovalAllow, AllowedToolIDs: []id.ToolID{}, AllowedMountIDs: []id.MountID{}, AllowedSecretVariableIDs: []id.ProjectVariableID{}, AddSkillsEnabled: &disabled, InstallSkillEnabled: &disabled, InjectAgentsMD: false})
	memoryM3StoppedCheck(t, "agent-intent", err)
	_, err = v.assembly.agents.service.CreateAgent(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), v.a, intent)
	memoryM3StoppedCheck(t, "agent", err)
	sprint := memoryM3CreateSprint(t, v)
	task := memoryM3CreateTask(t, v, sprint.ID, agent, "Retain the schema migration lesson once.")
	_, err = v.planning.sprints.StartSprint(v.ctx, v.actor, executionSchedulerAppMeta(t, &sprint.Version), v.a, sprint.ID)
	memoryM3StoppedCheck(t, "sprint", err)
	v.runtime.StopAdmission()
	v.requestStop()
	v.store.releaseDirectory()
	usageAppWait(t, v.directory.returned, "stopped-provision-directory")
	usageAppWait(t, v.root.done, "stopped-provision-app")
	memoryM3StoppedCheck(t, "provision-exit", v.root.err)
	if !v.runtime.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("provision App did not join")
	}
	memoryM3GuardReleased(t, v.workAppFixture)
	var count int
	memoryM3StoppedCheck(t, "no-provision-execution", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1`, v.a.String()).Scan(&count))
	if count != 0 {
		t.Fatal("provision escaped directory barrier")
	}
	stage = "original-model-boundary"
	first := memoryM3StartRecoveryChild(t, v, environment, mode)
	held := memoryM3RecoveryEvent(t, first, "memory_stopped_retain_held")
	if held["mode"] != mode || held["process_id"] != first.process {
		t.Fatal("barrier lacks original process")
	}
	var call, operation, execution, lease, before string
	var modelCount int
	memoryM3StoppedCheck(t, "original-tuple", v.admin.QueryRow(v.ctx, `SELECT c.call_id::text,c.operation_id::text,c.execution_id::text,l.id::text,
 jsonb_build_array(c.call_id,c.operation_id,c.project_id,c.agent_id,c.execution_id,c.purpose,c.input_digest,c.process_id,c.resolve_source,c.resolve_binding,o.input_digest,o.prepared_process_id,t.input_data,t.input_digest,t.fingerprint,a.attempt_id,a.process_id,a.backend_request_id,a.ordinal,a.fence)::text,
 (SELECT count(*) FROM agenteam_model.calls m WHERE m.id=c.call_id)
 FROM agenteam_memory.model_calls c JOIN agenteam_memory.operations o ON o.operation_id=c.operation_id
 JOIN agenteam_tool.operations t ON t.operation_id::text=c.operation_id::text JOIN agenteam_tool.attempts a ON a.attempt_id=t.active_attempt_id
 JOIN agenteam_secret.secret_leases l ON l.owner_id=c.call_id AND l.owner_kind='model_call'
 WHERE c.project_id=$1::uuid AND c.process_id=$2::uuid AND NOT c.retired AND c.purpose='memory_extraction'
 AND o.phase='prepared' AND o.prepared_process_id=c.process_id AND NOT l.released
 AND c.resolve_source IS NOT NULL AND c.resolve_binding IS NOT NULL
 AND t.phase='running' AND a.phase='running' AND NOT a.retired AND a.ordinal=1 AND a.fence=1`, v.a.String(), first.process).Scan(&call, &operation, &execution, &lease, &before, &modelCount))
	wantModels := 0
	if mode == "hold-model-authorized" {
		wantModels = 1
	}
	if modelCount != wantModels {
		t.Fatal("barrier is not the original admission window")
	}
	memoryM3RecoveryGuard(t, v, first, false)
	memoryM3KillRecoveryChild(t, v, first)
	stage = "recovered-app"
	second := memoryM3StartRecoveryChild(t, v, environment, "observe")
	if second.process == first.process {
		t.Fatal("recovery reused old process")
	}
	finalExecution := memoryM3AwaitExecution(t, v, task.ID)
	if finalExecution.String() != execution {
		t.Fatal("recovery replaced original execution")
	}
	var after string
	var domainRetired, operationFailed, toolFailed, leaseReleased, modelsSettled bool
	var operations, domainCalls, memories, audits int
	memoryM3StoppedCheck(t, "settled-original", v.admin.QueryRow(v.ctx, `SELECT
 jsonb_build_array(c.call_id,c.operation_id,c.project_id,c.agent_id,c.execution_id,c.purpose,c.input_digest,c.process_id,c.resolve_source,c.resolve_binding,o.input_digest,o.prepared_process_id,t.input_data,t.input_digest,t.fingerprint,a.attempt_id,a.process_id,a.backend_request_id,a.ordinal,a.fence)::text,
 c.retired AND ($4=1 OR c.terminal_version IS NOT NULL),
 o.phase='failed' AND o.failure_code='DEPENDENCY_UNAVAILABLE' AND o.receipt IS NULL AND o.failed_at IS NOT NULL,
 t.phase='failed' AND t.outcome='error' AND a.retired AND r.body_data=convert_to('{"error":{"code":"DEPENDENCY_UNAVAILABLE"}}','UTF8'),l.released,
 (SELECT count(*) FROM agenteam_model.calls m WHERE m.id=c.call_id)=$4 AND NOT EXISTS(SELECT 1 FROM agenteam_model.calls m LEFT JOIN agenteam_model.invocations i ON i.id=m.invocation_id WHERE m.id=c.call_id AND (NOT m.retired OR m.phase<>'unknown' OR i.final_status IS DISTINCT FROM 'unknown'))
 AND NOT EXISTS(SELECT 1 FROM agenteam_model.usage_project_calls u WHERE u.call_id=c.call_id AND u.phase<>'business_closed'),
 (SELECT count(*) FROM agenteam_memory.operations WHERE project_id=c.project_id),
 (SELECT count(*) FROM agenteam_memory.model_calls WHERE project_id=c.project_id),
 (SELECT count(*) FROM agenteam_memory.memories WHERE project_id=c.project_id),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id::text=c.project_id::text AND action='memory.retain')
 FROM agenteam_memory.model_calls c JOIN agenteam_memory.operations o ON o.operation_id=c.operation_id
 JOIN agenteam_tool.operations t ON t.operation_id::text=c.operation_id::text JOIN agenteam_tool.attempts a ON a.attempt_id=t.active_attempt_id
 JOIN agenteam_tool.builtin_results r ON r.operation_id=t.operation_id AND r.attempt_id=a.attempt_id
 JOIN agenteam_secret.secret_leases l ON l.id=$3::uuid
 WHERE c.call_id=$1::uuid AND c.operation_id=$2::uuid`, call, operation, lease, wantModels).Scan(&after, &domainRetired, &operationFailed, &toolFailed, &leaseReleased, &modelsSettled, &operations, &domainCalls, &memories, &audits))
	if before != after || !domainRetired || !operationFailed || !toolFailed || !leaseReleased || !modelsSettled || operations != 1 || domainCalls != 1 || memories != 0 || audits != 0 {
		t.Fatalf("settled identity=%t domain=%t operation=%t tool=%t lease=%t models=%t operations=%d calls=%d memories=%d audits=%d", before == after, domainRetired, operationFailed, toolFailed, leaseReleased, modelsSettled, operations, domainCalls, memories, audits)
	}
	stage = "normal-app-join"
	memoryM3StopRecoveryChild(t, v, second)
	peer := &m1Workflow{app: v, network: network}
	state := peer.joinedPeer(t, agentPeer, 2, 2)
	peer.joinedPeer(t, memoryPeer, 0, 0)
	peer.joinedPeer(t, embeddingPeer, 0, 0)
	if !strings.Contains(memoryM3LastTool(t, state.Requests[1].Body), "DEPENDENCY_UNAVAILABLE") {
		t.Fatal("Agent did not consume original retain failure")
	}
	t.Logf("MEMORY_STOPPED_RETAIN mode=%s original_sigkill_wait=true original_tuple_unchanged=true original_operation_failed=true no_model_resend=true no_memory_written=true secret_released=true app_joined=true external_model=false", mode)
}

func (s *memoryM3RecoveryStore) observeStoppedRetain(ctx context.Context, cause f.TransactionCause, result f.CommitResult) {
	if result.State() != f.Committed || s.held.Load() {
		return
	}
	d := cause.Details()
	if d.Kind != f.CommandsCause {
		return
	}
	query := ""
	switch s.stoppedMode {
	case "hold-model-authorized":
		if d.Primary.Namespace() != "model.usage" || d.Primary.Command() != "call-ledger" {
			return
		}
		query = `SELECT c.process_id::text FROM agenteam_memory.model_calls c JOIN agenteam_model.calls m ON m.id=c.call_id JOIN agenteam_model.runtime_attempts a ON a.id=m.invocation_id WHERE NOT c.retired AND c.purpose='memory_extraction' AND NOT m.retired AND m.phase='running' AND a.dispatch='authorized' AND a.fact_data#>'{Value,final}'='null'::jsonb`
	case "hold-resolved":
		if d.Primary.Namespace() != "model.resolve" {
			return
		}
		query = `SELECT c.process_id::text FROM agenteam_memory.model_calls c JOIN agenteam_model.resolution_preparations r ON r.owner_id=c.call_id AND r.owner_kind='model_call' JOIN agenteam_secret.secret_leases l ON l.owner_id=c.call_id AND l.owner_kind='model_call' WHERE NOT c.retired AND c.purpose='memory_extraction' AND r.phase='committed' AND NOT l.released AND NOT EXISTS(SELECT 1 FROM agenteam_model.calls m WHERE m.id=c.call_id)`
	default:
		return
	}
	var process string
	err := s.Store.QueryRow(ctx, query).Scan(&process)
	if errors.Is(err, pgx.ErrNoRows) {
		return
	}
	if err != nil {
		memoryM3RecoveryFailure("stopped-model-barrier", err)
		select {}
	}
	if s.held.CompareAndSwap(false, true) {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "memory_stopped_retain_held", "mode": s.stoppedMode, "process_id": process})
		select {} // Only the parent's exact SIGKILL ends the original call.
	}
}
func memoryM3StoppedCheck(t *testing.T, stage string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	var pg *pgconn.PgError
	state := ""
	if errors.As(err, &pg) && len(pg.Code) == 5 {
		state = pg.Code
	}
	var fault *f.Fault
	code := ""
	if errors.As(err, &fault) {
		code = string(fault.Code.Safe())
	}
	t.Fatalf("Memory stopped stage=%s fault=%s sqlstate=%s no_rows=%t canceled=%t deadline=%t", stage, code, state, errors.Is(err, pgx.ErrNoRows), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded))
}
