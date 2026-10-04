//go:build integration

package outbox_test

import (
	"context"
	"strconv"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

func TestOutboxAtomicAppendCurrentAuthorityAndReplay(t *testing.T) {
	f := newFixture(t)
	h1, h2 := f.handler("fixture.one"), f.handler("fixture.two")
	for _, h := range []*handler{h1, h2} {
		reg, err := f.svc.RegisterHandler(ctxFor(t), h.definition(1))
		if err != nil || len(reg.Subscriptions) != 1 || reg.Subscriptions[0].AcceptedAfter != 0 || !reg.Subscriptions[0].New {
			t.Fatal("initial registration", err)
		}
	}
	first, second := f.event(t, true, 1, "first"), f.event(t, true, 2, "second")
	p1, err := f.svc.PrepareAppend(ctxFor(t), f.actor, first)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := f.svc.PrepareAppend(ctxFor(t), f.actor, second)
	if err != nil {
		t.Fatal(err)
	}
	locks := append(p1.Locks(), p2.Locks()...)
	fact := id[struct{}](t)
	for _, rollback := range []bool{true, false} {
		commit := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, locks); err != nil {
				return err
			}
			x, _ := f.store.InTx(tx)
			if _, err := x.Exec(ctx, `INSERT INTO outbox_fixture.facts(id,value,version) VALUES($1,'current',1)`, fact.String()); err != nil {
				return err
			}
			if _, err := f.svc.AppendEventInTx(ctx, tx, f.actor, first, p1); err != nil {
				return err
			}
			if _, err := f.svc.AppendEventInTx(ctx, tx, f.actor, second, p2); err != nil {
				return err
			}
			if n := f.count(t, `SELECT count(*) FROM agenteam_outbox.events`); n != 0 {
				t.Fatal("uncommitted event visible")
			}
			if h1.calls.Load() != 0 || h2.calls.Load() != 0 {
				t.Fatal("append invoked handler")
			}
			if rollback {
				return foundation.NewFault(foundation.InvalidState, foundation.NotStarted)
			}
			return nil
		})
		want := foundation.Committed
		n := int64(2)
		facts := int64(1)
		if rollback {
			want = foundation.NotCommitted
			n = 0
			facts = 0
		}
		state(t, commit, want)
		if f.count(t, `SELECT count(*) FROM agenteam_outbox.events`) != n || f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries`) != 2*n || f.count(t, `SELECT count(*) FROM outbox_fixture.facts`) != facts {
			t.Fatal("event/delivery/fact not atomic")
		}
	}
	receipt, commit := f.append(t, f.svc, f.store, f.actor, first)
	state(t, commit, foundation.Committed)
	user, _ := foundation.ParseID[identity.User](f.actor.Details().UserID)
	actor, _ := identity.NewHuman(user, id[identity.Session](t))
	again, commit := f.append(t, f.svc, f.store, actor, first)
	state(t, commit, foundation.Committed)
	if again != receipt {
		t.Fatal("same actor new session changed replay")
	}
	// An already committed event does not bypass current permission, but an old
	// ordinary active gate is checked only for a genuinely new fact.
	f.sql(t, `UPDATE outbox_fixture.authority SET active=false WHERE id=$1`, f.project.String())
	_, commit = f.append(t, f.svc, f.store, actor, first)
	state(t, commit, foundation.Committed)
	_, commit = f.append(t, f.svc, f.store, actor, f.event(t, true, 1, "new"))
	state(t, commit, foundation.NotCommitted)
	code(t, commit.Fault(), foundation.ProjectNotActive)
	f.sql(t, `UPDATE outbox_fixture.authority SET visible=false WHERE id=$1`, f.project.String())
	_, commit = f.append(t, f.svc, f.store, actor, first)
	state(t, commit, foundation.NotCommitted)
	code(t, commit.Fault(), foundation.Forbidden)
	f.sql(t, `UPDATE outbox_fixture.authority SET visible=true,active=true WHERE id=$1`, f.project.String())
	changed, err := event.NewEvent(f.types[1], first.Header(), payload{"changed"})
	if err != nil {
		t.Fatal(err)
	}
	_, commit = f.append(t, f.svc, f.store, actor, changed)
	state(t, commit, foundation.NotCommitted)
	code(t, commit.Fault(), foundation.IdempotencyKeyReused)
	// Unknown schema acceptance belongs to dispatch, not fan-out filtering.
	d := f.delivery(t, second, string(h1.name))
	f.claim(t, d)
	if _, err = f.svc.PrepareDelivery(ctxFor(t), d); err == nil {
		t.Fatal("unsupported handler schema silently decoded")
	} else {
		code(t, err, foundation.SchemaUnsupported)
	}
	if _, err = f.store.Exec(ctxFor(t), `UPDATE agenteam_outbox.events SET producer='changed' WHERE id=$1`, first.Header().EventID.String()); err == nil {
		t.Fatal("committed event mutated")
	}
}

func TestOutboxRegistrationSerializesLateWriterAndRetainsSequenceBoundary(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[rollback], func(t *testing.T) {
			f := newFixture(t)
			e := f.event(t, false, 1, "before")
			plan, err := f.svc.PrepareAppend(ctxFor(t), f.actor, e)
			if err != nil {
				t.Fatal(err)
			}
			held := make(chan int32, 1)
			release := make(chan struct{})
			writer := make(chan foundation.CommitResult, 1)
			go func() {
				writer <- f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
					if err := f.store.AcquireAll(ctx, tx, plan.Locks()); err != nil {
						return err
					}
					if _, err := f.svc.AppendEventInTx(ctx, tx, f.actor, e, plan); err != nil {
						return err
					}
					x, _ := f.store.InTx(tx)
					var pid int32
					if err := x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
						return err
					}
					held <- pid
					select {
					case <-release:
					case <-ctx.Done():
						return ctx.Err()
					}
					if rollback {
						return foundation.NewFault(foundation.InvalidState, foundation.NotStarted)
					}
					return nil
				})
			}()
			pid := <-held
			h := f.handler("fixture.late")
			registration := make(chan oc.Registration, 1)
			errs := make(chan error, 1)
			go func() { r, e := f.svc.RegisterHandler(ctxFor(t), h.definition(1)); registration <- r; errs <- e }()
			waitDB(t, f.db.Connect(t), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)))`, pid)
			select {
			case err := <-errs:
				t.Fatalf("registration crossed live writer: %v", err)
			default:
			}
			close(release)
			want := foundation.Committed
			if rollback {
				want = foundation.NotCommitted
			}
			state(t, <-writer, want)
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
			reg := <-registration
			if reg.Subscriptions[0].AcceptedAfter != 1 {
				t.Fatal("late commit/rollback watermark")
			}
			if f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries`) != 0 {
				t.Fatal("old event replayed at registration")
			}
			post := f.event(t, false, 1, "after")
			receipt, commit := f.append(t, f.svc, f.store, f.actor, post)
			state(t, commit, foundation.Committed)
			if receipt.Sequence != 2 || f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries`) != 1 {
				t.Fatal("post barrier event lost")
			}
			restarted, err := outbox.New(f.store, f.cat, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{"fixture": f.auth}, Projects: f.auth, Processes: f.auth})
			if err != nil {
				t.Fatal(err)
			}
			again, err := restarted.RegisterHandler(ctxFor(t), h.definition(1, 2))
			if err != nil || again.Subscriptions[0].AcceptedAfter != 1 || again.Subscriptions[0].New {
				t.Fatal("restart/schema extension moved boundary", err)
			}
			if _, err = restarted.RegisterHandler(ctxFor(t), h.definition(1)); err == nil {
				t.Fatal("accepted version removed")
			}
			altered := h.definition(1, 2)
			altered.Ordering = oc.CanonicalReconcile
			if _, err = restarted.RegisterHandler(ctxFor(t), altered); err == nil {
				t.Fatal("ordering silently replaced")
			}
			f.sql(t, `DELETE FROM agenteam_outbox.deliveries`)
			f.sql(t, `DELETE FROM agenteam_outbox.events`)
			another := f.handler("fixture.new")
			last, err := f.svc.RegisterHandler(ctxFor(t), another.definition(1))
			if err != nil || last.Subscriptions[0].AcceptedAfter != 2 {
				t.Fatal("deleted history lowered registration boundary", err)
			}
		})
	}
}

