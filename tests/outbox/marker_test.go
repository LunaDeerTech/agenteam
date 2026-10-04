//go:build integration

package outbox_test

import (
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

func TestOutboxHandlerWritesMarkerAndDeliveryInOneTransaction(t *testing.T) {
	for _, mode := range []string{"ack", "retry", "reject", "panic"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			h := f.handler("fixture.marker")
			h.mode = mode
			if _, err := f.svc.RegisterHandler(ctxFor(t), h.definition(1)); err != nil {
				t.Fatal(err)
			}
			e := f.event(t, true, 1, "body-canary")
			_, commit := f.append(t, f.svc, f.store, f.actor, e)
			state(t, commit, foundation.Committed)
			delivery := f.delivery(t, e, string(h.name))
			f.claim(t, delivery)
			plan, err := f.svc.PrepareDelivery(ctxFor(t), delivery)
			if err != nil {
				t.Fatal(err)
			}
			commit, result := f.svc.ApplyDelivery(ctxFor(t), plan)
			want := foundation.NotCommitted
			count := int64(0)
			if mode == "ack" {
				want = foundation.Committed
				count = 1
			}
			state(t, commit, want)
			if f.count(t, `SELECT count(*) FROM outbox_fixture.effects`) != count || f.count(t, `SELECT count(*) FROM agenteam_outbox.processed`) != count || f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries WHERE phase='succeeded'`) != count {
				t.Fatal("handler effect/marker/delivery separated")
			}
			if h.calls.Load() != 1 {
				t.Fatal("unexpected handler calls")
			}
			if mode == "ack" {
				if result.Kind() != oc.Acknowledged {
					t.Fatal("successful marker result")
				}
				replay, _ := f.svc.ApplyDelivery(ctxFor(t), plan)
				state(t, replay, foundation.Committed)
				if h.calls.Load() != 1 {
					t.Fatal("completed marker called business twice")
				}
				receipt, found, err := f.svc.ConfirmProcessed(ctxFor(t), plan)
				if err != nil || !found || receipt.DeliveryID != delivery || receipt.AttemptID != plan.Identity().AttemptID {
					t.Fatal("marker identity lookup", err)
				}
				if f.count(t, `SELECT count(*) FROM agenteam_outbox.attempts WHERE joined_at IS NOT NULL`) != 0 {
					t.Fatal("DB success forged goroutine join")
				}
			} else {
				if result.Kind() == oc.Acknowledged || !result.Valid() {
					t.Fatal("rollback decision lost")
				}
				if mode == "panic" && result.Reason() != oc.HandlerPanic {
					t.Fatal("panic not safe reason")
				}
				if f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries WHERE phase='processing'`) != 1 {
					t.Fatal("B01 primitive forged B02 retry checkpoint")
				}
				_, found, err := f.svc.ConfirmProcessed(ctxFor(t), plan)
				if err != nil || found {
					t.Fatal("rollback left marker", err)
				}
			}
		})
	}
}
func TestOutboxPreparedHandlerRechecksClaimFenceAndAuthority(t *testing.T) {
	for _, mode := range []string{"permission", "fence", "delete"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			h := f.handler("fixture.current")
			if _, err := f.svc.RegisterHandler(ctxFor(t), h.definition(1)); err != nil {
				t.Fatal(err)
			}
			e := f.event(t, true, 1, "current")
			_, commit := f.append(t, f.svc, f.store, f.actor, e)
			state(t, commit, foundation.Committed)
			delivery := f.delivery(t, e, string(h.name))
			f.claim(t, delivery)
			plan, err := f.svc.PrepareDelivery(ctxFor(t), delivery)
			if err != nil {
				t.Fatal(err)
			}
			expected := foundation.Forbidden
			switch mode {
			case "permission":
				f.sql(t, `UPDATE outbox_fixture.authority SET visible=false WHERE id=$1`, f.project.String())
			case "delete":
				expected = foundation.ResourceDeleted
				f.sql(t, `INSERT INTO agenteam_outbox.project_lifecycle(project_id,operation_id,action,project_version,phase) VALUES($1,$2,'delete',1,'stopping')`, f.project.String(), id[struct{}](t).String())
			case "fence":
				expected = foundation.ResourceBusy
				// A real later claim is seeded only as an adversarial fixture; the old
				// attempt row is retained and cannot authorize the prepared callback.
				next := id[oc.Attempt](t)
				f.sql(t, `INSERT INTO agenteam_outbox.attempts(id,delivery_id,process_id,fence,redrive_cycle,cycle_number,lifetime_number,deadline) VALUES($1,$2,$3,2,0,2,2,clock_timestamp()+interval '20 seconds')`, next.String(), delivery.String(), f.auth.process.String())
				f.sql(t, `UPDATE agenteam_outbox.deliveries SET current_attempt_id=$2,fence=2,cycle_attempts=2,lifetime_attempts=2 WHERE id=$1`, delivery.String(), next.String())
			}
			commit, _ = f.svc.ApplyDelivery(ctxFor(t), plan)
			state(t, commit, foundation.NotCommitted)
			code(t, commit.Fault(), expected)
			if h.calls.Load() != 0 || f.count(t, `SELECT count(*) FROM outbox_fixture.effects`) != 0 || f.count(t, `SELECT count(*) FROM agenteam_outbox.processed`) != 0 {
				t.Fatal("stale claim/authority reached business")
			}
		})
	}
}
