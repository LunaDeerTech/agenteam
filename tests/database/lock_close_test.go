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
)

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		t.Fatal("fixture barrier timed out")
		var zero T
		return zero
	}
}
func TestSharedExclusiveLocksAndPoisonedOrder(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, map[string]string{"LOCK_TIMEOUT": "5s"})
	migrate(t, cfg)
	s := openStore(t, cfg)
	admin := db.Connect(t)
	project, _ := foundation.ProjectLock("01900000-0000-7000-8000-000000000001")
	agent, _ := foundation.AgentLock("01900000-0000-7000-8000-000000000002")
	sharedReady := make(chan struct{}, 2)
	releaseShared := make(chan struct{})
	results := make(chan foundation.CommitResult, 3)
	for range 2 {
		go func() {
			results <- s.WithinTx(testContext(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
				if err := s.Acquire(ctx, tx, project, foundation.Shared); err != nil {
					return err
				}
				sharedReady <- struct{}{}
				<-releaseShared
				return nil
			})
		}()
	}
	receive(t, sharedReady)
	receive(t, sharedReady)
	pidReady := make(chan int32, 1)
	exclusive := make(chan struct{}, 1)
	go func() {
		results <- s.WithinTx(testContext(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			var pid int32
			if err := executor(t, s, tx).QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
				return err
			}
			pidReady <- pid
			if err := s.Acquire(ctx, tx, project, foundation.Exclusive); err != nil {
				return err
			}
			exclusive <- struct{}{}
			return nil
		})
	}()
	pid := receive(t, pidReady)
	waitDatabase(t, admin, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')", pid)
	select {
	case <-exclusive:
		t.Fatal("exclusive entered while shared held")
	default:
	}
	close(releaseShared)
	receive(t, exclusive)
	for range 3 {
		requireState(t, receive(t, results), foundation.Committed)
	}
	result := s.WithinTx(testContext(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.Acquire(ctx, tx, agent, foundation.Exclusive); err != nil {
			return err
		}
		if err := s.Acquire(ctx, tx, project, foundation.Shared); postgres.CodeOf(err) != postgres.LockOrderViolation {
			t.Error("lock order not enforced")
		}
		return nil
	})
	requireState(t, result, foundation.NotCommitted)
	ready := make(chan struct{}, 2)
	upgrade := make(chan struct{})
	for range 2 {
		go func() {
			results <- s.WithinTx(testContext(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
				if err := s.Acquire(ctx, tx, project, foundation.Shared); err != nil {
					return err
				}
				ready <- struct{}{}
				<-upgrade
				if err := s.Acquire(ctx, tx, project, foundation.Exclusive); postgres.CodeOf(err) != postgres.LockUpgradeForbidden {
					t.Error("upgrade allowed")
				}
				return nil
			})
		}()
	}
	receive(t, ready)
	receive(t, ready)
	close(upgrade)
	for range 2 {
		requireState(t, receive(t, results), foundation.NotCommitted)
	}
	result = s.WithinTx(testContext(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		return s.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: agent, Mode: foundation.Exclusive}, {Key: project, Mode: foundation.Shared}, {Key: project, Mode: foundation.Exclusive}})
	})
	requireState(t, result, foundation.Committed)
}
func TestLocksReleaseOnCommitRollbackAndConnectionLoss(t *testing.T) {
	for _, mode := range []string{"commit", "rollback", "disconnect"} {
		t.Run(mode, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			cfg := db.Config(t, map[string]string{"LOCK_TIMEOUT": "5s"})
			migrate(t, cfg)
			s := openStore(t, cfg)
			admin := db.Connect(t)
			key, _ := foundation.ProjectLock("01900000-0000-7000-8000-000000000001")
			ready := make(chan int32, 1)
			release := make(chan struct{})
			first := make(chan foundation.CommitResult, 1)
			go func() {
				first <- s.WithinTx(testContext(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
					if err := s.Acquire(ctx, tx, key, foundation.Exclusive); err != nil {
						return err
					}
					var pid int32
					if err := executor(t, s, tx).QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
						return err
					}
					ready <- pid
					<-release
					if mode == "rollback" {
						return errors.New("fixture rejection")
					}
					return nil
				})
			}()
			firstPID := receive(t, ready)
			secondPID := make(chan int32, 1)
			second := make(chan foundation.CommitResult, 1)
			go func() {
				second <- s.WithinTx(testContext(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
					var pid int32
					if err := executor(t, s, tx).QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
						return err
					}
					secondPID <- pid
					return s.Acquire(ctx, tx, key, foundation.Exclusive)
				})
			}()
			waitDatabase(t, admin, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')", receive(t, secondPID))
			if mode == "disconnect" {
				if err := db.Terminate(testContext(t), firstPID); err != nil {
					t.Fatal(err)
				}
				requireState(t, receive(t, second), foundation.Committed)
				close(release)
			} else {
				close(release)
				requireState(t, receive(t, second), foundation.Committed)
			}
			want := foundation.Committed
			if mode != "commit" {
				want = foundation.NotCommitted
			}
			if mode == "disconnect" {
				want = foundation.Unknown
			}
			requireState(t, receive(t, first), want)
		})
	}
}
func TestStoreDrainAndBoundedForce(t *testing.T) {
	for _, mode := range []string{"drain", "blocked_io", "uncooperative_callback", "open_rows"} {
		t.Run(mode, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			cfg := db.Config(t, nil)
			migrate(t, cfg)
			s := openStore(t, cfg)
			admin := db.Connect(t)
			if mode == "open_rows" {
				rows, err := s.Query(testContext(t), "SELECT 1")
				if err != nil {
					t.Fatal(err)
				}
				short, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
				defer cancel()
				if s.Drain(short) == nil {
					t.Fatal("drain ignored rows checkout")
				}
				rows.Close()
				if err := s.Drain(testContext(t)); err != nil {
					t.Fatal(err)
				}
				return
			}
			ready := make(chan int32, 1)
			release := make(chan struct{})
			result := make(chan foundation.CommitResult, 1)
			go func() {
				result <- s.WithinTx(testContext(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
					e := executor(t, s, tx)
					var pid int32
					if err := e.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
						return err
					}
					ready <- pid
					if mode == "blocked_io" {
						_, err := e.Exec(ctx, "SELECT pg_sleep(60)")
						return err
					}
					<-release
					if mode == "drain" {
						_, err := e.Exec(ctx, "SELECT 1")
						return err
					}
					return nil
				})
			}()
			pid := receive(t, ready)
			if mode == "blocked_io" {
				waitDatabase(t, admin, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event='PgSleep')", pid)
			}
			if mode == "drain" {
				s.StopAdmission()
				if _, err := s.Exec(testContext(t), "SELECT 1"); postgres.CodeOf(err) != postgres.AdmissionStopped {
					t.Fatal("new checkout admitted")
				}
				done := make(chan error, 1)
				go func() { done <- s.Drain(testContext(t)) }()
				close(release)
				requireState(t, receive(t, result), foundation.Committed)
				if err := receive(t, done); err != nil {
					t.Fatal(err)
				}
			} else {
				start := time.Now()
				_ = s.ForceClose(testContext(t))
				if time.Since(start) > 1500*time.Millisecond {
					t.Fatal("force waited for callback")
				}
				if mode == "uncooperative_callback" {
					close(release)
				}
				requireState(t, receive(t, result), foundation.NotCommitted)
			}
			waitDatabase(t, admin, "SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1)", pid)
		})
	}
}

