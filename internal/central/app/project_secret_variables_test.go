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
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// Constructor-only Store doubles cannot authorize or execute a transaction.
// The lifecycle test below separately holds actual service calls at this port.
func secretVariableRootBundle(t *testing.T, store workRootStores) *projectSecretVariablesAssembly {
	t.Helper()
	cfg := testConfig(t, "1s")
	accounts, err := account.NewAuthority(store, cfg.AccountKeyring())
	if err != nil {
		t.Fatal(err)
	}
	projects, err := createProjectUsage(cfg, store, accounts)
	if err != nil {
		t.Fatal(err)
	}
	writes, err := createProjectSecretWriteAuthority(store, projects.projects)
	if err != nil {
		t.Fatal(err)
	}
	models, err := model.NewAuthority(store, model.Authorizations{Sessions: accounts, System: accounts, Projects: projects.projects})
	if err != nil {
		t.Fatal(err)
	}
	usage, err := model.NewSecretUsageRouter(models, accounts)
	if err != nil {
		t.Fatal(err)
	}
	auditor, err := audit.New(store, cfg.CursorKeyring(), audit.Authorizations{Accounts: accounts, Sessions: accounts, System: accounts, Projects: projects.projects})
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := createSecret(cfg, store, auditor, accounts, usage, projects.projects, writes)
	if err != nil || secrets.Status().Available {
		t.Fatal("pure D04 construction touched initialization", err)
	}
	if v, err := createSecret(cfg, store, auditor, accounts, usage, projects.projects, nil); err == nil || v != nil {
		t.Fatal("missing immutable D04 write authority accepted")
	}
	catalog := event.NewCatalog()
	if _, err := vc.RegisterVariableEvents(catalog); err != nil {
		t.Fatal(err)
	}
	events, err := vc.RegisterSecretVariableEvents(catalog)
	if err != nil {
		t.Fatal("ordinary and Secret event registration must coexist", err)
	}
	processes := projectCommandProcess{accountProcessAuthority{process: updateRootID[ac.Process](), guard: &object.ProcessGuard{}}}
	journal, err := outbox.New(store, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{vc.VariableProducer: projects.variables}, Projects: projects.projects, Sessions: accounts, System: accounts, Audit: auditor, Processes: processes, Cursors: cfg.CursorKeyring()})
	if err != nil {
		t.Fatal(err)
	}
	b, err := createProjectSecretVariables(cfg, store, projects.variables, writes, secrets, projects.projects, accounts, auditor, journal, events)
	if err != nil || b == nil || b.service == nil {
		t.Fatal("actual Secret Owner construction", err)
	}
	for _, db := range []database{nil, (*workRootStore)(nil), &unitDatabase{}, &workRootStore{}} {
		if v, err := createProjectSecretVariables(cfg, db, projects.variables, writes, secrets, projects.projects, accounts, auditor, journal, events); err == nil || v != nil {
			t.Fatal("nil or foreign Store accepted")
		}
	}
	for _, db := range []database{nil, (*workRootStore)(nil), &unitDatabase{}} {
		if v, err := createProjectSecretWriteAuthority(db, projects.projects); err == nil || v != nil {
			t.Fatal("missing write authority Store accepted")
		}
	}
	if v, err := createProjectSecretWriteAuthority(store, nil); err == nil || v != nil {
		t.Fatal("missing Project accepted")
	}
	foreignWrites, err := createProjectSecretWriteAuthority(&workRootStore{}, projects.projects)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := createProjectSecretVariables(cfg, store, projects.variables, foreignWrites, secrets, projects.projects, accounts, auditor, journal, events); err == nil || v != nil {
		t.Fatal("foreign write authority Store accepted")
	}
	if v, err := createProjectSecretVariables(cfg, store, projects.variables, writes, secrets, projects.projects, accounts, auditor, journal, vc.SecretVariableEvents{}); err == nil || v != nil {
		t.Fatal("missing Secret catalog accepted")
	}
	if v, err := createProjectSecretVariables(cfg, store, projects.variables, writes, nil, projects.projects, accounts, auditor, journal, events); err == nil || v != nil {
		t.Fatal("typed nil D04 accepted")
	}
	if h, err := projectSecretVariablesHandler(b, nil, cfg.PublicOrigin()); err == nil || h != nil {
		t.Fatal("missing Account boundary accepted")
	}
	return b
}

