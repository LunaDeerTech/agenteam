package app

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

// This controls the original Store's return, never a successful SQL callback.
// The real uninitialized ProcessGuard fails closed before business SQL.
type usageOwnedAppStore struct {
	*postgres.Store
	mu      sync.Mutex
	causes  []f.TransactionCause
	sql     int
	entered chan context.Context
	release chan struct{}
	unknown bool
	masked  bool
	attempt f.ID[f.TransactionAttempt]
}

func (s *usageOwnedAppStore) WithinTx(ctx context.Context, cause f.TransactionCause, callback func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	s.causes = append(s.causes, cause)
	entered, release := s.entered, s.release
	unknown := s.unknown && !s.masked && cause.Details().Owner == "usage-call-register"
	if unknown {
		s.masked = true
	}
	s.mu.Unlock()
	if entered != nil {
		entered <- ctx
		<-release
	}
	if unknown {
		return f.UnknownResult(s.attempt, cause)
	}
	var fault *f.Fault
	if errors.As(callback(ctx, f.NewTx()), &fault) {
		return f.NotCommittedResult(fault)
	}
	return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
}
func (*usageOwnedAppStore) AcquireAll(context.Context, f.Tx, []f.LockRequest) error { return nil }
func (s *usageOwnedAppStore) InTx(f.Tx) (postgres.SQLExecutor, error) {
	s.mu.Lock()
	s.sql++
	s.mu.Unlock()
	return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
}
func (s *usageOwnedAppStore) snapshot() ([]f.TransactionCause, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]f.TransactionCause(nil), s.causes...), s.sql
}
func (s *usageOwnedAppStore) hold(t *testing.T) func() {
	t.Helper()
	s.mu.Lock()
	s.entered, s.release = make(chan context.Context, 8), make(chan struct{})
	ch := s.release
	s.mu.Unlock()
	var once sync.Once
	release := func() { once.Do(func() { close(ch) }) }
	t.Cleanup(release)
	return release
}

type usageOwnedAppFixture struct {
	owned, legacy *projectUsageAssembly
	objects       *objectAssembly
	makeOwned     func(*projectUsageAssembly, *objectAssembly) (*projectUsageAssembly, error)
	base          *projectUsageAssembly
}

