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

// The ordinary Owner moves existing documents through the real default App,
// then independently reads their current location and unchanged content.
// Static hosting is private test infrastructure, not production SPA delivery.
func TestKnowledgeMoveWeb(t *testing.T) {
	for _, mode := range []string{"move-normal", "move-lost"} {
		t.Run(mode, func(t *testing.T) { knowledgeMoveWeb(t, mode) })
	}
}

func knowledgeMoveWeb(t *testing.T, mode string) {
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
			if stage == "evidence parent" || stage == "fresh evidence" {
				var pathErr *os.PathError
				var errno syscall.Errno
				if errors.As(err, &pathErr) && errors.As(pathErr.Err, &errno) {
					// Report the OS operation and error without its private path.
					t.Fatalf("Knowledge browser %s failed: op=%q errno=%d (%s)", stage, pathErr.Op, errno, errno.Error())
				}
			}
			t.Fatalf("Knowledge browser %s failed", stage)
		}
	}
	dist := os.Getenv("AGENTEAM_KNOWLEDGE_MOVE_WEB_DIST")
	runtime := os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	evidence := os.Getenv("AGENTEAM_KNOWLEDGE_MOVE_WEB_EVIDENCE")
	inputHash := os.Getenv("AGENTEAM_KNOWLEDGE_MOVE_WEB_INPUT_HASH")
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
	private, err := os.MkdirTemp(runtime, "kmv-")
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
	observations := &knowledgeMoveObservations{mode: mode, evidence: evidence, inputHash: inputHash, origin: origin, tails: map[string]*knowledgeMoveTail{}, reads: map[int]map[string]int{}}
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
	documents, knowledgeOK := assembly.knowledge.(*knowledgeCallAssembly)
	assembly.mu.Unlock()
	objects, objectOK := owned.objects().(*objectAssembly)
	if !projectOK || !skillOK || !knowledgeOK || documents == nil || documents.pair == nil || !objectOK {
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
	email, username := "move-owner@example.com", "move-owner"
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
	// Use the actual default Project/Skill/Object composition. Account owns its
	// original call lifetime; no test-only initializer or ready SQL is installed.
	conn := db.Connect(t)
	projectID := guardID[id.Project](t)
	created, createErr := projects.service.CreateProject(ctx, actor, meta(), pc.CreateProjectRequest{ProjectID: projectID, Name: "knowledge-move", Description: "Existing documents for the bounded UI read"})
	check("default real Project", createErr)
	if created.Validate() != nil || created.State != pc.CreationReady || created.Project == nil || created.Project.ID != projectID {
		t.Fatal("default real Project did not confirm")
	}

	parentID, documentID := guardID[kc.Document](t), guardID[kc.Document](t)
	prefix := "/api/v1/projects/" + projectID.String() + "/knowledge/documents"
	csrf := knowledgeWebString(t, session, "csrf_token")
	seed := func(target kc.DocumentID, title, text string) map[string]any {
		result := setup.call(ownerClient, "POST", prefix+"/"+target.String()+"/create-text", map[string]any{"parent_document_id": nil, "title": title, "media_type": kc.PlainText, "text": text}, csrf, true, 200)
		doc, ok := result["document"].(map[string]any)
		if !ok || len(result) != 1 || len(doc) != 12 || doc["id"] != target.String() || doc["project_id"] != projectID.String() || doc["parent_document_id"] != nil || doc["title"] != title || doc["content_version"] != "1" || doc["source_kind"] != "text" || doc["media_type"] != kc.PlainText || doc["status"] != "active" || !reflect.DeepEqual(doc["created_by"], map[string]any{"kind": "human", "user_id": actor.Details().UserID}) {
			t.Fatal("formal HTTP text seed lost safe document/Owner identity")
		}
		return doc
	}
	parent := seed(parentID, knowledgeMoveParentTitle, knowledgeMoveParentText)
	document := seed(documentID, knowledgeMoveTitle, knowledgeMoveText)
	var rows, commands, changes, published int
	check("formal seed facts", conn.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_knowledge.documents WHERE project_id=$1 AND content_version=1 AND source_kind='text' AND media_type='text/plain' AND status='active'),
 (SELECT count(*) FROM agenteam_knowledge.commands WHERE project_id=$1 AND command_name='create' AND state='completed' AND committed_at IS NOT NULL),
 (SELECT count(*) FROM agenteam_knowledge.command_events WHERE project_id=$1 AND event_type='knowledge.content_changed'),
 (SELECT count(*) FROM agenteam_knowledge.publications WHERE project_id=$1 AND phase='published' AND source IS NULL AND source_lease_id IS NULL)`, projectID.String()).Scan(&rows, &commands, &changes, &published))
	if rows != 2 || commands != 2 || changes != 2 || published != 2 {
		t.Fatal("real text seed commands/publications incomplete")
	}
	observations.project, observations.target, observations.parentID = projectID, documentID, parentID
	observations.actor, observations.conn = actor, conn
	observations.parent, observations.original, observations.current = parent, document, document
	observations.host = listener.Addr().String()
	check("original object identity", conn.QueryRow(ctx, `SELECT current_object_id::text FROM agenteam_knowledge.documents WHERE project_id=$1 AND id=$2`, projectID.String(), documentID.String()).Scan(&observations.objectID))
	observations.stable, err = observations.snapshot(ctx, false)
	check("seed immutable facts", err)
	observations.mu.Lock()
	observations.active = true
	observations.mu.Unlock()
	payload := map[string]any{"origin": origin, "mode": mode, "cookie": cookie, "project_id": projectID.String(), "route": "/" + username + "/knowledge-move/knowledge/" + documentID.String(), "parent_id": parentID.String(), "document_id": documentID.String(), "parent": parent, "document": document, "text": knowledgeMoveText, "input_hash": inputHash}
	raw, err := json.Marshal(payload)
	check("private material", err)
	check("private material file", os.WriteFile(filepath.Join(private, "knowledge-move-material.json"), raw, 0600))
	clear(raw)
	result := knowledgeMoveBrowser(t, ctx, private, origin, dist, evidence, inputHash, mode)
	observations.mu.Lock()
	observeErr, observed, tails := observations.err, observations.count, len(observations.tails)
	writes, receipts, lookups, cuts := len(observations.intents), len(observations.receipts), observations.lookups, observations.cuts
	readCounts := observations.reads
	observations.mu.Unlock()
	wantWrites, wantLookups, wantCuts := 2, 0, 0
	if mode == "move-lost" {
		wantWrites, wantLookups, wantCuts = 1, 1, 1
	}
	if observeErr != nil || observed != tails || writes != wantWrites || receipts != wantWrites || lookups != wantLookups || cuts != wantCuts || result["case"] != mode || result["test_status"] != "passed" || result["all_observed"] != true || result["original_requests"] != float64(observed) || result["updates"] != float64(writes) || result["lookups"] != float64(lookups) || result["declared_receipt_cut"] != (wantCuts == 1) {
		t.Fatal("Knowledge Move original response/consumer proofs incomplete")
	}
	for stage := 0; stage <= wantWrites; stage++ {
		for _, kind := range []string{"get", "ancestors", "content", "root-children"} {
			if readCounts[stage][kind] < 1 {
				t.Fatalf("current independent %s read absent after stage %d", kind, stage)
			}
		}
		if stage > 0 && readCounts[stage]["parent-children"] < 1 {
			t.Fatal("source/target parent directory was not independently refreshed", stage)
		}
	}
	after, err := observations.snapshot(ctx, false)
	check("final immutable facts", err)
	if after != observations.stable {
		t.Fatal("Move changed content identity, parent document, publications or original Audit/Outbox facts")
	}
	var moveCount, activeReaders, claims int
	var currentParent *string
	check("final command/current/ownership facts", conn.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_knowledge.commands WHERE project_id=$1 AND command_name='move' AND document_id=$2 AND actor_user_id=$3 AND state='completed' AND request IS NULL AND plan IS NULL AND committed_at IS NOT NULL),
 (SELECT parent_document_id::text FROM agenteam_knowledge.documents WHERE project_id=$1 AND id=$2),
 (SELECT count(*) FROM agenteam_object.object_leases WHERE owner_kind='reader' AND state='active'),
 (SELECT count(*) FROM agenteam_knowledge.work_claims WHERE project_id=$1 AND phase='active')`, projectID.String(), documentID.String(), actor.Details().UserID).Scan(&moveCount, &currentParent, &activeReaders, &claims))
	if moveCount != wantWrites || activeReaders != 0 || claims != 0 || mode == "move-normal" && currentParent != nil || mode == "move-lost" && (currentParent == nil || *currentParent != parentID.String()) {
		t.Fatal("final exact Move or original reader ownership facts differ")
	}
	for _, secret := range []string{adminRecord.Password, inviteURL.Fragment, password, cookie} {
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
	if !projects.Joined() || !skills.service.Joined() || !documents.Joined() || !assembly.Joined() || !owned.producersJoined() {
		t.Fatal("original domain owners did not join")
	}
	check("released guard", syscall.Flock(int(claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	check("guard unlock", syscall.Flock(int(claim.Fd()), syscall.LOCK_UN))
	var stopped string
	check("guard stopped fact", conn.QueryRow(ctx, `SELECT state FROM agenteam_object.process_claims WHERE process_id=$1`, objects.process.String()).Scan(&stopped))
	if stopped != "stopped" {
		t.Fatal("original process claim not stopped")
	}
	t.Log("Knowledge Move browser chain: original Node Wait, proxy/root join, domain retirement and guard facts complete; default creation uses the actual initializer; destructive lifecycle remains unbound")
}

const knowledgeMoveParentTitle = "目标父文档"
const knowledgeMoveParentText = "父文档内容保持不变。\n"
const knowledgeMoveTitle = "待移动文档 中文🙂"
const knowledgeMoveText = "移动保留正文 <%_!>。\n第二行🙂"

type knowledgeMoveIntent struct {
	kind                  string
	rid                   f.ID[f.Request]
	key                   f.IdempotencyKey
	input                 kc.MoveRequest
	bodyDigest, keyDigest [sha256.Size]byte
}
type knowledgeMoveTail struct {
	done        chan struct{}
	returned    bool
	method, uri string
	stage       int
	document    map[string]any
	intent      *knowledgeMoveIntent
}
type knowledgeMoveObservations struct {
	mu, dbMu                                sync.Mutex
	mode, evidence, inputHash, origin, host string
	active                                  bool
	err                                     error
	count, lookups, cuts                    int
	lookupCompleted                         bool
	project                                 id.ProjectID
	target, parentID                        kc.DocumentID
	actor                                   id.Actor
	conn                                    *pgx.Conn
	objectID, stable, after                 string
	parent, original, current               map[string]any
	intents                                 []knowledgeMoveIntent
	receipts                                []map[string]any
	tails                                   map[string]*knowledgeMoveTail
	reads                                   map[int]map[string]int
}
type knowledgeMoveDrop struct {
	header http.Header
	length int
	record map[string]any
}

func (*knowledgeMoveDrop) Error() string { return "declared Knowledge Move receipt cut" }
func (o *knowledgeMoveObservations) fail() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.err == nil {
		o.err = errors.New("original Knowledge Move browser observation failed")
	}
	return o.err
}
func (o *knowledgeMoveObservations) prefix() string {
	return "/api/v1/projects/" + o.project.String() + "/knowledge/documents"
}
func (o *knowledgeMoveObservations) observeHandler(next http.Handler) http.Handler {
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
		o.mu.Lock()
		tail := &knowledgeMoveTail{done: make(chan struct{}), method: r.Method, uri: r.URL.RequestURI(), stage: len(o.receipts), document: o.current}
		lookupCompleted := o.lookupCompleted
		o.mu.Unlock()
		if r.Method == http.MethodGet {
			targetPath := o.prefix() + "/" + o.target.String()
			if o.mode == "move-lost" && tail.stage > 0 && !lookupCompleted && (r.URL.Path == targetPath || r.URL.Path == targetPath+"/ancestors" || r.URL.Path == targetPath+"/content") {
				_ = o.fail()
				http.Error(w, "current read before original Lookup confirmation", 502)
				return
			}
			if o.readRequest(r) != nil {
				_ = o.fail()
				http.Error(w, "closed Knowledge Move read", 502)
				return
			}
		} else {
			raw, readErr := io.ReadAll(io.LimitReader(r.Body, 4097))
			closeErr := r.Body.Close()
			defer clear(raw)
			key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
			if r.Method != http.MethodPost || readErr != nil || closeErr != nil || len(raw) > 4096 || r.ContentLength != int64(len(raw)) || key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 || len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") != o.origin || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("X-CSRF-Token") == "" || r.Header.Get("Cookie") == "" || r.URL.RawQuery != "" || r.URL.ForceQuery {
				_ = o.fail()
				http.Error(w, "original Move body", 502)
				return
			}
			in := knowledgeMoveIntent{rid: rid, key: key, bodyDigest: sha256.Sum256(raw), keyDigest: sha256.Sum256([]byte(key))}
			o.mu.Lock()
			valid := false
			if r.URL.Path == o.prefix()+"/"+o.target.String()+"/move" {
				var fields map[string]json.RawMessage
				valid = skillWebDecode(raw, &in.input) == nil && json.Unmarshal(raw, &fields) == nil && len(fields) == 2 && fields["expected_parent_id"] != nil && fields["target_parent_id"] != nil && len(o.intents) == len(o.receipts)
				n := len(o.intents)
				if n == 0 {
					valid = valid && in.input.ExpectedParentID == nil && in.input.TargetParentID != nil && *in.input.TargetParentID == o.parentID
				} else if n == 1 {
					valid = valid && o.mode == "move-normal" && in.input.ExpectedParentID != nil && *in.input.ExpectedParentID == o.parentID && in.input.TargetParentID == nil
				} else {
					valid = false
				}
				for _, old := range o.intents {
					valid = valid && old.key != key
				}
				in.kind = "move"
				if valid {
					o.intents = append(o.intents, in)
				}
			} else if r.URL.Path == o.prefix()+"/commands/lookup" {
				var body struct {
					Command  string         `json:"command"`
					Document kc.DocumentID  `json:"document_id"`
					Request  kc.MoveRequest `json:"request"`
				}
				var fields map[string]json.RawMessage
				var requestFields map[string]json.RawMessage
				valid = skillWebDecode(raw, &body) == nil && json.Unmarshal(raw, &fields) == nil && len(fields) == 3 && json.Unmarshal(fields["request"], &requestFields) == nil && len(requestFields) == 2 && requestFields["expected_parent_id"] != nil && requestFields["target_parent_id"] != nil && body.Command == "move" && body.Document == o.target && o.mode == "move-lost" && len(o.intents) == 1 && len(o.receipts) == 1 && o.cuts == 1 && o.lookups == 0
				if valid {
					valid = key == o.intents[0].key && reflect.DeepEqual(body.Request, o.intents[0].input)
				}
				in.kind, in.input = "lookup", body.Request
				if valid {
					o.lookups++
				}
			}
			o.mu.Unlock()
			if !valid {
				_ = o.fail()
				http.Error(w, "closed original Move intent", 502)
				return
			}
			tail.intent = &in
			r.Body = io.NopCloser(bytes.NewReader(raw))
		}
		o.mu.Lock()
		if o.tails[rid.String()] != nil || len(o.tails) >= 96 {
			o.mu.Unlock()
			_ = o.fail()
			http.Error(w, "duplicate Move request", 502)
			return
		}
		o.tails[rid.String()] = tail
		o.mu.Unlock()
		defer close(tail.done)
		next.ServeHTTP(w, r)
		tail.returned = true
	})
}
func (o *knowledgeMoveObservations) readRequest(r *http.Request) error {
	if r.ContentLength > 0 || len(r.TransferEncoding) != 0 || r.Header.Get("Idempotency-Key") != "" || r.URL.ForceQuery {
		return errors.New("read carried body/key")
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return err
	}
	global := map[string]bool{"/api/v1/auth/bootstrap": true, "/api/v1/session": true, "/api/v1/me": true, "/api/v1/me/preferences": true, "/api/v1/projects": true, "/api/v1/projects/resolve": true}
	if global[r.URL.Path] {
		return nil
	}
	if r.URL.Path == "/api/v1/projects/"+o.project.String() && len(query) == 0 {
		return nil
	}
	prefix := o.prefix()
	if r.URL.Path == prefix+"/children" {
		parent := query.Get("parent_document_id")
		if (parent == "null" || parent == o.parentID.String() || parent == o.target.String()) && reflect.DeepEqual(query, url.Values{"parent_document_id": {parent}, "limit": {"50"}}) {
			return nil
		}
	}
	for _, target := range []string{o.target.String(), o.parentID.String()} {
		path := prefix + "/" + target
		if (r.URL.Path == path || r.URL.Path == path+"/ancestors") && len(query) == 0 {
			return nil
		}
		limit := query.Get("max_bytes")
		if r.URL.Path == path+"/content" && (limit == "65536" || limit == "1048576") && reflect.DeepEqual(query, url.Values{"byte_offset": {"0"}, "max_bytes": {limit}}) {
			return nil
		}
	}
	return errors.New("undeclared Knowledge Move browser read")
}

