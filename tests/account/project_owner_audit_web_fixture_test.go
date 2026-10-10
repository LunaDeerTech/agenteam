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
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
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
)

// Private Audit harness: production app.Run remains the sole HTTP authority.
// Existing package-local Account/Project adapters are reused without edits.
// No Audit Append or SQL writes to Audit records are permitted here.
type projectOwnerAuditWebFixture struct {
	*projectOwnerWebFixture
	skills                   *projectOwnerAuditWebSkills
	counts                   map[string]int
	setupCookies             map[[32]byte]bool
	sessions                 map[string]auditWebSession
	browserActive            bool
	controlPath, controlKind string
	release                  chan struct{}
	releaseOnce              sync.Once
	renamed                  bool
	privateValues            []string
	proxyErrors              int
}
type projectOwnerAuditWebCut struct {
	header http.Header
	prefix []byte
	length int
}

type projectOwnerAuditWebCompletionKey struct{}

// One token belongs to one outer API handler. held is protected by f.mu;
// ModifyResponse may mark it, but only that handler's exit can complete it.
type projectOwnerAuditWebCompletion struct {
	held bool
}

func (*projectOwnerAuditWebCut) Error() string {
	return "owned Project Audit response intentionally truncated"
}

type projectOwnerAuditWebSkills struct {
	*projectOwnerWebSkills
	pending identity.ProjectID
}

func (s *projectOwnerAuditWebSkills) InitializeProjectSkills(ctx context.Context, actor identity.Actor, r pc.InitializationRequest) (pc.InitializationResult, error) {
	if r.ProjectID == s.pending {
		// Formal CreateProject persists pending. Inspect checks the exact service
		// actor/cause/project; no publication or initialized marker is fabricated.
		return s.projectOwnerWebSkills.InspectProjectSkills(ctx, actor, r)
	}
	return s.projectOwnerWebSkills.InitializeProjectSkills(ctx, actor, r)
}

func projectOwnerAuditWebPage(raw string) bool {
	if projectOwnerWebPage(raw) {
		return true
	}
	return strings.HasSuffix(raw, "/settings/audit") && projectOwnerWebPage(strings.TrimSuffix(raw, "/audit")+"/general")
}

func (f *projectOwnerAuditWebFixture) startRoot(t *testing.T, ctx context.Context) config.Config {
	t.Helper()
	dist := os.Getenv("AGENTEAM_PROJECT_AUDIT_WEB_DIST")
	runtime := os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 {
		t.Fatal("explicit frozen Project dist and short owned browser runtime required")
	}
	if _, err := os.Stat(filepath.Join(dist, "index.html")); err != nil {
		t.Fatal("frozen production dist missing")
	}
	f.evidence = os.Getenv("AGENTEAM_PROJECT_AUDIT_WEB_EVIDENCE")
	f.inputHash = os.Getenv("AGENTEAM_PROJECT_AUDIT_WEB_INPUT_HASH")
	decoded, err := hex.DecodeString(f.inputHash)
	if !filepath.IsAbs(f.evidence) || len(decoded) != sha256.Size || err != nil {
		t.Fatal("explicit safe evidence directory and frozen input hash required")
	}
	f.evidence = filepath.Join(f.evidence, t.Name())
	if err := os.Mkdir(f.evidence, 0700); err != nil {
		t.Fatal("fresh Project evidence directory required")
	}
	directory, err := os.MkdirTemp(runtime, "audit-")
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
	proxy.ModifyResponse = func(response *http.Response) error {
		err := f.controlResponse(response)
		var cut *projectOwnerAuditWebCut
		if err != nil && !errors.As(err, &cut) && (response.Request == nil || response.Request.Context().Err() == nil) {
			f.mu.Lock()
			f.proxyErrors++
			f.mu.Unlock()
		}
		return err
	}
	proxy.ErrorLog = slog.NewLogLogger(slog.NewTextHandler(io.Discard, nil), slog.LevelError)
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		var lost *projectOwnerAuditWebCut
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
		defer clear(lost.prefix)
		_, _ = w.Write(lost.prefix)
		_ = http.NewResponseController(w).Flush()
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			_ = conn.Close()
		}
	}
	assets := http.FileServer(http.Dir(dist))
	api := f.apiHandler(proxy)
	var handlers sync.WaitGroup
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlers.Add(1)
		defer handlers.Done()
		if r.URL.Path == "/api/v1" || strings.HasPrefix(r.URL.Path, "/api/v1/") {
			api.ServeHTTP(w, r)
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
		if projectOwnerAuditWebPage(r.RequestURI) {
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

func (f *projectOwnerAuditWebFixture) apiHandler(proxy http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !f.observeRequest(w, r) {
			return
		}
		completion := &projectOwnerAuditWebCompletion{}
		r = r.WithContext(context.WithValue(r.Context(), projectOwnerAuditWebCompletionKey{}, completion))
		f.count("server_started")
		defer func() {
			// ReverseProxy has returned or unwound, including its response body,
			// downstream writes and ErrorHandler. Neither release nor ctx.Done
			// can publish completion while any of that request still runs.
			f.mu.Lock()
			f.counts["server_finished"]++
			if completion.held {
				f.counts["joined"]++
			}
			f.mu.Unlock()
		}()
		proxy.ServeHTTP(w, r)
	})
}

