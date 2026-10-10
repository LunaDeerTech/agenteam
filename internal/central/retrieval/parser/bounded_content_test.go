package parser_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	p "github.com/LunaDeerTech/agenteam/internal/central/retrieval/parser"
)

func content(text string) kc.DocumentContent {
	s := source()
	at := must(f.ParseInstant("2026-10-10T00:00:00Z"))
	creator := must(kc.NewCreatorRef(kc.CreatorDetails{Kind: id.Human, UserID: key[id.User](5)}))
	return kc.DocumentContent{
		Document: kc.DocumentRef{ID: s.DocumentID, ProjectID: s.ProjectID, Title: "plain fixture", ContentVersion: s.ContentVersion, SourceKind: kc.Text, MediaType: kc.PlainText, ObjectID: s.ObjectID, Status: kc.Active, IndexingStatus: kc.IndexPending, CreatedBy: creator, CreatedAt: at, UpdatedAt: at},
		Text:     &kc.TextContent{Text: text, NextByteOffset: f.Progress(len(text))},
	}
}

func TestBoundedContentUsesTheActualD12Value(t *testing.T) {
	for _, raw := range []string{"", "原文。\r\nNext!", strings.Repeat("x", kc.MaxReadBytes)} {
		c := content(raw)
		if err := c.Validate(); err != nil {
			t.Fatal("invalid D12 fixture", err)
		}
		q := kc.ReadRequest{MaxBytes: kc.MaxReadBytes}
		got := must(p.ParseBoundedContent(context.Background(), q, c))
		want := must(p.ParsePlainText(context.Background(), source(), raw))
		if !reflect.DeepEqual(got, want) {
			t.Fatal("D12 adapter changed exact source or text")
		}
		c.Document.ContentVersion++
		c.Document.ObjectID = key[oc.StoredObject](9)
		got = must(p.ParseBoundedContent(context.Background(), q, c))
		if got.Source.ContentVersion != c.Document.ContentVersion || got.Source.ObjectID != c.Document.ObjectID {
			t.Fatal("used an earlier metadata read instead of the actual D12 result")
		}
	}
}

