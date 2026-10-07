package model

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const credentialTestID = "01900000-0000-7000-8000-000000000019"
const credentialTestCollection = projectHTTPTestBase + "/model-credentials"
const credentialTestDetail = credentialTestCollection + "/" + credentialTestID
const credentialTestLookup = projectHTTPTestBase + "/model-credential-commands/lookup"

type credentialTestService struct {
	execute                func(context.Context, sc.WriteRequest) (sc.MutationResult, error)
	metadata               func(context.Context, id.Actor, sc.CredentialRef) (sc.Metadata, error)
	lookup                 func(context.Context, sc.WriteCommandLookupRequest) (sc.WriteCommandObservation, error)
	writes, reads, lookups atomic.Int32
}

func (s *credentialTestService) ExecuteWrite(c context.Context, r sc.WriteRequest) (sc.MutationResult, error) {
	s.writes.Add(1)
	return s.execute(c, r)
}
func (s *credentialTestService) Metadata(c context.Context, a id.Actor, r sc.CredentialRef) (sc.Metadata, error) {
	s.reads.Add(1)
	return s.metadata(c, a, r)
}
func (s *credentialTestService) LookupWriteCommand(c context.Context, r sc.WriteCommandLookupRequest) (sc.WriteCommandObservation, error) {
	s.lookups.Add(1)
	return s.lookup(c, r)
}
func credentialTestRef() sc.CredentialRef {
	p, _ := f.ParseID[id.Project](projectHTTPTestProject)
	scope, _ := id.InProject(p)
	key, _ := f.ParseID[sc.Credential](credentialTestID)
	ref, _ := sc.NewCredentialRef(key, scope)
	return ref
}
func credentialTestHandler(t *testing.T) (*projectCredentialHTTP, *projectHTTPTestBoundary, *credentialTestService) {
	t.Helper()
	b := &projectHTTPTestBoundary{actor: testActor(t)}
	s := &credentialTestService{}
	s.execute = func(_ context.Context, r sc.WriteRequest) (sc.MutationResult, error) {
		return sc.MutationResult{Metadata: sc.Metadata{CredentialRef: credentialTestRef(), Purpose: sc.Model, Version: r.ExpectedVersion + 1}, Deleted: r.Kind == sc.Delete}, nil
	}
	s.metadata = func(context.Context, id.Actor, sc.CredentialRef) (sc.Metadata, error) {
		return sc.Metadata{CredentialRef: credentialTestRef(), Purpose: sc.Model, Version: 1}, nil
	}
	s.lookup = func(context.Context, sc.WriteCommandLookupRequest) (sc.WriteCommandObservation, error) {
		return sc.WriteCommandObservation{}, nil
	}
	return &projectCredentialHTTP{s, b}, b, s
}
func credentialTestRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, "https://credentials.example"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "original-key")
	return r
}
func TestProjectCredentialHTTPPureConstruction(t *testing.T) {
	var typed *credentialTestService
	for _, s := range []sc.HumanWriteCommands{nil, typed} {
		if h, e := NewProjectCredentialHTTPHandler(s, &account.HTTPBoundary{}); e == nil || h != nil {
			t.Fatal("nil port accepted")
		}
	}
	s := &credentialTestService{}
	if h, e := NewProjectCredentialHTTPHandler(s, nil); e == nil || h != nil {
		t.Fatal("nil boundary accepted")
	}
	if h, e := NewProjectCredentialHTTPHandler(s, &account.HTTPBoundary{}); e != nil || h == nil || s.writes.Load()+s.reads.Load()+s.lookups.Load() != 0 {
		t.Fatal("constructor I/O or failure")
	}
}
func TestProjectCredentialHTTPPureCallOrder(t *testing.T) {
	for _, mode := range []string{"unobserved", "observed", "late", "absent", "lookup_error", "metadata_purpose", "mutate_reject", "unknown", "material_conflict"} {
		t.Run(mode, func(t *testing.T) {
			h, b, s := credentialTestHandler(t)
			var order []string
			var retained sc.SecretMaterial
			original := f.NewFault(f.CommitUnknown, f.Unknown)
			s.lookup = func(_ context.Context, r sc.WriteCommandLookupRequest) (sc.WriteCommandObservation, error) {
				order = append(order, "lookup")
				if r.Validate() != nil || r.Identity.Key() != "original-key" || !r.Actor.Equal(b.actor) || !r.Ref.Equal(credentialTestRef()) {
					t.Fatal("lookup request altered")
				}
				if mode == "lookup_error" {
					return sc.WriteCommandObservation{}, original
				}
				if mode == "observed" || mode == "mutate_reject" || mode == "material_conflict" || mode == "late" && s.lookups.Load() == 2 {
					v := sc.MutationResult{Metadata: sc.Metadata{CredentialRef: r.Ref, Purpose: sc.Model, Version: 2}}
					return sc.WriteCommandObservation{Observed: true, Result: &v}, nil
				}
				return sc.WriteCommandObservation{}, nil
			}
			s.metadata = func(context.Context, id.Actor, sc.CredentialRef) (sc.Metadata, error) {
				order = append(order, "metadata")
				if mode == "late" || mode == "absent" {
					return sc.Metadata{}, fault(f.NotFound)
				}
				v := sc.Metadata{CredentialRef: credentialTestRef(), Purpose: sc.Model, Version: 1}
				if mode == "metadata_purpose" {
					v.Purpose = sc.SMTP
				}
				return v, nil
			}
			s.execute = func(_ context.Context, r sc.WriteRequest) (sc.MutationResult, error) {
				order = append(order, "execute")
				retained = r.Value
				if r.Identity.Key() != "original-key" || r.ExpectedVersion != 1 || r.Purpose != sc.Model || !r.Ref.Equal(credentialTestRef()) {
					t.Fatal("original write altered")
				}
				if e := r.Value.Use(func(v []byte) error {
					if string(v) != "material-private" {
						t.Fatal("material changed")
					}
					return nil
				}); e != nil {
					t.Fatal("material unavailable")
				}
				if mode == "mutate_reject" {
					return sc.MutationResult{}, fault(f.Forbidden)
				}
				if mode == "unknown" {
					return sc.MutationResult{}, original
				}
				if mode == "material_conflict" {
					return sc.MutationResult{}, fault(f.IdempotencyKeyReused)
				}
				return sc.MutationResult{Metadata: sc.Metadata{CredentialRef: r.Ref, Purpose: sc.Model, Version: 2}}, nil
			}
			w := summaryWriter()
			h.ServeHTTP(w, credentialTestRequest("PUT", credentialTestDetail, `{"expected_version":"1","value":"material-private"}`))
			want, status := "lookup,metadata,execute", 200
			switch mode {
			case "observed":
				want = "lookup,execute"
			case "late":
				want = "lookup,metadata,lookup,execute"
			case "absent", "metadata_purpose":
				want = "lookup,metadata,lookup"
				status = 404
			case "lookup_error":
				want = "lookup"
				status = 503
			case "mutate_reject":
				want = "lookup,execute"
				status = 403
			case "unknown":
				status = 503
			case "material_conflict":
				want = "lookup,execute"
				status = 409
			}
			if strings.Join(order, ",") != want || w.Code != status || s.writes.Load() > 1 {
				t.Fatal("call order/status", order, w.Code)
			}
			if retained.Use(func([]byte) error { return nil }) == nil || strings.Contains(w.Body.String(), "material-private") {
				t.Fatal("material retained or exposed")
			}
			w.cleared(t)
		})
	}
}
func TestProjectCredentialHTTPPureMetadataAndErrors(t *testing.T) {
	var length string
	for _, method := range []string{"GET", "HEAD"} {
		h, _, s := credentialTestHandler(t)
		w := summaryWriter()
		h.ServeHTTP(w, credentialTestRequest(method, credentialTestDetail, ""))
		if w.Code != 200 || s.reads.Load() != 1 || s.writes.Load()+s.lookups.Load() != 0 {
			t.Fatal("metadata dispatch")
		}
		if method == "GET" {
			length = strconv.Itoa(w.Body.Len())
		} else if w.Body.Len() != 0 {
			t.Fatal("HEAD body")
		}
		if w.Header().Get("Content-Length") != length {
			t.Fatal("HEAD full encoding")
		}
		w.cleared(t)
	}
	for _, phase := range []string{"check", "auth", "service"} {
		t.Run(phase, func(t *testing.T) {
			h, b, s := credentialTestHandler(t)
			original := f.NewFault(f.CommitUnknown, f.Unknown)
			var got error
			b.problem = func(w http.ResponseWriter, r *http.Request, e error) { got = e; httpapi.WriteProblem(w, r, e) }
			switch phase {
			case "check":
				b.check = func(http.ResponseWriter, *http.Request) error { return original }
			case "auth":
				b.auth = func(*http.Request) (id.Actor, error) { return id.Actor{}, original }
			case "service":
				s.metadata = func(context.Context, id.Actor, sc.CredentialRef) (sc.Metadata, error) { return sc.Metadata{}, original }
			}
			w := summaryWriter()
			h.ServeHTTP(w, credentialTestRequest("HEAD", credentialTestDetail, ""))
			if got != original || w.Code != 503 || w.Body.Len() != 0 {
				t.Fatal("original error/HEAD lost")
			}
		})
	}
}

