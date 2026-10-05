//go:build integration

package account_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/app"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	"github.com/LunaDeerTech/agenteam/tests/testsupport/accountenv"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

const httpOrigin = "http://localhost:8080"

type httpFixtureLog struct {
	mu        sync.Mutex
	data      bytes.Buffer
	listening chan string
}

func (l *httpFixtureLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.data.Write(p)
	var entry struct {
		Event   string `json:"event"`
		Address string `json:"listen_address"`
	}
	if json.Unmarshal(bytes.TrimSpace(p), &entry) == nil && entry.Event == "listening" {
		select {
		case l.listening <- entry.Address:
		default:
		}
	}
	return len(p), nil
}
func (l *httpFixtureLog) contains(value string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Contains(l.data.String(), value)
}
func (l *httpFixtureLog) requests(id string) []map[string]any {
	l.mu.Lock()
	defer l.mu.Unlock()
	var result []map[string]any
	for _, line := range bytes.Split(l.data.Bytes(), []byte{'\n'}) {
		var entry map[string]any
		if json.Unmarshal(line, &entry) == nil && entry["event"] == "http_request" && entry["request_id"] == id {
			result = append(result, entry)
		}
	}
	return result
}

type httpFixture struct {
	t       *testing.T
	db      *pgfixture.Database
	config  config.Config
	address string
	log     *httpFixtureLog
}
type httpBrowser struct {
	f             *httpFixture
	client        *http.Client
	transport     *http.Transport
	csrf          string
	anonymousCSRF string
}
type httpResult struct {
	status  int
	headers http.Header
	data    []byte
}

func (r httpResult) object(t *testing.T) map[string]any {
	t.Helper()
	var out map[string]any
	if json.Unmarshal(r.data, &out) != nil {
		t.Fatal("HTTP response is not a JSON object")
	}
	return out
}
func (r httpResult) want(t *testing.T, status int) httpResult {
	t.Helper()
	if r.status != status {
		t.Fatalf("HTTP status=%d want=%d", r.status, status)
	}
	if r.headers.Get("Cache-Control") != "no-store" || r.headers.Get("X-Content-Type-Options") != "nosniff" || r.headers.Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("account response lost security headers")
	}
	return r
}
func (r httpResult) problem(t *testing.T, status int, code string) {
	t.Helper()
	out := r.want(t, status).object(t)
	if out["code"] != code {
		t.Fatalf("HTTP Problem code=%v want=%s", out["code"], code)
	}
	if len(r.headers.Values("Set-Cookie")) != 0 && status != http.StatusUnauthorized {
		t.Fatal("failed request changed a cookie")
	}
}

func newHTTPFixture(t *testing.T) *httpFixture {
	t.Helper()
	db := pgfixture.NewDatabase(t)
	inputs := accountenv.New(t)
	objects, err := objectfixture.Environment(ctxFor(t), db.Name)
	if err != nil {
		t.Fatal("HTTP fixture owned object environment unavailable")
	}
	values := inputs.Values()
	for _, entry := range objects {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	for key, value := range map[string]string{
		"DATABASE_URL": db.Fixture.URL(db.Name), "DATABASE_CA_FILE": db.Fixture.CAFile,
		"DATABASE_STARTUP_TIMEOUT": "15s", "HTTP_ADDR": "127.0.0.1:0", "PUBLIC_ORIGIN": httpOrigin, "SHUTDOWN_TIMEOUT": "2s",
		"CURSOR_KEYRING": `{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`,
		"SECRET_KEYRING": `{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="}]}`,
	} {
		values[config.Prefix+key] = value
	}
	var environment []string
	for key, value := range values {
		environment = append(environment, key+"="+value)
	}
	cfg, err := config.Load(func(key string) (string, bool) { value, ok := values[key]; return value, ok }, environment)
	if err != nil {
		t.Fatal(err)
	}
	output := &httpFixtureLog{listening: make(chan string, 1)}
	logger, err := logging.New(logging.Central, slog.LevelInfo, output)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, cfg, logger, nil) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error("real HTTP root failed graceful shutdown", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("real HTTP root did not finish the original shutdown budget")
		}
	})
	var address string
	select {
	case address = <-output.listening:
	case err := <-done:
		// Keep cleanup from waiting on a result already observed here.
		done <- err
		t.Fatal("real HTTP root initialization failed", err)
	case <-time.After(40 * time.Second):
		t.Fatal("real HTTP root never installed its listener")
	}
	return &httpFixture{t: t, db: db, config: cfg, address: address, log: output}
}

