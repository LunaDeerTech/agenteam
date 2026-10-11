//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	runnerservice "github.com/LunaDeerTech/agenteam/internal/central/runner/service"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	"github.com/LunaDeerTech/agenteam/internal/central/usage"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
)

// Only the Model JSON handle below is controlled. Account/Owner, Agent writes,
// Tool Registry, Launch, Preparation, current authority, locks and PostgreSQL
// are real. This does not exercise the stopped tools HTTPS adapter, App default
// construction, ToolOperation creation, Runner dispatch or physical execution.
type toolFactsModel struct {
	mu                       sync.Mutex
	request                  mc.ModelRequest
	response                 mc.ModelResponse
	arguments                []byte
	invocation               mc.InvocationID
	begin, result, closes    int
	joined                   bool
	closeEntered             chan struct{}
	release                  chan struct{}
	enteredOnce, releaseOnce sync.Once
}

func newToolFactsModel(t *testing.T, mountID i.MountID) *toolFactsModel {
	t.Helper()
	// Deliberately noncanonical order/whitespace: the original protected payload
	// and its independently written canonical representation must both survive.
	arguments := []byte(fmt.Sprintf(`{ "path":"private-tool-call-argument.txt", "offset":"0", "mount_id":%q, "length":64, "encoding":"utf8" }`, mountID.String()))
	if _, err := tc.DecodeReadFileToolArguments(ctxFor(t), arguments); err != nil {
		t.Fatal("owned read-file input", err)
	}
	return &toolFactsModel{arguments: arguments, invocation: id[mc.Invocation](t), closeEntered: make(chan struct{}), release: make(chan struct{})}
}
func (m *toolFactsModel) BeginChat(ctx context.Context, request mc.ModelRequest) (mc.JSONCall, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if m.begin != 0 || request.Validate() != nil || len(request.Tools) != 1 || request.ToolChoice.Kind != "required" || request.Input.SchemaVersion != ec.ToolCallRoundSchemaVersion {
		return nil, errors.New("controlled Model received an invalid or repeated original call")
	}
	m.begin++
	m.request = request.Clone()
	m.response = mc.ModelResponse{CallID: request.CallID, InvocationID: m.invocation, Message: mc.Message{Role: "assistant", Parts: []mc.MessagePart{{ToolCall: &mc.ToolCallPart{ID: "owned-read-file-call", Name: request.Tools[0].Name, Arguments: bytes.Clone(m.arguments)}}}}, FinishReason: "tool_calls", Usage: mc.Usage{Source: mc.UnknownUsage}}
	if m.response.Validate() != nil {
		return nil, errors.New("controlled Model response is invalid")
	}
	return m, nil
}
func (m *toolFactsModel) Result(ctx context.Context) (mc.ModelResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil {
		return mc.ModelResponse{}, ctx.Err()
	}
	m.result++
	return m.response.Clone(), nil
}
func (m *toolFactsModel) Close(ctx context.Context) error {
	m.mu.Lock()
	m.closes++
	m.mu.Unlock()
	m.enteredOnce.Do(func() { close(m.closeEntered) })
	select {
	case <-m.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	m.mu.Lock()
	m.joined = true
	m.mu.Unlock()
	return nil
}
func (m *toolFactsModel) Joined() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.joined }
func (m *toolFactsModel) unhold()      { m.releaseOnce.Do(func() { close(m.release) }) }
func (m *toolFactsModel) observed() (mc.ModelRequest, mc.ModelResponse, int, int, int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.request.Clone(), m.response.Clone(), m.begin, m.result, m.closes, m.joined
}

type toolFactsFixture struct {
	capture  *modelEnvironmentFixture
	agents   *agent.Service
	registry *registry.Registry
	spec     tc.SpecRef
	mount    ac.RunnerMountBinding
	runner   rc.Snapshot
	version  f.Version
	input    ec.PreparationInput
	context  ec.ExecutionContext
	model    *toolFactsModel
	driver   *execution.ToolCallDriver
}

