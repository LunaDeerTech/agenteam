package app

import (
	"context"
	"errors"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	ac "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func updateRootID[K any]() f.ID[K] {
	v, e := f.ParseID[K]("01900000-0000-7000-8000-000000000001")
	if e != nil {
		panic(e)
	}
	return v
}
func updateRootService(t *testing.T, store interface {
	database
	project.Store
	audit.Store
}) (*project.Service, *project.Authority) {
	t.Helper()
	cfg := testConfig(t, "1s")
	accounts, e := account.NewAuthority(store, cfg.AccountKeyring())
	if e != nil {
		t.Fatal(e)
	}
	usage, e := createProjectUsage(cfg, store, accounts)
	if e != nil {
		t.Fatal(e)
	}
	aud, e := audit.New(store, cfg.CursorKeyring(), audit.Authorizations{Accounts: accounts, Sessions: accounts, System: accounts, Projects: usage.projects})
	if e != nil {
		t.Fatal(e)
	}
	catalog := ec.NewCatalog()
	events, e := pc.RegisterProjectEvents(catalog)
	if e != nil {
		t.Fatal(e)
	}
	process := accountProcessAuthority{process: updateRootID[ac.Process](), guard: &object.ProcessGuard{}}
	journal, e := outbox.New(store, catalog, outbox.Authorizations{Producers: map[ec.StableName]oc.ProducerAuthority{pc.ProjectProducer: usage.projects}, Projects: usage.projects, Sessions: accounts, System: accounts, Audit: aud, Processes: projectCommandProcess{process}, Cursors: cfg.CursorKeyring()})
	if e != nil {
		t.Fatal(e)
	}
	service, e := createProjectUpdate(cfg, store, usage.projects, accounts, aud, journal, events, process, nil)
	if e != nil {
		t.Fatal(e)
	}
	for _, db := range []database{nil, &unitDatabase{}, &projectUsageRootStore{}} {
		if got, err := createProjectUpdate(cfg, db, usage.projects, accounts, aud, journal, events, process, nil); err == nil || got != nil {
			t.Fatal("missing or foreign store")
		}
	}
	if got, err := createProjectUpdate(cfg, store, usage.projects, accounts, aud, journal, events, accountProcessAuthority{process: process.process}, nil); err == nil || got != nil {
		t.Fatal("missing actual guard")
	}
	if got, err := projectUpdateHandler(service, nil, cfg.PublicOrigin()); err == nil || got != nil {
		t.Fatal("missing account core")
	}
	return service, usage.projects
}
func TestProjectUpdateRootPureConstruction(t *testing.T) {
	service, _ := updateRootService(t, &projectUsageRootStore{})
	work := &projectCommandWork{service: service}
	if work.Joined() {
		t.Fatal("unstarted work claimed joined")
	}
	work.StopAdmission()
	if work.Joined() {
		t.Fatal("Stop claimed actual Drain")
	}
	if err := work.Drain(context.Background()); err != nil || !work.Joined() {
		t.Fatal("empty service drain", err)
	}
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			work.StopAdmission()
			_ = work.Force(context.Background())
			_ = work.Drain(context.Background())
			if !work.Joined() {
				t.Error("race join lost")
			}
		})
	}
	wg.Wait()
}

type updateRootBlockedStore struct {
	projectUsageRootStore
	entered, release chan struct{}
	once             sync.Once
}

func (s *updateRootBlockedStore) WithinTx(ctx context.Context, cause f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
}
func TestProjectUpdateRootPureActualDrain(t *testing.T) {
	store := &updateRootBlockedStore{entered: make(chan struct{}), release: make(chan struct{})}
	service, _ := updateRootService(t, store)
	work := &projectCommandWork{service: service}
	actor, _ := id.NewHuman(updateRootID[id.User](), updateRootID[id.Session]())
	done := make(chan error, 1)
	go func() {
		_, err := service.LookupCommand(context.Background(), actor, pc.CommandLookupRequest{ProjectID: updateRootID[id.Project](), Command: pc.UpdateCommand, Key: "key"})
		done <- err
	}()
	<-store.entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := work.Force(ctx); !errors.Is(err, context.DeadlineExceeded) || work.Joined() {
		t.Fatal("cancel pretended join", err)
	}
	select {
	case <-done:
		t.Fatal("call abandoned")
	default:
	}
	close(store.release)
	if err := <-done; err == nil {
		t.Fatal("fixture unexpected success")
	}
	if err := work.Drain(context.Background()); err != nil || !work.Joined() {
		t.Fatal("actual completion not observed", err)
	}
}
func TestProjectUpdateRootPureRouteOwnership(t *testing.T) {
	const p = "/api/v1/projects/01900000-0000-7000-8000-000000000001"
	for _, tc := range []struct{ method, path, owner string }{{"PATCH", p, "update"}, {"DELETE", p, "update"}, {"HEAD", p, "read"}, {"GET", p, "read"}, {"POST", p + "/commands/lookup", "update"}, {"HEAD", p + "/commands/lookup", "update"}, {"POST", "/api/v1/projects", "read"}, {"GET", "/api/v1/projects/resolve", "usage"}, {"POST", p + "/archive", "old"}, {"GET", "/api/v1/system/model-selection/meeting-summary", "old"}, {"PATCH", p + "/", "old"}} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path+"?private", strings.NewReader("input"))
			body, url := r.Body, r.URL
			count := 0
			child := func(owner string) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, got *http.Request) {
					count++
					if tc.owner != owner || got != r || got.Body != body || got.URL != url {
						t.Fatal("dispatch changed ownership")
					}
					w.WriteHeader(204)
				})
			}
			h := projectUpdateRoutes(projectReadRoutes(projectUsageRoutes(child("old"), child("usage")), child("read")), child("update"))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if count != 1 || w.Code != 204 {
				t.Fatal("dispatch count")
			}
		})
	}
}
