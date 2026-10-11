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
	"sort"
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
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
)

// The ordinary Owner previews and deletes an exact subtree through the default
// App, then checks current reads. The lost case recovers only by original intent.
// Static hosting is private test infrastructure, not production SPA delivery.
func TestKnowledgeDeleteWeb(t *testing.T) {
	for _, mode := range []string{"delete-normal", "delete-lost"} {
		t.Run(mode, func(t *testing.T) { knowledgeDeleteWeb(t, mode) })
	}
}

func knowledgeDeleteWeb(t *testing.T, mode string) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(func() {
		cancel()
		if time.Since(started) > 120*time.Second {
			t.Error("Knowledge browser exceeded its original 120s including cleanup")
		}
	})
	check := func(stage string, err error) {
		t.Helper()
		if err != nil {
			var fault *f.Fault
			if errors.As(err, &fault) {
				t.Fatalf("Knowledge browser %s: %s/%s", stage, fault.Code.Safe(), fault.CommitState.Safe())
			}
			t.Fatalf("Knowledge browser %s failed", stage)
		}
	}
	dist := os.Getenv("AGENTEAM_KNOWLEDGE_DELETE_WEB_DIST")
	runtime := os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	evidence := os.Getenv("AGENTEAM_KNOWLEDGE_DELETE_WEB_EVIDENCE")
	inputHash := os.Getenv("AGENTEAM_KNOWLEDGE_DELETE_WEB_INPUT_HASH")
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
	check("evidence parent", os.MkdirAll(filepath.Dir(evidence), 0700))
	check("fresh evidence", os.Mkdir(evidence, 0700))
	private, err := os.MkdirTemp(runtime, "kd-")
	check("private browser directory", err)
	t.Cleanup(func() {
		check("private removal", os.RemoveAll(private))
		if _, err := os.Lstat(private); !errors.Is(err, os.ErrNotExist) {
			t.Error("private Knowledge directory remains")
		}
	})
	db := pgfixture.NewDatabase(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	check("private listener", err)
	t.Cleanup(func() { _ = listener.Close() })
	origin := "http://" + listener.Addr().String()
	cfg := guardConfiguration(t, append(guardEnvironment(t, db), "AGENTEAM_CENTRAL_PUBLIC_ORIGIN="+origin, "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s"))
	observations := &knowledgeDeleteObservations{knowledgeTextObservations: &knowledgeTextObservations{evidence: evidence, inputHash: inputHash, origin: origin, tails: map[string]*knowledgeTextTail{}}}
	observations.reportFailure = func(stage string) { t.Logf("KnowledgeDelete observer first_failure_stage=%s", stage) }
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
			t.Error("Knowledge root actual join exceeded cleanup observation")
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
	email, username := "knowledge-owner@example.com", "knowledge-owner"
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
	password := "Knowledge private " + guardID[struct{}](t).String() + "!"
	setup.call(ownerClient, "POST", "/api/v1/invitations/redeem", map[string]string{"token": inviteURL.Fragment, "username": username, "display_name": "Knowledge Owner", "password": password, "confirmation": password}, anonymousCSRF, true, 201)
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
	blocked, err := projects.service.CreateProject(ctx, actor, meta(), pc.CreateProjectRequest{ProjectID: blockedID, Name: "knowledge-delete", Description: "Default initializer stays unbound"})
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.DependencyUnbound || fault.CommitState != f.NotCommitted || blocked.State != "" || blocked.Project != nil || blocked.Operation != nil {
		t.Fatal("default Project creation did not remain unbound/not_committed")
	}
	conn := db.Connect(t)
	var facts int
	check("default unbound zero facts", conn.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_project.projects WHERE id=$1 OR (owner_user_id=$2 AND normalized_name='knowledge-delete'))+
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
	created, createErr := fixture.CreateProject(ctx, actor, meta(), pc.CreateProjectRequest{ProjectID: projectID, Name: "knowledge-delete", Description: "Existing documents for the bounded UI read"})
	drainErr := fixtureOwner.Drain(ctx)
	check("test-only real Project", createErr)
	check("test-only actual Drain", drainErr)
	if !fixtureOwner.Joined() || created.Validate() != nil || created.State != pc.CreationReady || created.Project == nil || created.Project.ID != projectID {
		t.Fatal("isolated real Project did not confirm and join")
	}

	rootID, childID, siblingID := guardID[kc.Document](t), guardID[kc.Document](t), guardID[kc.Document](t)
	prefix := "/api/v1/projects/" + projectID.String() + "/knowledge/documents"
	csrf := knowledgeWebString(t, session, "csrf_token")
	seed := func(document kc.DocumentID, parent any, title, text string) map[string]any {
		result := setup.call(ownerClient, "POST", prefix+"/"+document.String()+"/create-text", map[string]any{"parent_document_id": parent, "title": title, "media_type": kc.PlainText, "text": text}, csrf, true, 200)
		value, ok := result["document"].(map[string]any)
		if !ok || len(result) != 1 || len(value) != 12 || value["id"] != document.String() || value["project_id"] != projectID.String() || value["parent_document_id"] != parent || value["title"] != title || value["content_version"] != "1" || value["source_kind"] != "text" || value["media_type"] != kc.PlainText || value["status"] != "active" || !reflect.DeepEqual(value["created_by"], map[string]any{"kind": "human", "user_id": actor.Details().UserID}) {
			t.Fatal("formal text seed lost its safe current Owner identity")
		}
		return value
	}
	root := seed(rootID, nil, knowledgeDeleteRootTitle, knowledgeDeleteRootText)
	child := seed(childID, rootID.String(), knowledgeDeleteChildTitle, knowledgeDeleteChildText)
	sibling := seed(siblingID, nil, knowledgeDeleteSiblingTitle, knowledgeDeleteSiblingText)
	var rows, commands, changes, published int
	check("formal seed facts", conn.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_knowledge.documents WHERE project_id=$1 AND content_version=1 AND source_kind='text' AND media_type='text/plain' AND status='active'),
 (SELECT count(*) FROM agenteam_knowledge.commands WHERE project_id=$1 AND command_name='create' AND state='completed' AND committed_at IS NOT NULL),
 (SELECT count(*) FROM agenteam_knowledge.command_events WHERE project_id=$1 AND event_type='knowledge.content_changed'),
 (SELECT count(*) FROM agenteam_knowledge.publications WHERE project_id=$1 AND phase='published' AND source IS NULL AND source_lease_id IS NULL)`, projectID.String()).Scan(&rows, &commands, &changes, &published))
	if rows != 3 || commands != 3 || changes != 3 || published != 3 {
		t.Fatal("formal seed commands and object publications incomplete")
	}
	observations.mode, observations.project, observations.host = mode, projectID.String(), listener.Addr().String()
	observations.conn, observations.actor = conn, actor
	observations.root, observations.child, observations.sibling = root, child, sibling
	observations.requests = map[string]knowledgeDeleteIntent{}
	observations.before, err = observations.snapshot(ctx)
	check("original seed snapshot", err)
	observations.mu.Lock()
	observations.active = true
	observations.mu.Unlock()
	payload := map[string]any{"mode": mode, "cookie": cookie, "project_id": projectID.String(), "route": "/" + username + "/knowledge-delete/knowledge/" + rootID.String(), "root_id": rootID.String(), "child_id": childID.String(), "sibling_id": siblingID.String(), "root": root, "child": child, "sibling": sibling, "root_text": knowledgeDeleteRootText, "child_text": knowledgeDeleteChildText, "sibling_text": knowledgeDeleteSiblingText, "input_hash": inputHash}
	raw, err := json.Marshal(payload)
	check("private material", err)
	check("private material file", os.WriteFile(filepath.Join(private, "knowledge-delete-material.json"), raw, 0600))
	clear(raw)
	result := knowledgeDeleteBrowser(t, ctx, private, origin, dist, evidence, inputHash, mode)
	wantLookup := 0
	if mode == "delete-lost" {
		wantLookup = 1
	}
	observations.mu.Lock()
	observeErr, count, tails := observations.err, observations.count, len(observations.tails)
	previews, deletes, lookups, cuts := observations.previews, observations.deletes, observations.lookups, observations.cuts
	currentRoots, tombstones, siblingReads := observations.currentRoots, observations.tombstones, observations.siblingReads
	originalToken, originalKey, after := observations.token, observations.key, observations.after
	observations.mu.Unlock()
	if observeErr != nil || count != tails || previews != 1 || deletes != 1 || lookups != wantLookup || cuts != wantLookup || currentRoots < 1 || tombstones < 1 || siblingReads < 1 || result["completed"] != true || result["input_hash"] != inputHash || result["case"] != mode || result["original_requests"] != float64(count) || result["all_observed"] != true || result["lookups"] != float64(wantLookup) || result["declared_receipt_cut"] != float64(wantLookup) {
		t.Fatal("Knowledge delete original receipt/current consumer proofs incomplete")
	}
	final, err := observations.snapshot(ctx)
	check("final immutable deletion facts", err)
	if final != after {
		t.Fatal("Lookup or current reads changed the original committed deletion")
	}
	var readers, claims int
	check("original read ownership closed", conn.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_object.object_leases WHERE owner_kind='reader' AND state='active'),
 (SELECT count(*) FROM agenteam_knowledge.work_claims WHERE project_id=$1 AND phase='active')`, projectID.String()).Scan(&readers, &claims))
	if readers != 0 || claims != 0 {
		t.Fatal("Knowledge deletion/current content left an active original owner")
	}
	for _, secret := range []string{adminRecord.Password, inviteURL.Fragment, password, cookie, originalToken, string(originalKey)} {
		if secret != "" && strings.Contains(output.String(), secret) {
			t.Error("private Account or deletion material escaped into root log")
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
	t.Log("Knowledge delete browser chain: original Node Wait, proxy/root join, domain retirement and guard facts complete; cleanup_pending is not physical Object cleanup; production initializer remains unbound")
}

const knowledgeDeleteRootTitle = "待删除目录"
const knowledgeDeleteChildTitle = "目录内正文"
const knowledgeDeleteSiblingTitle = "保留旁支"
const knowledgeDeleteRootText = "删除前根正文 <script>仍是文本</script>。\n"
const knowledgeDeleteChildText = "删除范围内子正文 中文🙂。\n"
const knowledgeDeleteSiblingText = "删除后仍可读取的旁支正文。\n"

type knowledgeDeleteIntent struct {
	kind                  string
	rid                   f.ID[f.Request]
	key                   f.IdempotencyKey
	bodyDigest, keyDigest [sha256.Size]byte
}

// Reuse the delivered Knowledge transport tail container and safe failure path.
// Only this scenario's request intents, SQL facts and current reads are added.
type knowledgeDeleteObservations struct {
	*knowledgeTextObservations
	reportFailure                                func(string)
	dbMu                                         sync.Mutex
	mode                                         string
	conn                                         *pgx.Conn
	actor                                        id.Actor
	root, child, sibling                         map[string]any
	requests                                     map[string]knowledgeDeleteIntent
	token                                        string
	key                                          f.IdempotencyKey
	before, after                                string
	committed                                    bool
	previews, deletes, lookups, cuts             int
	currentRoots, tombstones, gone, siblingReads int
}

type knowledgeDeleteDrop struct {
	header http.Header
	length int
	record map[string]any
}

func (*knowledgeDeleteDrop) Error() string { return "declared Knowledge deletion receipt cut" }

// Record only a fixed caller-selected stage. Never log the original error,
// request body, confirmation token, key, SQL, or private response material.
func (o *knowledgeDeleteObservations) failAt(stage string) error {
	o.mu.Lock()
	first := o.err == nil
	if first {
		o.err = errors.New("closed Knowledge text observation failed")
	}
	err := o.err
	o.mu.Unlock()
	if first && o.reportFailure != nil {
		o.reportFailure(stage)
	}
	return err
}

func (o *knowledgeDeleteObservations) observeHandler(next http.Handler) http.Handler {
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
			_ = o.failAt("request-identity")
			http.Error(w, "request identity", 502)
			return
		}
		tail := &knowledgeTextTail{done: make(chan struct{}), method: r.Method, uri: r.URL.RequestURI()}
		if r.Method == http.MethodGet {
			if o.readRequest(r) != nil {
				_ = o.failAt("request-current-read")
				http.Error(w, "closed deletion current read", 502)
				return
			}
		} else {
			raw, readErr := io.ReadAll(io.LimitReader(r.Body, (16<<10)+1))
			closeErr := r.Body.Close()
			defer clear(raw)
			if r.Method != http.MethodPost || readErr != nil || closeErr != nil || len(raw) > 16<<10 || r.ContentLength != int64(len(raw)) || len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") != o.origin || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("X-CSRF-Token") == "" || r.Header.Get("Cookie") == "" || r.URL.RawQuery != "" || r.URL.ForceQuery || o.writeRequest(r, rid, raw) != nil {
				_ = o.failAt("request-original-intent")
				http.Error(w, "closed deletion original intent", 502)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(raw))
		}
		o.mu.Lock()
		if o.tails[rid.String()] != nil || len(o.tails) >= 96 {
			o.mu.Unlock()
			_ = o.failAt("request-duplicate")
			http.Error(w, "duplicate deletion request", 502)
			return
		}
		o.tails[rid.String()] = tail
		o.mu.Unlock()
		defer close(tail.done)
		next.ServeHTTP(w, r)
		tail.returned = true
	})
}

