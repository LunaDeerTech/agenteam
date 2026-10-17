package artifact

import (
	"context"
	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	media "github.com/LunaDeerTech/agenteam/internal/central/model/media"
	outbox "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

// NewAttachmentCapture binds metadata-only capture before Object transport is constructed.
func NewAttachmentCapture(owners *OwnerProvider) (*OwnerFileReferences, error) {
	if owners == nil || owners.bound() != nil {
		return nil, failure(f.DependencyUnbound, nil)
	}
	if _, ok := owners.state().store.(fileReferenceLockStore); !ok {
		return nil, failure(f.DependencyUnbound, nil)
	}
	return &OwnerFileReferences{store: owners.state().store, owners: owners}, nil
}

func (r *OwnerFileReferences) AttachmentLocks(ctx context.Context, actor i.Actor, project i.ProjectID, ids []ac.ArtifactID) ([]f.LockRequest, error) {
	if len(ids) > ec.MaxInputAttachments {
		return nil, invalid()
	}
	var locks []f.LockRequest
	seen := map[ac.ArtifactID]bool{}
	for _, id := range ids {
		if seen[id] {
			return nil, invalid()
		}
		seen[id] = true
		next, err := r.FileReferenceLocks(ctx, actor, project, id)
		if err != nil {
			return nil, err
		}
		locks = append(locks, next...)
	}
	return outbox.NormalizeLocks(locks)
}

// CaptureAttachmentsInTx freezes published metadata under the caller's complete
// lock union. It grants neither subsequent reads nor permission to send bytes.
func (r *OwnerFileReferences) CaptureAttachmentsInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, ids []ac.ArtifactID) ([]ec.CapturedArtifactAttachment, error) {
	if len(ids) > ec.MaxInputAttachments {
		return nil, invalid()
	}
	result := make([]ec.CapturedArtifactAttachment, 0, len(ids))
	seen := map[ac.ArtifactID]bool{}
	total := int64(0)
	for _, id := range ids {
		if seen[id] {
			return nil, invalid()
		}
		seen[id] = true
		if err := r.RequireFileReferenceInTx(ctx, tx, actor, project, id); err != nil {
			return nil, err
		}
		x, err := r.store.InTx(tx)
		if err != nil {
			return nil, portError(err)
		}
		row, ok, err := loadArtifact(ctx, x, id.String())
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, failure(f.NotFound, nil)
		}
		d := row.meta.Details()
		limit := media.Limit(d.Object.MediaType)
		if limit == 0 || !media.ValidFilename(d.Name) {
			return nil, failure(f.CapabilityUnsupported, nil)
		}
		total += int64(d.Object.ByteSize)
		if d.Object.ByteSize <= 0 || int64(d.Object.ByteSize) > int64(limit) || total > media.MaxTotalBytes {
			return nil, failure(f.PayloadTooLarge, nil)
		}
		v := ec.CapturedArtifactAttachment{ProjectID: project, ArtifactID: id, FileID: d.Reference.FileID, ArtifactVersion: d.Version, ObjectID: d.Object.ID, ObjectVersion: d.Object.Version, SHA256: d.Object.SHA256, MediaType: d.Object.MediaType, ByteSize: d.Object.ByteSize, Filename: d.Name}
		if v.Validate() != nil {
			return nil, unavailable(nil)
		}
		result = append(result, v)
	}
	return result, nil
}
