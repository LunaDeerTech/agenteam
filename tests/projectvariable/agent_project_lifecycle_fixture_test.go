//go:build integration

package projectvariable_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	toolruntime "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
	assembly "github.com/LunaDeerTech/agenteam/tests/testsupport/agentconfiguration"
)

type agentProjectLifecycleFixture struct {
	*agentCreateFixture
	directory *agent.DirectoryReader
	lifecycle *project.LifecycleAuthority
	stopper   *agent.ProjectCallStopper
	process   oc.ProcessID
}

// Reuse the existing real Skill/Object installation and Agent configuration
// owners. Only this composition additionally supplies the actual Lifecycle
// authority to the same Project authority used by the Agent owner graph.
func newAgentProjectLifecycleFixture(t *testing.T) *agentProjectLifecycleFixture {
	t.Helper()
	var installer *toolruntime.InstallAuthority
	var processGuard *object.ProcessGuard
	var process objc.ProcessID
	var installerRetired sync.Once
	retireInstaller := func() {
		installerRetired.Do(func() {
			installer.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := installer.Drain(ctx); err != nil || !installer.Joined() {
				t.Error("actual InstallAuthority did not retire", err)
			}
		})
	}
	p2 := newSkillInstallationFixtureWithAuthority(t, func(store *hookStore, projects *project.Authority, guard *object.ProcessGuard, p objc.ProcessID) (*skill.Authority, error) {
		process = p
		processGuard = guard
		var err error
		installer, err = toolruntime.NewInstallAuthority(store, guard, p)
		if err != nil {
			return nil, err
		}
		// Construction-failure fallback; the successful composition below
		// retires this borrower before Runtime releases its original guard.
		t.Cleanup(retireInstaller)
		return skill.NewAuthorityWithInstallExecution(store, projects, installer)
	})
	t.Cleanup(retireInstaller)
	source, err := toolruntime.NewInstallSource(installer, p2.service)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := project.NewLifecycleAuthority(p2.base.tracked, p2.manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := assembly.New(p2.base.tracked, assembly.Options{Accounts: p2.base.accounts, Lifecycle: lifecycle, BuiltinSources: map[string]registry.BuiltinSource{builtin.SkillInstallStableKey: source}, InstallSourceKey: builtin.SkillInstallStableKey})
	if err != nil {
		t.Fatal(err)
	}
	v := &agentCreateFixture{p2: p2, guard: processGuard, providers: graph.Providers()}
	// Registration traverses the actual bound guard and exactly the service
	// constructed with this InstallAuthority, in the original Registry EX Tx.
	p2.base.tx(t, []f.LockRequest{registry.RegistryLock(f.Exclusive)}, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		if err := p2.service.RequireInstallBindingInTx(ctx, tx, installer, process); err != nil {
			return err
		}
		registration, err := source.DescribeInTx(ctx, tx)
		if err != nil {
			return err
		}
		if err = source.CheckBindingInTx(ctx, tx, registration); err != nil {
			return err
		}
		v.tool, err = v.providers.Registry.ReconcileBuiltinInTx(ctx, tx, builtin.SkillInstallStableKey)
		return err
	})
	outProcess, err := f.ParseID[oc.Process](process.String())
	if err != nil {
		t.Fatal(err)
	}
	v.modelID = v.createModel(t, fixtureProcess{outProcess})
	v.agents, err = graph.NewService(p2.base.keys, fixtureProcess{outProcess})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		v.agents.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := v.agents.Drain(ctx); err != nil || !v.agents.Joined() {
			t.Error("original Agent calls did not join", err)
		}
	})
	directory, err := agent.NewDirectoryReader(p2.base.tracked, v.providers.Agents, p2.base.keys)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		directory.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := directory.Drain(ctx); err != nil || !directory.Joined() {
			t.Error("original Directory calls did not join", err)
		}
	})
	stopper, err := agent.NewProjectCallStopper(v.agents, directory)
	if err != nil {
		t.Fatal(err)
	}
	return &agentProjectLifecycleFixture{agentCreateFixture: v, directory: directory, lifecycle: lifecycle, stopper: stopper, process: outProcess}
}

// This hook holds the real original transaction after BEGIN, before its first
// lock acquisition. It never substitutes a Store, SQL result or stop report.
// Ignoring cancellation at this one barrier makes actual return observable.
type agentProjectCallKey struct{}
type agentProjectCall struct {
	entered chan struct{}
	done    chan struct{}
	release *phaseRelease
	once    sync.Once
	ctx     context.Context
	pid     int32
	err     error
}

