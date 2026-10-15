package document

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/chunker"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/parser"
	"testing"
)

func TestMarkdownPreservesLocatedStructure(t *testing.T) {
	p, _ := f.NewID[id.Project]()
	d, _ := f.NewID[kc.Document]()
	o, _ := f.NewID[oc.StoredObject]()
	text := "# 原题\r\n\r\n第一段。\r\n\r\n## 索引\n- a\n- b\n\n```go\nx := 1\n```\n\n| a | b |\n|---|---|\n| 甲 | 乙 |\n"
	out, err := Parse(context.Background(), parser.SourceIdentity{ProjectID: p, DocumentID: d, ObjectID: o, ContentVersion: 1}, "文件", "text/markdown", text)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"heading", "paragraph", "heading", "list", "code", "table"}
	if len(out.Elements) != len(want) {
		t.Fatal(len(out.Elements))
	}
	for n, e := range out.Elements {
		if e.Kind != want[n] || e.Bytes.End <= e.Bytes.Start {
			t.Fatalf("element %d", n)
		}
	}
	if got := out.Elements[4].HeadingPath; len(got) != 2 || got[1] != "索引" {
		t.Fatal(got)
	}
	chunks, err := chunker.Split(context.Background(), out, chunker.Config{SoftBytes: 128, HardBytes: 256})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range chunks {
		if text[c.Locator.ByteStart:c.Locator.ByteEnd] != c.RawText {
			t.Fatal("source drift")
		}
	}
}
