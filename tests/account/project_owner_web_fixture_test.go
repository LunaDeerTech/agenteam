//go:build integration

package account_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/app"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objectc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	"github.com/LunaDeerTech/agenteam/tests/testsupport/accountenv"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
)

const projectOwnerWebPath = "/api/v1/projects"

type projectOwnerWebCredential struct {
	personalWebCredential
	Username string `json:"username"`
}

type projectOwnerWebAttempt struct {
	key  string
	body [32]byte
	csrf [32]byte
	path string
}

type projectOwnerWebHold struct {
	path              string
	release           chan struct{}
	once              sync.Once
	started, finished bool
}

type projectOwnerWebFixture struct {
	*authenticationWebFixture
	mode, evidence, inputHash string
	setup                     *personalWebFixture
	admin, owner, other       projectOwnerWebCredential
	adminClient               *http.Client
	adminCSRF                 string
	ownerClient               *http.Client
	ownerCSRF                 string
	adminActor                identity.Actor
	ownerActor, otherActor    identity.Actor
	store                     *postgres.Store
	projects                  *project.Service
	ids                       map[string]string
	initial                   map[string]any
	mu                        sync.Mutex
	attempts                  []projectOwnerWebAttempt
	dropPath, failReadPath    string
	failSession               bool
	lostKey                   string
	lostAttempt               projectOwnerWebAttempt
	lostProject               string
	receipt                   []byte
	dropped, replayed         int
	patches, lookups          int
	sameOriginal              bool
	hold                      *projectOwnerWebHold
	responseSequence          int
	stopProxy                 func()
}

// The production no-tag app.Run owns authentication, authorization, commands
// and all API responses. This private server only hosts a frozen dist and
// observes or truncates an already completed response; it is not SPA hosting.
func (f *projectOwnerWebFixture) startRoot(t *testing.T, ctx context.Context) config.Config {
	t.Helper()
	dist := os.Getenv("AGENTEAM_PROJECT_OWNER_WEB_DIST")
	runtime := os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 {
		t.Fatal("explicit frozen Project dist and short owned browser runtime required")
	}
	if _, err := os.Stat(filepath.Join(dist, "index.html")); err != nil {
		t.Fatal("frozen production dist missing")
	}
	f.evidence = os.Getenv("AGENTEAM_PROJECT_OWNER_WEB_EVIDENCE")
	f.inputHash = os.Getenv("AGENTEAM_PROJECT_OWNER_WEB_INPUT_HASH")
	decoded, err := hex.DecodeString(f.inputHash)
	if !filepath.IsAbs(f.evidence) || len(decoded) != sha256.Size || err != nil {
		t.Fatal("explicit safe evidence directory and frozen input hash required")
	}
	f.evidence = filepath.Join(f.evidence, t.Name())
	if err := os.Mkdir(f.evidence, 0700); err != nil {
		t.Fatal("fresh Project evidence directory required")
	}
	directory, err := os.MkdirTemp(runtime, "owner-")
	if err != nil {
		t.Fatal("private browser runtime unavailable")
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Error("private Project runtime cleanup failed")
		}
		if _, err := os.Lstat(directory); !errors.Is(err, os.ErrNotExist) {
			t.Error("private Project runtime remains")
		}
	})
	db := pgfixture.NewDatabase(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("owned Project listener unavailable")
	}
	t.Cleanup(func() { _ = listener.Close() })
	origin := "http://" + listener.Addr().String()
	values := accountenv.New(t).Values()
	objects, err := objectfixture.Environment(ctx, db.Name)
	if err != nil {
		t.Fatal("owned full object fixture required")
	}
	for _, entry := range objects {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	for key, value := range map[string]string{
		"DATABASE_URL": db.Fixture.URL(db.Name), "DATABASE_CA_FILE": db.Fixture.CAFile,
		"DATABASE_STARTUP_TIMEOUT": "15s", "HTTP_ADDR": "127.0.0.1:0", "PUBLIC_ORIGIN": origin, "SHUTDOWN_TIMEOUT": "2s",
		"CURSOR_KEYRING":                 `{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`,
		"SECRET_KEYRING":                 `{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="}]}`,
		"KNOWLEDGE_CONFIRMATION_KEYRING": `{"format":1,"current_kid":"knowledge","keys":[{"kid":"knowledge","key_b64":"gIGCg4SFhoeIiYqLjI2Oj5CRkpOUlZaXmJmam5ydnp8="}]}`,
	} {
		values[config.Prefix+key] = value
	}
	var environment []string
	for key, value := range values {
		environment = append(environment, key+"="+value)
	}
	cfg, err := config.Load(func(key string) (string, bool) { value, ok := values[key]; return value, ok }, environment)
	if err != nil {
		t.Fatal("owned default root configuration invalid")
	}
	output := &httpFixtureLog{listening: make(chan string, 1)}
	logger, err := logging.New(logging.Central, slog.LevelInfo, output)
	if err != nil {
		t.Fatal(err)
	}
	rootCtx, cancel := context.WithCancel(ctx)
	rootDone := make(chan error, 1)
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-rootDone:
			if err != nil {
				t.Error("Project default root shutdown failed", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Project default root exceeded cleanup observation budget; ownership retained until actual return")
			<-rootDone
		}
		t.Log("Project default root actual join")
	})
	go func() { rootDone <- app.Run(rootCtx, cfg, logger, nil) }()
	var address string
	select {
	case address = <-output.listening:
	case err := <-rootDone:
		rootDone <- err
		t.Fatal("Project default root initialization failed", err)
	case <-ctx.Done():
		t.Fatal("Project root startup exhausted top budget")
	}
	backend, err := url.Parse("http://" + address)
	if err != nil {
		t.Fatal(err)
	}
	old := &httpFixture{t: t, db: db, config: cfg, address: address, log: output}
	entry := old.record("bootstrap", "admin@mail.com", "")
	f.authenticationWebFixture = &authenticationWebFixture{t: t, db: db, origin: origin, directory: directory, webRoot: dist, entry: entry, log: output, record: old.record, recoveryLogPath: cfg.AccountRecoveryLog()}
	transport := &http.Transport{Proxy: nil}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport = transport
	proxy.ModifyResponse = f.controlResponse
	proxy.ErrorLog = slog.NewLogLogger(slog.NewTextHandler(io.Discard, nil), slog.LevelError)
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		var lost *projectOwnerWebLost
		if !errors.As(err, &lost) {
			http.Error(w, "owned Project API unavailable", http.StatusBadGateway)
			return
		}
		for key, values := range lost.header {
			w.Header()[key] = append([]string(nil), values...)
		}
		w.Header().Del("Transfer-Encoding")
		w.Header().Set("Content-Length", strconv.Itoa(lost.length))
		w.Header().Set("Connection", "close")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{"))
		_ = http.NewResponseController(w).Flush()
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			_ = conn.Close()
		}
	}
	assets := http.FileServer(http.Dir(dist))
	var handlers sync.WaitGroup
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlers.Add(1)
		defer handlers.Done()
		if r.URL.Path == "/api/v1" || strings.HasPrefix(r.URL.Path, "/api/v1/") {
			if !f.observeRequest(w, r) {
				return
			}
			proxy.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method unavailable", http.StatusMethodNotAllowed)
			return
		}
		clean := filepath.Clean("/" + r.URL.Path)
		if info, err := os.Stat(filepath.Join(dist, clean)); err == nil && !info.IsDir() {
			assets.ServeHTTP(w, r)
			return
		}
		// Legal Project names may contain dots. Classify the original request
		// path before treating an extension as a missing static asset; the
		// frontend still performs its independent raw-route and API checks.
		if projectOwnerWebPage(r.RequestURI) {
			w.Header().Set("Cache-Control", "no-store")
			http.ServeFile(w, r, filepath.Join(dist, "index.html"))
			return
		}
		if strings.HasPrefix(clean, "/assets/") || filepath.Ext(clean) != "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, filepath.Join(dist, "index.html"))
	})}
	serverDone := make(chan error, 1)
	var stopProxyOnce sync.Once
	f.stopProxy = func() {
		stopProxyOnce.Do(func() {
			f.releaseRead()
			shutdown, stop := context.WithTimeout(context.Background(), 2*time.Second)
			defer stop()
			if err := server.Shutdown(shutdown); err != nil {
				t.Error("Project proxy exceeded graceful cleanup budget")
				_ = server.Close()
			}
			if err := <-serverDone; !errors.Is(err, http.ErrServerClosed) {
				t.Error("Project proxy Serve failed", err)
			}
			handlers.Wait()
			transport.CloseIdleConnections()
			t.Log("Project proxy Serve and all body handlers actually joined")
		})
	}
	t.Cleanup(f.stopProxy)
	go func() { serverDone <- server.Serve(listener) }()
	return cfg
}

