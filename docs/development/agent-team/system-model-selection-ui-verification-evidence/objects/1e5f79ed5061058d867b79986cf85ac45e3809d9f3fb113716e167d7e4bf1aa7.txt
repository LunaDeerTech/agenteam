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
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/app"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	"github.com/LunaDeerTech/agenteam/tests/testsupport/accountenv"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// Selection uses the accepted Account/Model helpers only for formal setup and
// read-only projections. All mutable controls belong to this exact new root.
type selectionWebAttempt struct {
	key  string
	body [32]byte
}
type selectionWebHold struct {
	path              string
	release           chan struct{}
	started, finished bool
}
type selectionWebFixture struct {
	*modelsWebFixture
	secondAdmin          personalWebCredential
	selectorID, failPath string
	attempts             []selectionWebAttempt
	lostBody             [32]byte
	lostReceipt          map[string]any
	replayBodyEqual      bool
	hold                 *selectionWebHold
}

func newSelectionWebRoot(t *testing.T, ctx context.Context, own *selectionWebFixture) *authenticationWebFixture {
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
		"CURSOR_KEYRING": `{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`,
		"SECRET_KEYRING": `{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="}]}`,
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
			t.Error("full root did not join within its original shutdown budget")
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
	root, err := filepath.Abs("../../web/dist")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "index.html")); err != nil {
		t.Fatal("formal production dist must be built before browser execution")
	}
	assets := http.FileServer(http.Dir(root))
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/api/v1/session" && own.failSession.CompareAndSwap(true, false) {
			own.sessionFailures.Add(1)
			http.Error(w, "owned bounded session read unavailable", 503)
			return
		}
		if r.Method == "GET" && own.shouldFailRead(r.URL.Path) {
			own.readFailures.Add(1)
			http.Error(w, "owned exact configuration read unavailable", 503)
			return
		}
		if r.Method == "PUT" && r.URL.Path == "/api/v1/system/model-selection" {
			raw, err := io.ReadAll(io.LimitReader(r.Body, 16385))
			closeErr := r.Body.Close()
			defer clear(raw)
			if err != nil || closeErr != nil || len(raw) > 16384 {
				http.Error(w, "owned write observation unavailable", 400)
				return
			}
			own.mu.Lock()
			own.attempts = append(own.attempts, selectionWebAttempt{key: r.Header.Get("Idempotency-Key"), body: sha256.Sum256(raw)})
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

func newSelectionWebFixture(t *testing.T, ctx context.Context, mode string) *selectionWebFixture {
	t.Helper()
	f := &selectionWebFixture{modelsWebFixture: &modelsWebFixture{mode: mode, ids: map[string]string{}}}
	f.authenticationWebFixture = newSelectionWebRoot(t, ctx, f)
	f.setup = &personalWebFixture{authenticationWebFixture: f.authenticationWebFixture}
	f.admin = personalWebCredential{Email: f.entry.Email, Password: f.entry.Password, UserID: f.entry.ID}
	f.adminClient = f.setup.setupClient()
	bootstrap := f.setup.setupRequest(ctx, f.adminClient, "GET", "/api/v1/auth/bootstrap", nil, "", false, 200)
	f.setup.setupRequest(ctx, f.adminClient, "POST", "/api/v1/sessions/login", map[string]string{"email": f.admin.Email, "password": f.admin.Password}, httpString(t, bootstrap, "csrf_token"), true, 200)
	f.csrf = httpString(t, f.setup.setupRequest(ctx, f.adminClient, "GET", "/api/v1/session", nil, "", false, 200), "csrf_token")
	initial := f.request(ctx, "GET", "/api/v1/system/model-selection", nil, 200)
	if initial["configured"] != nil || initial["version"] != "1" {
		t.Fatal("formal Selection Initialize was not null/v1")
	}
	f.selectorID = httpString(t, initial, "id")
	f.secrets = []string{f.admin.Password}
	if mode == "authority" {
		f.member = f.setup.inviteMember(ctx, f.adminClient, f.csrf, "selection-member@example.com", "selection-member")
		f.secrets = append(f.secrets, f.member.Password)
	}
	if mode == "concurrency" {
		f.secondAdmin = f.setup.inviteMember(ctx, f.adminClient, f.csrf, "selection-admin@example.com", "selection-admin")
		conn, closeConnection := invitationsWebConnect(t, ctx, f.db)
		tag, err := conn.Exec(ctx, `UPDATE agenteam_account.users SET role='admin',version=version+1,updated_at=clock_timestamp() WHERE id=$1 AND role='user'`, f.secondAdmin.UserID)
		closeConnection()
		if err != nil || tag.RowsAffected() != 1 {
			t.Fatal("exact owned second administrator preparation failed", err)
		}
		f.secrets = append(f.secrets, f.secondAdmin.Password)
	}
	if mode == "read" {
		for n := 0; n < 26; n++ {
			f.ids[fmt.Sprintf("extra_provider_%d", n)] = f.createProvider(ctx, fmt.Sprintf("Earlier Provider %02d", n), "openai-chat-completions", "https://provider.invalid/v1", true)
		}
	}
	for _, p := range []struct{ role, protocol, kind string }{
		{"embedding", "openai-embeddings", "embedding"}, {"memory", "openai-chat-completions", "chat"}, {"reranker", "jina-rerank", "reranker"}, {"image", "openai-images-generations", "image_generation"},
	} {
		name := "Owned " + p.role + " Provider"
		if mode == "navigation" {
			name += " " + strings.Repeat("长名称", 34)
		}
		parent := f.createProvider(ctx, name, p.protocol, "https://provider.invalid/v1", true)
		f.ids[p.role+"_provider"] = parent
		count := 1
		if mode == "read" && p.role == "memory" {
			count = 26
		}
		for n := 0; n < count; n++ {
			input := modelsWebInput(fmt.Sprintf("Owned %s Model %02d", p.role, n), p.kind)
			if mode == "navigation" {
				input["name"] = "Owned " + p.role + " " + strings.Repeat("模型长名", 27)
				input["provider_model_id"] = strings.Repeat("n", 256)
			}
			f.ids[p.role] = f.createModel(ctx, parent, input)
		}
		if mode != "read" {
			replacementProvider := f.createProvider(ctx, "Replacement "+p.role+" Provider", p.protocol, "https://provider.invalid/v1", true)
			f.ids[p.role+"_replacement_provider"] = replacementProvider
			f.ids[p.role+"_replacement"] = f.createModel(ctx, replacementProvider, modelsWebInput("Replacement "+p.role, p.kind))
		}
	}
	f.ids["empty_provider"] = f.createProvider(ctx, "Owned empty Provider", "openai-chat-completions", "https://provider.invalid/v1", true)
	if mode != "read" {
		bad := modelsWebInput("No schema Model", "chat")
		bad["capabilities"].(map[string]any)["structured_output_modes"] = []string{"text"}
		f.ids["no_schema"] = f.createModel(ctx, f.ids["memory_provider"], bad)
		disabled := modelsWebInput("Disabled Model", "chat")
		disabled["enabled"] = false
		f.ids["disabled"] = f.createModel(ctx, f.ids["memory_provider"], disabled)
		f.ids["doomed"] = f.createModel(ctx, f.ids["embedding_provider"], modelsWebInput("Doomed candidate", "embedding"))
	}
	if mode != "lifecycle" {
		f.selection(ctx, "initial", "")
	}
	f.private("selection-material.json", map[string]any{"admin": f.admin, "member": f.member, "second_admin": f.secondAdmin, "ids": f.ids, "selector_id": f.selectorID, "initial": initial, "providers": f.snapshotProviders(ctx), "models": f.snapshotModels(ctx)})
	t.Cleanup(func() {
		f.releaseHold()
		for _, secret := range f.secrets {
			if secret != "" && f.log.contains(secret) {
				t.Error("Selection material escaped restricted transfer")
			}
		}
		f.secrets = nil
		f.csrf = ""
		f.admin.Password = ""
		f.member.Password = ""
		f.secondAdmin.Password = ""
		f.mu.Lock()
		f.lostKey = ""
		f.attempts = nil
		f.lostReceipt = nil
		f.mu.Unlock()
	})
	return f
}
func (f *selectionWebFixture) owned(target string) bool {
	if target == f.selectorID {
		return true
	}
	for _, value := range f.ids {
		if value == target {
			return true
		}
	}
	return false
}
func (f *selectionWebFixture) shouldFailRead(path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return path == f.failPath && f.failRead.CompareAndSwap(true, false)
}
func (f *selectionWebFixture) releaseHold() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hold != nil {
		select {
		case <-f.hold.release:
		default:
			close(f.hold.release)
		}
	}
}
func (f *selectionWebFixture) controlResponse(response *http.Response) error {
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
			}
			timer.Stop()
			f.mu.Lock()
			hold.finished = true
			f.mu.Unlock()
		}
		return nil
	}
	if r.Method != "PUT" || r.URL.Path != "/api/v1/system/model-selection" {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	key := r.Header.Get("Idempotency-Key")
	var digest [32]byte
	for n := len(f.attempts) - 1; n >= 0; n-- {
		if f.attempts[n].key == key {
			digest = f.attempts[n].body
			break
		}
	}
	if f.lostKey != "" && key == f.lostKey {
		f.replayed++
		f.replayBodyEqual = f.replayBodyEqual && digest == f.lostBody
	}
	if !f.dropNext {
		return nil
	}
	f.dropNext = false
	raw, err := io.ReadAll(io.LimitReader(response.Body, 600001))
	closeErr := response.Body.Close()
	defer clear(raw)
	var receipt map[string]any
	if err != nil || closeErr != nil || len(raw) > 600000 || json.Unmarshal(raw, &receipt) != nil || len(receipt) != 4 || receipt["kind"] != "model.selection.update" || receipt["resource_id"] != f.selectorID || receipt["affected_references"] != "0" {
		return errors.New("owned response control requires formal Selection receipt")
	}
	f.lostKey = key
	f.lostBody = digest
	f.lostID = f.selectorID
	f.lostReceipt = receipt
	f.dropped++
	return &modelsResponseLost{header: response.Header.Clone(), prefix: append([]byte(nil), raw[:len(raw)/2]...), length: len(raw)}
}
func (f *selectionWebFixture) selectionFacts(ctx context.Context) map[string]any {
	conn, closeConnection := invitationsWebConnect(f.t, ctx, f.db)
	defer closeConnection()
	var configured bool
	var version string
	var embedding, memory, reranker, image *string
	if err := conn.QueryRow(ctx, `SELECT version::text,configured,embedding_id::text,memory_id::text,reranker_id::text,image_id::text FROM agenteam_model.platform_selection WHERE singleton AND id=$1`, f.selectorID).Scan(&version, &configured, &embedding, &memory, &reranker, &image); err != nil {
		f.t.Fatal("owned Selection facts unavailable", err)
	}
	var bindings any
	if configured {
		bindings = map[string]any{"embedding": embedding, "memory": memory, "reranker": reranker, "image": image}
	}
	var commands, audits, events, embeddingEvents, deliveries, invocations, distinctKeys, actors int
	if err := conn.QueryRow(ctx, `SELECT
        (SELECT count(*) FROM agenteam_model.commands WHERE resource_id=$1 AND command_name='model.selection.update' AND phase='committed'),
        (SELECT count(*) FROM agenteam_audit.audit_records WHERE action='model.selection.update' AND outcome='success'),
        (SELECT count(*) FROM agenteam_outbox.events WHERE aggregate_id=$1 AND producer='model' AND event_type='model.configuration_changed'),
        (SELECT count(*) FROM agenteam_outbox.events WHERE aggregate_id=$1 AND producer='model' AND event_type='model.embedding_selection_changed'),
        (SELECT count(*) FROM agenteam_outbox.deliveries d JOIN agenteam_outbox.events e ON e.id=d.event_id WHERE e.producer='model'),
        (SELECT count(*) FROM agenteam_model.invocations),
        (SELECT count(DISTINCT key_digest) FROM agenteam_model.commands WHERE resource_id=$1 AND command_name='model.selection.update' AND phase='committed'),
        (SELECT count(DISTINCT user_id) FROM agenteam_model.commands WHERE resource_id=$1 AND command_name='model.selection.update' AND phase='committed')`, f.selectorID).Scan(&commands, &audits, &events, &embeddingEvents, &deliveries, &invocations, &distinctKeys, &actors); err != nil {
		f.t.Fatal("owned Selection fact counts unavailable", err)
	}
	rows, err := conn.Query(ctx, `SELECT role,model_id::text,owner_version::text FROM agenteam_model.references WHERE owner_kind='platform_selector' AND owner_id=$1 ORDER BY role`, f.selectorID)
	if err != nil {
		f.t.Fatal("owned Selection references unavailable", err)
	}
	defer rows.Close()
	references := []map[string]string{}
	for rows.Next() {
		var role, target, version string
		if rows.Scan(&role, &target, &version) != nil {
			f.t.Fatal("owned Selection references invalid")
		}
		references = append(references, map[string]string{"role": role, "model_id": target, "version": version})
	}
	if rows.Err() != nil {
		f.t.Fatal("owned Selection reference scan failed")
	}
	return map[string]any{"selection": map[string]any{"id": f.selectorID, "version": version, "configured": bindings}, "references": references, "commands": commands, "audits": audits, "events": events, "embedding_events": embeddingEvents, "deliveries": deliveries, "invocations": invocations, "distinct_keys": distinctKeys, "actors": actors}
}