func (f *projectOwnerAuditWebFixture) prepareService(ctx context.Context, cfg config.Config) {
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
	skills := &projectOwnerAuditWebSkills{projectOwnerWebSkills: &projectOwnerWebSkills{store: store, authority: authority, issuer: pc.NewInitializationPlanIssuer()}}
	skills.pending = id[identity.Project](f.t)
	f.skills = skills
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

func newProjectOwnerAuditWebFixture(t *testing.T, ctx context.Context, mode string) *projectOwnerAuditWebFixture {
	t.Helper()
	f := &projectOwnerAuditWebFixture{
		projectOwnerWebFixture: &projectOwnerWebFixture{mode: mode, ids: map[string]string{}, initial: map[string]any{}},
		counts:                 map[string]int{}, setupCookies: map[[32]byte]bool{}, sessions: map[string]auditWebSession{},
	}
	for _, key := range projectOwnerAuditWebCountKeys {
		f.counts[key] = 0
	}
	cfg := f.startRoot(t, ctx)
	f.setup = &personalWebFixture{authenticationWebFixture: f.authenticationWebFixture}
	f.admin = projectOwnerWebCredential{personalWebCredential: personalWebCredential{Email: f.entry.Email, Password: f.entry.Password, UserID: f.entry.ID}}
	var session map[string]any
	f.adminClient, session = f.login(ctx, f.admin.personalWebCredential)
	f.admin.Username = httpString(t, httpObject(t, session, "user"), "username")
	f.adminCSRF, f.adminActor = httpString(t, session, "csrf_token"), f.actor(session)
	f.owner = projectOwnerWebCredential{personalWebCredential: f.setup.inviteMember(ctx, f.adminClient, f.adminCSRF, "audit-owner@example.com", "audit-owner"), Username: "audit-owner"}
	f.other = projectOwnerWebCredential{personalWebCredential: f.setup.inviteMember(ctx, f.adminClient, f.adminCSRF, "audit-other@example.com", "audit-other"), Username: "audit-other"}
	f.ownerClient, session = f.login(ctx, f.owner.personalWebCredential)
	f.ownerCSRF, f.ownerActor = httpString(t, session, "csrf_token"), f.actor(session)
	_, session = f.login(ctx, f.other.personalWebCredential)
	f.otherActor = f.actor(session)
	f.prepareService(ctx, cfg)
	// Register before any preparation can fail: proxy handlers may still own
	// the preparation Store, so join them before its older LIFO cleanups.
	t.Cleanup(func() {
		f.stopProxy()
		f.mu.Lock()
		counts := map[string]int{}
		for _, key := range projectOwnerAuditWebCountKeys {
			counts[key] = f.counts[key]
		}
		retired := f.counts["held"] == f.counts["joined"] && f.counts["server_started"] == f.counts["server_finished"]
		proxyErrors := f.proxyErrors
		f.mu.Unlock()
		f.safeEvidence("proxy-retirement-facts.json", map[string]any{"mode": f.mode, "input_hash": f.inputHash, "counts": counts, "proxy_actual_join": retired, "observer_errors": proxyErrors, "test_failed_at_proxy_retirement": t.Failed(), "scope": "proxy Serve/handlers only; root/preparation Store/guard remain owned by later LIFO cleanups and outer actual retirement evidence"})
		for _, value := range append(append([]string(nil), f.privateValues...), f.admin.Password, f.owner.Password, f.other.Password, f.adminCSRF, f.ownerCSRF) {
			if value != "" && f.log.contains(value) {
				t.Error("Project Audit private setup material escaped restricted transfer")
			}
		}
		f.mu.Lock()
		clear(f.sessions)
		clear(f.setupCookies)
		f.privateValues = nil
		f.mu.Unlock()
		f.admin.Password, f.owner.Password, f.other.Password, f.adminCSRF, f.ownerCSRF = "", "", "", "", ""
	})
	for _, item := range []struct{ key, name string }{{"main", "audit-main"}, {"second", "audit-second"}, {"dotted", "audit.demo-v1"}, {"archiving", "audit-archiving"}, {"archived", "audit-archived"}, {"deleting", "audit-deleting"}} {
		f.ids[item.key] = f.create(ctx, f.ownerActor, item.name).ID.String()
	}
	f.ids["other"] = f.create(ctx, f.otherActor, "audit-other-project").ID.String()
	f.ids["admin"] = f.create(ctx, f.adminActor, "audit-admin-project").ID.String()
	pending, err := f.projects.CreateProject(ctx, f.ownerActor, foundation.CommandMeta{RequestID: id[foundation.Request](t), IdempotencyKey: foundation.IdempotencyKey(id[struct{}](t).String())}, pc.CreateProjectRequest{ProjectID: f.skills.pending, Name: "audit-pending"})
	if err != nil || pending.State != pc.CreationPending {
		t.Fatal("formal pending Project creation failed", err)
	}
	f.ids["pending"] = f.skills.pending.String()
	f.seed(ctx)
	f.lifecycle(ctx, "archiving", pc.Archiving)
	f.lifecycle(ctx, "archived", pc.Archived)
	f.lifecycle(ctx, "deleting", pc.Deleting)
	f.auxiliaryFacts(ctx)
	projects := map[string]any{}
	for _, key := range projectOwnerAuditWebProjectKeys {
		projects[key] = f.locator(ctx, key)
	}
	expected := f.seedFacts(ctx)
	f.safeEvidence("seed-facts.json", map[string]any{"project_id": f.ids["main"], "input_hash": f.inputHash, "expected": expected, "source": "read-only own rows after formal producer commands; not browser GET evidence"})
	var system map[string]any
	if mode == "navigation" {
		system = f.prepareSystemDrafts(ctx)
	}
	f.private("project-audit-material.json", map[string]any{"admin": f.admin, "owner": f.owner, "other": f.other, "projects": projects, "missing_id": id[struct{}](t).String(), "expected": expected, "system": system})
	return f
}

var projectOwnerAuditWebProjectKeys = []string{"main", "second", "other", "admin", "dotted", "archiving", "archived", "deleting", "pending"}
var projectOwnerAuditWebCountKeys = []string{"browser_list_gets", "browser_detail_gets", "setup_audit_gets", "control_audit_gets", "browser_project_mutations", "browser_command_lookups", "held", "joined", "cuts", "failures", "session_failures", "server_started", "server_finished"}

// Locators are restricted setup facts, not fabricated public Project DTOs.
func (f *projectOwnerAuditWebFixture) locator(ctx context.Context, key string) map[string]any {
	target, ok := f.ids[key]
	if !ok {
		f.t.Fatal("owned Project locator target invalid")
	}
	var owner, name, normalized, lifecycle string
	var initialized bool
	err := f.store.QueryRow(ctx, `SELECT owner_user_id::text,name,normalized_name,lifecycle,initialized_at IS NOT NULL FROM agenteam_project.projects WHERE id=$1`, target).Scan(&owner, &name, &normalized, &lifecycle, &initialized)
	if err != nil {
		f.t.Fatal("owned Project locator fact unavailable", err)
	}
	username := f.owner.Username
	if owner == f.admin.UserID {
		username = f.admin.Username
	} else if owner == f.other.UserID {
		username = f.other.Username
	} else if owner != f.owner.UserID {
		f.t.Fatal("foreign Project locator owner")
	}
	return map[string]any{"id": target, "username": username, "name": name, "normalized_name": normalized, "owner_user_id": owner, "initialized": initialized, "lifecycle": lifecycle}
}

// The no-tag production root performs every producer command. Merely storing
// an endpoint URL does not call that provider or bind Invocation consumers.
func (f *projectOwnerAuditWebFixture) seed(ctx context.Context) {
	path := projectOwnerWebPath + "/" + f.ids["main"]
	current := f.setup.setupRequest(ctx, f.ownerClient, http.MethodGet, path, nil, "", false, http.StatusOK)
	for n := 0; n < 3; n++ {
		description := "AUDIT-PRIVATE-DESCRIPTION-" + id[struct{}](f.t).String()
		f.privateValues = append(f.privateValues, description)
		current = f.setup.setupRequest(ctx, f.ownerClient, http.MethodPatch, path, map[string]any{"expected_version": current["version"], "description": description}, f.ownerCSRF, true, http.StatusOK)
	}
	secret := "AUDIT-PRIVATE-CREDENTIAL-" + id[struct{}](f.t).String()
	f.privateValues = append(f.privateValues, secret)
	credential := f.setup.setupRequest(ctx, f.ownerClient, http.MethodPost, path+"/model-credentials", map[string]any{"value": secret}, f.ownerCSRF, true, http.StatusOK)
	credentialID := httpString(f.t, credential, "credential_id")
	rotated := "AUDIT-PRIVATE-ROTATED-" + id[struct{}](f.t).String()
	f.privateValues = append(f.privateValues, rotated)
	f.setup.setupRequest(ctx, f.ownerClient, http.MethodPut, path+"/model-credentials/"+credentialID, map[string]any{"expected_version": "1", "value": rotated}, f.ownerCSRF, true, http.StatusOK)
	provider := f.setup.setupRequest(ctx, f.ownerClient, http.MethodPost, path+"/model-providers", map[string]any{"input": map[string]any{"name": "Audit Project provider", "protocol": "openai-chat-completions", "base_url": "https://audit-provider.invalid/v1", "enabled": true, "credential_ref": credentialID, "options": map[string]any{}}}, f.ownerCSRF, true, http.StatusOK)
	providerID := httpString(f.t, provider, "resource_id")
	input := map[string]any{"name": "Audit Project chat", "provider_model_id": "audit-fixture-chat", "type": "chat", "enabled": true, "parameters": map[string]any{}, "request_overwrite": map[string]any{}, "header_overwrite": map[string]any{}, "capabilities": map[string]any{"tool_calls": false, "parallel_tool_calls": false, "streaming": false, "reasoning": false, "input_modalities": []string{"text"}, "output_modalities": []string{"text"}, "reasoning_efforts": []string{}, "structured_output_modes": []string{}, "context_length": nil, "max_output": nil}}
	f.setup.setupRequest(ctx, f.ownerClient, http.MethodPost, path+"/models", map[string]any{"provider_id": providerID, "input": input}, f.ownerCSRF, true, http.StatusOK)
}

func (f *projectOwnerAuditWebFixture) seedFacts(ctx context.Context) map[string]any {
	rows, err := f.store.Query(ctx, `SELECT id::text,action,producer FROM agenteam_audit.audit_records WHERE scope='project' AND project_id=$1 ORDER BY created_at DESC,id DESC LIMIT 201`, f.ids["main"])
	if err != nil {
		f.t.Fatal("formal Audit producer facts unavailable", err)
	}
	defer rows.Close()
	ids, actions := []string{}, []string{}
	families := map[string]bool{}
	for rows.Next() {
		var recordID, action, producer string
		if err := rows.Scan(&recordID, &action, &producer); err != nil {
			f.t.Fatal("formal Audit producer fact invalid", err)
		}
		ids, actions = append(ids, recordID), append(actions, action)
		families[producer] = true
	}
	if rows.Err() != nil || len(ids) < 5 || len(ids) > 200 || !families["project"] || !families["secret"] || !families["model"] {
		f.t.Fatal("three formal Audit producer families not prepared")
	}
	return map[string]any{"main_record_ids": ids, "main_actions": actions, "main_producer_families": []string{"project", "secret", "model"}}
}

func (f *projectOwnerAuditWebFixture) count(key string) { f.mu.Lock(); f.counts[key]++; f.mu.Unlock() }
func (f *projectOwnerAuditWebFixture) source(r *http.Request) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.browserActive {
		return "setup"
	}
	if f.setupCookies[sha256.Sum256([]byte(r.Header.Get("Cookie")))] {
		return "control"
	}
	return "browser"
}
func projectOwnerAuditWebEndpoint(path string) (projectID, auditID string, ok bool) {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) != 5 && len(parts) != 6 || len(parts) < 5 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "projects" || parts[4] != "audit" {
		return "", "", false
	}
	p, err := foundation.ParseID[identity.Project](parts[3])
	if err != nil || p.String() != parts[3] {
		return "", "", false
	}
	if len(parts) == 6 {
		a, err := foundation.ParseID[struct{}](parts[5])
		if err != nil || a.String() != parts[5] {
			return "", "", false
		}
		auditID = a.String()
	}
	return p.String(), auditID, true
}
func (f *projectOwnerAuditWebFixture) observeRequest(_ http.ResponseWriter, r *http.Request) bool {
	source := f.source(r)
	if _, aid, ok := projectOwnerAuditWebEndpoint(r.URL.Path); ok && r.Method == http.MethodGet {
		key := source + "_audit_gets"
		if source == "browser" {
			key = "browser_list_gets"
			if aid != "" {
				key = "browser_detail_gets"
			}
		}
		f.count(key)
	}
	if source == "browser" && r.Method != http.MethodGet && r.Method != http.MethodHead {
		if r.URL.Path == projectOwnerWebPath || strings.HasPrefix(r.URL.Path, projectOwnerWebPath+"/") {
			f.count("browser_project_mutations")
		}
		if strings.HasSuffix(r.URL.Path, "commands/lookup") {
			f.count("browser_command_lookups")
		}
	}
	return true
}
func (f *projectOwnerAuditWebFixture) releaseRead() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.release != nil {
		f.releaseOnce.Do(func() { close(f.release) })
	}
}
func projectOwnerAuditWebReplace(response *http.Response, raw []byte) {
	response.Body = &projectOwnerWebBody{Reader: bytes.NewReader(raw), raw: raw}
	response.ContentLength = int64(len(raw))
	response.Header.Set("Content-Length", strconv.Itoa(len(raw)))
	response.Header.Del("Transfer-Encoding")
}
func (f *projectOwnerAuditWebFixture) controlResponse(response *http.Response) error {
	r := response.Request
	if r == nil || r.Method != http.MethodGet {
		return nil
	}
	source := f.source(r)
	if r.URL.Path == "/api/v1/session" && response.StatusCode == http.StatusOK {
		raw, err := io.ReadAll(io.LimitReader(response.Body, 600001))
		closeErr := response.Body.Close()
		var value struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
			Session struct {
				ID string `json:"id"`
			} `json:"session"`
			CSRF string `json:"csrf_token"`
		}
		if err != nil || closeErr != nil || len(raw) > 600000 || json.Unmarshal(raw, &value) != nil || value.User.ID == "" || value.Session.ID == "" || value.CSRF == "" || r.Header.Get("Cookie") == "" {
			clear(raw)
			return errors.New("owned Session observation invalid")
		}
		f.mu.Lock()
		if source == "setup" {
			f.setupCookies[sha256.Sum256([]byte(r.Header.Get("Cookie")))] = true
		}
		if source == "browser" {
			f.sessions[value.Session.ID] = auditWebSession{cookie: r.Header.Get("Cookie"), csrf: value.CSRF, user: value.User.ID}
		}
		fail := source == "browser" && f.failSession
		if fail {
			f.failSession = false
			f.counts["session_failures"]++
		}
		f.mu.Unlock()
		if fail {
			clear(raw)
			response.StatusCode, response.Status = http.StatusServiceUnavailable, "503 Service Unavailable"
			response.Header.Set("Content-Type", "text/plain; charset=utf-8")
			raw = []byte("owned bounded Session transport unavailable\n")
		}
		projectOwnerAuditWebReplace(response, raw)
		return nil
	}
	projectID, _, ok := projectOwnerAuditWebEndpoint(r.URL.Path)
	if !ok {
		return nil
	}
	// Evidence is only for this fixture's exact projects. Unknown project IDs
	// may still reach the real API, but cannot turn the observer into a dump.
	owned := false
	f.mu.Lock()
	for _, value := range f.ids {
		if value == projectID {
			owned = true
			break
		}
	}
	f.mu.Unlock()
	if !owned {
		return nil
	}
	cap := int64(600000)
	if response.StatusCode == http.StatusOK {
		cap = 1 << 20
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, cap+1))
	closeErr := response.Body.Close()
	if err != nil || closeErr != nil || int64(len(raw)) > cap || !json.Valid(raw) {
		clear(raw)
		return errors.New("owned Audit upstream did not complete within safe cap")
	}
	media := response.Header.Get("Content-Type")
	if response.StatusCode == http.StatusOK && !strings.HasPrefix(media, "application/json") || response.StatusCode != http.StatusOK && !strings.HasPrefix(media, "application/problem+json") {
		clear(raw)
		return errors.New("owned Audit upstream media invalid")
	}
	for _, private := range append(append([]string(nil), f.privateValues...), f.admin.Password, f.owner.Password, f.other.Password, f.adminCSRF, f.ownerCSRF) {
		if private != "" && bytes.Contains(raw, []byte(private)) {
			clear(raw)
			return errors.New("private setup value reached Audit projection")
		}
	}
	f.mu.Lock()
	kind, release := "forwarded", f.release
	if source == "browser" && response.StatusCode == http.StatusOK && f.controlPath == r.URL.Path {
		kind, f.controlPath = f.controlKind, ""
		f.controlKind = ""
		if kind == "hold" {
			completion, ok := r.Context().Value(projectOwnerAuditWebCompletionKey{}).(*projectOwnerAuditWebCompletion)
			if !ok || completion == nil || completion.held {
				f.mu.Unlock()
				clear(raw)
				return errors.New("owned Audit hold requires its live outer handler token")
			}
			completion.held = true
			f.counts["held"]++
		} else if kind == "cut" {
			f.counts["cuts"]++
		} else if kind == "failure" {
			f.counts["failures"]++
		}
	}
	f.mu.Unlock()
	if err := f.saveResponse(response, raw, source, kind); err != nil {
		clear(raw)
		return err
	}
	if kind == "hold" {
		select {
		case <-release:
		case <-r.Context().Done():
			err = r.Context().Err()
		}
		if err != nil {
			clear(raw)
			return err
		}
	}
	if kind == "cut" {
		if len(raw) < 2 {
			clear(raw)
			return errors.New("owned Audit cut needs nonempty completed body")
		}
		cut := &projectOwnerAuditWebCut{header: response.Header.Clone(), prefix: bytes.Clone(raw[:len(raw)/2]), length: len(raw)}
		clear(raw)
		return cut
	}
	if kind == "failure" {
		// Explicit transport failure after real read completion; never manufacture
		// a safe business Problem, authorization result or transaction outcome.
		clear(raw)
		response.StatusCode, response.Status = http.StatusServiceUnavailable, "503 Service Unavailable"
		response.Header.Set("Content-Type", "text/plain; charset=utf-8")
		raw = []byte("owned bounded Audit transport unavailable\n")
	}
	projectOwnerAuditWebReplace(response, raw)
	return nil
}