func (o *knowledgeMoveObservations) snapshot(ctx context.Context, includeMoves bool) (string, error) {
	o.dbMu.Lock()
	defer o.dbMu.Unlock()
	var value string
	err := o.conn.QueryRow(ctx, `SELECT jsonb_build_object(
 'project',(SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1),
 'documents',(SELECT jsonb_agg(CASE WHEN d.id=$2 AND NOT $3 THEN to_jsonb(d)-'parent_document_id'-'updated_at' ELSE to_jsonb(d) END ORDER BY d.id) FROM agenteam_knowledge.documents d WHERE d.project_id=$1),
 'commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY c.id) FROM agenteam_knowledge.commands c WHERE c.project_id=$1 AND ($3 OR c.command_name<>'move')),
 'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY e.id) FROM agenteam_knowledge.command_events e WHERE e.project_id=$1),
 'publications',(SELECT jsonb_agg(to_jsonb(p) ORDER BY p.command_id) FROM agenteam_knowledge.publications p WHERE p.project_id=$1),
 'objects',(SELECT jsonb_agg(to_jsonb(b) ORDER BY b.id) FROM agenteam_object.objects b WHERE b.project_id=$1),
 'uploads',(SELECT jsonb_agg(to_jsonb(u) ORDER BY u.id) FROM agenteam_object.uploads u WHERE u.project_id=$1),
 'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM agenteam_audit.audit_records a WHERE a.project_id=$1),
 'outbox',(SELECT jsonb_agg(to_jsonb(e) ORDER BY e.id) FROM agenteam_outbox.events e WHERE e.project_id=$1))::text`, o.project.String(), o.target.String(), includeMoves).Scan(&value)
	return value, err
}
func (o *knowledgeMoveObservations) commandFacts(ctx context.Context, in knowledgeMoveIntent, raw []byte) (map[string]any, error) {
	semantic, err := kc.MoveDigest(o.actor, f.CommandMeta{RequestID: in.rid, IdempotencyKey: in.key}, o.project, o.target, in.input)
	if err != nil {
		return nil, err
	}
	o.dbMu.Lock()
	var target, user, digest, state string
	var requestNull, planNull, committed bool
	var receiptRaw []byte
	err = o.conn.QueryRow(ctx, `SELECT document_id::text,actor_user_id::text,semantic_digest,state,request IS NULL,plan IS NULL,committed_at IS NOT NULL,receipt FROM agenteam_knowledge.commands WHERE project_id=$1 AND command_name='move' AND command_key=$2`, o.project.String(), string(in.key)).Scan(&target, &user, &digest, &state, &requestNull, &planNull, &committed, &receiptRaw)
	o.dbMu.Unlock()
	if err != nil {
		return nil, err
	}
	var receipt kc.MutationReceipt
	if target != o.target.String() || user != o.actor.Details().UserID || digest != semantic.String() || state != "completed" || !requestNull || !planNull || !committed || json.Unmarshal(receiptRaw, &receipt) != nil || receipt.Validate() != nil || receipt.Command != kc.Move || !receipt.Changed || receipt.Document == nil {
		return nil, errors.New("original Move command/digest/receipt not committed")
	}
	doc := *receipt.Document
	// This isolated browser only moves the two HTTP seeds. With no concurrent
	// content author, unchanged version one is a persisted fixture fact, not a
	// Move HTTP precondition or a client rule rejecting a newer content version.
	if doc.ID != o.target || doc.ProjectID != o.project || doc.ObjectID.String() != o.objectID || doc.ContentVersion != 1 || !reflect.DeepEqual(doc.ParentDocumentID, in.input.TargetParentID) {
		return nil, errors.New("Move changed original content/target identity")
	}
	projectedRaw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var projected map[string]any
	var actual map[string]any
	if json.Unmarshal(projectedRaw, &projected) != nil || json.Unmarshal(raw, &actual) != nil {
		return nil, errors.New("Move receipt JSON invalid")
	}
	delete(projected, "object_id")
	if len(projected) != 12 || !reflect.DeepEqual(actual, map[string]any{"document": projected, "changed": true}) {
		return nil, errors.New("original HTTP Move differs from historical committed receipt")
	}
	for key, want := range o.original {
		if key != "parent_document_id" && key != "updated_at" && !reflect.DeepEqual(projected[key], want) {
			return nil, errors.New("Move changed an immutable document field")
		}
	}
	if err := o.currentRow(ctx, projected); err != nil {
		return nil, err
	}
	stable, err := o.snapshot(ctx, false)
	if err != nil || stable != o.stable {
		return nil, errors.New("Move changed content/publication/original Audit/Outbox facts")
	}
	return projected, nil
}
func (o *knowledgeMoveObservations) currentRow(ctx context.Context, doc map[string]any) error {
	o.dbMu.Lock()
	defer o.dbMu.Unlock()
	var parent *string
	var objectID, title string
	var version int64
	var updated time.Time
	if err := o.conn.QueryRow(ctx, `SELECT parent_document_id::text,current_object_id::text,title,content_version,updated_at FROM agenteam_knowledge.documents WHERE project_id=$1 AND id=$2 AND status='active'`, o.project.String(), o.target.String()).Scan(&parent, &objectID, &title, &version, &updated); err != nil {
		return err
	}
	var actualParent any
	if parent != nil {
		actualParent = *parent
	}
	instant, err := f.NewInstant(updated)
	if err != nil || actualParent != doc["parent_document_id"] || objectID != o.objectID || title != knowledgeMoveTitle || version != 1 || doc["content_version"] != "1" || instant.String() != doc["updated_at"] {
		return errors.New("current metadata differs from canonical document row")
	}
	return nil
}
func (o *knowledgeMoveObservations) currentRead(ctx context.Context, r *http.Request, tail *knowledgeMoveTail, raw []byte) error {
	var actual map[string]any
	if json.Unmarshal(raw, &actual) != nil {
		return errors.New("current read invalid JSON")
	}
	prefix := o.prefix()
	target := prefix + "/" + o.target.String()
	parentPath := prefix + "/" + o.parentID.String()
	doc := tail.document
	var want map[string]any
	kind := ""
	switch r.URL.Path {
	case target:
		want = map[string]any{"active": doc}
		kind = "get"
	case target + "/ancestors":
		items := []any{}
		if doc["parent_document_id"] != nil {
			items = append(items, o.parent)
		}
		want = map[string]any{"items": items}
		kind = "ancestors"
	case target + "/content":
		want = map[string]any{"document": doc, "text": map[string]any{"text": knowledgeMoveText, "next_byte_offset": fmt.Sprint(len(knowledgeMoveText)), "truncated": false}}
		kind = "content"
	case parentPath:
		want = map[string]any{"active": o.parent}
	case parentPath + "/ancestors":
		want = map[string]any{"items": []any{}}
	case parentPath + "/content":
		want = map[string]any{"document": o.parent, "text": map[string]any{"text": knowledgeMoveParentText, "next_byte_offset": fmt.Sprint(len(knowledgeMoveParentText)), "truncated": false}}
	case prefix + "/children":
		items := []any{}
		switch r.URL.Query().Get("parent_document_id") {
		case "null":
			items = append(items, o.parent)
			if doc["parent_document_id"] == nil {
				items = append(items, doc)
			}
			kind = "root-children"
		case o.parentID.String():
			if doc["parent_document_id"] == o.parentID.String() {
				items = append(items, doc)
			}
			kind = "parent-children"
		case o.target.String():
		default:
			return errors.New("foreign directory read")
		}
		sort.Slice(items, func(i, j int) bool {
			return items[i].(map[string]any)["title"].(string) < items[j].(map[string]any)["title"].(string)
		})
		want = map[string]any{"items": items}
	default:
		return nil
	}
	if !reflect.DeepEqual(actual, want) {
		return errors.New("independent current document/ancestor/directory/body differs")
	}
	if kind == "get" && o.currentRow(ctx, doc) != nil {
		return errors.New("independent current GET lacks canonical row proof")
	}
	o.mu.Lock()
	if o.reads[tail.stage] == nil {
		o.reads[tail.stage] = map[string]int{}
	}
	if kind != "" {
		o.reads[tail.stage][kind]++
	}
	o.mu.Unlock()
	return nil
}