func newToolFactsFixture(t *testing.T) *toolFactsFixture {
	t.Helper()
	v := &toolFactsFixture{}
	var modelAuthority *model.RuntimeAuthority
	var modelSecrets *secret.Service
	var modelService *model.Service
	v.capture = newModelEnvironmentFixtureWithSource(t,
		func(task *taskTransitionFixture, owner *execution.Authority) captureModelPorts {
			modelActor, err := i.RegisterService(i.ModelRuntime)
			firstRoundRequire(t, err)
			secretActor, err := i.RegisterService(i.SecretService)
			firstRoundRequire(t, err)
			outboundActor, err := i.RegisterService(i.OutboundService)
			firstRoundRequire(t, err)
			modelAuthority, err = model.NewRuntimeAuthority(task.base.tracked, model.RuntimeAuthorizations{Consumers: owner, Process: task.agent.guard, ModelRuntime: modelActor, SecretService: secretActor, OutboundService: outboundActor})
			firstRoundRequire(t, err)
			return captureModelPorts{usageFactory: func(authority *model.Authority) (sc.UsageAuthority, error) {
				return model.NewRuntimeSecretUsageRouter(authority, modelAuthority, task.base.accounts)
			}, ready: func(_ *model.Service, secrets *secret.Service, _ *audit.Service) { modelSecrets = secrets }}
		},
		func(task *taskTransitionFixture, models *model.Service, _ *ec.Policy) {
			modelService = models
			current, err := models.GetProjectModel(ctxFor(t), task.base.ownerBrowser.actor, task.base.project.ID, task.agent.modelID)
			firstRoundRequire(t, err)
			input := current.Input.Clone()
			input.Capabilities.ToolCalls = true
			input.Capabilities.ParallelToolCalls = false
			_, err = models.UpdateModel(ctxFor(t), mc.UpdateModelRequest{CommandMeta: mc.CommandMeta{Actor: task.base.ownerBrowser.actor, Scope: current.Scope, Key: "tool-facts-model-capability"}, ID: current.ID, ExpectedVersion: current.Version, Input: input})
			firstRoundRequire(t, err)
		}, true)
	c, b := v.capture, v.capture.v.base
	modelCapture, err := model.NewToolCallFactsCapture(modelService, c.authority)
	firstRoundRequire(t, err)
	c.models.provider = modelCapture
	v.bindRetirementRuntime(t, modelAuthority, modelSecrets)
	var ordinary bool
	firstRoundRequire(t, b.raw.QueryRow(ctxFor(t), `SELECT role='user' FROM agenteam_account.users WHERE id::text=$1`, b.ownerBrowser.actor.Details().UserID).Scan(&ordinary))
	if !ordinary {
		t.Fatal("Tool configuration must use the original non-admin Project Owner")
	}
	source, err := registry.NewReadFileMetadataSource(b.tracked)
	firstRoundRequire(t, err)
	v.registry, err = registry.New(b.tracked, registry.Options{RunnerSources: map[string]registry.RunnerSource{tc.RunnerReadFileKey: source}, Authorizations: registry.Authorizations{Sessions: b.accounts, Projects: c.v.agent.providers.Projects}})
	firstRoundRequire(t, err)
	b.tx(t, []f.LockRequest{registry.RegistryLock(f.Exclusive)}, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		var err error
		v.spec, err = v.registry.ReconcileRunnerInTx(ctx, tx, tc.RunnerReadFileKey)
		return err
	})
	if v.spec.Validate() != nil {
		t.Fatal("explicit real read-file registration failed")
	}
	v.agents = v.newAgentService(t)
	runnerAuthority, err := runnerservice.NewAuthority(b.tracked, b.accounts)
	firstRoundRequire(t, err)
	runnerAudit, err := audit.New(b.tracked, b.keys, audit.Authorizations{Sessions: b.accounts, System: b.accounts, Accounts: b.accounts, Runners: runnerAuthority})
	firstRoundRequire(t, err)
	runners, err := runnerservice.New(runnerAuthority, runnerAudit)
	firstRoundRequire(t, err)
	t.Cleanup(func() {
		runners.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := runners.Drain(ctx); err != nil {
			t.Error("original Runner service did not join", err)
		}
	})
	create, err := rc.NewCreate(rc.CreateRequest{RunnerID: id[rc.Runner](t), Name: "tool-facts-runner", Description: "owned offline metadata", Tags: []string{}, RootPath: "/tool-facts-private-root"})
	firstRoundRequire(t, err)
	created, err := runners.Execute(ctxFor(t), b.adminBrowser.actor, "tool-facts-runner", create)
	firstRoundRequire(t, err)
	if created.Receipt.Validate() != nil || created.Receipt.Runner.Status != rc.Offline || created.Material == nil || !created.Material.Token.Valid() || created.Material.ExpiresAt.Validate() != nil {
		t.Fatal("formal Runner creation or its original private material is invalid")
	}
	v.runner = created.Receipt.Runner
	current, err := v.agents.GetAgent(ctxFor(t), b.ownerBrowser.actor, b.project.ID, c.v.agentID)
	firstRoundRequire(t, err)
	tools := []i.ToolID{v.spec.ToolID}
	mounts := []ac.RunnerMountDeclaration{{RunnerID: v.runner.ID, Workspace: "tool-call.workspace"}}
	change, err := ac.NewAgentUpdate(ac.AgentUpdateFields{AllowedToolIDs: &tools, RunnerMounts: &mounts})
	firstRoundRequire(t, err)
	before := current.Fields().Core.Version
	updated, err := v.agents.UpdateAgent(ctxFor(t), b.ownerBrowser.actor, meta(t, "tool-facts-configure", &before), b.project.ID, c.v.agentID, change)
	firstRoundRequire(t, err)
	if updated.Validate() != nil || updated.Fields().RunnerMounts == nil || len(*updated.Fields().RunnerMounts) != 1 {
		t.Fatal("Agent command did not publish its canonical Runner mapping")
	}
	v.mount, v.version = (*updated.Fields().RunnerMounts)[0], updated.Fields().Agent.Fields().Core.Version
	if v.version != before+1 || v.mount.RunnerID != v.runner.ID || v.mount.Workspace != mounts[0].Workspace || !reflect.DeepEqual(updated.Fields().Agent.Fields().AllowedToolIDs, tools) {
		t.Fatal("Agent command changed original selected Tool or Mount")
	}
	claim, _ := schedulerClaimRequest(t, c.v)
	dispatch, err := c.claims.ClaimTask(ctxFor(t), claim, c.launchPolicy)
	firstRoundRequire(t, err)
	c.request, err = dispatch.LaunchRequest()
	firstRoundRequire(t, err)
	handoff, counter := newBusyLaunchHandoff(t, c.v)
	launched, err := handoff.LaunchOnce(ctxFor(t), b.project.ID, dispatch.Summary().ID)
	firstRoundRequire(t, err)
	count, _, executionCreated := counter.observed()
	if count != 1 || executionCreated.Status != ec.Created || launched.Summary().ExecutionID == nil || *launched.Summary().ExecutionID != executionCreated.ID {
		t.Fatal("real Launch did not publish the associated Execution")
	}
	c.created = executionCreated
	toolProvider, err := registry.NewExecutionCapture(v.registry, c.authority)
	firstRoundRequire(t, err)
	c.tools = &observedToolCapture{provider: toolProvider}
	c.models.observe = c.observeModel
	// The older observer deliberately fixes Agent v2; this slice checks its
	// actual updated version and complete immutable environment below.
	c.environment.observe = nil
	directory, err := runnerservice.NewExecutionMetadataDirectory(b.tracked, c.authority)
	firstRoundRequire(t, err)
	mountsProvider, err := mount.NewExecutionCapture(b.tracked, c.authority, directory)
	firstRoundRequire(t, err)
	observedMounts := &executionMountObserver{provider: mountsProvider}
	preparation, err := execution.NewPreparationDriver(b.tracked, c.authority, execution.PreparationDependencies{Agents: c.configuration, Projects: c.projectAuthority, Processes: captureProviderProcesses{c.v.agent.guard}, Task: c.task, Skills: c.skills, Tools: c.tools, Models: c.models, Environment: c.environment, Mounts: observedMounts})
	firstRoundRequire(t, err)
	t.Cleanup(func() {
		preparation.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := preparation.Drain(ctx); err != nil || !preparation.Joined() {
			t.Error("original Tool preparation did not join", err)
		}
	})
	if err := preparation.Run(ctxFor(t), c.created.ID); err != nil {
		t.Log("tool_facts_stage=preparation_run")
		firstRoundRequire(t, err)
	}
	c.requireProviderCalls(t, false)
	v.input = (&firstRoundFixture{capture: c}).preparedInput(t)
	fields := v.input.Fields()
	if observedMounts.capture != 1 || observedMounts.discover != 1 || observedMounts.verify == 0 || observedMounts.tx != c.models.tx || len(fields.Tools) != 1 || fields.Tools[0].ToolID != v.spec.ToolID || fields.Tools[0].SpecRevision != v.spec.SpecRevision || fields.Tools[0].BindingSnapshot.RunnerBinding != tc.ReadFileRegistration().Binding || len(fields.Mounts.Mounts) != 1 || fields.Mounts.Mounts[0].ID != v.mount.MountID || fields.Mounts.Mounts[0].WorkspaceID != v.mount.WorkspaceID || fields.Mounts.Mounts[0].RunnerID != v.runner.ID || fields.Agent.Fields().Core.Version != v.version || fields.Environment.Fields().AgentVersion != v.version || !fields.Model.Snapshot.Capabilities.ToolCalls || fields.Model.Snapshot.Capabilities.ParallelToolCalls {
		t.Fatal("real Preparation lost selected Tool/Mount/current model input")
	}
	builder, err := execution.NewContextBuilder(work.TaskContextBuilder{})
	firstRoundRequire(t, err)
	v.context, err = builder.BuildContext(ctxFor(t), v.input)
	firstRoundRequire(t, err)
	v.model = newToolFactsModel(t, v.mount.MountID)
	v.driver = v.newDriver(t, builder, modelAuthority, modelSecrets)
	return v
}

