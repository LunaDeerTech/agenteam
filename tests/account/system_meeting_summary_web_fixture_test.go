//go:build integration

package account_test

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/app"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	"github.com/LunaDeerTech/agenteam/tests/testsupport/accountenv"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

const selectionWebPath = "/api/v1/system/model-selection"
const summaryWebPath = selectionWebPath + "/meeting-summary"

type summaryWebSlot struct {
	id                string
	drop              bool
	attempts          []selectionWebAttempt
	lostKey           string
	lostBody          [32]byte
	receipt           map[string]any
	dropped, replayed int
	same              bool
}
type meetingSummaryWebFixture struct {
	*selectionWebFixture
	slots        map[string]*summaryWebSlot
	summaryID    string
	rolePrepared bool
}

func newMeetingSummaryWebRoot(t *testing.T, ctx context.Context, own *meetingSummaryWebFixture) *authenticationWebFixture {
	t.Helper()
	db := pgfixture.NewDatabase(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("owned frontend listener unavailable")
	}
	t.Cleanup(func() { _ = listener.Close() })
	origin := "http://" + listener.Addr().String()
	inputs := accountenv.New(t)
	objects, err := objectfixture.Environment(ctx, db.Name)
	if err != nil {
		t.Fatal("owned object fixture unavailable")
	}
	values := inputs.Values()
	for _, v := range objects {
		k, value, _ := strings.Cut(v, "=")
		values[k] = value
	}
	for k, v := range map[string]string{
		"DATABASE_URL": db.Fixture.URL(db.Name), "DATABASE_CA_FILE": db.Fixture.CAFile,
		"DATABASE_STARTUP_TIMEOUT": "15s", "HTTP_ADDR": "127.0.0.1:0", "PUBLIC_ORIGIN": origin, "SHUTDOWN_TIMEOUT": "2s",
		"CURSOR_KEYRING":                 `{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`,
		"SECRET_KEYRING":                 `{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="}]}`,
		"KNOWLEDGE_CONFIRMATION_KEYRING": `{"format":1,"current_kid":"knowledge","keys":[{"kid":"knowledge","key_b64":"gIGCg4SFhoeIiYqLjI2Oj5CRkpOUlZaXmJmam5ydnp8="}]}`,
	} {
		values[config.Prefix+k] = v
	}
	var environment []string
	for k, v := range values {
		environment = append(environment, k+"="+v)
	}
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := values[k]; return v, ok }, environment)
	if err != nil {
		t.Fatal("owned root configuration invalid")
	}
	output := &httpFixtureLog{listening: make(chan string, 1)}
	logger, err := logging.New(logging.Central, slog.LevelInfo, output)
	if err != nil {
		t.Fatal(err)
	}
	rootCtx, cancel := context.WithCancel(ctx)
	rootDone := make(chan error, 1)
	go func() { rootDone <- app.Run(rootCtx, cfg, logger, nil) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-rootDone:
			if err != nil {
				t.Error("full Account/Secret/Model root shutdown failed", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("full root did not join within its original shutdown budget; retaining ownership until actual exit")
			<-rootDone
		}
	})
	var address string
	select {
	case address = <-output.listening:
	case err := <-rootDone:
		rootDone <- err
		t.Fatal("full root initialization failed", err)
	case <-ctx.Done():
		t.Fatal("shared browser budget ended during root startup")
	}
	backend, err := url.Parse("http://" + address)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{Proxy: nil}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport = transport // NewSingleHostReverseProxy preserves the original Host.
	proxy.ModifyResponse = own.controlResponse
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		var lost *modelsResponseLost
		if !errors.As(err, &lost) {
			http.Error(w, "owned API unavailable", 502)
			return
		}
		defer clear(lost.prefix)
		for name, values := range lost.header {
			w.Header()[name] = append([]string(nil), values...)
		}
		w.Header().Del("Transfer-Encoding")
		w.Header().Set("Content-Length", strconv.Itoa(lost.length))
		w.Header().Set("Connection", "close")
		w.WriteHeader(200)
		_, _ = w.Write(lost.prefix)
		_ = http.NewResponseController(w).Flush()
		if hijacker, ok := w.(http.Hijacker); ok {
			if conn, _, e := hijacker.Hijack(); e == nil {
				_ = conn.Close()
			}
		}
	}
	root := os.Getenv("AGENTEAM_MEETING_SUMMARY_WEB_DIST")
	if !filepath.IsAbs(root) {
		t.Fatal("explicit frozen Summary production dist required")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "index.html")); err != nil {
		t.Fatal("formal production dist must be built before browser execution")
	}
	assets := http.FileServer(http.Dir(root))
	var handlerWG sync.WaitGroup
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerWG.Add(1)
		defer handlerWG.Done()
		if r.Method == "GET" && r.URL.Path == "/api/v1/session" && own.failSession.CompareAndSwap(true, false) {
			own.sessionFailures.Add(1)
			http.Error(w, "owned bounded session read unavailable", 503)
			return
		}
		if r.Method == "GET" && own.shouldFailSummaryRead(r.URL.Path) {
			own.readFailures.Add(1)
			http.Error(w, "owned exact configuration read unavailable", 503)
			return
		}
		if r.Method == "PUT" && (r.URL.Path == summaryWebPath || r.URL.Path == selectionWebPath) {
			raw, err := io.ReadAll(io.LimitReader(r.Body, 16385))
			closeErr := r.Body.Close()
			defer clear(raw)
			if err != nil || closeErr != nil || len(raw) > 16384 {
				http.Error(w, "owned write observation unavailable", 400)
				return
			}
			own.mu.Lock()
			slot := own.slots[r.URL.Path]
			slot.attempts = append(slot.attempts, selectionWebAttempt{key: r.Header.Get("Idempotency-Key"), body: sha256.Sum256(raw)})
			own.mu.Unlock()
			r.Body = io.NopCloser(bytes.NewReader(raw))
		}

		if r.URL.Path == "/api/v1" || strings.HasPrefix(r.URL.Path, "/api/v1/") {
			proxy.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method unavailable", 405)
			return
		}
		clean := filepath.Clean("/" + r.URL.Path)
		if info, e := os.Stat(filepath.Join(root, clean)); e == nil && !info.IsDir() {
			assets.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(clean, "/assets/") || filepath.Ext(clean) != "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, filepath.Join(root, "index.html"))
	})}
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve(listener) }()
	t.Cleanup(func() {
		shutdown, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		if err := server.Shutdown(shutdown); err != nil {
			t.Error("frontend server did not drain")
			_ = server.Close()
		}
		if err := <-serverDone; !errors.Is(err, http.ErrServerClosed) {
			t.Error("owned frontend server failed", err)
		}
		handlerWG.Wait()
		transport.CloseIdleConnections()
	})
	runtimeRoot := os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	if !filepath.IsAbs(runtimeRoot) || len(runtimeRoot) > 45 {
		t.Fatal("explicit short task-owned browser runtime is required")
	}
	directory, err := os.MkdirTemp(runtimeRoot, "run-")
	if err != nil {
		t.Fatal("private browser runtime creation failed")
	}
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Error("private browser runtime cleanup failed")
		}
		if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
			t.Error("private browser runtime still exists")
		}
	})
	old := &httpFixture{t: t, db: db, config: cfg, address: address, log: output}
	entry := old.record("bootstrap", "admin@mail.com", "")
	material, err := json.Marshal(map[string]string{"email": entry.Email, "password": entry.Password})
	if err != nil {
		t.Fatal("private bootstrap encoding failed")
	}
	defer clear(material)
	if err := os.WriteFile(filepath.Join(directory, "credentials.json"), material, 0600); err != nil {
		t.Fatal("private bootstrap transfer failed")
	}
	f := &authenticationWebFixture{t: t, db: db, origin: origin, directory: directory, webRoot: root, entry: entry, log: output, record: old.record, recoveryLogPath: cfg.AccountRecoveryLog()}
	t.Cleanup(func() {
		if output.contains(entry.Password) {
			t.Error("bootstrap material escaped the restricted recovery log")
		}
	})
	conn, closeConnection := invitationsWebConnect(t, ctx, db)
	defer closeConnection()
	var configured bool
	if err := conn.QueryRow(ctx, `SELECT configured FROM agenteam_model.platform_selection WHERE singleton`).Scan(&configured); err != nil || configured {
		t.Fatal("real Model Initialize technical singleton missing", err)
	}
	return f
}