type credentialNoFlush struct {
	http.ResponseWriter
	read, write atomic.Int32
}

func (w *credentialNoFlush) SetReadDeadline(time.Time) error  { w.read.Add(1); return nil }
func (w *credentialNoFlush) SetWriteDeadline(time.Time) error { w.write.Add(1); return nil }

type credentialWrapper struct {
	http.ResponseWriter
	next func() http.ResponseWriter
}

func (w *credentialWrapper) Unwrap() http.ResponseWriter { return w.next() }

type credentialReadLayer struct {
	http.ResponseWriter
	inner http.ResponseWriter
	calls atomic.Int32
}

func (w *credentialReadLayer) Unwrap() http.ResponseWriter     { return w.inner }
func (w *credentialReadLayer) SetReadDeadline(time.Time) error { w.calls.Add(1); return nil }
func TestProjectCredentialHTTPPureCapabilityOwnership(t *testing.T) {
	for _, mode := range []string{"missing", "cycle", "unwrap_panic", "typed_nil"} {
		for _, path := range []string{credentialTestCollection, credentialTestLookup} {
			t.Run(mode+"/"+path, func(t *testing.T) {
				h, b, s := credentialTestHandler(t)
				base := summaryWriter()
				var w http.ResponseWriter = base
				switch mode {
				case "missing":
					w = &credentialNoFlush{ResponseWriter: httptest.NewRecorder()}
				case "cycle":
					wrap := &credentialWrapper{ResponseWriter: base}
					wrap.next = func() http.ResponseWriter { return wrap }
					w = wrap
				case "unwrap_panic":
					w = &credentialWrapper{ResponseWriter: base, next: func() http.ResponseWriter { panic("private-panic") }}
				case "typed_nil":
					var nilWriter *summaryHTTPWriter
					w = nilWriter
				}
				body := &summaryHTTPBody{Reader: strings.NewReader(`{"value":"private"}`)}
				r := credentialTestRequest("POST", path, "")
				r.Body = body
				// Handler-only seam: cyclic wrappers do not enter existing stateOf middleware.
				if !summaryAborts(func() { h.ServeHTTP(w, r) }) || b.checks.Load()+b.auths.Load()+s.writes.Load()+s.lookups.Load() != 0 || body.closes.Load() != 1 || base.Body.Len() != 0 {
					t.Fatal("preflight escaped ownership")
				}
			})
		}
	}
	t.Run("first_receiver_and_transparent_state", func(t *testing.T) {
		h, _, _ := credentialTestHandler(t)
		base := summaryWriter()
		outer := &credentialReadLayer{ResponseWriter: base, inner: base}
		var logs bytes.Buffer
		httpapi.Handler(slog.New(slog.NewJSONHandler(&logs, nil)), h).ServeHTTP(outer, credentialTestRequest("GET", credentialTestDetail, ""))
		if base.Code != 200 || outer.calls.Load() != 2 || len(base.reads) != 0 || len(base.writes) != 2 || base.flushes != 1 || !strings.Contains(logs.String(), `"status":200`) {
			t.Fatal("capability precedence or tracked state", logs.String())
		}
	})
}
func TestProjectCredentialHTTPPureDeadlinePanicAndJoin(t *testing.T) {
	for _, phase := range []string{"start", "abort", "reset", "callback"} {
		for _, setter := range []string{"read", "write"} {
			t.Run(phase+"/"+setter, func(t *testing.T) {
				h, b, s := credentialTestHandler(t)
				w := summaryWriter()
				r := credentialTestRequest("GET", credentialTestDetail, "")
				body := &summaryHTTPBody{Reader: strings.NewReader("")}
				r.Body = body
				ctx, cancel := context.WithCancel(r.Context())
				defer cancel()
				r = r.WithContext(ctx)
				var reads, writes atomic.Int32
				fired := make(chan struct{})
				var once sync.Once
				hook := func(which string, counter *atomic.Int32) func(time.Time) error {
					return func(at time.Time) error {
						n := counter.Add(1)
						fire := which == setter && ((phase == "start" && n == 1) || (phase == "abort" && n >= 2) || (phase == "reset" && at.IsZero()) || (phase == "callback" && n >= 2))
						if fire {
							once.Do(func() { close(fired) })
							panic("never_format_private_material")
						}
						return nil
					}
				}
				w.setRead = hook("read", &reads)
				w.setWrite = hook("write", &writes)
				if phase == "abort" {
					b.check = func(http.ResponseWriter, *http.Request) error { panic("private") }
				}
				if phase == "callback" {
					s.metadata = func(context.Context, id.Actor, sc.CredentialRef) (sc.Metadata, error) {
						cancel()
						<-fired
						return sc.Metadata{}, context.Canceled
					}
				}
				if !summaryAborts(func() { h.ServeHTTP(w, r) }) || body.closes.Load() != 1 || reads.Load() < 2 || writes.Load() < 2 {
					t.Fatal("setter panic skipped second setter/Close")
				}
				if phase == "start" && b.checks.Load()+s.reads.Load() != 0 {
					t.Fatal("failed deadline reached business")
				}
			})
		}
	}
	for _, closePanic := range []bool{false, true} {
		t.Run("callback_owned_close_"+strconv.FormatBool(closePanic), func(t *testing.T) {
			h, _, s := credentialTestHandler(t)
			w := summaryWriter()
			entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan bool, 1)
			var once sync.Once
			releaseOnce := func() { once.Do(func() { close(release) }) }
			defer releaseOnce()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls atomic.Int32
			w.setRead = func(time.Time) error {
				if calls.Add(1) == 2 {
					close(entered)
					<-release
				}
				return nil
			}
			s.metadata = func(context.Context, id.Actor, sc.CredentialRef) (sc.Metadata, error) {
				cancel()
				<-entered
				return sc.Metadata{}, context.Canceled
			}
			body := &summaryHTTPBody{Reader: strings.NewReader(""), close: func() error {
				if closePanic {
					panic("private")
				}
				return nil
			}}
			r := credentialTestRequest("GET", credentialTestDetail, "").WithContext(ctx)
			r.Body = body
			done := make(chan struct{})
			t.Cleanup(func() {
				cancel()
				releaseOnce()
				<-done // Always join, including a fatal assertion before result receive.
			})
			go func() {
				defer close(done)
				returned <- summaryAborts(func() { h.ServeHTTP(w, r) })
			}()
			<-entered
			select {
			case <-returned:
				t.Fatal("callback not joined")
			case <-time.After(20 * time.Millisecond):
			}
			releaseOnce()
			if !summaryJoined(t, returned) || body.closes.Load() != 1 {
				t.Fatal("callback/Close panic not owned")
			}
		})
	}
}
func TestProjectCredentialHTTPPureBodyWriteAndFlushFailures(t *testing.T) {
	for _, mode := range []string{"close_error", "close_panic", "write", "short", "flush", "service_panic"} {
		t.Run(mode, func(t *testing.T) {
			h, _, s := credentialTestHandler(t)
			w := summaryWriter()
			r := credentialTestRequest("POST", credentialTestCollection, `{"value":"private"}`)
			body := &summaryHTTPBody{Reader: r.Body}
			r.Body = body
			switch mode {
			case "close_error":
				body.close = func() error { return errors.New("closed") }
			case "close_panic":
				body.close = func() error { panic("private") }
			case "write":
				w.write = func([]byte) (int, error) { return 0, io.ErrClosedPipe }
			case "short":
				w.write = func(p []byte) (int, error) { return len(p) - 1, nil }
			case "flush":
				w.flush = func() error { return io.ErrClosedPipe }
			case "service_panic":
				s.execute = func(context.Context, sc.WriteRequest) (sc.MutationResult, error) { panic("private") }
			}
			if !summaryAborts(func() { h.ServeHTTP(w, r) }) || body.closes.Load() != 1 {
				t.Fatal("tail did not abort/close")
			}
			if (mode == "close_error" || mode == "close_panic") && w.Body.Len() != 0 {
				t.Fatal("Close failed after publish")
			}
		})
	}
}

