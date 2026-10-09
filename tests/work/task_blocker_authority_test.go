//go:build integration

package work_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func TestTaskBlockerAuthority(t *testing.T) {
	f := newBlockerFixture(t)
	a := f.human(t, "blocker-auth-owner", "user")
	other := f.human(t, "blocker-auth-other", "user")
	admin := f.human(t, "blocker-auth-admin", "admin")
	p, _, _ := f.create(t, a, "blocker-authority")
	m := f.milestone(t, a, p.ID, "m")
	s := f.sprint(t, a, p.ID, m.ID, "s")
	target := f.task(t, a, p.ID, s.ID, "target")
	related := f.task(t, a, p.ID, s.ID, "related")
	request := blockerWaiting(t, "private description remains in authorized data")
	cm := meta(t, "original", &target.Version)
	first, err := f.blockers.AddTaskBlocker(ctxFor(t), a, cm, p.ID, target.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	semantic, err := wc.TaskBlockerAddDigest(a, cm, p.ID, target.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	query := wc.TaskBlockerCommandLookupRequest{ProjectID: p.ID, Command: wc.TaskBlockerCommandAdd, IdempotencyKey: cm.IdempotencyKey, SemanticDigest: semantic}
	t.Run("current-authority-and-identity-priority", func(t *testing.T) {
		for _, actor := range []identity.Actor{other, admin} {
			_, err := f.blockers.AddTaskBlocker(ctxFor(t), actor, cm, p.ID, target.ID, request)
			requireCode(t, err, foundation.NotFound)
			_, err = f.blockers.ListTaskBlockers(ctxFor(t), actor, p.ID, target.ID, wc.TaskBlockersAll)
			requireCode(t, err, foundation.NotFound)
			_, err = f.blockers.LookupTaskBlockerCommand(ctxFor(t), actor, query)
			requireCode(t, err, foundation.NotFound)
		}
		_, err := f.blockers.AddTaskBlocker(ctxFor(t), identity.Actor{}, cm, p.ID, target.ID, request)
		requireCode(t, err, foundation.Unauthenticated)
		agent, err := identity.NewAgentRun(p.ID, id[identity.Agent](t), id[identity.Execution](t))
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.blockers.AddTaskBlocker(ctxFor(t), agent, cm, p.ID, target.ID, request)
		requireCode(t, err, foundation.DependencyUnbound)
		registration, err := identity.RegisterService(identity.ProjectLifecycle)
		if err != nil {
			t.Fatal(err)
		}
		scope, err := identity.InProject(p.ID)
		if err != nil {
			t.Fatal(err)
		}
		service, err := registration.Actor(id[struct{}](t).String(), scope)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.blockers.AddTaskBlocker(ctxFor(t), service, cm, p.ID, target.ID, request)
		requireCode(t, err, foundation.Forbidden)
		changed := request.Clone()
		changed.Description += " changed"
		_, err = f.blockers.AddTaskBlocker(ctxFor(t), a, cm, p.ID, target.ID, changed)
		requireCode(t, err, foundation.IdempotencyKeyReused)
		_, err = f.blockers.AddTaskBlocker(ctxFor(t), a, cm, p.ID, id[wc.Task](t), request)
		requireCode(t, err, foundation.IdempotencyKeyReused)
		fresh := f.renew(t, a)
		replayed, err := f.blockers.AddTaskBlocker(ctxFor(t), fresh, cm, p.ID, target.ID, request)
		if err != nil {
			t.Fatal("same User fresh Session replay", err)
		}
		equalBlockerMutation(t, first, replayed)
		// A different current Owner still cannot learn the original writer's
		// command via a digest error. This is test-owned ownership input.
		f.transferOwner(t, a, other, p.ID)
		defer f.transferOwner(t, other, a, p.ID)
		_, err = f.blockers.AddTaskBlocker(ctxFor(t), other, cm, p.ID, target.ID, changed)
		requireCode(t, err, foundation.NotFound)
	})
	t.Run("foreign-object-and-dependency-presence", func(t *testing.T) {
		p2, _, _ := f.create(t, a, "blocker-foreign")
		m2 := f.milestone(t, a, p2.ID, "m")
		s2 := f.sprint(t, a, p2.ID, m2.ID, "s")
		foreign := f.task(t, a, p2.ID, s2.ID, "foreign")
		before := f.blockerSnapshot(t, a)
		for _, task := range []wc.TaskID{foreign.ID, id[wc.Task](t)} {
			_, err := f.blockers.AddTaskBlocker(ctxFor(t), a, meta(t, id[struct{}](t).String(), &first.Task.Version), p.ID, target.ID, blockerDependency(t, task))
			requireCode(t, err, foundation.NotFound)
		}
		_, err := f.blockers.AddTaskBlocker(ctxFor(t), a, meta(t, "self", &first.Task.Version), p.ID, target.ID, blockerDependency(t, target.ID))
		requireCode(t, err, foundation.InvalidArgument)
		for _, task := range []wc.TaskID{related.ID, foreign.ID} {
			version := related.Version
			_, err = f.blockers.ResolveTaskBlocker(ctxFor(t), a, meta(t, id[struct{}](t).String(), &version), p.ID, task, wc.TaskBlockerResolve{BlockerID: first.Blocker.ID})
			if task == foreign.ID {
				requireCode(t, err, foundation.TaskNotFound)
			} else {
				requireCode(t, err, foundation.BlockerNotFound)
			}
		}
		_, err = f.blockers.ResolveTaskBlocker(ctxFor(t), a, meta(t, "missing-blocker", &first.Task.Version), p.ID, target.ID, wc.TaskBlockerResolve{BlockerID: id[wc.TaskBlockerIdentity](t)})
		requireCode(t, err, foundation.BlockerNotFound)
		_, err = f.blockers.AddTaskBlocker(ctxFor(t), a, meta(t, "occupied", &related.Version), p.ID, related.ID, request)
		requireCode(t, err, foundation.ResourceBusy)
		if f.blockerSnapshot(t, a) != before {
			t.Fatal("denied request partially mutated business facts")
		}
	})
	t.Run("nonbacklog-is-only-a-negative-fixture", func(t *testing.T) {
		for _, state := range []wc.TaskState{wc.TaskStateTodo, wc.TaskStateInProgress, wc.TaskStateInReview, wc.TaskStateBlocked, wc.TaskStateDone, wc.TaskStateCancelled, wc.TaskStateBacklog} {
			agent := id[identity.Agent](t)
			f.seedTaskState(t, a, related, state, &agent)
			_, err := f.blockers.AddTaskBlocker(ctxFor(t), a, meta(t, id[struct{}](t).String(), &related.Version), p.ID, related.ID, blockerWaiting(t, "negative"))
			if state.Terminal() {
				requireCode(t, err, foundation.TaskTerminalImmutable)
			} else {
				requireCode(t, err, foundation.DependencyUnbound)
			}
		}
		f.seedTaskState(t, a, related, wc.TaskStateBacklog, nil)
	})
	t.Run("constructor-and-stored-schema-denials", func(t *testing.T) {
		deps := work.BlockerDependencies{Authority: f.authority, Structure: f.reader, Events: f.blockerOutbox, BlockerEvents: f.blockerEvents, Activity: f.accounts}
		for _, edit := range []func(*work.BlockerDependencies){func(d *work.BlockerDependencies) { d.Authority = nil }, func(d *work.BlockerDependencies) { d.Structure = nil }, func(d *work.BlockerDependencies) { d.Events = nil }, func(d *work.BlockerDependencies) { d.Activity = nil }, func(d *work.BlockerDependencies) { d.BlockerEvents = wc.TaskBlockerEvents{} }} {
			d := deps
			edit(&d)
			_, err := work.NewBlocker(f.store, d)
			requireCode(t, err, foundation.DependencyUnbound)
		}
		var plan []byte
		if err := f.raw.QueryRow(ctxFor(t), `SELECT plan FROM agenteam_work.task_blocker_commands WHERE project_id=$1 AND idempotency_key=$2`, p.ID.String(), string(cm.IdempotencyKey)).Scan(&plan); err != nil {
			t.Fatal(err)
		}
		if _, err := f.raw.Exec(ctxFor(t), `UPDATE agenteam_work.task_blocker_commands SET plan=plan||'{"Unknown":true}'::jsonb WHERE project_id=$1 AND idempotency_key=$2`, p.ID.String(), string(cm.IdempotencyKey)); err != nil {
			t.Fatal(err)
		}
		_, err := f.blockers.LookupTaskBlockerCommand(ctxFor(t), a, query)
		requireCode(t, err, foundation.InternalError)
		if _, err := f.raw.Exec(ctxFor(t), `UPDATE agenteam_work.task_blocker_commands SET plan=$3 WHERE project_id=$1 AND idempotency_key=$2`, p.ID.String(), string(cm.IdempotencyKey), plan); err != nil {
			t.Fatal(err)
		}
		looked, err := f.blockers.LookupTaskBlockerCommand(ctxFor(t), a, query)
		if err != nil || looked.Receipt == nil {
			t.Fatal("restored private record", err)
		}
	})
	t.Run("same-transaction-producer-and-rollback", func(t *testing.T) {
		for _, boundary := range []string{"outbox", "activity"} {
			before := f.blockerSnapshot(t, a)
			var writer *work.BlockerService
			if boundary == "outbox" {
				writer = f.newBlockerService(t, &capturingAppender{Appender: f.blockerOutbox, failAppend: true}, f.accounts)
			} else {
				writer = f.newBlockerService(t, f.blockerOutbox, failActivity{f.accounts})
			}
			_, err := writer.AddTaskBlocker(ctxFor(t), a, meta(t, id[struct{}](t).String(), &first.Task.Version), p.ID, target.ID, blockerWaiting(t, "must rollback"))
			requireCode(t, err, foundation.DependencyUnavailable)
			if f.blockerSnapshot(t, a) != before {
				t.Fatal("failed final transaction leaked", boundary)
			}
		}
		capture := &capturingAppender{Appender: f.blockerOutbox}
		writer := f.newBlockerService(t, capture, f.accounts)
		created, err := writer.AddTaskBlocker(ctxFor(t), a, meta(t, "producer", &first.Task.Version), p.ID, target.ID, blockerWaiting(t, "producer"))
		if err != nil {
			t.Fatal(err)
		}
		saved := capture.last(t)
		deps, err := f.authority.DiscoverAppend(ctxFor(t), a, saved.Event.Summary())
		if err != nil {
			t.Fatal("current access discovery for persisted command", err)
		}
		result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, deps.Locks()); err != nil {
				return err
			}
			if err := f.authority.ValidateAppendInTx(ctx, tx, a, saved.Event.Summary(), deps, oc.CurrentAccess); err != nil {
				return err
			}
			err := f.authority.ValidateAppendInTx(ctx, tx, a, saved.Event.Summary(), deps, oc.NewFact)
			requireCode(t, err, foundation.Forbidden)
			return nil
		})
		if result.State() != foundation.Committed {
			t.Fatal("producer historical access proof", result.Fault())
		}
		withoutLocks := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			return f.authority.ValidateAppendInTx(ctx, tx, a, saved.Event.Summary(), deps, oc.CurrentAccess)
		})
		if withoutLocks.State() == foundation.Committed {
			t.Fatal("producer accepted missing locks")
		}
		otherStore := openStore(t, f.db.Config(t, nil))
		foreign := otherStore.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			return f.authority.ValidateAppendInTx(ctx, tx, a, saved.Event.Summary(), deps, oc.CurrentAccess)
		})
		if foreign.State() == foundation.Committed {
			t.Fatal("producer accepted foreign physical Tx")
		}
		var raw []byte
		if err = f.raw.QueryRow(ctxFor(t), `SELECT payload FROM agenteam_work.task_events WHERE id=$1`, created.TaskEventID.String()).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var payload map[string]json.RawMessage
		if err = json.Unmarshal(raw, &payload); err != nil || len(payload) != 2 {
			t.Fatal("safe added payload is not the two-field projection", err)
		}
		first = created
	})
	t.Run("current-session-and-archived-replay", func(t *testing.T) {
		fresh := f.renew(t, a)
		user, _ := foundation.UserLock(a.Details().UserID)
		f.tx(t, []foundation.LockRequest{{Key: user, Mode: foundation.Exclusive}}, func(ctx context.Context, _ foundation.Tx, x postgres.SQLExecutor) error {
			_, err := x.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, a.Details().SessionID)
			return err
		})
		_, err := f.blockers.LookupTaskBlockerCommand(ctxFor(t), a, query)
		requireCode(t, err, foundation.SessionRevoked)
		a = fresh
		taskSeedArchivedProject(t, f.taskFixture, a, p.ID)
		lookup, err := f.blockers.LookupTaskBlockerCommand(ctxFor(t), a, query)
		if err != nil || lookup.Receipt == nil {
			t.Fatal("archived historical lookup", err)
		}
		_, err = f.blockers.ListTaskBlockers(ctxFor(t), a, p.ID, target.ID, wc.TaskBlockersAll)
		if err != nil {
			t.Fatal("archived authorized read", err)
		}
		_, err = f.blockers.AddTaskBlocker(ctxFor(t), a, meta(t, "archive-new", &first.Task.Version), p.ID, target.ID, blockerWaiting(t, "new"))
		requireCode(t, err, foundation.ProjectNotActive)
	})
}
