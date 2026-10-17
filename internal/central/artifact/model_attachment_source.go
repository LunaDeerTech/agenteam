package artifact

import (
	"context"
	"encoding/json"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	outbox "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

type modelAttachmentKey struct{}
type modelAttachmentScope struct {
	authority ec.AttachmentContentReadAuthority
	request   ec.AttachmentContentReadRequest
}

// BindModelAttachmentRead is a trusted composition port. The authority must
// recognize the original live model call; the supplied snapshot grants nothing.
func BindModelAttachmentRead(ctx context.Context, authority ec.AttachmentContentReadAuthority, actor i.Actor, ref oc.BusinessFileRef) (context.Context, ec.CapturedArtifactAttachment, error) {
	if ctx == nil || nilPort(authority) {
		return nil, ec.CapturedArtifactAttachment{}, failure(f.DependencyUnbound, nil)
	}
	actual, err := authority.AttachmentContentReadActor(ctx)
	if err != nil {
		return nil, ec.CapturedArtifactAttachment{}, err
	}
	if !actual.Equal(actor) {
		return nil, ec.CapturedArtifactAttachment{}, failure(f.Forbidden, nil)
	}
	binding, err := authority.ResolveAttachmentContentRead(ctx, actor, ref)
	if err != nil {
		return nil, binding, err
	}
	req := ec.AttachmentContentReadRequest{Actor: actor, Attachment: binding}
	if req.Validate() != nil || !binding.MatchesReference(ref) {
		return nil, binding, invalid()
	}
	return context.WithValue(ctx, modelAttachmentKey{}, &modelAttachmentScope{authority, req}), binding, nil
}
func modelAttachment(ctx context.Context) *modelAttachmentScope {
	if ctx == nil {
		return nil
	}
	v, _ := ctx.Value(modelAttachmentKey{}).(*modelAttachmentScope)
	return v
}
func (w *modelAttachmentScope) matches(actor i.Actor, owner oc.ObjectOwner) bool {
	return actor.Equal(w.request.Actor) && owner.Validate() == nil && owner.Details().Kind == oc.Artifact && owner.Details().ID == w.request.Attachment.ArtifactID.String() && owner.Details().ProjectID == w.request.Attachment.ProjectID.String()
}
func (p *OwnerProvider) modelDependencies(ctx context.Context, tx f.Tx, request oc.AccessRequest) (oc.AccessDependencies, error) {
	w := modelAttachment(ctx)
	if w == nil || request.Validate() != nil {
		return oc.AccessDependencies{}, invalid()
	}
	d := request.Details()
	b := w.request.Attachment
	tail := d.Kind == oc.MaintenanceAccess && d.Operation == oc.ReleaseReaderAccess
	var deps oc.AccessDependencies
	var err error
	if tail {
		a, ok := w.authority.(ec.AttachmentContentReadTailAuthority)
		if !ok {
			return deps, failure(f.DependencyUnbound, nil)
		}
		deps, err = a.DiscoverAttachmentContentReadTail(ctx, w.request, request)
	} else {
		if !d.Actor.Equal(w.request.Actor) {
			return deps, failure(f.Forbidden, nil)
		}
		switch d.Kind {
		case oc.SourceAccess:
			if !b.MatchesSource(d.Source) {
				return deps, failure(f.Forbidden, nil)
			}
		case oc.OwnerAccess, oc.ObjectReadAccess:
			if !w.matches(d.Actor, d.Owner) || d.ObjectID.Validate() == nil && d.ObjectID != b.ObjectID {
				return deps, failure(f.Forbidden, nil)
			}
		default:
			return deps, failure(f.Forbidden, nil)
		}
		deps, err = w.authority.DiscoverAttachmentContentRead(ctx, w.request)
	}
	if err != nil {
		return deps, err
	}
	if deps.Validate() != nil {
		return deps, unavailable(nil)
	}
	if tx != (f.Tx{}) {
		if tail {
			err = w.authority.(ec.AttachmentContentReadTailAuthority).ValidateAttachmentContentReadTailInTx(ctx, tx, w.request, request, deps)
		} else {
			err = w.authority.ValidateAttachmentContentReadInTx(ctx, tx, w.request, deps)
		}
		if err != nil {
			return oc.AccessDependencies{}, err
		}
	}
	locks, err := outbox.NormalizeLocks(append(deps.Locks(), artifactLock(b.ArtifactID.String())))
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	raw, _ := json.Marshal([]any{"model-artifact-read-v1", deps.Mapping(), b, tail})
	return oc.NewAccessDependencies(digestBytes(raw), locks)
}
func (p *OwnerProvider) modelAuthorize(ctx context.Context, tx f.Tx, actor i.Actor, owner oc.ObjectOwner, intent i.AccessIntent) (oc.OwnerAuthorization, error) {
	w := modelAttachment(ctx)
	if w == nil || intent != i.Read || !w.matches(actor, owner) {
		return oc.OwnerAuthorization{}, failure(f.Forbidden, nil)
	}
	grant, err := w.authority.AuthorizeAttachmentContentReadInTx(ctx, tx, w.request)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	scope, scopeErr := i.InProject(w.request.Attachment.ProjectID)
	if scopeErr != nil || !grant.Matches(actor, scope, i.Read) {
		return oc.OwnerAuthorization{}, unavailable(nil)
	}
	if locks, ok := p.state().store.(fileReferenceLockStore); !ok {
		return oc.OwnerAuthorization{}, failure(f.DependencyUnbound, nil)
	} else if err = locks.RequireHeldLocks(ctx, tx, []f.LockRequest{artifactLock(owner.Details().ID)}); err != nil {
		return oc.OwnerAuthorization{}, err
	}
	x, err := p.state().store.InTx(tx)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	row, ok, err := loadArtifact(ctx, x, owner.Details().ID)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	if !ok {
		return oc.OwnerAuthorization{}, failure(f.ResourceDeleted, nil)
	}
	ref, _ := w.request.Attachment.Reference()
	source, err := artifactSource(row, ref)
	if err != nil || !w.request.Attachment.MatchesSource(source) {
		return oc.OwnerAuthorization{}, failure(f.VersionConflict, err)
	}
	return oc.NewOwnerAuthorization(oc.OwnerAuthorizationDetails{Actor: actor, Owner: owner, Intent: i.Read, Existence: oc.ExistingOwner, CreationCause: row.cause, Version: row.meta.Details().Version, ReadObjectID: w.request.Attachment.ObjectID})
}

// AttachmentSources only resolves a frozen Artifact through a live private
// model-call witness. It never falls back to a raw object ID or current bytes.
type AttachmentSources struct {
	owners  *OwnerProvider
	objects oc.Objects
}

func NewAttachmentSources(owners *OwnerProvider, objects oc.Objects) (*AttachmentSources, error) {
	if owners == nil || owners.bound() != nil || nilPort(objects) {
		return nil, failure(f.DependencyUnbound, nil)
	}
	return &AttachmentSources{owners, objects}, nil
}
func (s *AttachmentSources) Resolve(ctx context.Context, actor i.Actor, ref oc.BusinessFileRef) (oc.ResolvedSource, error) {
	w := modelAttachment(ctx)
	if w == nil || !w.request.Actor.Equal(actor) || !w.request.Attachment.MatchesReference(ref) {
		return oc.ResolvedSource{}, failure(f.Forbidden, nil)
	}
	deps, err := w.authority.DiscoverAttachmentContentRead(ctx, w.request)
	if err != nil {
		return oc.ResolvedSource{}, err
	}
	locks, err := outbox.NormalizeLocks(append(deps.Locks(), artifactLock(w.request.Attachment.ArtifactID.String())))
	if err != nil {
		return oc.ResolvedSource{}, err
	}
	cause, err := txCause()
	if err != nil {
		return oc.ResolvedSource{}, err
	}
	var source oc.ResolvedSource
	result := s.owners.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.owners.state().store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		if err := w.authority.ValidateAttachmentContentReadInTx(ctx, tx, w.request, deps); err != nil {
			return err
		}
		owner, _ := oc.NewObjectOwner(oc.Artifact, w.request.Attachment.ArtifactID.String(), w.request.Attachment.ProjectID.String())
		if _, err := s.owners.modelAuthorize(ctx, tx, actor, owner, i.Read); err != nil {
			return err
		}
		x, err := s.owners.state().store.InTx(tx)
		if err != nil {
			return err
		}
		row, ok, err := loadArtifact(ctx, x, owner.Details().ID)
		if err != nil {
			return err
		}
		if !ok {
			return failure(f.ResourceDeleted, nil)
		}
		source, err = artifactSource(row, ref)
		return err
	})
	if err = commitError(result); err != nil {
		return oc.ResolvedSource{}, err
	}
	return source, nil
}
func (s *AttachmentSources) ValidateInTx(ctx context.Context, tx f.Tx, actor i.Actor, source oc.ResolvedSource, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
	req, err := oc.NewSourceAccess(oc.AccessRequestDetails{Operation: plan.Details().Request.Details().Operation, Actor: actor, Source: source})
	if err != nil {
		return err
	}
	if err = s.objects.ValidateAccessPlanInTx(ctx, tx, req, plan, locked); err != nil {
		return err
	}
	w := modelAttachment(ctx)
	if w == nil || !w.request.Attachment.MatchesSource(source) {
		return failure(f.Forbidden, nil)
	}
	_, err = s.owners.modelAuthorize(ctx, tx, actor, source.Details().Owner, i.Read)
	return err
}