func newProjectOwnerWebFixture(t *testing.T, ctx context.Context, mode string) *projectOwnerWebFixture {
	t.Helper()
	f := &projectOwnerWebFixture{mode: mode, ids: map[string]string{}, initial: map[string]any{}, sameOriginal: true}
	cfg := f.startRoot(t, ctx)
	f.setup = &personalWebFixture{authenticationWebFixture: f.authenticationWebFixture}
	f.admin = projectOwnerWebCredential{personalWebCredential: personalWebCredential{Email: f.entry.Email, Password: f.entry.Password, UserID: f.entry.ID}, Username: "admin"}
	admin, session := f.login(ctx, f.admin.personalWebCredential)
	f.admin.Username = httpString(t, httpObject(t, session, "user"), "username")
	csrf := httpString(t, session, "csrf_token")
	f.adminClient, f.adminCSRF, f.adminActor = admin, csrf, f.actor(session)
	f.owner = projectOwnerWebCredential{personalWebCredential: f.setup.inviteMember(ctx, admin, csrf, "owner-workspace@example.com", "owner-workspace"), Username: "owner-workspace"}
	f.other = projectOwnerWebCredential{personalWebCredential: f.setup.inviteMember(ctx, admin, csrf, "other-workspace@example.com", "other-workspace"), Username: "other-workspace"}
	f.ownerClient, session = f.login(ctx, f.owner.personalWebCredential)
	f.ownerCSRF = httpString(t, session, "csrf_token")
	f.ownerActor = f.actor(session)
	_, otherSession := f.login(ctx, f.other.personalWebCredential)
	f.otherActor = f.actor(otherSession)
	f.prepareService(ctx, cfg)
	for _, entry := range []struct{ key, name string }{{"main", "owner-main"}, {"duplicate", "owner-duplicate"}, {"archived", "owner-archived"}, {"archiving", "owner-archiving"}, {"deleting", "owner-deleting"}} {
		p := f.create(ctx, f.ownerActor, entry.name)
		f.ids[entry.key] = p.ID.String()
	}
	f.ids["other"] = f.create(ctx, f.otherActor, "other-project").ID.String()
	if mode == "read" {
		f.ids["dotted"] = f.create(ctx, f.ownerActor, "owner.dot-name").ID.String()
		for n := range 23 {
			p := f.create(ctx, f.ownerActor, fmt.Sprintf("owner-page-%02d", n))
			f.ids[fmt.Sprintf("page_%02d", n)] = p.ID.String()
		}
	}
	f.lifecycle(ctx, "archived", pc.Archived)
	f.lifecycle(ctx, "archiving", pc.Archiving)
	f.lifecycle(ctx, "deleting", pc.Deleting)
	var system map[string]any
	if mode == "identity" {
		f.ids["admin"] = f.create(ctx, f.adminActor, "admin-owner-project").ID.String()
		system = f.prepareSystemDrafts(ctx)
	}
	for key, target := range f.ids {
		if key != "deleting" && key != "other" {
			client := f.ownerClient
			if key == "admin" {
				client = f.adminClient
			}
			f.initial[key] = f.setup.setupRequest(ctx, client, http.MethodGet, projectOwnerWebPath+"/"+target, nil, "", false, http.StatusOK)
		}
	}
	f.private("project-owner-material.json", map[string]any{"admin": f.admin, "owner": f.owner, "other": f.other, "ids": f.ids, "projects": f.initial, "system": system})
	t.Cleanup(func() {
		// The proxy may still own a cancelled browser's response handler. Join
		// it before the later LIFO cleanups close the preparation store/guard.
		f.stopProxy()
		for _, secret := range []string{f.admin.Password, f.owner.Password, f.other.Password, f.ownerCSRF, f.adminCSRF} {
			if secret != "" && f.log.contains(secret) {
				t.Error("Project private material escaped restricted transfer")
			}
		}
		f.admin.Password, f.owner.Password, f.other.Password, f.ownerCSRF = "", "", "", ""
		f.adminCSRF = ""
		f.mu.Lock()
		f.lostKey = ""
		f.lostAttempt = projectOwnerWebAttempt{}
		f.attempts = nil
		clear(f.receipt)
		f.receipt = nil
		f.mu.Unlock()
	})
	return f
}

