//go:build integration

package projectvariable_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/artifact"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// The original creator accepts Delete and the existing real owners perform
// Stop/cleanup. The fixture-only Registry is unavailable for execution: its
// ports are never called. Agent/Variables, Skill, Artifact/Object and Outbox
// stop through their real owners; Secret/Audit stop facts and the progress
// handoff remain controlled prerequisites. This limited manifest is not the
// enabled App's complete lifecycle graph.
func TestProjectFinalize(t *testing.T) {
	t.Run("original-final-return-and-atomic-receipt", testProjectFinalizeReturn)
	t.Run("committed-unknown-receipt-first-confirmation", testProjectFinalizeCommittedUnknown)
	t.Run("registration-rollback-unknown-explicit-resume", testProjectFinalizeRegistrationUnknown)
}

type projectFinalizePGAssembly struct {
	processes oc.ProcessAuthority
	meta      f.CommandMeta
	request   pc.DeleteProjectRequest
	cause     pc.LifecycleCause
	artifacts *artifact.ProjectCallOwnership
	combined  pc.ProjectLifecycleParticipant
}

func (a *projectFinalizePGAssembly) creator(t *testing.T, manifest pc.RequiredManifest, guard *object.ProcessGuard, deps project.Dependencies) project.Dependencies {
	t.Helper()
	var bindings []project.LifecycleParticipantBinding
	for _, entry := range manifest.Entries() {
		port := &unavailableParticipant{name: entry.Name}
		bindings = append(bindings, project.LifecycleParticipantBinding{Registration: entry, Participant: port})
		t.Cleanup(func() {
			if port.calls.Load() != 0 {
				t.Error("finalizer invoked an unavailable acceptance-only participant")
			}
		})
	}
	registry, err := project.NewLifecycleRegistry(manifest, bindings)
	outboxCleanupRequire(t, err)
	a.processes = captureProviderProcesses{guard: guard}
	deps.Processes, deps.LifecycleRegistry = a.processes, registry
	return deps
}

