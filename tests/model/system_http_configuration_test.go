//go:build integration

package model_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestModelSystemHTTPConfigurationCRUD(t *testing.T) {
	v := newSystemHTTPFixture(t)
	for _, tc := range []struct {
		protocol mc.Protocol
		kind     mc.ModelType
	}{{mc.OpenAIChat, mc.ChatModel}, {mc.AnthropicMessages, mc.ChatModel}, {mc.OpenAIEmbeddings, mc.EmbeddingModel}, {mc.JinaRerank, mc.RerankerModel}, {mc.OpenAIImages, mc.ImageModel}} {
		t.Run(string(tc.protocol), func(t *testing.T) {
			body := httpProviderBody(tc.protocol)
			key := newID[struct{}](t).String()
			created := v.request(t, v.adminBrowser, "POST", "/api/v1/system/model-providers", key, body).want(t, 200)
			before := v.facts(t)
			replay := v.request(t, v.adminBrowser, "POST", "/api/v1/system/model-providers", key, body).want(t, 200)
			httpReceiptSame(t, created, replay)
			httpSameFacts(t, before, v.facts(t))
			provider := created.object(t)["resource_id"].(string)
			body["input"].(map[string]any)["name"] = "changed"
			v.request(t, v.adminBrowser, "POST", "/api/v1/system/model-providers", key, body).problem(t, 409, f.IdempotencyKeyReused)
			v.request(t, v.adminBrowser, "PUT", "/api/v1/system/model-providers/"+provider, newID[struct{}](t).String(), map[string]any{"expected_version": "1", "input": body["input"]}).want(t, 200)
			view := v.request(t, v.adminBrowser, "GET", "/api/v1/system/model-providers/"+provider, "", nil).want(t, 200).object(t)
			if view["version"] != "2" || view["scope"] != nil {
				t.Fatal("provider projection")
			}
			model := v.model(t, provider, tc.kind)
			v.request(t, v.adminBrowser, "DELETE", "/api/v1/system/model-providers/"+provider, newID[struct{}](t).String(), map[string]any{"expected_version": "2"}).problem(t, 409, f.InvalidState)
			updated := httpModelBody(provider, tc.kind)
			updated["input"].(map[string]any)["name"] = "updated"
			v.request(t, v.adminBrowser, "PUT", "/api/v1/system/models/"+model, newID[struct{}](t).String(), map[string]any{"expected_version": "1", "input": updated["input"]}).want(t, 200)
			get := v.request(t, v.adminBrowser, "GET", "/api/v1/system/models/"+model, "", nil).want(t, 200).object(t)
			if get["version"] != "2" || get["scope"] != nil {
				t.Fatal("model projection")
			}
			page := v.request(t, v.adminBrowser, "GET", "/api/v1/system/models?provider_id="+provider, "", nil).want(t, 200).object(t)
			if len(page["items"].([]any)) != 1 || page["next_cursor"] != nil {
				t.Fatal("model list")
			}
			v.request(t, v.adminBrowser, "DELETE", "/api/v1/system/models/"+model, newID[struct{}](t).String(), map[string]any{"expected_version": "2", "replacement": nil}).want(t, 200)
			v.request(t, v.adminBrowser, "DELETE", "/api/v1/system/model-providers/"+provider, newID[struct{}](t).String(), map[string]any{"expected_version": "2"}).want(t, 200)
			v.request(t, v.adminBrowser, "GET", "/api/v1/system/models/"+model, "", nil).problem(t, 404, f.NotFound)
		})
	}
}

