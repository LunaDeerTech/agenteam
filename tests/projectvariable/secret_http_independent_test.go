//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	variablehttp "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/http"
)

// This observer is installed beneath the one shared hookStore BEFORE any
// Authority or Service is constructed. It never supplies authorization or a
// CommitResult. The existing fixture's Skills initialization is controlled;
// these checks do not constitute default-root or lifecycle acceptance.
type independentSecretStore struct {
	fixtureStore
	mu   sync.Mutex
	gate *independentSecretGate
}

type independentSecretGate struct {
	owner                                         string
	entered                                       chan struct{}
	release                                       chan struct{}
	once                                          sync.Once
	mu                                            sync.Mutex
	tx                                            f.Tx
	pid                                           int32
	deadline                                      time.Time
	hits, callbacks, locks                        int
	released, earlyLock, callbackLive, callerLive bool
	callbackCode                                  f.Code
	callbackState                                 f.CommitState
	result                                        f.CommitResult
}

func (g *independentSecretGate) unhold() {
	g.once.Do(func() {
		g.mu.Lock()
		g.released = true
		g.mu.Unlock()
		close(g.release)
	})
}

func (s *independentSecretStore) WithinTx(caller context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	g := s.gate
	s.mu.Unlock()
	if g == nil || cause.Kind() != f.RecoveryCause || cause.Details().Owner != g.owner {
		return s.fixtureStore.WithinTx(caller, cause, fn)
	}
	g.mu.Lock()
	g.hits++
	g.mu.Unlock()
	result := s.fixtureStore.WithinTx(caller, cause, func(ctx context.Context, tx f.Tx) error {
		x, err := s.fixtureStore.InTx(tx)
		if err != nil {
			return err
		}
		var pid int32
		if err = x.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
			return err
		}
		deadline, _ := ctx.Deadline()
		g.mu.Lock()
		g.tx, g.pid, g.deadline = tx, pid, deadline
		g.mu.Unlock()
		select {
		case g.entered <- struct{}{}:
		default:
		}
		select {
		case <-g.release:
		case <-ctx.Done():
			return ctx.Err()
		}
		live := ctx.Err() == nil
		err = fn(ctx, tx) // original callback, original context and original Tx
		var fault *f.Fault
		g.mu.Lock()
		g.callbacks++
		g.callbackLive = live && ctx.Err() == nil
		if errors.As(err, &fault) {
			g.callbackCode, g.callbackState = fault.Code, fault.CommitState
		}
		g.mu.Unlock()
		return err
	})
	// Normal postgres operation retirement cancels callback ctx. The caller is
	// still live here, before SecretService's deferred done cancels its call.
	g.mu.Lock()
	g.result, g.callerLive = result, caller.Err() == nil
	g.mu.Unlock()
	return result
}

func (s *independentSecretStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.mu.Lock()
	g := s.gate
	s.mu.Unlock()
	if g != nil {
		g.mu.Lock()
		if tx == g.tx {
			g.locks++
			g.earlyLock = g.earlyLock || !g.released
		}
		g.mu.Unlock()
	}
	return s.fixtureStore.AcquireAll(ctx, tx, locks)
}

func independentSecretFixture(t *testing.T) (*secretOwnerFixture, *independentSecretStore) {
	t.Helper()
	db := newDatabase(t)
	raw := openStore(t, db.Config(t, nil))
	observer := &independentSecretStore{fixtureStore: raw}
	base := assembleVariableHTTPFixture(t, db, raw, &hookStore{fixtureStore: observer})
	v := assembleSecretOwnerFixture(t, base)
	h, err := variablehttp.NewSecretHTTPHandler(v.owner, v.boundary)
	if err != nil {
		t.Fatal("independent real HTTP construction failed")
	}
	v.handler = httpapi.Handler(slog.New(slog.NewJSONHandler(v.logs, nil)), h)
	return v, observer
}