func TestProjectCredentialHTTPPureDamagedMetadataNeverReplays(t *testing.T) {
	for _, mutate := range []func(*sc.Metadata){
		func(v *sc.Metadata) { v.CredentialRef = sc.CredentialRef{} },
		func(v *sc.Metadata) {
			v.CredentialRef, _ = sc.NewCredentialRef(v.CredentialRef.Details().ID, id.SystemScope())
		},
		func(v *sc.Metadata) {
			v.CredentialRef, _ = sc.NewCredentialRef(mustID[sc.Credential](t), v.CredentialRef.Details().Scope)
		},
		func(v *sc.Metadata) { v.Version = 0 }, func(v *sc.Metadata) { v.Purpose = sc.Purpose("damaged") },
	} {
		h, _, s := credentialTestHandler(t)
		s.metadata = func(context.Context, id.Actor, sc.CredentialRef) (sc.Metadata, error) {
			v := sc.Metadata{CredentialRef: credentialTestRef(), Purpose: sc.Model, Version: 1}
			mutate(&v)
			return v, nil
		}
		s.lookup = func(context.Context, sc.WriteCommandLookupRequest) (sc.WriteCommandObservation, error) {
			if s.lookups.Load() == 1 {
				return sc.WriteCommandObservation{}, nil
			}
			v := sc.MutationResult{Metadata: sc.Metadata{CredentialRef: credentialTestRef(), Purpose: sc.Model, Version: 2}}
			return sc.WriteCommandObservation{Observed: true, Result: &v}, nil
		}
		w := summaryWriter()
		h.ServeHTTP(w, credentialTestRequest("PUT", credentialTestDetail, `{"expected_version":"1","value":"private"}`))
		if w.Code != 503 || s.lookups.Load() != 1 || s.writes.Load() != 0 || strings.Contains(w.Body.String(), `"credential_id"`) {
			t.Fatal("damaged metadata admitted historical write")
		}
	}
}

