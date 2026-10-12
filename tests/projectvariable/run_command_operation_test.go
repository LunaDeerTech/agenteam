//go:build integration && linux

package projectvariable_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	runnerhttp "github.com/LunaDeerTech/agenteam/internal/central/runner/http"
	runnerservice "github.com/LunaDeerTech/agenteam/internal/central/runner/service"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	toolruntime "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	"github.com/LunaDeerTech/agenteam/internal/runner/control"
	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	"github.com/LunaDeerTech/agenteam/internal/runner/runcommand"
	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/jackc/pgx/v5"
)

// Runner run-command Operation fixture. The explicit Execution command profile is
// composed below; only the original Model JSON result and Store-return timing
// are controlled. Neither a raw SQL ToolCall seed nor a caller-supplied grant
// may stand in for the real Owner/Preparation/Registry/Execution producer.

// This observer surrounds the real DeviceHandler's hijacked TLS connection.
// It neither substitutes a peer/Handler nor changes any command result. Only
// bytes actually accepted by the original connection Write enter the decoder;
// request identities are retained, while payload equality is a private bool.
type operationCommandWire struct {
	mu       sync.Mutex
	expected p.RunCommandPayload
	requests []operationCommandFrame
	invalid  bool
}

type operationCommandFrame struct {
	request, operation, execution, project, agent, mount, workspace p.ID
}

func (o *operationCommandWire) wrap(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(&operationCommandResponse{ResponseWriter: w, observer: o}, r)
	})
}

func (o *operationCommandWire) snapshot() ([]operationCommandFrame, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.requests), !o.invalid
}

func (o *operationCommandWire) reject() {
	o.mu.Lock()
	o.invalid = true
	o.mu.Unlock()
}

func (o *operationCommandWire) message(raw []byte) {
	message, err := p.Decode(raw)
	if err != nil || message.AllowedFrom(p.Central) != nil {
		o.reject()
		return
	}
	request, ok := message.Payload().(p.Request)
	if !ok {
		return
	}
	payload, err := p.DecodeRunCommandPayload(request.Payload)
	o.mu.Lock()
	defer o.mu.Unlock()
	if err != nil || request.OperationName != p.RunCommandOperation || request.OperationRevision != p.RunCommandRevision || request.Environment != nil || request.IdempotencyKey != nil || (payload.WorkspaceName != o.expected.WorkspaceName || payload.Arguments.Mode != o.expected.Arguments.Mode || payload.Arguments.Program != o.expected.Arguments.Program || payload.Arguments.CWD != o.expected.Arguments.CWD || !slices.Equal(payload.Arguments.Args, o.expected.Arguments.Args)) {
		o.invalid = true
	}
	o.requests = append(o.requests, operationCommandFrame{
		request: request.RequestID, operation: request.OperationID,
		execution: request.ExecutionID, project: request.ProjectID, agent: request.AgentID,
		mount: request.Mount.MountID, workspace: request.Mount.WorkspaceID,
	})
}

type operationCommandResponse struct {
	http.ResponseWriter
	observer *operationCommandWire
}

func (w *operationCommandResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *operationCommandResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buffered, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err != nil {
		return conn, buffered, err
	}
	// The standard server has no buffered response before Upgrade. Preserve
	// its original reader (including already buffered request bytes), and put
	// both subsequent buffered and direct writes through the same observer.
	if buffered == nil || buffered.Writer == nil || buffered.Writer.Buffered() != 0 {
		w.observer.reject()
		return conn, buffered, nil // Observation failure never changes native I/O.
	}
	observed := &operationCommandConnection{Conn: conn, observer: w.observer}
	return observed, bufio.NewReadWriter(buffered.Reader, bufio.NewWriter(observed)), nil
}

type operationCommandConnection struct {
	net.Conn
	observer *operationCommandWire
	mu       sync.Mutex // Native Gorilla writers already serialize frame writes.
	header   bool
	invalid  bool
	buffer   []byte
	message  []byte
	fragment bool
}

func (c *operationCommandConnection) Write(raw []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, err := c.Conn.Write(raw)
	if n > 0 {
		c.consume(raw[:n])
	}
	return n, err
}

func (c *operationCommandConnection) Close() error {
	err := c.Conn.Close()
	c.mu.Lock()
	if len(c.buffer) != 0 || c.fragment {
		c.reject()
	}
	c.mu.Unlock()
	return err
}

func (c *operationCommandConnection) reject() {
	c.invalid = true
	clear(c.buffer)
	clear(c.message)
	c.buffer, c.message = nil, nil
	c.observer.reject()
}

func (c *operationCommandConnection) consume(raw []byte) {
	if c.invalid {
		return
	}
	if len(c.buffer)+len(raw) > p.MaxMessageBytes+4096 {
		c.reject()
		return
	}
	c.buffer = append(c.buffer, raw...)
	if !c.header {
		end := bytes.Index(c.buffer, []byte("\r\n\r\n"))
		if end < 0 {
			if len(c.buffer) > 4096 {
				c.reject()
			}
			return
		}
		if !bytes.HasPrefix(c.buffer, []byte("HTTP/1.1 101 ")) {
			c.reject()
			return
		}
		c.buffer = c.buffer[end+4:]
		c.header = true
	}
	for len(c.buffer) >= 2 {
		first, second := c.buffer[0], c.buffer[1]
		final, opcode := first&128 != 0, first&15
		if first&112 != 0 || second&128 != 0 {
			c.reject() // No extensions or server masking in the real adapter.
			return
		}
		header, length := 2, uint64(second&127)
		if length == 126 {
			if len(c.buffer) < 4 {
				return
			}
			header, length = 4, uint64(binary.BigEndian.Uint16(c.buffer[2:4]))
		} else if length == 127 {
			if len(c.buffer) < 10 {
				return
			}
			header, length = 10, binary.BigEndian.Uint64(c.buffer[2:10])
		}
		if length > p.MaxMessageBytes || opcode >= 8 && (!final || length > 125) {
			c.reject()
			return
		}
		if uint64(len(c.buffer)-header) < length {
			return
		}
		payload := c.buffer[header : header+int(length)]
		switch opcode {
		case 1:
			if c.fragment {
				c.reject()
				return
			}
			c.message = append(c.message[:0], payload...)
			c.fragment = !final
		case 0:
			if !c.fragment || len(c.message)+len(payload) > p.MaxMessageBytes {
				c.reject()
				return
			}
			c.message = append(c.message, payload...)
			c.fragment = !final
		case 8, 9, 10:
		default:
			c.reject()
			return
		}
		c.buffer = c.buffer[header+int(length):]
		if opcode < 8 && final {
			c.observer.message(c.message)
			clear(c.message)
			c.message = c.message[:0]
		}
	}
}

