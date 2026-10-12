package app

import (
	"context"
	"errors"
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
	objc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
)

// The real Project and Skill constructors are used, but the concrete Object
// service and guard are unbound. The controlled Store never runs a callback or
// returns Committed. These tests prove original call ownership, not successful
// initialization, SQL authorization or a physical ProcessGuard release.
type projectCreationRootStore struct {
	projectUsageRootStore
	entered chan context.Context
	release chan struct{}
	mu      sync.Mutex
	causes  []f.TransactionCause
}

func (s *projectCreationRootStore) WithinTx(ctx context.Context, cause f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	s.causes = append(s.causes, cause)
	s.mu.Unlock()
	if s.entered != nil {
		s.entered <- ctx
		<-s.release // Cancellation cannot return the original Store call.
	}
	return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
}

func (s *projectCreationRootStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.causes)
}

type projectCreationRootBundle struct {
	work    *projectCommandWork
	skills  *skillWork
	legacy  *project.Service
	factory func(*skill.Service) (*project.Service, error)
}

func newProjectCreationRootBundle(t *testing.T, store *projectCreationRootStore) projectCreationRootBundle {
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
	authorities, err := createKnowledgeSkillAuthorities(store, projects)
	if err != nil {
		t.Fatal(err)
	}
	objects := &objectAssembly{service: &object.Service{}, guard: &object.ProcessGuard{}, process: updateRootID[objc.Process]()}
	skills, err := createSkills(context.Background(), authorities.skills, objects)
	if err != nil {
		t.Fatal(err)
	}
	skillOwner := &skillWork{service: skills}
	t.Cleanup(func() {
		if err := skillOwner.Drain(context.Background()); err != nil {
			t.Error("empty Skill owner did not drain", err)
		}
	})
	auditor, err := audit.New(store, cfg.CursorKeyring(), audit.Authorizations{Accounts: accounts, Sessions: accounts, System: accounts, Projects: projects.projects})
	if err != nil {
		t.Fatal(err)
	}
	catalog := ec.NewCatalog()
	events, err := pc.RegisterProjectEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	process := accountProcessAuthority{process: updateRootID[ac.Process](), guard: objects.guard}
	journal, err := outbox.New(store, catalog, outbox.Authorizations{Producers: map[ec.StableName]oc.ProducerAuthority{pc.ProjectProducer: projects.projects}, Projects: projects.projects, Sessions: accounts, System: accounts, Audit: auditor, Processes: projectCommandProcess{process}, Cursors: cfg.CursorKeyring()})
	if err != nil {
		t.Fatal(err)
	}
	factory := func(initializer *skill.Service) (*project.Service, error) {
		return createProjectCommands(cfg, store, projects.projects, accounts, auditor, journal, events, process, initializer)
	}
	service, err := factory(skills)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := createProjectUpdate(cfg, store, projects.projects, accounts, auditor, journal, events, process)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		service.Stop()
		legacy.Stop()
		if err := service.Drain(context.Background()); err != nil {
			t.Error(err)
		}
		if err := legacy.Drain(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return projectCreationRootBundle{work: &projectCommandWork{service: service}, skills: skillOwner, legacy: legacy, factory: factory}
}

type projectCreationProvider struct {
	accountWork
	contexts []context.Context
}

func (p *projectCreationProvider) Force(ctx context.Context) error {
	p.contexts = append(p.contexts, ctx)
	return p.accountWork.Force(ctx)
}

func projectCreationAwait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		t.Fatal("original Project call did not reach its expected boundary")
		var zero T
		return zero
	}
}

func projectCreationHold(t *testing.T, store *projectCreationRootStore, service *project.Service) (context.Context, <-chan error, func()) {
	t.Helper()
	actor, err := id.NewHuman(updateRootID[id.User](), updateRootID[id.Session]())
	if err != nil {
		t.Fatal(err)
	}
	meta := f.CommandMeta{RequestID: updateRootID[f.Request](), IdempotencyKey: "project-creation-original"}
	request := pc.CreateProjectRequest{ProjectID: updateRootID[id.Project](), Name: "app-create", Description: "Owned original call"}
	if _, err := pc.CreateDigest(actor, meta, request); err != nil {
		t.Fatal("invalid creation fixture", err)
	}
	done := make(chan error, 1)
	returned := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(store.release) }) }
	t.Cleanup(func() {
		release()
		select {
		case <-returned:
		case <-time.After(time.Second):
			t.Error("original Project goroutine still owned after release")
			<-returned
		}
	})
	go func() {
		defer close(returned)
		_, err := service.CreateProject(context.Background(), actor, meta, request)
		done <- err
	}()
	return projectCreationAwait(t, store.entered), done, release
}