func (v *agentProjectLifecycleFixture) installCallBarrier(t *testing.T) {
	t.Helper()
	s := v.p2.base.tracked
	s.mu.Lock()
	s.beforeLocks = func(ctx context.Context, tx f.Tx, _ []f.LockRequest) error {
		call, _ := ctx.Value(agentProjectCallKey{}).(*agentProjectCall)
		if call == nil {
			return nil
		}
		var hookErr error
		call.once.Do(func() {
			x, err := s.InTx(tx)
			if err != nil {
				hookErr = err
				return
			}
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&call.pid); err != nil {
				hookErr = err
				return
			}
			if call.pid <= 0 {
				hookErr = errors.New("original Agent BEGIN PID missing")
				return
			}
			call.ctx = ctx
			close(call.entered)
			<-call.release.done
		})
		return hookErr
	}
	s.mu.Unlock()
	t.Cleanup(func() { s.mu.Lock(); s.beforeLocks = nil; s.mu.Unlock() })
}

func beginAgentProjectCall(t *testing.T, fn func(context.Context) error) *agentProjectCall {
	t.Helper()
	call := &agentProjectCall{entered: make(chan struct{}), done: make(chan struct{}), release: newPhaseRelease()}
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), agentProjectCallKey{}, call), 20*time.Second)
	go func() { defer close(call.done); call.err = fn(ctx) }()
	t.Cleanup(func() { cancel(); call.release.release(); phaseAwait(t, call.done) })
	select {
	case <-call.entered:
	case <-call.done:
		t.Fatal("Agent call did not reach its original BEGIN barrier", call.err)
	case <-time.After(5 * time.Second):
		t.Fatal("Agent original BEGIN barrier missing")
	}
	return call
}

func (c *agentProjectCall) requireHeld(t *testing.T, canceled bool) {
	t.Helper()
	if c.pid <= 0 || c.ctx == nil || (c.ctx.Err() != nil) != canceled {
		t.Fatal("original Agent transaction cancellation differs from selection")
	}
	select {
	case <-c.done:
		t.Fatal("original Agent call retired before its real return")
	default:
	}
}
func (c *agentProjectCall) join(t *testing.T, success bool) {
	t.Helper()
	c.release.release()
	phaseAwait(t, c.done)
	if (c.err == nil) != success {
		t.Fatal("original Agent call result", c.err)
	}
}

func (v *agentProjectLifecycleFixture) foreignProject(t *testing.T) pc.ProjectRef {
	t.Helper()
	input := pc.CreateProjectRequest{ProjectID: id[i.Project](t), Name: "agent-stop-foreign", Description: "actual protected Skill initialization"}
	result, err := v.p2.creator.CreateProject(ctxFor(t), v.p2.base.ownerBrowser.actor, meta(t, id[struct{}](t).String(), nil), input)
	if err != nil || result.State != pc.CreationReady || result.Project == nil || result.Project.ID != input.ProjectID {
		t.Fatal("real foreign Project initialization", err)
	}
	return *result.Project
}

