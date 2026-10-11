//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	agentc "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	mt "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	runnerservice "github.com/LunaDeerTech/agenteam/internal/central/runner/service"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
)

// Domain integration only: the original fixture owns real Account/Project,
// PG/MinIO, ProcessGuard, Model HTTPS adapter and bounded service cleanup. Its
// explicit test-only Skills initializer remains; no default App, Runner
// connection, operation grant or physical workspace is claimed here.
type mountFirstRoundFixture struct {
	x        *firstRoundFixture
	binding  agentc.RunnerMountBinding
	runner   rc.Snapshot
	version  f.Version
	captured ec.ExecutionContext
	material []byte
	driver   *execution.DirectTextDriver
}

const mountFirstRoundPrivateRoot = "/mount-first-round/private-root"

func newMountFirstRoundFixture(t *testing.T, held bool) *mountFirstRoundFixture {
	t.Helper()
	x := newFirstRoundFixtureWithSource(t, held, true)
	v, c := x.capture.v, x.capture
	b := v.base
	var ordinary bool
	firstRoundRequire(t, b.raw.QueryRow(ctxFor(t), `SELECT role='user' FROM agenteam_account.users WHERE id::text=$1`, b.ownerBrowser.actor.Details().UserID).Scan(&ordinary))
	if !ordinary {
		t.Fatal("Mount configuration must use the real non-admin Owner")
	}
	authority, err := runnerservice.NewAuthority(b.tracked, b.accounts)
	firstRoundRequire(t, err)
	aud, err := audit.New(b.tracked, b.keys, audit.Authorizations{Sessions: b.accounts, System: b.accounts, Accounts: b.accounts, Runners: authority})
	firstRoundRequire(t, err)
	runners, err := runnerservice.New(authority, aud)
	firstRoundRequire(t, err)
	t.Cleanup(func() {
		runners.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := runners.Drain(ctx); err != nil {
			t.Error("original Runner management did not join", err)
		}
	})
	intent, err := rc.NewCreate(rc.CreateRequest{RunnerID: id[rc.Runner](t), Name: "first-round-runner", Description: "offline captured runner 中文", Tags: []string{}, RootPath: mountFirstRoundPrivateRoot})
	firstRoundRequire(t, err)
	created, err := runners.Execute(ctxFor(t), b.adminBrowser.actor, "mount-first-round-runner", intent)
	if err != nil || created.Receipt.Validate() != nil || created.Receipt.Runner.Status != rc.Offline {
		t.Fatal("formal Runner creation failed")
	}
	if created.Material == nil || !created.Material.Token.Valid() || created.Material.ExpiresAt.Validate() != nil {
		t.Fatal("first known Runner commit lacked valid one-time material")
	}
	// Enrollment material remains local and is never logged or projected.
	current, err := v.agent.agents.GetAgent(ctxFor(t), b.ownerBrowser.actor, b.project.ID, v.agentID)
	firstRoundRequire(t, err)
	declarations := []agentc.RunnerMountDeclaration{{RunnerID: created.Receipt.Runner.ID, Workspace: "first-round.workspace"}}
	update, err := agentc.NewAgentUpdate(agentc.AgentUpdateFields{RunnerMounts: &declarations})
	firstRoundRequire(t, err)
	version := current.Fields().Core.Version
	configured, err := v.agent.agents.UpdateAgent(ctxFor(t), b.ownerBrowser.actor, meta(t, "mount-first-round-configure", &version), b.project.ID, v.agentID, update)
	firstRoundRequire(t, err)
	if configured.Validate() != nil || configured.Fields().RunnerMounts == nil || len(*configured.Fields().RunnerMounts) != 1 {
		t.Fatal("formal configuration did not return one complete mapping")
	}
	out := &mountFirstRoundFixture{x: x, binding: (*configured.Fields().RunnerMounts)[0], runner: created.Receipt.Runner, version: configured.Fields().Agent.Fields().Core.Version}
	if out.version != version+1 || out.binding.RunnerID != out.runner.ID || out.binding.Workspace != declarations[0].Workspace {
		t.Fatal("Agent receipt changed declared Mount identity")
	}
	claim, _ := schedulerClaimRequest(t, v)
	dispatch, err := c.claims.ClaimTask(ctxFor(t), claim, c.launchPolicy)
	firstRoundRequire(t, err)
	c.request, err = dispatch.LaunchRequest()
	firstRoundRequire(t, err)
	handoff, counter := newBusyLaunchHandoff(t, v)
	launched, err := handoff.LaunchOnce(ctxFor(t), b.project.ID, dispatch.Summary().ID)
	firstRoundRequire(t, err)
	launches, _, executionCreated := counter.observed()
	if launches != 1 || executionCreated.Status != ec.Created || launched.Summary().ExecutionID == nil || *launched.Summary().ExecutionID != executionCreated.ID {
		t.Fatal("original Claim/Launch did not create the associated Execution")
	}
	c.created = executionCreated
	c.models.observe = c.observeModel
	// The old environment observer fixes Agent v2. Read the new actual version
	// from this complete input instead of weakening the older fixture.
	c.environment.observe = nil
	directory, err := runnerservice.NewExecutionMetadataDirectory(b.tracked, c.authority)
	firstRoundRequire(t, err)
	provider, err := mount.NewExecutionCapture(b.tracked, c.authority, directory)
	firstRoundRequire(t, err)
	observed := &executionMountObserver{provider: provider}
	preparation, err := execution.NewPreparationDriver(b.tracked, c.authority, execution.PreparationDependencies{Agents: c.configuration, Projects: c.projectAuthority, Processes: captureProviderProcesses{v.agent.guard}, Task: c.task, Skills: c.skills, Tools: c.tools, Models: c.models, Environment: c.environment, Mounts: observed})
	firstRoundRequire(t, err)
	t.Cleanup(func() {
		preparation.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := preparation.Drain(ctx); err != nil || !preparation.Joined() {
			t.Error("original preparation did not join", err)
		}
	})
	firstRoundRequire(t, preparation.Run(ctxFor(t), c.created.ID))
	c.requireProviderCalls(t, false)
	firstRoundRequire(t, errors.Join(c.environment.readErr, c.models.readErr))
	input := x.preparedInput(t)
	fields := input.Fields()
	if observed.capture != 1 || observed.discover != 1 || observed.tx != c.models.tx || observed.verify == 0 || len(fields.Mounts.Mounts) != 1 || fields.Agent.Fields().Core.Version != out.version || fields.Environment.Fields().AgentVersion != out.version || len(fields.Tools) != 0 || fields.Tools == nil || len(fields.Skills.Bindings) != 1 || fields.Agent.Fields().Core.InjectAgentsMD {
		t.Fatal("nonempty preparation changed the original provider/profile facts")
	}
	m := fields.Mounts.Mounts[0]
	if fields.Mounts.AttemptBinding != fields.AttemptBinding || fields.Mounts.AgentVersion != out.version || m.ID != out.binding.MountID || m.WorkspaceID != out.binding.WorkspaceID || m.RunnerID != out.runner.ID || m.RunnerVersion != out.runner.Version || m.RunnerName != out.runner.Name || m.RunnerDescription != out.runner.Description || m.Workspace != out.binding.Workspace || m.Name != out.binding.MountID.String() {
		t.Fatal("input did not retain full original safe Mount/Runner metadata")
	}
	counts, err := captureInputCounts(ctxFor(t), b.raw, c.created.ID.String())
	firstRoundRequire(t, err)
	if counts != [11]int64{1, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1} {
		t.Fatal("nonempty Mount input lost its other original providers")
	}
	firstRoundRequire(t, b.raw.QueryRow(ctxFor(t), `SELECT material FROM agenteam_mount.execution_captures WHERE execution_id=$1`, c.created.ID.String()).Scan(&out.material))
	var material mt.ExecutionMountSet
	if json.Unmarshal(out.material, &material) != nil || material.Validate() != nil || !bytes.Equal(jsonBytesForMount(material), jsonBytesForMount(fields.Mounts)) {
		t.Fatal("original Mount material differs from preparation")
	}
	builder, err := execution.NewContextBuilder(work.TaskContextBuilder{})
	firstRoundRequire(t, err)
	out.captured, err = builder.BuildContext(ctxFor(t), input)
	firstRoundRequire(t, err)
	out.driver = out.newDriver(t)
	return out
}

