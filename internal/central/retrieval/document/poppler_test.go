package document

import (
	"context"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/chunker"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/parser"
)

func pdfSource(t *testing.T) parser.SourceIdentity {
	t.Helper()
	p, _ := f.NewID[id.Project]()
	d, _ := f.NewID[kc.Document]()
	o, _ := f.NewID[oc.StoredObject]()
	return parser.SourceIdentity{ProjectID: p, DocumentID: d, ContentVersion: 1, ObjectID: o}
}
func TestPDFPageLocationsRetainExtractedByteCoordinates(t *testing.T) {
	text := "第一页 Mercury。\n\nAnother paragraph.\n\f\fThird page.\n\f"
	source := pdfSource(t)
	out, err := locatedPDF(context.Background(), source, "PDF source", text)
	if err != nil {
		t.Fatal(err)
	}
	chunks, err := chunker.Split(context.Background(), out, chunker.Config{SoftBytes: 128, HardBytes: 256})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 || chunks[0].Locator.Page != 1 || chunks[1].Locator.Page != 3 {
		t.Fatal("blank PDF page changed source locations")
	}
	for _, chunk := range chunks {
		if text[chunk.Locator.ByteStart:chunk.Locator.ByteEnd] != chunk.RawText || !strings.Contains(chunk.IndexText, "Page: ") {
			t.Fatal("PDF byte or page location lost")
		}
	}
	if _, err = locatedPDF(context.Background(), source, "image only", "\f\f"); err == nil {
		t.Fatal("empty extraction accepted as OCR")
	}
	if _, err = ParseBytes(context.Background(), source, "old profile", PDFMediaType, RegistryProfile, []byte("%PDF-1.4")); err == nil {
		t.Fatal("old profile silently discovered host PDF parser")
	}
}
func TestPopplerConfigurationRequiresAbsoluteLockedDistinctExecutables(t *testing.T) {
	valid := PopplerConfig{PDFToTextPath: "/usr/bin/pdftotext", PDFToTextSHA256: strings.Repeat("a", 64), PrlimitPath: "/usr/bin/prlimit", PrlimitSHA256: strings.Repeat("b", 64)}
	if valid.Validate() != nil {
		t.Fatal("valid explicit binaries rejected")
	}
	for _, mutate := range []func(*PopplerConfig){
		func(c *PopplerConfig) { c.PDFToTextPath = "pdftotext" },
		func(c *PopplerConfig) { c.PrlimitPath = c.PDFToTextPath },
		func(c *PopplerConfig) { c.PDFToTextSHA256 = "" },
		func(c *PopplerConfig) { c.PrlimitSHA256 = strings.Repeat("G", 64) },
	} {
		bad := valid
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatal("unsafe executable identity accepted")
		}
	}
}
