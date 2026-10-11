//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	mt "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	runnerservice "github.com/LunaDeerTech/agenteam/internal/central/runner/service"
)

// The original Model/Environment fixture owns the real Account, Project,
// Agent, Claim/Launch, providers and ProcessGuard. Its Project uses the
// documented test-only Skills receipt. No default App, model call, physical
// workspace, Runner connection or running Execution is claimed here.
type executionMountFixture struct {
	source  *modelEnvironmentFixture
	binding ac.RunnerMountBinding
	runner  rc.Snapshot
	version f.Version
	mounts  *executionMountObserver
	driver  *execution.PreparationDriver
}

type executionMountObserver struct {
	provider                  *mount.ExecutionCapture
	discover, capture, verify int
	tx                        f.Tx
	result                    mt.ExecutionMountSet
}

func (o *executionMountObserver) DiscoverExecutionMounts(ctx context.Context, r mt.ExecutionMountCaptureRequest) (mt.ExecutionMountCapturePlan, error) {
	p, err := o.provider.DiscoverExecutionMounts(ctx, r)
	if err == nil {
		o.discover++
	}
	return p, err
}
func (o *executionMountObserver) CaptureExecutionMountsInTx(ctx context.Context, tx f.Tx, r mt.ExecutionMountCaptureRequest, plan mt.ExecutionMountCapturePlan) (mt.ExecutionMountSet, error) {
	v, err := o.provider.CaptureExecutionMountsInTx(ctx, tx, r, plan)
	if err == nil {
		o.capture++
		o.tx, o.result = tx, v.Clone()
	}
	return v, err
}
func (o *executionMountObserver) VerifyExecutionMountMaterialInTx(ctx context.Context, tx f.Tx, expected mt.ExecutionMountSet) error {
	err := o.provider.VerifyExecutionMountMaterialInTx(ctx, tx, expected)
	if err == nil {
		o.verify++
	}
	return err
}

func newExecutionMountFixture(t *testing.T) *executionMountFixture {
	t.Helper()
	out := &executionMountFixture{}
	x := newModelEnvironmentFixtureWithPorts(t, nil, func(v *taskTransitionFixture, _ *model.Service, _ *ec.Policy) {
		b := v.base
		var ordinary bool
		if err := b.raw.QueryRow(ctxFor(t), `SELECT role='user' FROM agenteam_account.users WHERE id::text=$1`, b.ownerBrowser.actor.Details().UserID).Scan(&ordinary); err != nil || !ordinary {
			t.Fatal("capture configuration caller is not the actual ordinary Owner", err)
		}
		authority, err := runnerservice.NewAuthority(b.tracked, b.accounts)
		if err != nil {
			t.Fatal(err)
		}
		aud, err := audit.New(b.tracked, b.keys, audit.Authorizations{Sessions: b.accounts, System: b.accounts, Accounts: b.accounts, Runners: authority})
		if err != nil {
			t.Fatal(err)
		}
		runners, err := runnerservice.New(authority, aud)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			runners.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := runners.Drain(ctx); err != nil {
				t.Error("original Runner management calls failed to join", err)
			}
		})
		intent, err := rc.NewCreate(rc.CreateRequest{RunnerID: id[rc.Runner](t), Name: "capture-runner", Description: "owned offline preparation source", Tags: []string{}, RootPath: "/execution-mount-capture/private-root"})
		if err != nil {
			t.Fatal("invalid owned Runner creation intent")
		}
		created, err := runners.Execute(ctxFor(t), b.adminBrowser.actor, "execution-mount-runner-create", intent)
		if err != nil || created.Receipt.Validate() != nil || created.Receipt.Runner.Status != rc.Offline {
			t.Fatal("formal Runner creation did not produce its offline receipt")
		}
		if created.Material == nil || !created.Material.Token.Valid() || created.Material.ExpiresAt.Validate() != nil {
			t.Fatal("first known Runner creation omitted valid one-time material")
		}
		out.runner = created.Receipt.Runner // Enrollment material stays local and is never logged.
		current, err := v.agent.agents.GetAgent(ctxFor(t), b.ownerBrowser.actor, b.project.ID, v.agentID)
		if err != nil {
			t.Fatal("read actual Agent before Mount declaration", err)
		}
		declarations := []ac.RunnerMountDeclaration{{RunnerID: out.runner.ID, Workspace: "capture.workspace"}}
		request, err := ac.NewAgentUpdate(ac.AgentUpdateFields{RunnerMounts: &declarations})
		if err != nil {
			t.Fatal(err)
		}
		version := current.Fields().Core.Version
		receipt, err := v.agent.agents.UpdateAgent(ctxFor(t), b.ownerBrowser.actor, meta(t, "execution-mount-configure", &version), b.project.ID, v.agentID, request)
		if err != nil || receipt.Validate() != nil || receipt.Fields().RunnerMounts == nil || len(*receipt.Fields().RunnerMounts) != 1 {
			t.Fatal("formal Agent declaration did not commit one mapping", err)
		}
		out.binding = (*receipt.Fields().RunnerMounts)[0]
		out.version = receipt.Fields().Agent.Fields().Core.Version
		if out.binding.RunnerID != out.runner.ID || out.binding.Workspace != declarations[0].Workspace || out.version != version+1 {
			t.Fatal("formal Agent receipt differs from its original declaration")
		}
	})
	out.source = x
	// The old environment observation intentionally fixes Agent v2. This
	// fixture uses its real provider and verifies the actual new v3 below.
	x.environment.observe = nil
	directory, err := runnerservice.NewExecutionMetadataDirectory(x.v.base.tracked, x.authority)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := mount.NewExecutionCapture(x.v.base.tracked, x.authority, directory)
	if err != nil {
		t.Fatal(err)
	}
	out.mounts = &executionMountObserver{provider: provider}
	out.driver, err = execution.NewPreparationDriver(x.v.base.tracked, x.authority, execution.PreparationDependencies{Agents: x.configuration, Projects: x.projectAuthority, Processes: captureProviderProcesses{x.v.agent.guard}, Task: x.task, Skills: x.skills, Tools: x.tools, Models: x.models, Environment: x.environment, Mounts: out.mounts})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		out.driver.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := out.driver.Drain(ctx); err != nil || !out.driver.Joined() {
			t.Error("original Mount preparation did not join before providers/guard", err)
		}
	})
	return out
}