func (o *knowledgeDeleteObservations) writeRequest(r *http.Request, rid f.ID[f.Request], raw []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	prefix := "/api/v1/projects/" + o.project + "/knowledge/documents"
	root := prefix + "/" + o.root["id"].(string)
	in := knowledgeDeleteIntent{rid: rid, bodyDigest: sha256.Sum256(raw)}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return errors.New("original deletion JSON absent")
	}
	if r.URL.Path == root+"/delete-preview" {
		if len(fields) != 0 || len(r.Header.Values("Idempotency-Key")) != 0 || o.previews != 0 || o.deletes != 0 {
			return errors.New("preview refreshed or supplied a command key")
		}
		in.kind = "preview"
		o.previews++
	} else {
		in.key = f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
		in.keyDigest = sha256.Sum256([]byte(in.key))
		if in.key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 || o.previews != 1 || o.token == "" {
			return errors.New("deletion lacked the original preview or command key")
		}
		if r.URL.Path == root+"/delete-subtree" {
			var body struct {
				Token string `json:"confirmation_token"`
			}
			if skillWebDecode(raw, &body) != nil || len(fields) != 1 || body.Token != o.token || o.deletes != 0 {
				return errors.New("deletion changed the preview token or repeated mutation")
			}
			in.kind = "delete"
			o.key = in.key
			o.deletes++
		} else if r.URL.Path == prefix+"/commands/lookup" {
			var body struct {
				Command  string `json:"command"`
				Document string `json:"document_id"`
				Request  struct {
					Token string `json:"confirmation_token"`
				} `json:"request"`
			}
			if skillWebDecode(raw, &body) != nil || len(fields) != 3 || body.Command != "delete-subtree" || body.Document != o.root["id"] || body.Request.Token != o.token || in.key != o.key || o.mode != "delete-lost" || !o.committed || o.cuts != 1 || o.deletes != 1 || o.lookups != 0 {
				return errors.New("Lookup changed the original complete deletion intent")
			}
			in.kind = "lookup"
			o.lookups++
		} else {
			return errors.New("undeclared deletion mutation")
		}
	}
	o.requests[rid.String()] = in
	return nil
}

