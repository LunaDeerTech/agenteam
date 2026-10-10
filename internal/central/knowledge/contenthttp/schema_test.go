package contenthttp

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

func TestContentHTTPActualSchema(t *testing.T) {
	python := os.Getenv("AGENTEAM_KNOWLEDGE_CONTENT_SCHEMA_PYTHON")
	if python == "" {
		t.Skip("explicit local Schema Python required")
	}
	type vector struct {
		Label    string `json:"label"`
		Document string `json:"document,omitempty"`
		Schema   string `json:"schema"`
		Value    any    `json:"value"`
		Valid    bool   `json:"valid"`
	}
	var cases []vector
	var original map[string]any
	for _, media := range []string{kc.PlainText, kc.Markdown, kc.PDF, kc.DOCX} {
		h, _, p := testHandler()
		p.content.Document.MediaType = media
		if media == kc.PDF || media == kc.DOCX {
			x := kc.ReadableUnbound
			p.content.Document.SourceKind = kc.File
			p.content.Text = nil
			p.content.Unavailable = &x
		}
		w := newTestWriter()
		if serveTest(h, httptest.NewRequest("GET", testPath(), nil), w) || w.Code != 200 {
			t.Fatal("valid Schema control did not reach actual handler")
		}
		var value map[string]any
		if json.Unmarshal(w.Body.Bytes(), &value) != nil {
			t.Fatal("invalid actual JSON")
		}
		cases = append(cases, vector{media, "", "Content", value, true})
		if media == kc.PlainText {
			original = value
		}
	}
	for _, code := range []f.Code{f.ResourceDeleted, f.NotFound, f.CommitUnknown} {
		h, _, p := testHandler()
		p.err = f.NewFault(code, f.NotStarted)
		if code == f.CommitUnknown {
			p.err = f.NewFault(code, f.Unknown)
		}
		w := newTestWriter()
		if serveTest(h, httptest.NewRequest("GET", testPath(), nil), w) {
			t.Fatal("classified Problem aborted")
		}
		var value any
		if json.Unmarshal(w.Body.Bytes(), &value) != nil {
			t.Fatal("invalid Problem JSON")
		}
		cases = append(cases, vector{string(code), "common.json", "Problem", value, true})
	}
	for _, kind := range []string{"object", "extra", "missing", "numeric-offset", "negative-offset", "leading-zero", "newline-offset", "union", "file-text", "unknown-unavailable"} {
		raw, _ := json.Marshal(original)
		var value map[string]any
		_ = json.Unmarshal(raw, &value)
		doc := value["document"].(map[string]any)
		text := value["text"].(map[string]any)
		switch kind {
		case "object":
			doc["object_id"] = "private object canary"
		case "extra":
			value["receipt"] = map[string]any{}
		case "missing":
			delete(doc, "created_by")
		case "numeric-offset":
			text["next_byte_offset"] = 4
		case "negative-offset":
			text["next_byte_offset"] = "-1"
		case "leading-zero":
			text["next_byte_offset"] = "04"
		case "newline-offset":
			text["next_byte_offset"] = "4\n"
		case "union":
			value["unavailable"] = "dependency_unbound"
		case "file-text":
			doc["source_kind"] = "file"
			doc["media_type"] = kc.PDF
		case "unknown-unavailable":
			delete(value, "text")
			value["unavailable"] = "processing"
			doc["source_kind"] = "file"
			doc["media_type"] = kc.PDF
		}
		cases = append(cases, vector{kind, "", "Content", value, false})
	}
	input, err := json.Marshal(map[string]any{"cases": cases})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(python, "-B", filepath.Join("..", "..", "..", "..", ".agent-state", "knowledge-content-http", "schema-controls.py"))
	cmd.Stdin = bytes.NewReader(input)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("local Schema validation failed: %v %s", err, output)
	}
	t.Logf("%s", output)
}