func (f *projectOwnerAuditWebFixture) saveResponse(response *http.Response, raw []byte, source, kind string) error {
	r := response.Request
	allowed := map[string]bool{"limit": true, "cursor": true, "from": true, "to": true, "actor_kind": true, "actor_id": true, "action": true, "outcome": true, "resource_kind": true, "resource_id": true, "tool_id": true, "execution_id": true, "operation_id": true, "approval_id": true, "runner_id": true, "agent_id": true}
	if len(r.URL.RawQuery) > 32768 {
		return errors.New("owned Audit query evidence too large")
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return errors.New("owned Audit query evidence malformed")
	}
	for key, values := range query {
		if !allowed[key] || len(values) != 1 || len(values[0]) > 8192 {
			return errors.New("owned Audit query evidence outside closed fields")
		}
	}
	projectID, auditID, ok := projectOwnerAuditWebEndpoint(r.URL.Path)
	if !ok || r.Method != http.MethodGet {
		return errors.New("owned Audit evidence target rejected")
	}
	sum := fmt.Sprintf("%x", sha256.Sum256(raw))
	bodyName := "body-" + sum + ".json"
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.responseSequence >= 512 {
		return errors.New("owned Audit evidence bound reached")
	}
	f.responseSequence++
	bodyPath := filepath.Join(f.evidence, bodyName)
	if existing, e := os.ReadFile(bodyPath); e == nil {
		if !bytes.Equal(existing, raw) {
			return errors.New("owned Audit digest collision")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	} else if e := os.WriteFile(bodyPath, raw, 0600); e != nil {
		return e
	}
	metadata, err := json.Marshal(map[string]any{
		"sequence": f.responseSequence, "source_run": f.t.Name(), "input_hash": f.inputHash, "source": source,
		"method": r.Method, "endpoint": r.URL.Path, "query": r.URL.RawQuery,
		"status": response.StatusCode, "content_type": response.Header.Get("Content-Type"), "content_length": response.Header.Get("Content-Length"), "request_id": response.Header.Get("X-Request-ID"),
		"project_id": projectID, "audit_id": auditID, "body_file": bodyName, "body_sha256": sum, "body_bytes": len(raw), "transfer_kind": kind, "body_stage": "complete_formal_upstream",
	})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(f.evidence, fmt.Sprintf("response-%03d.json", f.responseSequence)), metadata, 0600)
}

