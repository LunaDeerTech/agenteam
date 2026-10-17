package contract

import (
	"context"
	"strings"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

const MaxInputAttachments = 4

// CapturedArtifactAttachment is immutable source data captured by the business
// input owner. It is never a read grant and is never accepted from an HTTP
// caller. The containing trigger/message canonical bytes bind its position.
type CapturedArtifactAttachment struct {
	ProjectID       i.ProjectID   `json:"project_id"`
	ArtifactID      ac.ArtifactID `json:"artifact_id"`
	FileID          ac.FileID     `json:"file_id"`
	ArtifactVersion f.Version     `json:"artifact_version"`
	ObjectID        oc.ObjectID   `json:"object_id"`
	ObjectVersion   f.Version     `json:"object_version"`
	SHA256          f.Digest      `json:"sha256"`
	MediaType       string        `json:"media_type"`
	ByteSize        f.Progress    `json:"byte_size"`
	Filename        string        `json:"filename"`
}

func (v CapturedArtifactAttachment) Clone() CapturedArtifactAttachment { return v }
func (v CapturedArtifactAttachment) Validate() error {
	media, err := oc.NormalizeMediaType(v.MediaType)
	if v.ProjectID.Validate() != nil || v.ArtifactID.Validate() != nil || v.FileID.Validate() != nil || v.ArtifactVersion.Validate() != nil || v.ObjectID.Validate() != nil || v.ObjectVersion.Validate() != nil || v.SHA256.Validate() != nil || v.ByteSize.Validate() != nil || int64(v.ByteSize) > 4<<20 || err != nil || media != v.MediaType || oc.ValidateFilename(v.Filename) != nil {
		return invalid()
	}
	return nil
}
func (v CapturedArtifactAttachment) Reference() (oc.BusinessFileRef, error) {
	if v.Validate() != nil {
		return oc.BusinessFileRef{}, invalid()
	}
	return (ac.ArtifactRef{ProjectID: v.ProjectID, ArtifactID: v.ArtifactID, FileID: v.FileID}).BusinessFile()
}
func (v CapturedArtifactAttachment) MatchesReference(ref oc.BusinessFileRef) bool {
	if v.Validate() != nil || ref.Validate() != nil {
		return false
	}
	d := ref.Details()
	return d.Kind == oc.ArtifactFile && d.ProjectID == v.ProjectID && d.ArtifactID == v.ArtifactID.String() && d.FileID == v.FileID.String()
}
func (v CapturedArtifactAttachment) MatchesSource(source oc.ResolvedSource) bool {
	if source.Validate() != nil || !v.MatchesReference(source.Details().Reference) {
		return false
	}
	d := source.Details()
	return d.Revision == v.ArtifactVersion && d.Meta.ID == v.ObjectID && d.Meta.Version == v.ObjectVersion && d.Meta.SHA256 == v.SHA256 && d.Meta.MediaType == v.MediaType && d.Meta.ByteSize == v.ByteSize
}
func (v CapturedArtifactAttachment) MessagePart() (mc.MessagePart, error) {
	ref, err := v.Reference()
	if err != nil {
		return mc.MessagePart{}, err
	}
	media := &mc.MediaPart{Ref: ref, MediaType: v.MediaType}
	if strings.HasPrefix(v.MediaType, "image/") {
		return mc.MessagePart{Image: media}, nil
	}
	return mc.MessagePart{File: media}, nil
}

// Capture is lent by the original Artifact owner into the business mutation's
// transaction. Discovery is not a grant; Capture requires the complete held
// lock union and checks current Human/Project and published Artifact access.
type ArtifactAttachmentCapture interface {
	AttachmentLocks(context.Context, i.Actor, i.ProjectID, []ac.ArtifactID) ([]f.LockRequest, error)
	CaptureAttachmentsInTx(context.Context, f.Tx, i.Actor, i.ProjectID, []ac.ArtifactID) ([]CapturedArtifactAttachment, error)
}

type AttachmentContentReadRequest struct {
	Actor      i.Actor
	Attachment CapturedArtifactAttachment
}

func (r AttachmentContentReadRequest) Validate() error {
	if r.Actor.Validate() != nil || r.Attachment.Validate() != nil {
		return invalid()
	}
	d := r.Actor.Details()
	if d.ProjectID != r.Attachment.ProjectID.String() || d.Kind != i.AgentRun && (d.Kind != i.Service || d.ServiceName != i.MeetingRuntime) {
		return invalid()
	}
	return nil
}

// Both Execution and Meeting Summary implement this port using their original
// live Model call witness. Resolve finds the exact frozen binding; a caller's
// Actor, business ref, persisted snapshot or recomputed dependency is no grant.
type AttachmentContentReadAuthority interface {
	AttachmentContentReadActor(context.Context) (i.Actor, error)
	ResolveAttachmentContentRead(context.Context, i.Actor, oc.BusinessFileRef) (CapturedArtifactAttachment, error)
	DiscoverAttachmentContentRead(context.Context, AttachmentContentReadRequest) (oc.AccessDependencies, error)
	ValidateAttachmentContentReadInTx(context.Context, f.Tx, AttachmentContentReadRequest, oc.AccessDependencies) error
	RequireAttachmentContentReadInTx(context.Context, f.Tx, AttachmentContentReadRequest) error
	AuthorizeAttachmentContentReadInTx(context.Context, f.Tx, AttachmentContentReadRequest) (i.AccessGrant, error)
}

// Tail permits only retiring the original reader while its registered Model
// call closes. It cannot open another reader after cancellation or revocation.
type AttachmentContentReadTailAuthority interface {
	DiscoverAttachmentContentReadTail(context.Context, AttachmentContentReadRequest, oc.AccessRequest) (oc.AccessDependencies, error)
	ValidateAttachmentContentReadTailInTx(context.Context, f.Tx, AttachmentContentReadRequest, oc.AccessRequest, oc.AccessDependencies) error
}
