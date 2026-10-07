package model

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

const configurationTestProviders = projectHTTPTestBase + "/model-providers"
const configurationTestProvider = configurationTestProviders + "/" + projectHTTPTestProvider
const configurationTestModels = projectHTTPTestBase + "/models"
const configurationTestModel = configurationTestModels + "/" + projectHTTPTestModel
const configurationTestLookup = projectHTTPTestBase + "/model-commands/lookup"
const configurationTestProviderJSON = `{"name":"provider-private","protocol":"openai-chat-completions","base_url":"https://provider.example/v1","enabled":true,"credential_ref":null,"options":{}}`
const configurationTestModelJSON = `{"name":"model-private","provider_model_id":"model-remote-private","type":"chat","enabled":true,"parameters":{},"request_overwrite":{},"header_overwrite":{},"capabilities":{"tool_calls":false,"parallel_tool_calls":false,"streaming":false,"reasoning":false,"input_modalities":["text"],"output_modalities":["text"],"reasoning_efforts":[],"structured_output_modes":[],"context_length":null,"max_output":null}}`

type configurationTestCase struct{ kind, method, path, body string }

func configurationTestCases() []configurationTestCase {
	return []configurationTestCase{
		{"provider.create", "POST", configurationTestProviders, `{"input":` + configurationTestProviderJSON + `}`},
		{"provider.update", "PUT", configurationTestProvider, `{"expected_version":"1","input":` + configurationTestProviderJSON + `}`},
		{"provider.delete", "DELETE", configurationTestProvider, `{"expected_version":"1"}`},
		{"model.create", "POST", configurationTestModels, `{"provider_id":"` + projectHTTPTestProvider + `","input":` + configurationTestModelJSON + `}`},
		{"model.update", "PUT", configurationTestModel, `{"expected_version":"1","input":` + configurationTestModelJSON + `}`},
		{"model.delete", "DELETE", configurationTestModel, `{"expected_version":"1","replacement":null}`},
		{"lookup", "POST", configurationTestLookup, `{"command":"model.delete"}`},
	}
}

type configurationTestService struct {
	execute         func(context.Context, configurationRequest) (mc.CommandReceipt, error)
	lookup          func(context.Context, LookupCommandRequest) (CommandLookup, error)
	writes, lookups atomic.Int32
}

func (s *configurationTestService) run(c context.Context, r configurationRequest) (mc.CommandReceipt, error) {
	s.writes.Add(1)
	return s.execute(c, r)
}
func (s *configurationTestService) CreateProvider(c context.Context, r mc.CreateProviderRequest) (mc.CommandReceipt, error) {
	return s.run(c, configurationRequest{meta: r.CommandMeta, kind: "provider.create", provider: &r.Input})
}
func (s *configurationTestService) UpdateProvider(c context.Context, r mc.UpdateProviderRequest) (mc.CommandReceipt, error) {
	return s.run(c, configurationRequest{meta: r.CommandMeta, kind: "provider.update", providerID: r.ID, expected: r.ExpectedVersion, provider: &r.Input})
}
func (s *configurationTestService) DeleteProvider(c context.Context, r mc.DeleteProviderRequest) (mc.CommandReceipt, error) {
	return s.run(c, configurationRequest{meta: r.CommandMeta, kind: "provider.delete", providerID: r.ID, expected: r.ExpectedVersion})
}
func (s *configurationTestService) CreateModel(c context.Context, r mc.CreateModelRequest) (mc.CommandReceipt, error) {
	return s.run(c, configurationRequest{meta: r.CommandMeta, kind: "model.create", providerID: r.ProviderID, model: &r.Input})
}
func (s *configurationTestService) UpdateModel(c context.Context, r mc.UpdateModelRequest) (mc.CommandReceipt, error) {
	return s.run(c, configurationRequest{meta: r.CommandMeta, kind: "model.update", modelID: r.ID, expected: r.ExpectedVersion, model: &r.Input})
}
func (s *configurationTestService) DeleteModel(c context.Context, r mc.DeleteModelRequest) (mc.CommandReceipt, error) {
	return s.run(c, configurationRequest{meta: r.CommandMeta, kind: "model.delete", modelID: r.ID, expected: r.ExpectedVersion, replacement: r.Replacement})
}
func (s *configurationTestService) LookupCommand(c context.Context, r LookupCommandRequest) (CommandLookup, error) {
	s.lookups.Add(1)
	return s.lookup(c, r)
}
func configurationTestReceipt(r configurationRequest) mc.CommandReceipt {
	target := projectHTTPTestModel
	if strings.HasPrefix(r.kind, "provider.") {
		target = projectHTTPTestProvider
	}
	return mc.CommandReceipt{Kind: r.kind, ResourceID: target, Version: r.expected + 1}
}
func configurationTestHandler(t *testing.T) (*configurationHTTP, *projectHTTPTestBoundary, *configurationTestService) {
	t.Helper()
	b := &projectHTTPTestBoundary{actor: testActor(t)}
	s := &configurationTestService{}
	s.execute = func(_ context.Context, r configurationRequest) (mc.CommandReceipt, error) {
		return configurationTestReceipt(r), nil
	}
	s.lookup = func(context.Context, LookupCommandRequest) (CommandLookup, error) { return CommandLookup{}, nil }
	return &configurationHTTP{s, b}, b, s
}
func configurationTestRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, "https://configuration.example"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "original-key")
	return r
}

