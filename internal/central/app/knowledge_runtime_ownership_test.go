package app

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// The real constructors and original public call reach this Store. Its return
// is controlled; the uninitialized guard never grants SQL/business success.
type knowledgeOwnedAppStore struct {
	*postgres.Store
	mu      sync.Mutex
	causes  []f.TransactionCause
	entered chan context.Context
	release chan struct{}
	unknown bool
	attempt f.ID[f.TransactionAttempt]
}

func (s *knowledgeOwnedAppStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	s.causes = append(s.causes, cause)
	entered, release, unknown := s.entered, s.release, s.unknown
	s.mu.Unlock()
	if entered != nil {
		entered <- ctx
		<-release
	}
	if unknown {
		return f.UnknownResult(s.attempt, cause)
	}
	var failure *f.Fault
	if errors.As(fn(ctx, f.NewTx()), &failure) {
		return f.NotCommittedResult(failure)
	}
	return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
}
func (*knowledgeOwnedAppStore) AcquireAll(context.Context, f.Tx, []f.LockRequest) error { return nil }
func (*knowledgeOwnedAppStore) InTx(f.Tx) (postgres.SQLExecutor, error) {
	return nil, f.NewFault(f.DependencyUnbound, f.NotCommitted)
}

type knowledgeOwnedAppFixture struct {
	cfg       config.Config
	store     *knowledgeOwnedAppStore
	projects  *projectUsageAssembly
	accounts  *account.Authority
	authority *knowledge.Authority
	objects   *objectAssembly
	events    kc.KnowledgeEvents
	owned     *knowledgeCallAssembly
}

func newKnowledgeOwnedAppFixture(t *testing.T) knowledgeOwnedAppFixture {
	t.Helper()
	v := knowledgeOwnedAppFixture{cfg: testConfig(t, "1s"), store: &knowledgeOwnedAppStore{}}
	var err error
	v.accounts, err = account.NewAuthority(v.store, v.cfg.AccountKeyring())
	if err != nil {
		t.Fatal(err)
	}
	life, err := createProjectLifecycleAuthority(v.store)
	if err != nil {
		t.Fatal(err)
	}
	v.projects, err = createProjectUsageAuthorityWithLifecycle(v.store, v.accounts, life)
	if err != nil {
		t.Fatal(err)
	}
	v.authority, err = knowledge.NewAuthority(v.store, v.projects.projects)
	if err != nil {
		t.Fatal(err)
	}
	v.events, err = kc.RegisterKnowledgeEvents(ec.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	process := updateRootID[oc.Process]()
	spool, err := object.OpenSpool(filepath.Join(t.TempDir(), "spool"), process)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := spool.Close(); err != nil {
			t.Error(err)
		}
	})
	guard, err := object.OpenProcessGuard(spool, process)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := guard.Close(); err != nil {
			t.Error(err)
		}
	})
	backend, err := object.NewBackend(v.cfg.Objects().Storage())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	})
	objects, err := object.New(v.store, backend, spool, &audit.Service{}, object.Authorizations{Planner: v.authority, Resources: v.authority, Read: v.authority, Gate: v.authority, Cleanup: v.authority, Processes: guard})
	if err != nil {
		t.Fatal(err)
	}
	v.objects = &objectAssembly{process: process, guard: guard, service: objects}
	v.owned, err = v.make(v.projects, v.objects)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.store.causes) != 0 {
		t.Fatal("constructor performed SQL before original Object.Initialize")
	}
	t.Cleanup(func() { v.owned.StopAdmission() })
	return v
}
func (v knowledgeOwnedAppFixture) make(projects *projectUsageAssembly, objects *objectAssembly) (*knowledgeCallAssembly, error) {
	return createOwnedKnowledge(v.cfg, v.store, v.authority, projects, v.accounts, objects, &audit.Service{}, &outbox.Service{}, v.events)
}
func TestKnowledgeProjectCallAppConstruction(t *testing.T) {
	v := newKnowledgeOwnedAppFixture(t)
	if v.owned.pair.Service() == nil || v.owned.pair.SourceResolver() == nil || v.owned.pair.SourceReads() == nil || v.owned.cleanup.Name() != pc.KnowledgeParticipant || v.owned.Joined() {
		t.Fatal("first original graph was not published together")
	}
	var err error
	for _, mode := range []string{"nil-life", "foreign-life", "nil-guard", "invalid-process"} {
		t.Run(mode, func(t *testing.T) {
			p := *v.projects
			o := objectAssembly{process: v.objects.process, guard: v.objects.guard, service: v.objects.service}
			switch mode {
			case "nil-life":
				p.lifecycle = nil
			case "foreign-life":
				p.lifecycle, err = createProjectLifecycleAuthority(&knowledgeOwnedAppStore{})
				if err != nil {
					t.Fatal(err)
				}
			case "nil-guard":
				o.guard = nil
			case "invalid-process":
				o.process = oc.ProcessID{}
			}
			if got, err := v.make(&p, &o); err == nil || got != nil {
				t.Fatal("invalid original binding published", err)
			}
		})
	}
	if len(v.store.causes) != 0 {
		t.Fatal("invalid constructors performed SQL")
	}
	v.owned.StopAdmission()
	if err := v.owned.Drain(context.Background()); err != nil || !v.owned.Joined() {
		t.Fatal("empty original pair did not return", err)
	}
}