func TestProjectSecretVariablesPureConstructionAndActualJoin(t *testing.T) {
	store := &workRootBlockedStore{entered: make(chan context.Context, 3), release: make(chan struct{})}
	bundle := secretVariableRootBundle(t, store)
	actor, err := id.NewHuman(updateRootID[id.User](), updateRootID[id.Session]())
	if err != nil {
		t.Fatal(err)
	}
	project, target := updateRootID[id.Project](), updateRootID[id.ProjectVariable]()
	version := f.Version(1)
	query, err := vc.NewSecretVariableCommandLookupRequest(vc.SecretVariableCommandLookupFields{ProjectID: project, Command: vc.SecretDeleteCommand, TargetID: target, ExpectedVersion: &version, IdempotencyKey: "root-secret-lookup"})
	if err != nil {
		t.Fatal(err)
	}
	meta := f.CommandMeta{RequestID: updateRootID[f.Request](), IdempotencyKey: "root-secret-delete", ExpectedVersion: &version}
	done := make(chan error, 3)
	var once sync.Once
	release := func() { once.Do(func() { close(store.release) }) }
	t.Cleanup(release)
	go func() {
		_, err := bundle.service.GetSecretVariable(context.Background(), actor, project, target)
		done <- err
	}()
	go func() {
		_, err := bundle.service.LookupSecretVariableCommand(context.Background(), actor, query)
		done <- err
	}()
	go func() {
		_, err := bundle.service.DeleteSecretVariable(context.Background(), actor, meta, project, target)
		done <- err
	}()
	var calls []context.Context
	for range 3 {
		select {
		case ctx := <-store.entered:
			calls = append(calls, ctx)
		case <-time.After(2 * time.Second):
			t.Fatal("actual Secret call did not enter controlled Store")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := bundle.Force(ctx); !errors.Is(err, context.Canceled) || bundle.Joined() {
		t.Fatal("Force confused cancellation with actual return")
	}
	for _, call := range calls {
		if call.Err() != context.Canceled {
			t.Fatal("admitted Secret call was not cancelled")
		}
	}
	select {
	case <-done:
		t.Fatal("held Store call was abandoned")
	default:
	}
	release()
	for range 3 {
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("controlled failure became success")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("released call did not actually return")
		}
	}
	if err := bundle.Drain(context.Background()); err != nil || !bundle.Joined() {
		t.Fatal("actual join lost", err)
	}
	if _, err := bundle.service.GetSecretVariable(context.Background(), actor, project, target); err == nil {
		t.Fatal("stopped service admitted a new read")
	}
}

func TestProjectSecretVariablesRootOrderAndLateInstall(t *testing.T) {
	variables, secrets, planning, projects, mail, core := &b04Work{}, &b04Work{}, &b04Work{}, &b04Work{}, &b04Work{}, &b04Work{}
	assembly := &accountAssembly{variables: variables, secretVariables: secrets, planning: planning, projects: projects, mail: mail, core: core}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	order := []string{}
	for _, entry := range []struct {
		name string
		work *b04Work
	}{{"ordinary", variables}, {"secret", secrets}, {"work", planning}, {"project", projects}, {"mail", mail}, {"account", core}} {
		entry.work.drain = func(got context.Context) error {
			if got != ctx || !variables.stopped.Load() || !secrets.stopped.Load() || !core.stopped.Load() {
				t.Fatal("provider drain preceded global stop or renewed context")
			}
			order = append(order, entry.name)
			entry.work.joined.Store(true)
			return nil
		}
	}
	if err := assembly.Drain(ctx); err != nil || !assembly.Joined() || !slices.Equal(order, []string{"ordinary", "secret", "work", "project", "mail", "account"}) {
		t.Fatal("Secret calls did not retire before their providers")
	}
	expired, cancelExpired := context.WithCancel(context.Background())
	cancelExpired()
	assembly = &accountAssembly{constructing: true}
	if err := assembly.Force(expired); err != nil {
		t.Fatal(err)
	}
	observer := &workDrainObserver{err: expired.Err()}
	late := &projectSecretVariablesAssembly{calls: observer}
	if assembly.install(context.Background(), func() { assembly.secretVariables = late }) || !observer.stopped || observer.seen != expired || late.Joined() || assembly.Joined() {
		t.Fatal("late install lost the original Force context or claimed join")
	}
}

func TestProjectSecretVariablesRootPreciseRouteOwnership(t *testing.T) {
	const base = "/api/v1/projects/01900000-0000-7000-8000-000000000001"
	for _, tc := range []struct{ path, owner string }{
		{base + "/secret-variables", "secret"}, {base + "/secret-variables/01900000-0000-7000-8000-000000000002", "secret"}, {base + "/secret-variables/commands/lookup", "secret"},
		{base + "/secret-variables/commands/other", "existing"}, {base + "/secret-variables/", "existing"},
		{base + "/variables", "ordinary"}, {base + "/variables/01900000-0000-7000-8000-000000000002", "ordinary"}, {base + "/variables/commands/lookup", "ordinary"},
		{base + "/model-credentials", "existing"}, {base + "/audit", "existing"}, {base + "/skills", "existing"}, {"/api/v1/auth/session", "existing"},
	} {
		r := httptest.NewRequest("POST", tc.path+"?untouched=true", strings.NewReader("original"))
		body, url, count := r.Body, r.URL, 0
		child := func(owner string) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, got *http.Request) {
				count++
				if got != r || got.Body != body || got.URL != url || owner != tc.owner {
					t.Fatal("route changed input or dispatched to wrong owner")
				}
				w.WriteHeader(http.StatusNoContent)
			})
		}
		w := httptest.NewRecorder()
		projectSecretVariablesRoutes(projectVariablesRoutes(child("existing"), child("ordinary")), child("secret")).ServeHTTP(w, r)
		if count != 1 || w.Code != http.StatusNoContent {
			t.Fatal("route dispatch was not singular")
		}
	}
}
