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
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// One real, ordinary Owner creates and replaces plain/Markdown documents through the UI. The separate Project
// fixture never replaces the default root's deliberately unbound initializer.
// Static hosting is private test infrastructure, not production SPA delivery.
func TestKnowledgeTextWeb(t *testing.T) {
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
	dist := os.Getenv("AGENTEAM_KNOWLEDGE_TEXT_WEB_DIST")
	runtime := os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	evidence := os.Getenv("AGENTEAM_KNOWLEDGE_TEXT_WEB_EVIDENCE")
	inputHash := os.Getenv("AGENTEAM_KNOWLEDGE_TEXT_WEB_INPUT_HASH")
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
	private, err := os.MkdirTemp(runtime, "kt-")
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
	observations := &knowledgeTextObservations{evidence: evidence, inputHash: inputHash, origin: origin, tails: map[string]*knowledgeTextTail{}}
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
	blocked, err := projects.service.CreateProject(ctx, actor, meta(), pc.CreateProjectRequest{ProjectID: blockedID, Name: "knowledge-text", Description: "Default initializer stays unbound"})
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.DependencyUnbound || fault.CommitState != f.NotCommitted || blocked.State != "" || blocked.Project != nil || blocked.Operation != nil {
		t.Fatal("default Project creation did not remain unbound/not_committed")
	}
	conn := db.Connect(t)
	var facts int
	check("default unbound zero facts", conn.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_project.projects WHERE id=$1 OR (owner_user_id=$2 AND normalized_name='knowledge-text'))+
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
	created, createErr := fixture.CreateProject(ctx, actor, meta(), pc.CreateProjectRequest{ProjectID: projectID, Name: "knowledge-text", Description: "Existing documents for the bounded UI read"})
	drainErr := fixtureOwner.Drain(ctx)
	check("test-only real Project", createErr)
	check("test-only actual Drain", drainErr)
	if !fixtureOwner.Joined() || created.Validate() != nil || created.State != pc.CreationReady || created.Project == nil || created.Project.ID != projectID {
		t.Fatal("isolated real Project did not confirm and join")
	}

	observations.mu.Lock()
	observations.project = projectID.String()
	observations.host = listener.Addr().String()
	observations.verify = func(c context.Context, in knowledgeTextIntent, raw []byte) error {
		source, err := kc.NewTextSource(in.media, in.text)
		if err != nil {
			return err
		}
		command := f.CommandMeta{RequestID: in.rid, IdempotencyKey: in.key}
		var digest f.Digest
		kind := kc.Create
		if in.kind == "create" {
			digest, err = kc.CreateDigest(actor, command, kc.CreateRequest{ProjectID: projectID, DocumentID: in.target, ParentDocumentID: in.parent, Title: in.title}, source)
		} else {
			kind = kc.Update
			version := f.Version(1)
			command.ExpectedVersion = &version
			digest, err = kc.UpdateDigest(actor, command, projectID, in.target, kc.UpdateRequest{ReplaceSource: true}, &source)
		}
		if err != nil {
			return err
		}
		found, err := documents.service.LookupCommand(c, actor, kc.LookupRequest{ProjectID: projectID, Command: kind, Key: in.key, SemanticDigest: digest})
		if err != nil {
			return err
		}
		version := f.Version(1)
		if in.kind == "update" {
			version = 2
		}
		if found.Validate() != nil || found.State != kc.Committed || found.Receipt == nil || !found.Receipt.Changed || found.Receipt.Document == nil {
			return errors.New("original text command not committed")
		}
		document := *found.Receipt.Document
		var result struct {
			Document struct {
				ID      string  `json:"id"`
				Project string  `json:"project_id"`
				Title   string  `json:"title"`
				Version string  `json:"content_version"`
				Media   string  `json:"media_type"`
				Parent  *string `json:"parent_document_id"`
				Source  string  `json:"source_kind"`
			} `json:"document"`
		}
		if json.Unmarshal(raw, &result) != nil || document.ID != in.target || document.ProjectID != projectID || document.ContentVersion != version || document.MediaType != in.media || result.Document.ID != in.target.String() || result.Document.Project != projectID.String() || result.Document.Version != fmt.Sprint(version) || result.Document.Media != in.media || result.Document.Source != "text" || result.Document.Title != document.Title {
			return errors.New("original text receipt differs from committed document")
		}
		if (document.ParentDocumentID == nil) != (in.parent == nil) || document.ParentDocumentID != nil && *document.ParentDocumentID != *in.parent || (result.Document.Parent == nil) != (in.parent == nil) || result.Document.Parent != nil && *result.Document.Parent != in.parent.String() {
			return errors.New("original text parent differs")
		}
		if in.kind == "create" && document.Title != in.title {
			return errors.New("original text title differs")
		}
		return nil
	}
	observations.active = true
	observations.mu.Unlock()
	payload := map[string]any{"mode": "create-and-replace", "cookie": cookie, "project_id": projectID.String(), "route": "/" + username + "/knowledge-text/knowledge", "plain": knowledgeTextPlain, "plain_updated": knowledgeTextPlainUpdated, "markdown": knowledgeTextMarkdown, "markdown_updated": knowledgeTextMarkdownUpdated}
	raw, err := json.Marshal(payload)
	check("private material", err)
	check("private material file", os.WriteFile(filepath.Join(private, "knowledge-text-material.json"), raw, 0600))
	clear(raw)
	result := knowledgeTextBrowser(t, ctx, private, origin, dist, evidence, inputHash)
	if result["completed"] != true {
		t.Fatal("Knowledge text browser did not complete")
	}
	observations.mu.Lock()
	observeErr, writes, observed := observations.err, len(observations.intents), observations.count
	observations.mu.Unlock()
	if observeErr != nil || writes != 4 || result["original_requests"] != float64(observed) || result["all_observed"] != true || result["lookups"] != float64(0) {
		t.Fatal("Knowledge text original request proofs incomplete")
	}
	var documentRows, commands, readers, changes int
	check("text committed facts", conn.QueryRow(ctx, `SELECT
  (SELECT count(*) FROM agenteam_knowledge.documents WHERE project_id=$1 AND content_version=2 AND source_kind='text'),
  (SELECT count(*) FROM agenteam_knowledge.commands WHERE project_id=$1),
  (SELECT count(*) FROM agenteam_object.object_leases WHERE owner_kind='reader' AND state='active'),
  (SELECT count(*) FROM agenteam_knowledge.command_events e JOIN agenteam_knowledge.commands c ON c.id=e.command_id WHERE c.project_id=$1 AND e.event_type='knowledge.content_changed')`, projectID.String()).Scan(&documentRows, &commands, &readers, &changes))
	if documentRows != 2 || commands != 4 || readers != 0 || changes != 4 {
		t.Fatal("Knowledge text persisted facts incomplete")
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
	t.Log("Knowledge text browser chain: original Node Wait, proxy/root join, domain retirement and guard facts complete; production initializer remains unbound")
}

const knowledgeTextPlain = "原始纯文本 世界\n<script>only text</script>\n"
const knowledgeTextPlainUpdated = "更新纯文本 🌍\nSecond complete line.\n"
const knowledgeTextMarkdown = "# Markdown 子文档\n\n**原始内容** and `<script>`\n"
const knowledgeTextMarkdownUpdated = "# Markdown 更新\n\n- 第一行\n- second 🌍\n"

type knowledgeTextIntent struct {
	kind, media, text, title string
	target                   kc.DocumentID
	parent                   *kc.DocumentID
	rid                      f.ID[f.Request]
	key                      f.IdempotencyKey
	bodyDigest, keyDigest    [sha256.Size]byte
}
type knowledgeTextTail struct {
	done        chan struct{}
	returned    bool
	method, uri string
	intent      *knowledgeTextIntent
}
type knowledgeTextObservations struct {
	mu                                         sync.Mutex
	evidence, inputHash, project, origin, host string
	active                                     bool
	count                                      int
	err                                        error
	tails                                      map[string]*knowledgeTextTail
	intents                                    []knowledgeTextIntent
	verify                                     func(context.Context, knowledgeTextIntent, []byte) error
}

func (o *knowledgeTextObservations) fail() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.err == nil {
		o.err = errors.New("closed Knowledge text observation failed")
	}
	return o.err
}
func (o *knowledgeTextObservations) observeHandler(next http.Handler) http.Handler {
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
		if r.Method != http.MethodGet {
			prefix := "/api/v1/projects/" + o.project + "/knowledge/documents/"
			suffix := strings.TrimPrefix(r.URL.Path, prefix)
			parts := strings.Split(suffix, "/")
			target, err := f.ParseID[kc.Document](parts[0])
			if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, prefix) || len(parts) != 2 || err != nil || target.String() != parts[0] || r.URL.RawQuery != "" || r.Header.Get("Origin") != o.origin || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("X-CSRF-Token") == "" || r.Header.Get("Cookie") == "" {
				_ = o.fail()
				http.Error(w, "closed text write", 502)
				return
			}
			raw, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
			closed := r.Body.Close()
			defer clear(raw)
			key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
			if err != nil || closed != nil || len(raw) > 1<<20 || r.ContentLength != int64(len(raw)) || key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 {
				_ = o.fail()
				http.Error(w, "original text body", 502)
				return
			}
			o.mu.Lock()
			index := len(o.intents)
			old := append([]knowledgeTextIntent(nil), o.intents...)
			o.mu.Unlock()
			in := knowledgeTextIntent{target: target, rid: rid, key: key, bodyDigest: sha256.Sum256(raw), keyDigest: sha256.Sum256([]byte(key))}
			valid := index < 4
			for _, v := range old {
				valid = valid && v.key != key
			}
			if index%2 == 0 {
				var body struct {
					Parent *kc.DocumentID `json:"parent_document_id"`
					Title  string         `json:"title"`
					Media  string         `json:"media_type"`
					Text   string         `json:"text"`
				}
				valid = valid && parts[1] == "create-text" && skillWebDecode(raw, &body) == nil
				in.kind, in.parent, in.title, in.media, in.text = "create", body.Parent, body.Title, body.Media, body.Text
				if index == 0 {
					valid = valid && body.Parent == nil && body.Title == "纯文本笔记" && body.Media == kc.PlainText && body.Text == knowledgeTextPlain
				}
				if index == 2 {
					valid = valid && body.Parent != nil && *body.Parent == old[0].target && target != old[0].target && body.Title == "Markdown 子文档" && body.Media == kc.Markdown && body.Text == knowledgeTextMarkdown
				}
			} else {
				var body struct {
					Version string `json:"expected_version"`
					Media   string `json:"media_type"`
					Text    string `json:"text"`
				}
				valid = valid && parts[1] == "replace-text" && skillWebDecode(raw, &body) == nil && body.Version == "1"
				in.kind, in.media, in.text = "update", body.Media, body.Text
				if index == 1 || index == 3 {
					in.parent = old[index-1].parent
					valid = valid && target == old[index-1].target
				}
				if index == 1 {
					valid = valid && body.Media == kc.PlainText && body.Text == knowledgeTextPlainUpdated
				}
				if index == 3 {
					valid = valid && body.Media == kc.Markdown && body.Text == knowledgeTextMarkdownUpdated
				}
			}
			if !valid {
				_ = o.fail()
				http.Error(w, "text intent", 502)
				return
			}
			tail.intent = &in
			o.mu.Lock()
			o.intents = append(o.intents, in)
			o.mu.Unlock()
			r.Body = io.NopCloser(bytes.NewReader(raw))
		}
		o.mu.Lock()
		if o.tails[rid.String()] != nil || len(o.tails) >= 96 {
			o.mu.Unlock()
			_ = o.fail()
			http.Error(w, "duplicate text request", 502)
			return
		}
		o.tails[rid.String()] = tail
		o.mu.Unlock()
		defer close(tail.done)
		next.ServeHTTP(w, r)
		tail.returned = true
	})
}
func (o *knowledgeTextObservations) capture(response *http.Response) error {
	o.mu.Lock()
	active := o.active
	o.mu.Unlock()
	if !active {
		return nil
	}
	if response.StatusCode != 200 {
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
	record := map[string]any{"input_hash": o.inputHash, "request_id": rid.String(), "method": tail.method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(tail.uri))), "status": response.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true}
	if tail.intent != nil {
		if o.verify == nil || o.verify(response.Request.Context(), *tail.intent, raw) != nil {
			return o.fail()
		}
		record["kind"] = tail.intent.kind
		record["request_body_sha256"] = fmt.Sprintf("%x", tail.intent.bodyDigest)
		record["idempotency_key_sha256"] = fmt.Sprintf("%x", tail.intent.keyDigest)
		record["committed_verified"] = true
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return o.fail()
	}
	if os.WriteFile(filepath.Join(o.evidence, "knowledge-text-create-and-replace-response-"+rid.String()+".json"), encoded, 0600) != nil {
		return o.fail()
	}
	o.mu.Lock()
	o.count++
	o.mu.Unlock()
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}

