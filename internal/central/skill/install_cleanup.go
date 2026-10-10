package skill

import (
	"context"
	"encoding/json"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

func installationCleanupMatches(c cleanupRow, r installationRow) bool {
	return c.project == r.project && c.skill == r.skill && c.revision == r.revision && c.object == r.object && c.upload == r.upload
}

func installationCleanupCause(c cleanupRow, r installationRow) (oc.ObjectCleanupCause, error) {
	if !installationCleanupMatches(c, r) || c.id.Validate() != nil || c.cause.Validate() != nil || c.cause.Action != pc.Delete {
		return oc.ObjectCleanupCause{}, unavailable(nil)
	}
	owner, err := r.owner()
	if err != nil {
		return oc.ObjectCleanupCause{}, err
	}
	return oc.NewObjectCleanupCause(oc.CleanupDetails{OperationID: c.id, Owner: owner, Reason: oc.ProjectDeleted})
}

func loadInstallationCleanup(ctx context.Context, x postgres.SQLExecutor, r installationRow) (*cleanupRow, error) {
	var ids []string
	err := x.QueryRow(ctx, `SELECT COALESCE(array_agg(id::text ORDER BY id),ARRAY[]::text[]) FROM (SELECT id FROM agenteam_skill.cleanup WHERE project_id=$1 AND skill_id=$2 AND installation_id=$3 ORDER BY id LIMIT 2) gates`, r.project.String(), r.skill.String(), r.id.String()).Scan(&ids)
	if err != nil {
		return nil, unavailable(err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) != 1 {
		return nil, unavailable(nil)
	}
	c, err := loadCleanupID(ctx, x, r.project, ids[0])
	if err != nil {
		return nil, err
	}
	if !installationCleanupMatches(*c, r) {
		return nil, unavailable(nil)
	}
	return c, nil
}

// Published sources retain their real canonical origin; unfinished commands
// must have no visible Skill/revision. A phase is never a fake publication.
func installationCleanupCore(ctx context.Context, x postgres.SQLExecutor, r installationRow, closed bool) error {
	if err := r.validate(); err != nil {
		return err
	}
	if r.phase == installationPublished {
		_, _, err := loadInstalledCore(ctx, x, r, closed)
		return err
	}
	var empty bool
	err := x.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM agenteam_skill.skills WHERE project_id=$1 AND id=$2) AND NOT EXISTS(SELECT 1 FROM agenteam_skill.revisions WHERE project_id=$1 AND skill_id=$2)`, r.project.String(), r.skill.String()).Scan(&empty)
	if err != nil {
		return unavailable(err)
	}
	if !empty {
		return unavailable(nil)
	}
	return nil
}

func insertInstallationCleanup(ctx context.Context, x postgres.SQLExecutor, r installationRow, c cleanupRow) error {
	if !installationCleanupMatches(c, r) || c.phase != cleanupGated || c.version != 1 {
		return invalid()
	}
	if err := installationCleanupCore(ctx, x, r, false); err != nil {
		return err
	}
	_, err := x.Exec(ctx, `INSERT INTO agenteam_skill.cleanup(id,project_id,lifecycle_operation_id,project_version,action,skill_id,revision_id,object_id,upload_id,phase,version,created_at,updated_at,installation_id) VALUES($1,$2,$3,$4,'delete',$5,$6,$7,$8,'gated',1,clock_timestamp(),clock_timestamp(),$9)`, c.id.String(), c.project.String(), c.cause.OperationID.String(), int64(c.cause.ProjectVersion), r.skill.String(), r.revision.String(), r.object.String(), r.upload.String(), r.id.String())
	if err != nil {
		return unavailable(err)
	}
	if r.phase == installationPublished {
		tag, err := x.Exec(ctx, `UPDATE agenteam_skill.skills SET serving=false,version=version+1 WHERE project_id=$1 AND id=$2 AND revision_id=$3 AND installation_id=$4 AND NOT protected AND serving`, r.project.String(), r.skill.String(), r.revision.String(), r.id.String())
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return unavailable(nil)
		}
	}
	return nil
}

// At most one batch of 32, shared between work and attempt history. Retain the
// current attempt and every mapping until the original Object tail completes.
func compressInstallationHistory(ctx context.Context, x postgres.SQLExecutor, r installationRow, physicalComplete bool) (bool, error) {
	var work []string
	err := x.QueryRow(ctx, `SELECT COALESCE(array_agg(id::text ORDER BY id),ARRAY[]::text[]) FROM (SELECT id FROM agenteam_skill.work WHERE project_id=$1 AND skill_id=$2 AND phase='joined' AND kind IN ('installation','installed_package_reader') ORDER BY id LIMIT 33) history`, r.project.String(), r.skill.String()).Scan(&work)
	if err != nil {
		return false, unavailable(err)
	}
	if len(work) > cleanupBatchLimit+1 {
		return false, unavailable(nil)
	}
	for _, raw := range work[:min(len(work), cleanupBatchLimit)] {
		key, err := f.ParseID[skillWork](raw)
		if err != nil {
			return false, unavailable(err)
		}
		w, err := loadWork(ctx, x, key)
		if err != nil {
			return false, err
		}
		if w == nil || w.project != r.project || w.skill != r.skill || !installedWork(w.kind) || w.phase != workJoined {
			return false, unavailable(nil)
		}
		tag, err := x.Exec(ctx, `DELETE FROM agenteam_skill.work WHERE id=$1 AND project_id=$2 AND skill_id=$3 AND phase='joined' AND process_id=$4 AND kind=$5 AND fence=$6`, raw, r.project.String(), r.skill.String(), w.process.String(), string(w.kind), int64(w.fence))
		if err != nil {
			return false, unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return false, unavailable(nil)
		}
	}
	if len(work) > 0 {
		return true, nil
	}
	if !physicalComplete {
		return false, nil
	}
	var attempts []string
	err = x.QueryRow(ctx, `SELECT COALESCE(array_agg(attempt_id::text ORDER BY attempt_id),ARRAY[]::text[]) FROM (SELECT attempt_id FROM agenteam_skill.installation_attempts WHERE object_id=$1 AND attempt_id<>$2 ORDER BY attempt_id LIMIT 33) history`, r.object.String(), r.attempt.String()).Scan(&attempts)
	if err != nil {
		return false, unavailable(err)
	}
	if len(attempts) > cleanupBatchLimit+1 {
		return false, unavailable(nil)
	}
	for _, raw := range attempts[:min(len(attempts), cleanupBatchLimit)] {
		if _, err = f.ParseID[oc.Attempt](raw); err != nil {
			return false, unavailable(err)
		}
		tag, err := x.Exec(ctx, `DELETE FROM agenteam_skill.installation_attempts WHERE attempt_id=$1 AND project_id=$2 AND installation_id=$3 AND skill_id=$4 AND revision_id=$5 AND object_id=$6 AND upload_id=$7 AND attempt_id<>$8`, raw, r.project.String(), r.id.String(), r.skill.String(), r.revision.String(), r.object.String(), r.upload.String(), r.attempt.String())
		if err != nil {
			return false, unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return false, unavailable(nil)
		}
	}
	return len(attempts) > 0, nil
}

func installationHistoryEmpty(ctx context.Context, x postgres.SQLExecutor, r installationRow) (bool, error) {
	var empty bool
	err := x.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM agenteam_skill.work WHERE project_id=$1 AND skill_id=$2) AND NOT EXISTS(SELECT 1 FROM agenteam_skill.installation_attempts WHERE project_id=$1 AND installation_id=$3 AND ($4='' OR attempt_id::text<>$4))`, r.project.String(), r.skill.String(), r.id.String(), r.attempt.String()).Scan(&empty)
	return empty, portError(err)
}

