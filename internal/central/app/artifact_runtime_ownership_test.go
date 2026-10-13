package app

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/artifact"
	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
)

type artifactOwnedAppFixture struct {
	cfg      config.Config
	store    *knowledgeOwnedAppStore
	projects *projectUsageAssembly
	owners   *artifact.OwnerProvider
	objects  *objectAssembly
	owned    *artifactCallAssembly
}

func newArtifactOwnedAppFixture(t *testing.T) artifactOwnedAppFixture {
	t.Helper()
	v := artifactOwnedAppFixture{cfg: testConfig(t, "1s"), store: &knowledgeOwnedAppStore{}}
	accounts, err := account.NewAuthority(v.store, v.cfg.AccountKeyring())
	if err != nil {
		t.Fatal(err)
	}
	life, err := createProjectLifecycleAuthority(v.store)
	if err != nil {
		t.Fatal(err)
	}
	v.projects, err = createProjectUsageAuthorityWithLifecycle(v.store, accounts, life)
	if err != nil {
		t.Fatal(err)
	}
	v.owners, err = createArtifactOwners(v.store, v.projects)
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
	objects, err := object.New(v.store, backend, spool, &audit.Service{}, object.Authorizations{Planner: v.owners, Resources: v.owners, Read: v.owners, Gate: v.owners, Cleanup: v.owners, Processes: guard})
	if err != nil {
		t.Fatal(err)
	}
	v.objects = &objectAssembly{process: process, guard: guard, service: objects}
	v.owned, err = createOwnedArtifacts(v.cfg, v.store, v.owners, v.projects, v.objects, &audit.Service{})
	if err != nil {
		t.Fatal(err)
	}
	if len(v.store.causes) != 0 {
		t.Fatal("construction performed SQL or initialized the guard")
	}
	t.Cleanup(v.owned.StopAdmission)
	return v
}

func TestArtifactHumanAppConstructionAndFixedDispatch(t *testing.T) {
	v := newArtifactOwnedAppFixture(t)
	if v.owned.pair.Service() == nil || v.owned.pair.Service().RequireProjectStopBinding(v.objects.service) != nil || v.owned.Joined() {
		t.Fatal("original Object/pair binding lost")
	}
	avatar, documents, skills := &account.AvatarAuthority{}, &knowledge.Authority{}, &skill.Authority{}
	dispatch, err := newObjectAuthoritiesWithArtifacts(v.store, avatar, documents, skills, v.owners)
	if err != nil {
		t.Fatal(err)
	}
	owner, _ := oc.NewObjectOwner(oc.Artifact, updateRootID[ac.ArtifactEntity]().String(), updateRootID[i.Project]().String())
	selected, err := dispatch.owner(owner)
	if err != nil || selected != v.owners {
		t.Fatal("Artifact selection substituted a provider", err)
	}
	legacy, err := newObjectAuthorities(v.store, avatar, documents, skills)
	if err != nil {
		t.Fatal(err)
	}
	if selected, err := legacy.owner(owner); selected != nil || err == nil {
		t.Fatal("legacy unbound dispatch silently changed")
	}
	for _, mode := range []string{"nil-life", "foreign-life", "nil-guard", "foreign-process", "nil-owner"} {
		t.Run(mode, func(t *testing.T) {
			p := *v.projects
			o := objectAssembly{process: v.objects.process, guard: v.objects.guard, service: v.objects.service}
			owners := v.owners
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
			case "foreign-process":
				o.process, err = f.NewID[oc.Process]()
				if err != nil || o.process == v.objects.process {
					t.Fatal("fixture did not create a distinct process", err)
				}
			case "nil-owner":
				owners = nil
			}
			if got, err := createOwnedArtifacts(v.cfg, v.store, owners, &p, &o, &audit.Service{}); got != nil || err == nil {
				t.Fatal("invalid original composition published", err)
			}
		})
	}
	if len(v.store.causes) != 0 {
		t.Fatal("constructor admission performed SQL")
	}
	v.owned.StopAdmission()
	if err := v.owned.Drain(context.Background()); err != nil || !v.owned.Joined() {
		t.Fatal("empty actual pair did not join", err)
	}
}

func TestArtifactHumanAppOriginalReturnAndLateInstall(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "installed", true: "late-install"}[late], func(t *testing.T) {
			v := newArtifactOwnedAppFixture(t)
			v.store.entered, v.store.release = make(chan context.Context, 4), make(chan struct{})
			var once sync.Once
			release := func() { once.Do(func() { close(v.store.release) }) }
			actor, err := i.NewHuman(updateRootID[i.User](), updateRootID[i.Session]())
			if err != nil {
				t.Fatal(err)
			}
			invocation, err := ac.NewInvocation(ac.InvocationDetails{Actor: actor, ProjectID: updateRootID[i.Project](), AttemptID: updateRootID[ac.CallAttempt]()})
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			returned := false
			t.Cleanup(func() {
				release()
				if !returned {
					<-done
				}
			})
			go func() {
				_, err := v.owned.pair.Service().ListArtifacts(context.Background(), invocation, ac.ListFilter{}, f.PageRequest{})
				done <- err
			}()
			original := <-v.store.entered
			ended, cancel := context.WithCancel(context.Background())
			cancel()
			provider := &b04Work{}
			provider.force = func(ctx context.Context) error {
				if ctx != ended {
					t.Error("successor renewed the original Force budget")
				}
				provider.joined.Store(true)
				return nil
			}
			root := &accountAssembly{}
			if late {
				_ = root.Force(ended)
				if root.install(ended, func() { root.artifacts = v.owned; root.core = provider }) {
					t.Fatal("late install reopened root")
				}
			} else {
				root.artifacts, root.core = v.owned, provider
				if err := root.Force(ended); !errors.Is(err, context.Canceled) {
					t.Fatal("original Force cancellation lost", err)
				}
			}
			if provider.forced.Load() != 0 || original.Err() == nil || v.owned.Joined() || root.Joined() || (&resources{stopping: true, accountService: root}).producersJoined() {
				t.Fatal("cancellation retired original Store, provider or guard early")
			}
			select {
			case <-done:
				returned = true
				t.Fatal("public call returned before original Store")
			default:
			}
			release()
			err = <-done
			returned = true
			if err == nil {
				t.Fatal("uninitialized original guard produced business success")
			}
			if err := root.Force(context.Background()); err != nil && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if provider.forced.Load() != 1 {
				t.Fatal("actual return did not release next Force")
			}
			if err := root.Drain(context.Background()); err != nil || !root.Joined() {
				t.Fatal("actual returned owner did not join", err)
			}
		})
	}
}