// Run as its own explicitly selected group: the natural product budgets make
// this test about 32s. It starts no listener and creates no detached work.
func TestProjectCredentialHTTPPureNaturalBudgets(t *testing.T) {
	for _, tc := range []struct {
		method, path, body string
		budget             time.Duration
	}{{"POST", credentialTestCollection, `{"value":"private"}`, projectCredentialWriteBudget}, {"POST", credentialTestLookup, `{"kind":"create"}`, projectCredentialReadBudget}} {
		t.Run(tc.budget.String(), func(t *testing.T) {
			h, b, s := credentialTestHandler(t)
			var seen context.Context
			b.auth = func(r *http.Request) (id.Actor, error) { seen = r.Context(); return b.actor, nil }
			if tc.budget == projectCredentialWriteBudget {
				s.execute = func(ctx context.Context, _ sc.WriteRequest) (sc.MutationResult, error) {
					<-ctx.Done()
					return sc.MutationResult{}, ctx.Err()
				}
			} else {
				s.lookup = func(ctx context.Context, _ sc.WriteCommandLookupRequest) (sc.WriteCommandObservation, error) {
					<-ctx.Done()
					return sc.WriteCommandObservation{}, ctx.Err()
				}
			}
			w := summaryWriter()
			body := &summaryHTTPBody{Reader: strings.NewReader(tc.body)}
			r := credentialTestRequest(tc.method, tc.path, tc.body)
			r.Body = body
			start := time.Now()
			if !summaryAborts(func() { h.ServeHTTP(w, r) }) || seen == nil || seen.Err() == nil || time.Since(start) < tc.budget || time.Since(start) > tc.budget+time.Second || w.Body.Len() != 0 || body.closes.Load() != 1 {
				t.Fatal("natural preauthorization budget/tail not honored")
			}
		})
	}
}