func newMeetingSummaryWebFixture(t *testing.T, ctx context.Context, mode string) *meetingSummaryWebFixture {
	t.Helper()
	f := &meetingSummaryWebFixture{selectionWebFixture: &selectionWebFixture{modelsWebFixture: &modelsWebFixture{mode: mode, ids: map[string]string{}}}, slots: map[string]*summaryWebSlot{selectionWebPath: {same: true}, summaryWebPath: {same: true}}}
	f.authenticationWebFixture = newMeetingSummaryWebRoot(t, ctx, f)
	f.setup = &personalWebFixture{authenticationWebFixture: f.authenticationWebFixture}
	f.admin = personalWebCredential{Email: f.entry.Email, Password: f.entry.Password, UserID: f.entry.ID}
	f.adminClient = f.setup.setupClient()
	bootstrap := f.setup.setupRequest(ctx, f.adminClient, "GET", "/api/v1/auth/bootstrap", nil, "", false, 200)
	f.setup.setupRequest(ctx, f.adminClient, "POST", "/api/v1/sessions/login", map[string]string{"email": f.admin.Email, "password": f.admin.Password}, httpString(t, bootstrap, "csrf_token"), true, 200)
	f.csrf = httpString(t, f.setup.setupRequest(ctx, f.adminClient, "GET", "/api/v1/session", nil, "", false, 200), "csrf_token")
	initial := f.request(ctx, "GET", selectionWebPath, nil, 200)
	summary := f.request(ctx, "GET", summaryWebPath, nil, 200)
	f.selectorID, f.summaryID = httpString(t, initial, "id"), httpString(t, summary, "id")
	if initial["configured"] != nil || initial["version"] != "1" || summary["model"] != nil || summary["version"] != "1" || f.summaryID == f.selectorID {
		t.Fatal("production root did not independently initialize both null/v1 selectors")
	}
	f.slots[selectionWebPath].id, f.slots[summaryWebPath].id = f.selectorID, f.summaryID
	f.secrets = []string{f.admin.Password}
	if mode == "authority" {
		f.member = f.setup.inviteMember(ctx, f.adminClient, f.csrf, "summary-member@example.com", "summary-member")
		f.secrets = append(f.secrets, f.member.Password)
	}
	for _, p := range []struct{ role, protocol, kind string }{{"embedding", "openai-embeddings", "embedding"}, {"memory", "openai-chat-completions", "chat"}, {"reranker", "jina-rerank", "reranker"}, {"image", "openai-images-generations", "image_generation"}} {
		parent := f.createProvider(ctx, "Owned "+p.role+" Provider", p.protocol, "https://provider.invalid/v1", true)
		f.ids[p.role+"_provider"] = parent
		f.ids[p.role] = f.createModel(ctx, parent, modelsWebInput("Owned "+p.role, p.kind))
	}
	if mode == "read" {
		for n := 0; n < 26; n++ {
			f.ids[fmt.Sprintf("extra_provider_%d", n)] = f.createProvider(ctx, fmt.Sprintf("Earlier Summary Provider %02d", n), "openai-chat-completions", "https://provider.invalid/v1", true)
		}
	}
	f.ids["summary_provider"] = f.createProvider(ctx, "Summary Provider "+strings.Repeat("长名称", 20), "openai-chat-completions", "https://provider.invalid/v1", true)
	count := 2
	if mode == "read" {
		count = 27
	}
	for n := 0; n < count; n++ {
		input := modelsWebInput(fmt.Sprintf("Summary plain Model %02d", n), "chat")
		input["provider_model_id"] = strings.Repeat("n", 256)
		input["capabilities"].(map[string]any)["structured_output_modes"] = []string{"text"}
		f.ids[fmt.Sprintf("summary_%d", n)] = f.createModel(ctx, f.ids["summary_provider"], input)
	}
	f.ids["empty_provider"] = f.createProvider(ctx, "Summary empty Provider", "openai-chat-completions", "https://provider.invalid/v1", true)
	if mode != "lifecycle" {
		f.selection(ctx, "initial", "")
		f.setSummary(ctx, f.ids["summary_0"])
	}
	f.private("summary-material.json", map[string]any{"admin": f.admin, "member": f.member, "ids": f.ids, "selector_id": f.selectorID, "summary_id": f.summaryID, "providers": f.snapshotProviders(ctx), "models": f.snapshotModels(ctx)})
	t.Cleanup(func() {
		f.releaseHold()
		if f.rolePrepared {
			cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			f.prepareRole(cleanup, "admin")
		}
		for _, secret := range f.secrets {
			if secret != "" && f.log.contains(secret) {
				t.Error("Summary private material escaped restricted transfer")
			}
		}
		f.secrets = nil
		f.csrf = ""
		f.admin.Password = ""
		f.member.Password = ""
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, slot := range f.slots {
			slot.lostKey = ""
			slot.attempts = nil
			slot.receipt = nil
		}
	})
	return f
}
func (f *meetingSummaryWebFixture) setSummary(ctx context.Context, target string) map[string]any {
	current := f.request(ctx, "GET", summaryWebPath, nil, 200)
	return f.request(ctx, "PUT", summaryWebPath, map[string]any{"id": current["id"], "expected_version": current["version"], "model": target}, 200)
}
func (f *meetingSummaryWebFixture) shouldFailSummaryRead(path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return path == f.failPath && f.failRead.CompareAndSwap(true, false)
}

