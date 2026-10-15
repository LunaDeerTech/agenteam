package document

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/chunker"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/parser"
)

const RegistryProfile = "plain-markdown-docx:v1"
const DOCXMediaType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

// ParseBytes freezes the format set in the profile. Enabling a new adapter is
// an indexing-profile change, never a silent reinterpretation of old jobs.
func ParseBytes(ctx context.Context, source parser.SourceIdentity, title, media, profile string, raw []byte) (chunker.Document, error) {
	if profile != Profile && profile != RegistryProfile {
		return chunker.Document{}, f.NewFault(f.CapabilityUnsupported, f.NotStarted)
	}
	if media == DOCXMediaType {
		if profile != RegistryProfile {
			return chunker.Document{}, f.NewFault(f.CapabilityUnsupported, f.NotStarted)
		}
		return ParseDOCX(ctx, source, title, raw)
	}
	return Parse(ctx, source, title, media, string(raw))
}
