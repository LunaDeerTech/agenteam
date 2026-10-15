package chunker

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/parser"
)

func source(t *testing.T) parser.SourceIdentity {
	t.Helper()
	p, _ := f.ParseID[id.Project]("018f0000-0000-7000-8000-000000000001")
	d, _ := f.ParseID[kc.Document]("018f0000-0000-7000-8000-000000000002")
	o, _ := f.ParseID[oc.StoredObject]("018f0000-0000-7000-8000-000000000003")
	return parser.SourceIdentity{ProjectID: p, DocumentID: d, ContentVersion: 2, ObjectID: o}
}

func TestChunksKeepCanonicalBytesAndSeparateContext(t *testing.T) {
	text := "甲。\r\n乙！\r\n\r\nA. B?"
	parsed, err := parser.ParsePlainText(context.Background(), source(t), text)
	if err != nil {
		t.Fatal(err)
	}
	document, err := FromPlain(parsed, "文档", text)
	if err != nil {
		t.Fatal(err)
	}
	chunks, err := Split(context.Background(), document, Config{SoftBytes: 10, HardBytes: 20, ForcedOverlapBytes: 4})
	if err != nil || len(chunks) != 2 {
		t.Fatalf("split: %v, count %d", err, len(chunks))
	}
	if chunks[0].RawText != text[0:14] || chunks[1].RawText != text[18:23] || chunks[0].Locator.ByteEnd != 14 || chunks[1].Locator.ByteStart != 18 {
		t.Fatal("canonical locator/raw bytes changed")
	}
	for _, chunk := range chunks {
		if chunk.Locator.ForcedSplit || strings.HasPrefix(chunk.RawText, "Document:") || chunk.IndexText != "Document: 文档\n\n"+chunk.RawText {
			t.Fatal("normal boundaries overlapped or prefix contaminated source")
		}
	}
}

func TestForcedSplitRespectsUTF8AndSectionBoundaries(t *testing.T) {
	text := strings.Repeat("甲", 12) + "\n\n" + "second"
	d := Document{Source: source(t), Title: "title", ParserProfile: "test", Text: text, Elements: []Element{
		{Kind: "paragraph", Ordinal: 1, Bytes: parser.ByteRange{Start: 0, End: 36}, HeadingPath: []string{"一"}},
		{Kind: "paragraph", Ordinal: 2, Bytes: parser.ByteRange{Start: 38, End: 44}, HeadingPath: []string{"二"}},
	}}
	chunks, err := Split(context.Background(), d, Config{SoftBytes: 8, HardBytes: 11, ForcedOverlapBytes: 4})
	if err != nil || len(chunks) < 2 {
		t.Fatal(err)
	}
	for n, chunk := range chunks {
		if !utf8.ValidString(chunk.RawText) || len(chunk.RawText) > 11 || chunk.RawText != text[chunk.Locator.ByteStart:chunk.Locator.ByteEnd] {
			t.Fatal("bad forced split locator")
		}
		if n+1 < len(chunks) && chunks[n+1].Locator.ElementStart == chunk.Locator.ElementStart && chunks[n+1].Locator.ByteStart >= chunk.Locator.ByteEnd {
			t.Fatal("forced overlap missing")
		}
	}
	last := chunks[len(chunks)-1]
	if last.RawText != "second" || last.Locator.ForcedSplit || last.Locator.HeadingPath[0] != "二" {
		t.Fatal("section boundary lost")
	}
}