func TestIgnoredLockTimeoutAndCancellationPoisonTransaction(t *testing.T) {
	for _, mode := range []string{"timeout", "cancellation"} {
		t.Run(mode, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			cfg := db.Config(t, map[string]string{"LOCK_TIMEOUT": "100ms"})
			migrate(t, cfg)
			store := openStore(t, cfg)
			admin := db.Connect(t)
			key, _ := foundation.ProjectLock("01900000-0000-7000-8000-000000000001")
			if _, err := admin.Exec(testContext(t), "CREATE TABLE lock_fact(id integer)"); err != nil {
				t.Fatal("fixture table failed")
			}
			if _, err := admin.Exec(testContext(t), "SELECT pg_advisory_lock($1)", key.AdvisoryKey()); err != nil {
				t.Fatal("fixture lock failed")
			}
			result := store.WithinTx(testContext(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
				if _, err := executor(t, store, tx).Exec(ctx, "INSERT INTO lock_fact VALUES(1)"); err != nil {
					return err
				}
				lockCtx := ctx
				if mode == "cancellation" {
					var cancel context.CancelFunc
					lockCtx, cancel = context.WithCancel(ctx)
					cancel()
				}
				if err := store.Acquire(lockCtx, tx, key, foundation.Exclusive); postgres.CodeOf(err) != postgres.LockFailed {
					t.Error("lock failure not returned")
				}
				return nil // Ignoring the error must still prevent COMMIT.
			})
			requireState(t, result, foundation.NotCommitted)
			var count int
			if err := admin.QueryRow(testContext(t), "SELECT count(*) FROM lock_fact").Scan(&count); err != nil || count != 0 {
				t.Fatal("poisoned transaction persisted a row")
			}
		})
	}
}
