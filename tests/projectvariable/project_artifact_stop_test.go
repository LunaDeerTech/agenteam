//go:build integration

package projectvariable_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/artifact"
	art "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// Only accepted Delete and unrelated participants remain controlled. Artifact
// and Object Stop use the original live runtime/guard and actual returned calls.
// There is no source/lease or foreign-process-death positive case here. The
// later Artifact cleanup must retire its own technical history; it cannot clear
// Object's ledger, finish Delete, or establish a default LifecycleRegistry.
func TestProjectArtifactStop(t *testing.T) {
	t.Run("migration78-to79-and-original-public-return", func(t *testing.T) { testProjectArtifactStop(t, false) })
	t.Run("original-stop-unknown-readonly-confirmation", func(t *testing.T) { testProjectArtifactStop(t, true) })
}

type artifactStopPG struct {
	base        *artifactCleanupPG
	assembly    *artifactCleanupPGAssembly
	pair        *artifact.ProjectCallOwnership
	other       *artifact.ProjectCallOwnership
	participant pc.ProjectLifecycleParticipant
	driver      *project.LifecycleStopDriver
	step        project.LifecycleLocalStopStep
	actor       i.Actor
	cause       pc.LifecycleCause
	scope       pc.ScopeRef
	downstream  oc.ProjectStopCause
	barriers    []*schedulerStopBarrier
	calls       []*skillsCleanupPGCall
	confirm     atomic.Bool
	readContext context.Context
	receipt     string
	closed      bool
}

