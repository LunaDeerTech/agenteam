//go:build integration

package model_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestModelProjectCredentialHTTPCRUDAndHistory(t *testing.T) {
	projectCredentialTop(t)
	v := newProjectCredentialFixture(t)
	before := v.facts(t)
	original := `{"value":"` + strings.Repeat(`\u0000`, sc.MaxValueBytes-1) + `\u0001"}`
	createKey := "project-create-original"
	created := v.request(t, v.ownerBrowser, "POST", v.collection(), createKey, []byte(original)).want(t, 200)
	target := created.object(t)["credential_id"].(string)
	path := v.collection() + "/" + target
	metadata := v.request(t, v.ownerBrowser, "GET", path, "", nil).want(t, 200)
	head := v.request(t, v.ownerBrowser, "HEAD", path, "", nil).want(t, 200)
	if len(head.body) != 0 || head.headers.Get("Content-Length") != metadata.headers.Get("Content-Length") {
		t.Fatal("HEAD representation differs")
	}
	projectCredentialSchema(t, "Mutation", created.body)
	projectCredentialSchema(t, "Metadata", metadata.body)
	projectCredentialExport(t, "create", "POST", v.collection(), created)
	projectCredentialExport(t, "metadata", "GET", path, metadata)
	after := v.facts(t)
	if after.Canonical != before.Canonical+1 || after.Receipts != before.Receipts+1 || after.Audits != before.Audits+1 || after.Events != before.Events {
		t.Fatal("Create atomic facts")
	}
	if !bytes.Equal(v.request(t, v.ownerBrowser, "POST", v.collection(), createKey, []byte(original)).want(t, 200).body, created.body) {
		t.Fatal("maximum material replay changed")
	}
	v.unchanged(t, after)
	altered := strings.TrimSuffix(original, `\u0001"}`) + `\u0002"}`
	v.request(t, v.ownerBrowser, "POST", v.collection(), createKey, []byte(altered)).problem(t, 409, f.IdempotencyKeyReused)
	v.unchanged(t, after)
	lookupRequest := v.lookup(t, v.ownerBrowser, createKey, sc.Create, "", 0)
	observed, e := v.writer.LookupWriteCommand(testContext(t), lookupRequest)
	if e != nil || !observed.Observed || observed.Result.Metadata.CredentialRef.Details().ID.String() != target {
		t.Fatal("real Project lookup lost Create")
	}
	// Real Model reference retains the credential. Rotation remains allowed, but
	// deleting a referenced credential is ResourceBusy until formal release.
	provider := v.projectProvider(t, &observed.Result.Metadata.CredentialRef)
	v.request(t, v.ownerBrowser, "DELETE", path, "retained-delete", projectUpdateJSON(t, map[string]any{"expected_version": "1"})).problem(t, 409, f.ResourceBusy)
	rotate := projectUpdateJSON(t, map[string]any{"expected_version": "1", "value": "rotated-private-material"})
	rotated := v.request(t, v.ownerBrowser, "PUT", path, "rotate-original", rotate).want(t, 200)
	if rotated.object(t)["version"] != "2" {
		t.Fatal("rotation version")
	}
	_, e = v.service.DeleteProvider(testContext(t), mc.DeleteProviderRequest{CommandMeta: v.projectMeta(t, "provider-release"), ID: provider.ID, ExpectedVersion: provider.Version})
	if e != nil {
		t.Fatal("formal reference release", e)
	}
	deleted := v.request(t, v.ownerBrowser, "DELETE", path, "delete-original", projectUpdateJSON(t, map[string]any{"expected_version": "2"})).want(t, 200)
	if deleted.object(t)["version"] != "3" || deleted.object(t)["deleted"] != true {
		t.Fatal("deleted result")
	}
	v.request(t, v.ownerBrowser, "GET", path, "", nil).problem(t, 404, f.NotFound)
	stable := v.facts(t)
	for _, tc := range []struct {
		key     string
		kind    sc.MutationKind
		version f.Version
		want    systemHTTPResponse
	}{{createKey, sc.Create, 0, created}, {"rotate-original", sc.Update, 1, rotated}, {"delete-original", sc.Delete, 2, deleted}} {
		body := projectCredentialLookupBody(t, tc.kind, target, tc.version)
		response := v.request(t, v.ownerBrowser, "POST", v.lookupPath(), tc.key, body).want(t, 200)
		projectCredentialSchema(t, "Observation", response.body)
		var got struct {
			Observed bool            `json:"observed"`
			Result   json.RawMessage `json:"result"`
		}
		if json.Unmarshal(response.body, &got) != nil || !got.Observed || !bytes.Equal(got.Result, tc.want.body) {
			t.Fatal("historical safe receipt changed")
		}
		projectCredentialExport(t, "lookup-"+string(tc.kind), "POST", v.lookupPath(), response)
	}
	if !bytes.Equal(v.request(t, v.ownerBrowser, "POST", v.collection(), createKey, []byte(original)).want(t, 200).body, created.body) || !bytes.Equal(v.request(t, v.ownerBrowser, "PUT", path, "rotate-original", rotate).want(t, 200).body, rotated.body) || !bytes.Equal(v.request(t, v.ownerBrowser, "DELETE", path, "delete-original", projectUpdateJSON(t, map[string]any{"expected_version": "2"})).want(t, 200).body, deleted.body) {
		t.Fatal("deleted target historical replay changed")
	}
	v.unchanged(t, stable)
	v.request(t, v.ownerBrowser, "PUT", path, "rotate-original", projectUpdateJSON(t, map[string]any{"expected_version": "1", "value": "changed-private-material"})).problem(t, 409, f.IdempotencyKeyReused)
	v.unchanged(t, stable)
	absent := v.request(t, v.ownerBrowser, "POST", v.lookupPath(), "absent-key", projectCredentialLookupBody(t, sc.Create, "", 0)).want(t, 200)
	if string(absent.body) != `{"observed":false,"result":null}` {
		t.Fatal("false observation shape")
	}
	for _, private := range []string{"rotated-private-material", "changed-private-material", createKey, v.ownerBrowser.cookie, v.ownerBrowser.csrf} {
		if strings.Contains(v.logs.text(), private) {
			t.Fatal("sensitive material entered log")
		}
	}
	t.Logf("raw_maximum_bytes=%d decoded_bytes=%d last_byte_replay_conflict=true; safe bodies/schema only; real Model retain/rotate/release/delete; no new Secret Outbox events", len(original), sc.MaxValueBytes)
}
