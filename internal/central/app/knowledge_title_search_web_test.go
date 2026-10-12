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
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// The ordinary Owner searches an unloaded child by its literal title and reads
// its current canonical content through the original default App.
// Static hosting is private test infrastructure, not production SPA delivery.
func TestKnowledgeTitleSearchWeb(t *testing.T) {
	t.Run(knowledgeTitleSearchMode, knowledgeTitleSearchWeb)
}

func knowledgeTitleSearchWeb(t *testing.T) {
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
	dist := os.Getenv("AGENTEAM_KNOWLEDGE_TITLE_SEARCH_WEB_DIST")
	runtime := os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	evidence := os.Getenv("AGENTEAM_KNOWLEDGE_TITLE_SEARCH_WEB_EVIDENCE")
	inputHash := os.Getenv("AGENTEAM_KNOWLEDGE_TITLE_SEARCH_WEB_INPUT_HASH")
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
	private, err := os.MkdirTemp(runtime, "kts-")
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
	observations := &knowledgeTitleSearchObservations{knowledgeTextObservations: &knowledgeTextObservations{evidence: evidence, inputHash: inputHash, origin: origin, tails: map[string]*knowledgeTextTail{}}}
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
	email, username := "title-owner@example.com", "title-owner"
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
	created, createErr := projects.service.CreateProject(ctx, actor, meta(), pc.CreateProjectRequest{ProjectID: projectID, Name: "title-search", Description: "Existing documents for the bounded UI read"})
	check("default real Project", createErr)
	if created.Validate() != nil || created.State != pc.CreationReady || created.Project == nil || created.Project.ID != projectID {
		t.Fatal("default real Project did not confirm")
	}

	parentID, documentID := guardID[kc.Document](t), guardID[kc.Document](t)
	prefix := "/api/v1/projects/" + projectID.String() + "/knowledge/documents"
	csrf := knowledgeWebString(t, session, "csrf_token")
	parentResult := setup.call(ownerClient, "POST", prefix+"/"+parentID.String()+"/create-text", map[string]any{"parent_document_id": nil, "title": knowledgeTitleSearchParentTitle, "media_type": kc.PlainText, "text": knowledgeTitleSearchParentText}, csrf, true, 200)
	documentResult := setup.call(ownerClient, "POST", prefix+"/"+documentID.String()+"/create-text", map[string]any{"parent_document_id": parentID.String(), "title": knowledgeTitleSearchTitle, "media_type": kc.PlainText, "text": knowledgeTitleSearchText}, csrf, true, 200)
	parent, parentOK := parentResult["document"].(map[string]any)
	document, documentOK := documentResult["document"].(map[string]any)
	if !parentOK || !documentOK || len(parentResult) != 1 || len(documentResult) != 1 || len(parent) != 12 || len(document) != 12 || parent["id"] != parentID.String() || parent["parent_document_id"] != nil || parent["title"] != knowledgeTitleSearchParentTitle || document["id"] != documentID.String() || document["parent_document_id"] != parentID.String() || document["title"] != knowledgeTitleSearchTitle {
		t.Fatal("formal text seeds lost their safe document identities")
	}
	for _, value := range []map[string]any{parent, document} {
		if value["project_id"] != projectID.String() || value["content_version"] != "1" || value["source_kind"] != "text" || value["media_type"] != kc.PlainText || value["status"] != "active" || !reflect.DeepEqual(value["created_by"], map[string]any{"kind": "human", "user_id": actor.Details().UserID}) {
			t.Fatal("formal text seeds lost their current Owner/content metadata")
		}
	}
	var rows, commands, changes, published int
	check("formal seed facts", conn.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_knowledge.documents WHERE project_id=$1 AND content_version=1 AND source_kind='text' AND media_type='text/plain' AND status='active'),
 (SELECT count(*) FROM agenteam_knowledge.commands WHERE project_id=$1 AND command_name='create' AND state='completed' AND committed_at IS NOT NULL),
 (SELECT count(*) FROM agenteam_knowledge.command_events WHERE project_id=$1 AND event_type='knowledge.content_changed'),
 (SELECT count(*) FROM agenteam_knowledge.publications WHERE project_id=$1 AND phase='published' AND source IS NULL AND source_lease_id IS NULL)`, projectID.String()).Scan(&rows, &commands, &changes, &published))
	if rows != 2 || commands != 2 || changes != 2 || published != 2 {
		t.Fatal("formal seed commands and object publications incomplete")
	}
	persisted := func() string {
		var value string
		check("read-only persisted facts", conn.QueryRow(ctx, `SELECT jsonb_build_object(
 'project',(SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1),
 'documents',(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM agenteam_knowledge.documents d WHERE project_id=$1),
 'commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_knowledge.commands c WHERE project_id=$1),
 'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_knowledge.command_events e WHERE project_id=$1),
 'publications',(SELECT jsonb_agg(to_jsonb(p) ORDER BY command_id) FROM agenteam_knowledge.publications p WHERE project_id=$1),
 'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a WHERE project_id=$1),
 'outbox',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_outbox.events e WHERE project_id=$1))::text`, projectID.String()).Scan(&value))
		return value
	}
	before := persisted()
	observations.mu.Lock()
	observations.project, observations.host = projectID.String(), listener.Addr().String()
	observations.parent, observations.document = parent, document
	observations.active = true
	observations.mu.Unlock()
	payload := map[string]any{"mode": knowledgeTitleSearchMode, "cookie": cookie, "project_id": projectID.String(), "route": "/" + username + "/title-search/knowledge", "parent_id": parentID.String(), "document_id": documentID.String(), "parent_title": knowledgeTitleSearchParentTitle, "title": knowledgeTitleSearchTitle, "text": knowledgeTitleSearchText, "query": knowledgeTitleSearchQuery, "miss_query": knowledgeTitleSearchMiss, "parent": parent, "document": document, "input_hash": inputHash}
	raw, err := json.Marshal(payload)
	check("private material", err)
	check("private material file", os.WriteFile(filepath.Join(private, "knowledge-title-search-material.json"), raw, 0600))
	clear(raw)
	result := knowledgeTitleSearchBrowser(t, ctx, private, origin, dist, evidence, inputHash)
	observations.mu.Lock()
	observeErr, observed, tails := observations.err, observations.count, len(observations.tails)
	searches, searchCompleted, roots, gets, ancestors, contents := observations.searches, observations.searchCompleted, observations.roots, observations.gets, observations.ancestors, observations.contents
	observations.mu.Unlock()
	if observeErr != nil || observed != tails || searches != 2 || searchCompleted != 2 || roots < 1 || gets < 1 || ancestors < 1 || contents < 1 || result["completed"] != true || result["original_requests"] != float64(observed) || result["all_observed"] != true || result["lookups"] != float64(0) {
		t.Fatal("Knowledge title search original response/consumer proofs incomplete")
	}
	if persisted() != before {
		t.Fatal("title search or current reads changed committed business facts")
	}
	var readers, claims int
	check("original read ownership closed", conn.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_object.object_leases WHERE owner_kind='reader' AND state='active'),
 (SELECT count(*) FROM agenteam_knowledge.work_claims WHERE project_id=$1 AND phase='active')`, projectID.String()).Scan(&readers, &claims))
	if readers != 0 || claims != 0 {
		t.Fatal("Knowledge search/current content left an active original owner")
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
	t.Log("Knowledge title search browser chain: original Node Wait, proxy/root join, domain retirement and guard facts complete; default creation uses the actual initializer; destructive lifecycle remains unbound")
}