func (a *projectFinalizePGAssembly) enterCleanup(t *testing.T, v *skillInstallationFixture, stop func(i.Actor, pc.LifecycleCause, pc.ScopeRef)) (i.Actor, pc.LifecycleCause, pc.ScopeRef) {
	t.Helper()
	var path string
	v.base.tx(t, ownerLocks(v.base.ownerBrowser.actor, v.project.ID), func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		route, err := v.base.accounts.CurrentUserRouteInTx(ctx, tx, v.base.ownerBrowser.actor)
		if err != nil {
			return err
		}
		path, err = pc.NormalizeProjectPath(route.Username, v.project.Name)
		return err
	})
	a.meta = meta(t, "finalizer-original-delete", &v.project.Version)
	a.request = pc.DeleteProjectRequest{NormalizedCurrentPath: path, Permanent: true}
	accepted, err := v.creator.BeginDeleteProject(ctxFor(t), v.base.ownerBrowser.actor, a.meta, v.project.ID, a.request)
	finalizeRequire(t, "formal-delete-acceptance", err)
	if accepted.Validate() != nil || accepted.Operation == nil || accepted.Receipt != nil || accepted.Operation.Action != pc.Delete || accepted.Operation.State != pc.OperationAccepted {
		t.Fatal("original creator did not return formal Delete acceptance")
	}
	a.cause = pc.LifecycleCause{OperationID: accepted.Operation.ID, Action: pc.Delete, ProjectVersion: accepted.Operation.ProjectVersion}
	scope := pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: v.project.ID}
	var actor i.Actor
	prior, skillStopped, artifactStopped := false, false, false
	driver, err := project.NewLifecycleStopDriver(v.base.tracked, v.lifecycle, a.processes, func(ctx context.Context, current i.Actor, cause pc.LifecycleCause, target pc.ScopeRef) error {
		if cause != a.cause || target != scope || a.combined == nil {
			return errors.New("original stop scope or owned Artifact binding changed")
		}
		actor = current
		if !prior {
			if stop != nil {
				stop(current, cause, target)
			}
			prior = true
		}
		report, err := v.service.RequestStop(ctx, current, cause, target)
		if err != nil {
			return err
		}
		if !report.Matches(pc.SkillsParticipant, cause, target) {
			return errors.New("original Skill stop identity changed")
		}
		skillStopped = report.Details().State == pc.Stopped
		if !skillStopped && report.Details().State != pc.StopPending {
			return errors.New("Skill lacked an original Pending report")
		}
		report, err = a.combined.RequestStop(ctx, current, cause, target)
		if err != nil {
			return err
		}
		if !report.Matches(pc.ArtifactObjectParticipant, cause, target) {
			return errors.New("original Artifact/Object stop identity changed")
		}
		artifactStopped = report.Details().State == pc.Stopped
		if !artifactStopped && report.Details().State != pc.StopPending {
			return errors.New("Artifact/Object lacked an original Pending report")
		}
		return nil
	})
	outboxCleanupRequire(t, err)
	t.Cleanup(func() {
		driver.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := driver.Drain(ctx); err != nil {
			t.Error("original predecessor Stop driver did not return", err)
		}
	})
	for round := 0; round < 16 && (!skillStopped || !artifactStopped); round++ {
		finalizeRequire(t, "original-predecessor-stop", driver.Run(ctxFor(t), scope.ProjectID, a.cause.OperationID))
	}
	if !prior || !skillStopped || !artifactStopped {
		t.Fatal("finalizer prefix lacked actual original Stop returns")
	}
	driver.Stop()
	outboxCleanupRequire(t, driver.Drain(ctxFor(t)))
	// Preserve the inherited explicit progress handoff. It never invokes the
	// unavailable Registry or supplies a false private owner-return witness.
	lock, _ := f.ProjectLock(scope.ProjectID.String())
	v.base.tx(t, []f.LockRequest{{Key: lock, Mode: f.Exclusive}}, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		if _, err := x.Exec(ctx, `UPDATE agenteam_project.lifecycle_participants SET stop_state='stopped',version=version+1 WHERE operation_id=$1`, a.cause.OperationID.String()); err != nil {
			return err
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_project.lifecycle_operations SET state='cleaning',cleanup_stage='domains',version=version+1,updated_at=clock_timestamp() WHERE id=$1 AND project_id=$2 AND state='stopping'`, a.cause.OperationID.String(), scope.ProjectID.String())
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return errors.New("formal Delete lost the original stopping handoff")
		}
		return nil
	})
	return actor, a.cause, scope
}

type projectFinalizePG struct {
	*artifactCleanupPG
	setup                   *projectFinalizePGAssembly
	final                   *project.LifecycleCleanupDriver
	closed                  atomic.Bool
	barriers                []*schedulerStopBarrier
	foreign, system, before string
	original                pc.DeletionReceiptData
}

func newProjectFinalizePG(t *testing.T) *projectFinalizePG {
	t.Helper()
	v := &projectFinalizePG{setup: &projectFinalizePGAssembly{}}
	base := newVariableHTTPFixture(t)
	base.finalizerAssembly = v.setup
	v.artifactCleanupPG = newArtifactCleanupPGConfigured(t, base, func(prefix *artifactCleanupPG, assembly *artifactCleanupPGAssembly) {
		v.artifactCleanupPG = prefix
		// Legacy publication has actually returned. As in the verified Stop
		// fixture, the same Store/Object/Audit/guard now own this explicit pair.
		owned := &artifactStopPG{base: prefix, assembly: assembly}
		v.setup.artifacts = owned.newPair(t)
		prefix.service = v.setup.artifacts.Service()
		t.Cleanup(func() {
			v.setup.artifacts.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := v.setup.artifacts.Drain(ctx); err != nil || !v.setup.artifacts.Joined() {
				t.Error("original Artifact pair did not join", err)
			}
		})
		owned.readA(t)
		var err error
		v.setup.combined, err = project.NewArtifactObjectLifecycleParticipant(prefix.p2.lifecycle, prefix.service, prefix.p2.objects)
		outboxCleanupRequire(t, err)
	})
	driver := v.artifactCleanupPG.driver(t)
	v.ready(t, driver)
	v.finishArtifact(t, driver)
	auditing, err := audit.New(base.tracked, base.keys, audit.Authorizations{Sessions: base.accounts, System: base.accounts, Accounts: base.accounts, Projects: v.projects})
	outboxCleanupRequire(t, err)
	participant, err := project.NewAuditLifecycleParticipant(v.p2.lifecycle, auditing)
	outboxCleanupRequire(t, err)
	auditTap := &secretCleanupPGTap{ProjectLifecycleParticipant: participant}
	tail, err := project.NewLifecycleCleanupDriverWithAudit(base.tracked, v.p2.lifecycle, v.setup.processes, v.outbox, auditTap)
	outboxCleanupRequire(t, err)
	v.drivers = append(v.drivers, tail)
	for round := 0; round < 6; round++ {
		var priorStage string
		outboxCleanupRequire(t, base.raw.QueryRow(ctxFor(t), `SELECT cleanup_stage FROM agenteam_project.lifecycle_operations WHERE id=$1`, v.cause.OperationID.String()).Scan(&priorStage))
		selected, name := v.outbox, pc.OutboxParticipant
		if priorStage == "audit" {
			selected, name = auditTap, pc.AuditParticipant
		} else if priorStage != "outbox" {
			t.Fatal("predecessor continuation lacked its canonical stage", priorStage)
		}
		call := v.run(t, tail)
		outboxCleanupWait(t, call.done, "original Outbox/Audit predecessor")
		finalizeRequire(t, "outbox-audit-prefix", call.err)
		selected.mu.Lock()
		report := selected.report
		selected.mu.Unlock()
		if !report.Matches(name, v.cause, pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: v.p2.project.ID}) || report.Details().State != pc.CleanupPending && report.Details().State != pc.CleanupCompleted {
			t.Fatal("predecessor lacked its original Pending or Completed report")
		}
		var stage string
		var incomplete, audits, events int
		outboxCleanupRequire(t, base.raw.QueryRow(ctxFor(t), `SELECT o.cleanup_stage,(SELECT count(*) FROM agenteam_project.lifecycle_participants WHERE operation_id=o.id AND (stop_state<>'stopped' OR cleanup_state<>'completed')),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=o.project_id),(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=o.project_id) FROM agenteam_project.lifecycle_operations o WHERE o.id=$1`, v.cause.OperationID.String()).Scan(&stage, &incomplete, &audits, &events))
		if stage == "final" {
			if name != pc.AuditParticipant || report.Details().State != pc.CleanupCompleted || incomplete != 0 || audits != 0 || events != 0 || auditTap.calls.Load() == 0 || v.outbox.calls.Load() == 0 {
				t.Fatal("final stage lacked actual Outbox/Audit completion")
			}
			break
		}
		if report.Details().State == pc.CleanupPending && stage != priorStage || report.Details().State == pc.CleanupCompleted && (priorStage != "outbox" || stage != "audit") || round == 5 {
			t.Fatal("bounded Outbox/Audit rounds did not reach final", stage)
		}
	}
	v.final = v.newDriver(t)
	v.before = v.snapshotProject(t)
	v.foreign = v.foreignArtifacts(t)
	outboxCleanupRequire(t, base.raw.QueryRow(ctxFor(t), `SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY id),'[]')::text FROM agenteam_audit.audit_records a WHERE scope='system'`).Scan(&v.system))
	t.Cleanup(func() {
		for _, barrier := range v.barriers {
			barrier.open()
		}
		for _, call := range v.calls {
			outboxCleanupWait(t, call.done, "finalizer original return before shutdown")
		}
		v.clearHooks()
		// Do not globally stop the only owner able to confirm an ambiguous
		// original final transaction after an assertion failure.
		if !v.closed.Load() {
			for round := 0; round < 3; round++ {
				call := v.run(t, v.final)
				outboxCleanupWait(t, call.done, "retained finalizer confirmation before shutdown")
				if call.err != nil {
					t.Error("original finalizer owner could not settle", call.err)
					break
				}
				var absent bool
				err := base.raw.QueryRow(ctxFor(t), `SELECT NOT EXISTS(SELECT 1 FROM agenteam_project.projects WHERE id=$1)`, v.p2.project.ID.String()).Scan(&absent)
				if err != nil {
					t.Error("finalizer shutdown observation failed", err)
					break
				}
				if absent {
					v.closed.Store(true)
					break
				}
			}
		}
	})
	return v
}

func finalizeRequire(t *testing.T, stage string, err error) {
	t.Helper()
	if err != nil {
		agentRetirementDiagnostic(t, "finalizer-"+stage, err)
		t.Fatalf("original finalizer chain failed at %s", stage)
	}
}

func (v *projectFinalizePG) newDriver(t *testing.T) *project.LifecycleCleanupDriver {
	t.Helper()
	driver, err := project.NewLifecycleFinalizeDriver(v.p2.base.tracked, v.p2.creator, v.p2.lifecycle, v.setup.processes)
	outboxCleanupRequire(t, err)
	v.drivers = append(v.drivers, driver)
	return driver
}

func (v *projectFinalizePG) clearHooks() {
	s := v.p2.base.tracked
	s.setAfter(nil)
	s.mu.Lock()
	s.enter, s.before, s.afterResult, s.transformResult = nil, nil, nil, nil
	s.mu.Unlock()
}

func (v *projectFinalizePG) barrier() *schedulerStopBarrier {
	barrier := newSchedulerStopBarrier()
	v.barriers = append(v.barriers, barrier)
	return barrier
}

func (v *projectFinalizePG) causeMatches(cause f.TransactionCause) bool {
	return cause.Kind() == f.JobCause && cause.Details().JobType == "project-lifecycle" && cause.Details().JobID == v.cause.OperationID.String()
}

const finalizeProjectSnapshotSQL = `SELECT jsonb_build_array(
 (SELECT to_jsonb(x) FROM agenteam_project.projects x WHERE id=$1),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_project.creations x WHERE project_id=$1),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_project.commands x WHERE project_id=$1),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_project.lifecycle_operations x WHERE project_id=$1),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY participant_name) FROM agenteam_project.lifecycle_participants x WHERE operation_id=$2),
 (SELECT jsonb_agg(to_jsonb(x) ORDER BY work_kind,work_id) FROM agenteam_project.work_claims x WHERE project_id=$1),
 (SELECT to_jsonb(x) FROM agenteam_project.deletion_receipts x WHERE deleted_project_id=$1))::text`

func (v *projectFinalizePG) snapshotProject(t *testing.T) string {
	t.Helper()
	var out string
	outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), finalizeProjectSnapshotSQL, v.p2.project.ID.String(), v.cause.OperationID.String()).Scan(&out))
	return out
}

func (v *projectFinalizePG) receipt(t *testing.T) pc.DeletionReceiptData {
	t.Helper()
	var row struct {
		Operation pc.OperationID `json:"operation_id"`
		Project   pc.ProjectID   `json:"deleted_project_id"`
		Owner     i.UserID       `json:"original_owner_user_id"`
		Key       f.Digest       `json:"command_key_hash"`
		Digest    f.Digest       `json:"request_digest"`
		At        f.Instant      `json:"completed_at"`
		Status    string         `json:"status"`
	}
	var raw []byte
	var remaining int
	outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT to_jsonb(r),(SELECT count(*) FROM agenteam_project.projects WHERE id=$1)+(SELECT count(*) FROM agenteam_project.creations WHERE project_id=$1)+(SELECT count(*) FROM agenteam_project.commands WHERE project_id=$1)+(SELECT count(*) FROM agenteam_project.lifecycle_operations WHERE project_id=$1)+(SELECT count(*) FROM agenteam_project.lifecycle_participants WHERE operation_id=$2)+(SELECT count(*) FROM agenteam_project.work_claims WHERE project_id=$1) FROM agenteam_project.deletion_receipts r WHERE deleted_project_id=$1`, v.p2.project.ID.String(), v.cause.OperationID.String()).Scan(&raw, &remaining))
	outboxCleanupRequire(t, json.Unmarshal(raw, &row))
	key, err := pc.DeletionCommandKeyHash(v.p2.project.ID, v.p2.project.OwnerUserID, v.setup.meta.IdempotencyKey)
	outboxCleanupRequire(t, err)
	digest, err := pc.DeleteDigest(v.p2.base.ownerBrowser.actor, v.setup.meta, v.p2.project.ID, v.setup.request)
	outboxCleanupRequire(t, err)
	result := pc.DeletionReceiptData{OperationID: row.Operation, DeletedProjectID: row.Project, OriginalOwnerUserID: row.Owner, CommandKeyHash: row.Key, RequestDigest: row.Digest, CompletedAt: row.At}
	if remaining != 0 || row.Status != "completed" || result.Validate() != nil || result.OperationID != v.cause.OperationID || result.DeletedProjectID != v.p2.project.ID || result.OriginalOwnerUserID != v.p2.project.OwnerUserID || result.CommandKeyHash != key || result.RequestDigest != digest {
		t.Fatal("finalizer did not atomically retain the exact receipt and remove all Project-owned FK rows", remaining)
	}
	return result
}

