//go:build integration

package account_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
)

func TestProjectModelsWebBrowserFailureProjection(t *testing.T) {
	private := "private-canary-key-value-cookie-never-exported"
	raw := []byte("ReferenceError: " + private + "\n at /private/project-owner-models.spec.ts:713:22\n at /private/project-owner-models.spec.ts:713:22\n at unrelated.spec.ts:999:1\n TypeError: " + private)
	value := projectModelsWebBrowserFailure(raw, 1, false)
	encoded, err := json.Marshal(value)
	if err != nil || bytes.Contains(encoded, []byte(private)) || bytes.Contains(encoded, []byte("/private")) || bytes.Contains(encoded, []byte("999")) {
		t.Fatal("runner failure projection exported private diagnostics")
	}
	if string(encoded) != `{"context_done":false,"exit_code":1,"reported_categories":["reference-error-reported","type-error-reported"],"spec_locations":[{"column":22,"line":713}]}` {
		t.Fatal("runner failure projection did not preserve closed categories and unique own location")
	}
	unknown, _ := json.Marshal(projectModelsWebBrowserFailure([]byte(private), -1, true))
	if string(unknown) != `{"context_done":true,"exit_code":-1,"reported_categories":[],"spec_locations":[]}` {
		t.Fatal("unknown runner diagnostic escaped closed projection")
	}
}

func TestProjectModelsWebResultCounters(t *testing.T) {
	checks, err := projectModelsWebChecks("recovery")
	if err != nil {
		t.Fatal(err)
	}
	confirmed := map[string]bool{}
	for _, name := range checks {
		confirmed[name] = true
	}
	operations := []any{}
	attachment, err := os.ReadFile("../../docs/development/work-items/d27-project-owner-model-settings-ui-endpoints.json")
	if err != nil {
		t.Fatal(err)
	}
	declared, err := projectModelsWebLoadOperations(attachment)
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range declared {
		operations = append(operations, map[string]any{"operation": operation.Operation, "setup": 0, "browser": 0, "control": 0, "upstream_complete": 0, "handler_joined": 0})
	}
	server := map[string]any{"operations": operations, "session": map[string]int{"setup": 0, "browser": 0, "control": 0}, "server": map[string]int{"started": 0, "finished": 0}, "controls": map[string]int{"armed": 0, "claimed": 0, "held": 0, "held_joined": 0, "cut": 0, "disconnected": 0}, "browser_eof": nil, "schema_bodies": nil, "client_bodies": nil}
	for _, test := range []struct {
		name                                     string
		attempts, eof, incomplete, typed, schema int
		valid                                    bool
	}{
		{"consistent", 1, 1, 0, 1, 1, true}, {"zero-attempts-with-typed-body", 0, 0, 0, 1, 1, false}, {"unverified-complete-body", 2, 2, 0, 1, 1, false}, {"missing-complete-eof", 2, 0, 2, 1, 1, false}, {"impossible-attempt-decomposition", 2, 1, 0, 1, 1, false}, {"schema-count-differs", 1, 1, 0, 1, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			browser := map[string]int{"attempts": test.attempts, "complete_eof": test.eof, "incomplete": test.incomplete, "typed_client_ok": test.typed, "schema_ok": test.schema}
			raw, _ := json.Marshal(map[string]any{"protocol": projectModelsWebProtocol, "input_hash": "pure-boundary", "completed": true, "mode": "recovery", "checks": confirmed, "counts": map[string]any{"server": server, "browser": browser}, "schema_bodies": 1, "client_bodies": 1, "layouts": 0})
			_, err := decodeProjectModelsWebResult(raw, "recovery", "pure-boundary")
			if (err == nil) != test.valid {
				t.Fatal("final count boundary admitted an impossible observation")
			}
		})
	}
}

