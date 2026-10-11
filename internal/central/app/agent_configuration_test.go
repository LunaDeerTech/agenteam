package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	agentc "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type agentConfigurationRootStore struct {
	*postgres.Store
	entered chan context.Context
	release chan struct{}
	unknown bool
	attempt f.ID[f.TransactionAttempt]
	called  int
}

func (s *agentConfigurationRootStore) WithinTx(ctx context.Context, cause f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
	s.called++
	if s.entered != nil {
		s.entered <- ctx
		<-s.release
	}
	if s.unknown {
		return f.UnknownResult(s.attempt, cause)
	}
	return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
}

// These are pure real constructors, with an intentionally unbound ProcessGuard
// and Object service. They cannot establish a live install source, a published
// Project or a successful Agent mutation. The Store never invokes SQL callbacks.
func agentConfigurationRootBundle(t *testing.T, store *agentConfigurationRootStore) (*agentConfigurationAssembly, *agentInstallWork, *agent.Authority) {
	t.Helper()
	cfg := testConfig(t, "1s")
	accounts, err := account.NewAuthority(store, cfg.AccountKeyring())
	if err != nil {
		t.Fatal(err)
	}
	projects, err := createProjectUsageAuthority(store, accounts)
	if err != nil {
		t.Fatal(err)
	}
	agents, err := createAgentConfigurationAuthority(store, projects.projects)
	if err != nil {
		t.Fatal(err)
	}
	process, err := f.NewID[objc.Process]()
	if err != nil {
		t.Fatal(err)
	}
	objects := &objectAssembly{process: process, guard: &object.ProcessGuard{}, service: &object.Service{}}
	install, err := createAgentInstall(store, objects)
	if err != nil {
		t.Fatal(err)
	}
	content, err := createKnowledgeSkillAuthoritiesWithInstall(store, projects, install.authority)
	if err != nil {
		t.Fatal(err)
	}
	skills, err := createSkills(context.Background(), content.skills, objects)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		skills.Stop()
		if err := skills.Drain(context.Background()); err != nil {
			t.Error(err)
		}
	})
	models, err := model.NewAuthority(store, model.Authorizations{Sessions: accounts, System: accounts, Projects: projects.projects})
	if err != nil {
		t.Fatal(err)
	}
	auditor, err := audit.New(store, cfg.CursorKeyring(), audit.Authorizations{Accounts: accounts, Sessions: accounts, System: accounts, Projects: content.audit, Models: models})
	if err != nil {
		t.Fatal(err)
	}
	catalog := event.NewCatalog()
	if _, err = defineWorkPlanningEvents(catalog); err != nil {
		t.Fatal(err)
	}
	events, err := agentc.RegisterAgentEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	producer, err := agent.NewEventAuthority(agents, events)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := outbox.New(store, catalog, outbox.Authorizations{
		Producers: map[event.StableName]oc.ProducerAuthority{agentc.AgentProducer: producer},
		Projects:  projects.projects, Sessions: accounts, System: accounts, Audit: auditor,
		Processes: outboxProcessAuthority{process: process, guard: objects.guard}, Cursors: cfg.CursorKeyring(),
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := createAgentConfiguration(store, projects.projects, accounts, agents, producer, models, skills, content.skills, install, auditor, journal)
	if err != nil || b == nil || b.service == nil || b.registry == nil || store.called != 0 {
		t.Fatal("pure binding performed SQL or omitted a provider", err)
	}
	t.Cleanup(func() {
		if err := b.Drain(context.Background()); err != nil {
			t.Error(err)
		}
		if err := install.Drain(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if _, err = createAgentDirectoryWithAuthority(cfg, store, agents); err != nil {
		t.Fatal("same Agent authority did not construct directory", err)
	}
	executions, err := createExecutionRuntimeAuthorities(store, projects.projects, objects.guard, agents)
	if err != nil || executions.agents != agents {
		t.Fatal("optional Runtime replaced the original Agent authority", err)
	}
	if _, err = createAgentRuntimeProviders(store, projects.projects, accounts, content.skills, executions, b.registry); err != nil {
		t.Fatal("optional Runtime rejected the same real Registry", err)
	}
	return b, install, agents
}

func TestAgentConfigurationRootCompositionAndRequiredProviders(t *testing.T) {
	store := &agentConfigurationRootStore{}
	b, install, _ := agentConfigurationRootBundle(t, store)
	for _, db := range []database{nil, (*agentConfigurationRootStore)(nil), &unitDatabase{}} {
		if got, err := createAgentConfigurationAuthority(db, nil); err == nil || got != nil {
			t.Fatal("missing Store/Project accepted")
		}
		if got, err := createAgentInstall(db, nil); err == nil || got != nil {
			t.Fatal("missing original guard accepted")
		}
	}
	if got, err := createAgentConfiguration(store, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil); err == nil || got != nil {
		t.Fatal("incomplete configuration graph accepted")
	}
	if got, err := agentConfigurationHandler(b, nil, "https://example.test"); err == nil || got != nil {
		t.Fatal("HTTP accepted absent Account boundary")
	}
	if b.Joined() || install.Joined() {
		t.Fatal("unretired pure graph reported joined")
	}
	if err := b.Drain(context.Background()); err != nil || !b.Joined() {
		t.Fatal("original Agent owner did not retire", err)
	}
	if err := install.Drain(context.Background()); err != nil || !install.Joined() {
		t.Fatal("original Install authority did not retire", err)
	}
	if err := b.Initialize(context.Background()); err == nil || store.called != 0 {
		t.Fatal("retired graph started registration")
	}
}

func TestAgentConfigurationRootStartupAndCallJoin(t *testing.T) {
	store := &agentConfigurationRootStore{entered: make(chan context.Context, 1), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(store.release) }) }
	defer release()
	b, install, _ := agentConfigurationRootBundle(t, store)
	returned := make(chan error, 1)
	go func() { returned <- b.Initialize(context.Background()) }()
	var original context.Context
	select {
	case original = <-store.entered:
	case err := <-returned:
		t.Fatal("initialization returned before its original transaction", err)
	}
	shared := &runtimeExternalProbe{}
	root := &accountAssembly{agents: b, agentInstall: install, skills: shared}
	root.StopAdmission()
	if original.Err() == nil {
		t.Fatal("original initialization was not cancelled")
	}
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	if err := root.Force(expired); !errors.Is(err, context.Canceled) || b.Joined() || root.Joined() || shared.forced != 0 {
		t.Fatal("unreturned startup released a shared provider", err)
	}
	release()
	if err := <-returned; err == nil {
		t.Fatal("controlled NotCommitted became a successful registration")
	}
	if err := root.Drain(context.Background()); err != nil || !root.Joined() {
		t.Fatal("actual returned startup did not permit retirement", err)
	}
	// No callback/registration is synthesized for the Unknown boundary either.
	unknownStore := &agentConfigurationRootStore{unknown: true}
	var err error
	unknownStore.attempt, err = f.NewID[f.TransactionAttempt]()
	if err != nil {
		t.Fatal(err)
	}
	unknown, unknownInstall, _ := agentConfigurationRootBundle(t, unknownStore)
	err = unknown.Initialize(context.Background())
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown || fault.CauseID != unknownStore.attempt.String() || unknownStore.called != 1 {
		t.Fatal("startup lost the original physical Unknown", err)
	}
	if err := unknown.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := unknownInstall.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAgentConfigurationRootRouteOwnership(t *testing.T) {
	const prefix = "/api/v1/projects/01900000-0000-7000-8000-000000000001"
	for _, test := range []struct{ method, suffix, owner string }{
		{"POST", "/agents", "configuration"}, {"GET", "/agents", "directory"}, {"HEAD", "/agents", "directory"},
		{"GET", "/agents/01900000-0000-7000-8000-000000000002", "directory"},
		{"GET", "/agents/01900000-0000-7000-8000-000000000002/configuration", "configuration"},
		{"HEAD", "/agents/01900000-0000-7000-8000-000000000002/configuration", "configuration"},
		{"POST", "/agents/01900000-0000-7000-8000-000000000002/configuration", "configuration"},
		{"POST", "/agent-commands/lookup", "configuration"}, {"GET", "/agent-commands/lookup", "configuration"},
		{"POST", "/tasks", "original"}, {"GET", "/agents/", "original"},
	} {
		request := httptest.NewRequest(test.method, prefix+test.suffix, nil)
		calls := 0
		handler := func(owner string) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if owner != test.owner || r != request {
					t.Fatal("route replaced its original method/path owner", test)
				}
				w.WriteHeader(http.StatusNoContent)
			})
		}
		response := httptest.NewRecorder()
		agentConfigurationRoutes(agentDirectoryRoutes(handler("original"), handler("directory")), handler("configuration")).ServeHTTP(response, request)
		if calls != 1 || response.Code != http.StatusNoContent {
			t.Fatal("route was missing or delegated twice", test)
		}
	}
}