// Only the accepted upstream prerequisite is controlled. The actual driver,
// not this helper, must create stopping/claim facts. No participant is completed.
func (v *agentProjectLifecycleFixture) accept(t *testing.T, action pc.LifecycleAction) pc.LifecycleCause {
	t.Helper()
	p := v.p2.project
	cause := pc.LifecycleCause{OperationID: id[pc.Operation](t), Action: action, ProjectVersion: p.Version + 1}
	if cause.Validate() != nil {
		t.Fatal("invalid accepted fixture cause")
	}
	raw, err := json.Marshal(v.p2.manifest.Entries())
	if err != nil {
		t.Fatal(err)
	}
	digest, err := v.p2.manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	gate, cleanup := "archiving", "not_applicable"
	if action == pc.Delete {
		gate, cleanup = "deleting", "required"
	}
	txCause, err := f.NewRecoveryCause("agent.accepted-fixture", cause.OperationID.String(), "")
	if err != nil {
		t.Fatal(err)
	}
	store := v.p2.base.raw
	result := store.WithinTx(ctxFor(t), txCause, func(ctx context.Context, tx f.Tx) error {
		key, _ := f.ProjectLock(p.ID.String())
		if err := store.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); err != nil {
			return err
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_operations(id,project_id,owner_user_id,action,project_version,state,version,required_manifest,manifest_digest,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'accepted',1,$6::jsonb,$7,clock_timestamp(),clock_timestamp())`, cause.OperationID.String(), p.ID.String(), p.OwnerUserID.String(), string(action), int64(cause.ProjectVersion), raw, digest.String()); err != nil {
			return err
		}
		for _, entry := range v.p2.manifest.Entries() {
			if _, err = x.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_participants(operation_id,participant_name,contract_version,stop_state,cleanup_state,version) VALUES($1,$2,$3,'required',$4,1)`, cause.OperationID.String(), string(entry.Name), int64(entry.ContractVersion), cleanup); err != nil {
				return err
			}
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_project.projects SET lifecycle=$2,version=$3,current_lifecycle_operation_id=$4,updated_at=clock_timestamp() WHERE id=$1 AND version=$5 AND lifecycle='active' AND current_lifecycle_operation_id IS NULL`, p.ID.String(), gate, int64(cause.ProjectVersion), cause.OperationID.String(), int64(p.Version))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return errors.New("accepted Project gate mismatch")
		}
		return nil
	})
	if result.State() != f.Committed {
		t.Fatal("accepted prerequisite did not commit", result.Fault())
	}
	return cause
}

func (v *agentProjectLifecycleFixture) requirePhase(t *testing.T, cause pc.LifecycleCause, state, claim string) {
	t.Helper()
	raw := v.p2.base.raw
	cleanup, gate := "not_applicable", "archiving"
	if cause.Action == pc.Delete {
		cleanup, gate = "required", "deleting"
	}
	var actual, digest, actualGate, op string
	var version, projectVersion, currentVersion int64
	var participants, changed int
	err := raw.QueryRow(ctxFor(t), `SELECT o.state,o.version,o.project_version,o.manifest_digest,p.lifecycle,p.version,p.current_lifecycle_operation_id::text,(SELECT count(*) FROM agenteam_project.lifecycle_participants WHERE operation_id=$1),(SELECT count(*) FROM agenteam_project.lifecycle_participants WHERE operation_id=$1 AND (stop_state<>'required' OR cleanup_state<>$2 OR version<>1 OR safe_pending_refs<>'[]'::jsonb)) FROM agenteam_project.lifecycle_operations o JOIN agenteam_project.projects p ON p.id=o.project_id WHERE o.id=$1`, cause.OperationID.String(), cleanup).Scan(&actual, &version, &projectVersion, &digest, &actualGate, &currentVersion, &op, &participants, &changed)
	expectedVersion := int64(2)
	if state == "accepted" {
		expectedVersion = 1
	}
	expectedDigest, _ := v.p2.manifest.Digest()
	if err != nil || actual != state || version != expectedVersion || projectVersion != int64(cause.ProjectVersion) || currentVersion != projectVersion || actualGate != gate || op != cause.OperationID.String() || digest != expectedDigest.String() || participants != len(v.p2.manifest.Entries()) || changed != 0 {
		t.Fatal("Agent local report changed global phase/manifest/participant completion", err)
	}
	var count int
	if err = raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_project.work_claims WHERE work_kind='lifecycle' AND work_id=$1`, cause.OperationID.String()).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if claim == "" {
		if count != 0 {
			t.Fatal("accepted prerequisite fabricated a claim")
		}
		return
	}
	var actualClaim string
	var fence int64
	if count != 1 {
		t.Fatal("original lifecycle claim missing/duplicated")
	}
	if err = raw.QueryRow(ctxFor(t), `SELECT phase,fence FROM agenteam_project.work_claims WHERE work_kind='lifecycle' AND work_id=$1`, cause.OperationID.String()).Scan(&actualClaim, &fence); err != nil || actualClaim != claim || fence != 1 {
		t.Fatal("original lifecycle claim state/fence", err)
	}
}

func (v *agentProjectLifecycleFixture) requirePending(t *testing.T, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, pending int) {
	t.Helper()
	report, err := v.stopper.InspectStop(ctxFor(t), actor, cause, scope)
	if err != nil || !report.Matches(cause, scope) || report.Details().PendingCalls != pending || report.Details().LocalJoined != (pending == 0) {
		t.Fatal("actual local observation differs from original calls still held", err)
	}
}
