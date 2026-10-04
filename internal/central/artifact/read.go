package artifact

import (
	"context"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func (s *Service) ReadArtifact(ctx context.Context, v ac.Invocation, ref ac.ArtifactRef, preview ac.PreviewRequest) (ac.ReadResult, error) {
	preview, err := preview.Normalize()
	if err != nil {
		return ac.ReadResult{}, err
	}
	meta, err := s.metadata(ctx, v, ref)
	if err != nil {
		return ac.ReadResult{}, err
	}
	total := int64(meta.Details().Object.ByteSize)
	if preview.Offset > total {
		return ac.ReadResult{}, failure(foundation.RangeNotSatisfiable, nil)
	}
	header, err := s.readWindow(ctx, v, meta, 0, min(total, 512))
	if err != nil {
		return ac.ReadResult{}, err
	}
	d := ac.ReadDetails{Artifact: meta, Projection: ac.FileProjection}
	sniff := http.DetectContentType(header)
	if supportedImage(sniff) {
		d.Projection = ac.ImageProjection
	} else if textMedia(meta.Details().Object.MediaType, sniff) {
		start := max(int64(0), preview.Offset-3)
		length := min(total-start, int64(preview.Limit)+7)
		window := header
		if start != 0 || length > int64(len(header)) {
			window, err = s.readWindow(ctx, v, meta, start, length)
			if err != nil {
				return ac.ReadResult{}, err
			}
		} else {
			window = window[:length]
		}
		text, next, valid, boundary := previewText(window, start, total, preview)
		if !boundary {
			return ac.ReadResult{}, invalid()
		}
		if !valid {
			d.InvalidUTF8 = true
		} else {
			d.Projection = ac.TextProjection
			d.Text = text
			d.Offset = foundation.Progress(preview.Offset)
			d.NextOffset = foundation.Progress(next)
			d.Truncated = next < total
		}
	}
	out, err := ac.NewReadResult(d)
	if err != nil {
		return ac.ReadResult{}, err
	}
	cause, err := txCause()
	if err != nil {
		return ac.ReadResult{}, err
	}
	result := s.within(ctx, invocationSubject(v), identity.Read, cause, nil, []foundation.LockRequest{artifactLock(ref.ArtifactID.String())}, func(ctx context.Context, tx foundation.Tx, _ oc.LockedAccess) error {
		e, err := s.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		row, ok, err := loadArtifact(ctx, e, ref.ArtifactID.String())
		if err != nil {
			return err
		}
		currentDigest, digestErr := jsonDigest(row.meta.Details().Object)
		expectedDigest, expectedErr := jsonDigest(meta.Details().Object)
		if !ok || row.meta.Details().Reference != ref || digestErr != nil || expectedErr != nil || currentDigest != expectedDigest {
			return failure(foundation.Forbidden, nil)
		}
		if _, err = s.state().owners.AuthorizeOwnerInTx(ctx, tx, v.Details().Actor, artifactOwner(ref), identity.Read); err != nil {
			return err
		}
		fields := au.ArtifactMetadataFields{ArtifactID: ref.ArtifactID.String(), ObjectID: meta.Details().Object.ID.String(), MediaType: auditMedia(meta.Details().Object.MediaType), ByteSize: meta.Details().Object.ByteSize, SentBytes: foundation.Progress(len(d.Text)), Phase: au.ReadPhase}
		metadata, err := au.ArtifactMetadata(au.ArtifactRead, fields)
		if err != nil {
			return err
		}
		resource, _ := au.NewResource(au.ArtifactResource, ref.ArtifactID.String())
		key, err := jsonDigest(struct {
			Action, Attempt, Actor, Artifact string
			Offset                           int64
			Limit                            int
		}{"read", v.Details().AttemptID.String(), stableActor(v.Details().Actor), ref.ArtifactID.String(), preview.Offset, preview.Limit})
		if err != nil {
			return err
		}
		return s.appendReadAudit(ctx, tx, v, au.ArtifactRead, resource, metadata, key)
	})
	if err = commitError(result); err != nil {
		return ac.ReadResult{}, err
	}
	return out, nil
}
func (s *Service) readWindow(ctx context.Context, v ac.Invocation, meta ac.Metadata, offset, length int64) ([]byte, error) {
	m := meta.Details()
	var requested *oc.ByteRange
	if offset != 0 || length != int64(m.Object.ByteSize) {
		requested = &oc.ByteRange{Offset: offset, Length: length}
	}
	reader, err := s.state().objects.ReadObject(ctx, v.Details().Actor, artifactOwner(m.Reference), m.Object.ID, requested)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, length+1))
	closeErr := reader.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if int64(len(data)) != length {
		return nil, failure(foundation.ObjectIntegrityMismatch, nil)
	}
	return data, nil
}
func supportedImage(media string) bool {
	switch media {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	}
	return false
}
func textMedia(declared, sniff string) bool {
	base, _, err := mime.ParseMediaType(declared)
	if err != nil {
		return false
	}
	return strings.HasPrefix(base, "text/") || base == "application/json" || base == "application/ld+json" || sniff == "text/plain; charset=utf-8"
}

// Inspect an at-most limit+7 byte window including up to three preceding bytes.
// Never replace malformed bytes or split a rune to fill the preview limit.
func previewText(window []byte, start, total int64, p ac.PreviewRequest) (text string, next int64, valid, boundary bool) {
	offset := int(p.Offset - start)
	if offset < 0 || offset > len(window) {
		return "", 0, false, false
	}
	i := 0
	if start > 0 {
		for i < len(window) && !utf8.RuneStart(window[i]) {
			i++
		}
	}
	if i > offset {
		return "", 0, true, false
	}
	boundary = offset == i
	end := offset
	for i < len(window) {
		if !utf8.FullRune(window[i:]) {
			if start+int64(len(window)) == total {
				return "", 0, false, true
			}
			break
		}
		r, size := utf8.DecodeRune(window[i:])
		if r == utf8.RuneError && size == 1 {
			return "", 0, false, true
		}
		if i == offset {
			boundary = true
		}
		if i < offset && i+size > offset {
			return "", 0, true, false
		}
		i += size
		if i >= offset && i-offset <= p.Limit {
			end = i
		}
	}
	if offset == len(window) {
		boundary = true
	}
	if !boundary {
		return "", 0, true, false
	}
	return string(window[offset:end]), start + int64(end), true, true
}
