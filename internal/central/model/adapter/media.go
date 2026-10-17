package adapter

import (
	"context"
	"strings"
	"unicode/utf8"

	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model/media"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// PreparedMedia is an attempt-owned encoding input. The business owner has
// already authorized and verified its bytes; it is never a canonical input.
type PreparedMedia struct {
	Part  mc.MediaPart
	Value *media.Prepared
}

type mediaEncoder struct {
	prepared []PreparedMedia
	content  []*media.ContentPart
	used     []bool
	caps     mc.Capabilities
}

func sameMediaRef(a, b oc.BusinessFileRef) bool {
	x, y := a.Details(), b.Details()
	if x.Kind != y.Kind || x.ProjectID != y.ProjectID || x.ArtifactID != y.ArtifactID || x.FileID != y.FileID || x.DocumentID != y.DocumentID || x.ExecutionID != y.ExecutionID || x.PayloadID != y.PayloadID || x.Revision != y.Revision {
		return false
	}
	if x.Kind != oc.UploadedObject {
		return true
	}
	r, s := x.Receipt.Details(), y.Receipt.Details()
	return r.ID == s.ID && r.UploadID == s.UploadID && r.ObjectID == s.ObjectID && r.CreationCause == s.CreationCause && r.Owner.Equal(s.Owner)
}

func newMediaEncoder(prepared []PreparedMedia, caps mc.Capabilities) (*mediaEncoder, error) {
	if len(prepared) > media.MaxAttachments {
		return nil, limitFailure()
	}
	total := 0
	for i, p := range prepared {
		if p.Part.Validate() != nil || p.Value == nil || p.Value.Size() == 0 || p.Value.MediaType() != p.Part.MediaType {
			return nil, invalid()
		}
		total += p.Value.Size()
		if total > media.MaxTotalBytes {
			return nil, limitFailure()
		}
		for _, previous := range prepared[:i] {
			if sameMediaRef(previous.Part.Ref, p.Part.Ref) {
				return nil, invalid()
			}
		}
	}
	return &mediaEncoder{prepared: prepared, content: make([]*media.ContentPart, len(prepared)), used: make([]bool, len(prepared)), caps: caps}, nil
}

func (e *mediaEncoder) part(ctx context.Context, part *mc.MediaPart, image bool) (media.ContentPart, error) {
	if part == nil || part.Validate() != nil {
		return media.ContentPart{}, invalid()
	}
	kind := "file"
	if image {
		kind = "image"
	}
	if image != media.Image(part.MediaType) || !stringsIn(e.caps.InputModalities, kind) {
		return media.ContentPart{}, unsupported()
	}
	for i, p := range e.prepared {
		if p.Part.MediaType != part.MediaType || !sameMediaRef(p.Part.Ref, part.Ref) {
			continue
		}
		e.used[i] = true
		if e.content[i] == nil {
			value, err := preparedContent(ctx, p.Value)
			if err != nil {
				return media.ContentPart{}, err
			}
			e.content[i] = &value
		}
		return *e.content[i], nil
	}
	return media.ContentPart{}, invalid()
}

func (e *mediaEncoder) complete() error {
	for _, used := range e.used {
		if !used {
			return invalid()
		}
	}
	return nil
}

func stringsIn(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func preparedContent(ctx context.Context, p *media.Prepared) (media.ContentPart, error) {
	if ctx == nil || p == nil {
		return media.ContentPart{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return media.ContentPart{}, contextFailure(err)
	}
	content, err := p.Content(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return media.ContentPart{}, contextFailure(ctx.Err())
		}
		return media.ContentPart{}, invalid()
	}
	return content, nil
}

// Charge the native JSON shape before marshaling the complete request. Media
// strings are cached per prepared reference, including repeated history uses.
func contentSize(content media.ContentPart) int {
	n := 128 + quotedSize(content.Text)
	if content.ImageURL != nil {
		n += quotedSize(content.ImageURL.URL)
	}
	if content.File != nil {
		n += quotedSize(content.File.Filename) + quotedSize(content.File.FileData)
	}
	return n
}

func (e *mediaEncoder) user(ctx context.Context, parts []mc.MessagePart, charge func(int) error) (any, error) {
	if len(parts) == 1 && parts[0].Text != nil {
		text := parts[0].Text.Text
		if err := charge(64 + quotedSize(text)); err != nil {
			return nil, err
		}
		return text, nil
	}
	content := make([]media.ContentPart, 0, len(parts))
	hasMedia := false
	for _, p := range parts {
		var value media.ContentPart
		var err error
		switch {
		case p.Text != nil:
			if p.Text.Text == "" {
				continue
			}
			value = media.Text(p.Text.Text)
		case p.Image != nil:
			hasMedia = true
			value, err = e.part(ctx, p.Image, true)
		case p.File != nil:
			hasMedia = true
			value, err = e.part(ctx, p.File, false)
		default:
			return nil, unsupported()
		}
		if err != nil {
			return nil, err
		}
		if err := charge(contentSize(value)); err != nil {
			return nil, err
		}
		content = append(content, value)
	}
	if !hasMedia {
		return nil, unsupported()
	}
	return content, nil
}

// EncodeUserContent shares the native user-content encoding with the System
// test path. It performs no I/O and does not grant Object-read authorization.
// The caller supplies capabilities from its current authorized model.
func EncodeUserContent(ctx context.Context, text string, attachments []*media.Prepared, caps mc.Capabilities) (any, error) {
	if ctx == nil || !utf8.ValidString(text) || strings.ContainsRune(text, 0) || len(text) > media.MaxPromptBytes || len(attachments) > media.MaxAttachments || strings.TrimSpace(text) == "" && len(attachments) == 0 || caps.Validate() != nil || !caps.SupportsTextInput() {
		return nil, invalid()
	}
	if ctx.Err() != nil {
		return nil, contextFailure(ctx.Err())
	}
	if len(attachments) == 0 {
		return text, nil
	}
	content := make([]media.ContentPart, 0, len(attachments)+1)
	size, total := 512, 0
	if text != "" {
		content = append(content, media.Text(text))
		size += contentSize(content[0])
	}
	for _, p := range attachments {
		if p == nil || p.Size() == 0 {
			return nil, invalid()
		}
		kind := "file"
		if media.Image(p.MediaType()) {
			kind = "image"
		}
		if !stringsIn(caps.InputModalities, kind) {
			return nil, unsupported()
		}
		total += p.Size()
		if total > media.MaxTotalBytes {
			return nil, limitFailure()
		}
		value, err := preparedContent(ctx, p)
		if err != nil {
			return nil, err
		}
		size += contentSize(value)
		if size > media.MaxRequestBytes {
			return nil, limitFailure()
		}
		content = append(content, value)
	}
	return content, nil
}