// Only exact own rows requested by the browser are compared. Typed decoding
// clips metadata to its official safe representation; nothing writes Audit.
func (f *projectOwnerAuditWebFixture) snapshot(ctx context.Context, key string, ids []string) []map[string]any {
	target := f.ids[key]
	if len(ids) > 200 {
		f.t.Fatal("owned Audit snapshot bound exceeded")
	}
	out := make([]map[string]any, 0, len(ids))
	seen := map[string]bool{}
	for _, recordID := range ids {
		parsed, err := foundation.ParseID[struct{}](recordID)
		if err != nil || parsed.String() != recordID || seen[recordID] {
			f.t.Fatal("owned Audit snapshot ID invalid")
		}
		seen[recordID] = true
		var at time.Time
		var kind, user, actorProject, agent, actorExecution, service, causeRef, action, outcome, resourceKind, resourceID, metadata string
		var tool, execution, call, operation, request, approval, runner, correlation, trace string
		err = f.store.QueryRow(ctx, `SELECT created_at,actor_kind,coalesce(user_id::text,''),coalesce(actor_project_id::text,''),coalesce(agent_id::text,''),coalesce(actor_execution_id::text,''),coalesce(service_name,''),coalesce(service_cause,''),action,outcome,resource_kind,coalesce(resource_id::text,''),metadata::text,coalesce(tool_id::text,''),coalesce(execution_id::text,''),coalesce(tool_call_id::text,''),coalesce(operation_id::text,''),coalesce(request_id::text,''),coalesce(approval_id::text,''),coalesce(runner_id::text,''),coalesce(correlation_id::text,''),coalesce(http_trace_id::text,'') FROM agenteam_audit.audit_records WHERE scope='project' AND project_id=$1 AND id=$2`, target, recordID).Scan(&at, &kind, &user, &actorProject, &agent, &actorExecution, &service, &causeRef, &action, &outcome, &resourceKind, &resourceID, &metadata, &tool, &execution, &call, &operation, &request, &approval, &runner, &correlation, &trace)
		if err != nil {
			f.t.Fatal("exact owned Project Audit row unavailable", err)
		}
		typed, err := ac.DecodeMetadata(ac.Action(action), []byte(metadata))
		if err != nil {
			f.t.Fatal("owned Project typed metadata invalid")
		}
		var projected map[string]any
		if json.Unmarshal(typed.JSON(), &projected) != nil {
			f.t.Fatal("owned Project safe metadata invalid")
		}
		actor := map[string]any{"kind": kind}
		switch kind {
		case "human":
			actor["id"] = user
		case "agent_run":
			actor["id"], actor["project_id"], actor["execution_id"] = agent, actorProject, actorExecution
		case "service":
			actor["service"], actor["cause_ref"], actor["project_id"] = service, causeRef, actorProject
		default:
			f.t.Fatal("owned Project actor kind invalid")
		}
		resource := map[string]any{"kind": resourceKind}
		if resourceID != "" {
			resource["id"] = resourceID
		}
		assoc := map[string]any{}
		for field, value := range map[string]string{"tool_id": tool, "execution_id": execution, "tool_call_id": call, "operation_id": operation, "request_id": request, "approval_id": approval, "runner_id": runner, "correlation_id": correlation, "http_trace_id": trace} {
			if value != "" {
				assoc[field] = value
			}
		}
		summary := "Audit event"
		switch action {
		case "secret.create":
			summary = "Secret created"
		case "secret.update":
			summary = "Secret updated"
		case "secret.delete":
			summary = "Secret deleted"
		case "secret.resolve":
			summary = "Secret use recorded"
		case "outbound.access.deny":
			summary = "Outbound access denied"
		}
		instant, err := foundation.NewInstant(at)
		if err != nil {
			f.t.Fatal("owned Project Audit instant invalid")
		}
		out = append(out, map[string]any{"audit_id": recordID, "created_at": instant.String(), "scope": "project", "project_id": target, "actor": actor, "action": action, "outcome": outcome, "resource": resource, "metadata": projected, "associations": assoc, "summary": summary})
	}
	return out
}