// This is the card's closed Project page path shape, not account creation's
// reserved-name policy. It runs on raw RequestURI: no URL parser, decoding or
// path cleaning can erase a forbidden query, escape, slash or dot segment.
func projectOwnerWebPage(raw string) bool {
	if !strings.HasPrefix(raw, "/") || strings.ContainsAny(raw, "?#%\\") || strings.Contains(raw, "//") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(raw, "/"), "/")
	if len(parts) != 2 && !(len(parts) == 3 && parts[2] == "settings") && !(len(parts) == 4 && parts[2] == "settings" && parts[3] == "general") {
		return false
	}
	username, err := pc.NormalizeUserRoute(parts[0])
	if err != nil {
		return false
	}
	switch username {
	case "api", "assets", "auth", "login", "logout", "invite", "reset", "settings", "system", "personal", "diagnostics", "livez", "readyz", "debug":
		return false
	}
	_, err = pc.NormalizeName(parts[1])
	return err == nil
}

// Reuse the accepted Summary harness's formal Provider/Model setup helpers on
// this root. No SDK is called and there is no Project-level Summary override.
// Two choices per purpose let the browser exercise each independent draft.
func (f *projectOwnerWebFixture) prepareSystemDrafts(ctx context.Context) map[string]any {
	m := &modelsWebFixture{authenticationWebFixture: f.authenticationWebFixture, setup: f.setup, admin: f.admin.personalWebCredential, adminClient: f.adminClient, csrf: f.adminCSRF, ids: map[string]string{}}
	names := map[string]string{}
	for _, role := range []struct{ name, protocol, kind string }{{"embedding", "openai-embeddings", "embedding"}, {"memory", "openai-chat-completions", "chat"}, {"reranker", "jina-rerank", "reranker"}, {"image", "openai-images-generations", "image_generation"}} {
		provider := m.createProvider(ctx, "Owner draft "+role.name+" Provider", role.protocol, "https://provider.invalid/v1", true)
		m.ids[role.name+"_provider"] = provider
		for _, suffix := range []string{"", "_replacement"} {
			key := role.name + suffix
			name := "Owner draft " + key
			m.ids[key] = m.createModel(ctx, provider, modelsWebInput(name, role.kind))
			names[key] = name
		}
	}
	m.selection(ctx, "initial", "")
	summary := m.request(ctx, http.MethodGet, summaryWebPath, nil, http.StatusOK)
	m.request(ctx, http.MethodPut, summaryWebPath, map[string]any{"id": summary["id"], "expected_version": summary["version"], "model": m.ids["memory"]}, http.StatusOK)
	selection := m.request(ctx, http.MethodGet, selectionWebPath, nil, http.StatusOK)
	summary = m.request(ctx, http.MethodGet, summaryWebPath, nil, http.StatusOK)
	m.ids["summary_0"], m.ids["summary_1"] = m.ids["memory"], m.ids["memory_replacement"]
	names["summary_0"], names["summary_1"] = names["memory"], names["memory_replacement"]
	m.csrf, m.admin.Password = "", ""
	return map[string]any{"ids": m.ids, "names": names, "selector_id": selection["id"], "summary_id": summary["id"], "selection": selection, "summary": summary}
}

func (f *projectOwnerWebFixture) login(ctx context.Context, credential personalWebCredential) (*http.Client, map[string]any) {
	client := f.setup.setupClient()
	bootstrap := f.setup.setupRequest(ctx, client, http.MethodGet, "/api/v1/auth/bootstrap", nil, "", false, http.StatusOK)
	f.setup.setupRequest(ctx, client, http.MethodPost, "/api/v1/sessions/login", map[string]string{"email": credential.Email, "password": credential.Password}, httpString(f.t, bootstrap, "csrf_token"), true, http.StatusOK)
	return client, f.setup.setupRequest(ctx, client, http.MethodGet, "/api/v1/session", nil, "", false, http.StatusOK)
}

func (f *projectOwnerWebFixture) actor(session map[string]any) identity.Actor {
	user, err := foundation.ParseID[identity.User](httpString(f.t, httpObject(f.t, session, "user"), "id"))
	if err != nil {
		f.t.Fatal("formal Session User ID invalid")
	}
	sid, err := foundation.ParseID[identity.Session](httpString(f.t, httpObject(f.t, session, "session"), "id"))
	if err != nil {
		f.t.Fatal("formal Session ID invalid")
	}
	actor, err := identity.NewHuman(user, sid)
	if err != nil {
		f.t.Fatal("formal Session actor invalid")
	}
	return actor
}

