package account

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// These are real account routes denied before any storage call. Keeping their
// 401 responses cookie-free lets a late browser response coexist with a newer
// login in another tab. Explicit logout's cookie behavior remains separate.
func TestAccountHTTPAuthenticationFailureDoesNotClearCookie(t *testing.T) {
	boundary, err := csrfNewBoundary("https://example.test")
	if err != nil {
		t.Fatal(err)
	}
	handler := (&accountHTTP{csrf: boundary}).httpHandler()
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/session"},
		{http.MethodGet, "/api/v1/me"},
		{http.MethodGet, "/api/v1/me/preferences"},
		{http.MethodGet, "/api/v1/me/avatar"},
		{http.MethodHead, "/api/v1/me/avatar"},
		{http.MethodPatch, "/api/v1/me"},
		{http.MethodPost, "/api/v1/sessions/logout"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, "https://example.test"+tc.path, nil)
			request.Header.Set("Origin", "https://example.test")
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "stale-session-request")
			request.Header.Set("X-CSRF-Token", "stale-session-csrf")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d want=401", response.Code)
			}
			if len(response.Header().Values("Set-Cookie")) != 0 {
				t.Fatal("an authentication error can overwrite a newer browser Session cookie")
			}
			httpAssertSecurityHeaders(t, response)
		})
	}
}
