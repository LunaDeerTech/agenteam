//go:build integration

package outbox_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

// This port controls only the test-owned claim's terminal evidence. The app
// process test separately proves exact ProcessGuard death with SIGKILL+Wait.
type releasedProcessProof struct {
	*authority
	target   oc.ProcessID
	released atomic.Bool
}

func (p *releasedProcessProof) ConfirmStopped(ctx context.Context, id oc.ProcessID) error {
	if id == p.target && p.released.Load() {
		return nil
	}
	return p.authority.ConfirmStopped(ctx, id)
}
func TestOutboxLifecycleStopsFactsBeforeFinalErasure(t *testing.T) {
	f := newFixture(t)
	process := &releasedProcessProof{authority: f.auth, target: id[oc.Process](t)}
	svc, _ := lifecycleService(t, f, process)
	deliveryID, version := failedDelivery(t, f, svc, true, "cleanup.failed")
	if _, err := svc.Requeue(ctxFor(t), f.actor, commandMeta(t), deliveryID, version, oc.OperatorRetry); err != nil {
		t.Fatal(err)
	}
	h := f.handler("cleanup.failed")
	r := ownedRuntime(t, svc, []oc.HandlerDefinition{h.definition(1)})
	if err := r.Start(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	waitOutbox(t, func() bool {
		return f.count(t, `SELECT count(*) FROM agenteam_outbox.attempts WHERE joined_at IS NOT NULL`) == 2 && f.count(t, `SELECT count(*) FROM agenteam_outbox.processed`) == 1
	})
	actor, cause := lifecycleCause(t, f, oc.DeleteProject, 2)
	stop, err := svc.RequestStop(ctxFor(t), actor, cause)
	if err != nil || !stop.Stopped {
		t.Fatal("joined callback not stopped", err)
	}
	// Other producers still have a legitimate durable stopping fact. The real
	// fixture provider checks that exact registered actor/cause, not caller bool.
	e := f.event(t, true, 1, "final private stopping fact")
	_, result := f.append(t, svc, f.store, actor, e)
	state(t, result, foundation.Committed)
	_, result = f.append(t, svc, f.store, f.actor, f.event(t, true, 1, "forbidden ordinary mutation"))
	state(t, result, foundation.NotCommitted)
	code(t, result.Fault(), foundation.ProjectNotActive)
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.requeue_commands`) != 1 || f.count(t, `SELECT count(*) FROM agenteam_outbox.processed`) != 1 || f.count(t, `SELECT count(*) FROM agenteam_outbox.attempts`) != 2 {
		t.Fatal("fixture did not retain all deletion participants")
	}
	f.sql(t, `UPDATE outbox_fixture.lifecycle SET others=true WHERE project_id=$1`, f.project.String())
	// An exact known-live foreign claim keeps its Event present while cleaning
	// commits the irreversible gate. It is not a timeout-based death proof.
	d := f.delivery(t, e, string(h.name))
	f.claim(t, d)
	f.sql(t, `UPDATE agenteam_outbox.attempts SET process_id=$2 WHERE delivery_id=$1`, d.String(), process.target.String())
	report, err := svc.Cleanup(ctxFor(t), actor, cause)
	if err != nil || report.Completed {
		t.Fatal("active foreign claim bypassed cleanup", err)
	}
	_, result = f.append(t, svc, f.store, actor, e)
	state(t, result, foundation.NotCommitted)
	code(t, result.Fault(), foundation.ResourceDeleted)
	// The formal fixture proof now permits terminal inspection; the service
	// must still take the original writer lock and must not invent callback time.
	process.released.Store(true)
	for i := 0; i < 4 && !report.Completed; i++ {
		report, err = svc.Cleanup(ctxFor(t), actor, cause)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !report.Completed {
		t.Fatal("cleanup failed to converge after exact join")
	}
	for _, table := range []string{"events", "deliveries", "attempts", "processed", "requeue_commands"} {
		if f.count(t, `SELECT count(*) FROM agenteam_outbox.`+table) != 0 {
			t.Fatal("retained project data", table)
		}
	}
	// The whole runtime still owns its worker and can now drain without any
	// callback being admitted from the erased stopping event.
	r.StopClaims()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = r.Drain(ctx); err != nil {
		t.Fatal(err)
	}
}