// Only the original Store's return boundary is controlled. The Attempt is
// physically Committed before the barrier; no acknowledgement is fabricated.
// The final-loss mode projects Unknown only after that final real COMMIT.
type operationCommandStore struct {
	fixtureStore
	execution                      i.ExecutionID
	mu                             sync.Mutex
	entered, release               chan struct{}
	releaseOnce                    sync.Once
	held, loss                     bool
	loseFinal, holdClaim           bool
	claimHeld                      bool
	physicalAttempt, physicalFinal f.CommitResult
	unknown                        f.ID[f.TransactionAttempt]
	problem                        bool
}

func (s *operationCommandStore) resume() { s.releaseOnce.Do(func() { close(s.release) }) }

func (s *operationCommandStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	details := cause.Details()
	// A Claim transaction is entered only after the executor has received the
	// original Attempt COMMIT return and opened its private running admission.
	// Pause before the actual Claim transaction/locks, so the Owner environment mutation can
	// commit normally. Resolve occurs earlier and cannot enter this barrier.
	s.mu.Lock()
	claim := s.holdClaim && !s.claimHeld && s.physicalAttempt.State() == f.Committed && details.Kind == f.RecoveryCause && details.Owner == "runner-command"
	if claim {
		s.claimHeld = true
		close(s.entered)
	}
	s.mu.Unlock()
	if claim {
		select {
		case <-s.release:
		case <-ctx.Done():
			s.mu.Lock()
			s.problem = true
			s.mu.Unlock()
		}
	}
	owners := details.Primary.OwnerIDs()
	ours := details.Kind == f.CommandsCause && details.Primary.Namespace() == "tool.operation" && details.Primary.Command() == "prepare" && len(owners) == 1 && owners[0] == s.execution.String()
	var phase, attempt, request string
	var version int64
	var retired, reportAvailable bool
	observed := false
	result := s.fixtureStore.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := fn(ctx, tx); err != nil {
			return err
		}
		if !ours {
			return nil
		}
		x, err := s.fixtureStore.InTx(tx)
		if err == nil {
			err = x.QueryRow(ctx, `SELECT o.phase,o.version,COALESCE(a.attempt_id,''),COALESCE(a.backend_request_id,''),COALESCE(a.retired,false),EXISTS(SELECT 1 FROM agenteam_tool.command_results r WHERE r.operation_id=o.operation_id AND r.attempt_id=a.attempt_id AND r.request_id=a.backend_request_id) FROM agenteam_tool.operations o LEFT JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id AND a.operation_id=o.operation_id WHERE o.execution_id=$1 AND o.operation_kind='runner.run-command'`, s.execution.String()).Scan(&phase, &version, &attempt, &request, &retired, &reportAvailable)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			s.mu.Lock()
			s.problem = true
			s.mu.Unlock()
			return nil
		}
		observed = true
		return nil
	})
	if !ours || !observed || result.State() != f.Committed {
		return result
	}
	s.mu.Lock()
	if !s.held && phase == "running" && version == 2 && attempt != "" && p.ID(request).Valid() && !retired {
		s.held = true
		s.physicalAttempt = result
		if s.holdClaim {
			s.mu.Unlock()
			return result
		}
		close(s.entered)
		s.mu.Unlock()
		select {
		case <-s.release:
		case <-ctx.Done():
			s.mu.Lock()
			s.problem = true
			s.mu.Unlock()
		}
		return result
	}
	if s.loseFinal && !s.loss && phase == "failed" && version == 3 && retired && reportAvailable {
		s.loss = true
		s.physicalFinal = result
		s.mu.Unlock()
		return f.UnknownResult(s.unknown, cause)
	}
	s.mu.Unlock()
	return result
}