// Register the real Runtime's borrower registry for lease retirement. It is
// never the JSONCaller: no Chat, adapter Start, credential read or wire request
// occurs here. Its original owner retires after the Tool driver/controlled
// handle and before the shared Secret providers and ProcessGuard.
func (v *toolFactsFixture) bindRetirementRuntime(t *testing.T, authority *model.RuntimeAuthority, secrets *secret.Service) {
	t.Helper()
	c, b := v.capture, v.capture.v.base
	invocations, err := usage.NewAuthority(b.tracked, usage.Authorizations{Sessions: b.accounts, Projects: c.projectAuthority, Invocations: authority})
	firstRoundRequire(t, err)
	ledger, err := usage.New(b.tracked, invocations, usage.Dependencies{Cursors: b.keys})
	firstRoundRequire(t, err)
	firstRoundRequire(t, ledger.Initialize(ctxFor(t)))
	policy, err := outbound.NewPolicyService(b.tracked, b.audit, outbound.Authorizations{Sessions: b.accounts, System: b.accounts})
	firstRoundRequire(t, err)
	firstRoundRequire(t, policy.Reload(ctxFor(t)))
	trust, err := outbound.LoadTrustStore("")
	firstRoundRequire(t, err)
	budget := wire.NewBudget()
	var runtime *model.Runtime
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if runtime != nil {
			runtime.StopAdmission()
			if err := runtime.Drain(ctx); err != nil || !runtime.Joined() {
				t.Error("original retirement Model Runtime did not join", err)
			}
		}
		if err := budget.Force(ctx); err != nil || !budget.Joined() {
			t.Error("original unused Model transport budget did not join", err)
		}
	})
	adapter, err := wire.NewOpenAIChat(wire.Transport{Policy: policy, Trust: trust}, budget)
	firstRoundRequire(t, err)
	runtime, err = model.NewRuntime(b.tracked, authority, model.RuntimeDependencies{Usage: ledger, SecretReader: secrets, SecretUsage: secrets, Adapter: adapter})
	firstRoundRequire(t, err)
	firstRoundRequire(t, runtime.Initialize(ctxFor(t)))
}