// Object-bound callers must have just purged the last Object anchors in this
// same Tx. No helper here manufactures that capability or commits separately.
func deleteInstallationCore(ctx context.Context, x postgres.SQLExecutor, r installationRow, c *cleanupRow) error {
	queries := []struct {
		sql  string
		args []any
	}{}
	if r.object.Validate() == nil {
		if c == nil || c.phase != cleanupCompleted || !installationCleanupMatches(*c, r) {
			return invalid()
		}
		queries = append(queries, struct {
			sql  string
			args []any
		}{`DELETE FROM agenteam_skill.cleanup WHERE id=$1 AND project_id=$2 AND installation_id=$3 AND phase='completed'`, []any{c.id.String(), r.project.String(), r.id.String()}})
		if r.phase == installationPublished {
			queries = append(queries,
				struct {
					sql  string
					args []any
				}{`DELETE FROM agenteam_skill.revisions WHERE id=$1 AND project_id=$2 AND skill_id=$3 AND object_id=$4 AND installation_id=$5`, []any{r.revision.String(), r.project.String(), r.skill.String(), r.object.String(), r.id.String()}},
				struct {
					sql  string
					args []any
				}{`DELETE FROM agenteam_skill.skills WHERE id=$1 AND project_id=$2 AND revision_id=$3 AND installation_id=$4 AND NOT serving AND NOT protected`, []any{r.skill.String(), r.project.String(), r.revision.String(), r.id.String()}})
		}
		queries = append(queries, struct {
			sql  string
			args []any
		}{`DELETE FROM agenteam_skill.installation_attempts WHERE attempt_id=$1 AND project_id=$2 AND installation_id=$3 AND skill_id=$4 AND revision_id=$5 AND object_id=$6 AND upload_id=$7`, []any{r.attempt.String(), r.project.String(), r.id.String(), r.skill.String(), r.revision.String(), r.object.String(), r.upload.String()}})
	} else if c != nil || r.phase != installationPlanned && r.phase != installationFailed {
		return invalid()
	}
	queries = append(queries, struct {
		sql  string
		args []any
	}{`DELETE FROM agenteam_skill.installations WHERE id=$1 AND project_id=$2 AND skill_id=$3 AND revision_id=$4 AND phase=$5 AND version=$6`, []any{r.id.String(), r.project.String(), r.skill.String(), r.revision.String(), string(r.phase), int64(r.version)}})
	for _, q := range queries {
		tag, err := x.Exec(ctx, q.sql, q.args...)
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return unavailable(nil)
		}
	}
	return nil
}