func TestKnowledgeProjectCallAppOriginalStoreAndLateInstall(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "installed", true: "late-install"}[late], func(t *testing.T) {
			v := newKnowledgeOwnedAppFixture(t)
			s := v.store
			s.entered = make(chan context.Context, 4)
			s.release = make(chan struct{})
			var once sync.Once
			release := func() { once.Do(func() { close(s.release) }) }
			actor, err := i.NewHuman(updateRootID[i.User](), updateRootID[i.Session]())
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			joined := false
			t.Cleanup(func() {
				release()
				if !joined {
					<-done
				}
			})
			go func() {
				_, err := v.owned.pair.Service().GetDocument(context.Background(), actor, updateRootID[i.Project](), updateRootID[kc.Document]())
				done <- err
			}()
			original := <-s.entered
			ended, cancel := context.WithCancel(context.Background())
			cancel()
			provider := &b04Work{}
			provider.force = func(ctx context.Context) error {
				if ctx != ended {
					t.Error("successor received a renewed Force budget")
				}
				provider.joined.Store(true)
				return nil
			}
			root := &accountAssembly{}
			if late {
				if err := root.Force(ended); err != nil && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if root.install(ended, func() { root.knowledge = v.owned; root.core = provider }) {
					t.Fatal("stopped root accepted late producer")
				}
			} else {
				root.knowledge = v.owned
				root.core = provider
				if err := root.Force(ended); !errors.Is(err, context.Canceled) {
					t.Fatal("Force lost original context", err)
				}
			}
			if provider.forced.Load() != 0 {
				t.Fatal("successor forced before original Knowledge Store returned")
			}
			if original.Err() == nil || v.owned.Joined() || root.Joined() {
				t.Fatal("Stop fabricated original Store return")
			}
			if (&resources{stopping: true, accountService: root}).producersJoined() {
				t.Fatal("Object guard could retire before Knowledge returned")
			}
			select {
			case <-done:
				joined = true
				t.Fatal("Store did not hold public return")
			default:
			}
			release()
			err = <-done
			joined = true
			if err == nil {
				t.Fatal("controlled registration became business success")
			}
			if err := root.Force(context.Background()); err != nil && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if provider.forced.Load() != 1 || !provider.Joined() {
				t.Fatal("returned Knowledge call did not release the successor Force gate")
			}
			if err := root.Drain(context.Background()); err != nil || !root.Joined() {
				t.Fatal("original returned call did not join", err)
			}
		})
	}
}