func (o *knowledgeDeleteObservations) readRequest(r *http.Request) error {
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 || len(r.Header.Values("Idempotency-Key")) != 0 || r.URL.ForceQuery {
		return errors.New("current read carried a body/key")
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return err
	}
	global := map[string]bool{"/api/v1/auth/bootstrap": true, "/api/v1/session": true, "/api/v1/me": true, "/api/v1/me/preferences": true, "/api/v1/projects": true, "/api/v1/projects/resolve": true}
	if global[r.URL.Path] {
		return nil
	}
	project := "/api/v1/projects/" + o.project
	prefix := project + "/knowledge/documents"
	if r.URL.Path == project && len(query) == 0 {
		return nil
	}
	if r.URL.Path == prefix+"/children" {
		parent := query.Get("parent_document_id")
		if (parent == "null" || parent == o.root["id"] || parent == o.child["id"] || parent == o.sibling["id"]) && reflect.DeepEqual(query, url.Values{"parent_document_id": {parent}, "limit": {"50"}}) {
			return nil
		}
	}
	for _, doc := range []map[string]any{o.root, o.child, o.sibling} {
		target := prefix + "/" + doc["id"].(string)
		if (r.URL.Path == target || r.URL.Path == target+"/ancestors") && len(query) == 0 {
			return nil
		}
		limit := query.Get("max_bytes")
		if r.URL.Path == target+"/content" && (limit == "65536" || limit == "1048576") && reflect.DeepEqual(query, url.Values{"byte_offset": {"0"}, "max_bytes": {limit}}) {
			return nil
		}
	}
	return errors.New("undeclared deletion current endpoint")
}

