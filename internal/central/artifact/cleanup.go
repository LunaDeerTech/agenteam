package artifact

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// CancelUpload cancels this domain's prospective identity. A consumed Artifact
// is never deleted by this endpoint. The exact cancellation remains durable
// until Object cleanup and the original source (if any) actually converge.
func (s *Service) CancelUpload(ctx context.Context, v ac.Invocation, target ac.UploadTarget) (oc.CleanupState, error) {
	target.Command = copyCommand(target.Command)
	if v.Validate() != nil || v.Details().Actor.Details().Kind != identity.Human || target.Reference.Validate() != nil || !target.Owner.Equal(artifactOwner(target.Reference)) || v.Details().ProjectID != target.Reference.ProjectID || target.Command.Validate() != nil {
		return oc.CleanupPending, invalid()
	}
	id, err := commandIdentity(target.Reference.ProjectID, target.Command.IdempotencyKey, "prepare_upload")
	if err != nil {
		return oc.CleanupPending, err
	}
	cause, _ := foundation.NewCommandsCause(id)
	result := s.within(ctx, invocationSubject(v), identity.Converge, cause, nil, createLocks(id, target.Reference.ArtifactID.String()), func(ctx context.Context, tx foundation.Tx, _ oc.LockedAccess) error {
		e, err := s.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		u, ok, err := loadIntent(ctx, e, id)
		if err != nil {
			return err
		}
		if !ok || u.ref != target.Reference || u.originalActor != stableActor(v.Details().Actor) {
			return failure(foundation.Forbidden, nil)
		}
		if u.state == "consumed" || u.state == "deleted" {
			return failure(foundation.InvalidState, nil)
		}
		_, err = e.Exec(ctx, `UPDATE agenteam_artifact.upload_intents SET state='cancel_requested' WHERE id=$1`, u.ref.ArtifactID.String())
		return portOrNil(err)
	})
	if err = commitError(result); err != nil {
		return oc.CleanupPending, err
	}
	return s.finishCancellation(ctx, v, id, target.Reference, false)
}

