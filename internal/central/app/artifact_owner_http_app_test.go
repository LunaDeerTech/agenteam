//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// Creation remains the original installed Human Service. Only the bounded
// List/Read surface is HTTP. This reuses the real default App, Object and Store;
// no SQL seed, replacement owner, source route or model output is involved.
func TestArtifactOwnerHTTPDefaultApp(t *testing.T) {
	t.Run("pagination-current-owner-and-utf8-head", artifactOwnerHTTPReads)
	t.Run("original-reader-handler-and-owner-return", artifactOwnerHTTPReturn)
}

type artifactOwnerHTTPResponse struct {
	status int
	header http.Header
	body   []byte
}

func artifactOwnerHTTPRequest(ctx context.Context, client *http.Client, address, cookie, method, path string, body []byte, csrf, key string) (artifactOwnerHTTPResponse, error) {
	req, err := http.NewRequestWithContext(ctx, method, address+path, bytes.NewReader(body))
	if err != nil {
		return artifactOwnerHTTPResponse{}, err
	}
	req.Host = "localhost:8080"
	req.Header.Set("Origin", "http://localhost:8080")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "agenteam_local_session", Value: cookie})
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	response, err := client.Do(req)
	if err != nil {
		return artifactOwnerHTTPResponse{}, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 1<<20 {
		return artifactOwnerHTTPResponse{}, errors.New("original Artifact HTTP body did not return")
	}
	if len(response.Header.Values("X-Request-ID")) != 1 {
		return artifactOwnerHTTPResponse{}, errors.New("Artifact HTTP request identity missing")
	}
	if _, err := f.ParseID[f.Request](response.Header.Get("X-Request-ID")); err != nil {
		return artifactOwnerHTTPResponse{}, err
	}
	return artifactOwnerHTTPResponse{response.StatusCode, response.Header.Clone(), raw}, nil
}

func artifactOwnerHTTPGet(t *testing.T, v *humanArtifactApp, method, path string, status int) artifactOwnerHTTPResponse {
	t.Helper()
	r, err := artifactOwnerHTTPRequest(v.ctx, v.client, v.address, v.cookie, method, path, nil, "", "")
	workAppCheck(t, "artifact-owner-http-original-response", err)
	if r.status != status {
		var problem struct {
			Code f.Code `json:"code"`
		}
		_ = json.Unmarshal(r.body, &problem)
		t.Fatalf("Artifact HTTP status=%d wanted=%d code=%s", r.status, status, problem.Code)
	}
	if status == http.StatusOK {
		if r.header.Get("Content-Type") != "application/json" || r.header.Get("Cache-Control") != "no-store" {
			t.Fatal("Artifact HTTP representation headers differ")
		}
		length, err := strconv.Atoi(r.header.Get("Content-Length"))
		workAppCheck(t, "artifact-owner-http-length", err)
		if length < 1 || method == http.MethodGet && length != len(r.body) || method == http.MethodHead && len(r.body) != 0 {
			t.Fatal("Artifact GET/HEAD representation length differs")
		}
	}
	return r
}

func artifactOwnerHTTPListPath(p id.ProjectID) string {
	return "/api/v1/projects/" + p.String() + "/artifacts"
}

func artifactOwnerHTTPReadPath(ref ac.ArtifactRef) string {
	return artifactOwnerHTTPListPath(ref.ProjectID) + "/" + ref.ArtifactID.String() + "/files/" + ref.FileID.String() + "/content"
}

func artifactOwnerHTTPDecode(t *testing.T, raw []byte, value any) {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	workAppCheck(t, "artifact-owner-http-closed-json", d.Decode(value))
	if d.Decode(new(any)) != io.EOF {
		t.Fatal("Artifact HTTP representation has trailing data")
	}
}

type artifactOwnerHTTPPage struct {
	Items      []json.RawMessage `json:"items"`
	NextCursor string            `json:"next_cursor"`
}

func artifactOwnerHTTPMetadata(t *testing.T, raw json.RawMessage, want ac.Metadata) {
	t.Helper()
	var actual, expected map[string]any
	workAppCheck(t, "artifact-http-metadata", json.Unmarshal(raw, &actual))
	workAppCheck(t, "artifact-original-metadata", json.Unmarshal(workAppJSON(t, want), &expected))
	object, ok := expected["object"].(map[string]any)
	if !ok {
		t.Fatal("original Artifact Object metadata missing")
	}
	expected["object"] = map[string]any{"media_type": object["media_type"], "byte_size": object["byte_size"], "sha256": object["sha256"]}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatal("HTTP metadata differs from original receipt or exposes internal Object fields")
	}
}