// This is the existing first-round composition with its actual Mount use port.
// Existing shared helpers and their empty-profile assertions remain unchanged.
func (v *mountFirstRoundFixture) newDriver(t *testing.T) *execution.DirectTextDriver {
	t.Helper()
	x, c := v.x, v.x.capture
	b := c.v.base
	builder, err := execution.NewContextBuilder(work.TaskContextBuilder{})
	firstRoundRequire(t, err)
	loop, err := agentloop.NewDirectTextController(x.runtime)
	firstRoundRequire(t, err)
	t.Cleanup(func() {
		loop.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := loop.Drain(ctx); err != nil || !loop.Joined() {
			t.Error("original nonempty Loop did not join", err)
		}
	})
	skills, err := skill.NewInitialRoundBindings(c.v.agent.providers.SkillAuthority)
	firstRoundRequire(t, err)
	models, err := model.NewExecutionLeaseRetirement(x.runtimeAuthority, x.secrets)
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
	uses, err := mount.NewExecutionUse(b.tracked, c.authority)
	firstRoundRequire(t, err)
	driver, err := execution.NewDirectTextDriver(b.tracked, c.authority, execution.DirectTextDependencies{Context: builder, Loop: loop, Processes: captureProviderProcesses{c.v.agent.guard}, Projects: c.projectAuthority, Agents: c.configuration, Skills: skills, Events: box, Lifecycle: lifecycle, Models: models, Environment: environment, Mounts: uses})
	firstRoundRequire(t, err)
	t.Cleanup(func() {
		driver.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := driver.Drain(ctx); err != nil || !driver.Joined() {
			t.Error("original nonempty Execution did not join before Loop/Model", err)
		}
	})
	return driver
}

