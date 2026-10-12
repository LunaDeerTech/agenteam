//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Only accepted Delete is a controlled prerequisite. The original StopDriver
// writes stopping and its running/terminal attempt, using the real bound guard.
// The published Agent, nonempty Model/Tool/Skill references and P2 are real.
// Neither this snapshot nor an attempt's terminal phase proves retirement,
// completed participants, a complete Registry or production BeginDelete.
func TestAgentRetirementPreparation(t *testing.T) {
	t.Run("committed-stop-round-config-snapshot", testRetirementPreparationCommitted)
	t.Run("same-transaction-and-callback-boundaries", testRetirementPreparationBoundaries)
	t.Run("original-loan-return-before-driver-drain", testRetirementPreparationReturn)
}

type retirementPreparationFixture struct {
	a        *agentCreateFixture
	b        *variableHTTPFixture
	config   ac.AgentConfig
	manifest pc.RequiredManifest
	cause    pc.LifecycleCause
	driver   *project.LifecycleStopDriver
	owner    *project.AgentRetirementPreparation
	reader   *agent.RetirementPreparation
	before   string
}

func newRetirementPreparationFixture(t *testing.T, update bool, step func(*retirementPreparationFixture, context.Context, i.Actor, pc.LifecycleCause, pc.ScopeRef) error) *retirementPreparationFixture {
	t.Helper()
	a := newAgentCreateFixture(t)
	v := &retirementPreparationFixture{a: a, b: a.p2.base, manifest: lifecycleManifest(t)}
	request, meta, command := a.request(t)
	receipt, err := a.agents.CreateAgent(ctxFor(t), v.b.ownerBrowser.actor, meta, a.p2.project.ID, request)
	retirementPreparationRequire(t, err)
	v.config = receipt.Fields().Agent
	if receipt.Validate() != nil || len(v.config.Fields().AllowedToolIDs) != 1 {
		t.Fatal("preparation requires a real nonempty published Agent")
	}
	a.checkFacts(t, request.Fields().AgentID, meta.IdempotencyKey, command, [16]int64{1, 1, 0, 0, 2, 1, 1, 1, 0, 0, 1, 1, 1, 1, 1, 0}, true)
	if update {
		description := "Published second configuration for stale preparation validation"
		change, err := ac.NewAgentUpdate(ac.AgentUpdateFields{Description: &description})
		retirementPreparationRequire(t, err)
		version := v.config.Fields().Core.Version
		updated, err := a.agents.UpdateAgent(ctxFor(t), v.b.ownerBrowser.actor, f.CommandMeta{RequestID: id[f.Request](t), IdempotencyKey: f.IdempotencyKey(id[struct{}](t).String()), ExpectedVersion: &version}, a.p2.project.ID, request.Fields().AgentID, change)
		retirementPreparationRequire(t, err)
		if updated.Validate() != nil || !updated.Fields().Changed || updated.Fields().Agent.Fields().Core.Version != 2 {
			t.Fatal("preparation stale-version setup did not publish its real update")
		}
		v.config = updated.Fields().Agent
	}
	v.before = v.snapshot(t)
	v.acceptedDelete(t)
	authority, err := project.NewLifecycleAuthority(v.b.tracked, v.manifest, nil)
	retirementPreparationRequire(t, err)
	v.driver, err = project.NewLifecycleStopDriver(v.b.tracked, authority, captureProviderProcesses{guard: a.guard}, func(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
		return step(v, ctx, actor, cause, scope)
	})
	retirementPreparationRequire(t, err)
	v.owner, err = project.NewAgentRetirementPreparation(v.driver)
	retirementPreparationRequire(t, err)
	v.reader, err = agent.NewRetirementPreparation(a.providers.Agents, v.owner)
	retirementPreparationRequire(t, err)
	t.Cleanup(func() {
		v.driver.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := v.driver.Drain(ctx)
		cancel()
		if err != nil {
			t.Error("preparation cleanup retains original driver/guard until actual return")
			// Failure retention uses the original Go/global supervisor deadline;
			// it is not a successful bounded local join. Inherited graph cleanup
			// must not release the guard or delete this database first.
			_ = v.driver.Drain(context.Background())
		}
	})
	return v
}

