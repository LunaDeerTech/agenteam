//go:build integration

package projectvariable_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	objc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// The ordinary Account/Project fixture is real; its persistent test Skills
// receipt remains explicit. The Object process uses the complete recovery graph
// and its actual initialized guard. No delivery callback is started. Accepted
// Delete and other domains' completed states below are controlled prerequisites,
// not evidence that BeginDelete or a complete registry performed retirement.
func TestProjectOutboxCleanup(t *testing.T) {
	t.Run("original-d06-return-before-project-progress", testOutboxCleanupReturn)
	t.Run("unknown-preflight-and-expired-owner", testOutboxCleanupUnknown)
}

type outboxCleanupPGHandler struct{ calls atomic.Int64 }

func (h *outboxCleanupPGHandler) Prepare(context.Context, event.Event) (oc.HandlerPlan, error) {
	h.calls.Add(1)
	return oc.HandlerPlan{}, errors.New("ordinary delivery is outside cleanup fixture")
}
func (h *outboxCleanupPGHandler) ValidateInTx(context.Context, f.Tx, event.Event, oc.HandlerPlan) error {
	h.calls.Add(1)
	return errors.New("ordinary delivery is outside cleanup fixture")
}
func (h *outboxCleanupPGHandler) HandleInTx(context.Context, f.Tx, event.Event, oc.HandlerPlan) oc.Result {
	h.calls.Add(1)
	return oc.Result{}
}

type outboxCleanupPGTap struct {
	pc.ProjectLifecycleParticipant
	mu              sync.Mutex
	saved           context.Context
	actor           i.Actor
	calls, returned int
}

func (p *outboxCleanupPGTap) Cleanup(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, checkpoint *pc.CleanupCheckpoint) (pc.CleanupReport, error) {
	p.mu.Lock()
	p.saved, p.actor = ctx, actor
	p.calls++
	p.mu.Unlock()
	defer func() { p.mu.Lock(); p.returned++; p.mu.Unlock() }()
	return p.ProjectLifecycleParticipant.Cleanup(ctx, actor, cause, scope, checkpoint)
}

type outboxCleanupPGCall struct {
	done   chan struct{}
	cancel context.CancelFunc
	err    error
}

type outboxCleanupPG struct {
	base     *variableHTTPFixture
	runtime  *agentOwnershipRuntime
	process  *phaseGuardProcesses
	facts    *project.LifecycleAuthority
	box      *outbox.Service
	adapter  pc.ProjectLifecycleParticipant
	tap      *outboxCleanupPGTap
	stop     *project.LifecycleStopDriver
	drivers  []*project.LifecycleCleanupDriver
	calls    []*outboxCleanupPGCall
	barriers []*schedulerStopBarrier
	handler  *outboxCleanupPGHandler
	a, b     pc.ProjectRef
	manifest pc.RequiredManifest
	cause    pc.LifecycleCause
	actor    i.Actor
	foreign  string
	stable   string
	closed   sync.Once
	claim    *os.File
}