func artifactOwnerHTTPReads(t *testing.T) {
	v := newHumanArtifactApp(t)
	items := make([]ac.Metadata, 0, 51)
	for n := 0; n < 51; n++ {
		name, media, text := fmt.Sprintf("page-%02d.txt", n), "text/plain", "Formal Human inline fixture.\n"
		if n == 50 {
			name, media, text = "literal%_世界.json", "application/json", "A世界Z"
		}
		m, err := v.service.CreateFromContent(v.ctx, v.invocation(t, v.a), humanArtifactMeta(t), ac.Display{Name: name}, media, text)
		workAppCheck(t, "artifact-http-formal-service-seed", err)
		items = append(items, m)
	}
	unicode := items[50]
	b := v.create(t, v.b, humanArtifactMeta(t), "Other Project remains independent.\n")
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i].Details(), items[j].Details()
		if a.CreatedAt.Time().Equal(b.CreatedAt.Time()) {
			return a.Reference.ArtifactID.String() > b.Reference.ArtifactID.String()
		}
		return a.CreatedAt.Time().After(b.CreatedAt.Time())
	})
	var first, last artifactOwnerHTTPPage
	artifactOwnerHTTPDecode(t, artifactOwnerHTTPGet(t, v, "GET", artifactOwnerHTTPListPath(v.a), 200).body, &first)
	if len(first.Items) != 50 || first.NextCursor == "" {
		t.Fatal("default Artifact page must expose 50 plus one continuation")
	}
	artifactOwnerHTTPDecode(t, artifactOwnerHTTPGet(t, v, "GET", artifactOwnerHTTPListPath(v.a)+"?cursor="+url.QueryEscape(first.NextCursor), 200).body, &last)
	if len(last.Items) != 1 || last.NextCursor != "" {
		t.Fatal("original continuation lost or repeated the 51st Artifact")
	}
	for i, row := range append(first.Items, last.Items...) {
		artifactOwnerHTTPMetadata(t, row, items[i])
	}
	filteredPath := artifactOwnerHTTPListPath(v.a) + "?" + url.Values{"kind": {"generated"}, "media_type": {"application/json"}, "name_query": {"%_世界"}}.Encode()
	var filtered artifactOwnerHTTPPage
	filteredResponse := artifactOwnerHTTPGet(t, v, "GET", filteredPath, 200)
	artifactOwnerHTTPDecode(t, filteredResponse.body, &filtered)
	if len(filtered.Items) != 1 || filtered.NextCursor != "" {
		t.Fatal("AND filters or literal name wildcards changed the actual page")
	}
	artifactOwnerHTTPMetadata(t, filtered.Items[0], unicode)
	head := artifactOwnerHTTPGet(t, v, "HEAD", filteredPath, 200)
	if head.header.Get("Content-Length") != filteredResponse.header.Get("Content-Length") {
		t.Fatal("HEAD did not encode the same actual filtered page")
	}
	// A real cursor cannot be reused with a changed filter or another Project.
	artifactOwnerHTTPGet(t, v, "GET", artifactOwnerHTTPListPath(v.a)+"?kind=user_upload&cursor="+url.QueryEscape(first.NextCursor), 400)
	artifactOwnerHTTPGet(t, v, "GET", artifactOwnerHTTPListPath(v.b)+"?cursor="+url.QueryEscape(first.NextCursor), 400)
	var other artifactOwnerHTTPPage
	artifactOwnerHTTPDecode(t, artifactOwnerHTTPGet(t, v, "GET", artifactOwnerHTTPListPath(v.b), 200).body, &other)
	if len(other.Items) != 1 || other.NextCursor != "" {
		t.Fatal("Project B list differs")
	}
	artifactOwnerHTTPMetadata(t, other.Items[0], b)
	readPath := artifactOwnerHTTPReadPath(unicode.Details().Reference) + "?offset=1&limit=4"
	r := artifactOwnerHTTPGet(t, v, "GET", readPath, 200)
	var read struct {
		Artifact   json.RawMessage `json:"artifact"`
		Projection string          `json:"projection"`
		Text       string          `json:"text"`
		Offset     string          `json:"offset"`
		Next       string          `json:"next_offset"`
		Truncated  bool            `json:"truncated"`
		Invalid    bool            `json:"invalid_utf8"`
	}
	artifactOwnerHTTPDecode(t, r.body, &read)
	artifactOwnerHTTPMetadata(t, read.Artifact, unicode)
	if read.Projection != "text" || read.Text != "世" || read.Offset != "1" || read.Next != "4" || !read.Truncated || read.Invalid {
		t.Fatal("HTTP preview lost real UTF8 byte window semantics")
	}
	head = artifactOwnerHTTPGet(t, v, "HEAD", readPath, 200)
	if head.header.Get("Content-Length") != r.header.Get("Content-Length") {
		t.Fatal("HEAD preview skipped the actual representation")
	}
	artifactOwnerHTTPGet(t, v, "GET", artifactOwnerHTTPReadPath(unicode.Details().Reference)+"?offset=2&limit=4", 400)
	wrong := unicode.Details().Reference
	wrong.FileID = b.Details().Reference.FileID
	artifactOwnerHTTPGet(t, v, "GET", artifactOwnerHTTPReadPath(wrong), 403)
	wrong = unicode.Details().Reference
	wrong.ProjectID = v.b
	artifactOwnerHTTPGet(t, v, "GET", artifactOwnerHTTPReadPath(wrong), 403)
	v.audit(t, v.a, 51, 4, 2)
	v.audit(t, v.b, 1, 1, 0)
	// A separately redeemed real account is currently not either Project's
	// Owner. Service labels or cached receipts cannot authorize its reads.
	foreign := artifactOwnerHTTPForeignClient(t, v)
	for _, path := range []string{artifactOwnerHTTPListPath(v.a), artifactOwnerHTTPReadPath(unicode.Details().Reference)} {
		denied, err := artifactOwnerHTTPRequest(v.ctx, foreign, v.address, "", "GET", path, nil, "", "")
		workAppCheck(t, "artifact-http-nonowner-original-return", err)
		if denied.status != 404 {
			t.Fatalf("current nonowner Artifact status=%d wanted=404", denied.status)
		}
		var problem struct {
			Code f.Code `json:"code"`
		}
		_ = json.Unmarshal(denied.body, &problem)
		if problem.Code != f.NotFound || bytes.Contains(denied.body, []byte(unicode.Details().Name)) {
			t.Fatal("denied Artifact response exposed a metadata candidate")
		}
	}
	v.audit(t, v.a, 51, 4, 2)
	v.joined(t)
	t.Log("ARTIFACT_OWNER_HTTP reads=true default_page_50_plus_1=true filter_cursor_binding=true exact_p_a_f=true utf8=true head_actual_audit=true current_nonowner_denied=true safe_object_projection=true formal_service_seed=true no_http_create=true")
}