func TestProjectModelConfigurationHTTPPureInputAndDispatch(t *testing.T) {
	t.Run("constructor", func(t *testing.T) {
		store := &noIOStore{}
		auth := pureAuthority(t, store)
		core, e := New(store, auth, testDependencies(t))
		if e != nil {
			t.Fatal(e)
		}
		for _, v := range []*Service{nil, {}, core} {
			if h, e := NewProjectConfigurationHTTPHandler(v, &account.HTTPBoundary{}); e == nil || h != nil {
				t.Fatal("unbound construction")
			}
		}
		auth.state().auth.Projects = make(projectGrantChannel)
		if h, e := NewProjectConfigurationHTTPHandler(core, &account.HTTPBoundary{}); e != nil || h == nil {
			t.Fatal("pure bound constructor", e)
		}
		if h, e := NewProjectConfigurationHTTPHandler(core, nil); e == nil || h != nil {
			t.Fatal("nil boundary")
		}
	})
	for _, tc := range configurationTestCases() {
		t.Run(tc.kind, func(t *testing.T) {
			h, b, s := configurationTestHandler(t)
			var seen string
			check := func(ctx context.Context, meta mc.CommandMeta, budget time.Duration) {
				d, ok := ctx.Deadline()
				if !ok || time.Until(d) > budget || time.Until(d) < budget-time.Second {
					t.Fatal("wrong published budget")
				}
				if meta.Validate() != nil || !meta.Actor.Equal(b.actor) || meta.Scope.Details().ProjectID != projectHTTPTestProject || meta.Key != "original-key" {
					t.Fatal("identity/scope changed")
				}
			}
			s.execute = func(ctx context.Context, r configurationRequest) (mc.CommandReceipt, error) {
				seen = r.kind
				check(ctx, r.meta, configurationWriteBudget)
				if r.validate() != nil {
					t.Fatal("invalid typed projection")
				}
				return configurationTestReceipt(r), nil
			}
			s.lookup = func(ctx context.Context, r LookupCommandRequest) (CommandLookup, error) {
				seen = "lookup"
				check(ctx, r.Meta, configurationLookupBudget)
				if r.Command != "model.delete" {
					t.Fatal("lookup kind")
				}
				return CommandLookup{}, nil
			}
			w := summaryWriter()
			h.ServeHTTP(w, configurationTestRequest(tc.method, tc.path, tc.body))
			if w.Code != 200 || seen != tc.kind || s.writes.Load()+s.lookups.Load() != 1 || w.flushes != 1 || strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "original-key") {
				t.Fatal("dispatch/output", w.Code, seen)
			}
			w.cleared(t)
		})
	}
	t.Run("project_credential_scope", func(t *testing.T) {
		h, _, s := configurationTestHandler(t)
		s.execute = func(_ context.Context, r configurationRequest) (mc.CommandReceipt, error) {
			if r.provider.CredentialRef == nil || !r.provider.CredentialRef.Details().Scope.Equal(r.meta.Scope) {
				t.Fatal("credential converted to System scope")
			}
			return configurationTestReceipt(r), nil
		}
		body := strings.Replace(configurationTestCases()[0].body, `"credential_ref":null`, `"credential_ref":"`+credentialTestID+`"`, 1)
		w := summaryWriter()
		h.ServeHTTP(w, configurationTestRequest("POST", configurationTestProviders, body))
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
	})
	t.Run("authentication_before_method", func(t *testing.T) {
		h, b, s := configurationTestHandler(t)
		b.check = func(http.ResponseWriter, *http.Request) error { return fault(f.Forbidden) }
		w := summaryWriter()
		h.ServeHTTP(w, configurationTestRequest("PATCH", configurationTestModel, ""))
		if w.Code != 403 || b.auths.Load() != 0 || s.writes.Load()+s.lookups.Load() != 0 {
			t.Fatal("boundary order")
		}
	})
	for _, tc := range []struct{ path, allow string }{{configurationTestModels, "GET, HEAD, POST"}, {configurationTestProvider, "DELETE, GET, HEAD, PUT"}, {configurationTestLookup, "POST"}} {
		t.Run("allow_"+tc.allow, func(t *testing.T) {
			h, _, s := configurationTestHandler(t)
			w := summaryWriter()
			h.ServeHTTP(w, configurationTestRequest("PATCH", tc.path, ""))
			if w.Code != 405 || w.Header().Get("Allow") != tc.allow || s.writes.Load()+s.lookups.Load() != 0 {
				t.Fatal("Allow ownership")
			}
		})
	}
}

