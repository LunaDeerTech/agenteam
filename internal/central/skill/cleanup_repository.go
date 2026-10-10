package skill

import (
	"context"
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

const cleanupBatchLimit = 32

type cleanupPhase string

const (
	cleanupGated     cleanupPhase = "gated"
	cleanupPending   cleanupPhase = "pending"
	cleanupCompleted cleanupPhase = "completed"
)

// The durable gate retains the original lifecycle and initialization parents.
// This row records no Object-private payload, lease or completion proof.
type cleanupRow struct {
	id       oc.CleanupID
	project  id.ProjectID
	cause    pc.LifecycleCause
	skill    sc.SkillID
	revision sc.RevisionID
	object   oc.ObjectID
	upload   oc.UploadID
	phase    cleanupPhase
	version  f.Version
}

func (c cleanupRow) matches(r initializationRow) bool {
	return c.project == r.request.ProjectID && c.skill == r.skill && c.revision == r.revision && c.object == r.object && c.upload == r.upload
}

func (c cleanupRow) objectCause(r initializationRow) (oc.ObjectCleanupCause, error) {
	if !c.matches(r) || c.id.Validate() != nil || c.cause.Validate() != nil || c.cause.Action != pc.Delete {
		return oc.ObjectCleanupCause{}, unavailable(nil)
	}
	owner, err := r.owner()
	if err != nil {
		return oc.ObjectCleanupCause{}, err
	}
	return oc.NewObjectCleanupCause(oc.CleanupDetails{OperationID: c.id, Owner: owner, Reason: oc.ProjectDeleted})
}

func loadCleanup(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID) (*cleanupRow, error) {
	// A published Project has one revision and one accepted Delete. Detect a
	// conflicting older gate instead of arbitrarily choosing one of its rows.
	var ids []string
	err := x.QueryRow(ctx, `SELECT COALESCE(array_agg(id::text ORDER BY id),ARRAY[]::text[]) FROM (SELECT id FROM agenteam_skill.cleanup WHERE project_id=$1 ORDER BY id LIMIT 2) gates`, project.String()).Scan(&ids)
	if err != nil {
		return nil, unavailable(err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) != 1 {
		return nil, unavailable(nil)
	}
	var c cleanupRow
	var identity, p, operation, action, skill, revision, object, upload, phase string
	var version, projectVersion int64
	var created, updated time.Time
	err = x.QueryRow(ctx, `SELECT id::text,project_id::text,lifecycle_operation_id::text,project_version,action,skill_id::text,revision_id::text,object_id::text,upload_id::text,phase,version,created_at,updated_at FROM agenteam_skill.cleanup WHERE id=$1 AND project_id=$2`, ids[0], project.String()).Scan(&identity, &p, &operation, &projectVersion, &action, &skill, &revision, &object, &upload, &phase, &version, &created, &updated)
	if err != nil {
		return nil, unavailable(err)
	}
	if c.id, err = f.ParseID[oc.CleanupOperation](identity); err != nil {
		return nil, unavailable(err)
	}
	if c.project, err = f.ParseID[id.Project](p); err != nil {
		return nil, unavailable(err)
	}
	if c.cause.OperationID, err = f.ParseID[pc.Operation](operation); err != nil {
		return nil, unavailable(err)
	}
	if c.skill, err = f.ParseID[pc.Skill](skill); err != nil {
		return nil, unavailable(err)
	}
	if c.revision, err = f.ParseID[sc.Revision](revision); err != nil {
		return nil, unavailable(err)
	}
	if c.object, err = f.ParseID[oc.StoredObject](object); err != nil {
		return nil, unavailable(err)
	}
	if c.upload, err = f.ParseID[oc.Upload](upload); err != nil {
		return nil, unavailable(err)
	}
	c.cause.Action, c.cause.ProjectVersion = pc.LifecycleAction(action), f.Version(projectVersion)
	c.phase, c.version = cleanupPhase(phase), f.Version(version)
	_, createdErr := f.NewInstant(created)
	_, updatedErr := f.NewInstant(updated)
	if c.project != project || c.id.String() != ids[0] || c.cause.Validate() != nil || c.cause.Action != pc.Delete || c.version.Validate() != nil || createdErr != nil || updatedErr != nil || updated.Before(created) || c.phase != cleanupGated && c.phase != cleanupPending && c.phase != cleanupCompleted {
		return nil, unavailable(nil)
	}
	return &c, nil
}

// Read the real published core even after serving has been closed. The normal
// reader deliberately rejects that state and cannot authorize this path.
func cleanupCore(ctx context.Context, x postgres.SQLExecutor, r initializationRow, closed bool) error {
	if r.validate() != nil || r.phase != initializationPublished {
		return unavailable(nil)
	}
	var creation, revision, name, normalized, description, object, attempt, process string
	var protected, serving bool
	var current, version, revisionNumber, objectVersion int64
	var objectCreated, published time.Time
	err := x.QueryRow(ctx, `SELECT s.creation_id::text,s.revision_id::text,s.name,s.normalized_name,s.description,s.protected,s.current_revision,s.version,s.serving,r.revision,r.object_id::text,r.object_version,r.object_created_at,r.published_at,a.attempt_id::text,a.process_id::text FROM agenteam_skill.skills s JOIN agenteam_skill.revisions r ON r.project_id=s.project_id AND r.skill_id=s.id AND r.id=s.revision_id JOIN agenteam_skill.object_attempts a ON a.attempt_id=$3 AND a.project_id=s.project_id AND a.creation_id=s.creation_id AND a.skill_id=s.id AND a.revision_id=s.revision_id AND a.object_id=r.object_id AND a.upload_id=$4 WHERE s.project_id=$1 AND s.id=$2`, r.request.ProjectID.String(), r.skill.String(), r.attempt.String(), r.upload.String()).Scan(&creation, &revision, &name, &normalized, &description, &protected, &current, &version, &serving, &revisionNumber, &object, &objectVersion, &objectCreated, &published, &attempt, &process)
	if err != nil {
		return unavailable(err)
	}
	_, processErr := f.ParseID[oc.Process](process)
	_, createdErr := f.NewInstant(objectCreated)
	_, publishedErr := f.NewInstant(published)
	if creation != r.request.CreationID.String() || revision != r.revision.String() || name != r.bundle.name || normalized != AddSkillsNormalizedName || description != r.bundle.description || !protected || serving == closed || current != 1 || revisionNumber != 1 || f.Version(version).Validate() != nil || f.Version(objectVersion).Validate() != nil || object != r.object.String() || attempt != r.attempt.String() || processErr != nil || createdErr != nil || publishedErr != nil || published.Before(objectCreated) {
		return unavailable(nil)
	}
	return nil
}

func cleanupWorkJoined(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID) (bool, error) {
	var joined bool
	if err := x.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM agenteam_skill.work WHERE project_id=$1 AND phase<>'joined')`, project.String()).Scan(&joined); err != nil {
		return false, unavailable(err)
	}
	return joined, nil
}

func cleanupAllEmpty(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID) (bool, error) {
	var empty bool
	err := x.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM agenteam_skill.initializations WHERE project_id=$1) AND NOT EXISTS(SELECT 1 FROM agenteam_skill.skills WHERE project_id=$1) AND NOT EXISTS(SELECT 1 FROM agenteam_skill.revisions WHERE project_id=$1) AND NOT EXISTS(SELECT 1 FROM agenteam_skill.cleanup WHERE project_id=$1) AND NOT EXISTS(SELECT 1 FROM agenteam_skill.work WHERE project_id=$1) AND NOT EXISTS(SELECT 1 FROM agenteam_skill.object_attempts WHERE project_id=$1)`, project.String()).Scan(&empty)
	return empty, portError(err)
}