func newUsageOwnedAppFixture(t *testing.T, store *usageOwnedAppStore) usageOwnedAppFixture {
	t.Helper()
	cfg := testConfig(t, "1s")
	accounts, err := account.NewAuthority(store, cfg.AccountKeyring())
	if err != nil {
		t.Fatal(err)
	}
	life, err := createProjectLifecycleAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	base, err := createProjectUsageAuthorityWithLifecycle(store, accounts, life)
	if err != nil {
		t.Fatal(err)
	}
	objects := &objectAssembly{process: updateRootID[oc.Process](), guard: &object.ProcessGuard{}}
	makeOwned := func(p *projectUsageAssembly, o *objectAssembly) (*projectUsageAssembly, error) {
		return bindOwnedProjectUsage(cfg, store, accounts, p, nil, o)
	}
	owned, err := makeOwned(base, objects)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := bindProjectUsage(cfg, store, accounts, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if causes, sql := store.snapshot(); len(causes) != 0 || sql != 0 {
		t.Fatal("constructors performed transaction work before Object.Initialize")
	}
	t.Cleanup(func() { owned.usageCalls.StopAdmission() })
	return usageOwnedAppFixture{owned: owned, legacy: legacy, objects: objects, makeOwned: makeOwned, base: base}
}

func usageOwnedAppEntries(v *projectUsageAssembly, actor i.Actor, p i.ProjectID) []func() error {
	e := updateRootID[i.Execution]()
	return []func() error{
		func() error {
			_, err := v.reader.List(context.Background(), actor, uc.Query{Filter: uc.Filter{ProjectID: p}, Limit: 50})
			return err
		},
		func() error {
			_, err := v.reader.Aggregate(context.Background(), actor, uc.AggregateQuery{Filter: uc.Filter{ProjectID: p}, GroupBy: uc.ByDay, Limit: 50})
			return err
		},
		func() error { _, err := v.reader.GetExecutionSummary(context.Background(), actor, p, e); return err },
		func() error {
			_, err := v.reader.RebuildExecutionSummary(context.Background(), actor, p, e)
			return err
		},
	}
}

func TestUsageProjectCallAppConstruction(t *testing.T) {
	s := &usageOwnedAppStore{}
	v := newUsageOwnedAppFixture(t, s)
	if v.owned.reader != v.owned.usageCalls.pair.Service() || v.owned.projects != v.base.projects || v.owned.lifecycle != v.base.lifecycle || v.legacy.usageCalls != nil || v.owned.usageCalls.cleanup.Name() != pc.UsageParticipant {
		t.Fatal("original service, Life or legacy constructor changed")
	}
	if v.owned.usageCalls.Joined() {
		t.Fatal("unstarted owner was already stopped")
	}
	executions := &executionRuntimeAssembly{}
	root := &accountAssembly{executions: executions, usageCalls: v.owned.usageCalls}
	if got := root.works(); len(got) != 2 || got[0] != executions || got[1] != v.owned.usageCalls {
		t.Fatal("Model/Usage original retirement order changed")
	}
	foreign, err := createProjectLifecycleAuthority(&usageOwnedAppStore{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"nil-life", "foreign-life", "nil-guard", "already-bound"} {
		t.Run(name, func(t *testing.T) {
			base, objects := *v.base, *v.objects
			switch name {
			case "nil-life":
				base.lifecycle = nil
			case "foreign-life":
				base.lifecycle = foreign
			case "nil-guard":
				objects.guard = nil
			case "already-bound":
				base = *v.owned
			}
			if got, err := v.makeOwned(&base, &objects); err == nil || got != nil {
				t.Fatal("invalid original binding accepted", err)
			}
		})
	}
	if causes, sql := s.snapshot(); len(causes) != 0 || sql != 0 {
		t.Fatal("constructor rejection performed SQL")
	}
	v.owned.usageCalls.StopAdmission()
	if err := v.owned.usageCalls.Drain(context.Background()); err != nil || !v.owned.usageCalls.Joined() {
		t.Fatal("empty original owner did not join", err)
	}
}

func TestUsageProjectCallAppReadsHoldOriginalReturn(t *testing.T) {
	for index, name := range []string{"list", "aggregate", "summary_get", "summary_rebuild"} {
		t.Run(name, func(t *testing.T) {
			s := &usageOwnedAppStore{}
			v := newUsageOwnedAppFixture(t, s)
			release := s.hold(t)
			p := updateRootID[i.Project]()
			call := usageOwnedAppEntries(v.owned, variableAppActor(t), p)[index]
			done := variableAppAsync(t, release, call)
			ownedContext := variableAppReceive(t, s.entered)
			provider := &runtimeExternalProbe{}
			root := &accountAssembly{usageCalls: v.owned.usageCalls, projects: provider}
			root.StopAdmission()
			if ownedContext.Err() == nil {
				t.Fatal("Stop did not cancel original Usage context")
			}
			expired, cancel := context.WithCancel(context.Background())
			cancel()
			if root.Force(expired) == nil || root.Joined() || provider.forced != 0 {
				t.Fatal("Force retired provider before Store returned")
			}
			resources := &resources{stopping: true, accountService: root}
			guard := &runtimeGuardProbe{}
			resources.forceObjects(expired, guard)
			if guard.forced != 0 {
				t.Fatal("held Usage return released guard")
			}
			select {
			case <-done:
				t.Fatal("cancel replaced actual original return")
			default:
			}
			if call() == nil {
				t.Fatal("stopped Usage admitted fresh call")
			}
			release()
			if variableAppReceive(t, done) == nil {
				t.Fatal("controlled guard rejection became business success")
			}
			if err := root.Drain(context.Background()); err != nil || !root.Joined() {
				t.Fatal("real return did not join", err)
			}
			causes, sql := s.snapshot()
			if len(causes) != 1 || sql != 0 || causes[0].Details().Owner != "usage-call-register" || causes[0].Details().CheckpointRef != p.String() {
				t.Fatal("wrong registration or leaked business SQL")
			}
		})
	}
}

func TestUsageProjectCallAppUnknownAndLateInstall(t *testing.T) {
	s := &usageOwnedAppStore{unknown: true, attempt: updateRootID[f.TransactionAttempt]()}
	v := newUsageOwnedAppFixture(t, s)
	call := usageOwnedAppEntries(v.owned, variableAppActor(t), updateRootID[i.Project]())[0]
	original := variableAppFault(t, call(), f.CommitUnknown)
	if original.CauseID != s.attempt.String() || original.CommitState != f.Unknown {
		t.Fatal("raw registration identity lost")
	}
	root := &accountAssembly{constructing: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := root.Force(ctx); err != nil {
		t.Fatal(err)
	}
	release := s.hold(t)
	provider := &runtimeExternalProbe{}
	done := variableAppAsync(t, release, func() error {
		if root.install(context.Background(), func() { root.usageCalls = v.owned.usageCalls; root.projects = provider }) {
			return errors.New("late Usage install escaped original stopped root")
		}
		return nil
	})
	if got := variableAppReceive(t, s.entered); got != ctx {
		t.Fatal("confirmation replaced original Force budget")
	}
	if root.Joined() || provider.forced != 0 {
		t.Fatal("held confirmation released provider")
	}
	cancel()
	select {
	case <-done:
		t.Fatal("confirmation cancellation substituted for return")
	default:
	}
	release()
	if err := variableAppReceive(t, done); err != nil {
		t.Fatal(err)
	}
	root.constructionDone()
	if root.Joined() || v.owned.usageCalls.Joined() || provider.forced != 0 || root.forced != ctx {
		t.Fatal("late retained owner or original budget lost")
	}
	causes, sql := s.snapshot()
	if len(causes) != 2 || sql != 0 || !reflect.DeepEqual(causes[0].Details(), causes[1].Details()) {
		t.Fatal("confirmation changed first cause or ran SQL")
	}
}