// Reuse the original real Agent providers, changing only the explicitly bound
// Tool directory/reference service. Update does not resolve Create defaults.
func (v *toolFactsFixture) newAgentService(t *testing.T) *agent.Service {
	t.Helper()
	b, p := v.capture.v.base, v.capture.v.agent.providers
	tools, err := registry.NewConfiguration(v.registry, p.Agents, "")
	firstRoundRequire(t, err)
	catalog := event.NewCatalog()
	types, err := ac.RegisterAgentEvents(catalog)
	firstRoundRequire(t, err)
	events, err := agent.NewEventAuthority(p.Agents, types)
	firstRoundRequire(t, err)
	auditor, err := audit.New(b.tracked, b.keys, audit.Authorizations{Accounts: p.Accounts, Sessions: p.Accounts, System: p.Accounts, Projects: p.AuditProjects, Models: p.Models})
	firstRoundRequire(t, err)
	journal, err := outbox.New(b.tracked, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{ac.AgentProducer: events}, Projects: p.Projects, Processes: captureProviderProcesses{v.capture.v.agent.guard}, Sessions: p.Accounts, System: p.Accounts, Audit: auditor, Cursors: b.keys})
	firstRoundRequire(t, err)
	service, err := agent.New(b.tracked, agent.Dependencies{RunnerCandidates: p.RunnerCandidates, RunnerCursorKeys: b.keys, Authority: p.Agents, EventAuthority: events, Events: journal, Audit: auditor, Activity: p.Accounts, Models: p.ModelReferences, Tools: tools, ToolReferences: tools, Mounts: p.Mounts, Secrets: p.SecretDirectory, SecretReferences: p.SecretReferences, Skills: p.Skills})
	firstRoundRequire(t, err)
	t.Cleanup(func() {
		service.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := service.Drain(ctx); err != nil || !service.Joined() {
			t.Error("original Agent writer did not join", err)
		}
	})
	return service
}
func (v *toolFactsFixture) newDriver(t *testing.T, builder *execution.ContextBuilder, authority *model.RuntimeAuthority, secrets *secret.Service) *execution.ToolCallDriver {
	t.Helper()
	c, b := v.capture, v.capture.v.base
	loop, err := agentloop.NewToolCallController(v.model)
	firstRoundRequire(t, err)
	t.Cleanup(func() {
		v.model.unhold()
		loop.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := loop.Drain(ctx); err != nil || !loop.Joined() {
			t.Error("original controlled Model/Loop owner did not join", err)
		}
	})
	skills, err := skill.NewInitialRoundBindings(c.v.agent.providers.SkillAuthority)
	firstRoundRequire(t, err)
	models, err := model.NewExecutionLeaseRetirement(authority, secrets)
	firstRoundRequire(t, err)
	environmentAuthority, err := pv.NewEnvironmentRetirementAuthority(b.authority, c.authority)
	firstRoundRequire(t, err)
	secretRetirement, err := secret.NewProjectVariableLeaseRetirement(c.environmentSecrets, environmentAuthority)
	firstRoundRequire(t, err)
	environment, err := pv.NewEnvironmentRetirement(environmentAuthority, secretRetirement)
	firstRoundRequire(t, err)
	catalog := event.NewCatalog()
	lifecycle, err := ec.RegisterDirectTextEvents(catalog)
	firstRoundRequire(t, err)
	events, err := execution.NewDirectTextEventAuthority(c.authority)
	firstRoundRequire(t, err)
	box, err := outbox.New(b.tracked, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{ec.ExecutionProducer: events}, Sessions: b.accounts, System: b.accounts, Projects: c.projectAuthority, Audit: b.audit, Cursors: b.keys, Processes: captureProviderProcesses{c.v.agent.guard}})
	firstRoundRequire(t, err)
	mounts, err := mount.NewExecutionUse(b.tracked, c.authority)
	firstRoundRequire(t, err)
	driver, err := execution.NewToolCallDriver(b.tracked, c.authority, execution.ToolCallDependencies{Base: execution.DirectTextDependencies{Context: builder, Processes: captureProviderProcesses{c.v.agent.guard}, Projects: c.projectAuthority, Agents: c.configuration, Skills: skills, Events: box, Lifecycle: lifecycle, Models: models, Environment: environment, Mounts: mounts}, Loop: loop, Tools: v.registry})
	firstRoundRequire(t, err)
	t.Cleanup(func() {
		v.model.unhold()
		driver.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := driver.Drain(ctx); err != nil || !driver.Joined() {
			t.Error("original ToolCall driver did not join", err)
		}
	})
	return driver
}

type toolFactsOutcome struct {
	receipt ec.DirectTextReceipt
	err     error
}