func retirementPreparationRequire(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal("retirement preparation prerequisite failed", err)
	}
}

func (v *retirementPreparationFixture) request(actor i.Actor, cause pc.LifecycleCause) pc.AgentRetirementPreparationRequest {
	core := v.config.Fields().Core
	return pc.AgentRetirementPreparationRequest{Actor: actor, ProjectID: core.ProjectID, Cause: cause, AgentID: core.ID, ExpectedConfigVersion: core.Version}
}

func (v *retirementPreparationFixture) locks() []f.LockRequest {
	core := v.config.Fields().Core
	p, _ := f.ProjectLock(core.ProjectID.String())
	s, _ := f.ProjectScheduleLock(core.ProjectID.String())
	a, _ := f.AgentLock(core.ID.String())
	return []f.LockRequest{{Key: p, Mode: f.Shared}, {Key: s, Mode: f.Exclusive}, {Key: a, Mode: f.Exclusive}}
}

func (v *retirementPreparationFixture) txCause() f.TransactionCause {
	c, _ := f.NewRecoveryCause("agent.retirement-preparation.fixture", v.cause.OperationID.String(), "")
	return c
}

func (v *retirementPreparationFixture) within(ctx context.Context, fn func(context.Context, f.Tx) error) f.CommitResult {
	return v.b.tracked.WithinTx(ctx, v.txCause(), func(ctx context.Context, tx f.Tx) error {
		if err := v.b.tracked.AcquireAll(ctx, tx, v.locks()); err != nil {
			return err
		}
		return fn(ctx, tx)
	})
}

func retirementPreparationCommitted(result f.CommitResult) error {
	if result.State() != f.Committed {
		return errors.New("preparation original transaction did not confirm commit")
	}
	return nil
}

func (v *retirementPreparationFixture) acceptedDelete(t *testing.T) {
	t.Helper()
	p := v.a.p2.project
	v.cause = pc.LifecycleCause{OperationID: id[pc.Operation](t), Action: pc.Delete, ProjectVersion: p.Version + 1}
	raw, err := json.Marshal(v.manifest.Entries())
	retirementPreparationRequire(t, err)
	digest, err := v.manifest.Digest()
	retirementPreparationRequire(t, err)
	result := v.b.tracked.WithinTx(ctxFor(t), v.txCause(), func(ctx context.Context, tx f.Tx) error {
		key, _ := f.ProjectLock(p.ID.String())
		if err := v.b.tracked.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); err != nil {
			return err
		}
		x, err := v.b.tracked.InTx(tx)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_operations(id,project_id,owner_user_id,action,project_version,state,version,required_manifest,manifest_digest,created_at,updated_at) VALUES($1,$2,$3,'delete',$4,'accepted',1,$5::jsonb,$6,clock_timestamp(),clock_timestamp())`, v.cause.OperationID.String(), p.ID.String(), p.OwnerUserID.String(), int64(v.cause.ProjectVersion), raw, digest.String())
		if err != nil {
			return err
		}
		for _, entry := range v.manifest.Entries() {
			if _, err = x.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_participants(operation_id,participant_name,contract_version,stop_state,cleanup_state,version) VALUES($1,$2,$3,'required','required',1)`, v.cause.OperationID.String(), string(entry.Name), int64(entry.ContractVersion)); err != nil {
				return err
			}
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_project.projects SET lifecycle='deleting',version=$2,current_lifecycle_operation_id=$3,updated_at=clock_timestamp() WHERE id=$1 AND version=$4 AND lifecycle='active' AND current_lifecycle_operation_id IS NULL`, p.ID.String(), int64(v.cause.ProjectVersion), v.cause.OperationID.String(), int64(p.Version))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return errors.New("preparation accepted prerequisite lost original Project")
		}
		return nil
	})
	retirementPreparationRequire(t, retirementPreparationCommitted(result))
}

// Exact row content stays private to this test; diagnostics expose no Agent
// instructions or reference payloads. Only lifecycle phase/claim are excluded.
func (v *retirementPreparationFixture) snapshot(t *testing.T) string {
	t.Helper()
	var out string
	core := v.config.Fields().Core
	retirementPreparationRequire(t, v.b.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'agent',(SELECT to_jsonb(a) FROM agenteam_agent.agents a WHERE id::text=$2),
 'commands',(SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY id),'[]') FROM agenteam_agent.commands r WHERE target_id::text=$2),
 'tools',(SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY tool_id),'[]') FROM agenteam_agent.tool_allowlist r WHERE agent_id::text=$2),
 'mounts',(SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY mount_id),'[]') FROM agenteam_agent.mount_allowlist r WHERE agent_id::text=$2),
 'secrets',(SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY variable_id),'[]') FROM agenteam_agent.secret_allowlist r WHERE agent_id::text=$2),
 'model_refs',(SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY role),'[]') FROM agenteam_model.references r WHERE owner_kind='agent' AND owner_id::text=$2),
 'tool_head',(SELECT to_jsonb(r) FROM agenteam_tool.agent_configurations r WHERE agent_id::text=$2),
 'tool_refs',(SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY tool_id),'[]') FROM agenteam_tool.agent_references r WHERE agent_id::text=$2),
 'mount_head',(SELECT to_jsonb(r) FROM agenteam_mount.agent_mount_heads r WHERE agent_id::text=$2),
 'mount_refs',(SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY mount_id),'[]') FROM agenteam_mount.agent_mount_refs r WHERE agent_id::text=$2),
 'secret_refs',(SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY variable_id),'[]') FROM agenteam_projectvariable.secret_references r WHERE owner_kind='agent' AND owner_id::text=$2),
 'skill_head',(SELECT to_jsonb(r) FROM agenteam_skill.agent_assignment_heads r WHERE agent_id::text=$2),
 'assignments',(SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY id),'[]') FROM agenteam_skill.agent_assignments r WHERE agent_id::text=$2),
 'audit',(SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY id),'[]') FROM agenteam_audit.audit_records r WHERE project_id::text=$1),
 'outbox',(SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY id),'[]') FROM agenteam_outbox.events r WHERE project_id::text=$1),
 'activity',(SELECT last_activity_at FROM agenteam_account.sessions WHERE id::text=$3)
 )::text`, core.ProjectID.String(), core.ID.String(), v.b.ownerBrowser.actor.Details().SessionID).Scan(&out))
	return out
}