type projectOwnerAuditWebIPC struct {
	Sequence  int      `json:"sequence"`
	Action    string   `json:"action"`
	Project   string   `json:"project,omitempty"`
	AuditID   string   `json:"audit_id,omitempty"`
	IDs       []string `json:"ids,omitempty"`
	SessionID string   `json:"session_id,omitempty"`
}

func decodeProjectOwnerAuditWebIPC(raw []byte) (projectOwnerAuditWebIPC, error) {
	var request projectOwnerAuditWebIPC
	bad := func() (projectOwnerAuditWebIPC, error) {
		return request, errors.New("owned Project Audit IPC rejected")
	}
	if len(raw) > 32768 {
		return bad()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return bad()
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		token, err := d.Token()
		name, ok := token.(string)
		if err != nil || !ok || fields[name] != nil {
			return bad()
		}
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return bad()
		}
		fields[name] = value
	}
	if token, err := d.Token(); err != nil || token != json.Delim('}') {
		return bad()
	}
	var tail any
	if d.Decode(&tail) != io.EOF || json.Unmarshal(raw, &request) != nil || request.Sequence < 1 || request.Sequence > 128 {
		return bad()
	}
	required := []string{"sequence", "action"}
	optional := ""
	switch request.Action {
	case "counts", "release", "session-fail":
	case "snapshot":
		required = append(required, "project", "ids")
	case "arm-hold", "arm-cut", "arm-failure":
		required = append(required, "project")
		optional = "audit_id"
	case "logout":
		required = append(required, "session_id")
	case "rename-reuse":
		required = append(required, "project")
	default:
		return bad()
	}
	allowed := map[string]bool{}
	for _, name := range required {
		if fields[name] == nil {
			return bad()
		}
		allowed[name] = true
	}
	if optional != "" {
		allowed[optional] = true
	}
	for name := range fields {
		if !allowed[name] {
			return bad()
		}
	}
	if value, present := fields["audit_id"]; present {
		parsed, err := foundation.ParseID[struct{}](request.AuditID)
		if err != nil || parsed.String() != request.AuditID || len(value) == 0 {
			return bad()
		}
	}
	if fields["project"] != nil && request.Project == "" {
		return bad()
	}
	if request.Action == "snapshot" && (request.IDs == nil || len(request.IDs) > 200) {
		return bad()
	}
	if request.Action == "logout" {
		if parsed, err := foundation.ParseID[identity.Session](request.SessionID); err != nil || parsed.String() != request.SessionID {
			return bad()
		}
	}
	return request, nil
}

