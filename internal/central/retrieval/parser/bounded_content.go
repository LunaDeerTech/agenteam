package parser

import (
	"context"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

// ParseBoundedContent consumes an already-returned D12 value, not a reader or an
// HTTP DTO. The D12 caller retains responsibility for authorization and the
// original read/transaction/Close outcome. This adapter does no I/O, continuation,
// retry or current-version check. A partial page never becomes a full document.
func ParseBoundedContent(ctx context.Context, request kc.ReadRequest, content kc.DocumentContent) (StructuredDocument, error) {
	if ctx == nil {
		return StructuredDocument{}, parseFault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return StructuredDocument{}, err
	}
	if request.Validate() != nil {
		return StructuredDocument{}, parseFault(f.InvalidArgument)
	}
	d := content.Document
	if d.MediaType != kc.PlainText {
		return StructuredDocument{}, parseFault(f.UnsupportedMediaType)
	}
	// Bound metadata validation too. D12 permits at most 512 title runes.
	// Do not call TextContent.Validate's whole-string UTF-8 scan here: the core
	// validates the original text with the same bounded cancellation checkpoints.
	if len(d.Title) > kc.MaxTitleRunes*utf8.UTFMax || d.Validate() != nil || d.Status != kc.Active || d.SourceKind != kc.Text || content.Text == nil || content.File != nil || content.Unavailable != nil {
		return StructuredDocument{}, parseFault(f.InvalidArgument)
	}
	t := content.Text
	if request.ByteOffset != 0 || t.Truncated || len(t.Text) > request.MaxBytes || len(t.Text) > MaxSourceBytes {
		return StructuredDocument{}, parseFault(f.PayloadTooLarge)
	}
	if t.NextByteOffset.Validate() != nil || t.NextByteOffset != f.Progress(len(t.Text)) {
		return StructuredDocument{}, parseFault(f.InvalidArgument)
	}
	return ParsePlainText(ctx, SourceIdentity{ProjectID: d.ProjectID, DocumentID: d.ID, ContentVersion: d.ContentVersion, ObjectID: d.ObjectID}, t.Text)
}