// Creation uses the accepted service on the root's migrated database. The
// root remains the sole dispatcher: a second empty Outbox Runtime would reject
// its already registered Account mail handler. CheckStorage is the formal
// append-only initialization contract, not a fake handler or a bypass.
func (f *projectOwnerWebFixture) prepareService(ctx context.Context, cfg config.Config) {
	f.t.Helper()
	store, err := postgres.Open(ctx, f.db.Config(f.t, nil))
	if err != nil {
		f.t.Fatal("Project preparation store unavailable", err)
	}
	f.store = store
	f.t.Cleanup(func() {
		closeCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		if err := store.ForceClose(closeCtx); err != nil {
			f.t.Error("Project preparation store close failed", err)
			_ = store.ForceClose(context.Background())
		}
	})
	accounts, err := account.NewAuthority(store, cfg.AccountKeyring())
	if err != nil {
		f.t.Fatal(err)
	}
	if err = accounts.Initialize(ctx); err != nil {
		f.t.Fatal(err)
	}
	authority, err := project.NewAuthority(store, project.AuthorityDependencies{Sessions: accounts, Routes: accounts})
	if err != nil {
		f.t.Fatal(err)
	}
	aud, err := audit.New(store, cfg.CursorKeyring(), audit.Authorizations{Sessions: accounts, System: accounts, Accounts: accounts, Projects: authority})
	if err != nil {
		f.t.Fatal(err)
	}
	if err = aud.CheckStorage(ctx); err != nil {
		f.t.Fatal(err)
	}
	catalog := ec.NewCatalog()
	typed, err := pc.RegisterProjectEvents(catalog)
	if err != nil {
		f.t.Fatal(err)
	}
	process := id[objectc.Process](f.t)
	spool, err := object.OpenSpool(filepath.Join(f.directory, "prepare-spool"), process)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() {
		if err := spool.Close(); err != nil {
			f.t.Error("Project preparation spool close failed", err)
		}
	})
	guard, err := object.OpenProcessGuard(spool, process)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() {
		if err := guard.Close(); err != nil {
			f.t.Error("Project preparation guard close failed", err)
		}
	})
	processes := projectOwnerWebProcesses{guard: guard, process: process}
	journal, err := outbox.New(store, catalog, outbox.Authorizations{Producers: map[ec.StableName]oc.ProducerAuthority{pc.ProjectProducer: authority}, Projects: authority, Sessions: accounts, System: accounts, Audit: aud, Cursors: cfg.CursorKeyring(), Processes: processes})
	if err != nil {
		f.t.Fatal(err)
	}
	if err := journal.CheckStorage(ctx); err != nil {
		f.t.Fatal("Project formal Outbox storage check failed", err)
	}
	if _, err := store.Exec(ctx, `CREATE SCHEMA owner_web_fixture; CREATE TABLE owner_web_fixture.skills(creation_id uuid PRIMARY KEY,project_id uuid NOT NULL UNIQUE,init_key text NOT NULL,skill_id uuid NOT NULL UNIQUE,revision bigint NOT NULL CHECK(revision>0),protected boolean NOT NULL,published boolean NOT NULL)`); err != nil {
		f.t.Fatal("owned Skills preparation schema unavailable", err)
	}
	skills := &projectOwnerWebSkills{store: store, authority: authority, issuer: pc.NewInitializationPlanIssuer()}
	service, err := project.New(store, project.Dependencies{Authority: authority, Activity: accounts, Audit: aud, Events: journal, ProjectEvents: typed, Initializer: skills, Processes: processes, Cursors: cfg.CursorKeyring(), LifecycleRegistry: projectOwnerWebRegistry(f.t)}, project.DefaultConfig())
	if err != nil {
		f.t.Fatal(err)
	}
	f.projects = service
	f.t.Cleanup(func() {
		service.Stop()
		drain, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		if err := service.Drain(drain); err != nil {
			f.t.Error("Project preparation exceeded drain observation budget", err)
			_ = service.Drain(context.Background())
		}
		f.t.Log("Project preparation service actually joined before ProcessGuard release")
	})
}

type projectOwnerWebProcesses struct {
	guard   *object.ProcessGuard
	process objectc.ProcessID
}

func (p projectOwnerWebProcesses) CurrentProcess() oc.ProcessID {
	id, _ := foundation.ParseID[oc.Process](p.process.String())
	return id
}

func (p projectOwnerWebProcesses) ConfirmStopped(ctx context.Context, process oc.ProcessID) error {
	id, err := foundation.ParseID[objectc.Process](process.String())
	if err != nil {
		return err
	}
	return p.guard.ConfirmStopped(ctx, id)
}

func (f *projectOwnerWebFixture) create(ctx context.Context, actor identity.Actor, name string) pc.ProjectRef {
	f.t.Helper()
	result, err := f.projects.CreateProject(ctx, actor, foundation.CommandMeta{RequestID: id[foundation.Request](f.t), IdempotencyKey: foundation.IdempotencyKey(id[struct{}](f.t).String())}, pc.CreateProjectRequest{ProjectID: id[identity.Project](f.t), Name: name, Description: "Owner browser fixture " + name})
	if err != nil || result.State != pc.CreationReady || result.Project == nil {
		f.t.Fatal("formal Project creation did not reach ready", err)
	}
	return *result.Project
}

