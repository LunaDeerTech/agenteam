//go:build integration

package project_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objectc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Account rows and Skills initialization are the original R3 controlled
// fixtures, consumed by the real authorities. This is not Login/default App or
// a production registry. Public registration and original Project commands
// enqueue actual D06 deliveries, but no dispatcher or callback is started.
func TestProjectOutboxStopParticipant(t *testing.T) {
	t.Run("nonempty-queue-stop-and-inspect", func(t *testing.T) {
		v := newOutboxStopPG(t)
		op, actor, original := v.accept(t, c.Archive)
		scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: op.ProjectID}
		cause := r3Cause(op)
		v.tap.want, v.tap.actor = original.Details(), actor.Details()
		adapter := outboxStopPGAdapter(t, v.facts, v.tap)
		v.queue(t, op, false)
		before := v.snapshot(t, op)
		report, err := adapter.RequestStop(v.context(t), actor, cause, scope)
		barrierPGReject(t, report, err, f.InvalidState)
		if v.tap.calls != 0 || v.snapshot(t, op) != before {
			t.Fatal("accepted phase reached D06 or changed facts")
		}
		v.run(t, op, func(ctx context.Context, current identity.Actor, got c.LifecycleCause, gotScope c.ScopeRef) error {
			if got != cause || !gotScope.Equal(scope) || current.Details() != actor.Details() {
				return errors.New("original driver changed accepted identity")
			}
			canonical := v.canonical(t, op, ctx)
			stopped, err := adapter.RequestStop(ctx, current, got, gotScope)
			if err != nil || !outboxStopPGStopped(stopped, got, gotScope) {
				return errors.New("real D06 nonempty stop did not return exact stopped")
			}
			v.queue(t, op, true, ctx)
			technical := v.technical(t, ctx)
			stopped, err = adapter.InspectStop(ctx, current, got, gotScope)
			if err != nil || !outboxStopPGStopped(stopped, got, gotScope) || v.technical(t, ctx) != technical {
				return errors.New("terminal D06 inspection changed its receipt or queue")
			}
			if v.canonical(t, op, ctx) != canonical {
				return errors.New("adapter changed Project progress, command, Audit, event or activity")
			}
			return nil
		})
		if v.tap.calls != 2 || v.tap.returned != 2 || v.tap.exCommitted != 2 || v.handler.calls.Load() != 0 {
			t.Fatal("delegate/EX/actual-return counts or no-callback boundary differ")
		}
		v.claimTerminal(t, op)
		before = v.snapshot(t, op)
		_, err = adapter.Cleanup(v.context(t), actor, cause, scope, nil)
		requireCode(t, err, f.DependencyUnbound)
		if v.tap.calls != 2 || v.tap.cleanups != 0 || v.snapshot(t, op) != before {
			t.Fatal("unbound cleanup delegated or mutated a domain")
		}
		v.finish(t)
		t.Log("real Project acceptance/stop driver and D06 queued-delivery retirement confirmed; no ordinary delivery, callback, participant completion or cleanup is claimed")
	})

	t.Run("current-facts-unknown-and-terminal", func(t *testing.T) {
		v := newOutboxStopPG(t)
		op, actor, original := v.accept(t, c.Archive)
		scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: op.ProjectID}
		cause := r3Cause(op)
		v.tap.want, v.tap.actor = original.Details(), actor.Details()
		adapter := outboxStopPGAdapter(t, v.facts, v.tap)
		// The real driver alone enters stopping and retires its original claim.
		// Its local step deliberately does not advance any participant.
		v.run(t, op, func(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef) error { return nil })
		v.claimTerminal(t, op)
		v.queue(t, op, false)
		before := v.snapshot(t, op)
		wrong := cause
		wrong.ProjectVersion++
		report, err := adapter.RequestStop(v.context(t), actor, wrong, scope)
		barrierPGReject(t, report, err, f.Forbidden)
		manifest, err := c.NewRequiredManifest(r2Entries(2, false))
		if err != nil {
			t.Fatal("incompatible manifest fixture", err)
		}
		incompatible, err := project.NewLifecycleAuthority(v.raw, manifest, nil)
		if err != nil {
			t.Fatal("incompatible authority fixture", err)
		}
		other := outboxStopPGAdapter(t, incompatible, v.tap)
		report, err = other.RequestStop(v.context(t), actor, cause, scope)
		barrierPGReject(t, report, err, f.DependencyUnbound)
		if v.tap.calls != 0 || v.snapshot(t, op) != before {
			t.Fatal("stale cause/manifest reached D06 or changed committed facts")
		}

		// The original Store physically commits the read-only preflight first.
		// Only its public result is decorated Unknown; this is not a network
		// COMMIT fault, and the adapter must not use the callback's observation.
		attempt := id[f.TransactionAttempt](t)
		var originalCause f.TransactionCause
		committed := 0
		store := &barrierPGStore{Store: v.raw, after: func(_ context.Context, txCause f.TransactionCause, result f.CommitResult) f.CommitResult {
			if result.State() != f.Committed || txCause.Details().Owner != "project.outbox-stop-preflight" {
				t.Fatal("underlying preflight did not actually commit")
			}
			committed++
			originalCause = txCause
			return f.UnknownResult(attempt, txCause)
		}}
		manifest, err = v.registry.Manifest()
		if err != nil {
			t.Fatal(err)
		}
		unknownAuthority, err := project.NewLifecycleAuthority(store, manifest, nil)
		if err != nil {
			t.Fatal(err)
		}
		unknown := outboxStopPGAdapter(t, unknownAuthority, v.tap)
		report, err = unknown.RequestStop(v.context(t), actor, cause, scope)
		barrierPGReject(t, report, err, f.CommitUnknown)
		physical, ok := project.UnknownAttempt(err)
		if !ok || physical.State() != f.Unknown || physical.AttemptID() != attempt || !reflect.DeepEqual(physical.Cause().Details(), originalCause.Details()) || committed != 1 || v.tap.calls != 0 || v.snapshot(t, op) != before {
			t.Fatal("Unknown lost its original attempt/cause or delegated before confirmation")
		}

		// This normal second transaction changes only canonical observation
		// inputs after preflight returned. The real D06 must revalidate them.
		var changed string
		v.tap.before = func(ctx context.Context, tx f.Tx) error {
			x, err := v.raw.InTx(tx)
			if err != nil {
				return err
			}
			_, err = x.Exec(ctx, `UPDATE agenteam_project.lifecycle_operations SET state='failed',resume_phase='stop',safe_reason='operation_failed',version=version+1,updated_at=clock_timestamp() WHERE id=$1`, op.ID.String())
			return err
		}
		v.tap.afterEX = func(ctx context.Context) { changed = v.snapshot(t, op, ctx) }
		report, err = adapter.RequestStop(v.context(t), actor, cause, scope)
		barrierPGReject(t, report, err, f.InvalidState)
		v.tap.before, v.tap.afterEX = nil, nil
		if v.tap.calls != 1 || v.tap.returned != 1 || changed == "" || v.snapshot(t, op) != changed {
			t.Fatal("D06 reused preflight authority or changed facts after current phase rejection")
		}
		v.queue(t, op, false)
		// Restore the explicit observation fixture; this is not RetryLifecycle.
		v.sql(t, `UPDATE agenteam_project.lifecycle_operations SET state='stopping',resume_phase=NULL,safe_reason=NULL,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, op.ID.String())
		report, err = adapter.RequestStop(v.context(t), actor, cause, scope)
		barrierPGReport(t, adapter, report, err, cause, scope, c.Stopped)
		v.queue(t, op, true)
		// The existing R3 completed rows are controlled authority inputs, not
		// a claim that this stop-only adapter completed the other participants.
		v.phase(t, op, c.OperationCompleted)
		before = v.snapshot(t, op)
		report, err = adapter.InspectStop(v.context(t), actor, cause, scope)
		barrierPGReport(t, adapter, report, err, cause, scope, c.Stopped)
		report, err = adapter.RequestStop(v.context(t), actor, cause, scope)
		barrierPGReject(t, report, err, f.InvalidState)
		_, err = adapter.Cleanup(v.context(t), actor, cause, scope, nil)
		requireCode(t, err, f.DependencyUnbound)
		if v.snapshot(t, op) != before || v.tap.calls != 3 || v.tap.returned != 3 || v.tap.exCommitted != 3 || v.tap.cleanups != 0 || v.handler.calls.Load() != 0 {
			t.Fatal("terminal read or rejected cleanup changed facts or invented a callback")
		}
		v.finish(t)
		t.Log("preflight Unknown followed an actual Committed read; stale downstream facts and terminal reads stayed closed, with original guard retirement")
	})
}

type outboxStopPGKey struct{}

type outboxStopPGTap struct {
	oc.ProjectLifecycleParticipant
	store                                          *postgres.Store
	want                                           oc.LifecycleDetails
	actor                                          identity.ActorDetails
	txCause                                        f.TransactionCause
	before                                         func(context.Context, f.Tx) error
	afterEX                                        func(context.Context)
	calls, returned, active, exCommitted, cleanups int
}

func (p *outboxStopPGTap) call(ctx context.Context, actor identity.Actor, cause oc.LifecycleCause, inspect bool) (oc.StopReport, error) {
	p.calls++
	p.active++
	defer func() { p.active--; p.returned++ }()
	deadline, present := ctx.Deadline()
	if cause.Details() != p.want || actor.Details() != p.actor || ctx.Value(outboxStopPGKey{}) != true || !present || time.Until(deadline) > 2*time.Second || ctx.Err() != nil {
		return oc.StopReport{}, errors.New("original adapter context/cause was not preserved")
	}
	// A normal separate transaction obtains EX before the underlying D06 call.
	// Success proves the preflight SH no longer exists; no lock spans delegation.
	result := p.store.WithinTx(ctx, p.txCause, func(ctx context.Context, tx f.Tx) error {
		if err := p.store.AcquireAll(ctx, tx, []f.LockRequest{r3Lock(p.want.ProjectID, f.Exclusive)}); err != nil {
			return err
		}
		if p.before != nil {
			return p.before(ctx, tx)
		}
		return nil
	})
	if result.State() != f.Committed {
		return oc.StopReport{}, errors.New("downstream boundary did not acquire and commit actual Project EX")
	}
	p.exCommitted++
	if p.afterEX != nil {
		p.afterEX(ctx)
	}
	if inspect {
		return p.ProjectLifecycleParticipant.InspectStop(ctx, actor, cause)
	}
	return p.ProjectLifecycleParticipant.RequestStop(ctx, actor, cause)
}
func (p *outboxStopPGTap) RequestStop(ctx context.Context, actor identity.Actor, cause oc.LifecycleCause) (oc.StopReport, error) {
	return p.call(ctx, actor, cause, false)
}
func (p *outboxStopPGTap) InspectStop(ctx context.Context, actor identity.Actor, cause oc.LifecycleCause) (oc.StopReport, error) {
	return p.call(ctx, actor, cause, true)
}
func (p *outboxStopPGTap) Cleanup(context.Context, identity.Actor, oc.LifecycleCause) (oc.CleanupReport, error) {
	p.cleanups++
	return oc.CleanupReport{}, errors.New("stop adapter must not delegate cleanup")
}

// This explicitly bounded provider supplies only a real registry declaration.
// None of its methods is a substitute for an actual business callback.
type outboxStopPGHandler struct{ calls atomic.Int64 }

func (h *outboxStopPGHandler) Prepare(context.Context, event.Event) (oc.HandlerPlan, error) {
	h.calls.Add(1)
	return oc.HandlerPlan{}, errors.New("ordinary Project delivery remains unbound")
}
func (h *outboxStopPGHandler) ValidateInTx(context.Context, f.Tx, event.Event, oc.HandlerPlan) error {
	h.calls.Add(1)
	return errors.New("ordinary Project delivery remains unbound")
}
func (h *outboxStopPGHandler) HandleInTx(context.Context, f.Tx, event.Event, oc.HandlerPlan) oc.Result {
	h.calls.Add(1)
	return oc.Result{}
}

type outboxStopPG struct {
	*r3Fixture
	runtime   *object.Runtime
	claim     *os.File
	processID objectc.ProcessID
	driver    *project.LifecycleStopDriver
	tap       *outboxStopPGTap
	handler   *outboxStopPGHandler
	finished  bool
}

func newOutboxStopPG(t *testing.T) *outboxStopPG {
	t.Helper()
	v := &outboxStopPG{r3Fixture: newR3Fixture(t), processID: id[objectc.Process](t), handler: &outboxStopPGHandler{}}
	storage, _ := projectStorage(t, "")
	spool := filepath.Join(t.TempDir(), "spool")
	runtime, _, guard := v.processRuntime(t, storage, spool, v.processID)
	v.runtime = runtime
	if err := runtime.Initialize(ctxFor(t)); err != nil {
		t.Fatal("original idle Object runtime Initialize", err)
	}
	v.process = projectGuardProcesses{guard: guard, process: v.processID}
	catalog := event.NewCatalog()
	var err error
	v.typed, err = c.RegisterProjectEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	v.events, err = outbox.New(v.raw, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{c.ProjectProducer: v.authority}, Projects: v.authority, Sessions: v.accounts, System: v.accounts, Processes: v.process, Audit: v.aud, Cursors: v.keys})
	if err != nil {
		t.Fatal(err)
	}
	v.service = v.lifecycleService(t, nil)
	definition := oc.HandlerDefinition{ID: "project.fixture-stop", Effect: oc.DomainIngress, Ordering: oc.VersionGuarded, Handler: v.handler, Subscriptions: []oc.Subscription{{EventType: c.CreatedEventName, Versions: []uint32{c.ProjectEventSchemaVersion}}, {EventType: c.LifecycleChangedEventName, Versions: []uint32{c.ProjectEventSchemaVersion}}}}
	registration, err := v.events.RegisterHandler(ctxFor(t), definition)
	if err != nil || registration.HandlerID != definition.ID || len(registration.Subscriptions) != 2 {
		t.Fatal("formal bounded handler registration", err)
	}
	txCause, err := f.NewRecoveryCause("fixture.outbox-stop-unlocked", id[struct{}](t).String(), "")
	if err != nil {
		t.Fatal(err)
	}
	v.tap = &outboxStopPGTap{ProjectLifecycleParticipant: v.events, store: v.raw, txCause: txCause}
	v.claim, err = os.OpenFile(filepath.Join(spool+".processes", v.processID.String()+".claim"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal("original process guard claim unavailable")
	}
	t.Cleanup(func() { _ = v.claim.Close() })
	t.Cleanup(func() { v.finish(t) })
	return v
}

func (v *outboxStopPG) context(t *testing.T) context.Context {
	return context.WithValue(ctxFor(t), outboxStopPGKey{}, true)
}
func outboxStopPGAdapter(t *testing.T, authority *project.LifecycleAuthority, delegate oc.ProjectLifecycleParticipant) c.ProjectLifecycleParticipant {
	t.Helper()
	adapter, err := project.NewOutboxStopParticipant(authority, delegate)
	if err != nil || adapter.Name() != c.OutboxParticipant {
		t.Fatal("formal Outbox adapter construction", err)
	}
	return adapter
}
func outboxStopPGStopped(report c.StopReport, cause c.LifecycleCause, scope c.ScopeRef) bool {
	details := report.Details()
	return report.Matches(c.OutboxParticipant, cause, scope) && details.State == c.Stopped && details.SafeReason == "" && len(details.ActiveRefs) == 0 && len(details.UnknownRefs) == 0
}
func (v *outboxStopPG) run(t *testing.T, op c.LifecycleOperation, step project.LifecycleLocalStopStep) {
	t.Helper()
	var err error
	v.driver, err = project.NewLifecycleStopDriver(v.raw, v.facts, v.process, step)
	if err != nil {
		t.Fatal(err)
	}
	if err = v.driver.Run(v.context(t), op.ProjectID, op.ID); err != nil {
		t.Fatal("original lifecycle stop round", err)
	}
	v.driver.Stop()
	if err = v.driver.Drain(ctxFor(t)); err != nil {
		t.Fatal("original lifecycle driver actual Drain", err)
	}
}
func (v *outboxStopPG) claimTerminal(t *testing.T, op c.LifecycleOperation) {
	t.Helper()
	var operation, claim, process string
	var fence int64
	var advanced int
	err := v.raw.QueryRow(ctxFor(t), `SELECT o.state,w.phase,w.process_id::text,w.fence,
 (SELECT count(*) FROM agenteam_project.lifecycle_participants p WHERE p.operation_id=o.id AND p.stop_state<>'required')
 FROM agenteam_project.lifecycle_operations o JOIN agenteam_project.work_claims w ON w.work_id=o.id AND w.work_kind='lifecycle' WHERE o.id=$1`, op.ID.String()).Scan(&operation, &claim, &process, &fence, &advanced)
	if err != nil || operation != "stopping" || claim != "terminal" || process != v.processID.String() || fence != 1 || advanced != 0 {
		t.Fatal("original driver claim/phase or untouched participant facts differ", err)
	}
}
func (v *outboxStopPG) queue(t *testing.T, op c.LifecycleOperation, stopped bool, parents ...context.Context) {
	t.Helper()
	ctx := outboxStopPGQueryContext(t, parents)
	phase, version, reason := "pending", int64(1), ""
	if stopped {
		phase, version, reason = "dead_letter", 2, "project_stopped"
	}
	var events, deliveries, exact, attempts, processed, receipts int
	err := v.raw.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND event_type IN ('project.created','project.lifecycle_changed')),
 (SELECT count(*) FROM agenteam_outbox.deliveries WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_outbox.deliveries d JOIN agenteam_outbox.events e ON e.id=d.event_id
 WHERE d.project_id=$1 AND e.project_id=d.project_id AND e.event_type IN ('project.created','project.lifecycle_changed') AND d.handler_id='project.fixture-stop'
 AND d.phase=$2 AND d.version=$3 AND coalesce(d.safe_reason,'')=$4 AND d.current_attempt_id IS NULL AND d.fence=0 AND d.cycle_attempts=0 AND d.lifetime_attempts=0),
 (SELECT count(*) FROM agenteam_outbox.attempts a JOIN agenteam_outbox.deliveries d ON d.id=a.delivery_id WHERE d.project_id=$1),
 (SELECT count(*) FROM agenteam_outbox.processed p JOIN agenteam_outbox.deliveries d ON d.id=p.delivery_id WHERE d.project_id=$1),
 (SELECT count(*) FROM agenteam_outbox.project_lifecycle WHERE project_id=$1 AND operation_id=$5 AND action='archive' AND project_version=$6 AND phase='stopped')`, op.ProjectID.String(), phase, version, reason, op.ID.String(), int64(op.ProjectVersion)).Scan(&events, &deliveries, &exact, &attempts, &processed, &receipts)
	wantReceipts := 0
	if stopped {
		wantReceipts = 1
	}
	if err != nil || events != 2 || deliveries != 2 || exact != 2 || attempts != 0 || processed != 0 || receipts != wantReceipts {
		t.Fatal("original nonempty D06 queue or exact technical receipt differs", err)
	}
}
func (v *outboxStopPG) canonical(t *testing.T, op c.LifecycleOperation, parents ...context.Context) string {
	t.Helper()
	ctx := outboxStopPGQueryContext(t, parents)
	var value string
	err := v.raw.QueryRow(ctx, `SELECT jsonb_build_object(
 'project',(SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1::text::uuid),
 'operation',(SELECT to_jsonb(o) FROM agenteam_project.lifecycle_operations o WHERE id=$2::text::uuid),
 'participants',(SELECT jsonb_agg(to_jsonb(p) ORDER BY participant_name) FROM agenteam_project.lifecycle_participants p WHERE operation_id=$2::text::uuid),
 'commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_project.commands c WHERE project_id=$1::text::uuid),
 'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_outbox.events e WHERE project_id=$1::text::uuid),
 'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a WHERE project_id=$1::text::uuid),
 'activity',(SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$3::text::uuid))::text`, op.ProjectID.String(), op.ID.String(), v.owner.Details().SessionID).Scan(&value)
	if err != nil {
		t.Fatal("canonical snapshot unavailable", err)
	}
	return value
}
func (v *outboxStopPG) snapshot(t *testing.T, op c.LifecycleOperation, parents ...context.Context) string {
	ctx := outboxStopPGQueryContext(t, parents)
	return v.canonical(t, op, ctx) + "\n" + v.technical(t, ctx)
}
func outboxStopPGQueryContext(t *testing.T, parents []context.Context) context.Context {
	if len(parents) != 0 {
		return parents[0]
	}
	return ctxFor(t)
}
func (v *outboxStopPG) technical(t *testing.T, ctx context.Context) string {
	t.Helper()
	var value string
	err := v.raw.QueryRow(ctx, `SELECT jsonb_build_array(
 (SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY project_id),'[]') FROM agenteam_outbox.project_lifecycle x),
 (SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_outbox.deliveries x),
 (SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_outbox.attempts x),
 (SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY delivery_id),'[]') FROM agenteam_outbox.processed x))::text`).Scan(&value)
	if err != nil {
		t.Fatal("original technical snapshot unavailable", err)
	}
	return value
}
func (v *outboxStopPG) finish(t *testing.T) {
	t.Helper()
	if v.finished {
		return
	}
	if v.driver != nil {
		v.driver.Stop()
		if err := v.driver.Drain(ctxFor(t)); err != nil {
			t.Fatal("original driver did not retire", err)
		}
	}
	v.service.Stop()
	if err := v.service.Drain(ctxFor(t)); err != nil {
		t.Fatal("original Project service did not retire", err)
	}
	if v.tap.active != 0 || v.tap.calls != v.tap.returned || v.handler.calls.Load() != 0 {
		t.Fatal("original D06 call or unexpected callback is not retired")
	}
	if err := syscall.Flock(int(v.claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); !errors.Is(err, syscall.EWOULDBLOCK) {
		if err == nil {
			_ = syscall.Flock(int(v.claim.Fd()), syscall.LOCK_UN)
		}
		t.Fatal("original shared process guard released before producer join")
	}
	v.runtime.StopAdmission()
	if err := v.runtime.Drain(ctxFor(t)); err != nil {
		t.Fatal("original idle Object runtime did not drain", err)
	}
	if err := syscall.Flock(int(v.claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal("joined original guard remained locked")
	}
	if err := syscall.Flock(int(v.claim.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal("guard observation unlock failed")
	}
	var state string
	if err := v.raw.QueryRow(ctxFor(t), `SELECT state FROM agenteam_object.process_claims WHERE process_id=$1`, v.processID.String()).Scan(&state); err != nil || state != "stopped" {
		t.Fatal("original process stopped fact missing", err)
	}
	v.finished = true
}
