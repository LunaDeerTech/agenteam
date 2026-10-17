// Package media validates bounded Chat inputs and encodes the supported
// OpenAI-compatible content parts. It grants no authority to read an object.
package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/document"
)

const (
	MaxAttachments     = 4
	MaxAttachmentBytes = 4 << 20
	MaxTextFileBytes   = 1 << 20
	MaxTotalBytes      = 8 << 20
	MaxRequestBytes    = 16 << 20
	MaxPromptBytes     = 16 << 10
	MaxAnswerBytes     = 8 << 10
)

// Attachment is the bounded administrative upload wire, not a Project file
// reference. Ordinary logging must not expose its contents.
type Attachment struct {
	Name       string `json:"name"`
	MediaType  string `json:"media_type"`
	DataBase64 string `json:"data_base64"`
}
type Input struct {
	Text        string       `json:"text"`
	Attachments []Attachment `json:"attachments"`
}

func (Attachment) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "chat_attachment") }
func (Attachment) LogValue() slog.Value       { return slog.StringValue("chat_attachment") }
func (Input) Format(w fmt.State, _ rune)      { _, _ = io.WriteString(w, "chat_input") }
func (Input) LogValue() slog.Value            { return slog.StringValue("chat_input") }

func ValidFilename(name string) bool {
	if name == "" || len(name) > 255 || !utf8.ValidString(name) || strings.TrimSpace(name) != name || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		return false
	}
	for _, c := range name {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}

func Image(mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return true
	}
	return false
}
func Limit(mime string) int {
	if Image(mime) || mime == "application/pdf" {
		return MaxAttachmentBytes
	}
	switch mime {
	case "text/plain", "text/markdown", document.DOCXMediaType:
		return MaxTextFileBytes
	}
	return 0
}

// Prepared contains owned, verified bytes only. It is intentionally redacted
// from JSON/logging and is not stored in a canonical model input or receipt.
type Prepared struct {
	name, mime, text string
	raw              []byte
}

func (p *Prepared) Digest() f.Digest {
	value, _ := f.ParseDigest(fmt.Sprintf("sha256:%x", sha256.Sum256(p.raw)))
	return value
}
func (p *Prepared) Name() string              { return p.name }
func (p *Prepared) MediaType() string         { return p.mime }
func (p *Prepared) Size() int                 { return len(p.raw) }
func (p *Prepared) Clear()                    { clear(p.raw); p.raw = nil; p.text = "" }
func (Prepared) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "prepared_chat_attachment") }
func (Prepared) LogValue() slog.Value         { return slog.StringValue("prepared_chat_attachment") }
func (Prepared) MarshalJSON() ([]byte, error) { return []byte(`"prepared_chat_attachment"`), nil }

func fault(code f.Code) error { return f.NewFault(code, f.NotStarted) }

