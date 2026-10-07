package account

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
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

// Controlled SQL rows exercise the real Service/Authority/CSRF boundary without
// opening a database. They make no claim about persisted Session authorization.
type boundarySessionStore struct {
	directoryQueryFailureStore
	role                          c.Role
	revoked                       bool
	lookup, current, transactions int
}

func (s *boundarySessionStore) WithinTx(ctx context.Context, _ f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.tx = f.NewTx()
	s.transactions++
	if err := fn(ctx, s.tx); err != nil {
		var fault *f.Fault
		if !errors.As(err, &fault) {
			panic("untyped boundary failure")
		}
		return f.NotCommittedResult(fault)
	}
	return f.CommittedResult()
}
func (s *boundarySessionStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		panic("boundary authorization left transaction")
	}
	return s, nil
}
func (s *boundarySessionStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if strings.Contains(query, "WHERE token_verifier=") {
		s.lookup++
		return directoryScanFunc(func(dest ...any) error {
			*dest[0].(*string), *dest[1].(*string) = s.actor.Details().UserID, s.actor.Details().SessionID
			return nil
		})
	}
	return directoryScanFunc(func(dest ...any) error {
		if err := s.directoryQueryFailureStore.QueryRow(ctx, query, args...).Scan(dest...); err != nil {
			return err
		}
		if strings.Contains(query, "FROM agenteam_account.sessions WHERE id=") {
			s.current++
			*dest[6].(*bool) = s.revoked
		} else {
			*dest[4].(*c.Role) = s.role
		}
		return nil
	})
}
func humanBoundaryFixture(t *testing.T, role c.Role) (*HTTPBoundary, *boundarySessionStore, string, string) {
	t.Helper()
	core := httpConstructionCore(t)
	s := &boundarySessionStore{directoryQueryFailureStore: directoryQueryFailureStore{actor: httpTestActor(t)}, role: role}
	a, err := NewAuthority(s, core.state().keys)
	if err != nil {
		t.Fatal(err)
	}
	core.state().store, core.state().deps.Authority = s, a
	core.state().operations, core.state().changed = map[*operation]bool{}, make(chan struct{})
	b, err := NewHTTPBoundary(core, "https://example.test")
	if err != nil {
		t.Fatal(err)
	}
	cookie := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{11}, 32))
	mac, err := core.state().keys.mac("a2", "csrf-v1", []byte("session"), []byte(s.actor.Details().SessionID), []byte(cookie))
	if err != nil {
		t.Fatal(err)
	}
	return b, s, cookie, base64.RawURLEncoding.EncodeToString(mac)
}
func TestHTTPBoundaryHumanAndOriginalSystemAuthority(t *testing.T) {
	for _, role := range []c.Role{"user", "admin"} {
		for _, method := range []string{"GET", "HEAD", "OPTIONS", "POST", "PUT", "DELETE"} {
			for _, system := range []bool{false, true} {
				t.Run(string(role)+"/"+method+"/"+map[bool]string{false: "human", true: "system"}[system], func(t *testing.T) {
					b, store, cookie, csrf := humanBoundaryFixture(t, role)
					r := httptest.NewRequest(method, "https://example.test/api/v1/projects/resolve", nil)
					r.AddCookie(&http.Cookie{Name: b.csrf.sessionName, Value: cookie})
					r.Header.Set("Origin", "https://example.test")
					if csrfUnsafe(method) {
						r.Header.Set("X-CSRF-Token", csrf)
					}
					var actor id.Actor
					var err error
					if system {
						actor, err = b.RequireSystem(r, id.Read)
					} else {
						actor, err = b.RequireHuman(r)
					}
					if system && role == "user" {
						if !hasFaultCode(err, f.Forbidden) || actor.Validate() == nil {
							t.Fatal("System admitted ordinary user", err)
						}
					} else if err != nil || !actor.Equal(store.actor) {
						t.Fatal("wrong authenticated Human", err)
					}
					checks := 1
					if csrfUnsafe(method) {
						checks++
					}
					if system {
						checks++
					}
					if store.lookup != 1 || store.current != checks || store.transactions != checks || len(b.core.state().operations) != 0 {
						t.Fatal("authentication/CSRF/System order changed or operation retained", store.lookup, store.current, store.transactions)
					}
				})
			}
		}
	}
}
func TestHTTPBoundaryHumanRejectsBeforeIdentityAndPreservesSystemIntent(t *testing.T) {
	for name, test := range map[string]struct {
		change func(*http.Request)
		code   f.Code
	}{
		"host":                  {func(r *http.Request) { r.Host = "other.test" }, f.OriginDenied},
		"origin":                {func(r *http.Request) { r.Header.Set("Origin", "https://other.test") }, f.OriginDenied},
		"fetch":                 {func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, f.OriginDenied},
		"path":                  {func(r *http.Request) { r.URL.Path = "/api//v1" }, f.InvalidArgument},
		"raw_path":              {func(r *http.Request) { r.URL.RawPath = "/api/%76%31" }, f.InvalidArgument},
		"cookie_absent":         {func(r *http.Request) { r.Header.Del("Cookie"); r.Header.Set("Authorization", "Bearer private") }, f.Unauthenticated},
		"cookie_duplicate":      {func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "__Host-agenteam_session", Value: "other"}) }, f.Unauthenticated},
		"unsafe_without_csrf":   {func(r *http.Request) { r.Method = "POST" }, f.CSRFFailed},
		"unsafe_without_origin": {func(r *http.Request) { r.Method = "POST"; r.Header.Del("Origin") }, f.OriginDenied},
	} {
		t.Run(name, func(t *testing.T) {
			b, store, cookie, _ := humanBoundaryFixture(t, "user")
			r := httptest.NewRequest("GET", "https://example.test/api/v1", nil)
			r.Header.Set("Origin", "https://example.test")
			r.AddCookie(&http.Cookie{Name: b.csrf.sessionName, Value: cookie})
			test.change(r)
			actor, err := b.RequireHuman(r)
			if !hasFaultCode(err, test.code) || actor.Validate() == nil || store.lookup != 0 || store.transactions != 0 {
				t.Fatal("boundary admitted identity I/O", err)
			}
		})
	}
	b, store, cookie, _ := humanBoundaryFixture(t, "admin")
	r := httptest.NewRequest("GET", "https://example.test/api/v1", nil)
	r.AddCookie(&http.Cookie{Name: b.csrf.sessionName, Value: cookie})
	if _, err := b.RequireSystem(r, id.Launch); !hasFaultCode(err, f.InvalidArgument) || store.lookup != 0 {
		t.Fatal("System intent check moved after authentication", err)
	}
	r.Host = "other.test"
	if _, err := b.RequireSystem(r, id.Launch); !hasFaultCode(err, f.OriginDenied) {
		t.Fatal("System security priority changed", err)
	}
	for _, boundary := range []*HTTPBoundary{nil, {}, b} {
		for _, request := range []*http.Request{nil, {}} {
			if actor, err := boundary.RequireHuman(request); err == nil || actor.Validate() == nil {
				t.Fatal("nil Human boundary accepted")
			}
		}
	}
}
func TestHTTPBoundaryHumanCurrentRevocationCSRFAndCancellation(t *testing.T) {
	for _, mode := range []string{"revoked", "bad_csrf", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			b, store, cookie, csrf := humanBoundaryFixture(t, "user")
			r := httptest.NewRequest("GET", "https://example.test/api/v1", nil)
			r.AddCookie(&http.Cookie{Name: b.csrf.sessionName, Value: cookie})
			want := f.SessionRevoked
			switch mode {
			case "revoked":
				store.revoked = true
			case "bad_csrf":
				r.Method = "POST"
				r.Header.Set("Origin", "https://example.test")
				r.Header.Set("X-CSRF-Token", "x"+csrf)
				want = f.CSRFFailed
			case "cancelled":
				ctx, cancel := context.WithCancel(r.Context())
				cancel()
				r = r.WithContext(ctx)
				want = f.DependencyUnavailable
			}
			actor, err := b.RequireHuman(r)
			if !hasFaultCode(err, want) || actor.Validate() == nil || len(b.core.state().operations) != 0 {
				t.Fatal("failed Human retained candidate/operation", err)
			}
			if mode == "cancelled" && store.lookup != 0 {
				t.Fatal("cancelled request reached SQL")
			}
		})
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
