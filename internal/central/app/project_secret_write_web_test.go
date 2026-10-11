//go:build integration

package app

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
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
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
)

// This fixture reuses the default App, ordinary Account Owner and original
// seven-resource lifecycle. Only its lost PATCH receipt is physically cut.
// Plaintext is generated in Go/browser memory and never enters fixture material.
func TestProjectSecretWriteWeb(t *testing.T) {
	for _, mode := range []string{"secret-write-normal", "secret-write-lost"} {
		t.Run(mode, func(t *testing.T) { projectSecretWriteWeb(t, mode) })
	}
}

func projectSecretWriteWeb(t *testing.T, mode string) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(func() {
		cancel()
		if time.Since(started) > 120*time.Second {
			t.Error("Project Secret Write browser exceeded its original 120s including cleanup")
		}
	})
	var forbidden [][]byte
	// Registered before the proxy/root cleanup: erase only after their original
	// handlers and observations have actually retired, including failure paths.
	t.Cleanup(func() {
		for _, value := range forbidden {
			clear(value)
		}
	})
	check := func(stage string, err error) {
		t.Helper()
		if err != nil {
			var fault *f.Fault
			if errors.As(err, &fault) {
				t.Fatalf("Project Secret Write browser %s: %s/%s", stage, fault.Code.Safe(), fault.CommitState.Safe())
			}
			var pathErr *os.PathError
			var errno syscall.Errno
			if errors.As(err, &pathErr) && errors.As(pathErr.Err, &errno) {
				t.Fatalf("Project Secret Write browser %s failed: op=%q errno=%d", stage, pathErr.Op, errno)
			}
			t.Fatalf("Project Secret Write browser %s failed", stage)
		}
	}
	dist := os.Getenv("AGENTEAM_PROJECT_SECRET_WRITE_WEB_DIST")
	runtime := os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	evidence := os.Getenv("AGENTEAM_PROJECT_SECRET_WRITE_WEB_EVIDENCE")
	inputHash := os.Getenv("AGENTEAM_PROJECT_SECRET_WRITE_WEB_INPUT_HASH")
	decoded, hashErr := hex.DecodeString(inputHash)
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || hashErr != nil || len(decoded) != sha256.Size {
		t.Fatal("exact owned Project Secret Write assets/runtime/evidence/input required")
	}
	info, err := os.Lstat(filepath.Join(dist, "index.html"))
	check("dist", err)
	if !info.Mode().IsRegular() {
		t.Fatal("regular private dist required")
	}
	evidence = filepath.Join(evidence, t.Name())
	check("evidence parent", os.MkdirAll(filepath.Dir(evidence), 0700))
	check("fresh evidence", os.Mkdir(evidence, 0700))
	t.Cleanup(func() { projectSecretsScanEvidence(t, evidence, forbidden) })
	private, err := os.MkdirTemp(runtime, "pscw-")
	check("private browser directory", err)
	t.Cleanup(func() {
		check("private removal", os.RemoveAll(private))
		if _, err := os.Lstat(private); !errors.Is(err, os.ErrNotExist) {
			t.Error("private Project Secret Write directory remains")
		}
	})
	db := pgfixture.NewDatabase(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	check("private listener", err)
	t.Cleanup(func() { _ = listener.Close() })
	origin := "http://" + listener.Addr().String()
	cfg := guardConfiguration(t, append(guardEnvironment(t, db), "AGENTEAM_CENTRAL_PUBLIC_ORIGIN="+origin, "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s"))
	observations := &projectSecretWriteObservations{knowledgeTextObservations: &knowledgeTextObservations{evidence: evidence, inputHash: inputHash, origin: origin, tails: map[string]*knowledgeTextTail{}}, mode: mode, intents: map[string]projectSecretWriteIntent{}, forbidden: &forbidden}
	t.Cleanup(func() {
		observations.mu.Lock()
		defer observations.mu.Unlock()
		for _, in := range observations.intents {
			clear(in.value)
		}
	})
	var core *account.Service
	var owned *resources
	deps := dependencies{observeAccount: func(v *account.Service) { core = v }, bind: func(c context.Context, cfg config.Config, store database, owner *resources, d *dependencies) error {
		owned = owner
		if err := bindAccounts(c, cfg, store, owner, d); err != nil {
			return err
		}
		a, ok := owner.accounts().(*accountAssembly)
		if !ok {
			return errors.New("original Account assembly absent")
		}
		a.mu.Lock()
		a.handler = observations.observeHandler(a.handler)
		a.mu.Unlock()
		return nil
	}}
	output := newEventLog()
	logger, err := logging.New(logging.Central, slog.LevelInfo, output)
	check("logger", err)
	rootCtx, cancelRoot := context.WithCancel(ctx)
	signals := make(chan os.Signal, 2)
	rootDone := make(chan struct{})
	var rootErr error
	go func() { rootErr = run(rootCtx, cfg, logger, signals, deps); close(rootDone) }()
	t.Cleanup(func() {
		cancelRoot()
		select {
		case <-rootDone:
		case <-time.After(7 * time.Second):
			t.Error("Project Secret Write root actual join exceeded cleanup observation")
			<-rootDone
		}
	})
	var address string
	for address == "" {
		select {
		case event := <-output.events:
			if event["event"] == "listening" {
				address, _ = event["listen_address"].(string)
			}
		case <-rootDone:
			check("root startup", rootErr)
			t.Fatal("root returned before listening")
		case <-ctx.Done():
			t.Fatal("root startup exhausted original budget")
		}
	}
	if core == nil || owned == nil {
		t.Fatal("original root owners missing")
	}
	assembly, ok := owned.accounts().(*accountAssembly)
	if !ok {
		t.Fatal("original Account assembly missing")
	}
	assembly.mu.Lock()
	projects, projectOK := assembly.projects.(*projectCommandWork)
	skills, skillOK := assembly.skills.(*skillWork)
	variables, secretOK := assembly.secretVariables.(*projectSecretVariablesAssembly)
	assembly.mu.Unlock()
	objects, objectOK := owned.objects().(*objectAssembly)
	if !projectOK || !skillOK || !secretOK || !objectOK {
		t.Fatal("original concrete domains missing")
	}
	backend, err := url.Parse("http://" + address)
	check("backend URL", err)
	transport := &http.Transport{Proxy: nil}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport = transport
	proxy.ErrorLog = slog.NewLogLogger(slog.NewTextHandler(io.Discard, nil), slog.LevelError)
	proxy.ErrorHandler = observations.proxyError
	proxy.ModifyResponse = observations.capture
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second}
	// Asset paths remain under the actual frozen assets directory.
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			proxy.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method", 405)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			skillWebServeAsset(w, r, dist)
			return
		}
		http.ServeFile(w, r, filepath.Join(dist, "index.html"))
	})
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	var proxyStop sync.Once
	stopProxy := func() {
		proxyStop.Do(func() {
			if err := server.Shutdown(ctx); err != nil {
				t.Error("private proxy Shutdown did not complete in original budget")
				_ = server.Close()
			}
			err := <-served
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Error("private proxy original Serve failed")
			}
			transport.CloseIdleConnections()
		})
	}
	t.Cleanup(stopProxy)
	setup := &knowledgeWebSetup{t: t, ctx: ctx, origin: origin, cfg: cfg, check: check}
	adminRecord := setup.record("bootstrap", "", "")
	admin := setup.client()
	anon := setup.call(admin, "GET", "/api/v1/auth/bootstrap", nil, "", false, 200)
	setup.call(admin, "POST", "/api/v1/sessions/login", map[string]string{"email": adminRecord.Email, "password": adminRecord.Password}, knowledgeWebString(t, anon, "csrf_token"), true, 200)
	adminSession := setup.call(admin, "GET", "/api/v1/session", nil, "", false, 200)
	email, username := "secrets-owner@example.com", "secrets-owner"
	invitation := setup.call(admin, "POST", "/api/v1/system/invitations", map[string]string{"email": email}, knowledgeWebString(t, adminSession, "csrf_token"), true, 201)
	inviteRecord := setup.record("invitation", email, knowledgeWebString(t, invitation, "id"))
	inviteURL, err := url.Parse(inviteRecord.URL)
	check("invitation URL", err)
	if inviteURL.Scheme+"://"+inviteURL.Host != origin || inviteURL.Fragment == "" {
		t.Fatal("invitation origin binding missing")
	}
	ownerClient := setup.client()
	anon = setup.call(ownerClient, "GET", "/api/v1/auth/bootstrap", nil, "", false, 200)
	anonymousCSRF := knowledgeWebString(t, anon, "csrf_token")
	setup.call(ownerClient, "POST", "/api/v1/invitations/inspect", map[string]string{"token": inviteURL.Fragment}, anonymousCSRF, false, 200)
	password := "Secrets private " + guardID[struct{}](t).String() + "!"
	setup.call(ownerClient, "POST", "/api/v1/invitations/redeem", map[string]string{"token": inviteURL.Fragment, "username": username, "display_name": "Secrets Owner", "password": password, "confirmation": password}, anonymousCSRF, true, 201)
	setup.call(ownerClient, "POST", "/api/v1/sessions/login", map[string]string{"email": email, "password": password}, anonymousCSRF, true, 200)
	session := setup.call(ownerClient, "GET", "/api/v1/session", nil, "", false, 200)
	user, ok := session["user"].(map[string]any)
	if !ok || user["role"] != "user" || user["username"] != username {
		t.Fatal("formal ordinary Owner missing")
	}
	originURL, err := url.Parse(origin)
	check("owned origin", err)
	var cookie string
	for _, value := range ownerClient.Jar.Cookies(originURL) {
		if value.Name == "agenteam_local_session" {
			cookie = value.Value
		}
	}
	material, err := sc.NewSecretMaterial([]byte(cookie))
	check("formal Owner cookie", err)
	defer material.Destroy()
	actor, err := core.Authenticate(ctx, material)
	check("same current Account authority", err)
	if actor.Details().UserID != knowledgeWebString(t, user, "id") {
		t.Fatal("Owner Session identity mismatch")
	}
	meta := func() f.CommandMeta {
		return f.CommandMeta{RequestID: guardID[f.Request](t), IdempotencyKey: f.IdempotencyKey(guardID[struct{}](t).String())}
	}
	blockedID := guardID[id.Project](t)
	blocked, err := projects.service.CreateProject(ctx, actor, meta(), pc.CreateProjectRequest{ProjectID: blockedID, Name: "project-secret-write", Description: "Default initializer stays unbound"})
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.DependencyUnbound || fault.CommitState != f.NotCommitted || blocked.State != "" || blocked.Project != nil || blocked.Operation != nil {
		t.Fatal("default Project creation did not remain unbound/not_committed")
	}
	conn := db.Connect(t)
	var facts int
	check("default unbound zero facts", conn.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_project.projects WHERE id=$1 OR (owner_user_id=$2 AND normalized_name='project-secret-write'))+
 (SELECT count(*) FROM agenteam_project.creations WHERE project_id=$1)+
 (SELECT count(*) FROM agenteam_project.commands WHERE project_id=$1)+
 (SELECT count(*) FROM agenteam_skill.initializations WHERE project_id=$1)+
 (SELECT count(*) FROM agenteam_skill.skills WHERE project_id=$1)+
 (SELECT count(*) FROM agenteam_skill.revisions WHERE project_id=$1)+
 (SELECT count(*) FROM agenteam_skill.work WHERE project_id=$1)+
 (SELECT count(*) FROM agenteam_skill.object_attempts WHERE project_id=$1)+
 (SELECT count(*) FROM agenteam_object.uploads WHERE project_id=$1)+
 (SELECT count(*) FROM agenteam_object.objects WHERE project_id=$1)+
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1)+
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1)`, blockedID.String(), actor.Details().UserID).Scan(&facts))
	if facts != 0 {
		t.Fatal("default unbound produced facts")
	}
	fixture := rootCompositionProjectFixture(t, cfg, owned, objects, skills.service)
	fixtureOwner := &projectCommandWork{service: fixture}
	t.Cleanup(func() {
		if !fixtureOwner.Joined() {
			check("test-only Project cleanup", fixtureOwner.Drain(ctx))
		}
	})
	projectID := guardID[id.Project](t)
	created, createErr := fixture.CreateProject(ctx, actor, meta(), pc.CreateProjectRequest{ProjectID: projectID, Name: "project-secret-write", Description: "Owner Secret writes and current safe metadata"})
	drainErr := fixtureOwner.Drain(ctx)
	check("test-only real Project", createErr)
	check("test-only actual Drain", drainErr)
	if !fixtureOwner.Joined() || created.Validate() != nil || created.State != pc.CreationReady || created.Project == nil || created.Project.ID != projectID {
		t.Fatal("isolated real Project did not confirm and join")
	}

	observations.project, observations.host = projectID.String(), listener.Addr().String()
	observations.actor, observations.conn = actor.Details().UserID, conn
	var seeded map[string]any
	if mode == "secret-write-lost" {
		entropy := make([]byte, 32)
		_, err := rand.Read(entropy)
		check("seed entropy", err)
		value := []byte(base64.RawURLEncoding.EncodeToString(entropy))
		clear(entropy)
		projectSecretWriteRemember(&forbidden, value)
		material, err := sc.NewSecretMaterial(value)
		clear(value)
		check("seed material", err)
		target := guardID[id.ProjectVariable](t)
		request, err := vc.NewSecretVariableCreate(vc.SecretVariableCreateFields{ID: target, Name: "BROWSER_SECRET", Description: "现有安全元数据", Value: material})
		material.Destroy()
		check("seed intent", err)
		receipt, seedErr := variables.service.CreateSecretVariable(ctx, actor, meta(), projectID, request)
		request.Destroy()
		check("formal seed", seedErr)
		if receipt.Validate() != nil || receipt.Fields().Command != vc.SecretCreateCommand || !receipt.Fields().Changed {
			t.Fatal("formal seed receipt invalid")
		}
		raw, err := json.Marshal(receipt.Fields().Variable)
		check("safe seed metadata", err)
		check("safe seed projection", json.Unmarshal(raw, &seeded))
		clear(raw)
		observations.target, observations.current = target.String(), seeded
		observations.credential, err = observations.credentialState(ctx)
		check("seed D04 state", err)
	}
	snapshot := func(ctx context.Context) (string, error) {
		var value string
		err := conn.QueryRow(ctx, `SELECT jsonb_build_object(
 'project',(SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1),
 'variables',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM agenteam_projectvariable.variables v WHERE project_id=$1),
 'commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_projectvariable.secret_commands c WHERE project_id=$1),
 'history',(SELECT jsonb_agg(to_jsonb(h) ORDER BY id) FROM agenteam_projectvariable.secret_history h WHERE project_id=$1),
 'generation',(SELECT to_jsonb(g) FROM agenteam_projectvariable.secret_project_generations g WHERE project_id=$1),
 'references',(SELECT jsonb_agg(to_jsonb(r) ORDER BY variable_id,owner_kind,owner_id) FROM agenteam_projectvariable.secret_references r WHERE project_id=$1),
 'credentials',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM agenteam_secret.secrets s WHERE project_id=$1),
 'payloads',(SELECT jsonb_agg(to_jsonb(p) ORDER BY payload_id) FROM agenteam_secret.secret_payloads p WHERE project_id=$1),
 'protected_receipts',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM agenteam_secret.project_variable_receipts r WHERE project_id=$1),
 'leases',(SELECT jsonb_agg(to_jsonb(l) ORDER BY id) FROM agenteam_secret.secret_leases l WHERE project_id=$1),
 'credential_references',(SELECT jsonb_agg(to_jsonb(r) ORDER BY credential_id,consumer,owner_id) FROM agenteam_secret.secret_references r WHERE project_id=$1),
 'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a WHERE project_id=$1),
 'outbox',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_outbox.events e WHERE project_id=$1))::text`, projectID.String()).Scan(&value)
		return value, err // Private equality only; no snapshot or digest leaves Go.
	}

	persisted := func() string { value, err := snapshot(ctx); check("persisted facts", err); return value }

	observations.snapshot = snapshot
	observations.stable = persisted()
	observations.active = true
	payload := map[string]any{"mode": mode, "cookie": cookie, "project_id": projectID.String(), "route": "/" + username + "/project-secret-write/settings/secrets", "seeded": seeded, "input_hash": inputHash}
	raw, err := json.Marshal(payload)
	check("safe browser metadata", err)
	if projectSecretsLeak(raw, forbidden) {
		t.Fatal("private metadata contains Secret material")
	}
	check("private safe metadata file", os.WriteFile(filepath.Join(private, "project-secret-write-material.json"), raw, 0600))
	clear(raw)
	result := projectSecretWriteBrowser(t, ctx, private, origin, dist, evidence, inputHash, mode, observations)
	observations.mu.Lock()
	observeErr, observed, tails := observations.err, observations.count, len(observations.tails)
	writes, lookups, cuts, gets, lists := observations.writes, observations.lookups, observations.cuts, observations.gets, observations.lists
	observations.mu.Unlock()
	wantWrites, wantLookup, wantVersion := 3, 0, "3"
	if mode == "secret-write-lost" {
		wantWrites, wantLookup, wantVersion = 1, 1, "2"
	}
	if observeErr != nil || writes != wantWrites || lookups != wantLookup || cuts != wantLookup || observed != tails || gets < wantWrites || lists < wantWrites || observations.current["version"] != wantVersion ||
		result["completed"] != true || result["case"] != mode || result["all_observed"] != true || result["original_requests"] != float64(observed) || result["lookups"] != float64(wantLookup) || result["declared_receipt_cut"] != float64(wantLookup) {
		t.Fatal("Secret writes and original consumer proofs incomplete")
	}
	if persisted() != observations.stable {
		t.Fatal("current reads changed Secret facts")
	}
	projectSecretsScanEvidence(t, evidence, forbidden)
	setup.close()
	stopProxy()
	claim, err := os.OpenFile(filepath.Join(cfg.Objects().SpoolDirectory()+".processes", objects.process.String()+".claim"), os.O_RDWR, 0)
	check("guard claim", err)
	defer claim.Close()
	if err := syscall.Flock(int(claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); !errors.Is(err, syscall.EWOULDBLOCK) {
		if err == nil {
			_ = syscall.Flock(int(claim.Fd()), syscall.LOCK_UN)
		}
		t.Fatal("live root process guard not held")
	}
	signals <- syscall.SIGTERM
	select {
	case <-rootDone:
	case <-ctx.Done():
		t.Fatal("root did not join within original browser budget")
	}
	check("root actual result", rootErr)
	if !fixtureOwner.Joined() || !projects.Joined() || !skills.service.Joined() || !variables.Joined() || !assembly.Joined() || !owned.producersJoined() {
		t.Fatal("original domain owners did not join")
	}
	check("released guard", syscall.Flock(int(claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	check("guard unlock", syscall.Flock(int(claim.Fd()), syscall.LOCK_UN))
	var stopped string
	check("guard stopped fact", conn.QueryRow(ctx, `SELECT state FROM agenteam_object.process_claims WHERE process_id=$1`, objects.process.String()).Scan(&stopped))
	if stopped != "stopped" {
		t.Fatal("original process claim not stopped")
	}
	var noBorrowers bool
	check("retired database borrowers", conn.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam')`).Scan(&noBorrowers))
	if !noBorrowers {
		t.Fatal("original root retained database borrowers")
	}
	if projectSecretsLeak([]byte(output.String()), forbidden) {
		t.Fatal("Secret material escaped into the actual root log")
	}
	for _, secret := range []string{adminRecord.Password, inviteURL.Fragment, password, cookie} {
		if secret != "" && strings.Contains(output.String(), secret) {
			t.Error("private Account material escaped into root log")
		}
	}
	t.Log("Project Secret Write browser chain: original Node Wait, proxy/root join, domain retirement and guard facts complete; production initializer remains unbound")
}