func knowledgeTextBrowser(t *testing.T, ctx context.Context, private, origin, dist, evidence, inputHash string) map[string]any {
	t.Helper()
	harness, err := filepath.Abs("../../../tests/account-captcha-web")
	if err != nil {
		t.Fatal("browser harness path unavailable")
	}
	cmd := exec.CommandContext(ctx, "node", filepath.Join(harness, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(harness, "knowledge-text.config.js"))
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
	cmd.Env = append(cmd.Env, "TMPDIR="+private, "AGENTEAM_AUTH_WEB_ORIGIN="+origin, "AGENTEAM_AUTH_WEB_PRIVATE="+private, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_KNOWLEDGE_TEXT_WEB_DIST="+dist, "AGENTEAM_KNOWLEDGE_TEXT_WEB_EVIDENCE="+evidence, "AGENTEAM_KNOWLEDGE_TEXT_WEB_INPUT_HASH="+inputHash, "AGENTEAM_KNOWLEDGE_TEXT_WEB_CASE=create-and-replace", "PLAYWRIGHT_NO_COPY_PROMPT=1")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal("locked KnowledgeText Node could not start")
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
	t.Logf("KnowledgeText Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	// Never print the browser's credential-bearing call log. The spec writes
	// fixed-stage safe failure diagnostics; the original child status is final.
	if err != nil || !regexp.MustCompile(`(?m)\b1 passed\b`).Match(output.Bytes()) {
		t.Fatal("KnowledgeText locked browser did not actually pass its one case")
	}
	raw, err := os.ReadFile(filepath.Join(private, "knowledge-text-result.json"))
	if err != nil {
		t.Fatal("KnowledgeText browser result missing")
	}
	if len(raw) > 16<<10 {
		t.Fatal("KnowledgeText browser result too large")
	}
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil || result["input_hash"] != inputHash {
		t.Fatal("KnowledgeText browser result not bound to frozen inputs")
	}
	return result
}
