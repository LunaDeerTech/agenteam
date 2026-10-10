//go:build integration

package app

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
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// One real, ordinary Owner reads the published builtin Skill metadata. The separate Project
// fixture never replaces the default root's deliberately unbound initializer.
// Static hosting is private test infrastructure, not production SPA delivery.
func TestSkillOwnerReadWeb(t *testing.T) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(func() {
		cancel()
		if time.Since(started) > 120*time.Second {
			t.Error("Skill browser exceeded its original 120s including cleanup")
		}
	})
	check := func(stage string, err error) {
		t.Helper()
		if err != nil {
			var fault *f.Fault
			if errors.As(err, &fault) {
				t.Fatalf("Skill browser %s: %s/%s", stage, fault.Code.Safe(), fault.CommitState.Safe())
			}
			t.Fatalf("Skill browser %s failed", stage)
		}
	}
	dist := os.Getenv("AGENTEAM_SKILL_OWNER_WEB_DIST")
	runtime := os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	evidence := os.Getenv("AGENTEAM_SKILL_OWNER_WEB_EVIDENCE")
	inputHash := os.Getenv("AGENTEAM_SKILL_OWNER_WEB_INPUT_HASH")
	decoded, hashErr := hex.DecodeString(inputHash)
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || hashErr != nil || len(decoded) != sha256.Size {
		t.Fatal("exact owned Skill assets/runtime/evidence/input required")
	}
	info, err := os.Lstat(filepath.Join(dist, "index.html"))
	check("dist", err)
	if !info.Mode().IsRegular() {
		t.Fatal("regular private dist required")
	}
	evidence = filepath.Join(evidence, t.Name())
	check("fresh evidence", os.Mkdir(evidence, 0700))
	private, err := os.MkdirTemp(runtime, "su-")
	check("private browser directory", err)
	t.Cleanup(func() {
		check("private removal", os.RemoveAll(private))
		if _, err := os.Lstat(private); !errors.Is(err, os.ErrNotExist) {
			t.Error("private Skill directory remains")
		}
	})
	db := pgfixture.NewDatabase(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	check("private listener", err)
	t.Cleanup(func() { _ = listener.Close() })
	origin := "http://" + listener.Addr().String()
	cfg := guardConfiguration(t, append(guardEnvironment(t, db), "AGENTEAM_CENTRAL_PUBLIC_ORIGIN="+origin, "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s"))
	var core *account.Service
	var owned *resources
	deps := dependencies{observeAccount: func(v *account.Service) { core = v }, bind: func(c context.Context, cfg config.Config, store database, owner *resources, d *dependencies) error {
		owned = owner
		return bindAccounts(c, cfg, store, owner, d)
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
			t.Error("Skill root actual join exceeded cleanup observation")
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
	documents, knowledgeOK := assembly.knowledge.(*knowledgeWork)
	assembly.mu.Unlock()
	objects, objectOK := owned.objects().(*objectAssembly)
	if !projectOK || !skillOK || !knowledgeOK || !objectOK {
		t.Fatal("original concrete domains missing")
	}
	backend, err := url.Parse("http://" + address)
	check("backend URL", err)
	transport := &http.Transport{Proxy: nil}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport = transport
	proxy.ErrorLog = slog.NewLogLogger(slog.NewTextHandler(io.Discard, nil), slog.LevelError)
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "Owned backend unavailable", http.StatusBadGateway)
	}
	observations := &skillWebObservations{evidence: evidence, inputHash: inputHash, run: t.Name()}
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
			if strings.TrimPrefix(r.URL.Path, "/assets/") != filepath.Base(r.URL.Path) {
				http.NotFound(w, r)
				return
			}
			http.ServeFile(w, r, filepath.Join(dist, "assets", filepath.Base(r.URL.Path)))
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
	// Both Humans are created by the original Invitation and Session HTTP paths.
	type human struct {
		email, username, password, cookie, token string
		client                                   *http.Client
		actor                                    id.Actor
	}
	newHuman := func(email, username string) human {
		invitation := setup.call(admin, "POST", "/api/v1/system/invitations", map[string]string{"email": email}, knowledgeWebString(t, adminSession, "csrf_token"), true, 201)
		record := setup.record("invitation", email, knowledgeWebString(t, invitation, "id"))
		inviteURL, err := url.Parse(record.URL)
		check("invitation URL", err)
		if inviteURL.Scheme+"://"+inviteURL.Host != origin || inviteURL.Fragment == "" {
			t.Fatal("invitation origin binding missing")
		}
		client := setup.client()
		bootstrap := setup.call(client, "GET", "/api/v1/auth/bootstrap", nil, "", false, 200)
		csrf := knowledgeWebString(t, bootstrap, "csrf_token")
		setup.call(client, "POST", "/api/v1/invitations/inspect", map[string]string{"token": inviteURL.Fragment}, csrf, false, 200)
		password := "Skill private " + guardID[struct{}](t).String() + "!"
		setup.call(client, "POST", "/api/v1/invitations/redeem", map[string]string{"token": inviteURL.Fragment, "username": username, "display_name": username, "password": password, "confirmation": password}, csrf, true, 201)
		setup.call(client, "POST", "/api/v1/sessions/login", map[string]string{"email": email, "password": password}, csrf, true, 200)
		session := setup.call(client, "GET", "/api/v1/session", nil, "", false, 200)
		user, ok := session["user"].(map[string]any)
		if !ok || user["role"] != "user" || user["username"] != username {
			t.Fatal("formal ordinary Human missing")
		}
		originURL, err := url.Parse(origin)
		check("owned origin", err)
		var cookie string
		for _, value := range client.Jar.Cookies(originURL) {
			if value.Name == "agenteam_local_session" {
				cookie = value.Value
			}
		}
		material, err := sc.NewSecretMaterial([]byte(cookie))
		check("formal Human cookie", err)
		defer material.Destroy()
		actor, err := core.Authenticate(ctx, material)
		check("same current Account authority", err)
		if actor.Details().UserID != knowledgeWebString(t, user, "id") {
			t.Fatal("Human Session identity mismatch")
		}
		return human{email, username, password, cookie, inviteURL.Fragment, client, actor}
	}
	owner := newHuman("skill-owner@example.com", "skill-owner")
	other := newHuman("skill-other@example.com", "skill-other")
	actor := owner.actor
	meta := func() f.CommandMeta {
		return f.CommandMeta{RequestID: guardID[f.Request](t), IdempotencyKey: f.IdempotencyKey(guardID[struct{}](t).String())}
	}
	blockedID := guardID[id.Project](t)
	blocked, err := projects.service.CreateProject(ctx, actor, meta(), pc.CreateProjectRequest{ProjectID: blockedID, Name: "skill-read", Description: "Default initializer stays unbound"})
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.DependencyUnbound || fault.CommitState != f.NotCommitted || blocked.State != "" || blocked.Project != nil || blocked.Operation != nil {
		t.Fatal("default Project creation did not remain unbound/not_committed")
	}
	conn := db.Connect(t)
	var facts int
	check("default unbound zero facts", conn.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_project.projects WHERE id=$1 OR (owner_user_id=$2 AND normalized_name='skill-read'))+
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
	created, createErr := fixture.CreateProject(ctx, actor, meta(), pc.CreateProjectRequest{ProjectID: projectID, Name: "skill-read", Description: "Existing documents for the bounded UI read"})
	drainErr := fixtureOwner.Drain(ctx)
	check("test-only real Project", createErr)
	check("test-only actual Drain", drainErr)
	if !fixtureOwner.Joined() || created.Validate() != nil || created.State != pc.CreationReady || created.Project == nil || created.Project.ID != projectID {
		t.Fatal("isolated real Project did not confirm and join")
	}
	listed, err := skills.service.ListSkills(ctx, actor, projectID)
	check("published Skill directory", err)
	if len(listed) != 1 || listed[0].Validate() != nil || listed[0].ProjectID != projectID || !listed[0].Protected || listed[0].Name != "Add Skills" || listed[0].CurrentRevision != 1 || listed[0].Version != 1 {
		t.Fatal("real builtin Skill not published")
	}
	metadata := listed[0]
	got, err := skills.service.GetSkill(ctx, actor, projectID, metadata.ID)
	check("published Skill detail", err)
	if got != metadata {
		t.Fatal("real published Skill mismatch")
	}
	// Exact Project-scoped snapshots exclude expected Account login/logout facts.
	factsSnapshot := func() [10]string {
		var values [10]string
		for i, table := range []string{"agenteam_skill.initializations", "agenteam_skill.skills", "agenteam_skill.revisions", "agenteam_skill.work", "agenteam_skill.object_attempts", "agenteam_project.commands", "agenteam_object.uploads", "agenteam_object.objects", "agenteam_audit.audit_records", "agenteam_outbox.events"} {
			check("Project read-only snapshot", conn.QueryRow(ctx, `SELECT md5(COALESCE(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text)::text,'[]')) FROM `+table+` x WHERE project_id=$1`, projectID.String()).Scan(&values[i]))
		}
		return values
	}
	before := factsSnapshot()
	for _, path := range []string{"/api/v1/projects/" + projectID.String() + "/skills", "/api/v1/projects/" + projectID.String() + "/skills/" + metadata.ID.String()} {
		problem := setup.call(other.client, "GET", path, nil, "", false, 404)
		if problem["code"] != "NOT_FOUND" || problem["commit_state"] != "not_started" {
			t.Fatal("other Human Skills read did not return strict inaccessible Problem")
		}
	}
	observations.mu.Lock()
	observations.project = projectID.String()
	observations.skill = metadata.ID.String()
	observations.mu.Unlock()
	payload := map[string]any{"email": owner.email, "password": owner.password, "user_id": actor.Details().UserID, "username": owner.username, "project_id": projectID.String(), "project_name": "skill-read", "skill": metadata, "other_email": other.email, "other_password": other.password, "other_user_id": other.actor.Details().UserID, "input_hash": inputHash}
	raw, err := json.Marshal(payload)
	check("private transfer", err)
	check("private material", os.WriteFile(filepath.Join(private, "skill-owner-material.json"), raw, 0600))
	clear(raw)
	result := skillWebBrowser(t, ctx, private, origin, dist, evidence, inputHash)
	for _, key := range []string{"completed", "two_get", "keyboard_focus", "themes_layout", "other_human_hidden", "original_consumed", "typed_published", "observers_joined"} {
		if result[key] != true {
			t.Fatalf("Skill browser proof incomplete: %s", key)
		}
	}
	observations.mu.Lock()
	observed, observeErr := observations.count, observations.err
	observations.mu.Unlock()
	if observeErr != nil || observed == 0 || result["responses"] != float64(observed) {
		t.Fatal("browser/real response observation count mismatch")
	}
	if factsSnapshot() != before {
		t.Fatal("Skills reads changed Project business facts")
	}
	for _, secret := range []string{adminRecord.Password, owner.token, owner.password, owner.cookie, other.token, other.password, other.cookie} {
		if secret != "" && strings.Contains(output.String(), secret) {
			t.Error("private Account material escaped into root log")
		}
	}
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
	if !fixtureOwner.Joined() || !projects.Joined() || !skills.service.Joined() || !documents.Joined() || !assembly.Joined() || !owned.producersJoined() {
		t.Fatal("original domain owners did not join")
	}
	check("released guard", syscall.Flock(int(claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	check("guard unlock", syscall.Flock(int(claim.Fd()), syscall.LOCK_UN))
	var stopped string
	check("guard stopped fact", conn.QueryRow(ctx, `SELECT state FROM agenteam_object.process_claims WHERE process_id=$1`, objects.process.String()).Scan(&stopped))
	if stopped != "stopped" {
		t.Fatal("original process claim not stopped")
	}
	t.Log("Skill normal browser chain: original Node Wait, proxy/root join, domain retirement and guard facts complete; production initializer remains unbound")
}

type skillWebObservations struct {
	mu                                       sync.Mutex
	evidence, inputHash, run, project, skill string
	count                                    int
	err                                      error
}

func (o *skillWebObservations) capture(response *http.Response) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.project == "" || !strings.HasPrefix(response.Request.URL.Path, "/api/v1/projects/"+o.project+"/skills") {
		return nil
	}
	fail := func() error { o.err = errors.New("closed Skill read observation failed"); return o.err }
	if (response.Request.URL.Path != "/api/v1/projects/"+o.project+"/skills" && response.Request.URL.Path != "/api/v1/projects/"+o.project+"/skills/"+o.skill) || response.Request.Method != "GET" || response.StatusCode != 200 || o.count >= 32 || response.Request.URL.RawQuery != "" {
		return fail()
	}
	limit := int64(64 << 10)
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	closed := response.Body.Close()
	if err != nil || closed != nil || int64(len(raw)) > limit || response.ContentLength != int64(len(raw)) || !json.Valid(raw) || response.Header.Get("Cache-Control") != "no-store" {
		return fail()
	}
	if _, err := f.ParseID[f.Request](response.Header.Get("X-Request-ID")); err != nil {
		return fail()
	}
	sum := fmt.Sprintf("%x", sha256.Sum256(raw))
	name := "body-" + sum + ".json"
	if err := os.WriteFile(filepath.Join(o.evidence, name), raw, 0600); err != nil {
		return fail()
	}
	o.count++
	meta := map[string]any{"sequence": o.count, "source_run": o.run, "input_hash": o.inputHash, "method": response.Request.Method, "path": response.Request.URL.Path, "query": response.Request.URL.RawQuery, "status": response.StatusCode, "request_id": response.Header.Get("X-Request-ID"), "content_length": response.ContentLength, "body_file": name, "body_sha256": sum}
	encoded, err := json.Marshal(meta)
	if err != nil {
		return fail()
	}
	if err := os.WriteFile(filepath.Join(o.evidence, fmt.Sprintf("response-%03d.json", o.count)), encoded, 0600); err != nil {
		return fail()
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}

func skillWebBrowser(t *testing.T, ctx context.Context, private, origin, dist, evidence, inputHash string) map[string]any {
	t.Helper()
	harness, err := filepath.Abs("../../../tests/account-captcha-web")
	if err != nil {
		t.Fatal("browser harness path unavailable")
	}
	cmd := exec.CommandContext(ctx, "node", filepath.Join(harness, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(harness, "skill-owner-read.config.js"))
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
	cmd.Env = append(cmd.Env, "TMPDIR="+private, "AGENTEAM_AUTH_WEB_ORIGIN="+origin, "AGENTEAM_AUTH_WEB_PRIVATE="+private, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_SKILL_OWNER_WEB_DIST="+dist, "AGENTEAM_SKILL_OWNER_WEB_EVIDENCE="+evidence, "AGENTEAM_SKILL_OWNER_WEB_INPUT_HASH="+inputHash, "AGENTEAM_SKILL_OWNER_WEB_CASE=read", "AGENTEAM_SKILL_OWNER_WEB_SCHEMA_PYTHON="+os.Getenv("AGENTEAM_SKILL_OWNER_WEB_SCHEMA_PYTHON"), "PLAYWRIGHT_NO_COPY_PROMPT=1")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal("locked Skill Node could not start")
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
	t.Logf("Skill Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	// Never print the browser's credential-bearing call log. The spec writes
	// fixed-stage safe failure diagnostics; the original child status is final.
	if err != nil || !regexp.MustCompile(`(?m)\b1 passed\b`).Match(output.Bytes()) {
		t.Fatal("Skill locked browser did not actually pass its one case")
	}
	raw, err := os.ReadFile(filepath.Join(private, "skill-result.json"))
	if err != nil {
		t.Fatal("Skill browser result missing")
	}
	if len(raw) > 16<<10 {
		t.Fatal("Skill browser result too large")
	}
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil || result["input_hash"] != inputHash {
		t.Fatal("Skill browser result not bound to frozen inputs")
	}
	return result
}
