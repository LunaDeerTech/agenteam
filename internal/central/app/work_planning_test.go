package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
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

func workRootBundle(t *testing.T, store workRootStores, transitions ...bool) *workPlanningAssembly {
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
	var capability []*project.Authority
	count := 3
	if len(transitions) == 1 && transitions[0] {
		capability = []*project.Authority{projects}
		count = 4
	}
	b, err := createWorkPlanning(cfg, store, authority, accounts, journal, events, capability...)
	if err != nil || b == nil {
		t.Fatal("pure actual Work construction", err)
	}
	if b.structure == nil || b.structureReader == nil || b.tasks == nil || b.taskReader == nil || b.blockers == nil || b.blockerReader == nil || len(b.commands) != count || (b.transitions != nil) != (count == 4) {
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
	if count == 4 {
		for _, invalid := range [][]*project.Authority{{nil}, {projects, projects}} {
			if got, err := createWorkPlanning(cfg, store, authority, accounts, journal, events, invalid...); err == nil || got != nil {
				t.Fatal("invalid optional transition assembly accepted")
			}
		}
		missing := events
		missing.transitions = wc.TaskTransitionEvents{}
		if got, err := createWorkPlanning(cfg, store, authority, accounts, journal, missing, projects); err == nil || got != nil {
			t.Fatal("missing transition factory accepted")
		}
	}
	return b
}

func TestWorkPlanningTransitionActualCallMustJoin(t *testing.T) {
	s := &workRootBlockedStore{entered: make(chan context.Context, 1), release: make(chan struct{})}
	b := workRootBundle(t, s, true)
	actor, err := id.NewHuman(updateRootID[id.User](), updateRootID[id.Session]())
	if err != nil {
		t.Fatal(err)
	}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(s.release) }) })
	done := make(chan error, 1)
	go func() {
		_, err := b.transitions.LookupTaskTransition(context.Background(), actor, wc.TaskTransitionLookupRequest{
			ProjectID: updateRootID[id.Project](), Command: wc.TaskTransitionTransfer,
			IdempotencyKey: "transition-lookup", SemanticDigest: f.Digest("sha256:" + strings.Repeat("b", 64)),
		})
		done <- err
	}()
	var original context.Context
	select {
	case original = <-s.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("transition did not enter the original Store")
	}
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.Force(expired); !errors.Is(err, context.Canceled) || b.Joined() {
		t.Fatal("fourth service tail was omitted", err)
	}
	select {
	case <-original.Done():
	default:
		t.Fatal("transition admission not cancelled")
	}
	select {
	case <-done:
		t.Fatal("transaction falsely joined")
	default:
	}
	release.Do(func() { close(s.release) })
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("held failure returned success")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("original transition did not return")
	}
	if err := b.Drain(context.Background()); err != nil || !b.Joined() {
		t.Fatal("actual transition completion not joined", err)
	}
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