// Reuse the existing task-owned Account recovery reader, not a SQL user seed.
// Tokens/passwords remain in memory and are never included in test output.
func artifactOwnerHTTPForeignClient(t *testing.T, v *humanArtifactApp) *http.Client {
	t.Helper()
	call := func(client *http.Client, cookie, method, path string, body any, csrf string, keyed bool, status int) map[string]any {
		var raw []byte
		if body != nil {
			raw = workAppJSON(t, body)
		}
		defer clear(raw)
		key := ""
		if keyed {
			key = guardID[struct{}](t).String()
		}
		r, err := artifactOwnerHTTPRequest(v.ctx, client, v.address, cookie, method, path, raw, csrf, key)
		workAppCheck(t, "artifact-http-formal-account", err)
		defer clear(r.body)
		if r.status != status {
			t.Fatalf("formal Account setup status=%d wanted=%d", r.status, status)
		}
		var out map[string]any
		workAppCheck(t, "artifact-http-account-json", json.Unmarshal(r.body, &out))
		return out
	}
	email, username := "artifact-reader@example.com", "artifact-reader"
	invitation := call(v.client, v.cookie, "POST", "/api/v1/system/invitations", map[string]string{"email": email}, v.csrf, true, 201)
	setup := &knowledgeWebSetup{t: t, ctx: v.ctx, cfg: v.root.cfg}
	record := setup.record("invitation", email, knowledgeWebString(t, invitation, "id"))
	link, err := url.Parse(record.URL)
	workAppCheck(t, "artifact-http-invitation-url", err)
	if link.Scheme+"://"+link.Host != "http://localhost:8080" || link.Fragment == "" {
		t.Fatal("formal invitation has no bound token")
	}
	jar, err := cookiejar.New(nil)
	workAppCheck(t, "artifact-http-account-cookiejar", err)
	transport := &http.Transport{Proxy: nil}
	client := &http.Client{Transport: transport, Jar: jar, Timeout: 8 * time.Second}
	t.Cleanup(transport.CloseIdleConnections)
	anonymous := call(client, "", "GET", "/api/v1/auth/bootstrap", nil, "", false, 200)
	csrf := knowledgeWebString(t, anonymous, "csrf_token")
	call(client, "", "POST", "/api/v1/invitations/inspect", map[string]string{"token": link.Fragment}, csrf, false, 200)
	password := "Artifact private " + guardID[struct{}](t).String() + "!"
	call(client, "", "POST", "/api/v1/invitations/redeem", map[string]string{"token": link.Fragment, "username": username, "display_name": "Artifact Reader", "password": password, "confirmation": password}, csrf, true, 201)
	call(client, "", "POST", "/api/v1/sessions/login", map[string]string{"email": email, "password": password}, csrf, true, 200)
	session := call(client, "", "GET", "/api/v1/session", nil, "", false, 200)
	user, ok := session["user"].(map[string]any)
	if !ok || user["role"] != "user" || user["username"] != username {
		t.Fatal("formal second Human session missing")
	}
	return client
}

