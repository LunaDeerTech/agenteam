package object

import (
	"context"
	"crypto/sha256"
	"encoding/json"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

var _ oc.DeletedObjectMetadataPurger = (*Service)(nil)

// The plan captures this private, bounded deletion graph. Public result fields
// and a caller supplied list of IDs can never select native rows for deletion.
type metadataDelete struct {
	Table string
	ID    string
	Row   json.RawMessage
}

type metadataBatch struct {
	anchors []metadataDelete // current cleanup, current attempt, upload, object
	rows    []metadataDelete // at most 32 rows, in actual FK deletion order
	locks   []foundation.LockRequest
	final   bool
}

func skillProjectCleanup(cause oc.ObjectCleanupCause) bool {
	if cause.Validate() != nil {
		return false
	}
	d := cause.Details()
	return d.Owner.Details().Kind == oc.SkillRevision && d.Reason == oc.ProjectDeleted
}

// consumeMetadataTransaction is deliberately not reset after an error or a
// Pending response. The caller must end this transaction before another batch.
func (s *Service) consumeMetadataTransaction(tx foundation.Tx) error {
	r := s.state()
	if _, err := r.store.InTx(tx); err != nil {
		return unavailable(err)
	}
	r.accessMu.Lock()
	defer r.accessMu.Unlock()
	if !r.accessTransactions[tx] || r.metadataTransactions[tx] {
		return invalid()
	}
	if r.metadataTransactions == nil {
		r.metadataTransactions = make(map[foundation.Tx]bool)
	}
	r.metadataTransactions[tx] = true
	return nil
}

func (s *Service) PurgeDeletedObjectMetadataInTx(ctx context.Context, tx foundation.Tx, cause oc.ObjectCleanupCause, id oc.ObjectID, plan oc.AccessLockPlan, locked oc.LockedAccess) (oc.ObjectMetadataPurgeResult, error) {
	request, err := oc.NewObjectCleanupAccess(oc.AccessRequestDetails{Operation: oc.PurgeDeletedObjectMetadataAccess, Cleanup: cause, ObjectID: id})
	if err != nil {
		return oc.ObjectMetadataPurgeResult{}, invalid()
	}
	out := oc.ObjectMetadataPurgeResult{State: oc.CleanupPending, OperationID: cause.Details().OperationID, ObjectID: id}
	if err = s.ValidateAccessPlanInTx(ctx, tx, request, plan, locked); err != nil {
		return out, err
	}
	if nilPort(s.state().auth.Cleanup) {
		return out, failure(foundation.DependencyUnbound, nil)
	}
	if err = s.state().auth.Cleanup.CheckCleanupInTx(ctx, tx, cause, id); err != nil {
		return out, portError(err)
	}
	if err = s.consumeMetadataTransaction(tx); err != nil {
		return out, err
	}
	e, err := executor(s, tx)
	if err != nil {
		return out, err
	}
	batch, err := s.discoverMetadataBatch(ctx, e, cause, id)
	if err != nil {
		return out, err
	}
	if err = s.metadataPhysicalCompleted(ctx, e, id); err != nil {
		return out, err
	}
	rows := batch.rows
	if batch.final {
		rows = batch.anchors
		// The upload->current-attempt edge is immediate. All four anchors are
		// still present until this final, caller-owned transaction.
		tag, err := e.Exec(ctx, `UPDATE agenteam_object.uploads SET current_attempt_id=NULL WHERE id=$1 AND object_id=$2 AND current_attempt_id=$3`, batch.anchors[2].ID, id.String(), batch.anchors[1].ID)
		if err != nil {
			return out, unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return out, accessChanged()
		}
	}
	if len(rows) == 0 || len(rows) > oc.ObjectMetadataPurgeBatchLimit {
		return out, invalid()
	}
	for _, row := range rows {
		query, err := metadataDeleteSQL(row.Table)
		if err != nil {
			return out, err
		}
		tag, err := e.Exec(ctx, query, row.ID)
		if err != nil {
			return out, unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return out, accessChanged()
		}
	}
	if batch.final {
		out.State = oc.CleanupCompleted
	}
	return out, nil
}

func metadataDeleteSQL(table string) (string, error) {
	switch table {
	case "cleanup_operations", "upload_attempts", "uploads", "objects", "object_transfers", "object_leases", "project_work":
		return `DELETE FROM agenteam_object.` + table + ` WHERE id=$1`, nil
	default:
		return "", invalid()
	}
}

func metadataRecord(ctx context.Context, e postgres.SQLExecutor, table, id string) (metadataDelete, error) {
	if _, err := metadataDeleteSQL(table); err != nil {
		return metadataDelete{}, err
	}
	var raw []byte
	if err := e.QueryRow(ctx, `SELECT to_jsonb(r) FROM agenteam_object.`+table+` r WHERE id=$1`, id).Scan(&raw); err != nil {
		return metadataDelete{}, unavailable(err)
	}
	return metadataDelete{Table: table, ID: id, Row: json.RawMessage(raw)}, nil
}

// LIMIT is a bounded work list, never a terminal-state test. Every completion
// decision below uses full EXISTS predicates under the original lock union.
func metadataIDs(ctx context.Context, e postgres.SQLExecutor, query string, args ...any) ([]string, error) {
	rows, err := e.Query(ctx, query, args...)
	if err != nil {
		return nil, unavailable(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, unavailable(err)
		}
		if _, err = foundation.ParseID[struct{}](id); err != nil || len(ids) == oc.ObjectMetadataPurgeBatchLimit {
			return nil, unavailable(err)
		}
		ids = append(ids, id)
	}
	return ids, unavailableIf(rows.Err())
}

func (s *Service) discoverMetadataBatch(ctx context.Context, e postgres.SQLExecutor, cause oc.ObjectCleanupCause, id oc.ObjectID) (metadataBatch, error) {
	var batch metadataBatch
	if !skillProjectCleanup(cause) || id.Validate() != nil {
		return batch, invalid()
	}
	u, found, err := scanUpload(e.QueryRow(ctx, `SELECT `+uploadColumns+` FROM agenteam_object.uploads WHERE object_id=$1`, id.String()))
	if err != nil {
		return batch, err
	}
	if !found || !u.owner.Equal(cause.Details().Owner) || u.disposition != "revoked" || u.state != "committed" || u.attempt.Validate() != nil {
		return batch, failure(foundation.Forbidden, nil)
	}
	obj, found, err := loadObject(ctx, e, id)
	if err != nil {
		return batch, err
	}
	if !found || !objectPartition(obj, u.owner) || obj.meta.State != oc.Deleted || !obj.cleaning {
		return batch, failure(foundation.ResourceBusy, nil)
	}
	var cleanup string
	err = e.QueryRow(ctx, `SELECT c.id::text FROM agenteam_object.cleanup_operations c JOIN agenteam_object.upload_attempts a ON a.id=c.attempt_id WHERE c.object_id=$1 AND a.object_id=$1 AND a.upload_id=$2 AND c.attempt_id=$3 AND c.operation_id=$4 AND c.reason='project_deleted' AND c.phase='completed' AND a.phase='cleaned' AND a.kind='private_candidate' AND a.io_closed AND a.cleanup_gate`, id.String(), u.id.String(), u.attempt.String(), cause.Details().OperationID.String()).Scan(&cleanup)
	if err != nil {
		return batch, unavailable(err)
	}
	for _, r := range []metadataDelete{{Table: "cleanup_operations", ID: cleanup}, {Table: "upload_attempts", ID: u.attempt.String()}, {Table: "uploads", ID: u.id.String()}, {Table: "objects", ID: id.String()}} {
		record, err := metadataRecord(ctx, e, r.Table, r.ID)
		if err != nil {
			return batch, err
		}
		batch.anchors = append(batch.anchors, record)
	}
	project, _ := foundation.ProjectLock(u.owner.Details().ProjectID)
	batch.locks = append(batch.locks, foundation.LockRequest{Key: project, Mode: foundation.Exclusive})
	appendRow := func(table, id string) error {
		r, err := metadataRecord(ctx, e, table, id)
		if err != nil {
			return err
		}
		batch.rows = append(batch.rows, r)
		return nil
	}
	// PUT staging and transfer form a deferred two-way edge. The private
	// candidate remains until all transfer edges have been removed.
	ids, err := metadataIDs(ctx, e, `SELECT t.id::text FROM agenteam_object.object_transfers t WHERE t.object_id=$1 ORDER BY t.id LIMIT 32`, id.String())
	if err != nil {
		return batch, err
	}
	for _, raw := range ids {
		tid, _ := foundation.ParseID[oc.Transfer](raw)
		t, found, err := loadTransfer(ctx, e, tid)
		if err != nil {
			return batch, err
		}
		if !found || t.object != id || !t.revoked || t.retirement.Validate() != nil || t.leaseActive {
			return batch, failure(foundation.ResourceBusy, nil)
		}
		var leasesSafe bool
		err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_leases WHERE id=$1 AND object_id=$3 AND state='released') AND ($2::uuid IS NULL OR EXISTS(SELECT 1 FROM agenteam_object.object_leases WHERE id=$2 AND object_id=$3 AND state='released'))`, t.lease.String(), null(t.sourceLease.String()), id.String()).Scan(&leasesSafe)
		if err != nil {
			return batch, unavailable(err)
		}
		if !leasesSafe {
			return batch, failure(foundation.ResourceBusy, nil)
		}
		cost := 1
		var stagingCleanup string
		if t.spec.Details().Direction == oc.TransferPUT {
			if t.upload != u.id || t.staging == u.attempt || !t.stagingCleaned {
				return batch, failure(foundation.ResourceBusy, nil)
			}
			err = e.QueryRow(ctx, `SELECT c.id::text FROM agenteam_object.cleanup_operations c JOIN agenteam_object.upload_attempts a ON a.id=c.attempt_id WHERE a.id=$1 AND a.transfer_id=$2 AND a.object_id=$3 AND a.upload_id=$4 AND a.kind='runner_staging' AND a.phase='cleaned' AND c.phase='completed' AND c.object_id=$3`, t.staging.String(), raw, id.String(), u.id.String()).Scan(&stagingCleanup)
			if err != nil {
				return batch, unavailable(err)
			}
			cost = 3
		}
		if len(batch.rows)+cost > oc.ObjectMetadataPurgeBatchLimit {
			break
		}
		request, err := t.request(t.actor, oc.TransferInspect, nil, nil, oc.PreparedPayload{})
		if err != nil {
			return batch, err
		}
		facts, err := s.transferAccessFacts(ctx, e, request.Details().Transfer)
		if err != nil {
			return batch, err
		}
		for _, lock := range facts.locks {
			lock.Mode = foundation.Exclusive
			batch.locks = append(batch.locks, lock)
		}
		if cost == 3 {
			if err = appendRow("cleanup_operations", stagingCleanup); err != nil {
				return batch, err
			}
			if err = appendRow("upload_attempts", t.staging.String()); err != nil {
				return batch, err
			}
		}
		if err = appendRow("object_transfers", raw); err != nil {
			return batch, err
		}
	}
	if len(batch.rows) != 0 {
		return batch, nil
	}
	for _, phase := range []struct{ table, query string }{
		{"object_leases", `SELECT l.id::text FROM agenteam_object.object_leases l WHERE l.object_id=$1 ORDER BY l.id LIMIT 32`},
		{"project_work", `SELECT w.id::text FROM agenteam_object.project_work w WHERE w.object_id=$1 ORDER BY w.id LIMIT 32`},
	} {
		ids, err = metadataIDs(ctx, e, phase.query, id.String())
		if err != nil {
			return batch, err
		}
		for _, raw := range ids {
			if err = appendRow(phase.table, raw); err != nil {
				return batch, err
			}
			if phase.table == "project_work" {
				batch.locks = append(batch.locks, workLock(raw))
			}
		}
		if len(batch.rows) != 0 {
			return batch, nil
		}
	}
	ids, err = metadataIDs(ctx, e, `SELECT a.id::text FROM agenteam_object.upload_attempts a WHERE a.object_id=$1 AND a.id<>$2 ORDER BY a.id LIMIT 16`, id.String(), u.attempt.String())
	if err != nil {
		return batch, err
	}
	for _, raw := range ids {
		var oldCleanup string
		err = e.QueryRow(ctx, `SELECT c.id::text FROM agenteam_object.cleanup_operations c JOIN agenteam_object.upload_attempts a ON a.id=c.attempt_id WHERE a.id=$1 AND a.object_id=$2 AND a.upload_id=$3 AND a.kind='private_candidate' AND a.phase='cleaned' AND a.io_closed AND c.object_id=$2 AND c.phase='completed'`, raw, id.String(), u.id.String()).Scan(&oldCleanup)
		if err != nil {
			return batch, unavailable(err)
		}
		if err = appendRow("cleanup_operations", oldCleanup); err != nil {
			return batch, err
		}
		if err = appendRow("upload_attempts", raw); err != nil {
			return batch, err
		}
	}
	batch.final = len(batch.rows) == 0
	return batch, nil
}

func (s *Service) metadataAccessFacts(ctx context.Context, e postgres.SQLExecutor, cause oc.ObjectCleanupCause, id oc.ObjectID, base accessFacts) (accessFacts, error) {
	batch, err := s.discoverMetadataBatch(ctx, e, cause, id)
	if err != nil {
		return accessFacts{}, err
	}
	raw, err := json.Marshal([]any{base.binding, batch.anchors, batch.rows, batch.final})
	if err != nil {
		return accessFacts{}, invalid()
	}
	hash := sha256.Sum256(raw)
	base.binding = newDigest(hash[:])
	base.locks = append(base.locks, batch.locks...)
	return base, nil
}

func (s *Service) metadataPhysicalCompleted(ctx context.Context, e postgres.SQLExecutor, object oc.ObjectID) error {
	// Deleted was produced together with native ObjectDelete Audit in its
	// original transaction. Do not recompute that Audit's earliest cause after
	// earlier batches have legitimately removed old cleanup history.
	var pending bool
	err := e.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM agenteam_object.object_references WHERE object_id=$1)
	OR EXISTS(SELECT 1 FROM agenteam_object.object_leases WHERE object_id=$1 AND state='active')
 OR EXISTS(SELECT 1 FROM agenteam_object.upload_attempts WHERE object_id=$1 AND (phase<>'cleaned' OR NOT cleanup_gate OR (kind='private_candidate' AND NOT io_closed)))
 OR EXISTS(SELECT 1 FROM agenteam_object.cleanup_operations WHERE object_id=$1 AND phase<>'completed')
 OR EXISTS(SELECT 1 FROM agenteam_object.project_work WHERE object_id=$1 AND joined_at IS NULL)
 OR EXISTS(SELECT 1 FROM agenteam_object.object_transfers WHERE object_id=$1 AND (revoked_at IS NULL OR retirement_evidence IS NULL))`, object.String()).Scan(&pending)
	if err != nil {
		return unavailable(err)
	}
	if pending {
		return failure(foundation.ResourceBusy, nil)
	}
	// A committed joined_at also has a local actual-return obligation. A
	// stale/forged durable flag never turns a live local operation into proof.
	r := s.state()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, h := range r.projectWork {
		if h.work.object != object {
			continue
		}
		if !h.ended {
			return failure(foundation.ResourceBusy, nil)
		}
		if _, err = r.store.InTx(h.origin); err == nil {
			return failure(foundation.ResourceBusy, nil)
		}
	}
	return nil
}