type retirementPreparationClaim struct {
	project, process, attempt string
	fence                     int64
}

func (v *retirementPreparationFixture) state(t *testing.T, phase string) retirementPreparationClaim {
	t.Helper()
	var claim retirementPreparationClaim
	var operationState, claimPhase, digest string
	var version, projectVersion int64
	var participants, changed int
	retirementPreparationRequire(t, v.b.raw.QueryRow(ctxFor(t), `SELECT o.state,o.version,o.project_version,o.manifest_digest,c.project_id::text,c.process_id::text,c.attempt_id::text,c.fence,c.phase,
 (SELECT count(*) FROM agenteam_project.lifecycle_participants WHERE operation_id=o.id),
 (SELECT count(*) FROM agenteam_project.lifecycle_participants WHERE operation_id=o.id AND (stop_state<>'required' OR cleanup_state<>'required' OR version<>1 OR checkpoint IS NOT NULL OR safe_pending_refs<>'[]'::jsonb))
 FROM agenteam_project.lifecycle_operations o JOIN agenteam_project.work_claims c ON c.work_kind='lifecycle' AND c.work_id=o.id WHERE o.id=$1`, v.cause.OperationID.String()).Scan(&operationState, &version, &projectVersion, &digest, &claim.project, &claim.process, &claim.attempt, &claim.fence, &claimPhase, &participants, &changed))
	wantDigest, err := v.manifest.Digest()
	retirementPreparationRequire(t, err)
	process, err := v.a.guard.CurrentProcess()
	retirementPreparationRequire(t, err)
	if operationState != "stopping" || version != 2 || projectVersion != int64(v.cause.ProjectVersion) || digest != wantDigest.String() || claim.project != v.config.Fields().Core.ProjectID.String() || claim.process != process.String() || claim.fence != 1 || claimPhase != phase || participants != len(v.manifest.Entries()) || changed != 0 {
		t.Fatal("preparation changed operation/participants or lost original claim identity")
	}
	if _, err = f.ParseID[struct{}](claim.attempt); err != nil {
		t.Fatal("preparation claim has no real attempt")
	}
	return claim
}

