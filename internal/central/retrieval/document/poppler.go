package document

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/chunker"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/parser"
)

const (
	PDFMediaType           = "application/pdf"
	PDFProfile             = "pdf_poppler_25.03.0:v1"
	PopplerRegistryProfile = "plain-markdown-docx-pdf-poppler:v1"
	PopplerVersion         = "25.03.0"
	PrlimitVersion         = "2.41.5"
)

// Explicit deployment binaries are verified before every launch. SHA256 locks
// the executable bytes; dynamic-library packaging remains a deployment duty.
type PopplerConfig struct {
	PDFToTextPath, PDFToTextSHA256 string
	PrlimitPath, PrlimitSHA256     string
}

func (c PopplerConfig) Validate() error {
	for path, digest := range map[string]string{c.PDFToTextPath: c.PDFToTextSHA256, c.PrlimitPath: c.PrlimitSHA256} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsRune(path, 0) || len(path) > 4096 || len(digest) != 64 || strings.ToLower(digest) != digest {
			return invalid()
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return invalid()
		}
	}
	if c.PDFToTextPath == c.PrlimitPath {
		return invalid()
	}
	return nil
}

type Poppler struct{ config PopplerConfig }

func NewPoppler(ctx context.Context, c PopplerConfig) (*Poppler, error) {
	if ctx == nil || c.Validate() != nil {
		return nil, invalid()
	}
	p := &Poppler{config: c}
	if err := p.verifyBinaries(); err != nil {
		return nil, err
	}
	for _, probe := range []struct{ path, argument, want string }{
		{c.PDFToTextPath, "-v", "pdftotext version " + PopplerVersion},
		{c.PrlimitPath, "--version", "prlimit from util-linux " + PrlimitVersion},
	} {
		out, stderr, err := runPopplerProcess(ctx, c, probe.path, []string{probe.argument}, nil, 4096, 3*time.Second)
		if err != nil {
			return nil, err
		}
		first := strings.SplitN(strings.TrimSpace(string(out)+string(stderr)), "\n", 2)[0]
		if first != probe.want {
			return nil, f.NewFault(f.CapabilityUnsupported, f.NotStarted)
		}
	}
	return p, nil
}
func (p *Poppler) verifyBinaries() error {
	if p == nil || p.config.Validate() != nil {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	for path, digest := range map[string]string{p.config.PDFToTextPath: p.config.PDFToTextSHA256, p.config.PrlimitPath: p.config.PrlimitSHA256} {
		file, err := os.Open(path)
		if err != nil {
			return f.NewFault(f.DependencyUnavailable, f.NotStarted)
		}
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Size() > 64<<20 {
			_ = file.Close()
			return f.NewFault(f.DependencyUnavailable, f.NotStarted)
		}
		hash := sha256.New()
		_, readErr := io.Copy(hash, io.LimitReader(file, (64<<20)+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || hex.EncodeToString(hash.Sum(nil)) != digest {
			return f.NewFault(f.DependencyUnavailable, f.NotStarted)
		}
	}
	return nil
}

func (p *Poppler) Parse(ctx context.Context, source parser.SourceIdentity, title string, raw []byte) (chunker.Document, error) {
	if ctx == nil || source.Validate() != nil {
		return chunker.Document{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return chunker.Document{}, err
	}
	if len(raw) > parser.MaxSourceBytes {
		return chunker.Document{}, f.NewFault(f.PayloadTooLarge, f.NotStarted)
	}
	if err := p.verifyBinaries(); err != nil {
		return chunker.Document{}, err
	}
	out, _, err := runPopplerProcess(ctx, p.config, p.config.PDFToTextPath, []string{"-enc", "UTF-8", "-eol", "unix", "-", "-"}, raw, parser.MaxSourceBytes, 15*time.Second)
	if err != nil {
		return chunker.Document{}, err
	}
	defer clear(out)
	return locatedPDF(ctx, source, title, string(out))
}

func locatedPDF(ctx context.Context, source parser.SourceIdentity, title, text string) (chunker.Document, error) {
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return chunker.Document{}, invalid()
	}
	out := chunker.Document{Source: source, Title: title, ParserProfile: PDFProfile, Text: text, Elements: []chunker.Element{}}
	offset, page := 0, 1
	for _, part := range strings.Split(text, "\f") {
		parsed, err := parser.ParsePlainText(ctx, source, part)
		if err != nil {
			return chunker.Document{}, err
		}
		for _, element := range parsed.Elements {
			if len(out.Elements) >= parser.MaxParagraphs {
				return chunker.Document{}, f.NewFault(f.PayloadTooLarge, f.NotStarted)
			}
			out.Elements = append(out.Elements, chunker.Element{Kind: "paragraph", Ordinal: len(out.Elements) + 1, Bytes: parser.ByteRange{Start: offset + element.Bytes.Start, End: offset + element.Bytes.End}, Page: page})
		}
		offset += len(part) + 1
		page++
	}
	if len(out.Elements) == 0 {
		return chunker.Document{}, f.NewFault(f.CapabilityUnsupported, f.NotStarted)
	} // No OCR is configured.
	return out, nil
}

// Registry binds the external process explicitly. The historical ParseBytes
// function retains its exact old format set and never discovers host tools.
type Registry struct{ pdf *Poppler }

func NewRegistry(pdf *Poppler) (*Registry, error) {
	if pdf == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &Registry{pdf: pdf}, nil
}
func (r *Registry) ParseBytes(ctx context.Context, source parser.SourceIdentity, title, media, profile string, raw []byte) (chunker.Document, error) {
	if profile != PopplerRegistryProfile {
		return ParseBytes(ctx, source, title, media, profile, raw)
	}
	if r == nil || r.pdf == nil {
		return chunker.Document{}, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if media == PDFMediaType {
		return r.pdf.Parse(ctx, source, title, raw)
	}
	return ParseBytes(ctx, source, title, media, RegistryProfile, raw)
}
