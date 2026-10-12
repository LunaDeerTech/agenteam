//go:build integration && linux

package projectvariable_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
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
	"github.com/LunaDeerTech/agenteam/internal/runner/filesystem"
	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	"github.com/LunaDeerTech/agenteam/internal/runner/writefile"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/jackc/pgx/v5"
)

// Runner write-file Operation fixture. The actual Execution write profile is
// composed below; only the original Model JSON result and Store-return timing
// are controlled. Neither a raw SQL ToolCall seed nor a caller-supplied grant
// may stand in for the real Owner/Preparation/Registry/Execution producer.

// This observer surrounds the real DeviceHandler's hijacked TLS connection.
// It neither substitutes a peer/Handler nor changes any write result. Only
// bytes actually accepted by the original Write enter the bounded decoder;
// request identities are retained, while payload equality is a private bool.
type operationWriteFileWire struct {
	mu       sync.Mutex
	expected p.WriteFilePayload
	requests []operationWriteFileFrame
	invalid  bool
}

type operationWriteFileFrame struct {
	request, operation, execution, project, agent, mount, workspace p.ID
}

func (o *operationWriteFileWire) wrap(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(&operationWriteFileResponse{ResponseWriter: w, observer: o}, r)
	})
}

func (o *operationWriteFileWire) snapshot() ([]operationWriteFileFrame, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.requests), !o.invalid
}

func (o *operationWriteFileWire) reject() {
	o.mu.Lock()
	o.invalid = true
	o.mu.Unlock()
}

func (o *operationWriteFileWire) message(raw []byte) {
	message, err := p.Decode(raw)
	if err != nil || message.AllowedFrom(p.Central) != nil {
		o.reject()
		return
	}
	request, ok := message.Payload().(p.Request)
	if !ok {
		return
	}
	payload, err := p.DecodeWriteFilePayload(request.Payload)
	defer clear(payload.Arguments.Data)
	o.mu.Lock()
	defer o.mu.Unlock()
	if err != nil || request.OperationName != p.WriteFileOperation || request.OperationRevision != p.WriteFileRevision || (payload.WorkspaceName != o.expected.WorkspaceName || payload.Arguments.Path != o.expected.Arguments.Path || payload.Arguments.Overwrite != o.expected.Arguments.Overwrite || !bytes.Equal(payload.Arguments.Data, o.expected.Arguments.Data)) {
		o.invalid = true
	}
	o.requests = append(o.requests, operationWriteFileFrame{
		request: request.RequestID, operation: request.OperationID,
		execution: request.ExecutionID, project: request.ProjectID, agent: request.AgentID,
		mount: request.Mount.MountID, workspace: request.Mount.WorkspaceID,
	})
}

type operationWriteFileResponse struct {
	http.ResponseWriter
	observer *operationWriteFileWire
}

func (w *operationWriteFileResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *operationWriteFileResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
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
	observed := &operationWriteFileConnection{Conn: conn, observer: w.observer}
	return observed, bufio.NewReadWriter(buffered.Reader, bufio.NewWriter(observed)), nil
}

type operationWriteFileConnection struct {
	net.Conn
	observer *operationWriteFileWire
	mu       sync.Mutex // Native Gorilla writers already serialize frame writes.
	header   bool
	invalid  bool
	buffer   []byte
	message  []byte
	fragment bool
}

func (c *operationWriteFileConnection) Write(raw []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, err := c.Conn.Write(raw)
	if n > 0 {
		c.consume(raw[:n])
	}
	return n, err
}

func (c *operationWriteFileConnection) Close() error {
	err := c.Conn.Close()
	c.mu.Lock()
	if len(c.buffer) != 0 || c.fragment {
		c.reject()
	}
	c.mu.Unlock()
	return err
}

func (c *operationWriteFileConnection) reject() {
	c.invalid = true
	clear(c.buffer)
	clear(c.message)
	c.buffer, c.message = nil, nil
	c.observer.reject()
}