func TestOutboxMissingLocksPoisonAndMappingChangeRollsBack(t *testing.T) {
	f := newFixture(t)
	e := f.event(t, true, 1, "locks")
	plan, err := f.svc.PrepareAppend(ctxFor(t), f.actor, e)
	if err != nil {
		t.Fatal(err)
	}
	for index, omitted := range plan.Locks() {
		t.Run(strconv.Itoa(index), func(t *testing.T) {
			locks := plan.Locks()
			var reduced []foundation.LockRequest
			for _, r := range locks {
				if r.Key.Canonical() != omitted.Key.Canonical() {
					reduced = append(reduced, r)
				}
			}
			if len(reduced) == len(locks) {
				return
			}
			commit := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
				if err := f.store.AcquireAll(ctx, tx, reduced); err != nil {
					return err
				}
				_, _ = f.svc.AppendEventInTx(ctx, tx, f.actor, e, plan)
				return nil
			})
			state(t, commit, foundation.NotCommitted)
		})
	}
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.events`) != 0 {
		t.Fatal("ignored missing locks committed")
	}
	f.sql(t, `UPDATE outbox_fixture.authority SET parent_id=$2 WHERE id=$1`, f.project.String(), id[struct{}](t).String())
	commit := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if err := f.store.AcquireAll(ctx, tx, plan.Locks()); err != nil {
			return err
		}
		_, err := f.svc.AppendEventInTx(ctx, tx, f.actor, e, plan)
		return err
	})
	state(t, commit, foundation.NotCommitted)
	code(t, commit.Fault(), foundation.ResourceBusy)
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.events`) != 0 {
		t.Fatal("stale planner published")
	}
}

