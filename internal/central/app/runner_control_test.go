package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/runner/service"
)

func pureRunnerRoot(t *testing.T, store workRootStores) *runnerControlAssembly {
	t.Helper()
	cfg := testConfig(t, "1s")
	accounts, e := account.NewAuthority(store, cfg.AccountKeyring())
	if e != nil {
		t.Fatal(e)
	}
	authority, e := createRunnerAuthority(store, accounts)
	if e != nil {
		t.Fatal(e)
	}
	auditor, e := audit.New(store, cfg.CursorKeyring(), audit.Authorizations{Accounts: accounts, Sessions: accounts, System: accounts, Runners: authority})
	if e != nil {
		t.Fatal(e)
	}
	bundle, e := createRunnerControl(authority, auditor)
	if e != nil {
		t.Fatal(e)
	}
	for _, invalid := range []database{nil, (*workRootStore)(nil), &unitDatabase{}} {
		if got, e := createRunnerAuthority(invalid, accounts); e == nil || got != nil {
			t.Fatal("missing real Store capability accepted")
		}
	}
	if got, e := createRunnerAuthority(store, nil); e == nil || got != nil {
		t.Fatal("missing current Account authority accepted")
	}
	for _, missing := range []*service.Authority{nil, {}} {
		if got, e := createRunnerControl(missing, auditor); e == nil || got != nil {
			t.Fatal("invalid Runner authority accepted")
		}
	}
	if got, e := createRunnerControl(authority, nil); e == nil || got != nil {
		t.Fatal("missing Audit silently accepted")
	}
	return bundle
}

func TestRunnerRootPureConstructionAndRetirement(t *testing.T) {
	bundle := pureRunnerRoot(t, &workRootStore{})
	if bundle.Joined() {
		t.Fatal("construction was mistaken for retirement")
	}
	bundle.StopAdmission()
	if bundle.Joined() {
		t.Fatal("Stop was mistaken for actual Drain")
	}
	if e := bundle.Drain(context.Background()); e != nil || !bundle.Joined() {
		t.Fatal("empty owner failed to retire", e)
	}
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			bundle.StopAdmission()
			if e := bundle.Force(context.Background()); e != nil || !bundle.Joined() {
				t.Error("concurrent retirement lost join", e)
			}
		})
	}
	workers.Wait()
}

func TestRunnerRootPureActualCallAndOriginalForce(t *testing.T) {
	store := &workRootBlockedStore{entered: make(chan context.Context, 1), release: make(chan struct{})}
	bundle := pureRunnerRoot(t, store)
	actor, e := id.NewHuman(updateRootID[id.User](), updateRootID[id.Session]())
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := bundle.service.List(context.Background(), actor, c.ListRequest{Limit: 1}); done <- e }()
	var call context.Context
	select {
	case call = <-store.entered:
	case e := <-done:
		t.Fatal("reader returned before entering owned transaction", e)
	case <-time.After(time.Second):
		t.Fatal("reader did not enter owned transaction")
	}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(store.release) }) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assembly := &accountAssembly{runners: bundle}
	if e := assembly.Force(ctx); !errors.Is(e, context.Canceled) || assembly.forced != ctx || assembly.Joined() || bundle.Joined() {
		t.Fatal("original expired Force lost actual call ownership", e)
	}
	select {
	case <-call.Done():
	default:
		t.Fatal("Runner did not receive Stop cancellation")
	}
	select {
	case <-done:
		t.Fatal("cancel was substituted for transaction return")
	default:
	}
	release.Do(func() { close(store.release) })
	if e := <-done; e == nil {
		t.Fatal("transaction failure was hidden")
	}
	if e := assembly.Drain(context.Background()); e != nil || !assembly.Joined() {
		t.Fatal("returned Runner call did not join", e)
	}
}

func TestRunnerRootPureRouteOwnership(t *testing.T) {
	for _, test := range []struct{ path, owner string }{
		{"/api/v1/system/runners", "admin"}, {"/api/v1/system/runners/invalid", "admin"},
		{"/api/v1/runner/enroll", "device"}, {"/api/v1/runner/challenge", "device"}, {"/api/v1/runner/control", "device"},
		{"/api/v1/runner/control/", "existing"}, {"/api/v1/system/runners-extra", "existing"}, {"/api/v1/session", "existing"},
	} {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest("GET", test.path, nil)
			calls, got := 0, ""
			handler := func(owner string) http.Handler {
				return http.HandlerFunc(func(_ http.ResponseWriter, received *http.Request) {
					if received != request {
						t.Error("routing changed original request ownership")
					}
					calls++
					got = owner
				})
			}
			runnerControlRoutes(handler("existing"), handler("admin"), handler("device")).ServeHTTP(httptest.NewRecorder(), request)
			if calls != 1 || got != test.owner {
				t.Fatal("wrong or duplicate consumer", got, calls)
			}
		})
	}
}