func (v *projectFinalizePG) other(t *testing.T) {
	t.Helper()
	v.otherReadable(t)
	var system string
	outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY id),'[]')::text FROM agenteam_audit.audit_records a WHERE scope='system'`).Scan(&system))
	if system != v.system || v.foreignArtifacts(t) != v.foreign {
		t.Fatal("Project finalization changed System or the independent Project")
	}
}

func (v *projectFinalizePG) busy(t *testing.T, call *skillsCleanupPGCall) {
	t.Helper()
	if err := v.newDriver(t).Run(ctxFor(t), v.p2.project.ID, v.cause.OperationID); !outboxCleanupFault(err, f.ResourceBusy) {
		t.Fatal("another finalizer overtook the original owner", err)
	}
	if call != nil {
		select {
		case <-call.done:
			t.Fatal("held original finalizer returned prematurely")
		default:
		}
	}
	v.other(t)
}

func (v *projectFinalizePG) await(t *testing.T, call *skillsCleanupPGCall, barrier *schedulerStopBarrier, state f.CommitState) {
	t.Helper()
	select {
	case result := <-barrier.entered:
		if result.State() != state {
			t.Fatal("finalizer barrier has the wrong original outcome", result.State())
		}
	case <-call.done:
		finalizeRequire(t, "before-original-return-barrier", call.err)
		t.Fatal("finalizer returned without its selected barrier")
	case <-time.After(5 * time.Second):
		t.Fatal("original finalizer Store return barrier was not reached")
	}
}

func sameFinalizerPGReceipt(a, b pc.DeletionReceiptData) bool {
	return a.OperationID == b.OperationID && a.DeletedProjectID == b.DeletedProjectID && a.OriginalOwnerUserID == b.OriginalOwnerUserID && a.CommandKeyHash == b.CommandKeyHash && a.RequestDigest == b.RequestDigest && a.CompletedAt.Time().Equal(b.CompletedAt.Time())
}

func (v *projectFinalizePG) verifyPublicReceipt(t *testing.T) {
	t.Helper()
	before := v.snapshotProject(t)
	observed, err := v.p2.creator.GetLifecycle(ctxFor(t), v.p2.base.ownerBrowser.actor, v.p2.project.ID, v.cause.OperationID)
	finalizeRequire(t, "public-receipt-read", err)
	replayed, err := v.p2.creator.BeginDeleteProject(ctxFor(t), v.p2.base.ownerBrowser.actor, v.setup.meta, v.p2.project.ID, v.setup.request)
	finalizeRequire(t, "original-delete-replay", err)
	for _, value := range []pc.LifecycleResult{observed, replayed} {
		if value.Validate() != nil || value.Operation != nil || value.Receipt == nil || !sameFinalizerPGReceipt(value.Receipt.Details(), v.original) {
			t.Fatal("public Delete receipt replay changed its six retained fields")
		}
	}
	if v.snapshotProject(t) != before {
		t.Fatal("receipt read or Delete replay resurrected Project history")
	}
	v.other(t)
}

func (v *projectFinalizePG) holdMutation(t *testing.T, mask bool) (*skillsCleanupPGCall, *schedulerStopBarrier, *f.CommitResult) {
	t.Helper()
	store, barrier := v.p2.base.tracked, v.barrier()
	var selected, wrapped atomic.Bool
	var original f.CommitResult
	attempt := id[f.TransactionAttempt](t)
	store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !v.causeMatches(cause) {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var exists bool
		if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_project.deletion_receipts WHERE deleted_project_id=$1)`, v.p2.project.ID.String()).Scan(&exists); err != nil {
			return err
		}
		if exists {
			selected.Store(true)
		}
		return nil
	})
	store.mu.Lock()
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if v.causeMatches(cause) && selected.Load() && result.State() == f.Committed {
			barrier.hold(result)
		}
	}
	if mask {
		store.transformResult = func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
			if v.causeMatches(cause) && selected.Load() && result.State() == f.Committed && wrapped.CompareAndSwap(false, true) {
				original = f.UnknownResult(attempt, cause) // Local ACK mask, not a network failure.
				return original
			}
			return result
		}
	}
	store.mu.Unlock()
	return v.run(t, v.final), barrier, &original
}

