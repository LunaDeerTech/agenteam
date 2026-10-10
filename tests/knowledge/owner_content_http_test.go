//go:build integration

package knowledge_test

import (
	"bytes"
	"io"
	"strconv"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

func TestKnowledgeOwnerContentHTTPCurrentBytes(t *testing.T) {
	knowledgeOwnerHTTPTop(t)
	v := newContentHTTPFixture(t)
	text := "文a🙂end\n"
	doc := contentHTTPCreate(t, v, kc.Markdown, "Current UTF-8", text)
	var cases []contentHTTPSchemaCase
	t.Run("utf8_slices_default_head_and_zero_business_facts", func(t *testing.T) {
		before := v.facts(t)
		for _, tc := range []struct {
			query, text string
			next        int64
			truncated   bool
		}{
			{"", text, int64(len(text)), false}, {"?max_bytes=3", "文", 3, true}, {"?byte_offset=3&max_bytes=5", "a🙂", 8, true},
			{"?max_bytes=1", "", 0, true}, {"?byte_offset=" + strconv.Itoa(len(text)), "", int64(len(text)), false},
		} {
			body := contentHTTPText(t, v.request(t, v.ownerBrowser, "GET", contentHTTPPath(v, doc.ID, tc.query), "", ""), doc, tc.text, tc.next, tc.truncated)
			cases = append(cases, contentHTTPSchemaCase{Label: "actual byte slice", Schema: "Content", Value: body, Valid: true})
			contentHTTPNoReader(t, v, doc)
		}
		get := v.request(t, v.ownerBrowser, "GET", contentHTTPPath(v, doc.ID, "?byte_offset=3&max_bytes=5"), "", "")
		head := v.request(t, v.ownerBrowser, "HEAD", contentHTTPPath(v, doc.ID, "?byte_offset=3&max_bytes=5"), "", "")
		if head.aborted || head.status != 200 || len(head.body) != 0 || head.header.Get("Content-Length") != get.header.Get("Content-Length") || head.header.Get("Content-Type") != "application/json" {
			t.Fatal("HEAD failed same bounded representation")
		}
		contentHTTPNoReader(t, v, doc)
		for _, query := range []string{"?byte_offset=1", "?byte_offset=999", "?max_bytes=0", "?max_bytes=1048577", "?byte_offset=0&byte_%6fffset=3"} {
			v.request(t, v.ownerBrowser, "GET", contentHTTPPath(v, doc.ID, query), "", "").want(t, 400)
			contentHTTPNoReader(t, v, doc)
		}
		if v.facts(t) != before {
			t.Fatal("content read changed commands/events/Audit/Activity")
		}
	})
	t.Run("default_and_maximum_are_utf8_byte_limits", func(t *testing.T) {
		large := strings.Repeat("a", (1<<20)+3)
		d := contentHTTPCreate(t, v, kc.PlainText, "Large current text", large)
		before := v.facts(t)
		contentHTTPText(t, v.request(t, v.ownerBrowser, "GET", contentHTTPPath(v, d.ID, ""), "", ""), d, large[:65536], 65536, true)
		contentHTTPText(t, v.request(t, v.ownerBrowser, "GET", contentHTTPPath(v, d.ID, "?max_bytes=1048576"), "", ""), d, large[:1<<20], 1<<20, true)
		contentHTTPNoReader(t, v, d)
		if v.facts(t) != before {
			t.Fatal("large reads changed business facts")
		}
	})
	t.Run("real_pdf_docx_have_no_readable_provider", func(t *testing.T) {
		for _, media := range []string{kc.PDF, kc.DOCX} {
			raw := []byte("%PDF-1.7\n\x00\xff\n%%EOF")
			if media == kc.DOCX {
				raw = independentDOCX(t)
			}
			source, err := kc.NewUploadSource(media, f.Progress(len(raw)), independentDigest(raw), io.NopCloser(bytes.NewReader(raw)))
			if err != nil {
				t.Fatal(err)
			}
			d, err := v.service.CreateDocument(knowledgeContext(t), v.ownerBrowser.actor, treeMeta(t), kc.CreateRequest{ProjectID: v.project, DocumentID: treeID[kc.Document](t), Title: "Unbound current file"}, source)
			if err != nil {
				t.Fatal("actual binary source publication", err)
			}
			before := v.facts(t)
			response := v.request(t, v.ownerBrowser, "GET", contentHTTPPath(v, d.ID, ""), "", "")
			body := response.want(t, 200)
			metadata, ok := body["document"].(map[string]any)
			if !ok || len(body) != 2 || len(metadata) != 12 || metadata["id"] != d.ID.String() || metadata["source_kind"] != "file" || body["unavailable"] != "dependency_unbound" || bytes.Contains(response.body, []byte(d.ObjectID.String())) {
				t.Fatal("binary content invented text or raw access")
			}
			cases = append(cases, contentHTTPSchemaCase{Label: "actual binary unavailable", Schema: "Content", Value: body, Valid: true})
			contentHTTPNoReader(t, v, d)
			if v.facts(t) != before {
				t.Fatal("unbound read changed business facts")
			}
		}
	})
	t.Run("deleted_410_missing_foreign_404_and_bodyless_head", func(t *testing.T) {
		preview, err := v.service.PrepareDeleteSubtree(knowledgeContext(t), v.ownerBrowser.actor, v.project, doc.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = v.service.DeleteSubtree(knowledgeContext(t), v.ownerBrowser.actor, treeMeta(t), v.project, doc.ID, preview.Confirmation); err != nil {
			t.Fatal("actual canonical delete", err)
		}
		before := v.facts(t)
		response := v.request(t, v.ownerBrowser, "GET", contentHTTPPath(v, doc.ID, ""), "", "")
		body := response.want(t, 410)
		if body["code"] != "RESOURCE_DELETED" || bytes.Contains(response.body, []byte(text)) {
			t.Fatal("tombstone read lost exact code or retained content")
		}
		cases = append(cases, contentHTTPSchemaCase{Label: "authorized deleted", Document: "common.json", Schema: "Problem", Value: body, Valid: true})
		head := v.request(t, v.ownerBrowser, "HEAD", contentHTTPPath(v, doc.ID, ""), "", "")
		if head.aborted || head.status != 410 || len(head.body) != 0 || head.header.Get("Content-Length") == "0" {
			t.Fatal("deleted HEAD leaked a body or avoided authorization")
		}
		for _, browser := range []knowledgeOwnerHTTPBrowser{v.otherBrowser, v.adminBrowser} {
			v.request(t, browser, "GET", contentHTTPPath(v, doc.ID, ""), "", "").want(t, 404)
		}
		v.request(t, v.ownerBrowser, "GET", contentHTTPPath(v, treeID[kc.Document](t), ""), "", "").want(t, 404)
		foreign := v.ownerTreeFixture.project(t, v.ownerBrowser.actor, true)
		v.request(t, v.ownerBrowser, "GET", knowledgeOwnerHTTPPath(foreign, "/"+doc.ID.String()+"/content"), "", "").want(t, 404)
		v.request(t, v.ownerBrowser, "GET", knowledgeOwnerHTTPPath(treeID[id.Project](t), "/"+doc.ID.String()+"/content"), "", "").want(t, 404)
		if v.facts(t) != before {
			t.Fatal("deleted/missing/foreign read changed business facts")
		}
	})
	contentHTTPValidateSchema(t, cases)
}
