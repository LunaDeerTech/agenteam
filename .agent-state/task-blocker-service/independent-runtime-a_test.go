//go:build integration

package work_test

// Independent runtime probes use accepted fixture mechanics and real authorities.
// The task_blocker_* author tests are neither imported nor an assertion oracle.
import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type ibEnv struct {
	base    *taskFixture
	events  *outbox.Service
	factory wc.TaskBlockerEvents
	service *work.BlockerService
}

func ibAttach(t *testing.T, base *taskFixture) *ibEnv {
	t.Helper()
	cat := event.NewCatalog()
	typed, err := wc.RegisterTaskBlockerEvents(cat)
	if err != nil {
		t.Fatal(err)
	}
	aud, err := audit.New(base.store, base.keys, audit.Authorizations{Sessions: base.accounts, System: base.accounts, Accounts: base.accounts, Projects: base.projectAuthority})
	if err != nil {
		t.Fatal(err)
	}
	events, err := outbox.New(base.store, cat, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{wc.WorkProducer: base.authority}, Projects: base.projectAuthority, Sessions: base.accounts, System: base.accounts, Processes: fixtureProcess{id[oc.Process](t)}, Audit: aud, Cursors: base.keys})
	if err != nil {
		t.Fatal(err)
	}
	v := &ibEnv{base: base, events: events, factory: typed}
	v.service = v.newService(t, events, base.accounts)
	return v
}
func (e *ibEnv) newService(t *testing.T, events oc.Appender, activity work.ActivityAuthority) *work.BlockerService {
	t.Helper()
	s, err := work.NewBlocker(e.base.store, work.BlockerDependencies{Authority: e.base.authority, Structure: e.base.reader, Events: events, BlockerEvents: e.factory, Activity: activity})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.Drain(ctx); err != nil {
			t.Error("independent Blocker Drain", err)
		}
	})
	return s
}
func ibWait(t *testing.T) wc.TaskBlockerCreate {
	return wc.TaskBlockerCreate{BlockerID: id[wc.TaskBlockerIdentity](t), Type: wc.TaskBlockerWaitingForHuman, Description: "independent human evidence", Metadata: wc.TaskBlockerMetadata{WaitingForHuman: &wc.TaskBlockerWaitingForHumanMetadata{}}}
}
func ibRely(t *testing.T, target wc.TaskID) wc.TaskBlockerCreate {
	r := ibWait(t)
	r.Type = wc.TaskBlockerRelyOn
	r.Metadata = wc.TaskBlockerMetadata{RelyOn: &wc.TaskBlockerRelyOnMetadata{RelatedTaskID: target}}
	return r
}

type ibReply struct {
	value wc.TaskBlockerMutation
	err   error
}

