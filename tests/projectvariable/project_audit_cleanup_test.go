//go:build integration

package projectvariable_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// The inherited graph uses real Account/Project commands, D06 and the complete
// initialized Object runtime. Its test Skills receipt and accepted Delete /
// other domains' completed prerequisite remain controlled. These tests neither
// bind a complete Registry nor run Audit-independent cleanup or the finalizer.
func TestProjectAuditCleanup(t *testing.T) {
	t.Run("outbox-to-audit-500-plus-one-original-return", testProjectAuditBatches)
	t.Run("original-unknown-confirmation-and-expired-scope", testProjectAuditUnknown)
}

type auditCleanupPG struct {
	*outboxCleanupPG
	audit      *audit.Service
	adapter    pc.ProjectLifecycleParticipant
	auditTap   *outboxCleanupPGTap
	driver     *project.LifecycleCleanupDriver
	system     string
	canonical  string
	wantEvents int
}

func newAuditCleanupPG(t *testing.T, seed501 bool) *auditCleanupPG {
	t.Helper()
	v := &auditCleanupPG{wantEvents: 1}
	if seed501 {
		v.outboxCleanupPG = newOutboxCleanupPGWithSeed(t, func(base *outboxCleanupPG) int {
			var initial, initialEvents int
			err := base.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_audit.audit_records WHERE scope='project' AND project_id=$1),(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1)`, base.a.ID.String()).Scan(&initial, &initialEvents)
			outboxCleanupRequire(t, err)
			if initial != 2 || initialEvents != 1 {
				t.Fatal("original accepted/completed creation Audit and event differ", initial, initialEvents)
			}
			for n := initial; n < 501; n++ {
				description := fmt.Sprintf("audit-batch-%d", n)
				version := base.a.Version
				updated, err := base.base.projects.UpdateProject(ctxFor(t), base.base.ownerBrowser.actor, meta(t, id[struct{}](t).String(), &version), base.a.ID, pc.UpdateProjectRequest{Description: &description})
				outboxCleanupRequire(t, err)
				if updated.ID != base.a.ID || updated.Version != version+1 || updated.Description != description {
					t.Fatal("original Project update did not return its exact new version")
				}
				base.a = updated
			}
			var all, accepted, completed, updates int
			err = base.base.raw.QueryRow(ctxFor(t), `SELECT count(*),count(*) FILTER(WHERE action='project.create.accepted'),count(*) FILTER(WHERE action='project.create.completed'),count(*) FILTER(WHERE action='project.update') FROM agenteam_audit.audit_records WHERE scope='project' AND project_id=$1`, base.a.ID.String()).Scan(&all, &accepted, &completed, &updates)
			outboxCleanupRequire(t, err)
			if all != 501 || accepted != 1 || completed != 1 || updates != 501-initial {
				t.Fatal("501 Audit rows were not produced by the original commands", all, accepted, completed, updates)
			}
			v.wantEvents = initialEvents + 501 - initial
			return v.wantEvents
		})
	} else {
		v.outboxCleanupPG = newOutboxCleanupPG(t)
	}
	projects, err := project.NewAuthority(v.base.tracked, project.AuthorityDependencies{Sessions: v.base.accounts, Routes: v.base.accounts, Lifecycle: v.facts})
	outboxCleanupRequire(t, err)
	v.audit, err = audit.New(v.base.tracked, v.base.keys, audit.Authorizations{Sessions: v.base.accounts, System: v.base.accounts, Accounts: v.base.accounts, Projects: projects})
	outboxCleanupRequire(t, err)
	v.adapter, err = project.NewAuditLifecycleParticipant(v.facts, v.audit)
	outboxCleanupRequire(t, err)
	v.auditTap = &outboxCleanupPGTap{ProjectLifecycleParticipant: v.adapter}
	v.driver, err = project.NewLifecycleCleanupDriverWithAudit(v.base.tracked, v.facts, v.process, v.tap, v.auditTap)
	outboxCleanupRequire(t, err)
	v.drivers = append(v.drivers, v.driver)
	// This graph has only formal Project command Audit rows for A. It does not
	// model prior Object cleanup by removing upload rows or suppressing its FK.
	var uploadReferences int
	err = v.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_object.uploads u JOIN agenteam_audit.audit_records a ON a.id=u.audit_id WHERE a.scope='project' AND a.project_id=$1`, v.a.ID.String()).Scan(&uploadReferences)
	outboxCleanupRequire(t, err)
	if uploadReferences != 0 {
		t.Fatal("Audit fixture requires a Project without Object upload references")
	}
	v.system = v.systemSnapshot(t)
	v.canonical = v.originalCanonical(t)
	return v
}

func (v *auditCleanupPG) systemSnapshot(t *testing.T) string {
	t.Helper()
	var snapshot string
	var count int
	err := v.base.raw.QueryRow(ctxFor(t), `SELECT count(*),coalesce(jsonb_agg(to_jsonb(a) ORDER BY id),'[]'::jsonb)::text FROM agenteam_audit.audit_records a WHERE scope='system'`).Scan(&count, &snapshot)
	outboxCleanupRequire(t, err)
	if count == 0 {
		t.Fatal("System Audit isolation needs original nonempty Account facts")
	}
	return snapshot
}

func (v *auditCleanupPG) originalCanonical(t *testing.T) string {
	t.Helper()
	var snapshot string
	err := v.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_array((SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1),(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_project.commands c WHERE project_id=$1),(SELECT jsonb_agg(to_jsonb(p) ORDER BY participant_name) FROM agenteam_project.lifecycle_participants p WHERE operation_id=$2 AND participant_name NOT IN ('outbox','audit')))::text`, v.a.ID.String(), v.cause.OperationID.String()).Scan(&snapshot)
	outboxCleanupRequire(t, err)
	return snapshot
}

func (v *auditCleanupPG) countAudit(t *testing.T) int {
	t.Helper()
	var count int
	outboxCleanupRequire(t, v.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_audit.audit_records WHERE scope='project' AND project_id=$1`, v.a.ID.String()).Scan(&count))
	return count
}

