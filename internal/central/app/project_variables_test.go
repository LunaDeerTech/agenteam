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
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

func variableRootBundle(t *testing.T, store workRootStores) *projectVariablesAssembly {
	t.Helper()
	cfg := testConfig(t, "1s")
	accounts, e := account.NewAuthority(store, cfg.AccountKeyring())
	if e != nil {
		t.Fatal(e)
	}
	projects, e := createProjectUsage(cfg, store, accounts)
	if e != nil || projects.variables == nil {
		t.Fatal("single authority construction", e)
	}
	catalog := ec.NewCatalog()
	events, e := vc.RegisterVariableEvents(catalog)
	if e != nil {
		t.Fatal(e)
	}
	auditor, e := audit.New(store, cfg.CursorKeyring(), audit.Authorizations{Accounts: accounts, Sessions: accounts, System: accounts, Projects: projects.projects})
	if e != nil {
		t.Fatal(e)
	}
	processes := projectCommandProcess{accountProcessAuthority{process: updateRootID[ac.Process](), guard: &object.ProcessGuard{}}}
	journal, e := outbox.New(store, catalog, outbox.Authorizations{Producers: map[ec.StableName]oc.ProducerAuthority{vc.VariableProducer: projects.variables}, Projects: projects.projects, Sessions: accounts, System: accounts, Audit: auditor, Processes: processes, Cursors: cfg.CursorKeyring()})
	if e != nil {
		t.Fatal(e)
	}
	b, e := createProjectVariables(cfg, store, projects.variables, projects.projects, accounts, auditor, journal, events)
	if e != nil || b.service == nil {
		t.Fatal(e)
	}
	for _, db := range []database{nil, (*workRootStore)(nil), &unitDatabase{}, &workRootStore{}} {
		if v, e := createProjectVariables(cfg, db, projects.variables, projects.projects, accounts, auditor, journal, events); e == nil || v != nil {
			t.Fatal("nil/different Store admitted")
		}
	}
	if v, e := createProjectVariables(cfg, store, projects.variables, projects.projects, accounts, auditor, journal, vc.VariableEvents{}); e == nil || v != nil {
		t.Fatal("missing catalog")
	}
	return b
}
func TestProjectVariablesPureConstructionAndActualReadLookupJoin(t *testing.T) {
	store := &workRootBlockedStore{entered: make(chan context.Context, 3), release: make(chan struct{})}
	bundle := variableRootBundle(t, store)
	actor, _ := id.NewHuman(updateRootID[id.User](), updateRootID[id.Session]())
	p := updateRootID[id.Project]()
	target := updateRootID[id.ProjectVariable]()
	q, _ := vc.NewVariableCommandLookupRequest(vc.VariableCommandLookupFields{ProjectID: p, Command: vc.DeleteCommand, IdempotencyKey: "lookup", SemanticDigest: f.Digest("sha256:" + strings.Repeat("a", 64))})
	done := make(chan error, 3)
	var once sync.Once
	release := func() { once.Do(func() { close(store.release) }) }
	t.Cleanup(release)
	go func() { _, e := bundle.service.GetVariable(context.Background(), actor, p, target); done <- e }()
	go func() {
		_, e := bundle.service.ListVariables(context.Background(), actor, p, f.DefaultPageRequest())
		done <- e
	}()
	go func() { _, e := bundle.service.LookupVariableCommand(context.Background(), actor, q); done <- e }()
	var calls []context.Context
	for range 3 {
		select {
		case ctx := <-store.entered:
			calls = append(calls, ctx)
		case <-time.After(2 * time.Second):
			t.Fatal("real library call did not enter Store")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := bundle.Force(ctx); !errors.Is(e, context.Canceled) || bundle.Joined() {
		t.Fatal("Force abandoned actual DB call", e)
	}
	for _, ctx := range calls {
		if ctx.Err() != context.Canceled {
			t.Fatal("uncancelled call")
		}
	}
	select {
	case <-done:
		t.Fatal("released unfinished transaction")
	default:
	}
	release()
	for range 3 {
		select {
		case e := <-done:
			if e == nil {
				t.Fatal("failure became success")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("call did not join")
		}
	}
	if e := bundle.Drain(context.Background()); e != nil || !bundle.Joined() {
		t.Fatal("actual join lost", e)
	}
}
func TestProjectVariablesRootOrderAndLateInstall(t *testing.T) {
	variables, planning, projects, mail, core := &b04Work{}, &b04Work{}, &b04Work{}, &b04Work{}, &b04Work{}
	assembly := &accountAssembly{variables: variables, planning: planning, projects: projects, mail: mail, core: core}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	order := []string{}
	for _, v := range []struct {
		name  string
		calls *b04Work
	}{{"variables", variables}, {"work", planning}, {"project", projects}, {"mail", mail}, {"account", core}} {
		v.calls.drain = func(got context.Context) error {
			if got != ctx || !variables.stopped.Load() || !planning.stopped.Load() || !projects.stopped.Load() || !mail.stopped.Load() || !core.stopped.Load() {
				t.Fatal("provider before global stop or changed budget")
			}
			order = append(order, v.name)
			v.calls.joined.Store(true)
			return nil
		}
	}
	if e := assembly.Drain(ctx); e != nil || !assembly.Joined() || !slices.Equal(order, []string{"variables", "work", "project", "mail", "account"}) {
		t.Fatal("dependency order", order, e)
	}
	expired, cancelExpired := context.WithCancel(context.Background())
	cancelExpired()
	assembly = &accountAssembly{constructing: true}
	if e := assembly.Force(expired); e != nil {
		t.Fatal(e)
	}
	observer := &workDrainObserver{err: expired.Err()}
	late := &projectVariablesAssembly{calls: observer}
	if assembly.install(context.Background(), func() { assembly.variables = late }) || !observer.stopped || observer.seen != expired || assembly.Joined() {
		t.Fatal("late installation lost original Force")
	}
}
func TestProjectVariablesRootPreciseRouteOwnership(t *testing.T) {
	const base = "/api/v1/projects/01900000-0000-7000-8000-000000000001"
	for _, tc := range []struct {
		path  string
		owned bool
	}{{base + "/variables", true}, {base + "/variables/01900000-0000-7000-8000-000000000002", true}, {base + "/variables/commands/lookup", true}, {base + "/variables/commands/other", false}, {base + "/variables/", false}, {base + "/model-credentials", false}, {base + "/models", false}, {base + "/tasks", false}, {base + "/audit", false}, {base + "/usage", false}, {"/api/v1/auth/session", false}} {
		r := httptest.NewRequest("POST", tc.path+"?untouched=true", strings.NewReader("original"))
		count := 0
		child := func(owned bool) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, got *http.Request) {
				count++
				if got != r || owned != tc.owned {
					t.Fatal("changed request or wrong route")
				}
				w.WriteHeader(204)
			})
		}
		w := httptest.NewRecorder()
		projectVariablesRoutes(child(false), child(true)).ServeHTTP(w, r)
		if count != 1 || w.Code != 204 {
			t.Fatal("route call count")
		}
	}
}