func independentSecretProblem(t *testing.T, r variableHTTPResponse, status int, code f.Code, state f.CommitState) httpapi.Problem {
	t.Helper()
	var p httpapi.Problem
	decoder := json.NewDecoder(bytes.NewReader(r.body))
	decoder.DisallowUnknownFields()
	if r.aborted || r.status != status || decoder.Decode(&p) != nil || p.Code != code || p.Status != status || p.CommitState != state || p.RequestID.Validate() != nil {
		t.Fatal("independent safe Problem code/state/shape mismatch")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(r.body, &fields) != nil || len(fields) != 8+independentSecretBoolInt(len(p.FieldErrors) != 0) || p.RetryHint != "" || p.Instance != "/api/v1" {
		t.Fatal("independent Problem exposed unexpected fields")
	}
	title, detail, kind := "Session revoked", "Sign in again.", "session-revoked"
	if code == f.InvalidArgument {
		title, detail, kind = "Invalid argument", "The request is invalid.", "invalid-argument"
	}
	if p.Title != title || p.Detail != detail || p.Type != "urn:agenteam:problem:"+kind {
		t.Fatal("independent Problem contains noncanonical diagnostic text")
	}
	if r.header.Get("X-Request-ID") != p.RequestID.String() || r.header.Get("Cache-Control") != "no-store" || !strings.HasPrefix(r.header.Get("Content-Type"), "application/problem+json") {
		t.Fatal("independent safe Problem header mismatch")
	}
	return p
}

func independentSecretBoolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func independentSecretForms(value string) []string {
	encoded, _ := json.Marshal(value)
	ascii := strconv.QuoteToASCII(value)
	digest := sha256.Sum256([]byte(value))
	return []string{value, string(encoded[1 : len(encoded)-1]), ascii[1 : len(ascii)-1],
		base64.StdEncoding.EncodeToString([]byte(value)), base64.RawStdEncoding.EncodeToString([]byte(value)),
		base64.URLEncoding.EncodeToString([]byte(value)), base64.RawURLEncoding.EncodeToString([]byte(value)),
		hex.EncodeToString(digest[:]), base64.StdEncoding.EncodeToString(digest[:])}
}

func independentSecretContains(raw []byte, forms []string) bool {
	for _, form := range forms {
		if bytes.Contains(raw, []byte(form)) {
			return true
		}
	}
	return false
}

func independentSecretLog(t *testing.T, raw string, p httpapi.Problem, route string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	var entry map[string]json.RawMessage
	if len(lines) != 1 || json.Unmarshal([]byte(lines[0]), &entry) != nil {
		t.Fatal("independent original log missing or duplicated")
	}
	for key, value := range map[string]string{"event": "http_request", "request_id": p.RequestID.String(), "route": route, "code": string(p.Code)} {
		var actual string
		if json.Unmarshal(entry[key], &actual) != nil || actual != value {
			t.Fatal("independent original log identity mismatch")
		}
	}
	var status int
	if json.Unmarshal(entry["status"], &status) != nil || status != p.Status {
		t.Fatal("independent original log status mismatch")
	}
}

func TestIndependentSecretHTTPCurrentSessionAndSafeErrors(t *testing.T) {
	v, observer := independentSecretFixture(t)
	path := variableHTTPPath(v.project.ID, "/secret-variables")
	target := id[i.ProjectVariable](t)
	created := secretHTTPMutation(t, v.request(t, v.ownerBrowser, "POST", path, secretHTTPCreateBody(t, target, "INDEPENDENT_SEED_VALUE"), "independent-seed"))
	if created.Fields().Variable.Validate() != nil {
		t.Fatal("independent seed metadata absent")
	}

	t.Run("current-session-after-domain-begin", func(t *testing.T) {
		for _, mode := range []string{"get", "list"} {
			browser := v.login(t, v.ownerBrowser.email)
			requestPath := path
			if mode == "get" {
				requestPath += "/" + target.String()
			}
			positive := v.request(t, browser, "GET", requestPath, "", "")
			requireHTTP(t, positive)
			var one vc.SecretVariable
			if mode == "get" {
				if json.Unmarshal(positive.body, &one) != nil || one.Validate() != nil || one.Fields().ID != target {
					t.Fatal("independent GET positive control failed")
				}
			} else {
				var page f.Page[vc.SecretVariable]
				if json.Unmarshal(positive.body, &page) != nil || len(page.Items) != 1 || page.Items[0].Validate() != nil || page.Items[0].Fields().ID != target {
					t.Fatal("independent List positive control failed")
				}
			}
			before := v.secretCounts(t)
			gate := &independentSecretGate{owner: "projectvariable.secret_" + mode, entered: make(chan struct{}, 1), release: make(chan struct{})}
			observer.mu.Lock()
			observer.gate = gate
			observer.mu.Unlock()
			ctx, cancel := context.WithCancel(ctxFor(t))
			finished := make(chan struct{})
			var response variableHTTPResponse
			t.Cleanup(func() {
				gate.unhold()
				cancel()
				select {
				case <-finished:
				case <-time.After(3 * time.Second):
					t.Error("independent HTTP call did not actually join")
				}
				observer.mu.Lock()
				if observer.gate == gate {
					observer.gate = nil
				}
				observer.mu.Unlock()
			})
			go func() {
				defer close(finished)
				response = v.serve(variableHTTPRequest(ctx, browser, "GET", requestPath, "", ""))
			}()
			select {
			case <-gate.entered:
			case <-finished:
				t.Fatal("independent original BEGIN barrier was not reached")
			case <-time.After(2 * time.Second):
				t.Fatal("independent BEGIN barrier timed out")
			}
			gate.mu.Lock()
			valid := gate.pid > 0 && gate.tx.Valid() && gate.hits == 1 && gate.locks == 0 && !gate.deadline.IsZero() && time.Until(gate.deadline) > 0 && time.Until(gate.deadline) <= 2*time.Second
			gate.mu.Unlock()
			if !valid {
				t.Fatal("independent original live Tx/deadline not established before Logout")
			}
			if err := v.core.Logout(ctxFor(t), account.LogoutRequest{Actor: browser.actor, Key: f.IdempotencyKey(id[struct{}](t).String())}); err != nil {
				t.Fatal("independent formal Logout failed")
			}
			gate.unhold()
			select {
			case <-finished:
			case <-time.After(3 * time.Second):
				t.Fatal("independent original HTTP call did not return")
			}
			cancel()
			observer.mu.Lock()
			observer.gate = nil
			observer.mu.Unlock()
			gate.mu.Lock()
			physical := gate.result.Fault()
			valid = gate.hits == 1 && gate.callbacks == 1 && gate.locks == 1 && !gate.earlyLock && gate.callbackLive && gate.callerLive && gate.callbackCode == f.SessionRevoked && gate.callbackState == f.NotStarted && gate.result.State() == f.NotCommitted && physical != nil && physical.Code == f.SessionRevoked && physical.CommitState == f.NotCommitted
			gate.mu.Unlock()
			if !valid {
				t.Fatal("independent original Session rejection or transaction provenance mismatch")
			}
			p := independentSecretProblem(t, response, 401, f.SessionRevoked, f.NotCommitted)
			if len(p.FieldErrors) != 0 {
				t.Fatal("independent Session Problem contains fields")
			}
			cleared := false
			for _, cookie := range (&http.Response{Header: response.header}).Cookies() {
				if cookie.Name == "__Host-agenteam_session" && cookie.Value == "" && cookie.MaxAge < 0 {
					cleared = true
				}
			}
			if !cleared || v.secretCounts(t) != before {
				t.Fatal("independent revoked read retained cookie or changed Secret facts")
			}
			t.Log("independent original Session rejection joined", mode)
		}
	})

	t.Run("hostile-member-errors-are-safe", func(t *testing.T) {
		const nameCanary = "INDEPENDENT_MEMBER_3c8\"\\\n中"
		const valueCanary = "INDEPENDENT_VALUE_82d\"\\\n中"
		const invalidCanary = "INDEPENDENT_INVALID_VALUE_1a9"
		forms := append(independentSecretForms(nameCanary), independentSecretForms(valueCanary)...)
		forms = append(forms, independentSecretForms(invalidCanary)...)
		if independentSecretContains([]byte("safe HTTP metadata"), forms) {
			t.Fatal("independent leak detector negative control failed")
		}
		for _, form := range forms {
			if !independentSecretContains([]byte("prefix"+form+"suffix"), forms) {
				t.Fatal("independent leak detector positive control failed")
			}
		}
		for _, mode := range []string{"top", "nested", "declared-value"} {
			newTarget := id[i.ProjectVariable](t)
			request := map[string]any{"variable_id": newTarget.String(), "name": "INDEPENDENT_REJECTED", "description": "safe", "value": valueCanary}
			body := map[string]any{"request": request}
			switch mode {
			case "top":
				body[nameCanary] = valueCanary
			case "nested":
				request[nameCanary] = valueCanary
			case "declared-value":
				request["value"] = invalidCanary + "\x00"
			}
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal("independent synthetic body construction failed")
			}
			before, logStart := v.secretCounts(t), len(v.logs.text())
			response := v.request(t, v.ownerBrowser, "POST", path, string(raw), f.IdempotencyKey("independent-reject-"+mode))
			clear(raw)
			p := independentSecretProblem(t, response, 400, f.InvalidArgument, f.NotStarted)
			if mode == "declared-value" {
				if len(p.FieldErrors) != 1 || p.FieldErrors[0].Path != "/request/value" || p.FieldErrors[0].Code != "INVALID_SECRET_VALUE" {
					t.Fatal("independent declared field diagnostic mismatch")
				}
			} else if len(p.FieldErrors) != 0 {
				t.Fatal("independent unknown member became a diagnostic path")
			}
			logs := v.logs.text()[logStart:]
			independentSecretLog(t, logs, p, "/api/v1/projects/{project_id}/secret-variables")
			headers, err := json.Marshal(response.header)
			if err != nil {
				t.Fatal("independent header observation failed")
			}
			for _, observed := range [][]byte{response.body, headers, []byte(logs)} {
				if independentSecretContains(observed, forms) {
					t.Fatal("independent response or original log leaked synthetic material")
				}
			}
			for name, values := range response.header {
				for _, observed := range append([]string{name}, values...) {
					if independentSecretContains([]byte(observed), forms) {
						t.Fatal("independent raw response header leaked synthetic material")
					}
				}
			}
			var rows int
			err = v.raw.QueryRow(ctxFor(t), "SELECT count(*) FROM agenteam_projectvariable.variables WHERE project_id=$1 AND id=$2", v.project.ID.String(), newTarget.String()).Scan(&rows)
			if err != nil || rows != 0 || v.secretCounts(t) != before {
				t.Fatal("independent rejected body changed Secret facts")
			}
		}
	})
}