func (o *knowledgeDeleteObservations) capture(response *http.Response) error {
	o.mu.Lock()
	active := o.active
	o.mu.Unlock()
	if !active {
		return nil
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (5<<20)+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 5<<20 || response.ContentLength != int64(len(raw)) || !json.Valid(raw) || response.Header.Get("Cache-Control") != "no-store" {
		return o.failAt("response-complete-json")
	}
	rid, err := f.ParseID[f.Request](response.Header.Get("X-Request-ID"))
	if err != nil || rid.String() != response.Header.Get("X-Request-ID") || len(response.Header.Values("X-Request-ID")) != 1 {
		return o.failAt("response-request-id")
	}
	o.mu.Lock()
	tail, in := o.tails[rid.String()], o.requests[rid.String()]
	o.mu.Unlock()
	if tail == nil || tail.method != response.Request.Method || tail.uri != response.Request.URL.RequestURI() {
		return o.failAt("response-original-tail")
	}
	select {
	case <-tail.done:
	case <-response.Request.Context().Done():
		return o.failAt("response-tail-context")
	}
	if !tail.returned {
		return o.failAt("response-handler-return")
	}
	currentVerified := false
	if in.kind != "" {
		if response.StatusCode != http.StatusOK {
			return o.failAt("response-command-status")
		}
		switch in.kind {
		case "preview":
			if o.preview(response.Request.Context(), raw) != nil {
				return o.failAt("response-preview-facts")
			}
		case "delete":
			if o.deleteFacts(response.Request.Context(), in, raw) != nil {
				return o.failAt("response-delete-facts")
			}
		case "lookup":
			if o.lookupFacts(response.Request.Context(), raw) != nil {
				return o.failAt("response-lookup-facts")
			}
		default:
			return o.failAt("response-command-kind")
		}
	} else {
		currentVerified, err = o.currentRead(response, raw)
		if err != nil {
			return o.failAt("response-current-facts")
		}
	}
	record := map[string]any{"input_hash": o.inputHash, "request_id": rid.String(), "method": tail.method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(tail.uri))), "status": response.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true, "request_host_matches": true}
	if in.kind != "" {
		o.mu.Lock()
		token := o.token
		o.mu.Unlock()
		record["kind"] = in.kind
		record["request_body_sha256"] = fmt.Sprintf("%x", in.bodyDigest)
		record["confirmation_token_sha256"] = fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
		if in.kind != "preview" {
			record["idempotency_key_sha256"] = fmt.Sprintf("%x", in.keyDigest)
			record["committed_verified"] = true
			record["cleanup_pending"] = true
		}
	} else if currentVerified {
		record["current_read_verified"] = true
	}
	if o.writeProof(record) != nil {
		return o.failAt("response-proof-write")
	}
	o.mu.Lock()
	o.count++
	o.mu.Unlock()
	if in.kind == "delete" && o.mode == "delete-lost" {
		return &knowledgeDeleteDrop{response.Header.Clone(), len(raw), record}
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}

