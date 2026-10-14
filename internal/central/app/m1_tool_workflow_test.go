//go:build integration && linux

package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

const m1FinalOutput = "已完成统计：count=4, sum=20, mean=5。文件：stats.py、result.json。校验命令退出0。"
const m1Result = "{\"count\":4,\"sum\":20,\"mean\":5}\n"
const m1Program = `import json
from pathlib import Path
values = json.loads(Path("input.json").read_text())["values"]
result = {"count": len(values), "sum": sum(values), "mean": sum(values) // len(values)}
assert result == {"count": 4, "sum": 20, "mean": 5}
text = json.dumps(result, separators=(",", ":")) + "\n"
Path("result.json").write_text(text)
print(text, end="")
`

// This exercises the deployment tool-loop-v1 graph. The original Scheduler
// discovers and claims the Task; the original Associated owner runs every
// round. Only the model peer is deterministic. Runner and filesystem effects,
// Model HTTP, durable results, final projection and Owner review are real.
func TestM1AppToolWorkflow(t *testing.T) {
	fixture, listener := newM1Workflow(t)
	v := fixture.app
	fixture.seed(t)
	fixture.startRunner(t)
	fixture.createModelAndTask(t, "https://fixture.test:8443/m1-not-yet-scheduled")
	// Release only a completed empty directory observation; there is no direct
	// ClaimTask, LaunchOnce, Advance, driver Start or synthetic Execution row.
	v.store.releaseDirectory()
	fixture.awaitResult(t)
	fixture.checkArtifacts(t)
	fixture.browser(t, listener)
	// Owner review may legitimately have started a new task/review Execution.
	// Stop and join the original Runtime while its Runner/Store remain alive;
	// only then stop the Runner and the rest of the App.
	v.runtime.StopAdmission()
	drainContext, cancelDrain := context.WithTimeout(context.Background(), 5*time.Second)
	drainErr := v.runtime.Drain(drainContext)
	cancelDrain()
	workAppCheck(t, "m1-original-runtime-drain", drainErr)
	fixture.stopRunner(t)
	v.requestStop()
	usageAppWait(t, v.directory.returned, "m1-manager-original-return")
	usageAppWait(t, v.root.done, "m1-original-app-return")
	workAppCheck(t, "m1-original-app-exit", v.root.err)
	if !v.runtime.Joined() || !v.runtime.executionCalls.Joined() || !v.runtime.schedulerCalls.Joined() || !v.planning.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("M1 App returned before its original owners joined")
	}
	taskExecutionGuardReleased(t, v.workAppFixture)
	fixture.checkPeer(t)
	t.Log("M1_APP_TOOL_WORKFLOW deployment_profile=tool-loop-v1 scheduler_claim=true model_http_rounds=5 runner_read_write_run=true actual_file_stdout_exit=true final_output_v2=true owner_review=true original_app_runner_wait=true external_model=false")
}

func newM1Workflow(t *testing.T) (*m1Workflow, net.Listener) {
	t.Helper()
	return newM1WorkflowWithOptions(t, nil, true)
}

func newM1WorkflowWithObserver(t *testing.T, observer func(f.TransactionCause, f.CommitResult)) (*m1Workflow, net.Listener) {
	t.Helper()
	return newM1WorkflowWithOptions(t, observer, false)
}

func newM1WorkflowWithOptions(t *testing.T, observer func(f.TransactionCause, f.CommitResult), ownerReview bool) (*m1Workflow, net.Listener) {
	t.Helper()
	return newM1WorkflowConfigured(t, observer, ownerReview, nil)
}

