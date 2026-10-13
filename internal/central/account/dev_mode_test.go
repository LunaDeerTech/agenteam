package account

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestDevModeHTTPConstructorsAndCookies(t *testing.T) {
	for _, mode := range []bool{false, true} {
		for _, origin := range []string{"http://localhost:8080", "https://example.test", "http://192.0.2.40:8080"} {
			core := httpConstructionCore(t)
			core.state().deps.DevMode = mode
			ring, _, _ := testRings(t)
			facade, err := NewSystemHTTPFacade(core, ring)
			if err != nil {
				t.Fatal(err)
			}
			handler, handlerErr := NewHTTPHandler(core, &httpProfileSpy{t: t}, HTTPOptions{PublicOrigin: origin, System: facade})
			boundary, boundaryErr := NewHTTPBoundary(core, origin)
			if !mode && origin == "http://192.0.2.40:8080" {
				if handlerErr == nil || boundaryErr == nil {
					t.Fatal("production accepted remote HTTP")
				}
				continue
			}
			if handlerErr != nil || boundaryErr != nil {
				t.Fatal(handlerErr, boundaryErr)
			}
			for _, host := range []string{"192.168.1.42:8080", "public.example.test:9090", "[2001:db8::1]:8080"} {
				r := httptest.NewRequest("GET", "http://"+host+"/api/v1/session", nil)
				r.Header.Set("Origin", "http://unrelated.example.test")
				r.Header.Set("Sec-Fetch-Site", "cross-site")
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				want := http.StatusForbidden
				if mode {
					want = http.StatusUnauthorized
				}
				if w.Code != want || (boundary.CheckRequest(httptest.NewRecorder(), r) == nil) != mode {
					t.Fatalf("mode=%v origin=%s host=%s status=%d", mode, origin, host, w.Code)
				}
			}
			wantSecure := !mode && origin == "https://example.test"
			for _, name := range []string{boundary.csrf.sessionName, boundary.csrf.browserName} {
				cookie, err := boundary.csrf.csrfMakeCookie(name, "safe-token", time.Now().Add(time.Hour))
				if err != nil || cookie.Secure != wantSecure || cookie.Domain != "" || cookie.Path != "/" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
					t.Fatal("cookie policy changed", err)
				}
			}
			if mode && (boundary.csrf.sessionName != "agenteam_local_session" || boundary.csrf.browserName != "agenteam_local_browser") {
				t.Fatal("development cookie uses a secure-only name")
			}
		}
	}
	core := httpConstructionCore(t)
	core.state().deps.DevMode = true
	for _, origin := range []string{"ftp://example.test", "http://*.example.test", "http://user@example.test", "http://example.test/path", "http://example.test?", "http://example.test#"} {
		if _, err := NewHTTPBoundary(core, origin); err == nil {
			t.Fatal("invalid configured origin accepted", origin)
		}
	}
}

func TestDevModeRetainsSessionCSRFAndAuthentication(t *testing.T) {
	for _, mode := range []string{"valid", "no_origin", "missing_csrf", "forged_csrf", "missing_cookie", "revoked"} {
		t.Run(mode, func(t *testing.T) {
			original, store, cookie, token := humanBoundaryFixture(t, "user")
			core := original.core
			core.state().deps.DevMode = true
			boundary, err := NewHTTPBoundary(core, "http://localhost:8080")
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "http://public.example.test:9090/api/v1/projects/resolve", nil)
			r.Header.Set("Origin", "http://unrelated.example.test")
			r.Header.Set("Sec-Fetch-Site", "cross-site")
			r.Header.Set("X-CSRF-Token", token)
			r.AddCookie(&http.Cookie{Name: "agenteam_local_session", Value: cookie})
			var want foundation.Code
			switch mode {
			case "no_origin":
				r.Header.Del("Origin")
			case "missing_csrf":
				r.Header.Del("X-CSRF-Token")
				want = foundation.CSRFFailed
			case "forged_csrf":
				r.Header.Set("X-CSRF-Token", "x"+token)
				want = foundation.CSRFFailed
			case "missing_cookie":
				r.Header.Del("Cookie")
				want = foundation.Unauthenticated
			case "revoked":
				store.revoked = true
				want = foundation.SessionRevoked
			}
			actor, err := boundary.RequireHuman(r)
			if want == "" {
				if err != nil || !actor.Equal(store.actor) {
					t.Fatal("valid development session rejected", err)
				}
			} else if !hasFaultCode(err, want) || actor.Validate() == nil {
				t.Fatal("development bypassed identity/CSRF", err)
			}
		})
	}
}

func TestDevModeLoginRequiresBrowserCSRF(t *testing.T) {
	core := httpConstructionCore(t)
	core.state().deps.DevMode = true
	keys, _, _ := testRings(t)
	facade, err := NewSystemHTTPFacade(core, keys)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHTTPHandler(core, &httpProfileSpy{t: t}, HTTPOptions{PublicOrigin: "http://localhost:8080", System: facade})
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range []bool{false, true} {
		r := httptest.NewRequest("POST", "http://public.example.test/api/v1/sessions/login", nil)
		if cookie {
			r.AddCookie(&http.Cookie{Name: "agenteam_local_browser", Value: "untrusted"})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("login without browser CSRF was accepted: %d", w.Code)
		}
	}
}