func testProjectFinalizeReturn(t *testing.T) {
	v := newProjectFinalizePG(t)
	call, barrier, _ := v.holdMutation(t, false)
	v.await(t, call, barrier, f.Committed)
	v.original = v.receipt(t)
	v.busy(t, call)
	barrier.open()
	outboxCleanupWait(t, call.done, "original atomic Project finalization")
	finalizeRequire(t, "atomic-final-return", call.err)
	v.closed.Store(true)
	v.clearHooks()
	if !sameFinalizerPGReceipt(v.receipt(t), v.original) {
		t.Fatal("original final Store return changed its receipt")
	}
	v.verifyPublicReceipt(t)
	t.Log("PROJECT_FINALIZE_RETURN formal_delete_command=true real_predecessor_stop_and_cleanup=true original_final_store_return=true atomic_project_fk_removal=true exact_six_field_receipt=true other_project_live=true no_default_registry=true")
}

func (v *projectFinalizePG) unknown(t *testing.T, err error, original f.CommitResult) {
	t.Helper()
	var problem *f.Fault
	if !errors.As(err, &problem) || problem.Code != f.CommitUnknown || problem.CommitState != f.Unknown || problem.CauseID != original.AttemptID().String() || original.State() != f.Unknown || !v.causeMatches(original.Cause()) {
		agentRetirementDiagnostic(t, "finalizer-original-unknown", err)
		t.Fatal("finalizer lost the original local acknowledgement Unknown")
	}
}