type mountRoundIdentity struct{ Snapshot, Start, Round, Call, Input, Digest string }

func (v *mountFirstRoundFixture) facts(ctx context.Context, sql postgres.SQLExecutor, terminal bool) (mountRoundIdentity, error) {
	c := v.x.capture
	var identity mountRoundIdentity
	var material, inputRaw, snapshotRaw []byte
	var project, agent, attempt, captureDigest, state string
	var agentVersion, executionVersion, refs, active int64
	var retiredVersion *int64
	var retiredEvent, roundEvent *string
	err := sql.QueryRow(ctx, `SELECT u.project_id,u.agent_id,u.agent_version,u.attempt_binding,u.capture_digest,u.input_digest,u.snapshot_digest,
 c.material,p.input,s.content,s.id,s.start_id,r.id,r.call_id,e.status,e.version,
 (SELECT count(*) FROM agenteam_mount.execution_references m WHERE m.execution_id=e.id AND m.mount_id::text=$2 AND m.workspace_id::text=$3 AND m.runner_id::text=$4),
 (SELECT count(*) FROM agenteam_execution.executions a WHERE a.agent_id=e.agent_id AND a.status IN('created','preparing','running','waiting')),
 t.terminal_version,t.terminal_event_id::text,r.terminal_event_id::text
 FROM agenteam_mount.execution_uses u
 JOIN agenteam_mount.execution_captures c ON c.execution_id=u.execution_id
 JOIN agenteam_execution.preparation_inputs p ON p.execution_id=u.execution_id
 JOIN agenteam_execution.snapshots s ON s.execution_id=u.execution_id
 JOIN agenteam_execution.rounds r ON r.execution_id=u.execution_id AND r.snapshot_id=s.id
 JOIN agenteam_execution.executions e ON e.id=u.execution_id AND e.snapshot_id=s.id
 LEFT JOIN agenteam_mount.execution_use_retirements t ON t.execution_id=u.execution_id
 WHERE u.execution_id=$1 AND c.capture_digest=u.capture_digest AND p.input_digest=u.input_digest
 AND s.digest=u.snapshot_digest AND s.input_digest=u.input_digest`, c.created.ID.String(), v.binding.MountID.String(), v.binding.WorkspaceID.String(), v.binding.RunnerID.String()).Scan(&project, &agent, &agentVersion, &attempt, &captureDigest, &identity.Input, &identity.Digest, &material, &inputRaw, &snapshotRaw, &identity.Snapshot, &identity.Start, &identity.Round, &identity.Call, &state, &executionVersion, &refs, &active, &retiredVersion, &retiredEvent, &roundEvent)
	if err != nil {
		return identity, err
	}
	fields := v.captured.Input().Fields()
	if project != c.created.ProjectID.String() || agent != c.created.AgentID.String() || agentVersion != int64(v.version) || attempt != string(fields.AttemptBinding) || captureDigest != string(ec.TriggerInputDigest(v.material)) || !bytes.Equal(material, v.material) || !bytes.Equal(inputRaw, v.captured.Input().CanonicalBytes()) || identity.Input != string(v.captured.InputDigest()) || refs != 1 {
		return identity, errors.New("Mount use lost original full capture/input/reference identity")
	}
	snapshot, err := ec.DecodeDirectTextSnapshot(snapshotRaw)
	if err != nil || string(snapshot.Digest()) != identity.Digest || !bytes.Equal(snapshot.Fields().Context.CanonicalBytes(), v.captured.CanonicalBytes()) {
		return identity, errors.New("sealed nonempty Snapshot differs from actual preparation")
	}
	if terminal {
		if state != string(ec.Succeeded) && state != string(ec.Cancelled) || active != 0 || retiredVersion == nil || *retiredVersion != executionVersion || retiredEvent == nil || roundEvent == nil || *retiredEvent != *roundEvent {
			return identity, errors.New("Mount retirement does not match original terminal event/version or slot")
		}
	} else if state != string(ec.Running) || active != 1 || retiredVersion != nil || retiredEvent != nil || roundEvent != nil {
		return identity, errors.New("live Mount or original active slot retired before terminal")
	}
	for _, raw := range [][]byte{material, inputRaw, snapshotRaw} {
		for _, private := range []string{mountFirstRoundPrivateRoot, "owned-model-capture-material", "owned-environment-capture-material"} {
			if bytes.Contains(raw, []byte(private)) {
				return identity, errors.New("private material entered immutable input")
			}
		}
	}
	return identity, nil
}