func newArtifactStopPG(t *testing.T, unknown bool) *artifactStopPG {
	t.Helper()
	s := &artifactStopPG{}
	db := pgfixture.NewDatabase(t)
	migrate(t, db, migrationPrefix(t, "00078"))
	raw := openStore(t, db.Config(t, nil))
	var original *variableHTTPFixture
	t.Cleanup(func() {
		if original != nil && original.afterOwners != nil && !t.Failed() {
			original.afterOwners()
		}
	})
	original = assembleVariableHTTPFixture(t, db, raw, &hookStore{fixtureStore: raw})
	s.base = newArtifactCleanupPGConfigured(t, original, func(v *artifactCleanupPG, assembly *artifactCleanupPGAssembly) {
		s.base, s.assembly = v, assembly
		t.Cleanup(func() { s.close(t) })
		before, foreign := v.snapshot(t), v.foreignArtifacts(t)
		migrate(t, db, migrationPrefix(t, "00079"))
		migrate(t, db, migrationPrefix(t, "00079"))
		if v.snapshot(t) != before || v.foreignArtifacts(t) != foreign || v.counts(t) != [5]int{2, 2, 1, 2, 1} {
			t.Fatal("migration/repeat changed real inline/upload/download business facts")
		}
		var work, stops int
		outboxCleanupRequire(t, raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_artifact.project_work),(SELECT count(*) FROM agenteam_artifact.project_stops)`).Scan(&work, &stops))
		if work != 0 || stops != 0 {
			t.Fatal("legacy publication unexpectedly supplied an owned call or stop receipt")
		}
		s.pair = s.newPair(t)
		v.service = s.pair.Service()
		var err error
		s.participant, err = project.NewArtifactObjectLifecycleParticipant(v.p2.lifecycle, v.service, v.p2.objects)
		outboxCleanupRequire(t, err)
		s.driver, err = project.NewLifecycleStopDriver(original.tracked, v.p2.lifecycle, captureProviderProcesses{guard: v.guard}, func(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
			return s.step(ctx, actor, cause, scope)
		})
		outboxCleanupRequire(t, err)
		prior := assembly.stop
		if unknown {
			// A genuine completed owned read, rather than an inserted ledger row.
			s.readA(t)
			assembly.stop = func(actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) {
				s.bindCause(t, actor, cause, scope)
				s.unknownStop(t)
				prior(actor, cause, scope)
			}
		} else {
			call, held := s.holdRead(t)
			assembly.stop = func(actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) {
				s.bindCause(t, actor, cause, scope)
				s.stopHeldRead(t, call, held)
				prior(actor, cause, scope)
			}
		}
	})
	return s
}

func (s *artifactStopPG) newPair(t *testing.T) *artifact.ProjectCallOwnership {
	t.Helper()
	v := s.base
	pair, err := artifact.NewProjectCallOwnership(v.p2.base.tracked, s.assembly.owners, v.p2.objects, v.p2.objects, v.p2.objects, nil, nil, s.assembly.audit, v.p2.base.keys, v.guard, v.process, v.p2.lifecycle)
	outboxCleanupRequire(t, err)
	return pair
}
func (s *artifactStopPG) bindCause(t *testing.T, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) {
	t.Helper()
	s.actor, s.cause, s.scope = actor, cause, scope
	op, err := f.ParseID[oc.ProjectStopOperation](cause.OperationID.String())
	outboxCleanupRequire(t, err)
	s.downstream, err = oc.NewProjectStopCause(oc.ProjectStopCauseDetails{ProjectID: scope.ProjectID, OperationID: op, Action: oc.ProjectStopDelete, ProjectVersion: cause.ProjectVersion})
	outboxCleanupRequire(t, err)
}
func (s *artifactStopPG) barrier() *schedulerStopBarrier {
	b := newSchedulerStopBarrier()
	s.barriers = append(s.barriers, b)
	return b
}
func (s *artifactStopPG) start(fn func(context.Context) error) *skillsCleanupPGCall {
	call := &skillsCleanupPGCall{done: make(chan struct{})}
	s.calls = append(s.calls, call)
	go func() {
		defer close(call.done)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		call.err = fn(ctx)
	}()
	return call
}
func (s *artifactStopPG) run(step project.LifecycleLocalStopStep) *skillsCleanupPGCall {
	s.step = step // Previous original Run has returned before the next assignment.
	return s.start(func(ctx context.Context) error { return s.driver.Run(ctx, s.scope.ProjectID, s.cause.OperationID) })
}
func (s *artifactStopPG) await(t *testing.T, call *skillsCleanupPGCall) {
	t.Helper()
	outboxCleanupWait(t, call.done, "original Artifact Stop call")
	if call.err != nil {
		agentRetirementDiagnostic(t, "artifact-stop-original-return", call.err)
		t.Fatal("original Artifact Stop call failed")
	}
}
func (s *artifactStopPG) awaitHeld(t *testing.T, call *skillsCleanupPGCall, b *schedulerStopBarrier) {
	t.Helper()
	select {
	case result := <-b.entered:
		if result.State() != f.Committed {
			t.Fatal("Artifact barrier lacked an actual committed original Store result")
		}
	case <-call.done:
		agentRetirementDiagnostic(t, "artifact-stop-before-barrier", call.err)
		t.Fatal("original Artifact return barrier was not reached")
	case <-time.After(5 * time.Second):
		t.Fatal("original Artifact return barrier was not reached")
	}
}
func (s *artifactStopPG) clearHooks() {
	store := s.base.p2.base.tracked
	store.setAfter(nil)
	store.mu.Lock()
	store.enter = nil
	store.before, store.afterResult, store.transformResult = nil, nil, nil
	store.mu.Unlock()
}

func (s *artifactStopPG) ref(t *testing.T) art.ArtifactRef {
	t.Helper()
	var artifactID, fileID string
	outboxCleanupRequire(t, s.base.p2.base.raw.QueryRow(ctxFor(t), `SELECT id::text,file_id::text FROM agenteam_artifact.artifacts WHERE project_id=$1 AND kind='generated'`, s.base.p2.project.ID.String()).Scan(&artifactID, &fileID))
	a, err := f.ParseID[art.ArtifactEntity](artifactID)
	outboxCleanupRequire(t, err)
	file, err := f.ParseID[art.File](fileID)
	outboxCleanupRequire(t, err)
	return art.ArtifactRef{ProjectID: s.base.p2.project.ID, ArtifactID: a, FileID: file}
}
func (s *artifactStopPG) readA(t *testing.T) {
	t.Helper()
	v := s.base
	out, err := v.service.ReadArtifact(ctxFor(t), v.invocation(t, v.p2.project.ID), s.ref(t), art.PreviewRequest{})
	outboxCleanupRequire(t, err)
	if out.Details().Text != "原始 inline bytes\n" {
		t.Fatal("owned public Artifact read changed its original bytes")
	}
}

type artifactStopReadObservation struct{}

func (s *artifactStopPG) holdRead(t *testing.T) (*skillsCleanupPGCall, *schedulerStopBarrier) {
	t.Helper()
	v, store := s.base, s.base.p2.base.tracked
	var prior int
	outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='artifact.read'`, v.p2.project.ID.String()).Scan(&prior))
	barrier := s.barrier()
	var mu sync.Mutex
	var selected string
	var callers sync.Map
	store.mu.Lock()
	store.enter = func(ctx context.Context, cause f.TransactionCause) {
		if ctx.Value(artifactStopReadObservation{}) == true && cause.Kind() == f.RecoveryCause && cause.Details().Owner == "artifact" {
			callers.Store(cause.Details().RecoveryRunID, ctx)
		}
	}
	store.mu.Unlock()
	store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if ctx.Value(artifactStopReadObservation{}) != true || cause.Kind() != f.RecoveryCause || cause.Details().Owner != "artifact" {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var count int
		if err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='artifact.read'`, v.p2.project.ID.String()).Scan(&count); err != nil {
			return err
		}
		if count == prior+1 {
			caller, ok := callers.LoadAndDelete(cause.Details().RecoveryRunID)
			if !ok {
				return errors.New("original Artifact Audit caller context was not observed")
			}
			mu.Lock()
			selected = cause.Details().RecoveryRunID
			s.readContext = caller.(context.Context)
			mu.Unlock()
		}
		return nil
	})
	store.mu.Lock()
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		mu.Lock()
		match := selected != "" && selected == cause.Details().RecoveryRunID
		mu.Unlock()
		if match && result.State() == f.Committed {
			barrier.hold(result)
		}
	}
	store.mu.Unlock()
	ref, invocation := s.ref(t), v.invocation(t, v.p2.project.ID)
	call := s.start(func(ctx context.Context) error {
		out, err := v.service.ReadArtifact(context.WithValue(ctx, artifactStopReadObservation{}, true), invocation, ref, art.PreviewRequest{})
		if err == nil && out.Details().Text != "原始 inline bytes\n" {
			return errors.New("original committed Artifact read changed its bytes")
		}
		return err
	})
	s.awaitHeld(t, call, barrier)
	if s.readContext == nil || s.readContext.Err() != nil {
		t.Fatal("original Artifact read context was not live before Project Stop")
	}
	return call, barrier
}
func (s *artifactStopPG) requireState(t *testing.T, state string, joined bool) {
	t.Helper()
	v := s.base
	var got, process, instance string
	var total, pending int
	outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT state,owner_process_id::text,owner_instance_id::text FROM agenteam_artifact.project_stops WHERE project_id=$1 AND operation_id=$2`, s.scope.ProjectID.String(), s.cause.OperationID.String()).Scan(&got, &process, &instance))
	outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT count(*),count(*) FILTER(WHERE joined_at IS NULL) FROM agenteam_artifact.project_work WHERE project_id=$1`, s.scope.ProjectID.String()).Scan(&total, &pending))
	if got != state || process != v.process.String() || instance == "" || total != 1 || joined && pending != 0 || !joined && pending != 1 {
		t.Fatal("Artifact stop receipt or original work return differs", got, total, pending)
	}
	if _, err := f.ParseID[struct{}](instance); err != nil {
		t.Fatal("invalid persisted Artifact instance identity")
	}
}
func (s *artifactStopPG) observe(t *testing.T, inspect bool, want pc.StopState) {
	t.Helper()
	var report pc.StopReport
	call := s.run(func(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (err error) {
		if inspect {
			report, err = s.participant.InspectStop(ctx, actor, cause, scope)
		} else {
			report, err = s.participant.RequestStop(ctx, actor, cause, scope)
		}
		return err
	})
	s.await(t, call)
	if !report.Matches(pc.ArtifactObjectParticipant, s.cause, s.scope) || report.Details().State != want {
		t.Fatal("real Artifact/Object Stop report differs", report.Details().State)
	}
}

// Object advances one of its five bounded lanes per original invocation. This
// fixture has no unfinished work/upload/lease in lanes 0..2 and exactly one
// issued download grant in lane 3. Consume those explicit Pending results;
// neither the adapter nor an Artifact stopped receipt completes Object's scan.
func (s *artifactStopPG) completeStop(t *testing.T, inspect bool) {
	t.Helper()
	v := s.base
	var work, uploads, leases int
	outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_object.project_work WHERE project_id=$1 AND joined_at IS NULL),(SELECT count(*) FROM agenteam_object.uploads WHERE project_id=$1 AND disposition='reserved'),(SELECT count(*) FROM agenteam_object.object_leases l JOIN agenteam_object.objects o ON o.id=l.object_id WHERE o.project_id=$1 AND l.state='active')`, s.scope.ProjectID.String()).Scan(&work, &uploads, &leases))
	if work != 0 || uploads != 0 || leases != 0 {
		t.Fatal("original returned Artifact fixture still has Object work before download lane", work, uploads, leases)
	}
	read := func() (state string, lane int, after string, grants int) {
		t.Helper()
		outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT COALESCE((SELECT state FROM agenteam_object.project_stops WHERE project_id=$1 AND operation_id=$2),''),COALESCE((SELECT scan_kind FROM agenteam_object.project_stops WHERE project_id=$1 AND operation_id=$2),0),COALESCE((SELECT scan_after::text FROM agenteam_object.project_stops WHERE project_id=$1 AND operation_id=$2),''),(SELECT count(*) FROM agenteam_download.grants WHERE project_id=$1 AND NOT revoked)`, s.scope.ProjectID.String(), s.cause.OperationID.String()).Scan(&state, &lane, &after, &grants))
		return
	}
	state, first, after, grants := read()
	if (state != "" && state != "stopping") || first < 0 || first > 3 || after != "" || grants != 1 {
		t.Fatal("original Object scan did not retain the issued download grant", state, first, after != "", grants)
	}
	var artifactReceipt string
	for lane := first; lane <= 3; lane++ {
		want := pc.StopPending
		if lane == 3 {
			want = pc.Stopped
		}
		s.observe(t, inspect, want)
		s.requireState(t, "stopped", true)
		currentArtifactReceipt := s.stopReceipt(t)
		if artifactReceipt != "" && currentArtifactReceipt != artifactReceipt {
			t.Fatal("Object scan changed the original completed Artifact receipt")
		}
		artifactReceipt = currentArtifactReceipt
		state, next, after, grants := read()
		t.Logf("PROJECT_ARTIFACT_STOP_SCAN original_lane=%d returned_state=%s next_lane=%d remaining_download_grants=%d", lane, want, next, grants)
		if lane < 3 {
			if state != "stopping" || next != lane+1 || after != "" || grants != 1 {
				t.Fatal("original Pending scan did not advance exactly one empty lane", state, next, after != "", grants)
			}
		} else if state != "stopped" || next != 0 || after != "" || grants != 0 {
			t.Fatal("original download lane did not revoke its grant and confirm Object Stopped", state, next, after != "", grants)
		}
	}
}

func (s *artifactStopPG) stopHeldRead(t *testing.T, read *skillsCleanupPGCall, held *schedulerStopBarrier) {
	t.Helper()
	s.observe(t, false, pc.StopPending)
	s.requireState(t, "stopping", false)
	if s.readContext == nil || !errors.Is(s.readContext.Err(), context.Canceled) {
		t.Fatal("selected original Artifact call was not canceled by Project Stop")
	}
	s.base.otherReadable(t)
	select {
	case <-read.done:
		t.Fatal("cancellation substituted for the original public call return")
	default:
	}
	held.open()
	outboxCleanupWait(t, read.done, "original canceled Artifact read return")
	// This read had already consumed its bytes and committed its final Audit.
	// Cancellation does not undo that result; its original outer return and
	// own joined checkpoint, rather than global Joined, delimit Stop.
	outboxCleanupRequire(t, read.err)
	s.clearHooks()
	s.completeStop(t, true)
	s.requireState(t, "stopped", true)
	s.receipt = s.stopReceipt(t)
	s.base.otherReadable(t)
}

func (s *artifactStopPG) stopReceipt(t *testing.T) string {
	t.Helper()
	var value string
	outboxCleanupRequire(t, s.base.p2.base.raw.QueryRow(ctxFor(t), `SELECT to_jsonb(r)::text FROM agenteam_artifact.project_stops r WHERE project_id=$1 AND operation_id=$2`, s.scope.ProjectID.String(), s.cause.OperationID.String()).Scan(&value))
	return value
}

func (s *artifactStopPG) snapshot(t *testing.T) string {
	t.Helper()
	var value string
	outboxCleanupRequire(t, s.base.p2.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_array((SELECT COALESCE(jsonb_agg(to_jsonb(w) ORDER BY id),'[]') FROM agenteam_artifact.project_work w WHERE project_id=$1),(SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY operation_id),'[]') FROM agenteam_artifact.project_stops r WHERE project_id=$1))::text`, s.scope.ProjectID.String()).Scan(&value))
	return value
}
func (s *artifactStopPG) stopCause(cause f.TransactionCause) bool {
	d := cause.Details()
	return d.Kind == f.RecoveryCause && d.Owner == "artifact-project-stop" && d.RecoveryRunID == s.cause.OperationID.String()
}
func (s *artifactStopPG) unknownStop(t *testing.T) {
	t.Helper()
	v, store := s.base, s.base.p2.base.tracked
	var stopped, masked atomic.Bool
	attempt := id[f.TransactionAttempt](t)
	store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !s.stopCause(cause) {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var confirmed bool
		if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_artifact.project_stops WHERE project_id=$1 AND operation_id=$2 AND state='stopped')`, s.scope.ProjectID.String(), s.cause.OperationID.String()).Scan(&confirmed); err != nil {
			return err
		}
		if confirmed {
			stopped.Store(true)
		}
		return nil
	})
	store.mu.Lock()
	store.transformResult = func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if s.stopCause(cause) && stopped.Load() && result.State() == f.Committed && masked.CompareAndSwap(false, true) {
			s.confirm.Store(true)
			return f.UnknownResult(attempt, cause)
		}
		return result
	}
	store.mu.Unlock()
	var report oc.ObjectStopReport
	call := s.run(func(ctx context.Context, actor i.Actor, _ pc.LifecycleCause, _ pc.ScopeRef) (err error) {
		report, err = v.service.RequestProjectStop(ctx, actor, s.downstream)
		return err
	})
	outboxCleanupWait(t, call.done, "original Artifact stop Unknown")
	var fault *f.Fault
	if !masked.Load() || !errors.As(call.err, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown || fault.CauseID != attempt.String() || !report.Matches(oc.ArtifactStopComponent, s.downstream) || report.Details().State != oc.ProjectStopPending {
		agentRetirementDiagnostic(t, "artifact-stop-unknown", call.err)
		t.Fatal("original Artifact stop Unknown lost its actual cause/attempt")
	}
	s.clearHooks()
	s.requireState(t, "stopped", true)
	before := s.snapshot(t)
	s.other = s.newPair(t)
	call = s.run(func(ctx context.Context, actor i.Actor, _ pc.LifecycleCause, _ pc.ScopeRef) (err error) {
		report, err = s.other.Service().InspectProjectStop(ctx, actor, s.downstream)
		return err
	})
	s.await(t, call)
	if !report.Matches(oc.ArtifactStopComponent, s.downstream) || report.Details().State != oc.ProjectStopPending || s.snapshot(t) != before {
		t.Fatal("another live same-process instance borrowed the original stopped receipt")
	}

	barrier := s.barrier()
	var delta atomic.Int64
	delta.Store(-1)
	var baseline sync.Map
	store.mu.Lock()
	store.before = func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !s.stopCause(cause) {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var count int64
		err = x.QueryRow(ctx, `SELECT COALESCE(sum(n_tup_ins+n_tup_upd+n_tup_del),0)::bigint FROM pg_stat_xact_user_tables WHERE schemaname='agenteam_artifact'`).Scan(&count)
		if err == nil {
			baseline.Store(tx, count)
		}
		return err
	}
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if s.stopCause(cause) && result.State() == f.Committed {
			barrier.hold(result)
		}
	}
	store.mu.Unlock()
	store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !s.stopCause(cause) {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var count int64
		if err = x.QueryRow(ctx, `SELECT COALESCE(sum(n_tup_ins+n_tup_upd+n_tup_del),0)::bigint FROM pg_stat_xact_user_tables WHERE schemaname='agenteam_artifact'`).Scan(&count); err != nil {
			return err
		}
		start, ok := baseline.LoadAndDelete(tx)
		if !ok || count < start.(int64) {
			return errors.New("missing original Artifact stop transaction baseline")
		}
		delta.Store(count - start.(int64))
		return nil
	})
	call = s.run(func(ctx context.Context, actor i.Actor, _ pc.LifecycleCause, _ pc.ScopeRef) (err error) {
		report, err = v.service.InspectProjectStop(ctx, actor, s.downstream)
		return err
	})
	s.awaitHeld(t, call, barrier)
	if delta.Load() != 0 || s.snapshot(t) != before {
		t.Fatal("original Artifact stop confirmation changed durable facts", delta.Load())
	}
	select {
	case <-call.done:
		t.Fatal("held confirmation returned prematurely")
	default:
	}
	barrier.open()
	s.await(t, call)
	if !report.Matches(oc.ArtifactStopComponent, s.downstream) || report.Details().State != oc.ProjectStopPending {
		t.Fatal("confirmation invocation performed a new stop round")
	}
	s.confirm.Store(false)
	s.clearHooks()
	if s.snapshot(t) != before {
		t.Fatal("actual read-only confirmation return changed original receipt")
	}
	s.completeStop(t, false)
	s.requireState(t, "stopped", true)
	s.receipt = s.stopReceipt(t)
	v.otherReadable(t)
}

func testProjectArtifactStop(t *testing.T, unknown bool) {
	s := newArtifactStopPG(t, unknown)
	v := s.base
	otherWork := func() string {
		var value string
		outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT COALESCE(jsonb_agg(to_jsonb(w) ORDER BY id),'[]')::text FROM agenteam_artifact.project_work w WHERE project_id=$1`, v.p2.base.project.ID.String()).Scan(&value))
		return value
	}
	beforeOtherWork := otherWork()
	driver := v.driver(t)
	v.ready(t, driver)
	v.finishArtifact(t, driver)
	var work, stops, receipt int
	outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_artifact.project_work WHERE project_id=$1),(SELECT count(*) FROM agenteam_artifact.project_stops WHERE project_id=$1),(SELECT count(*) FROM agenteam_artifact.project_stops WHERE project_id=$1 AND operation_id=$2 AND action='delete' AND state='stopped' AND project_version=$3 AND owner_process_id=$4)`, v.p2.project.ID.String(), s.cause.OperationID.String(), int64(s.cause.ProjectVersion), v.process.String()).Scan(&work, &stops, &receipt))
	if work != 0 || stops != 1 || receipt != 1 || s.receipt == "" || s.stopReceipt(t) != s.receipt {
		t.Fatal("Artifact cleanup did not retire its legal technical history or preserve exact current Delete receipt", work, stops, receipt)
	}
	if beforeOtherWork == "[]" || otherWork() != beforeOtherWork {
		t.Fatal("Project A cleanup changed Project B's original owned work history")
	}
	var objectReceipt bool
	outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT EXISTS(SELECT 1 FROM agenteam_object.project_stops WHERE project_id=$1 AND operation_id=$2 AND state='stopped')`, v.p2.project.ID.String(), s.cause.OperationID.String()).Scan(&objectReceipt))
	if !objectReceipt {
		t.Fatal("Artifact technical cleanup removed Object's own stop receipt")
	}
	v.otherReadable(t)
	s.close(t)
	if unknown {
		t.Log("PROJECT_ARTIFACT_STOP_UNKNOWN original_commit_local_ackmask=true original_attempt_preserved=true confirmation_same_tx_zero_writes=true original_return_held=true other_instance_pending=true next_explicit_stop=true technical_work_retired_current_delete_receipt_retained=true no_network_or_foreign_death_proof=true")
	} else {
		t.Log("PROJECT_ARTIFACT_STOP_RETURN migration78_to79_repeat=true public_read_original_store_held=true project_stop_pending_until_actual_return=true other_project_live=true actual_object_stop=true technical_work_retired_current_delete_receipt_retained=true no_default_registry_or_full_delete=true")
	}
}