// Keep the old read-only fixture unchanged. This explicit profile reuses its
// real Account/Owner, lifecycle, Agent facade and underlying preparation ports,
// while reconciling only the actual run-command registration before Launch.
func newOperationCommandFacts(t *testing.T, privateRoot string, seed func(*runnerservice.Service, rc.EnrollmentMaterial)) (*toolFactsFixture, *model.RuntimeAuthority, *secret.Service) {
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
	// Retain the real source fixture, but formally remove its ordinary entry
	// before Launch. Its unselected Secret remains stored, not injected.
	ordinaryFields := c.ordinary.Fields()
	removed, err := b.service.DeleteVariable(ctxFor(t), b.ownerBrowser.actor, meta(t, "command-environment-empty", &ordinaryFields.Version), b.project.ID, ordinaryFields.ID)
	firstRoundRequire(t, err)
	if removed.Validate() != nil || !removed.Fields().Changed || removed.Fields().Deleted == nil || removed.Fields().Deleted.ID != ordinaryFields.ID || removed.Fields().Deleted.Version != ordinaryFields.Version+1 {
		t.Fatal("formal ordinary environment removal did not commit")
	}
	modelCapture, err := model.NewToolCallFactsCapture(modelService, c.authority)
	firstRoundRequire(t, err)
	c.models.provider = modelCapture
	v.bindRetirementRuntime(t, modelAuthority, modelSecrets)
	var ordinary bool
	firstRoundRequire(t, b.raw.QueryRow(ctxFor(t), `SELECT role='user' FROM agenteam_account.users WHERE id::text=$1`, b.ownerBrowser.actor.Details().UserID).Scan(&ordinary))
	if !ordinary {
		t.Fatal("Tool configuration must use the original non-admin Project Owner")
	}
	source, err := registry.NewRunCommandMetadataSource(b.tracked)
	firstRoundRequire(t, err)
	v.registry, err = registry.New(b.tracked, registry.Options{RunnerSources: map[string]registry.RunnerSource{tc.RunnerRunCommandKey: source}, Authorizations: registry.Authorizations{Sessions: b.accounts, Projects: c.v.agent.providers.Projects}})
	firstRoundRequire(t, err)
	b.tx(t, []f.LockRequest{registry.RegistryLock(f.Exclusive)}, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		var err error
		v.spec, err = v.registry.ReconcileRunnerInTx(ctx, tx, tc.RunnerRunCommandKey)
		return err
	})
	if v.spec.Validate() != nil {
		t.Fatal("explicit real run-command registration failed")
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
	create, err := rc.NewCreate(rc.CreateRequest{RunnerID: id[rc.Runner](t), Name: "tool-facts-runner", Description: "owned offline metadata", Tags: []string{}, RootPath: privateRoot})
	firstRoundRequire(t, err)
	created, err := runners.Execute(ctxFor(t), b.adminBrowser.actor, "tool-facts-runner", create)
	firstRoundRequire(t, err)
	if created.Receipt.Validate() != nil || created.Receipt.Runner.Status != rc.Offline || created.Material == nil || !created.Material.Token.Valid() || created.Material.ExpiresAt.Validate() != nil {
		t.Fatal("formal Runner creation or its original private material is invalid")
	}
	v.runner = created.Receipt.Runner
	if seed != nil {
		seed(runners, *created.Material)
	}
	current, err := v.agents.GetAgent(ctxFor(t), b.ownerBrowser.actor, b.project.ID, c.v.agentID)
	firstRoundRequire(t, err)
	tools := []i.ToolID{v.spec.ToolID}
	mounts := []ac.RunnerMountDeclaration{{RunnerID: v.runner.ID, Workspace: "tool-call.workspace"}}
	allow := ac.ApprovalAllow
	emptySecrets := []i.ProjectVariableID{}
	change, err := ac.NewAgentUpdate(ac.AgentUpdateFields{AllowedToolIDs: &tools, RunnerMounts: &mounts, ApprovalPolicy: &allow, AllowedSecretVariableIDs: &emptySecrets, ApprovalModelRef: ac.NullableChange[mc.ModelID]{Present: true}})
	firstRoundRequire(t, err)
	before := current.Fields().Core.Version
	updated, err := v.agents.UpdateAgent(ctxFor(t), b.ownerBrowser.actor, meta(t, "tool-facts-configure", &before), b.project.ID, c.v.agentID, change)
	firstRoundRequire(t, err)
	if updated.Validate() != nil || updated.Fields().RunnerMounts == nil || len(*updated.Fields().RunnerMounts) != 1 {
		t.Fatal("Agent command did not publish its canonical Runner mapping")
	}
	v.mount, v.version = (*updated.Fields().RunnerMounts)[0], updated.Fields().Agent.Fields().Core.Version
	if v.version != before+1 || v.mount.RunnerID != v.runner.ID || v.mount.Workspace != mounts[0].Workspace || !reflect.DeepEqual(updated.Fields().Agent.Fields().AllowedToolIDs, tools) || updated.Fields().Agent.Fields().Core.ApprovalPolicy != ac.ApprovalAllow || updated.Fields().Agent.Fields().Core.ApprovalModelRef != nil || len(updated.Fields().Agent.Fields().AllowedSecretVariableIDs) != 0 {
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
	if observedMounts.capture != 1 || observedMounts.discover != 1 || observedMounts.verify == 0 || observedMounts.tx != c.models.tx || len(fields.Tools) != 1 || fields.Tools[0].ToolID != v.spec.ToolID || fields.Tools[0].SpecRevision != v.spec.SpecRevision || fields.Tools[0].BindingSnapshot.RunnerBinding != tc.RunCommandRegistration().Binding || len(fields.Mounts.Mounts) != 1 || fields.Mounts.Mounts[0].ID != v.mount.MountID || fields.Mounts.Mounts[0].WorkspaceID != v.mount.WorkspaceID || fields.Mounts.Mounts[0].RunnerID != v.runner.ID || fields.Agent.Fields().Core.Version != v.version || fields.Environment.Fields().AgentVersion != v.version || len(fields.Environment.Fields().Variables) != 0 || len(fields.Environment.Fields().Secrets) != 0 || fields.Agent.Fields().Core.ApprovalPolicy != ac.ApprovalAllow || len(fields.Agent.Fields().AllowedSecretVariableIDs) != 0 || !fields.Model.Snapshot.Capabilities.ToolCalls || fields.Model.Snapshot.Capabilities.ParallelToolCalls {
		t.Fatal("real Preparation lost selected Tool/Mount/current model input")
	}
	builder, err := execution.NewContextBuilder(work.TaskContextBuilder{})
	firstRoundRequire(t, err)
	v.context, err = builder.BuildContext(ctxFor(t), v.input)
	firstRoundRequire(t, err)
	return v, modelAuthority, modelSecrets
}

func (v *toolFactsFixture) newOperationCommandDriver(t *testing.T, builder *execution.ContextBuilder, authority *model.RuntimeAuthority, secrets *secret.Service) *execution.ToolCallDriver {
	t.Helper()
	c, b := v.capture, v.capture.v.base
	loop, err := agentloop.NewRunCommandToolCallController(v.model)
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
	currentEnvironment, err := pv.NewCurrentProcessEnvironment(b.tracked, c.authority)
	firstRoundRequire(t, err)
	driver, err := execution.NewRunCommandToolCallDriver(b.tracked, c.authority, execution.ToolCallDependencies{Base: execution.DirectTextDependencies{Context: builder, Processes: captureProviderProcesses{c.v.agent.guard}, Projects: c.projectAuthority, Agents: c.configuration, Skills: skills, Events: box, Lifecycle: lifecycle, Models: models, Environment: environment, Mounts: mounts}, Loop: loop, Tools: v.registry, CurrentEnvironment: currentEnvironment})
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

func (fixture *operationCommandFixture) factsSQL(ctx context.Context, sql postgres.SQLExecutor, expectedOperations int64) (toolFactsIdentity, error) {
	v := fixture.facts
	expectedCanonical := fixture.expectedCanonical
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
	if state != "running" || out.Version != int64(v.capture.created.Version)+3 || schema != 2 || !pending || through != 2 || !sameTx || transcriptCount != 2 || argumentCount != 1 || callCount != 1 || operationCount != expectedOperations || terminalCount != 0 || mountRefs != 1 || retired != 0 || modelLeases != 1 || environmentLeases != 0 {
		return out, errors.New("tool checkpoint is partial, terminal, or detached from live provider facts")
	}
	if projectID != v.capture.created.ProjectID.String() || agentID != v.capture.created.AgentID.String() || out.Invocation != v.model.invocation.String() || out.Call != "owned-read-file-call" || modelName != v.input.Fields().Tools[0].ModelVisibleName || toolID != v.spec.ToolID.String() || spec != int64(v.spec.SpecRevision) || operation != "run-command" || revision != "1" || protocol != 1 {
		return out, errors.New("Tool call lost original model or captured binding identities")
	}
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
		for _, private := range []string{fixture.rootPath, "owned-model-capture-material", "owned-environment-capture-material", fixture.arguments.Program} {
			if bytes.Contains(raw, []byte(private)) {
				return out, errors.New("private output leaked into immutable pre-call input")
			}
		}
	}
	return out, nil
}

type operationCommandFixture struct {
	facts             *toolFactsFixture
	device            *runnerhttp.DeviceHandler
	wire              *operationCommandWire
	rootPath          string
	manager           *workspace.Manager
	arguments         p.RunCommandArguments
	expectedCanonical []byte
	checkpoint        toolFactsIdentity
	expectedRequests  int
}

func newOperationCommandFixture(t *testing.T) *operationCommandFixture {
	t.Helper()
	return newOperationCommandFixtureWithChild(t, "TestRunCommandOperationChild", []string{"run-command-operation-child", "literal ; $(not-shell)"}, nil)
}

// The optional test child is selected before the real Model/ToolCall producer.
// The old entry retains its exact argv; no persisted call is rewritten.
func newOperationCommandFixtureWithChild(t *testing.T, selector string, argv []string, wrap func(http.Handler) http.Handler) *operationCommandFixture {
	t.Helper()
	directory := readFileAdapterPrivateDirectory(t)
	rootPath := filepath.Join(directory, "root")
	readFileAdapterOK(t, os.Mkdir(rootPath, 0700), "owned command Runner root")
	var runners *runnerservice.Service
	var enrollment rc.EnrollmentMaterial
	facts, modelAuthority, modelSecrets := newOperationCommandFacts(t, rootPath, func(service *runnerservice.Service, material rc.EnrollmentMaterial) {
		runners, enrollment = service, material
	})
	defer func() { enrollment = rc.EnrollmentMaterial{} }()
	if runners == nil || !enrollment.Token.Valid() {
		t.Fatal("formal Runner enrollment handoff missing")
	}
	program, err := os.Executable()
	readFileAdapterOK(t, err, "actual owned argv helper executable")
	arguments := p.RunCommandArguments{Mode: "argv", Program: program, Args: append([]string{"-test.run=^" + selector + "$", "--"}, argv...), CWD: "."}
	v := &operationCommandFixture{facts: facts, rootPath: rootPath, arguments: arguments}
	original, err := json.Marshal(struct {
		Mount   string   `json:"mount_id"`
		Mode    string   `json:"mode"`
		Program string   `json:"program"`
		Args    []string `json:"args"`
		CWD     string   `json:"cwd"`
	}{facts.mount.MountID.String(), arguments.Mode, arguments.Program, arguments.Args, arguments.CWD})
	readFileAdapterOK(t, err, "private Model argv arguments")
	decoded, err := tc.DecodeRunCommandToolArguments(ctxFor(t), original)
	readFileAdapterOK(t, err, "formal protected command argument decoder")
	if decoded.MountID != facts.mount.MountID || decoded.Arguments.Mode != arguments.Mode || decoded.Arguments.Program != arguments.Program || decoded.Arguments.CWD != arguments.CWD || !slices.Equal(decoded.Arguments.Args, arguments.Args) {
		t.Fatal("Model command arguments changed")
	}
	argsJSON, err := json.Marshal(arguments.Args)
	readFileAdapterOK(t, err, "independent expected argv representation")
	v.expectedCanonical = []byte(fmt.Sprintf(`{"args":%s,"cwd":".","mode":"argv","mount_id":%q,"program":%q}`, argsJSON, facts.mount.MountID.String(), program))
	t.Cleanup(func() { clear(original); clear(argsJSON); clear(v.expectedCanonical) })
	facts.model = &toolFactsModel{arguments: original, invocation: id[mc.Invocation](t), closeEntered: make(chan struct{}), release: make(chan struct{})}
	builder, err := execution.NewContextBuilder(work.TaskContextBuilder{})
	readFileAdapterOK(t, err, "real command context builder")
	facts.driver = facts.newOperationCommandDriver(t, builder, modelAuthority, modelSecrets)
	completion := facts.startHeld(t)
	facts.model.unhold()
	completed := toolFactsWait(t, completion)
	readFileAdapterOK(t, completed.err, "actual command ToolCall producer")
	got, err := v.factsSQL(ctxFor(t), facts.capture.v.base.raw, 0)
	readFileAdapterOK(t, err, "command ToolCall independent canonical facts")
	if completed.receipt.ExecutionID != facts.capture.created.ID || completed.receipt.Status != ec.Running || completed.receipt.TranscriptThrough != 2 || int64(completed.receipt.Version) != got.Version || completed.receipt.SnapshotID.String() != got.Snapshot || completed.receipt.RoundID.String() != got.Round || completed.receipt.CallID.String() != got.Logical {
		t.Fatal("command ToolCall receipt differs from canonical checkpoint")
	}
	facts.checkBinding(t, got)
	v.checkpoint = got
	root, err := os.Open(rootPath)
	readFileAdapterOK(t, err, "trusted root descriptor")
	manager, managerErr := workspace.New(root)
	closeErr := root.Close()
	readFileAdapterOK(t, managerErr, "real Workspace manager")
	managerCleanupRegistered := false
	t.Cleanup(func() {
		if !managerCleanupRegistered {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if manager.Drain(ctx) != nil {
				t.Error("original Manager construction-failure cleanup did not return")
			}
		}
	})
	readFileAdapterOK(t, closeErr, "borrowed root descriptor close")
	// The server cleanup is registered before Manager/Client below, so actual
	// Tool owner -> Client.Run -> Manager -> server/Runner retirement is joined.
	wire := &operationCommandWire{expected: p.RunCommandPayload{WorkspaceName: facts.mount.Workspace, Arguments: arguments}}
	device, err := runnerhttp.NewDeviceHandler(runners)
	readFileAdapterOK(t, err, "real device HTTP handler")
	v.device, v.wire, v.manager = device, wire, manager
	var actualHandler http.Handler = wire.wrap(device)
	if wrap != nil {
		actualHandler = wrap(actualHandler)
	}
	server := newReadFileAdapterServer(t, actualHandler, runners)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := manager.Drain(ctx); err != nil {
			t.Error("original Workspace manager did not actually join")
		}
	})
	managerCleanupRegistered = true
	handler, err := runcommand.New(manager)
	readFileAdapterOK(t, err, "real run-command Handler")
	configuration := identity.Configuration{CentralURL: server.origin, RunnerID: p.ID(facts.runner.ID.String()), RootPath: rootPath}
	states := make(chan control.ConnectionState, 16)
	client, err := control.NewRunCommand(control.Options{IdentityFile: filepath.Join(directory, "identity.json"), Configuration: &configuration, EnrollmentToken: enrollment.Token, RunnerVersion: "run-command-operation-fixture", Roots: server.roots, Observe: func(ctx context.Context, state control.ConnectionState) {
		select {
		case states <- state:
		case <-ctx.Done():
		}
	}}, handler)
	readFileAdapterOK(t, err, "real authenticated Runner control client")
	enrollment = rc.EnrollmentMaterial{}
	runDone := make(chan struct{})
	var runErr error
	go func() { runErr = client.Run(context.Background()); close(runDone) }()
	t.Cleanup(func() {
		client.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Drain(ctx); err != nil {
			t.Error("original control client Drain did not join")
		}
		select {
		case <-runDone:
			if !errors.Is(runErr, context.Canceled) {
				t.Error("original control Run outcome differs")
			}
		case <-ctx.Done():
			t.Error("original control Run return missing")
		}
		if frames, valid := wire.snapshot(); !valid || len(frames) != v.expectedRequests {
			t.Error("final actual wire observation was incomplete or changed request count")
		}
	})
	connected, cancelConnect := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancelConnect()
	for {
		select {
		case state := <-states:
			if state == control.Connected {
				return v
			}
		case <-runDone:
			t.Fatal("original Runner returned before authenticated Hello")
		case <-connected.Done():
			t.Fatal("original Runner did not complete authenticated Hello")
		}
	}
}