func (o *knowledgeDeleteObservations) preview(ctx context.Context, raw []byte) error {
	var value struct {
		Root    string           `json:"root_id"`
		Nodes   []map[string]any `json:"nodes"`
		Digest  f.Digest         `json:"scope_digest"`
		Token   string           `json:"confirmation_token"`
		Expires f.Instant        `json:"expires_at"`
	}
	var fields map[string]json.RawMessage
	if skillWebDecode(raw, &value) != nil || json.Unmarshal(raw, &fields) != nil || len(fields) != 5 || value.Root != o.root["id"] || len(value.Nodes) != 2 || value.Digest.Validate() != nil || value.Expires.Validate() != nil {
		return errors.New("preview is not the exact safe subtree")
	}
	if _, err := kc.ParseConfirmationToken(value.Token); err != nil {
		return errors.New("original preview token invalid")
	}
	seen := map[string]bool{}
	for _, doc := range value.Nodes {
		name, ok := doc["id"].(string)
		if !ok || seen[name] || !reflect.DeepEqual(doc, o.root) && !reflect.DeepEqual(doc, o.child) {
			return errors.New("preview changed or repeated a safe seed document")
		}
		seen[name] = true
	}
	snapshot, err := o.snapshot(ctx)
	if err != nil || snapshot != o.before {
		return errors.New("preview changed persisted business facts")
	}
	o.mu.Lock()
	o.token = value.Token
	o.mu.Unlock()
	return nil
}

func (o *knowledgeDeleteObservations) deletedIDs() []string {
	ids := []string{o.root["id"].(string), o.child["id"].(string)}
	sort.Strings(ids)
	return ids
}