func artifactOwnerHTTPReturn(t *testing.T) {
	v := newHumanArtifactApp(t)
	a := v.create(t, v.a, humanArtifactMeta(t), "Original typed reader and HTTP handler.\n")
	b := v.create(t, v.b, humanArtifactMeta(t), "Independent Project B.\n")
	h := &usageAppHook{match: func(c f.TransactionCause) bool {
		return c.Details().Kind == f.RecoveryCause && c.Details().Owner == "object"
	}}
	h.selectRow = func(ctx context.Context, x postgres.SQLExecutor, _ f.TransactionCause, before, after int64) (bool, error) {
		var released bool
		err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_leases WHERE object_id=$1::uuid AND process_id=$2::uuid AND owner_kind='reader' AND state='released' AND xmin::text=pg_current_xact_id()::text)`, a.Details().Object.ID.String(), v.objects.process.String()).Scan(&released)
		return released && after > before, err
	}
	v.tracked.arm(h)
	done := make(chan struct{})
	v.calls = append(v.calls, done)
	var first artifactOwnerHTTPResponse
	var firstErr error
	go func() {
		defer close(done)
		first, firstErr = artifactOwnerHTTPRequest(v.ctx, v.client, v.address, v.cookie, "GET", artifactOwnerHTTPReadPath(a.Details().Reference), nil, "", "")
	}()
	held := humanArtifactHeld(t, h, done)
	humanArtifactNotReturned(t, done)
	v.ledger(t, v.a, 1)
	v.requireGuard(t)
	other := artifactOwnerHTTPGet(t, v, "GET", artifactOwnerHTTPReadPath(b.Details().Reference), 200)
	if !bytes.Contains(other.body, []byte("Independent Project B.")) {
		t.Fatal("Project B read did not return while A reader was held")
	}
	h.open()
	usageAppWait(t, held.returned, "HTTP original typed reader Store return")
	usageAppWait(t, done, "HTTP original Artifact read return")
	workAppCheck(t, "artifact-http-original-read", firstErr)
	if first.status != 200 || !bytes.Contains(first.body, []byte("Original typed reader and HTTP handler.")) {
		t.Fatal("actual HTTP read value lost")
	}
	tailHook := &usageAppHook{
		match: func(c f.TransactionCause) bool {
			d := c.Details()
			return d.Kind == f.RecoveryCause && d.Owner == "artifact-call-return" && d.CheckpointRef == v.a.String()
		},
		selectRow: func(context.Context, postgres.SQLExecutor, f.TransactionCause, int64, int64) (bool, error) {
			return true, nil
		},
	}
	v.tracked.arm(tailHook)
	lastDone := make(chan struct{})
	v.calls = append(v.calls, lastDone)
	var last artifactOwnerHTTPResponse
	var lastErr error
	go func() {
		defer close(lastDone)
		last, lastErr = artifactOwnerHTTPRequest(v.ctx, v.client, v.address, v.cookie, "HEAD", artifactOwnerHTTPReadPath(a.Details().Reference), nil, "", "")
	}()
	tail := humanArtifactHeld(t, tailHook, lastDone)
	v.ledger(t, v.a, 0)
	v.requestStop()
	v.root.logs.wait(t, func(e map[string]any) bool { return e["phase"] == "stopping" })
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		v.assembly.mu.Lock()
		stopped := v.assembly.stopped
		v.assembly.mu.Unlock()
		if stopped {
			break
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("root did not close original Account admission")
		case <-v.root.done:
			t.Fatal("root returned before original HTTP Store")
		}
	}
	if v.pair.Joined() || v.assembly.Joined() {
		t.Fatal("closed ledger replaced original HTTP call return")
	}
	select {
	case <-v.root.done:
		t.Fatal("root returned before original HTTP handler")
	case <-v.store.stopped:
		t.Fatal("database stopped before original HTTP handler")
	case <-lastDone:
		t.Fatal("HEAD returned before its original Service Store")
	default:
	}
	v.requireGuard(t)
	tailHook.open()
	usageAppWait(t, tail.returned, "HEAD original technical Store return")
	usageAppWait(t, lastDone, "HEAD original handler return during shutdown")
	workAppCheck(t, "artifact-http-head-after-stop", lastErr)
	if last.status != 200 || len(last.body) != 0 || last.header.Get("Content-Length") == "" {
		t.Fatal("HEAD lost original completed read semantics")
	}
	v.audit(t, v.a, 1, 0, 2)
	v.audit(t, v.b, 1, 0, 1)
	v.joined(t)
	t.Log("ARTIFACT_OWNER_HTTP_RETURN reader_close_store_held=true http_head_service_store_held=true project_b=true actual_store_handler_owner_return=true root_guard_db_joined=true no_new_budget=true")
}