func (v *operationCommandFixture) executor(t *testing.T, loseFinal, holdClaim bool) (*toolruntime.RunCommandExecutor, *toolruntime.RunCommandAuthority, *operationCommandStore) {
	t.Helper()
	b := v.facts.capture.v.base
	store := &operationCommandStore{fixtureStore: b.tracked, execution: v.facts.capture.created.ID, entered: make(chan struct{}), release: make(chan struct{}), loseFinal: loseFinal, holdClaim: holdClaim, unknown: id[f.TransactionAttempt](t)}
	process, err := v.facts.capture.v.agent.guard.CurrentProcess()
	readFileAdapterOK(t, err, "original process identity")
	authority, err := toolruntime.NewRunCommandAuthority(store, v.facts.capture.v.agent.guard, process, v.facts.driver)
	readFileAdapterOK(t, err, "real Execution-backed Tool run-command authority")
	resolver, err := mount.NewCommandResolver(store, v.facts.capture.configuration, authority)
	readFileAdapterOK(t, err, "real current Mount resolver")
	dispatcher, err := runnerhttp.NewCommandDispatcher(v.device, resolver)
	readFileAdapterOK(t, err, "real WSS dispatcher")
	schemas, err := toolruntime.NewRegistrySchemaValidator(store, v.facts.registry)
	readFileAdapterOK(t, err, "actual historical Registry schema validator")
	executor, err := toolruntime.NewRunCommandExecutor(authority, resolver, dispatcher, schemas)
	readFileAdapterOK(t, err, "explicit single run-command executor")
	t.Cleanup(func() {
		store.resume()
		authority.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := authority.Drain(ctx); err != nil || !authority.Joined() {
			t.Error("original Tool owner did not actually retire")
		}
		store.mu.Lock()
		problem := store.problem
		store.mu.Unlock()
		if problem {
			t.Error("original Store return barrier observation failed")
		}
	})
	return executor, authority, store
}