func TestProjectModelConfigurationHTTPPureResultBoundary(t *testing.T) {
	for _, tc := range configurationTestCases() {
		t.Run(tc.kind, func(t *testing.T) {
			h, b, s := configurationTestHandler(t)
			cause := errors.New("private-original-cause")
			original := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(cause)
			var got error
			b.problem = func(w http.ResponseWriter, r *http.Request, e error) {
				got = e
				(&account.HTTPBoundary{}).WriteProblem(w, r, e)
			}
			s.execute = func(context.Context, configurationRequest) (mc.CommandReceipt, error) {
				return mc.CommandReceipt{}, original
			}
			s.lookup = func(context.Context, LookupCommandRequest) (CommandLookup, error) {
				return CommandLookup{Found: true}, original
			}
			w := summaryWriter()
			h.ServeHTTP(w, configurationTestRequest(tc.method, tc.path, tc.body))
			if got != original || !errors.Is(got, cause) || w.Code != 503 || s.writes.Load()+s.lookups.Load() != 1 || strings.Contains(w.Body.String(), "private") {
				t.Fatal("original error replaced/retried")
			}
		})
	}
	for _, kind := range []string{"kind", "id", "version", "affected", "overflow"} {
		t.Run("bad_write_"+kind, func(t *testing.T) {
			h, b, s := configurationTestHandler(t)
			var got error
			b.problem = func(w http.ResponseWriter, r *http.Request, e error) {
				got = e
				(&account.HTTPBoundary{}).WriteProblem(w, r, e)
			}
			s.execute = func(_ context.Context, r configurationRequest) (mc.CommandReceipt, error) {
				v := configurationTestReceipt(r)
				switch kind {
				case "kind":
					v.Kind = "provider.delete"
				case "id":
					v.ResourceID = projectHTTPTestProvider
				case "version":
					v.Version = 1
				case "affected":
					v.AffectedReferences = 1
				case "overflow":
					v.Version = 1
				}
				return v, nil
			}
			body := configurationTestCases()[5].body
			if kind == "overflow" {
				body = strings.Replace(body, `"1"`, `"9223372036854775807"`, 1)
			}
			w := summaryWriter()
			h.ServeHTTP(w, configurationTestRequest("DELETE", configurationTestModel, body))
			var ff *f.Fault
			if !errors.As(got, &ff) || ff.Code != f.DependencyUnavailable || ff.CommitState != f.Unknown || ff.CauseID != "" || errors.Unwrap(ff) != nil || w.Code != 503 || s.writes.Load() != 1 || s.lookups.Load() != 0 {
				t.Fatal("bad write receipt classification")
			}
		})
	}
	for _, v := range []CommandLookup{{Found: true}, {Receipt: &mc.CommandReceipt{}}, {Found: true, Receipt: &mc.CommandReceipt{Kind: "provider.create", ResourceID: projectHTTPTestProvider, Version: 1}}} {
		t.Run("bad_lookup", func(t *testing.T) {
			h, b, s := configurationTestHandler(t)
			var got error
			b.problem = func(w http.ResponseWriter, r *http.Request, e error) {
				got = e
				(&account.HTTPBoundary{}).WriteProblem(w, r, e)
			}
			s.lookup = func(context.Context, LookupCommandRequest) (CommandLookup, error) { return v, nil }
			w := summaryWriter()
			h.ServeHTTP(w, configurationTestRequest("POST", configurationTestLookup, `{"command":"model.delete"}`))
			var ff *f.Fault
			if !errors.As(got, &ff) || ff.CommitState != f.NotStarted || ff.Code != f.DependencyUnavailable || s.writes.Load() != 0 || s.lookups.Load() != 1 {
				t.Fatal("lookup unavailable observation")
			}
		})
	}
	for _, committed := range []bool{false, true} {
		t.Run("tracked", func(t *testing.T) {
			h, b, _ := configurationTestHandler(t)
			w := summaryWriter()
			var logs bytes.Buffer
			b.check = func(w http.ResponseWriter, _ *http.Request) error {
				if committed {
					w.WriteHeader(202)
					_, _ = w.Write([]byte("owned"))
				}
				return fault(f.Forbidden)
			}
			aborted := summaryAborts(func() {
				httpapi.Handler(slog.New(slog.NewJSONHandler(&logs, nil)), h).ServeHTTP(w, configurationTestRequest("POST", configurationTestLookup, `{}`))
			})
			if committed {
				if !aborted || w.Code != 202 || w.Body.String() != "owned" {
					t.Fatal("second write")
				}
			} else if aborted || w.Code != 403 || !strings.Contains(logs.String(), `"code":"FORBIDDEN"`) {
				t.Fatal("hidden middleware state")
			}
		})
	}
}

func configurationTestSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("owned stage not entered")
	}
}
func TestProjectModelConfigurationHTTPPureActualTail(t *testing.T) {
	t.Run("capability_preflight", func(t *testing.T) {
		for _, w := range []http.ResponseWriter{httptest.NewRecorder(), &credentialNoFlush{ResponseWriter: httptest.NewRecorder()}} {
			h, b, s := configurationTestHandler(t)
			body := &summaryHTTPBody{Reader: strings.NewReader(`{}`)}
			r := configurationTestRequest("POST", configurationTestLookup, "")
			r.Body = body
			if !summaryAborts(func() { h.ServeHTTP(w, r) }) || b.checks.Load() != 0 || s.lookups.Load() != 0 || body.closes.Load() != 1 {
				t.Fatal("missing capability reached boundary")
			}
		}
	})
	for _, phase := range []string{"start", "abort", "reset", "callback"} {
		for _, setter := range []string{"read", "write"} {
			t.Run(phase+"_"+setter, func(t *testing.T) {
				h, b, s := configurationTestHandler(t)
				w := summaryWriter()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				fired := make(chan struct{})
				var once sync.Once
				var reads, writes atomic.Int32
				hook := func(which string, counter *atomic.Int32) func(time.Time) error {
					return func(at time.Time) error {
						n := counter.Add(1)
						if which == setter && ((phase == "start" && n == 1) || (phase == "abort" && n >= 2) || (phase == "reset" && at.IsZero()) || (phase == "callback" && n >= 2)) {
							once.Do(func() { close(fired) })
							panic("private")
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
					s.lookup = func(context.Context, LookupCommandRequest) (CommandLookup, error) {
						cancel()
						select {
						case <-fired:
						case <-time.After(time.Second):
							return CommandLookup{}, errors.New("callback not entered")
						}
						return CommandLookup{}, context.Canceled
					}
				}
				body := &summaryHTTPBody{Reader: strings.NewReader(`{"command":"model.delete"}`)}
				r := configurationTestRequest("POST", configurationTestLookup, "").WithContext(ctx)
				r.Body = body
				if !summaryAborts(func() { h.ServeHTTP(w, r) }) || body.closes.Load() != 1 || reads.Load() < 2 || writes.Load() < 2 {
					t.Fatal("setter panic escaped ownership")
				}
			})
		}
	}
	for _, phase := range []string{"service", "body_close", "callback"} {
		t.Run("join_"+phase, func(t *testing.T) {
			h, _, s := configurationTestHandler(t)
			w := summaryWriter()
			ctx, cancel := context.WithCancel(context.Background())
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			enter := func() { once.Do(func() { close(entered) }); <-release }
			body := &summaryHTTPBody{Reader: strings.NewReader(`{"command":"model.delete"}`)}
			s.lookup = func(context.Context, LookupCommandRequest) (CommandLookup, error) {
				if phase == "service" {
					enter()
				} else if phase == "callback" {
					cancel()
					select {
					case <-entered:
					case <-ctx.Done(): // Wait for the callback's actual entry with a bounded timer below.
						select {
						case <-entered:
						case <-time.After(time.Second):
							return CommandLookup{}, context.Canceled
						}
					}
				}
				return CommandLookup{}, nil
			}
			if phase == "body_close" {
				body.close = func() error { enter(); return nil }
			}
			if phase == "callback" {
				var calls atomic.Int32
				w.setRead = func(time.Time) error {
					if calls.Add(1) == 2 {
						enter()
					}
					return nil
				}
			}
			r := configurationTestRequest("POST", configurationTestLookup, "").WithContext(ctx)
			r.Body = body
			done, unblock := projectHTTPAsync(t, cancel, release, func() { h.ServeHTTP(w, r) })
			configurationTestSignal(t, entered)
			cancel()
			select {
			case <-done:
				t.Fatal("handler returned before actual tail")
			case <-time.After(15 * time.Millisecond):
			}
			unblock()
			if !summaryJoined(t, done) || body.closes.Load() != 1 {
				t.Fatal("tail did not abort and close")
			}
		})
	}
	for _, mode := range []string{"read_error", "close_error", "close_panic", "partial_write", "write_error", "write_panic", "flush_error", "flush_panic"} {
		t.Run(mode, func(t *testing.T) {
			h, _, s := configurationTestHandler(t)
			w := summaryWriter()
			body := &summaryHTTPBody{Reader: strings.NewReader(`{"command":"model.delete"}`)}
			boom := errors.New("private")
			switch mode {
			case "read_error":
				body.Reader = configurationErrorReader{}
			case "close_error":
				body.close = func() error { return boom }
			case "close_panic":
				body.close = func() error { panic(boom) }
			case "partial_write":
				w.write = func(p []byte) (int, error) { return len(p) - 1, nil }
			case "write_error":
				w.write = func([]byte) (int, error) { return 0, boom }
			case "write_panic":
				w.write = func([]byte) (int, error) { panic(boom) }
			case "flush_error":
				w.flush = func() error { return boom }
			case "flush_panic":
				w.flush = func() error { panic(boom) }
			}
			r := configurationTestRequest("POST", configurationTestLookup, "")
			r.Body = body
			aborted := summaryAborts(func() { h.ServeHTTP(w, r) })
			if body.closes.Load() != 1 {
				t.Fatal("body not closed exactly once")
			}
			if mode == "read_error" {
				if s.lookups.Load() != 0 || w.Code != 400 {
					t.Fatal("read error dispatched")
				}
			} else if !aborted {
				t.Fatal("tail failure did not abort")
			}
		})
	}
}

type configurationErrorReader struct{}

func (configurationErrorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