func sameInstallationCleanup(a, b installationRow) bool {
	return a.id == b.id && a.project == b.project && a.skill == b.skill && a.revision == b.revision && a.semantic == b.semantic && a.object == b.object && a.upload == b.upload && a.attempt == b.attempt && a.phase == b.phase && a.version == b.version
}

func (s *Service) installationCleanupTransaction(ctx context.Context, actor id.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, discovered installationRow, plans []oc.AccessLockPlan, fn func(context.Context, f.Tx, postgres.SQLExecutor, installationRow, oc.LockedAccess) error) error {
	if err := stopArguments(actor, cause, scope); err != nil {
		return err
	}
	if cause.Action != pc.Delete || discovered.project != scope.ProjectID {
		return invalid()
	}
	locks, err := discovered.locks(f.Exclusive)
	if err != nil {
		return err
	}
	txCause, err := f.NewRecoveryCause("skill-install-cleanup", cause.OperationID.String(), discovered.id.String())
	if err != nil {
		return err
	}
	state := s.state()
	store := state.authority.state().store
	var callbackErr error
	result := store.WithinTx(ctx, txCause, func(ctx context.Context, tx f.Tx) (err error) {
		defer func() { callbackErr = err }()
		var locked oc.LockedAccess
		if len(plans) == 0 {
			err = store.AcquireAll(ctx, tx, locks)
		} else {
			locked, err = state.objects.AcquireAccessPlansInTx(ctx, tx, plans, locks)
		}
		if err != nil {
			return portError(err)
		}
		if err = store.RequireHeldLocks(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if err = state.authority.state().projects.ValidateLifecycleInTx(ctx, tx, actor, cause, pc.SkillsParticipant, pc.CleanupPhase); err != nil {
			return portError(err)
		}
		r, err := loadInstallationSkill(ctx, x, scope.ProjectID, discovered.skill)
		if err != nil {
			return err
		}
		if r == nil || !sameInstallationCleanup(*r, discovered) {
			return fault(f.ResourceBusy)
		}
		joined, err := cleanupWorkJoined(ctx, x, scope.ProjectID)
		if err != nil {
			return err
		}
		if !joined || !s.cleanupLocalJoined(scope.ProjectID) {
			return fault(f.ResourceBusy)
		}
		return fn(ctx, tx, x, *r, locked)
	})
	if result.State() == f.NotCommitted && callbackErr != nil {
		return callbackErr
	}
	return commitError(result)
}

// Discovery never authorizes. The original locked Tx rechecks the exact
// parent, lifecycle and all joined work; nil only selects the old builtin path.
func (s *Service) cleanupInstallations(ctx context.Context, actor id.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, purger oc.DeletedObjectMetadataPurger) (bool, error) {
	store := s.state().authority.state().store
	var raw string
	err := store.QueryRow(ctx, `SELECT skill_id::text FROM agenteam_skill.installations WHERE project_id=$1 ORDER BY skill_id LIMIT 1`, scope.ProjectID.String()).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, unavailable(err)
	}
	key, err := f.ParseID[pc.Skill](raw)
	if err != nil {
		return false, unavailable(err)
	}
	r, err := loadInstallationSkill(ctx, store, scope.ProjectID, key)
	if err != nil {
		return false, err
	}
	if r == nil {
		return false, fault(f.ResourceBusy)
	}
	var c *cleanupRow
	removed := false
	err = s.installationCleanupTransaction(ctx, actor, cause, scope, *r, nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor, row installationRow, _ oc.LockedAccess) error {
		var err error
		c, err = loadInstallationCleanup(ctx, x, row)
		if err != nil {
			return err
		}
		if c != nil && c.cause != cause {
			return fault(f.Forbidden)
		}
		if err = installationCleanupCore(ctx, x, row, c != nil); err != nil {
			return err
		}
		if row.object.Validate() != nil {
			if c != nil {
				return unavailable(nil)
			}
			removed, err = compressInstallationHistory(ctx, x, row, false)
			if err != nil || removed {
				return err
			}
			empty, err := installationHistoryEmpty(ctx, x, row)
			if err != nil {
				return err
			}
			if !empty {
				return fault(f.ResourceBusy)
			}
			return deleteInstallationCore(ctx, x, row, nil)
		}
		return nil
	})
	if err != nil {
		return true, err
	}
	if r.object.Validate() != nil {
		return true, nil
	}
	if c == nil {
		key, err := f.NewID[oc.CleanupOperation]()
		if err != nil {
			return true, unavailable(err)
		}
		next := cleanupRow{id: key, project: r.project, cause: cause, skill: r.skill, revision: r.revision, object: r.object, upload: r.upload, phase: cleanupGated, version: 1}
		objectCause, err := installationCleanupCause(next, *r)
		if err != nil {
			return true, err
		}
		request, err := oc.NewCleanupReleaseAccess(oc.AccessRequestDetails{Operation: oc.ReleaseForCleanupAccess, Cleanup: objectCause, ObjectID: r.object, UploadID: r.upload})
		if err != nil {
			return true, err
		}
		plan, err := s.cleanupPlan(ctx, request)
		if err != nil {
			return true, err
		}
		err = s.installationCleanupTransaction(ctx, actor, cause, scope, *r, []oc.AccessLockPlan{plan}, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, row installationRow, locked oc.LockedAccess) error {
			current, err := loadInstallationCleanup(ctx, x, row)
			if err != nil {
				return err
			}
			if current != nil {
				return fault(f.ResourceBusy)
			}
			if err = insertInstallationCleanup(ctx, x, row, next); err != nil {
				return err
			}
			return portError(s.state().objects.ReleaseForCleanupInTx(ctx, tx, objectCause, row.object, plan, locked))
		})
		if err != nil {
			return true, err
		}
		c = &next
	}
	if c.phase != cleanupCompleted {
		objectCause, err := installationCleanupCause(*c, *r)
		if err != nil {
			return true, err
		}
		result, err := s.state().objects.DeleteUnreferencedWithinBudget(ctx, objectCause, r.object)
		if err != nil {
			return true, portError(err)
		}
		if result.OperationID != c.id || result.State != oc.CleanupPending && result.State != oc.CleanupCompleted {
			return true, unavailable(nil)
		}
		next := cleanupPending
		if result.State == oc.CleanupCompleted {
			if len(result.Remaining.References) != 0 || len(result.Remaining.ActiveLeases) != 0 {
				return true, unavailable(nil)
			}
			next = cleanupCompleted
		}
		err = s.installationCleanupTransaction(ctx, actor, cause, scope, *r, nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor, row installationRow, _ oc.LockedAccess) error {
			current, err := requireInstallationCleanup(ctx, x, row, *c)
			if err != nil {
				return err
			}
			if current.phase == cleanupCompleted || current.phase == next {
				return nil
			}
			return setCleanupPhase(ctx, x, *current, next)
		})
		return true, err
	}
	err = s.installationCleanupTransaction(ctx, actor, cause, scope, *r, nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor, row installationRow, _ oc.LockedAccess) error {
		if _, err := requireInstallationCleanup(ctx, x, row, *c); err != nil {
			return err
		}
		var err error
		removed, err = compressInstallationHistory(ctx, x, row, true)
		return err
	})
	if err != nil || removed {
		return true, err
	}
	objectCause, err := installationCleanupCause(*c, *r)
	if err != nil {
		return true, err
	}
	request, err := oc.NewObjectCleanupAccess(oc.AccessRequestDetails{Operation: oc.PurgeDeletedObjectMetadataAccess, Cleanup: objectCause, ObjectID: r.object})
	if err != nil {
		return true, err
	}
	plan, err := s.cleanupPlan(ctx, request)
	if err != nil {
		return true, err
	}
	err = s.installationCleanupTransaction(ctx, actor, cause, scope, *r, []oc.AccessLockPlan{plan}, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, row installationRow, locked oc.LockedAccess) error {
		current, err := requireInstallationCleanup(ctx, x, row, *c)
		if err != nil {
			return err
		}
		if current.phase != cleanupCompleted {
			return fault(f.InvalidState)
		}
		empty, err := installationHistoryEmpty(ctx, x, row)
		if err != nil {
			return err
		}
		if !empty {
			return fault(f.ResourceBusy)
		}
		result, err := purger.PurgeDeletedObjectMetadataInTx(ctx, tx, objectCause, row.object, plan, locked)
		if err != nil {
			return portError(err)
		}
		if !result.MatchesOperation(c.id, row.object) {
			return unavailable(nil)
		}
		if result.State == oc.CleanupPending {
			return nil
		}
		return deleteInstallationCore(ctx, x, row, current)
	})
	return true, err
}