type operationCommandCompletion struct {
	result toolruntime.RunCommandOutcome
	err    error
}

func startOperationCommand(t *testing.T, executor *toolruntime.RunCommandExecutor, execution i.ExecutionID, store *operationCommandStore) <-chan operationCommandCompletion {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	done := make(chan operationCommandCompletion, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		out, err := executor.ExecuteRunCommand(ctx, execution)
		done <- operationCommandCompletion{out, err}
	}()
	t.Cleanup(func() {
		store.resume()
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("original ExecuteRunCommand goroutine did not actually return")
		}
	})
	select {
	case <-store.entered:
	case <-done:
		t.Fatal("original run-command returned before the selected original transaction barrier")
	case <-ctx.Done():
		t.Fatal("original run-command did not reach the selected original transaction barrier")
	}
	return done
}

func waitOperationCommand(t *testing.T, done <-chan operationCommandCompletion) operationCommandCompletion {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	select {
	case out := <-done:
		return out
	case <-ctx.Done():
		t.Fatal("original ExecuteRunCommand actual return missing")
	}
	return operationCommandCompletion{}
}

func requireOperationCommandIdentity(t *testing.T, before, after operationCommandSQL) {
	t.Helper()
	if before.operation != after.operation || before.attempt != after.attempt || before.request != after.request || before.fingerprint != after.fingerprint || before.inputDigest != after.inputDigest || !bytes.Equal(before.input, after.input) {
		t.Fatal("the original Operation/Attempt/RID or logical input identity changed")
	}
}

