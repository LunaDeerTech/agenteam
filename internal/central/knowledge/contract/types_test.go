package contract_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	k "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func must[T any](v T, e error) T {
	if e != nil {
		panic(e)
	}
	return v
}
func keyID[T any](n int) f.ID[T] {
	return must(f.ParseID[T](fmt.Sprintf("01902e15-1000-7000-8000-%012x", n)))
}
func instant(s string) f.Instant { return must(f.ParseInstant(s)) }
func actor() id.Actor            { return must(id.NewHuman(keyID[id.User](1), keyID[id.Session](2))) }
func meta() f.CommandMeta {
	return f.CommandMeta{RequestID: keyID[f.Request](3), IdempotencyKey: "knowledge-test-command"}
}
func doc(n int) k.DocumentRef {
	return k.DocumentRef{ID: keyID[k.Document](n), ProjectID: keyID[id.Project](4), Title: " 原始 title é ", ContentVersion: 1, SourceKind: k.Text, MediaType: k.PlainText, ObjectID: keyID[oc.StoredObject](5 + n), Status: k.Active, IndexingStatus: k.IndexPending, CreatedBy: must(k.NewCreatorRef(k.CreatorDetails{Kind: id.Human, UserID: keyID[id.User](1)})), CreatedAt: instant("2026-01-01T00:00:00Z"), UpdatedAt: instant("2026-01-01T00:00:00Z")}
}
func ptr[T any](v T) *T { return &v }
func requireError(t *testing.T, e error) {
	t.Helper()
	if e == nil {
		t.Fatal("expected rejection")
	}
}
func requireOK(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func roundTrip(t *testing.T, v any, dst any) {
	t.Helper()
	b, e := json.Marshal(v)
	requireOK(t, e)
	requireOK(t, json.Unmarshal(b, dst))
	again, e := json.Marshal(dst)
	requireOK(t, e)
	if string(b) != string(again) {
		t.Fatalf("unstable wire\n%s\n%s", b, again)
	}
}

func TestClosedEnums(t *testing.T) {
	cases := []struct {
		name   string
		good   []string
		target func() any
	}{
		{"source", []string{"text", "file"}, func() any { return new(k.SourceKind) }},
		{"status", []string{"active", "deleted"}, func() any { return new(k.DocumentStatus) }},
		{"index", []string{"pending", "processing", "ready", "failed"}, func() any { return new(k.IndexingStatus) }},
		{"unavailable", []string{"processing", "failed", "dependency_unbound"}, func() any { return new(k.ReadableUnavailable) }},
		{"input", []string{"text", "upload", "business_file"}, func() any { return new(k.InputKind) }},
		{"command", []string{"create", "update", "move", "delete-subtree"}, func() any { return new(k.CommandName) }},
		{"lookup", []string{"committed", "in_progress", "not_observed"}, func() any { return new(k.LookupState) }},
		{"change", []string{"created", "title", "source"}, func() any { return new(k.ContentChange) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, good := range tc.good {
				target := tc.target()
				raw, _ := json.Marshal(good)
				requireOK(t, json.Unmarshal(raw, target))
				baseline, _ := json.Marshal(target)
				for _, bad := range []string{`null`, `""`, `"unknown"`, `0`, `true`, `[]`, `{}`, `"ACTIVE"`} {
					requireError(t, json.Unmarshal([]byte(bad), target))
					after, _ := json.Marshal(target)
					if string(after) != string(baseline) {
						t.Fatal("failed enum decode mutated value")
					}
				}
			}
			_, e := json.Marshal(tc.target())
			requireError(t, e)
		})
	}
}
func TestMetadataRulesAndLosslessWire(t *testing.T) {
	for _, s := range []string{" ", "同名", strings.Repeat("树", 512), "e\u0301"} {
		requireOK(t, k.ValidateTitle(s))
	}
	for _, s := range []string{"", strings.Repeat("树", 513), "a\nb", "a\u0000b", string([]byte{0xff})} {
		requireError(t, k.ValidateTitle(s))
	}
	d := doc(10)
	d.ContentVersion = f.Version(9007199254740993)
	var decoded k.DocumentRef
	roundTrip(t, d, &decoded)
	if decoded.Title != d.Title || decoded.ContentVersion != d.ContentVersion || decoded.CreatedBy.Details() != d.CreatedBy.Details() {
		t.Fatal("metadata changed")
	}
	other := doc(11)
	other.Title = d.Title
	requireOK(t, other.Validate())
	for _, change := range []func(*k.DocumentRef){
		func(x *k.DocumentRef) { x.SourceKind = k.File },
		func(x *k.DocumentRef) { x.MediaType = "text/plain; charset=utf-8" },
		func(x *k.DocumentRef) { x.ParentDocumentID = &x.ID },
		func(x *k.DocumentRef) { x.CreatedBy = k.CreatorRef{} },
		func(x *k.DocumentRef) { x.UpdatedAt = instant("2025-01-01T00:00:00Z") },
	} {
		bad := d
		change(&bad)
		requireError(t, bad.Validate())
	}
	agent := must(k.NewCreatorRef(k.CreatorDetails{Kind: id.AgentRun, ProjectID: d.ProjectID, AgentID: keyID[id.Agent](1), ExecutionID: keyID[id.Execution](2)}))
	d.CreatedBy = agent
	roundTrip(t, d, &decoded)
	d.ProjectID = keyID[id.Project](90)
	requireError(t, d.Validate())
	_, e := k.NewCreatorRef(k.CreatorDetails{Kind: id.Human, UserID: keyID[id.User](1), AgentID: keyID[id.Agent](1)})
	requireError(t, e)
	_, e = k.NewCreatorRef(k.CreatorDetails{Kind: id.Service})
	requireError(t, e)
	requireError(t, (k.CreatorRef{}).Validate())
}
func TestMetadataDecoderRejectsAmbiguityAtomically(t *testing.T) {
	original := doc(10)
	raw := string(must(json.Marshal(original)))
	cases := []string{
		strings.TrimSuffix(raw, "}") + `,"extra":true}`,
		strings.TrimSuffix(raw, "}") + `,"title":"duplicate"}`,
		strings.Replace(raw, `"title":`, `"Title":`, 1),
		strings.Replace(raw, `"parent_document_id":null,`, "", 1),
		strings.Replace(raw, `"content_version":"1"`, `"content_version":9007199254740993`, 1),
		strings.Replace(raw, `"title":" 原始 title é "`, `"title":null`, 1),
		strings.Replace(raw, `"kind":"human"`, `"kind":"human","session_id":"secret"`, 1),
		strings.Replace(raw, `"kind":"human"`, `"kind":"human","agent_id":""`, 1),
		strings.Replace(raw, `"status":"active"`, `"status":null`, 1),
		strings.Replace(raw, " 原始 title é ", string([]byte{0xff}), 1),
	}
	for i, b := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			got := original
			requireError(t, json.Unmarshal([]byte(b), &got))
			after := string(must(json.Marshal(got)))
			if after != raw {
				t.Fatal("partial assignment")
			}
		})
	}
}
func TestDeletedAndContentUnions(t *testing.T) {
	d := doc(10)
	tomb := k.DocumentTombstone{ID: d.ID, ProjectID: d.ProjectID, ContentVersion: 2, DeletedAt: d.CreatedAt}
	h := k.DocumentHead{Deleted: &tomb}
	var decoded k.DocumentHead
	roundTrip(t, h, &decoded)
	raw := string(must(json.Marshal(h)))
	for _, forbidden := range []string{"title", "object_id", "body", "created_by"} {
		if strings.Contains(raw, forbidden) {
			t.Fatal("tombstone retained metadata", raw)
		}
	}
	requireError(t, (k.DocumentHead{Active: &d, Deleted: &tomb}).Validate())
	requireError(t, json.Unmarshal([]byte(`{"deleted":null}`), &decoded))
	text := k.DocumentContent{Document: d, Text: &k.TextContent{Text: "树\n", NextByteOffset: 4}}
	var out k.DocumentContent
	roundTrip(t, text, &out)
	fileDoc := d
	fileDoc.SourceKind = k.File
	fileDoc.MediaType = k.PDF
	requireError(t, (k.DocumentContent{Document: fileDoc, Text: text.Text}).Validate())
	ref := must(oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.KnowledgeFile, ProjectID: d.ProjectID, DocumentID: d.ID.String(), Revision: d.ContentVersion}))
	roundTrip(t, k.DocumentContent{Document: fileDoc, File: &ref}, &out)
	unknown := k.ReadableUnbound
	roundTrip(t, k.DocumentContent{Document: fileDoc, Unavailable: &unknown}, &out)
	requireError(t, (k.DocumentContent{Document: fileDoc, File: &ref, Unavailable: &unknown}).Validate())
	stale := must(oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.KnowledgeFile, ProjectID: d.ProjectID, DocumentID: d.ID.String(), Revision: 2}))
	requireError(t, (k.DocumentContent{Document: d, File: &stale}).Validate())
}
func TestReadDefaultsAndCurrentPath(t *testing.T) {
	var r k.ReadRequest
	requireOK(t, json.Unmarshal([]byte("{}"), &r))
	if r.MaxBytes != 65536 || r.ByteOffset != 0 {
		t.Fatal(r)
	}
	for _, b := range []string{`{"max_bytes":null}`, `{"max_bytes":0}`, `{"max_bytes":1048577}`, `{"byte_offset":1}`, `{"byte_offset":"-1"}`, `{"byte_offset":"0","byte_offset":"1"}`} {
		requireError(t, json.Unmarshal([]byte(b), &r))
	}
	root := doc(10)
	child := doc(11)
	child.ParentDocumentID = &root.ID
	hit := k.TitleHit{Document: child, Ancestors: []k.DocumentRef{root}}
	requireOK(t, hit.Validate())
	var decoded k.TitleHit
	roundTrip(t, hit, &decoded)
	hit.Ancestors = nil
	requireError(t, hit.Validate())
	hit.Ancestors = []k.DocumentRef{child, root}
	requireError(t, hit.Validate())
	filter := k.ListFilter{TitleQuery: "%_literal", SourceKind: ptr(k.File), MediaType: ptr(k.PDF)}
	requireOK(t, filter.Validate())
	var got k.ListFilter
	roundTrip(t, filter, &got)
	if !reflect.DeepEqual(filter, got) {
		t.Fatal("filter normalized")
	}
}