func (v *auditCleanupPG) state(t *testing.T) (stage, cleanup, claim string, checkpoint *ac.CleanupCheckpoint) {
	t.Helper()
	var operation, lifecycle, process string
	var schema *int64
	var body []byte
	err := v.base.raw.QueryRow(ctxFor(t), `SELECT o.state,o.cleanup_stage,a.cleanup_state,w.phase,w.process_id::text,p.lifecycle,a.checkpoint_schema,a.checkpoint FROM agenteam_project.lifecycle_operations o JOIN agenteam_project.projects p ON p.id=o.project_id AND p.current_lifecycle_operation_id=o.id JOIN agenteam_project.lifecycle_participants a ON a.operation_id=o.id AND a.participant_name='audit' JOIN agenteam_project.work_claims w ON w.work_kind='lifecycle' AND w.work_id=o.id AND w.project_id=p.id WHERE o.id=$1`, v.cause.OperationID.String()).Scan(&operation, &stage, &cleanup, &claim, &process, &lifecycle, &schema, &body)
	outboxCleanupRequire(t, err)
	if operation != "cleaning" || lifecycle != "deleting" || process != v.process.process.String() {
		t.Fatal("cleanup invented lifecycle completion or changed original process")
	}
	if schema != nil {
		if *schema != 1 || !json.Valid(body) || len(body) > 1024 {
			t.Fatal("invalid persisted Audit checkpoint")
		}
		var decoded ac.CleanupCheckpoint
		outboxCleanupRequire(t, json.Unmarshal(body, &decoded))
		if decoded.ProjectID != v.a.ID || decoded.OperationID.String() != v.cause.OperationID.String() {
			t.Fatal("checkpoint changed original Project/operation")
		}
		// The Project checkpoint is opaque to callers. Inspect stored JSON only;
		// never supply or edit this cursor to direct the real delete boundary.
		checkpoint = &decoded
	} else if len(body) != 0 {
		t.Fatal("checkpoint payload lacks its schema")
	}
	return
}

func (v *auditCleanupPG) requireState(t *testing.T, wantStage, wantCleanup, wantClaim string, wantCheckpoint bool) {
	t.Helper()
	stage, cleanup, claim, checkpoint := v.state(t)
	if stage != wantStage || cleanup != wantCleanup || claim != wantClaim || (checkpoint != nil) != wantCheckpoint {
		t.Fatalf("Audit progress differs: %s/%s/%s checkpoint=%t", stage, cleanup, claim, checkpoint != nil)
	}
}