// Role changes are exact owned authorization-fact preparation only. Bootstrap,
// invitation redemption, login and logout use the production Account boundary.
// This does not claim a role-management endpoint or a role-management E2E flow.
func (f *meetingSummaryWebFixture) prepareRole(ctx context.Context, role string) {
	if f.mode != "authority" || (role != "admin" && role != "user") {
		f.t.Fatal("owned role preparation rejected")
	}
	conn, closeConnection := invitationsWebConnect(f.t, ctx, f.db)
	defer closeConnection()
	var before string
	if err := conn.QueryRow(ctx, `SELECT role FROM agenteam_account.users WHERE id=$1`, f.admin.UserID).Scan(&before); err != nil {
		f.t.Fatal("owned role baseline missing", err)
	}
	tag, err := conn.Exec(ctx, `UPDATE agenteam_account.users SET role=$1,version=version+1,updated_at=clock_timestamp() WHERE id=$2 AND role=$3`, role, f.admin.UserID, before)
	if err != nil || tag.RowsAffected() != 1 {
		f.t.Fatal("owned role fact update failed", err)
	}
	var after string
	if err := conn.QueryRow(ctx, `SELECT role FROM agenteam_account.users WHERE id=$1`, f.admin.UserID).Scan(&after); err != nil || after != role {
		f.t.Fatal("owned role fact verification failed", err)
	}
	f.rolePrepared = role != "admin"
	f.t.Logf("owned auxiliary authorization fact role %s -> %s; not production role-management API", before, after)
}
func (f *meetingSummaryWebFixture) controlResponse(response *http.Response) error {
	r := response.Request
	if r == nil || response.StatusCode != 200 {
		return nil
	}
	if r.Method == "GET" {
		f.mu.Lock()
		hold := f.hold
		if hold != nil && hold.path == r.URL.Path && !hold.started {
			hold.started = true
		} else {
			hold = nil
		}
		f.mu.Unlock()
		if hold != nil {
			// The bounded late response belongs to this root; it does not grant
			// another application's owner release or ignore the outer deadline.
			timer := time.NewTimer(2 * time.Second)
			select {
			case <-hold.release:
			case <-timer.C:
			case <-r.Context().Done():
			}
			timer.Stop()
			f.mu.Lock()
			hold.finished = true
			f.mu.Unlock()
		}
		return nil
	}
	if r.Method != "PUT" || f.slots[r.URL.Path] == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	slot := f.slots[r.URL.Path]
	key := r.Header.Get("Idempotency-Key")
	var digest [32]byte
	for n := len(slot.attempts) - 1; n >= 0; n-- {
		if slot.attempts[n].key == key {
			digest = slot.attempts[n].body
			break
		}
	}
	if slot.lostKey != "" && key == slot.lostKey {
		slot.replayed++
		slot.same = slot.same && digest == slot.lostBody
	}
	if !slot.drop {
		return nil
	}
	slot.drop = false
	raw, err := io.ReadAll(io.LimitReader(response.Body, 600001))
	closeErr := response.Body.Close()
	defer clear(raw)
	var receipt map[string]any
	if err != nil || closeErr != nil || len(raw) > 600000 || json.Unmarshal(raw, &receipt) != nil || len(receipt) != 4 || receipt["kind"] != "model.selection.update" || receipt["resource_id"] != slot.id || receipt["affected_references"] != "0" {
		return errors.New("owned response control requires formal Selection receipt")
	}
	slot.lostKey = key
	slot.lostBody = digest
	slot.receipt = receipt
	slot.dropped++
	return &modelsResponseLost{header: response.Header.Clone(), prefix: append([]byte(nil), raw[:len(raw)/2]...), length: len(raw)}
}