func insertCleanupGate(ctx context.Context, x postgres.SQLExecutor, r initializationRow, c cleanupRow) error {
	if !c.matches(r) || c.phase != cleanupGated || c.version != 1 || c.id.Validate() != nil || c.cause.Validate() != nil || c.cause.Action != pc.Delete {
		return invalid()
	}
	if err := cleanupCore(ctx, x, r, false); err != nil {
		return err
	}
	// The caller holds the original command/Project/Skill/Object lock union.
	// Any existing gate was re-read under it; conflict is not an upsert/replay.
	_, err := x.Exec(ctx, `INSERT INTO agenteam_skill.cleanup(id,project_id,lifecycle_operation_id,project_version,action,skill_id,revision_id,object_id,upload_id,phase,version,created_at,updated_at) VALUES($1,$2,$3,$4,'delete',$5,$6,$7,$8,'gated',1,clock_timestamp(),clock_timestamp())`, c.id.String(), c.project.String(), c.cause.OperationID.String(), int64(c.cause.ProjectVersion), c.skill.String(), c.revision.String(), c.object.String(), c.upload.String())
	if err != nil {
		return unavailable(err)
	}
	tag, err := x.Exec(ctx, `UPDATE agenteam_skill.skills SET serving=false,version=version+1 WHERE project_id=$1 AND id=$2 AND revision_id=$3 AND serving`, c.project.String(), c.skill.String(), c.revision.String())
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return unavailable(nil)
	}
	return nil
}

