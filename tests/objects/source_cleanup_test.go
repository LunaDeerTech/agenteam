//go:build integration

package objects_test

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
)

func TestObjectSourceLeaseFailedReleaseRemainsObservableAndRetryable(t *testing.T) {
	f := newFixture(t, false)
	stored := f.put(t, "source-release", "payload")
	provider := newSourceAuthority(t, f, f.service, stored)
	reads, err := object.NewSourceReads(f.service, provider)
	if err != nil {
		t.Fatal(err)
	}
	lease, result := acquireSource(t, f, f.service, reads, provider.source, nil)
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	f.sql(t, `CREATE FUNCTION object_fixture.reject_source_release() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='released' AND OLD.owner_kind='source' THEN RAISE EXCEPTION 'owned source release barrier'; END IF; RETURN NEW; END $$;CREATE TRIGGER reject_source_release BEFORE UPDATE ON agenteam_object.object_leases FOR EACH ROW EXECUTE FUNCTION object_fixture.reject_source_release()`)
	for range 2 {
		if err = reads.CancelSourceLease(contextFor(t), lease); err == nil {
			t.Fatal("failed release falsely completed")
		}
	}
	var active bool
	if err = f.store.QueryRow(contextFor(t), `SELECT state='active' FROM agenteam_object.object_leases WHERE id=$1`, lease.ID().String()).Scan(&active); err != nil || !active {
		t.Fatal("failed lease checkpoint missing", err)
	}
	f.sql(t, `DROP TRIGGER reject_source_release ON agenteam_object.object_leases`)
	if err = reads.CancelSourceLease(contextFor(t), lease); err != nil {
		t.Fatal(err)
	}
	if err = f.store.QueryRow(contextFor(t), `SELECT state='active' FROM agenteam_object.object_leases WHERE id=$1`, lease.ID().String()).Scan(&active); err != nil || active {
		t.Fatal("released lease not durably confirmed", err)
	}
	lease, result = acquireSource(t, f, f.service, reads, provider.source, nil)
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	ctx, cancel := context.WithCancel(contextFor(t))
	cancel()
	if err = reads.CancelSourceLease(ctx, lease); err == nil {
		t.Fatal("cancelled cleanup pretended success")
	}
	if _, err = reads.OpenLeasedSource(contextFor(t), f.actor, lease); err == nil {
		t.Fatal("cancelled cleanup reopened handle")
	}
	if err = reads.CancelSourceLease(contextFor(t), lease); err != nil {
		t.Fatal(err)
	}
}

