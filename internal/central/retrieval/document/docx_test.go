package document

import (
	"archive/zip"
	"bytes"
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/chunker"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/parser"
	"strings"
	"testing"
)

func docxFixture(t *testing.T, document, styles string) []byte {
	t.Helper()
	var raw bytes.Buffer
	w := zip.NewWriter(&raw)
	for name, value := range map[string]string{"word/document.xml": document, "word/styles.xml": styles} {
		if value == "" {
			continue
		}
		file, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return raw.Bytes()
}
func TestDOCXStructuralTextAndNoInventedPages(t *testing.T) {
	p, _ := f.NewID[id.Project]()
	d, _ := f.NewID[kc.Document]()
	o, _ := f.NewID[oc.StoredObject]()
	source := parser.SourceIdentity{ProjectID: p, DocumentID: d, ContentVersion: 1, ObjectID: o}
	xml := `<w:document xmlns:w="` + wordNamespace + `"><w:body><w:p><w:pPr><w:pStyle w:val="custom-section"/></w:pPr><w:r><w:t>章节</w:t></w:r></w:p><w:p><w:r><w:t xml:space="preserve">原文 </w:t><w:tab/><w:t>正文</w:t></w:r><w:del><w:r><w:t>已删文字</w:t></w:r></w:del></w:p><w:tbl><w:tr><w:tc><w:p><w:r><w:t>列一</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>列二</w:t></w:r></w:p></w:tc></w:tr></w:tbl></w:body></w:document>`
	styles := `<w:styles xmlns:w="` + wordNamespace + `"><w:style w:styleId="custom-section"><w:pPr><w:outlineLvl w:val="1"/></w:pPr></w:style></w:styles>`
	out, err := ParseDOCX(context.Background(), source, "Word文件", docxFixture(t, xml, styles))
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != "章节\n\n原文 \t正文\n\n列一\t列二" || len(out.Elements) != 3 || out.Elements[0].Kind != "heading" || out.Elements[2].Kind != "table" {
		t.Fatal("DOCX structure differs")
	}
	for _, e := range out.Elements {
		if e.Page != 0 || len(e.HeadingPath) != 1 || e.HeadingPath[0] != "章节" {
			t.Fatal("invented page or lost style heading")
		}
	}
	chunks, err := chunker.Split(context.Background(), out, chunker.Config{SoftBytes: 128, HardBytes: 256})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range chunks {
		if strings.Contains(c.RawText, "已删文字") || out.Text[c.Locator.ByteStart:c.Locator.ByteEnd] != c.RawText {
			t.Fatal("deleted text or wrong extracted range")
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ParseDOCX(cancelled, source, "Word", docxFixture(t, xml, styles)); err == nil {
		t.Fatal("cancellation ignored")
	}
}