// CancelCreation is an internal incomplete-command convergence port, not a
// delete-artifact Core Tool. Current authority precedes all history checks.
func (s *Service) CancelCreation(ctx context.Context, v ac.Invocation, key foundation.IdempotencyKey) (oc.CleanupState, error) {
	if v.Validate() != nil || key.Validate() != nil {
		return oc.CleanupPending, invalid()
	}
	id, err := commandIdentity(v.Details().ProjectID, key, "create")
	if err != nil {
		return oc.CleanupPending, err
	}
	before, found, err := loadCommand(ctx, s.state().store, id)
	if err != nil {
		return oc.CleanupPending, err
	}
	var entity string
	if found {
		entity = before.ref.ArtifactID.String()
	}
	cause, _ := foundation.NewCommandsCause(id)
	result := s.within(ctx, invocationSubject(v), identity.Converge, cause, nil, createLocks(id, entity), func(ctx context.Context, tx foundation.Tx, _ oc.LockedAccess) error {
		e, err := s.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		c, ok, err := loadCommand(ctx, e, id)
		if err != nil {
			return err
		}
		if !ok || !found || c.ref != before.ref || c.originalActor != stableActor(v.Details().Actor) {
			return failure(foundation.Forbidden, nil)
		}
		if c.state == "completed" || c.state == "deleted" {
			return failure(foundation.InvalidState, nil)
		}
		_, err = e.Exec(ctx, `UPDATE agenteam_artifact.commands SET state='cancel_requested' WHERE command_hash=$1`, digestRaw(digestBytes([]byte(id.Canonical()))))
		return portOrNil(err)
	})
	if err = commitError(result); err != nil {
		return oc.CleanupPending, err
	}
	return s.finishCancellation(ctx, v, id, before.ref, true)
}
func (s *Service) finishCancellation(ctx context.Context, v ac.Invocation, id foundation.CommandIdentity, ref ac.ArtifactRef, creation bool) (oc.CleanupState, error) {
	r := s.state()
	var object oc.ObjectID
	var key foundation.IdempotencyKey
	if creation {
		c, ok, err := loadCommand(ctx, r.store, id)
		if err != nil {
			return oc.CleanupPending, err
		}
		if !ok || c.ref != ref || c.state != "cancel_requested" && c.state != "cancelled" {
			return oc.CleanupPending, failure(foundation.InvalidState, nil)
		}
		if err = s.retireOldSource(ctx, c); err != nil {
			return oc.CleanupPending, err
		}
		object = c.object
		key = c.identity.Key()
	} else {
		u, ok, err := loadIntent(ctx, r.store, id)
		if err != nil {
			return oc.CleanupPending, err
		}
		if !ok || u.ref != ref || u.state != "cancel_requested" && u.state != "cancelled" {
			return oc.CleanupPending, failure(foundation.InvalidState, nil)
		}
		object = u.object
		key = id.Key()
	}
	if object.Validate() == nil {
		out, err := r.objects.CancelUpload(ctx, v.Details().Actor, artifactOwner(ref), key)
		if err != nil {
			return oc.CleanupPending, err
		}
		if out.Cleanup != oc.CleanupCompleted {
			return oc.CleanupPending, nil
		}
	}
	cause, _ := foundation.NewCommandsCause(id)
	result := s.within(ctx, invocationSubject(v), identity.Converge, cause, nil, createLocks(id, ref.ArtifactID.String()), func(ctx context.Context, tx foundation.Tx, _ oc.LockedAccess) error {
		e, err := r.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if creation {
			_, err = e.Exec(ctx, `UPDATE agenteam_artifact.commands SET state='cancelled',source_lease_id=NULL WHERE command_hash=$1 AND state IN('cancel_requested','cancelled')`, digestRaw(digestBytes([]byte(id.Canonical()))))
		} else {
			_, err = e.Exec(ctx, `UPDATE agenteam_artifact.upload_intents SET state='cancelled' WHERE id=$1 AND state IN('cancel_requested','cancelled')`, ref.ArtifactID.String())
		}
		return portOrNil(err)
	})
	if err := commitError(result); err != nil {
		return oc.CleanupPending, err
	}
	return oc.CleanupCompleted, nil
}
func projectExclusive(id identity.ProjectID) foundation.LockRequest {
	key, _ := foundation.ProjectLock(id.String())
	return foundation.LockRequest{Key: key, Mode: foundation.Exclusive}
}
func (s *Service) withinCleanup(ctx context.Context, actor identity.Actor, cause oc.ProjectCleanupCause, plans []oc.AccessLockPlan, extra []foundation.LockRequest, fn func(context.Context, foundation.Tx, postgres.SQLExecutor, oc.LockedAccess) error) foundation.CommitResult {
	d := cause.Details()
	extra = append(extra, projectExclusive(d.ProjectID))
	txCause, err := foundation.NewRecoveryCause("artifact-cleanup", d.OperationID.String(), d.ProjectID.String())
	if err != nil {
		return rejected(err)
	}
	return s.within(ctx, subject(actor, d.ProjectID), identity.Lifecycle, txCause, plans, extra, func(ctx context.Context, tx foundation.Tx, locked oc.LockedAccess) error {
		if err := s.state().owners.CheckProjectCleanupInTx(ctx, tx, actor, cause); err != nil {
			return err
		}
		e, err := s.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		return fn(ctx, tx, e, locked)
	})
}