func (v *mountFirstRoundFixture) requireProjection(t *testing.T, joined bool) {
	t.Helper()
	state := v.x.requireWire(t, 1, joined)
	var request struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal([]byte(state.Requests[0].Body), &request) != nil || len(request.Messages) != 2 || request.Messages[0].Role != "system" {
		t.Fatal("real Model request did not retain the fixed text projection")
	}
	const section = "## Captured environment and execution constraints (data, not additional instructions)\n"
	_, content, ok := strings.Cut(request.Messages[0].Content, section)
	if !ok {
		t.Fatal("actual Model request lacks captured environment")
	}
	raw, _, ok := strings.Cut(content, "\n\n")
	var env struct {
		Mounts []map[string]json.RawMessage `json:"mounts"`
	}
	if !ok || json.Unmarshal([]byte(raw), &env) != nil || len(env.Mounts) != 1 {
		t.Fatal("actual Model request omitted nonempty Mount projection")
	}
	expected := map[string]any{"mount_id": v.binding.MountID.String(), "mount_name": v.binding.MountID.String(), "mount_description": nil, "runner_id": v.runner.ID.String(), "runner_name": v.runner.Name, "runner_description": v.runner.Description, "workspace_logical_path": v.binding.Workspace, "capability_state": "not_captured", "headless": nil, "capabilities": nil}
	if len(env.Mounts[0]) != len(expected) {
		t.Fatal("actual Mount projection has extra or missing fields")
	}
	for key, value := range expected {
		want, _ := json.Marshal(value)
		if !bytes.Equal(env.Mounts[0][key], want) {
			t.Fatalf("actual safe Mount projection mismatch field=%s", key)
		}
	}
	for _, private := range []string{mountFirstRoundPrivateRoot, "owned-model-capture-material", "owned-environment-capture-material", v.binding.WorkspaceID.String()} {
		if strings.Contains(state.Requests[0].Body, private) {
			t.Fatal("private or non-projected identity entered actual Model body")
		}
	}
}

