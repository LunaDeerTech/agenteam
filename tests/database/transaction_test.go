//go:build integration

package database_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
)

func cause(t *testing.T) foundation.TransactionCause {
	t.Helper()
	id, err := foundation.NewID[foundation.TransactionAttempt]()
	if err != nil {
		t.Fatal(err)
	}
	cause, err := foundation.NewRecoveryCause("database.fixture", id.String(), "fixture/checkpoint")
	if err != nil {
		t.Fatal(err)
	}
	return cause
}
func executor(t *testing.T, s *postgres.Store, tx foundation.Tx) postgres.SQLExecutor {
	t.Helper()
	e, err := s.InTx(tx)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func waitDatabase(t *testing.T, conn *pgx.Conn, query string, args ...any) {
	t.Helper()
	ctx := testContext(t)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var ready bool
		if err := conn.QueryRow(ctx, query, args...).Scan(&ready); err != nil {
			t.Fatal("database barrier failed")
		}
		if ready {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("database barrier timed out")
		case <-ticker.C:
		}
	}
}
func requireState(t *testing.T, result foundation.CommitResult, state foundation.CommitState) {
	t.Helper()
	if result.State() != state {
		t.Fatalf("commit state=%s wanted=%s fault=%v", result.State(), state, result.Fault())
	}
}
func TestTransactionsCommitRollbackPanicCancellationAndHandles(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	migrate(t, cfg)
	store := openStore(t, cfg)
	other := openStore(t, cfg)
	conn := db.Connect(t)
	if _, err := conn.Exec(testContext(t), "CREATE TABLE tx_fact(id text PRIMARY KEY)"); err != nil {
		t.Fatal("fixture table failed")
	}
	for _, scenario := range []string{"commit", "reject", "cancel", "panic", "rows_open", "escaped", "cross_store", "nested", "transaction_control"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(testContext(t))
			defer cancel()
			var escaped postgres.SQLExecutor
			var token foundation.Tx
			var result foundation.CommitResult
			func() {
				defer func() {
					if value := recover(); value != nil {
						if scenario != "panic" || value != "private-panic-sentinel" {
							t.Fatal("panic identity changed")
						}
						result = foundation.NotCommittedResult(nil)
					}
				}()
				result = store.WithinTx(ctx, cause(t), func(ctx context.Context, tx foundation.Tx) error {
					e := executor(t, store, tx)
					escaped = e
					token = tx
					if _, err := e.Exec(ctx, "INSERT INTO tx_fact VALUES($1)", scenario); err != nil {
						return err
					}
					switch scenario {
					case "reject":
						return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
					case "cancel":
						cancel()
					case "panic":
						panic("private-panic-sentinel")
					case "rows_open":
						_, err := e.Query(ctx, "SELECT 1")
						return err
					case "cross_store":
						_, _ = other.InTx(tx)
					case "nested":
						nested := store.WithinTx(ctx, cause(t), func(context.Context, foundation.Tx) error { t.Error("nested callback called"); return nil })
						requireState(t, nested, foundation.NotCommitted)
					case "transaction_control":
						_, _ = e.Exec(ctx, "/* adapter misuse */ COMMIT")
					}
					return nil
				})
			}()
			want := foundation.NotCommitted
			if scenario == "commit" || scenario == "escaped" {
				want = foundation.Committed
			}
			requireState(t, result, want)
			if _, err := store.InTx(token); err == nil {
				t.Fatal("expired token accepted")
			}
			if _, err := escaped.Exec(testContext(t), "SELECT 1"); err == nil {
				t.Fatal("escaped executor accepted")
			}
			var exists bool
			if err := conn.QueryRow(testContext(t), "SELECT EXISTS(SELECT 1 FROM tx_fact WHERE id=$1)", scenario).Scan(&exists); err != nil || exists != (want == foundation.Committed) {
				t.Fatal("row contradicts commit result")
			}
		})
	}
}

type barrierScanner struct {
	entered chan struct{}
	release chan struct{}
}

