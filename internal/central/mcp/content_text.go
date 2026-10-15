package mcp

import (
	"context"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

const maxInlineResourceText = 64 << 10

// Text is a bounded model projection of the actual canonical Object. The
// durable MCPFile remains separately addressable by the returned reference.
// Reading and Close both finish while the original admitted Tool witness is
// alive; a descriptor or a historical upload receipt cannot authorize this.
func (s *ContentStore) projectCanonicalText(ctx context.Context, w *contentUse, owner oc.ObjectOwner, meta oc.ObjectMeta, ref oc.BusinessFileRef, uri string) (mc.ResultPart, error) {
	if s == nil || s.authority == nil || ctx == nil || w == nil || w.owner != s.authority || ctx.Value(contentUseKey{}) != w || !w.active.Load() || meta.Validate() != nil {
		return mc.ResultPart{}, fail(f.Forbidden)
	}
	reader, err := s.objects.ReadObject(ctx, w.call.Actor, owner, meta.ID, nil)
	if err != nil {
		return mc.ResultPart{}, safeError(err)
	}
	if reader == nil {
		return mc.ResultPart{}, fail(f.InvalidState)
	}
	actual := reader.Meta()
	if actual.Validate() != nil || actual.ID != meta.ID || actual.SHA256 != meta.SHA256 || actual.ByteSize != meta.ByteSize || actual.MediaType != meta.MediaType || !actual.Scope.Equal(meta.Scope) || reader.Range() != nil {
		closeErr := reader.Close()
		return mc.ResultPart{}, safeError(errors.Join(fail(f.ObjectIntegrityMismatch), closeErr))
	}
	body, readErr := io.ReadAll(io.LimitReader(reader, protocol.MaxContentBytes+1))
	closeErr := reader.Close()
	defer clear(body)
	if err = errors.Join(readErr, closeErr); err != nil {
		return mc.ResultPart{}, safeError(err)
	}
	if err = ctx.Err(); err != nil {
		return mc.ResultPart{}, err
	}
	// ReadObject performed the actual Object lease and I/O join. Recheck the
	// original invocation after that wait before publishing its text projection.
	if _, err = s.authority.AuthorizeOwner(ctx, w.call.Actor, owner, id.Read); err != nil {
		return mc.ResultPart{}, safeError(err)
	}
	return canonicalTextProjection(meta, ref, uri, body)
}

func canonicalTextProjection(meta oc.ObjectMeta, ref oc.BusinessFileRef, uri string, body []byte) (mc.ResultPart, error) {
	if meta.Validate() != nil || ref.Validate() != nil || ref.Details().Kind != oc.MCPFile || meta.Scope.Details().Kind != id.ProjectScope || meta.Scope.Details().ProjectID != ref.Details().ProjectID.String() || len(uri) == 0 || len(uri) > 8192 {
		return mc.ResultPart{}, fail(f.InvalidArgument)
	}
	if len(body) > protocol.MaxContentBytes || f.Progress(len(body)) != meta.ByteSize || f.Digest(digest(body)) != meta.SHA256 {
		return mc.ResultPart{}, fail(f.ObjectIntegrityMismatch)
	}
	if !utf8.Valid(body) {
		return mc.ResultPart{}, fail(f.CapabilityUnsupported)
	}
	if !strings.HasPrefix(meta.MediaType, "text/") && meta.MediaType != "application/json" && !strings.HasSuffix(meta.MediaType, "+json") && meta.MediaType != "application/xml" && !strings.HasSuffix(meta.MediaType, "+xml") {
		return mc.ResultPart{}, fail(f.CapabilityUnsupported)
	}
	limit := min(len(body), maxInlineResourceText)
	for limit > 0 && !utf8.Valid(body[:limit]) {
		limit--
	}
	d := ref.Details()
	value := struct {
		File struct {
			Kind      oc.SourceKind `json:"kind"`
			ProjectID id.ProjectID  `json:"project_id"`
			FileID    string        `json:"file_id"`
		} `json:"file_ref"`
		URI       string     `json:"uri"`
		MIMEType  string     `json:"mime_type"`
		ByteSize  f.Progress `json:"byte_size"`
		SHA256    f.Digest   `json:"sha256"`
		Text      string     `json:"text"`
		Truncated bool       `json:"truncated"`
	}{URI: uri, MIMEType: meta.MediaType, ByteSize: meta.ByteSize, SHA256: meta.SHA256, Text: string(body[:limit]), Truncated: limit < len(body)}
	value.File.Kind, value.File.ProjectID, value.File.FileID = d.Kind, d.ProjectID, d.FileID
	raw, err := encode(value)
	if err != nil {
		return mc.ResultPart{}, err
	}
	return mc.ResultPart{Text: &mc.TextPart{Text: string(raw)}}, nil
}
