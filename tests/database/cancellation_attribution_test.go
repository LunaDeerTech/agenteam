//go:build integration

package database_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// A real ReadyForQuery is held until the actual cancellation request reaches
// PostgreSQL. The control ACK is then held until the driver's SQL owner is in
// Unwatch. No SQL result or cancellation return is synthesized by this fixture.
func TestPoolSQLCancellationAttribution(t *testing.T) {
	const query = "SELECT $1::int /* pool-cancellation-attribution */"
	for _, mode := range []string{"tx_exec", "tx_rows", "exec", "rows", "lock", "begin", "set_config"} {
		t.Run(mode, func(t *testing.T) {
			targetQuery := query
			switch mode {
			case "lock":
				targetQuery = "SELECT pg_advisory_xact_lock($1)"
			case "begin":
				targetQuery = "begin isolation level read committed"
			case "set_config":
				targetQuery = "SELECT set_config('lock_timeout',$1,true)"
			}
			db := pgfixture.NewDatabase(t)
			proxy := newAttributionProxy(t, db, targetQuery, false)
			store := openStore(t, proxy.config(t, db))
			var pid int32
			if err := store.QueryRow(testContext(t), "SELECT pg_backend_pid()").Scan(&pid); err != nil {
				t.Fatal(err)
			}
			admin := db.Connect(t)
			parent := testContext(t)
			txCause := cause(t)
			lock, err := foundation.SystemConfigLock("pool-cancellation-fixture")
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				if mode == "exec" || mode == "rows" {
					item, cancel := context.WithTimeout(parent, 20*time.Millisecond)
					defer cancel()
					if mode == "exec" {
						_, err := store.Exec(item, query, 7)
						done <- err
					} else {
						rows, err := store.Query(item, query, 7)
						if err == nil {
							for rows.Next() {
							}
							rows.Close()
							err = rows.Err()
						}
						done <- err
					}
					return
				}
				txCtx := parent
				if mode == "begin" || mode == "set_config" {
					item, cancel := context.WithTimeout(parent, 20*time.Millisecond)
					defer cancel()
					txCtx = item
				}
				result := store.WithinTx(txCtx, txCause, func(ctx context.Context, tx foundation.Tx) error {
					if mode == "begin" || mode == "set_config" {
						t.Error("cancelled initialization entered the callback")
						return errors.New("unexpected callback")
					}
					item, cancel := context.WithTimeout(parent, 20*time.Millisecond)
					defer cancel()
					if mode == "lock" {
						return store.AcquireAll(item, tx, []foundation.LockRequest{{Key: lock, Mode: foundation.Exclusive}})
					}
					x, err := store.InTx(tx)
					if err != nil {
						return err
					}
					if mode == "tx_exec" {
						_, err = x.Exec(item, query, 7)
						return err
					}
					rows, err := x.Query(item, query, 7)
					if err == nil {
						for rows.Next() {
						}
						rows.Close()
						err = rows.Err()
					}
					return err
				})
				if result.State() != foundation.NotCommitted {
					t.Error("cancelled pre-COMMIT operation changed commit state")
				}
				done <- result.Fault()
			}()
			attributionAwait(t, proxy.ready)
			attributionAwait(t, proxy.requested)
			attributionAwait(t, proxy.delivered)
			unwatch := false
			limit, tick := time.NewTimer(50*time.Millisecond), time.NewTicker(time.Millisecond)
		loop:
			for {
				if attributionOwnerUnwatch() {
					unwatch = true
					break
				}
				select {
				case <-limit.C:
					break loop
				case <-tick.C:
				}
			}
			limit.Stop()
			tick.Stop()
			proxy.release()
			err = receive(t, done)
			if !unwatch || !proxy.ackSeen.Load() || parent.Err() != nil {
				t.Fatal("real cancellation/owner-unwatch sequence not reached")
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("%s lost actual cancellation cause: code=%s exact_closed=%t", mode, postgres.CodeOf(err), errors.Is(err, pgconn.ErrConnClosed))
			}
			if mode != "set_config" && !errors.Is(err, pgconn.ErrConnClosed) {
				t.Fatalf("%s did not retain the original closed-driver error", mode)
			}
			waitDatabase(t, admin, "SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1)", pid)
		})
	}
}
