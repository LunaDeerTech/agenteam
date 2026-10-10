package skill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5"
)

type frozenInstallation struct {
	name, normalized, description string
	packageDigest, manifestDigest f.Digest
	size                          f.Progress
	manifest                      sc.Manifest
}

func freezeInstallation(v installInput) frozenInstallation {
	return frozenInstallation{v.name, v.normalized, v.description, v.packageDigest, v.manifestDigest, v.size, v.manifest}
}
func (v frozenInstallation) validate(project id.ProjectID, skill sc.SkillID) error {
	if v.manifest.Validate() != nil || v.packageDigest.Validate() != nil || v.manifestDigest.Validate() != nil || v.size <= 0 || v.size > sc.MaxArchiveBytes || v.normalized == AddSkillsNormalizedName {
		return unavailable(nil)
	}
	if (sc.Metadata{ID: skill, ProjectID: project, Name: v.name, NormalizedName: v.normalized, Description: v.description, CurrentRevision: 1, Version: 1}).Validate() != nil {
		return unavailable(nil)
	}
	d, err := v.manifest.Digest()
	if err != nil || d != v.manifestDigest {
		return unavailable(err)
	}
	return nil
}
func installationSemantic(project id.ProjectID, user id.UserID, skill sc.SkillID, v frozenInstallation) (f.Digest, error) {
	if user.Validate() != nil || v.validate(project, skill) != nil {
		return "", invalid()
	}
	raw, err := json.Marshal(struct {
		Format, Project, User, Skill, Name, Normalized, Description string
		Package, Manifest                                           f.Digest
		Size                                                        f.Progress
	}{"skill.install.v1", project.String(), user.String(), skill.String(), v.name, v.normalized, v.description, v.packageDigest, v.manifestDigest, v.size})
	if err != nil {
		return "", unavailable(err)
	}
	return sum(raw), nil
}

type installationPhase string

const (
	installationPlanned   installationPhase = "planned"
	installationReserved  installationPhase = "reserved"
	installationPublished installationPhase = "published"
	installationFailed    installationPhase = "failed"
)

type installationRow struct {
	id               InstallationID
	project          id.ProjectID
	user             id.UserID
	key              f.IdempotencyKey
	skill            sc.SkillID
	revision         sc.RevisionID
	semantic         f.Digest
	pkg              frozenInstallation
	phase            installationPhase
	version          f.Version
	object           oc.ObjectID
	upload           oc.UploadID
	attempt          oc.AttemptID
	reason           string
	created, updated f.Instant
}

func (installationRow) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "skill_installation") }
func (installationRow) LogValue() slog.Value       { return slog.StringValue("skill_installation") }
func (installationRow) MarshalJSON() ([]byte, error) {
	return []byte(`"skill_installation"`), nil
}

func (r installationRow) validate() error {
	if r.id.Validate() != nil || r.key.Validate() != nil || r.revision.Validate() != nil || r.version.Validate() != nil || r.created.Validate() != nil || r.updated.Validate() != nil || r.updated.Time().Before(r.created.Time()) {
		return unavailable(nil)
	}
	semantic, err := installationSemantic(r.project, r.user, r.skill, r.pkg)
	if err != nil || semantic != r.semantic {
		return unavailable(err)
	}
	empty := r.object == (oc.ObjectID{}) && r.upload == (oc.UploadID{}) && r.attempt == (oc.AttemptID{})
	bound := r.object.Validate() == nil && r.upload.Validate() == nil && r.attempt.Validate() == nil
	if !empty && !bound {
		return unavailable(nil)
	}
	switch r.phase {
	case installationPlanned:
		if !empty || r.reason != "" {
			return unavailable(nil)
		}
	case installationReserved, installationPublished:
		if !bound || r.reason != "" {
			return unavailable(nil)
		}
	case installationFailed:
		switch r.reason {
		case "dependency_unavailable", "work_pending", "outcome_unknown", "operation_failed":
		default:
			return unavailable(nil)
		}
	default:
		return unavailable(nil)
	}
	return nil
}
func (r installationRow) owner() (oc.ObjectOwner, error) {
	return oc.NewObjectOwner(oc.SkillRevision, r.revision.String(), r.project.String())
}
func (r installationRow) identity() (f.CommandIdentity, error) {
	return installIdentity(r.project, r.key)
}
func (r installationRow) locks(mode f.LockMode) ([]f.LockRequest, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	command, err := r.identity()
	if err != nil {
		return nil, err
	}
	locks := []f.LockRequest{commandLock(command), userLock(r.user.String(), f.Exclusive), projectLock(r.project, mode), skillLock(r.skill, mode)}
	if r.object.Validate() == nil {
		locks = append(locks, objectLock(r.object, mode))
	}
	return oc.NormalizeAccessLocks(locks)
}
func (r installationRow) receipt() (InstallReceipt, error) {
	if r.validate() != nil || r.phase != installationPublished {
		return InstallReceipt{}, fault(f.InvalidState)
	}
	result := InstallReceipt{r.id, r.skill, r.project, r.revision, 1, 1, r.object, r.pkg.packageDigest}
	return result, result.Validate()
}

const installationColumns = `id::text,project_id::text,actor_user_id::text,command_key,skill_id::text,revision_id::text,semantic_digest,package_sha256,manifest_sha256,byte_size,manifest,name,normalized_name,description,phase,version,COALESCE(object_id::text,''),COALESCE(upload_id::text,''),COALESCE(current_attempt_id::text,''),COALESCE(safe_reason,''),created_at,updated_at`