func setCleanupPhase(ctx context.Context, x postgres.SQLExecutor, c cleanupRow, phase cleanupPhase) error {
	if phase != cleanupPending && phase != cleanupCompleted || c.phase == cleanupCompleted {
		return invalid()
	}
	tag, err := x.Exec(ctx, `UPDATE agenteam_skill.cleanup SET phase=$3,version=version+1,updated_at=clock_timestamp() WHERE id=$1 AND project_id=$2 AND phase=$4 AND version=$5`, c.id.String(), c.project.String(), string(phase), string(c.phase), int64(c.version))
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ResourceBusy)
	}
	return nil
}

// Each successful call removes only one bounded batch across the two local
// histories. There is no cursor to skip low IDs after a lost COMMIT response.
func compressCleanupHistory(ctx context.Context, x postgres.SQLExecutor, r initializationRow) (bool, error) {
	var work []string
	err := x.QueryRow(ctx, `SELECT COALESCE(array_agg(id::text ORDER BY id),ARRAY[]::text[]) FROM (SELECT id FROM agenteam_skill.work WHERE project_id=$1 AND phase='joined' ORDER BY id LIMIT 33) history`, r.request.ProjectID.String()).Scan(&work)
	if err != nil {
		return false, unavailable(err)
	}
	if len(work) > cleanupBatchLimit+1 {
		return false, unavailable(nil)
	}
	if len(work) > 0 {
		for _, text := range work[:min(len(work), cleanupBatchLimit)] {
			workID, err := f.ParseID[skillWork](text)
			if err != nil {
				return false, unavailable(err)
			}
			w, err := loadWork(ctx, x, workID)
			if err != nil {
				return false, err
			}
			if w == nil || w.project != r.request.ProjectID || w.skill != r.skill || w.phase != workJoined {
				return false, unavailable(nil)
			}
			tag, err := x.Exec(ctx, `DELETE FROM agenteam_skill.work WHERE id=$1 AND project_id=$2 AND skill_id=$3 AND phase='joined' AND process_id=$4 AND kind=$5 AND fence=$6`, workID.String(), r.request.ProjectID.String(), r.skill.String(), w.process.String(), string(w.kind), int64(w.fence))
			if err != nil {
				return false, unavailable(err)
			}
			if tag.RowsAffected() != 1 {
				return false, unavailable(nil)
			}
		}
		return true, nil
	}
	var attempts []string
	err = x.QueryRow(ctx, `SELECT COALESCE(array_agg(attempt_id::text ORDER BY attempt_id),ARRAY[]::text[]) FROM (SELECT attempt_id FROM agenteam_skill.object_attempts WHERE object_id=$1 AND attempt_id<>$2 ORDER BY attempt_id LIMIT 33) history`, r.object.String(), r.attempt.String()).Scan(&attempts)
	if err != nil {
		return false, unavailable(err)
	}
	if len(attempts) > cleanupBatchLimit+1 {
		return false, unavailable(nil)
	}
	for _, text := range attempts[:min(len(attempts), cleanupBatchLimit)] {
		if _, err = f.ParseID[oc.Attempt](text); err != nil {
			return false, unavailable(err)
		}
		tag, err := x.Exec(ctx, `DELETE FROM agenteam_skill.object_attempts WHERE attempt_id=$1 AND project_id=$2 AND creation_id=$3 AND skill_id=$4 AND revision_id=$5 AND object_id=$6 AND upload_id=$7 AND attempt_id<>$8`, text, r.request.ProjectID.String(), r.request.CreationID.String(), r.skill.String(), r.revision.String(), r.object.String(), r.upload.String(), r.attempt.String())
		if err != nil {
			return false, unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return false, unavailable(nil)
		}
	}
	return len(attempts) != 0, nil
}