const finalizeWritesSQL = `SELECT COALESCE(sum(n_tup_ins+n_tup_upd+n_tup_del),0)::bigint FROM pg_stat_xact_user_tables WHERE schemaname='agenteam_project'`

func (v *projectFinalizePG) confirmation(t *testing.T, snapshot string, original f.TransactionCause) {
	t.Helper()
	v.clearHooks()
	store, barrier := v.p2.base.tracked, v.barrier()
	var before sync.Map
	var observations atomic.Int64
	store.mu.Lock()
	store.before = func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !v.causeMatches(cause) {
			return nil
		}
		if cause.Details().JobAttemptID != original.Details().JobAttemptID {
			return errors.New("confirmation replaced the original finalizer claim cause")
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var writes int64
		if err = x.QueryRow(ctx, finalizeWritesSQL).Scan(&writes); err != nil {
			return err
		}
		before.Store(tx, writes)
		return nil
	}
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if v.causeMatches(cause) && observations.Load() == 1 && result.State() == f.Committed {
			barrier.hold(result)
		}
	}
	store.mu.Unlock()
	store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !v.causeMatches(cause) {
			return nil
		}
		baseline, ok := before.LoadAndDelete(tx)
		if !ok {
			return errors.New("confirmation lacks its same-transaction baseline")
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var writes int64
		if err = x.QueryRow(ctx, finalizeWritesSQL).Scan(&writes); err != nil {
			return err
		}
		if writes-baseline.(int64) != 0 {
			return errors.New("original finalizer confirmation performed a write")
		}
		observations.Add(1)
		return nil
	})
	call := v.run(t, v.final)
	v.await(t, call, barrier, f.Committed)
	if observations.Load() != 1 || v.snapshotProject(t) != snapshot {
		t.Fatal("Unknown confirmation changed facts before its original return")
	}
	v.busy(t, call)
	barrier.open()
	outboxCleanupWait(t, call.done, "original read-only finalizer confirmation")
	finalizeRequire(t, "readonly-confirmation-return", call.err)
	if observations.Load() != 1 || v.snapshotProject(t) != snapshot {
		t.Fatal("confirmation was not one original read-only transaction")
	}
	v.clearHooks()
}

