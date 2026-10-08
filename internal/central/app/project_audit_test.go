package app

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
)

func TestProjectAuditRootPureCompositionAndRoutes(t *testing.T) {
	if h, e := projectAuditHandler(nil, nil, "http://localhost:8080"); h != nil || e == nil {
		t.Fatal("unbound auditor/account")
	}
	const p = "/api/v1/projects/01900000-0000-7000-8000-000000000001"
	for _, tc := range []struct{ path, owner string }{{p + "/audit", "project-audit"}, {p + "/audit/", "project-audit"}, {p + "/audit/bad/extra", "project-audit"}, {"/api/v1/projects//audit", "project-audit"}, {"/api/v1/projects/bad/audit", "project-audit"}, {p + "/audit-other", "account"}, {p + "/model-providers", "models"}, {p + "/models", "models"}, {p + "/model-commands/lookup", "models"}, {p + "/model-credentials", "credentials"}, {p + "/model-credential-commands/lookup", "credentials"}, {p, "read"}, {"/api/v1/projects", "read"}, {p + "/commands/lookup", "update"}, {p + "/model-usage", "usage"}, {"/api/v1/projects/resolve", "usage"}, {"/api/v1/system/audit", "system-audit"}, {"/api/v1/system/model-selection/meeting-summary", "system-model"}, {"/api/v1/session", "account"}, {"/readyz", "account"}} {
		t.Run(tc.path, func(t *testing.T) {
			r := httptest.NewRequest("GET", tc.path+"?private-canary=1", strings.NewReader("body-canary"))
			url, body, uri := r.URL, r.Body, r.RequestURI
			calls := 0
			child := func(owner string) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, got *http.Request) {
					calls++
					if owner != tc.owner || got.URL != url || got.Body != body || got.RequestURI != uri || httpapi.RequestID(got.Context()).Validate() != nil {
						t.Fatal("root dispatch ownership", owner, tc.owner)
					}
					data, e := io.ReadAll(got.Body)
					if e != nil || string(data) != "body-canary" {
						t.Fatal("body changed")
					}
					w.WriteHeader(204)
				})
			}
			h := projectAuditRoutes(projectCredentialsRoutes(projectModelsRoutes(projectUpdateRoutes(projectReadRoutes(projectUsageRoutes(systemAuditRoutes(systemModelRoutes(child("account"), child("system-model")), child("system-audit")), child("usage")), child("read")), child("update")), child("models")), child("credentials")), child("project-audit"))
			var logs bytes.Buffer
			w := httptest.NewRecorder()
			httpapi.Handler(slog.New(slog.NewJSONHandler(&logs, nil)), h).ServeHTTP(w, r)
			if calls != 1 || w.Code != 204 || w.Header().Get("Location") != "" || len(w.Header().Values("X-Request-ID")) != 1 || strings.Count(logs.String(), `"event":"http_request"`) != 1 || strings.Contains(logs.String(), "canary") {
				t.Fatal("middleware duplicated/reflected request")
			}
		})
	}
	file, e := parser.ParseFile(token.NewFileSet(), "account.go", nil, 0)
	if e != nil {
		t.Fatal(e)
	}
	bindings := 0
	is := func(x ast.Expr, n string) bool { id, ok := x.(*ast.Ident); return ok && id.Name == n }
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if is(call.Fun, "projectAuditHandler") {
			bindings++
			if len(call.Args) != 3 || !is(call.Args[0], "auditor") || !is(call.Args[1], "core") {
				t.Fatal("different root auditor/Account")
			}
		}
		return true
	})
	if bindings != 1 {
		t.Fatal("duplicate/missing read binding")
	}
	// Existing same-Store/authority/producer graph remains independently checked
	// by TestAuditRootPureSameInstanceConstructionGraph and the real root fixture.
}
