package knowledgehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

func TestKnowledgeHTTPStandardSchema(t *testing.T) {
	python := os.Getenv("AGENTEAM_KNOWLEDGE_HTTP_SCHEMA_PYTHON")
	if python == "" {
		t.Skip("set explicit installed schema interpreter for local standard validation")
	}
	d := testDocument()
	ctx := context.Background()
	q := query{page: f.DefaultPageRequest()}
	type schemaCase struct {
		Label  string `json:"label"`
		Schema string `json:"schema"`
		Value  any    `json:"value"`
		Valid  bool   `json:"valid"`
	}
	var cases []schemaCase
	add := func(name string, raw []byte, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		cases = append(cases, schemaCase{name, name, wireObject(t, raw), true})
	}
	raw, err := encodeHead(ctx, d.ProjectID, d.ID, kc.DocumentHead{Active: &d})
	add("DocumentHead", raw, err)
	raw, err = encodePage(ctx, d.ProjectID, documents, q, f.Page[kc.DocumentRef]{Items: []kc.DocumentRef{d}})
	add("DocumentPage", raw, err)
	raw, err = encodeAncestors(ctx, d.ProjectID, d.ID, nil)
	add("Ancestors", raw, err)
	raw, err = encodeHits(ctx, d.ProjectID, q, f.Page[kc.TitleHit]{Items: []kc.TitleHit{{Document: d, Ancestors: []kc.DocumentRef{}}}})
	add("TitlePage", raw, err)
	tombstone := kc.DocumentTombstone{ID: d.ID, ProjectID: d.ProjectID, ContentVersion: 2, DeletedAt: testAt()}
	raw, err = encodeHead(ctx, d.ProjectID, d.ID, kc.DocumentHead{Deleted: &tombstone})
	add("DocumentHead", raw, err)
	base := cases[0].Value.(map[string]any)["active"].(map[string]any)
	for _, tc := range []struct {
		key   string
		value any
	}{{"object_id", d.ObjectID.String()}, {"title", "bad\n"}, {"id", d.ID.String() + "\n"}, {"content_version", "01"}, {"content_version", "9223372036854775808"}, {"content_version", 1}, {"parent_document_id", ""}, {"media_type", kc.PDF}, {"status", "deleted"}, {"indexing_status", "not_ready"}, {"created_by", map[string]any{"kind": "human", "user_id": d.CreatedBy.Details().UserID.String(), "project_id": d.ProjectID.String()}}} {
		copy := map[string]any{}
		for key, v := range base {
			copy[key] = v
		}
		copy[tc.key] = tc.value
		cases = append(cases, schemaCase{"reject " + tc.key, "Document", copy, false})
	}
	cases = append(cases, schemaCase{"both head variants", "DocumentHead", map[string]any{"active": base, "deleted": cases[4].Value.(map[string]any)["deleted"]}, false}, schemaCase{"null ancestors", "Ancestors", map[string]any{"items": nil}, false}, schemaCase{"extra title body", "TitleHit", map[string]any{"document": base, "ancestors": []any{}, "text": "private-body"}, false}, schemaCase{"null cursor", "DocumentPage", map[string]any{"items": []any{}, "next_cursor": nil}, false})
	input, err := json.Marshal(map[string]any{"cases": cases})
	if err != nil {
		t.Fatal(err)
	}
	path, err := filepath.Abs("../../../../.agent-state/knowledge-owner-read/schema-controls.py")
	if err != nil {
		t.Fatal(err)
	}
	deadline, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(deadline, python, path)
	command.Stdin = bytes.NewReader(input)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("local Schema: %v\n%s", err, output)
	}
	t.Log(string(output))
}