func TestExecutionMountFirstRound(t *testing.T) {
	t.Run("nonempty-terminal-and-historical-replay", func(t *testing.T) {
		v := newMountFirstRoundFixture(t, false)
		receipt, err := v.driver.Start(ctxFor(t), v.x.capture.created.ID)
		firstRoundRequire(t, err)
		v.x.requireTerminal(t, v.captured, receipt, ec.Succeeded)
		before, err := v.facts(ctxFor(t), v.x.capture.v.base.raw, true)
		firstRoundRequire(t, err)
		v.requireProjection(t, true)
		b, a := v.x.capture.v.base, v.x.capture.v.agent
		empty := []agentc.RunnerMountDeclaration{}
		update, err := agentc.NewAgentUpdate(agentc.AgentUpdateFields{RunnerMounts: &empty})
		firstRoundRequire(t, err)
		cleared, err := a.agents.UpdateAgent(ctxFor(t), b.ownerBrowser.actor, meta(t, "mount-first-round-clear", &v.version), b.project.ID, v.x.capture.v.agentID, update)
		firstRoundRequire(t, err)
		if cleared.Fields().RunnerMounts == nil || len(*cleared.Fields().RunnerMounts) != 0 {
			t.Fatal("formal clear did not remove current references")
		}
		replay, err := v.driver.Start(ctxFor(t), v.x.capture.created.ID)
		firstRoundRequire(t, err)
		actual, _ := json.Marshal(replay)
		original, _ := json.Marshal(receipt)
		after, err := v.facts(ctxFor(t), b.raw, true)
		if err != nil || before != after || !bytes.Equal(actual, original) {
			t.Fatal("historical terminal replay changed identity or reread current configuration", err)
		}
		v.requireProjection(t, true)
		firstRoundDriverJoined(t, v.driver)
	})
	t.Run("held-join-terminal-rollback-and-unknown", func(t *testing.T) {
		v := newMountFirstRoundFixture(t, true)
		store := v.x.capture.v.base.tracked
		executionID := v.x.capture.created.ID
		marker := errors.New("owned-mount-terminal-rollback")
		seen, returned := false, false
		var physical f.CommitResult
		var tentative mountRoundIdentity
		store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			d := cause.Details()
			if seen || d.Kind != f.JobCause || d.JobType != "execution-direct-text-terminal" || d.JobID != executionID.String() {
				return nil
			}
			sql, err := store.InTx(tx)
			if err != nil {
				return err
			}
			tentative, err = v.facts(ctx, sql, true)
			if err != nil {
				return err
			}
			seen = true
			return marker
		})
		store.mu.Lock()
		store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
			d := cause.Details()
			if seen && !returned && d.Kind == f.JobCause && d.JobType == "execution-direct-text-terminal" && d.JobID == executionID.String() {
				physical, returned = result, true
			}
		}
		store.mu.Unlock()
		originalStore := store.fixtureStore
		defer func() {
			store.setAfter(nil)
			store.mu.Lock()
			store.afterResult = nil
			store.mu.Unlock()
			store.fixtureStore = originalStore
		}()
		ctx, cancel := context.WithCancel(ctxFor(t))
		defer cancel()
		type outcome struct {
			receipt ec.DirectTextReceipt
			err     error
		}
		done := make(chan outcome, 1)
		go func() { r, e := v.driver.Start(ctx, executionID); done <- outcome{r, e} }()
		v.requireProjection(t, false)
		live, err := v.facts(ctxFor(t), v.x.capture.v.base.raw, false)
		firstRoundRequire(t, err)
		if v.driver.Joined() {
			t.Fatal("held Model falsely reported original Execution joined")
		}
		cancel()
		var failed outcome
		select {
		case failed = <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("original held Execution did not return after cancel")
		}
		store.setAfter(nil)
		store.mu.Lock()
		store.afterResult = nil
		store.mu.Unlock()
		if failed.err == nil || !seen || !returned || physical.State() != f.NotCommitted || live != tentative {
			t.Fatal("original terminal rollback did not follow complete same-Tx retirement")
		}
		v.x.requireWire(t, 1, true)
		v.x.reader.requireDestroyed(t, 1)
		rolledBack, err := v.facts(ctxFor(t), v.x.capture.v.base.raw, false)
		if err != nil || rolledBack != live {
			t.Fatal("terminal rollback leaked retirement or changed original live identity", err)
		}
		ready := false
		store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			d := cause.Details()
			if d.Kind != f.JobCause || d.JobType != "execution-direct-text-terminal" || d.JobID != executionID.String() {
				return nil
			}
			sql, err := store.InTx(tx)
			if err != nil {
				return err
			}
			got, err := v.facts(ctx, sql, true)
			if err != nil {
				return err
			}
			if got != live {
				return errors.New("recovery changed original run/use identity")
			}
			ready = true
			return nil
		})
		loss := &captureCommitReceiptLoss{fixtureStore: originalStore, attempt: id[f.TransactionAttempt](t), match: func(cause f.TransactionCause) bool {
			d := cause.Details()
			return ready && d.Kind == f.JobCause && d.JobType == "execution-direct-text-terminal" && d.JobID == executionID.String()
		}}
		store.fixtureStore = loss
		_, err = v.driver.ResolveUnknown(ctxFor(t), executionID)
		store.setAfter(nil)
		store.fixtureStore = originalStore
		requireCode(t, err, f.CommitUnknown)
		fired, committed := loss.observed()
		if !ready || !fired || committed.State() != f.Committed {
			t.Fatal("receipt loss did not follow actual terminal COMMIT")
		}
		committedFacts, err := v.facts(ctxFor(t), v.x.capture.v.base.raw, true)
		if err != nil || committedFacts != live {
			t.Fatal("committed terminal did not keep original use", err)
		}
		// The original transaction committed. This is only its acknowledgement
		// being hidden; no physical PostgreSQL Unknown or network fault is claimed.
		receipt, err := v.driver.ResolveUnknown(ctxFor(t), executionID)
		if !errors.Is(err, context.Canceled) {
			t.Fatal("original terminal observation lost cancellation cause")
		}
		v.x.requireTerminal(t, v.captured, receipt, ec.Cancelled)
		firstRoundDriverJoined(t, v.driver)
		v.requireProjection(t, true)
	})
}
