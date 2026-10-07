package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

type projectUsageRootStore struct{ *postgres.Store } // Any constructor I/O panics.

func TestProjectUsageRootPureConstructionAndReadOnly(t *testing.T) {
	cfg := testConfig(t, "1s")
	store := &projectUsageRootStore{}
	accounts, err := account.NewAuthority(store, cfg.AccountKeyring())
	if err != nil {
		t.Fatal(err)
	}
	assembly, err := createProjectUsage(cfg, store, accounts)
	if err != nil || assembly.projects == nil || assembly.reader == nil {
		t.Fatal("pure real construction", err)
	}
	// writerReady checks the genuinely absent facts authority before accepting a
	// request. A typed-nil or substitute Runtime must not be silently installed.
	_, err = assembly.reader.DiscoverInvocation(context.Background(), uc.InvocationRequest{})
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.DependencyUnbound {
		t.Fatal("production Invocation writer became bound", err)
	}
	var typedNil *projectUsageRootStore
	for _, db := range []database{nil, typedNil, &unitDatabase{}} {
		if value, e := createProjectUsage(cfg, db, accounts); e == nil || value != nil {
			t.Fatal("invalid store accepted")
		}
	}
	if value, e := createProjectUsage(cfg, store, nil); e == nil || value != nil {
		t.Fatal("missing Session/route authority accepted")
	}
	if value, e := assembly.handler(nil, cfg.PublicOrigin()); e == nil || value != nil {
		t.Fatal("missing formal Account core accepted")
	}
}

func TestProjectUsageRootPureStartupContextAndFailure(t *testing.T) {
	for _, mode := range []string{"normal", "expired", "model_error", "model_cancels", "summary_error", "summary_cancels", "usage_error"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &projectUsageRootStore{}
			cfg := testConfig(t, "1s")
			accounts, e := account.NewAuthority(store, cfg.AccountKeyring())
			if e != nil {
				t.Fatal(e)
			}
			_, e = createProjectUsage(cfg, store, accounts)
			if e != nil {
				t.Fatal(e)
			}
			sentinel := errors.New("schema unavailable")
			if mode == "expired" {
				cancel()
			}
			models, summaryChecks, usageChecks := 0, 0, 0
			err := initializeModelsAndUsage(ctx, func(got context.Context) error {
				models++
				if got != ctx || summaryChecks != 0 || usageChecks != 0 {
					t.Fatal("startup context/order changed")
				}
				if mode == "model_error" {
					return sentinel
				}
				if mode == "model_cancels" {
					cancel()
				}
				return nil
			}, func(got context.Context) error {
				summaryChecks++
				if got != ctx || models != 1 || usageChecks != 0 {
					t.Fatal("Summary startup context/order changed")
				}
				if mode == "summary_error" {
					return sentinel
				}
				if mode == "summary_cancels" {
					cancel()
				}
				return nil
			}, func(got context.Context) error {
				usageChecks++
				if got != ctx || models != 1 || summaryChecks != 1 {
					t.Fatal("Usage schema startup context/order changed")
				}
				if mode == "usage_error" {
					return sentinel
				}
				return nil
			})
			switch mode {
			case "normal":
				if err != nil || usageChecks != 1 {
					t.Fatal("incomplete Usage schema check", err)
				}
			case "usage_error":
				if !errors.Is(err, sentinel) || usageChecks != 1 {
					t.Fatal("Usage failure not propagated", err)
				}
			case "model_error", "summary_error":
				if !errors.Is(err, sentinel) || usageChecks != 0 {
					t.Fatal("checked Usage after Model failure", err)
				}
			default:
				if !errors.Is(err, context.Canceled) || usageChecks != 0 {
					t.Fatal("restarted cancelled startup", err)
				}
			}
			if mode == "expired" && models != 0 || mode != "expired" && models != 1 {
				t.Fatal("Model check repeated/skipped")
			}
			wantSummary := 1
			if mode == "expired" || mode == "model_error" || mode == "model_cancels" {
				wantSummary = 0
			}
			if summaryChecks != wantSummary {
				t.Fatal("Summary repeated/skipped or ran after Model failure")
			}
		})
	}
}

func TestProjectUsageRootPureRoutesPreserveOriginalChain(t *testing.T) {
	const id = "01900000-0000-7000-8000-000000000001"
	for _, tc := range []struct{ path, owner string }{
		{"/api/v1/projects/resolve", "usage"}, {"/api/v1/projects/" + id + "/model-usage", "usage"}, {"/api/v1/projects/" + id + "/model-usage/summary", "usage"},
		{"/api/v1/projects/bad-id/model-usage", "usage"}, {"/api/v1/projects/resolve/", "account"}, {"/api/v1/projects/" + id + "/model-usage/", "account"},
		{"/api/v1/projects//model-usage", "account"}, {"/api/v1/projects/x/y/model-usage", "account"}, {"/api/v1/projects/resolve-other", "account"},
		{"/api/v1/session", "account"}, {"/readyz", "account"}, {"/api/v1/system/models", "model"}, {"/api/v1/system/outbound-policy", "policy"}, {"/api/v1/system/audit", "audit"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			r := httptest.NewRequest("POST", "http://localhost:8080"+tc.path+"?cursor=private-canary", strings.NewReader("owned-body"))
			r.URL.RawPath = tc.path
			r.Header.Set("Origin", "http://localhost:8080")
			url, body, uri := r.URL, r.Body, r.RequestURI
			calls := 0
			child := func(owner string) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, got *http.Request) {
					calls++
					if owner != tc.owner || got.URL != url || got.Body != body || got.RequestURI != uri || got.Method != "POST" || got.Host != r.Host || got.Header.Get("Origin") != r.Header.Get("Origin") {
						t.Fatal("dispatch mutated original request")
					}
					data, err := io.ReadAll(got.Body)
					if err != nil || string(data) != "owned-body" || httpapi.RequestID(got.Context()).Validate() != nil {
						t.Fatal("body/id lost")
					}
					w.WriteHeader(204)
				})
			}
			var logs bytes.Buffer
			recorder := httptest.NewRecorder()
			chain := projectUsageRoutes(systemAuditRoutes(systemOutboundPolicyRoutes(systemModelRoutes(child("account"), child("model")), child("policy")), child("audit")), child("usage"))
			httpapi.Handler(slog.New(slog.NewJSONHandler(&logs, nil)), chain).ServeHTTP(recorder, r)
			if calls != 1 || recorder.Code != 204 || len(recorder.Header().Values("X-Request-ID")) != 1 || recorder.Header().Get("Location") != "" || strings.Count(logs.String(), `"event":"http_request"`) != 1 || strings.Contains(logs.String(), "private-canary") {
				t.Fatal("routing/middleware changed")
			}
		})
	}
}