func (c *operationWriteFileConnection) consume(raw []byte) {
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
type operationWriteFileStore struct {
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

func (s *operationWriteFileStore) resume() { s.releaseOnce.Do(func() { close(s.release) }) }

func (s *operationWriteFileStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	details := cause.Details()
	// A Claim transaction is entered only after the executor has received the
	// original Attempt COMMIT return and opened its private running admission.
	// Pause before the actual Claim transaction/locks, so Owner revocation can
	// commit normally. Resolve occurs earlier and cannot enter this barrier.
	s.mu.Lock()
	claim := s.holdClaim && !s.claimHeld && s.physicalAttempt.State() == f.Committed && details.Kind == f.RecoveryCause && details.Owner == "runner-write-file"
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
	var retired bool
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
			err = x.QueryRow(ctx, `SELECT o.phase,o.version,COALESCE(a.attempt_id,''),COALESCE(a.backend_request_id,''),COALESCE(a.retired,false) FROM agenteam_tool.operations o LEFT JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id AND a.operation_id=o.operation_id WHERE o.execution_id=$1 AND o.operation_kind='runner.write-file'`, s.execution.String()).Scan(&phase, &version, &attempt, &request, &retired)
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
	if s.loseFinal && !s.loss && phase == "succeeded" && version == 3 && retired {
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
// while reconciling only the actual write-file registration before Launch.
func newOperationWriteFacts(t *testing.T, privateRoot string, seed func(*runnerservice.Service, rc.EnrollmentMaterial)) (*toolFactsFixture, *model.RuntimeAuthority, *secret.Service) {
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
	source, err := registry.NewWriteFileMetadataSource(b.tracked)
	firstRoundRequire(t, err)
	v.registry, err = registry.New(b.tracked, registry.Options{RunnerSources: map[string]registry.RunnerSource{tc.RunnerWriteFileKey: source}, Authorizations: registry.Authorizations{Sessions: b.accounts, Projects: c.v.agent.providers.Projects}})
	firstRoundRequire(t, err)
	b.tx(t, []f.LockRequest{registry.RegistryLock(f.Exclusive)}, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		var err error
		v.spec, err = v.registry.ReconcileRunnerInTx(ctx, tx, tc.RunnerWriteFileKey)
		return err
	})
	if v.spec.Validate() != nil {
		t.Fatal("explicit real write-file registration failed")
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
	if observedMounts.capture != 1 || observedMounts.discover != 1 || observedMounts.verify == 0 || observedMounts.tx != c.models.tx || len(fields.Tools) != 1 || fields.Tools[0].ToolID != v.spec.ToolID || fields.Tools[0].SpecRevision != v.spec.SpecRevision || fields.Tools[0].BindingSnapshot.RunnerBinding != tc.WriteFileRegistration().Binding || len(fields.Mounts.Mounts) != 1 || fields.Mounts.Mounts[0].ID != v.mount.MountID || fields.Mounts.Mounts[0].WorkspaceID != v.mount.WorkspaceID || fields.Mounts.Mounts[0].RunnerID != v.runner.ID || fields.Agent.Fields().Core.Version != v.version || fields.Environment.Fields().AgentVersion != v.version || !fields.Model.Snapshot.Capabilities.ToolCalls || fields.Model.Snapshot.Capabilities.ParallelToolCalls {
		t.Fatal("real Preparation lost selected Tool/Mount/current model input")
	}
	builder, err := execution.NewContextBuilder(work.TaskContextBuilder{})
	firstRoundRequire(t, err)
	v.context, err = builder.BuildContext(ctxFor(t), v.input)
	firstRoundRequire(t, err)
	return v, modelAuthority, modelSecrets
}

func (v *toolFactsFixture) newOperationWriteDriver(t *testing.T, builder *execution.ContextBuilder, authority *model.RuntimeAuthority, secrets *secret.Service) *execution.ToolCallDriver {
	t.Helper()
	c, b := v.capture, v.capture.v.base
	loop, err := agentloop.NewWriteFileToolCallController(v.model)
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
	driver, err := execution.NewWriteFileToolCallDriver(b.tracked, c.authority, execution.ToolCallDependencies{Base: execution.DirectTextDependencies{Context: builder, Processes: captureProviderProcesses{c.v.agent.guard}, Projects: c.projectAuthority, Agents: c.configuration, Skills: skills, Events: box, Lifecycle: lifecycle, Models: models, Environment: environment, Mounts: mounts}, Loop: loop, Tools: v.registry})
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

func (fixture *operationWriteFileFixture) factsSQL(ctx context.Context, sql postgres.SQLExecutor, expectedOperations int64) (toolFactsIdentity, error) {
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
	if state != "running" || out.Version != int64(v.capture.created.Version)+3 || schema != 2 || !pending || through != 2 || !sameTx || transcriptCount != 2 || argumentCount != 1 || callCount != 1 || operationCount != expectedOperations || terminalCount != 0 || mountRefs != 1 || retired != 0 || modelLeases != 1 || environmentLeases != 1 {
		return out, errors.New("tool checkpoint is partial, terminal, or detached from live provider facts")
	}
	if projectID != v.capture.created.ProjectID.String() || agentID != v.capture.created.AgentID.String() || out.Invocation != v.model.invocation.String() || out.Call != "owned-read-file-call" || modelName != v.input.Fields().Tools[0].ModelVisibleName || toolID != v.spec.ToolID.String() || spec != int64(v.spec.SpecRevision) || operation != "write-file" || revision != "1" || protocol != 1 {
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
		for _, private := range []string{fixture.rootPath, "owned-model-capture-material", "owned-environment-capture-material", fixture.path} {
			if bytes.Contains(raw, []byte(private)) {
				return out, errors.New("private output leaked into immutable pre-call input")
			}
		}
	}
	return out, nil
}

type operationWriteFileSQL struct {
	operation, attempt, request, project, agent, execution, process string
	phase, attemptPhase, outcome, fingerprint, inputDigest          string
	version, ordinal, fence                                         int64
	retired                                                         bool
	input, terminal, attemptOutcome, result                         []byte
	resultDigest                                                    string
	resultBytes, operations, attempts, results                      int64
}

func (v *operationWriteFileFixture) sql(t *testing.T) operationWriteFileSQL {
	t.Helper()
	var out operationWriteFileSQL
	e := v.facts.capture.created.ID.String()
	err := v.facts.capture.v.base.raw.QueryRow(ctxFor(t), `SELECT o.operation_id,o.active_attempt_id,a.backend_request_id,o.project_id,o.agent_id,o.execution_id,a.process_id,o.phase,a.phase,COALESCE(o.outcome,''),o.fingerprint,o.input_digest,o.version,a.ordinal,a.fence,a.retired,o.input_data,COALESCE(o.terminal_data,''::bytea),COALESCE(a.outcome_data,''::bytea),COALESCE(r.body_data,''::bytea),COALESCE(r.body_sha256,''),COALESCE(r.body_bytes,0),(SELECT count(*) FROM agenteam_tool.operations WHERE execution_id=$1),(SELECT count(*) FROM agenteam_tool.attempts WHERE execution_id=$1),(SELECT count(*) FROM agenteam_tool.write_file_results WHERE execution_id=$1) FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id AND a.operation_id=o.operation_id AND a.execution_id=o.execution_id AND a.project_id=o.project_id AND a.agent_id=o.agent_id LEFT JOIN agenteam_tool.write_file_results r ON r.operation_id=o.operation_id AND r.attempt_id=a.attempt_id AND r.execution_id=o.execution_id AND r.project_id=o.project_id AND r.agent_id=o.agent_id WHERE o.execution_id=$1 AND o.operation_kind='runner.write-file'`, e).Scan(&out.operation, &out.attempt, &out.request, &out.project, &out.agent, &out.execution, &out.process, &out.phase, &out.attemptPhase, &out.outcome, &out.fingerprint, &out.inputDigest, &out.version, &out.ordinal, &out.fence, &out.retired, &out.input, &out.terminal, &out.attemptOutcome, &out.result, &out.resultDigest, &out.resultBytes, &out.operations, &out.attempts, &out.results)
	readFileAdapterOK(t, err, "independent durable Operation/Attempt observation")
	process, err := v.facts.capture.v.agent.guard.CurrentProcess()
	readFileAdapterOK(t, err, "original ProcessGuard identity")
	if !p.ID(out.operation).Valid() || !p.ID(out.attempt).Valid() || !p.ID(out.request).Valid() || out.project != v.facts.capture.v.base.project.ID.String() || out.agent != v.facts.capture.v.agentID.String() || out.execution != e || out.process != process.String() || out.operations != 1 || out.attempts != 1 || out.ordinal != 1 || out.fence != 1 || out.fingerprint == "" || out.inputDigest != fmt.Sprintf("sha256:%x", sha256.Sum256(out.input)) {
		t.Fatal("durable original identity or scope differs")
	}
	if bytes.Contains(out.input, []byte(v.path)) || bytes.Contains(out.input, []byte(base64.StdEncoding.EncodeToString(v.data))) {
		t.Fatal("Operation metadata duplicated protected input or file data")
	}
	return out
}

func (v *operationWriteFileFixture) executor(t *testing.T, loseFinal, holdClaim bool) (*toolruntime.WriteFileExecutor, *toolruntime.WriteFileAuthority, *operationWriteFileStore) {
	t.Helper()
	b := v.facts.capture.v.base
	store := &operationWriteFileStore{fixtureStore: b.tracked, execution: v.facts.capture.created.ID, entered: make(chan struct{}), release: make(chan struct{}), loseFinal: loseFinal, holdClaim: holdClaim, unknown: id[f.TransactionAttempt](t)}
	process, err := v.facts.capture.v.agent.guard.CurrentProcess()
	readFileAdapterOK(t, err, "original process identity")
	authority, err := toolruntime.NewWriteFileAuthority(store, v.facts.capture.v.agent.guard, process, v.facts.driver)
	readFileAdapterOK(t, err, "real Execution-backed Tool write-file authority")
	resolver, err := mount.NewWriteFileResolver(store, v.facts.capture.configuration, authority)
	readFileAdapterOK(t, err, "real current Mount resolver")
	dispatcher, err := runnerhttp.NewWriteFileDispatcher(v.device, resolver)
	readFileAdapterOK(t, err, "real WSS dispatcher")
	schemas, err := toolruntime.NewRegistrySchemaValidator(store, v.facts.registry)
	readFileAdapterOK(t, err, "actual historical Registry schema validator")
	executor, err := toolruntime.NewWriteFileExecutor(authority, resolver, dispatcher, schemas)
	readFileAdapterOK(t, err, "explicit single write-file executor")
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

type operationWriteFileCompletion struct {
	result toolruntime.WriteFileOutcome
	err    error
}

func startOperationWriteFile(t *testing.T, executor *toolruntime.WriteFileExecutor, execution i.ExecutionID, store *operationWriteFileStore) <-chan operationWriteFileCompletion {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	done := make(chan operationWriteFileCompletion, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		out, err := executor.ExecuteWriteFile(ctx, execution)
		done <- operationWriteFileCompletion{out, err}
	}()
	t.Cleanup(func() {
		store.resume()
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("original ExecuteWriteFile goroutine did not actually return")
		}
	})
	select {
	case <-store.entered:
	case <-done:
		t.Fatal("original write-file returned before the selected original transaction barrier")
	case <-ctx.Done():
		t.Fatal("original write-file did not reach the selected original transaction barrier")
	}
	return done
}

func waitOperationWriteFile(t *testing.T, done <-chan operationWriteFileCompletion) operationWriteFileCompletion {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	select {
	case out := <-done:
		return out
	case <-ctx.Done():
		t.Fatal("original ExecuteWriteFile actual return missing")
	}
	return operationWriteFileCompletion{}
}

func requireOperationWriteFileIdentity(t *testing.T, before, after operationWriteFileSQL) {
	t.Helper()
	if before.operation != after.operation || before.attempt != after.attempt || before.request != after.request || before.fingerprint != after.fingerprint || before.inputDigest != after.inputDigest || !bytes.Equal(before.input, after.input) {
		t.Fatal("the original Operation/Attempt/RID or logical input identity changed")
	}
}

func (v *operationWriteFileFixture) requireWire(t *testing.T, count int, row operationWriteFileSQL) {
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

func (v *operationWriteFileFixture) requireTerminal(t *testing.T, row operationWriteFileSQL, success bool) {
	t.Helper()
	var terminal struct {
		Format                                     int
		Operation, Attempt, Request, Outcome, Code string
		Attempted                                  bool
		Result                                     *struct {
			Operation, Attempt, SHA256 string
			Bytes                      f.Progress
		}
	}
	if json.Unmarshal(row.terminal, &terminal) != nil || !bytes.Equal(row.terminal, row.attemptOutcome) || len(row.terminal) > 32768 || terminal.Format != 1 || terminal.Operation != row.operation || terminal.Attempt != row.attempt || terminal.Request != row.request || !row.retired || row.version != 3 {
		t.Fatal("final metadata did not bind one original retired Attempt")
	}
	if success {
		var sameTransaction bool
		err := v.facts.capture.v.base.raw.QueryRow(ctxFor(t), `SELECT o.xmin=a.xmin AND a.xmin=r.xmin FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id AND a.operation_id=o.operation_id JOIN agenteam_tool.write_file_results r ON r.operation_id=o.operation_id AND r.attempt_id=a.attempt_id WHERE o.operation_id=$1 AND o.operation_kind='runner.write-file'`, row.operation).Scan(&sameTransaction)
		readFileAdapterOK(t, err, "independent original final transaction identity")
		if !sameTransaction {
			t.Fatal("write result and both original retirements did not share the final transaction")
		}
		want, err := p.EncodeWriteFileResult(p.WriteFileResult{Path: v.path, State: "committed", BytesWritten: len(v.data), Durable: true})
		readFileAdapterOK(t, err, "independently expected committed write-file result")
		if len(want) > 32768 || row.phase != "succeeded" || row.attemptPhase != "succeeded" || row.outcome != "success" || terminal.Outcome != "success" || terminal.Code != "" || !terminal.Attempted || terminal.Result == nil || row.results != 1 || !bytes.Equal(row.result, want) || row.resultBytes != int64(len(want)) || row.resultDigest != fmt.Sprintf("sha256:%x", sha256.Sum256(want)) || terminal.Result.Operation != row.operation || terminal.Result.Attempt != row.attempt || terminal.Result.SHA256 != row.resultDigest || int64(terminal.Result.Bytes) != row.resultBytes {
			t.Fatal("original full result was truncated, replaced or not atomically retired")
		}
	} else if row.phase != "failed" || row.attemptPhase != "failed" || row.outcome != "error" || terminal.Outcome != "failure" || terminal.Code != "not_dispatched" || terminal.Attempted || terminal.Result != nil || row.results != 0 || len(row.result) != 0 {
		t.Fatal("current Mount denial was not recorded as a retired unsent failure")
	}
}

type operationWriteFileFixture struct {
	facts                             *toolFactsFixture
	device                            *runnerhttp.DeviceHandler
	wire                              *operationWriteFileWire
	path, rootPath                    string
	manager                           *workspace.Manager
	data, original, expectedCanonical []byte
	checkpoint                        toolFactsIdentity
	expectedRequests                  int
}

func newOperationWriteFileFixture(t *testing.T) *operationWriteFileFixture {
	t.Helper()
	directory := readFileAdapterPrivateDirectory(t)
	rootPath := filepath.Join(directory, "root")
	readFileAdapterOK(t, os.Mkdir(rootPath, 0700), "owned write Runner root")
	var runners *runnerservice.Service
	var enrollment rc.EnrollmentMaterial
	facts, modelAuthority, modelSecrets := newOperationWriteFacts(t, rootPath, func(service *runnerservice.Service, material rc.EnrollmentMaterial) {
		runners, enrollment = service, material
	})
	defer func() { enrollment = rc.EnrollmentMaterial{} }()
	if runners == nil || !enrollment.Token.Valid() {
		t.Fatal("formal Runner enrollment handoff missing")
	}
	v := &operationWriteFileFixture{facts: facts, rootPath: rootPath, path: "private-write-file.bin", data: make([]byte, p.MaxWriteFileBytes), original: []byte("original-owned-file-before-dispatch\r\n")}
	for n := range v.data {
		v.data[n] = byte(n*31 + n/257)
	}
	t.Cleanup(func() { clear(v.data); clear(v.original) })
	arguments := p.WriteFileArguments{Path: v.path, Data: v.data, Overwrite: true}
	// Four explicit JSON fields are supplied only to the sole controlled Model.
	// The real write-profile producer seals the original and canonical bytes.
	original, err := json.Marshal(struct {
		Mount     string `json:"mount_id"`
		Path      string `json:"path"`
		Data      string `json:"data_base64"`
		Overwrite bool   `json:"overwrite"`
	}{facts.mount.MountID.String(), v.path, base64.StdEncoding.EncodeToString(v.data), true})
	readFileAdapterOK(t, err, "private Model write arguments")
	decoded, err := tc.DecodeWriteFileToolArguments(ctxFor(t), original)
	readFileAdapterOK(t, err, "formal protected write argument decoder")
	if decoded.MountID != facts.mount.MountID || decoded.Arguments.Path != v.path || !decoded.Arguments.Overwrite || !bytes.Equal(decoded.Arguments.Data, v.data) {
		t.Fatal("Model write input differs")
	}
	clear(decoded.Arguments.Data)
	v.expectedCanonical = []byte(fmt.Sprintf(`{"data_base64":%q,"mount_id":%q,"overwrite":true,"path":%q}`, base64.StdEncoding.EncodeToString(v.data), facts.mount.MountID.String(), v.path))
	facts.model = &toolFactsModel{arguments: original, invocation: id[mc.Invocation](t), closeEntered: make(chan struct{}), release: make(chan struct{})}
	builder, err := execution.NewContextBuilder(work.TaskContextBuilder{})
	readFileAdapterOK(t, err, "real write context builder")
	facts.driver = facts.newOperationWriteDriver(t, builder, modelAuthority, modelSecrets)
	completion := facts.startHeld(t)
	facts.model.unhold()
	completed := toolFactsWait(t, completion)
	readFileAdapterOK(t, completed.err, "actual write ToolCall producer")
	got, err := v.factsSQL(ctxFor(t), facts.capture.v.base.raw, 0)
	readFileAdapterOK(t, err, "write ToolCall independent canonical facts")
	if completed.receipt.ExecutionID != facts.capture.created.ID || completed.receipt.Status != ec.Running || completed.receipt.TranscriptThrough != 2 || int64(completed.receipt.Version) != got.Version || completed.receipt.SnapshotID.String() != got.Snapshot || completed.receipt.RoundID.String() != got.Round || completed.receipt.CallID.String() != got.Logical {
		t.Fatal("write ToolCall receipt differs from canonical checkpoint")
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
	wire := &operationWriteFileWire{expected: p.WriteFilePayload{WorkspaceName: facts.mount.Workspace, Arguments: arguments}}
	device, err := runnerhttp.NewDeviceHandler(runners)
	readFileAdapterOK(t, err, "real device HTTP handler")
	v.device, v.wire, v.manager = device, wire, manager
	server := newReadFileAdapterServer(t, wire.wrap(device), runners)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := manager.Drain(ctx); err != nil {
			t.Error("original Workspace manager did not actually join")
		}
	})
	managerCleanupRegistered = true
	project, agentID := facts.capture.v.base.project.ID, facts.capture.v.agentID
	handle, err := manager.Ensure(ctxFor(t), workspace.Identity{ProjectID: p.ID(project.String()), AgentID: p.ID(agentID.String()), Name: facts.mount.Workspace})
	readFileAdapterOK(t, err, "real Workspace Ensure")
	seeded, err := handle.Write(ctxFor(t), filesystem.WriteRequest{Path: v.path, Data: v.original, Overwrite: false})
	readFileAdapterOK(t, err, "original owned file seed through actual Root.Write")
	if seeded.State != filesystem.WriteCommitted || !seeded.Durable || seeded.BytesWritten != len(v.original) {
		t.Fatal("original file seed did not durably commit")
	}
	closeContext, cancelClose := context.WithTimeout(context.Background(), 5*time.Second)
	closeErr = handle.Drain(closeContext)
	cancelClose()
	readFileAdapterOK(t, closeErr, "original initial Workspace handle close")
	handler, err := writefile.New(manager)
	readFileAdapterOK(t, err, "real write-file Handler")
	configuration := identity.Configuration{CentralURL: server.origin, RunnerID: p.ID(facts.runner.ID.String()), RootPath: rootPath}
	states := make(chan control.ConnectionState, 16)
	client, err := control.NewWriteFile(control.Options{IdentityFile: filepath.Join(directory, "identity.json"), Configuration: &configuration, EnrollmentToken: enrollment.Token, RunnerVersion: "write-file-operation-fixture", Roots: server.roots, Observe: func(ctx context.Context, state control.ConnectionState) {
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

func (v *operationWriteFileFixture) readBytes(t *testing.T, expected []byte) {
	t.Helper()
	c := v.facts.capture
	handle, err := v.manager.Ensure(ctxFor(t), workspace.Identity{ProjectID: p.ID(c.created.ProjectID.String()), AgentID: p.ID(c.created.AgentID.String()), Name: v.facts.mount.Workspace})
	readFileAdapterOK(t, err, "independent real write-result read handle")
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if handle.Drain(ctx) != nil {
			t.Error("original independent read Handle did not join")
		}
	}()
	actual := make([]byte, 0, len(expected))
	defer func() { clear(actual) }()
	for offset := 0; offset < len(expected); {
		length := min(filesystem.MaxReadBytes, len(expected)-offset)
		result, err := handle.Read(ctxFor(t), filesystem.ReadRequest{Path: v.path, Offset: int64(offset), Length: length, Encoding: "base64"})
		readFileAdapterOK(t, err, "independent complete write-result read")
		if result.Path != v.path || result.Offset != int64(offset) || result.Encoding != "base64" || result.BytesRead != length || result.EOF != (offset+length == len(expected)) {
			t.Fatal("independent file size/range differs")
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(result.Data)
		readFileAdapterOK(t, err, "independent complete read decode")
		actual = append(actual, decoded...)
		clear(decoded)
		offset += length
	}
	if !bytes.Equal(actual, expected) {
		t.Fatal("independent complete bytes differ from original expected file")
	}
}

func (v *operationWriteFileFixture) requireJoined(t *testing.T, row operationWriteFileSQL, success bool) {
	t.Helper()
	v.facts.driver.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if v.facts.driver.Drain(ctx) != nil || !v.facts.driver.Joined() || !v.facts.model.Joined() {
		t.Fatal("original write ToolCall/Model did not join")
	}
	got, err := v.factsSQL(ctxFor(t), v.facts.capture.v.base.raw, 1)
	readFileAdapterOK(t, err, "unchanged running ToolCall after Operation")
	if got != v.checkpoint {
		t.Fatal("Operation altered the original nonterminal Execution checkpoint")
	}
	_, _, begin, result, closes, joined := v.facts.model.observed()
	if begin != 1 || result != 1 || closes != 1 || !joined {
		t.Fatal("Operation repeated Model or skipped original close")
	}
	after := v.sql(t)
	requireOperationWriteFileIdentity(t, row, after)
	v.requireTerminal(t, after, success)
}

func TestWriteFileOperation(t *testing.T) {
	t.Run("committed-attempt-write-and-lookup", func(t *testing.T) {
		v := newOperationWriteFileFixture(t)
		v.expectedRequests = 1
		executor, authority, store := v.executor(t, true, false)
		executionID := v.facts.capture.created.ID
		done := startOperationWriteFile(t, executor, executionID, store)
		pending := v.sql(t)
		if pending.phase != "running" || pending.attemptPhase != "running" || pending.version != 2 || pending.retired || pending.results != 0 || len(pending.terminal) != 0 {
			t.Fatal("Attempt not durably running before held original return")
		}
		v.requireWire(t, 0, pending)
		v.readBytes(t, v.original)
		store.resume()
		completed := waitOperationWriteFile(t, done)
		requireCode(t, completed.err, f.CommitUnknown)
		if completed.result.OperationID().Validate() == nil {
			t.Fatal("lost final acknowledgement exposed success")
		}
		store.mu.Lock()
		attempt, final, loss, problem := store.physicalAttempt, store.physicalFinal, store.loss, store.problem
		store.mu.Unlock()
		if attempt.State() != f.Committed || final.State() != f.Committed || !loss || problem {
			t.Fatal("acknowledgement loss did not follow both actual COMMIT returns")
		}
		committed := v.sql(t)
		requireOperationWriteFileIdentity(t, pending, committed)
		v.requireTerminal(t, committed, true)
		v.requireWire(t, 1, committed)
		v.readBytes(t, v.data)
		for range 2 {
			observed, found, err := executor.LookupWriteFile(ctxFor(t), executionID)
			readFileAdapterOK(t, err, "original write outcome Lookup")
			phase, outcome, attempted := observed.Status()
			result, hasResult := observed.Result()
			if !found || phase != "succeeded" || outcome != "success" || !attempted || !hasResult || observed.OperationID().String() != committed.operation || observed.AttemptID().String() != committed.attempt || string(observed.RequestID()) != committed.request || result.Path != v.path || result.State != "committed" || result.BytesWritten != len(v.data) || !result.Durable || result.CompletionCode != nil {
				t.Fatal("Lookup changed original publication or identity")
			}
			after := v.sql(t)
			requireOperationWriteFileIdentity(t, committed, after)
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
		t.Log("write_file_operation original_attempt_return_before_send=true bytes_written=524288 wire_requests=1 original_commit_then_unknown=true lookup_no_resend=true")
	})
	t.Run("revoke-before-claim-no-dispatch", func(t *testing.T) {
		v := newOperationWriteFileFixture(t)
		executor, authority, store := v.executor(t, false, true)
		done := startOperationWriteFile(t, executor, v.facts.capture.created.ID, store)
		pending := v.sql(t)
		store.mu.Lock()
		claim, attempt, problem := store.claimHeld, store.physicalAttempt, store.problem
		store.mu.Unlock()
		if !claim || attempt.State() != f.Committed || problem || pending.phase != "running" || pending.version != 2 || pending.retired || pending.results != 0 {
			t.Fatal("Claim did not follow the original committed Attempt return")
		}
		v.requireWire(t, 0, pending)
		b := v.facts.capture.v.base
		current, err := v.facts.agents.GetAgent(ctxFor(t), b.ownerBrowser.actor, b.project.ID, v.facts.capture.v.agentID)
		readFileAdapterOK(t, err, "ordinary Owner current Agent")
		version := current.Fields().Core.Version
		empty := []ac.RunnerMountDeclaration{}
		change, err := ac.NewAgentUpdate(ac.AgentUpdateFields{RunnerMounts: &empty})
		readFileAdapterOK(t, err, "formal Mount removal input")
		changed, err := v.facts.agents.UpdateAgent(ctxFor(t), b.ownerBrowser.actor, meta(t, "write-file-operation-revoke", &version), b.project.ID, v.facts.capture.v.agentID, change)
		readFileAdapterOK(t, err, "formal Owner Mount revocation")
		if changed.Validate() != nil || changed.Fields().Agent.Fields().Core.Version != version+1 || len(changed.Fields().Agent.Fields().AllowedMountIDs) != 0 || changed.Fields().RunnerMounts == nil || len(*changed.Fields().RunnerMounts) != 0 {
			t.Fatal("formal Mount revocation differs")
		}
		v.requireWire(t, 0, pending)
		store.resume()
		completed := waitOperationWriteFile(t, done)
		readFileAdapterOK(t, completed.err, "old ticket not_dispatched retirement")
		phase, outcome, attempted := completed.result.Status()
		_, hasResult := completed.result.Result()
		if phase != "failed" || outcome != "failure" || attempted || hasResult || completed.result.OperationID().String() != pending.operation || completed.result.AttemptID().String() != pending.attempt || string(completed.result.RequestID()) != pending.request {
			t.Fatal("revoked ticket dispatched or changed its original identity")
		}
		final := v.sql(t)
		requireOperationWriteFileIdentity(t, pending, final)
		v.requireTerminal(t, final, false)
		v.requireWire(t, 0, final)
		v.readBytes(t, v.original)
		authority.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if authority.Drain(ctx) != nil || !authority.Joined() {
			t.Fatal("unsent original Tool owner did not join")
		}
		v.requireJoined(t, final, false)
		t.Log("write_file_operation attempt_returned_before_claim=true formal_mount_revoked=true wire_requests=0 original_bytes_unchanged=true not_dispatched_retired=true")
	})
}