func (v *auditCleanupPG) isolated(t *testing.T) {
	t.Helper()
	if v.snapshot(t, v.b.ID) != v.foreign || v.systemSnapshot(t) != v.system || v.originalCanonical(t) != v.canonical || v.handler.calls.Load() != 0 {
		t.Fatal("Audit cleanup affected B, System, original Project/commands or another domain")
	}
	actual, err := v.runtime.guard.CurrentProcess()
	outboxCleanupRequire(t, err)
	if actual != v.process.process {
		t.Fatal("original guard identity changed")
	}
	if err = syscall.Flock(int(v.claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); !errors.Is(err, syscall.EWOULDBLOCK) {
		if err == nil {
			_ = syscall.Flock(int(v.claim.Fd()), syscall.LOCK_UN)
		}
		t.Fatal("guard released while cleanup graph remains owned")
	}
}

func (v *auditCleanupPG) enterAudit(t *testing.T) {
	t.Helper()
	initial := v.countAudit(t)
	// D06 deletes at most 100 original events per round. Stop may leave its scan
	// at the tail; an empty-tail Pending round resets that cursor before the
	// remaining batches. Continue only known Pending, using this same driver.
	for n := 0; n < v.wantEvents/100+2; n++ {
		call := v.run(t, v.driver)
		outboxCleanupWait(t, call.done, "original Outbox cleanup round")
		outboxCleanupRequire(t, call.err)
		stage, state, claim, checkpoint := v.state(t)
		if state != "required" || claim != "terminal" || checkpoint != nil || v.countAudit(t) != initial {
			t.Fatal("Outbox round prematurely invoked Audit or lost its original return")
		}
		if stage == "audit" {
			v.requireQueue(t, v.a.ID, 0, "dead_letter")
			v.requireReceipt(t, "completed")
			v.isolated(t)
			return
		}
		if stage != "outbox" {
			t.Fatal("unexpected stage during original Outbox rounds", stage)
		}
		var outboxState string
		outboxCleanupRequire(t, v.base.raw.QueryRow(ctxFor(t), `SELECT cleanup_state FROM agenteam_project.lifecycle_participants WHERE operation_id=$1 AND participant_name='outbox'`, v.cause.OperationID.String()).Scan(&outboxState))
		if outboxState != "pending" {
			t.Fatal("original Outbox round did not persist known Pending", outboxState)
		}
	}
	t.Fatal("Outbox did not reach Audit within its known remaining batches")
}

// This is a real ordinary command denial, not a synthetic Object witness or
// proof that the Object convergence append branch itself was exercised by PG.
func (v *auditCleanupPG) requireNewWriteRefused(t *testing.T) {
	t.Helper()
	before := v.countAudit(t)
	description := "must-not-reopen-project"
	version := v.cause.ProjectVersion
	_, err := v.base.projects.UpdateProject(ctxFor(t), v.base.ownerBrowser.actor, meta(t, id[struct{}](t).String(), &version), v.a.ID, pc.UpdateProjectRequest{Description: &description})
	if !outboxCleanupFault(err, f.ProjectNotActive) || v.countAudit(t) != before {
		t.Fatal("Audit/final stage admitted a new ordinary Project write", err)
	}
	v.isolated(t)
}

func testProjectAuditBatches(t *testing.T) {
	v := newAuditCleanupPG(t, true)
	v.enterAudit(t)
	v.requireNewWriteRefused(t)
	barrier := newSchedulerStopBarrier()
	v.barriers = append(v.barriers, barrier)
	var held atomic.Bool
	v.base.tracked.mu.Lock()
	v.base.tracked.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if cause.Details().Owner == "audit-cleanup" && result.State() == f.Committed && held.CompareAndSwap(false, true) {
			barrier.hold(result)
		}
	}
	v.base.tracked.mu.Unlock()
	call := v.run(t, v.driver)
	barrier.await(t)
	if v.countAudit(t) != 1 {
		t.Fatal("first actual Audit batch did not delete exactly 500 of 501")
	}
	v.requireState(t, "audit", "required", "running", false)
	v.isolated(t)
	drainCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	err := v.driver.Drain(drainCtx)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("Drain replaced the original held Audit Store return", err)
	}
	select {
	case <-call.done:
		t.Fatal("Audit physical COMMIT was mistaken for original driver return")
	default:
	}
	barrier.open()
	outboxCleanupWait(t, call.done, "first original Audit batch")
	outboxCleanupRequire(t, call.err)
	v.requireState(t, "audit", "pending", "terminal", true)
	v.base.tracked.mu.Lock()
	v.base.tracked.afterResult = nil
	v.base.tracked.mu.Unlock()
	next := v.run(t, v.driver)
	outboxCleanupWait(t, next.done, "last original Audit row")
	outboxCleanupRequire(t, next.err)
	if v.countAudit(t) != 0 {
		t.Fatal("remaining-first Audit cleanup left the final row")
	}
	v.requireState(t, "final", "completed", "terminal", true)
	v.requireNewWriteRefused(t)
	v.auditTap.mu.Lock()
	calls, returned := v.auditTap.calls, v.auditTap.returned
	v.auditTap.mu.Unlock()
	if calls != 2 || returned != 2 {
		t.Fatal("Audit batches did not retain their original adapter returns")
	}
	v.close(t)
	t.Log("AUDIT_CLEANUP_BATCHES formal_command_rows=501 first_remaining=1 second_remaining=0 original_store_return_held=true final_stage_only=true B_and_system_preserved=true")
}