func (v *operationCommandFixture) requireWire(t *testing.T, count int, row operationCommandSQL) {
	t.Helper()
	frames, valid := v.wire.snapshot()
	if !valid || len(frames) != count {
		t.Fatal("actual successful WSS request writes differ")
	}
	if count == 1 {
		frame := frames[0]
		if string(frame.request) != row.request || string(frame.operation) != row.operation || string(frame.execution) != row.execution || string(frame.project) != row.project || string(frame.agent) != row.agent || frame.mount != p.ID(v.facts.mount.MountID.String()) || frame.workspace != p.ID(v.facts.mount.WorkspaceID.String()) {
			t.Fatal("actual wire identities differ from the original durable Attempt")
		}
	}
}

const (
	operationCommandStdout = "literal ; $(not-shell)\nstdout α\n"
	operationCommandStderr = "stderr β\n"
)

// This is the only child entry and it is never selected as a PG behaviour top.
// The actual Runner Owner starts this absolute binary with literal argv, joins
// both pipes and Wait, and transports the original output to the real Central.
func TestRunCommandOperationChild(t *testing.T) {
	at := slices.Index(os.Args, "--")
	if at < 0 || at+1 >= len(os.Args) || os.Args[at+1] != "run-command-operation-child" {
		t.Skip("explicit owned command child only")
	}
	if len(os.Args) != at+3 || os.Args[at+2] != "literal ; $(not-shell)" {
		os.Exit(91)
	}
	if n, err := os.Stdout.WriteString(operationCommandStdout); err != nil || n != len(operationCommandStdout) {
		os.Exit(92)
	}
	if n, err := os.Stderr.WriteString(operationCommandStderr); err != nil || n != len(operationCommandStderr) {
		os.Exit(93)
	}
	os.Exit(23)
}

type operationCommandSQL struct {
	operation, attempt, request, project, agent, execution, process      string
	phase, attemptPhase, outcome, fingerprint, inputDigest               string
	version, ordinal, fence                                              int64
	retired                                                              bool
	input, terminal, attemptOutcome, report, stdout, stderr              []byte
	reportDigest, stdoutDigest, stderrDigest, resultRequest              string
	reportBytes, stdoutBytes, stderrBytes, operations, attempts, results int64
}

func (v *operationCommandFixture) sql(t *testing.T) operationCommandSQL {
	t.Helper()
	var out operationCommandSQL
	e := v.facts.capture.created.ID.String()
	err := v.facts.capture.v.base.raw.QueryRow(ctxFor(t), `SELECT o.operation_id,o.active_attempt_id,a.backend_request_id,o.project_id,o.agent_id,o.execution_id,a.process_id,o.phase,a.phase,COALESCE(o.outcome,''),o.fingerprint,o.input_digest,o.version,a.ordinal,a.fence,a.retired,o.input_data,COALESCE(o.terminal_data,''::bytea),COALESCE(a.outcome_data,''::bytea),COALESCE(r.body_data,''::bytea),COALESCE(r.body_sha256,''),COALESCE(r.body_bytes,0),COALESCE(r.request_id,''),COALESCE(r.stdout_data,''::bytea),COALESCE(r.stdout_sha256,''),COALESCE(r.stdout_bytes,0),COALESCE(r.stderr_data,''::bytea),COALESCE(r.stderr_sha256,''),COALESCE(r.stderr_bytes,0),
 (SELECT count(*) FROM agenteam_tool.operations WHERE execution_id=$1),
 (SELECT count(*) FROM agenteam_tool.attempts WHERE execution_id=$1),
 (SELECT count(*) FROM agenteam_tool.command_results WHERE execution_id=$1)
 FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id AND a.operation_id=o.operation_id AND a.execution_id=o.execution_id AND a.project_id=o.project_id AND a.agent_id=o.agent_id
 LEFT JOIN agenteam_tool.command_results r ON r.operation_id=o.operation_id AND r.attempt_id=a.attempt_id AND r.execution_id=o.execution_id AND r.project_id=o.project_id AND r.agent_id=o.agent_id AND r.request_id=a.backend_request_id
 WHERE o.execution_id=$1 AND o.operation_kind='runner.run-command'`, e).Scan(&out.operation, &out.attempt, &out.request, &out.project, &out.agent, &out.execution, &out.process, &out.phase, &out.attemptPhase, &out.outcome, &out.fingerprint, &out.inputDigest, &out.version, &out.ordinal, &out.fence, &out.retired, &out.input, &out.terminal, &out.attemptOutcome, &out.report, &out.reportDigest, &out.reportBytes, &out.resultRequest, &out.stdout, &out.stdoutDigest, &out.stdoutBytes, &out.stderr, &out.stderrDigest, &out.stderrBytes, &out.operations, &out.attempts, &out.results)
	readFileAdapterOK(t, err, "independent original command Operation/Attempt/result observation")
	process, err := v.facts.capture.v.agent.guard.CurrentProcess()
	readFileAdapterOK(t, err, "original ProcessGuard identity")
	if !p.ID(out.operation).Valid() || !p.ID(out.attempt).Valid() || !p.ID(out.request).Valid() || out.project != v.facts.capture.v.base.project.ID.String() || out.agent != v.facts.capture.v.agentID.String() || out.execution != e || out.process != process.String() || out.operations != 1 || out.attempts != 1 || out.ordinal != 1 || out.fence != 1 || out.fingerprint == "" || out.inputDigest != fmt.Sprintf("sha256:%x", sha256.Sum256(out.input)) {
		t.Fatal("durable original command identity or scope differs")
	}
	for _, raw := range [][]byte{out.input, out.terminal} {
		for _, private := range []string{v.arguments.Program, v.arguments.Args[len(v.arguments.Args)-1], operationCommandStdout, operationCommandStderr} {
			if bytes.Contains(raw, []byte(private)) {
				t.Fatal("command metadata duplicated protected argv or output")
			}
		}
	}
	return out
}

func requireOperationCommandReport(t *testing.T, report p.RunCommandResult, stdout, stderr []byte) {
	t.Helper()
	if report.Validate() != nil || report.State != "completed" || report.ExitCode != 23 || report.Signal != 0 || report.Cancelled || report.TimedOut || report.CompletionCode != nil || report.StdoutTruncated || report.StderrTruncated || !report.OutputComplete || !report.ProcessGroupTerminationConfirmed || string(report.StdoutBytes) != fmt.Sprint(len(operationCommandStdout)) || string(report.StderrBytes) != fmt.Sprint(len(operationCommandStderr)) || !bytes.Equal(stdout, []byte(operationCommandStdout)) || !bytes.Equal(stderr, []byte(operationCommandStderr)) {
		t.Fatal("known original nonzero command report or either complete stream changed")
	}
}

