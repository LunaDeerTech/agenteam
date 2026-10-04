package account

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	object "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
)

func httpAssertSecurityHeaders(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	for name, want := range map[string]string{"Cache-Control": "no-store", "X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer"} {
		if w.Header().Get(name) != want {
			t.Fatalf("%s missing: %v", name, w.Header())
		}
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("permissive CORS")
	}
}
func httpTestActor(t *testing.T) identity.Actor {
	t.Helper()
	u, e := foundation.NewID[identity.User]()
	if e != nil {
		t.Fatal(e)
	}
	s, e := foundation.NewID[identity.Session]()
	if e != nil {
		t.Fatal(e)
	}
	a, e := identity.NewHuman(u, s)
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func httpTestProfile(t *testing.T, actor identity.Actor) c.ProfileView {
	t.Helper()
	id, e := foundation.ParseID[identity.User](actor.Details().UserID)
	if e != nil {
		t.Fatal(e)
	}
	return c.ProfileView{User: c.User{ID: id, Email: "member@example.test", Username: "member", DisplayName: "Member", Role: c.RegularUser, Theme: c.SystemTheme, Version: 9}}
}

// ProfilePort substitution is restricted to wire tests. These callbacks receive
// an explicit test actor and prove no production authentication or DB behavior.
type httpProfileSpy struct {
	t      *testing.T
	get    func(context.Context, identity.Actor) (c.ProfileView, error)
	update func(context.Context, c.ProfileChange) (c.ProfileView, error)
	theme  func(context.Context, c.ThemeChange) (c.ProfileView, error)
	put    func(context.Context, c.AvatarUpload) (c.ProfileView, error)
	delete func(context.Context, c.ProfileMutation) (c.ProfileView, error)
	read   func(context.Context, identity.Actor, c.AvatarRange) (*object.ObjectReader, error)
}

func (p *httpProfileSpy) GetProfile(ctx context.Context, a identity.Actor) (c.ProfileView, error) {
	if p.get == nil {
		p.t.Fatal("unexpected GetProfile")
	}
	return p.get(ctx, a)
}
func (p *httpProfileSpy) UpdateProfile(ctx context.Context, a c.ProfileChange) (c.ProfileView, error) {
	if p.update == nil {
		p.t.Fatal("unexpected UpdateProfile")
	}
	return p.update(ctx, a)
}
func (p *httpProfileSpy) SetTheme(ctx context.Context, a c.ThemeChange) (c.ProfileView, error) {
	if p.theme == nil {
		p.t.Fatal("unexpected SetTheme")
	}
	return p.theme(ctx, a)
}
func (p *httpProfileSpy) PutAvatar(ctx context.Context, a c.AvatarUpload) (c.ProfileView, error) {
	if p.put == nil {
		p.t.Fatal("unexpected PutAvatar")
	}
	return p.put(ctx, a)
}
func (p *httpProfileSpy) DeleteAvatar(ctx context.Context, a c.ProfileMutation) (c.ProfileView, error) {
	if p.delete == nil {
		p.t.Fatal("unexpected DeleteAvatar")
	}
	return p.delete(ctx, a)
}
func (p *httpProfileSpy) ReadAvatar(ctx context.Context, a identity.Actor, b c.AvatarRange) (*object.ObjectReader, error) {
	if p.read == nil {
		p.t.Fatal("unexpected ReadAvatar")
	}
	return p.read(ctx, a, b)
}

func TestB04HTTPConstructorsFailClosed(t *testing.T) {
	p := &httpProfileSpy{t: t}
	for _, core := range []*Service{nil, {}, {data: func() *serviceState { return nil }}, {data: func() *serviceState { return &serviceState{} }}} {
		if h, e := NewHTTPHandler(core, p, HTTPOptions{PublicOrigin: "https://example.test"}); h != nil || e == nil {
			t.Fatal("unbound core succeeded")
		}
	}
	var typedNil *httpProfileSpy
	if !nilPort(c.ProfilePort(typedNil)) {
		t.Fatal("typed nil profile accepted")
	}
}

// These construction-only sentinels deliberately panic if any embedded port
// method is called. They assert zero I/O and are never used for a request.
type httpNoCallAudit struct{ ac.Appender }
type httpNoCallEvents struct{ oc.Appender }
type httpNoCallChallenges struct{ c.ChallengeAuthority }
type httpNoCallProcesses struct{ c.ProcessAuthority }

func httpConstructionCore(t *testing.T) *Service {
	t.Helper()
	store := &httpDenyStore{}
	keys := testKeys(t)
	a, e := NewAuthority(store, keys)
	if e != nil {
		t.Fatal(e)
	}
	catalog := event.NewCatalog()
	sessions, e := c.DefineSessionsRevoked(catalog)
	if e != nil {
		t.Fatal(e)
	}
	delivery, e := c.DefineDeliveryRequested(catalog)
	if e != nil {
		t.Fatal(e)
	}
	st := &serviceState{store: store, keys: keys, hasher: NewPasswordHasher(), deps: Dependencies{Authority: a, Audit: &httpNoCallAudit{}, Secrets: &secret.Service{}, Events: &httpNoCallEvents{}, Challenges: &httpNoCallChallenges{}, Processes: &httpNoCallProcesses{}, RecoveryLog: &recoverylog.Sink{}, SessionsRevoked: sessions, DeliveryRequested: delivery}}
	return &Service{data: func() *serviceState { return st }}
}
func TestB04HTTPConstructorRequiresExactFacadeAndProfile(t *testing.T) {
	core := httpConstructionCore(t)
	ring, _, _ := testRings(t)
	facade, e := NewSystemHTTPFacade(core, ring)
	if e != nil {
		t.Fatal(e)
	}
	profile := &httpProfileSpy{t: t}
	options := HTTPOptions{PublicOrigin: "https://example.test", System: facade}
	if handler, e := NewHTTPHandler(core, profile, options); e != nil || handler == nil {
		t.Fatal("pure construction failed", e)
	}
	for _, system := range []*SystemHTTPFacade{nil, {}, {core: core}, {core: httpConstructionCore(t), pagination: ring}} {
		options.System = system
		if handler, e := NewHTTPHandler(core, profile, options); e == nil || handler != nil {
			t.Fatal("missing/foreign facade accepted")
		}
	}
	options.System = facade
	var typedNil *httpProfileSpy
	for _, p := range []c.ProfilePort{nil, typedNil} {
		if handler, e := NewHTTPHandler(core, p, options); e == nil || handler != nil {
			t.Fatal("unbound profile accepted")
		}
	}
	options.PublicOrigin = "http://external.example.test"
	if handler, e := NewHTTPHandler(core, profile, options); e == nil || handler != nil {
		t.Fatal("unsafe deployment origin accepted")
	}
	if facade, e := NewSystemHTTPFacade(core, cursor.Keyring{}); e == nil || facade != nil {
		t.Fatal("missing real ring accepted")
	}
}
func TestB04HTTPStrictProfileWireAndProjection(t *testing.T) {
	actor := httpTestActor(t)
	out := httpTestProfile(t, actor)
	calls := 0
	p := &httpProfileSpy{t: t, update: func(ctx context.Context, q c.ProfileChange) (c.ProfileView, error) {
		calls++
		if ctx == nil || q.Actor.Details() != actor.Details() || q.Key != "profile-command" || q.ExpectedVersion != 8 || q.Username != nil || q.DisplayName == nil || *q.DisplayName != "" {
			t.Fatal("profile arguments changed")
		}
		return out, nil
	}}
	h := &accountHTTP{profiles: p}
	for _, tc := range []struct {
		body string
		want int
	}{
		{`{"version":"8","display_name":""}`, 200},
		{`{"version":"8","display_name":null}`, 400},
		{`{"version":"8","display_name":"a","display_name":"b"}`, 400},
		{`{"version":"8","email":"other@example.test"}`, 400},
		{`{"version":"8","role":"admin"}`, 400},
		{`{"version":8,"display_name":""}`, 400},
		{`{"version":"08","display_name":""}`, 400},
		{`{"version":"8","display_name":""} {}`, 400},
		{`{"version":"8"}`, 400},
		{`{"version":"8","username":"ab"}`, 400},
		{`{"version":"8","display_name":"` + strings.Repeat("x", 16<<10) + `"}`, 413},
	} {
		r := httptest.NewRequest("PATCH", "https://example.test/api/v1/me", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.httpUpdateProfile(w, r, httpRequest{actor: actor, key: "profile-command"})
		if w.Code != tc.want {
			t.Fatalf("status=%d want=%d body=%s", w.Code, tc.want, w.Body.String())
		}
		httpAssertSecurityHeaders(t, w)
		for _, secret := range []string{"password_phc", "credential_ref", "object_id", "token_verifier"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("unsafe profile projection")
			}
		}
	}
	if calls != 1 {
		t.Fatalf("profile calls=%d", calls)
	}
}
func TestB04HTTPProfilePreferencesAndDelete(t *testing.T) {
	actor := httpTestActor(t)
	view := httpTestProfile(t, actor)
	calls := 0
	p := &httpProfileSpy{t: t, theme: func(_ context.Context, q c.ThemeChange) (c.ProfileView, error) {
		calls++
		if q.Actor.Details() != actor.Details() || q.Key != "theme" || q.ExpectedVersion != 8 || q.Theme != c.DarkTheme {
			t.Fatal("theme args")
		}
		view.User.Theme = c.DarkTheme
		return view, nil
	}, delete: func(_ context.Context, q c.ProfileMutation) (c.ProfileView, error) {
		calls++
		if q.Actor.Details() != actor.Details() || q.Key != "delete" || q.ExpectedVersion != 9 {
			t.Fatal("delete args")
		}
		return view, nil
	}}
	h := &accountHTTP{profiles: p}
	r := httptest.NewRequest("PUT", "https://example.test/api/v1/me/preferences", strings.NewReader(`{"version":"8","theme":"dark"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.httpSetPreferences(w, r, httpRequest{actor: actor, key: "theme"})
	if w.Code != 200 || w.Body.String() != `{"version":"9","theme":"dark"}` {
		t.Fatal("theme response", w.Code, w.Body.String())
	}
	r = httptest.NewRequest("DELETE", "https://example.test/api/v1/me/avatar", strings.NewReader(`{"version":"9"}`))
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.httpDeleteAvatar(w, r, httpRequest{actor: actor, key: "delete"})
	if w.Code != 204 || w.Body.Len() != 0 || calls != 2 {
		t.Fatal("delete response", w.Code, calls)
	}
}
func TestB04HTTPAvatarUploadRawLimitAndOwnership(t *testing.T) {
	actor := httpTestActor(t)
	view := httpTestProfile(t, actor)
	for _, tc := range []struct {
		name, media, match string
		size, declared     int64
		want               int
		call               bool
	}{
		{"raw", "image/png", `"8"`, 4, 4, 200, true},
		{"chunked", "image/webp", `"8"`, 4, -1, 200, true},
		{"limit", "image/jpeg", `"8"`, accountHTTPAvatarLimit, accountHTTPAvatarLimit, 200, true},
		{"overflow", "image/png", `"8"`, accountHTTPAvatarLimit + 1, -1, 413, true},
		{"declared-large", "image/png", `"8"`, 1, accountHTTPAvatarLimit + 1, 413, false},
		{"multipart", "multipart/form-data; boundary=x", `"8"`, 1, 1, 415, false},
		{"svg", "image/svg+xml", `"8"`, 1, 1, 415, false},
		{"weak", "image/png", `W/"8"`, 1, 1, 400, false},
		{"wildcard", "image/png", "*", 1, 1, 400, false},
		{"many", "image/png", `"8", "9"`, 1, 1, 400, false},
		{"zero", "image/png", `"0"`, 1, 1, 400, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &httpBodySpy{Reader: io.LimitReader(httpRepeatReader{}, tc.size)}
			calls := 0
			p := &httpProfileSpy{t: t, put: func(_ context.Context, q c.AvatarUpload) (c.ProfileView, error) {
				calls++
				defer q.Body.Close()
				if q.Actor.Details() != actor.Details() || q.Key != "avatar" || q.ExpectedVersion != 8 || q.MediaType != tc.media || q.ByteSize != tc.declared {
					t.Fatal("upload arguments")
				}
				n, e := io.Copy(io.Discard, q.Body)
				if e != nil {
					var tooLarge *http.MaxBytesError
					if !errors.As(e, &tooLarge) || n != accountHTTPAvatarLimit {
						t.Fatal("limit error", n, e)
					}
					return c.ProfileView{}, e
				}
				if n != tc.size {
					t.Fatal("raw body changed")
				}
				return view, nil
			}}
			h := &accountHTTP{profiles: p}
			r := httptest.NewRequest("PUT", "https://example.test/api/v1/me/avatar", body)
			r.ContentLength = tc.declared
			r.Header.Set("Content-Type", tc.media)
			r.Header.Set("If-Match", tc.match)
			w := httptest.NewRecorder()
			h.httpPutAvatar(w, r, httpRequest{actor: actor, key: "avatar"})
			wantCalls := 0
			if tc.call {
				wantCalls = 1
			}
			if w.Code != tc.want || calls != wantCalls || body.closes != 1 || !tc.call && body.reads != 0 {
				t.Fatalf("status=%d calls=%d reads=%d closes=%d", w.Code, calls, body.reads, body.closes)
			}
		})
	}
}

type httpRepeatReader struct{}

func (httpRepeatReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}
func TestB04HTTPAvatarRangeSyntax(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want c.AvatarRange
	}{
		{"", c.AvatarRange{Kind: c.AvatarRangeAll}},
		{"bytes=0-0", c.AvatarRange{Kind: c.AvatarRangeClosed, Length: 1}},
		{"bytes=2-4", c.AvatarRange{Kind: c.AvatarRangeClosed, Offset: 2, Length: 3}},
		{"bytes=2-", c.AvatarRange{Kind: c.AvatarRangeFrom, Offset: 2}},
		{"bytes=-4", c.AvatarRange{Kind: c.AvatarRangeSuffix, Length: 4}},
	} {
		r := httptest.NewRequest("GET", "https://example.test", nil)
		if tc.raw != "" {
			r.Header.Set("Range", tc.raw)
		}
		got, e := httpAvatarRange(r)
		if e != nil || got != tc.want {
			t.Fatalf("%s: %+v %v", tc.raw, got, e)
		}
	}
	for _, raw := range []string{"bytes=", "bytes=-0", "bytes=4-2", "bytes=0-9223372036854775807", "bytes=9223372036854775808-", "bytes=0-1,3-4", "bytes= 0-1", "Bytes=0-1", "bytes=1--2", "bytes=+1-2"} {
		r := httptest.NewRequest("GET", "https://example.test", nil)
		r.Header.Set("Range", raw)
		if _, e := httpAvatarRange(r); !hasFaultCode(e, foundation.RangeNotSatisfiable) {
			t.Fatalf("accepted %s", raw)
		}
	}
	r := httptest.NewRequest("GET", "https://example.test", nil)
	r.Header.Add("Range", "bytes=0-1")
	r.Header.Add("Range", "bytes=2-3")
	if _, e := httpAvatarRange(r); e == nil {
		t.Fatal("multiple ranges")
	}
}
func httpTestObjectReader(t *testing.T, body io.ReadCloser, resolved *object.ResolvedRange) *object.ObjectReader {
	t.Helper()
	id, e := foundation.NewID[object.StoredObject]()
	if e != nil {
		t.Fatal(e)
	}
	at, _ := foundation.NewInstant(time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC))
	reader, e := object.NewObjectReader(object.ObjectMeta{ID: id, Scope: identity.SystemScope(), MediaType: "image/png", ByteSize: 6, SHA256: foundation.Digest("sha256:" + strings.Repeat("1", 64)), State: object.Available, Version: 1, CreatedAt: at}, resolved, body)
	if e != nil {
		t.Fatal(e)
	}
	return reader
}
func TestB04HTTPAvatarStreamingAndHeadClose(t *testing.T) {
	actor := httpTestActor(t)
	for _, tc := range []struct {
		method, rangeHeader, payload string
		resolved                     *object.ResolvedRange
		want                         int
	}{
		{"GET", "", "abcdef", nil, 200},
		{"GET", "bytes=2-4", "cde", &object.ResolvedRange{Offset: 2, Length: 3, Total: 6}, 206},
		{"HEAD", "", "abcdef", nil, 200},
		{"HEAD", "bytes=-2", "ef", &object.ResolvedRange{Offset: 4, Length: 2, Total: 6}, 206},
	} {
		t.Run(tc.method+tc.rangeHeader, func(t *testing.T) {
			body := &httpBodySpy{Reader: strings.NewReader(tc.payload)}
			calls := 0
			p := &httpProfileSpy{t: t, read: func(_ context.Context, a identity.Actor, q c.AvatarRange) (*object.ObjectReader, error) {
				calls++
				if a.Details() != actor.Details() {
					t.Fatal("reader actor")
				}
				want, _ := httpAvatarRange(func() *http.Request {
					r := httptest.NewRequest(tc.method, "https://example.test", nil)
					if tc.rangeHeader != "" {
						r.Header.Set("Range", tc.rangeHeader)
					}
					return r
				}())
				if q != want {
					t.Fatal("range wire")
				}
				return httpTestObjectReader(t, body, tc.resolved), nil
			}}
			h := &accountHTTP{profiles: p}
			r := httptest.NewRequest(tc.method, "https://example.test/api/v1/me/avatar", nil)
			if tc.rangeHeader != "" {
				r.Header.Set("Range", tc.rangeHeader)
			}
			w := httptest.NewRecorder()
			httpSecurityHeaders(w)
			h.httpGetAvatar(w, r, httpRequest{actor: actor})
			if w.Code != tc.want || calls != 1 || body.closes != 1 {
				t.Fatal("stream outcome", w.Code, calls, body.closes)
			}
			if tc.method == "HEAD" {
				if body.reads != 0 || w.Body.Len() != 0 {
					t.Fatal("HEAD read payload")
				}
			} else if w.Body.String() != tc.payload {
				t.Fatal("payload mismatch")
			}
			if tc.resolved != nil && w.Header().Get("Content-Range") == "" {
				t.Fatal("missing range")
			}
			httpAssertSecurityHeaders(t, w)
		})
	}
}

type httpBlockingClose struct {
	reads, closes    atomic.Int32
	entered, release chan struct{}
}

func (b *httpBlockingClose) Read(p []byte) (int, error) { b.reads.Add(1); return 0, io.EOF }
func (b *httpBlockingClose) Close() error               { b.closes.Add(1); close(b.entered); <-b.release; return nil }
func TestB04HTTPHeadWaitsForActualClose(t *testing.T) {
	actor := httpTestActor(t)
	body := &httpBlockingClose{entered: make(chan struct{}), release: make(chan struct{})}
	reader := httpTestObjectReader(t, body, nil)
	p := &httpProfileSpy{t: t, read: func(_ context.Context, a identity.Actor, q c.AvatarRange) (*object.ObjectReader, error) {
		if a.Details() != actor.Details() || q.Kind != c.AvatarRangeAll {
			t.Error("read arguments")
		}
		return reader, nil
	}}
	h := &accountHTTP{profiles: p}
	done := make(chan struct{})
	w := httptest.NewRecorder()
	go func() {
		defer close(done)
		h.httpGetAvatar(w, httptest.NewRequest("HEAD", "https://example.test/api/v1/me/avatar", nil), httpRequest{actor: actor})
	}()
	select {
	case <-body.entered:
	case <-time.After(time.Second):
		close(body.release)
		t.Fatal("Close not reached")
	}
	select {
	case <-done:
		close(body.release)
		t.Fatal("returned before actual close")
	default:
	}
	close(body.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close did not join")
	}
	if body.closes.Load() != 1 || body.reads.Load() != 0 || w.Code != 200 {
		t.Fatal("head ownership")
	}
}

type httpErrorCloser struct {
	io.Reader
	err    error
	closes int
}

func (b *httpErrorCloser) Close() error { b.closes++; return b.err }
func TestB04HTTPStreamErrorsAbortWithoutProblemSuffix(t *testing.T) {
	actor := httpTestActor(t)
	for _, mode := range []string{"short", "long", "close-error"} {
		t.Run(mode, func(t *testing.T) {
			payload := "abcdef"
			if mode == "short" {
				payload = "abc"
			}
			if mode == "long" {
				payload = "abcdefg"
			}
			body := &httpErrorCloser{Reader: strings.NewReader(payload)}
			if mode == "close-error" {
				body.err = errors.New("private-reader-diagnostic")
			}
			reader := httpTestObjectReader(t, body, nil)
			p := &httpProfileSpy{t: t, read: func(_ context.Context, _ identity.Actor, _ c.AvatarRange) (*object.ObjectReader, error) {
				return reader, nil
			}}
			h := &accountHTTP{profiles: p}
			w := httptest.NewRecorder()
			func() {
				defer func() {
					if v := recover(); v != http.ErrAbortHandler {
						t.Fatalf("stream did not abort: %v", v)
					}
				}()
				h.httpGetAvatar(w, httptest.NewRequest("GET", "https://example.test/api/v1/me/avatar", nil), httpRequest{actor: actor})
			}()
			if body.closes != 1 || strings.Contains(w.Body.String(), "problem") || strings.Contains(w.Body.String(), "private-reader-diagnostic") {
				t.Fatal("error body or close")
			}
		})
	}
}
func TestB04HTTPSafeErrorsAndEncodedResponses(t *testing.T) {
	r := httptest.NewRequest("POST", "https://example.test/api/v1/pasted-secret?token=private", nil)
	w := httptest.NewRecorder()
	httpProblem(w, r, foundation.NewFault(foundation.CommitUnknown, foundation.Unknown).WithCause(errors.New("private-db-diagnostic")))
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"retry_hint":"lookup"`) || strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "pasted-secret") {
		t.Fatal("unsafe problem", w.Body.String())
	}
	httpAssertSecurityHeaders(t, w)
	w = httptest.NewRecorder()
	httpJSON(w, r, 200, struct {
		Version foundation.Version `json:"version"`
	}{})
	if w.Code != 503 || len(w.Result().Cookies()) != 0 {
		t.Fatal("encoding failure committed success")
	}
	var secret httpSecret
	if e := json.Unmarshal([]byte(`"super-private-token"`), &secret); e != nil {
		t.Fatal(e)
	}
	defer secret.httpDestroy()
	encoded, e := json.Marshal(struct {
		Secret httpSecret `json:"secret"`
	}{secret})
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(fmt.Sprintf("%+v %#v %s", secret, struct{ Secret httpSecret }{secret}, encoded), "super-private-token") {
		t.Fatal("input carrier leaks")
	}
}

func TestB04HTTPCookieEncodingAndMaterialFailureSetNoCookie(t *testing.T) {
	b, _ := csrfNewBoundary("https://example.test")
	r := httptest.NewRequest("POST", "https://example.test/api/v1/sessions/login", nil)
	calls := 0
	use := func(fn func([]byte) error) error { calls++; return fn([]byte("opaque-session")) }
	w := httptest.NewRecorder()
	b.httpCookieJSON(w, r, 200, struct {
		Version foundation.Version `json:"version"`
	}{}, b.sessionName, time.Time{}, use)
	if calls != 0 || w.Code != 503 || len(w.Result().Cookies()) != 0 {
		t.Fatal("material consumed before encoding")
	}
	w = httptest.NewRecorder()
	b.httpCookieJSON(w, r, 200, struct {
		Completed bool `json:"completed"`
	}{true}, b.sessionName, time.Time{}, func(func([]byte) error) error { calls++; return fault(foundation.CommitUnknown, nil) })
	if calls != 1 || w.Code != 503 || len(w.Result().Cookies()) != 0 || !strings.Contains(w.Body.String(), `"retry_hint":"lookup"`) {
		t.Fatal("unknown material set cookie")
	}
	w = httptest.NewRecorder()
	b.httpCookieJSON(w, r, 200, struct {
		Completed bool `json:"completed"`
	}{true}, b.sessionName, time.Time{}, use)
	cookies := w.Result().Cookies()
	if calls != 2 || w.Code != 200 || len(cookies) != 1 || cookies[0].Name != b.sessionName || cookies[0].Value != "opaque-session" || w.Body.String() != `{"completed":true}` {
		t.Fatal("safe cookie response")
	}
	httpAssertSecurityHeaders(t, w)
	w = httptest.NewRecorder()
	b.httpCookieJSON(w, r, 200, struct{}{}, b.sessionName, time.Time{}, func(fn func([]byte) error) error { return fn([]byte("invalid;cookie")) })
	if w.Code != 503 || len(w.Result().Cookies()) != 0 {
		t.Fatal("invalid cookie value emitted")
	}
}
func TestB04HTTPStrictJSONMediaAndBoundedProof(t *testing.T) {
	for _, tc := range []struct {
		media, encoding, body string
		want                  foundation.Code
	}{
		{"text/plain", "", `{}`, foundation.UnsupportedMediaType},
		{"application/json", "gzip", `{}`, foundation.UnsupportedMediaType},
		{"application/json", "", `{"value":1,"\u0076alue":2}`, foundation.InvalidArgument},
		{"application/json", "", `{"value":1} false`, foundation.InvalidArgument},
	} {
		r := httptest.NewRequest("POST", "https://example.test", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", tc.media)
		if tc.encoding != "" {
			r.Header.Set("Content-Encoding", tc.encoding)
		}
		var dto struct {
			Value int `json:"value"`
		}
		if e := httpapi.DecodeJSON(httptest.NewRecorder(), r, &dto, accountHTTPJSONLimit); !hasFaultCode(e, tc.want) {
			t.Fatalf("unexpected decode error %v", e)
		}
	}
	r := httptest.NewRequest("POST", "https://example.test", bytes.NewReader(bytes.Repeat([]byte(" "), (4<<10)+1)))
	r.Header.Set("Content-Type", "application/json")
	var dto struct {
		Angle int `json:"angle"`
	}
	if e := httpapi.DecodeJSON(httptest.NewRecorder(), r, &dto, 4<<10); !hasFaultCode(e, foundation.PayloadTooLarge) {
		t.Fatal("proof bound")
	}
}
func TestB04HTTPOpenAPIRoutesAgree(t *testing.T) {
	data, e := os.ReadFile("../../../api/openapi/account.json")
	if e != nil {
		t.Fatal(e)
	}
	var document struct {
		OpenAPI string                                `json:"openapi"`
		Paths   map[string]map[string]json.RawMessage `json:"paths"`
	}
	if json.Unmarshal(data, &document) != nil || document.OpenAPI != "3.1.0" {
		t.Fatal("invalid OpenAPI")
	}
	want := map[string]bool{}
	for _, route := range (&accountHTTP{}).httpRoutes() {
		key := route.method + " /api/v1" + route.path
		want[key] = true
		if len(document.Paths["/api/v1"+route.path][strings.ToLower(route.method)]) == 0 {
			t.Fatal("route missing in OpenAPI", key)
		}
	}
	for path, methods := range document.Paths {
		for method := range methods {
			if method == "parameters" {
				continue
			}
			if !want[strings.ToUpper(method)+" "+path] {
				t.Fatal("OpenAPI route absent from handler", method, path)
			}
		}
	}
}