// Auxiliary terminal SQL is limited to an operation first accepted by the
// real Project service. It supplies read-only page inputs and does not claim
// participant execution, Object joins, or lifecycle completion acceptance.
func (f *projectOwnerWebFixture) lifecycle(ctx context.Context, key string, state pc.Lifecycle) {
	target, err := foundation.ParseID[identity.Project](f.ids[key])
	if err != nil {
		f.t.Fatal("owned lifecycle target missing")
	}
	actor, username := f.ownerActor, f.owner.Username
	if key == "admin" {
		actor, username = f.adminActor, f.admin.Username
	}
	current, err := f.projects.GetProject(ctx, actor, target)
	if err != nil {
		f.t.Fatal(err)
	}
	meta := foundation.CommandMeta{RequestID: id[foundation.Request](f.t), IdempotencyKey: foundation.IdempotencyKey(id[struct{}](f.t).String()), ExpectedVersion: &current.Version}
	if state == pc.Deleting {
		result, err := f.projects.BeginDeleteProject(ctx, actor, meta, target, pc.DeleteProjectRequest{NormalizedCurrentPath: username + "/" + current.NormalizedName, Permanent: true})
		if err != nil || result.Operation == nil {
			f.t.Fatal("formal deleting input failed", err)
		}
		return
	}
	op, err := f.projects.BeginArchive(ctx, actor, meta, target)
	if err != nil {
		f.t.Fatal("formal archive acceptance failed", err)
	}
	if state != pc.Archived {
		return
	}
	result := f.store.WithinTx(ctx, cause(f.t), func(ctx context.Context, tx foundation.Tx) error {
		lock, _ := foundation.ProjectLock(target.String())
		if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: lock, Mode: foundation.Exclusive}}); err != nil {
			return err
		}
		x, err := f.store.InTx(tx)
		if err != nil {
			return err
		}
		var at time.Time
		if err := x.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
			return err
		}
		if _, err := x.Exec(ctx, `UPDATE agenteam_project.lifecycle_participants SET stop_state='stopped' WHERE operation_id=$1`, op.ID.String()); err != nil {
			return err
		}
		if _, err := x.Exec(ctx, `UPDATE agenteam_project.lifecycle_operations SET state='completed',completed_project_version=project_version+1,completed_at=$2,updated_at=$2,version=version+1 WHERE id=$1`, op.ID.String(), at); err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_project.projects SET lifecycle='archived',archived_at=$2,updated_at=$2,version=version+1 WHERE id=$1`, target.String(), at)
		return err
	})
	if result.State() != foundation.Committed {
		f.t.Fatal("owned archived read fact failed", result.Fault())
	}
	f.t.Log("auxiliary archived fact prepared after real acceptance; lifecycle participants were not executed")
}

// The only isolated dependency is unbound D10 Skills. Facts are durable in a
// private schema, protected/published and rechecked through the accepted
// initialization authority in the caller's exact locked transaction.
type projectOwnerWebSkills struct {
	store     *postgres.Store
	authority *project.Authority
	issuer    pc.InitializationPlanIssuer
}

func (s *projectOwnerWebSkills) InspectProjectSkills(ctx context.Context, actor identity.Actor, r pc.InitializationRequest) (pc.InitializationResult, error) {
	if actor.Details().ServiceName != identity.ProjectInitialization || actor.Details().CauseRef != r.CreationID.String() || actor.Details().ProjectID != r.ProjectID.String() {
		return pc.InitializationResult{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	var projectID, key, skill string
	var revision int64
	err := s.store.QueryRow(ctx, `SELECT project_id::text,init_key,skill_id::text,revision FROM owner_web_fixture.skills WHERE creation_id=$1 AND protected AND published`, r.CreationID.String()).Scan(&projectID, &key, &skill, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return pc.InitializationResult{State: pc.InitializationResultPending, CreationID: r.CreationID, ProjectID: r.ProjectID, SafeReason: pc.ReasonWorkPending}, nil
	}
	if err != nil {
		return pc.InitializationResult{}, err
	}
	if projectID != r.ProjectID.String() || key != string(r.InitializationKey) {
		return pc.InitializationResult{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	skillID, err := foundation.ParseID[pc.Skill](skill)
	if err != nil {
		return pc.InitializationResult{}, err
	}
	rev := foundation.Revision(revision)
	return pc.InitializationResult{State: pc.InitializationCompleted, CreationID: r.CreationID, ProjectID: r.ProjectID, AddSkillsID: &skillID, Revision: &rev}, nil
}

func (s *projectOwnerWebSkills) InitializeProjectSkills(ctx context.Context, actor identity.Actor, r pc.InitializationRequest) (pc.InitializationResult, error) {
	lock, _ := foundation.ProjectLock(r.ProjectID.String())
	cause, err := foundation.NewRecoveryCause("project.owner-web-skills", r.CreationID.String(), "")
	if err != nil {
		return pc.InitializationResult{}, err
	}
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: lock, Mode: foundation.Exclusive}}); err != nil {
			return err
		}
		if err := s.authority.ValidateInitializationInTx(ctx, tx, actor, r.CreationID, r.ProjectID, r.InitializationKey); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		skill, err := foundation.NewID[pc.Skill]()
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `INSERT INTO owner_web_fixture.skills(creation_id,project_id,init_key,skill_id,revision,protected,published) VALUES($1,$2,$3,$4,1,true,true) ON CONFLICT(creation_id) DO NOTHING`, r.CreationID.String(), r.ProjectID.String(), string(r.InitializationKey), skill.String())
		return err
	})
	if result.State() != foundation.Committed {
		return pc.InitializationResult{}, foundation.NewFault(foundation.DependencyUnavailable, result.State())
	}
	return s.InspectProjectSkills(ctx, actor, r)
}

func (s *projectOwnerWebSkills) DiscoverConfirmation(ctx context.Context, actor identity.Actor, r pc.InitializationRequest) (pc.InitializationConfirmationPlan, error) {
	result, err := s.InspectProjectSkills(ctx, actor, r)
	if err != nil {
		return pc.InitializationConfirmationPlan{}, err
	}
	if result.State != pc.InitializationCompleted {
		return pc.InitializationConfirmationPlan{}, foundation.NewFault(foundation.InvalidState, foundation.NotStarted)
	}
	lock, _ := foundation.ProjectLock(r.ProjectID.String())
	return s.issuer.Plan(actor, r, pc.InitializationReceipt{CreationID: r.CreationID, ProjectID: r.ProjectID, AddSkillsID: *result.AddSkillsID, Revision: *result.Revision}, []foundation.LockRequest{{Key: lock, Mode: foundation.Exclusive}})
}

func (s *projectOwnerWebSkills) ConfirmInitializedInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, r pc.InitializationRequest, plan pc.InitializationConfirmationPlan) (pc.InitializationReceipt, error) {
	if !s.issuer.Matches(plan, actor, r) {
		return pc.InitializationReceipt{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	if err := s.store.RequireHeldLocks(ctx, tx, plan.RequiredLocks()); err != nil {
		return pc.InitializationReceipt{}, err
	}
	if err := s.authority.ValidateInitializationInTx(ctx, tx, actor, r.CreationID, r.ProjectID, r.InitializationKey); err != nil {
		return pc.InitializationReceipt{}, err
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return pc.InitializationReceipt{}, err
	}
	receipt := plan.ProposedReceipt()
	var valid bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM owner_web_fixture.skills WHERE creation_id=$1 AND project_id=$2 AND init_key=$3 AND skill_id=$4 AND revision=$5 AND protected AND published)`, r.CreationID.String(), r.ProjectID.String(), string(r.InitializationKey), receipt.AddSkillsID.String(), int64(receipt.Revision)).Scan(&valid)
	if err != nil {
		return pc.InitializationReceipt{}, err
	}
	if !valid {
		return pc.InitializationReceipt{}, foundation.NewFault(foundation.InvalidState, foundation.NotStarted)
	}
	return receipt, nil
}