func requireInstallationCleanup(ctx context.Context, x postgres.SQLExecutor, r installationRow, expected cleanupRow) (*cleanupRow, error) {
	c, err := loadInstallationCleanup(ctx, x, r)
	if err != nil {
		return nil, err
	}
	if c == nil || c.id != expected.id || c.cause != expected.cause || !installationCleanupMatches(*c, r) {
		return nil, fault(f.ResourceBusy)
	}
	if err = installationCleanupCore(ctx, x, r, true); err != nil {
		return nil, err
	}
	return c, nil
}

func loadInstallationCleanupObject(ctx context.Context, x postgres.SQLExecutor, object oc.ObjectID) (*installationRow, *cleanupRow, error) {
	r, err := scanInstallation(x.QueryRow(ctx, `SELECT `+installationColumns+` FROM agenteam_skill.installations WHERE object_id=$1`, object.String()))
	if err != nil {
		return nil, nil, err
	}
	if r == nil {
		return nil, nil, fault(f.DependencyUnbound)
	}
	if r.object != object {
		return nil, nil, unavailable(nil)
	}
	c, err := loadInstallationCleanup(ctx, x, *r)
	return r, c, err
}

func (a *Authority) checkInstallationCleanupInTx(ctx context.Context, tx f.Tx, r installationRow, c cleanupRow) error {
	state := a.state()
	if state == nil {
		return fault(f.DependencyUnbound)
	}
	if !installationCleanupMatches(c, r) {
		return fault(f.Forbidden)
	}
	locks, err := r.locks(f.Exclusive)
	if err != nil {
		return err
	}
	if err = state.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	x, err := state.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	actor, err := cleanupActor(c.project, c.cause)
	if err != nil {
		return err
	}
	if err = state.projects.ValidateLifecycleInTx(ctx, tx, actor, c.cause, pc.SkillsParticipant, pc.CleanupPhase); err != nil {
		return portError(err)
	}
	if _, err = requireInstallationCleanup(ctx, x, r, c); err != nil {
		return err
	}
	joined, err := cleanupWorkJoined(ctx, x, c.project)
	if err != nil {
		return err
	}
	if !joined {
		return fault(f.ResourceBusy)
	}
	return nil
}

