package execution

import (
	"context"
	"encoding/json"
	"reflect"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/meeting"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
)

func executionAttachments(facts directTextModelFacts) ([]c.CapturedArtifactAttachment, error) {
	if facts.Snapshot.Validate() != nil || facts.Round == nil || facts.Round.Validate() != nil {
		return nil, invalid()
	}
	trigger := facts.Snapshot.Fields().Context.Trigger()
	switch trigger.Request().Launch.Trigger.Kind {
	case "task":
		input, err := work.DecodeTaskContext(trigger)
		if err != nil {
			return nil, portError(err)
		}
		return input.Attachments(), nil
	case "meeting":
		input, err := meeting.DecodeContext(trigger)
		if err != nil {
			return nil, portError(err)
		}
		return input.Attachments()
	}
	return nil, fault(f.DependencyUnbound)
}

func attachmentInMessages(attachment c.CapturedArtifactAttachment, messages []mc.Message) bool {
	for _, message := range messages {
		if message.Role != "user" {
			continue
		}
		for _, part := range message.Parts {
			for _, media := range []*mc.MediaPart{part.Image, part.File} {
				if media != nil && attachment.MediaType == media.MediaType && attachment.MatchesReference(media.Ref) {
					return true
				}
			}
		}
	}
	return false
}

func executionAttachmentSource(actor i.Actor, ref oc.BusinessFileRef, facts directTextModelFacts) (c.CapturedArtifactAttachment, error) {
	if actor.Validate() != nil || ref.Validate() != nil || ref.Details().Kind != oc.ArtifactFile {
		return c.CapturedArtifactAttachment{}, invalid()
	}
	attachments, err := executionAttachments(facts)
	if err != nil {
		return c.CapturedArtifactAttachment{}, err
	}
	request := facts.Snapshot.Fields().Context.Input().Fields().Request
	expected, err := i.NewAgentRun(request.Launch.ProjectID, request.Launch.AgentID, request.ExecutionID)
	if err != nil || !expected.Equal(actor) || facts.Round.Fields().ExecutionID != request.ExecutionID {
		return c.CapturedArtifactAttachment{}, fault(f.Forbidden)
	}
	for _, attachment := range attachments {
		if attachment.MatchesReference(ref) && attachmentInMessages(attachment, facts.Round.Fields().Messages) {
			return attachment, nil
		}
	}
	return c.CapturedArtifactAttachment{}, fault(f.Forbidden)
}

func (a *Authority) attachmentPlanning(ctx context.Context, actor i.Actor, tail bool) (directTextModelFacts, error) {
	facts, err := a.directTextModelPlanningScope(ctx, false)
	if err != nil {
		return directTextModelFacts{}, err
	}
	run, err := a.directTextOwner(ctx)
	if err != nil {
		return directTextModelFacts{}, err
	}
	run.owner.mu.Lock()
	defer run.owner.mu.Unlock()
	if run.phase != "calling" && !(tail && run.phase == "closing") || run.allJoined || run.invocation == nil {
		return directTextModelFacts{}, fault(f.Forbidden)
	}
	request, round := run.invocation.request.Request(), facts.Round.Fields()
	if !request.Actor.Equal(actor) || request.CallID != round.CallID || !reflect.DeepEqual(request.Input, round.Input) || !mcpContentMessagesEqual(request.Messages, round.Messages) {
		return directTextModelFacts{}, fault(f.Forbidden)
	}
	return facts, ctx.Err()
}

func (a *Authority) AttachmentContentReadActor(ctx context.Context) (i.Actor, error) {
	return a.MCPContentReadActor(ctx)
}

func (a *Authority) ResolveAttachmentContentRead(ctx context.Context, actor i.Actor, ref oc.BusinessFileRef) (c.CapturedArtifactAttachment, error) {
	facts, err := a.attachmentPlanning(ctx, actor, false)
	if err != nil {
		return c.CapturedArtifactAttachment{}, err
	}
	return executionAttachmentSource(actor, ref, facts)
}

func executionAttachmentDependencies(r c.AttachmentContentReadRequest, facts directTextModelFacts) (oc.AccessDependencies, error) {
	if r.Validate() != nil {
		return oc.AccessDependencies{}, invalid()
	}
	ref, err := r.Attachment.Reference()
	if err != nil {
		return oc.AccessDependencies{}, portError(err)
	}
	actual, err := executionAttachmentSource(r.Actor, ref, facts)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	if actual != r.Attachment {
		return oc.AccessDependencies{}, fault(f.ConfirmationStale)
	}
	raw, err := json.Marshal(struct {
		Format     string
		Attachment c.CapturedArtifactAttachment
		Actor      i.ActorDetails
		Process    string
		Snapshot   f.Digest
		Round      f.Digest
	}{"execution-attachment-source-v1", r.Attachment, r.Actor.Details(), facts.ProcessID.String(), facts.Snapshot.Digest(), facts.Round.Digest()})
	if err != nil {
		return oc.AccessDependencies{}, unavailable(err)
	}
	return oc.NewAccessDependencies(c.TriggerInputDigest(raw), facts.Locks)
}