func TestProjectCredentialHTTPPureTrackedProblemAndCommitted(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(strconv.FormatBool(committed), func(t *testing.T) {
			h, b, s := credentialTestHandler(t)
			w := summaryWriter()
			var logs bytes.Buffer
			body := &summaryHTTPBody{Reader: strings.NewReader("")}
			r := credentialTestRequest("GET", credentialTestDetail, "")
			r.Body = body
			b.check = func(w http.ResponseWriter, r *http.Request) error {
				if committed {
					w.WriteHeader(http.StatusAccepted)
					_, _ = w.Write([]byte("already-owned"))
				}
				return f.NewFault(f.Forbidden, f.NotStarted)
			}
			aborted := summaryAborts(func() { httpapi.Handler(slog.New(slog.NewJSONHandler(&logs, nil)), h).ServeHTTP(w, r) })
			if s.reads.Load()+s.lookups.Load()+s.writes.Load() != 0 || body.closes.Load() != 1 {
				t.Fatal("boundary failure lost ownership")
			}
			if committed {
				if !aborted || w.Code != 202 || w.Body.String() != "already-owned" {
					t.Fatal("tracked committed response was written twice")
				}
			} else if aborted || w.Code != 403 || !strings.Contains(logs.String(), `"code":"FORBIDDEN"`) || !strings.Contains(logs.String(), `"status":403`) {
				t.Fatal("controller adapter hid Problem tracking")
			}
		})
	}
}