func newM1WorkflowConfigured(t *testing.T, observer func(f.TransactionCause, f.CommitResult), ownerReview bool, live *m1LiveConfiguration) (*m1Workflow, net.Listener) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	workAppCheck(t, "m1-listener", err)
	t.Cleanup(func() { _ = listener.Close() })
	origin := "http://" + listener.Addr().String()
	var network *netfixture.Descriptor
	var address netip.Addr
	if live == nil {
		network, err = netfixture.Load()
		workAppCheck(t, "m1-owned-network", err)
		address, err = netip.ParseAddr(network.PrivateIP)
		workAppCheck(t, "m1-owned-address", err)
	}
	fixture := &m1Workflow{network: network, origin: origin, private: t.TempDir(), reviewEnabled: ownerReview, live: live}
	var transport *outboundRuntime
	v := newExecutionSchedulerAppWith(t, func(d *dependencies, v *executionSchedulerApp) {
		diagnose := m1CommitObserver(t)
		v.store.observeCommit = func(cause f.TransactionCause, result f.CommitResult) {
			diagnose(cause, result)
			if observer != nil {
				observer(cause, result)
			}
		}
		runtimeConfig := strings.Replace(executionSchedulerAppConfig, "direct-text-v1", "tool-loop-v1", 1)
		if ownerReview {
			// An explicit test deployment choice: 240 skipped visits at the
			// existing 250ms tick gives a bounded Owner viewing window. This
			// is neither a production default nor a Scheduler pause.
			runtimeConfig = strings.Replace(runtimeConfig, `"relaunch_skip_count":0`, `"relaunch_skip_count":240`, 1)
		}
		if live != nil {
			runtimeConfig = strings.Replace(runtimeConfig, `"max_request_timeout":"120s"`, `"max_request_timeout":"60s"`, 1)
			// The observation lifetime is separate from the original App run.
			// The live budget owner below stops that original run explicitly.
			var cancel context.CancelFunc
			v.ctx, cancel = context.WithTimeout(context.Background(), m1LiveTotalBudget+m1LiveJoinBudget)
			t.Cleanup(cancel)
		}
		env := append(guardEnvironment(t, v.root.db), "AGENTEAM_CENTRAL_PUBLIC_ORIGIN="+origin,
			"AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s",
			"AGENTEAM_CENTRAL_EXECUTION_RUNTIME="+runtimeConfig,
			"AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_ATTEMPTS=2", "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_INITIAL_BACKOFF=100ms", "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_BACKOFF=1s")
		if live == nil {
			env = append(env, "AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+network.CAFile)
		}
		v.root.cfg = guardConfiguration(t, env)
		d.outboundConstruct = func(_ context.Context, _ config.Config, actual *outboundRuntime) error {
			if live == nil {
				actual.resolver = appFixtureResolver{address}
			} else if actual.resolver != nil {
				return fmt.Errorf("live outbound resolver must remain the original system resolver")
			}
			transport = actual
			return nil
		}
		if ownerReview {
			fixture.bindBrowserObservation(d)
		}
	})
	if transport == nil || v.root.owned.outbound() != transport {
		t.Fatal("M1 original outbound owner absent")
	}
	if live == nil {
		m1AllowOwnedPeer(t, v, transport, network)
	}
	fixture.app = v
	return fixture, listener
}

type m1Workflow struct {
	runners                                    *runnerControlAssembly
	app                                        *executionSchedulerApp
	network                                    *netfixture.Descriptor
	origin, private, scenario, root, workspace string
	tools                                      map[string]id.ToolID
	mount                                      id.MountID
	runnerID                                   rc.RunnerID
	runnerToken                                string
	runner                                     *m1RunnerProcess
	task                                       wc.Task
	reviewer                                   id.AgentID
	execution                                  id.ExecutionID
	view                                       ec.TaskRuntimeView
	responseSequence                           func(*testing.T) []netfixture.WireScenario
	reviewEnabled                              bool
	reviewScenario, reviewWorkspace            string
	reviewMount                                id.MountID
	reviewModel                                mc.ModelID
	browserObservation                         *m1BrowserObservation
	live                                       *m1LiveConfiguration
}

func m1AllowOwnedPeer(t *testing.T, v *executionSchedulerApp, transport *outboundRuntime, network *netfixture.Descriptor) {
	t.Helper()
	ports, err := outbound.SelectedPorts(8443)
	workAppCheck(t, "m1-port", err)
	rule, err := outbound.NewRule(network.PrivateIP+"/32", ports, false)
	workAppCheck(t, "m1-rule", err)
	rules, err := outbound.NewRules(rule)
	workAppCheck(t, "m1-rules", err)
	identity, err := f.NewCommandIdentity("outbound-policy", []string{v.actor.Details().UserID}, "update", f.IdempotencyKey(guardID[struct{}](t).String()))
	workAppCheck(t, "m1-policy-command", err)
	before := transport.policy.Status()
	if before.Version == nil {
		t.Fatal("M1 original outbound policy absent")
	}
	_, err = transport.policy.UpdatePolicy(v.ctx, v.actor, outbound.CommandMeta{Identity: identity, ExpectedVersion: *before.Version, HTTPTraceID: guardID[f.Request](t).String()}, rules)
	workAppCheck(t, "m1-formal-policy-update", err)
}