func (o *knowledgeDeleteObservations) deleteFacts(ctx context.Context, in knowledgeDeleteIntent, raw []byte) error {
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		return errors.New("deletion receipt JSON invalid")
	}
	ids := o.deletedIDs()
	want := map[string]any{"root_id": o.root["id"], "deleted_ids": []any{ids[0], ids[1]}, "cleanup_pending": true}
	if !reflect.DeepEqual(body, want) {
		return errors.New("deletion receipt differs from the original subtree")
	}
	project, err := f.ParseID[id.Project](o.project)
	if err != nil {
		return err
	}
	root, err := f.ParseID[kc.Document](o.root["id"].(string))
	if err != nil {
		return err
	}
	o.mu.Lock()
	token := o.token
	o.mu.Unlock()
	confirmation, err := kc.ParseConfirmationToken(token)
	if err != nil {
		return err
	}
	digest, err := kc.DeleteDigest(o.actor, f.CommandMeta{RequestID: in.rid, IdempotencyKey: in.key}, project, root, confirmation)
	if err != nil {
		return err
	}
	// The separate SQL connection observes the committed original service result;
	// it never creates commands/receipts or invokes another deletion/preview.
	o.dbMu.Lock()
	var receipt []byte
	err = o.conn.QueryRow(ctx, `SELECT receipt FROM agenteam_knowledge.commands
 WHERE project_id=$1 AND command_name='delete-subtree' AND command_key=$2 AND document_id=$3
 AND actor_user_id=$4 AND semantic_digest=$5 AND state='completed' AND committed_at IS NOT NULL
 AND request IS NULL AND plan IS NULL`, o.project, string(in.key), root.String(), o.actor.Details().UserID, string(digest)).Scan(&receipt)
	if err != nil {
		o.dbMu.Unlock()
		return err
	}
	var committed kc.MutationReceipt
	if json.Unmarshal(receipt, &committed) != nil || committed.Validate() != nil || committed.Command != kc.DeleteSubtree || committed.RootID == nil || *committed.RootID != root || !committed.Changed || !committed.CleanupPending || len(committed.DeletedIDs) != 2 || committed.DeletedIDs[0].String() != ids[0] || committed.DeletedIDs[1].String() != ids[1] {
		o.dbMu.Unlock()
		return errors.New("SQL receipt differs from original deletion")
	}
	var total, deletes, tombstones, sibling, events, outbox, audit, pending int
	err = o.conn.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_knowledge.commands WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_knowledge.commands WHERE project_id=$1 AND command_name='delete-subtree'),
 (SELECT count(*) FROM agenteam_knowledge.documents WHERE project_id=$1 AND id IN ($2,$3) AND status='deleted' AND content_version=1 AND deleted_at IS NOT NULL AND parent_document_id IS NULL AND title IS NULL AND source_kind IS NULL AND current_object_id IS NULL AND current_upload_id IS NULL),
 (SELECT count(*) FROM agenteam_knowledge.documents WHERE project_id=$1 AND id=$4 AND status='active' AND content_version=1 AND parent_document_id IS NULL AND title=$5 AND source_kind='text' AND media_type='text/plain' AND current_object_id IS NOT NULL AND current_upload_id IS NOT NULL),
 (SELECT count(*) FROM agenteam_knowledge.command_events e JOIN agenteam_knowledge.commands c ON c.id=e.command_id WHERE c.project_id=$1 AND c.command_name='delete-subtree' AND c.command_key=$6 AND e.event_type='knowledge.deleted' AND e.document_id IN ($2,$3)),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND event_type='knowledge.deleted' AND aggregate_id IN ($2,$3)),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='knowledge.delete_subtree' AND resource_id=$2 AND user_id=$7 AND metadata->>'deleted_count'='2'),
 (SELECT count(*) FROM agenteam_knowledge.object_cleanup c JOIN agenteam_knowledge.commands d ON d.id=c.command_id WHERE c.project_id=$1 AND d.command_name='delete-subtree' AND d.command_key=$6 AND c.document_id IN ($2,$3) AND c.reason='owner_deleted' AND c.phase='object')`, o.project, o.root["id"], o.child["id"], o.sibling["id"], o.sibling["title"], string(in.key), o.actor.Details().UserID).Scan(&total, &deletes, &tombstones, &sibling, &events, &outbox, &audit, &pending)
	o.dbMu.Unlock()
	if err != nil {
		return err
	}
	if total != 4 || deletes != 1 || tombstones != 2 || sibling != 1 || events != 2 || outbox != 2 || audit != 1 || pending != 2 {
		return errors.New("committed deletion SQL facts incomplete")
	}
	after, err := o.snapshot(ctx)
	if err != nil || after == o.before {
		return errors.New("committed deletion snapshot absent")
	}
	o.mu.Lock()
	o.after, o.committed = after, true
	o.mu.Unlock()
	return nil
}

func (o *knowledgeDeleteObservations) lookupFacts(ctx context.Context, raw []byte) error {
	ids := o.deletedIDs()
	want := map[string]any{"state": "committed", "receipt": map[string]any{"command": "delete-subtree", "root_id": o.root["id"], "changed": true, "deleted_ids": []any{ids[0], ids[1]}, "cleanup_pending": true}}
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil || !reflect.DeepEqual(value, want) {
		return o.failAt("lookup-receipt-shape")
	}
	after, err := o.snapshot(ctx)
	o.mu.Lock()
	before := o.after
	o.mu.Unlock()
	if err != nil || before != after {
		if err != nil {
			return o.failAt("lookup-snapshot-read")
		}
		return o.failAt("lookup-snapshot-changed")
	}
	return nil
}

func (o *knowledgeDeleteObservations) snapshot(ctx context.Context) (string, error) {
	o.dbMu.Lock()
	defer o.dbMu.Unlock()
	var value string
	err := o.conn.QueryRow(ctx, `SELECT jsonb_build_object(
 'project',(SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1),
 'documents',(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM agenteam_knowledge.documents d WHERE project_id=$1),
 'commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_knowledge.commands c WHERE project_id=$1),
 'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_knowledge.command_events e WHERE project_id=$1),
 'publications',(SELECT jsonb_agg(to_jsonb(p) ORDER BY command_id) FROM agenteam_knowledge.publications p WHERE project_id=$1),
 'cleanup',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_knowledge.object_cleanup c WHERE project_id=$1),
 'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a WHERE project_id=$1),
 'outbox',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_outbox.events e WHERE project_id=$1))::text`, o.project).Scan(&value)
	return value, err
}

func (o *knowledgeDeleteObservations) currentRead(response *http.Response, raw []byte) (bool, error) {
	r := response.Request
	prefix := "/api/v1/projects/" + o.project + "/knowledge/documents"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		if response.StatusCode != http.StatusOK {
			return false, errors.New("ordinary Owner page read failed")
		}
		return false, nil
	}
	o.mu.Lock()
	deleted := o.committed
	o.mu.Unlock()
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return false, errors.New("current read JSON invalid")
	}
	problem := func(code f.Code, status int, title, detail string) (bool, error) {
		want := map[string]any{"type": "urn:agenteam:problem:" + strings.ReplaceAll(strings.ToLower(string(code)), "_", "-"), "title": title, "status": float64(status), "detail": detail, "instance": r.URL.EscapedPath(), "code": string(code), "request_id": response.Header.Get("X-Request-ID"), "commit_state": "not_started"}
		if response.StatusCode != status || response.Header.Get("Content-Type") != "application/problem+json" || !reflect.DeepEqual(value, want) {
			return false, errors.New("current deletion Problem differs from formal contract")
		}
		return true, nil
	}
	var want map[string]any
	if r.URL.Path == prefix+"/children" {
		parent := r.URL.Query().Get("parent_document_id")
		items := []any{}
		switch parent {
		case "null":
			docs := []map[string]any{o.sibling}
			if !deleted {
				docs = append(docs, o.root)
			}
			sort.Slice(docs, func(i, j int) bool {
				if docs[i]["title"] == docs[j]["title"] {
					return docs[i]["id"].(string) < docs[j]["id"].(string)
				}
				return docs[i]["title"].(string) < docs[j]["title"].(string)
			})
			for _, doc := range docs {
				items = append(items, doc)
			}
		case o.root["id"].(string):
			if deleted {
				return problem(f.NotFound, 404, "Not found", "The requested resource was not found.")
			}
			items = append(items, o.child)
		case o.child["id"].(string):
			if deleted {
				return problem(f.NotFound, 404, "Not found", "The requested resource was not found.")
			}
		case o.sibling["id"].(string):
		default:
			return false, errors.New("current child list changed its parent")
		}
		want = map[string]any{"items": items}
		if deleted && parent == "null" {
			o.mu.Lock()
			o.currentRoots++
			o.mu.Unlock()
		}
	} else {
		texts := []string{knowledgeDeleteRootText, knowledgeDeleteChildText, knowledgeDeleteSiblingText}
		for index, doc := range []map[string]any{o.root, o.child, o.sibling} {
			target := prefix + "/" + doc["id"].(string)
			isDeleted := deleted && index < 2
			switch r.URL.Path {
			case target:
				if !isDeleted {
					want = map[string]any{"active": doc}
					break
				}
				var head struct {
					Deleted kc.DocumentTombstone `json:"deleted"`
				}
				tombstone, ok := value["deleted"].(map[string]any)
				if response.StatusCode != http.StatusOK || !ok || len(value) != 1 || len(tombstone) != 4 || skillWebDecode(raw, &head) != nil || head.Deleted.Validate() != nil || head.Deleted.ID.String() != doc["id"] || head.Deleted.ProjectID.String() != o.project || head.Deleted.ContentVersion != 1 {
					return false, errors.New("metadata did not return its safe current tombstone")
				}
				o.dbMu.Lock()
				var at time.Time
				err := o.conn.QueryRow(r.Context(), `SELECT deleted_at FROM agenteam_knowledge.documents WHERE project_id=$1 AND id=$2 AND status='deleted'`, o.project, doc["id"]).Scan(&at)
				o.dbMu.Unlock()
				if err != nil || !at.Equal(head.Deleted.DeletedAt.Time()) {
					return false, errors.New("current tombstone differs from SQL fact")
				}
				if index == 0 {
					o.mu.Lock()
					o.tombstones++
					o.mu.Unlock()
				}
				return true, nil
			case target + "/ancestors":
				if isDeleted {
					return problem(f.NotFound, 404, "Not found", "The requested resource was not found.")
				}
				items := []any{}
				if index == 1 {
					items = append(items, o.root)
				}
				want = map[string]any{"items": items}
			case target + "/content":
				if isDeleted {
					ok, err := problem(f.ResourceDeleted, 410, "Resource deleted", "The requested resource has been deleted.")
					if err == nil && index == 0 {
						o.mu.Lock()
						o.gone++
						o.mu.Unlock()
					}
					return ok, err
				}
				want = map[string]any{"document": doc, "text": map[string]any{"text": texts[index], "next_byte_offset": fmt.Sprint(len(texts[index])), "truncated": false}}
				if deleted && index == 2 {
					o.mu.Lock()
					o.siblingReads++
					o.mu.Unlock()
				}
			}
		}
	}
	if response.StatusCode != http.StatusOK || want == nil || !reflect.DeepEqual(value, want) {
		return false, errors.New("current document read differs from canonical subtree facts")
	}
	return true, nil
}

func (o *knowledgeDeleteObservations) writeProof(record map[string]any) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	rid, ok := record["request_id"].(string)
	if !ok {
		return errors.New("proof identity absent")
	}
	return os.WriteFile(filepath.Join(o.evidence, "knowledge-delete-"+o.mode+"-response-"+rid+".json"), raw, 0600)
}

// Follow the delivered Scheduler physical cut exactly: the original response
// has already reached EOF/Close, handler return and committed SQL verification.
func (o *knowledgeDeleteObservations) proxyError(w http.ResponseWriter, _ *http.Request, err error) {
	var drop *knowledgeDeleteDrop
	if !errors.As(err, &drop) {
		_ = o.failAt("proxy-upstream")
		http.Error(w, "owned Knowledge deletion proxy failed", 502)
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
		_ = o.failAt("cut-delivery")
		return
	}
	drop.record["receipt_cut"] = true
	if o.writeProof(drop.record) != nil {
		_ = o.failAt("cut-proof-write")
		return
	}
	o.mu.Lock()
	o.cuts++
	o.mu.Unlock()
}

func knowledgeDeleteBrowser(t *testing.T, ctx context.Context, private, origin, dist, evidence, inputHash, mode string) map[string]any {
	t.Helper()
	harness, err := filepath.Abs("../../../tests/account-captcha-web")
	if err != nil {
		t.Fatal("browser harness path unavailable")
	}
	cmd := exec.CommandContext(ctx, "node", filepath.Join(harness, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(harness, "knowledge-delete.config.js"))
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
	cmd.Env = append(cmd.Env, "TMPDIR="+private, "AGENTEAM_AUTH_WEB_ORIGIN="+origin, "AGENTEAM_AUTH_WEB_PRIVATE="+private, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_KNOWLEDGE_DELETE_WEB_DIST="+dist, "AGENTEAM_KNOWLEDGE_DELETE_WEB_EVIDENCE="+evidence, "AGENTEAM_KNOWLEDGE_DELETE_WEB_INPUT_HASH="+inputHash, "AGENTEAM_KNOWLEDGE_DELETE_WEB_CASE="+mode, "PLAYWRIGHT_NO_COPY_PROMPT=1")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal("locked KnowledgeDelete Node could not start")
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
	t.Logf("KnowledgeDelete Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	// Never print the browser's credential-bearing call log. The spec writes
	// fixed-stage safe failure diagnostics; the original child status is final.
	if err != nil || !regexp.MustCompile(`(?m)\b1 passed\b`).Match(output.Bytes()) {
		t.Fatal("KnowledgeDelete locked browser did not actually pass its one case")
	}
	raw, err := os.ReadFile(filepath.Join(private, "knowledge-delete-result.json"))
	if err != nil {
		t.Fatal("KnowledgeDelete browser result missing")
	}
	if len(raw) > 16<<10 {
		t.Fatal("KnowledgeDelete browser result too large")
	}
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil || result["input_hash"] != inputHash {
		t.Fatal("KnowledgeDelete browser result not bound to frozen inputs")
	}
	return result
}
