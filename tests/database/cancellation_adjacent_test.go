//go:build integration

package database_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// The first SQL really completes, then its actual Unwatch closes the driver.
// A following call has a new private SQL scope but the same physical checkout.
func TestPoolSQLCancellationAttributionAcrossBusinessCalls(t *testing.T) {
	const query = "SELECT $1::int /* cancellation-after-execute */"
	for _, mode := range []string{"exec", "rows", "other_caller", "commit_after_first", "independent_drop"} {
		t.Run(mode, func(t *testing.T) {
			drop := mode == "independent_drop"
			db := pgfixture.NewDatabase(t)
			proxy := newAttributionExecuteProxy(t, db, query, drop)
			store := openStore(t, proxy.config(t, db))
			var pid int32
			if err := store.QueryRow(testContext(t), "SELECT pg_backend_pid()").Scan(&pid); err != nil {
				t.Fatal(err)
			}
			admin := db.Connect(t)
			parent := testContext(t)
			txCause := cause(t)
			type observed struct {
				result        foundation.CommitResult
				first, second error
				item          context.Context
				firstLive     bool
				rows          int
			}
			done := make(chan observed, 1)
			go func() {
				var got observed
				got.result = store.WithinTx(parent, txCause, func(ctx context.Context, tx foundation.Tx) error {
					item, cancel := context.WithTimeout(parent, 20*time.Millisecond)
					defer cancel()
					got.item = item
					x, err := store.InTx(tx)
					if err != nil {
						return err
					}
					if mode == "rows" {
						rows, err := x.Query(item, query, 7)
						got.first = err
						if err == nil {
							for rows.Next() {
								var value int
								if err := rows.Scan(&value); err != nil {
									got.first = err
									break
								}
								if value == 7 {
									got.rows++
								}
							}
							rows.Close()
							if got.first == nil {
								got.first = rows.Err()
							}
						}
					} else {
						_, got.first = x.Exec(item, query, 7)
					}
					got.firstLive = item.Err() == nil
					if got.first != nil {
						return got.first
					}
					if mode == "commit_after_first" {
						return nil // invoking COMMIT must retain its conservative Unknown boundary
					}
					next := item
					if mode == "other_caller" {
						// Even the same deadline/cause cannot transfer the proof
						// to an independently constructed caller context.
						deadline, _ := item.Deadline()
						other, stop := context.WithDeadline(parent, deadline)
						defer stop()
						next = other
					}
					_, got.second = x.Exec(next, query, 8)
					return got.second
				})
				done <- got
			}()
			attributionAwait(t, proxy.ready)
			unwatch := false
			if !drop {
				attributionAwait(t, proxy.requested)
				attributionAwait(t, proxy.delivered)
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
			}
			got := receive(t, done)
			// ACK receipt is published by the actual control reader. Inspect it
			// after the SQL owner joins, not immediately after releasing its gate.
			if !drop && (!unwatch || !proxy.ackSeen.Load()) {
				t.Fatalf("real Execute/Unwatch sequence not reached: unwatch=%t ack=%t first_nil=%t", unwatch, proxy.ackSeen.Load(), got.first == nil)
			}
			wantState := foundation.NotCommitted
			if mode == "commit_after_first" {
				wantState = foundation.Unknown
			}
			if got.result.State() != wantState || parent.Err() != nil {
				t.Fatal("pre-COMMIT cancellation changed state or the operation parent")
			}
			if drop {
				if got.first == nil || !got.firstLive || got.second != nil || errors.Is(got.result.Fault(), context.Canceled) || errors.Is(got.result.Fault(), context.DeadlineExceeded) {
					t.Fatal("independent disconnect was changed into cancellation")
				}
			} else if mode == "commit_after_first" {
				if got.first != nil || got.firstLive || got.second != nil {
					t.Fatal("COMMIT control did not follow the completed first SQL")
				}
			} else {
				if got.first != nil || got.firstLive || !errors.Is(got.second, pgconn.ErrConnClosed) {
					t.Fatal("expected completed first SQL followed by exact closed-driver error")
				}
				if mode == "rows" && got.rows != 1 {
					t.Fatal("the original real row was not consumed")
				}
				if errors.Is(got.second, context.DeadlineExceeded) != (mode != "other_caller") {
					t.Fatal("terminal proof was lost or inherited by another caller")
				}
			}
			waitDatabase(t, admin, "SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1)", pid)
		})
	}
}