func installationCleanupDependencies(r installationRow, request oc.AccessRequest) (oc.AccessDependencies, error) {
	locks, err := r.locks(f.Exclusive)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	d := request.Details()
	c := d.Cleanup.Details()
	raw, err := json.Marshal(struct {
		Project, Installation, User, Key, Skill, Revision, Object, Upload, Attempt string
		Semantic                                                                   f.Digest
		Kind                                                                       oc.AccessKind
		Operation                                                                  oc.AccessOperation
		Cleanup, RequestUpload, Instance, RequestedAttempt, Worker, Checkpoint     string
		Fence                                                                      int64
		Reason                                                                     oc.CleanupReason
	}{r.project.String(), r.id.String(), r.user.String(), r.key.String(), r.skill.String(), r.revision.String(), r.object.String(), r.upload.String(), r.attempt.String(), r.semantic, d.Kind, d.Operation, c.OperationID.String(), d.UploadID.String(), d.InstanceID.String(), d.AttemptID.String(), d.WorkerID.String(), d.CleanupID.String(), int64(d.Fence), c.Reason})
	if err != nil {
		return oc.AccessDependencies{}, unavailable(err)
	}
	return oc.NewAccessDependencies(sum(raw), locks)
}

func loadInstallationCleanupRequest(ctx context.Context, x postgres.SQLExecutor, request oc.AccessRequest, proposed bool) (*installationRow, *cleanupRow, error) {
	d := request.Details()
	project, err := cleanupObjectProject(d.Cleanup, d.ObjectID)
	if err != nil {
		return nil, nil, err
	}
	r, c, err := loadInstallationCleanupObject(ctx, x, d.ObjectID)
	if err != nil {
		return nil, nil, err
	}
	owner, err := r.owner()
	if err != nil {
		return nil, nil, err
	}
	if r.project != project || !owner.Equal(d.Cleanup.Details().Owner) || d.Kind == oc.CleanupReleaseAccess && d.UploadID != r.upload {
		return nil, nil, fault(f.Forbidden)
	}
	if c != nil && c.id != d.Cleanup.Details().OperationID {
		return nil, nil, fault(f.Forbidden)
	}
	if err = cleanupAccessPhase(d, c, proposed); err != nil {
		return nil, nil, err
	}
	return r, c, nil
}