func (f *httpFixture) browser() *httpBrowser {
	f.t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		f.t.Fatal(err)
	}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, f.address)
	}}
	f.t.Cleanup(transport.CloseIdleConnections)
	return &httpBrowser{f: f, transport: transport, client: &http.Client{Transport: transport, Jar: jar, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (b *httpBrowser) raw(method, path string, body []byte, change func(*http.Request)) httpResult {
	b.f.t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	r, err := http.NewRequest(method, httpOrigin+path, reader)
	if err != nil {
		b.f.t.Fatal("invalid test request")
	}
	r.Header.Set("Origin", httpOrigin)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if change != nil {
		change(r)
	}
	response, err := b.client.Do(r)
	if err != nil {
		b.f.t.Fatal("HTTP fixture request failed", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (6<<20)+1))
	if err != nil || len(data) > 6<<20 {
		b.f.t.Fatal("HTTP response did not terminate within its expected body limit")
	}
	return httpResult{response.StatusCode, response.Header.Clone(), data}
}
func (b *httpBrowser) request(method, path string, body any, key, csrf string) httpResult {
	b.f.t.Helper()
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			b.f.t.Fatal("invalid fixture body")
		}
	}
	return b.raw(method, path, data, func(r *http.Request) {
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
	})
}
func (b *httpBrowser) bootstrap() httpResult {
	b.f.t.Helper()
	r := b.request("GET", "/api/v1/auth/bootstrap", nil, "", "").want(b.f.t, 200)
	body := r.object(b.f.t)
	b.anonymousCSRF, _ = body["csrf_token"].(string)
	if b.anonymousCSRF == "" || body["delivery_channel"] != "backend_log" {
		b.f.t.Fatal("anonymous context or recovery channel unavailable")
	}
	return r
}
func (b *httpBrowser) login(email, password, key string) httpResult {
	b.f.t.Helper()
	if b.anonymousCSRF == "" {
		b.bootstrap()
	}
	r := b.request("POST", "/api/v1/sessions/login", map[string]any{"email": email, "password": password}, key, b.anonymousCSRF).want(b.f.t, 200)
	session := b.request("GET", "/api/v1/session", nil, "", "").want(b.f.t, 200).object(b.f.t)
	b.csrf, _ = session["csrf_token"].(string)
	if b.csrf == "" {
		b.f.t.Fatal("session CSRF missing")
	}
	return r
}
func (f *httpFixture) admin() *httpBrowser {
	f.t.Helper()
	entry := f.record("bootstrap", "admin@mail.com", "")
	b := f.browser()
	b.login(entry.Email, entry.Password, id[struct{}](f.t).String())
	if f.log.contains(entry.Password) {
		f.t.Fatal("bootstrap password escaped dedicated log")
	}
	return b
}

type httpRecoveryRecord struct {
	Purpose  string `json:"purpose"`
	ID       string `json:"id"`
	Email    string `json:"email"`
	Password string `json:"initial_password"`
	URL      string `json:"url"`
}

