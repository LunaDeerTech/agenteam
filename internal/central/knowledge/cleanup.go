package knowledge

import (
	"context"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

func (a *Authority) CheckCleanupInTx(ctx context.Context, tx f.Tx, cause oc.ObjectCleanupCause, object oc.ObjectID) error {
	return a.checkCleanup(ctx, tx, cause, object, nil)
}

func (a *Authority) checkCleanup(ctx context.Context, tx f.Tx, cause oc.ObjectCleanupCause, object oc.ObjectID, upload *oc.UploadID) error {
	st := a.state()
	if st == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || cause.Validate() != nil || object.Validate() != nil || upload != nil && upload.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return unavailable(err)
	}
	d := cause.Details()
	p, k, err := knowledgeOwner(d.Owner)
	if err != nil {
		return err
	}
	switch d.Reason {
	case oc.ReplacedObject, oc.CancelledUpload, oc.OwnerDeleted:
	default:
		return fault(f.Forbidden)
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	locks, err := authorityLocks(id.Actor{}, p, id.Converge)
	if err != nil {
		return err
	}
	if err = st.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	var found bool
	var exactUpload any
	if upload != nil {
		exactUpload = upload.String()
	}
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.object_cleanup c
 WHERE c.id=$1 AND c.project_id=$2 AND c.document_id=$3 AND c.object_id=$4 AND c.reason=$5
 AND ($6::uuid IS NULL OR c.upload_id=$6::uuid) AND c.phase IN ('reference','object','completed')
 AND NOT EXISTS(SELECT 1 FROM agenteam_knowledge.documents d WHERE d.project_id=c.project_id
 AND d.id=c.document_id AND d.current_object_id=c.object_id))`, d.OperationID.String(), p.String(), k.String(), object.String(), string(d.Reason), exactUpload).Scan(&found)
	if err != nil {
		return unavailable(err)
	}
	if !found {
		return fault(f.Forbidden)
	}
	return nil
}

func (a *Authority) CheckProjectCleanupInTx(context.Context, f.Tx, id.Actor, oc.ProjectCleanupCause) error {
	// B03 must bind the real Project lifecycle registration and stop barrier.
	// A per-document cleanup record is never a Project deletion grant.
	return fault(f.DependencyUnbound)
}

var _ oc.CleanupAuthority = (*Authority)(nil)

type cleanupRecord struct {
	id       oc.CleanupID
	project  id.ProjectID
	document kc.DocumentID
	command  f.ID[command]
	object   oc.ObjectID
	upload   oc.UploadID
	reason   oc.CleanupReason
	phase    string
}

func loadCleanup(ctx context.Context, x postgres.SQLExecutor, key oc.CleanupID) (*cleanupRecord, error) {
	var operation, project, document, commandID, object, upload, reason, phase string
	err := x.QueryRow(ctx, `SELECT id::text,project_id::text,document_id::text,command_id::text,object_id::text,upload_id::text,reason,phase
 FROM agenteam_knowledge.object_cleanup WHERE id=$1`, key.String()).Scan(&operation, &project, &document, &commandID, &object, &upload, &reason, &phase)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	r := &cleanupRecord{reason: oc.CleanupReason(reason), phase: phase}
	if r.id, err = f.ParseID[oc.CleanupOperation](operation); err != nil {
		return nil, internal(err)
	}
	if r.project, err = f.ParseID[id.Project](project); err != nil {
		return nil, internal(err)
	}
	if r.document, err = f.ParseID[kc.Document](document); err != nil {
		return nil, internal(err)
	}
	if r.command, err = f.ParseID[command](commandID); err != nil {
		return nil, internal(err)
	}
	if r.object, err = f.ParseID[oc.StoredObject](object); err != nil {
		return nil, internal(err)
	}
	if r.upload, err = f.ParseID[oc.Upload](upload); err != nil {
		return nil, internal(err)
	}
	if r.id != key || r.reason != oc.OwnerDeleted && r.reason != oc.ReplacedObject && r.reason != oc.CancelledUpload || r.phase != "reference" && r.phase != "object" && r.phase != "completed" {
		return nil, internal(nil)
	}
	return r, nil
}
func (r cleanupRecord) cause() (oc.ObjectCleanupCause, error) {
	owner, err := oc.NewObjectOwner(oc.Knowledge, r.document.String(), r.project.String())
	if err != nil {
		return oc.ObjectCleanupCause{}, internal(err)
	}
	return oc.NewObjectCleanupCause(oc.CleanupDetails{OperationID: r.id, Owner: owner, Reason: r.reason})
}

func (s *Service) recoverCleanup(ctx context.Context, key oc.CleanupID) error {
	st := s.state()
	row, err := loadCleanup(ctx, st.store, key)
	if err != nil {
		return err
	}
	if row == nil || row.phase == "completed" {
		return nil
	}
	cause, err := row.cause()
	if err != nil {
		return err
	}
	transaction, err := f.NewRecoveryCause("knowledge.cleanup", key.String(), "")
	if err != nil {
		return portError(err)
	}
	if row.phase == "reference" {
		if row.reason == oc.CancelledUpload {
			registration, err := id.RegisterService(id.ObjectMaintenance)
			if err != nil {
				return internal(err)
			}
			scope, err := id.InProject(row.project)
			if err != nil {
				return internal(err)
			}
			actor, err := registration.Actor(row.id.String(), scope)
			if err != nil {
				return internal(err)
			}
			// The private D05 command key is the persisted Knowledge command UUID,
			// fixed before preparation. Never generate a new upload identity here.
			_, err = st.deps.Objects.CancelUpload(ctx, actor, cause.Details().Owner, f.IdempotencyKey(row.command.String()))
			var known *f.Fault
			if err != nil && (!errors.As(err, &known) || known.Code != f.InvalidState) {
				return portError(err)
			}
			// An already-attached upload requires ReferenceCleanup. A successful
			// cancellation is confirmed idempotently through that same exact gate.
		}
		request, err := oc.NewCleanupReleaseAccess(oc.AccessRequestDetails{Operation: oc.ReleaseForCleanupAccess, Cleanup: cause, ObjectID: row.object, UploadID: row.upload})
		if err != nil {
			return internal(err)
		}
		plan, err := st.deps.Objects.DiscoverAccess(ctx, request)
		if err != nil {
			return portError(err)
		}
		result := st.store.WithinTx(ctx, transaction, func(ctx context.Context, tx f.Tx) error {
			access, err := st.deps.Objects.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, nil)
			if err != nil {
				return portError(err)
			}
			x, err := st.store.InTx(tx)
			if err != nil {
				return portError(err)
			}
			if err = st.store.RequireHeldLocks(ctx, tx, access.Locks()); err != nil {
				return portError(err)
			}
			current, err := loadCleanup(ctx, x, key)
			if err != nil {
				return err
			}
			if current == nil || *current != *row {
				return fault(f.ResourceBusy)
			}
			if err = st.deps.ReferenceCleanup.ReleaseForCleanupInTx(ctx, tx, cause, row.object, plan, access); err != nil {
				return portError(err)
			}
			tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.object_cleanup SET phase='object' WHERE id=$1 AND phase='reference'`, key.String())
			if err != nil {
				return unavailable(err)
			}
			if tag.RowsAffected() != 1 {
				return internal(nil)
			}
			return nil
		})
		if err = txError(result); err != nil {
			return err
		}
		row.phase = "object"
	}
	// The real cleaner owns reader/writer join and physical deletion. Pending,
	// Unknown and any error keep the exact durable work for another attempt.
	result, err := st.deps.ObjectCleanup.DeleteUnreferenced(ctx, cause, row.object)
	if err != nil {
		return portError(err)
	}
	if result.OperationID != key {
		return internal(nil)
	}
	if result.State != oc.CleanupCompleted {
		return fault(f.ResourceBusy)
	}
	if len(result.Remaining.References) != 0 || len(result.Remaining.ActiveLeases) != 0 {
		return internal(nil)
	}
	locks, err := authorityLocks(id.Actor{}, row.project, id.Converge)
	if err != nil {
		return err
	}
	commit := st.store.WithinTx(ctx, transaction, func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := st.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		current, err := loadCleanup(ctx, x, key)
		if err != nil {
			return err
		}
		if current == nil {
			return fault(f.ResourceBusy)
		}
		if current.phase == "completed" {
			copy := *current
			copy.phase = row.phase
			if copy == *row {
				return nil
			}
		}
		if *current != *row {
			return fault(f.ResourceBusy)
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.object_cleanup SET phase='completed' WHERE id=$1 AND phase='object'`, key.String())
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return internal(nil)
		}
		return nil
	})
	return txError(commit)
}
