//go:build integration

package work_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// A PostgreSQL AFTER ROW trigger fails only after the selected real statement
// has changed its row. Its nontransactional sequence proves that boundary was
// reached even though all business rows must roll back.
func installBlockerSQLFailure(t *testing.T, f *blockerFixture, table, operation, field, value, state string) {
	t.Helper()
	if _, err := f.raw.Exec(ctxFor(t), `ALTER SEQUENCE work_fixture.blocker_fault_hits RESTART WITH 1`); err != nil {
		t.Fatal(err)
	}
	// All identifiers below come from the fixed local case table; values are
	// generated IDs, command keys, or fixed state strings, never user input.
	sql := fmt.Sprintf(`CREATE TRIGGER blocker_test_failure AFTER %s ON %s FOR EACH ROW EXECUTE FUNCTION work_fixture.blocker_fail_after_row('%s','%s','%s')`, operation, table, field, value, state)
	if _, err := f.raw.Exec(ctxFor(t), sql); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.raw.Exec(ctxFor(t), "DROP TRIGGER IF EXISTS blocker_test_failure ON "+table); err != nil {
			t.Error(err)
		}
	})
}

type blockerMissingHistoryAppender struct {
	oc.Appender
	fixture *blockerFixture
	seen    bool
}

func (a *blockerMissingHistoryAppender) AppendEventInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, ev event.Event, plan oc.AppendPlan) (oc.AppendReceipt, error) {
	// Prove that the original caller Tx, complete locks, plan, and all original
	// canonical postimages really pass before removing just the history row.
	if err := a.fixture.authority.ValidateAppendInTx(ctx, tx, actor, ev.Summary(), plan.Details().Producer, oc.NewFact); err != nil {
		return oc.AppendReceipt{}, err
	}
	payload, err := a.fixture.blockerEvents.DecodeTaskBlockersChanged(ev)
	if err != nil {
		return oc.AppendReceipt{}, err
	}
	x, err := a.fixture.store.InTx(tx)
	if err != nil {
		return oc.AppendReceipt{}, err
	}
	tag, err := x.Exec(ctx, `DELETE FROM agenteam_work.task_events WHERE id=$1 AND blocker_operation_id=$2`, payload.TaskEventID.String(), payload.OperationID.String())
	if err != nil {
		return oc.AppendReceipt{}, err
	}
	if tag.RowsAffected() != 1 {
		return oc.AppendReceipt{}, errors.New("test did not delete the one new Blocker history")
	}
	a.seen = true
	return a.Appender.AppendEventInTx(ctx, tx, actor, ev, plan)
}