func TestModelSystemHTTPSelectionAndReferenceDeletion(t *testing.T) {
	v := newSystemHTTPFixture(t)
	selection := v.request(t, v.adminBrowser, "GET", "/api/v1/system/model-selection", "", nil).want(t, 200).object(t)
	if selection["configured"] != nil || selection["version"] != "1" {
		t.Fatal("invented default")
	}
	embed := v.model(t, v.provider(t, mc.OpenAIEmbeddings), mc.EmbeddingModel)
	chatProvider := v.provider(t, mc.OpenAIChat)
	chatBody := httpModelBody(chatProvider, mc.ChatModel)
	chatBody["input"].(map[string]any)["capabilities"].(map[string]any)["structured_output_modes"] = []string{"text", "json_schema"}
	memory := v.request(t, v.adminBrowser, "POST", "/api/v1/system/models", newID[struct{}](t).String(), chatBody).want(t, 200).object(t)["resource_id"].(string)
	rerank := v.model(t, v.provider(t, mc.JinaRerank), mc.RerankerModel)
	image := v.model(t, v.provider(t, mc.OpenAIImages), mc.ImageModel)
	change := map[string]any{"id": selection["id"], "expected_version": "1", "embedding": embed, "memory": memory, "reranker": rerank, "image": image}
	v.request(t, v.adminBrowser, "PUT", "/api/v1/system/model-selection", newID[struct{}](t).String(), change).want(t, 200)
	before := v.facts(t)
	v.request(t, v.adminBrowser, "PUT", "/api/v1/system/model-selection", newID[struct{}](t).String(), change).problem(t, 409, f.VersionConflict)
	httpSameFacts(t, before, v.facts(t))
	change["expected_version"] = "2"
	change["embedding"] = nil
	v.request(t, v.adminBrowser, "PUT", "/api/v1/system/model-selection", newID[struct{}](t).String(), change).problem(t, 400, f.InvalidArgument)
	httpSameFacts(t, before, v.facts(t))
	change["embedding"] = embed
	change["reranker"], change["image"] = nil, nil
	v.request(t, v.adminBrowser, "PUT", "/api/v1/system/model-selection", newID[struct{}](t).String(), change).want(t, 200)
	newEmbedding := v.model(t, v.provider(t, mc.OpenAIEmbeddings), mc.EmbeddingModel)
	v.request(t, v.adminBrowser, "DELETE", "/api/v1/system/models/"+embed, newID[struct{}](t).String(), map[string]any{"expected_version": "1", "replacement": nil}).problem(t, 409, f.InvalidState)
	deleted := v.request(t, v.adminBrowser, "DELETE", "/api/v1/system/models/"+embed, newID[struct{}](t).String(), map[string]any{"expected_version": "1", "replacement": newEmbedding}).want(t, 200).object(t)
	if deleted["affected_references"] != "1" {
		t.Fatal("missing replacement receipt")
	}
	view := v.request(t, v.adminBrowser, "GET", "/api/v1/system/model-selection", "", nil).want(t, 200).object(t)
	if view["configured"].(map[string]any)["embedding"] != newEmbedding {
		t.Fatal("selector replacement not atomic")
	}
	owner := newID[struct{}](t)
	project := newID[id.Project](t)
	if _, err := v.raw.Exec(testContext(t), `INSERT INTO agenteam_model.references(owner_kind,owner_id,role,project_id,model_id,owner_version) VALUES('agent',$1,'agent_model',$2,$3,1)`, owner.String(), project.String(), memory); err != nil {
		t.Fatal(err)
	}
	before = v.facts(t)
	v.request(t, v.adminBrowser, "DELETE", "/api/v1/system/models/"+memory, newID[struct{}](t).String(), map[string]any{"expected_version": "1", "replacement": memory}).problem(t, 400, f.InvalidArgument)
	replacement := v.request(t, v.adminBrowser, "POST", "/api/v1/system/models", newID[struct{}](t).String(), chatBody).want(t, 200).object(t)["resource_id"].(string)
	before = v.facts(t)
	v.request(t, v.adminBrowser, "DELETE", "/api/v1/system/models/"+memory, newID[struct{}](t).String(), map[string]any{"expected_version": "1", "replacement": replacement}).problem(t, 503, f.DependencyUnbound)
	httpSameFacts(t, before, v.facts(t))
}

