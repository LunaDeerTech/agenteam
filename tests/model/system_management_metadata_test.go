//go:build integration

package model_test

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestSystemModelManagementMetadata(t *testing.T) {
	v := newSystemHTTPFixture(t)
	createKey, updateKey := newID[struct{}](t).String(), newID[struct{}](t).String()
	credential := v.credential(t, createKey, "management-material-canary")
	path := "/api/v1/system/model-credentials/" + credential
	read := func(version string) {
		t.Helper()
		before, n, start := v.facts(t), v.nonce(t), len(v.tracked.observations())
		first := v.rawRequest(testContext(t), v.adminBrowser, "GET", path, "", nil, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }).want(t, 200)
		want := map[string]any{"credential_id": credential, "purpose": "model", "version": version}
		if !reflect.DeepEqual(first.object(t), want) {
			t.Fatal("current metadata is not exact")
		}
		second := v.request(t, v.adminBrowser, "GET", path, "", nil).want(t, 200)
		httpReceiptSame(t, first, second)
		if head := v.request(t, v.adminBrowser, "HEAD", path, "", nil).want(t, 200); len(head.body) != 0 {
			t.Fatal("HEAD exposed a body")
		}
		httpSameFacts(t, before, v.facts(t))
		if v.nonce(t) != n {
			t.Fatal("metadata allocated nonce")
		}
		reads := 0
		for _, trace := range v.tracked.observations()[start:] {
			if trace.owner == "secret-metadata" {
				reads++
				if trace.actual != f.Committed || trace.projected != f.Committed || trace.payloadReads != 0 || trace.receiptReads != 0 || len(trace.locks) != 2 || trace.held == 0 {
					t.Fatal("metadata bypassed read transaction or read material")
				}
			}
		}
		if reads != 3 {
			t.Fatal("GET/HEAD did not each read once")
		}
	}
	t.Run("create-and-refresh", func(t *testing.T) { read("1") })
	v.request(t, v.adminBrowser, "PUT", path, updateKey, map[string]any{"expected_version": "1", "value": "management-updated-canary"}).want(t, 200)
	t.Run("current-not-receipt", func(t *testing.T) {
		read("2")
		old := v.request(t, v.adminBrowser, "POST", "/api/v1/system/model-credential-commands/lookup", createKey, map[string]any{"kind": "create"}).want(t, 200).object(t)
		if old["result"].(map[string]any)["version"] != "1" {
			t.Fatal("history overwritten")
		}
	})
	t.Run("strict-wire-and-current-admin", func(t *testing.T) {
		regular := v.addBrowser(t, "user")
		for _, target := range []string{path, path + "?", path + "?limit=1", "/api/v1/system/model-credentials/invalid"} {
			v.request(t, regular, "GET", target, "", nil).problem(t, 403, f.Forbidden)
			v.request(t, systemHTTPBrowser{}, "GET", target, "", nil).problem(t, 401, f.Unauthenticated)
		}
		for _, suffix := range []string{"?", "?cursor=x", "?limit=1"} {
			v.request(t, v.adminBrowser, "GET", path+suffix, "", nil).problem(t, 400, f.InvalidArgument)
		}
		v.request(t, v.adminBrowser, "GET", path, "", map[string]any{}).problem(t, 400, f.InvalidArgument)
		v.rawRequest(testContext(t), v.adminBrowser, "GET", path, "", nil, func(r *http.Request) { r.TransferEncoding = []string{"chunked"} }).problem(t, 400, f.InvalidArgument)
		v.request(t, v.adminBrowser, "PATCH", path, "", nil).want(t, 405)
	})
	t.Run("other-purpose-and-unknown", func(t *testing.T) {
		other := v.secretWrite(t, v.adminBrowser, newID[struct{}](t).String(), sc.Create, "", 0, sc.SMTP, "management-smtp-canary")
		for _, key := range []string{other.Metadata.CredentialRef.Details().ID.String(), newID[sc.Credential](t).String()} {
			v.request(t, v.adminBrowser, "GET", "/api/v1/system/model-credentials/"+key, "", nil).problem(t, 404, f.NotFound)
		}
	})
	t.Run("deleted-current-and-historical-replay", func(t *testing.T) {
		v.request(t, v.adminBrowser, "DELETE", path, newID[struct{}](t).String(), map[string]any{"expected_version": "2"}).want(t, 200)
		v.request(t, v.adminBrowser, "GET", path, "", nil).problem(t, 404, f.NotFound)
		old := v.request(t, v.adminBrowser, "POST", "/api/v1/system/model-credential-commands/lookup", updateKey, map[string]any{"kind": "update", "credential_id": credential, "expected_version": "1"}).want(t, 200).object(t)
		if old["result"].(map[string]any)["version"] != "2" || old["result"].(map[string]any)["deleted"] != false {
			t.Fatal("historical receipt became current metadata")
		}
		v.request(t, v.adminBrowser, "PUT", path, updateKey, map[string]any{"expected_version": "1", "value": "management-updated-canary"}).want(t, 200)
		v.request(t, v.adminBrowser, "GET", path, "", nil).problem(t, 404, f.NotFound)
	})
	for _, canary := range []string{"management-material-canary", "management-updated-canary", "management-smtp-canary", v.adminBrowser.cookie, v.adminBrowser.csrf} {
		if strings.Contains(v.log.text(), canary) {
			t.Fatal("sensitive read/write input in ordinary logs")
		}
	}
}
