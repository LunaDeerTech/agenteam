//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/config"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

// This is the actual default App and its original DirectText producer. Only
// the remote peer is controlled: owned TLS, original production policy/CA/DNS
// plumbing, real credential use, complete HTTP response and actual close.
// It neither uses an external model account nor exercises ToolsHTTP.
func TestRuntimeFinalTextDefaultApp(t *testing.T) {
	t.Run("original-producer-and-restarted-history", runtimeFinalTextApp)
}

const runtimeFinalTextAnswer = "真实最终输出 · αβ 🙂\n<script>globalThis.__runtime_text_executed = true</script>\n<img src=x onerror=alert(1)>\nMarkdown stays literal: **结果**\n"
const runtimeFinalTextCredential = "owned-runtime-final-text-credential"

type runtimeFinalTextFixture struct {
	*executionSchedulerApp
	env      []string
	task     wc.Task
	network  *netfixture.Descriptor
	scenario string
	view     ec.TaskRuntimeView
}

func newRuntimeFinalTextFixture(t *testing.T, origin string, configure func(*dependencies, *executionSchedulerApp)) *runtimeFinalTextFixture {
	t.Helper()
	network, err := netfixture.Load()
	workAppCheck(t, "runtime-text-owned-network", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	workAppCheck(t, "runtime-text-owned-address", err)
	var env []string
	var transport *outboundRuntime
	v := newExecutionSchedulerAppWith(t, func(d *dependencies, v *executionSchedulerApp) {
		env = append(guardEnvironment(t, v.root.db), "AGENTEAM_CENTRAL_PUBLIC_ORIGIN="+origin, "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s", "AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+network.CAFile)
		v.root.cfg = guardConfiguration(t, append(append([]string(nil), env...), "AGENTEAM_CENTRAL_EXECUTION_RUNTIME="+executionSchedulerAppConfig,
			"AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_ATTEMPTS=2", "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_INITIAL_BACKOFF=100ms", "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_BACKOFF=1s"))
		d.outboundConstruct = func(_ context.Context, _ config.Config, created *outboundRuntime) error {
			created.resolver = appFixtureResolver{address}
			transport = created
			return nil
		}
		if configure != nil {
			configure(d, v)
		}
	})
	if transport == nil || v.root.owned.outbound() != transport {
		t.Fatal("Runtime text did not retain original outbound owner")
	}
	ports, err := outbound.SelectedPorts(8443)
	workAppCheck(t, "runtime-text-owned-port", err)
	rule, err := outbound.NewRule(network.PrivateIP+"/32", ports)
	workAppCheck(t, "runtime-text-owned-rule", err)
	rules, err := outbound.NewRules(rule)
	workAppCheck(t, "runtime-text-owned-rules", err)
	identity, err := f.NewCommandIdentity("outbound-policy-v2", []string{v.actor.Details().UserID}, "update", f.IdempotencyKey(guardID[struct{}](t).String()))
	workAppCheck(t, "runtime-text-policy-identity", err)
	status := transport.policy.Status()
	if status.Version == nil {
		t.Fatal("original outbound policy absent")
	}
	_, err = transport.policy.UpdatePolicy(v.ctx, v.actor, outbound.CommandMeta{Identity: identity, ExpectedVersion: *status.Version, HTTPTraceID: guardID[f.Request](t).String()}, outbound.Allowlist, rules)
	workAppCheck(t, "runtime-text-formal-policy-update", err)
	scope, err := id.InProject(v.a)
	workAppCheck(t, "runtime-text-secret-scope", err)
	command, err := f.NewCommandIdentity("secret", []string{v.a.String(), v.actor.Details().UserID}, "create", f.IdempotencyKey(guardID[struct{}](t).String()))
	workAppCheck(t, "runtime-text-secret-command", err)
	material, err := sc.NewSecretMaterial([]byte(runtimeFinalTextCredential))
	workAppCheck(t, "runtime-text-secret-material", err)
	defer material.Destroy()
	secrets, ok := v.root.owned.secret().(*secret.Service)
	if !ok {
		t.Fatal("original Secret service absent")
	}
	written, err := secrets.ExecuteWrite(v.ctx, sc.WriteRequest{Actor: v.actor, Scope: scope, Identity: command, Kind: sc.Create, Purpose: sc.Model, Value: material})
	workAppCheck(t, "runtime-text-formal-credential", err)
	response, err := json.Marshal(map[string]any{"id": "owned-runtime-text-response", "object": "chat.completion", "created": 1, "model": "runtime-app-model",
		"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": runtimeFinalTextContent()}, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5}})
	workAppCheck(t, "runtime-text-controlled-response", err)
	scenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", Wire: &netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{response}}})
	workAppCheck(t, "runtime-text-owned-scenario", err)
	credential := written.Metadata.CredentialRef.Details().ID
	v.seedWithProvider(t, "https://fixture.test:8443/case/"+scenario, &credential)
	v.originalFacts(t, 1)
	taskID, err := f.ParseID[wc.Task](v.execution.Trigger.TaskID)
	workAppCheck(t, "runtime-text-task-id", err)
	task, err := v.planning.taskReader.GetTask(v.ctx, v.actor, v.a, taskID)
	workAppCheck(t, "runtime-text-original-task", err)
	return &runtimeFinalTextFixture{executionSchedulerApp: v, env: env, task: task, network: network, scenario: scenario}
}