func TestModelSystemHTTPCurrentAdministrator(t *testing.T) {
	v := newSystemHTTPFixture(t)
	regular := v.addBrowser(t, "user")
	other := v.addBrowser(t, "admin")
	v.request(t, regular, "GET", "/api/v1/system/model-providers", "", nil).problem(t, 403, f.Forbidden)
	v.request(t, systemHTTPBrowser{cookie: "fake-agent-cookie", csrf: "fake-actor-csrf"}, "GET", "/api/v1/system/model-providers", "", nil).problem(t, 401, f.Unauthenticated)
	key := newID[struct{}](t).String()
	v.request(t, v.adminBrowser, "POST", "/api/v1/system/model-providers", key, httpProviderBody(mc.OpenAIChat)).want(t, 200)
	if out := v.request(t, other, "POST", "/api/v1/system/model-commands/lookup", key, map[string]any{"command": "provider.create"}).want(t, 200).object(t); out["found"] != false || out["receipt"] != nil {
		t.Fatal("another admin claimed receipt")
	}
	for _, mode := range []string{"role", "session"} {
		t.Run(mode, func(t *testing.T) {
			browser := v.addBrowser(t, "admin")
			var fired atomic.Bool
			v.tracked.setHooks(func(_ context.Context, cause f.TransactionCause) error {
				if cause.Details().Primary.Namespace() == "model.system" && fired.CompareAndSwap(false, true) {
					if mode == "role" {
						v.role(t, browser, "user")
					} else {
						if err := v.account.Logout(testContext(t), account.LogoutRequest{Actor: browser.actor, Key: f.IdempotencyKey(newID[struct{}](t).String())}); err != nil {
							t.Fatal(err)
						}
					}
				}
				return nil
			}, nil)
			response := v.request(t, browser, "POST", "/api/v1/system/model-providers", newID[struct{}](t).String(), httpProviderBody(mc.OpenAIChat))
			v.tracked.setHooks(nil, nil)
			if !fired.Load() {
				t.Fatal("entry-to-business barrier missed")
			}
			if mode == "role" {
				response.problem(t, 403, f.Forbidden)
			} else {
				response.problem(t, 401, f.SessionRevoked)
			}
			v.request(t, browser, "POST", "/api/v1/system/model-commands/lookup", key, map[string]any{"command": "provider.create"}).want(t, response.status)
			v.request(t, browser, "GET", "/api/v1/system/model-providers?limit=1", "", nil).want(t, response.status)
		})
	}
}