func (v *toolFactsFixture) startHeld(t *testing.T) <-chan toolFactsOutcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	result := make(chan toolFactsOutcome, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		r, err := v.driver.Start(ctx, v.capture.created.ID)
		result <- toolFactsOutcome{r, err}
	}()
	t.Cleanup(func() {
		v.model.unhold()
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("original ToolCall Start goroutine did not return")
		}
	})
	select {
	case <-v.model.closeEntered:
	case <-finished:
		r := <-result
		t.Fatal("original Start returned before the Model Close barrier", r.err)
	case <-time.After(5 * time.Second):
		t.Fatal("original Model Close was not reached")
	}
	request, _, begin, results, closes, joined := v.model.observed()
	if begin != 1 || results != 1 || closes != 1 || joined || request.Input.ExecutionID == nil || *request.Input.ExecutionID != v.capture.created.ID || request.Model.Snapshot.ID != v.input.Fields().Model.Snapshot.ID || request.Tools[0].Name != v.input.Fields().Tools[0].ModelVisibleName {
		t.Fatal("held original Model owner/request identity changed")
	}
	v.requireNoCall(t)
	select {
	case <-finished:
		t.Fatal("Start returned while original Model Close remained held")
	default:
	}
	return result
}
func toolFactsWait(t *testing.T, done <-chan toolFactsOutcome) toolFactsOutcome {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(10 * time.Second):
		t.Fatal("original ToolCall Start did not return")
		return toolFactsOutcome{}
	}
}

// The row counts distinguish a live startup from a partial Tool checkpoint.
// In particular a held Close or rolled back final Tx cannot publish seq2,
// protected arguments, a call row, or a terminal/operation fact.
func (v *toolFactsFixture) requireNoCall(t *testing.T) {
	t.Helper()
	var state string
	var version, through, arguments, calls, transcript, operations, terminal, retired int64
	var pending bool
	err := v.capture.v.base.raw.QueryRow(ctxFor(t), `SELECT e.status,e.version,r.transcript_through,r.tool_pending,
 (SELECT count(*) FROM agenteam_execution.tool_call_arguments a WHERE a.execution_id=e.id),
 (SELECT count(*) FROM agenteam_execution.tool_calls c WHERE c.execution_id=e.id),
 (SELECT count(*) FROM agenteam_execution.transcript_entries t WHERE t.execution_id=e.id AND t.sequence=2),
 (SELECT count(*) FROM agenteam_tool.operations o WHERE o.execution_id=e.id),
 (SELECT count(*) FROM agenteam_outbox.events o WHERE o.aggregate_id=e.id::uuid AND o.event_type IN('execution.succeeded','execution.failed','execution.cancelled')),
 (SELECT count(*) FROM agenteam_mount.execution_use_retirements m WHERE m.execution_id=e.id)
 FROM agenteam_execution.executions e JOIN agenteam_execution.rounds r ON r.execution_id=e.id WHERE e.id=$1`, v.capture.created.ID.String()).Scan(&state, &version, &through, &pending, &arguments, &calls, &transcript, &operations, &terminal, &retired)
	firstRoundRequire(t, err)
	if state != "running" || version != int64(v.capture.created.Version)+2 || through != 1 || pending || arguments != 0 || calls != 0 || transcript != 0 || operations != 0 || terminal != 0 || retired != 0 {
		t.Fatal("unjoined/rejected/rolled-back call published partial or terminal facts")
	}
}

type toolFactsIdentity struct {
	Snapshot, Start, Round, Input, Logical, Invocation, Payload, Call string
	Version                                                           int64
}