func outboxCleanupRequire(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func newOutboxCleanupPG(t *testing.T) *outboxCleanupPG {
	t.Helper()
	v := &outboxCleanupPG{base: newVariableHTTPFixture(t), handler: &outboxCleanupPGHandler{}}
	storage, _ := phaseGuardStorage(t, "")
	spool := filepath.Join(t.TempDir(), "spool")
	// Register after the owned path: the actual graph must retire before
	// testing removes that path or the inherited Store/database fixture.
	t.Cleanup(func() { v.close(t) })
	process := id[objc.Process](t)
	var err error
	v.runtime, err = newAgentOwnershipRuntime(v.base.tracked, v.base.accounts, v.base.keys, storage, spool, process)
	outboxCleanupRequire(t, err)
	outboxCleanupRequire(t, v.runtime.runtime.Initialize(ctxFor(t)))
	actual, err := v.runtime.guard.CurrentProcess()
	outboxCleanupRequire(t, err)
	if actual != process {
		t.Fatal("original bound process identity changed")
	}
	v.process = &phaseGuardProcesses{guard: v.runtime.guard, process: process}
	v.claim, err = os.OpenFile(filepath.Join(spool+".processes", process.String()+".claim"), os.O_RDWR, 0)
	outboxCleanupRequire(t, err)
	v.manifest, err = agentOwnershipManifest()
	outboxCleanupRequire(t, err)
	v.facts, err = project.NewLifecycleAuthority(v.base.tracked, v.manifest, nil)
	outboxCleanupRequire(t, err)
	projects, err := project.NewAuthority(v.base.tracked, project.AuthorityDependencies{Sessions: v.base.accounts, Routes: v.base.accounts, Lifecycle: v.facts})
	outboxCleanupRequire(t, err)
	catalog := event.NewCatalog()
	_, err = pc.RegisterProjectEvents(catalog)
	outboxCleanupRequire(t, err)
	v.box, err = outbox.New(v.base.tracked, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{pc.ProjectProducer: projects}, Projects: projects, Sessions: v.base.accounts, System: v.base.accounts, Processes: v.process, Audit: v.base.audit, Cursors: v.base.keys})
	outboxCleanupRequire(t, err)
	definition := oc.HandlerDefinition{ID: "project.cleanup-fixture", Effect: oc.DomainIngress, Ordering: oc.VersionGuarded, Handler: v.handler, Subscriptions: []oc.Subscription{{EventType: pc.CreatedEventName, Versions: []uint32{pc.ProjectEventSchemaVersion}}}}
	registration, err := v.box.RegisterHandler(ctxFor(t), definition)
	outboxCleanupRequire(t, err)
	if registration.HandlerID != definition.ID {
		t.Fatal("original handler registration changed identity")
	}
	// These original Project commands create actual events and deliveries after
	// handler registration. The independent Project B is never stopped.
	v.a, _, _ = v.base.createProject(t, v.base.ownerBrowser.actor, "cleanup-target")
	v.b, _, _ = v.base.createProject(t, v.base.ownerBrowser.actor, "cleanup-foreign")
	v.requireQueue(t, v.a.ID, 1, "pending")
	v.requireQueue(t, v.b.ID, 1, "pending")
	v.foreign = v.snapshot(t, v.b.ID)
	v.cause = pc.LifecycleCause{OperationID: id[pc.Operation](t), Action: pc.Delete, ProjectVersion: v.a.Version + 1}
	v.stageAccepted(t)
	v.adapter, err = project.NewOutboxLifecycleParticipant(v.facts, v.box)
	outboxCleanupRequire(t, err)
	v.tap = &outboxCleanupPGTap{ProjectLifecycleParticipant: v.adapter}
	v.stop, err = project.NewLifecycleStopDriver(v.base.tracked, v.facts, v.process, func(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
		if cause != v.cause || scope.ProjectID != v.a.ID {
			return errors.New("original stop identity changed")
		}
		v.actor = actor
		report, err := v.adapter.RequestStop(ctx, actor, cause, scope)
		if err != nil {
			return err
		}
		if !report.Matches(pc.OutboxParticipant, cause, scope) || report.Details().State != pc.Stopped {
			return errors.New("real nonempty D06 stop was not confirmed")
		}
		return nil
	})
	outboxCleanupRequire(t, err)
	outboxCleanupRequire(t, v.stop.Run(ctxFor(t), v.a.ID, v.cause.OperationID))
	v.stop.Stop()
	outboxCleanupRequire(t, v.stop.Drain(ctxFor(t)))
	v.requireQueue(t, v.a.ID, 1, "dead_letter")
	v.requireState(t, "stopping", "", "required", "terminal", 1)
	var atEnd bool
	err = v.base.raw.QueryRow(ctxFor(t), `SELECT l.scan_sequence=e.sequence AND l.recovery_pass=0 FROM agenteam_outbox.project_lifecycle l JOIN agenteam_outbox.events e ON e.project_id=l.project_id WHERE l.project_id=$1`, v.a.ID.String()).Scan(&atEnd)
	outboxCleanupRequire(t, err)
	if !atEnd {
		t.Fatal("original stop did not preserve its exact completed scan position")
	}
	v.stageOutbox(t)
	v.stable = v.canonical(t)
	return v
}

func (v *outboxCleanupPG) tx(t *testing.T, callback func(context.Context, f.Tx) error) {
	t.Helper()
	cause, err := f.NewRecoveryCause("outbox-cleanup.fixture", v.cause.OperationID.String(), "")
	outboxCleanupRequire(t, err)
	result := v.base.tracked.WithinTx(ctxFor(t), cause, func(ctx context.Context, tx f.Tx) error {
		lock, _ := f.ProjectLock(v.a.ID.String())
		if err := v.base.tracked.AcquireAll(ctx, tx, []f.LockRequest{{Key: lock, Mode: f.Exclusive}}); err != nil {
			return err
		}
		return callback(ctx, tx)
	})
	if result.State() != f.Committed {
		t.Fatal("controlled prerequisite transaction did not commit")
	}
}

