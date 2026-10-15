package mcp

import (
	"context"
	"errors"
	"math"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// StartRecovery uses the same process admission and join barrier as protocol
// work. It never invents a process for historical materializations without one.
func (s *ContentStore) StartRecovery(ctx context.Context) error {
	if s == nil || s.authority == nil || s.authority.service() == nil || s.authority.service().owner == nil || nilPort(s.authority.recovery) || nilPort(s.recovery) {
		return fail(f.DependencyUnbound)
	}
	ctx, call, err := s.authority.service().owner.begin(ctx)
	if err != nil {
		return err
	}
	go func() { defer call.returnedWith(nil); _ = s.recoverContents(ctx) }()
	return nil
}

func (s *ContentStore) recoverContents(ctx context.Context) error {
	service := s.authority.service()
	after := ""
	for {
		rows, err := service.store.Query(ctx, `SELECT id FROM agenteam_mcp.materializations WHERE state='pending' AND process_id IS NOT NULL AND process_id<>$1 AND id>$2 ORDER BY id LIMIT 64`, service.owner.process.String(), after)
		if err != nil {
			return safeError(err)
		}
		var batch []string
		for rows.Next() {
			var value string
			if err = rows.Scan(&value); err != nil {
				break
			}
			if _, err = f.ParseID[f.Request](value); err != nil || len(batch) == 64 {
				err = fail(f.InvalidState)
				break
			}
			batch = append(batch, value)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return safeError(err)
		}
		if len(batch) == 0 {
			return ctx.Err()
		}
		for _, value := range batch {
			if err = ctx.Err(); err != nil {
				return err
			}
			_ = s.recoverContent(ctx, value)
			after = value
		}
	}
}

type contentRecoveryRow struct{ id, process, original, upload, object, phase string }

func loadContentRecovery(ctx context.Context, x postgres.SQLExecutor, materialization string) (contentRecoveryRow, bool, error) {
	var r contentRecoveryRow
	err := x.QueryRow(ctx, `SELECT id,recovery_process_id::text,original_process_id::text,COALESCE(upload_id::text,''),COALESCE(object_id::text,''),phase FROM agenteam_mcp.content_recoveries WHERE materialization_id=$1`, materialization).Scan(&r.id, &r.process, &r.original, &r.upload, &r.object, &r.phase)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, false, nil
	}
	return r, err == nil, safeError(err)
}

func (s *ContentStore) recoverContent(parent context.Context, materialization string) error {
	a := s.authority
	service := a.service()
	ctx, call, err := service.owner.begin(parent)
	if err != nil {
		return err
	}
	retained := false
	defer func() {
		if !retained {
			call.returnedWith(nil)
		}
	}()
	fact, err := loadContentFact(ctx, service.store, materialization)
	if err != nil {
		return err
	}
	if fact.state != "pending" {
		return nil
	}
	if fact.version.Validate() != nil || int64(fact.version) == math.MaxInt64 {
		return fail(f.InvalidState)
	}
	var rawProcess string
	if err = service.store.QueryRow(ctx, `SELECT COALESCE(process_id::text,'') FROM agenteam_mcp.materializations WHERE id=$1`, materialization).Scan(&rawProcess); err != nil {
		return safeError(err)
	}
	process, err := f.ParseID[oc.Process](rawProcess)
	if err != nil || process == service.owner.process {
		return fail(f.Forbidden)
	}
	if err = service.owner.guard.ConfirmStopped(ctx, process); err != nil {
		return safeError(err)
	}
	prior, exists, err := loadContentRecovery(ctx, service.store, materialization)
	if err != nil {
		return err
	}
	if exists && prior.phase == "complete" {
		return fail(f.InvalidState)
	}
	if exists {
		previous, err := f.ParseID[oc.Process](prior.process)
		if err != nil || prior.original != process.String() {
			return fail(f.InvalidState)
		}
		if previous == service.owner.process {
			return fail(f.ResourceBusy)
		}
		if err = service.owner.guard.ConfirmStopped(ctx, previous); err != nil {
			return safeError(err)
		}
	}
	w := &contentRecoveryUse{authority: a, call: call, fact: fact, process: process, previousProcess: prior.process, stopped: true}
	w.owner, err = oc.NewObjectOwner(oc.MCPContent, materialization, fact.project)
	if err != nil {
		return fail(f.InvalidState)
	}
	w.key = f.IdempotencyKey(materialization)
	w.locks, err = contentRecoveryLocks(fact)
	if err != nil {
		return err
	}
	w.recovery = prior.id
	if !exists {
		request, err := f.NewID[f.Request]()
		if err != nil {
			return safeError(err)
		}
		w.recovery = request.String()
	}
	w.active.Store(true)
	ctx = context.WithValue(ctx, contentRecoveryKey{}, w)
	confirmed, err := s.advanceContentRecovery(ctx, w)
	if err == nil && confirmed {
		w.active.Store(false)
		return nil
	}
	// Keep this exact recovery owner even when publication or cleanup is
	// uncertain. Drain observes/finishes it; it never creates a new claim.
	retained = true
	call.returnedWith(func(observe context.Context) (bool, error) {
		done, err := s.advanceContentRecovery(context.WithValue(observe, contentRecoveryKey{}, w), w)
		if err == nil && done {
			w.active.Store(false)
		}
		return done, err
	})
	return err
}

