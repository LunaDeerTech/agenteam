package artifact

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"time"

	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// Sources binds only real Artifact facts. Other business variants require
// their owning providers in the composition root; they never fall back to a
// raw StoredObject lookup. The same adapter supplies Artifact download Audit.
type Sources struct{ data func() sourceState }
type sourceState struct {
	owners  *OwnerProvider
	objects oc.Objects
	audit   au.Appender
}

func NewSources(owners *OwnerProvider, objects oc.Objects, auditing au.Appender) (*Sources, error) {
	if owners == nil || owners.data == nil || nilPort(objects) || nilPort(auditing) {
		return nil, invalid()
	}
	d := sourceState{owners, objects, auditing}
	return &Sources{func() sourceState { return d }}, nil
}
func (s *Sources) state() sourceState        { return s.data() }
func (Sources) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "artifact_sources") }
func (Sources) MarshalJSON() ([]byte, error) { return []byte(`"artifact_sources"`), nil }
func (*Sources) UnmarshalJSON([]byte) error  { return invalid() }
func (Sources) LogValue() slog.Value         { return slog.StringValue("artifact_sources") }
func referenceOwner(ref oc.BusinessFileRef) (oc.ObjectOwner, error) {
	if ref.Validate() != nil {
		return oc.ObjectOwner{}, invalid()
	}
	d := ref.Details()
	switch d.Kind {
	case oc.ArtifactFile:
		return oc.NewObjectOwner(oc.Artifact, d.ArtifactID, d.ProjectID.String())
	case oc.UploadedObject:
		if d.Receipt.Details().Owner.Details().Kind == oc.Artifact {
			return d.Receipt.Details().Owner, nil
		}
	}
	return oc.ObjectOwner{}, failure(foundation.DependencyUnbound, nil)
}
func artifactSource(row artifactRow, ref oc.BusinessFileRef) (oc.ResolvedSource, error) {
	d := row.meta.Details()
	r := ref.Details()
	if r.Kind == oc.ArtifactFile && (r.ArtifactID != d.Reference.ArtifactID.String() || r.FileID != d.Reference.FileID.String() || r.ProjectID != d.Reference.ProjectID) {
		return oc.ResolvedSource{}, failure(foundation.Forbidden, nil)
	}
	if r.Kind == oc.UploadedObject {
		u := r.Receipt.Details()
		if !u.Owner.Equal(artifactOwner(d.Reference)) || u.ObjectID != d.Object.ID || u.CreationCause != row.cause {
			return oc.ResolvedSource{}, failure(foundation.Forbidden, nil)
		}
	}
	return oc.NewResolvedSource(oc.ResolvedSourceDetails{Reference: ref, Owner: artifactOwner(d.Reference), Meta: d.Object, Revision: d.Version})
}
func (s *Sources) Resolve(ctx context.Context, actor identity.Actor, ref oc.BusinessFileRef) (oc.ResolvedSource, error) {
	owner, err := referenceOwner(ref)
	if err != nil {
		return oc.ResolvedSource{}, err
	}
	p := s.state().owners
	if _, err = p.AuthorizeOwner(ctx, actor, owner, identity.Read); err != nil {
		return oc.ResolvedSource{}, err
	}
	if ref.Details().Kind == oc.UploadedObject {
		// The current object lookup validates original actor, receipt disposition,
		// owner and cause. This never opens MinIO or replaces atomic SourceReads.
		f, ok, err := ownerByID(ctx, p.state().store, owner.Details().ID)
		if err != nil {
			return oc.ResolvedSource{}, err
		}
		if !ok || f.exists {
			return oc.ResolvedSource{}, failure(foundation.InvalidState, nil)
		}
		lookup, err := s.state().objects.LookupPut(ctx, actor, owner, foundation.IdempotencyKey(f.commandKey))
		if err != nil {
			return oc.ResolvedSource{}, err
		}
		if lookup.State != oc.UploadCommitted || lookup.Meta == nil || referenceStorage(ref).Receipt != lookup.Receipt.Details().ID.String() {
			return oc.ResolvedSource{}, failure(foundation.Forbidden, nil)
		}
		stored, err := uploadSource(ctx, p.state().store, actor, ref)
		if err != nil {
			return oc.ResolvedSource{}, err
		}
		if stored.Details().Meta.ID != lookup.Meta.ID || stored.Details().Meta.SHA256 != lookup.Meta.SHA256 {
			return oc.ResolvedSource{}, unavailable(nil)
		}
		return stored, nil
	}
	row, ok, err := loadArtifact(ctx, p.state().store, owner.Details().ID)
	if err != nil {
		return oc.ResolvedSource{}, err
	}
	if !ok {
		return oc.ResolvedSource{}, failure(foundation.Forbidden, nil)
	}
	return artifactSource(row, ref)
}
func (s *Sources) ValidateInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, source oc.ResolvedSource, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
	if source.Validate() != nil {
		return invalid()
	}
	operation := plan.Details().Request.Details().Operation
	req, err := oc.NewSourceAccess(oc.AccessRequestDetails{Operation: operation, Actor: actor, Source: source})
	if err != nil {
		return err
	}
	if err = s.state().objects.ValidateAccessPlanInTx(ctx, tx, req, plan, locked); err != nil {
		return err
	}
	p := s.state().owners
	owner, err := referenceOwner(source.Details().Reference)
	if err != nil {
		return err
	}
	if _, err = p.AuthorizeOwnerInTx(ctx, tx, actor, owner, identity.Read); err != nil {
		return err
	}
	e, err := p.state().store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	var current oc.ResolvedSource
	if source.Details().Reference.Details().Kind == oc.UploadedObject {
		current, err = uploadSource(ctx, e, actor, source.Details().Reference)
	} else {
		var row artifactRow
		var ok bool
		row, ok, err = loadArtifact(ctx, e, owner.Details().ID)
		if err == nil && !ok {
			return failure(foundation.Forbidden, nil)
		}
		if err == nil {
			current, err = artifactSource(row, source.Details().Reference)
		}
	}
	if err != nil {
		return err
	}
	if !sameSource(current, source) {
		return foundation.NewFault(foundation.ResourceBusy, foundation.NotCommitted)
	}
	return nil
}
func uploadSource(ctx context.Context, e postgres.SQLExecutor, actor identity.Actor, ref oc.BusinessFileRef) (oc.ResolvedSource, error) {
	u := ref.Details().Receipt.Details()
	var object, upload, receipt, original, cause, media, state string
	var size, version int64
	var sha []byte
	var created *time.Time
	err := e.QueryRow(ctx, `SELECT COALESCE(target_object_id::text,''),COALESCE(target_upload_id::text,''),COALESCE(receipt_id::text,''),stable_actor,creation_cause,media_type,byte_size,sha256,COALESCE(object_version,0),object_created_at,state FROM agenteam_artifact.upload_intents WHERE id=$1 AND project_id=$2`, u.Owner.Details().ID, u.Owner.Details().ProjectID).Scan(&object, &upload, &receipt, &original, &cause, &media, &size, &sha, &version, &created, &state)
	if noRows(err) {
		return oc.ResolvedSource{}, failure(foundation.Forbidden, nil)
	}
	if err != nil {
		return oc.ResolvedSource{}, unavailable(err)
	}
	if state != "uploaded" || created == nil || original != stableActor(actor) || object != u.ObjectID.String() || upload != u.UploadID.String() || receipt != u.ID.String() || cause != u.CreationCause {
		return oc.ResolvedSource{}, failure(foundation.Forbidden, nil)
	}
	at, err := foundation.NewInstant(*created)
	if err != nil {
		return oc.ResolvedSource{}, unavailable(err)
	}
	return oc.NewResolvedSource(oc.ResolvedSourceDetails{Reference: ref, Owner: u.Owner, Meta: oc.ObjectMeta{ID: u.ObjectID, Scope: u.Owner.Scope(), MediaType: media, ByteSize: foundation.Progress(size), SHA256: rawDigest(sha), State: oc.Available, Version: foundation.Version(version), CreatedAt: at}, Revision: 1})
}
func (s *Sources) ResolveDownload(ctx context.Context, actor identity.Actor, ref oc.BusinessFileRef) (oc.DownloadTarget, error) {
	if actor.Validate() != nil || actor.Details().Kind != identity.Human {
		return oc.DownloadTarget{}, failure(foundation.Forbidden, nil)
	}
	owner, err := referenceOwner(ref)
	if err != nil {
		return oc.DownloadTarget{}, err
	}
	p := s.state().owners
	if _, err = p.AuthorizeOwner(ctx, actor, owner, identity.Read); err != nil {
		return oc.DownloadTarget{}, err
	}
	row, ok, err := loadArtifact(ctx, p.state().store, owner.Details().ID)
	if err != nil {
		return oc.DownloadTarget{}, err
	}
	if !ok {
		return oc.DownloadTarget{}, failure(foundation.Forbidden, nil)
	}
	source, err := artifactSource(row, ref)
	if err != nil {
		return oc.DownloadTarget{}, err
	}
	return oc.NewDownloadTarget(oc.DownloadTargetDetails{Source: source, Filename: row.meta.Details().Name})
}
func (s *Sources) ValidateDownloadInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, target oc.DownloadTarget, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
	if target.Validate() != nil || actor.Validate() != nil || actor.Details().Kind != identity.Human {
		return failure(foundation.Forbidden, nil)
	}
	d := target.Details()
	req, err := oc.NewSourceAccess(oc.AccessRequestDetails{Operation: oc.ValidateSourceAccess, Actor: actor, Source: d.Source})
	if err != nil {
		return err
	}
	if err = s.state().objects.ValidateAccessPlanInTx(ctx, tx, req, plan, locked); err != nil {
		return err
	}
	p := s.state().owners
	owner, err := referenceOwner(d.Source.Details().Reference)
	if err != nil {
		return err
	}
	if _, err = p.AuthorizeOwnerInTx(ctx, tx, actor, owner, identity.Read); err != nil {
		return err
	}
	e, err := p.state().store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	row, ok, err := loadArtifact(ctx, e, owner.Details().ID)
	if err != nil {
		return err
	}
	if !ok {
		return failure(foundation.Forbidden, nil)
	}
	current, err := artifactSource(row, d.Source.Details().Reference)
	if err != nil {
		return err
	}
	if !sameSource(current, d.Source) || row.meta.Details().Name != d.Filename {
		return failure(foundation.Forbidden, nil)
	}
	return nil
}
func (s *Sources) AppendDownloadInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, target oc.DownloadTarget, event oc.DownloadEvent, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
	if event.Validate() != nil {
		return invalid()
	}
	if err := s.ValidateDownloadInTx(ctx, tx, actor, target, plan, locked); err != nil {
		return err
	}
	d := target.Details()
	v := event
	m := d.Source.Details().Meta
	phase := au.ContentPhase(v.Phase)
	outcome := au.Success
	var reason au.Reason
	if v.Phase == oc.DownloadFailed {
		outcome = au.Failed
		switch v.Failure {
		case oc.DownloadCancelled:
			reason = au.Cancelled
		case oc.DownloadIntegrityFailed:
			reason = au.IntegrityMismatch
		case oc.DownloadReadFailed:
			reason = au.StorageUnavailable
		default:
			reason = au.InternalError
		}
	}
	metadata, err := au.ArtifactMetadata(au.ArtifactDownload, au.ArtifactMetadataFields{ArtifactID: d.Source.Details().Owner.Details().ID, ObjectID: m.ID.String(), MediaType: auditMedia(m.MediaType), ByteSize: foundation.Progress(v.Length), SentBytes: foundation.Progress(v.SentBytes), Phase: phase, Reason: reason})
	if err != nil {
		return err
	}
	resource, _ := au.NewResource(au.ArtifactResource, d.Source.Details().Owner.Details().ID)
	entry, err := au.NewEntry(au.EntryFields{Scope: m.Scope, Actor: actor, Action: au.ArtifactDownload, Outcome: outcome, Resource: resource, Metadata: metadata})
	if err != nil {
		return err
	}
	digest, err := jsonDigest([]string{"download", v.GrantID.String(), v.AttemptID.String(), string(v.Phase)})
	if err != nil {
		return err
	}
	key, err := au.NewAppendKey(au.ArtifactProducer, digest.String(), 0)
	if err != nil {
		return err
	}
	_, err = s.state().audit.AppendInTx(ctx, tx, entry, key)
	return portOrNil(err)
}
func auditMedia(media string) string {
	base, _, err := mime.ParseMediaType(media)
	if err != nil {
		return "application/octet-stream"
	}
	return base
}
