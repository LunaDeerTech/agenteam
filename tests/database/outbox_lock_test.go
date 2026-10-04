//go:build integration

package database_test

import (
	"context"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func TestOutboxRequireHeldRealPoisonAndNoLockAcquisition(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	migrate(t, cfg)
	s := openStore(t, cfg)
	other := openStore(t, cfg)
	if _, err := s.Exec(testContext(t), `CREATE TABLE require_held_fact(id text PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	key, _ := foundation.SystemConfigLock("outbox-registration")
	record, _ := foundation.RecordLock(foundation.OutboxRecordLock, "higher")
	for _, scenario := range []string{"missing", "weak", "other", "concurrent", "valid"} {
		t.Run(scenario, func(t *testing.T) {
			var token foundation.Tx
			result := s.WithinTx(testContext(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
				token = tx
				x := executor(t, s, tx)
				if _, err := x.Exec(ctx, `INSERT INTO require_held_fact VALUES($1)`, scenario); err != nil {
					return err
				}
				if scenario != "missing" {
					if err := s.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Shared}, {Key: record, Mode: foundation.Exclusive}}); err != nil {
						return err
					}
				}
				switch scenario {
				case "other":
					_ = other.RequireHeldLocks(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Shared}})
				case "concurrent":
					rows, err := x.Query(ctx, `SELECT 1`)
					if err != nil {
						return err
					}
					_ = s.RequireHeldLocks(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Shared}})
					rows.Close()
				case "valid":
					var before, after int64
					if err := x.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE pid=pg_backend_pid() AND locktype='advisory'`).Scan(&before); err != nil {
						return err
					}
					if err := s.RequireHeldLocks(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Shared}}); err != nil {
						return err
					}
					if err := x.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE pid=pg_backend_pid() AND locktype='advisory'`).Scan(&after); err != nil {
						return err
					}
					if before != after || before != 2 {
						t.Fatal("RequireHeld took locks")
					}
				default:
					_ = s.RequireHeldLocks(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}})
				}
				return nil
			})
			want := foundation.NotCommitted
			if scenario == "valid" {
				want = foundation.Committed
			}
			requireState(t, result, want)
			if err := s.RequireHeldLocks(testContext(t), token, nil); err == nil || postgres.CodeOf(err) != postgres.InvalidTransaction {
				t.Fatal("ended Tx must be removed from the live handle registry")
			}
		})
	}
	var count int
	if err := s.QueryRow(testContext(t), `SELECT count(*) FROM require_held_fact`).Scan(&count); err != nil || count != 1 {
		t.Fatal("ignored requirement violation committed business")
	}
}
