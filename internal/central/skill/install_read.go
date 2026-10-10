package skill

import (
	"context"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// The command phase is not a publication receipt. Read the exact canonical
// Skill, revision and original attempt in the caller's already-locked Tx.
func loadInstalled(ctx context.Context, x postgres.SQLExecutor, r installationRow) (sc.Metadata, sc.RevisionMetadata, error) {
	return loadInstalledCore(ctx, x, r, false)
}

// Cleanup alone reads the same immutable origin after its serving gate closed.
// No returned Object metadata is an authorization or a physical deletion proof.
func loadInstalledCore(ctx context.Context, x postgres.SQLExecutor, r installationRow, closed bool) (sc.Metadata, sc.RevisionMetadata, error) {
	empty, emptyRevision := sc.Metadata{}, sc.RevisionMetadata{}
	if err := r.validate(); err != nil {
		return empty, emptyRevision, err
	}
	if r.phase != installationPublished {
		return empty, emptyRevision, fault(f.InvalidState)
	}
	var project, skill, revision, origin, revisionOrigin, name, normalized, description, object, attempt, process string
	var current, version, revisionNumber, objectVersion int64
	var protected, serving, noCreation bool
	var objectCreated, published time.Time
	err := x.QueryRow(ctx, `SELECT s.project_id::text,s.id::text,s.revision_id::text,s.installation_id::text,r.installation_id::text,s.creation_id IS NULL,s.name,s.normalized_name,s.description,s.protected,s.current_revision,s.version,s.serving,r.revision,r.object_id::text,r.object_version,r.object_created_at,r.published_at,a.attempt_id::text,a.process_id::text FROM agenteam_skill.skills s JOIN agenteam_skill.revisions r ON r.id=s.revision_id AND r.skill_id=s.id AND r.project_id=s.project_id JOIN agenteam_skill.installation_attempts a ON a.attempt_id=$3 AND a.project_id=s.project_id AND a.installation_id=s.installation_id AND a.skill_id=s.id AND a.revision_id=r.id AND a.object_id=r.object_id AND a.upload_id=$4 WHERE s.project_id=$1 AND s.id=$2`, r.project.String(), r.skill.String(), r.attempt.String(), r.upload.String()).Scan(&project, &skill, &revision, &origin, &revisionOrigin, &noCreation, &name, &normalized, &description, &protected, &current, &version, &serving, &revisionNumber, &object, &objectVersion, &objectCreated, &published, &attempt, &process)
	if err != nil {
		return empty, emptyRevision, unavailable(err)
	}
	_, processErr := f.ParseID[oc.Process](process)
	if project != r.project.String() || skill != r.skill.String() || revision != r.revision.String() || origin != r.id.String() || revisionOrigin != origin || !noCreation || protected || name != r.pkg.name || normalized != r.pkg.normalized || description != r.pkg.description || current != 1 || revisionNumber != 1 || object != r.object.String() || attempt != r.attempt.String() || processErr != nil {
		return empty, emptyRevision, unavailable(nil)
	}
	if serving == closed {
		if !closed {
			return empty, emptyRevision, fault(f.ResourceDeleted)
		}
		return empty, emptyRevision, unavailable(nil)
	}
	meta, err := sc.NewMetadata(sc.Metadata{ID: r.skill, ProjectID: r.project, Name: name, NormalizedName: normalized, Description: description, CurrentRevision: 1, Version: f.Version(version)})
	if err != nil {
		return empty, emptyRevision, unavailable(err)
	}
	scope, err := id.InProject(r.project)
	if err != nil {
		return empty, emptyRevision, unavailable(err)
	}
	createdAt, err := f.NewInstant(objectCreated)
	if err != nil {
		return empty, emptyRevision, unavailable(err)
	}
	publishedAt, err := f.NewInstant(published)
	if err != nil {
		return empty, emptyRevision, unavailable(err)
	}
	objectMeta := oc.ObjectMeta{ID: r.object, Scope: scope, MediaType: sc.PackageMediaType, ByteSize: r.pkg.size, SHA256: r.pkg.packageDigest, State: oc.Available, Version: f.Version(objectVersion), CreatedAt: createdAt}
	revisionMeta, err := sc.NewRevisionMetadata(sc.RevisionMetadata{ID: r.revision, SkillID: r.skill, ProjectID: r.project, Revision: 1, Name: name, Description: description, EntryPath: sc.EntryPath, Object: objectMeta, Manifest: r.pkg.manifest, PackageSHA256: r.pkg.packageDigest, PublishedAt: publishedAt})
	if err != nil {
		return empty, emptyRevision, unavailable(err)
	}
	return meta, revisionMeta, nil
}

// Reader admission is equivalent for both sources: Delete stops both and
// Archive admits both. Change the local ledger kind under its own lock before
// registering the ordinary source's durable work, without opening a new call.
func (s *Service) installedReaderWork(row installationRow, call *serviceCall) (*ownedWork, error) {
	state := s.state()
	state.mu.Lock()
	_, admitted := state.calls[call]
	if !admitted || call.kind != packageReaderWork {
		state.mu.Unlock()
		return nil, fault(f.InvalidState)
	}
	call.kind = installedPackageReaderWork
	state.mu.Unlock()
	return s.newInstallationOwnedWork(row, installedPackageReaderWork, call)
}