func TestBoundedContentRejectsPartialAndMalformedValues(t *testing.T) {
	tests := []struct {
		name string
		edit func(*kc.ReadRequest, *kc.DocumentContent)
		code f.Code
	}{
		{"negative offset", func(q *kc.ReadRequest, _ *kc.DocumentContent) { q.ByteOffset = -1 }, f.InvalidArgument},
		{"zero budget", func(q *kc.ReadRequest, _ *kc.DocumentContent) { q.MaxBytes = 0 }, f.InvalidArgument},
		{"oversized request", func(q *kc.ReadRequest, _ *kc.DocumentContent) { q.MaxBytes = kc.MaxReadBytes + 1 }, f.InvalidArgument},
		{"continuation page", func(q *kc.ReadRequest, c *kc.DocumentContent) { q.ByteOffset = 4; c.Text.NextByteOffset += 4 }, f.PayloadTooLarge},
		{"truncated", func(_ *kc.ReadRequest, c *kc.DocumentContent) { c.Text.Truncated = true }, f.PayloadTooLarge},
		{"truncated malformed next", func(_ *kc.ReadRequest, c *kc.DocumentContent) { c.Text.Truncated = true; c.Text.NextByteOffset = -1 }, f.InvalidArgument},
		{"truncated malformed utf8", func(_ *kc.ReadRequest, c *kc.DocumentContent) {
			c.Text.Truncated = true
			c.Text.Text = "\xff"
			c.Text.NextByteOffset = 1
		}, f.InvalidArgument},
		{"budget overflow", func(q *kc.ReadRequest, _ *kc.DocumentContent) { q.MaxBytes = 1 }, f.PayloadTooLarge},
		{"wrong next", func(_ *kc.ReadRequest, c *kc.DocumentContent) { c.Text.NextByteOffset++ }, f.InvalidArgument},
		{"negative next", func(_ *kc.ReadRequest, c *kc.DocumentContent) { c.Text.NextByteOffset = -1 }, f.InvalidArgument},
		{"invalid utf8", func(_ *kc.ReadRequest, c *kc.DocumentContent) { c.Text.Text = "\xff"; c.Text.NextByteOffset = 1 }, f.InvalidArgument},
		{"missing text", func(_ *kc.ReadRequest, c *kc.DocumentContent) { c.Text = nil }, f.InvalidArgument},
		{"unavailable union", func(_ *kc.ReadRequest, c *kc.DocumentContent) { x := kc.ReadableUnbound; c.Unavailable = &x }, f.InvalidArgument},
		{"file union", func(_ *kc.ReadRequest, c *kc.DocumentContent) { c.File = new(oc.BusinessFileRef) }, f.InvalidArgument},
		{"nonplain classification precedes union", func(_ *kc.ReadRequest, c *kc.DocumentContent) {
			c.Document.MediaType = kc.Markdown
			c.File = new(oc.BusinessFileRef)
		}, f.UnsupportedMediaType},
		{"deleted", func(_ *kc.ReadRequest, c *kc.DocumentContent) { c.Document.Status = kc.Deleted }, f.InvalidArgument},
		{"source kind", func(_ *kc.ReadRequest, c *kc.DocumentContent) { c.Document.SourceKind = kc.File }, f.InvalidArgument},
		{"object id", func(_ *kc.ReadRequest, c *kc.DocumentContent) { c.Document.ObjectID = oc.ObjectID{} }, f.InvalidArgument},
		{"project id", func(_ *kc.ReadRequest, c *kc.DocumentContent) { c.Document.ProjectID = id.ProjectID{} }, f.InvalidArgument},
		{"document id", func(_ *kc.ReadRequest, c *kc.DocumentContent) { c.Document.ID = kc.DocumentID{} }, f.InvalidArgument},
		{"version", func(_ *kc.ReadRequest, c *kc.DocumentContent) { c.Document.ContentVersion = 0 }, f.InvalidArgument},
		{"self parent", func(_ *kc.ReadRequest, c *kc.DocumentContent) { x := c.Document.ID; c.Document.ParentDocumentID = &x }, f.InvalidArgument},
		{"bad title", func(_ *kc.ReadRequest, c *kc.DocumentContent) { c.Document.Title = "\xff" }, f.InvalidArgument},
		{"unbounded title", func(_ *kc.ReadRequest, c *kc.DocumentContent) { c.Document.Title = strings.Repeat("x", 2049) }, f.InvalidArgument},
		{"indexing state", func(_ *kc.ReadRequest, c *kc.DocumentContent) { c.Document.IndexingStatus = "future" }, f.InvalidArgument},
		{"creator", func(_ *kc.ReadRequest, c *kc.DocumentContent) { c.Document.CreatedBy = kc.CreatorRef{} }, f.InvalidArgument},
		{"time order", func(_ *kc.ReadRequest, c *kc.DocumentContent) {
			c.Document.UpdatedAt = must(f.ParseInstant("2026-10-09T00:00:00Z"))
		}, f.InvalidArgument},
		{"non-plain", func(_ *kc.ReadRequest, c *kc.DocumentContent) { c.Document.MediaType = kc.Markdown }, f.UnsupportedMediaType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, c := kc.DefaultReadRequest(), content("甲。")
			tt.edit(&q, &c)
			got, err := p.ParseBoundedContent(context.Background(), q, c)
			wantFault(t, got, err, tt.code)
		})
	}
}

func TestBoundedContentCancellationAndLiteralMetadata(t *testing.T) {
	c := content("正文 unchanged.")
	c.Document.Title = " 原始 title é "
	q := kc.DefaultReadRequest()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := p.ParseBoundedContent(ctx, q, c)
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, p.StructuredDocument{}) {
		t.Fatal("canceled value conversion published a result")
	}
	got, err = p.ParseBoundedContent(nil, q, c)
	wantFault(t, got, err, f.InvalidArgument)
	// CreatorRef contains a closure, so reflect.DeepEqual is never a valid
	// equality oracle for a populated DocumentRef. Compare its actual wire value.
	before := must(json.Marshal(c.Document))
	got = must(p.ParseBoundedContent(context.Background(), q, c))
	if !bytes.Equal(must(json.Marshal(c.Document)), before) || got.Source != source() || got.Elements[0].Text != c.Text.Text {
		t.Fatal("conversion mutated canonical metadata or content")
	}
}