func TestProjectCreationRootRequiresSkillAndKeepsLegacy(t *testing.T) {
	store := &projectCreationRootStore{}
	b := newProjectCreationRootBundle(t, store)
	var missing *skill.Service
	service, err := b.factory(missing)
	var fault *f.Fault
	if service != nil || !errors.As(err, &fault) || fault.Code != f.DependencyUnbound || fault.CommitState != f.NotStarted {
		t.Fatal("missing actual initializer accepted", err)
	}
	if store.count() != 0 || b.work.Joined() || b.skills.Joined() {
		t.Fatal("constructors performed I/O or claimed retirement")
	}
	actor, err := id.NewHuman(updateRootID[id.User](), updateRootID[id.Session]())
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.legacy.LookupCommand(context.Background(), actor, pc.CommandLookupRequest{ProjectID: updateRootID[id.Project](), Command: pc.UpdateCommand, Key: "legacy-update"})
	if !errors.As(err, &fault) || fault.Code != f.DependencyUnavailable || store.count() != 1 {
		t.Fatal("metadata-only legacy entry no longer reached its original Store", err)
	}
}

func TestProjectCreationRootWaitsBeforeProviderRetirement(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "drain", true: "force"}[force], func(t *testing.T) {
			store := &projectCreationRootStore{entered: make(chan context.Context, 1), release: make(chan struct{})}
			b := newProjectCreationRootBundle(t, store)
			original, done, release := projectCreationHold(t, store, b.work.service)
			provider := &projectCreationProvider{accountWork: b.skills}
			assembly := &accountAssembly{projects: b.work, skills: provider}
			owned := &resources{stopping: true, accountService: assembly}
			guard := &runtimeGuardProbe{} // Only observes the root's guard gate.
			expired, cancel := context.WithCancel(context.Background())
			cancel()
			var err error
			if force {
				err = assembly.Force(expired)
			} else {
				err = assembly.Drain(expired)
			}
			if !errors.Is(err, context.Canceled) || original.Err() == nil || b.work.Joined() || assembly.Joined() || len(provider.contexts) != 0 {
				t.Fatal("cancellation crossed the original Project return", err)
			}
			owned.forceObjects(expired, guard)
			if guard.forced != 0 || owned.producersJoined() {
				t.Fatal("unreturned Project made the guard releasable")
			}
			select {
			case <-done:
				t.Fatal("original Store call returned before release")
			default:
			}
			release()
			var fault *f.Fault
			if err := projectCreationAwait(t, done); !errors.As(err, &fault) || fault.Code != f.DependencyUnavailable || fault.CommitState != f.NotCommitted {
				t.Fatal("original controlled Store outcome changed", err)
			}
			if force {
				err = assembly.Force(context.Background())
				if len(provider.contexts) != 1 || provider.contexts[0] != expired {
					t.Fatal("second Force renewed the original remaining context")
				}
			} else {
				err = assembly.Drain(context.Background())
			}
			if err != nil || !b.work.Joined() || !assembly.Joined() || !owned.producersJoined() || store.count() != 1 {
				t.Fatal("actual returned Project did not retire with its provider", err)
			}
			owned.forceObjects(expired, guard)
			if guard.forced != 1 {
				t.Fatal("actual original completion did not open the root guard gate")
			}
		})
	}
}

func TestProjectCreationRootLateInstallUsesOriginalForce(t *testing.T) {
	store := &projectCreationRootStore{entered: make(chan context.Context, 1), release: make(chan struct{})}
	b := newProjectCreationRootBundle(t, store)
	original, done, release := projectCreationHold(t, store, b.work.service)
	assembly := &accountAssembly{constructing: true}
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	if err := assembly.Force(expired); err != nil {
		t.Fatal(err)
	}
	provider := &projectCreationProvider{accountWork: b.skills}
	if assembly.install(context.Background(), func() { assembly.projects, assembly.skills = b.work, provider }) {
		t.Fatal("late creation owner escaped stopped construction")
	}
	work := assembly.works()
	if len(work) != 2 || work[0] != b.work || work[1] != provider || assembly.forced != expired || original.Err() == nil || len(provider.contexts) != 0 || b.work.Joined() || assembly.Joined() {
		t.Fatal("late owner lost original context, admission or provider ordering")
	}
	release()
	if err := projectCreationAwait(t, done); err == nil {
		t.Fatal("controlled noncommitted Create returned success")
	}
	if err := assembly.Force(context.Background()); err != nil || !b.work.Joined() || len(provider.contexts) != 1 || provider.contexts[0] != expired || assembly.Joined() {
		t.Fatal("late original return renewed Force or hid ongoing construction", err)
	}
	assembly.constructionDone()
	if !assembly.Joined() || store.count() != 1 {
		t.Fatal("completed construction did not expose actual Project/Skill join")
	}
}