type projectOwnerWebParticipant struct{ name pc.ParticipantName }

func (p projectOwnerWebParticipant) Name() pc.ParticipantName { return p.name }
func (p projectOwnerWebParticipant) RequestStop(context.Context, identity.Actor, pc.LifecycleCause, pc.ScopeRef) (pc.StopReport, error) {
	return pc.StopReport{}, errors.New("Owner UI preparation must not execute lifecycle participants")
}
func (p projectOwnerWebParticipant) InspectStop(context.Context, identity.Actor, pc.LifecycleCause, pc.ScopeRef) (pc.StopReport, error) {
	return pc.StopReport{}, errors.New("Owner UI preparation must not execute lifecycle participants")
}
func (p projectOwnerWebParticipant) Cleanup(context.Context, identity.Actor, pc.LifecycleCause, pc.ScopeRef, *pc.CleanupCheckpoint) (pc.CleanupReport, error) {
	return pc.CleanupReport{}, errors.New("Owner UI preparation must not execute lifecycle participants")
}

func projectOwnerWebRegistry(t *testing.T) *project.LifecycleRegistry {
	t.Helper()
	entries := []pc.ParticipantRegistration{
		{Name: pc.ArtifactObjectParticipant, ContractVersion: 1, OwnerModule: "artifact-object", ReferenceKinds: []pc.ReferenceKind{"object"}},
		{Name: pc.SecretParticipant, ContractVersion: 1, OwnerModule: "secret", ReferenceKinds: []pc.ReferenceKind{"secret"}},
		{Name: pc.OutboxParticipant, ContractVersion: 1, OwnerModule: "outbox", CleanupAfter: []pc.ParticipantName{pc.ArtifactObjectParticipant, pc.SecretParticipant}},
		{Name: pc.AuditParticipant, ContractVersion: 1, OwnerModule: "audit", CleanupAfter: []pc.ParticipantName{pc.OutboxParticipant}},
	}
	manifest, err := pc.NewRequiredManifest(entries)
	if err != nil {
		t.Fatal(err)
	}
	var bindings []project.LifecycleParticipantBinding
	for _, entry := range entries {
		bindings = append(bindings, project.LifecycleParticipantBinding{Registration: entry, Participant: projectOwnerWebParticipant{name: entry.Name}})
	}
	registry, err := project.NewLifecycleRegistry(manifest, bindings)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func projectOwnerWebEndpoint(path string) bool {
	if path == projectOwnerWebPath || path == projectOwnerWebPath+"/resolve" {
		return true
	}
	tail, ok := strings.CutPrefix(path, projectOwnerWebPath+"/")
	if !ok {
		return false
	}
	tail = strings.TrimSuffix(tail, "/commands/lookup")
	_, err := foundation.ParseID[identity.Project](tail)
	return err == nil
}

type projectOwnerWebBody struct {
	*bytes.Reader
	raw []byte
}

func (b *projectOwnerWebBody) Close() error { clear(b.raw); return nil }

func (f *projectOwnerWebFixture) observeRequest(w http.ResponseWriter, r *http.Request) bool {
	f.mu.Lock()
	fail := r.Method == http.MethodGet && (f.failSession && r.URL.Path == "/api/v1/session" || f.failReadPath != "" && r.URL.Path == f.failReadPath)
	if fail {
		if r.URL.Path == "/api/v1/session" {
			f.failSession = false
		} else {
			f.failReadPath = ""
		}
	}
	if r.Method == http.MethodPost && projectOwnerWebEndpoint(r.URL.Path) && strings.HasSuffix(r.URL.Path, "/commands/lookup") {
		f.lookups++
	}
	f.mu.Unlock()
	if fail {
		// An explicit transport failure, never a fabricated Project Problem,
		// receipt, authorization result or PostgreSQL state.
		http.Error(w, "owned bounded read unavailable", http.StatusServiceUnavailable)
		return false
	}
	if r.Method != http.MethodPatch || !projectOwnerWebEndpoint(r.URL.Path) {
		return true
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 65537))
	closeErr := r.Body.Close()
	if err != nil || closeErr != nil || len(raw) > 65536 {
		clear(raw)
		http.Error(w, "owned mutation observation unavailable", http.StatusBadRequest)
		return false
	}
	f.mu.Lock()
	f.patches++
	if len(f.attempts) >= 128 {
		f.mu.Unlock()
		clear(raw)
		http.Error(w, "owned mutation observation bound reached", http.StatusServiceUnavailable)
		return false
	}
	f.attempts = append(f.attempts, projectOwnerWebAttempt{key: r.Header.Get("Idempotency-Key"), body: sha256.Sum256(raw), csrf: sha256.Sum256([]byte(r.Header.Get("X-CSRF-Token"))), path: r.URL.Path})
	f.mu.Unlock()
	r.Body = &projectOwnerWebBody{Reader: bytes.NewReader(raw), raw: raw}
	return true
}

type projectOwnerWebLost struct {
	header http.Header
	length int
}

func (*projectOwnerWebLost) Error() string { return "owned committed response deliberately truncated" }