func (v *toolFactsFixture) facts(ctx context.Context, sql postgres.SQLExecutor) (toolFactsIdentity, error) {
	var out toolFactsIdentity
	var original, canonical, transcript, response, snapshotRaw, roundRaw []byte
	var state, modelName, toolID, originalDigest, canonicalDigest, inputDigest, projectID, agentID, operation, revision string
	var schema, through, spec, protocol, transcriptCount, argumentCount, callCount, operationCount, terminalCount, mountRefs, retired, modelLeases, environmentLeases int64
	var sameTx, pending bool
	err := sql.QueryRow(ctx, `SELECT e.status,e.version,s.id,s.start_id,s.content,r.id,r.input_binding_id,r.call_id,r.schema_version,r.content,r.input_digest,r.tool_pending,r.transcript_through,r.response,
 a.id,a.original,a.original_digest,a.canonical,a.canonical_digest,a.project_id,a.agent_id,c.invocation_id,c.call_id,c.model_visible_name,c.tool_id,c.spec_revision,c.operation_name,c.operation_revision,c.protocol_major,t.message,
 (e.xmin=c.xmin AND c.xmin=a.xmin AND a.xmin=r.xmin AND r.xmin=t.xmin),
 (SELECT count(*) FROM agenteam_execution.transcript_entries z WHERE z.execution_id=e.id),
 (SELECT count(*) FROM agenteam_execution.tool_call_arguments z WHERE z.execution_id=e.id),
 (SELECT count(*) FROM agenteam_execution.tool_calls z WHERE z.execution_id=e.id),
 (SELECT count(*) FROM agenteam_tool.operations z WHERE z.execution_id=e.id),
 (SELECT count(*) FROM agenteam_outbox.events z WHERE z.aggregate_id=e.id::uuid AND z.event_type IN('execution.succeeded','execution.failed','execution.cancelled')),
 (SELECT count(*) FROM agenteam_mount.execution_references z WHERE z.execution_id=e.id AND z.mount_id::text=$2 AND z.workspace_id::text=$3 AND z.runner_id::text=$4),
 (SELECT count(*) FROM agenteam_mount.execution_use_retirements z WHERE z.execution_id=e.id),
 (SELECT count(*) FROM agenteam_model.snapshot_bindings z JOIN agenteam_secret.secret_leases l ON l.id=z.lease_id WHERE z.owner_kind='execution' AND z.owner_id::text=e.id AND NOT l.released),
 (SELECT count(*) FROM agenteam_projectvariable.execution_secret_references z WHERE z.execution_id=e.id)
 FROM agenteam_execution.executions e
 JOIN agenteam_execution.snapshots s ON (s.execution_id,s.id)=(e.id,e.snapshot_id)
 JOIN agenteam_execution.rounds r ON (r.execution_id,r.snapshot_id)=(e.id,s.id)
 JOIN agenteam_execution.tool_calls c ON (c.execution_id,c.round_id,c.checkpoint_version)=(e.id,r.id,e.version)
 JOIN agenteam_execution.tool_call_arguments a ON (a.execution_id,a.round_id,a.id,a.snapshot_id,a.logical_call_id,a.input_binding_id)=(e.id,r.id,c.payload_id,s.id,r.call_id,r.input_binding_id)
 JOIN agenteam_execution.transcript_entries t ON (t.execution_id,t.round_id,t.sequence)=(e.id,r.id,2)
 WHERE e.id=$1`, v.capture.created.ID.String(), v.mount.MountID.String(), v.mount.WorkspaceID.String(), v.mount.RunnerID.String()).Scan(&state, &out.Version, &out.Snapshot, &out.Start, &snapshotRaw, &out.Round, &out.Input, &out.Logical, &schema, &roundRaw, &inputDigest, &pending, &through, &response, &out.Payload, &original, &originalDigest, &canonical, &canonicalDigest, &projectID, &agentID, &out.Invocation, &out.Call, &modelName, &toolID, &spec, &operation, &revision, &protocol, &transcript, &sameTx, &transcriptCount, &argumentCount, &callCount, &operationCount, &terminalCount, &mountRefs, &retired, &modelLeases, &environmentLeases)
	if err != nil {
		return out, err
	}
	if state != "running" || out.Version != int64(v.capture.created.Version)+3 || schema != 2 || !pending || through != 2 || !sameTx || transcriptCount != 2 || argumentCount != 1 || callCount != 1 || operationCount != 0 || terminalCount != 0 || mountRefs != 1 || retired != 0 || modelLeases != 1 || environmentLeases != 1 {
		return out, errors.New("tool checkpoint is partial, terminal, or detached from live provider facts")
	}
	if projectID != v.capture.created.ProjectID.String() || agentID != v.capture.created.AgentID.String() || out.Invocation != v.model.invocation.String() || out.Call != "owned-read-file-call" || modelName != v.input.Fields().Tools[0].ModelVisibleName || toolID != v.spec.ToolID.String() || spec != int64(v.spec.SpecRevision) || operation != "read-file" || revision != "1" || protocol != 1 {
		return out, errors.New("Tool call lost original model or captured binding identities")
	}
	expectedCanonical := []byte(fmt.Sprintf(`{"encoding":"utf8","length":64,"mount_id":%q,"offset":"0","path":"private-tool-call-argument.txt"}`, v.mount.MountID.String()))
	if !bytes.Equal(original, v.model.arguments) || !bytes.Equal(canonical, expectedCanonical) || originalDigest != string(ec.TriggerInputDigest(v.model.arguments)) || canonicalDigest != string(ec.TriggerInputDigest(expectedCanonical)) {
		return out, errors.New("protected original or canonical arguments changed")
	}
	snapshot, err := ec.DecodeDirectTextSnapshot(snapshotRaw)
	if err != nil || !bytes.Equal(snapshot.Fields().Context.CanonicalBytes(), v.context.CanonicalBytes()) {
		return out, errors.New("immutable Snapshot lost original complete preparation")
	}
	round, err := ec.DecodeToolCallRound(roundRaw)
	if err != nil {
		return out, err
	}
	request, expectedResponse, begin, _, _, joined := v.model.observed()
	rf := round.Fields().Round
	if begin != 1 || !joined || rf.ID.String() != out.Round || rf.InputBindingID.String() != out.Input || rf.CallID.String() != out.Logical || rf.Input.Digest.String() != inputDigest || rf.Input.Digest != request.Input.Digest || !reflect.DeepEqual(round.Fields().Tool, request.Tools[0]) || !reflect.DeepEqual(rf.Messages, request.Messages) {
		return out, errors.New("checkpoint did not bind the original joined Model input")
	}
	var message mc.Message
	var savedResponse mc.ModelResponse
	if json.Unmarshal(transcript, &message) != nil || json.Unmarshal(response, &savedResponse) != nil || !toolFactsJSONEqual(message, expectedResponse.Message) || !toolFactsJSONEqual(savedResponse, expectedResponse) {
		return out, errors.New("canonical assistant transcript differs from original complete Model response")
	}
	for _, raw := range [][]byte{snapshotRaw, roundRaw} {
		for _, private := range []string{"/tool-facts-private-root", "owned-model-capture-material", "owned-environment-capture-material", "private-tool-call-argument.txt"} {
			if bytes.Contains(raw, []byte(private)) {
				return out, errors.New("private output leaked into immutable pre-call input")
			}
		}
	}
	return out, nil
}