func TestProjectModelsWebNativeWitness(t *testing.T) {
	project := "018f1234-5678-7abc-8def-123456789abc"
	op := projectModelsWebOperation{Operation: "listProjectModelProviders", Method: "GET", Path: "/model-providers", Family: "read", Query: "page"}
	f := &projectModelsWebFixture{projectOwnerAuditWebFixture: &projectOwnerAuditWebFixture{projectOwnerWebFixture: &projectOwnerWebFixture{authenticationWebFixture: &authenticationWebFixture{t: t}, evidence: t.TempDir(), inputHash: strings.Repeat("0", 64)}}, registry: projectModelsWebRegistry{Projects: map[string]string{"main": project}, Operations: []projectModelsWebOperation{op}}, modelLastCounts: json.RawMessage(`{}`)}
	path := "/api/v1/projects/" + project + "/model-providers"
	// '&' has different JSON escaping in Go and Node. Correlation compares its
	// value, while response evidence and body hash keep the original bytes.
	query := "cursor=opaque-fixture-cursor&limit=25"
	body := []byte(`{"items":[],"next_cursor":null}`)
	request := &projectModelsWebRequest{Token: "r000001", Source: "browser", Project: "main", Operation: &op}
	response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}, "Content-Length": []string{fmt.Sprint(len(body))}, "X-Request-Id": []string{project}}, Request: &http.Request{Method: "GET", URL: &url.URL{Path: path, RawQuery: query}}}
	if err := f.saveModelResponse(request, response, body); err != nil {
		t.Fatal(err)
	}
	write := func(name string, value any) {
		t.Helper()
		raw, _ := json.Marshal(value)
		if err := os.WriteFile(filepath.Join(f.evidence, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("same-body-input.json", []any{map[string]any{"sidecar": "response-001.json", "request_token": "r000001", "browser_eof": true, "bytes": len(body), "sha256": fmt.Sprintf("%x", sha256.Sum256(body))}})
	counts := json.RawMessage(`{"server":{},"browser":{"attempts":1,"complete_eof":1,"incomplete":0}}`)
	result := projectModelsWebResult{Counts: counts, SchemaBodies: 1, ClientBodies: 1}
	fact := func() map[string]any {
		return map[string]any{"token": "r000001", "method": "GET", "path": path, "query": query, "status": 200, "eof": true, "ended": true, "cancelled": false, "released": true, "bytes": len(body), "chunks": []int{len(body)}, "has_body": false, "mutation_headers": false}
	}
	write("native-browser-observations.json", []any{fact()})
	if err := f.verifyModelBrowserEvidence(result); err != nil {
		t.Fatal("consistent pure native witness rejected")
	}
	for _, key := range []string{"token", "eof", "released", "status", "bytes"} {
		t.Run(key, func(t *testing.T) {
			v := fact()
			switch key {
			case "token":
				v[key] = "r000002"
			case "eof", "released":
				v[key] = false
			case "status":
				v[key] = 201
			case "bytes":
				v[key] = len(body) + 1
			}
			write("native-browser-observations.json", []any{v})
			if f.verifyModelBrowserEvidence(result) == nil {
				t.Fatal("mismatched native witness accepted")
			}
		})
	}
	write("native-browser-observations.json", []any{})
	if f.verifyModelBrowserEvidence(result) == nil {
		t.Fatal("missing native witness accepted")
	}
}

func runProjectModelsWeb(t *testing.T, mode string) {
	t.Helper()
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(func() {
		cancel()
		if time.Since(started) > 120*time.Second {
			t.Error("Project Models top exceeded 120 seconds including actual cleanup")
		}
	})
	f := newProjectModelsWebFixture(t, ctx, mode)
	result := f.browserModels(ctx)
	f.stopProxy()
	f.mu.Lock()
	retired := f.modelServer.Started == f.modelServer.Finished && f.modelControls.Held == f.modelControls.HeldJoined && !f.modelFailure
	f.mu.Unlock()
	if !retired {
		t.Fatal("Project Models proxy callbacks did not actually retire")
	}
	f.safeEvidence("go-facts.json", map[string]any{"protocol": projectModelsWebProtocol, "input_hash": f.inputHash, "mode": mode, "proxy_actual_join": retired, "browser_result": result, "auxiliary_skills_and_lifecycle_only": true})
}

func TestAccountProjectOwnerModelsWebConfigurationLifecycle(t *testing.T) {
	runProjectModelsWeb(t, "configuration")
}
func TestAccountProjectOwnerModelsWebCredentialLifecycle(t *testing.T) {
	runProjectModelsWeb(t, "credential")
}
func TestAccountProjectOwnerModelsWebOriginalRecovery(t *testing.T) {
	runProjectModelsWeb(t, "recovery")
}
func TestAccountProjectOwnerModelsWebReadAndPagination(t *testing.T) { runProjectModelsWeb(t, "read") }
func TestAccountProjectOwnerModelsWebAuthorityAndIdentity(t *testing.T) {
	runProjectModelsWeb(t, "authority")
}
func TestAccountProjectOwnerModelsWebNavigationAndLayouts(t *testing.T) {
	runProjectModelsWeb(t, "navigation")
}

// Completion here is diagnostic evidence, never an authority/business PASS.
func TestAccountProjectOwnerModelsWebSessionProxyDiagnostic(t *testing.T) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(func() {
		cancel()
		if time.Since(started) > 120*time.Second {
			t.Error("Session proxy diagnostic exceeded original top budget")
		}
	})
	f := newProjectModelsWebFixture(t, ctx, "configuration")
	f.browserSessionProxyDiagnostic(ctx)
	f.stopProxy()
	f.mu.Lock()
	defer f.mu.Unlock()
	modelRequests := 0
	for _, count := range f.modelCounts {
		modelRequests += count.Browser
	}
	joined := f.modelServer.Started == f.modelServer.Finished && f.modelControls.Held == f.modelControls.HeldJoined && !f.modelFailure
	if !joined || f.modelSessionCounts["browser"] != 1 || modelRequests != 0 {
		t.Fatal("isolated Session proxy diagnostic request or callback ownership invalid")
	}
	f.safeEvidence("go-session-proxy-diagnostic.json", map[string]any{"protocol": "project-session-proxy.v1", "input_hash": f.inputHash, "diagnostic_completed": true, "proxy_actual_join": joined, "browser_session_requests": 1, "browser_model_requests": 0, "authority_business_pass": false})
}

func TestAccountProjectOwnerModelsWebSessionAppDiagnostic(t *testing.T) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(func() {
		cancel()
		if time.Since(started) > 120*time.Second {
			t.Error("Session App diagnostic exceeded original top budget")
		}
	})
	f := newProjectModelsWebFixture(t, ctx, "configuration")
	f.browserSessionProxyDiagnostic(ctx, true)
	f.stopProxy()
	f.mu.Lock()
	defer f.mu.Unlock()
	modelRequests := 0
	for _, count := range f.modelCounts {
		modelRequests += count.Browser
	}
	joined := f.modelServer.Started == f.modelServer.Finished && f.modelControls.Held == f.modelControls.HeldJoined && !f.modelFailure
	if !joined || f.modelSessionCounts["browser"] != 4 || modelRequests != 0 || f.sessionDiagnostic == nil || f.sessionDiagnostic.Compared != 4 || f.sessionDiagnostic.Matched != 4 {
		t.Fatal("App Session diagnostic request, identity or callback ownership invalid")
	}
	f.safeEvidence("go-session-app-diagnostic.json", map[string]any{"protocol": "project-session-proxy.v1", "input_hash": f.inputHash, "diagnostic_completed": true, "proxy_actual_join": joined, "browser_session_requests": 4, "browser_model_requests": 0, "session_identity_matches": 4, "authority_business_pass": false})
}