func (f *projectOwnerAuditWebFixture) ipc(ctx context.Context, r projectOwnerAuditWebIPC) map[string]any {
	result := map[string]any{}
	if r.Project != "" {
		if _, ok := f.ids[r.Project]; !ok {
			f.t.Fatal("private Audit IPC Project key rejected")
		}
	}
	switch r.Action {
	case "counts":
		f.mu.Lock()
		for _, key := range projectOwnerAuditWebCountKeys {
			result[key] = f.counts[key]
		}
		f.mu.Unlock()
	case "snapshot":
		result["records"] = f.snapshot(ctx, r.Project, r.IDs)
	case "arm-hold", "arm-cut", "arm-failure":
		path := projectOwnerWebPath + "/" + f.ids[r.Project] + "/audit"
		if r.AuditID != "" {
			path += "/" + r.AuditID
		}
		f.mu.Lock()
		if f.controlPath != "" || f.counts["held"] != f.counts["joined"] {
			f.mu.Unlock()
			f.t.Fatal("owned Audit controls may not overlap")
		}
		f.controlPath, f.controlKind = path, strings.TrimPrefix(r.Action, "arm-")
		if r.Action == "arm-hold" {
			f.release, f.releaseOnce = make(chan struct{}), sync.Once{}
		}
		f.mu.Unlock()
	case "release":
		f.releaseRead()
	case "session-fail":
		f.mu.Lock()
		if f.failSession {
			f.mu.Unlock()
			f.t.Fatal("owned Session failure already armed")
		}
		f.failSession = true
		f.mu.Unlock()
	case "logout":
		result = f.logout(ctx, r.SessionID)
	case "rename-reuse":
		if r.Project != "main" || f.renamed {
			f.t.Fatal("owned rename/reuse target rejected")
		}
		f.renamed = true
		current := f.setup.setupRequest(ctx, f.ownerClient, http.MethodGet, projectOwnerWebPath+"/"+f.ids["main"], nil, "", false, http.StatusOK)
		oldName := httpString(f.t, current, "name")
		f.setup.setupRequest(ctx, f.ownerClient, http.MethodPatch, projectOwnerWebPath+"/"+f.ids["main"], map[string]any{"expected_version": current["version"], "name": "audit-renamed-main"}, f.ownerCSRF, true, http.StatusOK)
		replacement := f.create(ctx, f.ownerActor, oldName)
		f.mu.Lock()
		f.ids["replacement"] = replacement.ID.String()
		f.mu.Unlock()
		result["renamed"], result["replacement"] = f.locator(ctx, "main"), f.locator(ctx, "replacement")
	default:
		f.t.Fatal("owned Project Audit action rejected")
	}
	return map[string]any{"sequence": r.Sequence, "result": result}
}