const knowledgeTitleSearchMode = "literal-title-and-current-read"
const knowledgeTitleSearchParentTitle = "标题检索目录"
const knowledgeTitleSearchParentText = "目录正文 %_! 不是标题命中。\n"
const knowledgeTitleSearchTitle = "操作 %_! 指南 中文🙂"
const knowledgeTitleSearchText = "原文 <%_!> 不执行 HTML。\n第二行🙂"
const knowledgeTitleSearchQuery = "%_!"
const knowledgeTitleSearchMiss = "没有匹配的标题 019a"

// Reuse the original root-handler RequestID/Host and actual-return observation.
// This profile accepts browser reads only; formal seed writes precede activation.
type knowledgeTitleSearchObservations struct {
	*knowledgeTextObservations
	parent, document                                            map[string]any
	searches, searchCompleted, roots, gets, ancestors, contents int
}

func (o *knowledgeTitleSearchObservations) observeHandler(next http.Handler) http.Handler {
	return o.knowledgeTextObservations.observeHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.mu.Lock()
		active := o.active
		o.mu.Unlock()
		if active && o.observeRead(r) != nil {
			_ = o.fail()
			http.Error(w, "closed title search read", http.StatusBadGateway)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func (o *knowledgeTitleSearchObservations) observeRead(r *http.Request) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if r.Method != http.MethodGet || r.ContentLength > 0 || len(r.TransferEncoding) != 0 || r.Header.Get("Idempotency-Key") != "" {
		return errors.New("browser title search attempted a write/body/key")
	}
	project := "/api/v1/projects/" + o.project
	prefix := project + "/knowledge/documents"
	target := prefix + "/" + o.document["id"].(string)
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || r.URL.ForceQuery {
		return errors.New("title search query did not parse")
	}
	global := map[string]bool{"/api/v1/auth/bootstrap": true, "/api/v1/session": true, "/api/v1/me": true, "/api/v1/me/preferences": true, "/api/v1/projects": true, "/api/v1/projects/resolve": true}
	if global[r.URL.Path] {
		return nil
	}
	switch r.URL.Path {
	case project:
		if len(query) == 0 {
			return nil
		}
	case prefix + "/search-titles":
		want := knowledgeTitleSearchQuery
		if o.searches == 1 {
			want = knowledgeTitleSearchMiss
		}
		if o.searches >= 2 || o.roots == 0 || !reflect.DeepEqual(query, url.Values{"title_query": {want}, "limit": {"50"}}) || o.searches == 1 && (o.gets == 0 || o.ancestors == 0 || o.contents == 0) {
			break
		}
		o.searches++
		return nil
	case prefix + "/children":
		parent := query.Get("parent_document_id")
		if !reflect.DeepEqual(query, url.Values{"parent_document_id": {parent}, "limit": {"50"}}) {
			break
		}
		if parent == "null" {
			o.roots++
			return nil
		}
		// No child list may be loaded before the complete literal search hit.
		if o.searchCompleted >= 1 && (parent == o.parent["id"] || parent == o.document["id"]) {
			return nil
		}
	case target:
		if len(query) == 0 && o.searchCompleted >= 1 {
			o.gets++
			return nil
		}
	case target + "/ancestors":
		if len(query) == 0 && o.searchCompleted >= 1 {
			o.ancestors++
			return nil
		}
	case target + "/content":
		limit := query.Get("max_bytes")
		if o.searchCompleted >= 1 && (limit == "65536" || limit == "1048576") && reflect.DeepEqual(query, url.Values{"byte_offset": {"0"}, "max_bytes": {limit}}) {
			o.contents++
			return nil
		}
	}
	return errors.New("undeclared title search or current-read endpoint")
}

func (o *knowledgeTitleSearchObservations) current(r *http.Request, raw []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	prefix := "/api/v1/projects/" + o.project + "/knowledge/documents"
	target := prefix + "/" + o.document["id"].(string)
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return errors.New("original current read is not complete JSON")
	}
	var want map[string]any
	switch r.URL.Path {
	case prefix + "/children":
		items := []any{}
		switch r.URL.Query().Get("parent_document_id") {
		case "null":
			items = append(items, o.parent)
		case o.parent["id"].(string):
			items = append(items, o.document)
		case o.document["id"].(string):
		default:
			return errors.New("foreign original child list")
		}
		want = map[string]any{"items": items}
	case prefix + "/search-titles":
		items := []any{}
		if r.URL.Query().Get("title_query") == knowledgeTitleSearchQuery {
			items = append(items, map[string]any{"document": o.document, "ancestors": []any{o.parent}})
		} else if r.URL.Query().Get("title_query") != knowledgeTitleSearchMiss {
			return errors.New("search changed literal original query")
		}
		want = map[string]any{"items": items}
	case target:
		want = map[string]any{"active": o.document}
	case target + "/ancestors":
		want = map[string]any{"items": []any{o.parent}}
	case target + "/content":
		want = map[string]any{"document": o.document, "text": map[string]any{"text": knowledgeTitleSearchText, "next_byte_offset": fmt.Sprint(len(knowledgeTitleSearchText)), "truncated": false}}
	default:
		return nil
	}
	if !reflect.DeepEqual(value, want) {
		return errors.New("title search/current content differs from canonical seed")
	}
	if r.URL.Path == prefix+"/search-titles" {
		o.searchCompleted++
	}
	return nil
}