func (f *projectOwnerWebFixture) controlResponse(response *http.Response) error {
	r := response.Request
	if r == nil || !projectOwnerWebEndpoint(r.URL.Path) {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 5*1024*1024+1))
	closeErr := response.Body.Close()
	if err != nil || closeErr != nil || len(raw) > 5*1024*1024 || !json.Valid(raw) {
		clear(raw)
		return errors.New("owned Project response incomplete")
	}
	if err := f.saveResponse(response, raw); err != nil {
		clear(raw)
		f.t.Error("safe original Project response recording failed")
		return err
	}
	response.Body = &projectOwnerWebBody{Reader: bytes.NewReader(raw), raw: raw}
	f.mu.Lock()
	hold := f.hold
	if r.Method != http.MethodGet || hold == nil || hold.path != r.URL.Path || hold.started {
		hold = nil
	} else {
		hold.started = true
	}
	f.mu.Unlock()
	if hold != nil {
		// The backend body has fully returned. Waiting here measures the
		// browser's actual transport tail, not the backend transaction tail.
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-hold.release:
		case <-r.Context().Done():
		case <-timer.C:
		}
		timer.Stop()
		f.mu.Lock()
		hold.finished = true
		f.mu.Unlock()
	}
	if r.Method != http.MethodPatch || response.StatusCode != http.StatusOK {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	key := r.Header.Get("Idempotency-Key")
	var attempt projectOwnerWebAttempt
	for n := len(f.attempts) - 1; n >= 0; n-- {
		if f.attempts[n].key == key {
			attempt = f.attempts[n]
			break
		}
	}
	if f.lostKey != "" && key == f.lostKey {
		f.replayed++
		f.sameOriginal = f.sameOriginal && attempt == f.lostAttempt && bytes.Equal(raw, f.receipt)
	}
	if f.dropPath != r.URL.Path {
		return nil
	}
	var receipt map[string]any
	expectedOwner := f.owner.UserID
	if r.URL.Path == projectOwnerWebPath+"/"+f.ids["admin"] {
		expectedOwner = f.admin.UserID
	}
	if json.Unmarshal(raw, &receipt) != nil || receipt["id"] != strings.TrimPrefix(r.URL.Path, projectOwnerWebPath+"/") || receipt["owner_user_id"] != expectedOwner || receipt["lifecycle"] != "active" {
		return errors.New("owned committed Project receipt invalid")
	}
	var complete bool
	if err := f.store.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM agenteam_project.commands WHERE project_id=$1 AND command_name='update' AND key=$2 AND state='completed')`, receipt["id"], key).Scan(&complete); err != nil || !complete {
		return errors.New("completed Project command fact missing before response loss")
	}
	f.dropPath = ""
	f.lostKey, f.lostAttempt, f.lostProject = key, attempt, receipt["id"].(string)
	clear(f.receipt)
	f.receipt = append([]byte(nil), raw...)
	f.dropped++
	_ = response.Body.Close()
	return &projectOwnerWebLost{header: response.Header.Clone(), length: len(raw)}
}

func (f *projectOwnerWebFixture) saveResponse(response *http.Response, raw []byte) error {
	// Only the five safe Project endpoints enter here; request bodies, queries,
	// Cookies, CSRF and idempotency keys are deliberately absent from sidecars.
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.responseSequence >= 512 {
		return errors.New("owned safe response bound reached")
	}
	f.responseSequence++
	sum := fmt.Sprintf("%x", sha256.Sum256(raw))
	bodyName := "body-" + sum + ".json"
	bodyPath := filepath.Join(f.evidence, bodyName)
	if existing, err := os.ReadFile(bodyPath); err == nil {
		if !bytes.Equal(existing, raw) {
			return errors.New("owned response digest mismatch")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else if err := os.WriteFile(bodyPath, raw, 0600); err != nil {
		return err
	}
	projectID := ""
	tail := strings.TrimPrefix(response.Request.URL.Path, projectOwnerWebPath+"/")
	tail = strings.TrimSuffix(tail, "/commands/lookup")
	if id, err := foundation.ParseID[identity.Project](tail); err == nil {
		projectID = id.String()
	}
	metadata, err := json.Marshal(map[string]any{
		"status": response.StatusCode, "content_type": response.Header.Get("Content-Type"), "request_id": response.Header.Get("X-Request-ID"),
		"endpoint": response.Request.URL.Path, "method": response.Request.Method, "project_id": projectID,
		"source_run": f.t.Name(), "input_hash": f.inputHash, "body_file": bodyName, "body_sha256": sum,
	})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(f.evidence, fmt.Sprintf("response-%03d.json", f.responseSequence)), metadata, 0600)
}

func (f *projectOwnerWebFixture) private(name string, value any) {
	f.t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		f.t.Fatal("private Project transfer encoding failed")
	}
	defer clear(raw)
	path := filepath.Join(f.directory, name)
	if err := os.WriteFile(path+".tmp", raw, 0600); err != nil {
		f.t.Fatal("private Project transfer write failed")
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		f.t.Fatal("private Project transfer publication failed")
	}
}

type projectOwnerWebIPC struct {
	Sequence    int     `json:"sequence"`
	Action      string  `json:"action"`
	Project     string  `json:"project,omitempty"`
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

func (f *projectOwnerWebFixture) releaseRead() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hold != nil {
		f.hold.once.Do(func() { close(f.hold.release) })
	}
}

func (f *projectOwnerWebFixture) ipc(ctx context.Context, r projectOwnerWebIPC) map[string]any {
	key := r.Project
	if key == "" {
		key = "main"
	}
	target, ok := f.ids[key]
	if !ok || key == "other" {
		f.t.Fatal("private Project IPC target rejected")
	}
	path := projectOwnerWebPath + "/" + target
	actor, client, csrf := f.ownerActor, f.ownerClient, f.ownerCSRF
	if key == "admin" {
		actor, client, csrf = f.adminActor, f.adminClient, f.adminCSRF
	}
	out := map[string]any{"sequence": r.Sequence}
	switch r.Action {
	case "observe":
		projectID, _ := foundation.ParseID[identity.Project](target)
		p, err := f.projects.GetProject(ctx, actor, projectID)
		if err != nil {
			f.t.Fatal("owned current Project observation failed", err)
		}
		out["project"] = p
		var commands, audits, events int
		if err := f.store.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_project.commands WHERE project_id=$1 AND command_name='update' AND state='completed'),(SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='project' AND resource_id=$1 AND action='project.update' AND outcome='success'),(SELECT count(*) FROM agenteam_outbox.events WHERE producer='project' AND project_id=$1 AND event_type='project.updated')`, target).Scan(&commands, &audits, &events); err != nil {
			f.t.Fatal("owned Project command observation failed", err)
		}
		out["update_commands"], out["update_audits"], out["update_events"] = commands, audits, events
		f.mu.Lock()
		out["dropped"], out["replayed"], out["same_original"] = f.dropped, f.replayed, f.sameOriginal
		out["patch_requests"], out["lookup_requests"] = f.patches, f.lookups
		f.mu.Unlock()
	case "drop-next-update":
		f.mu.Lock()
		f.dropPath = path
		f.mu.Unlock()
	case "fail-next-read":
		f.mu.Lock()
		f.failReadPath = path
		f.mu.Unlock()
	case "fail-session":
		f.mu.Lock()
		f.failSession = true
		f.mu.Unlock()
	case "hold-next-read":
		f.releaseRead()
		f.mu.Lock()
		f.hold = &projectOwnerWebHold{path: path, release: make(chan struct{})}
		f.mu.Unlock()
	case "hold-status":
		f.mu.Lock()
		out["started"] = f.hold != nil && f.hold.started
		out["finished"] = f.hold != nil && f.hold.finished
		f.mu.Unlock()
	case "release-read":
		f.releaseRead()
	case "update":
		current := f.setup.setupRequest(ctx, client, http.MethodGet, path, nil, "", false, http.StatusOK)
		body := map[string]any{"expected_version": current["version"]}
		if r.Name != nil {
			body["name"] = *r.Name
		}
		if r.Description != nil {
			body["description"] = *r.Description
		}
		out["project"] = f.setup.setupRequest(ctx, client, http.MethodPatch, path, body, csrf, true, http.StatusOK)
	case "archive":
		f.lifecycle(ctx, key, pc.Archived)
		out["fact_only"] = true
	case "reuse-name":
		if r.Name == nil {
			f.t.Fatal("owned name reuse needs explicit previous name")
		}
		p := f.create(ctx, f.ownerActor, *r.Name)
		f.mu.Lock()
		f.ids["reused"] = p.ID.String()
		f.mu.Unlock()
		out["project"] = p
	default:
		f.t.Fatal("private Project IPC action rejected")
	}
	return out
}