func testProjectAuditUnknown(t *testing.T) {
	v := newAuditCleanupPG(t, false)
	v.enterAudit(t)
	if v.countAudit(t) != 2 {
		t.Fatal("original creation Audit rows missing")
	}
	report, err := v.adapter.Cleanup(ctxFor(t), v.actor, v.cause, v.scope(), nil)
	if !outboxCleanupFault(err, f.Forbidden) || report.Details().State != "" || v.countAudit(t) != 2 {
		t.Fatal("public cause alone authorized Audit cleanup", err)
	}
	attempts := []f.ID[f.TransactionAttempt]{id[f.TransactionAttempt](t), id[f.TransactionAttempt](t)}
	var masked atomic.Int64
	var originalCause f.TransactionCause
	v.base.tracked.mu.Lock()
	v.base.tracked.transformResult = func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if cause.Details().Owner != "audit-cleanup" || result.State() != f.Committed {
			return result
		}
		n := masked.Add(1)
		if n == 1 {
			originalCause = cause
		} else if !reflect.DeepEqual(cause.Details(), originalCause.Details()) {
			t.Error("Audit confirmation changed the original recovery cause")
		}
		if n <= 2 {
			// Local acknowledgement projection after the real physical COMMIT;
			// these are test acknowledgement IDs, not physical backend attempts.
			return f.UnknownResult(attempts[n-1], cause)
		}
		return result
	}
	v.base.tracked.mu.Unlock()
	call := v.run(t, v.driver)
	outboxCleanupWait(t, call.done, "original Audit Unknown and confirmation")
	if masked.Load() != 2 || !outboxCleanupFault(call.err, f.CommitUnknown) || v.countAudit(t) != 0 {
		t.Fatal("original ambiguous delete/confirmation did not preserve its outcome", call.err)
	}
	v.requireState(t, "audit", "required", "terminal", false)
	v.isolated(t)
	v.base.tracked.mu.Lock()
	v.base.tracked.transformResult = nil
	v.base.tracked.mu.Unlock()
	v.auditTap.mu.Lock()
	saved, actor, calls, returned := v.auditTap.saved, v.auditTap.actor, v.auditTap.calls, v.auditTap.returned
	v.auditTap.mu.Unlock()
	if saved == nil || calls != 1 || returned != 1 {
		t.Fatal("original ambiguous Audit adapter did not actually return")
	}
	report, err = v.adapter.Cleanup(context.WithoutCancel(saved), actor, v.cause, v.scope(), nil)
	if !outboxCleanupFault(err, f.Forbidden) || report.Details().State != "" {
		t.Fatal("expired original scope authorized a new Audit round", err)
	}
	// Explicitly re-enter the original operation after both ambiguous original
	// calls returned. Real remaining-row inspection, not Unknown-as-rollback,
	// permits this round to persist final; it never executes Project finalizer.
	next := v.run(t, v.driver)
	outboxCleanupWait(t, next.done, "explicit same-operation Audit confirmation")
	outboxCleanupRequire(t, next.err)
	v.requireState(t, "final", "completed", "terminal", true)
	v.requireNewWriteRefused(t)
	v.auditTap.mu.Lock()
	calls, returned = v.auditTap.calls, v.auditTap.returned
	v.auditTap.mu.Unlock()
	if calls != 2 || returned != 2 {
		t.Fatal("Audit confirmation did not retain both original adapter returns")
	}
	v.close(t)
	t.Log("AUDIT_CLEANUP_UNKNOWN local_ack_mask=true physical_delete_committed=true original_confirmation_returned=true expired_scope_rejected=true explicit_same_operation=true finalizer_not_run=true")
}