func runtimeFinalTextContent() string {
	return runtimeFinalTextAnswer + strings.Repeat("Unicode 输出行 · αβ 🙂\n", 24) + strings.Repeat("long_output_token_", 32) + "\n完整末尾"
}
func (v *runtimeFinalTextFixture) path() string {
	return taskExecutionReadPath(v.a, v.task.ID.String(), v.execution.ID) + "/runtime-view"
}

func (v *runtimeFinalTextFixture) advance(t *testing.T) {
	t.Helper()
	d := ec.AssociatedDispatch{ExecutionID: v.execution.ID, AgentID: v.agent, Key: v.launchRequest.Meta.IdempotencyKey, Digest: v.digest, DispatchID: v.dispatch.Summary().ID.String()}
	accepted, err := v.runtime.executor.Advance(v.ctx, v.a, d)
	workAppCheck(t, "runtime-text-original-associated-advance", err)
	// Observe durable completion without calling Advance a second time. A
	// terminal record is not treated as the worker's outer return; the original
	// root Stop/join below owns that separate fact.
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var snapshotReady, modelReady, associatedReady bool
		var progress []byte
		err = v.admin.QueryRow(v.ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_execution.runtime_snapshots WHERE execution_id=$1)
 ,(SELECT count(*)=1 AND bool_and(retired AND phase='succeeded') FROM agenteam_model.calls WHERE project_id=$2::uuid)
 ,(SELECT count(*)=1 AND bool_and(phase='business_closed') FROM agenteam_execution.project_calls WHERE project_id=$2::uuid AND lane='associated')
 ,jsonb_build_object(
 'execution',(SELECT jsonb_build_object('status',CASE WHEN status IN ('created','preparing','running','waiting','succeeded','failed','cancelled') THEN status ELSE 'invalid' END,'version',version) FROM agenteam_execution.executions WHERE id=$1),
 'snapshots',(SELECT count(*) FROM agenteam_execution.runtime_snapshots WHERE execution_id=$1),
 'preparation',(SELECT jsonb_build_object('count',count(*),'running',count(*) FILTER(WHERE phase='running'),'terminal',count(*) FILTER(WHERE phase='terminal')) FROM agenteam_execution.preparation_attempts WHERE execution_id=$1),
 'rounds',(SELECT jsonb_build_object('count',count(*),'active',count(*) FILTER(WHERE terminal_status IS NULL),'succeeded',count(*) FILTER(WHERE terminal_status='succeeded'),'failed',count(*) FILTER(WHERE terminal_status='failed'),'cancelled',count(*) FILTER(WHERE terminal_status='cancelled'),
   'reasons',COALESCE(jsonb_agg(CASE WHEN terminal_reason IN ('completed','model_failed','incomplete_response','runtime_failed','cancelled') THEN terminal_reason WHEN terminal_reason IS NULL THEN 'none' ELSE 'invalid' END),'[]'::jsonb)) FROM agenteam_execution.rounds WHERE execution_id=$1),
 'model_calls',(SELECT jsonb_build_object('count',count(*),'retired',count(*) FILTER(WHERE retired),'accepted',count(*) FILTER(WHERE phase='accepted'),'running',count(*) FILTER(WHERE phase='running'),'retry_wait',count(*) FILTER(WHERE phase='retry_wait'),'succeeded',count(*) FILTER(WHERE phase='succeeded'),'failed',count(*) FILTER(WHERE phase='failed'),'cancelled',count(*) FILTER(WHERE phase='cancelled'),'unknown',count(*) FILTER(WHERE phase='unknown')) FROM agenteam_model.calls WHERE project_id=$2::uuid),
 'associated',(SELECT jsonb_build_object('count',count(*),'running',count(*) FILTER(WHERE phase='running'),'closed',count(*) FILTER(WHERE phase='business_closed')) FROM agenteam_execution.project_calls WHERE project_id=$2::uuid AND lane='associated'),
 'model_attempts',(SELECT count(*) FROM agenteam_model.invocations WHERE project_id=$2::uuid AND execution_id=$1::uuid),
 'model_attempt_states',COALESCE((SELECT jsonb_agg(jsonb_build_object(
   'dispatch',CASE WHEN dispatch IN ('reserved','authorized','sent','not_sent','unknown') THEN dispatch ELSE 'invalid' END,
   'final_status',CASE WHEN final_status IN ('succeeded','failed','cancelled','unknown') THEN final_status WHEN final_status IS NULL THEN 'none' ELSE 'invalid' END,
   'category',CASE WHEN error_data IS NULL THEN 'none' WHEN error_data->>'category' IN ('invalid_request','authentication','permission','model_not_found','rate_limited','context_too_large','provider_unavailable','timeout','network','cancelled','content_filter','unsupported_feature','provider_error','unknown') THEN error_data->>'category' ELSE 'invalid' END))
   FROM (SELECT dispatch,final_status,error_data FROM agenteam_model.invocations WHERE project_id=$2::uuid AND execution_id=$1::uuid ORDER BY attempt_index LIMIT 8) attempts),'[]'::jsonb))`, v.execution.ID.String(), v.a.String()).Scan(&snapshotReady, &modelReady, &associatedReady, &progress)
		workAppCheck(t, "runtime-text-persisted-completion", err)
		if snapshotReady && modelReady && associatedReady {
			break
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			phase := "invalid"
			switch accepted.Phase {
			case ec.ExecutionAccepted, ec.ExecutionPreparing, ec.ExecutionStarting, ec.ExecutionRetained, ec.ExecutionDeferred, ec.ExecutionTerminal:
				phase = string(accepted.Phase)
			}
			status := "none"
			if accepted.Status != "" {
				status = "invalid"
				if accepted.Status.Valid() {
					status = string(accepted.Status)
				}
			}
			t.Logf("RUNTIME_TEXT_ADVANCE phase=%s status=%s active=%t retained=%t joined=%t", phase, status, accepted.Active, accepted.Retained, accepted.Joined)
			t.Logf("RUNTIME_TEXT_COMPLETION snapshot=%t model=%t associated=%t", snapshotReady, modelReady, associatedReady)
			// This projection contains only fixed SQL keys, counts and closed
			// enums. Never emit the original rows, IDs, error codes or bodies.
			if len(progress) <= 8192 && json.Valid(progress) {
				t.Logf("RUNTIME_TEXT_PROGRESS %s", progress)
			} else {
				t.Log("RUNTIME_TEXT_PROGRESS invalid_projection=true")
			}
			peer, peerErr := v.network.State(v.ctx, v.scenario)
			if peerErr != nil {
				t.Log("RUNTIME_TEXT_PEER available=false")
			} else {
				closed := 0
				for _, returned := range peer.Closed {
					if returned {
						closed++
					}
				}
				t.Logf("RUNTIME_TEXT_PEER available=true requests=%d active_handlers=%d completed_handlers=%d connections=%d closed_connections=%d", len(peer.Requests), peer.ActiveHandlers, peer.CompletedHandlers, len(peer.Connections), closed)
			}
			t.Fatal("original Runtime final text did not complete")
		}
	}
	v.view = v.read(t, v.workAppFixture)
	v.facts(t)
}

func (v *runtimeFinalTextFixture) read(t *testing.T, app *workAppFixture) ec.TaskRuntimeView {
	t.Helper()
	r := taskExecutionGet(t, app, app.client, app.cookie, "GET", v.path(), 200)
	var view ec.TaskRuntimeView
	workAppCheck(t, "runtime-text-original-response", skillWebDecode(r.body, &view))
	workAppCheck(t, "runtime-text-projection", view.ValidateProjection())
	if view.Execution.ID != v.execution.ID || view.Execution.ProjectID != v.a || view.Execution.TaskID != v.task.ID.String() || view.Execution.AgentID != v.agent || view.Execution.Status != ec.Succeeded || view.Availability != ec.RuntimeAvailable || view.Snapshot == nil || len(view.Snapshot.Items) != 1 || view.Snapshot.Items[0].Data.Content != runtimeFinalTextContent() {
		t.Fatal("original Runtime final text differs")
	}
	return view
}

func (v *runtimeFinalTextFixture) facts(t *testing.T) {
	t.Helper()
	var valid bool
	var snapshot []byte
	err := v.admin.QueryRow(v.ctx, `SELECT e.xmin=r.xmin AND e.xmin=s.xmin AND e.xmin=a.xmin
 AND e.status='succeeded' AND r.terminal_status=e.status AND s.terminal_status=e.status
 AND e.version=r.terminal_version AND e.version=s.terminal_version
 AND e.project_id=s.project_id
 AND e.snapshot_id=r.snapshot_id AND e.snapshot_id=s.snapshot_id AND r.id=s.round_id AND a.round_id=r.id
 AND e.completed_at=r.finished_at AND e.completed_at=s.completed_at
 AND r.schema_version=1 AND NOT r.tool_pending AND a.kind='assistant' AND a.sequence=2
 AND convert_from(a.message,'UTF8')::jsonb=convert_from(r.response,'UTF8')::jsonb->'message'
 AND s.digest='sha256:'||encode(sha256(s.content),'hex')
 AND EXISTS(SELECT 1 FROM agenteam_model.invocations m WHERE m.execution_id::text=e.id AND m.project_id::text=e.project_id AND m.final_status='succeeded'),
 s.content FROM agenteam_execution.executions e
 JOIN agenteam_execution.rounds r ON r.execution_id=e.id
 JOIN agenteam_execution.runtime_snapshots s ON s.execution_id=e.id
 JOIN agenteam_execution.transcript_entries a ON a.execution_id=e.id AND a.sequence=2 WHERE e.id=$1`, v.execution.ID.String()).Scan(&valid, &snapshot)
	workAppCheck(t, "runtime-text-original-terminal-transaction", err)
	var durable ec.RuntimeSnapshot
	workAppCheck(t, "runtime-text-original-snapshot", skillWebDecode(snapshot, &durable))
	if !valid || !reflect.DeepEqual(&durable, v.view.Snapshot) {
		t.Fatal("Runtime projection lost original same-transaction terminal provenance")
	}
	var models, tools, preparations int
	err = v.admin.QueryRow(v.ctx, `SELECT (SELECT count(*) FROM agenteam_model.invocations WHERE project_id=$1::uuid AND execution_id=$2::uuid),
 (SELECT count(*) FROM agenteam_tool.operations),(SELECT count(*) FROM agenteam_execution.preparation_attempts WHERE execution_id=$2::text)`, v.a.String(), v.execution.ID.String()).Scan(&models, &tools, &preparations)
	workAppCheck(t, "runtime-text-real-invocation-counts", err)
	if models != 1 || tools != 0 || preparations != 1 {
		t.Fatal("Runtime text did not use one original preparation and Model call")
	}
	current, err := v.planning.taskReader.GetTask(v.ctx, v.actor, v.a, v.task.ID)
	workAppCheck(t, "runtime-text-current-task", err)
	if current.State.Terminal() || current.State != v.task.State || current.Version != v.task.Version {
		t.Fatal("Execution success changed Task state or version")
	}
	state, err := v.network.State(v.ctx, v.scenario)
	workAppCheck(t, "runtime-text-owned-peer-facts", err)
	if len(state.Requests) != 1 || state.ActiveHandlers != 0 || state.CompletedHandlers != 1 {
		t.Fatal("controlled Model HTTP did not return exactly once")
	}
	request := state.Requests[0]
	if request.Method != "POST" || request.Host != "fixture.test:8443" || request.Path != "/case/"+v.scenario+"/chat/completions" || request.Query != "" || request.Headers.Get("Authorization") != "Bearer "+runtimeFinalTextCredential {
		t.Fatal("original Model wire identity or credential use differs")
	}
	var body struct {
		Model    string            `json:"model"`
		Stream   bool              `json:"stream"`
		Tools    []json.RawMessage `json:"tools"`
		Messages []json.RawMessage `json:"messages"`
	}
	workAppCheck(t, "runtime-text-original-model-request", json.Unmarshal([]byte(request.Body), &body))
	if body.Model != "runtime-app-model" || body.Stream || len(body.Tools) != 0 || len(body.Messages) == 0 {
		t.Fatal("original Model request did not retain DirectText profile")
	}
}

func runtimeFinalTextApp(t *testing.T) {
	v := newRuntimeFinalTextFixture(t, "http://localhost:8080", nil)
	pending := taskExecutionGet(t, v.workAppFixture, v.client, v.cookie, "GET", v.path(), 200)
	var initial ec.TaskRuntimeView
	workAppCheck(t, "runtime-text-pending-decode", skillWebDecode(pending.body, &initial))
	workAppCheck(t, "runtime-text-pending-projection", initial.ValidateProjection())
	if initial.Availability != ec.RuntimePending || initial.Snapshot != nil || !reflect.DeepEqual(initial.Execution, taskExecutionExpected(v.executionSchedulerApp)) {
		t.Fatal("unstarted Runtime did not remain pending")
	}
	v.advance(t)
	first := taskExecutionGet(t, v.workAppFixture, v.client, v.cookie, "GET", v.path(), 200)
	head := taskExecutionGet(t, v.workAppFixture, v.client, v.cookie, "HEAD", v.path(), 200)
	if len(head.body) != 0 || head.header.Get("Content-Length") != strconv.Itoa(len(first.body)) {
		t.Fatal("Runtime HEAD differs from complete original GET")
	}
	foreign := artifactOwnerHTTPForeignClient(t, &humanArtifactApp{workAppFixture: v.workAppFixture})
	taskExecutionRecoverForeignInvitation(t, v.workAppFixture, "artifact-reader", "artifact-reader@example.com")
	taskExecutionGet(t, v.workAppFixture, foreign, "", "GET", v.path(), 404)
	taskExecutionGet(t, v.workAppFixture, v.client, "", "GET", v.path(), 401)
	taskExecutionGet(t, v.workAppFixture, v.client, v.cookie, "GET", taskExecutionReadPath(v.b, v.task.ID.String(), v.execution.ID)+"/runtime-view", 404)
	taskExecutionGet(t, v.workAppFixture, v.client, v.cookie, "GET", taskExecutionReadPath(v.a, guardID[wc.Task](t).String(), v.execution.ID)+"/runtime-view", 404)
	taskExecutionReadClosed(t, v.workAppFixture, v.objects.process.String())
	before := taskExecutionFacts(t, v.workAppFixture)
	taskExecutionJoinEnabled(t, v.executionSchedulerApp)
	second := taskExecutionDisabled(t, v.executionSchedulerApp, v.env)
	restarted := v.read(t, second)
	if !reflect.DeepEqual(restarted, v.view) {
		t.Fatal("same-database restart changed final Runtime identity or text")
	}
	secondBody := taskExecutionGet(t, second, second.client, second.cookie, "GET", v.path(), 200)
	taskExecutionReadClosed(t, second, second.objects.process.String())
	if !bytes.Equal(first.body, secondBody.body) || taskExecutionFacts(t, second) != before {
		t.Fatal("history restart changed original bytes or invoked another model")
	}
	second.requestStop()
	usageAppWait(t, second.root.done, "runtime-text-history-root-return")
	workAppCheck(t, "runtime-text-history-root", second.root.err)
	if !second.assembly.executionReads.Joined() || !second.assembly.Joined() || !second.root.owned.producersJoined() {
		t.Fatal("Runtime history owners did not join")
	}
	taskExecutionGuardReleased(t, second)
	v.peerClosed(t)
	t.Log("RUNTIME_FINAL_TEXT_APP default_app=true original_coordinator_associated_oneround=true controlled_model_http_once=true terminal_same_tx=true complete_text=true owner_scope=true same_db_disabled_restart=true original_roots_guard_store_joined=true external_model_not_tested=true tools_http_not_tested=true")
}

func (v *runtimeFinalTextFixture) peerClosed(t *testing.T) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		state, err := v.network.State(v.ctx, v.scenario)
		workAppCheck(t, "runtime-text-final-peer-state", err)
		if len(state.Requests) != 1 {
			t.Fatal("controlled Model peer observed another invocation")
		}
		if state.ActiveHandlers == 0 && state.CompletedHandlers == 1 && state.Closed[state.Requests[0].Connection] {
			return
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("original controlled Model connection not joined")
		}
	}
}