func (f *projectOwnerAuditWebFixture) logout(ctx context.Context, sessionID string) map[string]any {
	f.mu.Lock()
	session, ok := f.sessions[sessionID]
	f.mu.Unlock()
	if !ok || session.cookie == "" || session.csrf == "" || session.user != f.owner.UserID && session.user != f.admin.UserID && session.user != f.other.UserID {
		f.t.Fatal("exact owned browser Session unavailable")
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, f.origin+"/api/v1/sessions/logout", strings.NewReader(`{}`))
	if err != nil {
		f.t.Fatal("owned Logout request invalid")
	}
	r.Header.Set("Cookie", session.cookie)
	r.Header.Set("Origin", f.origin)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", session.csrf)
	r.Header.Set("Idempotency-Key", id[struct{}](f.t).String())
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	response, err := client.Do(r)
	if err != nil {
		f.t.Fatal("formal exact browser Logout failed")
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, 600001))
	closeErr := response.Body.Close()
	complete := response.StatusCode == http.StatusNoContent && readErr == nil && closeErr == nil && len(raw) == 0
	clear(raw)
	if !complete {
		f.t.Fatal("formal exact browser Logout terminal mismatch")
	}
	var revoked bool
	var audits int
	err = f.store.QueryRow(ctx, `SELECT revoked_at IS NOT NULL AND revoked_reason='logout',(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='account.logout' AND metadata->>'session_id'=$1::text AND outcome='success') FROM agenteam_account.sessions WHERE id=$1::uuid AND user_id=$2`, sessionID, session.user).Scan(&revoked, &audits)
	if err != nil || !revoked || audits != 1 {
		f.t.Fatal("formal exact Session Logout facts missing")
	}
	f.mu.Lock()
	delete(f.sessions, sessionID)
	f.mu.Unlock()
	return map[string]any{"revoked": true, "logout_audit": true}
}

