//go:build integration

package knowledge_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	contenthttp "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contenthttp"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	knowledgehttp "github.com/LunaDeerTech/agenteam/internal/central/knowledge/http"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge/texthttp"
)

// The original fixture owns real Account Login, PostgreSQL, Object/MinIO and
// their actual cleanup. Only upstream Project initialization is seeded there.
// All text publication below enters the public HTTP adapter. Its recorder is
// business-path evidence; the separate App test exercises a native listener.
func newTextHTTPFixture(t *testing.T) *treeCommandHTTPFixture {
	t.Helper()
	v := newTreeCommandHTTPFixture(t)
	text, err := texthttp.NewHTTPHandler(v.service, v.boundary)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := knowledgehttp.NewHTTPHandler(v.service, v.boundary)
	if err != nil {
		t.Fatal(err)
	}
	content, err := contenthttp.NewHTTPHandler(v.service, v.boundary)
	if err != nil {
		t.Fatal(err)
	}
	v.handler = httpapi.Handler(slog.New(slog.NewJSONHandler(v.logs, nil)), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case texthttp.HandlesPath(r.URL.Path):
			text.ServeHTTP(w, r)
		case contenthttp.HandlesPath(r.URL.Path):
			content.ServeHTTP(w, r)
		default:
			metadata.ServeHTTP(w, r)
		}
	}))
	return v
}

func textHTTPLookup(t *testing.T, v *treeCommandHTTPFixture, document kc.DocumentID, command, request string, key f.IdempotencyKey) treeCommandHTTPResponse {
	t.Helper()
	body := treeCommandJSON(t, map[string]any{"command": command, "document_id": document.String(), "request": json.RawMessage(request)})
	return v.request(t, v.ownerBrowser, http.MethodPost, treeCommandHTTPPath(v.project, "/text-commands/lookup"), body, key)
}

func textHTTPRead(t *testing.T, v *treeCommandHTTPFixture, document kc.DocumentRef, text string) {
	t.Helper()
	path := treeCommandHTTPPath(v.project, "/"+document.ID.String())
	metadata := treeCommandSuccess(t, v.request(t, v.ownerBrowser, http.MethodGet, path, "", ""))
	if len(metadata) != 1 {
		t.Fatal("metadata returned body or extra fields")
	}
	treeCommandDocument(t, metadata["active"], document)
	content := treeCommandSuccess(t, v.request(t, v.ownerBrowser, http.MethodGet, path+"/content", "", ""))
	treeCommandDocument(t, content["document"], document)
	var body struct {
		Text      string `json:"text"`
		Next      string `json:"next_byte_offset"`
		Truncated bool   `json:"truncated"`
	}
	if len(content) != 2 || json.Unmarshal(content["text"], &body) != nil || body.Text != text || body.Next != strconv.Itoa(len(text)) || body.Truncated {
		t.Fatal("HTTP reread did not return the exact canonical UTF-8 text")
	}
	publicationFacts(t, v.ownerTreeFixture, document, []byte(text))
	var readers int
	if err := v.raw.QueryRow(knowledgeContext(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND owner_kind='reader' AND state='active'`, document.ObjectID.String()).Scan(&readers); err != nil || readers != 0 {
		t.Fatal("original HTTP canonical reader did not retire", err)
	}
}