func TestAccountProjectOwnerModelsWebBrowserLoginDiagnostic(t *testing.T) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(func() {
		cancel()
		if time.Since(started) > 120*time.Second {
			t.Error("browser login diagnostic exceeded original top budget")
		}
	})
	f := newProjectModelsWebFixture(t, ctx, "configuration")
	f.browserSessionDiagnostic(ctx, "owned-login")
	f.stopProxy()
	f.mu.Lock()
	defer f.mu.Unlock()
	modelRequests := 0
	for _, count := range f.modelCounts {
		modelRequests += count.Browser
	}
	joined := f.modelServer.Started == f.modelServer.Finished && f.modelControls.Held == f.modelControls.HeldJoined && !f.modelFailure
	d := f.sessionDiagnostic
	sessions := f.modelSessionCounts["browser"]
	if !joined || modelRequests != 0 || d == nil || sessions < 6 || sessions > 10 || d.Compared < 4 || d.Compared > 8 || d.Matched != d.Compared || d.Unauthenticated < 2 || d.Unauthenticated > 4 || sessions != d.Compared+d.Unauthenticated || d.Bootstrap != d.Unauthenticated || d.Logins != 2 || d.NewSessions != 2 {
		t.Fatal("browser login diagnostic request, identity or callback ownership invalid")
	}
	f.safeEvidence("go-browser-login-diagnostic.json", map[string]any{"protocol": "project-session-proxy.v1", "input_hash": f.inputHash, "diagnostic_completed": true, "proxy_actual_join": joined, "browser_session_requests": sessions, "browser_model_requests": 0, "session_identity_matches": d.Matched, "new_sessions": d.NewSessions, "unauthenticated": d.Unauthenticated, "bootstrap": d.Bootstrap, "logins": d.Logins, "authority_business_pass": false})
}