// Start is immediately followed by ownership registration before diagnostics,
// IPC or any assertion. Both success and failure actually Wait the direct child.
func (f *projectOwnerAuditWebFixture) browser(ctx context.Context) projectOwnerAuditWebResult {
	root, err := filepath.Abs("../account-captcha-web")
	if err != nil {
		f.t.Fatal("owned browser root unavailable")
	}
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "project-owner-audit.config.js"))
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
	cmd.Env = append(cmd.Env, "TMPDIR="+f.directory, "AGENTEAM_AUTH_WEB_ORIGIN="+f.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+f.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_PROJECT_AUDIT_WEB_CASE="+f.mode, "AGENTEAM_PROJECT_AUDIT_WEB_DIST="+f.webRoot, "AGENTEAM_PROJECT_AUDIT_WEB_EVIDENCE="+f.evidence, "PLAYWRIGHT_NO_COPY_PROMPT=1")
	images := os.Getenv("AGENTEAM_AUTH_WEB_IMAGES")
	if f.mode == "navigation" && !filepath.IsAbs(images) {
		f.t.Fatal("navigation requires an explicit absolute image directory")
	}
	if images != "" {
		if !filepath.IsAbs(images) {
			f.t.Fatal("owned image directory must be absolute")
		}
		cmd.Env = append(cmd.Env, "AGENTEAM_AUTH_WEB_IMAGES="+images)
	}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	f.mu.Lock()
	f.browserActive = true
	f.mu.Unlock()
	if err := cmd.Start(); err != nil {
		f.t.Fatal("locked Project Audit browser runner could not start")
	}
	done := make(chan error, 1)
	joined := false
	defer func() {
		f.releaseRead()
		if !joined {
			_ = cmd.Cancel()
			<-done
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cmd.Env = nil
		f.t.Log("Project Audit Node direct child actually waited; adopted descendants remain owned by outer subreaper")
	}()
	go func() { done <- cmd.Wait() }()
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
			raw, err := projectOwnerAuditWebReadPrivate(filepath.Join(f.directory, "project-audit-ipc.json"))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			request, decodeErr := decodeProjectOwnerAuditWebIPC(raw)
			clear(raw)
			if err != nil || decodeErr != nil {
				f.t.Fatal("private Project Audit request rejected")
			}
			if request.Sequence <= sequence {
				continue
			}
			if request.Sequence != sequence+1 {
				f.t.Fatal("private Project Audit sequence skipped")
			}
			reply := f.ipc(ctx, request)
			sequence = request.Sequence
			f.private("project-audit-ack-"+strconv.Itoa(sequence)+".json", reply)
		}
	}
	safe := output.String()
	for _, secret := range append(append([]string(nil), f.privateValues...), f.admin.Password, f.owner.Password, f.other.Password, f.adminCSRF, f.ownerCSRF) {
		if secret != "" {
			safe = strings.ReplaceAll(safe, secret, "[redacted]")
		}
	}
	f.mu.Lock()
	for _, session := range f.sessions {
		for _, secret := range []string{session.cookie, session.csrf} {
			if secret != "" {
				safe = strings.ReplaceAll(safe, secret, "[redacted]")
			}
		}
	}
	f.mu.Unlock()
	f.t.Log(safe)
	if runErr != nil {
		f.t.Fatalf("actual Project Audit browser failed: %v", runErr)
	}
	raw, err := projectOwnerAuditWebReadPrivate(filepath.Join(f.directory, "project-audit-result.json"))
	if err != nil || len(raw) > 32768 {
		f.t.Fatal("safe Project Audit browser result unavailable")
	}
	defer clear(raw)
	result, err := decodeProjectOwnerAuditWebResult(raw, f.mode)
	if err != nil {
		f.t.Fatal("safe Project Audit browser result invalid", err)
	}
	f.safeEvidence("browser-result.json", result)
	return result
}

// Safe summaries survive the private-runtime deletion; they contain only
// owned IDs, closed state labels and assertion/count facts, never IPC secrets.
func (f *projectOwnerAuditWebFixture) safeEvidence(name string, value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		f.t.Fatal("safe Project Audit evidence encoding failed")
	}
	if err := os.WriteFile(filepath.Join(f.evidence, name), raw, 0600); err != nil {
		f.t.Fatal("safe Project Audit evidence write failed")
	}
}
func (f *projectOwnerAuditWebFixture) auxiliaryFacts(ctx context.Context) {
	sources := map[string]string{"archiving": "formal BeginArchive accepted", "archived": "formal BeginArchive plus exact owned locked completion facts; no participant execution", "deleting": "formal BeginDeleteProject accepted", "pending": "formal CreateProject pending with the test-only persisted Skills adapter"}
	facts := map[string]any{}
	for _, key := range []string{"archiving", "archived", "deleting", "pending"} {
		var lifecycle, creation, operation string
		var initialized bool
		err := f.store.QueryRow(ctx, `SELECT lifecycle,initialized_at IS NOT NULL,creation_id::text,coalesce(current_lifecycle_operation_id::text,'') FROM agenteam_project.projects WHERE id=$1`, f.ids[key]).Scan(&lifecycle, &initialized, &creation, &operation)
		if err != nil {
			f.t.Fatal("owned auxiliary lifecycle facts unavailable", err)
		}
		facts[key] = map[string]any{"project_id": f.ids[key], "lifecycle": lifecycle, "initialized": initialized, "creation_id": creation, "operation_id": operation, "source": sources[key], "fact_only": true}
	}
	f.safeEvidence("auxiliary-facts.json", map[string]any{"input_hash": f.inputHash, "before_browser": true, "facts": facts, "production_skills_or_lifecycle_runtime_acceptance": false})
}

func projectOwnerAuditWebReadPrivate(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, 32769))
	closeErr := file.Close()
	if readErr != nil {
		clear(raw)
		return nil, readErr
	}
	if closeErr != nil {
		clear(raw)
		return nil, closeErr
	}
	if len(raw) > 32768 {
		clear(raw)
		return nil, errors.New("owned private JSON bound exceeded")
	}
	return raw, nil
}