func ibAsync(t *testing.T, fn func(context.Context) (wc.TaskBlockerMutation, error)) <-chan ibReply {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan ibReply, 1)
	done := make(chan struct{})
	go func() { defer close(done); v, err := fn(ctx); out <- ibReply{v, err} }()
	t.Cleanup(func() { cancel(); await(t, done) })
	return out
}
func ibJoin(t *testing.T, ch <-chan ibReply) ibReply {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("independent Blocker call did not join")
	}
	return ibReply{}
}
func ibStage(t *testing.T, stage <-chan struct{}, reply <-chan ibReply) {
	t.Helper()
	select {
	case <-stage:
		return
	case r := <-reply:
		t.Fatalf("command returned before controlled stage: %v", r.err)
	case <-time.After(8 * time.Second):
		t.Fatal("controlled Blocker stage absent")
	}
}
func ibGate(t *testing.T) (chan struct{}, chan struct{}, func()) {
	t.Helper()
	hit, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)
	return hit, release, free
}
func ibPause(ctx context.Context, hit chan struct{}, release <-chan struct{}) error {
	close(hit)
	select {
	case <-release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func ibSnapshot(t *testing.T, base *taskFixture, a i.Actor) string {
	t.Helper()
	var raw string
	err := base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'tasks',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_work.tasks t),
 'blockers',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_blockers t),
 'history',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_events t),
 'events',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_outbox.events t WHERE producer='work'),
 'completed',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_blocker_commands t WHERE state='completed'),
 'groups',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY project_id,sprint_id,state,priority),'[]'::jsonb) FROM agenteam_work.task_order_groups t),
 'query',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY project_id),'[]'::jsonb) FROM agenteam_work.task_query_generations t),
 'activity',(SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1))::text`, a.Details().SessionID).Scan(&raw)
	if err != nil {
		t.Fatal("independent snapshot", err)
	}
	return raw
}
func ibLookup(t *testing.T, a i.Actor, m f.CommandMeta, p wc.ProjectID, target wc.TaskID, r wc.TaskBlockerCreate) wc.TaskBlockerCommandLookupRequest {
	t.Helper()
	dg, err := wc.TaskBlockerAddDigest(a, m, p, target, r)
	if err != nil {
		t.Fatal(err)
	}
	return wc.TaskBlockerCommandLookupRequest{ProjectID: p, Command: wc.TaskBlockerCommandAdd, IdempotencyKey: m.IdempotencyKey, SemanticDigest: dg}
}
func ibEqual(t *testing.T, a, b wc.TaskBlockerMutation) {
	t.Helper()
	if string(jsonBytes(t, a)) != string(jsonBytes(t, b)) {
		t.Fatal("independent receipt changed")
	}
}
func ibFact(t *testing.T, e *ibEnv, a i.Actor, out wc.TaskBlockerMutation) {
	t.Helper()
	if out.Validate() != nil {
		t.Fatal("invalid successful receipt")
	}
	var taskVersion int64
	var when time.Time
	if err := e.base.raw.QueryRow(ctxFor(t), `SELECT version,updated_at FROM agenteam_work.tasks WHERE id=$1`, out.Task.ID.String()).Scan(&taskVersion, &when); err != nil || taskVersion != int64(out.Task.Version) || !when.Equal(out.Task.UpdatedAt.Time()) {
		t.Fatal("canonical Task mismatch", err)
	}
	var histories, events, commands int
	err := e.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_work.task_events WHERE id=$1 AND blocker_operation_id IS NOT NULL AND operation_id IS NULL AND task_version=$4),(SELECT count(*) FROM agenteam_outbox.events WHERE id=$2 AND event_type='work.task_blockers_changed'),(SELECT count(*) FROM agenteam_work.task_blocker_commands WHERE state='completed' AND task_event_id=$1 AND event_id=$2 AND receipt->'blocker'->>'id'=$3)`, out.TaskEventID.String(), out.EventIDs[0].String(), out.Blocker.ID.String(), int64(out.Task.Version)).Scan(&histories, &events, &commands)
	if err != nil || histories != 1 || events != 1 || commands != 1 {
		t.Fatal("canonical completion facts", histories, events, commands, err)
	}
	rows, err := e.service.ListTaskBlockers(ctxFor(t), a, out.Task.ProjectID, out.Task.ID, wc.TaskBlockersAll)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range rows {
		if v.ID == out.Blocker.ID {
			found = true
			if string(jsonBytes(t, v)) != string(jsonBytes(t, out.Blocker)) {
				t.Fatal("canonical Blocker mismatch")
			}
		}
	}
	if !found {
		t.Fatal("canonical Blocker absent")
	}
}