func (v *retirementPreparationFixture) unchanged(t *testing.T) {
	t.Helper()
	if v.snapshot(t) != v.before {
		t.Fatal("preparation changed canonical configuration, references, history or effects")
	}
}

type retirementPreparationRun struct {
	done chan struct{}
	err  error
}

func (v *retirementPreparationFixture) run(t *testing.T, releases ...func()) *retirementPreparationRun {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	run := &retirementPreparationRun{done: make(chan struct{})}
	go func() {
		defer close(run.done)
		run.err = v.driver.Run(ctx, v.config.Fields().Core.ProjectID, v.cause.OperationID)
	}()
	t.Cleanup(func() {
		cancel()
		for _, release := range releases {
			release()
		}
		retirementPreparationJoin(t, run.done, "original driver")
	})
	return run
}

func retirementPreparationJoin(t *testing.T, done <-chan struct{}, phase string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("preparation retains unreturned owner at", phase)
		<-done // Do not let inherited fixture cleanup release an unjoined guard.
	}
}

func retirementPreparationReady(t *testing.T, ready, returned <-chan struct{}, phase string) {
	t.Helper()
	select {
	case <-ready:
	case <-returned:
		t.Fatal("preparation original call returned before", phase)
	case <-time.After(3 * time.Second):
		t.Fatal("preparation original readiness missing at", phase)
	}
}

func retirementPreparationRelease() (chan struct{}, func()) {
	ready := make(chan struct{})
	var once sync.Once
	return ready, func() { once.Do(func() { close(ready) }) }
}

func retirementPreparationEqual(a, b ac.AgentConfig) bool {
	x, ex := json.Marshal(a)
	y, ey := json.Marshal(b)
	return ex == nil && ey == nil && bytes.Equal(x, y)
}

func testRetirementPreparationCommitted(t *testing.T) {
	var entered atomic.Bool
	v := newRetirementPreparationFixture(t, false, func(v *retirementPreparationFixture, ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
		entered.Store(true)
		if scope.ProjectID != v.config.Fields().Core.ProjectID || cause != v.cause {
			return errors.New("preparation original step identity mismatch")
		}
		return retirementPreparationCommitted(v.within(ctx, func(ctx context.Context, tx f.Tx) error {
			value, err := v.reader.ReadRetirementPreparationInTx(ctx, tx, v.request(actor, cause))
			if err != nil {
				return err
			}
			if !retirementPreparationEqual(value, v.config) {
				return errors.New("preparation did not read the full published configuration")
			}
			fields := value.Fields()
			fields.Core.Name = "caller-local-copy"
			fields.AllowedToolIDs[0] = i.ToolID{}
			again, err := v.reader.ReadRetirementPreparationInTx(ctx, tx, v.request(actor, cause))
			if err != nil || !retirementPreparationEqual(again, v.config) || !retirementPreparationEqual(value, v.config) {
				return errors.New("preparation result leaked mutable configuration state")
			}
			return nil
		}))
	})
	release, allowReturn := retirementPreparationRelease()
	held := make(chan struct{})
	var first atomic.Bool
	var physical f.CommitResult
	store := v.b.tracked
	store.mu.Lock()
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		d := cause.Details()
		if d.Kind == f.JobCause && d.JobType == "project-lifecycle" && d.JobID == v.cause.OperationID.String() && first.CompareAndSwap(false, true) {
			physical = result
			close(held)
			<-release // Actual COMMIT is visible; original Store return is not.
		}
	}
	store.mu.Unlock()
	t.Cleanup(func() { store.mu.Lock(); store.afterResult = nil; store.mu.Unlock() })
	run := v.run(t, allowReturn)
	retirementPreparationReady(t, held, run.done, "committed start original return")
	retirementPreparationRequire(t, retirementPreparationCommitted(physical))
	claim := v.state(t, "running")
	if entered.Load() {
		t.Fatal("preparation step entered before the original committed Store returned")
	}
	allowReturn()
	retirementPreparationReady(t, run.done, nil, "original successful stop round")
	retirementPreparationRequire(t, run.err)
	if !entered.Load() || v.state(t, "terminal") != claim {
		t.Fatal("preparation did not retire its exact original attempt")
	}
	v.unchanged(t)
	t.Log("AGENT_RETIREMENT_PREPARATION_COMMITTED_SNAPSHOT refs_unchanged=true participants_required=true")
}