func TestObjectSourceLeaseUnopenedCancellationSharesShutdown(t *testing.T) {
	for _, mode := range []string{"drain", "force", "late_cancel"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, false)
			stored := f.put(t, "unopened-shutdown", "payload")
			proxy := newStorageProxy(t, f)
			service, _ := f.on(t, f.store, proxy.server.URL, nil)
			provider := newSourceAuthority(t, f, service, stored)
			reads, err := object.NewSourceReads(service, provider)
			if err != nil {
				t.Fatal(err)
			}
			lease, result := acquireSource(t, f, service, reads, provider.source, nil)
			if result.State() != foundation.Committed {
				t.Fatal(result.Fault())
			}
			service.StopAdmission()
			if mode == "late_cancel" {
				ctx, cancel := context.WithTimeout(contextFor(t), time.Second)
				defer cancel()
				if err = service.Drain(ctx); err != nil {
					t.Fatal(err)
				}
				requireCode(t, reads.CancelSourceLease(ctx, lease), foundation.ShuttingDown)
				var active bool
				if err = f.store.QueryRow(ctx, `SELECT state='active' FROM agenteam_object.object_leases WHERE id=$1`, lease.ID().String()).Scan(&active); err != nil || !active {
					t.Fatal("late cleanup erased durable recovery checkpoint", err)
				}
				if proxy.gets.Load() != 0 {
					t.Fatal("late cancel opened storage")
				}
				return
			}
			key, _ := foundation.AggregateLock(foundation.ObjectAggregate, stored.Meta.ID.String())
			held, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unlock := func() { once.Do(func() { close(release) }) }
			defer unlock()
			holderDone := make(chan foundation.CommitResult, 1)
			holderCtx, cancelHolder := context.WithTimeout(contextFor(t), 4*time.Second)
			defer cancelHolder()
			holderCause := cause(t)
			go func() {
				holderDone <- f.store.WithinTx(holderCtx, holderCause, func(ctx context.Context, tx foundation.Tx) error {
					if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}); err != nil {
						return err
					}
					close(held)
					select {
					case <-release:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				})
			}()
			select {
			case <-held:
			case <-holderCtx.Done():
				t.Fatal("exact Object holder did not start")
			}
			cancelDone := make(chan error, 1)
			go func() { cancelDone <- reads.CancelSourceLease(contextFor(t), lease) }()
			waitAdvisory(t, f, key)
			if mode == "drain" {
				ctx, cancel := context.WithTimeout(contextFor(t), 150*time.Millisecond)
				err = service.Drain(ctx)
				cancel()
				if err == nil {
					t.Fatal("Drain skipped unopened source cleanup transaction")
				}
				select {
				case <-cancelDone:
					t.Fatal("Cancel joined through held real Object lock")
				default:
				}
				var active bool
				if err = f.store.QueryRow(contextFor(t), `SELECT state='active' FROM agenteam_object.object_leases WHERE id=$1`, lease.ID().String()).Scan(&active); err != nil || !active {
					t.Fatal("blocked cleanup lost active checkpoint", err)
				}
				unlock()
			} else {
				ctx, cancel := context.WithTimeout(contextFor(t), 500*time.Millisecond)
				started := time.Now()
				err = service.Force(ctx)
				cancel()
				if err != nil {
					t.Fatal("Force did not join cancelled source release", err)
				}
				if time.Since(started) > 650*time.Millisecond {
					t.Fatal("Force renewed per-release timeout")
				}
			}
			select {
			case err = <-cancelDone:
				if mode == "drain" && err != nil || mode == "force" && err == nil {
					t.Fatal("cleanup result disagrees with actual release outcome", mode, err)
				}
			case <-holderCtx.Done():
				t.Fatal("source cleanup did not join")
			}
			unlock()
			select {
			case r := <-holderDone:
				if r.State() != foundation.Committed {
					t.Fatal(r.Fault())
				}
			case <-holderCtx.Done():
				t.Fatal("holder did not join")
			}
			ctx, cancel := context.WithTimeout(contextFor(t), time.Second)
			defer cancel()
			if err = service.Drain(ctx); err != nil {
				t.Fatal(err)
			}
			var active bool
			if err = f.store.QueryRow(ctx, `SELECT state='active' FROM agenteam_object.object_leases WHERE id=$1`, lease.ID().String()).Scan(&active); err != nil || active != (mode == "force") {
				t.Fatal("shutdown invented a completed lease release", err, active)
			}
			if mode == "force" {
				requireCode(t, reads.CancelSourceLease(ctx, lease), foundation.ShuttingDown)
			}
			if proxy.gets.Load() != 0 {
				t.Fatal("unopened cancel issued GET")
			}
		})
	}
}

func TestObjectSourceLeaseParticipatesInServiceDrainAndForce(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "drain", true: "force"}[force], func(t *testing.T) {
			f := newFixture(t, false)
			body := strings.Repeat("source", 40000)
			stored := f.put(t, "source-shutdown", body)
			proxy := newStorageProxy(t, f)
			service, _ := f.on(t, f.store, proxy.server.URL, nil)
			provider := newSourceAuthority(t, f, service, stored)
			reads, err := object.NewSourceReads(service, provider)
			if err != nil {
				t.Fatal(err)
			}
			lease, result := acquireSource(t, f, service, reads, provider.source, nil)
			if result.State() != foundation.Committed {
				t.Fatal(result.Fault())
			}
			proxy.mode.Store(proxyHoldReadBody)
			reader, err := reads.OpenLeasedSource(contextFor(t), f.actor, lease)
			if err != nil {
				t.Fatal(err)
			}
			<-proxy.began
			readDone := make(chan error, 1)
			go func() {
				b, err := io.ReadAll(reader)
				if !force && err == nil && string(b) != body {
					err = fault(foundation.ObjectIntegrityMismatch)
				}
				readDone <- err
			}()
			ctx, cancel := context.WithTimeout(contextFor(t), time.Second)
			defer cancel()
			if force {
				if err = service.Force(ctx); err != nil {
					t.Fatal(err)
				}
			} else {
				service.StopAdmission()
				drained := make(chan error, 1)
				go func() { drained <- service.Drain(ctx) }()
				select {
				case <-drained:
					t.Fatal("drained while real source body held")
				default:
				}
				proxy.release()
				select {
				case err = <-drained:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal("source drain not joined")
				}
			}
			select {
			case err = <-readDone:
				if force && err == nil || !force && err != nil {
					t.Fatal("source stop outcome", err)
				}
			case <-ctx.Done():
				t.Fatal("source read not joined")
			}
			_ = reader.Close()
			var n int
			if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE id=$1 AND state='active'`, lease.ID().String()).Scan(&n); err != nil || n != 0 {
				t.Fatal("source shutdown retained joined lease", err)
			}
		})
	}
}
