package account

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type httpBodySpy struct {
	io.Reader
	reads, closes int
}

func (s *httpBodySpy) Read(b []byte) (int, error) {
	s.reads++
	if s.Reader == nil {
		return 0, io.EOF
	}
	return s.Reader.Read(b)
}
func (s *httpBodySpy) Close() error { s.closes++; return nil }

func TestB04CSRFOriginHostBeforeRoutingAndBody(t *testing.T) {
	b, e := csrfNewBoundary("https://example.test")
	if e != nil {
		t.Fatal(e)
	}
	h := (&accountHTTP{csrf: b}).httpHandler()
	for _, tc := range []struct {
		name, host, origin, fetch, path string
		duplicate                       bool
	}{
		{"missing", "example.test", "", "", "/api/v1/me", false},
		{"null", "example.test", "null", "", "/api/v1/me", false},
		{"suffix", "example.test", "https://example.test.attacker.invalid", "", "/api/v1/me", false},
		{"multi", "example.test", "https://example.test", "", "/api/v1/me", true},
		{"comma", "example.test", "https://example.test, https://example.test", "", "/api/v1/me", false},
		{"cross-site", "example.test", "https://example.test", "cross-site", "/api/v1/me", false},
		{"wrong-host", "attacker.test", "https://example.test", "", "/api/v1//me", false},
		{"redirect-origin", "example.test", "https://attacker.test", "", "/api/v1/auth/../session", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &httpBodySpy{Reader: strings.NewReader(`{"secret":"not-to-be-read"}`)}
			r := httptest.NewRequest(http.MethodPost, "https://example.test"+tc.path, body)
			r.Host = tc.host
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.duplicate {
				r.Header.Add("Origin", tc.origin)
			}
			if tc.fetch != "" {
				r.Header.Set("Sec-Fetch-Site", tc.fetch)
			}
			r.Header.Set("X-Forwarded-Host", "example.test")
			r.Header.Set("X-Forwarded-Proto", "https")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 403 || body.reads != 0 || w.Header().Get("Location") != "" || len(w.Result().Cookies()) != 0 {
				t.Fatalf("status=%d reads=%d headers=%v", w.Code, body.reads, w.Header())
			}
			httpAssertSecurityHeaders(t, w)
		})
	}
}
func TestB04CSRFCanonicalOriginsAndLocalCookieNames(t *testing.T) {
	for _, raw := range []string{"http://example.test", "https://user@example.test", "https://example.test/path", "https://example.test?secret=x", "https://example.test#token", "https://127.1", "http://localhost.attacker.test", "http://localhost.", "http://192.0.2.1"} {
		if _, e := csrfNewBoundary(raw); e == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	for _, tc := range []struct {
		raw, origin, session, browser string
		secure                        bool
	}{
		{"https://EXAMPLE.test:443/", "https://example.test", "__Host-agenteam_session", "__Host-agenteam_browser", true},
		{"https://EXAMPLE.test.:443/", "https://example.test.", "__Host-agenteam_session", "__Host-agenteam_browser", true},
		{"http://localhost:8080/", "http://localhost:8080", "agenteam_local_session", "agenteam_local_browser", false},
		{"http://127.0.0.2", "http://127.0.0.2", "agenteam_local_session", "agenteam_local_browser", false},
		{"http://[::1]:80", "http://[::1]", "agenteam_local_session", "agenteam_local_browser", false},
	} {
		b, e := csrfNewBoundary(tc.raw)
		if e != nil || b.origin != tc.origin || b.sessionName != tc.session || b.browserName != tc.browser || b.secure != tc.secure {
			t.Fatalf("origin mismatch %s: %+v %v", tc.raw, b, e)
		}
		for _, name := range []string{b.sessionName, b.browserName} {
			cookie, e := b.csrfMakeCookie(name, "safe-token", time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC))
			if e != nil || cookie.Domain != "" || cookie.Path != "/" || !cookie.HttpOnly || cookie.Secure != tc.secure || cookie.SameSite != http.SameSiteLaxMode {
				t.Fatal("cookie flags", e)
			}
		}
		w := httptest.NewRecorder()
		b.csrfClearSession(w)
		cookies := w.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Name != tc.session || cookies[0].MaxAge != -1 || cookies[0].Secure != tc.secure || !cookies[0].HttpOnly {
			t.Fatal("clear cookie")
		}
	}
}
func TestB04CSRFCookieContextIsolationAndMultiplicity(t *testing.T) {
	b, _ := csrfNewBoundary("https://example.test")
	r := httptest.NewRequest("POST", "https://example.test/api/v1/me", nil)
	r.AddCookie(&http.Cookie{Name: "agenteam_local_session", Value: "local-session"})
	r.AddCookie(&http.Cookie{Name: b.browserName, Value: "anonymous-cookie"})
	if _, e := csrfCookie(r, b.sessionName); !hasFaultCode(e, foundation.Unauthenticated) {
		t.Fatal("local/browser cookie accepted as session")
	}
	r.AddCookie(&http.Cookie{Name: b.sessionName, Value: "session-cookie"})
	s, e := csrfCookie(r, b.sessionName)
	if e != nil {
		t.Fatal(e)
	}
	e = s.Use(func(raw []byte) error {
		if string(raw) != "session-cookie" {
			t.Fatal("cross context")
		}
		return nil
	})
	s.Destroy()
	if e != nil {
		t.Fatal(e)
	}
	r.AddCookie(&http.Cookie{Name: b.sessionName, Value: "second"})
	if _, e = csrfCookie(r, b.sessionName); e == nil {
		t.Fatal("duplicate cookie accepted")
	}
	r.Header.Set("X-CSRF-Token", "one")
	r.Header.Add("X-CSRF-Token", "two")
	if _, e = csrfToken(r); !hasFaultCode(e, foundation.CSRFFailed) {
		t.Fatal("duplicate csrf accepted")
	}
}
func TestB04CSRFNoIdentityBypassOnProtectedRoutes(t *testing.T) {
	b, _ := csrfNewBoundary("https://example.test")
	owner := &accountHTTP{csrf: b}
	handler := owner.httpHandler()
	for _, route := range owner.httpRoutes() {
		if route.authority == "public" {
			continue
		}
		url := "https://example.test/api/v1" + strings.ReplaceAll(route.path, "{id}", "not-an-id")
		body := &httpBodySpy{Reader: strings.NewReader(`{"version":"bad"}`)}
		r := httptest.NewRequest(route.method, url, body)
		r.Header.Set("Origin", b.origin)
		r.Header.Set("X-CSRF-Token", "forged")
		r.Header.Set("Idempotency-Key", "invalid, key")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		want := 401
		if route.authority == "browser" {
			want = 403
		}
		if w.Code != want || body.reads != 0 {
			t.Fatalf("%s %s: status=%d reads=%d", route.method, route.path, w.Code, body.reads)
		}
		httpAssertSecurityHeaders(t, w)
		if route.authority == "browser" && len(w.Result().Cookies()) != 0 {
			t.Fatal("anonymous failure changed session")
		}
	}
}
func TestB04CSRFRemoteAddressIgnoresForwarded(t *testing.T) {
	r := httptest.NewRequest("POST", "https://example.test", nil)
	r.RemoteAddr = "[2001:db8::2]:40000"
	r.Header.Set("X-Forwarded-For", "127.0.0.1")
	r.Header.Set("Forwarded", "for=127.0.0.1")
	ip, e := csrfClientIP(r)
	if e != nil || ip.String() != "2001:db8::2" {
		t.Fatal("forwarded trusted", e)
	}
	r.RemoteAddr = "garbage"
	if _, e = csrfClientIP(r); e == nil {
		t.Fatal("invalid client IP accepted")
	}
}