// CleanupProject retains the stable operation and domain owner mapping until
// canonical rows, prospective attempts and source uses have all converged.
// Each call processes at most 100 business rows and 100 source checkpoints,
// then delegates physical cleanup to Object's persistent fair batch scheduler.
func (s *Service) CleanupProject(ctx context.Context, actor identity.Actor, cause oc.ProjectCleanupCause) (oc.ProjectCleanupResult, error) {
	if actor.Validate() != nil || cause.Validate() != nil {
		return oc.ProjectCleanupResult{}, invalid()
	}
	d := cause.Details()
	out := oc.ProjectCleanupResult{State: oc.CleanupPending, ProjectID: d.ProjectID, OperationID: d.OperationID}
	r := s.state()
	result := s.withinCleanup(ctx, actor, cause, nil, nil, func(ctx context.Context, _ foundation.Tx, e postgres.SQLExecutor, _ oc.LockedAccess) error {
		var operation, state string
		var version int64
		err := e.QueryRow(ctx, `SELECT operation_id::text,project_version,state FROM agenteam_artifact.cleanup WHERE project_id=$1`, d.ProjectID.String()).Scan(&operation, &version, &state)
		if noRows(err) {
			_, err = e.Exec(ctx, `INSERT INTO agenteam_artifact.cleanup(project_id,operation_id,project_version,state) VALUES($1,$2,$3,'pending')`, d.ProjectID.String(), d.OperationID.String(), int64(d.Version))
			return portOrNil(err)
		}
		if err != nil {
			return unavailable(err)
		}
		if operation != d.OperationID.String() || version != int64(d.Version) {
			return failure(foundation.Forbidden, nil)
		}
		if state == "completed" {
			out.State = oc.CleanupCompleted
		}
		return nil
	})
	if err := commitError(result); err != nil {
		return out, err
	}
	if out.State == oc.CleanupCompleted {
		return out, nil
	}
	// Discovery only; the complete selected set is reread under the EX Project
	// gate and individual command/record/object plans in the following Tx.
	rows, err := r.store.Query(ctx, `SELECT `+artifactColumns+` FROM agenteam_artifact.artifacts WHERE project_id=$1 ORDER BY id LIMIT 100`, d.ProjectID.String())
	if err != nil {
		return out, unavailable(err)
	}
	var batch []artifactRow
	for rows.Next() {
		row, _, err := scanArtifact(rows)
		if err != nil {
			rows.Close()
			return out, err
		}
		batch = append(batch, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, unavailable(err)
	}
	registration, _ := identity.RegisterService(identity.ObjectMaintenance)
	maintenance, err := registration.Actor(d.OperationID.String(), identityScope(d.ProjectID))
	if err != nil {
		return out, invalid()
	}
	type closingRow struct{ entity, key, kind string }
	var closing []closingRow
	closingRows, err := r.store.Query(ctx, `SELECT artifact_id::text,command_key,'create' FROM agenteam_artifact.commands WHERE project_id=$1 AND state<>'deleted' AND NOT EXISTS(SELECT 1 FROM agenteam_artifact.artifacts a WHERE a.id=artifact_id) UNION ALL SELECT id::text,command_key,'prepare_upload' FROM agenteam_artifact.upload_intents WHERE project_id=$1 AND state<>'deleted' AND NOT EXISTS(SELECT 1 FROM agenteam_artifact.artifacts a WHERE a.id=upload_intents.id) ORDER BY 3,1 LIMIT 100`, d.ProjectID.String())
	if err != nil {
		return out, unavailable(err)
	}
	for closingRows.Next() {
		var item closingRow
		if err = closingRows.Scan(&item.entity, &item.key, &item.kind); err != nil {
			closingRows.Close()
			return out, unavailable(err)
		}
		closing = append(closing, item)
	}
	err = closingRows.Err()
	closingRows.Close()
	if err != nil {
		return out, unavailable(err)
	}
	var plans []oc.AccessLockPlan
	var locks []foundation.LockRequest
	for _, item := range closing {
		id, _ := commandIdentity(d.ProjectID, foundation.IdempotencyKey(item.key), item.kind)
		locks = append(locks, createLocks(id, item.entity)...)
	}
	for _, row := range batch {
		m := row.meta.Details()
		plan, err := s.ownerPlan(ctx, maintenance, artifactOwner(m.Reference), oc.ReleaseAccess, oc.AccessRequestDetails{ObjectID: m.Object.ID})
		if err != nil {
			return out, err
		}
		plans = append(plans, plan)
		locks = append(locks, artifactLock(m.Reference.ArtifactID.String()))
		var createKey string
		if err = r.store.QueryRow(ctx, `SELECT command_key FROM agenteam_artifact.commands WHERE artifact_id=$1`, m.Reference.ArtifactID.String()).Scan(&createKey); err != nil {
			return out, unavailable(err)
		}
		id, _ := commandIdentity(d.ProjectID, foundation.IdempotencyKey(createKey), "create")
		locks = append(locks, createLocks(id, "")...)
		var prepareKey string
		err = r.store.QueryRow(ctx, `SELECT command_key FROM agenteam_artifact.upload_intents WHERE id=$1`, m.Reference.ArtifactID.String()).Scan(&prepareKey)
		if err != nil && !noRows(err) {
			return out, unavailable(err)
		}
		if err == nil {
			prepareID, _ := commandIdentity(d.ProjectID, foundation.IdempotencyKey(prepareKey), "prepare_upload")
			locks = append(locks, createLocks(prepareID, "")...)
		}
	}
	result = s.withinCleanup(ctx, maintenance, cause, plans, locks, func(ctx context.Context, tx foundation.Tx, e postgres.SQLExecutor, locked oc.LockedAccess) error {
		for i, row := range batch {
			m := row.meta.Details()
			current, ok, err := loadArtifact(ctx, e, m.Reference.ArtifactID.String())
			if err != nil {
				return err
			}
			if !ok || current.meta.Details().Reference != m.Reference || current.meta.Details().Object.ID != m.Object.ID {
				return failure(foundation.ResourceBusy, nil)
			}
			if err = r.objects.ReleaseObjectInTx(ctx, tx, maintenance, artifactOwner(m.Reference), m.Object.ID, plans[i], locked); err != nil {
				return err
			}
			if _, err = e.Exec(ctx, `DELETE FROM agenteam_artifact.artifacts WHERE id=$1`, m.Reference.ArtifactID.String()); err != nil {
				return unavailable(err)
			}
			if _, err = e.Exec(ctx, `UPDATE agenteam_artifact.commands SET state='deleted' WHERE artifact_id=$1`, m.Reference.ArtifactID.String()); err != nil {
				return unavailable(err)
			}
			if _, err = e.Exec(ctx, `UPDATE agenteam_artifact.upload_intents SET state='deleted' WHERE id=$1`, m.Reference.ArtifactID.String()); err != nil {
				return unavailable(err)
			}
		}
		_, err := e.Exec(ctx, `UPDATE agenteam_download.grants SET revoked=true WHERE project_id=$1`, d.ProjectID.String())
		if err != nil {
			return unavailable(err)
		}
		// The selected incomplete identities have explicit command/record locks;
		// no unbounded mutation or incremental lock acquisition is hidden here.
		for _, item := range closing {
			query := `UPDATE agenteam_artifact.commands SET state='deleted' WHERE artifact_id=$1 AND project_id=$2 AND NOT EXISTS(SELECT 1 FROM agenteam_artifact.artifacts a WHERE a.id=artifact_id)`
			if item.kind == "prepare_upload" {
				query = `UPDATE agenteam_artifact.upload_intents SET state='deleted' WHERE id=$1 AND project_id=$2 AND NOT EXISTS(SELECT 1 FROM agenteam_artifact.artifacts a WHERE a.id=upload_intents.id)`
			}
			if _, err = e.Exec(ctx, query, item.entity, d.ProjectID.String()); err != nil {
				return unavailable(err)
			}
		}
		return nil
	})
	if err = commitError(result); err != nil {
		return out, err
	}
	var remaining int64
	if err = r.store.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_artifact.artifacts WHERE project_id=$1)+(SELECT count(*) FROM agenteam_artifact.commands WHERE project_id=$1 AND state<>'deleted')+(SELECT count(*) FROM agenteam_artifact.upload_intents WHERE project_id=$1 AND state<>'deleted')`, d.ProjectID.String()).Scan(&remaining); err != nil {
		return out, unavailable(err)
	}
	if remaining > 0 {
		out.Remaining = foundation.Progress(remaining)
		return out, nil
	}
	sourceErr := s.cleanupSources(ctx, maintenance, cause)
	physical, err := r.cleaner.CleanupProject(ctx, maintenance, cause)
	out.Remaining = physical.Remaining
	if err != nil {
		return out, err
	}
	if sourceErr != nil {
		return out, sourceErr
	}
	if physical.State != oc.CleanupCompleted {
		return out, nil
	}
	downloads, bound := r.cleaner.(oc.DownloadCleanup)
	if !bound || nilPort(downloads) {
		return out, failure(foundation.DependencyUnbound, nil)
	}
	request, err := oc.NewProjectCleanupAccess(oc.AccessRequestDetails{Operation: oc.FinishProjectAccess, Actor: maintenance, ProjectCleanup: cause})
	if err != nil {
		return out, err
	}
	finalPlan, err := r.objects.DiscoverAccess(ctx, request)
	if err != nil {
		return out, err
	}
	result = s.withinCleanup(ctx, maintenance, cause, []oc.AccessLockPlan{finalPlan}, nil, func(ctx context.Context, tx foundation.Tx, e postgres.SQLExecutor, locked oc.LockedAccess) error {
		var pending int64
		if err := e.QueryRow(ctx, `SELECT count(*) FROM agenteam_artifact.commands WHERE project_id=$1 AND source_lease_id IS NOT NULL`, d.ProjectID.String()).Scan(&pending); err != nil {
			return unavailable(err)
		}
		if pending > 0 {
			out.Remaining = foundation.Progress(pending)
			return nil
		}
		if err := downloads.PurgeProjectDownloadsInTx(ctx, tx, maintenance, cause, finalPlan, locked); err != nil {
			return err
		}
		// Recovery facts survived until every source use and physical object was
		// confirmed gone. Final deletion keeps only the minimal Project cleanup
		// gate receipt, not the original command/display/content/source payload.
		for _, query := range []string{
			`DELETE FROM agenteam_artifact.commands WHERE project_id=$1`,
			`DELETE FROM agenteam_artifact.upload_intents WHERE project_id=$1`,
		} {
			if _, err := e.Exec(ctx, query, d.ProjectID.String()); err != nil {
				return unavailable(err)
			}
		}
		_, err := e.Exec(ctx, `UPDATE agenteam_artifact.cleanup SET state='completed',updated_at=clock_timestamp() WHERE project_id=$1`, d.ProjectID.String())
		if err != nil {
			return unavailable(err)
		}
		out.State = oc.CleanupCompleted
		return nil
	})
	if err = commitError(result); err != nil {
		out.State = oc.CleanupPending
		return out, err
	}
	return out, nil
}
func identityScope(project identity.ProjectID) identity.Scope {
	scope, _ := identity.InProject(project)
	return scope
}
func (s *Service) cleanupSources(ctx context.Context, actor identity.Actor, cause oc.ProjectCleanupCause) error {
	r := s.state()
	d := cause.Details()
	rows, err := r.store.Query(ctx, `SELECT command_key FROM agenteam_artifact.commands WHERE project_id=$1 AND source_lease_id IS NOT NULL ORDER BY cleanup_pass,artifact_id LIMIT 100`, d.ProjectID.String())
	if err != nil {
		return unavailable(err)
	}
	var keys []string
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			rows.Close()
			return unavailable(err)
		}
		keys = append(keys, key)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return unavailable(err)
	}
	var first error
	for _, key := range keys {
		if ctx.Err() != nil {
			return unavailable(ctx.Err())
		}
		id, _ := commandIdentity(d.ProjectID, foundation.IdempotencyKey(key), "create")
		c, ok, err := loadCommand(ctx, r.store, id)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if !ok {
			continue
		}
		result := s.withinCleanup(ctx, actor, cause, nil, createLocks(id, c.ref.ArtifactID.String()), func(ctx context.Context, _ foundation.Tx, e postgres.SQLExecutor, _ oc.LockedAccess) error {
			_, err := e.Exec(ctx, `UPDATE agenteam_artifact.commands SET cleanup_pass=cleanup_pass+1 WHERE command_hash=$1`, digestRaw(digestBytes([]byte(id.Canonical()))))
			return portOrNil(err)
		})
		if err = commitError(result); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if err = s.retireOldSource(ctx, c); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		result = s.withinCleanup(ctx, actor, cause, nil, createLocks(id, c.ref.ArtifactID.String()), func(ctx context.Context, _ foundation.Tx, e postgres.SQLExecutor, _ oc.LockedAccess) error {
			_, err := e.Exec(ctx, `UPDATE agenteam_artifact.commands SET source_lease_id=NULL WHERE command_hash=$1 AND source_lease_id=$2 AND state='deleted'`, digestRaw(digestBytes([]byte(id.Canonical()))), c.sourceLease.String())
			return portOrNil(err)
		})
		if err = commitError(result); err != nil && first == nil {
			first = err
		}
	}
	return first
}