func (a *Authority) DiscoverAttachmentContentRead(ctx context.Context, r c.AttachmentContentReadRequest) (oc.AccessDependencies, error) {
	facts, err := a.attachmentPlanning(ctx, r.Actor, false)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return executionAttachmentDependencies(r, facts)
}

func (a *Authority) requireAttachmentContentRead(ctx context.Context, tx f.Tx, r c.AttachmentContentReadRequest) (oc.AccessDependencies, error) {
	if _, err := a.attachmentPlanning(ctx, r.Actor, false); err != nil {
		return oc.AccessDependencies{}, err
	}
	facts, err := a.directTextModelScope(ctx, tx, false)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	if _, err = a.directTextCurrentAgent(ctx, tx); err != nil {
		return oc.AccessDependencies{}, err
	}
	return executionAttachmentDependencies(r, facts)
}

func (a *Authority) ValidateAttachmentContentReadInTx(ctx context.Context, tx f.Tx, r c.AttachmentContentReadRequest, expected oc.AccessDependencies) error {
	if expected.Validate() != nil {
		return invalid()
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

func (a *Authority) RequireAttachmentContentReadInTx(ctx context.Context, tx f.Tx, r c.AttachmentContentReadRequest) error {
	_, err := a.requireAttachmentContentRead(ctx, tx, r)
	return err
}

func (a *Authority) AuthorizeAttachmentContentReadInTx(ctx context.Context, tx f.Tx, r c.AttachmentContentReadRequest) (i.AccessGrant, error) {
	if err := a.RequireAttachmentContentReadInTx(ctx, tx, r); err != nil {
		return i.AccessGrant{}, err
	}
	run, err := a.directTextOwner(ctx)
	if err != nil {
		return i.AccessGrant{}, err
	}
	project, err := run.owner.deps.Projects.RequirePreparingProjectInTx(ctx, tx, r.Attachment.ProjectID)
	if err != nil {
		return i.AccessGrant{}, portError(err)
	}
	x, err := a.state.store.InTx(tx)
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

func executionAttachmentTailDependencies(r c.AttachmentContentReadRequest, access oc.AccessRequest, facts directTextModelFacts) (oc.AccessDependencies, error) {
	if access.Validate() != nil || access.Details().Kind != oc.MaintenanceAccess || access.Details().Operation != oc.ReleaseReaderAccess || access.Details().InstanceID.String() != facts.ProcessID.String() || access.Details().ObjectID != r.Attachment.ObjectID {
		return oc.AccessDependencies{}, fault(f.Forbidden)
	}
	original, err := executionAttachmentDependencies(r, facts)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	d := access.Details()
	raw, err := json.Marshal([]any{"execution-attachment-source-tail-v1", original.Mapping(), d.ObjectID, d.LeaseID, d.InstanceID})
	if err != nil {
		return oc.AccessDependencies{}, invalid()
	}
	return oc.NewAccessDependencies(c.TriggerInputDigest(raw), facts.Locks)
}

func (a *Authority) DiscoverAttachmentContentReadTail(ctx context.Context, r c.AttachmentContentReadRequest, access oc.AccessRequest) (oc.AccessDependencies, error) {
	facts, err := a.attachmentPlanning(ctx, r.Actor, true)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return executionAttachmentTailDependencies(r, access, facts)
}

func (a *Authority) ValidateAttachmentContentReadTailInTx(ctx context.Context, tx f.Tx, r c.AttachmentContentReadRequest, access oc.AccessRequest, expected oc.AccessDependencies) error {
	if expected.Validate() != nil {
		return invalid()
	}
	facts, err := a.attachmentPlanning(ctx, r.Actor, true)
	if err != nil {
		return err
	}
	actual, err := executionAttachmentTailDependencies(r, access, facts)
	if err != nil {
		return err
	}
	if !actual.Equal(expected) {
		return fault(f.ConfirmationStale)
	}
	if err = a.state.store.RequireHeldLocks(ctx, tx, facts.Locks); err != nil {
		return portError(err)
	}
	// Retiring an already opened reader must still work after current business
	// access is revoked. The live registered owner and its original snapshot
	// remain required, but the new-read Project/Agent gates are not replayed.
	run, err := a.directTextOwner(ctx)
	if err != nil {
		return err
	}
	if run.owner.multi != nil {
		if nilPort(run.owner.multi.guard) {
			return fault(f.DependencyUnbound)
		}
		return portError(run.owner.multi.guard.RequireCurrentProcessInTx(ctx, tx, run.owner.multiProcess()))
	}
	// The original single-round owner has no multi-round guard. Its registered
	// ProcessAuthority was checked above; Object's release transaction still
	// validates the exact original Process/lease before retiring protection.
	return ctx.Err()
}

var _ c.AttachmentContentReadAuthority = (*Authority)(nil)
var _ c.AttachmentContentReadTailAuthority = (*Authority)(nil)