func testRetirementPreparationBoundaries(t *testing.T) {
	var foreign *postgres.Store
	var captured context.Context
	var request pc.AgentRetirementPreparationRequest
	v := newRetirementPreparationFixture(t, true, func(v *retirementPreparationFixture, ctx context.Context, actor i.Actor, cause pc.LifecycleCause, _ pc.ScopeRef) error {
		request = v.request(actor, cause)
		// Start outside any caller Tx. The private step scope remains genuine;
		// rejection must come from the real Store's live-token ownership gate.
		var foreignChecked bool
		foreignResult := foreign.WithinTx(ctx, v.txCause(), func(other context.Context, foreignTx f.Tx) error {
			x, err := foreign.InTx(foreignTx)
			if err != nil {
				return err
			}
			var one int
			if err = x.QueryRow(other, `SELECT 1`).Scan(&one); err != nil {
				return err
			}
			value, err := v.reader.ReadRetirementPreparationInTx(ctx, foreignTx, request)
			foreignChecked = one == 1 && err != nil && value.Validate() != nil
			return nil
		})
		// postgres deliberately poisons a live token borrowed through a foreign
		// Store. Its original transaction actually rolls back, never commits.
		if !foreignChecked || foreignResult.State() != f.NotCommitted {
			return errors.New("preparation did not reject and roll back the foreign Store token")
		}
		return retirementPreparationCommitted(v.within(ctx, func(ctx context.Context, tx f.Tx) error {
			bad := request
			bad.ExpectedConfigVersion-- // Original v1 is stale after actual Update v2.
			value, err := v.reader.ReadRetirementPreparationInTx(ctx, tx, bad)
			if !retirementPreparationCode(err, f.VersionConflict) || value.Validate() == nil {
				return errors.New("preparation stale version did not return zero configuration")
			}
			err = v.owner.WithAgentRetirementPreparationInTx(ctx, tx, request, func(loan context.Context) error {
				captured = loan
				value, err := v.reader.ReadRetirementPreparationInTx(loan, tx, request)
				if err != nil || !retirementPreparationEqual(value, v.config) {
					return errors.New("preparation active original nested loan failed")
				}
				return nil
			})
			if err != nil {
				return err
			}
			// Original step and Tx are still live. A saved, finished child loan
			// must not inherit the parent scope and mint a replacement loan.
			value, err = v.reader.ReadRetirementPreparationInTx(context.WithoutCancel(captured), tx, request)
			if !retirementPreparationCode(err, f.Forbidden) || value.Validate() == nil {
				return errors.New("preparation revived a callback after its actual return")
			}
			value, err = v.reader.ReadRetirementPreparationInTx(ctx, tx, request)
			if err != nil || !retirementPreparationEqual(value, v.config) {
				return errors.New("preparation rejected the still-live original step")
			}
			return nil
		}))
	})
	foreign = openStore(t, v.b.db.Config(t, nil))
	registration, err := i.RegisterService(i.ProjectLifecycle)
	retirementPreparationRequire(t, err)
	scope, err := i.InProject(v.config.Fields().Core.ProjectID)
	retirementPreparationRequire(t, err)
	actor, err := registration.Actor(v.cause.OperationID.String(), scope)
	retirementPreparationRequire(t, err)
	retirementPreparationRequire(t, retirementPreparationCommitted(v.within(ctxFor(t), func(ctx context.Context, tx f.Tx) error {
		value, err := v.reader.ReadRetirementPreparationInTx(ctx, tx, v.request(actor, v.cause))
		if err == nil || value.Validate() == nil {
			return errors.New("preparation accepted caller-built issuer DTOs")
		}
		return nil
	})))
	run := v.run(t)
	retirementPreparationReady(t, run.done, nil, "original boundary round")
	retirementPreparationRequire(t, run.err)
	v.state(t, "terminal")
	retirementPreparationRequire(t, retirementPreparationCommitted(v.within(ctxFor(t), func(_ context.Context, tx f.Tx) error {
		value, err := v.reader.ReadRetirementPreparationInTx(context.WithoutCancel(captured), tx, request)
		if err == nil || value.Validate() == nil {
			return errors.New("preparation reused an ended step in a later transaction")
		}
		return nil
	})))
	v.unchanged(t)
	t.Log("AGENT_RETIREMENT_PREPARATION_SCOPE_BOUNDARIES original_step_only=true refs_unchanged=true")
}

