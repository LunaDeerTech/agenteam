package commandhttp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

// Optional export contains only synthetic fixture identities and material. The
// standalone checker validates these actual handler outputs with JSON Schema;
// neither this controlled service nor Schema proves actual PG authorization.
func TestTreeCommandsHTTPSchemaVectors(t *testing.T) {
	type vector struct {
		Name   string `json:"name"`
		Schema string `json:"schema"`
		Valid  bool   `json:"valid"`
		Value  any    `json:"value"`
	}
	var vectors []vector
	add := func(name, schema string, valid bool, raw []byte) {
		var value any
		if json.Unmarshal(raw, &value) != nil {
			t.Fatal("vector JSON")
		}
		vectors = append(vectors, vector{name, schema, valid, value})
	}
	var document map[string]any
	for _, action := range []string{"rename", "move", "delete-preview", "delete-subtree", "lookup"} {
		h, s, _ := fixture()
		body := `{"expected_version":"1","title":"original"}`
		requestSchema, resultSchema := "RenameRequest", "RenameResult"
		switch action {
		case "move":
			body = `{"expected_parent_id":null,"target_parent_id":null}`
			requestSchema, resultSchema = "MoveRequest", "MoveResult"
		case "delete-preview":
			body = `{}`
			requestSchema, resultSchema = "PreviewRequest", "PreviewResult"
		case "delete-subtree":
			body = fmt.Sprintf(`{"confirmation_token":%q}`, s.preview.Confirmation.ForHumanResponse())
			requestSchema, resultSchema = "DeleteRequest", "DeleteResult"
		case "lookup":
			body = fmt.Sprintf(`{"command":"update","document_id":%q,"request":{"expected_version":"1","title":"original"}}`, s.doc.ID.String())
			requestSchema, resultSchema = "LookupRequest", "LookupResult"
		}
		w := requireSuccess(t, h, request(action, body))
		add(action+" request", requestSchema, true, []byte(body))
		add(action+" response", resultSchema, true, w.Body.Bytes())
		if action == "rename" {
			var result map[string]any
			if json.Unmarshal(w.Body.Bytes(), &result) != nil {
				t.Fatal("output")
			}
			document = result["document"].(map[string]any)
		}
		if action == "lookup" {
			for _, state := range []kc.LookupState{kc.InProgress, kc.Committed} {
				s.lookup = kc.CommandLookup{State: state}
				if state == kc.Committed {
					s.lookup.Receipt = &kc.MutationReceipt{Command: kc.Update, Document: &s.doc}
				}
				w = requireSuccess(t, h, request(action, body))
				add("lookup "+string(state), "LookupResult", true, w.Body.Bytes())
			}
		}
	}
	add("document", "Document", true, must(json.Marshal(document)))
	for _, change := range []string{"object_id", "missing_title", "creator", "status", "source_media", "version_number", "version_zero", "version_leading_zero"} {
		var d map[string]any
		_ = json.Unmarshal(must(json.Marshal(document)), &d)
		switch change {
		case "object_id":
			d["object_id"] = "private"
		case "missing_title":
			delete(d, "title")
		case "creator":
			d["created_by"] = map[string]any{"kind": "system", "user_id": "private"}
		case "status":
			d["status"] = "deleted"
		case "source_media":
			d["media_type"] = "application/pdf"
		case "version_number":
			d["content_version"] = 1
		case "version_zero":
			d["content_version"] = "0"
		case "version_leading_zero":
			d["content_version"] = "01"
		}
		add(change, "Document", false, must(json.Marshal(d)))
	}
	for _, title := range []string{"line\n", "\x00", "\u0085"} {
		add("control title", "Title", false, must(json.Marshal(title)))
	}
	for _, title := range []string{"🚀", " original "} {
		add("valid title", "Title", true, must(json.Marshal(title)))
	}
	for _, item := range []struct{ schema, raw string }{
		{"RenameRequest", `{"expected_version":"1","title":"original","replace_source":false}`},
		{"MoveRequest", `{"target_parent_id":null}`},
		{"PreviewRequest", `{"extra":true}`},
		{"DeleteRequest", `{"confirmation_token":null}`},
		{"LookupResult", `{"state":"committed","receipt":null}`},
		{"LookupResult", `{"state":"not_observed"}`},
		{"LookupResult", `{"state":"in_progress","receipt":{}}`},
	} {
		add("closed "+item.schema, item.schema, false, []byte(item.raw))
	}
	raw := must(json.MarshalIndent(vectors, "", "  "))
	path := os.Getenv("AGENTEAM_TREE_HTTP_SCHEMA_VECTORS")
	if path == "" {
		path = filepath.Join(t.TempDir(), "vectors.json")
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal("write synthetic schema vectors", err)
	}
	t.Logf("synthetic actual-handler schema vectors=%d", len(vectors))
}