func (v *executionMountFixture) input(ctx context.Context, sql postgres.SQLExecutor) (ec.PreparationInput, error) {
	x := v.source
	var raw, material []byte
	var digest, attempt, binding, materialDigest string
	var mountCount, references int64
	err := sql.QueryRow(ctx, `SELECT p.input,p.input_digest,p.attempt_id,p.attempt_binding,m.material,m.capture_digest,m.mount_count,
 (SELECT count(*) FROM agenteam_mount.execution_references r WHERE r.execution_id=p.execution_id)
 FROM agenteam_execution.preparation_inputs p
 JOIN agenteam_execution.executions e ON(e.id,e.project_id,e.agent_id)=(p.execution_id,p.project_id,p.agent_id)
 JOIN agenteam_mount.execution_captures m ON(m.execution_id,m.project_id,m.agent_id)=(p.execution_id,p.project_id,p.agent_id)
 WHERE p.execution_id=$1 AND e.status='preparing' AND e.snapshot_id IS NULL AND e.started_at IS NULL
 AND e.completed_at IS NULL AND m.agent_version=$2 AND m.attempt_binding=p.attempt_binding`, x.created.ID.String(), int64(v.version)).Scan(&raw, &digest, &attempt, &binding, &material, &materialDigest, &mountCount, &references)
	if err != nil {
		return ec.PreparationInput{}, err
	}
	input, err := ec.DecodePreparationInput(raw)
	if err != nil {
		return ec.PreparationInput{}, err
	}
	fields := input.Fields()
	if string(input.Digest()) != digest || !bytes.Equal(input.CanonicalBytes(), raw) || !fields.Request.Equal(ec.PreparationRequest{ExecutionID: x.created.ID, Launch: x.request}) || attempt != x.attempt || string(fields.AttemptBinding) != binding || mountCount != 1 || references != 1 || ec.TriggerInputDigest(material) != f.Digest(materialDigest) {
		return ec.PreparationInput{}, errors.New("Mount input/material lost original identity or complete cardinality")
	}
	var captured mt.ExecutionMountSet
	if json.Unmarshal(material, &captured) != nil || captured.Validate() != nil || !bytes.Equal(jsonBytesForMount(captured), jsonBytesForMount(fields.Mounts)) {
		return ec.PreparationInput{}, errors.New("Mount material differs from immutable preparation input")
	}
	a, env := fields.Agent.Fields(), fields.Environment.Fields()
	if a.Core.Version != v.version || len(a.AllowedMountIDs) != 1 || a.AllowedMountIDs[0] != v.binding.MountID || fields.Mounts.AgentVersion != v.version || len(fields.Mounts.Mounts) != 1 || fields.Mounts.AttemptBinding != fields.AttemptBinding || env.AgentVersion != v.version || len(env.Variables) != 1 || len(env.Secrets) != 1 || env.Variables[0].Fields() != x.ordinary.Fields() || env.Secrets[0].Variable.Fields() != x.secret.Fields() {
		return ec.PreparationInput{}, errors.New("original nonempty configuration/environment was not captured")
	}
	m := fields.Mounts.Mounts[0]
	if m.ID != v.binding.MountID || m.WorkspaceID != v.binding.WorkspaceID || m.RunnerID != v.runner.ID || m.Workspace != v.binding.Workspace || m.RunnerVersion != v.runner.Version || m.RunnerName != v.runner.Name || m.RunnerDescription != v.runner.Description || m.Name != v.binding.MountID.String() || m.Version != 1 {
		return ec.PreparationInput{}, errors.New("captured stable mapping or safe Runner metadata changed")
	}
	var exact int64
	err = sql.QueryRow(ctx, `SELECT count(*) FROM agenteam_mount.execution_references WHERE execution_id=$1 AND mount_id::text=$2 AND workspace_id::text=$3 AND runner_id::text=$4 AND mount_version=1 AND runner_version=$5`, x.created.ID.String(), m.ID.String(), m.WorkspaceID.String(), m.RunnerID.String(), int64(m.RunnerVersion)).Scan(&exact)
	if err != nil || exact != 1 {
		return ec.PreparationInput{}, errors.New("capture does not have its exact stable reference")
	}
	counts, err := captureInputCounts(ctx, sql, x.created.ID.String())
	if err != nil || counts != ([11]int64{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}) {
		return ec.PreparationInput{}, errors.New("original input/providers were not atomic")
	}
	for _, forbidden := range [][]byte{[]byte("/execution-mount-capture/private-root"), []byte("owned-model-capture-material"), []byte("owned-environment-capture-material")} {
		if bytes.Contains(raw, forbidden) || bytes.Contains(material, forbidden) {
			return ec.PreparationInput{}, errors.New("private material or host path entered preparation")
		}
	}
	return input, nil
}
func jsonBytesForMount(v mt.ExecutionMountSet) []byte { raw, _ := json.Marshal(v); return raw }