func retirementPreparationCode(err error, code f.Code) bool {
	var fault *f.Fault
	return errors.As(err, &fault) && fault.Code == code
}

func testRetirementPreparationReturn(t *testing.T) {
	callbackHeld := make(chan struct{})
	callbackRelease, releaseCallback := retirementPreparationRelease()
	stepRelease, releaseStep := retirementPreparationRelease()
	stepReturned := make(chan struct{})
	loanReturned := make(chan struct{})
	var loanStarted atomic.Bool
	var stepReleased atomic.Bool
	var loanResult f.CommitResult
	v := newRetirementPreparationFixture(t, false, func(v *retirementPreparationFixture, ctx context.Context, actor i.Actor, cause pc.LifecycleCause, _ pc.ScopeRef) error {
		defer close(stepReturned)
		request := v.request(actor, cause)
		loanStarted.Store(true)
		go func() {
			defer close(loanReturned)
			loanResult = v.within(ctx, func(ctx context.Context, tx f.Tx) error {
				return v.owner.WithAgentRetirementPreparationInTx(ctx, tx, request, func(loan context.Context) error {
					value, err := v.reader.ReadRetirementPreparationInTx(loan, tx, request)
					if err != nil || !retirementPreparationEqual(value, v.config) {
						return errors.New("preparation actual read failed before callback barrier")
					}
					close(callbackHeld)
					<-callbackRelease // Holds this actual callback, not kernel SQL.
					return nil
				})
			})
		}()
		select {
		case <-stepRelease:
			stepReleased.Store(ctx.Err() == nil)
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	// Registered before run cleanup: release, await the original Run, then this
	// owned caller's Store return; only then may the inherited graph retire.
	t.Cleanup(func() {
		releaseCallback()
		releaseStep()
		if loanStarted.Load() {
			retirementPreparationJoin(t, loanReturned, "original preparation transaction")
		}
	})
	run := v.run(t, releaseCallback, releaseStep)
	retirementPreparationReady(t, callbackHeld, loanReturned, "original synchronous loan callback")
	claim := v.state(t, "running")
	releaseStep()
	retirementPreparationReady(t, stepReturned, run.done, "original step return")
	if !stepReleased.Load() {
		t.Fatal("preparation step exhausted its original budget before the requested return")
	}
	v.driver.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	err := v.driver.Drain(ctx)
	cancel()
	if err == nil {
		t.Fatal("preparation Driver Drain crossed the held original loan callback")
	}
	select {
	case <-run.done:
		t.Fatal("preparation Run returned while its loan callback remained held")
	default:
	}
	select {
	case <-loanReturned:
		t.Fatal("preparation original loan returned through its callback barrier")
	default:
	}
	if v.state(t, "running") != claim {
		t.Fatal("preparation held attempt changed identity")
	}
	releaseCallback()
	retirementPreparationReady(t, loanReturned, nil, "original loan and transaction return")
	if loanResult.State() != f.NotCommitted {
		t.Fatal("canceled preparation caller transaction did not actually roll back")
	}
	retirementPreparationReady(t, run.done, nil, "original driver checkpoint return")
	if run.err != nil && !errors.Is(run.err, context.Canceled) {
		t.Fatal("preparation original driver returned an unexpected failure", run.err)
	}
	retirementPreparationRequire(t, v.driver.Drain(ctxFor(t)))
	if v.state(t, "terminal") != claim {
		t.Fatal("preparation settled a different claim attempt")
	}
	if _, err := v.a.guard.CurrentProcess(); err != nil {
		t.Fatal("preparation borrower released the original graph guard")
	}
	v.unchanged(t)
	t.Log("AGENT_RETIREMENT_PREPARATION_ORIGINAL_CALLBACK_RETURN attempt_terminal_only=true refs_unchanged=true")
}
