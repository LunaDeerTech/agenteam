package meeting

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func (a *SummaryAuthority) attachmentOwner(ctx context.Context, tail bool) (*summaryOwner, mc.ModelRequest, error) {
	if a == nil || ctx == nil {
		return nil, mc.ModelRequest{}, fault(f.InvalidArgument)
	}
	w, _ := ctx.Value(summaryOwnerKey{}).(*summaryOwner)
	if w == nil || w.authority != a {
		return nil, mc.ModelRequest{}, fault(f.Forbidden)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.active || w.sourceRequest == nil || w.sourceClosing && !tail || w.snapshot == nil || w.lease == nil {
		return nil, mc.ModelRequest{}, fault(f.Forbidden)
	}
	r := w.sourceRequest.Clone()
	if r.Validate() != nil || r.CallID != w.call || r.Model.Snapshot.ID != *w.snapshot || r.Consumer.Kind != mc.MeetingConsumer || r.Consumer.ProjectID != w.input.ProjectID || r.Consumer.MeetingID != w.input.MeetingID.String() || r.Consumer.OperationID != w.input.GenerationID.String() || r.Actor.Details().Kind != i.Service || r.Actor.Details().ServiceName != i.MeetingRuntime || r.Actor.Details().CauseRef != w.input.GenerationID.String() {
		return nil, mc.ModelRequest{}, fault(f.Forbidden)
	}
	return w, r, ctx.Err()
}

func summaryAttachmentSource(w *summaryOwner, request mc.ModelRequest, actor i.Actor, ref oc.BusinessFileRef) (ec.CapturedArtifactAttachment, error) {
	if !request.Actor.Equal(actor) || ref.Validate() != nil || ref.Details().Kind != oc.ArtifactFile {
		return ec.CapturedArtifactAttachment{}, fault(f.Forbidden)
	}
	sources, err := frozenAttachments(w.input.ProjectID, w.input.Messages)
	if err != nil {
		return ec.CapturedArtifactAttachment{}, err
	}
	for _, source := range sources {
		if !source.MatchesReference(ref) {
			continue
		}
		for _, message := range request.Messages {
			if message.Role != "user" {
				continue
			}
			for _, part := range message.Parts {
				for _, media := range []*mc.MediaPart{part.Image, part.File} {
					if media != nil && media.MediaType == source.MediaType && source.MatchesReference(media.Ref) {
						return source, nil
					}
				}
			}
		}
	}
	return ec.CapturedArtifactAttachment{}, fault(f.Forbidden)
}

func (a *SummaryAuthority) AttachmentContentReadActor(ctx context.Context) (i.Actor, error) {
	_, request, err := a.attachmentOwner(ctx, true)
	if err != nil {
		return i.Actor{}, err
	}
	return request.Actor, nil
}

func (a *SummaryAuthority) ResolveAttachmentContentRead(ctx context.Context, actor i.Actor, ref oc.BusinessFileRef) (ec.CapturedArtifactAttachment, error) {
	w, request, err := a.attachmentOwner(ctx, false)
	if err != nil {
		return ec.CapturedArtifactAttachment{}, err
	}
	return summaryAttachmentSource(w, request, actor, ref)
}

func (a *SummaryAuthority) summaryAttachmentDependencies(w *summaryOwner, original mc.ModelRequest, r ec.AttachmentContentReadRequest) (oc.AccessDependencies, error) {
	if r.Validate() != nil {
		return oc.AccessDependencies{}, fault(f.InvalidArgument)
	}
	ref, err := r.Attachment.Reference()
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	pinned, err := summaryAttachmentSource(w, original, r.Actor, ref)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	if pinned != r.Attachment {
		return oc.AccessDependencies{}, fault(f.ConfirmationStale)
	}
	raw, err := canonical(struct {
		Format     string
		Source     ec.CapturedArtifactAttachment
		Project    i.ProjectID
		Generation string
		Call       mc.CallID
		Input      f.Digest
		Process    oc.ProcessID
	}{"meeting-summary-attachment-source-v1", pinned, w.input.ProjectID, w.input.GenerationID.String(), w.call, original.Input.Digest, a.process})
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return oc.NewAccessDependencies(digest(raw), summaryLocks(w.input.ProjectID, w.input.MeetingID))
}

func (a *SummaryAuthority) DiscoverAttachmentContentRead(ctx context.Context, r ec.AttachmentContentReadRequest) (oc.AccessDependencies, error) {
	w, request, err := a.attachmentOwner(ctx, false)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return a.summaryAttachmentDependencies(w, request, r)
}

func (a *SummaryAuthority) requireAttachmentContentRead(ctx context.Context, tx f.Tx, r ec.AttachmentContentReadRequest) (oc.AccessDependencies, error) {
	w, request, err := a.attachmentOwner(ctx, false)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	deps, err := a.summaryAttachmentDependencies(w, request, r)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	if err = a.store.RequireHeldLocks(ctx, tx, deps.Locks()); err != nil {
		return oc.AccessDependencies{}, portError(err)
	}
	if err = a.guard.RequireCurrentProcessInTx(ctx, tx, a.process); err != nil {
		return oc.AccessDependencies{}, portError(err)
	}
	if _, err = a.projects.RequirePreparingProjectInTx(ctx, tx, w.input.ProjectID); err != nil {
		return oc.AccessDependencies{}, portError(err)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return oc.AccessDependencies{}, portError(err)
	}
	row, err := loadSummaryCall(ctx, x, w.call)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	if row.status != "running" || row.returned != nil || row.retired != nil || row.snapshot == nil || *row.snapshot != request.Model.Snapshot.ID.String() || row.digest != request.Input.Digest || row.input.ProjectID != w.input.ProjectID || row.input.MeetingID != w.input.MeetingID || row.input.GenerationID != w.input.GenerationID {
		return oc.AccessDependencies{}, fault(f.ConfirmationStale)
	}
	originalMessages, err := mc.EncodeCanonicalMessages(request.Messages)
	if err != nil {
		return oc.AccessDependencies{}, portError(err)
	}
	storedMessages, err := mc.DecodeCanonicalMessages(row.messages)
	if err != nil {
		return oc.AccessDependencies{}, portError(err)
	}
	reencoded, err := mc.EncodeCanonicalMessages(storedMessages)
	if err != nil || !bytes.Equal(originalMessages, reencoded) {
		return oc.AccessDependencies{}, fault(f.ConfirmationStale)
	}
	var status, state, inputDigest string
	var inputData []byte
	var cancel, deleting bool
	var attempt int
	err = x.QueryRow(ctx, `SELECT g.status,g.attempt,g.input_digest,g.input_data,t.status,t.cancel_requested,s.deleting FROM agenteam_meeting.summary_generations g JOIN agenteam_meeting.turns t ON t.id=g.turn_id JOIN agenteam_meeting.sessions s ON s.id=g.meeting_id WHERE g.id=$1 AND g.meeting_id=$2 AND g.turn_id=$3`, w.input.GenerationID.String(), w.input.MeetingID.String(), w.input.TurnID.String()).Scan(&status, &attempt, &inputDigest, &inputData, &state, &cancel, &deleting)
	if err != nil {
		return oc.AccessDependencies{}, portError(err)
	}
	originalInput, err := canonical(w.input.Messages)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	storedInput, err := canonical(json.RawMessage(inputData))
	if err != nil || status != "generating" || state != "finalizing" || cancel || deleting || attempt != row.attempt || inputDigest != w.input.InputDigest.String() || digest(originalInput) != w.input.InputDigest || !bytes.Equal(originalInput, storedInput) {
		return oc.AccessDependencies{}, fault(f.ConfirmationStale)
	}
	return deps, ctx.Err()
}

func (a *SummaryAuthority) ValidateAttachmentContentReadInTx(ctx context.Context, tx f.Tx, r ec.AttachmentContentReadRequest, expected oc.AccessDependencies) error {
	if expected.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	actual, err := a.requireAttachmentContentRead(ctx, tx, r)
	if err != nil {
		return err
	}
	if !actual.Equal(expected) {
		return fault(f.ConfirmationStale)
	}
	return ctx.Err()
}

func (a *SummaryAuthority) RequireAttachmentContentReadInTx(ctx context.Context, tx f.Tx, r ec.AttachmentContentReadRequest) error {
	_, err := a.requireAttachmentContentRead(ctx, tx, r)
	return err
}

func (a *SummaryAuthority) AuthorizeAttachmentContentReadInTx(ctx context.Context, tx f.Tx, r ec.AttachmentContentReadRequest) (i.AccessGrant, error) {
	if err := a.RequireAttachmentContentReadInTx(ctx, tx, r); err != nil {
		return i.AccessGrant{}, err
	}
	project, err := a.projects.RequirePreparingProjectInTx(ctx, tx, r.Attachment.ProjectID)
	if err != nil {
		return i.AccessGrant{}, portError(err)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return i.AccessGrant{}, portError(err)
	}
	var at time.Time
	if err = x.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
		return i.AccessGrant{}, portError(err)
	}
	checked, err := f.NewInstant(at)
	if err != nil {
		return i.AccessGrant{}, portError(err)
	}
	if err = ctx.Err(); err != nil {
		return i.AccessGrant{}, err
	}
	scope, err := i.InProject(project.ID)
	if err != nil {
		return i.AccessGrant{}, portError(err)
	}
	return i.NewAccessGrant(r.Actor, scope, i.Read, checked, project.Version)
}

