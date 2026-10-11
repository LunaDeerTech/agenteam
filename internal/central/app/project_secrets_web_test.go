//go:build integration

package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
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

// The ordinary Owner reads only safe Secret metadata through the default App.
// Secret values are seeded through its formal service and remain in Go only.
// Static hosting is private test infrastructure, not production SPA delivery.
func TestProjectSecretsWeb(t *testing.T) {
	t.Run(projectSecretsMode, projectSecretsWeb)
}

func projectSecretsWeb(t *testing.T) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(func() {
		cancel()
		if time.Since(started) > 120*time.Second {
			t.Error("Project Secrets browser exceeded its original 120s including cleanup")
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
				t.Fatalf("Project Secrets browser %s: %s/%s", stage, fault.Code.Safe(), fault.CommitState.Safe())
			}
			var pathErr *os.PathError
			var errno syscall.Errno
			if errors.As(err, &pathErr) && errors.As(pathErr.Err, &errno) {
				t.Fatalf("Project Secrets browser %s failed: op=%q errno=%d", stage, pathErr.Op, errno)
			}
			t.Fatalf("Project Secrets browser %s failed", stage)
		}
	}
	dist := os.Getenv("AGENTEAM_PROJECT_SECRETS_WEB_DIST")
	runtime := os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	evidence := os.Getenv("AGENTEAM_PROJECT_SECRETS_WEB_EVIDENCE")
	inputHash := os.Getenv("AGENTEAM_PROJECT_SECRETS_WEB_INPUT_HASH")
	decoded, hashErr := hex.DecodeString(inputHash)
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || hashErr != nil || len(decoded) != sha256.Size {
		t.Fatal("exact owned Project Secrets assets/runtime/evidence/input required")
	}
	info, err := os.Lstat(filepath.Join(dist, "index.html"))
	check("dist", err)
	if !info.Mode().IsRegular() {
		t.Fatal("regular private dist required")
	}
	evidence = filepath.Join(evidence, t.Name())
	check("evidence parent", os.MkdirAll(filepath.Dir(evidence), 0700))
	check("fresh evidence", os.Mkdir(evidence, 0700))
	private, err := os.MkdirTemp(runtime, "psw-")
	check("private browser directory", err)
	t.Cleanup(func() {
		check("private removal", os.RemoveAll(private))
		if _, err := os.Lstat(private); !errors.Is(err, os.ErrNotExist) {
			t.Error("private Project Secrets directory remains")
		}
	})
	db := pgfixture.NewDatabase(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	check("private listener", err)
	t.Cleanup(func() { _ = listener.Close() })
	origin := "http://" + listener.Addr().String()
	cfg := guardConfiguration(t, append(guardEnvironment(t, db), "AGENTEAM_CENTRAL_PUBLIC_ORIGIN="+origin, "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s"))
	observations := &projectSecretsObservations{knowledgeTextObservations: &knowledgeTextObservations{evidence: evidence, inputHash: inputHash, origin: origin, tails: map[string]*knowledgeTextTail{}}}
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
			t.Error("Project Secrets root actual join exceeded cleanup observation")
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
	blocked, err := projects.service.CreateProject(ctx, actor, meta(), pc.CreateProjectRequest{ProjectID: blockedID, Name: "project-secrets", Description: "Default initializer stays unbound"})
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.DependencyUnbound || fault.CommitState != f.NotCommitted || blocked.State != "" || blocked.Project != nil || blocked.Operation != nil {
		t.Fatal("default Project creation did not remain unbound/not_committed")
	}
	conn := db.Connect(t)
	var facts int
	check("default unbound zero facts", conn.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_project.projects WHERE id=$1 OR (owner_user_id=$2 AND normalized_name='project-secrets'))+
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
	created, createErr := fixture.CreateProject(ctx, actor, meta(), pc.CreateProjectRequest{ProjectID: projectID, Name: "project-secrets", Description: "Existing Secret metadata for the bounded UI read"})
	drainErr := fixtureOwner.Drain(ctx)
	check("test-only real Project", createErr)
	check("test-only actual Drain", drainErr)
	if !fixtureOwner.Joined() || created.Validate() != nil || created.State != pc.CreationReady || created.Project == nil || created.Project.ID != projectID {
		t.Fatal("isolated real Project did not confirm and join")
	}

	// These values and all leak-check derivatives stay in this Go process. The
	// original SecretService owns preparation, current Owner checks and commit.
	items := make([]map[string]any, 0, 51)
	for index := 1; index <= 51; index++ {
		entropy := make([]byte, 32)
		_, err := rand.Read(entropy)
		check("private seed randomness", err)
		value := []byte(base64.RawURLEncoding.EncodeToString(entropy))
		clear(entropy)
		digest := sha256.Sum256(value)
		forbidden = append(forbidden, append([]byte(nil), value...),
			[]byte(base64.StdEncoding.EncodeToString(value)), []byte(base64.RawStdEncoding.EncodeToString(value)),
			[]byte(hex.EncodeToString(value)), []byte(hex.EncodeToString(digest[:])),
			append([]byte(nil), value[:16]...), append([]byte(nil), value[len(value)-16:]...))
		clear(digest[:])
		secretMaterial, err := sc.NewSecretMaterial(value)
		clear(value)
		check("private seed material", err)
		variableID := guardID[id.ProjectVariable](t)
		name := fmt.Sprintf("SECRET_%03d", index)
		description := fmt.Sprintf("安全元数据 %03d。\n仅查看名称与说明，中文🙂。", index)
		request, err := vc.NewSecretVariableCreate(vc.SecretVariableCreateFields{ID: variableID, Name: name, Description: description, Value: secretMaterial})
		secretMaterial.Destroy()
		check("formal Secret create intent", err)
		receipt, createErr := variables.service.CreateSecretVariable(ctx, actor, meta(), projectID, request)
		request.Destroy()
		check("formal Secret create", createErr)
		fields := receipt.Fields()
		if receipt.Validate() != nil || fields.Command != vc.SecretCreateCommand || !fields.Changed || fields.EventID == nil || fields.AuditID == nil {
			t.Fatal("formal Secret seed did not confirm its original mutation facts")
		}
		v := fields.Variable.Fields()
		if v.ID != variableID || v.ProjectID != projectID || v.Type != "secret" || v.Name != name || v.Description != description || v.Version != 1 || !v.CreatedAt.Time().Equal(v.UpdatedAt.Time()) {
			t.Fatal("formal Secret seed metadata differs from the requested safe fields")
		}
		raw, err := json.Marshal(fields.Variable)
		check("safe seed metadata", err)
		var safe map[string]any
		check("safe seed projection", json.Unmarshal(raw, &safe))
		if len(safe) != 8 || projectSecretsLeak(raw, forbidden) {
			t.Fatal("formal Secret seed metadata escaped its eight-field boundary")
		}
		items = append(items, safe)
		clear(raw)
	}
	var rows, commands, history, audit, outbox, protected, joined int
	check("formal Secret seed durable facts", conn.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_projectvariable.variables WHERE project_id=$1 AND type='secret' AND value IS NULL AND version=1 AND deleted_at IS NULL),
 (SELECT count(*) FROM agenteam_projectvariable.secret_commands WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_projectvariable.secret_history WHERE project_id=$1 AND kind='created' AND version=1),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='project.secret_variable.create'),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND event_type='project.secret_variable_changed'),
 (SELECT count(*) FROM agenteam_secret.project_variable_receipts WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_projectvariable.secret_commands c
 JOIN agenteam_projectvariable.variables v ON v.project_id=c.project_id AND v.id=c.target_id
 JOIN agenteam_projectvariable.secret_history h ON h.project_id=c.project_id AND h.operation_id=c.id AND h.event_id=c.event_id
 JOIN agenteam_secret.project_variable_receipts r ON r.id=c.d04_receipt_id AND r.project_id=c.project_id AND r.variable_id=c.target_id AND r.credential_id=v.credential_id
 JOIN agenteam_audit.audit_records a ON a.id=c.audit_id AND a.project_id=c.project_id AND a.resource_id=c.target_id AND a.action='project.secret_variable.create'
 JOIN agenteam_outbox.events e ON e.id=c.event_id AND e.project_id=c.project_id AND e.event_type='project.secret_variable_changed'
 WHERE c.project_id=$1 AND c.actor_user_id=$2 AND c.command_name='project.secret_variable.create'
 AND c.expected_version IS NULL AND c.name_present AND c.description_present AND c.value_present
 AND c.secret_effect='create' AND c.credential_version=1 AND NOT c.secret_deleted
 AND c.credential_id=v.credential_id AND c.committed_at IS NOT NULL)`, projectID.String(), actor.Details().UserID).Scan(&rows, &commands, &history, &audit, &outbox, &protected, &joined))
	if rows != 51 || commands != 51 || history != 51 || audit != 51 || outbox != 51 || protected != 51 || joined != 51 {
		t.Fatal("formal Secret seeds did not retain the complete D10/D04/Audit/Outbox chain")
	}
	// Opaque storage identities are also forbidden in Node inputs, HTTP output
	// and evidence. They are never printed or persisted by this test.
	var privateIDs []string
	check("private storage identities", conn.QueryRow(ctx, `SELECT array_agg(value ORDER BY value) FROM (
 SELECT credential_id::text AS value FROM agenteam_projectvariable.variables WHERE project_id=$1 AND type='secret'
 UNION SELECT id::text FROM agenteam_secret.project_variable_receipts WHERE project_id=$1
 UNION SELECT payload_id::text FROM agenteam_secret.secret_payloads WHERE project_id=$1
 ) q`, projectID.String()).Scan(&privateIDs))
	for _, value := range privateIDs {
		forbidden = append(forbidden, []byte(value))
	}
	clear(privateIDs)
	persisted := func() string {
		var value string
		check("read-only persisted facts", conn.QueryRow(ctx, `SELECT jsonb_build_object(
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
 'outbox',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_outbox.events e WHERE project_id=$1))::text`, projectID.String()).Scan(&value))
		return value // Go-only equality; no raw snapshot or snapshot digest leaves Go.
	}
	before := persisted()
	observations.mu.Lock()
	observations.project, observations.host = projectID.String(), listener.Addr().String()
	observations.items, observations.forbidden = items, forbidden
	observations.active = true
	observations.mu.Unlock()
	payload := map[string]any{"mode": projectSecretsMode, "origin": origin, "cookie": cookie, "project_id": projectID.String(), "route": "/" + username + "/project-secrets/settings/secrets", "items": items, "selected": items[50], "input_hash": inputHash}
	raw, err := json.Marshal(payload)
	check("private safe metadata material", err)
	if projectSecretsLeak(raw, forbidden) {
		t.Fatal("private browser material contains a forbidden Secret derivative")
	}
	check("private metadata material file", os.WriteFile(filepath.Join(private, "project-secrets-material.json"), raw, 0600))
	clear(raw)
	result := projectSecretsBrowser(t, ctx, private, origin, dist, evidence, inputHash, forbidden)
	observations.mu.Lock()
	observeErr, observed, tails := observations.err, observations.count, len(observations.tails)
	lists, gets, pages, details := observations.lists, observations.gets, observations.pages, observations.details
	observations.mu.Unlock()
	if observeErr != nil || observed != tails || lists != 2 || pages != 2 || gets != 2 || details != 2 || result["completed"] != true || result["original_requests"] != float64(observed) || result["all_observed"] != true || result["lookups"] != float64(0) || result["lists"] != float64(2) || result["gets"] != float64(2) {
		t.Fatal("Project Secrets original response/consumer proofs incomplete")
	}
	if persisted() != before {
		t.Fatal("Secret metadata reads changed committed business or private storage facts")
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
	t.Log("Project Secrets browser chain: original Node Wait, proxy/root join, domain retirement and guard facts complete; production initializer remains unbound")
}

const projectSecretsMode = "metadata-pagination-and-detail"

// Only the read observer is reused. Activation follows formal seeding, and the
// browser cannot enter a write, Lookup, material or reference endpoint.
type projectSecretsObservations struct {
	*knowledgeTextObservations
	items                       []map[string]any
	forbidden                   [][]byte
	cursor                      string
	lists, gets, pages, details int
}

func (o *projectSecretsObservations) observeHandler(next http.Handler) http.Handler {
	return o.knowledgeTextObservations.observeHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.mu.Lock()
		active := o.active
		o.mu.Unlock()
		if active && o.observeRead(r) != nil {
			_ = o.fail()
			http.Error(w, "closed Secret metadata read", http.StatusBadGateway)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func (o *projectSecretsObservations) observeRead(r *http.Request) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if r.Method != http.MethodGet || r.ContentLength != 0 || len(r.TransferEncoding) != 0 || r.Header.Get("Idempotency-Key") != "" {
		return errors.New("Secret browser attempted a write/body/key")
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || r.URL.ForceQuery {
		return errors.New("Secret browser query is malformed")
	}
	global := map[string]bool{"/api/v1/auth/bootstrap": true, "/api/v1/session": true, "/api/v1/me": true, "/api/v1/me/preferences": true, "/api/v1/projects": true, "/api/v1/projects/resolve": true}
	if global[r.URL.Path] {
		return nil
	}
	project := "/api/v1/projects/" + o.project
	prefix := project + "/secret-variables"
	switch r.URL.Path {
	case project:
		if len(query) == 0 {
			return nil
		}
	case prefix:
		want := url.Values{"limit": {"50"}}
		if o.lists == 1 && o.pages == 1 && o.cursor != "" {
			want.Set("cursor", o.cursor)
		} else if o.lists != 0 || o.pages != 0 {
			break
		}
		if reflect.DeepEqual(query, want) {
			o.lists++
			return nil
		}
	case prefix + "/" + o.items[50]["id"].(string):
		if len(query) == 0 && o.pages == 2 && o.gets < 2 && o.gets == o.details {
			o.gets++
			return nil
		}
	}
	return errors.New("undeclared Secret metadata endpoint or read order")
}

func (o *projectSecretsObservations) current(r *http.Request, raw []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	prefix := "/api/v1/projects/" + o.project + "/secret-variables"
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return errors.New("original response is not complete JSON")
	}
	switch r.URL.Path {
	case prefix:
		var page f.Page[vc.SecretVariable]
		if skillWebDecode(raw, &page) != nil {
			return errors.New("original Secret page is not a safe typed page")
		}
		items, ok := value["items"].([]any)
		start, length := 0, 50
		if r.URL.Query().Get("cursor") != "" {
			start, length = 50, 1
			if len(value) != 1 || page.NextCursor != "" || o.pages != 1 || r.URL.Query().Get("cursor") != o.cursor {
				return errors.New("original second page has changed its cursor or shape")
			}
		} else {
			if len(value) != 2 || page.NextCursor == "" || len(page.NextCursor) > 8192 || value["next_cursor"] != page.NextCursor || o.pages != 0 {
				return errors.New("original first page omitted its real continuation")
			}
		}
		if !ok || len(items) != length || len(page.Items) != length {
			return errors.New("original Secret page did not preserve the bounded partition")
		}
		for index, item := range items {
			if !reflect.DeepEqual(item, o.items[start+index]) || page.Items[index].Validate() != nil {
				return errors.New("original Secret page metadata differs from formal seed")
			}
		}
		if start == 0 {
			o.cursor = page.NextCursor
		}
		o.pages++
	case prefix + "/" + o.items[50]["id"].(string):
		var variable vc.SecretVariable
		if skillWebDecode(raw, &variable) != nil || variable.Validate() != nil || !reflect.DeepEqual(value, o.items[50]) || o.pages != 2 || o.details >= 2 {
			return errors.New("original independent Secret detail differs from current metadata")
		}
		o.details++
	}
	return nil
}

func (o *projectSecretsObservations) capture(response *http.Response) error {
	o.mu.Lock()
	active := o.active
	o.mu.Unlock()
	if !active {
		return nil
	}
	limit := int64(1 << 20)
	if response.Request.URL.Path == "/api/v1/projects/"+o.project+"/secret-variables" {
		limit = 5 << 20
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	closed := response.Body.Close()
	headers, headerErr := json.Marshal(response.Header)
	if err != nil || closed != nil || headerErr != nil || response.StatusCode != http.StatusOK || int64(len(raw)) > limit || response.ContentLength != int64(len(raw)) || !json.Valid(raw) || response.Header.Get("Cache-Control") != "no-store" || projectSecretsLeak(raw, o.forbidden) || projectSecretsLeak(headers, o.forbidden) {
		clear(raw)
		return o.fail()
	}
	rid, err := f.ParseID[f.Request](response.Header.Get("X-Request-ID"))
	if err != nil || len(response.Header.Values("X-Request-ID")) != 1 {
		return o.fail()
	}
	o.mu.Lock()
	tail := o.tails[rid.String()]
	o.mu.Unlock()
	if tail == nil || tail.intent != nil || tail.method != response.Request.Method || tail.uri != response.Request.URL.RequestURI() {
		return o.fail()
	}
	select {
	case <-tail.done:
	case <-response.Request.Context().Done():
		return o.fail()
	}
	if !tail.returned || o.current(response.Request, raw) != nil {
		return o.fail()
	}
	// These hashes describe only checked public HTTP metadata, never a Secret
	// value, CredentialRef, ciphertext, private digest or database snapshot.
	record := map[string]any{"input_hash": o.inputHash, "request_id": rid.String(), "method": tail.method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(tail.uri))), "status": response.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true, "request_host_matches": true}
	encoded, err := json.Marshal(record)
	if err != nil || projectSecretsLeak(encoded, o.forbidden) || os.WriteFile(filepath.Join(o.evidence, "project-secrets-"+projectSecretsMode+"-response-"+rid.String()+".json"), encoded, 0600) != nil {
		return o.fail()
	}
	o.mu.Lock()
	o.count++
	o.mu.Unlock()
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}

func projectSecretsLeak(raw []byte, forbidden [][]byte) bool {
	for _, value := range forbidden {
		if len(value) == 0 || bytes.Contains(raw, value) {
			return true
		}
	}
	return false
}

func projectSecretsScanEvidence(t *testing.T, root string, forbidden [][]byte) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errors.New("owned evidence walk failed")
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 16<<20 {
			return errors.New("owned evidence is not a bounded regular file")
		}
		raw, err := os.ReadFile(path)
		defer clear(raw)
		if err != nil || projectSecretsLeak(raw, forbidden) {
			return errors.New("owned evidence contains forbidden Secret material or cannot be checked")
		}
		return nil
	})
	if err != nil {
		t.Fatal("Secret evidence failed its Go-only material boundary")
	}
}

func projectSecretsBrowser(t *testing.T, ctx context.Context, private, origin, dist, evidence, inputHash string, forbidden [][]byte) map[string]any {
	t.Helper()
	harness, err := filepath.Abs("../../../tests/account-captcha-web")
	if err != nil {
		t.Fatal("browser harness path unavailable")
	}
	cmd := exec.CommandContext(ctx, "node", filepath.Join(harness, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(harness, "project-secrets.config.js"))
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
	cmd.Env = append(cmd.Env, "TMPDIR="+private, "AGENTEAM_AUTH_WEB_ORIGIN="+origin, "AGENTEAM_AUTH_WEB_PRIVATE="+private, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_PROJECT_SECRETS_WEB_DIST="+dist, "AGENTEAM_PROJECT_SECRETS_WEB_EVIDENCE="+evidence, "AGENTEAM_PROJECT_SECRETS_WEB_INPUT_HASH="+inputHash, "AGENTEAM_PROJECT_SECRETS_WEB_CASE=metadata-pagination-and-detail", "PLAYWRIGHT_NO_COPY_PROMPT=1")
	var output bytes.Buffer
	defer func() { clear(output.Bytes()) }()
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal("locked ProjectSecrets Node could not start")
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
	t.Logf("ProjectSecrets Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	// Never print the browser's credential-bearing call log. The spec writes
	// fixed-stage safe failure diagnostics; the original child status is final.
	if projectSecretsLeak(output.Bytes(), forbidden) {
		t.Fatal("Secret material escaped into original Node output")
	}
	if err != nil || !regexp.MustCompile(`(?m)\b1 passed\b`).Match(output.Bytes()) {
		t.Fatal("ProjectSecrets locked browser did not actually pass its one case")
	}
	raw, err := os.ReadFile(filepath.Join(private, "project-secrets-result.json"))
	if err != nil {
		t.Fatal("ProjectSecrets browser result missing")
	}
	defer clear(raw)
	if len(raw) > 16<<10 || projectSecretsLeak(raw, forbidden) {
		t.Fatal("ProjectSecrets browser result too large")
	}
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil || result["input_hash"] != inputHash {
		t.Fatal("ProjectSecrets browser result not bound to frozen inputs")
	}
	return result
}
