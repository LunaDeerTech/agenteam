package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	ac "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// A constructor touching SQL would dereference this deliberately nil Store.
type workRootStore struct{ *postgres.Store }

type workRootStores interface {
	database
	account.Store
	audit.Store
	project.Store
	work.Store
	outbox.Store
}

func workRootBundle(t *testing.T, store workRootStores) *workPlanningAssembly {
	t.Helper()
	cfg := testConfig(t, "1s")
	accounts, err := account.NewAuthority(store, cfg.AccountKeyring())
	if err != nil {
		t.Fatal(err)
	}
	projects, err := project.NewAuthority(store, project.AuthorityDependencies{Sessions: accounts, Routes: accounts})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := createWorkPlanningAuthority(store, projects)
	if err != nil {
		t.Fatal(err)
	}
	catalog := ec.NewCatalog()
	events, err := defineWorkPlanningEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	auditor, err := audit.New(store, cfg.CursorKeyring(), audit.Authorizations{Accounts: accounts, Sessions: accounts, System: accounts, Projects: projects})
	if err != nil {
		t.Fatal(err)
	}
	processes := projectCommandProcess{accountProcessAuthority{process: updateRootID[ac.Process](), guard: &object.ProcessGuard{}}}
	journal, err := outbox.New(store, catalog, outbox.Authorizations{Producers: map[ec.StableName]oc.ProducerAuthority{wc.WorkProducer: authority}, Projects: projects, Sessions: accounts, System: accounts, Audit: auditor, Processes: processes, Cursors: cfg.CursorKeyring()})
	if err != nil {
		t.Fatal(err)
	}
	b, err := createWorkPlanning(cfg, store, authority, accounts, journal, events)
	if err != nil || b == nil {
		t.Fatal("pure actual Work construction", err)
	}
	if b.structure == nil || b.structureReader == nil || b.tasks == nil || b.taskReader == nil || b.blockers == nil || b.blockerReader == nil || len(b.commands) != 3 {
		t.Fatal("incomplete Work bindings")
	}
	for _, db := range []database{nil, (*workRootStore)(nil), &unitDatabase{}, &workRootStore{}} {
		if got, err := createWorkPlanning(cfg, db, authority, accounts, journal, events); err == nil || got != nil {
			t.Fatal("nil or different Work store accepted")
		}
	}
	if got, err := createWorkPlanning(cfg, store, authority, accounts, journal, workPlanningEvents{}); err == nil || got != nil {
		t.Fatal("missing typed event binding accepted")
	}
	if got, err := createWorkPlanningAuthority(store, nil); err == nil || got != nil {
		t.Fatal("missing Project authority accepted")
	}
	return b
}

func TestWorkPlanningPureConstructionAndDrain(t *testing.T) {
	b := workRootBundle(t, &workRootStore{})
	if b.Joined() {
		t.Fatal("construction pretended retirement")
	}
	b.StopAdmission()
	if b.Joined() {
		t.Fatal("cancellation pretended actual drain")
	}
	if err := b.Drain(context.Background()); err != nil || !b.Joined() {
		t.Fatal("empty services did not drain", err)
	}
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			b.StopAdmission()
			if b.Force(context.Background()) != nil || b.Drain(context.Background()) != nil || !b.Joined() {
				t.Error("concurrent retirement lost actual join")
			}
		})
	}
	group.Wait()
}

type workRootBlockedStore struct {
	workRootStore
	entered chan context.Context
	release chan struct{}
}

func (s *workRootBlockedStore) WithinTx(ctx context.Context, _ f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
	s.entered <- ctx
	<-s.release
	return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
}

func TestWorkPlanningAllThreeActualCallsMustJoin(t *testing.T) {
	s := &workRootBlockedStore{entered: make(chan context.Context, 3), release: make(chan struct{})}
	b := workRootBundle(t, s)
	actor, err := id.NewHuman(updateRootID[id.User](), updateRootID[id.Session]())
	if err != nil {
		t.Fatal(err)
	}
	projectID := updateRootID[id.Project]()
	digest := f.Digest("sha256:" + strings.Repeat("a", 64))
	done := make(chan error, 3)
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(s.release) }) })
	go func() {
		_, err := b.structure.LookupCommand(context.Background(), actor, wc.CommandLookupRequest{ProjectID: projectID, Command: wc.MilestoneUpdate, Key: "structure-lookup", Semantic: digest})
		done <- err
	}()
	go func() {
		_, err := b.tasks.LookupTaskCommand(context.Background(), actor, wc.TaskCommandLookupRequest{ProjectID: projectID, Command: wc.TaskCommandUpdate, IdempotencyKey: "task-lookup", SemanticDigest: digest})
		done <- err
	}()
	go func() {
		_, err := b.blockers.LookupTaskBlockerCommand(context.Background(), actor, wc.TaskBlockerCommandLookupRequest{ProjectID: projectID, Command: wc.TaskBlockerCommandAdd, IdempotencyKey: "blocker-lookup", SemanticDigest: digest})
		done <- err
	}()
	var calls []context.Context
	for range 3 {
		select {
		case ctx := <-s.entered:
			calls = append(calls, ctx)
		case <-time.After(2 * time.Second):
			t.Fatal("actual command did not enter its transaction")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.Force(ctx); !errors.Is(err, context.Canceled) || b.Joined() {
		t.Fatal("expired force abandoned a still-owned call", err)
	}
	for _, call := range calls {
		select {
		case <-call.Done():
		default:
			t.Fatal("force failed to cancel one of the three services")
		}
	}
	select {
	case <-done:
		t.Fatal("a call returned before its actual transaction completed")
	default:
	}
	release.Do(func() { close(s.release) })
	for range 3 {
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("blocked failure became success")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("owned transaction tail did not finish")
		}
	}
	if err := b.Drain(context.Background()); err != nil || !b.Joined() {
		t.Fatal("actual completion was not retained", err)
	}
}

type workDrainObserver struct {
	stopped bool
	seen    context.Context
	err     error
}

func (o *workDrainObserver) Stop() { o.stopped = true }
func (o *workDrainObserver) Drain(ctx context.Context) error {
	o.seen = ctx
	return o.err
}

func TestWorkPlanningForceAttemptsEveryDrainWithOriginalContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	first, second, third := &workDrainObserver{err: context.Canceled}, &workDrainObserver{err: errors.New("second drain failed")}, &workDrainObserver{}
	b := &workPlanningAssembly{commands: []workCommandCalls{first, second, third}}
	if err := b.Force(ctx); !errors.Is(err, context.Canceled) || !errors.Is(err, second.err) || b.Joined() {
		t.Fatal("force lost failures or pretended successful join", err)
	}
	for _, child := range []*workDrainObserver{first, second, third} {
		if !child.stopped || child.seen != ctx {
			t.Fatal("force skipped a child or gave it a new shutdown budget")
		}
	}
}