func (v *outboxCleanupPG) stageAccepted(t *testing.T) {
	t.Helper()
	raw, err := json.Marshal(v.manifest.Entries())
	outboxCleanupRequire(t, err)
	digest, err := v.manifest.Digest()
	outboxCleanupRequire(t, err)
	v.tx(t, func(ctx context.Context, tx f.Tx) error {
		x, err := v.base.tracked.InTx(tx)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_operations(id,project_id,owner_user_id,action,project_version,state,version,required_manifest,manifest_digest,created_at,updated_at) VALUES($1,$2,$3,'delete',$4,'accepted',1,$5::jsonb,$6,clock_timestamp(),clock_timestamp())`, v.cause.OperationID.String(), v.a.ID.String(), v.a.OwnerUserID.String(), int64(v.cause.ProjectVersion), raw, digest.String())
		if err != nil {
			return err
		}
		for _, entry := range v.manifest.Entries() {
			if _, err = x.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_participants(operation_id,participant_name,contract_version,stop_state,cleanup_state,version) VALUES($1,$2,$3,'required','required',1)`, v.cause.OperationID.String(), string(entry.Name), int64(entry.ContractVersion)); err != nil {
				return err
			}
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_project.projects SET lifecycle='deleting',version=$2,current_lifecycle_operation_id=$3,updated_at=clock_timestamp() WHERE id=$1 AND lifecycle='active' AND version=$4 AND current_lifecycle_operation_id IS NULL`, v.a.ID.String(), int64(v.cause.ProjectVersion), v.cause.OperationID.String(), int64(v.a.Version))
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("accepted Delete prerequisite changed original Project")
		}
		return nil
	})
}

func (v *outboxCleanupPG) stageOutbox(t *testing.T) {
	t.Helper()
	// Only this prerequisite is controlled. It is not a claim that the other
	// domains actually cleaned up; the following Outbox driver is real.
	v.tx(t, func(ctx context.Context, tx f.Tx) error {
		x, err := v.base.tracked.InTx(tx)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_project.lifecycle_participants SET stop_state='stopped',cleanup_state=CASE WHEN participant_name IN ('outbox','audit') THEN 'required' ELSE 'completed' END WHERE operation_id=$1`, v.cause.OperationID.String())
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_project.lifecycle_operations SET state='cleaning',cleanup_stage='outbox',version=version+1,updated_at=clock_timestamp() WHERE id=$1 AND state='stopping'`, v.cause.OperationID.String())
		return err
	})
}

func (v *outboxCleanupPG) newDriver(t *testing.T) *project.LifecycleCleanupDriver {
	t.Helper()
	driver, err := project.NewLifecycleCleanupDriver(v.base.tracked, v.facts, v.process, v.tap)
	outboxCleanupRequire(t, err)
	v.drivers = append(v.drivers, driver)
	return driver
}

func (v *outboxCleanupPG) run(t *testing.T, driver *project.LifecycleCleanupDriver) *outboxCleanupPGCall {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	call := &outboxCleanupPGCall{done: make(chan struct{}), cancel: cancel}
	v.calls = append(v.calls, call)
	go func() { defer close(call.done); call.err = driver.Run(ctx, v.a.ID, v.cause.OperationID) }()
	return call
}

func (v *outboxCleanupPG) scope() pc.ScopeRef {
	return pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: v.a.ID}
}

func (v *outboxCleanupPG) requireQueue(t *testing.T, project i.ProjectID, want int, phase string) {
	t.Helper()
	var events, deliveries, exact, attempts int
	err := v.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1),(SELECT count(*) FROM agenteam_outbox.deliveries WHERE project_id=$1),(SELECT count(*) FROM agenteam_outbox.deliveries WHERE project_id=$1 AND phase=$2 AND handler_id='project.cleanup-fixture' AND current_attempt_id IS NULL),(SELECT count(*) FROM agenteam_outbox.attempts a JOIN agenteam_outbox.deliveries d ON d.id=a.delivery_id WHERE d.project_id=$1)`, project.String(), phase).Scan(&events, &deliveries, &exact, &attempts)
	outboxCleanupRequire(t, err)
	if events != want || deliveries != want || exact != want || attempts != 0 {
		t.Fatalf("actual D06 queue differs: %d/%d/%d/%d want %d", events, deliveries, exact, attempts, want)
	}
}

