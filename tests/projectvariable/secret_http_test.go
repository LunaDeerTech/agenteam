//go:build integration

package projectvariable_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

func TestSecretVariableHTTPBoundary(t *testing.T) {
	t.Run("real-owner-protocol-and-safe-history", func(t *testing.T) {
		v := newSecretHTTPFixture(t)
		const canary = "HTTP_SECRET_MATERIAL_CANARY_7f49"
		target := id[i.ProjectVariable](t)
		collection := variableHTTPPath(v.project.ID, "/secret-variables")
		detail, lookupPath := collection+"/"+target.String(), collection+"/commands/lookup"
		body := secretHTTPCreateBody(t, target, canary)
		before := v.secretCounts(t)
		for _, browser := range []variableHTTPBrowser{{}, v.otherBrowser, v.adminBrowser} {
			code := f.NotFound
			if browser.cookie == "" {
				code = f.Unauthenticated
			}
			requireProblem(t, v.request(t, browser, "POST", collection, body, "create"), code)
		}
		missingCSRF := v.ownerBrowser
		missingCSRF.csrf = ""
		requireProblem(t, v.request(t, missingCSRF, "POST", collection, body, "create"), f.CSRFFailed)
		if before != v.secretCounts(t) {
			t.Fatal("authentication or CSRF refusal changed Secret facts")
		}
		created := secretHTTPMutation(t, v.request(t, v.ownerBrowser, "POST", collection, body, "create"))
		if created.Fields().Command != vc.SecretCreateCommand || created.Fields().Variable.Fields().ID != target || created.Fields().Variable.Fields().Version != 1 || v.secretCounts(t) != [6]int64{2, 1, 1, 1, 1, 1} {
			t.Fatal("create did not produce the exact Secret facts")
		}
		var safeBodies []any
		for _, path := range []string{collection, detail} {
			get := v.request(t, v.ownerBrowser, "GET", path, "", "")
			requireHTTP(t, get)
			head := v.request(t, v.ownerBrowser, "HEAD", path, "", "")
			if head.aborted || head.status != 200 || len(head.body) != 0 || head.header.Get("Content-Length") != strconv.Itoa(len(get.body)) {
				t.Fatal("real authorized GET/HEAD representations diverged")
			}
			if path == detail {
				var metadata vc.SecretVariable
				if json.Unmarshal(get.body, &metadata) != nil || metadata.Validate() != nil || metadata.Fields().ID != target || metadata.Fields().ProjectID != v.project.ID {
					t.Fatal("GET returned invalid safe metadata")
				}
			} else {
				var page f.Page[vc.SecretVariable]
				if json.Unmarshal(get.body, &page) != nil || len(page.Items) != 1 || page.Items[0].Fields().ID != target || page.NextCursor != "" {
					t.Fatal("list returned invalid Secret metadata")
				}
			}
			safeBodies = append(safeBodies, json.RawMessage(get.body))
		}
		noopBody := `{"expected_version":"1","request":{"name":"HTTP_TOKEN"}}`
		noop := secretHTTPMutation(t, v.request(t, v.ownerBrowser, "PATCH", detail, noopBody, "noop"))
		if noop.Fields().Changed || noop.Fields().Variable.Fields().Version != 1 || v.secretCounts(t) != [6]int64{2, 1, 1, 1, 2, 2} {
			t.Fatal("metadata no-op changed business facts")
		}
		// Even identical explicit material must reach D04 as a replacement.
		replaceBody := string(jsonBytes(t, map[string]any{"expected_version": "1", "request": map[string]any{"value": canary}}))
		replaced := secretHTTPMutation(t, v.request(t, v.ownerBrowser, "PATCH", detail, replaceBody, "replace"))
		if !replaced.Fields().Changed || replaced.Fields().Variable.Fields().Version != 2 || v.secretCounts(t) != [6]int64{3, 2, 2, 2, 3, 3} {
			t.Fatal("explicit value presence was lost")
		}
		deleted := secretHTTPMutation(t, v.request(t, v.ownerBrowser, "DELETE", detail, `{"expected_version":"2"}`, "delete"))
		if deleted.Fields().Deleted == nil || deleted.Fields().Deleted.Version != 3 || v.secretCounts(t) != [6]int64{4, 3, 3, 3, 4, 4} {
			t.Fatal("delete did not retain its safe receipt")
		}
		requireProblem(t, v.request(t, v.ownerBrowser, "GET", detail, "", ""), f.NotFound)
		browser := v.login(t, v.ownerBrowser.email)
		before = v.secretCounts(t)
		for _, entry := range []struct {
			command                      vc.SecretCommandName
			key                          f.IdempotencyKey
			method, path, body, expected string
			receipt                      vc.SecretVariableMutation
		}{
			{vc.SecretCreateCommand, "create", "POST", collection, body, "", created},
			{vc.SecretUpdateCommand, "noop", "PATCH", detail, noopBody, "1", noop},
			{vc.SecretUpdateCommand, "replace", "PATCH", detail, replaceBody, "1", replaced},
			{vc.SecretDeleteCommand, "delete", "DELETE", detail, `{"expected_version":"2"}`, "2", deleted},
		} {
			identity := map[string]any{"command": entry.command, "target_id": target}
			if entry.expected != "" {
				identity["expected_version"] = entry.expected
			}
			query := string(jsonBytes(t, identity))
			result := secretHTTPLookup(t, v.request(t, browser, "POST", lookupPath, query, entry.key))
			if result.Status() != vc.SecretLookupCommitted || result.Receipt() == nil {
				t.Fatal("new current Session could not recover historical identity")
			}
			sameSecretReceipt(t, entry.receipt, *result.Receipt())
			replayed := secretHTTPMutation(t, v.request(t, browser, entry.method, entry.path, entry.body, entry.key))
			sameSecretReceipt(t, entry.receipt, replayed)
			safeBodies = append(safeBodies, result, replayed)
		}
		unknown := `{"command":"project.secret_variable.create","target_id":"` + target.String() + `"}`
		missing := secretHTTPLookup(t, v.request(t, browser, "POST", lookupPath, unknown, "never-observed"))
		if missing.Status() != vc.SecretLookupNotObserved || missing.Receipt() != nil {
			t.Fatal("not_observed acquired a receipt")
		}
		wrong := secretHTTPCreateBody(t, target, canary+"_changed")
		requireProblem(t, v.request(t, browser, "POST", collection, wrong, "create"), f.IdempotencyKeyReused)
		if before != v.secretCounts(t) {
			t.Fatal("history/replay or refused intent duplicated facts")
		}
		safeBodies = append(safeBodies, created, noop, replaced, deleted, missing)
		v.assertNoSecretLeak(t, []byte(canary), safeBodies...)
	})
	t.Run("authenticated-session-revoked-before-domain", func(t *testing.T) {
		v := newSecretHTTPFixture(t)
		const canary = "REVOKED_HTTP_SECRET_CANARY_813c"
		target := id[i.ProjectVariable](t)
		collection := variableHTTPPath(v.project.ID, "/secret-variables")
		body := secretHTTPCreateBody(t, target, canary)
		created := secretHTTPMutation(t, v.request(t, v.ownerBrowser, "POST", collection, body, "seed"))
		// Each barrier runs only when the adapter reads the body, after real
		// Cookie/CSRF authentication. Logout uses the formal Account method.
		for _, entry := range []struct {
			path, body string
			key        f.IdempotencyKey
		}{
			{collection, secretHTTPCreateBody(t, id[i.ProjectVariable](t), canary), "revoked-create"},
			{collection + "/commands/lookup", `{"command":"project.secret_variable.create","target_id":"` + target.String() + `"}`, "seed"},
		} {
			browser := v.login(t, v.ownerBrowser.email)
			before := v.secretCounts(t)
			request := variableHTTPRequest(ctxFor(t), browser, "POST", entry.path, entry.body, entry.key)
			reached := false
			request.Body = &authReadBarrier{ReadCloser: request.Body, before: func() { reached = true; v.revoke(t, browser) }}
			response := v.serve(request)
			requireProblem(t, response, f.SessionRevoked)
			if !reached || !strings.Contains(response.header.Get("Set-Cookie"), "Max-Age=0") || before != v.secretCounts(t) {
				t.Fatal("post-authentication revocation bypassed the original domain transaction")
			}
			v.assertNoSecretLeak(t, []byte(canary), json.RawMessage(response.body))
		}
		v.assertNoSecretLeak(t, []byte(canary), created)
	})
}
