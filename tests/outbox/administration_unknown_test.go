//go:build integration

package outbox_test

import (
	"context"
	"errors"
	"net"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// Arm only the chosen transaction after its real callback/SQL completes. The
// returned Store outcome and original PostgreSQL transaction are never faked.
type administrationCommitStore struct {
	*postgres.Store
	proxy *commitProxy
	phase string
	fired atomic.Bool
}

func (s *administrationCommitStore) WithinTx(ctx context.Context, c foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	return s.Store.WithinTx(ctx, c, func(ctx context.Context, tx foundation.Tx) error {
		if err := fn(ctx, tx); err != nil {
			return err
		}
		match := c.Details().Owner == s.phase
		if s.phase == "requeue" && c.Details().Kind == foundation.CommandsCause {
			x, err := s.InTx(tx)
			if err != nil {
				return err
			}
			if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_outbox.requeue_commands)`).Scan(&match); err != nil {
				return err
			}
		}
		if match && s.phase == "outbox.cleanup-batch" {
			x, err := s.InTx(tx)
			if err != nil {
				return err
			}
			if err = x.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM agenteam_outbox.events)`).Scan(&match); err != nil {
				return err
			}
		}
		if match && s.fired.CompareAndSwap(false, true) {
			s.proxy.armed.Store(true)
		}
		return nil
	})
}
func administrationProxy(t *testing.T, f *fixture, committed bool, phase string) (*fixture, *administrationCommitStore, *commitProxy) {
	t.Helper()
	p := newCommitProxy(t, net.JoinHostPort("127.0.0.1", f.db.Fixture.Port), committed)
	u, _ := url.Parse(f.db.Fixture.URL(f.db.Name))
	u.Host = p.listener.Addr().String()
	store := openStore(t, f.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
	other := *f
	other.store = store
	other.auth = &authority{store: store, issuer: oc.NewPlanIssuer(), process: f.auth.process}
	return &other, &administrationCommitStore{Store: store, proxy: p, phase: phase}, p
}
func TestOutboxRequeueUnknownSerializesOriginalCommandAudit(t *testing.T) {
	for _, committed := range []bool{true, false} {
		name := "commit"
		if !committed {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			original, _ := administration(t, f, f.store, false)
			d, version := failedDelivery(t, f, original, true, "unknown.requeue")
			copy, store, proxy := administrationProxy(t, f, committed, "requeue")
			_, auditor := administration(t, copy, copy.store, false)
			auth := adminAuthority{copy}
			svc, err := outbox.New(store, f.cat, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{"fixture": copy.auth}, Projects: copy.auth, Processes: copy.auth, Sessions: auth, System: auth, Audit: auditor})
			if err != nil {
				t.Fatal(err)
			}
			h := copy.handler("unknown.requeue")
			if _, err = svc.RegisterHandler(ctxFor(t), h.definition(1)); err != nil {
				t.Fatal(err)
			}
			meta := commandMeta(t)
			type result struct {
				receipt oc.RequeueReceipt
				err     error
			}
			done := make(chan result, 1)
			go func() {
				r, e := svc.Requeue(ctxFor(t), f.actor, meta, d, version, oc.OperatorRetry)
				done <- result{r, e}
			}()
			reached(t, proxy.reached)
			conn := f.db.Connect(t)
			waitDB(t, conn, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND cardinality(pg_blocking_pids(pid))>0)`)
			if f.count(t, `SELECT count(*) FROM agenteam_outbox.requeue_commands`) != 0 || f.count(t, `SELECT count(*) FROM agenteam_audit.audit_records`) != 0 {
				t.Fatal("uncommitted command escaped")
			}
			select {
			case <-done:
				t.Fatal("unknown command reported before original writer terminal")
			default:
			}
			close(proxy.release)
			reached(t, proxy.completed)
			var first result
			select {
			case first = <-done:
			case <-ctxFor(t).Done():
				t.Fatal("requeue did not complete")
			}
			if committed {
				if first.err != nil || first.receipt.Cycle != 1 {
					t.Fatal("committed unknown not recognized", first.err)
				}
			} else {
				code(t, first.err, foundation.CommitUnknown)
			}
			again, err := svc.Requeue(ctxFor(t), f.actor, meta, d, version, oc.OperatorRetry)
			if err != nil || again.Cycle != 1 {
				t.Fatal(err)
			}
			if f.count(t, `SELECT count(*) FROM agenteam_outbox.requeue_commands`) != 1 || f.count(t, `SELECT count(*) FROM agenteam_audit.audit_records`) != 1 || f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries WHERE id=$1 AND redrive_cycle=1`, d.String()) != 1 {
				t.Fatal("unknown redrive duplicated facts")
			}
		})
	}
}
func TestOutboxLifecycleFinalCleanupUnknownAndRollback(t *testing.T) {
	for _, committed := range []bool{true, false} {
		name := "commit"
		if !committed {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			copy, store, proxy := administrationProxy(t, f, committed, "outbox.cleanup-batch")
			svc, _ := lifecycleService(t, copy, copy.auth, store)
			h := copy.handler("cleanup.unknown")
			if _, err := svc.RegisterHandler(ctxFor(t), h.definition(1)); err != nil {
				t.Fatal(err)
			}
			e := f.event(t, true, 1, "erase this payload")
			_, result := f.append(t, svc, copy.store, f.actor, e)
			state(t, result, foundation.Committed)
			actor, cause := lifecycleCause(t, f, oc.DeleteProject, 2)
			stop, err := svc.RequestStop(ctxFor(t), actor, cause)
			if err != nil || !stop.Stopped {
				t.Fatal("stop", err)
			}
			f.sql(t, `UPDATE outbox_fixture.lifecycle SET others=true WHERE project_id=$1`, f.project.String())
			type response struct {
				report oc.CleanupReport
				err    error
			}
			done := make(chan response, 1)
			go func() {
				var r oc.CleanupReport
				var e error
				// Wrap an empty tail page if necessary, then fault the real final deletion.
				for i := 0; i < 3; i++ {
					r, e = svc.Cleanup(ctxFor(t), actor, cause)
					if e != nil || r.Completed {
						break
					}
				}
				done <- response{r, e}
			}()
			reached(t, proxy.reached)
			first := <-done
			var fault *foundation.Fault
			if !errors.As(first.err, &fault) || fault.CommitState != foundation.Unknown || first.report.Completed {
				t.Fatalf("cleanup unknown fabricated completion: %+v %v", first.report, first.err)
			}
			// Final Project EX remains owned by the original transaction despite the
			// disconnected client; a new cleanup cannot infer rollback from old rows.
			retried := make(chan response, 1)
			go func() { r, e := svc.Cleanup(ctxFor(t), actor, cause); retried <- response{r, e} }()
			waitDB(t, f.db.Connect(t), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND cardinality(pg_blocking_pids(pid))>0)`)
			select {
			case <-retried:
				t.Fatal("cleanup passed old writer before its terminal")
			default:
			}
			close(proxy.release)
			reached(t, proxy.completed)
			var next response
			select {
			case next = <-retried:
			case <-time.After(5 * time.Second):
				t.Fatal("cleanup retry stalled")
			}
			if next.err != nil || !next.report.Completed {
				t.Fatalf("cleanup recovery failed: %+v %v", next.report, next.err)
			}
			for _, table := range []string{"events", "deliveries", "attempts", "processed", "requeue_commands"} {
				if f.count(t, `SELECT count(*) FROM agenteam_outbox.`+table) != 0 {
					t.Fatal("retained erased data", table)
				}
			}
			if f.count(t, `SELECT count(*) FROM agenteam_outbox.project_lifecycle WHERE phase='completed' AND scan_sequence=0 AND recovery_pass=0`) != 1 {
				t.Fatal("minimal irreversible gate missing")
			}
			_, result = f.append(t, svc, copy.store, f.actor, e)
			state(t, result, foundation.NotCommitted)
			code(t, result.Fault(), foundation.ResourceDeleted)
			replay, err := svc.Cleanup(ctxFor(t), actor, cause)
			if err != nil || !replay.Completed || replay.Removed != 0 {
				t.Fatal("completed replay recreated cleanup", err)
			}
		})
	}
}