func TestOutboxIrreversibleDeleteGatePrecedesCompletedReplay(t *testing.T) {
	f := newFixture(t)
	e := f.event(t, true, 1, "history")
	_, commit := f.append(t, f.svc, f.store, f.actor, e)
	state(t, commit, foundation.Committed)
	for _, phase := range []string{"cleaning", "completed"} {
		f.sql(t, `INSERT INTO agenteam_outbox.project_lifecycle(project_id,operation_id,action,project_version,phase,completed_at) VALUES($1,$2,'delete',1,$3,CASE WHEN $3='completed' THEN clock_timestamp() END) ON CONFLICT(project_id) DO UPDATE SET phase=EXCLUDED.phase,completed_at=EXCLUDED.completed_at`, f.project.String(), id[struct{}](t).String(), phase)
		_, commit = f.append(t, f.svc, f.store, f.actor, e)
		state(t, commit, foundation.NotCommitted)
		code(t, commit.Fault(), foundation.ResourceDeleted)
	}
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.events`) != 1 {
		t.Fatal("gate test must retain old event")
	}
	f.sql(t, `DELETE FROM agenteam_outbox.project_lifecycle`)
	// Old archive is a checkpoint, not an irreversible deletion gate.
	f.sql(t, `INSERT INTO agenteam_outbox.project_lifecycle(project_id,operation_id,action,project_version,phase) VALUES($1,$2,'archive',1,'stopped')`, f.project.String(), id[struct{}](t).String())
	_, commit = f.append(t, f.svc, f.store, f.actor, f.event(t, true, 1, "restored"))
	state(t, commit, foundation.Committed)
}
