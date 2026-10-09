//go:build integration

package app

import (
	"context"
	"errors"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"log/slog"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// A real BEGIN/SQL PID is retained until explicitly released, including after
// cancellation. No response, terminal commit state or successful join is faked.
type variableRootHeldStore struct {
	*postgres.Store
	entered      chan workRootHeldCall
	release      chan struct{}
	releaseOnce  sync.Once
	forceContext chan context.Context
	forceOnce    sync.Once
}

func (s *variableRootHeldStore) unhold() { s.releaseOnce.Do(func() { close(s.release) }) }
func (s *variableRootHeldStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	hold := cause.Kind() == foundation.CommandsCause && strings.HasPrefix(string(cause.Details().Primary.Key()), "variable-root-held-")
	if cause.Kind() == foundation.RecoveryCause && (cause.Details().Owner == "projectvariable.get" || cause.Details().Owner == "projectvariable.list") {
		hold = true
	}
	if !hold {
		return s.Store.WithinTx(ctx, cause, fn)
	}
	returned := make(chan struct{})
	defer close(returned)
	return s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		x, e := s.Store.InTx(tx)
		if e != nil {
			return e
		}
		var pid int32
		if e = x.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); e != nil {
			return e
		}
		s.entered <- workRootHeldCall{pid, ctx.Done(), returned}
		<-s.release
		return fn(ctx, tx)
	})
}
func (s *variableRootHeldStore) ForceClose(ctx context.Context) error {
	s.forceOnce.Do(func() { s.forceContext <- ctx })
	return s.Store.ForceClose(ctx)
}

func TestProjectVariablesRootActualCallJoin(t *testing.T) {
	for _, forced := range []bool{false, true} {
		t.Run(map[bool]string{false: "graceful-read-mutation-lookup", true: "force-does-not-invent-join"}[forced], func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			budget := "5s"
			if forced {
				budget = "1s"
			}
			cfg := guardConfiguration(t, append(guardEnvironment(t, db), "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT="+budget))
			var store *variableRootHeldStore
			var owner *resources
			var core *account.Service
			accountDrain := make(chan struct{})
			deps := dependencies{
				open: func(ctx context.Context, cfg postgres.Config) (database, error) {
					raw, err := postgres.Open(ctx, cfg)
					if err != nil {
						return nil, err
					}
					store = &variableRootHeldStore{Store: raw, entered: make(chan workRootHeldCall, 4), release: make(chan struct{}), forceContext: make(chan context.Context, 1)}
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
			variables := a.variables.(*projectVariablesAssembly)
			projectID := guardID[identity.Project](t)
			target := guardID[identity.ProjectVariable](t)
			semantic := foundation.Digest("sha256:" + strings.Repeat("0", 64))
			query, e := vc.NewVariableCommandLookupRequest(vc.VariableCommandLookupFields{ProjectID: projectID, Command: vc.DeleteCommand, IdempotencyKey: "variable-root-held-lookup", SemanticDigest: semantic})
			if e != nil {
				t.Fatal(e)
			}
			version := foundation.Version(1)
			deleteMeta := foundation.CommandMeta{RequestID: guardID[foundation.Request](t), IdempotencyKey: "variable-root-held-delete", ExpectedVersion: &version}
			if err := deleteMeta.Validate(); err != nil {
				t.Fatal("invalid Delete command test prerequisite")
			}
			type callResult struct {
				name string
				err  error
			}
			completed := make(chan callResult, 4)
			go func() {
				_, e := variables.service.GetVariable(context.Background(), actor, projectID, target)
				completed <- callResult{"get", e}
			}()
			go func() {
				_, e := variables.service.ListVariables(context.Background(), actor, projectID, foundation.DefaultPageRequest())
				completed <- callResult{"list", e}
			}()
			go func() {
				_, e := variables.service.LookupVariableCommand(context.Background(), actor, query)
				completed <- callResult{"lookup", e}
			}()
			go func() {
				_, e := variables.service.DeleteVariable(context.Background(), actor, deleteMeta, projectID, target)
				completed <- callResult{"delete", e}
			}()
			var calls []workRootHeldCall
			for range 4 {
				select {
				case call := <-store.entered:
					calls = append(calls, call)
				case result := <-completed:
					code := "unavailable"
					var fault *foundation.Fault
					if errors.As(result.err, &fault) && fault.Code.Known() {
						code = string(fault.Code)
					}
					t.Fatal("actual Variable call returned before transaction hold", result.name, code)
				case <-time.After(5 * time.Second):
					t.Fatal("actual Variable transaction admission missing")
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
					t.Fatal("root failed to stop every Variable admission before drain")
				}
			}
			if variables.Joined() {
				t.Fatal("cancelled but blocked callbacks claimed joined")
			}
			select {
			case <-accountDrain:
				t.Fatal("Account Activity retired before held Variable joined")
			default:
			}
			if !forced {
				store.unhold()
				for range 4 {
					select {
					case result := <-completed:
						if result.err == nil {
							t.Error("cancelled Variable callback reported success")
						}
					case <-time.After(3 * time.Second):
						t.Fatal("released Variable caller did not actually return")
					}
				}
				select {
				case <-done:
				case <-time.After(6 * time.Second):
					t.Fatal("graceful root did not join")
				}
				if runErr != nil || !variables.Joined() {
					t.Fatal("graceful root lost actual Variable join", runErr)
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
				if runErr == nil || variables.Joined() {
					t.Fatal("forced root claimed unresolved Variable had joined")
				}
				select {
				case actual := <-store.forceContext:
					owner.mu.Lock()
					original := owner.forced
					owner.mu.Unlock()
					if actual == nil || actual != original {
						t.Fatal("DB lost original Force context")
					}
					got, ok := actual.Deadline()
					want, bounded := original.Deadline()
					if !ok || !bounded || !got.Equal(want) {
						t.Fatal("DB renewed Force deadline")
					}
				default:
					t.Fatal("unjoined Variable calls prevented required DB ForceClose")
				}
				select {
				case <-completed:
					t.Fatal("held callback returned before explicit release")
				default:
				}
				store.unhold()
				for range 4 {
					select {
					case result := <-completed:
						if result.err == nil {
							t.Error("forced Variable callback reported success")
						}
					case <-time.After(3 * time.Second):
						t.Fatal("forced Variable callback did not return after release")
					}
				}
				joined, stop := context.WithTimeout(context.Background(), time.Second)
				err := variables.Drain(joined)
				stop()
				if err != nil || !variables.Joined() {
					t.Fatal("actual post-release Variable join missing", err)
				}
			}
			// These are actual admitted calls held before authorization against a
			// canonical absent Project. Formal successful mutation is proven by
			// the separate executable HTTP test, not this lifecycle barrier.
			if _, e := variables.service.GetVariable(context.Background(), actor, projectID, target); e == nil {
				t.Fatal("stopped root accepted new Variable read")
			}
			var absent bool
			if err := admin.QueryRow(databaseTestContext(t), "SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam')").Scan(&absent); err != nil || !absent {
				t.Fatal("root retained database borrowers", err)
			}
		})
	}
}
