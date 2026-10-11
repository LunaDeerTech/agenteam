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
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
)

// A real current Owner changes Scheduler configuration through the default App. The separate Project
// fixture never replaces the default root's deliberately unbound initializer.
// Static hosting is private test infrastructure, not production SPA delivery.
func TestProjectSchedulerWeb(t *testing.T) {
	for _, mode := range []string{"pause-limit-and-resume", "lost-update-confirmation-only-lookup"} {
		t.Run(mode, func(t *testing.T) { projectSchedulerWeb(t, mode) })
	}
}

func projectSchedulerWeb(t *testing.T, mode string) {
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
	dist := os.Getenv("AGENTEAM_PROJECT_SCHEDULER_WEB_DIST")
	runtime := os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	evidence := os.Getenv("AGENTEAM_PROJECT_SCHEDULER_WEB_EVIDENCE")
	inputHash := os.Getenv("AGENTEAM_PROJECT_SCHEDULER_WEB_INPUT_HASH")
	decoded, hashErr := hex.DecodeString(inputHash)
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || hashErr != nil || len(decoded) != sha256.Size {
		t.Fatal("exact owned Skill assets/runtime/evidence/input required")
	}
	info, err := os.Lstat(filepath.Join(dist, "index.html"))
	check("dist", err)
	if !info.Mode().IsRegular() {
		t.Fatal("regular private dist required")
	}
	evidence = filepath.Join(evidence, "TestProjectSchedulerWeb", mode)
	check("fresh evidence", os.MkdirAll(evidence, 0700))
	private, err := os.MkdirTemp(runtime, "psw-")
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
	observations := &projectSchedulerObservations{mode: mode, evidence: evidence, inputHash: inputHash, origin: origin, host: listener.Addr().String(), tails: map[string]*projectSchedulerTail{}}
	deps := dependencies{observeAccount: func(v *account.Service) { core = v }, bind: func(c context.Context, cfg config.Config, store database, owner *resources, d *dependencies) error {
		owned = owner
		if err := bindAccounts(c, cfg, store, owner, d); err != nil {
			return err
		}
		assembly, ok := owner.accounts().(*accountAssembly)
		if !ok {
			return f.NewFault(f.DependencyUnbound, f.NotStarted)
		}
		assembly.mu.Lock()
		assembly.handler = observations.observeHandler(assembly.handler)
		assembly.mu.Unlock()
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
	owner := newHuman("scheduler-owner@example.com", "scheduler-owner")
	other := newHuman("scheduler-other@example.com", "scheduler-other")
	actor := owner.actor
	meta := func() f.CommandMeta {
		return f.CommandMeta{RequestID: guardID[f.Request](t), IdempotencyKey: f.IdempotencyKey(guardID[struct{}](t).String())}
	}
	blockedID := guardID[id.Project](t)
	blocked, err := projects.service.CreateProject(ctx, actor, meta(), pc.CreateProjectRequest{ProjectID: blockedID, Name: "project-scheduler", Description: "Default initializer stays unbound"})
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.DependencyUnbound || fault.CommitState != f.NotCommitted || blocked.State != "" || blocked.Project != nil || blocked.Operation != nil {
		t.Fatal("default Project creation did not remain unbound/not_committed")
	}
	conn := db.Connect(t)
	var facts int
	check("default unbound zero facts", conn.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_project.projects WHERE id=$1 OR (owner_user_id=$2 AND normalized_name='project-scheduler'))+
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
	created, createErr := fixture.CreateProject(ctx, actor, meta(), pc.CreateProjectRequest{ProjectID: projectID, Name: "project-scheduler", Description: "Existing Project for Scheduler configuration"})
	drainErr := fixtureOwner.Drain(ctx)
	check("test-only real Project", createErr)
	check("test-only actual Drain", drainErr)
	if !fixtureOwner.Joined() || created.Validate() != nil || created.State != pc.CreationReady || created.Project == nil || created.Project.ID != projectID {
		t.Fatal("isolated real Project did not confirm and join")
	}

	observations.conn, observations.actor, observations.project = conn, actor, projectID
	schedulerPath := "/api/v1/projects/" + projectID.String() + "/scheduler"
	session := setup.call(owner.client, "GET", "/api/v1/session", nil, "", false, 200)
	seedKey := f.IdempotencyKey(guardID[struct{}](t).String())
	seedInput := projectSchedulerInput{Expected: 1, Enabled: true}
	seedRaw, err := json.Marshal(seedInput)
	check("seed input", err)
	request, err := http.NewRequestWithContext(ctx, http.MethodPatch, origin+schedulerPath, bytes.NewReader(seedRaw))
	check("seed request", err)
	request.Header.Set("Origin", origin)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", knowledgeWebString(t, session, "csrf_token"))
	request.Header.Set("Idempotency-Key", string(seedKey))
	response, err := owner.client.Do(request)
	check("formal Scheduler enable seed", err)
	seedReply, readErr := io.ReadAll(io.LimitReader(response.Body, 4097))
	closeErr := response.Body.Close()
	check("seed EOF", readErr)
	check("seed Close", closeErr)
	if response.StatusCode != 200 || len(seedReply) > 4096 || response.ContentLength != int64(len(seedReply)) || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("formal Scheduler seed failed")
	}
	seedID, err := f.ParseID[f.Request](response.Header.Get("X-Request-ID"))
	check("seed original request ID", err)
	seedIntent := projectSchedulerIntent{kind: "update", key: seedKey, rid: seedID, input: seedInput, bodyDigest: sha256.Sum256(seedRaw), keyDigest: sha256.Sum256([]byte(seedKey))}
	seedResult, err := observations.commandFacts(ctx, seedIntent, seedReply, []string{"scheduler_enabled"})
	check("seed original command/Audit/Outbox", err)
	observations.current = projectSchedulerCurrent{ProjectID: projectID, Version: 2, Enabled: true}
	initial := setup.call(owner.client, "GET", schedulerPath, nil, "", false, 200)
	initialRaw, err := json.Marshal(initial)
	check("seed current JSON", err)
	check("seed current GET", observations.currentFacts(ctx, initialRaw))
	for _, path := range []string{schedulerPath, "/api/v1/projects/" + projectID.String() + "/commands/lookup"} {
		method, body, keyed := "GET", any(nil), false
		if strings.HasSuffix(path, "/lookup") {
			method, body, keyed = "POST", map[string]string{"command": "update"}, true
		}
		problem := setup.call(other.client, method, path, body, knowledgeWebString(t, setup.call(other.client, "GET", "/api/v1/session", nil, "", false, 200), "csrf_token"), keyed, 404)
		if problem["code"] != "NOT_FOUND" {
			t.Fatal("other Human read did not retain current Owner gate")
		}
	}
	immutable := func() string {
		var snapshot string
		check("immutable Scheduler boundary facts", conn.QueryRow(ctx, `SELECT jsonb_build_object(
   'project',(SELECT to_jsonb(p)-ARRAY['version','updated_at','scheduler_enabled','scheduler_max_concurrency'] FROM agenteam_project.projects p WHERE id=$1::text::uuid),
   'work',(SELECT count(*) FROM agenteam_work.tasks WHERE project_id=$1::text::uuid),
   'sprints',(SELECT count(*) FROM agenteam_work.sprints WHERE project_id=$1::text::uuid),
   'dispatches',(SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text),
   'executions',(SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text))::text`, projectID.String()).Scan(&snapshot))
		return snapshot
	}
	before := immutable()
	observations.mu.Lock()
	observations.active = true
	observations.mu.Unlock()
	material := map[string]any{"mode": mode, "cookie": owner.cookie, "project_id": projectID.String(), "route": "/" + owner.username + "/project-scheduler/settings/scheduler", "version": "2", "scheduler_enabled": true, "scheduler_max_concurrency": nil, "input_hash": inputHash}
	raw, err := json.Marshal(material)
	check("private Scheduler material", err)
	check("private Scheduler material file", os.WriteFile(filepath.Join(private, "project-scheduler-material.json"), raw, 0600))
	clear(raw)
	result := projectSchedulerBrowser(t, ctx, private, origin, dist, evidence, inputHash, mode)
	observations.mu.Lock()
	observeErr, count, updates, lookups, cuts, reads := observations.err, observations.count, len(observations.intents), observations.lookups, observations.cuts, observations.reads
	intents := append([]projectSchedulerIntent(nil), observations.intents...)
	receipts := append([][]byte(nil), observations.receipts...)
	finalSnapshot := observations.after
	tails := make([]*projectSchedulerTail, 0, len(observations.tails))
	for _, tail := range observations.tails {
		tails = append(tails, tail)
	}
	observations.mu.Unlock()
	wantUpdates, wantLookup, wantReads := 2, 0, 3
	if mode == "lost-update-confirmation-only-lookup" {
		wantUpdates, wantLookup, wantReads = 1, 1, 2
	}
	if observeErr != nil || updates != wantUpdates || lookups != wantLookup || cuts != wantLookup || reads != wantReads || len(receipts) != updates || count != len(tails) || result["case"] != mode || result["test_status"] != "passed" || result["all_observed"] != true || result["original_requests"] != float64(count) || result["updates"] != float64(wantUpdates) || result["lookups"] != float64(wantLookup) || result["declared_receipt_cut"] != float64(wantLookup) || result["schedulerReads"] != float64(wantReads) {
		t.Fatal("Scheduler browser original request/receipt proofs incomplete")
	}
	for _, tail := range tails {
		select {
		case <-tail.done:
		default:
			t.Fatal("original Scheduler App handler still running")
		}
		if !tail.returned {
			t.Fatal("original Scheduler App handler did not return")
		}
	}
	for index, in := range intents {
		_, err := observations.commandFacts(ctx, in, receipts[index], []string{"scheduler_enabled", "scheduler_max_concurrency"})
		check("historical Scheduler command facts", err)
	}
	if seedResult.Project == nil || seedResult.Project.Version != 2 {
		t.Fatal("seed lost formal historical Project result")
	}
	after, err := observations.snapshot(ctx)
	check("original current read snapshot", err)
	if finalSnapshot == "" || after != finalSnapshot || immutable() != before {
		t.Fatal("Scheduler reads/recovery changed original business facts")
	}
	var commands, audits, events, runtimeFacts int64
	check("exact Scheduler consequences", conn.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_project.commands WHERE project_id=$1::text::uuid AND command_name='update'),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1::text::uuid AND action='project.update'),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND event_type='project.updated'),
 (SELECT count(*) FROM agenteam_work.tasks WHERE project_id=$1::text::uuid)+(SELECT count(*) FROM agenteam_work.sprints WHERE project_id=$1::text::uuid)+(SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text)+(SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text)`, projectID.String()).Scan(&commands, &audits, &events, &runtimeFacts))
	if commands != int64(1+wantUpdates) || audits != commands || events != commands || runtimeFacts != 0 {
		t.Fatal("Scheduler configuration lost exact consequences or started runtime facts")
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
	t.Log("Project Scheduler browser chain: original Node Wait, proxy/root join, domain retirement and guard facts complete; production initializer remains unbound")
}

type projectSchedulerInput struct {
	Expected f.Version `json:"expected_version"`
	Enabled  bool      `json:"scheduler_enabled"`
	Limit    *string   `json:"scheduler_max_concurrency"`
}
type projectSchedulerCurrent struct {
	ProjectID pc.ProjectID `json:"project_id"`
	Version   f.Version    `json:"version"`
	Enabled   bool         `json:"scheduler_enabled"`
	Limit     *string      `json:"scheduler_max_concurrency"`
}
type projectSchedulerIntent struct {
	kind                  string
	key                   f.IdempotencyKey
	rid                   f.ID[f.Request]
	input                 projectSchedulerInput
	bodyDigest, keyDigest [sha256.Size]byte
}
type projectSchedulerTail struct {
	done        chan struct{}
	returned    bool
	method, uri string
	intent      *projectSchedulerIntent
}
type projectSchedulerObservations struct {
	mu, dbMu                                sync.Mutex
	mode, evidence, inputHash, origin, host string
	active                                  bool
	err                                     error
	count, reads, lookups, cuts             int
	project                                 pc.ProjectID
	actor                                   id.Actor
	conn                                    *pgx.Conn
	current                                 projectSchedulerCurrent
	intents                                 []projectSchedulerIntent
	receipts                                [][]byte
	first                                   pc.CommandResult
	after                                   string
	tails                                   map[string]*projectSchedulerTail
}
type projectSchedulerDrop struct {
	header http.Header
	length int
	record map[string]any
}

func (*projectSchedulerDrop) Error() string { return "declared Scheduler receipt cut" }

func (o *projectSchedulerObservations) fail() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.err == nil {
		o.err = errors.New("original Scheduler browser observation failed")
	}
	return o.err
}
func (o *projectSchedulerObservations) observeHandler(next http.Handler) http.Handler {
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
		tail := &projectSchedulerTail{done: make(chan struct{}), method: r.Method, uri: r.URL.RequestURI()}
		if r.Method != http.MethodGet {
			raw, readErr := io.ReadAll(io.LimitReader(r.Body, 4097))
			closeErr := r.Body.Close()
			key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
			if readErr != nil || closeErr != nil || len(raw) > 4096 || r.ContentLength != int64(len(raw)) || key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 || r.Header.Get("Origin") != o.origin || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("X-CSRF-Token") == "" || r.Header.Get("Cookie") == "" || r.URL.RawQuery != "" || r.URL.ForceQuery {
				_ = o.fail()
				http.Error(w, "original Scheduler body", 502)
				return
			}
			in := projectSchedulerIntent{key: key, rid: rid, bodyDigest: sha256.Sum256(raw), keyDigest: sha256.Sum256([]byte(key))}
			o.mu.Lock()
			valid := true
			prefix := "/api/v1/projects/" + o.project.String()
			if r.Method == http.MethodPatch && r.URL.Path == prefix+"/scheduler" {
				var fields map[string]json.RawMessage
				valid = skillWebDecode(raw, &in.input) == nil && json.Unmarshal(raw, &fields) == nil && len(fields) == 3 && in.input.Expected == o.current.Version && len(o.intents) == len(o.receipts)
				index := len(o.intents)
				if index == 0 {
					valid = valid && in.input.Expected == 2 && !in.input.Enabled && in.input.Limit != nil && *in.input.Limit == "9007199254740993"
				} else if index == 1 {
					valid = valid && o.mode == "pause-limit-and-resume" && in.input.Expected == 3 && in.input.Enabled && in.input.Limit == nil
				} else {
					valid = false
				}
				for _, old := range o.intents {
					if old.key == key {
						valid = false
					}
				}
				in.kind = "update"
				if valid {
					o.intents = append(o.intents, in)
				}
			} else if r.Method == http.MethodPost && r.URL.Path == prefix+"/commands/lookup" {
				var body struct {
					Command string `json:"command"`
				}
				valid = skillWebDecode(raw, &body) == nil && body.Command == "update" && o.mode == "lost-update-confirmation-only-lookup" && len(o.intents) == 1 && len(o.receipts) == 1 && o.cuts == 1 && o.lookups == 0 && key == o.intents[0].key
				in.kind = "lookup"
				if valid {
					o.lookups++
				}
			} else {
				valid = false
			}
			o.mu.Unlock()
			if !valid {
				_ = o.fail()
				http.Error(w, "closed Scheduler intent", 502)
				return
			}
			tail.intent = &in
			r.Body = io.NopCloser(bytes.NewReader(raw))
		}
		o.mu.Lock()
		if o.tails[rid.String()] != nil || len(o.tails) >= 96 {
			o.mu.Unlock()
			_ = o.fail()
			http.Error(w, "duplicate Scheduler request", 502)
			return
		}
		o.tails[rid.String()] = tail
		o.mu.Unlock()
		defer close(tail.done)
		next.ServeHTTP(w, r)
		tail.returned = true
	})
}

func (o *projectSchedulerObservations) capture(response *http.Response) error {
	o.mu.Lock()
	active := o.active
	o.mu.Unlock()
	if !active {
		return nil
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 2<<20 || response.ContentLength != int64(len(raw)) || response.StatusCode != 200 || !json.Valid(raw) || response.Header.Get("Cache-Control") != "no-store" {
		return o.fail()
	}
	rid, err := f.ParseID[f.Request](response.Header.Get("X-Request-ID"))
	if err != nil || rid.String() != response.Header.Get("X-Request-ID") || len(response.Header.Values("X-Request-ID")) != 1 {
		return o.fail()
	}
	o.mu.Lock()
	tail := o.tails[rid.String()]
	o.mu.Unlock()
	if tail == nil || tail.method != response.Request.Method || tail.uri != response.Request.URL.RequestURI() {
		return o.fail()
	}
	select {
	case <-tail.done:
	case <-response.Request.Context().Done():
		return o.fail()
	}
	if !tail.returned {
		return o.fail()
	}
	lost := false
	if tail.intent != nil {
		in := *tail.intent
		if in.kind == "update" {
			result, err := o.commandFacts(response.Request.Context(), in, raw, []string{"scheduler_enabled", "scheduler_max_concurrency"})
			if err != nil {
				return o.fail()
			}
			snapshot, err := o.snapshot(response.Request.Context())
			if err != nil {
				return o.fail()
			}
			o.mu.Lock()
			o.current = projectSchedulerCurrent{ProjectID: o.project, Version: result.Project.Version, Enabled: in.input.Enabled, Limit: in.input.Limit}
			o.receipts = append(o.receipts, bytes.Clone(raw))
			o.after = snapshot
			if len(o.receipts) == 1 {
				o.first = result
			}
			lost = o.mode == "lost-update-confirmation-only-lookup"
			o.mu.Unlock()
		} else {
			// This endpoint observes only Project + update + key. Original body
			// identity comes from the captured PATCH and its persisted digest.
			var found struct {
				State  pc.LookupState    `json:"state"`
				Result *pc.CommandResult `json:"result"`
			}
			o.mu.Lock()
			original, before := o.first, o.after
			o.mu.Unlock()
			if skillWebDecode(raw, &found) != nil || found.State != pc.LookupCommitted || found.Result == nil || !reflect.DeepEqual(*found.Result, original) || bytes.Contains(raw, []byte("scheduler_")) {
				return o.fail()
			}
			after, err := o.snapshot(response.Request.Context())
			if err != nil || before != after {
				return o.fail()
			}
		}
	} else if response.Request.URL.Path == "/api/v1/projects/"+o.project.String()+"/scheduler" {
		if response.Request.URL.RawQuery != "" || o.currentFacts(response.Request.Context(), raw) != nil {
			return o.fail()
		}
		o.mu.Lock()
		o.reads++
		o.mu.Unlock()
	}
	record := map[string]any{"input_hash": o.inputHash, "request_id": rid.String(), "method": tail.method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(tail.uri))), "status": response.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true, "request_host_matches": true}
	if tail.intent != nil {
		record["kind"] = tail.intent.kind
		record["request_body_sha256"] = fmt.Sprintf("%x", tail.intent.bodyDigest)
		record["idempotency_key_sha256"] = fmt.Sprintf("%x", tail.intent.keyDigest)
		record["committed_verified"] = true
	}
	if o.writeProof(record) != nil {
		return o.fail()
	}
	o.mu.Lock()
	o.count++
	o.mu.Unlock()
	if lost {
		return &projectSchedulerDrop{response.Header.Clone(), len(raw), record}
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}

func (o *projectSchedulerObservations) writeProof(record map[string]any) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	rid, ok := record["request_id"].(string)
	if !ok {
		return errors.New("proof identity absent")
	}
	return os.WriteFile(filepath.Join(o.evidence, "project-scheduler-"+o.mode+"-response-"+rid+".json"), raw, 0600)
}
func (o *projectSchedulerObservations) proxyError(w http.ResponseWriter, _ *http.Request, err error) {
	var drop *projectSchedulerDrop
	if !errors.As(err, &drop) {
		_ = o.fail()
		http.Error(w, "owned Scheduler proxy failed", 502)
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

func (o *projectSchedulerObservations) currentFacts(ctx context.Context, raw []byte) error {
	var current projectSchedulerCurrent
	var fields map[string]json.RawMessage
	if skillWebDecode(raw, &current) != nil || json.Unmarshal(raw, &fields) != nil || len(fields) != 4 {
		return errors.New("current Scheduler wire invalid")
	}
	o.mu.Lock()
	expected := o.current
	o.mu.Unlock()
	if !reflect.DeepEqual(current, expected) {
		return errors.New("current Scheduler GET differs from last completed mutation")
	}
	o.dbMu.Lock()
	defer o.dbMu.Unlock()
	var version int64
	var enabled bool
	var limit *int64
	err := o.conn.QueryRow(ctx, `SELECT version,scheduler_enabled,scheduler_max_concurrency FROM agenteam_project.projects WHERE id=$1`, o.project.String()).Scan(&version, &enabled, &limit)
	if err != nil {
		return err
	}
	if version != int64(current.Version) || enabled != current.Enabled || (limit == nil) != (current.Limit == nil) || limit != nil && strconv.FormatInt(*limit, 10) != *current.Limit {
		return errors.New("Scheduler GET differs from actual Project row")
	}
	return nil
}

func (o *projectSchedulerObservations) commandFacts(ctx context.Context, in projectSchedulerIntent, raw []byte, changed []string) (pc.CommandResult, error) {
	var ack struct {
		ProjectID pc.ProjectID `json:"project_id"`
		Version   f.Version    `json:"version"`
	}
	var fields map[string]json.RawMessage
	if skillWebDecode(raw, &ack) != nil || json.Unmarshal(raw, &fields) != nil || len(fields) != 2 || ack.ProjectID != o.project || ack.Version != in.input.Expected+1 {
		return pc.CommandResult{}, errors.New("Scheduler acknowledgement changed original Project/version")
	}
	config := pc.ProjectSchedulerConfig{Enabled: in.input.Enabled}
	if in.input.Limit != nil {
		limit, err := strconv.ParseInt(*in.input.Limit, 10, 64)
		if err != nil {
			return pc.CommandResult{}, err
		}
		config.MaxConcurrency = &limit
	}
	meta := f.CommandMeta{RequestID: in.rid, IdempotencyKey: in.key, ExpectedVersion: &in.input.Expected}
	digest, err := pc.UpdateDigest(o.actor, meta, o.project, pc.UpdateProjectRequest{Scheduler: &config})
	if err != nil {
		return pc.CommandResult{}, err
	}
	o.dbMu.Lock()
	defer o.dbMu.Unlock()
	var storedRaw []byte
	var actualDigest, user string
	err = o.conn.QueryRow(ctx, `SELECT safe_result,semantic_digest,actor_user_id::text FROM agenteam_project.commands WHERE project_id=$1 AND command_name='update' AND key=$2 AND state='completed' AND committed_at IS NOT NULL`, o.project.String(), string(in.key)).Scan(&storedRaw, &actualDigest, &user)
	if err != nil {
		return pc.CommandResult{}, err
	}
	var stored pc.CommandResult
	if json.Unmarshal(storedRaw, &stored) != nil || stored.Validate() != nil || stored.Command != pc.UpdateCommand || stored.Project == nil || stored.Project.ID != o.project || stored.Project.Version != ack.Version || stored.Project.OwnerUserID.String() != o.actor.Details().UserID || actualDigest != digest.String() || user != o.actor.Details().UserID {
		return pc.CommandResult{}, errors.New("Scheduler command differs from original Owner/full body/key/version")
	}
	metadata, _ := json.Marshal(map[string]any{"project_id": o.project, "initiator_id": o.actor.Details().UserID, "project_version": ack.Version, "changed_fields": changed})
	payload, _ := json.Marshal(map[string]any{"changed_fields": changed})
	configRaw, _ := json.Marshal(config)
	changedRaw, _ := json.Marshal(changed)
	var matches, audits, events int64
	err = o.conn.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_audit.audit_records a JOIN agenteam_outbox.events e ON e.id=c.event_id
  WHERE a.project_id=c.project_id AND a.cause_ref=c.audit_cause AND a.producer='project' AND a.action='project.update'
  AND a.scope='project' AND a.outcome='success' AND a.actor_kind='human' AND a.user_id=$3::text::uuid AND a.session_id=$4::text::uuid
  AND a.resource_kind='project' AND a.resource_id=c.project_id AND a.ordinal=0 AND a.metadata=$5::jsonb
  AND e.project_id=c.project_id AND e.scope='project' AND e.producer='project' AND e.event_type='project.updated'
  AND e.aggregate_type='project' AND e.aggregate_id=c.project_id AND e.aggregate_version=$6 AND e.schema_version=1
  AND convert_from(e.payload,'UTF8')::jsonb=$7::jsonb AND c.event_ids=jsonb_build_array(e.id::text)
  AND c.plan->>'expected_version'=$8 AND c.plan->'scheduler'=$9::jsonb AND c.plan->'changed'=$10::jsonb
  AND c.plan->'project'=c.safe_result->'project' AND c.plan->'event_header'=e.header AND c.plan->'event_payload'=convert_from(e.payload,'UTF8')::jsonb),
 (SELECT count(*) FROM agenteam_audit.audit_records a WHERE a.project_id=c.project_id AND a.cause_ref=c.audit_cause),
 (SELECT count(*) FROM agenteam_outbox.events e WHERE e.id=c.event_id)
 FROM agenteam_project.commands c WHERE c.project_id=$1::text::uuid AND c.command_name='update' AND c.key=$2`, o.project.String(), string(in.key), o.actor.Details().UserID, o.actor.Details().SessionID, string(metadata), int64(ack.Version), string(payload), in.input.Expected.String(), string(configRaw), string(changedRaw)).Scan(&matches, &audits, &events)
	if err != nil {
		return pc.CommandResult{}, err
	}
	if matches != 1 || audits != 1 || events != 1 {
		return pc.CommandResult{}, errors.New("Scheduler original plan/Audit/Outbox consequences differ")
	}
	return stored, nil
}