func (s *ContentStore) claimContentRecovery(ctx context.Context, w *contentRecoveryUse) (bool, error) {
	service := s.authority.service()
	cause, _ := f.NewRecoveryCause("mcp.content.recovery", w.recovery, "")
	complete := false
	err := commit(service.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := service.store.AcquireAll(ctx, tx, w.locks); err != nil {
			return safeError(err)
		}
		if err := service.owner.requireInTx(ctx, tx, w.call); err != nil {
			return err
		}
		x, err := service.store.InTx(tx)
		if err != nil {
			return safeError(err)
		}
		fact, err := loadContentFact(ctx, x, w.fact.id)
		if err != nil {
			return err
		}
		var process string
		if err = x.QueryRow(ctx, `SELECT COALESCE(process_id::text,'') FROM agenteam_mcp.materializations WHERE id=$1`, w.fact.id).Scan(&process); err != nil {
			return safeError(err)
		}
		row, found, err := loadContentRecovery(ctx, x, w.fact.id)
		if err != nil {
			return err
		}
		completedFact := w.fact
		completedFact.state = "failed"
		completedFact.version++
		if found && row.id == w.recovery && row.original == w.process.String() && row.process == service.owner.process.String() && row.phase == "complete" && fact == completedFact && process == w.process.String() {
			complete = true
			return nil
		}
		if fact != w.fact || fact.state != "pending" || process != w.process.String() {
			return fail(f.Forbidden)
		}
		if !found {
			if w.previousProcess != "" {
				return fail(f.Forbidden)
			}
			_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.content_recoveries(id,materialization_id,project_id,original_process_id,recovery_process_id) VALUES($1,$2,$3,$4,$5)`, w.recovery, w.fact.id, w.fact.project, w.process.String(), service.owner.process.String())
			return safeError(err)
		}
		if row.id != w.recovery || row.original != w.process.String() || (row.process != w.previousProcess && row.process != service.owner.process.String()) {
			return fail(f.Forbidden)
		}
		if row.process != service.owner.process.String() {
			_, err = x.Exec(ctx, `UPDATE agenteam_mcp.content_recoveries SET recovery_process_id=$2 WHERE id=$1`, w.recovery, service.owner.process.String())
			return safeError(err)
		}
		return nil
	}))
	return complete, err
}

func (s *ContentStore) advanceContentRecovery(ctx context.Context, w *contentRecoveryUse) (bool, error) {
	if done, err := s.claimContentRecovery(ctx, w); err != nil || done {
		return done, err
	}
	a := s.authority
	mapping, found, err := a.recovery.Discover(ctx, w.owner, w.key)
	if err != nil {
		return false, safeError(err)
	}
	if found {
		if mapping.Validate() != nil || !mapping.Owner.Equal(w.owner) || mapping.Key != w.key || mapping.ExecutionID.String() != w.fact.execution || mapping.CreationCause != w.fact.request || mapping.AttemptID != (oc.AttemptID{}) && mapping.ProcessID != w.process {
			return false, fail(f.Forbidden)
		}
		if w.found && !sameRecoveryMapping(w.mapping, mapping) {
			return false, fail(f.Forbidden)
		}
		w.mapping, w.found = mapping, true
		if err = s.recordContentMapping(ctx, w); err != nil {
			return false, err
		}
		operation, _ := f.ParseID[oc.CleanupOperation](mapping.UploadID.String())
		cause, _ := oc.NewObjectCleanupCause(oc.CleanupDetails{Owner: w.owner, OperationID: operation, Reason: oc.AbandonedAttempt})
		if err = s.releaseContentReference(ctx, w, cause); err != nil {
			return false, err
		}
		if mapping.AttemptID != (oc.AttemptID{}) {
			if err = s.recovery.JoinMCPUploadWithinBudget(ctx, cause, w.key, w.process); err != nil {
				return false, safeError(err)
			}
		}
		result, err := s.recovery.DeleteUnreferencedWithinBudget(ctx, cause, mapping.ObjectID)
		if err != nil {
			return false, safeError(err)
		}
		if result.State != oc.CleanupCompleted || len(result.Remaining.References) != 0 || len(result.Remaining.ActiveLeases) != 0 {
			return false, fail(f.ResourceBusy)
		}
	} else if w.found {
		return false, fail(f.Forbidden)
	}
	return s.finishContentRecovery(ctx, w)
}

func (s *ContentStore) recordContentMapping(ctx context.Context, w *contentRecoveryUse) error {
	service := s.authority.service()
	cause, _ := f.NewRecoveryCause("mcp.content.recovery", w.recovery, "")
	return commit(service.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := service.store.AcquireAll(ctx, tx, w.locks); err != nil {
			return safeError(err)
		}
		// The first mapping has not been persisted yet; prove the claim before
		// asking the original Object resolver for the full current tuple.
		phase, err := s.authority.requireContentRecoveryClaim(ctx, tx, w, false)
		if err != nil {
			return err
		}
		actual, exists, err := s.authority.recovery.ResolveInTx(ctx, tx, w.owner, w.key)
		if err != nil {
			return safeError(err)
		}
		if !exists || !sameRecoveryMapping(actual, w.mapping) {
			return fail(f.Forbidden)
		}
		x, err := service.store.InTx(tx)
		if err != nil {
			return safeError(err)
		}
		row, _, err := loadContentRecovery(ctx, x, w.fact.id)
		if err != nil {
			return err
		}
		if phase != "observing" {
			if row.upload != w.mapping.UploadID.String() || row.object != w.mapping.ObjectID.String() {
				return fail(f.Forbidden)
			}
			return nil
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.content_recoveries SET upload_id=$2,object_id=$3,phase='reference' WHERE id=$1 AND phase='observing'`, w.recovery, w.mapping.UploadID.String(), w.mapping.ObjectID.String())
		return safeError(err)
	}))
}

