package knowledgehttp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func testID[T any](n int) f.ID[T] {
	v, err := f.ParseID[T](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if err != nil {
		panic(err)
	}
	return v
}
func testAt() f.Instant {
	v, err := f.ParseInstant("2026-10-09T01:02:03.123456Z")
	if err != nil {
		panic(err)
	}
	return v
}
func testDocument() kc.DocumentRef {
	creator, err := kc.NewCreatorRef(kc.CreatorDetails{Kind: id.Human, UserID: testID[id.User](1)})
	if err != nil {
		panic(err)
	}
	return kc.DocumentRef{ID: testID[kc.Document](3), ProjectID: testID[id.Project](4), Title: "literal%_<&中文", ContentVersion: math.MaxInt64, SourceKind: kc.Text, MediaType: kc.PlainText, ObjectID: testID[oc.StoredObject](99), Status: kc.Active, IndexingStatus: kc.IndexPending, CreatedBy: creator, CreatedAt: testAt(), UpdatedAt: testAt()}
}
func wireObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func TestKnowledgeHTTPExplicitSafeProjection(t *testing.T) {
	d := testDocument()
	ctx := context.Background()
	raw, err := encodeHead(ctx, d.ProjectID, d.ID, kc.DocumentHead{Active: &d})
	if err != nil {
		t.Fatal(err)
	}
	active := wireObject(t, raw)["active"].(map[string]any)
	if len(active) != 12 || active["content_version"] != "9223372036854775807" || active["parent_document_id"] != nil || active["title"] != d.Title {
		t.Fatal("safe exact document representation")
	}
	if strings.Contains(string(raw), "object_id") || strings.Contains(string(raw), d.ObjectID.String()) {
		t.Fatal("internal Object identity escaped")
	}
	tombstone := kc.DocumentTombstone{ID: d.ID, ProjectID: d.ProjectID, ContentVersion: 2, DeletedAt: testAt()}
	raw, err = encodeHead(ctx, d.ProjectID, d.ID, kc.DocumentHead{Deleted: &tombstone})
	if err != nil {
		t.Fatal(err)
	}
	deleted := wireObject(t, raw)["deleted"].(map[string]any)
	if len(deleted) != 4 || strings.Contains(string(raw), "title") || strings.Contains(string(raw), "created_by") {
		t.Fatal("tombstone retained metadata")
	}
	q := query{page: f.DefaultPageRequest()}
	raw, err = encodePage(ctx, d.ProjectID, documents, q, f.Page[kc.DocumentRef]{})
	if err != nil || string(raw) != `{"items":[]}` {
		t.Fatal("empty page", err)
	}
	raw, err = encodeAncestors(ctx, d.ProjectID, d.ID, nil)
	if err != nil || string(raw) != `{"items":[]}` {
		t.Fatal("empty ancestors", err)
	}
}
func TestKnowledgeHTTPProjectionRejectsWholeCandidate(t *testing.T) {
	d := testDocument()
	ctx := context.Background()
	q := query{page: f.DefaultPageRequest()}
	for _, mode := range []string{"foreign", "hidden-object", "duplicate", "order", "filter", "parent", "bad-time", "next-short", "next-control"} {
		t.Run(mode, func(t *testing.T) {
			page := f.Page[kc.DocumentRef]{Items: []kc.DocumentRef{d}}
			kind := documents
			input := q
			switch mode {
			case "foreign":
				page.Items[0].ProjectID = testID[id.Project](8)
			case "hidden-object":
				page.Items[0].ObjectID = oc.ObjectID{}
			case "duplicate":
				page.Items = append(page.Items, d)
			case "order":
				other := d
				other.ID = testID[kc.Document](5)
				other.Title = "A"
				page.Items = append(page.Items, other)
			case "filter":
				input.filter.TitleQuery = "%not-present%"
			case "parent":
				kind = children
				parent := testID[kc.Document](8)
				input.parent = &parent
			case "bad-time":
				page.Items[0].UpdatedAt = f.Instant{}
			case "next-short":
				page.NextCursor = "opaque"
			case "next-control":
				page.NextCursor = "opaque\n"
				input.page.Limit = 1
			}
			raw, err := encodePage(ctx, d.ProjectID, kind, input, page)
			if err == nil || len(raw) != 0 {
				t.Fatal("invalid candidate published", mode)
			}
		})
	}
	parent := d
	parent.ID = testID[kc.Document](7)
	parent.Title = "parent"
	child := d
	child.ParentDocumentID = &parent.ID
	hit := kc.TitleHit{Document: child, Ancestors: []kc.DocumentRef{parent}}
	page := f.Page[kc.TitleHit]{Items: []kc.TitleHit{hit}}
	if _, err := encodeHits(ctx, d.ProjectID, q, page); err != nil {
		t.Fatal("valid complete path", err)
	}
	for _, mode := range []string{"missing-root", "self", "foreign-ancestor", "wrong-last", "nil-path"} {
		t.Run(mode, func(t *testing.T) {
			h := hit
			h.Ancestors = append([]kc.DocumentRef(nil), hit.Ancestors...)
			switch mode {
			case "missing-root":
				other := testID[kc.Document](88)
				h.Ancestors[0].ParentDocumentID = &other
			case "self":
				h.Ancestors[0].ID = child.ID
			case "foreign-ancestor":
				h.Ancestors[0].ProjectID = testID[id.Project](8)
			case "wrong-last":
				h.Ancestors[0].ID = testID[kc.Document](9)
			case "nil-path":
				h.Ancestors = nil
			}
			raw, err := encodeHits(ctx, d.ProjectID, q, f.Page[kc.TitleHit]{Items: []kc.TitleHit{h}})
			if err == nil || len(raw) != 0 {
				t.Fatal("invalid ancestor candidate published")
			}
		})
	}
}
func TestKnowledgeHTTPRepresentationLimitAndCancellation(t *testing.T) {
	d := testDocument()
	chain := make([]kc.DocumentRef, 7000)
	for i := range chain {
		v := d
		v.ID = testID[kc.Document](100 + i)
		v.Title = strings.Repeat("<", kc.MaxTitleRunes)
		if i > 0 {
			v.ParentDocumentID = &chain[i-1].ID
		}
		chain[i] = v
	}
	raw, err := encodeAncestors(context.Background(), d.ProjectID, d.ID, chain)
	if err == nil || len(raw) != 0 {
		t.Fatal("oversized complete chain published or truncated")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw, err = encodeHead(ctx, d.ProjectID, d.ID, kc.DocumentHead{Active: &d})
	if err == nil || len(raw) != 0 {
		t.Fatal("cancelled projection published")
	}
}