type selectionWebIPC struct {
	Sequence    int    `json:"sequence"`
	Action      string `json:"action"`
	ID          string `json:"id"`
	Stage       string `json:"stage"`
	Replacement string `json:"replacement"`
	UserID      string `json:"user_id"`
	SessionID   string `json:"session_id"`
}

func (f *selectionWebFixture) selectionIPC(ctx context.Context, r selectionWebIPC) map[string]any {
	out := map[string]any{"ok": true, "sequence": r.Sequence}
	if r.ID != "" && !f.owned(r.ID) {
		f.t.Fatal("Selection IPC target not owned")
	}
	if r.Replacement != "" && !f.owned(r.Replacement) {
		f.t.Fatal("Selection IPC replacement not owned")
	}
	switch r.Action {
	case "facts":
		out["facts"] = f.selectionFacts(ctx)
	case "snapshot":
		out["providers"], out["models"] = f.snapshotProviders(ctx), f.snapshotModels(ctx)
	case "missing":
		f.request(ctx, "GET", "/api/v1/system/models/"+r.ID, nil, 404)
		out["missing"] = true
	case "selection":
		out["receipt"] = f.selection(ctx, r.Stage, r.ID)
	case "disable-model", "enable-model", "remove-schema", "restore-schema":
		value := f.request(ctx, "GET", "/api/v1/system/models/"+r.ID, nil, 200)
		input := httpObject(f.t, value, "input")
		if r.Action == "remove-schema" || r.Action == "restore-schema" {
			modes := []string{"text"}
			if r.Action == "restore-schema" {
				modes = append(modes, "json_schema")
			}
			httpObject(f.t, input, "capabilities")["structured_output_modes"] = modes
		} else {
			input["enabled"] = r.Action == "enable-model"
		}
		out["receipt"] = f.request(ctx, "PUT", "/api/v1/system/models/"+r.ID, map[string]any{"expected_version": value["version"], "input": input}, 200)
	case "disable-provider", "enable-provider":
		value := f.request(ctx, "GET", "/api/v1/system/model-providers/"+r.ID, nil, 200)
		input := httpObject(f.t, value, "input")
		input["enabled"] = r.Action == "enable-provider"
		out["receipt"] = f.request(ctx, "PUT", "/api/v1/system/model-providers/"+r.ID, map[string]any{"expected_version": value["version"], "input": input}, 200)
	case "delete":
		value := f.request(ctx, "GET", "/api/v1/system/models/"+r.ID, nil, 200)
		var replacement any
		if r.Replacement != "" {
			replacement = r.Replacement
		}
		out["receipt"] = f.request(ctx, "DELETE", "/api/v1/system/models/"+r.ID, map[string]any{"expected_version": value["version"], "replacement": replacement}, 200)
	case "arm-drop":
		f.mu.Lock()
		f.dropNext = true
		f.lostKey = ""
		f.lostID = ""
		f.lostReceipt = nil
		f.dropped = 0
		f.replayed = 0
		f.replayBodyEqual = true
		f.mu.Unlock()
		out["armed"] = true
	case "drop-facts":
		f.mu.Lock()
		out["dropped"], out["replayed"], out["same_original"], out["receipt"], out["armed"] = f.dropped, f.replayed, f.replayBodyEqual, f.lostReceipt, f.dropNext
		f.mu.Unlock()
	case "attempt-facts":
		f.mu.Lock()
		keys := map[string]bool{}
		for _, attempt := range f.attempts {
			keys[attempt.key] = true
		}
		out["attempts"], out["distinct_keys"] = len(f.attempts), len(keys)
		f.mu.Unlock()
	case "arm-get-fail":
		path := "/api/v1/system/model-selection"
		if r.ID != "" {
			path = "/api/v1/system/model-providers/" + r.ID
		}
		f.mu.Lock()
		f.failPath = path
		f.failRead.Store(true)
		f.mu.Unlock()
		out["armed"] = true
	case "arm-session-fail":
		f.failSession.Store(true)
		out["armed"] = true
	case "failure-facts":
		out["read_failures"], out["session_failures"] = f.readFailures.Load(), f.sessionFailures.Load()
	case "hold-get":
		if r.ID == "" {
			f.t.Fatal("exact Provider hold target required")
		}
		f.mu.Lock()
		if f.hold != nil && !f.hold.finished {
			f.mu.Unlock()
			f.t.Fatal("one owned hold at a time")
		}
		f.hold = &selectionWebHold{path: "/api/v1/system/model-providers/" + r.ID, release: make(chan struct{})}
		f.mu.Unlock()
		out["armed"] = true
	case "hold-facts":
		f.mu.Lock()
		if f.hold != nil {
			out["started"], out["finished"] = f.hold.started, f.hold.finished
		}
		f.mu.Unlock()
	case "release-get":
		f.releaseHold()
		out["released"] = true
	case "demote", "promote", "revoke-session":
		if f.mode != "authority" || r.UserID != f.admin.UserID {
			f.t.Fatal("exact Selection authority target rejected")
		}
		if _, err := foundation.ParseID[struct{}](r.SessionID); err != nil {
			f.t.Fatal("exact Selection Session invalid")
		}
		conn, closeConnection := invitationsWebConnect(f.t, ctx, f.db)
		defer closeConnection()
		var exists bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL)`, r.SessionID, r.UserID).Scan(&exists); err != nil || !exists {
			f.t.Fatal("exact active Selection Session absent", err)
		}
		if r.Action == "revoke-session" {
			tag, err := conn.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, r.SessionID, r.UserID)
			if err != nil || tag.RowsAffected() != 1 {
				f.t.Fatal("exact Session revoke failed", err)
			}
		} else {
			role := "user"
			if r.Action == "promote" {
				role = "admin"
			}
			tag, err := conn.Exec(ctx, `UPDATE agenteam_account.users SET role=$1,version=version+1,updated_at=clock_timestamp() WHERE id=$2`, role, r.UserID)
			if err != nil || tag.RowsAffected() != 1 {
				f.t.Fatal("exact role preparation failed", err)
			}
		}
	default:
		f.t.Fatal("unknown Selection IPC action")
	}
	return out
}
func (f *selectionWebFixture) browserSelection(ctx context.Context) map[string]any {
	f.t.Helper()
	root, e := filepath.Abs("../account-captcha-web")
	if e != nil {
		f.t.Fatal(e)
	}
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "system-model-selection.config.js"))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "TMPDIR=") && !strings.HasPrefix(value, "AGENTEAM_AUTH_WEB_") && !strings.HasPrefix(value, "AGENTEAM_MODEL_SELECTION_WEB_") && !strings.HasPrefix(value, "PLAYWRIGHT_NO_COPY_PROMPT=") && !strings.HasPrefix(value, "DEBUG=") && !strings.HasPrefix(value, "PWDEBUG=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "TMPDIR="+f.directory, "AGENTEAM_AUTH_WEB_ORIGIN="+f.origin, "AGENTEAM_MODEL_SELECTION_WEB_CASE="+f.mode, "AGENTEAM_AUTH_WEB_PRIVATE="+f.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "PLAYWRIGHT_NO_COPY_PROMPT=1")
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
			raw, err := os.ReadFile(filepath.Join(f.directory, "selection-ipc.json"))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			var request selectionWebIPC
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
			reply := f.selectionIPC(ctx, request)
			sequence = request.Sequence
			f.private("selection-ack-"+strconv.Itoa(sequence)+".json", reply)
		}
	}
	safe := output.String()
	for _, secret := range f.secrets {
		if secret != "" {
			safe = strings.ReplaceAll(safe, secret, "[redacted]")
		}
	}
	f.mu.Lock()
	if f.lostKey != "" {
		safe = strings.ReplaceAll(safe, f.lostKey, "[redacted]")
	}
	f.mu.Unlock()
	f.t.Log(safe)
	if runErr != nil {
		f.t.Fatalf("actual Selection production browser failed: %v", runErr)
	}
	raw, e := os.ReadFile(filepath.Join(f.directory, "selection-result.json"))
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
