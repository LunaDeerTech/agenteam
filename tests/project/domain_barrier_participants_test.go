//go:build integration

package project_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// The existing fixture uses canonical Account test rows and the real Account,
// Project, Secret and Audit authorities. Skills initialization is a persistent
// test receipt; other registry adapters must never be called. This is neither
// Login/default App nor full participant/ProcessGuard acceptance.
func TestProjectDomainBarrierParticipants(t *testing.T) {
	t.Run("gate-and-confirmed-stop", func(t *testing.T) {
		v := newBindingFixture(t)
		adapters := barrierPGAdapters(t, v.raw, v.registry)
		request := v.request(t, v.owner, sc.Create, "owned-barrier-test-material")
		prepared, err := v.bundle.secret.PrepareWrite(ctxFor(t), request)
		if err != nil {
			t.Fatal(err)
		}

		// Hold an actual Secret mutation inside its actual Audit call, with
		// both original User SH and Project SH already held. Acceptance first
		// waits for User EX in the global lock order, before Project EX.
		entered, release := make(chan int32, 1), make(chan struct{})
		var releaseOnce sync.Once
		writeDone, acceptDone := make(chan struct{}), make(chan struct{})
		var writeErr, acceptErr error
		var accepted c.LifecycleResult
		startedAccept := false
		writeCtx, cancelWrite := context.WithTimeout(context.Background(), 5*time.Second)
		acceptCtx, cancelAccept := context.WithTimeout(context.Background(), 5*time.Second)
		t.Cleanup(func() {
			releaseOnce.Do(func() { close(release) })
			cancelWrite()
			cancelAccept()
			<-writeDone
			if startedAccept {
				<-acceptDone
			}
		})
		v.bundle.tap.before = func(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) (context.Context, ac.Entry, ac.AppendKey) {
			var pid int32
			x, e := v.raw.InTx(tx)
			if e == nil {
				e = v.raw.RequireHeldLocks(ctx, tx, bindingLocks(v.owner, v.project.ID))
			}
			if e == nil {
				e = x.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid)
			}
			if e != nil {
				t.Error("original Secret/Audit transaction or locks missing", e)
				cancelWrite()
			}
			entered <- pid
			select {
			case <-release:
			case <-ctx.Done():
			}
			return ctx, entry, key
		}
		go func() { defer close(writeDone); _, writeErr = v.bundle.secret.ExecuteWrite(writeCtx, request) }()
		var holder int32
		select {
		case holder = <-entered:
		case <-writeCtx.Done():
			t.Fatal("original Secret write did not reach Audit")
		}
		if holder == 0 {
			t.Fatal("original backend identity missing")
		}
		intent := newR2Intent(t, v.project, c.ArchiveCommand, "barrier-archive")
		startedAccept = true
		go func() { defer close(acceptDone); accepted, acceptErr = intent.invoke(acceptCtx, v.service, v.owner) }()
		userLock, _ := f.UserLock(v.owner.Details().UserID)
		r2ObserveWaiter(t, v.r2Fixture, holder, userLock, "ExclusiveLock")
		beforeAccept := v.snapshot(t, v.project.ID, v.owner)
		if beforeAccept.Gate != "active" || beforeAccept.Operations != 0 {
			t.Fatal("acceptance crossed an original held write")
		}
		releaseOnce.Do(func() { close(release) })
		<-writeDone
		<-acceptDone
		v.bundle.tap.before = nil
		if writeErr != nil || acceptErr != nil || accepted.Operation == nil {
			t.Fatal("original write/acceptance failed", writeErr, acceptErr)
		}
		v.unchanged(t, bindingCounts{1, 1, 1})
		op := *accepted.Operation
		actor := barrierPGActor(t, op)
		scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: op.ProjectID}
		cause := r3Cause(op)
		for _, p := range adapters {
			report, e := p.RequestStop(ctxFor(t), actor, cause, scope)
			barrierPGReject(t, report, e, f.InvalidState)
		}
		barrierPGRun(t, v, op, adapters)
		// The driver changed only its phase/claim. Reports are observations;
		// they do not persist a participant or whole-operation completion.
		var state string
		var stopped int
		if e := v.raw.QueryRow(ctxFor(t), `SELECT state,(SELECT count(*) FROM agenteam_project.lifecycle_participants WHERE operation_id=$1 AND stop_state<>'required') FROM agenteam_project.lifecycle_operations WHERE id=$1`, op.ID.String()).Scan(&state, &stopped); e != nil || state != "stopping" || stopped != 0 {
			t.Fatal("report fabricated participant completion", e)
		}
		lookup, e := v.bundle.secret.LookupWrite(ctxFor(t), prepared)
		if e != nil || !lookup.Observed {
			t.Fatal("archive gate lost permitted Owner receipt Read", e)
		}
		if _, e = v.bundle.secret.ExecuteWrite(ctxFor(t), request); e == nil {
			t.Fatal("archive gate admitted ordinary Secret mutation")
		} else {
			requireCode(t, e, f.ProjectNotActive)
		}
		if _, e = v.service.GetProject(ctxFor(t), v.owner, v.project.ID); e != nil {
			t.Fatal("archive gate lost permitted Project Read", e)
		}
		v.unchanged(t, bindingCounts{1, 1, 1})

		// Decorate only the public outcome after a real, confirmed read Tx.
		// This proves result ownership, not a network COMMIT fault.
		attempt := id[f.TransactionAttempt](t)
		var original f.TransactionCause
		unknownStore := &barrierPGStore{Store: v.raw, after: func(_ context.Context, txCause f.TransactionCause, result f.CommitResult) f.CommitResult {
			if result.State() != f.Committed {
				t.Fatal("underlying real observation did not commit", result.State())
			}
			original = txCause
			return f.UnknownResult(attempt, txCause)
		}}
		unknown := barrierPGAdapters(t, unknownStore, v.registry)
		before := barrierPGSnapshot(t, v)
		for _, p := range unknown {
			report, e := p.InspectStop(ctxFor(t), actor, cause, scope)
			barrierPGReject(t, report, e, f.CommitUnknown)
			result, ok := project.UnknownAttempt(e)
			got, want := result.Cause().Details(), original.Details()
			if !ok || result.AttemptID() != attempt || got.Kind != f.RecoveryCause || got.Owner != want.Owner || got.RecoveryRunID != want.RecoveryRunID {
				t.Fatal("original physical outcome identity lost")
			}
		}
		if barrierPGSnapshot(t, v) != before {
			t.Fatal("Unknown observation wrote facts")
		}
		barrierPGHeldReturn(t, v, actor, cause, scope)
	})

	t.Run("current-cause-and-terminal-observation", func(t *testing.T) {
		v := newBindingFixture(t)
		v.credential(t)
		adapters := barrierPGAdapters(t, v.raw, v.registry)
		op := v.gate(t, c.Archiving)
		barrierPGRun(t, v, op, adapters)
		actor := barrierPGActor(t, op)
		cause := r3Cause(op)
		scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: op.ProjectID}
		for _, p := range adapters {
			wrong := cause
			wrong.ProjectVersion++
			report, e := p.InspectStop(ctxFor(t), actor, wrong, scope)
			barrierPGReject(t, report, e, f.Forbidden)
			other := scope
			other.ProjectID = id[identity.Project](t)
			report, e = p.InspectStop(ctxFor(t), actor, cause, other)
			barrierPGReject(t, report, e, f.Forbidden)
		}
		// These are explicit persisted observation fixtures, not worker output.
		// Production accepted->stopping above used the actual driver.
		v.phase(t, op, c.OperationFailed)
		for _, p := range adapters {
			for _, stored := range []string{"required", "pending", "failed", "stopped"} {
				reason := c.SafeReason("")
				if stored == "failed" {
					reason = c.ReasonOperationFailed
				}
				v.sql(t, `UPDATE agenteam_project.lifecycle_participants SET stop_state=$3,safe_reason=NULLIF($4,''),safe_pending_refs='[]'::jsonb WHERE operation_id=$1 AND participant_name=$2`, op.ID.String(), string(p.Name()), stored, string(reason))
				before := barrierPGSnapshot(t, v)
				report, e := p.InspectStop(ctxFor(t), actor, cause, scope)
				want := c.StopPending
				if stored == "failed" {
					want = c.StopFailed
				}
				if stored == "stopped" {
					want = c.Stopped
				}
				barrierPGReport(t, p, report, e, cause, scope, want)
				if report.Details().SafeReason != reason {
					t.Fatal("terminal reason changed")
				}
				denied, e := p.RequestStop(ctxFor(t), actor, cause, scope)
				barrierPGReject(t, denied, e, f.InvalidState)
				if barrierPGSnapshot(t, v) != before {
					t.Fatal("terminal inspection rewrote progress")
				}
			}
		}
		// A malformed *other* participant must fail the complete decoder.
		refs, _ := json.Marshal([]c.PendingRef{{Participant: c.ArtifactObjectParticipant, Kind: "not-declared", ID: id[c.ResourceIdentity](t)}})
		v.sql(t, `UPDATE agenteam_project.lifecycle_participants SET stop_state='pending',safe_pending_refs=$2::jsonb WHERE operation_id=$1 AND participant_name='artifact-object'`, op.ID.String(), refs)
		for _, p := range adapters {
			report, e := p.InspectStop(ctxFor(t), actor, cause, scope)
			barrierPGReject(t, report, e, f.DependencyUnavailable)
		}
		v.sql(t, `UPDATE agenteam_project.lifecycle_participants SET stop_state='required',safe_pending_refs='[]'::jsonb WHERE operation_id=$1 AND participant_name='artifact-object'`, op.ID.String())
		v.sql(t, `UPDATE agenteam_project.lifecycle_operations SET state='stopping',resume_phase=NULL,safe_reason=NULL,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, op.ID.String())
		v.phase(t, op, c.OperationCompleted)
		before := barrierPGSnapshot(t, v)
		for _, p := range adapters {
			report, e := p.InspectStop(ctxFor(t), actor, cause, scope)
			barrierPGReport(t, p, report, e, cause, scope, c.Stopped)
		}
		if barrierPGSnapshot(t, v) != before {
			t.Fatal("completed Inspect changed original facts")
		}
		// Delete uses real acceptance and the original phase driver too. The
		// cleaning row is then an explicit persisted observation fixture;
		// neither this adapter nor this test runs a cleanup worker.
		v.project, _, _ = v.create(t, v.owner, "BarrierDelete")
		deleting := v.gate(t, c.Deleting)
		barrierPGRun(t, v, deleting, adapters)
		v.phase(t, deleting, c.OperationCleaning)
		deleteActor := barrierPGActor(t, deleting)
		deleteCause := r3Cause(deleting)
		deleteScope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: deleting.ProjectID}
		before = barrierPGSnapshot(t, v)
		for _, p := range adapters {
			report, e := p.InspectStop(ctxFor(t), deleteActor, deleteCause, deleteScope)
			barrierPGReport(t, p, report, e, deleteCause, deleteScope, c.Stopped)
			denied, e := p.RequestStop(ctxFor(t), deleteActor, deleteCause, deleteScope)
			barrierPGReject(t, denied, e, f.InvalidState)
		}
		if barrierPGSnapshot(t, v) != before {
			t.Fatal("cleaning Inspect changed original facts")
		}
		// Minimal deletion receipt intentionally has neither original version
		// nor participant progress. It cannot prove either barrier completed.
		deleted, deletedActor, _ := v.deletion(t)
		for _, p := range adapters {
			report, e := p.InspectStop(ctxFor(t), deletedActor, r3Cause(deleted), c.ScopeRef{Kind: c.ProjectScope, ProjectID: deleted.ProjectID})
			barrierPGReject(t, report, e, f.DependencyUnbound)
		}
	})
}

type barrierPGStore struct {
	*postgres.Store
	after func(context.Context, f.TransactionCause, f.CommitResult) f.CommitResult
}

func (s *barrierPGStore) WithinTx(ctx context.Context, cause f.TransactionCause, callback func(context.Context, f.Tx) error) f.CommitResult {
	result := s.Store.WithinTx(ctx, cause, callback)
	return s.after(ctx, cause, result)
}

func barrierPGAdapters(t *testing.T, store project.Store, registry *project.LifecycleRegistry) []c.ProjectLifecycleParticipant {
	t.Helper()
	manifest, e := registry.Manifest()
	if e != nil {
		t.Fatal(e)
	}
	a, e := project.NewLifecycleAuthority(store, manifest, nil)
	if e != nil {
		t.Fatal(e)
	}
	secret, e := project.NewSecretStopParticipant(a)
	if e != nil {
		t.Fatal(e)
	}
	aud, e := project.NewAuditStopParticipant(a)
	if e != nil {
		t.Fatal(e)
	}
	return []c.ProjectLifecycleParticipant{secret, aud}
}
func barrierPGActor(t *testing.T, op c.LifecycleOperation) identity.Actor {
	t.Helper()
	registration, e := identity.RegisterService(identity.ProjectLifecycle)
	if e != nil {
		t.Fatal(e)
	}
	scope, e := identity.InProject(op.ProjectID)
	if e != nil {
		t.Fatal(e)
	}
	actor, e := registration.Actor(op.ID.String(), scope)
	if e != nil {
		t.Fatal(e)
	}
	return actor
}
func barrierPGReport(t *testing.T, p c.ProjectLifecycleParticipant, report c.StopReport, err error, cause c.LifecycleCause, scope c.ScopeRef, want c.StopState) {
	t.Helper()
	if err != nil || !report.Matches(p.Name(), cause, scope) || report.Details().State != want {
		t.Fatal("exact participant report mismatch", p.Name(), err)
	}
}
func barrierPGReject(t *testing.T, report c.StopReport, err error, want f.Code) {
	t.Helper()
	requireCode(t, err, want)
	if report.Details().State != "" {
		t.Fatal("failed observation exposed report")
	}
}
func barrierPGRun(t *testing.T, v *bindingFixture, op c.LifecycleOperation, adapters []c.ProjectLifecycleParticipant) {
	t.Helper()
	driver, e := project.NewLifecycleStopDriver(v.raw, v.facts, v.process, func(ctx context.Context, actor identity.Actor, cause c.LifecycleCause, scope c.ScopeRef) error {
		before := barrierPGSnapshot(t, v)
		for _, p := range adapters {
			report, e := p.RequestStop(ctx, actor, cause, scope)
			barrierPGReport(t, p, report, e, cause, scope, c.Stopped)
			report, e = p.InspectStop(ctx, actor, cause, scope)
			barrierPGReport(t, p, report, e, cause, scope, c.Stopped)
		}
		if barrierPGSnapshot(t, v) != before {
			return errors.New("adapter changed canonical lifecycle or domain facts")
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		driver.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := driver.Drain(ctx); e != nil {
			t.Error("original phase driver did not drain", e)
		}
	})
	if e = driver.Run(ctxFor(t), op.ProjectID, op.ID); e != nil {
		t.Fatal("real accepted phase driver", e)
	}
}
func barrierPGSnapshot(t *testing.T, v *bindingFixture) string {
	t.Helper()
	var value string
	e := v.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'project',(SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1::text::uuid),
 'operations',(SELECT coalesce(jsonb_agg(to_jsonb(o) ORDER BY id),'[]') FROM agenteam_project.lifecycle_operations o WHERE project_id=$1::text::uuid),
 'participants',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY operation_id,participant_name),'[]') FROM agenteam_project.lifecycle_participants p WHERE operation_id IN (SELECT id FROM agenteam_project.lifecycle_operations WHERE project_id=$1::text::uuid)),
 'commands',(SELECT count(*) FROM agenteam_project.commands WHERE project_id=$1::text::uuid),
 'audit',(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1::text::uuid),
 'events',(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::text::uuid),
 'secrets',(SELECT count(*) FROM agenteam_secret.secrets WHERE project_id=$1::text::uuid),
 'activity',(SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$2::text::uuid))::text`, v.project.ID.String(), v.owner.Details().SessionID).Scan(&value)
	if e != nil {
		t.Fatal("canonical observation snapshot", e)
	}
	return value
}
func barrierPGHeldReturn(t *testing.T, v *bindingFixture, actor identity.Actor, cause c.LifecycleCause, scope c.ScopeRef) {
	t.Helper()
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	st := &barrierPGStore{Store: v.raw, after: func(_ context.Context, _ f.TransactionCause, result f.CommitResult) f.CommitResult {
		if result.State() != f.Committed {
			t.Error("held observation did not actually commit", result.State())
		}
		close(entered)
		<-release
		return result
	}}
	p := barrierPGAdapters(t, st, v.registry)[0]
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(func() { cancel(); once.Do(func() { close(release) }); <-returned })
	var report c.StopReport
	var err error
	go func() { defer close(returned); report, err = p.InspectStop(ctx, actor, cause, scope) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("held original observation not reached")
	}
	cancel()
	select {
	case <-returned:
		t.Fatal("cancellation substituted for original WithinTx return")
	case <-time.After(20 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	<-returned
	if err == nil || report.Details().State != "" {
		t.Fatal("cancelled committed observation exposed candidate")
	}
}