func (w *m1Workflow) seed(t *testing.T) {
	t.Helper()
	v := w.app
	w.tools = make(map[string]id.ToolID)
	for _, key := range []string{tc.RunnerReadFileKey, tc.RunnerWriteFileKey, tc.RunnerRunCommandKey} {
		var raw string
		workAppCheck(t, "m1-real-tool-registration", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key=$1`, key).Scan(&raw))
		tool, err := f.ParseID[id.Tool](raw)
		workAppCheck(t, "m1-tool-id", err)
		w.tools[key] = tool
	}
	w.root = filepath.Join(w.private, "runner-root")
	workAppCheck(t, "m1-owned-runner-root", os.Mkdir(w.root, 0700))
	create, err := rc.NewCreate(rc.CreateRequest{RunnerID: guardID[rc.Runner](t), Name: "M1 owned Runner", Description: "Temporary statistics workspace", Tags: []string{}, RootPath: w.root})
	workAppCheck(t, "m1-runner-intent", err)
	runners, ok := v.assembly.runners.(*runnerControlAssembly)
	if !ok || runners == nil {
		t.Fatal("M1 original Runner service absent")
	}
	w.runners = runners
	result, err := runners.service.Execute(v.ctx, v.actor, f.IdempotencyKey(guardID[struct{}](t).String()), create)
	workAppCheck(t, "m1-formal-runner-create", err)
	if result.Material == nil {
		t.Fatal("M1 Runner enrollment material absent")
	}
	w.runnerID = result.Receipt.Runner.ID
	w.runnerToken, err = result.Material.Token.Wire()
	workAppCheck(t, "m1-enrollment-material", err)
	result.Material = nil
}

func (w *m1Workflow) createModelAndTask(t *testing.T, endpoint string) {
	t.Helper()
	v := w.app
	scope, err := id.InProject(v.a)
	workAppCheck(t, "m1-secret-scope", err)
	identity, err := f.NewCommandIdentity("secret", []string{v.a.String(), v.actor.Details().UserID}, "create", f.IdempotencyKey(guardID[struct{}](t).String()))
	workAppCheck(t, "m1-secret-command", err)
	secretBytes := []byte("owned-m1-provider-credential")
	if w.live != nil {
		secretBytes = w.live.credential(t)
	}
	material, err := sc.NewSecretMaterial(secretBytes)
	clear(secretBytes)
	workAppCheck(t, "m1-secret-material", err)
	defer material.Destroy()
	service, ok := v.root.owned.secret().(*secret.Service)
	if !ok {
		t.Fatal("M1 original Secret absent")
	}
	written, err := service.ExecuteWrite(v.ctx, sc.WriteRequest{Actor: v.actor, Scope: scope, Identity: identity, Kind: sc.Create, Purpose: sc.Model, Value: material})
	workAppCheck(t, "m1-secret-write", err)
	credential := written.Metadata.CredentialRef.Details().ID
	modelName, reviewName := "m1-statistics", "m1-review"
	var maxOutput any
	if w.live != nil {
		modelName, reviewName, maxOutput = w.live.Model, w.live.Model, m1LiveMaxOutput
	}
	var receipt mc.CommandReceipt
	v.httpSeed(t, "/model-providers", map[string]any{"input": map[string]any{"name": "M1 owned Provider", "protocol": mc.OpenAIChat, "base_url": endpoint, "enabled": true, "credential_ref": credential, "options": map[string]any{}}}, &receipt)
	provider, err := f.ParseID[mc.Provider](receipt.ResourceID)
	workAppCheck(t, "m1-provider", err)
	providerVersion := receipt.Version
	v.httpSeed(t, "/models", map[string]any{"provider_id": provider, "input": map[string]any{"name": "M1 model", "provider_model_id": modelName, "type": mc.ChatModel, "enabled": true, "parameters": map[string]any{}, "request_overwrite": map[string]any{}, "header_overwrite": map[string]string{}, "capabilities": map[string]any{"tool_calls": true, "parallel_tool_calls": false, "streaming": true, "reasoning": false, "input_modalities": []string{"text"}, "output_modalities": []string{"text"}, "reasoning_efforts": []string{}, "structured_output_modes": []string{}, "context_length": nil, "max_output": maxOutput}}}, &receipt)
	modelID, err := f.ParseID[mc.Model](receipt.ResourceID)
	workAppCheck(t, "m1-model", err)
	var reviewProvider mc.ProviderID
	reviewVersion := providerVersion
	if w.reviewEnabled {
		v.httpSeed(t, "/model-providers", map[string]any{"input": map[string]any{"name": "M1 review Provider", "protocol": mc.OpenAIChat, "base_url": endpoint, "enabled": true, "credential_ref": credential, "options": map[string]any{}}}, &receipt)
		reviewProvider, err = f.ParseID[mc.Provider](receipt.ResourceID)
		workAppCheck(t, "m1-review-provider", err)
		reviewVersion = receipt.Version
		v.httpSeed(t, "/models", map[string]any{"provider_id": reviewProvider, "input": map[string]any{"name": "M1 review model", "provider_model_id": reviewName, "type": mc.ChatModel, "enabled": true, "parameters": map[string]any{}, "request_overwrite": map[string]any{}, "header_overwrite": map[string]string{}, "capabilities": map[string]any{"tool_calls": true, "parallel_tool_calls": false, "streaming": true, "reasoning": false, "input_modalities": []string{"text"}, "output_modalities": []string{"text"}, "reasoning_efforts": []string{}, "structured_output_modes": []string{}, "context_length": nil, "max_output": maxOutput}}}, &receipt)
		w.reviewModel, err = f.ParseID[mc.Model](receipt.ResourceID)
		workAppCheck(t, "m1-review-model", err)
	}
	// Both agents are real directory members. Only the worker is assigned; the
	// reviewer receives the Task later through the Owner's explicit UI action.
	disabled := false
	for n := 0; n < 2; n++ {
		target := guardID[id.Agent](t)
		fields := ac.AgentCreateFields{AgentID: target, Name: "m1-reviewer", Description: "Statistics task review", Instructions: "Review the submitted program and statistics.", ModelRef: modelID, ApprovalPolicy: ac.ApprovalAllow, AllowedToolIDs: []id.ToolID{}, AllowedMountIDs: []id.MountID{}, AllowedSecretVariableIDs: []id.ProjectVariableID{}, AddSkillsEnabled: &disabled, InstallSkillEnabled: &disabled, InjectAgentsMD: disabled}
		if n == 0 {
			v.agent = target
			fields.Name = "m1-statistics-worker"
			fields.Instructions = "Read input.json; write stats.py; run /usr/bin/python3 stats.py; verify result.json and report the files and exit status."
			if w.live != nil {
				fields.Instructions = m1LiveTaskPlan
			}
			fields.AllowedToolIDs = []id.ToolID{w.tools[tc.RunnerReadFileKey], w.tools[tc.RunnerWriteFileKey], w.tools[tc.RunnerRunCommandKey]}
			declarations := []ac.RunnerMountDeclaration{{RunnerID: w.runnerID, Workspace: "source"}}
			fields.RunnerMounts = &declarations
			fields.AllowedMountIDs = nil
			slices.SortFunc(fields.AllowedToolIDs, func(a, b id.ToolID) int { return strings.Compare(a.String(), b.String()) })
		} else {
			w.reviewer = target
			if w.reviewEnabled {
				fields.ModelRef = w.reviewModel
				fields.Instructions = "Read review-instructions.txt and acknowledge the review task. Do not decide approval or change the Task state."
				fields.AllowedToolIDs = []id.ToolID{w.tools[tc.RunnerReadFileKey]}
				declarations := []ac.RunnerMountDeclaration{{RunnerID: w.runnerID, Workspace: "review"}}
				fields.RunnerMounts, fields.AllowedMountIDs = &declarations, nil
			}
		}
		request, err := ac.NewAgentCreate(fields)
		workAppCheck(t, "m1-agent-intent", err)
		mutation, err := v.assembly.agents.service.CreateAgent(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), v.a, request)
		workAppCheck(t, "m1-formal-agent", err)
		if n == 0 {
			mounts := mutation.Fields().RunnerMounts
			if mounts == nil || len(*mounts) != 1 {
				t.Fatal("M1 original mount absent")
			}
			w.mount = (*mounts)[0].MountID
			w.workspace = filepath.Join(w.root, v.a.String(), v.agent.String(), (*mounts)[0].Workspace)
			workAppCheck(t, "m1-workspace", os.MkdirAll(w.workspace, 0700))
			workAppCheck(t, "m1-input", os.WriteFile(filepath.Join(w.workspace, "input.json"), []byte("{\"values\":[2,4,6,8]}\n"), 0600))
		} else if w.reviewEnabled {
			mounts := mutation.Fields().RunnerMounts
			if mounts == nil || len(*mounts) != 1 {
				t.Fatal("M1 original review mount absent")
			}
			w.reviewMount = (*mounts)[0].MountID
			w.reviewWorkspace = filepath.Join(w.root, v.a.String(), w.reviewer.String(), (*mounts)[0].Workspace)
			workAppCheck(t, "m1-review-workspace", os.MkdirAll(w.reviewWorkspace, 0700))
			// This is fixture-owned review guidance, not a transfer of worker
			// artifacts and not evidence of an autonomous review decision.
			workAppCheck(t, "m1-review-instructions", os.WriteFile(filepath.Join(w.reviewWorkspace, "review-instructions.txt"), []byte("Acknowledge receipt of this review task. The Owner will decide subsequent review actions.\n"), 0600))
		}
	}
	// Freeze the actual mount identity into the owned peer only after Agent
	// creation. Formally update the not-yet-used Provider before Task scheduling.
	if w.live == nil {
		responses := w.responses
		if w.responseSequence != nil {
			responses = w.responseSequence
		}
		w.scenario, err = w.network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: responses(t)})
		workAppCheck(t, "m1-owned-model-sequence", err)
		providerInput := map[string]any{"name": "M1 owned Provider", "protocol": mc.OpenAIChat, "base_url": "https://fixture.test:8443/case/" + w.scenario, "enabled": true, "credential_ref": credential, "options": map[string]any{}}
		response, err := v.request(v.ctx, "PUT", "/api/v1/projects/"+v.a.String()+"/model-providers/"+provider.String(), workAppJSON(t, map[string]any{"expected_version": providerVersion, "input": providerInput}), guardID[struct{}](t).String())
		workAppCheck(t, "m1-formal-provider-endpoint", err)
		if response.status != 200 {
			t.Fatalf("M1 Provider update status=%d", response.status)
		}
		workAppCheck(t, "m1-provider-updated-receipt", json.Unmarshal(response.body, &receipt))
		if w.reviewEnabled {
			w.reviewScenario, err = w.network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: w.reviewResponses(t)})
			workAppCheck(t, "m1-owned-review-sequence", err)
			reviewInput := map[string]any{"name": "M1 review Provider", "protocol": mc.OpenAIChat, "base_url": "https://fixture.test:8443/case/" + w.reviewScenario, "enabled": true, "credential_ref": credential, "options": map[string]any{}}
			response, err := v.request(v.ctx, "PUT", "/api/v1/projects/"+v.a.String()+"/model-providers/"+reviewProvider.String(), workAppJSON(t, map[string]any{"expected_version": reviewVersion, "input": reviewInput}), guardID[struct{}](t).String())
			workAppCheck(t, "m1-formal-review-endpoint", err)
			if response.status != 200 {
				t.Fatalf("M1 review Provider update status=%d", response.status)
			}
			workAppCheck(t, "m1-review-updated-receipt", json.Unmarshal(response.body, &receipt))
		}
	}
	var structure wc.StructureMutation
	milestone := wc.CreateMilestoneRequest{MilestoneID: guardID[wc.Milestone](t), Title: "M1 statistics"}
	v.httpSeed(t, "/milestones", map[string]any{"request": milestone}, &structure)
	sprintIntent := wc.CreateSprintRequest{SprintID: guardID[pc.Sprint](t), MilestoneID: milestone.MilestoneID, Title: "M1 tool loop"}
	v.httpSeed(t, "/sprints", map[string]any{"request": sprintIntent}, &structure)
	sprint := *structure.Sprint
	var mutation wc.TaskMutation
	request := wc.TaskCreate{TaskID: guardID[wc.Task](t), SprintID: sprint.ID, Title: "读取输入并提交统计程序和结果", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium, Plan: "Read input.json, generate stats.py, execute it, inspect result.json, and report count/sum/mean with program exit status."}
	if w.live != nil {
		request.Plan = m1LiveTaskPlan
	}
	v.httpSeed(t, "/tasks", map[string]any{"request": request}, &mutation)
	before, err := v.projects.service.GetSchedulerConfig(v.ctx, v.actor, v.a)
	workAppCheck(t, "m1-scheduler-config", err)
	limit := int64(1)
	_, err = v.projects.service.UpdateProject(v.ctx, v.actor, executionSchedulerAppMeta(t, &before.Project.Version), v.a, pc.UpdateProjectRequest{Scheduler: &pc.ProjectSchedulerConfig{Enabled: true, MaxConcurrency: &limit}})
	workAppCheck(t, "m1-enable-scheduler", err)
	moved, err := v.planning.transitions.TransferTask(v.ctx, v.actor, executionSchedulerAppMeta(t, &mutation.Task.Version), v.a, request.TaskID, wc.TaskTransfer{TargetState: wc.TaskStateTodo, AssigneeAgentID: &v.agent, AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{}})
	workAppCheck(t, "m1-assign-task", err)
	w.task = moved.Task
	_, err = v.planning.sprints.StartSprint(v.ctx, v.actor, executionSchedulerAppMeta(t, &sprint.Version), v.a, sprint.ID)
	workAppCheck(t, "m1-start-sprint", err)
}

func (w *m1Workflow) responses(t *testing.T) []netfixture.WireScenario {
	t.Helper()
	read := func(path string) map[string]any {
		return map[string]any{"mount_id": w.mount.String(), "path": path, "offset": "0", "length": 4096, "encoding": "utf8"}
	}
	calls := []struct {
		key  string
		args any
	}{{tc.RunnerReadFileKey, read("input.json")}, {tc.RunnerWriteFileKey, map[string]any{"mount_id": w.mount.String(), "path": "stats.py", "data_base64": base64.StdEncoding.EncodeToString([]byte(m1Program)), "overwrite": false}}, {tc.RunnerRunCommandKey, map[string]any{"mount_id": w.mount.String(), "mode": "argv", "program": "/usr/bin/python3", "args": []string{"stats.py"}, "cwd": "."}}, {tc.RunnerReadFileKey, read("result.json")}}
	out := make([]netfixture.WireScenario, 0, 5)
	for n := 0; n < 5; n++ {
		message := map[string]any{"role": "assistant", "content": m1FinalOutput}
		finish := "stop"
		if n < 4 {
			raw, err := json.Marshal(calls[n].args)
			workAppCheck(t, "m1-tool-args", err)
			message["content"] = nil
			message["tool_calls"] = []any{map[string]any{"id": []string{"m1_read_input", "m1_write_program", "m1_run_program", "m1_read_result"}[n], "type": "function", "function": map[string]any{"name": "tool_" + strings.ReplaceAll(w.tools[calls[n].key].String(), "-", ""), "arguments": string(raw)}}}
			finish = "tool_calls"
		}
		raw, err := json.Marshal(map[string]any{"id": "owned-m1-response", "object": "chat.completion", "created": 1, "model": "m1-statistics", "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}})
		workAppCheck(t, "m1-model-response", err)
		out = append(out, netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json", "X-Request-Id": fmt.Sprintf("m1-work-round-%d", n+1)}, Chunks: [][]byte{raw}})
	}
	return out
}

func (w *m1Workflow) reviewResponses(t *testing.T) []netfixture.WireScenario {
	t.Helper()
	args, err := json.Marshal(map[string]any{"mount_id": w.reviewMount.String(), "path": "review-instructions.txt", "offset": "0", "length": 4096, "encoding": "utf8"})
	workAppCheck(t, "m1-review-tool-args", err)
	var sequence []netfixture.WireScenario
	for n := 0; n < 2; n++ {
		message := map[string]any{"role": "assistant", "content": "评审任务已收到，等待 Owner 后续处理。"}
		finish := "stop"
		if n == 0 {
			message["content"] = nil
			message["tool_calls"] = []any{map[string]any{"id": "m1_review_read_instructions", "type": "function", "function": map[string]any{"name": "tool_" + strings.ReplaceAll(w.tools[tc.RunnerReadFileKey].String(), "-", ""), "arguments": string(args)}}}
			finish = "tool_calls"
		}
		raw, err := json.Marshal(map[string]any{"id": "owned-m1-review-response", "object": "chat.completion", "created": 1, "model": "m1-review", "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}})
		workAppCheck(t, "m1-review-response", err)
		sequence = append(sequence, netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json", "X-Request-Id": fmt.Sprintf("m1-review-round-%d", n+1)}, Chunks: [][]byte{raw}})
	}
	return sequence
}
