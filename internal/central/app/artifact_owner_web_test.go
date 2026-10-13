//go:build integration

package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/artifact"
	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// Formal Service seeds precede browser activation. Every browser Artifact call
// reaches the original installed App handler, Service, authority and reader.
// Private static hosting and one pre-handler loading barrier are test-only.
func TestArtifactOwnerWeb(t *testing.T) {
	t.Run("catalog-and-bounded-read", artifactOwnerWeb)
}

func artifactOwnerWeb(t *testing.T) {
	const prefix = "AGENTEAM_ARTIFACT_OWNER_WEB"
	dist, runtime, evidence, inputHash := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv(prefix+"_EVIDENCE"), os.Getenv(prefix+"_INPUT_HASH")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(inputHash) {
		t.Fatal("frozen Artifact browser inputs required")
	}
	if info, err := os.Lstat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("frozen Artifact index missing")
	}
	evidence = filepath.Join(evidence, t.Name())
	workAppCheck(t, "artifact-web-evidence-parent", os.MkdirAll(filepath.Dir(evidence), 0700))
	workAppCheck(t, "artifact-web-fresh-evidence", os.Mkdir(evidence, 0700))
	private, err := os.MkdirTemp(runtime, "aw-")
	workAppCheck(t, "artifact-web-private", err)
	t.Cleanup(func() { workAppCheck(t, "artifact-web-private-remove", os.RemoveAll(private)) })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	workAppCheck(t, "artifact-web-listener", err)
	t.Cleanup(func() { _ = listener.Close() })
	origin := "http://" + listener.Addr().String()
	observer := &artifactWebObserver{host: listener.Addr().String(), private: private, evidence: evidence, inputHash: inputHash, tails: map[string]*artifactWebTail{}}
	// Registered before the App fixture: capture the final original handler tails
	// only after proxy/root cleanup, on success and on every t.Fatal path.
	t.Cleanup(func() { observer.saveDiagnostic(t) })
	// Existing nil-default seam is used before run. No live config/authority is
	// replaced. The old Work fixture's HTTP request helper is deliberately unused.
	v := newWorkAppFixtureWith(t, func(root *modelRootApp, d *dependencies) {
		root.cfg = guardConfiguration(t, append(guardEnvironment(t, root.db), "AGENTEAM_CENTRAL_PUBLIC_ORIGIN="+origin, "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s"))
		original := d.bind
		d.bind = func(ctx context.Context, cfg config.Config, store database, owned *resources, deps *dependencies) error {
			if err := original(ctx, cfg, store, owned, deps); err != nil {
				return err
			}
			assembly, ok := owned.accounts().(*accountAssembly)
			if !ok {
				return errors.New("original Artifact account assembly absent")
			}
			assembly.mu.Lock()
			assembly.handler = observer.handler(assembly.handler)
			assembly.mu.Unlock()
			return nil
		}
	})
	v.assembly.mu.Lock()
	installed := v.assembly.artifacts
	v.assembly.mu.Unlock()
	if installed == nil || installed.pair == nil {
		t.Fatal("original Artifact pair absent")
	}
	service := installed.pair.Service()
	workAppCheck(t, "artifact-web-original-object", service.RequireProjectStopBinding(v.objects.service))
	backend, err := url.Parse(v.address)
	workAppCheck(t, "artifact-web-backend", err)
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport, proxy.ErrorLog = transport, log.New(io.Discard, "", 0)
	proxy.ModifyResponse = observer.capture
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		_ = observer.fail("proxy_error")
		observer.mu.Lock()
		observer.proxyErrors++
		observer.mu.Unlock()
		http.Error(w, "owned Artifact observation failed", 502)
	}
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			proxy.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			skillWebServeAsset(w, r, dist)
			return
		}
		http.ServeFile(w, r, filepath.Join(dist, "index.html"))
	})}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			_ = os.WriteFile(filepath.Join(private, "release-loading"), []byte("cleanup"), 0600)
			if err := server.Shutdown(v.ctx); err != nil {
				t.Error("Artifact proxy exceeded original shutdown budget")
				_ = server.Close()
			}
			if err := <-served; err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Error("Artifact proxy original Serve failed")
			}
			transport.CloseIdleConnections()
		})
	}
	t.Cleanup(stop)
	check := func(stage string, e error) { t.Helper(); workAppCheck(t, "artifact-web-"+stage, e) }
	setup := &knowledgeWebSetup{t: t, ctx: v.ctx, origin: origin, cfg: v.root.cfg, check: check}
	t.Cleanup(setup.close)
	owner, cookie := artifactWebHuman(t, v, setup, origin)
	project := artifactWebProject(t, v, owner, "artifact-browser")
	other := artifactWebProject(t, v, owner, "artifact-browser-other")
	// The existing bootstrap user's Project is a true foreign Owner scope.
	foreign := v.a
	text := strings.Repeat("a", 8190) + "🙂<script>window.artifactExecuted=true</script>\n原始文字。\n"
	long := artifactWebCreate(t, v, service, owner, project, "Literal %_! 中文🙂.txt", "text/plain", text)
	image := artifactWebCreate(t, v, service, owner, project, "image-reference.gif", "image/gif", "GIF89a"+strings.Repeat("\x00", 32))
	file := artifactWebCreate(t, v, service, owner, project, "opaque-reference.bin", "application/octet-stream", "\x00\x01\x02\x03")
	rows := []ac.Metadata{long, image, file}
	for n := 0; n < 49; n++ {
		rows = append(rows, artifactWebCreate(t, v, service, owner, project, fmt.Sprintf("catalog-note-%02d.txt", n), "text/plain", fmt.Sprintf("Synthetic note %02d\n", n)))
	}
	otherItem := artifactWebCreate(t, v, service, owner, other, "Other project note.txt", "text/plain", "The other owned project remains isolated.\n")
	foreignItem := artifactWebCreate(t, v, service, v.actor, foreign, long.Details().Name, "text/plain", "Foreign Owner content must never be shown.\n")
	sort.Slice(rows, func(a, b int) bool {
		x, y := rows[a].Details(), rows[b].Details()
		if !x.CreatedAt.Time().Equal(y.CreatedAt.Time()) {
			return x.CreatedAt.Time().After(y.CreatedAt.Time())
		}
		return x.Reference.ArtifactID.String() > y.Reference.ArtifactID.String()
	})
	if len(rows) != 52 || rows[51].Details().Reference != long.Details().Reference {
		t.Fatal("real second-page Artifact target missing")
	}
	before := artifactWebFixedFacts(t, v, project, other, foreign)
	observer.mu.Lock()
	observer.active, observer.loadingPath = true, artifactWebContentPath(long.Details().Reference)
	observer.mu.Unlock()
	safeRows := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		safeRows = append(safeRows, artifactWebMetadata(row))
	}
	payload := map[string]any{"mode": "catalog-and-bounded-read", "origin": origin, "cookie": cookie, "foreign_cookie": v.cookie, "project_id": project.String(), "other_project_id": other.String(), "foreign_project_id": foreign.String(), "route": "/artifact-owner/artifact-browser/artifacts", "other_route": "/artifact-owner/artifact-browser-other/artifacts", "rows": safeRows, "text_artifact": artifactWebMetadata(long), "image_artifact": artifactWebMetadata(image), "file_artifact": artifactWebMetadata(file), "other_artifact": artifactWebMetadata(otherItem), "foreign_artifact": artifactWebMetadata(foreignItem), "text": text, "first_text": strings.Repeat("a", 8190), "second_text": strings.TrimPrefix(text, strings.Repeat("a", 8190)), "input_hash": inputHash}
	raw, err := json.Marshal(payload)
	check("private-material", err)
	check("private-material-write", os.WriteFile(filepath.Join(private, "artifact-owner-material.json"), raw, 0600))
	clear(raw)
	result := artifactWebBrowser(t, v.ctx, private, origin, dist, evidence, inputHash)
	observer.mu.Lock()
	observed, failed, lists, reads, denials, loading := observer.count, observer.failed, observer.lists, observer.reads, observer.denials, observer.loadingUsed
	observer.mu.Unlock()
	if failed || !loading || lists != 10 || reads != 9 || denials != 1 || observed != 20 || result["completed"] != true || result["original_requests"] != float64(observed) {
		t.Fatal("Artifact browser original response proofs incomplete")
	}
	if artifactWebFixedFacts(t, v, project, other, foreign) != before {
		t.Fatal("Artifact read UI changed immutable business facts")
	}
	var active int
	check("actual-reader-tail", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_object.object_leases WHERE process_id=$1::uuid AND state='active'`, v.objects.process.String()).Scan(&active))
	if active != 0 {
		t.Fatal("Artifact browser retained actual Object readers")
	}
	stop()
	v.joined(t)
	if !installed.pair.Joined() {
		t.Fatal("Artifact browser original pair not joined")
	}
	t.Logf("Artifact browser actual reads lists=%d content=%d refusals=%d original_requests=%d; actual Node/root/proxy/Guard joined; no browser writes", lists, reads, denials, observed)
}

func artifactWebHuman(t *testing.T, v *workAppFixture, s *knowledgeWebSetup, origin string) (id.Actor, string) {
	t.Helper()
	u, err := url.Parse(origin)
	s.check("origin", err)
	admin := s.client()
	admin.Jar.SetCookies(u, []*http.Cookie{{Name: "agenteam_local_session", Value: v.cookie}})
	invitation := s.call(admin, "POST", "/api/v1/system/invitations", map[string]string{"email": "artifact-owner@example.com"}, v.csrf, true, 201)
	record := s.record("invitation", "artifact-owner@example.com", knowledgeWebString(t, invitation, "id"))
	link, err := url.Parse(record.URL)
	s.check("invitation", err)
	if link.Scheme+"://"+link.Host != origin || link.Fragment == "" {
		t.Fatal("owned invitation binding differs")
	}
	client := s.client()
	bootstrap := s.call(client, "GET", "/api/v1/auth/bootstrap", nil, "", false, 200)
	csrf := knowledgeWebString(t, bootstrap, "csrf_token")
	s.call(client, "POST", "/api/v1/invitations/inspect", map[string]string{"token": link.Fragment}, csrf, false, 200)
	password := "Artifact private " + guardID[struct{}](t).String() + "!"
	s.call(client, "POST", "/api/v1/invitations/redeem", map[string]string{"token": link.Fragment, "username": "artifact-owner", "display_name": "Artifact Owner", "password": password, "confirmation": password}, csrf, true, 201)
	s.call(client, "POST", "/api/v1/sessions/login", map[string]string{"email": "artifact-owner@example.com", "password": password}, csrf, true, 200)
	session := s.call(client, "GET", "/api/v1/session", nil, "", false, 200)
	user, ok := session["user"].(map[string]any)
	if !ok || user["role"] != "user" {
		t.Fatal("ordinary Artifact Owner absent")
	}
	var cookie string
	for _, c := range client.Jar.Cookies(u) {
		if c.Name == "agenteam_local_session" {
			cookie = c.Value
		}
	}
	material, err := sc.NewSecretMaterial([]byte(cookie))
	s.check("cookie", err)
	defer material.Destroy()
	actor, err := v.root.core.Authenticate(v.ctx, material)
	s.check("current-owner", err)
	if actor.Details().UserID != knowledgeWebString(t, user, "id") {
		t.Fatal("Artifact Owner identity differs")
	}
	return actor, cookie
}
func artifactWebProject(t *testing.T, v *workAppFixture, actor id.Actor, name string) id.ProjectID {
	t.Helper()
	p := guardID[id.Project](t)
	result, err := v.projects.service.CreateProject(v.ctx, actor, humanArtifactMeta(t), pc.CreateProjectRequest{ProjectID: p, Name: name})
	workAppCheck(t, "artifact-web-real-project", err)
	if result.Validate() != nil || result.State != pc.CreationReady || result.Project == nil || result.Project.ID != p {
		t.Fatal("Artifact browser Project did not confirm")
	}
	return p
}
func artifactWebCreate(t *testing.T, v *workAppFixture, service *artifact.Service, actor id.Actor, p id.ProjectID, name, media, text string) ac.Metadata {
	t.Helper()
	invocation, err := ac.NewInvocation(ac.InvocationDetails{Actor: actor, ProjectID: p, AttemptID: guardID[ac.CallAttempt](t)})
	workAppCheck(t, "artifact-web-real-invocation", err)
	value, err := service.CreateFromContent(v.ctx, invocation, humanArtifactMeta(t), ac.Display{Name: name, Description: "Synthetic browser acceptance content"}, media, text)
	workAppCheck(t, "artifact-web-real-create", err)
	d := value.Details()
	if value.Validate() != nil || d.Reference.ProjectID != p || d.Name != name || d.Object.ByteSize != f.Progress(len(text)) || d.Kind != ac.Generated || d.CreatedByUserID != actor.Details().UserID {
		t.Fatal("Artifact browser creation receipt differs")
	}
	return value
}
func artifactWebMetadata(value ac.Metadata) map[string]any {
	d := value.Details()
	return map[string]any{"artifact_ref": d.Reference, "name": d.Name, "description": d.Description, "kind": d.Kind, "object": map[string]any{"media_type": d.Object.MediaType, "byte_size": d.Object.ByteSize, "sha256": d.Object.SHA256}, "created_by_user_id": d.CreatedByUserID, "created_at": d.CreatedAt, "version": d.Version}
}
func artifactWebContentPath(r ac.ArtifactRef) string {
	return "/api/v1/projects/" + r.ProjectID.String() + "/artifacts/" + r.ArtifactID.String() + "/files/" + r.FileID.String() + "/content"
}
func artifactWebFixedFacts(t *testing.T, v *workAppFixture, projects ...id.ProjectID) string {
	t.Helper()
	values := make([]string, 0, len(projects))
	for _, p := range projects {
		var raw string
		workAppCheck(t, "artifact-web-fixed-facts", v.admin.QueryRow(v.ctx, `SELECT jsonb_build_array(
   (SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_artifact.artifacts x WHERE project_id=$1::uuid),
   (SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY command_hash),'[]') FROM agenteam_artifact.commands x WHERE project_id=$1::uuid),
   (SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_object.objects x WHERE project_id=$1::uuid),
   (SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_object.uploads x WHERE project_id=$1::uuid)
  )::text`, p.String()).Scan(&raw))
		values = append(values, raw)
	}
	return strings.Join(values, "\n")
}

type artifactWebTail struct {
	done        chan struct{}
	method, uri string
	returned    bool
}
type artifactWebObserver struct {
	mu                                              sync.Mutex
	host, private, evidence, inputHash, loadingPath string
	active, failed, loadingUsed                     bool
	tails                                           map[string]*artifactWebTail
	count, lists, reads, denials                    int
	proxyErrors                                     int
	failureStage                                    string
	responses                                       []artifactWebResponseDiagnostic
}

// Only closed enums, counts and booleans survive. In particular, no URL,
// header map, cookie, body, cursor, error text or private material is saved.
type artifactWebResponseDiagnostic struct {
	Status          int    `json:"status"`
	Bytes           int    `json:"bytes"`
	DeclaredLength  int64  `json:"declared_length"`
	EOF             bool   `json:"original_eof"`
	Closed          bool   `json:"original_closed"`
	JSON            bool   `json:"valid_json"`
	NoStore         bool   `json:"no_store"`
	OneRequestID    bool   `json:"one_request_id"`
	RequestIDValid  bool   `json:"request_id_valid"`
	HandlerMatched  bool   `json:"handler_matched"`
	HandlerReturned bool   `json:"handler_returned"`
	ProblemCode     string `json:"problem_code"`
	ProblemCommit   string `json:"problem_commit"`
}

func (o *artifactWebObserver) fail(stage string) error {
	o.mu.Lock()
	o.failed = true
	if o.failureStage == "" {
		o.failureStage = stage
	}
	o.mu.Unlock()
	return errors.New("original Artifact observation failed")
}
func (o *artifactWebObserver) saveDiagnostic(t *testing.T) {
	t.Helper()
	o.mu.Lock()
	returned, ended := 0, 0
	for _, tail := range o.tails {
		if tail.returned {
			returned++
		}
		select {
		case <-tail.done:
			ended++
		default:
		}
	}
	record := struct {
		InputHash       string                          `json:"input_hash"`
		Failed          bool                            `json:"failed"`
		FailureStage    string                          `json:"first_failure_stage"`
		ProxyErrors     int                             `json:"proxy_errors"`
		HandlerStarted  int                             `json:"handler_started"`
		HandlerEnded    int                             `json:"handler_ended"`
		HandlerReturned int                             `json:"handler_returned"`
		Accepted        int                             `json:"accepted_responses"`
		Responses       []artifactWebResponseDiagnostic `json:"responses"`
	}{o.inputHash, o.failed, o.failureStage, o.proxyErrors, len(o.tails), ended, returned, o.count, append([]artifactWebResponseDiagnostic(nil), o.responses...)}
	o.mu.Unlock()
	raw, err := json.Marshal(record)
	if err != nil {
		t.Error("Artifact diagnostic encoding failed")
		return
	}
	if err = os.WriteFile(filepath.Join(o.evidence, "artifact-owner-driver-diagnostic.json"), raw, 0600); err != nil {
		t.Error("Artifact diagnostic write failed")
	}
}

func (o *artifactWebObserver) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.mu.Lock()
		selected := o.active && strings.Contains(r.URL.Path, "/artifacts")
		o.mu.Unlock()
		if !selected {
			next.ServeHTTP(w, r)
			return
		}
		rid := httpapi.RequestID(r.Context())
		if r.Method != "GET" || r.Host != o.host || rid.Validate() != nil {
			_ = o.fail("request_identity")
			http.Error(w, "owned read observation rejected", 502)
			return
		}
		tail := &artifactWebTail{done: make(chan struct{}), method: r.Method, uri: r.URL.RequestURI()}
		o.mu.Lock()
		if o.tails[rid.String()] != nil || len(o.tails) >= 64 {
			o.mu.Unlock()
			_ = o.fail("request_duplicate_or_bound")
			http.Error(w, "owned read observation duplicate", 502)
			return
		}
		o.tails[rid.String()] = tail
		held := r.URL.Path == o.loadingPath && !o.loadingUsed
		if held {
			o.loadingUsed = true
		}
		o.mu.Unlock()
		defer close(tail.done)
		if held {
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				_, err := os.Stat(filepath.Join(o.private, "release-loading"))
				if err == nil {
					break
				}
				if !errors.Is(err, os.ErrNotExist) {
					_ = o.fail("loading_release_observation")
					return
				}
				select {
				case <-ticker.C:
				case <-r.Context().Done():
					_ = o.fail("loading_request_cancelled")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
		o.mu.Lock()
		tail.returned = true
		o.mu.Unlock()
	})
}
func (o *artifactWebObserver) capture(response *http.Response) error {
	o.mu.Lock()
	selected := o.active && strings.Contains(response.Request.URL.Path, "/artifacts")
	o.mu.Unlock()
	if !selected {
		return nil
	}
	diagnostic := artifactWebResponseDiagnostic{Status: response.StatusCode, DeclaredLength: response.ContentLength}
	defer func() {
		o.mu.Lock()
		if len(o.responses) < 64 {
			o.responses = append(o.responses, diagnostic)
		}
		o.mu.Unlock()
	}()
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	closeErr := response.Body.Close()
	diagnostic.Bytes, diagnostic.EOF, diagnostic.Closed, diagnostic.JSON = len(raw), readErr == nil && len(raw) <= 8<<20, closeErr == nil, json.Valid(raw)
	diagnostic.NoStore = response.Header.Get("Cache-Control") == "no-store"
	diagnostic.OneRequestID = len(response.Header.Values("X-Request-ID")) == 1
	if response.StatusCode != 200 {
		var problem struct {
			Code   f.Code        `json:"code"`
			Commit f.CommitState `json:"commit_state"`
		}
		if json.Unmarshal(raw, &problem) == nil {
			if problem.Code.Known() {
				diagnostic.ProblemCode = string(problem.Code)
			}
			switch problem.Commit {
			case f.NotStarted, f.NotCommitted, f.Committed, f.Unknown:
				diagnostic.ProblemCommit = string(problem.Commit)
			}
		}
	}
	if readErr != nil || closeErr != nil || len(raw) > 8<<20 || response.ContentLength != int64(len(raw)) || !json.Valid(raw) || response.Header.Get("Cache-Control") != "no-store" || len(response.Header.Values("X-Request-ID")) != 1 {
		return o.fail("response_integrity")
	}
	rid, err := f.ParseID[f.Request](response.Header.Get("X-Request-ID"))
	if err != nil {
		return o.fail("response_request_id")
	}
	diagnostic.RequestIDValid = true
	o.mu.Lock()
	tail := o.tails[rid.String()]
	o.mu.Unlock()
	if tail == nil || tail.uri != response.Request.URL.RequestURI() || tail.method != response.Request.Method {
		return o.fail("handler_correlation")
	}
	diagnostic.HandlerMatched = true
	select {
	case <-tail.done:
	case <-response.Request.Context().Done():
		return o.fail("handler_wait_cancelled")
	}
	diagnostic.HandlerReturned = tail.returned
	if !tail.returned {
		return o.fail("handler_not_returned")
	}
	if response.StatusCode != 200 && response.StatusCode != 404 {
		return o.fail("response_status")
	}
	if response.StatusCode == 404 {
		var problem httpapi.Problem
		if skillWebDecode(raw, &problem) != nil || problem.Code != f.NotFound || problem.CommitState != f.NotCommitted {
			return o.fail("response_problem")
		}
	}
	record := map[string]any{"input_hash": o.inputHash, "request_id": rid.String(), "method": response.Request.Method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(tail.uri))), "status": response.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true, "request_host_matches": true}
	encoded, err := json.Marshal(record)
	if err != nil {
		return o.fail("evidence_encode")
	}
	if err = os.WriteFile(filepath.Join(o.evidence, "artifact-owner-response-"+rid.String()+".json"), encoded, 0600); err != nil {
		return o.fail("evidence_write")
	}
	o.mu.Lock()
	o.count++
	if response.StatusCode == 404 {
		o.denials++
	} else if strings.HasSuffix(response.Request.URL.Path, "/content") {
		o.reads++
	} else {
		o.lists++
	}
	o.mu.Unlock()
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}
func artifactWebBrowser(t *testing.T, parent context.Context, private, origin, dist, evidence, inputHash string) map[string]any {
	t.Helper()
	harness := os.Getenv("AGENTEAM_ARTIFACT_OWNER_WEB_HARNESS")
	if !filepath.IsAbs(harness) {
		t.Fatal("exact Artifact browser harness required")
	}
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(harness, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(harness, "artifact-owner.config.js"))
	cmd.Dir = harness
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "TMPDIR", "TMP", "TEMP", "NODE_COMPILE_CACHE", "PWTEST_CACHE_DIR", "XDG_CACHE_HOME", "DEBUG", "PWDEBUG", "DEV_CHAT_BASE_URL", "DEV_CHAT_API_KEY", "DEV_CHAT_MODEL", "PLAYWRIGHT_NO_COPY_PROMPT":
			continue
		}
		if !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	for _, name := range []string{"TMPDIR", "TMP", "TEMP", "NODE_COMPILE_CACHE", "PWTEST_CACHE_DIR", "XDG_CACHE_HOME"} {
		path := filepath.Join(private, strings.ToLower(name))
		workAppCheck(t, "artifact-web-child-cache", os.MkdirAll(path, 0700))
		cmd.Env = append(cmd.Env, name+"="+path)
	}
	cmd.Env = append(cmd.Env, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+origin, "AGENTEAM_AUTH_WEB_PRIVATE="+private, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_ARTIFACT_OWNER_WEB_DIST="+dist, "AGENTEAM_ARTIFACT_OWNER_WEB_EVIDENCE="+evidence, "AGENTEAM_ARTIFACT_OWNER_WEB_INPUT_HASH="+inputHash, "AGENTEAM_ARTIFACT_OWNER_WEB_CASE=catalog-and-bounded-read")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal("Artifact original Node did not start")
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
	err := <-done
	joined = true
	t.Logf("Artifact Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	// No raw Playwright log is printed: cookie-bearing setup and error details
	// stay private. The helper saves only closed-stage diagnostics on failure.
	if err != nil || !regexp.MustCompile(`(?m)\b1 passed\b`).Match(output.Bytes()) {
		t.Fatal("Artifact real browser did not pass its one case")
	}
	raw, err := os.ReadFile(filepath.Join(private, "artifact-owner-result.json"))
	workAppCheck(t, "artifact-web-result", err)
	var result map[string]any
	if len(raw) > 16<<10 || json.Unmarshal(raw, &result) != nil || result["input_hash"] != inputHash {
		t.Fatal("Artifact browser result differs from frozen inputs")
	}
	return result
}