func TestModelSystemHTTPBoundaryAndStrictWire(t *testing.T) {
	v := newSystemHTTPFixture(t)
	path := "/api/v1/system/model-commands/lookup"
	body := []byte(`{"command":"provider.create"}`)
	for _, tc := range []struct {
		name   string
		change func(*http.Request)
		status int
		code   f.Code
	}{
		{"host", func(r *http.Request) {
			r.Host = "evil.example"
			r.Header.Set("X-Forwarded-Host", "system-http.example.test")
		}, 403, f.OriginDenied},
		{"origin", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, 403, f.OriginDenied},
		{"duplicate-origin", func(r *http.Request) { r.Header.Add("Origin", systemHTTPOrigin) }, 403, f.OriginDenied},
		{"csrf", func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, 403, f.CSRFFailed},
		{"duplicate-csrf", func(r *http.Request) { r.Header.Add("X-CSRF-Token", v.adminBrowser.csrf) }, 403, f.CSRFFailed},
		{"duplicate-cookie", func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: "__Host-agenteam_session", Value: v.adminBrowser.cookie})
		}, 401, f.Unauthenticated},
		{"duplicate-key", func(r *http.Request) { r.Header.Add("Idempotency-Key", "another") }, 400, f.InvalidArgument},
		{"fetch", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, 403, f.OriginDenied},
		{"raw-path", func(r *http.Request) { r.URL.RawPath = "/api/%76%31/system/model-commands/lookup" }, 400, f.InvalidArgument},
		{"unclean", func(r *http.Request) { r.URL.Path = "/api/v1/../secret" }, 400, f.InvalidArgument},
		{"query", func(r *http.Request) { r.URL.ForceQuery = true }, 400, f.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := v.rawRequest(testContext(t), v.adminBrowser, "POST", path, "key", body, tc.change)
			response.problem(t, tc.status, tc.code)
			if response.headers.Get("Location") != "" {
				t.Fatal("redirect before security")
			}
			if (len(response.headers.Values("Set-Cookie")) > 0) != (tc.status == 401) {
				t.Fatal("wrong cookie clearing")
			}
		})
	}
	provider, _ := json.Marshal(httpProviderBody(mc.OpenAIChat))
	for _, tc := range []struct {
		name   string
		raw    []byte
		change func(*http.Request)
		status int
		code   f.Code
	}{
		{"nested-duplicate", bytes.Replace(provider, []byte(`"enabled":true`), []byte(`"enabled":true,"enabled":false`), 1), nil, 400, f.InvalidArgument},
		{"unknown", bytes.Replace(provider, []byte(`"enabled":true`), []byte(`"enabled":true,"actor":"fake"`), 1), nil, 400, f.InvalidArgument},
		{"null", []byte(`{"input":null}`), nil, 400, f.InvalidArgument},
		{"trailing", append(bytes.Clone(provider), []byte(` {}`)...), nil, 400, f.InvalidArgument},
		{"utf8", []byte{'{', '"', 'x', '"', ':', '"', 255, '"', '}'}, nil, 400, f.InvalidArgument},
		{"media", provider, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 415, f.UnsupportedMediaType},
		{"encoding", provider, func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") }, 415, f.UnsupportedMediaType},
		{"large", []byte(`{"input":"` + strings.Repeat("x", 1<<20) + `"}`), nil, 413, f.PayloadTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v.rawRequest(testContext(t), v.adminBrowser, "POST", "/api/v1/system/model-providers", "key", tc.raw, tc.change).problem(t, tc.status, tc.code)
		})
	}
	response := v.request(t, v.adminBrowser, "GET", "/private-input-sentinel?value=private-input-sentinel", "", nil)
	response.problem(t, 404, f.NotFound)
	if bytes.Contains(response.body, []byte("private-input-sentinel")) {
		t.Fatal("unknown path reflected")
	}
	response = v.request(t, v.adminBrowser, "PATCH", "/api/v1/system/model-providers", "", nil)
	response.problem(t, 405, f.MethodNotAllowed)
	if response.headers.Get("Allow") != "GET, HEAD, POST" {
		t.Fatal("wrong Allow")
	}
	response = v.request(t, v.adminBrowser, "HEAD", "/api/v1/system/model-providers", "", nil).want(t, 200)
	if len(response.body) != 0 {
		t.Fatal("HEAD body")
	}
	logs := v.log.text()
	if strings.Contains(logs, v.adminBrowser.cookie) || strings.Contains(logs, v.adminBrowser.csrf) || strings.Contains(logs, "private-input-sentinel") {
		t.Fatal("sensitive HTTP logs")
	}
	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		if strings.Count(line, `"event":"http_request"`) > 1 {
			t.Fatal("duplicate middleware event")
		}
	}
}