func (a *Authority) discoverInstallationCleanup(ctx context.Context, request oc.AccessRequest) (oc.AccessDependencies, error) {
	r, _, err := loadInstallationCleanupRequest(ctx, a.state().store, request, true)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return installationCleanupDependencies(*r, request)
}

func (a *Authority) validateInstallationCleanup(ctx context.Context, tx f.Tx, request oc.AccessRequest, expected oc.AccessDependencies) error {
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	r, c, err := loadInstallationCleanupRequest(ctx, x, request, false)
	if err != nil {
		return err
	}
	current, err := installationCleanupDependencies(*r, request)
	if err != nil {
		return err
	}
	if !current.Equal(expected) {
		return fault(f.ResourceBusy)
	}
	return a.checkInstallationCleanupInTx(ctx, tx, *r, *c)
}

type installationCleanupMaintenance struct {
	row     installationRow
	cleanup cleanupRow
	process oc.ProcessID
}

func loadInstallationCleanupMaintenance(ctx context.Context, x postgres.SQLExecutor, request oc.AccessRequest) (installationCleanupMaintenance, error) {
	var out installationCleanupMaintenance
	d := request.Details()
	if request.Validate() != nil || d.Kind != oc.MaintenanceAccess || !cleanupMaintenanceOperation(d.Operation) {
		return out, invalid()
	}
	r, c, err := loadInstallationCleanupObject(ctx, x, d.ObjectID)
	if err != nil {
		return out, err
	}
	if c == nil || c.phase != cleanupGated && c.phase != cleanupPending {
		return out, fault(f.Forbidden)
	}
	out.row, out.cleanup = *r, *c
	if d.Operation != oc.FinalizeCleanupAccess {
		var process string
		err = x.QueryRow(ctx, `SELECT process_id::text FROM agenteam_skill.installation_attempts WHERE attempt_id=$1 AND project_id=$2 AND installation_id=$3 AND skill_id=$4 AND revision_id=$5 AND object_id=$6 AND upload_id=$7`, d.AttemptID.String(), r.project.String(), r.id.String(), r.skill.String(), r.revision.String(), r.object.String(), r.upload.String()).Scan(&process)
		if err != nil {
			return out, unavailable(err)
		}
		out.process, err = f.ParseID[oc.Process](process)
		if err != nil {
			return out, unavailable(err)
		}
	}
	return out, nil
}