func (v *outboxCleanupPG) requireState(t *testing.T, state, stage, cleanup, claim string, fence int64) {
	t.Helper()
	var gotState, gotStage, gotCleanup, gotClaim, gotProcess string
	var gotFence int64
	err := v.base.raw.QueryRow(ctxFor(t), `SELECT o.state,coalesce(o.cleanup_stage,''),p.cleanup_state,w.phase,w.process_id::text,w.fence FROM agenteam_project.lifecycle_operations o JOIN agenteam_project.lifecycle_participants p ON p.operation_id=o.id AND p.participant_name='outbox' JOIN agenteam_project.work_claims w ON w.work_id=o.id AND w.work_kind='lifecycle' WHERE o.id=$1 AND w.project_id=o.project_id`, v.cause.OperationID.String()).Scan(&gotState, &gotStage, &gotCleanup, &gotClaim, &gotProcess, &gotFence)
	outboxCleanupRequire(t, err)
	if gotState != state || gotStage != stage || gotCleanup != cleanup || gotClaim != claim || gotFence != fence || gotProcess != v.process.process.String() {
		t.Fatalf("actual lifecycle state differs: %s/%s/%s/%s/%d", gotState, gotStage, gotCleanup, gotClaim, gotFence)
	}
}

func (v *outboxCleanupPG) requireReceipt(t *testing.T, phase string) {
	t.Helper()
	var matches int
	err := v.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_outbox.project_lifecycle WHERE project_id=$1 AND operation_id=$2 AND action='delete' AND project_version=$3 AND phase=$4 AND ((phase='completed')=(completed_at IS NOT NULL))`, v.a.ID.String(), v.cause.OperationID.String(), int64(v.cause.ProjectVersion), phase).Scan(&matches)
	outboxCleanupRequire(t, err)
	if matches != 1 {
		t.Fatal("original exact D06 lifecycle receipt differs")
	}
}

func (v *outboxCleanupPG) completePendingScan(t *testing.T, driver *project.LifecycleCleanupDriver, fence int64) {
	t.Helper()
	// Stop left its cursor at the sole Event. The first Cleanup therefore
	// commits an empty tail scan and resets that cursor; it does not delete the
	// Event. Consume this one explicit Pending round before the deletion round.
	call := v.run(t, driver)
	outboxCleanupWait(t, call.done, "original empty-tail cleanup round")
	outboxCleanupRequire(t, call.err)
	v.requireState(t, "cleaning", "outbox", "pending", "terminal", fence)
	v.requireQueue(t, v.a.ID, 1, "dead_letter")
	v.requireReceipt(t, "cleaning")
	var reset bool
	err := v.base.raw.QueryRow(ctxFor(t), `SELECT scan_sequence=0 AND recovery_pass=1 FROM agenteam_outbox.project_lifecycle WHERE project_id=$1 AND operation_id=$2`, v.a.ID.String(), v.cause.OperationID.String()).Scan(&reset)
	outboxCleanupRequire(t, err)
	if !reset {
		t.Fatal("Pending did not persist the exact empty-tail scan reset")
	}
	v.requireIsolated(t)
}

func (v *outboxCleanupPG) canonical(t *testing.T) string {
	t.Helper()
	var value string
	err := v.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_array((SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1),(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_project.commands c WHERE project_id=$1),(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a WHERE project_id=$1),(SELECT jsonb_agg(to_jsonb(p) ORDER BY participant_name) FROM agenteam_project.lifecycle_participants p WHERE operation_id=$2 AND participant_name<>'outbox'))::text`, v.a.ID.String(), v.cause.OperationID.String()).Scan(&value)
	outboxCleanupRequire(t, err)
	return value
}

