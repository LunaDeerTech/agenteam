package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
)

func directoryRootBundle(t *testing.T, store workRootStores) (*agentDirectoryAssembly, config.Config) {
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
	assembly, err := createAgentDirectory(cfg, store, projects)
	if err != nil || assembly == nil || assembly.reader == nil {
		t.Fatal("pure real Reader assembly", err)
	}
	for _, db := range []database{nil, (*workRootStore)(nil), &unitDatabase{}} {
		if got, e := createAgentDirectory(cfg, db, projects); e == nil || got != nil {
			t.Fatal("missing original Store accepted")
		}
	}
	if got, e := createAgentDirectory(cfg, store, nil); e == nil || got != nil {
		t.Fatal("missing original Project authority accepted")
	}
	return assembly, cfg
}
func TestAgentDirectoryRootConstructionAndRoutes(t *testing.T) {
	b, cfg := directoryRootBundle(t, &workRootStore{})
	if got, err := agentDirectoryHandler(b, nil, cfg.PublicOrigin()); err == nil || got != nil {
		t.Fatal("missing Account boundary accepted")
	}
	const prefix = "/api/v1/projects/01900000-0000-7000-8000-000000000001"
	for _, tc := range []struct {
		path      string
		directory bool
	}{
		{prefix + "/agents", true}, {prefix + "/agents/01900000-0000-7000-8000-000000000002", true}, {prefix + "/agents/bad-id", true},
		{prefix + "/tasks", false}, {prefix + "/skills", false}, {prefix + "/agents/", false}, {prefix + "/agents/x/configuration", false}, {"/api/v1/session", false}, {"/readyz", false},
	} {
		request := httptest.NewRequest("POST", tc.path+"?limit=1", nil)
		calls := 0
		child := func(directory bool) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if directory != tc.directory || r != request {
					t.Fatal("route changed owner/request")
				}
				w.WriteHeader(204)
			})
		}
		w := httptest.NewRecorder()
		agentDirectoryRoutes(child(false), child(true)).ServeHTTP(w, request)
		if calls != 1 || w.Code != 204 {
			t.Fatal("duplicate or missing route")
		}
	}
	root := &accountAssembly{}
	root.StopAdmission()
	if root.install(context.Background(), func() { root.agentDirectory = b }) {
		t.Fatal("late assembly admitted after Stop")
	}
	if err := root.Drain(context.Background()); err != nil || !b.Joined() || !root.Joined() {
		t.Fatal("late Reader not owned/joined", err)
	}
}
func TestAgentDirectoryRootWaitsForOriginalRead(t *testing.T) {
	s := &workRootBlockedStore{entered: make(chan context.Context, 1), release: make(chan struct{})}
	b, _ := directoryRootBundle(t, s)
	root := &accountAssembly{agentDirectory: b}
	actor, err := i.NewHuman(updateRootID[i.User](), updateRootID[i.Session]())
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	done := make(chan error, 1)
	t.Cleanup(func() {
		once.Do(func() { close(s.release) })
		root.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = root.Drain(ctx)
	})
	go func() {
		_, err := b.reader.GetAgent(context.Background(), actor, updateRootID[i.Project](), updateRootID[i.Agent]())
		done <- err
	}()
	var original context.Context
	select {
	case original = <-s.entered:
	case <-time.After(time.Second):
		t.Fatal("original Store call not entered")
	}
	root.StopAdmission()
	if original.Err() == nil || root.Joined() {
		t.Fatal("root retirement preceded original call")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if root.Force(canceled) == nil {
		t.Fatal("Force invented original join")
	}
	select {
	case <-done:
		t.Fatal("read escaped held Store")
	default:
	}
	once.Do(func() { close(s.release) })
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("failed original read published success")
		}
	case <-time.After(time.Second):
		t.Fatal("original Store return not joined")
	}
	if err = root.Drain(context.Background()); err != nil || !root.Joined() {
		t.Fatal("Reader retained after actual return", err)
	}
}