func (v *operationCommandFixture) requireTerminal(t *testing.T, row operationCommandSQL, reportAvailable bool) {
	t.Helper()
	var terminal struct {
		Format                                     int
		Operation, Attempt, Request, Outcome, Code string
		Attempted                                  bool
		ReportAvailable                            bool `json:"report_available"`
		Result                                     *struct {
			Operation, Attempt, Request, SHA256, StdoutSHA256, StderrSHA256 string
			Bytes, StdoutBytes, StderrBytes                                 f.Progress
		}
	}
	if json.Unmarshal(row.terminal, &terminal) != nil || !bytes.Equal(row.terminal, row.attemptOutcome) || len(row.terminal) > 32768 || terminal.Format != 1 || terminal.Operation != row.operation || terminal.Attempt != row.attempt || terminal.Request != row.request || !row.retired || row.version != 3 || row.phase != "failed" || row.attemptPhase != "failed" || row.outcome != "error" || terminal.Outcome != "failure" {
		t.Fatal("original command did not atomically retain one failed retired Attempt")
	}
	if !reportAvailable {
		if terminal.Attempted || terminal.ReportAvailable || terminal.Result != nil || terminal.Code != "not_dispatched" || row.results != 0 || len(row.report) != 0 || len(row.stdout) != 0 || len(row.stderr) != 0 {
			t.Fatal("nonempty current environment did not retire as unsent without a report")
		}
		return
	}
	var sameTransaction bool
	err := v.facts.capture.v.base.raw.QueryRow(ctxFor(t), `SELECT o.xmin=a.xmin AND a.xmin=r.xmin FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id AND a.operation_id=o.operation_id JOIN agenteam_tool.command_results r ON r.operation_id=o.operation_id AND r.attempt_id=a.attempt_id AND r.request_id=a.backend_request_id WHERE o.operation_id=$1 AND o.operation_kind='runner.run-command'`, row.operation).Scan(&sameTransaction)
	readFileAdapterOK(t, err, "independent complete command final transaction identity")
	if !sameTransaction || !terminal.Attempted || !terminal.ReportAvailable || terminal.Code != "command_exit_nonzero" || terminal.Result == nil || row.results != 1 || row.resultRequest != row.request {
		t.Fatal("known exit23 was lost, called success, or detached from the original final transaction")
	}
	report, err := p.DecodeRunCommandResult(row.report)
	readFileAdapterOK(t, err, "original stored complete command report")
	requireOperationCommandReport(t, report, row.stdout, row.stderr)
	canonical, err := p.EncodeRunCommandResult(report)
	readFileAdapterOK(t, err, "explicit complete command report codec")
	ref := terminal.Result
	if !bytes.Equal(canonical, row.report) || len(row.report) > 32768 || row.reportBytes != int64(len(row.report)) || row.reportDigest != fmt.Sprintf("sha256:%x", sha256.Sum256(row.report)) || row.stdoutBytes != int64(len(operationCommandStdout)) || row.stderrBytes != int64(len(operationCommandStderr)) || row.stdoutDigest != fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(operationCommandStdout))) || row.stderrDigest != fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(operationCommandStderr))) || ref.Operation != row.operation || ref.Attempt != row.attempt || ref.Request != row.request || ref.SHA256 != row.reportDigest || int64(ref.Bytes) != row.reportBytes || ref.StdoutSHA256 != row.stdoutDigest || int64(ref.StdoutBytes) != row.stdoutBytes || ref.StderrSHA256 != row.stderrDigest || int64(ref.StderrBytes) != row.stderrBytes {
		t.Fatal("complete report or stream byte/hash references differ from original output")
	}
}

func (v *operationCommandFixture) requireJoined(t *testing.T, row operationCommandSQL, reportAvailable bool) {
	t.Helper()
	v.facts.driver.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if v.facts.driver.Drain(ctx) != nil || !v.facts.driver.Joined() || !v.facts.model.Joined() {
		t.Fatal("original command ToolCall/Model did not join")
	}
	got, err := v.factsSQL(ctxFor(t), v.facts.capture.v.base.raw, 1)
	readFileAdapterOK(t, err, "unchanged running ToolCall after command Operation")
	if got != v.checkpoint {
		t.Fatal("command Operation altered original nonterminal Execution checkpoint")
	}
	_, _, begin, result, closes, joined := v.facts.model.observed()
	if begin != 1 || result != 1 || closes != 1 || !joined {
		t.Fatal("command repeated Model or skipped its original close")
	}
	after := v.sql(t)
	requireOperationCommandIdentity(t, row, after)
	v.requireTerminal(t, after, reportAvailable)
}