func TestAccountProjectOwnerModelsWebBrowserLoginActionDiagnostic(t *testing.T) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(func() {
		cancel()
		if time.Since(started) > 120*time.Second {
			t.Error("browser login action diagnostic exceeded original top budget")
		}
	})
	f := newProjectModelsWebFixture(t, ctx, "configuration")
	f.browserSessionDiagnostic(ctx, "owned-login-action")
	f.stopProxy()
	f.mu.Lock()
	defer f.mu.Unlock()
	modelRequests := 0
	for _, count := range f.modelCounts {
		modelRequests += count.Browser
	}
	joined := f.modelServer.Started == f.modelServer.Finished && f.modelControls.Held == f.modelControls.HeldJoined && !f.modelFailure
	d := f.sessionDiagnostic
	sessions := f.modelSessionCounts["browser"]
	if !joined || modelRequests != 0 || d == nil || sessions < 6 || sessions > 10 || d.Compared < 4 || d.Compared > 8 || d.Matched != d.Compared || d.Unauthenticated < 2 || d.Unauthenticated > 4 || sessions != d.Compared+d.Unauthenticated || d.Bootstrap != d.Unauthenticated || d.Logins != 2 || d.NewSessions != 2 {
		t.Fatal("browser login action diagnostic request, identity or callback ownership invalid")
	}
	f.safeEvidence("go-browser-login-action-diagnostic.json", map[string]any{"protocol": "project-session-proxy.v1", "input_hash": f.inputHash, "diagnostic_completed": true, "proxy_actual_join": joined, "browser_session_requests": sessions, "browser_model_requests": 0, "session_identity_matches": d.Matched, "new_sessions": d.NewSessions, "unauthenticated": d.Unauthenticated, "bootstrap": d.Bootstrap, "logins": d.Logins, "authority_business_pass": false})
}

func TestAccountProjectOwnerModelsWebBrowserLoginOwnerDiagnostic(t *testing.T) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(func() {
		cancel()
		if time.Since(started) > 120*time.Second {
			t.Error("browser login owner diagnostic exceeded original top budget")
		}
	})
	f := newProjectModelsWebFixture(t, ctx, "configuration")
	f.browserSessionDiagnostic(ctx, "owned-login-owner")
	f.stopProxy()
	f.mu.Lock()
	defer f.mu.Unlock()
	modelRequests := 0
	for _, count := range f.modelCounts {
		modelRequests += count.Browser
	}
	joined := f.modelServer.Started == f.modelServer.Finished && f.modelControls.Held == f.modelControls.HeldJoined && !f.modelFailure
	d := f.sessionDiagnostic
	sessions := f.modelSessionCounts["browser"]
	if !joined || modelRequests != 0 || d == nil || sessions < 3 || sessions > 4 || d.Compared != 2 || d.Matched != d.Compared || d.Unauthenticated < 1 || d.Unauthenticated > 2 || sessions != d.Compared+d.Unauthenticated || d.Bootstrap != d.Unauthenticated || d.Logins != 1 || d.NewSessions != 1 {
		t.Fatal("browser login owner diagnostic request, identity or callback ownership invalid")
	}
	f.safeEvidence("go-browser-login-owner-diagnostic.json", map[string]any{"protocol": "project-session-proxy.v1", "input_hash": f.inputHash, "diagnostic_completed": true, "proxy_actual_join": joined, "browser_session_requests": sessions, "browser_model_requests": 0, "session_identity_matches": d.Matched, "new_sessions": d.NewSessions, "unauthenticated": d.Unauthenticated, "bootstrap": d.Bootstrap, "logins": d.Logins, "authority_business_pass": false})
}

func TestProjectModelsWebLoginDiagnosticIdentity(t *testing.T) {
	expected := projectModelsWebSession{ID: "setup-session", User: "owner", Cookie: "setup-cookie", CSRF: "setup-csrf"}
	first := projectModelsWebSession{ID: "first-session", User: "owner", Cookie: "first-cookie", CSRF: "first-csrf"}
	second := projectModelsWebSession{ID: "second-session", User: "owner", Cookie: "second-cookie", CSRF: "second-csrf"}
	for _, test := range []struct {
		name              string
		values            []projectModelsWebSession
		matched, sessions int
	}{
		{"two-new-stable", []projectModelsWebSession{first, first, second, second}, 4, 2},
		{"reject-setup-cookie", []projectModelsWebSession{expected}, 0, 0},
		{"reject-other-owner", []projectModelsWebSession{{ID: first.ID, User: "other", Cookie: first.Cookie, CSRF: first.CSRF}}, 0, 0},
		{"reject-changed-csrf", []projectModelsWebSession{first, {ID: first.ID, User: first.User, Cookie: first.Cookie, CSRF: "changed"}}, 1, 1},
		{"reject-session-reuse", []projectModelsWebSession{first, {ID: first.ID, User: first.User, Cookie: second.Cookie, CSRF: first.CSRF}}, 1, 1},
		{"reject-third-context", []projectModelsWebSession{first, second, {ID: "third", User: first.User, Cookie: "third", CSRF: first.CSRF}}, 2, 2},
		{"reject-missing", []projectModelsWebSession{{}}, 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := projectModelsWebSessionDiagnostic{Expected: expected, LoginMode: true, Sessions: map[string]projectModelsWebSession{}}
			for _, value := range test.values {
				d.observe(value)
			}
			if d.Compared != len(test.values) || d.Matched != test.matched || d.NewSessions != test.sessions {
				t.Fatal("login identity boundary invalid")
			}
		})
	}
}