func (v *executionMountFixture) providers(t *testing.T) {
	t.Helper()
	x := v.source
	if x.models.readErr != nil || x.skills.discover != 1 || x.skills.resolve != 1 || x.tools.discover != 1 || x.tools.resolve != 1 || x.models.discover != 1 || x.models.resolve != 1 || x.environment.discover != 1 || x.environment.resolve != 1 || v.mounts.discover != 1 || v.mounts.capture != 1 || v.mounts.verify == 0 || x.skills.tx != x.tools.tx || x.tools.tx != x.models.tx || x.models.tx != x.environment.tx || x.environment.tx != v.mounts.tx {
		t.Fatal("real providers did not capture once in the exact original transaction", x.models.readErr)
	}
}

func TestExecutionMountCapture(t *testing.T) {
	t.Run("nonempty-unknown-and-historical-material", func(t *testing.T) {
		v := newExecutionMountFixture(t)
		x, store := v.source, v.source.v.base.tracked
		var tentative ec.PreparationInput
		ready := false
		store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			d := cause.Details()
			if ready || d.Kind != f.JobCause || d.JobType != "execution-preparation" || d.JobID != x.created.ID.String() || v.mounts.capture != 1 {
				return nil
			}
			sql, err := store.InTx(tx)
			if err != nil {
				return err
			}
			x.attempt = d.JobAttemptID
			tentative, err = v.input(ctx, sql)
			ready = err == nil
			return err
		})
		original := store.fixtureStore
		loss := &captureCommitReceiptLoss{fixtureStore: original, attempt: id[f.TransactionAttempt](t), match: func(cause f.TransactionCause) bool {
			d := cause.Details()
			return ready && d.Kind == f.JobCause && d.JobType == "execution-preparation" && d.JobID == x.created.ID.String()
		}}
		store.fixtureStore = loss
		defer func() { store.setAfter(nil); store.fixtureStore = original }()
		err := v.driver.Run(ctxFor(t), x.created.ID)
		store.setAfter(nil)
		store.fixtureStore = original
		requireCode(t, err, f.CommitUnknown)
		fired, physical := loss.observed()
		if !ready || !fired || physical.State() != f.Committed {
			t.Fatal("controlled Unknown did not follow actual original COMMIT")
		}
		committed, err := v.input(ctxFor(t), x.v.base.raw)
		if err != nil || !bytes.Equal(committed.CanonicalBytes(), tentative.CanonicalBytes()) {
			t.Fatal("original capture did not commit input/material/ref atomically", err)
		}
		if err = v.driver.ResolveUnknown(ctxFor(t), x.created.ID); err != nil {
			t.Fatal("original Unknown did not observe historical material", err)
		}
		v.providers(t)
		x.requireAttemptReturned(t)
		clear := []ac.RunnerMountDeclaration{}
		update, err := ac.NewAgentUpdate(ac.AgentUpdateFields{RunnerMounts: &clear})
		if err != nil {
			t.Fatal(err)
		}
		changed, err := x.v.agent.agents.UpdateAgent(ctxFor(t), x.v.base.ownerBrowser.actor, meta(t, "execution-mount-clear-current", &v.version), x.created.ProjectID, x.created.AgentID, update)
		if err != nil || changed.Validate() != nil || len(changed.Fields().Agent.Fields().AllowedMountIDs) != 0 {
			t.Fatal("current configuration was not really cleared", err)
		}
		verified := v.mounts.verify
		if err = v.driver.Run(ctxFor(t), x.created.ID); err != nil || v.mounts.verify <= verified {
			t.Fatal("known replay skipped historical material", err)
		}
		after, err := v.input(ctxFor(t), x.v.base.raw)
		if err != nil || !bytes.Equal(after.CanonicalBytes(), committed.CanonicalBytes()) {
			t.Fatal("current removal changed or recaptured original material", err)
		}
		v.providers(t)
		x.requireAttemptReturned(t)
	})
	t.Run("final-rollback-keeps-no-partial-capture", func(t *testing.T) {
		v := newExecutionMountFixture(t)
		x, store := v.source, v.source.v.base.tracked
		marker := errors.New("owned-mount-capture-final-rollback")
		seen, returned := false, false
		var physical f.CommitResult
		store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			d := cause.Details()
			if seen || d.Kind != f.JobCause || d.JobType != "execution-preparation" || d.JobID != x.created.ID.String() || v.mounts.capture != 1 {
				return nil
			}
			sql, err := store.InTx(tx)
			if err != nil {
				return err
			}
			x.attempt = d.JobAttemptID
			if _, err = v.input(ctx, sql); err != nil {
				return err
			}
			seen = true
			return marker
		})
		store.mu.Lock()
		store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
			d := cause.Details()
			if seen && !returned && d.Kind == f.JobCause && d.JobType == "execution-preparation" && d.JobID == x.created.ID.String() {
				physical, returned = result, true
			}
		}
		store.mu.Unlock()
		defer func() { store.setAfter(nil); store.mu.Lock(); store.afterResult = nil; store.mu.Unlock() }()
		err := v.driver.Run(ctxFor(t), x.created.ID)
		store.setAfter(nil)
		store.mu.Lock()
		store.afterResult = nil
		store.mu.Unlock()
		if err == nil || !seen || !returned || physical.State() != f.NotCommitted {
			t.Fatal("final failure did not follow original real rollback", err)
		}
		counts, err := captureInputCounts(ctxFor(t), x.v.base.raw, x.created.ID.String())
		var mounts int64
		readErr := x.v.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_mount.execution_captures WHERE execution_id=$1)+(SELECT count(*) FROM agenteam_mount.execution_references WHERE execution_id=$1)`, x.created.ID.String()).Scan(&mounts)
		if err != nil || readErr != nil || counts != ([11]int64{}) || mounts != 0 {
			t.Fatal("partial input/provider/material/reference escaped rollback", err, readErr)
		}
		v.providers(t)
		x.requireAttemptReturned(t)
	})
}