func (f *projectOwnerWebFixture) browser(ctx context.Context) map[string]any {
	f.t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	if err != nil {
		f.t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "project-owner.config.js"))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "TMPDIR" && key != "DEBUG" && key != "PWDEBUG" && key != "PLAYWRIGHT_NO_COPY_PROMPT" && !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "TMPDIR="+f.directory, "AGENTEAM_AUTH_WEB_ORIGIN="+f.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+f.directory, "AGENTEAM_PROJECT_OWNER_WEB_CASE="+f.mode, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_PROJECT_OWNER_WEB_EVIDENCE="+f.evidence, "PLAYWRIGHT_NO_COPY_PROMPT=1")
	if images := os.Getenv("AGENTEAM_AUTH_WEB_IMAGES"); images != "" {
		if !filepath.IsAbs(images) {
			f.t.Fatal("Project screenshots require explicit absolute directory")
		}
		cmd.Env = append(cmd.Env, "AGENTEAM_AUTH_WEB_IMAGES="+images)
	}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		f.t.Fatal("locked Project browser runner could not start")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	joined := false
	defer func() {
		if !joined {
			_ = cmd.Cancel()
			<-done
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cmd.Env = nil
		f.releaseRead()
		f.t.Log("Project Node direct child actually waited; adopted descendants remain owned by outer subreaper")
	}()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	sequence := 0
	var runErr error
wait:
	for {
		select {
		case runErr = <-done:
			joined = true
			break wait
		case <-ticker.C:
			raw, err := os.ReadFile(filepath.Join(f.directory, "project-owner-ipc.json"))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			var request projectOwnerWebIPC
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			decodeErr := decoder.Decode(&request)
			var tail any
			endErr := decoder.Decode(&tail)
			clear(raw)
			if err != nil || decodeErr != nil || endErr != io.EOF || request.Sequence < 1 || request.Sequence > 128 {
				f.t.Fatal("private Project IPC rejected")
			}
			if request.Sequence <= sequence {
				continue
			}
			if request.Sequence != sequence+1 {
				f.t.Fatal("private Project IPC sequence invalid")
			}
			reply := f.ipc(ctx, request)
			sequence = request.Sequence
			f.private("project-owner-ack-"+strconv.Itoa(sequence)+".json", reply)
		}
	}
	// The runner writes only safe assertion failures; remove even privately
	// captured credentials/keys before allowing those diagnostics into logs.
	safe := output.String()
	for _, secret := range []string{f.admin.Password, f.owner.Password, f.other.Password, f.ownerCSRF, f.adminCSRF} {
		if secret != "" {
			safe = strings.ReplaceAll(safe, secret, "[redacted]")
		}
	}
	f.mu.Lock()
	for _, attempt := range f.attempts {
		if attempt.key != "" {
			safe = strings.ReplaceAll(safe, attempt.key, "[redacted]")
		}
	}
	f.mu.Unlock()
	f.t.Log(safe)
	if runErr != nil {
		f.t.Fatalf("actual Project production browser failed: %v", runErr)
	}
	raw, err := os.ReadFile(filepath.Join(f.directory, "project-owner-result.json"))
	if err != nil {
		f.t.Fatal("safe Project browser result missing")
	}
	defer clear(raw)
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil || result["completed"] != true {
		f.t.Fatal("safe Project browser result invalid")
	}
	return result
}