func loadInstallation(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID, key f.IdempotencyKey) (*installationRow, error) {
	return scanInstallation(x.QueryRow(ctx, `SELECT `+installationColumns+` FROM agenteam_skill.installations WHERE project_id=$1 AND command_key=$2`, project.String(), key.String()))
}
func loadInstallationRevision(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID, revision sc.RevisionID) (*installationRow, error) {
	return scanInstallation(x.QueryRow(ctx, `SELECT `+installationColumns+` FROM agenteam_skill.installations WHERE project_id=$1 AND revision_id=$2`, project.String(), revision.String()))
}
func loadInstallationSkill(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID, skill sc.SkillID) (*installationRow, error) {
	return scanInstallation(x.QueryRow(ctx, `SELECT `+installationColumns+` FROM agenteam_skill.installations WHERE project_id=$1 AND skill_id=$2`, project.String(), skill.String()))
}
func scanInstallation(row postgres.Row) (*installationRow, error) {
	var rid, project, user, key, skill, revision, semantic, pkg, manifestDigest, name, normalized, description, phase, object, upload, attempt, reason string
	var size, version int64
	var manifest []byte
	var created, updated time.Time
	err := row.Scan(&rid, &project, &user, &key, &skill, &revision, &semantic, &pkg, &manifestDigest, &size, &manifest, &name, &normalized, &description, &phase, &version, &object, &upload, &attempt, &reason, &created, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	r := installationRow{key: f.IdempotencyKey(key), semantic: f.Digest(semantic), phase: installationPhase(phase), version: f.Version(version), reason: reason,
		pkg: frozenInstallation{name: name, normalized: normalized, description: description, packageDigest: f.Digest(pkg), manifestDigest: f.Digest(manifestDigest), size: f.Progress(size)}}
	if r.id, err = f.ParseID[Installation](rid); err != nil {
		return nil, unavailable(err)
	}
	if r.project, err = f.ParseID[id.Project](project); err != nil {
		return nil, unavailable(err)
	}
	if r.user, err = f.ParseID[id.User](user); err != nil {
		return nil, unavailable(err)
	}
	if r.skill, err = f.ParseID[pc.Skill](skill); err != nil {
		return nil, unavailable(err)
	}
	if r.revision, err = f.ParseID[sc.Revision](revision); err != nil {
		return nil, unavailable(err)
	}
	if r.pkg.manifest, err = sc.DecodeManifest(manifest); err != nil {
		return nil, unavailable(err)
	}
	if r.created, err = f.NewInstant(created); err != nil {
		return nil, unavailable(err)
	}
	if r.updated, err = f.NewInstant(updated); err != nil {
		return nil, unavailable(err)
	}
	if object != "" {
		if r.object, err = f.ParseID[oc.StoredObject](object); err != nil {
			return nil, unavailable(err)
		}
	}
	if upload != "" {
		if r.upload, err = f.ParseID[oc.Upload](upload); err != nil {
			return nil, unavailable(err)
		}
	}
	if attempt != "" {
		if r.attempt, err = f.ParseID[oc.Attempt](attempt); err != nil {
			return nil, unavailable(err)
		}
	}
	if err = r.validate(); err != nil {
		return nil, err
	}
	return &r, nil
}
func insertInstallation(ctx context.Context, x postgres.SQLExecutor, r installationRow) error {
	if r.validate() != nil || r.phase != installationPlanned || r.version != 1 {
		return invalid()
	}
	manifest, err := r.pkg.manifest.MarshalJSON()
	if err != nil {
		return unavailable(err)
	}
	_, err = x.Exec(ctx, `INSERT INTO agenteam_skill.installations(id,project_id,actor_user_id,command_key,skill_id,revision_id,semantic_digest,package_sha256,manifest_sha256,byte_size,manifest,name,normalized_name,description,phase,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'planned',1,$15,$15)`, r.id.String(), r.project.String(), r.user.String(), r.key.String(), r.skill.String(), r.revision.String(), r.semantic.String(), r.pkg.packageDigest.String(), r.pkg.manifestDigest.String(), int64(r.pkg.size), manifest, r.pkg.name, r.pkg.normalized, r.pkg.description, r.created.Time())
	return portError(err)
}
func reserveInstallation(ctx context.Context, x postgres.SQLExecutor, r installationRow, attempt oc.UploadAttempt, process oc.ProcessID) error {
	if r.validate() != nil || r.phase != installationPlanned || attempt.Validate() != nil || process.Validate() != nil {
		return invalid()
	}
	d := attempt.Details()
	_, err := x.Exec(ctx, `INSERT INTO agenteam_skill.installation_attempts(attempt_id,project_id,installation_id,skill_id,revision_id,object_id,upload_id,process_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,clock_timestamp())`, d.ID.String(), r.project.String(), r.id.String(), r.skill.String(), r.revision.String(), d.ObjectID.String(), d.UploadID.String(), process.String())
	if err != nil {
		return unavailable(err)
	}
	tag, err := x.Exec(ctx, `UPDATE agenteam_skill.installations SET phase='reserved',object_id=$3,upload_id=$4,current_attempt_id=$5,version=version+1,updated_at=clock_timestamp() WHERE project_id=$1 AND id=$2 AND phase='planned' AND version=$6`, r.project.String(), r.id.String(), d.ObjectID.String(), d.UploadID.String(), d.ID.String(), int64(r.version))
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ResourceBusy)
	}
	return nil
}
