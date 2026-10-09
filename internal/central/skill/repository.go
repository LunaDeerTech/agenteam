package skill

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5"
)

const initializationColumns = `project_id::text,creation_id::text,initialization_key,skill_id::text,revision_id::text,semantic_digest,bundle_id,revision,package_sha256,manifest_sha256,byte_size,manifest,name,description,phase,version,COALESCE(object_id::text,''),COALESCE(upload_id::text,''),COALESCE(current_attempt_id::text,''),COALESCE(safe_reason,''),created_at,updated_at`

func loadInitialization(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID) (*initializationRow, error) {
	return scanInitialization(x.QueryRow(ctx, `SELECT `+initializationColumns+` FROM agenteam_skill.initializations WHERE project_id=$1`, project.String()))
}
func scanInitialization(source postgres.Row) (*initializationRow, error) {
	var project, creation, key, skill, revision, semantic, bundle, pkg, manifestDigest, name, description, phase, object, upload, attempt, reason string
	var number, size, version int64
	var manifestRaw []byte
	var created, updated time.Time
	e := source.Scan(&project, &creation, &key, &skill, &revision, &semantic, &bundle, &number, &pkg, &manifestDigest, &size, &manifestRaw, &name, &description, &phase, &version, &object, &upload, &attempt, &reason, &created, &updated)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, unavailable(e)
	}
	p, e := f.ParseID[id.Project](project)
	if e != nil {
		return nil, unavailable(e)
	}
	c, e := f.ParseID[pc.Creation](creation)
	if e != nil {
		return nil, unavailable(e)
	}
	s, e := f.ParseID[pc.Skill](skill)
	if e != nil {
		return nil, unavailable(e)
	}
	r, e := f.ParseID[sc.Revision](revision)
	if e != nil {
		return nil, unavailable(e)
	}
	manifest, e := sc.DecodeManifest(manifestRaw)
	if e != nil {
		return nil, unavailable(e)
	}
	start, e := f.NewInstant(created)
	if e != nil {
		return nil, unavailable(e)
	}
	end, e := f.NewInstant(updated)
	if e != nil {
		return nil, unavailable(e)
	}
	out := initializationRow{request: pc.InitializationRequest{ProjectID: p, CreationID: c, InitializationKey: f.IdempotencyKey(key)}, skill: s, revision: r, semantic: f.Digest(semantic), bundle: frozenBundle{bundle, f.Revision(number), manifest, f.Digest(pkg), f.Digest(manifestDigest), f.Progress(size), name, description}, phase: initializationPhase(phase), version: f.Version(version), reason: pc.SafeReason(reason), created: start, updated: end}
	if object != "" {
		out.object, e = f.ParseID[oc.StoredObject](object)
		if e != nil {
			return nil, unavailable(e)
		}
	}
	if upload != "" {
		out.upload, e = f.ParseID[oc.Upload](upload)
		if e != nil {
			return nil, unavailable(e)
		}
	}
	if attempt != "" {
		out.attempt, e = f.ParseID[oc.Attempt](attempt)
		if e != nil {
			return nil, unavailable(e)
		}
	}
	if e = out.validate(); e != nil {
		return nil, e
	}
	return &out, nil
}
func insertInitialization(ctx context.Context, x postgres.SQLExecutor, r initializationRow) error {
	if e := r.validate(); e != nil {
		return e
	}
	if r.phase != initializationPlanned || r.version != 1 {
		return invalid()
	}
	manifest, e := json.Marshal(r.bundle.manifest)
	if e != nil {
		return unavailable(e)
	}
	_, e = x.Exec(ctx, `INSERT INTO agenteam_skill.initializations(project_id,creation_id,initialization_key,skill_id,revision_id,semantic_digest,bundle_id,revision,package_sha256,manifest_sha256,byte_size,manifest,name,description,phase,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'planned',1,$15,$16)`, r.request.ProjectID.String(), r.request.CreationID.String(), string(r.request.InitializationKey), r.skill.String(), r.revision.String(), string(r.semantic), r.bundle.id, int64(r.bundle.revision), string(r.bundle.packageDigest), string(r.bundle.manifestDigest), int64(r.bundle.size), manifest, r.bundle.name, r.bundle.description, r.created.Time(), r.updated.Time())
	if e != nil {
		return unavailable(e)
	}
	return nil
}