func (s *artifactStopPG) close(t *testing.T) {
	t.Helper()
	if s.closed {
		return
	}
	for _, b := range s.barriers {
		b.open()
	}
	for _, call := range s.calls {
		outboxCleanupWait(t, call.done, "Artifact fixture original return before shutdown")
	}
	s.clearHooks()
	if s.confirm.Load() && s.driver != nil {
		call := s.run(func(ctx context.Context, actor i.Actor, _ pc.LifecycleCause, _ pc.ScopeRef) error {
			_, err := s.pair.Service().InspectProjectStop(ctx, actor, s.downstream)
			return err
		})
		outboxCleanupWait(t, call.done, "original Artifact Unknown confirmation before global Stop")
		if call.err != nil {
			t.Error("unresolved Artifact stop owner remains retained", call.err)
		} else {
			s.confirm.Store(false)
		}
	}
	if s.driver != nil {
		s.driver.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := s.driver.Drain(ctx)
		cancel()
		if err != nil {
			t.Error("original Artifact StopDriver did not drain", err)
			_ = s.driver.Drain(context.Background())
		}
	}
	for _, pair := range []*artifact.ProjectCallOwnership{s.other, s.pair} {
		if pair == nil {
			continue
		}
		pair.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := pair.Drain(ctx)
		cancel()
		if err != nil || !pair.Joined() {
			t.Error("original Artifact pair not returned before Object guard cleanup", err)
			_ = pair.Drain(context.Background())
		}
	}
	s.closed = true
}
