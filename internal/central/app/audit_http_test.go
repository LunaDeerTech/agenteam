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

func TestAuditRootPureRoutePreservationAndSingleMiddleware(t *testing.T) {
	const root = "/api/v1/system/audit"
	for _, tc := range []struct{ path, raw, owner string }{
		{root, "", "audit"}, {root + "/", "", "audit"}, {root + "/child", "", "audit"}, {root + "//child", "", "audit"}, {root + "/../session", "", "audit"},
		{root + "/child", root + "%2fchild", "audit"}, {root + "-other", "", "account"}, {root + "x", "", "account"}, {"/api/v1/session", "", "account"},
		{"/api/v1/system/users", "", "account"}, {"/api/v1/system/mail-jobs", "", "account"}, {"/readyz", "", "account"},
		{"/api/v1/system/model-providers", "", "model"}, {"/api/v1/system/models/child", "", "model"}, {"/api/v1/system/model-selection", "", "model"},
		{"/api/v1/system/model-commands/lookup", "", "model"}, {"/api/v1/system/model-credentials/id", "", "model"}, {"/api/v1/system/model-credential-commands/lookup", "", "model"},
		{"/api/v1/system/outbound-policy", "", "policy"}, {"/api/v1/system/outbound-policy/child", "", "policy"},
	} {
		t.Run(tc.path+tc.raw, func(t *testing.T) {
			r := httptest.NewRequest("PATCH", "http://localhost:8080"+tc.path+"?filter=private-canary", strings.NewReader("body-canary"))
			r.URL.RawPath = tc.raw
			r.Header.Set("Origin", "http://localhost:8080")
			r.Header.Set("Cookie", "cookie-canary")
			r.Header.Set("Idempotency-Key", "key-canary")
			url, body, uri := r.URL, r.Body, r.RequestURI
			calls := 0
			child := func(owner string) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, got *http.Request) {
					calls++
					if owner != tc.owner || got.URL != url || got.Body != body || got.RequestURI != uri || got.Method != "PATCH" || got.URL.Path != tc.path || got.URL.RawPath != tc.raw || got.Host != "localhost:8080" || got.Header.Get("Cookie") != "cookie-canary" || got.Header.Get("Idempotency-Key") != "key-canary" || got.Header.Get("Origin") != "http://localhost:8080" {
						t.Fatal("request or route ownership changed")
					}
					b, err := io.ReadAll(got.Body)
					if err != nil || string(b) != "body-canary" || httpapi.RequestID(got.Context()).Validate() != nil {
						t.Fatal("body consumed or missing single outer request identity")
					}
					w.WriteHeader(204)
				})
			}
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			w := httptest.NewRecorder()
			httpapi.Handler(logger, systemAuditRoutes(systemOutboundPolicyRoutes(systemModelRoutes(child("account"), child("model")), child("policy")), child("audit"))).ServeHTTP(w, r)
			if calls != 1 || w.Code != 204 || w.Header().Get("Location") != "" || len(w.Header().Values("X-Request-ID")) != 1 || strings.Count(logs.String(), `"event":"http_request"`) != 1 {
				t.Fatal("duplicated middleware/dispatch or redirect")
			}
			for _, sensitive := range []string{"private-canary", "body-canary", "cookie-canary", "key-canary"} {
				if strings.Contains(logs.String(), sensitive) {
					t.Fatal("ordinary log exposed request material")
				}
			}
		})
	}
}

// The graph test establishes the concrete same-variable constructor binding;
// real producer reachability is checked separately through the actual root.
func TestAuditRootPureSameInstanceConstructionGraph(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "account.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var bind *ast.FuncDecl
	for _, node := range file.Decls {
		if f, ok := node.(*ast.FuncDecl); ok && f.Name.Name == "bindAccounts" {
			bind = f
		}
	}
	if bind == nil {
		t.Fatal("root assembly missing")
	}
	created, bound, accountProducer, modelProducer := 0, 0, 0, 0
	is := func(expr ast.Expr, name string) bool { id, ok := expr.(*ast.Ident); return ok && id.Name == name }
	ast.Inspect(bind, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if is(call.Fun, "createSecurityWithRunners") {
			created++
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if is(selector.X, "audit") && selector.Sel.Name == "New" {
			t.Error("root built a second auditor")
		}
		if is(selector.X, "audithttp") && selector.Sel.Name == "NewSystemHTTPHandler" {
			bound++
			if len(call.Args) != 3 || !is(call.Args[0], "auditor") || !is(call.Args[1], "core") {
				t.Error("read handler uses a different service instance")
			}
		}
		if selector.Sel.Name == "New" && (is(selector.X, "account") || is(selector.X, "model")) {
			for _, arg := range call.Args {
				literal, ok := arg.(*ast.CompositeLit)
				if !ok {
					continue
				}
				for _, element := range literal.Elts {
					kv, ok := element.(*ast.KeyValueExpr)
					if !ok || !is(kv.Key, "Audit") {
						continue
					}
					if !is(kv.Value, "auditor") {
						t.Error("formal producer uses a different auditor")
					}
					if is(selector.X, "account") {
						accountProducer++
					} else {
						modelProducer++
					}
				}
			}
		}
		return true
	})
	if created != 1 || bound != 1 || accountProducer != 1 || modelProducer != 1 {
		t.Fatal("incomplete singleton graph", created, bound, accountProducer, modelProducer)
	}
}
