package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestProblemMappings(t *testing.T) {
	for code, status := range map[foundation.Code]int{
		foundation.InvalidArgument: 400, foundation.CursorInvalid: 400, foundation.CursorStale: 409,
		foundation.Unauthenticated: 401, foundation.SessionRevoked: 401, foundation.Forbidden: 403,
		foundation.CSRFFailed: 403, foundation.OriginDenied: 403, foundation.NotFound: 404,
		foundation.MethodNotAllowed: 405, foundation.ResourceDeleted: 410, foundation.VersionConflict: 409,
		foundation.IdempotencyKeyReused: 409, foundation.InvalidState: 409, foundation.AgentBusy: 409,
		foundation.ResourceBusy: 409, foundation.ProjectNotActive: 409, foundation.ConfirmationStale: 409,
		foundation.SchemaUnsupported: 422, foundation.CapabilityUnsupported: 422, foundation.RateLimited: 429,
		foundation.DependencyUnbound: 503, foundation.DependencyUnavailable: 503, foundation.CommitUnknown: 503,
		foundation.InternalError: 500, foundation.PayloadTooLarge: 413, foundation.UnsupportedMediaType: 415,
		foundation.ShuttingDown:         503,
		foundation.ObjectPayloadMissing: 503, foundation.ObjectIntegrityMismatch: 502,
		foundation.RangeNotSatisfiable: 416,
	} {
		t.Run(string(code), func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/v1/items?secret=query-SENTINEL", nil)
			r.URL.Fragment = "fragment-SENTINEL"
			w := httptest.NewRecorder()
			Handler(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				WriteProblem(w, r, foundation.NewFault(code, foundation.NotCommitted))
			})).ServeHTTP(w, r)
			var p Problem
			if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
				t.Fatal(err)
			}
			if w.Code != status || p.Status != status || p.Code != code || p.CommitState != foundation.NotCommitted {
				t.Fatalf("mapping: %+v", p)
			}
			if w.Header().Get("Content-Type") != "application/problem+json" || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing Problem headers")
			}
			if p.RequestID.Validate() != nil || p.RequestID.String() != w.Header().Get("X-Request-ID") {
				t.Fatal("inconsistent request ID")
			}
			if p.Instance != "/api/v1/items" || strings.Contains(w.Body.String(), "SENTINEL") {
				t.Fatal("unsafe instance")
			}
			if p.Type != "urn:agenteam:problem:"+strings.ReplaceAll(strings.ToLower(string(code)), "_", "-") {
				t.Fatal("unstable problem type")
			}
			if code == foundation.CommitUnknown && p.RetryHint != "lookup" {
				t.Fatal("unknown result must be looked up")
			}
		})
	}
}

func TestProblemProjectionAndCommitState(t *testing.T) {
	for _, state := range []foundation.CommitState{foundation.NotStarted, foundation.NotCommitted, foundation.Committed, foundation.Unknown} {
		fault := foundation.NewFault(foundation.VersionConflict, state).WithCause(errors.New("cause-SENTINEL"))
		fault.SafeMessage = "对象已被修改，请重新读取。"
		fault.FieldErrors = []foundation.FieldError{{Path: "/expected_version", Code: "STALE_VERSION"}, {Path: "invalid", Code: "BAD"}}
		fault.CauseID = "private-diagnostic-correlation"
		w := httptest.NewRecorder()
		Handler(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			WriteProblem(w, r, fmt.Errorf("wrapper-SENTINEL: %w", fault))
		})).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
		var p Problem
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		if p.CommitState != state || p.Detail != fault.SafeMessage || len(p.FieldErrors) != 1 || p.RetryHint != "reread" {
			t.Fatalf("fault projection: %+v", p)
		}
		if strings.Contains(w.Body.String(), "SENTINEL") || strings.Contains(w.Body.String(), fault.CauseID) {
			t.Fatal("private diagnostics leaked")
		}
	}
}

func TestUnknownErrorPanicAndEncodingFailureAreSafe(t *testing.T) {
	const secret = "credential-SENTINEL-never-format"
	for name, handler := range map[string]http.HandlerFunc{
		"unknown_error": func(w http.ResponseWriter, r *http.Request) { WriteProblem(w, r, errors.New(secret)) },
		"unknown_code": func(w http.ResponseWriter, r *http.Request) {
			WriteProblem(w, r, &foundation.Fault{Code: foundation.Code(secret), SafeMessage: secret, RetryHint: secret, CommitState: foundation.Unknown, FieldErrors: []foundation.FieldError{{Path: "/secret", Code: "SECRET"}}})
		},
		"panic":           func(w http.ResponseWriter, r *http.Request) { panic(secret) },
		"panic_error":     func(w http.ResponseWriter, r *http.Request) { panic(errors.New(secret)) },
		"panic_slice":     func(w http.ResponseWriter, r *http.Request) { panic([]string{secret}) },
		"panic_nil":       func(w http.ResponseWriter, r *http.Request) { panic(nil) },
		"marshal_error":   func(w http.ResponseWriter, r *http.Request) { _ = WriteJSON(w, r, 200, unsafeMarshaler{}) },
		"unsupported_dto": func(w http.ResponseWriter, r *http.Request) { _ = WriteJSON(w, r, 200, make(chan string)) },
	} {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/x?credential="+secret, strings.NewReader(secret))
			r.Header.Set("X-Request-ID", secret)
			r.Header.Set("Authorization", secret)
			r.Header.Set("Cookie", secret)
			r.Header.Set("X-CSRF-Token", secret)
			Handler(logger, handler).ServeHTTP(w, r)
			if w.Code != 500 || strings.Contains(w.Body.String(), secret) || strings.Contains(logs.String(), secret) {
				t.Fatalf("unsafe failure body/log: %d %s %s", w.Code, w.Body, logs.String())
			}
			var p Problem
			if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil || p.CommitState != foundation.Unknown || p.Code != foundation.InternalError {
				t.Fatalf("unknown outcome: %+v %v", p, err)
			}
			if p.RequestID.String() != w.Header().Get("X-Request-ID") || p.RequestID.Validate() != nil {
				t.Fatal("failure request ID")
			}
			if strings.Contains(logs.String(), "/x") || !strings.Contains(logs.String(), "unknown_route") {
				t.Fatal("raw URL logged")
			}
		})
	}
}

