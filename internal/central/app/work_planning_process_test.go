//go:build integration

package app

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

type workRootHeldCall struct {
	pid       int32
	cancelled <-chan struct{}
}

// This port holds only explicitly selected calls after a real BEGIN and a real
// backend PID query. It does not manufacture a transaction, response or commit.
// Ignoring cancellation until release lets the root prove actual call ownership.
type workRootHeldStore struct {
	*postgres.Store
	entered     chan workRootHeldCall
	release     chan struct{}
	releaseOnce sync.Once
	force       chan bool
	forceOnce   sync.Once
}

func (s *workRootHeldStore) unhold() { s.releaseOnce.Do(func() { close(s.release) }) }
func (s *workRootHeldStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	hold := cause.Kind() == foundation.CommandsCause && strings.HasPrefix(string(cause.Details().Primary.Key()), "work-root-held-")
	if !hold {
		return s.Store.WithinTx(ctx, cause, fn)
	}
	return s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		x, err := s.Store.InTx(tx)
		if err != nil {
			return err
		}
		var pid int32
		if err = x.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
			return err
		}
		s.entered <- workRootHeldCall{pid, ctx.Done()}
		<-s.release
		return fn(ctx, tx)
	})
}
func (s *workRootHeldStore) ForceClose(ctx context.Context) error {
	s.forceOnce.Do(func() { s.force <- ctx.Err() != nil })
	return s.Store.ForceClose(ctx)
}

type workRootAccountDrain struct {
	accountRuntime
	entered chan struct{}
	once    sync.Once
}

func (w *workRootAccountDrain) Drain(ctx context.Context) error {
	w.once.Do(func() { close(w.entered) })
	return w.accountRuntime.Drain(ctx)
}

