// Package parser derives bounded, version-bound text structure. It performs no
// I/O or authorization and does not publish a current or serving projection.
package parser

import (
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

const (
	PlainTextProfile = "plain_text:v1"
	MaxSourceBytes   = 1 << 20
	MaxParagraphs    = 16384
	MaxSentences     = 65536
)

// SourceIdentity is copied from the exact canonical content being parsed.
// ObjectID is mandatory in D12's DocumentRef. This value is not an authority.
type SourceIdentity struct {
	ProjectID      id.ProjectID
	DocumentID     kc.DocumentID
	ContentVersion f.Version
	ObjectID       oc.ObjectID
}

func (s SourceIdentity) Validate() error {
	if s.ProjectID.Validate() != nil || s.DocumentID.Validate() != nil || s.ContentVersion.Validate() != nil || s.ObjectID.Validate() != nil {
		return parseFault(f.InvalidArgument)
	}
	return nil
}

// ByteRange is a zero-based, half-open range in the original UTF-8 bytes, not
// rune, UTF-16, grapheme or normalized-text coordinates. Sentence ranges use
// document coordinates too; their ordered union is the paragraph's range.
type ByteRange struct {
	Start int
	End   int
}

type ElementKind string

const Paragraph ElementKind = "paragraph"

type StructuredElement struct {
	Kind      ElementKind
	Ordinal   int // One-based document order.
	Text      string
	Bytes     ByteRange
	Sentences []ByteRange
}

// StructuredDocument is an in-memory derived result. Empty/blank input has zero
// elements but retains SourceBytes. Separating blank lines are not elements.
// Callers must revalidate the source version before any future publication.
type StructuredDocument struct {
	Source        SourceIdentity
	ParserProfile string
	SourceBytes   int
	Elements      []StructuredElement
}

// Text remains explicitly accessible to consumers, never implicit in logs.
func (StructuredElement) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "parsed_paragraph") }
func (StructuredElement) LogValue() slog.Value       { return slog.StringValue("parsed_paragraph") }
func (StructuredDocument) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "parsed_document")
}
func (StructuredDocument) LogValue() slog.Value { return slog.StringValue("parsed_document") }

func parseFault(code f.Code) error { return f.NewFault(code, f.NotStarted) }