func Prepare(ctx context.Context, name, mime string, raw []byte) (*Prepared, error) {
	if ctx == nil || !ValidFilename(name) || len(raw) == 0 {
		return nil, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limit := Limit(mime)
	if limit == 0 {
		return nil, fault(f.CapabilityUnsupported)
	}
	if len(raw) > limit {
		return nil, fault(f.PayloadTooLarge)
	}
	p := &Prepared{name: name, mime: mime}
	switch {
	case Image(mime):
		if http.DetectContentType(raw) != mime || mime == "image/gif" && !singleGIF(raw) {
			return nil, fault(f.InvalidArgument)
		}
	case mime == "application/pdf":
		if !bytes.HasPrefix(raw, []byte("%PDF-")) {
			return nil, fault(f.InvalidArgument)
		}
	case mime == document.DOCXMediaType:
		text, err := document.ExtractDOCXText(ctx, raw)
		if err != nil {
			return nil, err
		}
		p.text = text
	default:
		if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
			return nil, fault(f.InvalidArgument)
		}
		p.text = string(raw)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.raw = bytes.Clone(raw)
	return p, nil
}

func PrepareInput(ctx context.Context, input Input) ([]*Prepared, error) {
	if ctx == nil || !utf8.ValidString(input.Text) || strings.ContainsRune(input.Text, 0) || len(input.Text) > MaxPromptBytes || len(input.Attachments) > MaxAttachments || strings.TrimSpace(input.Text) == "" && len(input.Attachments) == 0 {
		return nil, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]*Prepared, 0, len(input.Attachments))
	fail := func(err error) ([]*Prepared, error) {
		for _, p := range out {
			p.Clear()
		}
		return nil, err
	}
	total := 0
	for _, attachment := range input.Attachments {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		limit := Limit(attachment.MediaType)
		if limit == 0 {
			return fail(fault(f.CapabilityUnsupported))
		}
		if len(attachment.DataBase64) > base64.StdEncoding.EncodedLen(limit) {
			return fail(fault(f.PayloadTooLarge))
		}
		if strings.ContainsAny(attachment.DataBase64, "\r\n") {
			return fail(fault(f.InvalidArgument))
		}
		raw, err := base64.StdEncoding.Strict().DecodeString(attachment.DataBase64)
		if err != nil {
			return fail(fault(f.InvalidArgument))
		}
		total += len(raw)
		if total > MaxTotalBytes {
			clear(raw)
			return fail(fault(f.PayloadTooLarge))
		}
		p, err := Prepare(ctx, attachment.Name, attachment.MediaType, raw)
		clear(raw)
		if err != nil {
			return fail(err)
		}
		out = append(out, p)
	}
	return out, nil
}

type ImageURL struct {
	URL string `json:"url"`
}
type File struct {
	Filename string `json:"filename"`
	FileData string `json:"file_data"`
}
type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
	File     *File     `json:"file,omitempty"`
}

func Text(text string) ContentPart { return ContentPart{Type: "text", Text: text} }

func (p *Prepared) Content(ctx context.Context) (ContentPart, error) {
	if ctx == nil || p == nil || len(p.raw) == 0 {
		return ContentPart{}, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return ContentPart{}, err
	}
	if Image(p.mime) {
		return ContentPart{Type: "image_url", ImageURL: &ImageURL{URL: "data:" + p.mime + ";base64," + base64.StdEncoding.EncodeToString(p.raw)}}, nil
	}
	if p.mime == "application/pdf" {
		return ContentPart{Type: "file", File: &File{Filename: p.name, FileData: "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(p.raw)}}, nil
	}
	return Text("Untrusted file attachment " + p.name + " (extracted text):\n" + p.text), nil
}

// Inspect GIF blocks without decoding compressed pixels or allocating frames.
// An animated file is not silently flattened to its first frame.
func singleGIF(raw []byte) bool {
	if len(raw) < 14 || string(raw[:6]) != "GIF87a" && string(raw[:6]) != "GIF89a" {
		return false
	}
	at := 13
	if raw[10]&0x80 != 0 {
		at += 3 * (1 << ((raw[10] & 7) + 1))
	}
	frames := 0
	blocks := func() bool {
		for at < len(raw) {
			n := int(raw[at])
			at++
			if n == 0 {
				return true
			}
			if n > len(raw)-at {
				return false
			}
			at += n
		}
		return false
	}
	for at < len(raw) {
		kind := raw[at]
		at++
		switch kind {
		case 0x3b:
			return frames == 1 && at == len(raw)
		case 0x21:
			if at >= len(raw) {
				return false
			}
			at++
			if !blocks() {
				return false
			}
		case 0x2c:
			frames++
			if frames > 1 || len(raw)-at < 9 {
				return false
			}
			if binary.LittleEndian.Uint16(raw[at+4:]) == 0 || binary.LittleEndian.Uint16(raw[at+6:]) == 0 {
				return false
			}
			packed := raw[at+8]
			at += 9
			if packed&0x80 != 0 {
				at += 3 * (1 << ((packed & 7) + 1))
			}
			if at >= len(raw) {
				return false
			}
			at++
			if !blocks() {
				return false
			}
		default:
			return false
		}
	}
	return false
}