func (f *httpFixture) record(purpose, email, exactID string) httpRecoveryRecord {
	f.t.Helper()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		data, err := os.ReadFile(f.config.AccountRecoveryLog())
		if err != nil {
			f.t.Fatal("owned recovery log unavailable")
		}
		reader := bufio.NewScanner(bytes.NewReader(data))
		for reader.Scan() {
			var entry httpRecoveryRecord
			if json.Unmarshal(reader.Bytes(), &entry) == nil && entry.Purpose == purpose && entry.Email == email && (exactID == "" || entry.ID == exactID) {
				clear(data)
				return entry
			}
		}
		clear(data)
		select {
		case <-ticker.C:
		case <-deadline.C:
			f.t.Fatal("exact owned recovery delivery did not complete")
		}
	}
}
func (f *httpFixture) invite(admin *httpBrowser, email, username string) *httpBrowser {
	f.t.Helper()
	created := admin.request("POST", "/api/v1/system/invitations", map[string]any{"email": email}, id[struct{}](f.t).String(), admin.csrf).want(f.t, 201).object(f.t)
	entry := f.record("invitation", email, created["id"].(string))
	u, err := url.Parse(entry.URL)
	if err != nil || u.Scheme+"://"+u.Host != httpOrigin || u.Fragment == "" {
		f.t.Fatal("invitation URL was not derived from configured origin")
	}
	b := f.browser()
	b.bootstrap()
	b.request("POST", "/api/v1/invitations/inspect", map[string]any{"token": u.Fragment}, "", b.anonymousCSRF).want(f.t, 200)
	password := "Http-Invitation-Secret-42!"
	body := map[string]any{"token": u.Fragment, "username": username, "display_name": "HTTP fixture", "password": password, "confirmation": password}
	b.request("POST", "/api/v1/invitations/redeem", body, id[struct{}](f.t).String(), b.anonymousCSRF).want(f.t, 201)
	b.login(email, password, id[struct{}](f.t).String())
	if f.log.contains(u.Fragment) || f.log.contains(password) {
		f.t.Fatal("invitation material escaped into ordinary diagnostics")
	}
	return b
}

func httpString(t *testing.T, object map[string]any, key string) string {
	t.Helper()
	v, ok := object[key].(string)
	if !ok || v == "" {
		t.Fatalf("HTTP field %s is not a nonempty string", key)
	}
	return v
}
func httpObject(t *testing.T, object map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := object[key].(map[string]any)
	if !ok {
		t.Fatalf("HTTP field %s is not an object", key)
	}
	return v
}
func httpItems(t *testing.T, object map[string]any) []any {
	t.Helper()
	v, ok := object["items"].([]any)
	if !ok {
		t.Fatal("HTTP list items is not an array")
	}
	return v
}
func (b *httpBrowser) profile() map[string]any {
	return b.request("GET", "/api/v1/me", nil, "", "").want(b.f.t, 200).object(b.f.t)
}
func (f *httpFixture) assertTrace(result httpResult, route string) {
	f.t.Helper()
	requestID := result.headers.Get("X-Request-ID")
	if requestID == "" || result.object(f.t)["request_id"] != requestID {
		f.t.Fatal("real root response and Problem identity differ")
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		entries := f.log.requests(requestID)
		if len(entries) != 0 {
			if len(entries) != 1 || entries[0]["route"] != route {
				f.t.Fatal("real root did not log exactly one request at its matched route")
			}
			return
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			f.t.Fatal("real root log does not share the response/Problem request identity")
		}
	}
}
func (b *httpBrowser) cookie(name string) string {
	u, _ := url.Parse(httpOrigin)
	for _, c := range b.client.Jar.Cookies(u) {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}
func (b *httpBrowser) setCookie(name, value string) {
	u, _ := url.Parse(httpOrigin)
	b.client.Jar.SetCookies(u, []*http.Cookie{{Name: name, Value: value, Path: "/"}})
}
func (b *httpBrowser) waitMail(job, phase string) map[string]any {
	b.f.t.Helper()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		view := b.request("GET", "/api/v1/system/mail-jobs/"+job, nil, "", "").want(b.f.t, 200).object(b.f.t)
		if view["phase"] == phase {
			return view
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			b.f.t.Fatal("mail job did not reach the required durable phase")
		}
	}
}