func (f *meetingSummaryWebFixture) summaryFacts(ctx context.Context) map[string]any {
	conn, closeConnection := invitationsWebConnect(f.t, ctx, f.db)
	defer closeConnection()
	out := map[string]any{}
	for _, slot := range []struct{ name, id, table string }{{"platform", f.selectorID, "platform_selection"}, {"summary", f.summaryID, "meeting_summary_selection"}} {
		var commands, audits, events, keys, invocations, deliveries int
		if err := conn.QueryRow(ctx, `SELECT
   (SELECT count(*) FROM agenteam_model.commands WHERE resource_id=$1 AND command_name='model.selection.update' AND phase='committed'),
   (SELECT count(*) FROM agenteam_audit.audit_records WHERE resource_id=$1 AND action='model.selection.update' AND outcome='success'),
   (SELECT count(*) FROM agenteam_outbox.events WHERE aggregate_id=$1 AND producer='model' AND event_type='model.configuration_changed'),
   (SELECT count(DISTINCT key_digest) FROM agenteam_model.commands WHERE resource_id=$1 AND command_name='model.selection.update' AND phase='committed'),
   (SELECT count(*) FROM agenteam_model.invocations),
   (SELECT count(*) FROM agenteam_outbox.deliveries d JOIN agenteam_outbox.events e ON e.id=d.event_id WHERE e.producer='model')`, slot.id).Scan(&commands, &audits, &events, &keys, &invocations, &deliveries); err != nil {
			f.t.Fatal("Summary command facts unavailable", err)
		}
		var version string
		if err := conn.QueryRow(ctx, `SELECT version::text FROM agenteam_model.`+slot.table+` WHERE id=$1`, slot.id).Scan(&version); err != nil {
			f.t.Fatal("Summary current version unavailable", err)
		}
		value := map[string]any{"id": slot.id, "version": version, "commands": commands, "audits": audits, "events": events, "distinct_keys": keys, "invocations": invocations, "deliveries": deliveries}
		rows, err := conn.Query(ctx, `SELECT role,model_id::text,owner_version::text FROM agenteam_model.references WHERE owner_kind='platform_selector' AND owner_id=$1 ORDER BY role`, slot.id)
		if err != nil {
			f.t.Fatal("Summary reference facts unavailable", err)
		}
		refs := []map[string]string{}
		for rows.Next() {
			var role, target, revision string
			if err := rows.Scan(&role, &target, &revision); err != nil {
				rows.Close()
				f.t.Fatal("Summary reference scan failed", err)
			}
			refs = append(refs, map[string]string{"role": role, "model": target, "version": revision})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			f.t.Fatal("Summary reference rows failed", err)
		}
		value["references"] = refs
		if slot.name == "summary" {
			var target *string
			if err := conn.QueryRow(ctx, `SELECT model_id::text FROM agenteam_model.meeting_summary_selection WHERE id=$1`, slot.id).Scan(&target); err != nil {
				f.t.Fatal(err)
			}
			value["model"] = target
		}
		out[slot.name] = value
	}
	return out
}