func projectSecretWriteBrowser(t *testing.T, ctx context.Context, private, origin, dist, evidence, inputHash, mode string, observations *projectSecretWriteObservations) map[string]any {
	t.Helper()
	harness, err := filepath.Abs("../../../tests/account-captcha-web")
	if err != nil {
		t.Fatal("browser harness path unavailable")
	}
	cmd := exec.CommandContext(ctx, "node", filepath.Join(harness, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(harness, "project-secret-write.config.js"))
	cmd.Dir = harness
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "TMPDIR" && key != "DEBUG" && key != "PWDEBUG" && !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "TMPDIR="+private, "AGENTEAM_AUTH_WEB_ORIGIN="+origin, "AGENTEAM_AUTH_WEB_PRIVATE="+private, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_PROJECT_SECRET_WRITE_WEB_DIST="+dist, "AGENTEAM_PROJECT_SECRET_WRITE_WEB_EVIDENCE="+evidence, "AGENTEAM_PROJECT_SECRET_WRITE_WEB_INPUT_HASH="+inputHash, "AGENTEAM_PROJECT_SECRET_WRITE_WEB_CASE="+mode, "PLAYWRIGHT_NO_COPY_PROMPT=1")
	var output bytes.Buffer
	defer func() { clear(output.Bytes()) }()
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal("locked ProjectSecretWrite Node could not start")
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
	}()
	err = <-done
	joined = true
	t.Logf("ProjectSecretWrite Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	// Never print the browser's credential-bearing call log. The spec writes
	// fixed-stage safe failure diagnostics; the original child status is final.
	if projectSecretsLeak(output.Bytes(), observations.forbiddenSnapshot()) {
		t.Fatal("Secret material escaped into original Node output")
	}
	if err != nil || !regexp.MustCompile(`(?m)\b1 passed\b`).Match(output.Bytes()) {
		t.Fatal("ProjectSecretWrite locked browser did not actually pass its one case")
	}
	raw, err := os.ReadFile(filepath.Join(private, "project-secret-write-result.json"))
	if err != nil {
		t.Fatal("ProjectSecretWrite browser result missing")
	}
	defer clear(raw)
	if len(raw) > 16<<10 || projectSecretsLeak(raw, observations.forbiddenSnapshot()) {
		t.Fatal("ProjectSecretWrite browser result too large")
	}
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil || result["input_hash"] != inputHash {
		t.Fatal("ProjectSecretWrite browser result not bound to frozen inputs")
	}
	return result
}

type projectSecretWriteIntent struct {
	kind, operation, key, target, expected, name, description string
	namePresent, descriptionPresent, valuePresent             bool
	value                                                     []byte
}
type projectSecretWriteObservations struct {
	*knowledgeTextObservations
	mode, actor, target, stable, credential string
	conn                                    *pgx.Conn
	current                                 map[string]any
	intents                                 map[string]projectSecretWriteIntent
	forbidden                               *[][]byte
	snapshot                                func(context.Context) (string, error)
	writes, lookups, cuts, gets, lists      int
	original                                *projectSecretWriteIntent
}
type projectSecretWriteDrop struct {
	header http.Header
	length int
	record map[string]any
}

func (*projectSecretWriteDrop) Error() string { return "declared Secret receipt cut" }

// Only these in-memory canaries are retained for leak checks. There is no
// plaintext/body digest, request dump or canary material file.
func projectSecretWriteRemember(forbidden *[][]byte, value []byte) {
	escaped, _ := json.Marshal(string(value))
	*forbidden = append(*forbidden, append([]byte(nil), value...),
		[]byte(base64.StdEncoding.EncodeToString(value)), []byte(base64.RawStdEncoding.EncodeToString(value)),
		[]byte(hex.EncodeToString(value)), append([]byte(nil), escaped[1:len(escaped)-1]...))
	clear(escaped)
}
func (o *projectSecretWriteObservations) observeHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.mu.Lock()
		active := o.active
		o.mu.Unlock()
		if !active {
			next.ServeHTTP(w, r)
			return
		}
		rid := httpapi.RequestID(r.Context())
		if rid.Validate() != nil || r.Host != o.host {
			_ = o.fail()
			http.Error(w, "request identity", 502)
			return
		}
		tail := &knowledgeTextTail{done: make(chan struct{}), method: r.Method, uri: r.URL.RequestURI()}
		var raw []byte
		var in *projectSecretWriteIntent
		if r.Method != http.MethodGet {
			if r.Header.Get("Origin") != o.origin || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("X-CSRF-Token") == "" || r.Header.Get("Cookie") == "" {
				_ = o.fail()
				http.Error(w, "closed Secret write", 502)
				return
			}
			var err error
			raw, err = io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
			closed := r.Body.Close()
			defer clear(raw)
			key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
			if err != nil || closed != nil || len(raw) > 1<<20 || r.ContentLength != int64(len(raw)) || key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 {
				_ = o.fail()
				http.Error(w, "original Secret body", 502)
				return
			}
			o.mu.Lock()
			value, decodeErr := o.intent(r, raw, string(key))
			o.mu.Unlock()
			if decodeErr != nil {
				_ = o.fail()
				http.Error(w, "closed Secret intent", 502)
				return
			}
			in = &value
			r.Body = io.NopCloser(bytes.NewReader(raw))
		} else if r.ContentLength != 0 || len(r.TransferEncoding) != 0 || r.Header.Get("Idempotency-Key") != "" {
			_ = o.fail()
			http.Error(w, "read body", 502)
			return
		}
		o.mu.Lock()
		valid := o.tails[rid.String()] == nil && len(o.tails) < 96
		if valid {
			o.tails[rid.String()] = tail
			if in != nil {
				o.intents[rid.String()] = *in
			}
		}
		o.mu.Unlock()
		if !valid {
			_ = o.fail()
			http.Error(w, "request identity", 502)
			return
		}
		defer close(tail.done)
		next.ServeHTTP(w, r)
		tail.returned = true
	})
}