func (o *projectSchedulerObservations) snapshot(ctx context.Context) (string, error) {
	o.dbMu.Lock()
	defer o.dbMu.Unlock()
	var snapshot string
	err := o.conn.QueryRow(ctx, `SELECT jsonb_build_object(
 'project',(SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1::text::uuid),
 'commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_project.commands c WHERE project_id=$1::text::uuid),
 'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a WHERE project_id=$1::text::uuid),
 'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_outbox.events e WHERE project_id=$1::text::uuid),
 'activity',(SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$2::uuid))::text`, o.project.String(), o.actor.Details().SessionID).Scan(&snapshot)
	return snapshot, err
}

func projectSchedulerBrowser(t *testing.T, ctx context.Context, private, origin, dist, evidence, inputHash, mode string) map[string]any {
	t.Helper()
	harness, err := filepath.Abs("../../../tests/account-captcha-web")
	if err != nil {
		t.Fatal("browser harness path unavailable")
	}
	cmd := exec.CommandContext(ctx, "node", filepath.Join(harness, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(harness, "project-scheduler.config.js"))
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
	cmd.Env = append(cmd.Env, "TMPDIR="+private, "AGENTEAM_AUTH_WEB_ORIGIN="+origin, "AGENTEAM_AUTH_WEB_PRIVATE="+private, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_PROJECT_SCHEDULER_WEB_DIST="+dist, "AGENTEAM_PROJECT_SCHEDULER_WEB_EVIDENCE="+evidence, "AGENTEAM_PROJECT_SCHEDULER_WEB_INPUT_HASH="+inputHash, "AGENTEAM_PROJECT_SCHEDULER_WEB_CASE="+mode, "PLAYWRIGHT_NO_COPY_PROMPT=1")
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
	t.Logf("Project Scheduler Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	// Never print the browser's credential-bearing call log. The spec writes
	// fixed-stage safe failure diagnostics; the original child status is final.
	if err != nil || !regexp.MustCompile(`(?m)\b1 passed\b`).Match(output.Bytes()) {
		t.Fatal("Skill locked browser did not actually pass its one case")
	}
	raw, err := os.ReadFile(filepath.Join(evidence, "project-scheduler-"+mode+".json"))
	if err != nil {
		t.Fatal("Skill browser result missing")
	}
	if len(raw) > 256<<10 {
		t.Fatal("Skill browser result too large")
	}
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil || result["input_hash"] != inputHash {
		t.Fatal("Skill browser result not bound to frozen inputs")
	}
	return result
}