type summaryWebIPC struct {
	selectionWebIPC
	Slot string `json:"slot"`
}

func (f *meetingSummaryWebFixture) summaryIPC(ctx context.Context, r summaryWebIPC) map[string]any {
	if r.Sequence < 1 || r.Sequence > 128 {
		f.t.Fatal("Summary IPC sequence outside bound")
	}
	if r.ID != "" && !f.owned(r.ID) {
		f.t.Fatal("Summary IPC target not owned")
	}
	path := summaryWebPath
	if r.Slot == "platform" {
		path = selectionWebPath
	} else if r.Slot != "" && r.Slot != "summary" {
		f.t.Fatal("Summary IPC slot invalid")
	}
	out := map[string]any{"ok": true, "sequence": r.Sequence}
	switch r.Action {
	case "facts":
		out["facts"] = f.summaryFacts(ctx)
	case "summary":
		out["receipt"] = f.setSummary(ctx, r.ID)
	case "arm-drop":
		f.mu.Lock()
		s := f.slots[path]
		s.drop = true
		s.lostKey = ""
		s.receipt = nil
		s.dropped = 0
		s.replayed = 0
		s.same = true
		f.mu.Unlock()
		out["armed"] = true
	case "drop-facts":
		f.mu.Lock()
		s := f.slots[path]
		out["dropped"], out["replayed"], out["same_original"], out["receipt"] = s.dropped, s.replayed, s.same, s.receipt
		f.mu.Unlock()
	case "arm-get-fail":
		if r.ID != "" {
			path = "/api/v1/system/model-providers/" + r.ID
		}
		f.mu.Lock()
		f.failPath = path
		f.failRead.Store(true)
		f.mu.Unlock()
		out["armed"] = true
	case "hold-get":
		if r.ID != "" {
			path = "/api/v1/system/model-providers/" + r.ID
		}
		f.mu.Lock()
		if f.hold != nil && !f.hold.finished {
			f.mu.Unlock()
			f.t.Fatal("Summary hold already active")
		}
		f.hold = &selectionWebHold{path: path, release: make(chan struct{})}
		f.mu.Unlock()
		out["armed"] = true
	case "demote":
		f.prepareRole(ctx, "user")
		out["fact_only"] = true
	case "promote":
		f.prepareRole(ctx, "admin")
		out["fact_only"] = true
	case "cross-key":
		f.mu.Lock()
		key := f.slots[selectionWebPath].lostKey
		f.mu.Unlock()
		if key == "" {
			f.t.Fatal("accepted original platform key missing")
		}
		current := f.request(ctx, "GET", summaryWebPath, nil, 200)
		raw, err := json.Marshal(map[string]any{"id": current["id"], "expected_version": current["version"], "model": f.ids["summary_0"]})
		if err != nil {
			f.t.Fatal(err)
		}
		defer clear(raw)
		request, err := http.NewRequestWithContext(ctx, "PUT", f.origin+summaryWebPath, bytes.NewReader(raw))
		if err != nil {
			f.t.Fatal(err)
		}
		request.Header.Set("Origin", f.origin)
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", f.csrf)
		request.Header.Set("Idempotency-Key", key)
		response, err := f.adminClient.Do(request)
		if err != nil {
			f.t.Fatal("cross-slot original-key request failed")
		}
		defer response.Body.Close()
		var problem map[string]any
		if response.StatusCode != 409 || json.NewDecoder(io.LimitReader(response.Body, 16385)).Decode(&problem) != nil || problem["code"] != "IDEMPOTENCY_KEY_REUSED" {
			f.t.Fatal("cross-slot command identity failed")
		}
		out["code"] = problem["code"]
	default:
		return f.selectionIPC(ctx, r.selectionWebIPC)
	}
	return out
}
func (f *meetingSummaryWebFixture) browserSummary(ctx context.Context) map[string]any {
	f.t.Helper()
	root, e := filepath.Abs("../account-captcha-web")
	if e != nil {
		f.t.Fatal(e)
	}
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "system-meeting-summary.config.js"))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "TMPDIR=") && !strings.HasPrefix(value, "AGENTEAM_AUTH_WEB_") && !strings.HasPrefix(value, "AGENTEAM_MEETING_SUMMARY_WEB_") && !strings.HasPrefix(value, "PLAYWRIGHT_NO_COPY_PROMPT=") && !strings.HasPrefix(value, "DEBUG=") && !strings.HasPrefix(value, "PWDEBUG=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "TMPDIR="+f.directory, "AGENTEAM_AUTH_WEB_ORIGIN="+f.origin, "AGENTEAM_MEETING_SUMMARY_WEB_CASE="+f.mode, "AGENTEAM_AUTH_WEB_PRIVATE="+f.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "PLAYWRIGHT_NO_COPY_PROMPT=1")
	if images := os.Getenv("AGENTEAM_AUTH_WEB_IMAGES"); images != "" {
		if !filepath.IsAbs(images) {
			f.t.Fatal("Selection image path must be absolute")
		}
		cmd.Env = append(cmd.Env, "AGENTEAM_AUTH_WEB_IMAGES="+images)
	}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if e = cmd.Start(); e != nil {
		f.t.Fatal("locked Selection browser runner could not start")
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
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	sequence := 0
	var runErr error
wait:
	for {
		select {
		case runErr = <-done:
			joined = true
			break wait
		case <-tick.C:
			raw, err := os.ReadFile(filepath.Join(f.directory, "summary-ipc.json"))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			var request summaryWebIPC
			decode := json.Unmarshal(raw, &request)
			clear(raw)
			if err != nil || decode != nil || request.Sequence < 1 || request.Sequence > 128 {
				f.t.Error("private Selection IPC rejected")
				_ = cmd.Cancel()
				continue
			}
			if request.Sequence <= sequence {
				continue
			}
			if request.Sequence != sequence+1 {
				f.t.Error("private Selection IPC sequence invalid")
				_ = cmd.Cancel()
				continue
			}
			reply := f.summaryIPC(ctx, request)
			sequence = request.Sequence
			f.private("summary-ack-"+strconv.Itoa(sequence)+".json", reply)
		}
	}
	safe := output.String()
	for _, secret := range f.secrets {
		if secret != "" {
			safe = strings.ReplaceAll(safe, secret, "[redacted]")
		}
	}
	f.mu.Lock()
	for _, slot := range f.slots {
		if slot.lostKey != "" {
			safe = strings.ReplaceAll(safe, slot.lostKey, "[redacted]")
		}
	}
	f.mu.Unlock()
	f.t.Log(safe)
	if runErr != nil {
		f.t.Fatalf("actual Selection production browser failed: %v", runErr)
	}
	raw, e := os.ReadFile(filepath.Join(f.directory, "summary-result.json"))
	if e != nil {
		f.t.Fatal("safe Selection browser result missing")
	}
	defer clear(raw)
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil || result["completed"] != true {
		f.t.Fatal("safe Selection browser result invalid")
	}
	return result
}