func toolFactsJSONEqual(a, b any) bool {
	rawA, errA := json.Marshal(a)
	rawB, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	var va, vb any
	return json.Unmarshal(rawA, &va) == nil && json.Unmarshal(rawB, &vb) == nil && reflect.DeepEqual(va, vb)
}
func (v *toolFactsFixture) requireReceipt(t *testing.T, receipt ec.DirectTextReceipt) toolFactsIdentity {
	t.Helper()
	got, err := v.facts(ctxFor(t), v.capture.v.base.raw)
	firstRoundRequire(t, err)
	if receipt.ExecutionID != v.capture.created.ID || receipt.Status != ec.Running || receipt.TranscriptThrough != 2 || int64(receipt.Version) != got.Version || receipt.SnapshotID.String() != got.Snapshot || receipt.StartID.String() != got.Start || receipt.RoundID.String() != got.Round || receipt.CallID.String() != got.Logical {
		t.Fatal("public receipt does not describe the original nonterminal checkpoint")
	}
	_, _, begin, results, closes, joined := v.model.observed()
	if begin != 1 || results != 1 || closes != 1 || !joined {
		t.Fatal("checkpoint observation repeated the Model call or skipped actual handle close")
	}
	return got
}
func (v *toolFactsFixture) changeTools(t *testing.T, key string, allowed []i.ToolID) {
	t.Helper()
	b, c := v.capture.v.base, v.capture
	current, err := v.agents.GetAgent(ctxFor(t), b.ownerBrowser.actor, b.project.ID, c.v.agentID)
	firstRoundRequire(t, err)
	expected := current.Fields().Core.Version
	change, err := ac.NewAgentUpdate(ac.AgentUpdateFields{AllowedToolIDs: &allowed})
	firstRoundRequire(t, err)
	receipt, err := v.agents.UpdateAgent(ctxFor(t), b.ownerBrowser.actor, meta(t, key, &expected), b.project.ID, c.v.agentID, change)
	firstRoundRequire(t, err)
	if receipt.Validate() != nil || receipt.Fields().Agent.Fields().Core.Version != expected+1 || !reflect.DeepEqual(receipt.Fields().Agent.Fields().AllowedToolIDs, allowed) {
		t.Fatal("formal current Agent Tool change failed")
	}
}
func (v *toolFactsFixture) checkBinding(t *testing.T, facts toolFactsIdentity) tc.ToolCallBinding {
	t.Helper()
	binding, err := v.driver.ReadToolCall(ctxFor(t), v.capture.created.ID)
	firstRoundRequire(t, err)
	if binding.Validate() != nil || binding.RoundID != facts.Round || binding.SnapshotID != facts.Snapshot || binding.InputBindingID != facts.Input || binding.LogicalCallID.String() != facts.Logical || binding.InvocationID.String() != facts.Invocation || binding.CallID != facts.Call || binding.Input.PayloadID != facts.Payload || binding.Spec != v.spec || binding.Input.SHA256 != ec.TriggerInputDigest(v.model.arguments) || binding.Input.Bytes != f.Progress(len(v.model.arguments)) {
		t.Fatal("safe Tool binding lost canonical original identities")
	}
	plan, err := v.driver.DiscoverToolCall(ctxFor(t), binding)
	firstRoundRequire(t, err)
	if plan == nil || len(plan.RequiredLocks()) == 0 {
		t.Fatal("canonical Tool plan omitted required current locks")
	}
	store := v.capture.v.base.tracked
	result := store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx f.Tx) error {
		if err := store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return err
		}
		return v.driver.RequireToolCallInTx(ctx, tx, binding, plan)
	})
	if result.State() != f.Committed || result.Fault() != nil {
		t.Fatal("actual current locked Tool authority rejected its original binding", result.Fault())
	}
	changed := binding
	changed.Input.SHA256 = ec.TriggerInputDigest([]byte("other-owned-arguments"))
	_, err = v.driver.DiscoverToolCall(ctxFor(t), changed)
	requireCode(t, err, f.Forbidden)
	raw, err := json.Marshal(binding)
	firstRoundRequire(t, err)
	safe := string(raw) + fmt.Sprintf("%+v %#v", binding, binding)
	for _, private := range []string{"private-tool-call-argument.txt", "/tool-facts-private-root", "owned-model-capture-material", "owned-environment-capture-material"} {
		if strings.Contains(safe, private) {
			t.Fatal("safe binding default projection leaked private material")
		}
	}
	return binding
}
func (v *toolFactsFixture) requireJoined(t *testing.T) {
	t.Helper()
	v.driver.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := v.driver.Drain(ctx); err != nil || !v.driver.Joined() || !v.model.Joined() {
		t.Fatal("original ToolCall driver/Model handle did not actually join", err)
	}
	// Successful Stop/Drain only retires the in-memory owner; the still-running
	// Tool checkpoint and Model/environment/Mount references remain canonical.
	_, err := v.facts(ctxFor(t), v.capture.v.base.raw)
	firstRoundRequire(t, err)
}

