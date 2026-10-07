package app

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
)

func TestModelRootRouteOwnershipAndRequestPreservation(t *testing.T) {
	roots := []string{"model-providers", "models", "model-selection", "model-commands", "model-credentials", "model-credential-commands"}
	type routeCase struct {
		path, raw string
		model     bool
	}
	var cases []routeCase
	for _, root := range roots {
		path := "/api/v1/system/" + root
		for _, suffix := range []string{"", "/", "/lookup", "/../session", "//opaque"} {
			cases = append(cases, routeCase{path: path + suffix, model: true})
		}
		cases = append(cases, routeCase{path: path + "-other"}, routeCase{path: path + "x"})
	}
	cases = append(cases, routeCase{path: "/api/v1/session"}, routeCase{path: "/diagnostics"}, routeCase{path: "/api/v1/system/model"}, routeCase{path: "/api/v1/system/models/opaque", raw: "/api/v1/system/models%2fopaque", model: true})
	cases = append(cases,
		routeCase{path: "/api/v1/system/model-selection/meeting-summary", model: true},
		routeCase{path: "/api/v1/system/model-selection/meeting-summary/", model: true},
		routeCase{path: "/api/v1/system/model-selection/meeting-summary-other", model: true},
		routeCase{path: "/api/v1/system/model-selection-other/meeting-summary"},
	)
	for _, tc := range cases {
		t.Run(tc.path+tc.raw, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPatch, "http://localhost:8080"+tc.path+"?private=query", strings.NewReader("body-sentinel"))
			r.URL.RawPath = tc.raw
			r.Header.Set("Origin", "http://localhost:8080")
			originalURL, originalBody, originalURI := r.URL, r.Body, r.RequestURI
			calls := 0
			child := func(isModel bool) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, got *http.Request) {
					calls++
					if isModel != tc.model || got.URL != originalURL || got.Body != originalBody || got.Method != http.MethodPatch || got.RequestURI != originalURI || got.URL.Path != tc.path || got.URL.RawPath != tc.raw || got.Host != "localhost:8080" || got.Header.Get("Origin") != "http://localhost:8080" {
						t.Error("composition changed request or selected wrong child")
					}
					data, err := io.ReadAll(got.Body)
					if err != nil || string(data) != "body-sentinel" || httpapi.RequestID(got.Context()).Validate() != nil {
						t.Error("body consumed or missing outer request ID")
					}
					w.WriteHeader(http.StatusNoContent)
				})
			}
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			w := httptest.NewRecorder()
			httpapi.Handler(logger, systemModelRoutes(child(false), child(true))).ServeHTTP(w, r)
			if calls != 1 || w.Code != http.StatusNoContent || w.Header().Get("Location") != "" || w.Header().Get("X-Request-ID") == "" || strings.Count(logs.String(), `"event":"http_request"`) != 1 {
				t.Fatal("route redirected, duplicated dispatch or middleware")
			}
			if strings.Contains(logs.String(), "body-sentinel") || strings.Contains(logs.String(), "private=query") {
				t.Fatal("ordinary log exposed request material")
			}
		})
	}
}

// The embedded pointer is deliberately nil: any database method called during
// these real constructors panics instead of silently supplying a SQL result.
type modelRootConstructionStore struct{ *postgres.Store }

func TestModelRootRequiredConstructionDependencies(t *testing.T) {
	cfg := testConfig(t, "1s")
	store := &modelRootConstructionStore{}
	accounts, err := account.NewAuthority(store, cfg.AccountKeyring())
	if err != nil {
		t.Fatal(err)
	}
	models, err := model.NewAuthority(store, model.Authorizations{Sessions: accounts, System: accounts})
	if err != nil {
		t.Fatal(err)
	}
	usage, err := model.NewSecretUsageRouter(models, accounts)
	if err != nil {
		t.Fatal(err)
	}
	projectUsage, err := createProjectUsage(cfg, store, accounts)
	if err != nil {
		t.Fatal(err)
	}
	projects := projectUsage.projects
	auditor, err := createSecurity(cfg, store, accounts, models, projects)
	if err != nil || auditor == nil {
		t.Fatal("valid Audit construction", err)
	}
	secrets, err := createSecret(cfg, store, auditor, accounts, usage, projects)
	if err != nil || secrets == nil || secrets.Status().Available {
		t.Fatal("valid Secret construction or premature initialized state", err)
	}
	for _, tc := range []struct {
		name     string
		db       database
		accounts *account.Authority
		models   *model.Authority
		projects *project.Authority
	}{
		{"missing-store", nil, accounts, models, projects},
		{"wrong-store", &unitDatabase{}, accounts, models, projects},
		{"missing-account", store, nil, models, projects},
		{"missing-model", store, accounts, nil, projects},
		{"missing-project", store, accounts, models, nil},
	} {
		t.Run("audit/"+tc.name, func(t *testing.T) {
			if got, err := createSecurity(cfg, tc.db, tc.accounts, tc.models, tc.projects); err == nil || got != nil {
				t.Fatal("missing graph dependency produced Audit service")
			}
		})
	}
	for _, tc := range []struct {
		name     string
		db       database
		audit    *audit.Service
		accounts *account.Authority
		usage    *model.SecretUsageRouter
	}{
		{"missing-store", nil, auditor, accounts, usage},
		{"wrong-store", &unitDatabase{}, auditor, accounts, usage},
		{"missing-account", store, auditor, nil, usage},
		{"missing-audit", store, nil, accounts, usage},
		{"missing-router", store, auditor, accounts, nil},
	} {
		t.Run("secret/"+tc.name, func(t *testing.T) {
			if got, err := createSecret(cfg, tc.db, tc.audit, tc.accounts, tc.usage, projects); err == nil || got != nil {
				t.Fatal("missing graph dependency produced Secret service")
			}
		})
	}
	for _, missing := range []*project.Authority{nil, {}} {
		if got, err := createSecret(cfg, store, auditor, accounts, usage, missing); err == nil || got != nil {
			t.Fatal("missing Project Secret authority accepted")
		}
	}
	owned := &resources{}
	if err := bindAccounts(context.Background(), cfg, &unitDatabase{}, owned, &dependencies{}); err == nil || owned.accounts() != nil {
		t.Fatal("invalid store published a partial handler or owner")
	}
}