func testProjectFinalizeCommittedUnknown(t *testing.T) {
	v := newProjectFinalizePG(t)
	call, barrier, original := v.holdMutation(t, true)
	v.await(t, call, barrier, f.Committed)
	v.original = v.receipt(t)
	v.busy(t, call)
	barrier.open()
	outboxCleanupWait(t, call.done, "original committed finalizer acknowledgement mask")
	v.unknown(t, call.err, *original)
	v.busy(t, nil)
	v.confirmation(t, v.snapshotProject(t), original.Cause())
	v.closed.Store(true)
	if !sameFinalizerPGReceipt(v.receipt(t), v.original) {
		t.Fatal("receipt-first confirmation replaced the original receipt")
	}
	v.verifyPublicReceipt(t)
	t.Log("PROJECT_FINALIZE_UNKNOWN local_committed_ack_mask=true original_attempt_cause=true project_already_absent=true receipt_first_confirmation=true same_transaction_zero_writes=true confirmation_original_return=true other_project_live=true no_network_lost_ack=true")
}

func testProjectFinalizeRegistrationUnknown(t *testing.T) {
	v := newProjectFinalizePG(t)
	store, barrier := v.p2.base.tracked, v.barrier()
	var selected, wrapped atomic.Bool
	var original f.CommitResult
	sentinel := errors.New("controlled finalizer registration rollback")
	attempt := id[f.TransactionAttempt](t)
	store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !v.causeMatches(cause) {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var exact bool
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_project.work_claims WHERE work_kind='lifecycle' AND work_id=$1 AND project_id=$2 AND phase='running' AND attempt_id::text=$3)`, v.cause.OperationID.String(), v.p2.project.ID.String(), cause.Details().JobAttemptID).Scan(&exact)
		if err != nil {
			return err
		}
		if exact {
			selected.Store(true)
			return sentinel
		}
		return nil
	})
	store.mu.Lock()
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if v.causeMatches(cause) && selected.Load() && result.State() == f.NotCommitted {
			barrier.hold(result)
		}
	}
	store.transformResult = func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if v.causeMatches(cause) && selected.Load() && result.State() == f.NotCommitted && errors.Is(result.Fault(), sentinel) && wrapped.CompareAndSwap(false, true) {
			original = f.UnknownResult(attempt, cause) // The original transaction actually rolled back.
			return original
		}
		return result
	}
	store.mu.Unlock()
	call := v.run(t, v.final)
	v.await(t, call, barrier, f.NotCommitted)
	if v.snapshotProject(t) != v.before {
		t.Fatal("rolled-back registration changed original facts")
	}
	v.busy(t, call)
	barrier.open()
	outboxCleanupWait(t, call.done, "original rolled-back registration acknowledgement")
	v.unknown(t, call.err, original)
	v.busy(t, nil)
	v.confirmation(t, v.before, original.Cause())
	// Read-only confirmation does not release this original retained round.
	v.busy(t, nil)
	next := v.run(t, v.final)
	outboxCleanupWait(t, next.done, "explicit finalization after unapplied confirmation")
	finalizeRequire(t, "explicit-resume", next.err)
	v.closed.Store(true)
	v.original = v.receipt(t)
	v.verifyPublicReceipt(t)
	t.Log("PROJECT_FINALIZE_REGISTRATION_UNKNOWN actual_registration_rollback=true local_ack_mask=true original_fact_snapshot_unchanged=true confirmation_same_tx_zero_writes=true held_confirmation_blocks_second_driver=true separate_explicit_run_applies=true no_default_registry=true")
}