func (o *knowledgeTitleSearchObservations) capture(response *http.Response) error {
	o.mu.Lock()
	active := o.active
	o.mu.Unlock()
	if !active {
		return nil
	}
	if response.StatusCode != http.StatusOK {
		return o.fail()
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	closed := response.Body.Close()
	if err != nil || closed != nil || len(raw) > 2<<20 || response.ContentLength != int64(len(raw)) || !json.Valid(raw) || response.Header.Get("Cache-Control") != "no-store" {
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
	record := map[string]any{"input_hash": o.inputHash, "request_id": rid.String(), "method": tail.method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(tail.uri))), "status": response.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true, "request_host_matches": true}
	encoded, err := json.Marshal(record)
	if err != nil || os.WriteFile(filepath.Join(o.evidence, "knowledge-title-search-"+knowledgeTitleSearchMode+"-response-"+rid.String()+".json"), encoded, 0600) != nil {
		return o.fail()
	}
	o.mu.Lock()
	o.count++
	o.mu.Unlock()
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}

func knowledgeTitleSearchBrowser(t *testing.T, ctx context.Context, private, origin, dist, evidence, inputHash string) map[string]any {
	t.Helper()
	harness, err := filepath.Abs("../../../tests/account-captcha-web")
	if err != nil {
		t.Fatal("browser harness path unavailable")
	}
	cmd := exec.CommandContext(ctx, "node", filepath.Join(harness, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(harness, "knowledge-title-search.config.js"))
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
	cmd.Env = append(cmd.Env, "TMPDIR="+private, "AGENTEAM_AUTH_WEB_ORIGIN="+origin, "AGENTEAM_AUTH_WEB_PRIVATE="+private, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_KNOWLEDGE_TITLE_SEARCH_WEB_DIST="+dist, "AGENTEAM_KNOWLEDGE_TITLE_SEARCH_WEB_EVIDENCE="+evidence, "AGENTEAM_KNOWLEDGE_TITLE_SEARCH_WEB_INPUT_HASH="+inputHash, "AGENTEAM_KNOWLEDGE_TITLE_SEARCH_WEB_CASE=literal-title-and-current-read", "PLAYWRIGHT_NO_COPY_PROMPT=1")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal("locked KnowledgeTitleSearch Node could not start")
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
	t.Logf("KnowledgeTitleSearch Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	// Never print the browser's credential-bearing call log. The spec writes
	// fixed-stage safe failure diagnostics; the original child status is final.
	if err != nil || !regexp.MustCompile(`(?m)\b1 passed\b`).Match(output.Bytes()) {
		t.Fatal("KnowledgeTitleSearch locked browser did not actually pass its one case")
	}
	raw, err := os.ReadFile(filepath.Join(private, "knowledge-title-search-result.json"))
	if err != nil {
		t.Fatal("KnowledgeTitleSearch browser result missing")
	}
	if len(raw) > 16<<10 {
		t.Fatal("KnowledgeTitleSearch browser result too large")
	}
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil || result["input_hash"] != inputHash {
		t.Fatal("KnowledgeTitleSearch browser result not bound to frozen inputs")
	}
	return result
}