func TestTaskBlockerAtomicity(t *testing.T) {
	f := newBlockerFixture(t)
	a := f.human(t, "blocker-atomicity", "user")
	p, _, _ := f.create(t, a, "blocker-atomicity")
	m := f.milestone(t, a, p.ID, "m")
	s := f.sprint(t, a, p.ID, m.ID, "s")
	target := f.task(t, a, p.ID, s.ID, "target")
	if _, err := f.raw.Exec(ctxFor(t), `
CREATE SEQUENCE work_fixture.blocker_fault_hits;
CREATE FUNCTION work_fixture.blocker_fail_after_row() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF to_jsonb(NEW)->>TG_ARGV[0]=TG_ARGV[1]
    AND (TG_ARGV[2]='any' OR to_jsonb(NEW)->>'state'=TG_ARGV[2]) THEN
   PERFORM nextval('work_fixture.blocker_fault_hits');
   RAISE EXCEPTION USING ERRCODE='P0001', MESSAGE='task-blocker-injected-boundary';
 END IF;
 RETURN NEW;
END $$`); err != nil {
		t.Fatal(err)
	}
	for _, resolve := range []bool{false, true} {
		for _, point := range []struct{ name, table, operation, field string }{
			{"task", "agenteam_work.tasks", "UPDATE", "id"},
			{"blocker", "agenteam_work.task_blockers", "INSERT OR UPDATE", "task_id"},
			{"query", "agenteam_work.task_query_generations", "UPDATE", "project_id"},
			{"history", "agenteam_work.task_events", "INSERT", "task_id"},
			{"outbox", "agenteam_outbox.events", "INSERT", "aggregate_id"},
			{"completed-command", "agenteam_work.task_blocker_commands", "UPDATE", "idempotency_key"},
			{"activity", "agenteam_account.sessions", "UPDATE", "id"},
		} {
			t.Run(fmt.Sprintf("resolve=%t/after-%s", resolve, point.name), func(t *testing.T) {
				current, err := f.taskReader.GetTask(ctxFor(t), a, p.ID, target.ID)
				if err != nil {
					t.Fatal(err)
				}
				request := blockerWaiting(t, "atomic boundary")
				command := wc.TaskBlockerCommandAdd
				if resolve {
					seed, err := f.blockers.AddTaskBlocker(ctxFor(t), a, meta(t, id[struct{}](t).String(), &current.Version), p.ID, target.ID, request)
					if err != nil {
						t.Fatal(err)
					}
					current = seed.Task
					command = wc.TaskBlockerCommandResolve
				}
				cm := meta(t, id[struct{}](t).String(), &current.Version)
				value, state := target.ID.String(), "any"
				switch point.name {
				case "query":
					value = p.ID.String()
				case "completed-command":
					value, state = string(cm.IdempotencyKey), "completed"
				case "activity":
					value = a.Details().SessionID
				}
				f.ageSession(t, a)
				before := f.blockerSnapshot(t, a)
				installBlockerSQLFailure(t, f, point.table, point.operation, point.field, value, state)
				invoke := func() (wc.TaskBlockerMutation, error) {
					if resolve {
						return f.blockers.ResolveTaskBlocker(ctxFor(t), a, cm, p.ID, target.ID, wc.TaskBlockerResolve{BlockerID: request.BlockerID})
					}
					return f.blockers.AddTaskBlocker(ctxFor(t), a, cm, p.ID, target.ID, request)
				}
				got, err := invoke()
				var pg *pgconn.PgError
				var fault *foundation.Fault
				if !errors.As(err, &pg) || pg.Code != "P0001" || pg.Message != "task-blocker-injected-boundary" || !errors.As(err, &fault) || fault.CommitState != foundation.NotCommitted || !reflect.DeepEqual(got, wc.TaskBlockerMutation{}) {
					t.Fatal("real injected write failure did not return a zero uncommitted mutation", err)
				}
				var hits int64
				var called bool
				if err = f.raw.QueryRow(ctxFor(t), `SELECT last_value,is_called FROM work_fixture.blocker_fault_hits`).Scan(&hits, &called); err != nil || !called || hits != 1 {
					t.Fatal("selected SQL boundary was not reached exactly once", hits, called, err)
				}
				if f.blockerSnapshot(t, a) != before {
					t.Fatal("partial Task/Blocker/query/history/Outbox/completed/Activity escaped rollback")
				}
				var storedState string
				if err = f.raw.QueryRow(ctxFor(t), `SELECT state FROM agenteam_work.task_blocker_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, p.ID.String(), string(command), string(cm.IdempotencyKey)).Scan(&storedState); err != nil || storedState != "planned" {
					t.Fatal("real prepared command was not retained", storedState, err)
				}
				if _, err = f.raw.Exec(ctxFor(t), "DROP TRIGGER blocker_test_failure ON "+point.table); err != nil {
					t.Fatal(err)
				}
				recovered, err := invoke()
				if err != nil || recovered.Task.Version != current.Version+1 || recovered.Blocker.ID != request.BlockerID || (recovered.Blocker.ResolvedAt != nil) != resolve {
					t.Fatal("same-key recovery after real rollback", err)
				}
				stable := f.blockerSnapshot(t, a)
				replay, err := invoke()
				if err != nil {
					t.Fatal(err)
				}
				equalBlockerMutation(t, recovered, replay)
				if f.blockerSnapshot(t, a) != stable {
					t.Fatal("recovery replay duplicated facts or Activity")
				}
			})
		}
	}
	for _, resolve := range []bool{false, true} {
		t.Run(fmt.Sprintf("producer-missing-history/resolve=%t", resolve), func(t *testing.T) {
			current, err := f.taskReader.GetTask(ctxFor(t), a, p.ID, target.ID)
			if err != nil {
				t.Fatal(err)
			}
			request := blockerWaiting(t, "missing history")
			if resolve {
				seed, err := f.blockers.AddTaskBlocker(ctxFor(t), a, meta(t, id[struct{}](t).String(), &current.Version), p.ID, target.ID, request)
				if err != nil {
					t.Fatal(err)
				}
				current = seed.Task
			}
			app := &blockerMissingHistoryAppender{Appender: f.blockerOutbox, fixture: f}
			writer := f.newBlockerService(t, app, f.accounts)
			before := f.blockerSnapshot(t, a)
			cm := meta(t, id[struct{}](t).String(), &current.Version)
			var got wc.TaskBlockerMutation
			if resolve {
				got, err = writer.ResolveTaskBlocker(ctxFor(t), a, cm, p.ID, target.ID, wc.TaskBlockerResolve{BlockerID: request.BlockerID})
			} else {
				got, err = writer.AddTaskBlocker(ctxFor(t), a, cm, p.ID, target.ID, request)
			}
			requireCode(t, err, foundation.Forbidden)
			if !app.seen || !reflect.DeepEqual(got, wc.TaskBlockerMutation{}) || f.blockerSnapshot(t, a) != before {
				t.Fatal("missing canonical history was not denied atomically")
			}
		})
	}
}
