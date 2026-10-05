package account

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestHTTPBoundaryUsesOriginalBrowserRules(t *testing.T) {
	for _, origin := range []string{"https://example.test", "http://localhost:8080"} {
		b, err := NewHTTPBoundary(httpConstructionCore(t), origin)
		if err != nil {
			t.Fatal(err)
		}
		for _, change := range []func(*http.Request){
			func(*http.Request) {},
			func(r *http.Request) { r.Host = "other.test" },
			func(r *http.Request) { r.Header.Set("Origin", "https://other.test") },
			func(r *http.Request) { r.Header.Add("Origin", origin) },
			func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
			func(r *http.Request) { r.Header.Del("Origin"); r.Method = "POST" },
		} {
			r := httptest.NewRequest("GET", origin+"/api/v1/system/models", nil)
			r.Header.Set("Origin", origin)
			change(r)
			w := httptest.NewRecorder()
			got, want := b.CheckRequest(w, r), b.csrf.csrfCheck(r)
			if (got == nil) != (want == nil) {
				t.Fatalf("wrapper changed original rule: %v / %v", got, want)
			}
			httpAssertSecurityHeaders(t, w)
		}
		for _, raw := range []string{"/api/v1/../secret", "/api//v1", "/api/v1\\private"} {
			r := httptest.NewRequest("GET", origin+"/api/v1", nil)
			r.URL.Path = raw
			if b.CheckRequest(httptest.NewRecorder(), r) == nil {
				t.Fatal("ambiguous path accepted")
			}
			if _, err = b.RequireSystem(r, id.Read); err == nil {
				t.Fatal("RequireSystem bypassed path check")
			}
		}
		r := httptest.NewRequest("GET", origin+"/api/v1", nil)
		r.URL.RawPath = "/api/%76%31"
		if b.CheckRequest(httptest.NewRecorder(), r) == nil {
			t.Fatal("RawPath accepted")
		}
	}
}

func TestHTTPBoundaryZeroAndSafeProblem(t *testing.T) {
	for _, core := range []*Service{nil, {}, {data: func() *serviceState { return nil }}} {
		if b, err := NewHTTPBoundary(core, "https://example.test"); err == nil || b != nil {
			t.Fatal("zero core accepted")
		}
	}
	b, err := NewHTTPBoundary(httpConstructionCore(t), "https://example.test")
	if err != nil {
		t.Fatal(err)
	}
	for _, boundary := range []*HTTPBoundary{nil, {}, b} {
		for _, r := range []*http.Request{nil, {}} {
			if boundary.CheckRequest(httptest.NewRecorder(), r) == nil {
				t.Fatal("nil request accepted")
			}
			if _, err = boundary.RequireSystem(r, id.Read); err == nil {
				t.Fatal("nil request authenticated")
			}
			w := httptest.NewRecorder()
			boundary.WriteProblem(w, r, f.NewFault(f.Forbidden, f.NotStarted))
			var problem httpapi.Problem
			if json.Unmarshal(w.Body.Bytes(), &problem) != nil || problem.Instance != "/api/v1" || w.Code != 403 {
				t.Fatal("unsafe nil Problem")
			}
		}
	}
	for _, code := range []f.Code{f.Unauthenticated, f.SessionRevoked, f.Forbidden, f.CommitUnknown} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "https://example.test/private-material?value=secret", nil)
		b.WriteProblem(w, r, f.NewFault(code, f.NotStarted))
		cleared := len(w.Result().Cookies()) > 0
		if cleared != (code == f.Unauthenticated || code == f.SessionRevoked) {
			t.Fatal("wrong cookie clearing")
		}
		var p httpapi.Problem
		_ = json.Unmarshal(w.Body.Bytes(), &p)
		if p.Instance != "/api/v1" {
			t.Fatal("path reflected")
		}
	}
}