func TestRunCommandOperation(t *testing.T) {
	t.Run("committed-attempt-command-and-lookup", func(t *testing.T) {
		v := newOperationCommandFixture(t)
		v.expectedRequests = 1
		executor, authority, store := v.executor(t, true, false)
		executionID := v.facts.capture.created.ID
		done := startOperationCommand(t, executor, executionID, store)
		pending := v.sql(t)
		if pending.phase != "running" || pending.attemptPhase != "running" || pending.version != 2 || pending.retired || pending.results != 0 || len(pending.terminal) != 0 {
			t.Fatal("Attempt not durable before held original COMMIT return")
		}
		v.requireWire(t, 0, pending)
		store.resume()
		completed := waitOperationCommand(t, done)
		requireCode(t, completed.err, f.CommitUnknown)
		if completed.result.OperationID().Validate() == nil {
			t.Fatal("lost final acknowledgment published an outcome")
		}
		store.mu.Lock()
		attempt, final, loss, problem := store.physicalAttempt, store.physicalFinal, store.loss, store.problem
		store.mu.Unlock()
		if attempt.State() != f.Committed || final.State() != f.Committed || !loss || problem {
			t.Fatal("acknowledgment loss did not follow both original physical COMMIT returns")
		}
		committed := v.sql(t)
		requireOperationCommandIdentity(t, pending, committed)
		v.requireTerminal(t, committed, true)
		v.requireWire(t, 1, committed)
		// Only original Lookup observes the lost final receipt. No second Execute,
		// new identity, Model invocation or Runner Request is permitted.
		for range 2 {
			observed, found, err := executor.LookupRunCommand(ctxFor(t), executionID)
			readFileAdapterOK(t, err, "original command outcome Lookup")
			phase, outcome, attempted := observed.Status()
			report, stdout, stderr, available := observed.Result()
			if !found || phase != "failed" || outcome != "failure" || !attempted || !available || observed.Code() != "command_exit_nonzero" || observed.OperationID().String() != committed.operation || observed.AttemptID().String() != committed.attempt || string(observed.RequestID()) != committed.request {
				t.Fatal("Lookup changed original known command error or identity")
			}
			requireOperationCommandReport(t, report, stdout, stderr)
			raw, err := p.EncodeRunCommandResult(report)
			readFileAdapterOK(t, err, "Lookup complete original report")
			if !bytes.Equal(raw, committed.report) {
				t.Fatal("Lookup changed original report facts")
			}
			clear(stdout)
			clear(stderr)
			after := v.sql(t)
			requireOperationCommandIdentity(t, committed, after)
			v.requireTerminal(t, after, true)
			v.requireWire(t, 1, after)
		}
		authority.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if authority.Drain(ctx) != nil || !authority.Joined() {
			t.Fatal("original Unknown owner did not join after Lookup")
		}
		v.requireJoined(t, committed, true)
		t.Log("run_command_operation original_attempt_return_before_send=true wire_requests=1 exit_code=23 known_tool_error=true complete_report_and_streams=true original_commit_then_unknown=true lookup_no_resend=true")
	})
	t.Run("environment-added-before-claim-no-dispatch", func(t *testing.T) {
		v := newOperationCommandFixture(t)
		executor, authority, store := v.executor(t, false, true)
		done := startOperationCommand(t, executor, v.facts.capture.created.ID, store)
		pending := v.sql(t)
		store.mu.Lock()
		claim, attempt, problem := store.claimHeld, store.physicalAttempt, store.problem
		store.mu.Unlock()
		if !claim || attempt.State() != f.Committed || problem || pending.phase != "running" || pending.attemptPhase != "running" || pending.version != 2 || pending.retired || pending.results != 0 {
			t.Fatal("Claim did not follow original Attempt COMMIT return")
		}
		v.requireWire(t, 0, pending)
		b := v.facts.capture.v.base
		input := createInput(t, "COMMAND_EMPTY_ENTRY", "")
		changed, err := b.service.CreateVariable(ctxFor(t), b.ownerBrowser.actor, meta(t, "command-current-environment-entry", nil), b.project.ID, input)
		readFileAdapterOK(t, err, "formal Owner ordinary empty-value entry")
		fields := changed.Fields()
		ordinary := fields.Variable.Fields()
		if changed.Validate() != nil || !fields.Changed || fields.AuditID == nil || fields.EventID == nil || ordinary.ID != input.Fields().ID || ordinary.ProjectID != b.project.ID || ordinary.Value != "" || ordinary.Version != 1 {
			t.Fatal("formal Owner empty-value mutation differs")
		}
		var currentCount int64
		var committed bool
		err = b.raw.QueryRow(ctxFor(t), `SELECT v.type='variable' AND v.value='' AND v.deleted_at IS NULL AND v.version=1 AND h.version=v.version AND h.event_id::text=$2 AND c.receipt->>'audit_id'=$3,
 (SELECT count(*) FROM agenteam_projectvariable.variables x WHERE x.project_id=v.project_id AND x.type='variable' AND x.deleted_at IS NULL)
 FROM agenteam_projectvariable.variables v JOIN agenteam_projectvariable.history h ON h.variable_id=v.id AND h.version=v.version JOIN agenteam_projectvariable.commands c ON c.id=h.operation_id AND c.project_id=v.project_id AND c.target_id=v.id WHERE v.id::text=$1 AND v.project_id::text=$4`, ordinary.ID.String(), fields.EventID.String(), fields.AuditID.String(), b.project.ID.String()).Scan(&committed, &currentCount)
		readFileAdapterOK(t, err, "independent committed ordinary entry before Claim")
		if !committed || currentCount != 1 {
			t.Fatal("empty value was mistaken for current environment absence")
		}
		current, err := v.facts.agents.GetAgent(ctxFor(t), b.ownerBrowser.actor, b.project.ID, v.facts.capture.v.agentID)
		readFileAdapterOK(t, err, "unchanged current Allow and Mount before Claim")
		if current.Fields().Core.Version != v.facts.version || current.Fields().Core.ApprovalPolicy != ac.ApprovalAllow || !slices.Equal(current.Fields().AllowedMountIDs, []i.MountID{v.facts.mount.MountID}) || len(current.Fields().AllowedSecretVariableIDs) != 0 {
			t.Fatal("ordinary mutation changed another command authority condition")
		}
		v.requireWire(t, 0, pending)
		store.resume()
		completed := waitOperationCommand(t, done)
		readFileAdapterOK(t, completed.err, "current environment not_dispatched retirement")
		phase, outcome, attempted := completed.result.Status()
		_, stdout, stderr, available := completed.result.Result()
		if phase != "failed" || outcome != "failure" || attempted || available || len(stdout) != 0 || len(stderr) != 0 || completed.result.Code() != "not_dispatched" || completed.result.OperationID().String() != pending.operation || completed.result.AttemptID().String() != pending.attempt || string(completed.result.RequestID()) != pending.request {
			t.Fatal("nonempty environment dispatched or changed original unsent identity")
		}
		final := v.sql(t)
		requireOperationCommandIdentity(t, pending, final)
		v.requireTerminal(t, final, false)
		v.requireWire(t, 0, final)
		authority.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if authority.Drain(ctx) != nil || !authority.Joined() {
			t.Fatal("unsent original Tool owner did not join")
		}
		v.requireJoined(t, final, false)
		t.Log("run_command_operation attempt_returned_before_claim=true formal_ordinary_empty_value_entry=true current_env_nonempty=true wire_requests=0 not_dispatched_retired=true")
	})
}