func (o *knowledgeMoveObservations) capture(response *http.Response) error {
	o.mu.Lock()
	active := o.active
	o.mu.Unlock()
	if !active {
		return nil
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 2<<20 || response.ContentLength != int64(len(raw)) || response.StatusCode != http.StatusOK || !json.Valid(raw) || response.Header.Get("Cache-Control") != "no-store" {
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
		if in.kind == "move" {
			doc, err := o.commandFacts(response.Request.Context(), in, raw)
			if err != nil {
				return o.fail()
			}
			after, err := o.snapshot(response.Request.Context(), true)
			if err != nil {
				return o.fail()
			}
			o.mu.Lock()
			o.current = doc
			o.receipts = append(o.receipts, doc)
			o.after = after
			lost = o.mode == "move-lost"
			o.mu.Unlock()
		} else {
			o.mu.Lock()
			original, before := o.receipts[0], o.after
			o.mu.Unlock()
			var actual map[string]any
			if json.Unmarshal(raw, &actual) != nil || !reflect.DeepEqual(actual, map[string]any{"state": "committed", "receipt": map[string]any{"command": "move", "document": original, "changed": true}}) {
				return o.fail()
			}
			// Unlike Project Lookup, Knowledge recomputes the full original
			// Move intent. Both nullable parents and the same key were checked
			// at the original handler; this receipt remains historical only.
			after, err := o.snapshot(response.Request.Context(), true)
			if err != nil || before != after {
				return o.fail()
			}
			o.mu.Lock()
			o.lookupCompleted = true
			o.mu.Unlock()
		}
	} else if err := o.currentRead(response.Request.Context(), response.Request, tail, raw); err != nil {
		return o.fail()
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
		return &knowledgeMoveDrop{header: response.Header.Clone(), length: len(raw), record: record}
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}
func (o *knowledgeMoveObservations) writeProof(record map[string]any) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	rid, ok := record["request_id"].(string)
	if !ok {
		return errors.New("proof request identity absent")
	}
	return os.WriteFile(filepath.Join(o.evidence, "knowledge-move-"+o.mode+"-response-"+rid+".json"), raw, 0600)
}
func (o *knowledgeMoveObservations) proxyError(w http.ResponseWriter, _ *http.Request, err error) {
	var drop *knowledgeMoveDrop
	if !errors.As(err, &drop) {
		_ = o.fail()
		http.Error(w, "owned Knowledge Move proxy failed", 502)
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

func knowledgeMoveBrowser(t *testing.T, ctx context.Context, private, origin, dist, evidence, inputHash, mode string) map[string]any {
	t.Helper()
	harness, err := filepath.Abs("../../../tests/account-captcha-web")
	if err != nil {
		t.Fatal("browser harness path unavailable")
	}
	cmd := exec.CommandContext(ctx, "node", filepath.Join(harness, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(harness, "knowledge-move.config.js"))
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
	cmd.Env = append(cmd.Env, "TMPDIR="+private, "AGENTEAM_AUTH_WEB_ORIGIN="+origin, "AGENTEAM_AUTH_WEB_PRIVATE="+private, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_KNOWLEDGE_MOVE_WEB_DIST="+dist, "AGENTEAM_KNOWLEDGE_MOVE_WEB_EVIDENCE="+evidence, "AGENTEAM_KNOWLEDGE_MOVE_WEB_INPUT_HASH="+inputHash, "AGENTEAM_KNOWLEDGE_MOVE_WEB_CASE="+mode, "PLAYWRIGHT_NO_COPY_PROMPT=1")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal("locked KnowledgeMove Node could not start")
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
	t.Logf("KnowledgeMove Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	// Never print the browser's credential-bearing call log. The spec writes
	// fixed-stage safe failure diagnostics; the original child status is final.
	if err != nil || !regexp.MustCompile(`(?m)\b1 passed\b`).Match(output.Bytes()) {
		t.Fatal("KnowledgeMove locked browser did not actually pass its one case")
	}
	raw, err := os.ReadFile(filepath.Join(evidence, "knowledge-move-"+mode+".json"))
	if err != nil {
		t.Fatal("KnowledgeMove browser result missing")
	}
	if len(raw) > 2<<20 {
		t.Fatal("KnowledgeMove browser result too large")
	}
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil || result["input_hash"] != inputHash {
		t.Fatal("KnowledgeMove browser result not bound to frozen inputs")
	}
	return result
}