func TestIndependentTaskBlockerRuntimeA(t *testing.T) {
	base := newTaskFixture(t)
	e := ibAttach(t, base)
	a := base.human(t, "ib-a-owner", "user")
	other := base.human(t, "ib-a-other", "admin")
	p, _, _ := base.create(t, a, "ib-a")
	m := base.milestone(t, a, p.ID, "m")
	s := base.sprint(t, a, p.ID, m.ID, "s")
	t.Run("cross_scope_and_writer", func(t *testing.T) {
		target := base.task(t, a, p.ID, s.ID, "scope-target")
		sibling := base.task(t, a, p.ID, s.ID, "scope-sibling")
		foreign, _, _ := base.create(t, a, "ib-a-foreign")
		fm := base.milestone(t, a, foreign.ID, "m")
		fs := base.sprint(t, a, foreign.ID, fm.ID, "s")
		ft := base.task(t, a, foreign.ID, fs.ID, "foreign")
		r := ibWait(t)
		meta := meta(t, "independent-scope", &target.Version)
		out, err := e.service.AddTaskBlocker(ctxFor(t), a, meta, p.ID, target.ID, r)
		if err != nil {
			t.Fatal(err)
		}
		ibFact(t, e, a, out)
		before := ibSnapshot(t, base, a)
		_, err = e.service.AddTaskBlocker(ctxFor(t), other, meta, p.ID, target.ID, r)
		requireCode(t, err, f.NotFound)
		_, err = e.service.ResolveTaskBlocker(ctxFor(t), a, meta, p.ID, sibling.ID, wc.TaskBlockerResolve{BlockerID: r.BlockerID})
		requireCode(t, err, f.BlockerNotFound)
		_, err = e.service.ResolveTaskBlocker(ctxFor(t), a, meta, foreign.ID, ft.ID, wc.TaskBlockerResolve{BlockerID: r.BlockerID})
		requireCode(t, err, f.BlockerNotFound)
		var reasons []string
		for _, related := range []wc.TaskID{ft.ID, id[wc.Task](t)} {
			req := ibRely(t, related)
			fresh := meta
			fresh.IdempotencyKey = f.IdempotencyKey(id[struct{}](t).String())
			fresh.ExpectedVersion = &sibling.Version
			_, err = e.service.AddTaskBlocker(ctxFor(t), a, fresh, p.ID, sibling.ID, req)
			requireCode(t, err, f.NotFound)
			var fault *f.Fault
			if !errors.As(err, &fault) {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(fault.FieldErrors)
			reasons = append(reasons, string(raw))
		}
		if reasons[0] != reasons[1] {
			t.Fatal("foreign target existence leaked")
		}
		if ibSnapshot(t, base, a) != before {
			t.Fatal("denied request changed business facts")
		}
		owned, _, _ := base.create(t, a, "ib-a-transfer")
		om := base.milestone(t, a, owned.ID, "m")
		os := base.sprint(t, a, owned.ID, om.ID, "s")
		ot := base.task(t, a, owned.ID, os.ID, "owner-target")
		req := ibWait(t)
		cmd := meta
		cmd.IdempotencyKey = "independent-old-writer"
		cmd.ExpectedVersion = &ot.Version
		_, err = e.service.AddTaskBlocker(ctxFor(t), a, cmd, owned.ID, ot.ID, req)
		if err != nil {
			t.Fatal(err)
		}
		base.transferOwner(t, a, other, owned.ID)
		q := ibLookup(t, other, cmd, owned.ID, ot.ID, req)
		_, err = e.service.LookupTaskBlockerCommand(ctxFor(t), other, q)
		requireCode(t, err, f.NotFound)
	})
	t.Run("graph_recompute_and_two_prepared_writers", func(t *testing.T) {
		var tasks []wc.Task
		for _, name := range []string{"graph-a", "graph-b", "graph-c", "graph-d", "graph-e"} {
			tasks = append(tasks, base.task(t, a, p.ID, s.ID, name))
		}
		ab, err := e.service.AddTaskBlocker(ctxFor(t), a, meta(t, "graph-ab", &tasks[0].Version), p.ID, tasks[0].ID, ibRely(t, tasks[1].ID))
		if err != nil {
			t.Fatal(err)
		}
		bc, err := e.service.AddTaskBlocker(ctxFor(t), a, meta(t, "graph-bc", &tasks[1].Version), p.ID, tasks[1].ID, ibRely(t, tasks[2].ID))
		if err != nil {
			t.Fatal(err)
		}
		_, err = e.service.AddTaskBlocker(ctxFor(t), a, meta(t, "graph-ca-cycle", &tasks[2].Version), p.ID, tasks[2].ID, ibRely(t, tasks[0].ID))
		requireCode(t, err, f.TaskDependencyCycle)
		resolved, err := e.service.ResolveTaskBlocker(ctxFor(t), a, meta(t, "graph-resolve-bc", &bc.Task.Version), p.ID, tasks[1].ID, wc.TaskBlockerResolve{BlockerID: bc.Blocker.ID})
		if err != nil {
			t.Fatal(err)
		}
		ibFact(t, e, a, resolved)
		ca, err := e.service.AddTaskBlocker(ctxFor(t), a, meta(t, "graph-ca-after-resolve", &tasks[2].Version), p.ID, tasks[2].ID, ibRely(t, tasks[0].ID))
		if err != nil {
			t.Fatal(err)
		}
		ibFact(t, e, a, ca)
		ibFact(t, e, a, ab)
		left, ls := observedTaskFixture(t, base)
		right, rs := observedTaskFixture(t, base)
		le, re := ibAttach(t, left), ibAttach(t, right)
		lh, lr, lf := ibGate(t)
		rh, rr, rf := ibGate(t)
		defer lf()
		defer rf()
		lc := &capturingAppender{Appender: le.events, after: func(ctx context.Context, _ i.Actor, _ event.Event, _ oc.AppendPlan) error {
			return ibPause(ctx, lh, lr)
		}}
		rc := &capturingAppender{Appender: re.events, after: func(ctx context.Context, _ i.Actor, _ event.Event, _ oc.AppendPlan) error {
			return ibPause(ctx, rh, rr)
		}}
		lservice, rservice := le.newService(t, lc, left.accounts), re.newService(t, rc, right.accounts)
		lmeta, rmeta := meta(t, "graph-de-race", &tasks[3].Version), meta(t, "graph-ed-race", &tasks[4].Version)
		lreq, rreq := ibRely(t, tasks[4].ID), ibRely(t, tasks[3].ID)
		lcall := ibAsync(t, func(ctx context.Context) (wc.TaskBlockerMutation, error) {
			return lservice.AddTaskBlocker(ctx, a, lmeta, p.ID, tasks[3].ID, lreq)
		})
		ibStage(t, lh, lcall)
		rcall := ibAsync(t, func(ctx context.Context) (wc.TaskBlockerMutation, error) {
			return rservice.AddTaskBlocker(ctx, a, rmeta, p.ID, tasks[4].ID, rreq)
		})
		ibStage(t, rh, rcall)
		user, _ := f.UserLock(a.Details().UserID)
		unlock, pid := holdLocks(t, base.fixture, []f.LockRequest{{Key: user, Mode: f.Exclusive}})
		defer unlock()
		lo, ro := observeLock(ls, user, nil), observeLock(rs, user, nil)
		lf()
		rf()
		lat, rat := awaitLockAttempt(t, lo), awaitLockAttempt(t, ro)
		waitExactLock(t, base.db, lat, false, pid)
		waitExactLock(t, base.db, rat, false, pid)
		unlock()
		lv, rv := ibJoin(t, lcall), ibJoin(t, rcall)
		if (lv.err == nil) == (rv.err == nil) {
			t.Fatal("opposing prepared edges did not have exactly one winner", lv.err, rv.err)
		}
		winner, loser := lv, rv
		if winner.err != nil {
			winner, loser = rv, lv
		}
		requireCode(t, loser.err, f.TaskDependencyCycle)
		ibFact(t, e, a, winner.value)
	})
	t.Run("same_key_revision_recovery", func(t *testing.T) {
		target := base.task(t, a, p.ID, s.ID, "revision-target")
		req := ibWait(t)
		cmd := meta(t, "same-key-independent", &target.Version)
		hit, release, free := ibGate(t)
		defer free()
		var once sync.Once
		cap := &capturingAppender{Appender: e.events, before: func(ctx context.Context, _ i.Actor, _ event.Event) error {
			first := false
			once.Do(func() { first = true })
			if first {
				return ibPause(ctx, hit, release)
			}
			return nil
		}}
		old := e.newService(t, cap, base.accounts)
		waiter := ibAsync(t, func(ctx context.Context) (wc.TaskBlockerMutation, error) {
			return old.AddTaskBlocker(ctx, a, cmd, p.ID, target.ID, req)
		})
		ibStage(t, hit, waiter)
		winner, err := e.service.AddTaskBlocker(ctxFor(t), a, cmd, p.ID, target.ID, req)
		if err != nil {
			t.Fatal(err)
		}
		free()
		denied := ibJoin(t, waiter)
		requireCode(t, denied.err, f.Forbidden)
		q := ibLookup(t, a, cmd, p.ID, target.ID, req)
		looked, err := e.service.LookupTaskBlockerCommand(ctxFor(t), a, q)
		if err != nil || looked.Status != wc.LookupCommitted || looked.Receipt == nil {
			t.Fatal("superseded revision not recoverable", err)
		}
		ibEqual(t, winner, *looked.Receipt)
		replay, err := old.AddTaskBlocker(ctxFor(t), a, cmd, p.ID, target.ID, req)
		if err != nil {
			t.Fatal(err)
		}
		ibEqual(t, winner, replay)
		ibFact(t, e, a, winner)
		var revision int
		if err := base.raw.QueryRow(ctxFor(t), `SELECT plan_revision FROM agenteam_work.task_blocker_commands WHERE project_id=$1 AND idempotency_key=$2`, p.ID.String(), string(cmd.IdempotencyKey)).Scan(&revision); err != nil || revision != 2 {
			t.Fatal("revision was not exactly replaced once", revision, err)
		}
	})
	t.Run("producer_missing_facts_and_session_revocation", func(t *testing.T) {
		target := base.task(t, a, p.ID, s.ID, "revoke-target")
		caller := base.renew(t, a)
		req := ibWait(t)
		cmd := meta(t, "independent-revoke", &target.Version)
		hit, release, free := ibGate(t)
		defer free()
		cap := &capturingAppender{Appender: e.events, after: func(ctx context.Context, _ i.Actor, _ event.Event, _ oc.AppendPlan) error {
			return ibPause(ctx, hit, release)
		}}
		svc := e.newService(t, cap, base.accounts)
		running := ibAsync(t, func(ctx context.Context) (wc.TaskBlockerMutation, error) {
			return svc.AddTaskBlocker(ctx, caller, cmd, p.ID, target.ID, req)
		})
		ibStage(t, hit, running)
		captured := cap.last(t)
		result := base.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx f.Tx) error {
			if err := base.store.AcquireAll(ctx, tx, captured.Plan.Locks()); err != nil {
				return err
			}
			deps := captured.Plan.Details().Producer
			if err := base.authority.ValidateAppendInTx(ctx, tx, caller, captured.Event.Summary(), deps, oc.CurrentAccess); err != nil {
				return err
			}
			return base.authority.ValidateAppendInTx(ctx, tx, caller, captured.Event.Summary(), deps, oc.NewFact)
		})
		if result.State() != f.NotCommitted {
			t.Fatal("NewFact accepted missing canonical facts")
		}
		requireCode(t, result.Fault(), f.Forbidden)
		key, _ := f.UserLock(caller.Details().UserID)
		base.tx(t, []f.LockRequest{{Key: key, Mode: f.Exclusive}}, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
			if err := base.accounts.RequireCurrentSession(ctx, tx, caller); err != nil {
				return err
			}
			_, err := x.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, caller.Details().SessionID)
			return err
		})
		before := ibSnapshot(t, base, a)
		free()
		denied := ibJoin(t, running)
		requireCode(t, denied.err, f.SessionRevoked)
		if ibSnapshot(t, base, a) != before {
			t.Fatal("revoked final changed facts")
		}
		_, err := svc.LookupTaskBlockerCommand(ctxFor(t), caller, ibLookup(t, caller, cmd, p.ID, target.ID, req))
		requireCode(t, err, f.SessionRevoked)
	})
}
