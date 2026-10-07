//go:build integration

package model_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestModelMeetingSummaryExistingHTTP(t *testing.T) {
	v := newMeetingSummaryFixture(t, true)
	old, next := v.chat(t, false), v.chat(t, false)
	request := v.choice(t, old)
	receipt, e := v.service.UpdateMeetingSummarySelection(testContext(t), request)
	if e != nil {
		t.Fatal(e)
	}
	path := "/api/v1/system/models/" + old.String() + "/deletion-impact"
	response := v.request(t, v.adminBrowser, http.MethodGet, path, "", nil).want(t, 200)
	fields := response.object(t)
	groups, ok := fields["reference_groups"].([]any)
	if !ok || len(groups) != 1 || fields["reference_count"] != "1" || fields["replacement_requirement"] != "required" || fields["delete_blocker"] != nil {
		t.Fatal("incorrect Summary deletion projection")
	}
	group := groups[0].(map[string]any)
	if len(group) != 3 || group["owner_kind"] != "platform_selector" || group["role"] != "meeting_summary" || group["count"] != "1" {
		t.Fatal("Summary hidden or mislabeled")
	}
	head := v.request(t, v.adminBrowser, http.MethodHead, path, "", nil).want(t, 200)
	if len(head.body) != 0 {
		t.Fatal("HEAD returned a body")
	}
	// Only the safe raw response is exported; cookies, keys and request bodies
	// stay private. The external strict client and schema consume these bytes.
	if dir := os.Getenv("AGENTEAM_MEETING_SUMMARY_EVIDENCE_DIR"); dir != "" {
		stat, e := os.Stat(dir)
		if e != nil || !stat.IsDir() || stat.Mode().Perm()&0077 != 0 {
			t.Fatal("private response evidence directory required")
		}
		sum := sha256.Sum256(response.body)
		if e = os.WriteFile(filepath.Join(dir, "summary-deletion-impact.json"), response.body, 0600); e != nil {
			t.Fatal(e)
		}
		metadata := map[string]any{"method": "GET", "path": path, "status": response.status, "content_type": response.headers.Get("Content-Type"), "model_id": old.String(), "body_sha256": hex.EncodeToString(sum[:]), "producer_test": "TestModelMeetingSummaryExistingHTTP"}
		if e = os.WriteFile(filepath.Join(dir, "summary-deletion-impact-source.json"), summaryJSON(t, metadata), 0600); e != nil {
			t.Fatal(e)
		}
		t.Logf("safe Summary deletion body SHA256=%x model_id=%s", sum, old.String())
	}
	lookup := v.request(t, v.adminBrowser, "POST", "/api/v1/system/model-commands/lookup", string(request.Key), map[string]any{"command": "model.selection.update"}).want(t, 200).object(t)
	if lookup["found"] != true {
		t.Fatal("existing lookup omitted Summary receipt")
	}
	raw, _ := json.Marshal(lookup["receipt"])
	if string(raw) == "null" {
		t.Fatal("empty receipt")
	}
	historical := lookup["receipt"].(map[string]any)
	if historical["resource_id"] != receipt.ResourceID || historical["version"] != receipt.Version.String() {
		t.Fatal("unsafe/inexact receipt")
	}
	other := v.addBrowser(t, "admin")
	otherLookup := v.request(t, other, "POST", "/api/v1/system/model-commands/lookup", string(request.Key), map[string]any{"command": "model.selection.update"}).want(t, 200).object(t)
	if otherLookup["found"] != false || otherLookup["receipt"] != nil {
		t.Fatal("cross-admin receipt leak")
	}
	legacy := v.request(t, v.adminBrowser, "GET", "/api/v1/system/model-selection", "", nil).want(t, 200).object(t)
	if len(legacy) != 3 || legacy["configured"] != nil || legacy["id"] == request.SelectionID {
		t.Fatal("old four-purpose response changed")
	}
	for _, method := range []string{"GET", "PUT"} {
		v.request(t, v.adminBrowser, method, "/api/v1/system/model-selection/meeting-summary", newID[struct{}](t).String(), nil).want(t, 404)
	}
	v.request(t, v.adminBrowser, "PUT", "/api/v1/system/model-selection", newID[struct{}](t).String(), map[string]any{"id": legacy["id"], "expected_version": "1", "meeting_summary": old.String()}).problem(t, 400, f.InvalidArgument)
	v.request(t, v.adminBrowser, "DELETE", "/api/v1/system/models/"+old.String(), newID[struct{}](t).String(), map[string]any{"expected_version": "1", "replacement": nil}).problem(t, 409, f.InvalidState)
	deleted := v.request(t, v.adminBrowser, "DELETE", "/api/v1/system/models/"+old.String(), newID[struct{}](t).String(), map[string]any{"expected_version": "1", "replacement": next.String()}).want(t, 200).object(t)
	if deleted["affected_references"] != "1" {
		t.Fatal("HTTP delete omitted Summary reference")
	}
	s := v.summary(t)
	if s.Model == nil || *s.Model != next {
		t.Fatal("old HTTP did not replace Summary canonical")
	}
}