func TestProjectModelsWebSessionDiagnosticIdentity(t *testing.T) {
	expected := projectModelsWebSession{ID: "session-canary", User: "user-canary", Cookie: "cookie-canary", CSRF: "csrf-canary"}
	for _, test := range []struct {
		name    string
		value   projectModelsWebSession
		matches int
	}{
		{"same", expected, 1}, {"missing", projectModelsWebSession{}, 0},
		{"other-session", projectModelsWebSession{ID: "other", User: expected.User, Cookie: expected.Cookie, CSRF: expected.CSRF}, 0},
		{"other-user", projectModelsWebSession{ID: expected.ID, User: "other", Cookie: expected.Cookie, CSRF: expected.CSRF}, 0},
		{"other-cookie", projectModelsWebSession{ID: expected.ID, User: expected.User, Cookie: "other", CSRF: expected.CSRF}, 0},
		{"other-csrf", projectModelsWebSession{ID: expected.ID, User: expected.User, Cookie: expected.Cookie, CSRF: "other"}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := projectModelsWebSessionDiagnostic{Expected: expected}
			d.observe(test.value)
			if d.Compared != 1 || d.Matched != test.matches {
				t.Fatal("Session diagnostic accepted another identity")
			}
		})
	}
}

func TestProjectModelsWebPrivateInput(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "input.json")
	if err := os.WriteFile(path, []byte(`{"ok":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if raw, err := projectModelsWebReadPrivate(path, 64); err != nil || !bytes.Equal(raw, []byte(`{"ok":true}`)) {
		t.Fatal("regular private input rejected")
	}
	link := filepath.Join(directory, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(directory, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{link, fifo, directory} {
		if raw, err := projectModelsWebReadPrivate(entry, 64); err == nil || len(raw) != 0 {
			t.Fatal("non-regular private input admitted")
		}
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := projectModelsWebReadPrivate(path, 64); err == nil {
		t.Fatal("publicly readable private input admitted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := projectModelsWebReadPrivate(path, 2); err == nil {
		t.Fatal("oversized private input admitted")
	}
}

func TestProjectModelsWebPrivateAtomicReplacement(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "input.json")
	a, b := []byte(`{"phase":"first"}`), []byte(`{"phase":"other"}`)
	if err := os.WriteFile(path, a, 0600); err != nil {
		t.Fatal(err)
	}
	var worker sync.WaitGroup
	worker.Add(1)
	errors := make(chan error, 1)
	go func() {
		defer worker.Done()
		for n := 0; n < 300; n++ {
			value := a
			if n%2 == 0 {
				value = b
			}
			temp := filepath.Join(directory, "next")
			if err := os.WriteFile(temp, value, 0600); err != nil {
				errors <- err
				return
			}
			if err := os.Rename(temp, path); err != nil {
				errors <- err
				return
			}
		}
	}()
	for n := 0; n < 300; n++ {
		raw, err := projectModelsWebReadPrivate(path, 64)
		if err != nil || !bytes.Equal(raw, a) && !bytes.Equal(raw, b) {
			t.Error("atomic publication produced missing, partial, or rejected input")
			break
		}
	}
	worker.Wait()
	select {
	case err := <-errors:
		t.Fatal(err)
	default:
	}
}

func TestProjectModelsWebStrictJSON(t *testing.T) {
	for _, raw := range []string{`{"x":null}`, `{"x":"\ud83d\ude00"}`, `{"x":"\\ud800"}`} {
		if _, err := projectModelsWebObject([]byte(raw), "x"); err != nil {
			t.Fatal("legal closed JSON rejected")
		}
	}
	for _, raw := range []string{
		`{"x":1,"x":2}`, `{"x":{"nested":1,"nested":2}}`,
		`{"x":1} {}`, `{"x":1,"extra":2}`, `null`, `[]`,
		`{"x":"\ud800"}`, `{"x":"\udc00"}`, `{"x":"\ud800\u0041"}`,
		"{\"x\":\"\xff\"}",
	} {
		if _, err := projectModelsWebObject([]byte(raw), "x"); err == nil {
			t.Fatal("malformed or open JSON admitted")
		}
	}
}

func TestProjectModelsWebIPCEnvelope(t *testing.T) {
	input := strings.Repeat("a", 64)
	makeRaw := func(action string, args any, sequence int) []byte {
		raw, err := json.Marshal(map[string]any{"protocol": projectModelsWebProtocol, "input_hash": input, "sequence": sequence, "action": action, "args": args})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	if _, code := decodeProjectModelsWebIPC(makeRaw("counts", map[string]any{}, 1), input, 1); code != "" {
		t.Fatal("valid counts request rejected")
	}
	for _, test := range []struct {
		action   string
		args     any
		sequence int
		code     string
	}{
		{"counts", map[string]any{}, 2, "invalid_sequence"},
		{"unknown", map[string]any{}, 1, "invalid_action"},
		{"counts", nil, 1, "invalid_arguments"},
		{"counts", map[string]any{"sql": "forbidden"}, 1, "invalid_arguments"},
		{"release", map[string]any{"arm_id": "a0001"}, 1, "invalid_arguments"},
		{"snapshot", map[string]any{"project": nil}, 1, "invalid_arguments"},
		{"snapshot", map[string]any{"project": 3}, 1, "invalid_arguments"},
		{"snapshot", map[string]any{"project": "unregistered"}, 1, "invalid_arguments"},
		{"reference-fact", map[string]any{"project": "referenced", "state": "invalid"}, 1, "invalid_arguments"},
		{"archive-recovery-project", map[string]any{"project": "main", "expected_version": "1"}, 1, "invalid_arguments"},
		{"archive-recovery-project", map[string]any{"project": "config_recovery", "expected_version": "9223372036854775808"}, 1, "invalid_arguments"},
		{"release", map[string]any{"arm_id": "a0000", "request_token": "r000001"}, 1, "invalid_arguments"},
	} {
		if _, code := decodeProjectModelsWebIPC(makeRaw(test.action, test.args, test.sequence), input, 1); code != test.code {
			t.Fatalf("invalid request classification: got %s want %s", code, test.code)
		}
	}
}

func TestProjectModelsWebRegistryAdmission(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../docs/development/work-items/d27-project-owner-model-settings-ui-endpoints.json"))
	if err != nil {
		t.Fatal(err)
	}
	operations, err := projectModelsWebLoadOperations(raw)
	if err != nil {
		t.Fatal(err)
	}
	provider := "01900000-0000-7000-8000-000000000001"
	registry := projectModelsWebRegistry{
		Operations: operations,
		Projects:   map[string]string{"main": "01900000-0000-7000-8000-000000000002"},
		Targets:    map[string]map[string]map[string]bool{"main": {"provider": {provider: true}}},
		Cursors:    map[string]map[string]map[string]bool{"main": {"listProjectModelProviders": {"registered_cursor": true}}},
	}
	arm := func(operation string, target, query any, effect string) projectModelsWebIPC {
		raw, err := json.Marshal(map[string]any{"operation": operation, "project": "main", "target_id": target, "query": query, "effect": effect})
		if err != nil {
			t.Fatal(err)
		}
		return projectModelsWebIPC{Action: "arm", Args: raw}
	}
	for _, request := range []projectModelsWebIPC{
		arm("listProjectModelProviders", nil, "limit=25", "before_dispatch_hold"),
		arm("listProjectModelProviders", nil, "cursor=registered_cursor&limit=25", "after_complete_hold"),
		arm("deleteProjectModelProvider", provider, nil, "after_complete_disconnect"),
	} {
		if code := registry.admit(request); code != "" {
			t.Fatalf("registered endpoint rejected: %s", code)
		}
	}
	for _, request := range []projectModelsWebIPC{
		arm("listProjectModels", nil, "provider_id="+provider, "before_dispatch_hold"),
		arm("listProjectModels", nil, "limit=025", "before_dispatch_hold"),
		arm("listProjectModels", nil, "limit=25&limit=50", "before_dispatch_hold"),
		arm("listProjectModels", nil, "cursor=unregistered", "before_dispatch_hold"),
		arm("listProjectModelProviders", nil, "limit=25&cursor=registered_cursor", "before_dispatch_hold"),
		arm("getProjectModel", provider, nil, "after_complete_hold"),
		arm("deleteProjectModelProvider", provider, nil, "before_dispatch_hold"),
		arm("lookupProjectModelConfiguration", nil, nil, "after_complete_disconnect"),
		arm("getCurrentSession", nil, nil, "before_dispatch_hold"),
	} {
		if code := registry.admit(request); code == "" {
			t.Fatal("unregistered target, query, or effect admitted")
		}
	}
}

func TestProjectModelsWebResponseAdmission(t *testing.T) {
	project := "01900000-0000-7000-8000-000000000001"
	resource := "01900000-0000-7000-8000-000000000002"
	f := &projectModelsWebFixture{projectOwnerAuditWebFixture: &projectOwnerAuditWebFixture{projectOwnerWebFixture: &projectOwnerWebFixture{}}, registry: projectModelsWebRegistry{Projects: map[string]string{"main": project}, Targets: map[string]map[string]map[string]bool{}}}
	request := &projectModelsWebRequest{Project: "main", Operation: &projectModelsWebOperation{Operation: "createProjectModelProvider", Family: "mutation"}}
	response := &http.Response{StatusCode: 200, Header: http.Header{"Cache-Control": {"no-store"}, "Content-Type": {"application/json"}, "X-Request-Id": {"01900000-0000-7000-8000-000000000003"}}, Request: &http.Request{Method: "POST", URL: &url.URL{Path: "/api/v1/projects/" + project + "/model-providers"}}}
	valid := `{"kind":"provider.create","resource_id":"` + resource + `","version":"1","affected_references":"0"}`
	if err := f.admitResponse(request, response, []byte(valid)); err != nil {
		t.Fatal("formal safe receipt rejected")
	}
	if !f.registry.Targets["main"]["provider"][resource] {
		t.Fatal("safe create did not register its real target")
	}
	for _, raw := range []string{
		strings.Replace(valid, `"version":"1"`, `"version":"2"`, 1),
		strings.Replace(valid, `"affected_references":"0"`, `"affected_references":"1"`, 1),
		strings.Replace(valid, `"provider.create"`, `"model.create"`, 1),
		strings.TrimSuffix(valid, "}") + `,"value":"private-canary"}`,
		strings.Replace(valid, resource, "private-canary", 1),
	} {
		if err := f.admitResponse(request, response, []byte(raw)); err == nil {
			t.Fatal("invalid or sensitive receipt admitted")
		}
	}
	request.Operation = &projectModelsWebOperation{Operation: "createProjectModelCredential", Family: "mutation"}
	credential := `{"credential_id":"` + resource + `","purpose":"model","version":"1","deleted":false}`
	if err := f.admitResponse(request, response, []byte(credential)); err != nil {
		t.Fatal("formal safe credential result rejected")
	}
	for _, raw := range []string{
		strings.Replace(credential, `"purpose":"model"`, `"purpose":"smtp"`, 1),
		strings.Replace(credential, `"deleted":false`, `"deleted":true`, 1),
		strings.TrimSuffix(credential, "}") + `,"value":"private-canary"}`,
	} {
		if err := f.admitResponse(request, response, []byte(raw)); err == nil {
			t.Fatal("invalid or sensitive credential result admitted")
		}
	}
	for _, lookup := range []struct {
		name, operation, flag, member, body string
	}{
		{"configuration", "lookupProjectModelConfiguration", "found", "receipt", valid},
		{"credential", "lookupProjectModelCredential", "observed", "result", credential},
	} {
		t.Run(lookup.name+"-lookup", func(t *testing.T) {
			request.Operation = &projectModelsWebOperation{Operation: lookup.operation, Family: "lookup"}
			wrap := func(flag, body string) []byte {
				return []byte(`{"` + lookup.flag + `":` + flag + `,"` + lookup.member + `":` + body + `}`)
			}
			for _, raw := range [][]byte{wrap("true", lookup.body), wrap("false", "null")} {
				if err := f.admitResponse(request, response, raw); err != nil {
					t.Error("formal safe lookup union rejected")
				}
			}
			for _, raw := range [][]byte{
				wrap("true", "null"),
				wrap("false", lookup.body),
				wrap("true", strings.TrimSuffix(lookup.body, "}")+`,"value":"private-canary"}`),
				wrap("true", strings.Replace(lookup.body, `"version":"1"`, `"version":"0"`, 1)),
				wrap("true", `{}`),
				[]byte(`{"` + lookup.flag + `":false,"` + lookup.member + `":null,"value":"private-canary"}`),
			} {
				if err := f.admitResponse(request, response, raw); err == nil {
					t.Error("invalid or sensitive lookup union admitted")
				}
			}
		})
	}
}

func TestProjectModelsWebFormalProblemAdmission(t *testing.T) {
	f := &projectModelsWebFixture{}
	request := &projectModelsWebRequest{}
	for _, code := range []foundation.Code{foundation.InvalidState, foundation.ResourceBusy, foundation.Forbidden, foundation.NotFound, foundation.ProjectNotActive, foundation.VersionConflict} {
		t.Run(string(code), func(t *testing.T) {
			upstreamRequest := httptest.NewRequest(http.MethodDelete, "http://127.0.0.1/api/v1/projects/01a11f14-be67-74a9-bcd9-fd6489273813/model-providers/01a11f14-c2d7-7b0c-a7d2-94680890d9b5", nil)
			recorder := httptest.NewRecorder()
			(&account.HTTPBoundary{}).WriteProblem(recorder, upstreamRequest, foundation.NewFault(code, foundation.NotStarted))
			response := recorder.Result()
			defer response.Body.Close()
			response.Request = upstreamRequest
			if err := f.admitResponse(request, response, recorder.Body.Bytes()); err != nil {
				t.Fatal("formal standard Problem rejected", code)
			}
			leaked := httptest.NewRecorder()
			httpapi.WriteProblem(leaked, upstreamRequest, foundation.NewFault(code, foundation.NotStarted))
			leakedResponse := leaked.Result()
			defer leakedResponse.Body.Close()
			leakedResponse.Request = upstreamRequest
			if err := f.admitResponse(request, leakedResponse, leaked.Body.Bytes()); err == nil {
				t.Fatal("resource-path Problem bypassed fixed boundary projection", code)
			}
		})
	}
}

func TestProjectModelsWebNavigationAvailableAdmission(t *testing.T) {
	modelID, providerID := "01900000-0000-7000-8000-000000000001", "01900000-0000-7000-8000-000000000002"
	f := &projectModelsWebFixture{}
	request := &projectModelsWebRequest{Operation: &projectModelsWebOperation{Operation: "listProjectAvailableChatModels"}}
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		accept bool
	}{
		{"memory", func(map[string]any) {}, true},
		{"memory-replacement", func(v map[string]any) { v["name"] = projectModelsWebSystemDraftName("memory_replacement") }, true},
		{"legacy-owner-model", func(v map[string]any) { v["name"] = "Owner draft memory" }, false},
		{"legacy-owner-provider", func(v map[string]any) { v["provider_name"] = "Owner draft memory Provider" }, false},
		{"legacy-owner-pair", func(v map[string]any) {
			v["name"], v["provider_name"] = "Owner draft memory_replacement", "Owner draft memory Provider"
		}, false},
		{"unregistered-name", func(v map[string]any) { v["name"] = "private-canary" }, false},
		{"extra-value", func(v map[string]any) { v["value"] = "private-canary" }, false},
		{"missing-field", func(v map[string]any) { delete(v, "provider_name") }, false},
		{"invalid-model-id", func(v map[string]any) { v["id"] = "private-canary" }, false},
		{"invalid-provider-id", func(v map[string]any) { v["provider_id"] = "private-canary" }, false},
		{"invalid-version", func(v map[string]any) { v["version"] = "0" }, false},
		{"extra-system-scope", func(v map[string]any) { v["scope"].(map[string]any)["project_id"] = modelID }, false},
		{"extra-capability", func(v map[string]any) { v["capabilities"].(map[string]any)["value"] = "private-canary" }, false},
		{"unsafe-capability-value", func(v map[string]any) {
			v["capabilities"].(map[string]any)["reasoning_efforts"] = []string{"private-canary"}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := map[string]any{
				"id": modelID, "provider_id": providerID, "scope": map[string]any{"kind": "system"},
				"name": projectModelsWebSystemDraftName("memory"), "provider_name": projectModelsWebSystemDraftName("memory") + " Provider", "version": "1",
				"capabilities": map[string]any{"tool_calls": false, "parallel_tool_calls": false, "streaming": true, "reasoning": false, "input_modalities": []string{"text"}, "output_modalities": []string{"text"}, "reasoning_efforts": []string{}, "structured_output_modes": []string{"text"}, "context_length": nil, "max_output": nil},
			}
			tc.change(value)
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal("pure directory fixture encoding failed")
			}
			got, err := f.admitResource(request, raw)
			if (err == nil) != tc.accept || tc.accept && got != modelID {
				t.Fatal("navigation seed directory admission disagreed")
			}
		})
	}
}
