package knowledge

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
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