// Parsing checks the actual original wire request. Value bytes are consumed
// only for validation/leak canaries; the intent retained below is safe identity.
func (o *projectSecretWriteObservations) intent(r *http.Request, raw []byte, key string) (projectSecretWriteIntent, error) {
	bad := func() (projectSecretWriteIntent, error) {
		return projectSecretWriteIntent{}, errors.New("Secret intent mismatch")
	}
	prefix := "/api/v1/projects/" + o.project + "/secret-variables"
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return bad()
	}
	in := projectSecretWriteIntent{key: key}
	if r.Method == http.MethodPost && r.URL.Path == prefix+"/commands/lookup" {
		var q struct {
			Command  string `json:"command"`
			Target   string `json:"target_id"`
			Expected string `json:"expected_version"`
		}
		if o.mode != "secret-write-lost" || o.original == nil || o.cuts != 1 || o.lookups != 0 || skillWebDecode(raw, &q) != nil || q.Command != "project.secret_variable.update" || q.Target != o.original.target || q.Expected != o.original.expected || key != o.original.key {
			return bad()
		}
		in = *o.original
		in.kind = "lookup"
		o.lookups++
		return in, nil
	}
	for _, previous := range o.intents {
		if previous.key == key {
			return bad()
		}
	}
	var body struct {
		Expected string                     `json:"expected_version,omitempty"`
		Request  map[string]json.RawMessage `json:"request"`
	}
	if skillWebDecode(raw, &body) != nil {
		return bad()
	}
	defer func() {
		for _, v := range body.Request {
			clear(v)
		}
	}()
	for name := range body.Request {
		if name != "variable_id" && name != "name" && name != "description" && name != "value" {
			return bad()
		}
	}
	if v, ok := body.Request["name"]; ok {
		in.namePresent = true
		if json.Unmarshal(v, &in.name) != nil {
			return bad()
		}
	}
	if v, ok := body.Request["description"]; ok {
		in.descriptionPresent = true
		if json.Unmarshal(v, &in.description) != nil {
			return bad()
		}
	}
	if v, ok := body.Request["value"]; ok {
		var value string
		if json.Unmarshal(v, &value) != nil {
			return bad()
		}
		material := []byte(value)
		value = ""
		defer clear(material)
		if vc.ValidateSecretValue(material) != nil {
			return bad()
		}
		projectSecretWriteRemember(o.forbidden, material)
		in.valuePresent = true
		in.value = append([]byte(nil), material...)
	}
	if o.mode == "secret-write-normal" && o.writes == 0 {
		if r.Method != http.MethodPost || r.URL.Path != prefix || body.Expected != "" || len(body.Request) != 4 || !in.namePresent || !in.descriptionPresent || !in.valuePresent || in.name != "BROWSER_SECRET" || in.description != "创建说明" {
			return bad()
		}
		if json.Unmarshal(body.Request["variable_id"], &in.target) != nil {
			return bad()
		}
		target, err := f.ParseID[id.ProjectVariable](in.target)
		if err != nil || target.String() != in.target {
			return bad()
		}
		in.kind, in.operation, o.target = "create", "create", in.target
	} else {
		in.kind, in.target, in.expected = "update", o.target, body.Expected
		if r.Method != http.MethodPatch || o.target == "" || r.URL.Path != prefix+"/"+o.target || o.current == nil || body.Expected != o.current["version"] {
			return bad()
		}
		if _, hasID := body.Request["variable_id"]; hasID {
			return bad()
		}
		if o.mode == "secret-write-normal" && o.writes == 1 {
			if len(body.Request) != 2 || !in.namePresent || !in.descriptionPresent || in.valuePresent || in.name != "RENAMED_BROWSER_SECRET" || in.description != "更新后的安全说明" {
				return bad()
			}
			in.operation = "metadata"
		} else {
			if (o.mode == "secret-write-normal" && o.writes != 2) || (o.mode == "secret-write-lost" && o.writes != 0) || len(body.Request) != 1 || !in.valuePresent || in.namePresent || in.descriptionPresent {
				return bad()
			}
			in.operation = "replace"
		}
	}
	o.writes++
	if o.mode == "secret-write-lost" {
		original := in
		o.original = &original
	}
	return in, nil
}