// Read the immutable publication in the caller's authorized live transaction.
// Object scope/meta are reconstructed through trusted types, never JSON input.
func loadPublished(ctx context.Context, x postgres.SQLExecutor, r initializationRow) (sc.Metadata, sc.RevisionMetadata, error) {
	empty, emptyRevision := sc.Metadata{}, sc.RevisionMetadata{}
	if e := r.validate(); e != nil {
		return empty, emptyRevision, e
	}
	if r.phase != initializationPublished {
		return empty, emptyRevision, fault(f.InvalidState)
	}
	var project, skill, revision, name, normalized, description, object string
	var current, version, revisionNumber, objectVersion int64
	var protected, serving bool
	var objectCreated, published time.Time
	e := x.QueryRow(ctx, `SELECT s.project_id::text,s.id::text,s.revision_id::text,s.name,s.normalized_name,s.description,s.protected,s.current_revision,s.version,s.serving,r.revision,r.object_id::text,r.object_version,r.object_created_at,r.published_at FROM agenteam_skill.skills s JOIN agenteam_skill.revisions r ON r.id=s.revision_id AND r.skill_id=s.id AND r.project_id=s.project_id WHERE s.project_id=$1 AND s.id=$2`, r.request.ProjectID.String(), r.skill.String()).Scan(&project, &skill, &revision, &name, &normalized, &description, &protected, &current, &version, &serving, &revisionNumber, &object, &objectVersion, &objectCreated, &published)
	if e != nil {
		return empty, emptyRevision, unavailable(e)
	}
	if project != r.request.ProjectID.String() || skill != r.skill.String() || revision != r.revision.String() || object != r.object.String() || current != int64(r.bundle.revision) || revisionNumber != current || !protected || name != r.bundle.name || normalized != AddSkillsNormalizedName || description != r.bundle.description {
		return empty, emptyRevision, unavailable(nil)
	}
	if !serving {
		return empty, emptyRevision, fault(f.ResourceDeleted)
	}
	meta, e := sc.NewMetadata(sc.Metadata{ID: r.skill, ProjectID: r.request.ProjectID, Name: name, NormalizedName: normalized, Description: description, Protected: protected, CurrentRevision: f.Revision(current), Version: f.Version(version)})
	if e != nil {
		return empty, emptyRevision, unavailable(e)
	}
	scope, e := id.InProject(r.request.ProjectID)
	if e != nil {
		return empty, emptyRevision, unavailable(e)
	}
	createdAt, e := f.NewInstant(objectCreated)
	if e != nil {
		return empty, emptyRevision, unavailable(e)
	}
	publishedAt, e := f.NewInstant(published)
	if e != nil {
		return empty, emptyRevision, unavailable(e)
	}
	objectMeta := oc.ObjectMeta{ID: r.object, Scope: scope, MediaType: sc.PackageMediaType, ByteSize: r.bundle.size, SHA256: r.bundle.packageDigest, State: oc.Available, Version: f.Version(objectVersion), CreatedAt: createdAt}
	out, e := sc.NewRevisionMetadata(sc.RevisionMetadata{ID: r.revision, SkillID: r.skill, ProjectID: r.request.ProjectID, Revision: f.Revision(current), Name: name, Description: description, EntryPath: sc.EntryPath, Object: objectMeta, Manifest: r.bundle.manifest, PackageSHA256: r.bundle.packageDigest, PublishedAt: publishedAt})
	if e != nil {
		return empty, emptyRevision, unavailable(e)
	}
	return meta, out, nil
}
