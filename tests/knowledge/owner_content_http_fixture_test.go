//go:build integration

package knowledge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	contenthttp "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contenthttp"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

// Reuse the already-real same-Store Account/B02/D05 fixture and its owned tails.
// Only the newly constructed handler changes. The original recorder is a PG
// transport control, not native socket/deadline evidence. Project init remains
// the upstream SQL fixture; positive browser identities use actual Login.
func newContentHTTPFixture(t *testing.T) *knowledgeOwnerHTTPFixture {
	t.Helper()
	v := newKnowledgeOwnerHTTPFixture(t)
	installContentHTTP(t, v, v.service)
	return v
}
func installContentHTTP(t *testing.T, v *knowledgeOwnerHTTPFixture, service *knowledge.Service) {
	t.Helper()
	handler, err := contenthttp.NewHTTPHandler(service, v.boundary)
	if err != nil {
		t.Fatal(err)
	}
	v.handler = httpapi.Handler(slog.New(slog.NewJSONHandler(v.logs, nil)), handler)
}
func contentHTTPPath(v *knowledgeOwnerHTTPFixture, document kc.DocumentID, query string) string {
	return knowledgeOwnerHTTPPath(v.project, "/"+document.String()+"/content") + query
}
func contentHTTPCreate(t *testing.T, v *knowledgeOwnerHTTPFixture, media, title, text string) kc.DocumentRef {
	t.Helper()
	source, err := kc.NewTextSource(media, text)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := v.service.CreateDocument(knowledgeContext(t), v.ownerBrowser.actor, treeMeta(t), kc.CreateRequest{ProjectID: v.project, DocumentID: treeID[kc.Document](t), Title: title}, source)
	if err != nil {
		t.Fatal("actual current content publication", err)
	}
	return doc
}
func contentHTTPText(t *testing.T, response knowledgeOwnerHTTPResponse, document kc.DocumentRef, text string, next int64, truncated bool) map[string]any {
	t.Helper()
	body := response.want(t, 200)
	metadata, ok := body["document"].(map[string]any)
	if !ok || len(body) != 2 || len(metadata) != 12 || metadata["id"] != document.ID.String() || metadata["project_id"] != document.ProjectID.String() || metadata["content_version"] != strconv.FormatInt(int64(document.ContentVersion), 10) {
		t.Fatal("unsafe or wrong current content metadata")
	}
	value, ok := body["text"].(map[string]any)
	if !ok || len(value) != 3 || value["text"] != text || value["next_byte_offset"] != strconv.FormatInt(next, 10) || value["truncated"] != truncated {
		t.Fatal("wrong original UTF-8 byte slice or continuation")
	}
	if bytes.Contains(response.body, []byte(document.ObjectID.String())) || strings.Contains(string(response.body), "object_id") {
		t.Fatal("canonical Object identity escaped")
	}
	return body
}
func contentHTTPNoReader(t *testing.T, v *knowledgeOwnerHTTPFixture, document kc.DocumentRef) {
	t.Helper()
	var active int
	if err := v.raw.QueryRow(knowledgeContext(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND owner_kind='reader' AND state='active'`, document.ObjectID.String()).Scan(&active); err != nil || active != 0 {
		t.Fatal("original canonical reader lease remains active", err)
	}
}

type contentHTTPSchemaCase struct {
	Label    string `json:"label"`
	Document string `json:"document,omitempty"`
	Schema   string `json:"schema"`
	Value    any    `json:"value"`
	Valid    bool   `json:"valid"`
}

func contentHTTPValidateSchema(t *testing.T, cases []contentHTTPSchemaCase) {
	t.Helper()
	python := os.Getenv("AGENTEAM_KNOWLEDGE_CONTENT_SCHEMA_PYTHON")
	if python == "" {
		t.Fatal("explicit existing local content Schema interpreter required")
	}
	raw, err := json.Marshal(map[string]any{"cases": cases})
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("../../.agent-state/knowledge-content-http/schema-controls.py")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(knowledgeContext(t), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-B", script)
	cmd.Stdin = bytes.NewReader(raw)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("actual HTTP content Schema failed: %v %s", err, output)
	}
	t.Log(string(output))
}