func cleanupHistoryEmpty(ctx context.Context, x postgres.SQLExecutor, r initializationRow) (bool, error) {
	var empty bool
	err := x.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM agenteam_skill.work WHERE project_id=$1) AND NOT EXISTS(SELECT 1 FROM agenteam_skill.object_attempts WHERE object_id=$2 AND attempt_id<>$3)`, r.request.ProjectID.String(), r.object.String(), r.attempt.String()).Scan(&empty)
	return empty, portError(err)
}

// Must follow an actual successful final Object purge in this SAME Tx. All
// foreign keys remain deferred, including the initialization/current attempt
// cycle; a failure rolls back both domains' last anchors together.
func deleteCleanupCore(ctx context.Context, x postgres.SQLExecutor, r initializationRow, c cleanupRow) error {
	if c.phase != cleanupCompleted || !c.matches(r) {
		return invalid()
	}
	queries := []struct {
		sql  string
		args []any
	}{
		{`DELETE FROM agenteam_skill.cleanup WHERE id=$1 AND project_id=$2 AND phase='completed'`, []any{c.id.String(), c.project.String()}},
		{`DELETE FROM agenteam_skill.revisions WHERE id=$1 AND project_id=$2 AND skill_id=$3 AND object_id=$4`, []any{r.revision.String(), c.project.String(), r.skill.String(), r.object.String()}},
		{`DELETE FROM agenteam_skill.skills WHERE id=$1 AND project_id=$2 AND revision_id=$3 AND NOT serving`, []any{r.skill.String(), c.project.String(), r.revision.String()}},
		{`DELETE FROM agenteam_skill.object_attempts WHERE attempt_id=$1 AND project_id=$2 AND creation_id=$3 AND skill_id=$4 AND revision_id=$5 AND object_id=$6 AND upload_id=$7`, []any{r.attempt.String(), c.project.String(), r.request.CreationID.String(), r.skill.String(), r.revision.String(), r.object.String(), r.upload.String()}},
		{`DELETE FROM agenteam_skill.initializations WHERE project_id=$1 AND creation_id=$2 AND skill_id=$3 AND revision_id=$4 AND object_id=$5 AND upload_id=$6 AND current_attempt_id=$7 AND phase='published'`, []any{c.project.String(), r.request.CreationID.String(), r.skill.String(), r.revision.String(), r.object.String(), r.upload.String(), r.attempt.String()}},
	}
	for _, q := range queries {
		tag, err := x.Exec(ctx, q.sql, q.args...)
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return unavailable(nil)
		}
	}
	empty, err := cleanupAllEmpty(ctx, x, c.project)
	if err != nil {
		return err
	}
	if !empty {
		return unavailable(nil)
	}
	return nil
}

func cleanupProjectForObject(ctx context.Context, x postgres.SQLExecutor, object oc.ObjectID) (id.ProjectID, error) {
	var project string
	err := x.QueryRow(ctx, `SELECT project_id::text FROM agenteam_skill.initializations WHERE object_id=$1`, object.String()).Scan(&project)
	if errors.Is(err, pgx.ErrNoRows) {
		return id.ProjectID{}, fault(f.DependencyUnbound)
	}
	if err != nil {
		return id.ProjectID{}, unavailable(err)
	}
	p, err := f.ParseID[id.Project](project)
	if err != nil {
		return id.ProjectID{}, unavailable(err)
	}
	return p, nil
}