func TestModelSystemHTTPPaginationAndSafeProjection(t *testing.T) {
	v := newSystemHTTPFixture(t)
	p1 := v.provider(t, mc.OpenAIChat)
	p2 := v.provider(t, mc.OpenAIChat)
	v.provider(t, mc.OpenAIChat)
	first := v.request(t, v.adminBrowser, "GET", "/api/v1/system/model-providers?limit=1", "", nil).want(t, 200).object(t)
	cursor, ok := first["next_cursor"].(string)
	if !ok {
		t.Fatal("missing signed cursor")
	}
	v.request(t, v.adminBrowser, "GET", "/api/v1/system/model-providers?limit=100&cursor="+url.QueryEscape(cursor), "", nil).want(t, 200)
	other := v.addBrowser(t, "admin")
	v.request(t, other, "GET", "/api/v1/system/model-providers?limit=1&cursor="+url.QueryEscape(cursor), "", nil).problem(t, 400, f.CursorInvalid)
	for _, q := range []string{"limit=0", "limit=101", "limit=01", "limit=1&limit=2", "cursor=", "bogus=1"} {
		v.request(t, v.adminBrowser, "GET", "/api/v1/system/model-providers?"+q, "", nil).problem(t, 400, f.InvalidArgument)
	}
	page := v.request(t, v.adminBrowser, "GET", "/api/v1/system/model-providers", "", nil).want(t, 200).object(t)
	if len(page["items"].([]any)) != 3 {
		t.Fatal("default page")
	}
	v.model(t, p1, mc.ChatModel)
	v.model(t, p1, mc.ChatModel)
	v.model(t, p2, mc.ChatModel)
	first = v.request(t, v.adminBrowser, "GET", "/api/v1/system/models?provider_id="+p1+"&limit=1", "", nil).want(t, 200).object(t)
	cursor = first["next_cursor"].(string)
	v.request(t, v.adminBrowser, "GET", "/api/v1/system/models?provider_id="+p2+"&cursor="+url.QueryEscape(cursor), "", nil).problem(t, 400, f.CursorInvalid)
	v.request(t, v.adminBrowser, "GET", "/api/v1/system/models", "", nil).problem(t, 400, f.InvalidArgument)
	if _, err := v.raw.Exec(testContext(t), `UPDATE agenteam_model.providers SET version=9007199254740993 WHERE id=$1`, p1); err != nil {
		t.Fatal(err)
	}
	view := v.request(t, v.adminBrowser, "GET", "/api/v1/system/model-providers/"+p1, "", nil).want(t, 200).object(t)
	if view["version"] != "9007199254740993" || len(view) != 5 {
		t.Fatal("projection precision/scope")
	}
	project := newID[id.Project](t)
	foreign := newID[mc.Provider](t)
	if _, err := v.raw.Exec(testContext(t), `INSERT INTO agenteam_model.providers(id,scope,project_id,name,protocol,base_url,provider_options,enabled,version,created_at,updated_at) SELECT $1,'project',$2,name,protocol,base_url,provider_options,enabled,1,created_at,updated_at FROM agenteam_model.providers WHERE id=$3`, foreign.String(), project.String(), p2); err != nil {
		t.Fatal(err)
	}
	v.request(t, v.adminBrowser, "GET", "/api/v1/system/model-providers/"+foreign.String(), "", nil).problem(t, 404, f.NotFound)
	v.request(t, v.adminBrowser, "GET", "/api/v1/system/models?provider_id="+foreign.String(), "", nil).problem(t, 404, f.NotFound)
}

func TestModelSystemHTTPAtomicEffects(t *testing.T) {
	v := newSystemHTTPFixture(t)
	credential := v.credential(t, newID[struct{}](t).String(), "separate-credential-sentinel")
	for _, kind := range []string{"model-audit", "model-event", "secret-audit"} {
		t.Run(kind, func(t *testing.T) {
			before := v.facts(t)
			if kind == "model-event" {
				v.eventFail.fail.Store(true)
			} else {
				v.auditFail.fail.Store(true)
			}
			if kind == "secret-audit" {
				v.request(t, v.adminBrowser, "POST", "/api/v1/system/model-credentials", newID[struct{}](t).String(), map[string]any{"value": "rollback-secret-sentinel"}).problem(t, 503, f.DependencyUnavailable)
			} else {
				body := httpProviderBody(mc.OpenAIChat)
				body["input"].(map[string]any)["credential_ref"] = credential
				v.request(t, v.adminBrowser, "POST", "/api/v1/system/model-providers", newID[struct{}](t).String(), body).problem(t, 503, f.DependencyUnavailable)
			}
			v.auditFail.fail.Store(false)
			v.eventFail.fail.Store(false)
			httpSameFacts(t, before, v.facts(t))
			if kind == "model-event" && v.eventFail.seen.Load() == 0 || kind != "model-event" && v.auditFail.seen.Load() == 0 {
				t.Fatal("real appender never reached")
			}
			t.Log("real Append succeeded inside failing transaction; all canonical/reference/receipt/payload/Audit/Event row hashes unchanged; technical nonce reservation excluded")
		})
	}
}
