//go:build integration

package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
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
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// Existing initialized Project from the explicit, actually drained test-only
// real Project ports. The default root's initializer remains unbound.
func TestProjectSecretOwnerWeb(t *testing.T) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(func() {
		cancel()
		if time.Since(started) > 120*time.Second {
			t.Error("Secret Owner browser exceeded its original 120s including cleanup")
		}
	})
	check := func(stage string, err error) {
		t.Helper()
		if err != nil {
			var fault *f.Fault
			if errors.As(err, &fault) {
				t.Fatalf("Secret Owner browser %s: %s/%s", stage, fault.Code.Safe(), fault.CommitState.Safe())
			}
			t.Fatalf("Secret Owner browser %s failed", stage)
		}
	}
	dist := os.Getenv("AGENTEAM_SECRET_OWNER_WEB_DIST")
	runtime := os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	evidence := os.Getenv("AGENTEAM_SECRET_OWNER_WEB_EVIDENCE")
	inputHash := os.Getenv("AGENTEAM_SECRET_OWNER_WEB_INPUT_HASH")
	decoded, hashErr := hex.DecodeString(inputHash)
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || hashErr != nil || len(decoded) != sha256.Size {
		t.Fatal("exact owned Knowledge assets/runtime/evidence/input required")
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
			t.Error("private Secret Owner directory remains")
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
			t.Error("Secret Owner root actual join exceeded cleanup observation")
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
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "Owned backend unavailable", http.StatusBadGateway)
	}
	observations := &secretOwnerWebObservations{evidence: evidence, inputHash: inputHash, run: t.Name(), counts: map[string]int{}}
	proxy.ErrorHandler = observations.unavailable
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
	email, username := "secret-owner@example.com", "secret-owner"
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
	password := "Secret Owner private " + guardID[struct{}](t).String() + "!"
	setup.call(ownerClient, "POST", "/api/v1/invitations/redeem", map[string]string{"token": inviteURL.Fragment, "username": username, "display_name": "Secret Owner", "password": password, "confirmation": password}, anonymousCSRF, true, 201)
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
	blocked, err := projects.service.CreateProject(ctx, actor, meta(), pc.CreateProjectRequest{ProjectID: blockedID, Name: "secret-owner", Description: "Default initializer stays unbound"})
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.DependencyUnbound || fault.CommitState != f.NotCommitted || blocked.State != "" || blocked.Project != nil || blocked.Operation != nil {
		t.Fatal("default Project creation did not remain unbound/not_committed")
	}
	conn := db.Connect(t)
	var facts int
	check("default unbound zero facts", conn.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_project.projects WHERE id=$1 OR (owner_user_id=$2 AND normalized_name='secret-owner'))+
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
	created, createErr := fixture.CreateProject(ctx, actor, meta(), pc.CreateProjectRequest{ProjectID: projectID, Name: "secret-owner", Description: "Existing documents for the bounded UI read"})
	drainErr := fixtureOwner.Drain(ctx)
	check("test-only real Project", createErr)
	check("test-only actual Drain", drainErr)
	if !fixtureOwner.Joined() || created.Validate() != nil || created.State != pc.CreationReady || created.Project == nil || created.Project.ID != projectID {
		t.Fatal("isolated real Project did not confirm and join")
	}
	canaries := []string{"SECRET_UI_FIRST_" + guardID[struct{}](t).String() + "中", "SECRET_UI_REPLACE_" + guardID[struct{}](t).String() + "文"}
	observations.mu.Lock()
	observations.project = projectID.String()
	observations.forbidden = secretOwnerWebForms(canaries)
	observations.mu.Unlock()
	payload := map[string]any{"email": email, "password": password, "user_id": actor.Details().UserID, "username": username, "project_id": projectID.String(), "project_name": "secret-owner", "values": canaries, "input_hash": inputHash}
	raw, err := json.Marshal(payload)
	check("private transfer", err)
	check("private material", os.WriteFile(filepath.Join(private, "secret-owner-material.json"), raw, 0600))
	clear(raw)
	result := secretOwnerWebBrowser(t, ctx, private, origin, dist, evidence, inputHash)
	for _, key := range []string{"completed", "created", "unknown", "lookup_confirmed", "current_version", "deleted", "input_cleared", "no_material", "original_consumed", "typed_published", "observers_joined"} {
		if result[key] != true {
			t.Fatalf("Secret browser proof incomplete: %s", key)
		}
	}
	observations.mu.Lock()
	observed, observeErr, target := observations.count, observations.err, observations.target
	correct := observations.dropped && observations.lookupSameKey && observations.counts["create"] == 1 && observations.counts["update"] == 1 && observations.counts["delete"] == 1 && observations.counts["lookup"] == 1
	observations.mu.Unlock()
	if observeErr != nil || !correct || target == "" || result["responses"] != float64(observed) {
		t.Fatal("Secret original response/command observations incomplete")
	}
	var history, audits, events, commands, receipts int
	check("exact Secret facts", conn.QueryRow(ctx, `SELECT
	 (SELECT count(*) FROM agenteam_projectvariable.secret_history WHERE project_id=$1),
	 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action IN ('project.secret_variable.create','project.secret_variable.update','project.secret_variable.delete')),
	 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND event_type='project.secret_variable_changed'),
	 (SELECT count(*) FROM agenteam_projectvariable.secret_commands WHERE project_id=$1),
	 (SELECT count(*) FROM agenteam_secret.project_variable_receipts WHERE project_id=$1)`, projectID.String()).Scan(&history, &audits, &events, &commands, &receipts))
	if history != 3 || audits != 3 || events != 3 || commands != 3 || receipts != 3 {
		t.Fatal("Secret command facts omitted or duplicated")
	}
	// Only safe postconditions are read. No material consumption or raw business SQL writes.
	targetID, err := f.ParseID[id.ProjectVariable](target)
	check("actual target", err)
	_, err = variables.service.GetSecretVariable(ctx, actor, projectID, targetID)
	if !errors.As(err, &fault) || fault.Code != f.NotFound {
		t.Fatal("deleted Secret remained readable")
	}
	for _, value := range append(secretOwnerWebForms(canaries), adminRecord.Password, inviteURL.Fragment, password, cookie) {
		if value != "" && strings.Contains(output.String(), value) {
			t.Fatal("private material escaped into root log")
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
	t.Log("Secret Owner browser chain: original Node Wait, proxy/root join, domain retirement and guard facts complete; production initializer remains unbound")
}

func secretOwnerWebBrowser(t *testing.T, ctx context.Context, private, origin, dist, evidence, inputHash string) map[string]any {
	t.Helper()
	harness, err := filepath.Abs("../../../tests/account-captcha-web")
	if err != nil {
		t.Fatal("browser harness path unavailable")
	}
	cmd := exec.CommandContext(ctx, "node", filepath.Join(harness, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(harness, "project-secret-owner.config.js"))
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
	cmd.Env = append(cmd.Env, "TMPDIR="+private, "AGENTEAM_AUTH_WEB_ORIGIN="+origin, "AGENTEAM_AUTH_WEB_PRIVATE="+private, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_SECRET_OWNER_WEB_DIST="+dist, "AGENTEAM_SECRET_OWNER_WEB_EVIDENCE="+evidence, "AGENTEAM_SECRET_OWNER_WEB_INPUT_HASH="+inputHash, "AGENTEAM_SECRET_OWNER_WEB_CASE=owner", "AGENTEAM_SECRET_OWNER_WEB_SCHEMA_PYTHON="+os.Getenv("AGENTEAM_SECRET_OWNER_WEB_SCHEMA_PYTHON"), "PLAYWRIGHT_NO_COPY_PROMPT=1")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal("locked SecretOwner Node could not start")
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
	t.Logf("SecretOwner Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	// Never print the browser's credential-bearing call log. The spec writes
	// fixed-stage safe failure diagnostics; the original child status is final.
	if err != nil || !regexp.MustCompile(`(?m)\b1 passed\b`).Match(output.Bytes()) {
		t.Fatal("Secret Owner locked browser did not actually pass its one case")
	}
	raw, err := os.ReadFile(filepath.Join(private, "secret-owner-result.json"))
	if err != nil {
		t.Fatal("Secret Owner browser result missing")
	}
	if len(raw) > 16<<10 {
		t.Fatal("Secret Owner browser result too large")
	}
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil || result["input_hash"] != inputHash {
		t.Fatal("Secret Owner browser result not bound to frozen inputs")
	}
	return result
}

var errSecretOwnerResponseLoss = errors.New("controlled committed response loss")

type secretOwnerWebObservations struct {
	mu                                        sync.Mutex
	evidence, inputHash, run, project, target string
	counts                                    map[string]int
	count                                     int
	err                                       error
	forbidden                                 []string
	dropped, lookupSameKey                    bool
	patchKey, dropXID                         string // Private command identity, never written to evidence.
}

func secretOwnerWebForms(values []string) []string {
	var out []string
	for _, value := range values {
		encoded, _ := json.Marshal(value)
		sum := sha256.Sum256([]byte(value))
		out = append(out, value, string(encoded[1:len(encoded)-1]), base64.StdEncoding.EncodeToString([]byte(value)), base64.RawStdEncoding.EncodeToString([]byte(value)), hex.EncodeToString(sum[:]))
	}
	return out
}

func (o *secretOwnerWebObservations) unavailable(w http.ResponseWriter, r *http.Request, err error) {
	o.mu.Lock()
	controlled := errors.Is(err, errSecretOwnerResponseLoss) && o.dropped && r.Method == http.MethodPatch && r.URL.Path == "/api/v1/projects/"+o.project+"/secret-variables/"+o.target
	xid := o.dropXID
	if !controlled && o.err == nil {
		o.err = errors.New("unexpected proxy failure")
	}
	o.mu.Unlock()
	if controlled {
		w.Header().Set("X-Request-ID", xid)
	}
	// This is deliberately not a domain Problem and makes no rollback assertion.
	http.Error(w, "Owned backend response unavailable", http.StatusBadGateway)
}

func (o *secretOwnerWebObservations) capture(response *http.Response) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	r := response.Request
	prefix := "/api/v1/projects/" + o.project + "/secret-variables"
	if o.project == "" || !(r.URL.Path == prefix || strings.HasPrefix(r.URL.Path, prefix+"/")) {
		return nil
	}
	fail := func() error { o.err = errors.New("invalid original Secret response"); return o.err }
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "application/json" {
		return fail()
	}
	xid := response.Header.Get("X-Request-ID")
	if _, err := f.ParseID[f.Request](xid); err != nil {
		return fail()
	}
	limit := int64(1 << 20)
	operation := ""
	switch {
	case r.Method == http.MethodGet && r.URL.Path == prefix:
		operation = "list"
		limit = 5 << 20
	case r.Method == http.MethodGet && o.target != "" && r.URL.Path == prefix+"/"+o.target:
		operation = "get"
	case r.Method == http.MethodPost && r.URL.Path == prefix:
		operation = "create"
	case r.Method == http.MethodPatch && o.target != "" && r.URL.Path == prefix+"/"+o.target:
		operation = "update"
	case r.Method == http.MethodDelete && o.target != "" && r.URL.Path == prefix+"/"+o.target:
		operation = "delete"
	case r.Method == http.MethodPost && r.URL.Path == prefix+"/commands/lookup":
		operation = "lookup"
	default:
		return fail()
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, limit+1))
	closeErr := response.Body.Close() // The original backend reader actually closes before any injected loss.
	if readErr != nil || closeErr != nil || int64(len(raw)) > limit || response.ContentLength != int64(len(raw)) || !json.Valid(raw) {
		return fail()
	}
	for _, forbidden := range o.forbidden {
		if bytes.Contains(raw, []byte(forbidden)) {
			return fail()
		}
		for key, values := range response.Header {
			for _, value := range values {
				if strings.Contains(key, forbidden) || strings.Contains(value, forbidden) {
					return fail()
				}
			}
		}
	}
	if operation == "create" || operation == "update" || operation == "delete" {
		var receipt vc.SecretVariableMutation
		if json.Unmarshal(raw, &receipt) != nil || receipt.Validate() != nil {
			return fail()
		}
		fields := receipt.Fields()
		if !fields.Changed || fields.Command != vc.SecretCommandName("project.secret_variable."+operation) || fields.EventID == nil || fields.AuditID == nil {
			return fail()
		}
		if operation == "delete" {
			if fields.Deleted.ProjectID.String() != o.project || fields.Deleted.ID.String() != o.target || fields.Deleted.Version != 3 {
				return fail()
			}
		} else {
			v := fields.Variable.Fields()
			if v.ProjectID.String() != o.project {
				return fail()
			}
			if operation == "create" {
				if o.target != "" || v.Version != 1 || v.Name != "UI_SECRET" || v.Description != "Initial safe metadata" {
					return fail()
				}
				o.target = v.ID.String()
			} else if v.ID.String() != o.target || v.Version != 2 || v.Name != "UI_SECRET_UPDATED" || v.Description != "Updated safe metadata" {
				return fail()
			}
		}
	}
	if operation == "lookup" {
		var lookup vc.SecretVariableCommandLookup
		if json.Unmarshal(raw, &lookup) != nil || lookup.Validate() != nil || lookup.Status() != vc.SecretLookupCommitted {
			return fail()
		}
		receipt := lookup.Receipt()
		if receipt == nil || receipt.Fields().Command != vc.SecretUpdateCommand || !receipt.Fields().Changed || receipt.Fields().Variable.Fields().ID.String() != o.target || receipt.Fields().Variable.Fields().Version != 2 {
			return fail()
		}
		o.lookupSameKey = o.dropped && o.patchKey != "" && r.Header.Get("Idempotency-Key") == o.patchKey
		if !o.lookupSameKey {
			return fail()
		}
	}
	if operation == "update" {
		if o.dropped || o.counts["update"] != 0 {
			return fail()
		}
		o.patchKey = r.Header.Get("Idempotency-Key")
		if f.IdempotencyKey(o.patchKey).Validate() != nil {
			return fail()
		}
	}
	o.counts[operation]++
	if (operation == "create" || operation == "update" || operation == "delete" || operation == "lookup") && o.counts[operation] != 1 {
		return fail()
	}
	o.count++
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	bodyFile := "body-" + digest + ".json"
	if os.WriteFile(filepath.Join(o.evidence, bodyFile), raw, 0600) != nil {
		return fail()
	}
	record := map[string]any{"source_run": o.run, "input_hash": o.inputHash, "sequence": o.count, "operation": operation, "method": r.Method, "path": r.URL.Path, "query": r.URL.RawQuery, "request_id": xid, "status": response.StatusCode, "wire_status": response.StatusCode, "content_length": len(raw), "body_file": bodyFile, "body_sha256": digest, "original_read_eof": true, "original_close": true}
	if operation == "update" {
		record["wire_status"] = 502
	}
	encoded, err := json.Marshal(record)
	if err != nil || os.WriteFile(filepath.Join(o.evidence, fmt.Sprintf("response-%03d.json", o.count)), encoded, 0600) != nil {
		return fail()
	}
	if operation == "update" {
		o.dropped = true
		o.dropXID = xid
		clear(raw)
		return errSecretOwnerResponseLoss
	}
	response.Body = io.NopCloser(bytes.NewReader(raw)) // Same original representation, never a second domain request.
	return nil
}