func (s *barrierScanner) Scan(any) error { close(s.entered); <-s.release; return nil }
func TestConcurrentSQLAndRowsPoisonIgnoredMisuse(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	migrate(t, cfg)
	store := openStore(t, cfg)
	admin := db.Connect(t)
	if _, err := admin.Exec(testContext(t), "SELECT pg_advisory_lock(88771)"); err != nil {
		t.Fatal("barrier lock failed")
	}
	defer admin.Exec(context.Background(), "SELECT pg_advisory_unlock(88771)")
	result := store.WithinTx(testContext(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		e := executor(t, store, tx)
		var pid int32
		if err := e.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
			return err
		}
		done := make(chan error, 1)
		go func() { _, err := e.Exec(ctx, "SELECT pg_advisory_xact_lock(88771)"); done <- err }()
		waitDatabase(t, admin, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')", pid)
		if _, err := e.Exec(ctx, "SELECT 1"); postgres.CodeOf(err) != postgres.TransactionConcurrentUse {
			t.Error("concurrent operation not rejected")
		}
		t.Cleanup(func() {
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("cancelled SQL worker remains")
			}
		})
		return nil
	})
	requireState(t, result, foundation.NotCommitted)
	result = store.WithinTx(testContext(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		e := executor(t, store, tx)
		rows, err := e.Query(ctx, "SELECT 1")
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			return rows.Err()
		}
		scanner := &barrierScanner{make(chan struct{}), make(chan struct{})}
		done := make(chan error, 1)
		go func() { done <- rows.Scan(scanner) }()
		<-scanner.entered
		if rows.Next() {
			t.Error("concurrent rows accepted")
		}
		close(scanner.release)
		<-done
		if postgres.CodeOf(rows.Err()) != postgres.TransactionConcurrentUse {
			t.Error("rows misuse not retained")
		}
		return nil
	})
	requireState(t, result, foundation.NotCommitted)
}
func TestSerializableDeadlockAndDeferredCommitFailure(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	admin := db.Connect(t)
	if _, err := admin.Exec(testContext(t), "ALTER DATABASE "+pgx.Identifier{db.Name}.Sanitize()+" SET deadlock_timeout='50ms'"); err != nil {
		t.Fatal("fixture deadlock timing failed")
	}
	cfg := db.Config(t, nil)
	migrate(t, cfg)
	store := openStore(t, cfg)
	if _, err := admin.Exec(testContext(t), "CREATE TABLE conflict_fact(id integer PRIMARY KEY,value integer NOT NULL); INSERT INTO conflict_fact VALUES(1,0),(2,0); CREATE TABLE parent_fact(id integer PRIMARY KEY); CREATE TABLE child_fact(id integer REFERENCES parent_fact DEFERRABLE INITIALLY DEFERRED)"); err != nil {
		t.Fatal("fixture schema failed")
	}
	for _, scenario := range []string{"serializable", "deadlock"} {
		t.Run(scenario, func(t *testing.T) {
			ready := make(chan struct{}, 2)
			release := make(chan struct{})
			results := make(chan foundation.CommitResult, 2)
			var calls atomic.Int32
			for i := 1; i <= 2; i++ {
				go func(id int) {
					options := postgres.TxOptions{Isolation: postgres.ReadCommitted}
					if scenario == "serializable" {
						options.Isolation = postgres.Serializable
					}
					results <- store.WithinTxOptions(testContext(t), cause(t), options, func(ctx context.Context, tx foundation.Tx) error {
						calls.Add(1)
						e := executor(t, store, tx)
						if scenario == "serializable" {
							var sum int
							if err := e.QueryRow(ctx, "SELECT sum(value) FROM conflict_fact").Scan(&sum); err != nil {
								return err
							}
						} else {
							if _, err := e.Exec(ctx, "UPDATE conflict_fact SET value=value+1 WHERE id=$1", id); err != nil {
								return err
							}
						}
						ready <- struct{}{}
						<-release
						if scenario == "deadlock" {
							id = 3 - id
						}
						_, err := e.Exec(ctx, "UPDATE conflict_fact SET value=value+1 WHERE id=$1", id)
						return err
					})
				}(i)
			}
			<-ready
			<-ready
			close(release)
			a, b := <-results, <-results
			if calls.Load() != 2 {
				t.Fatal("callback retried")
			}
			if a.State() == foundation.NotCommitted {
				a, b = b, a
			}
			requireState(t, a, foundation.Committed)
			requireState(t, b, foundation.NotCommitted)
			var safe *postgres.Error
			if !errors.As(b.Fault(), &safe) {
				t.Fatal("safe SQL classification missing")
			}
			wanted := "40001"
			if scenario == "deadlock" {
				wanted = "40P01"
			}
			if safe.SQLState() != wanted {
				t.Fatalf("SQLSTATE %s wanted %s", safe.SQLState(), wanted)
			}
		})
	}
	result := store.WithinTx(testContext(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		_, err := executor(t, store, tx).Exec(ctx, "INSERT INTO child_fact VALUES(123)")
		return err
	})
	requireState(t, result, foundation.NotCommitted)
	var safe *postgres.Error
	if !errors.As(result.Fault(), &safe) || safe.Code() != postgres.TransactionCommitFailed || safe.SQLState() != "23503" {
		t.Fatal("deferred COMMIT rollback evidence not classified")
	}
}