func TestExecutionToolCallFacts(t *testing.T) {
	t.Run("held-close-unknown-and-binding", func(t *testing.T) {
		v := newToolFactsFixture(t)
		store := v.capture.v.base.tracked
		originalStore := store.fixtureStore
		loss := &captureCommitReceiptLoss{fixtureStore: originalStore, attempt: id[f.TransactionAttempt](t), match: func(cause f.TransactionCause) bool {
			d := cause.Details()
			return d.Kind == f.JobCause && d.JobType == "execution-tool-call-checkpoint" && d.JobID == v.capture.created.ID.String()
		}}
		// Install before Start, never mutate the Store while the original goroutine
		// is executing. Only an actually committed checkpoint receipt is hidden.
		store.fixtureStore = loss
		t.Cleanup(func() { store.fixtureStore = originalStore })
		done := v.startHeld(t)
		v.model.unhold()
		first := toolFactsWait(t, done)
		requireCode(t, first.err, f.CommitUnknown)
		fired, physical := loss.observed()
		if !fired || physical.State() != f.Committed {
			t.Fatal("Unknown was not the original actually committed checkpoint acknowledgement")
		}
		before, err := v.facts(ctxFor(t), v.capture.v.base.raw)
		firstRoundRequire(t, err)
		receipt, err := v.driver.ResolveUnknown(ctxFor(t), v.capture.created.ID)
		firstRoundRequire(t, err)
		after := v.requireReceipt(t, receipt)
		if before != after {
			t.Fatal("Unknown observation rewrote original checkpoint identity")
		}
		binding := v.checkBinding(t, after)
		replay, err := v.driver.Start(ctxFor(t), v.capture.created.ID)
		firstRoundRequire(t, err)
		if replay != receipt || v.requireReceipt(t, replay) != after {
			t.Fatal("historical pending read repeated Start or replaced the checkpoint")
		}
		v.changeTools(t, "tool-facts-revoke-current", []i.ToolID{})
		_, err = v.driver.ReadToolCall(ctxFor(t), v.capture.created.ID)
		requireCode(t, err, f.Forbidden)
		plan, err := v.driver.DiscoverToolCall(ctxFor(t), binding)
		firstRoundRequire(t, err)
		denied := store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx f.Tx) error {
			if err := store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
				return err
			}
			return v.driver.RequireToolCallInTx(ctx, tx, binding, plan)
		})
		if denied.State() != f.NotCommitted {
			t.Fatal("current revocation was accepted by the canonical Tool transaction")
		}
		requireCode(t, denied.Fault(), f.Forbidden)
		unchanged, err := v.facts(ctxFor(t), v.capture.v.base.raw)
		firstRoundRequire(t, err)
		if unchanged != after {
			t.Fatal("denied current authority changed historical protected facts")
		}
		v.requireJoined(t)
	})
	t.Run("current-revocation-and-checkpoint-rollback", func(t *testing.T) {
		v := newToolFactsFixture(t)
		done := v.startHeld(t)
		v.changeTools(t, "tool-facts-revoke-before-join", []i.ToolID{})
		v.model.unhold()
		denied := toolFactsWait(t, done)
		requireCode(t, denied.err, f.Forbidden)
		v.requireNoCall(t)
		if !v.model.Joined() {
			t.Fatal("current-check refusal abandoned the original Model handle")
		}
		// Restore only through the real Owner command. Recovery retains the same
		// original model response; it is not a new model call or regenerated intent.
		v.changeTools(t, "tool-facts-restore-current", []i.ToolID{v.spec.ToolID})
		store := v.capture.v.base.tracked
		marker := errors.New("owned-tool-facts-final-rollback")
		seen, returned := false, false
		var physical f.CommitResult
		var tentative toolFactsIdentity
		store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			d := cause.Details()
			if d.Kind != f.JobCause || d.JobType != "execution-tool-call-checkpoint" || d.JobID != v.capture.created.ID.String() {
				return nil
			}
			sql, err := store.InTx(tx)
			if err != nil {
				return err
			}
			tentative, err = v.facts(ctx, sql)
			if err != nil {
				return err
			}
			seen = true
			return marker
		})
		store.mu.Lock()
		store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
			d := cause.Details()
			if seen && d.Kind == f.JobCause && d.JobType == "execution-tool-call-checkpoint" && d.JobID == v.capture.created.ID.String() {
				physical, returned = result, true
			}
		}
		store.mu.Unlock()
		clear := func() { store.setAfter(nil); store.mu.Lock(); store.afterResult = nil; store.mu.Unlock() }
		defer clear()
		_, err := v.driver.ResolveUnknown(ctxFor(t), v.capture.created.ID)
		clear()
		if !seen || !returned || physical.State() != f.NotCommitted || !errors.Is(physical.Fault(), marker) || !errors.Is(err, marker) {
			t.Fatal("final checkpoint did not physically roll back its complete same-Tx facts", err)
		}
		v.requireNoCall(t)
		receipt, err := v.driver.ResolveUnknown(ctxFor(t), v.capture.created.ID)
		firstRoundRequire(t, err)
		committed := v.requireReceipt(t, receipt)
		if tentative != committed {
			t.Fatal("known-rollback recovery regenerated the original Tool candidate")
		}
		v.checkBinding(t, committed)
		v.requireJoined(t)
	})
}