func (o *projectSecretWriteObservations) credentialState(ctx context.Context) (string, error) {
	var value string
	err := o.conn.QueryRow(ctx, `SELECT jsonb_build_object('credential',to_jsonb(s),'payload',to_jsonb(p))::text
 FROM agenteam_projectvariable.variables v JOIN agenteam_secret.secrets s ON s.id=v.credential_id
 JOIN agenteam_secret.secret_payloads p ON p.payload_id=s.current_payload_id
 WHERE v.project_id=$1 AND v.id=$2 AND v.type='secret' AND v.value IS NULL AND v.deleted_at IS NULL
 AND s.project_id=v.project_id AND s.purpose='project_variable' AND s.version=v.credential_version
 AND p.project_id=v.project_id AND p.owner_kind=1 AND p.owner_id=s.id`, o.project, o.target).Scan(&value)
	return value, err
}

// Like tests/account/link_material_test.go, this test-only decoder uses the
// isolated DB and public guardEnvironment key. No production reveal port exists.
func (o *projectSecretWriteObservations) materialMatches(ctx context.Context, expected []byte) bool {
	var owner, payload string
	var dn, ct, wn, wrapped []byte
	var version uint64
	err := o.conn.QueryRow(ctx, `SELECT p.owner_id::text,p.payload_id::text,p.data_nonce,p.ciphertext,p.wrap_nonce,p.wrapped_dek,p.master_version
 FROM agenteam_projectvariable.variables v JOIN agenteam_secret.secrets s ON s.id=v.credential_id
 JOIN agenteam_secret.secret_payloads p ON p.payload_id=s.current_payload_id
 WHERE v.project_id=$1 AND v.id=$2 AND p.project_id=v.project_id AND p.owner_kind=1 AND p.owner_id=s.id`, o.project, o.target).Scan(&owner, &payload, &dn, &ct, &wn, &wrapped, &version)
	if err != nil || version != 1 {
		return false
	}
	uuid := func(raw string) []byte { value, _ := hex.DecodeString(strings.ReplaceAll(raw, "-", "")); return value }
	aad := func(domain string) []byte {
		value := append([]byte(domain), 0)
		value = binary.BigEndian.AppendUint32(value, 1)
		value = append(value, 2)
		value = append(value, uuid(o.project)...)
		value = append(value, 1)
		value = append(value, uuid(owner)...)
		return append(value, uuid(payload)...)
	}
	gcm := func(key []byte) (cipher.AEAD, error) {
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, err
		}
		return cipher.NewGCM(block)
	}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(0x20 + i)
	}
	defer clear(key)
	master, err := gcm(key)
	if err != nil {
		return false
	}
	wa := binary.BigEndian.AppendUint64(aad("agenteam.secret.wrap.v1"), version)
	wa = append(wa, dn...)
	ciphertextDigest := sha256.Sum256(ct)
	wa = append(wa, ciphertextDigest[:]...)
	if len(wn) != master.NonceSize() {
		return false
	}
	dek, err := master.Open(nil, wn, wrapped, wa)
	if err != nil {
		return false
	}
	defer clear(dek)
	data, err := gcm(dek)
	if err != nil || len(dn) != data.NonceSize() {
		return false
	}
	actual, err := data.Open(nil, dn, ct, aad("agenteam.secret.data.v1"))
	if err != nil {
		return false
	}
	defer clear(actual)
	return bytes.Equal(actual, expected)
}
func (o *projectSecretWriteObservations) committed(ctx context.Context, in projectSecretWriteIntent, raw []byte) error {
	defer clear(in.value)
	fail := func() error { return errors.New("Secret committed facts mismatch") }
	var receipt vc.SecretVariableMutation
	if in.kind == "lookup" {
		var lookup vc.SecretVariableCommandLookup
		if skillWebDecode(raw, &lookup) != nil || lookup.Validate() != nil || lookup.Status() != vc.SecretLookupCommitted || lookup.Receipt() == nil {
			return fail()
		}
		receipt = *lookup.Receipt()
	} else if skillWebDecode(raw, &receipt) != nil {
		return fail()
	}
	if receipt.Validate() != nil {
		return fail()
	}
	fields := receipt.Fields()
	variable := fields.Variable.Fields()
	command := vc.SecretUpdateCommand
	if in.kind == "create" {
		command = vc.SecretCreateCommand
	}
	if fields.Command != command || !fields.Changed || fields.EventID == nil || fields.AuditID == nil || variable.ID.String() != o.target || variable.ProjectID.String() != o.project || variable.Type != "secret" {
		return fail()
	}
	expectedVersion := 1
	if in.expected != "" {
		parsed, err := strconv.Atoi(in.expected)
		if err != nil {
			return fail()
		}
		expectedVersion = parsed + 1
	}
	if in.kind != "create" && ((!in.namePresent && variable.Name != o.current["name"]) || (!in.descriptionPresent && variable.Description != o.current["description"])) {
		return fail()
	}
	if in.kind != "create" {
		previous, err := json.Marshal(variable.CreatedAt)
		if err != nil {
			return fail()
		}
		var instant string
		if json.Unmarshal(previous, &instant) != nil || instant != o.current["created_at"] {
			return fail()
		}
	}
	if int(variable.Version) != expectedVersion || (in.namePresent && variable.Name != in.name) || (in.descriptionPresent && variable.Description != in.description) {
		return fail()
	}
	safe, err := json.Marshal(receipt)
	if err != nil || projectSecretsLeak(safe, *o.forbidden) {
		return fail()
	}
	defer clear(safe)
	var stored []byte
	var matches bool
	err = o.conn.QueryRow(ctx, `SELECT c.receipt::text, (
 c.actor_user_id=$4 AND c.target_id=$5 AND c.expected_version IS NOT DISTINCT FROM $6::bigint
 AND c.name_present=$7 AND c.description_present=$8 AND c.value_present=$9
 AND c.secret_effect=$10 AND NOT c.secret_deleted AND c.committed_at IS NOT NULL
 AND c.event_id=$11 AND c.audit_id=$12
 AND c.credential_id=v.credential_id AND c.credential_version=v.credential_version
 AND r.project_id=c.project_id AND r.variable_id=c.target_id AND r.user_id=c.actor_user_id
 AND r.command_kind=c.command_name AND r.external_expected_version IS NOT DISTINCT FROM c.expected_version
 AND r.credential_id=c.credential_id AND r.effect=c.secret_effect AND r.result_version=c.credential_version AND NOT r.deleted
 AND p.owner_kind=3 AND p.owner_id=r.id AND p.project_id=r.project_id
 AND h.project_id=c.project_id AND h.variable_id=c.target_id AND h.version=$13 AND h.actor_user_id=c.actor_user_id
 AND a.project_id=c.project_id AND a.resource_id=c.target_id AND a.action=c.command_name
 AND e.project_id=c.project_id AND e.event_type='project.secret_variable_changed'
 AND v.type='secret' AND v.value IS NULL AND v.version=$13 AND v.deleted_at IS NULL AND v.name=$14 AND v.description=$15)
 FROM agenteam_projectvariable.secret_commands c
 JOIN agenteam_projectvariable.variables v ON v.project_id=c.project_id AND v.id=c.target_id
 JOIN agenteam_secret.project_variable_receipts r ON r.id=c.d04_receipt_id
 JOIN agenteam_secret.secret_payloads p ON p.payload_id=r.digest_payload_id
 JOIN agenteam_projectvariable.secret_history h ON h.operation_id=c.id AND h.event_id=c.event_id
 JOIN agenteam_audit.audit_records a ON a.id=c.audit_id
 JOIN agenteam_outbox.events e ON e.id=c.event_id
 WHERE c.project_id=$1 AND c.command_name=$2 AND c.idempotency_key=$3`, o.project, string(command), in.key, o.actor, o.target, projectSecretWriteExpected(in.expected), in.namePresent, in.descriptionPresent, in.valuePresent,
		map[string]string{"create": "create", "metadata": "none", "replace": "replace"}[in.operation], fields.EventID.String(), fields.AuditID.String(), expectedVersion, variable.Name, variable.Description).Scan(&stored, &matches)
	if err != nil || !matches {
		return fail()
	}
	defer clear(stored)
	var a, b any
	if json.Unmarshal(stored, &a) != nil || json.Unmarshal(safe, &b) != nil || !reflect.DeepEqual(a, b) {
		return fail()
	}
	total := expectedVersion
	var commands, history, audit, outbox, protected int
	err = o.conn.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_projectvariable.secret_commands WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_projectvariable.secret_history WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action LIKE 'project.secret_variable.%'),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND event_type='project.secret_variable_changed'),
 (SELECT count(*) FROM agenteam_secret.project_variable_receipts WHERE project_id=$1)`, o.project).Scan(&commands, &history, &audit, &outbox, &protected)
	if err != nil || commands != total || history != total || audit != total || outbox != total || protected != total {
		return fail()
	}
	if in.kind != "lookup" && in.valuePresent && !o.materialMatches(ctx, in.value) {
		return fail()
	}
	credential, err := o.credentialState(ctx)
	if err != nil {
		return fail()
	}
	if in.kind == "lookup" || in.operation == "metadata" {
		if credential != o.credential {
			return fail()
		}
	} else if credential == o.credential {
		return fail()
	}
	o.credential = credential
	var privateIDs []string
	err = o.conn.QueryRow(ctx, `SELECT array_agg(value ORDER BY value) FROM (
 SELECT credential_id::text AS value FROM agenteam_projectvariable.variables WHERE project_id=$1
 UNION SELECT id::text FROM agenteam_secret.project_variable_receipts WHERE project_id=$1
 UNION SELECT payload_id::text FROM agenteam_secret.secret_payloads WHERE project_id=$1) q`, o.project).Scan(&privateIDs)
	if err != nil {
		return fail()
	}
	for _, v := range privateIDs {
		*o.forbidden = append(*o.forbidden, []byte(v))
	}
	clear(privateIDs)
	state, err := o.snapshot(ctx)
	if err != nil {
		return fail()
	}
	if in.kind == "lookup" && state != o.stable {
		return fail()
	}
	o.stable = state
	metadata, err := json.Marshal(fields.Variable)
	if err != nil {
		return fail()
	}
	defer clear(metadata)
	return json.Unmarshal(metadata, &o.current)
}
func projectSecretWriteExpected(value string) any {
	if value == "" {
		return nil
	}
	parsed, _ := strconv.Atoi(value)
	return int64(parsed)
}

func (o *projectSecretWriteObservations) currentRead(ctx context.Context, r *http.Request, raw []byte) error {
	prefix := "/api/v1/projects/" + o.project + "/secret-variables"
	if r.URL.Path == prefix {
		if r.URL.RawQuery != "limit=50" {
			return errors.New("Secret list scope")
		}
		var page f.Page[vc.SecretVariable]
		var value map[string]any
		if skillWebDecode(raw, &page) != nil || json.Unmarshal(raw, &value) != nil || len(value) != 1 || page.NextCursor != "" {
			return errors.New("Secret list shape")
		}
		items, ok := value["items"].([]any)
		expected := 0
		if o.current != nil {
			expected = 1
		}
		if !ok || len(items) != expected || len(page.Items) != expected || (expected == 1 && (page.Items[0].Validate() != nil || !reflect.DeepEqual(items[0], o.current))) {
			return errors.New("Secret list current metadata")
		}
		o.lists++
	} else if o.target != "" && r.URL.Path == prefix+"/"+o.target {
		var variable vc.SecretVariable
		var value map[string]any
		if r.URL.RawQuery != "" || skillWebDecode(raw, &variable) != nil || variable.Validate() != nil || json.Unmarshal(raw, &value) != nil || !reflect.DeepEqual(value, o.current) {
			return errors.New("Secret current detail")
		}
		o.gets++
	} else {
		allowed := map[string]bool{"/api/v1/session": true, "/api/v1/auth/bootstrap": true, "/api/v1/me": true, "/api/v1/me/preferences": true, "/api/v1/projects": true, "/api/v1/projects/resolve": true, "/api/v1/projects/" + o.project: true}
		if !allowed[r.URL.Path] {
			return errors.New("undeclared Secret read")
		}
		return nil
	}
	state, err := o.snapshot(ctx)
	if err != nil || state != o.stable {
		return errors.New("Secret read changed facts")
	}
	return nil
}
func (o *projectSecretWriteObservations) capture(response *http.Response) error {
	o.mu.Lock()
	active := o.active
	o.mu.Unlock()
	if !active {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	closed := response.Body.Close()
	headers, headerErr := json.Marshal(response.Header)
	if err != nil || closed != nil || headerErr != nil || response.StatusCode != 200 || len(raw) > 1<<20 || response.ContentLength != int64(len(raw)) || !json.Valid(raw) || response.Header.Get("Cache-Control") != "no-store" {
		clear(raw)
		return o.fail()
	}
	rid, err := f.ParseID[f.Request](response.Header.Get("X-Request-ID"))
	if err != nil || len(response.Header.Values("X-Request-ID")) != 1 {
		clear(raw)
		return o.fail()
	}
	o.mu.Lock()
	tail := o.tails[rid.String()]
	in, write := o.intents[rid.String()]
	o.mu.Unlock()
	if tail == nil || tail.method != response.Request.Method || tail.uri != response.Request.URL.RequestURI() {
		clear(raw)
		return o.fail()
	}
	select {
	case <-tail.done:
	case <-response.Request.Context().Done():
		clear(raw)
		return o.fail()
	}
	o.mu.Lock()
	valid := tail.returned && !projectSecretsLeak(raw, *o.forbidden) && !projectSecretsLeak(headers, *o.forbidden)
	if valid && write {
		valid = o.committed(response.Request.Context(), in, raw) == nil
	}
	if valid && !write {
		valid = o.currentRead(response.Request.Context(), response.Request, raw) == nil
	}
	o.mu.Unlock()
	if !valid {
		clear(raw)
		return o.fail()
	}
	// The response is now a checked safe metadata/receipt. Only that response
	// may be hashed. Request bytes and private canonical snapshots never are.
	record := map[string]any{"input_hash": o.inputHash, "request_id": rid.String(), "method": tail.method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(tail.uri))), "status": 200, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true, "request_host_matches": true}
	if write {
		record["kind"] = in.kind
		record["request_bound"] = true
		record["input_verified"] = true
		record["committed_verified"] = true
		record["idempotency_key_sha256"] = fmt.Sprintf("%x", sha256.Sum256([]byte(in.key)))
	}
	if o.writeProof(record) != nil {
		clear(raw)
		return o.fail()
	}
	o.mu.Lock()
	o.count++
	o.mu.Unlock()
	if write && in.kind == "update" && o.mode == "secret-write-lost" {
		clear(raw)
		return &projectSecretWriteDrop{header: response.Header.Clone(), length: int(response.ContentLength), record: record}
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}
func (o *projectSecretWriteObservations) forbiddenSnapshot() [][]byte {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([][]byte(nil), (*o.forbidden)...)
}
func (o *projectSecretWriteObservations) writeProof(record map[string]any) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return errors.New("Secret proof encoding")
	}
	if projectSecretsLeak(raw, o.forbiddenSnapshot()) {
		return errors.New("Secret proof safety")
	}
	rid, ok := record["request_id"].(string)
	if !ok {
		return errors.New("Secret proof identity")
	}
	return os.WriteFile(filepath.Join(o.evidence, "project-secret-write-"+o.mode+"-response-"+rid+".json"), raw, 0600)
}
func (o *projectSecretWriteObservations) proxyError(w http.ResponseWriter, _ *http.Request, err error) {
	var drop *projectSecretWriteDrop
	if !errors.As(err, &drop) {
		_ = o.fail()
		http.Error(w, "owned Secret proxy failed", 502)
		return
	}
	for key, values := range drop.header {
		w.Header()[key] = append([]string(nil), values...)
	}
	w.Header().Set("Content-Length", strconv.Itoa(drop.length))
	w.Header().Set("Connection", "close")
	w.WriteHeader(http.StatusOK)
	n, writeErr := io.WriteString(w, "{")
	flushErr := http.NewResponseController(w).Flush()
	conn, _, hijackErr := http.NewResponseController(w).Hijack()
	var closeErr error
	if conn != nil {
		closeErr = conn.Close()
	}
	if n != 1 || writeErr != nil || flushErr != nil || hijackErr != nil || conn == nil || closeErr != nil {
		_ = o.fail()
		return
	}
	drop.record["receipt_cut"] = true
	if o.writeProof(drop.record) != nil {
		_ = o.fail()
		return
	}
	o.mu.Lock()
	o.cuts++
	o.mu.Unlock()
}
