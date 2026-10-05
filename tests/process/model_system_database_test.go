//go:build integration

package process_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

type modelSystemBinary struct {
	p          *process
	db         *pgfixture.Database
	env        []string
	address    string
	client     *http.Client
	cookies    map[string]*http.Cookie
	csrf       string
	logPath    string
	logSecrets []string
}
type modelSystemResponse struct {
	status int
	body   []byte
	header http.Header
}

func modelSystemKey(t *testing.T) string {
	t.Helper()
	id, err := foundation.NewID[struct{}]()
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}
func newModelSystemBinary(t *testing.T) *modelSystemBinary {
	t.Helper()
	db := pgfixture.NewDatabase(t)
	env := databaseEnvironment(t, db, "AGENTEAM_CENTRAL_PUBLIC_ORIGIN=http://localhost:8080")
	v := &modelSystemBinary{db: db, env: env, client: &http.Client{Timeout: 5 * time.Second}, cookies: map[string]*http.Cookie{}}
	for _, e := range env {
		if strings.HasPrefix(e, "AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG=") {
			v.logPath = strings.TrimPrefix(e, "AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG=")
		}
	}
	v.start(t)
	t.Cleanup(v.client.CloseIdleConnections)
	t.Cleanup(func() { clear(v.cookies); v.csrf = ""; clear(v.logSecrets) })
	v.login(t)
	return v
}
func (v *modelSystemBinary) start(t *testing.T) {
	t.Helper()
	v.p = launch(t, "agenteam", nil, v.env)
	v.address = "http://" + v.p.event(t, "event", "listening")["listen_address"].(string)
}
func (v *modelSystemBinary) request(t *testing.T, method, path, key string, input any, change func(*http.Request)) modelSystemResponse {
	t.Helper()
	var raw []byte
	var err error
	if input != nil {
		raw, err = json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
	}
	defer clear(raw)
	r, err := http.NewRequest(method, v.address+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	r.Host = "localhost:8080"
	r.Header.Set("Origin", "http://localhost:8080")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Content-Type", "application/json")
	if v.csrf != "" {
		r.Header.Set("X-CSRF-Token", v.csrf)
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	for _, c := range v.cookies {
		r.AddCookie(c)
	}
	if change != nil {
		change(r)
	}
	response, err := v.client.Do(r)
	if err != nil {
		t.Fatal("owned root HTTP request failed", err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range response.Cookies() {
		if c.MaxAge < 0 {
			delete(v.cookies, c.Name)
		} else {
			v.cookies[c.Name] = c
		}
	}
	return modelSystemResponse{response.StatusCode, body, response.Header.Clone()}
}
func (r modelSystemResponse) want(t *testing.T, status int) modelSystemResponse {
	t.Helper()
	if r.status != status {
		var p struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(r.body, &p)
		t.Fatalf("root HTTP status=%d want=%d safe_code=%s", r.status, status, p.Code)
	}
	return r
}
func (r modelSystemResponse) object(t *testing.T) map[string]any {
	t.Helper()
	var out map[string]any
	if json.Unmarshal(r.body, &out) != nil {
		t.Fatal("root response was not JSON object")
	}
	return out
}
func (v *modelSystemBinary) login(t *testing.T) {
	t.Helper()
	bootstrap := v.request(t, "GET", "/api/v1/auth/bootstrap", "", nil, nil).want(t, 200).object(t)
	v.csrf = bootstrap["csrf_token"].(string)
	raw, err := os.ReadFile(v.logPath)
	if err != nil {
		t.Fatal("owned bootstrap log unavailable")
	}
	defer clear(raw)
	var email, password string
	scan := bufio.NewScanner(bytes.NewReader(raw))
	for scan.Scan() {
		var record struct {
			Purpose  string `json:"purpose"`
			Email    string `json:"email"`
			Password string `json:"initial_password"`
		}
		if json.Unmarshal(scan.Bytes(), &record) != nil {
			t.Fatal("private record malformed")
		}
		if record.Purpose == "bootstrap" {
			email, password = record.Email, record.Password
		}
	}
	if scan.Err() != nil || email == "" || password == "" {
		t.Fatal("owned bootstrap record missing")
	}
	input := map[string]any{"email": email, "password": password}
	v.request(t, "POST", "/api/v1/sessions/login", modelSystemKey(t), input, nil).want(t, 200)
	delete(input, "password")
	// The old process helper's stdout buffer is read only after p.wait. Keep
	// this owned value for that check; never print it in the test report.
	v.logSecrets = append(v.logSecrets, password)
	password = ""
	session := v.request(t, "GET", "/api/v1/session", "", nil, nil).want(t, 200).object(t)
	v.csrf = session["csrf_token"].(string)
}
func (v *modelSystemBinary) stop(t *testing.T, signal syscall.Signal) {
	t.Helper()
	if err := v.p.command.Process.Signal(signal); err != nil {
		t.Fatal(err)
	}
	v.p.wait(t, 0)
	for _, value := range v.logSecrets {
		if strings.Contains(v.p.stderr.String()+v.p.stdout.String(), value) {
			t.Fatal("ordinary process log leaked bootstrap material")
		}
	}
	noCentralBackends(t, v.db)
	assertDatabaseLogsSafe(t, v.p, v.db)
}
func modelSystemProvider(protocol string, credential any) map[string]any {
	return map[string]any{"input": map[string]any{"name": "root provider", "protocol": protocol, "base_url": "https://never-contacted.example/v1", "enabled": true, "credential_ref": credential, "options": map[string]any{}}}
}
func modelSystemModel(provider, kind string) map[string]any {
	output := "text"
	structured := []string{"text", "json_schema"}
	if kind == "embedding" {
		output = "vector"
		structured = []string{}
	}
	return map[string]any{"provider_id": provider, "input": map[string]any{"name": "root model", "provider_model_id": "owned-test-model", "type": kind, "enabled": true, "parameters": map[string]any{}, "request_overwrite": map[string]any{}, "header_overwrite": map[string]string{}, "capabilities": map[string]any{"tool_calls": false, "parallel_tool_calls": false, "streaming": kind == "chat", "reasoning": false, "input_modalities": []string{"text"}, "output_modalities": []string{output}, "reasoning_efforts": []string{}, "structured_output_modes": structured, "context_length": nil, "max_output": nil}}}
}
func modelSystemSame(t *testing.T, a, b any) {
	t.Helper()
	if !reflect.DeepEqual(a, b) {
		t.Fatal("historical receipt/configuration changed")
	}
}

func TestCentralModelSystemConfigurationAndRestart(t *testing.T) {
	v := newModelSystemBinary(t)
	selection := v.request(t, "GET", "/api/v1/system/model-selection", "", nil, nil).want(t, 200).object(t)
	if selection["configured"] != nil || selection["version"] != "1" {
		t.Fatal("root invented model defaults")
	}
	credentialKey, providerKey := modelSystemKey(t), modelSystemKey(t)
	credentialBody := map[string]any{"value": "root-model-credential-sentinel"}
	credential := v.request(t, "POST", "/api/v1/system/model-credentials", credentialKey, credentialBody, nil).want(t, 200).object(t)["credential_id"].(string)
	providerBody := modelSystemProvider("openai-chat-completions", credential)
	providerReceipt := v.request(t, "POST", "/api/v1/system/model-providers", providerKey, providerBody, nil).want(t, 200).object(t)
	provider := providerReceipt["resource_id"].(string)
	modelSystemSame(t, providerReceipt, v.request(t, "POST", "/api/v1/system/model-providers", providerKey, providerBody, nil).want(t, 200).object(t))
	lookup := v.request(t, "POST", "/api/v1/system/model-commands/lookup", providerKey, map[string]any{"command": "provider.create"}, nil).want(t, 200).object(t)
	if lookup["found"] != true {
		t.Fatal("Model receipt missing")
	}
	modelSystemSame(t, lookup["receipt"], providerReceipt)
	secretLookup := v.request(t, "POST", "/api/v1/system/model-credential-commands/lookup", credentialKey, map[string]any{"kind": "create"}, nil).want(t, 200).object(t)
	if secretLookup["observed"] != true || secretLookup["result"].(map[string]any)["credential_id"] != credential {
		t.Fatal("Secret receipt missing")
	}
	v.request(t, "PUT", "/api/v1/system/model-credentials/"+credential, modelSystemKey(t), map[string]any{"expected_version": "1", "value": "root-model-updated-sentinel"}, nil).want(t, 200)
	v.request(t, "DELETE", "/api/v1/system/model-credentials/"+credential, modelSystemKey(t), map[string]any{"expected_version": "2"}, nil).want(t, 409)
	updateProvider := modelSystemProvider("openai-chat-completions", credential)["input"].(map[string]any)
	updateProvider["name"] = "root provider updated"
	v.request(t, "PUT", "/api/v1/system/model-providers/"+provider, modelSystemKey(t), map[string]any{"expected_version": "1", "input": updateProvider}, nil).want(t, 200)
	if v.request(t, "GET", "/api/v1/system/model-providers/"+provider, "", nil, nil).want(t, 200).object(t)["version"] != "2" {
		t.Fatal("Provider update absent")
	}
	chat := v.request(t, "POST", "/api/v1/system/models", modelSystemKey(t), modelSystemModel(provider, "chat"), nil).want(t, 200).object(t)["resource_id"].(string)
	embedProvider := v.request(t, "POST", "/api/v1/system/model-providers", modelSystemKey(t), modelSystemProvider("openai-embeddings", nil), nil).want(t, 200).object(t)["resource_id"].(string)
	embed := v.request(t, "POST", "/api/v1/system/models", modelSystemKey(t), modelSystemModel(embedProvider, "embedding"), nil).want(t, 200).object(t)["resource_id"].(string)
	v.request(t, "PUT", "/api/v1/system/model-selection", modelSystemKey(t), map[string]any{"id": selection["id"], "expected_version": "1", "embedding": embed, "memory": chat, "reranker": nil, "image": nil}, nil).want(t, 200)
	configured := v.request(t, "GET", "/api/v1/system/model-selection", "", nil, nil).want(t, 200).object(t)
	if configured["id"] != selection["id"] || configured["version"] != "2" {
		t.Fatal("selector identity/version changed incorrectly")
	}
	// Independently exercise deletions without discarding the configured state
	// which the real process restart below must preserve.
	spareProvider := v.request(t, "POST", "/api/v1/system/model-providers", modelSystemKey(t), modelSystemProvider("openai-chat-completions", nil), nil).want(t, 200).object(t)["resource_id"].(string)
	body := modelSystemModel(spareProvider, "chat")
	spare := v.request(t, "POST", "/api/v1/system/models", modelSystemKey(t), body, nil).want(t, 200).object(t)["resource_id"].(string)
	body["input"].(map[string]any)["name"] = "updated spare"
	v.request(t, "PUT", "/api/v1/system/models/"+spare, modelSystemKey(t), map[string]any{"expected_version": "1", "input": body["input"]}, nil).want(t, 200)
	v.request(t, "GET", "/api/v1/system/models/"+spare, "", nil, nil).want(t, 200)
	v.request(t, "DELETE", "/api/v1/system/models/"+spare, modelSystemKey(t), map[string]any{"expected_version": "2", "replacement": nil}, nil).want(t, 200)
	v.request(t, "DELETE", "/api/v1/system/model-providers/"+spareProvider, modelSystemKey(t), map[string]any{"expected_version": "1"}, nil).want(t, 200)
	v.request(t, "GET", "/api/v1/system/models/"+spare, "", nil, nil).want(t, 404)
	spareCredential := v.request(t, "POST", "/api/v1/system/model-credentials", modelSystemKey(t), map[string]any{"value": "temporary-root-value"}, nil).want(t, 200).object(t)["credential_id"].(string)
	v.request(t, "DELETE", "/api/v1/system/model-credentials/"+spareCredential, modelSystemKey(t), map[string]any{"expected_version": "1"}, nil).want(t, 200)
	conn := v.db.Connect(t)
	var refs, broken, deliveries, handlers, subscriptions, embeddingEvents int
	if err := conn.QueryRow(databaseContext(t), `SELECT
 (SELECT count(*) FROM agenteam_secret.secret_references WHERE credential_id=$1 AND consumer='model' AND owner_id=$2),
 (SELECT count(*) FROM agenteam_model.commands c WHERE phase<>'committed' OR NOT EXISTS(SELECT 1 FROM agenteam_audit.audit_records a WHERE a.producer='model' AND a.action=c.command_name AND a.resource_id=c.resource_id AND a.metadata->>'version'=c.safe_receipt->>'version') OR NOT EXISTS(SELECT 1 FROM agenteam_outbox.events e WHERE e.id=c.event_id AND e.producer='model')),
 (SELECT count(*) FROM agenteam_outbox.deliveries d JOIN agenteam_outbox.events e ON e.id=d.event_id WHERE e.producer='model'),
 (SELECT count(*) FROM agenteam_outbox.handlers WHERE id='account.mail-enqueue'),
 (SELECT count(*) FROM agenteam_outbox.subscriptions),
 (SELECT count(*) FROM agenteam_outbox.events WHERE event_type='model.embedding_selection_changed')`, credential, provider).Scan(&refs, &broken, &deliveries, &handlers, &subscriptions, &embeddingEvents); err != nil || refs != 1 || broken != 0 || deliveries != 0 || handlers != 1 || subscriptions != 1 || embeddingEvents != 1 {
		t.Fatal("root graph durable facts", err, refs, broken, deliveries, handlers, subscriptions, embeddingEvents)
	}
	var countBefore int
	if err := conn.QueryRow(databaseContext(t), `SELECT count(*) FROM agenteam_model.commands`).Scan(&countBefore); err != nil {
		t.Fatal(err)
	}
	privateLog, err := os.ReadFile(v.logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(privateLog)
	v.stop(t, syscall.SIGTERM)
	v.start(t)
	modelSystemSame(t, configured, v.request(t, "GET", "/api/v1/system/model-selection", "", nil, nil).want(t, 200).object(t))
	modelSystemSame(t, lookup, v.request(t, "POST", "/api/v1/system/model-commands/lookup", providerKey, map[string]any{"command": "provider.create"}, nil).want(t, 200).object(t))
	modelSystemSame(t, secretLookup, v.request(t, "POST", "/api/v1/system/model-credential-commands/lookup", credentialKey, map[string]any{"kind": "create"}, nil).want(t, 200).object(t))
	modelSystemSame(t, providerReceipt, v.request(t, "POST", "/api/v1/system/model-providers", providerKey, providerBody, nil).want(t, 200).object(t))
	v.request(t, "POST", "/api/v1/system/model-credentials", credentialKey, credentialBody, nil).want(t, 200)
	var countAfter int
	if err := conn.QueryRow(databaseContext(t), `SELECT count(*) FROM agenteam_model.commands`).Scan(&countAfter); err != nil || countAfter != countBefore {
		t.Fatal("restart/replay created commands", err)
	}
	after, err := os.ReadFile(v.logPath)
	defer clear(after)
	if err != nil || !bytes.Equal(privateLog, after) {
		t.Fatal("restart changed restricted bootstrap log")
	}
	v.stop(t, syscall.SIGINT)
	for _, sentinel := range []string{"root-model-credential-sentinel", "root-model-updated-sentinel", "temporary-root-value"} {
		if strings.Contains(v.p.stderr.String()+v.p.stdout.String(), sentinel) {
			t.Fatal("ordinary root output leaked credential")
		}
	}
}

func TestCentralModelSystemCurrentAuthorityAndRouting(t *testing.T) {
	v := newModelSystemBinary(t)
	credentialKey, providerKey := modelSystemKey(t), modelSystemKey(t)
	credentialBody := map[string]any{"value": "root-current-authority-value"}
	v.request(t, "POST", "/api/v1/system/model-credentials", credentialKey, credentialBody, nil).want(t, 200)
	providerBody := modelSystemProvider("openai-chat-completions", nil)
	v.request(t, "POST", "/api/v1/system/model-providers", providerKey, providerBody, nil).want(t, 200)
	sessionCookies := map[string]*http.Cookie{}
	for name, c := range v.cookies {
		copyCookie := *c
		sessionCookies[name] = &copyCookie
	}
	restoreCookies := func() {
		v.cookies = map[string]*http.Cookie{}
		for name, c := range sessionCookies {
			copyCookie := *c
			v.cookies[name] = &copyCookie
		}
	}
	for _, tc := range []struct {
		name   string
		change func(*http.Request)
		status int
	}{
		{"anonymous", func(r *http.Request) { r.Header.Del("Cookie") }, 401},
		{"wrong-host", func(r *http.Request) { r.Host = "untrusted.invalid" }, 403},
		{"wrong-origin", func(r *http.Request) { r.Header.Set("Origin", "https://untrusted.invalid") }, 403},
		{"missing-csrf", func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			restoreCookies()
			defer restoreCookies()
			// Each case starts with the actual issued, still-current Session.
			// An anonymous response legitimately clears a browser's cookie;
			// that must not turn the next CSRF case into another anonymous one.
			session := v.request(t, "GET", "/api/v1/session", "", nil, nil).want(t, 200).object(t)
			if session["csrf_token"] != v.csrf {
				t.Fatal("negative case lost current Session/CSRF prerequisite")
			}
			response := v.request(t, "POST", "/api/v1/system/model-commands/lookup", providerKey, map[string]any{"command": "provider.create"}, tc.change).want(t, tc.status)
			if tc.name == "anonymous" {
				cleared := false
				for _, c := range (&http.Response{Header: response.header}).Cookies() {
					if c.Name == "agenteam_local_session" && c.MaxAge < 0 {
						cleared = true
					}
				}
				if !cleared {
					t.Fatal("anonymous response did not clear the actual session cookie")
				}
			}
		})
	}
	head := v.request(t, "HEAD", "/api/v1/system/model-providers", "", nil, nil).want(t, 200)
	if len(head.body) != 0 {
		t.Fatal("HEAD returned body")
	}
	v.request(t, "PATCH", "/api/v1/system/model-providers", "", nil, nil).want(t, 405)
	for _, path := range []string{"/api/v1/system/model-providers-extra", "/api/v1/system/model-call", "/api/v1/model-invocations", "/api/v1/projects"} {
		v.request(t, "GET", path, "", nil, nil).want(t, 404)
	}
	v.request(t, "GET", "/api/v1/session", "", nil, nil).want(t, 200)
	ready := v.request(t, "GET", "/readyz", "", nil, nil).want(t, 503)
	if !bytes.Contains(ready.body, []byte("DEPENDENCY_UNBOUND")) {
		t.Fatal("false root ready")
	}
	diagnostic := v.request(t, "GET", "/diagnostics", "", nil, nil).want(t, 200).object(t)
	if diagnostic["ready"] != false {
		t.Fatal("diagnostics claimed ready")
	}
	response := v.request(t, "GET", "/api/v1/system/model-selection", "", nil, nil).want(t, 200)
	requestID := response.header.Get("X-Request-ID")
	count := 0
	for _, line := range strings.Split(v.p.stderr.String(), "\n") {
		var e map[string]any
		if json.Unmarshal([]byte(line), &e) == nil && e["event"] == "http_request" && e["request_id"] == requestID {
			count++
		}
	}
	if requestID == "" || count != 1 {
		t.Fatal("root added duplicate middleware", count)
	}
	oldCookies := map[string]*http.Cookie{}
	for k, c := range v.cookies {
		x := *c
		oldCookies[k] = &x
	}
	oldCSRF := v.csrf
	v.request(t, "POST", "/api/v1/sessions/logout", modelSystemKey(t), map[string]any{}, nil).want(t, 204)
	// Keep the old cookie snapshot independent of Set-Cookie clearing on each
	// rejection, so all four probes really replay the same revoked Session.
	v.cookies, v.csrf = map[string]*http.Cookie{}, oldCSRF
	var before int
	if err := v.db.Connect(t).QueryRow(databaseContext(t), `SELECT (SELECT count(*) FROM agenteam_model.commands)+(SELECT count(*) FROM agenteam_secret.secret_command_receipts)`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, key string
		body      any
	}{
		{"/api/v1/system/model-providers", providerKey, providerBody},
		{"/api/v1/system/model-commands/lookup", providerKey, map[string]any{"command": "provider.create"}},
		{"/api/v1/system/model-credentials", credentialKey, credentialBody},
		{"/api/v1/system/model-credential-commands/lookup", credentialKey, map[string]any{"kind": "create"}},
	} {
		v.request(t, "POST", tc.path, tc.key, tc.body, func(r *http.Request) {
			r.Header.Del("Cookie")
			for _, c := range oldCookies {
				r.AddCookie(c)
			}
		}).want(t, 401)
	}
	var after int
	if err := v.db.Connect(t).QueryRow(databaseContext(t), `SELECT (SELECT count(*) FROM agenteam_model.commands)+(SELECT count(*) FROM agenteam_secret.secret_command_receipts)`).Scan(&after); err != nil || after != before {
		t.Fatal("revoked session changed historical facts", err)
	}
	v.stop(t, syscall.SIGTERM)
}

func TestCentralModelInitializationFailureBeforeListen(t *testing.T) {
	for _, mode := range []string{"storage", "ordinary-lock-cancel"} {
		t.Run(mode, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			m, err := postgres.NewMigrator(db.Config(t, nil))
			if err != nil {
				t.Fatal(err)
			}
			if r := m.Migrate(databaseContext(t)); !r.Migrated {
				t.Fatal(r.Fault)
			}
			conn := db.Connect(t)
			if mode == "storage" {
				if _, err = conn.Exec(databaseContext(t), `ALTER TABLE agenteam_model.providers RENAME TO unavailable_providers`); err != nil {
					t.Fatal(err)
				}
				p := launch(t, "agenteam", nil, databaseEnvironment(t, db))
				p.wait(t, 1)
				if strings.Contains(p.stderr.String(), `"event":"listening"`) || strings.Contains(p.stderr.String(), `"phase":"secret_maintenance_starting"`) {
					t.Fatal("Model initialization failure reached later stage")
				}
				var secretRows, selectionRows, claims int
				if err = conn.QueryRow(databaseContext(t), `SELECT (SELECT count(*) FROM agenteam_secret.secret_master_registry WHERE canary_ciphertext IS NOT NULL),(SELECT count(*) FROM agenteam_model.platform_selection),(SELECT count(*) FROM agenteam_object.process_claims)`).Scan(&secretRows, &selectionRows, &claims); err != nil || secretRows != 1 || selectionRows != 0 || claims != 0 {
					t.Fatal("failure was not after real Secret/before Model and Object init", err, secretRows, selectionRows, claims)
				}
				noCentralBackends(t, db)
				assertDatabaseLogsSafe(t, p, db)
				return
			}
			tx, err := conn.Begin(databaseContext(t))
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			if _, err = tx.Exec(databaseContext(t), `LOCK TABLE agenteam_model.platform_selection IN ACCESS EXCLUSIVE MODE`); err != nil {
				t.Fatal(err)
			}
			p := launch(t, "agenteam", nil, databaseEnvironment(t, db, "AGENTEAM_CENTRAL_DATABASE_LOCK_TIMEOUT=10s"))
			waitDatabaseFact(t, db.Connect(t), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam' AND $1=ANY(pg_blocking_pids(pid)) AND position('agenteam_model.platform_selection' in query)>0)`, int32(conn.PgConn().PID()))
			if strings.Contains(p.stderr.String(), `"event":"listening"`) {
				t.Fatal("Model startup lock wait listened")
			}
			if err = p.command.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			p.wait(t, 0)
			if err = tx.Rollback(databaseContext(t)); err != nil {
				t.Fatal(err)
			}
			noCentralBackends(t, db)
			assertDatabaseLogsSafe(t, p, db)
		})
	}
}