func (a *SummaryAuthority) summaryAttachmentTail(ctx context.Context, r ec.AttachmentContentReadRequest, access oc.AccessRequest) (oc.AccessDependencies, error) {
	w, request, err := a.attachmentOwner(ctx, true)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	if access.Validate() != nil || access.Details().Kind != oc.MaintenanceAccess || access.Details().Operation != oc.ReleaseReaderAccess || access.Details().ObjectID != r.Attachment.ObjectID || access.Details().InstanceID != a.process {
		return oc.AccessDependencies{}, fault(f.Forbidden)
	}
	original, err := a.summaryAttachmentDependencies(w, request, r)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	d := access.Details()
	raw, err := canonical([]any{"meeting-summary-attachment-source-tail-v1", original.Mapping(), d.ObjectID, d.LeaseID, d.InstanceID})
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return oc.NewAccessDependencies(digest(raw), original.Locks())
}

func (a *SummaryAuthority) DiscoverAttachmentContentReadTail(ctx context.Context, r ec.AttachmentContentReadRequest, access oc.AccessRequest) (oc.AccessDependencies, error) {
	return a.summaryAttachmentTail(ctx, r, access)
}

func (a *SummaryAuthority) ValidateAttachmentContentReadTailInTx(ctx context.Context, tx f.Tx, r ec.AttachmentContentReadRequest, access oc.AccessRequest, expected oc.AccessDependencies) error {
	if expected.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	actual, err := a.summaryAttachmentTail(ctx, r, access)
	if err != nil {
		return err
	}
	if !actual.Equal(expected) {
		return fault(f.ConfirmationStale)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, actual.Locks()); err != nil {
		return portError(err)
	}
	return portError(a.guard.RequireCurrentProcessInTx(ctx, tx, a.process))
}

var _ ec.AttachmentContentReadAuthority = (*SummaryAuthority)(nil)
var _ ec.AttachmentContentReadTailAuthority = (*SummaryAuthority)(nil)