// Unlike the pure blocked-Store controls, all three calls here are admitted by
// the actual production bundle and own real PostgreSQL transactions. The test
// calls Lookup directly to isolate command lifetime from HTTP lifetime; native
// Reader/HTTP and command confirmation have separate acceptance scenarios.
func TestWorkOwnerRootActualCommandJoin(t *testing.T) {
	for _, forced := range []bool{false, true} {
		t.Run(map[bool]string{false: "graceful-three-calls", true: "force-does-not-invent-join"}[forced], func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			budget := "5s"
			if forced {
				budget = "1s"
			}
			cfg := guardConfiguration(t, append(guardEnvironment(t, db), "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT="+budget))
			var store *workRootHeldStore
			var owner *resources
			var core *account.Service
			accountDrain := make(chan struct{})
			deps := dependencies{
				open: func(ctx context.Context, cfg postgres.Config) (database, error) {
					raw, err := postgres.Open(ctx, cfg)
					if err != nil {
						return nil, err
					}
					store = &workRootHeldStore{Store: raw, entered: make(chan workRootHeldCall, 3), release: make(chan struct{}), force: make(chan bool, 1)}
					return store, nil
				},
				observeAccount: func(v *account.Service) { core = v },
				bind: func(ctx context.Context, cfg config.Config, db database, owned *resources, deps *dependencies) error {
					owner = owned
					if err := bindAccounts(ctx, cfg, db, owned, deps); err != nil {
						return err
					}
					a := owned.accounts().(*accountAssembly)
					a.mu.Lock()
					a.runtime = &workRootAccountDrain{accountRuntime: a.runtime, entered: accountDrain}
					a.mu.Unlock()
					return nil
				},
			}
			output := newEventLog()
			logger, err := logging.New(logging.Central, slog.LevelInfo, output)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			signals := make(chan os.Signal, 1)
			done := make(chan struct{})
			var runErr error
			go func() { runErr = run(ctx, cfg, logger, signals, deps); close(done) }()
			t.Cleanup(func() {
				if store != nil {
					store.unhold()
				}
				cancel()
				select {
				case <-done:
				case <-time.After(7 * time.Second):
					t.Error("actual root failed to join on cleanup")
				}
			})
			output.wait(t, func(e map[string]any) bool { return e["event"] == "listening" })
			actor, err := fixtureAccountActor(databaseTestContext(t), cfg, core)
			if err != nil {
				t.Fatal(err)
			}
			a := owner.accounts().(*accountAssembly)
			planning := a.planning.(*workPlanningAssembly)
			projectID := guardID[identity.Project](t)
			semantic := foundation.Digest("sha256:" + strings.Repeat("0", 64))
			completed := make(chan error, 3)
			go func() {
				_, err := planning.structure.LookupCommand(context.Background(), actor, wc.CommandLookupRequest{ProjectID: projectID, Command: wc.MilestoneCreate, Key: "work-root-held-structure", Semantic: semantic})
				completed <- err
			}()
			go func() {
				_, err := planning.tasks.LookupTaskCommand(context.Background(), actor, wc.TaskCommandLookupRequest{ProjectID: projectID, Command: wc.TaskCommandCreate, IdempotencyKey: "work-root-held-task", SemanticDigest: semantic})
				completed <- err
			}()
			go func() {
				_, err := planning.blockers.LookupTaskBlockerCommand(context.Background(), actor, wc.TaskBlockerCommandLookupRequest{ProjectID: projectID, Command: wc.TaskBlockerCommandAdd, IdempotencyKey: "work-root-held-blocker", SemanticDigest: semantic})
				completed <- err
			}()
			var calls []workRootHeldCall
			for range 3 {
				select {
				case call := <-store.entered:
					calls = append(calls, call)
				case err := <-completed:
					t.Fatal("actual Work call returned before transaction hold", err)
				case <-time.After(5 * time.Second):
					t.Fatal("actual Work transaction admission missing")
				}
			}
			admin := db.Connect(t)
			for _, call := range calls {
				var active bool
				if err := admin.QueryRow(databaseTestContext(t), "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid=$1 AND xact_start IS NOT NULL)", call.pid).Scan(&active); err != nil || !active {
					t.Fatal("held call did not own a real transaction", err)
				}
			}
			signals <- syscall.SIGTERM
			for _, call := range calls {
				select {
				case <-call.cancelled:
				case <-time.After(2 * time.Second):
					t.Fatal("root failed to stop every Work admission before drain")
				}
			}
			if planning.Joined() {
				t.Fatal("cancelled but blocked callbacks claimed joined")
			}
			select {
			case <-accountDrain:
				t.Fatal("Account Activity retired before held Work joined")
			default:
			}
			if !forced {
				store.unhold()
				for range 3 {
					select {
					case err := <-completed:
						if err == nil {
							t.Error("cancelled Work callback reported success")
						}
					case <-time.After(3 * time.Second):
						t.Fatal("released Work caller did not actually return")
					}
				}
				select {
				case <-done:
				case <-time.After(6 * time.Second):
					t.Fatal("graceful root did not join")
				}
				if runErr != nil || !planning.Joined() {
					t.Fatal("graceful root lost actual Work join", runErr)
				}
				select {
				case <-accountDrain:
				default:
					t.Fatal("Account runtime was never drained")
				}
			} else {
				select {
				case <-done:
				case <-time.After(4 * time.Second):
					t.Fatal("forced root waited beyond original shutdown budget")
				}
				if runErr == nil || planning.Joined() {
					t.Fatal("forced root claimed unresolved Work had joined")
				}
				select {
				case expired := <-store.force:
					if !expired {
						t.Fatal("forced DB got a renewed shutdown budget")
					}
				default:
					t.Fatal("unjoined Work prevented required DB ForceClose")
				}
				select {
				case <-completed:
					t.Fatal("held callback returned before explicit release")
				default:
				}
				store.unhold()
				for range 3 {
					select {
					case err := <-completed:
						if err == nil {
							t.Error("forced Work callback reported success")
						}
					case <-time.After(3 * time.Second):
						t.Fatal("forced Work callback did not return after release")
					}
				}
				joined, stop := context.WithTimeout(context.Background(), time.Second)
				err := planning.Drain(joined)
				stop()
				if err != nil || !planning.Joined() {
					t.Fatal("actual post-release Work join missing", err)
				}
			}
			// No command was submitted twice and no fake Project was required: these
			// are held read-only lookups against a canonical absent ID, not write proof.
			if _, err := planning.tasks.LookupTaskCommand(context.Background(), actor, wc.TaskCommandLookupRequest{ProjectID: projectID, Command: wc.TaskCommandCreate, IdempotencyKey: "work-root-late", SemanticDigest: semantic}); err == nil {
				t.Fatal("stopped root accepted new Work")
			}
			var absent bool
			if err := admin.QueryRow(databaseTestContext(t), "SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam')").Scan(&absent); err != nil || !absent {
				t.Fatal("root retained database borrowers", err)
			}
		})
	}
}
