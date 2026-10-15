// Package chunker creates deterministic, source-located retrieval chunks.
package chunker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/parser"
)

const Profile = "section_bytes:v1"

type Config struct {
	SoftBytes, HardBytes, ForcedOverlapBytes int
}

func (c Config) Validate() error {
	if c.SoftBytes < utf8.UTFMax || c.HardBytes < c.SoftBytes || c.HardBytes > parser.MaxSourceBytes || c.ForcedOverlapBytes < 0 || c.ForcedOverlapBytes >= c.HardBytes-utf8.UTFMax {
		return invalid()
	}
	return nil
}

type Element struct {
	Kind        string
	Ordinal     int
	Bytes       parser.ByteRange
	HeadingPath []string
	Page        int
}

type Document struct {
	Source        parser.SourceIdentity
	Title         string
	ParserProfile string
	Text          string
	Elements      []Element
}

type Locator struct {
	Ordinal      int      `json:"ordinal"`
	HeadingPath  []string `json:"heading_path,omitempty"`
	Page         int      `json:"page,omitempty"`
	ElementStart int      `json:"element_start"`
	ElementEnd   int      `json:"element_end"`
	ByteStart    int      `json:"byte_start"`
	ByteEnd      int      `json:"byte_end"`
	ForcedSplit  bool     `json:"forced_split,omitempty"`
}

type Chunk struct {
	RawText   string
	IndexText string
	Locator   Locator
}

func FromPlain(document parser.StructuredDocument, title, original string) (Document, error) {
	if document.ParserProfile != parser.PlainTextProfile || document.SourceBytes != len(original) || document.Source.Validate() != nil {
		return Document{}, invalid()
	}
	out := Document{Source: document.Source, Title: title, ParserProfile: document.ParserProfile, Text: original, Elements: make([]Element, 0, len(document.Elements))}
	for _, element := range document.Elements {
		if element.Bytes.Start < 0 || element.Bytes.End < element.Bytes.Start || element.Bytes.End > len(original) || original[element.Bytes.Start:element.Bytes.End] != element.Text {
			return Document{}, invalid()
		}
		out.Elements = append(out.Elements, Element{Kind: string(element.Kind), Ordinal: element.Ordinal, Bytes: element.Bytes})
	}
	return out, nil
}

func Split(ctx context.Context, document Document, config Config) ([]Chunk, error) {
	if ctx == nil || config.Validate() != nil || document.Source.Validate() != nil || len(document.Text) > parser.MaxSourceBytes || !utf8.ValidString(document.Text) || !utf8.ValidString(document.Title) || len(document.Title) > 2048 || len(document.Elements) > parser.MaxParagraphs || document.ParserProfile == "" {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for n, element := range document.Elements {
		if element.Ordinal != n+1 || element.Kind == "" || element.Bytes.Start < 0 || element.Bytes.End <= element.Bytes.Start || element.Bytes.End > len(document.Text) || !boundary(document.Text, element.Bytes.Start) || !boundary(document.Text, element.Bytes.End) || n > 0 && element.Bytes.Start < document.Elements[n-1].Bytes.End || element.Page < 0 || len(element.HeadingPath) > 32 {
			return nil, invalid()
		}
		for _, heading := range element.HeadingPath {
			if len(heading) == 0 || len(heading) > 2048 || !utf8.ValidString(heading) {
				return nil, invalid()
			}
		}
	}
	out := make([]Chunk, 0)
	emit := func(first, last Element, start, end int, forced bool) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(out) == 65536 {
			return f.NewFault(f.PayloadTooLarge, f.NotStarted)
		}
		locator := Locator{Ordinal: len(out) + 1, HeadingPath: append([]string(nil), first.HeadingPath...), Page: first.Page, ElementStart: first.Ordinal, ElementEnd: last.Ordinal, ByteStart: start, ByteEnd: end, ForcedSplit: forced}
		raw := document.Text[start:end]
		prefix := "Document: " + document.Title + "\n"
		if len(first.HeadingPath) != 0 {
			prefix += "Section: " + strings.Join(first.HeadingPath, " > ") + "\n"
		}
		if first.Page != 0 {
			prefix += "Page: " + strconv.Itoa(first.Page) + "\n"
		}
		if len(prefix) > 16<<10 {
			return f.NewFault(f.PayloadTooLarge, f.NotStarted)
		}
		out = append(out, Chunk{RawText: raw, IndexText: prefix + "\n" + raw, Locator: locator})
		return nil
	}
	for at := 0; at < len(document.Elements); {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		first := document.Elements[at]
		if first.Bytes.End-first.Bytes.Start > config.HardBytes {
			for start := first.Bytes.Start; start < first.Bytes.End; {
				end := min(start+config.HardBytes, first.Bytes.End)
				for !boundary(document.Text, end) {
					end--
				}
				if err := emit(first, first, start, end, true); err != nil {
					return nil, err
				}
				if end == first.Bytes.End {
					break
				}
				next := end - config.ForcedOverlapBytes
				for !boundary(document.Text, next) {
					next++
				}
				if next <= start {
					return nil, invalid()
				}
				start = next
			}
			at++
			continue
		}
		last := at
		for last+1 < len(document.Elements) && document.Elements[last].Bytes.End-first.Bytes.Start < config.SoftBytes {
			next := document.Elements[last+1]
			if !compatible(first, next) || next.Bytes.End-first.Bytes.Start > config.HardBytes {
				break
			}
			last++
		}
		if err := emit(first, document.Elements[last], first.Bytes.Start, document.Elements[last].Bytes.End, false); err != nil {
			return nil, err
		}
		at = last + 1
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func compatible(a, b Element) bool {
	if a.Page != b.Page || !slices.Equal(a.HeadingPath, b.HeadingPath) {
		return false
	}
	return a.Kind == b.Kind && (a.Kind == "paragraph" || a.Kind == "list")
}
func boundary(value string, at int) bool {
	return at == len(value) || at >= 0 && at < len(value) && utf8.RuneStart(value[at])
}
func invalid() error                          { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func (Document) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "chunk_document") }
func (Document) LogValue() slog.Value         { return slog.StringValue("chunk_document") }
func (Document) MarshalJSON() ([]byte, error) { return []byte(`"chunk_document"`), nil }
func (Chunk) Format(w fmt.State, _ rune)      { _, _ = io.WriteString(w, "retrieval_chunk") }
func (Chunk) LogValue() slog.Value            { return slog.StringValue("retrieval_chunk") }
func (Chunk) MarshalJSON() ([]byte, error)    { return []byte(`"retrieval_chunk"`), nil }