func (m installationCleanupMaintenance) dependencies(request oc.AccessRequest) (oc.AccessDependencies, error) {
	base, err := installationCleanupDependencies(m.row, request)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	raw, err := json.Marshal(struct {
		Parent                              f.Digest
		Cleanup, Operation, OriginalProcess string
		Version                             f.Version
	}{base.Mapping(), m.cleanup.id.String(), m.cleanup.cause.OperationID.String(), m.process.String(), m.cleanup.cause.ProjectVersion})
	if err != nil {
		return oc.AccessDependencies{}, unavailable(err)
	}
	return oc.NewAccessDependencies(sum(raw), base.Locks())
}

func (a *Authority) discoverInstallationCleanupMaintenance(ctx context.Context, request oc.AccessRequest) (oc.AccessDependencies, error) {
	m, err := loadInstallationCleanupMaintenance(ctx, a.state().store, request)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return m.dependencies(request)
}

func (a *Authority) validateInstallationCleanupMaintenance(ctx context.Context, tx f.Tx, request oc.AccessRequest, expected oc.AccessDependencies) error {
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	m, err := loadInstallationCleanupMaintenance(ctx, x, request)
	if err != nil {
		return err
	}
	current, err := m.dependencies(request)
	if err != nil {
		return err
	}
	if !current.Equal(expected) {
		return fault(f.ResourceBusy)
	}
	return a.checkInstallationCleanupInTx(ctx, tx, m.row, m.cleanup)
}