func TestWorkPlanningRootOrderingAndLateInstall(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "drain", true: "force"}[force], func(t *testing.T) {
			planning, projects, mail, core := &b04Work{}, &b04Work{}, &b04Work{}, &b04Work{}
			a := &accountAssembly{planning: planning, projects: projects, mail: mail, core: core}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var order []string
			for _, entry := range []struct {
				name  string
				calls *b04Work
			}{
				{"work", planning}, {"project", projects}, {"mail", mail}, {"account", core},
			} {
				join := func(got context.Context) error {
					if got != ctx || !planning.stopped.Load() || !projects.stopped.Load() || !mail.stopped.Load() || !core.stopped.Load() {
						t.Fatal("provider wait preceded global admission stop or renewed budget")
					}
					order = append(order, entry.name)
					entry.calls.joined.Store(true)
					return nil
				}
				entry.calls.drain, entry.calls.force = join, join
			}
			var err error
			if force {
				err = a.Force(ctx)
			} else {
				err = a.Drain(ctx)
			}
			if err != nil || !a.Joined() || !slices.Equal(order, []string{"work", "project", "mail", "account"}) {
				t.Fatal("Work retired after its Activity dependency", order, err)
			}
		})
	}
	t.Run("work-not-joined-preserves-activity", func(t *testing.T) {
		planning := &b04Work{drain: func(context.Context) error { return context.DeadlineExceeded }}
		core := &b04Work{drain: func(context.Context) error { t.Fatal("retired Activity while Work is owned"); return nil }}
		a := &accountAssembly{planning: planning, core: core}
		if err := a.Drain(context.Background()); !errors.Is(err, context.DeadlineExceeded) || a.Joined() {
			t.Fatal("unfinished Work disappeared", err)
		}
	})
	t.Run("force-before-work-install", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		a := &accountAssembly{constructing: true}
		if err := a.Force(ctx); err != nil {
			t.Fatal(err)
		}
		first, second, third := &workDrainObserver{err: ctx.Err()}, &workDrainObserver{err: ctx.Err()}, &workDrainObserver{err: ctx.Err()}
		planning := &workPlanningAssembly{commands: []workCommandCalls{first, second, third}}
		if a.install(context.Background(), func() { a.planning = planning }) {
			t.Fatal("late Work installation reopened admission")
		}
		a.constructionDone()
		for _, child := range []*workDrainObserver{first, second, third} {
			if !child.stopped || child.seen != ctx {
				t.Fatal("late Work escaped original expired Force")
			}
		}
		if a.Joined() {
			t.Fatal("failed concrete drain treated as join")
		}
	})
	t.Run("partial-sink-retained", func(t *testing.T) {
		planning, sink := &b04Work{}, &b04Work{}
		a := &accountAssembly{planning: planning, sink: sink}
		calls := a.works()
		if len(calls) != 2 || calls[0] != planning || calls[1] != sink {
			t.Fatal("Work hid partial Account sink")
		}
	})
}

func TestWorkPlanningRootRouteOwnership(t *testing.T) {
	const p = "/api/v1/projects/01900000-0000-7000-8000-000000000001"
	const id = "01900000-0000-7000-8000-000000000002"
	var cases []struct {
		path  string
		owned bool
	}
	for _, path := range []string{
		"/milestones", "/milestones/" + id, "/milestones/" + id + "/reorder",
		"/sprints", "/sprints/" + id, "/sprints/" + id + "/reorder", "/structure-commands/lookup",
		"/tasks", "/tasks/" + id, "/tasks/" + id + "/reorder", "/task-commands/lookup",
		"/tasks/" + id + "/blockers", "/tasks/" + id + "/blockers/resolve", "/tasks/" + id + "/blocker-commands/lookup",
	} {
		cases = append(cases, struct {
			path  string
			owned bool
		}{p + path, true})
	}
	for _, path := range []string{
		p, p + "/", p + "/commands/lookup", p + "/archive", p + "/models", p + "/model-selection", p + "/model-credentials", p + "/audit", p + "/usage", p + "/milestones/" + id + "/extra", "/api/v1/projects/resolve", "/api/v1/auth/session", "/api/v1/system/models",
	} {
		cases = append(cases, struct {
			path  string
			owned bool
		}{path, false})
	}
	for _, tc := range cases {
		for _, method := range []string{"GET", "HEAD", "POST", "PATCH", "DELETE"} {
			r := httptest.NewRequest(method, tc.path+"?untouched=private", strings.NewReader("body"))
			body, u := r.Body, r.URL
			calls := 0
			child := func(owned bool) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, got *http.Request) {
					calls++
					if got != r || got.Body != body || got.URL != u || tc.owned != owned {
						t.Fatal("routing changed request or stole another domain", method, tc.path)
					}
					w.WriteHeader(http.StatusNoContent)
				})
			}
			w := httptest.NewRecorder()
			workPlanningRoutes(child(false), child(true)).ServeHTTP(w, r)
			if calls != 1 || w.Code != http.StatusNoContent {
				t.Fatal("multiple or missing handlers")
			}
		}
	}
}
