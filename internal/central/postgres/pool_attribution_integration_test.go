//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/jackc/pgx/v5/pgconn"
)

// CheckIndependentPoolCloseForTest is compiled only into this package's test
// binary. The external test provides an ownership-checked PG fixture; this
// helper observes the actual private driver without exposing it in production.
func CheckIndependentPoolCloseForTest(t *testing.T, cfg Config, terminate func(int32)) {
	t.Helper()
	for _, cancelFirst := range []bool{false, true} {
		name := "closed_then_error_then_cancel"
		if cancelFirst {
			name = "closed_then_cancel_then_error"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			store, err := Open(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				cleanup, stop := context.WithTimeout(context.Background(), time.Second)
				defer stop()
				_ = store.ForceClose(cleanup)
			}()
			id, err := foundation.NewID[foundation.TransactionAttempt]()
			if err != nil {
				t.Fatal(err)
			}
			cause, err := foundation.NewRecoveryCause("postgres.fixture", id.String(), "independent/closed")
			if err != nil {
				t.Fatal(err)
			}
			var sqlErr error
			result := store.WithinTx(ctx, cause, func(txCtx context.Context, tx foundation.Tx) error {
				x, err := store.InTx(tx)
				if err != nil {
					return err
				}
				var pid int32
				if err = x.QueryRow(txCtx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
					return err
				}
				terminate(pid)
				owned, err := store.find(tx)
				if err != nil {
					return err
				}
				// Consume the real terminal FATAL/EOF below the adapter once. A
				// public first error would poison the transaction and correctly
				// prevent the next SQL call from reaching the driver's lock.
				_, first := owned.raw.Exec(txCtx, "SELECT 1")
				if first == nil || txCtx.Err() != nil {
					t.Fatal("independent server termination was not observed while caller live")
				}
				select {
				case <-owned.op.target.pg.CleanupDone():
				case <-ctx.Done():
					t.Fatal("independently closed driver failed to finish local cleanup")
				}
				if !owned.op.target.pg.IsClosed() || owned.op.target.work != nil {
					t.Fatal("independent close unexpectedly selected platform cancellation work")
				}
				item, stop := context.WithCancel(txCtx)
				defer stop()
				if cancelFirst {
					stop()
				}
				_, sqlErr = x.Exec(item, "SELECT 2")
				stop()
				if !errors.Is(sqlErr, pgconn.ErrConnClosed) || CodeOf(sqlErr) != SQLFailed {
					t.Fatal("negative control did not reach the exact closed-driver error")
				}
				if errors.Is(sqlErr, context.Canceled) || errors.Is(sqlErr, context.DeadlineExceeded) {
					t.Fatal("independent closed-driver failure was reclassified by later caller cancellation")
				}
				return sqlErr
			})
			if result.State() != foundation.NotCommitted || !errors.Is(result.Fault(), pgconn.ErrConnClosed) || ctx.Err() != nil {
				t.Fatal("independent closed-driver error changed pre-COMMIT state or caller")
			}
		})
	}
}