func (s *ContentStore) releaseContentReference(ctx context.Context, w *contentRecoveryUse, cause oc.ObjectCleanupCause) error {
	request, err := oc.NewCleanupReleaseAccess(oc.AccessRequestDetails{Operation: oc.ReleaseForCleanupAccess, Cleanup: cause, ObjectID: w.mapping.ObjectID, UploadID: w.mapping.UploadID})
	if err != nil {
		return fail(f.InvalidArgument)
	}
	plan, err := s.objects.DiscoverAccess(ctx, request)
	if err != nil {
		return safeError(err)
	}
	transaction, _ := f.NewRecoveryCause("mcp.content.recovery", w.recovery, "")
	return commit(s.authority.service().store.WithinTx(ctx, transaction, func(ctx context.Context, tx f.Tx) error {
		locked, err := s.objects.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, w.locks)
		if err != nil {
			return safeError(err)
		}
		if err = s.recovery.ReleaseForCleanupInTx(ctx, tx, cause, w.mapping.ObjectID, plan, locked); err != nil {
			return safeError(err)
		}
		x, err := s.authority.service().store.InTx(tx)
		if err != nil {
			return safeError(err)
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.content_recoveries SET phase='object' WHERE id=$1 AND phase IN ('reference','object')`, w.recovery)
		return safeError(err)
	}))
}

func (s *ContentStore) finishContentRecovery(ctx context.Context, w *contentRecoveryUse) (bool, error) {
	service := s.authority.service()
	cause, _ := f.NewRecoveryCause("mcp.content.recovery", w.recovery, "")
	err := commit(service.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := service.store.AcquireAll(ctx, tx, w.locks); err != nil {
			return safeError(err)
		}
		phase, err := s.authority.requireContentRecovery(ctx, tx, w)
		if err != nil {
			return err
		}
		mapping, found, err := s.authority.recovery.ResolveInTx(ctx, tx, w.owner, w.key)
		if err != nil {
			return safeError(err)
		}
		if found != w.found || found && (!sameRecoveryMapping(mapping, w.mapping) || mapping.Disposition != "revoked" || phase != "object") || !found && phase != "observing" {
			return fail(f.Forbidden)
		}
		x, err := service.store.InTx(tx)
		if err != nil {
			return safeError(err)
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_mcp.materializations SET state='failed',version=version+1 WHERE id=$1 AND state='pending'`, w.fact.id); err != nil {
			return safeError(err)
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.content_recoveries SET phase='complete',completed_at=clock_timestamp() WHERE id=$1`, w.recovery)
		return safeError(err)
	}))
	return err == nil, err
}