type unsafeMarshaler struct{}

func (unsafeMarshaler) MarshalJSON() ([]byte, error) {
	return nil, errors.New("credential-SENTINEL-never-format")
}

func TestRouterErrorAndHEADResponses(t *testing.T) {
	router := NewRouter()
	router.HandleFunc("GET /api/v1/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != "example" {
			t.Error("ServeMux path value lost")
		}
		_ = WriteJSON(w, r, 200, struct {
			OK bool `json:"ok"`
		}{true})
	})
	for _, tc := range []struct {
		method, path string
		status       int
		code         foundation.Code
		allow        string
	}{
		{"GET", "/api/v1/items/example", 200, "", ""}, {"HEAD", "/api/v1/items/example", 200, "", ""},
		{"POST", "/api/v1/items/example", 405, foundation.MethodNotAllowed, "GET, HEAD"},
		{"GET", "/api/v1/session", 404, foundation.NotFound, ""}, {"GET", "/some/page", 404, foundation.NotFound, ""},
		{"GET", "/assets/missing.js", 404, foundation.NotFound, ""}, {"HEAD", "/unknown", 404, foundation.NotFound, ""},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(tc.method, tc.path, nil)
			r.Header.Set("X-Request-ID", "01900000-0000-7000-8000-000000000001")
			Handler(nil, router).ServeHTTP(w, r)
			if w.Code != tc.status || w.Header().Get("Allow") != tc.allow {
				t.Fatalf("route: %d allow=%s", w.Code, w.Header().Get("Allow"))
			}
			if _, err := foundation.ParseID[foundation.Request](w.Header().Get("X-Request-ID")); err != nil || w.Header().Get("X-Request-ID") == r.Header.Get("X-Request-ID") {
				t.Fatal("untrusted request ID reused")
			}
			if tc.method == http.MethodHead {
				if w.Body.Len() != 0 {
					t.Fatal("HEAD response has body")
				}
				return
			}
			if tc.code != "" {
				var p Problem
				if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil || p.Code != tc.code || p.CommitState != foundation.NotStarted {
					t.Fatalf("routing Problem: %+v %v", p, err)
				}
			}
		})
	}
}

func TestConcurrentRequestIDAndAccessLog(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	router := NewRouter()
	router.HandleFunc("GET /requests/{ignored}", func(w http.ResponseWriter, r *http.Request) {
		id := RequestID(r.Context())
		_ = WriteJSON(w, r, 200, struct {
			ID foundation.ID[foundation.Request] `json:"id"`
		}{id})
	})
	server := httptest.NewServer(Handler(logger, router))
	defer server.Close()
	const count = 32
	ids := make(chan string, count)
	var wg sync.WaitGroup
	for range count {
		wg.Go(func() {
			r, err := http.NewRequest(http.MethodGet, server.URL+"/requests/path-SENTINEL?secret=query-SENTINEL", nil)
			if err != nil {
				t.Error(err)
				return
			}
			r.Header.Set("X-Request-ID", "header-SENTINEL")
			response, err := server.Client().Do(r)
			if err != nil {
				t.Error(err)
				return
			}
			defer response.Body.Close()
			var body struct {
				ID string `json:"id"`
			}
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			if body.ID != response.Header.Get("X-Request-ID") {
				t.Error("context/header mismatch")
				return
			}
			ids <- body.ID
		})
	}
	wg.Wait()
	close(ids)
	seen := make(map[string]bool)
	for id := range ids {
		if seen[id] {
			t.Fatal("request contexts shared an ID")
		}
		seen[id] = true
	}
	if len(seen) != count {
		t.Fatal("missing completed requests")
	}
	if strings.Contains(logs.String(), "SENTINEL") {
		t.Fatal("raw request values logged")
	}
	decoder := json.NewDecoder(bytes.NewReader(logs.Bytes()))
	logged := make(map[string]bool)
	for {
		var record map[string]any
		if err := decoder.Decode(&record); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		id := record["request_id"].(string)
		if !seen[id] || logged[id] || record["route"] != "GET /requests/{ignored}" || record["status"] != float64(200) || record["bytes"] != float64(45) {
			t.Fatalf("incorrect access record: %v", record)
		}
		logged[id] = true
	}
	if len(logged) != count {
		t.Fatal("access logs missing")
	}
}