func (v *outboxCleanupPG) snapshot(t *testing.T, project i.ProjectID) string {
	t.Helper()
	var value string
	err := v.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_array((SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1),(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_outbox.events e WHERE project_id=$1),(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM agenteam_outbox.deliveries d WHERE project_id=$1),(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a WHERE project_id=$1),(SELECT to_jsonb(l) FROM agenteam_outbox.project_lifecycle l WHERE project_id=$1))::text`, project.String()).Scan(&value)
	outboxCleanupRequire(t, err)
	return value
}

func (v *outboxCleanupPG) requireIsolated(t *testing.T) {
	t.Helper()
	if v.snapshot(t, v.b.ID) != v.foreign || v.canonical(t) != v.stable || v.handler.calls.Load() != 0 {
		t.Fatal("Outbox cleanup changed foreign Project, canonical/history, other participants or invoked a delivery")
	}
	actual, err := v.runtime.guard.CurrentProcess()
	outboxCleanupRequire(t, err)
	if actual != v.process.process {
		t.Fatal("original real guard retired before owned calls")
	}
	if err = syscall.Flock(int(v.claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); !errors.Is(err, syscall.EWOULDBLOCK) {
		if err == nil {
			_ = syscall.Flock(int(v.claim.Fd()), syscall.LOCK_UN)
		}
		t.Fatal("original process guard was physically released before owner return")
	}
}

func testOutboxCleanupReturn(t *testing.T) {
	v := newOutboxCleanupPG(t)
	// Matching public DTOs plus a persisted cleaning phase are insufficient.
	report, err := v.adapter.Cleanup(ctxFor(t), v.actor, v.cause, v.scope(), nil)
	if !outboxCleanupFault(err, f.Forbidden) || report.Details().State != "" {
		t.Fatal("public cause manufactured cleanup authority", err)
	}
	v.requireQueue(t, v.a.ID, 1, "dead_letter")
	driver := v.newDriver(t)
	v.completePendingScan(t, driver, 2)
	barrier := newSchedulerStopBarrier()
	v.barriers = append(v.barriers, barrier)
	var held atomic.Bool
	v.base.tracked.mu.Lock()
	v.base.tracked.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if cause.Details().Owner == "outbox.cleanup-batch" && result.State() == f.Committed && held.CompareAndSwap(false, true) {
			barrier.hold(result)
		}
	}
	v.base.tracked.mu.Unlock()
	call := v.run(t, driver)
	barrier.await(t)
	// Physical D06 COMMIT is independently visible while its original Store
	// return, adapter and driver have not returned. Project cannot advance yet.
	v.requireQueue(t, v.a.ID, 0, "dead_letter")
	v.requireReceipt(t, "completed")
	v.requireState(t, "cleaning", "outbox", "pending", "running", 3)
	v.requireIsolated(t)
	driver.Stop()
	// This bounded observation returns an error while the original call is
	// held; it is not a join or a background Drain left for cleanup to guess.
	drainCtx, cancelDrain := context.WithTimeout(context.Background(), 100*time.Millisecond)
	drainErr := driver.Drain(drainCtx)
	cancelDrain()
	if !errors.Is(drainErr, context.DeadlineExceeded) {
		t.Fatal("driver Drain bypassed the original held D06 return", drainErr)
	}
	select {
	case <-call.done:
		t.Fatal("held original D06 return was treated as driver return")
	default:
	}
	barrier.open()
	outboxCleanupWait(t, call.done, "original cancelled cleanup round")
	outboxCleanupRequire(t, driver.Drain(ctxFor(t)))
	if call.err == nil {
		t.Fatal("cancelled original round published successful progress")
	}
	v.requireState(t, "cleaning", "outbox", "pending", "terminal", 3)
	v.base.tracked.mu.Lock()
	v.base.tracked.afterResult = nil
	v.base.tracked.mu.Unlock()
	// A new explicitly invoked owner consumes the actual completed D06 receipt;
	// it does not recreate events or infer completion from a missing queue.
	next := v.run(t, v.newDriver(t))
	outboxCleanupWait(t, next.done, "original completed receipt replay")
	outboxCleanupRequire(t, next.err)
	v.requireState(t, "cleaning", "audit", "completed", "terminal", 4)
	v.requireQueue(t, v.a.ID, 0, "dead_letter")
	v.requireIsolated(t)
	v.close(t)
	t.Log("OUTBOX_CLEANUP_RETURN actual_d06_commit=true original_return_held=true stop_drain_waited=true original_receipt_replay=true audit_stage_only=true foreign_unchanged=true")
}

func testOutboxCleanupUnknown(t *testing.T) {
	v := newOutboxCleanupPG(t)
	unknownAttempt := id[f.TransactionAttempt](t)
	var originalCause f.TransactionCause
	var masked atomic.Bool
	v.base.tracked.mu.Lock()
	v.base.tracked.transformResult = func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if cause.Details().Owner == "project.outbox-cleanup-preflight" && result.State() == f.Committed && masked.CompareAndSwap(false, true) {
			originalCause = cause
			return f.UnknownResult(unknownAttempt, cause)
		}
		return result
	}
	v.base.tracked.mu.Unlock()
	driver := v.newDriver(t)
	call := v.run(t, driver)
	outboxCleanupWait(t, call.done, "original unknown preflight")
	if !masked.Load() || !outboxCleanupFault(call.err, f.CommitUnknown) {
		t.Fatal("actual committed preflight was not returned as controlled Unknown", call.err)
	}
	original, found := project.UnknownAttempt(call.err)
	if !found || original.State() != f.Unknown || original.AttemptID() != unknownAttempt || !reflect.DeepEqual(original.Cause().Details(), originalCause.Details()) {
		t.Fatal("controlled original Unknown lost its exact attempt or cause")
	}
	v.requireQueue(t, v.a.ID, 1, "dead_letter")
	v.requireReceipt(t, "stopped")
	v.requireState(t, "cleaning", "outbox", "required", "terminal", 2)
	v.tap.mu.Lock()
	saved, actor, calls, returned := v.tap.saved, v.tap.actor, v.tap.calls, v.tap.returned
	v.tap.mu.Unlock()
	if saved == nil || calls != 1 || returned != 1 {
		t.Fatal("original adapter return was not observed")
	}
	v.base.tracked.mu.Lock()
	v.base.tracked.transformResult = nil
	v.base.tracked.mu.Unlock()
	report, err := v.adapter.Cleanup(context.WithoutCancel(saved), actor, v.cause, v.scope(), nil)
	if !outboxCleanupFault(err, f.Forbidden) || report.Details().State != "" {
		t.Fatal("saved callback context outlived its original cleanup owner", err)
	}
	v.requireQueue(t, v.a.ID, 1, "dead_letter")
	// Same original operation, explicit new call. Unknown is never treated as
	// rollback; this group's mask changed only the read-only preflight result.
	v.completePendingScan(t, driver, 3)
	next := v.run(t, driver)
	outboxCleanupWait(t, next.done, "explicit original operation cleanup")
	outboxCleanupRequire(t, next.err)
	v.requireState(t, "cleaning", "audit", "completed", "terminal", 4)
	v.requireQueue(t, v.a.ID, 0, "dead_letter")
	v.requireIsolated(t)
	v.close(t)
	t.Log("OUTBOX_CLEANUP_UNKNOWN local_preflight_ackmask=true zero_downstream_cleanup=true original_return_observed=true saved_scope_rejected=true explicit_same_operation=true")
}

func outboxCleanupFault(err error, code f.Code) bool {
	var fault *f.Fault
	return errors.As(err, &fault) && fault.Code == code
}

func outboxCleanupWait(t *testing.T, done <-chan struct{}, phase string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Error(phase + " did not return; retaining the original resource graph until the Go hard deadline")
		<-done
	}
}

func (v *outboxCleanupPG) close(t *testing.T) {
	t.Helper()
	v.closed.Do(func() {
		for _, barrier := range v.barriers {
			barrier.open()
		}
		for _, call := range v.calls {
			call.cancel()
			outboxCleanupWait(t, call.done, "original cleanup Run")
		}
		for _, driver := range v.drivers {
			driver.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			err := driver.Drain(ctx)
			cancel()
			if err != nil {
				t.Error("original cleanup driver did not drain; retaining graph", err)
				select {}
			}
		}
		if v.stop != nil {
			v.stop.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			err := v.stop.Drain(ctx)
			cancel()
			if err != nil {
				t.Error("original stop driver did not drain; retaining graph", err)
				select {}
			}
		}
		v.base.tracked.mu.Lock()
		v.base.tracked.afterResult, v.base.tracked.transformResult = nil, nil
		v.base.tracked.mu.Unlock()
		if v.tap != nil {
			v.tap.mu.Lock()
			calls, returned := v.tap.calls, v.tap.returned
			v.tap.mu.Unlock()
			if calls != returned {
				t.Error("original D06 adapter has not returned; retaining graph")
				select {}
			}
		}
		if v.runtime != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := v.runtime.drain(ctx)
			cancel()
			if err != nil {
				v.runtime.retain()
				t.Error("full Object runtime did not retire; retaining original guard", err)
				select {}
			}
		}
		if v.claim != nil {
			if err := syscall.Flock(int(v.claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
				t.Error("original guard remains physically held; retaining graph", err)
				v.runtime.retain()
				select {}
			} else {
				outboxCleanupRequire(t, syscall.Flock(int(v.claim.Fd()), syscall.LOCK_UN))
			}
			outboxCleanupRequire(t, v.claim.Close())
			var state string
			err := v.base.raw.QueryRow(ctxFor(t), `SELECT state FROM agenteam_object.process_claims WHERE process_id=$1`, v.process.process.String()).Scan(&state)
			if err != nil || state != "stopped" {
				t.Error("original runtime terminal process receipt is missing", err)
			}
		}
	})
}
