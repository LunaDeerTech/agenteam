package object

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func (s *Service) AttachObjectInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, owner oc.ObjectOwner, id oc.ObjectID, plan oc.AccessLockPlan, locked oc.LockedAccess) (oc.ObjectReference, error) {
	return s.attach(ctx, tx, actor, owner, id, oc.UploadReceipt{}, plan, locked)
}
func (s *Service) ConsumeUploadInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, owner oc.ObjectOwner, receipt oc.UploadReceipt, plan oc.AccessLockPlan, locked oc.LockedAccess) (oc.ObjectReference, error) {
	if receipt.Validate() != nil {
		return oc.ObjectReference{}, invalid()
	}
	return s.attach(ctx, tx, actor, owner, receipt.Details().ObjectID, receipt, plan, locked)
}
func (s *Service) attach(ctx context.Context, tx foundation.Tx, actor identity.Actor, owner oc.ObjectOwner, id oc.ObjectID, receipt oc.UploadReceipt, plan oc.AccessLockPlan, locked oc.LockedAccess) (oc.ObjectReference, error) {
	if id.Validate() != nil {
		return oc.ObjectReference{}, invalid()
	}
	if err := s.metadataAdmission(ctx); err != nil {
		return oc.ObjectReference{}, err
	}
	request := ownerRequest(actor, owner, oc.AttachAccess, oc.AccessRequestDetails{ObjectID: id})
	if receipt.Validate() == nil {
		request = ownerRequest(actor, owner, oc.ConsumeAccess, oc.AccessRequestDetails{Receipt: receipt})
	}
	if err := s.ValidateAccessPlanInTx(ctx, tx, request, plan, locked); err != nil {
		return oc.ObjectReference{}, err
	}
	grant, err := s.authorize(ctx, tx, actor, owner, identity.Mutate)
	if err != nil {
		return oc.ObjectReference{}, err
	}
	if grant.Details().Existence != oc.ExistingOwner {
		return oc.ObjectReference{}, failure(foundation.Forbidden, nil)
	}
	if err = s.gate(ctx, tx, actor, owner, identity.Mutate); err != nil {
		return oc.ObjectReference{}, err
	}
	e, err := executor(s, tx)
	if err != nil {
		return oc.ObjectReference{}, err
	}
	obj, found, err := loadObject(ctx, e, id)
	if err != nil {
		return oc.ObjectReference{}, err
	}
	if !found {
		return oc.ObjectReference{}, failure(foundation.NotFound, nil)
	}
	if !objectPartition(obj, owner) {
		return oc.ObjectReference{}, failure(foundation.Forbidden, nil)
	}
	if obj.cleaning || obj.meta.State == oc.Deleted {
		return oc.ObjectReference{}, deleted(false)
	}
	if obj.meta.State != oc.Available {
		return oc.ObjectReference{}, failure(foundation.InvalidState, nil)
	}
	u, found, err := scanUpload(e.QueryRow(ctx, `SELECT `+uploadColumns+` FROM agenteam_object.uploads WHERE object_id=$1`, id.String()))
	if err != nil {
		return oc.ObjectReference{}, err
	}
	if !found || !u.owner.Equal(owner) {
		return oc.ObjectReference{}, failure(foundation.Forbidden, nil)
	}
	if u.disposition == "revoked" {
		return oc.ObjectReference{}, deleted(u.state == "committed")
	}
	if u.existence == oc.ProspectiveOwner {
		if receipt.Validate() != nil {
			return oc.ObjectReference{}, failure(foundation.Forbidden, nil)
		}
		d := receipt.Details()
		if d.ID != u.receipt || d.UploadID != u.id || d.ObjectID != id || !d.Owner.Equal(owner) || d.CreationCause != u.creation || grant.Details().CreationCause != u.creation {
			return oc.ObjectReference{}, failure(foundation.Forbidden, nil)
		}
		if err = checkOriginal(u, actor, owner); err != nil {
			return oc.ObjectReference{}, err
		}
	}
	if u.state != "committed" {
		return oc.ObjectReference{}, failure(foundation.InvalidState, nil)
	}
	// A revoked/released canonical cannot be recreated through a stale receipt.
	// Repeating the same active canonical is the only consumed-receipt replay.
	var canonical bool
	err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_references WHERE object_id=$1 AND owner_kind=$2 AND owner_id=$3 AND kind='canonical')`, id.String(), string(owner.Details().Kind), owner.Details().ID).Scan(&canonical)
	if err != nil {
		return oc.ObjectReference{}, unavailable(err)
	}
	ref := oc.ObjectReference{ObjectID: id, Owner: owner, Kind: oc.CanonicalReference}
	if canonical {
		return ref, nil
	}
	if u.disposition == "attached" {
		if u.existence == oc.ExistingOwner {
			_, err = e.Exec(ctx, `INSERT INTO agenteam_object.object_references(object_id,owner_kind,owner_id,partition_id,kind,upload_id) VALUES($1,$2,$3,$4,'canonical',$5)`, id.String(), string(owner.Details().Kind), owner.Details().ID, owner.Partition(), u.id.String())
			if err != nil {
				return oc.ObjectReference{}, unavailable(err)
			}
			return ref, nil
		}
		return oc.ObjectReference{}, failure(foundation.InvalidState, nil)
	}
	tag, err := e.Exec(ctx, `UPDATE agenteam_object.object_references SET kind='canonical' WHERE object_id=$1 AND owner_kind=$2 AND owner_id=$3 AND upload_id=$4 AND kind='reserved'`, id.String(), string(owner.Details().Kind), owner.Details().ID, u.id.String())
	if err != nil {
		return oc.ObjectReference{}, unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return oc.ObjectReference{}, failure(foundation.InvalidState, nil)
	}
	_, err = e.Exec(ctx, `UPDATE agenteam_object.uploads SET disposition='attached' WHERE id=$1`, u.id.String())
	if err != nil {
		return oc.ObjectReference{}, unavailable(err)
	}
	return ref, nil
}
func (s *Service) ReleaseObjectInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, owner oc.ObjectOwner, id oc.ObjectID, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
	if id.Validate() != nil {
		return invalid()
	}
	request := ownerRequest(actor, owner, oc.ReleaseAccess, oc.AccessRequestDetails{ObjectID: id})
	if err := s.ValidateAccessPlanInTx(ctx, tx, request, plan, locked); err != nil {
		return err
	}
	grant, err := s.authorize(ctx, tx, actor, owner, identity.Converge)
	if err != nil {
		return err
	}
	if grant.Details().Existence != oc.ExistingOwner {
		return failure(foundation.Forbidden, nil)
	}
	if err = s.gate(ctx, tx, actor, owner, identity.Converge); err != nil {
		return err
	}
	e, err := executor(s, tx)
	if err != nil {
		return err
	}
	obj, found, err := loadObject(ctx, e, id)
	if err != nil {
		return err
	}
	if !found {
		return failure(foundation.NotFound, nil)
	}
	if !objectPartition(obj, owner) {
		return failure(foundation.Forbidden, nil)
	}
	_, err = e.Exec(ctx, `DELETE FROM agenteam_object.object_references WHERE object_id=$1 AND owner_kind=$2 AND owner_id=$3 AND partition_id=$4 AND kind='canonical'`, id.String(), string(owner.Details().Kind), owner.Details().ID, owner.Partition())
	return unavailableIf(err)
}
func (s *Service) metadataAdmission(ctx context.Context) error {
	if op, ok := ctx.Value(operationKey{}).(*operation); ok && op.service == s {
		select {
		case <-op.done:
			return failure(foundation.ShuttingDown, nil)
		default:
			return nil
		}
	}
	r := s.state()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped || r.forced {
		return failure(foundation.ShuttingDown, nil)
	}
	if !r.initialized || r.runtime != nil && !r.runtimeReady {
		return unavailable(nil)
	}
	return nil
}
func inspect(ctx context.Context, e postgres.SQLExecutor, id oc.ObjectID) (oc.ReferenceInspection, error) {
	out := oc.ReferenceInspection{References: []oc.ObjectReference{}, ActiveLeases: []oc.ObjectLease{}}
	rows, err := e.Query(ctx, `SELECT owner_kind,owner_id::text,partition_id::text,kind FROM agenteam_object.object_references WHERE object_id=$1 ORDER BY owner_kind,owner_id`, id.String())
	if err != nil {
		return out, unavailable(err)
	}
	for rows.Next() {
		var kind oc.OwnerKind
		var owner, partition string
		var reference oc.ReferenceKind
		if err = rows.Scan(&kind, &owner, &partition, &reference); err != nil {
			rows.Close()
			return out, unavailable(err)
		}
		project := partition
		if kind == oc.Avatar {
			project = ""
		}
		o, err := oc.NewObjectOwner(kind, owner, project)
		if err != nil {
			rows.Close()
			return out, unavailable(err)
		}
		out.References = append(out.References, oc.ObjectReference{ObjectID: id, Owner: o, Kind: reference})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, unavailable(err)
	}
	rows, err = e.Query(ctx, `SELECT id::text,owner_kind,owner_id::text FROM agenteam_object.object_leases WHERE object_id=$1 AND state='active' ORDER BY owner_kind,owner_id,id`, id.String())
	if err != nil {
		return out, unavailable(err)
	}
	for rows.Next() {
		var raw, owner string
		var kind oc.LeaseOwnerKind
		if err = rows.Scan(&raw, &kind, &owner); err != nil {
			rows.Close()
			return out, unavailable(err)
		}
		lease, err := foundation.ParseID[oc.Lease](raw)
		if err != nil {
			rows.Close()
			return out, unavailable(err)
		}
		o, err := oc.NewLeaseOwner(kind, owner)
		if err != nil {
			rows.Close()
			return out, unavailable(err)
		}
		out.ActiveLeases = append(out.ActiveLeases, oc.ObjectLease{ID: lease, ObjectID: id, Owner: o})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, unavailable(err)
	}
	return out, nil
}

// InspectReferences is an internal cleanup projection, not an HTTP read port.
func (s *Service) InspectReferences(ctx context.Context, id oc.ObjectID) (oc.ReferenceInspection, error) {
	if id.Validate() != nil {
		return oc.ReferenceInspection{}, invalid()
	}
	op, finish, err := s.begin(ctx)
	if err != nil {
		return oc.ReferenceInspection{}, err
	}
	defer finish()
	var out oc.ReferenceInspection
	result := s.withinAccess(op.ctx, recoveryCause(), s.maintenanceRequest(oc.InspectAccess, id, oc.AccessRequestDetails{}), func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		out, err = inspect(ctx, e, id)
		return err
	})
	if err = commitError(result); err != nil {
		return oc.ReferenceInspection{}, err
	}
	return out, nil
}
