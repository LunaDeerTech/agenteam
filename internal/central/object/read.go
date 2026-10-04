package object

import (
	"bytes"
	"context"
	"errors"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/jackc/pgx/v5"
)

func (s *Service) LookupPut(ctx context.Context, actor identity.Actor, owner oc.ObjectOwner, key foundation.IdempotencyKey) (oc.LookupResult, error) {
	op, finish, err := s.begin(ctx)
	if err != nil {
		return oc.LookupResult{}, err
	}
	defer finish()
	return s.lookup(op.ctx, actor, owner, key, nil)
}
func (s *Service) lookup(ctx context.Context, actor identity.Actor, owner oc.ObjectOwner, key foundation.IdempotencyKey, expectedSemantic []byte) (oc.LookupResult, error) {
	command, err := commandIdentity(owner, key)
	if err != nil {
		return oc.LookupResult{}, err
	}
	var out oc.LookupResult
	var semantic foundation.Digest
	if expectedSemantic != nil {
		semantic = newDigest(expectedSemantic)
	}
	request := ownerRequest(actor, owner, oc.LookupAccess, oc.AccessRequestDetails{Key: key, ExpectedSemantic: semantic})
	result := s.withinAccess(ctx, recoveryCause(), request, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		if _, err := s.authorize(ctx, tx, actor, owner, identity.Read); err != nil {
			return err
		}
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		u, found, err := loadCommand(ctx, e, command)
		if err != nil {
			return err
		}
		var object oc.ObjectID
		if found {
			object = u.object
		}
		if _, err = s.authorize(ctx, tx, actor, owner, identity.Read); err != nil {
			return err
		}
		if err = s.gate(ctx, tx, actor, owner, identity.Read); err != nil {
			return err
		}
		u, found, err = loadCommand(ctx, e, command)
		if err != nil {
			return err
		}
		if !found {
			out.State = oc.NotObserved
			return nil
		}
		if object != u.object {
			return failure(foundation.ResourceBusy, nil)
		}
		if err = checkOriginal(u, actor, owner); err != nil {
			return err
		}
		// Resolving an unknown PUT must still identify this exact request. The
		// original transaction may have rolled back and another request may have
		// won the same command key before this locked read.
		if expectedSemantic != nil && !bytes.Equal(u.digest, expectedSemantic) {
			return failure(foundation.IdempotencyKeyReused, nil)
		}
		obj, ok, err := loadObject(ctx, e, u.object)
		if err != nil {
			return err
		}
		if !ok || !objectPartition(obj, owner) {
			return unavailable(nil)
		}
		if u.disposition == "revoked" {
			out.State = oc.UploadRevoked
			id := u.object
			out.ObjectID = &id
			out.Cleanup = oc.CleanupPending
			if obj.meta.State == oc.Deleted {
				out.Cleanup = oc.CleanupCompleted
			}
			return nil
		}
		switch u.state {
		case "pending":
			out.State = oc.UploadPending
		case "unknown":
			out.State = oc.UploadUnknown
		case "failed":
			out.State = oc.UploadFailed
		case "committed":
			out.State = oc.UploadCommitted
			meta := obj.meta
			out.Meta = &meta
			out.Receipt = receiptOf(u)
		default:
			return unavailable(nil)
		}
		return nil
	})
	if err = commitError(result); err != nil {
		return oc.LookupResult{}, err
	}
	return out, nil
}

// readRow requires all known owner/object locks. A lease is merely protection;
// only the exact current authority grant can turn a stable use into permission.
func (s *Service) readRow(ctx context.Context, tx foundation.Tx, actor identity.Actor, owner oc.ObjectOwner, id oc.ObjectID) (objectRow, error) {
	grant, err := s.authorize(ctx, tx, actor, owner, identity.Read)
	if err != nil {
		return objectRow{}, err
	}
	if grant.Details().Existence != oc.ExistingOwner {
		return objectRow{}, failure(foundation.Forbidden, nil)
	}
	if err = s.gate(ctx, tx, actor, owner, identity.Read); err != nil {
		return objectRow{}, err
	}
	e, err := executor(s, tx)
	if err != nil {
		return objectRow{}, err
	}
	obj, found, err := loadObject(ctx, e, id)
	if err != nil {
		return objectRow{}, err
	}
	if !found {
		return objectRow{}, failure(foundation.NotFound, nil)
	}
	if !objectPartition(obj, owner) {
		return objectRow{}, failure(foundation.Forbidden, nil)
	}
	if obj.cleaning || obj.meta.State == oc.Deleted {
		return objectRow{}, deleted(false)
	}
	if obj.meta.State != oc.Available {
		return objectRow{}, failure(foundation.InvalidState, nil)
	}
	var canonical bool
	err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_references WHERE object_id=$1 AND owner_kind=$2 AND owner_id=$3 AND partition_id=$4 AND kind='canonical')`, id.String(), string(owner.Details().Kind), owner.Details().ID, owner.Partition()).Scan(&canonical)
	if err != nil {
		return objectRow{}, unavailable(err)
	}
	if canonical {
		return obj, nil
	}
	if nilPort(s.state().auth.Read) {
		return objectRow{}, failure(foundation.DependencyUnbound, nil)
	}
	exact, err := s.state().auth.Read.AuthorizeObjectReadInTx(ctx, tx, actor, owner, id)
	if err != nil {
		return objectRow{}, portError(err)
	}
	if !exact.MatchesObjectRead(actor, owner, id) || exact.Details().ProtectedLease == nil {
		return objectRow{}, failure(foundation.Forbidden, nil)
	}
	lease := *exact.Details().ProtectedLease
	var active bool
	err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_leases WHERE id=$1 AND object_id=$2 AND owner_kind=$3 AND owner_id=$4 AND state='active')`, lease.ID.String(), id.String(), string(lease.Owner.Details().Kind), lease.Owner.Details().ID).Scan(&active)
	if err != nil {
		return objectRow{}, unavailable(err)
	}
	if !active {
		return objectRow{}, failure(foundation.Forbidden, nil)
	}
	return obj, nil
}
func (s *Service) StatObject(ctx context.Context, actor identity.Actor, owner oc.ObjectOwner, id oc.ObjectID) (oc.ObjectMeta, error) {
	if id.Validate() != nil {
		return oc.ObjectMeta{}, invalid()
	}
	op, finish, err := s.begin(ctx)
	if err != nil {
		return oc.ObjectMeta{}, err
	}
	defer finish()
	var obj objectRow
	request, _ := oc.NewObjectReadAccess(oc.AccessRequestDetails{Operation: oc.StatAccess, Actor: actor, Owner: owner, Intent: identity.Read, ObjectID: id})
	result := s.withinAccess(op.ctx, recoveryCause(), request, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		if _, err := s.authorize(ctx, tx, actor, owner, identity.Read); err != nil {
			return err
		}
		var err error
		obj, err = s.readRow(ctx, tx, actor, owner, id)
		return err
	})
	if err = commitError(result); err != nil {
		return oc.ObjectMeta{}, err
	}
	return obj.meta, nil
}
func (s *Service) ReadObject(ctx context.Context, actor identity.Actor, owner oc.ObjectOwner, id oc.ObjectID, wanted *oc.ByteRange) (*oc.ObjectReader, error) {
	return s.openRead(ctx, actor, owner, id, wanted, oc.UploadReceipt{})
}
func (s *Service) OpenUploadSource(ctx context.Context, actor identity.Actor, owner oc.ObjectOwner, receipt oc.UploadReceipt) (*oc.ObjectReader, error) {
	if receipt.Validate() != nil {
		return nil, invalid()
	}
	return s.openRead(ctx, actor, owner, receipt.Details().ObjectID, nil, receipt)
}
func (s *Service) openRead(ctx context.Context, actor identity.Actor, owner oc.ObjectOwner, id oc.ObjectID, wanted *oc.ByteRange, receipt oc.UploadReceipt) (*oc.ObjectReader, error) {
	if id.Validate() != nil {
		return nil, invalid()
	}
	// Freeze caller-owned range data before any asynchronous work.
	var requested *oc.ByteRange
	if wanted != nil {
		v := *wanted
		requested = &v
	}
	op, finish, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			finish()
		}
	}()
	leaseID, err := foundation.NewID[oc.Lease]()
	if err != nil {
		return nil, unavailable(err)
	}
	var obj objectRow
	var resolved *oc.ResolvedRange
	kind := oc.ReaderOwner
	if receipt.Validate() == nil {
		kind = oc.SourceOwner
	}
	requestDetails := oc.AccessRequestDetails{Operation: oc.ReadAccess, Actor: actor, Owner: owner, Intent: identity.Read, ObjectID: id, Range: requested}
	if kind == oc.SourceOwner {
		requestDetails.Operation = oc.OpenSourceAccess
		requestDetails.Receipt = receipt
	}
	request, _ := oc.NewObjectReadAccess(requestDetails)
	result := s.withinAccess(op.ctx, recoveryCause(), request, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		if _, err := s.authorize(ctx, tx, actor, owner, identity.Read); err != nil {
			return err
		}
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		if kind == oc.SourceOwner {
			grant, err := s.authorize(ctx, tx, actor, owner, identity.Read)
			if err != nil {
				return err
			}
			if err = s.gate(ctx, tx, actor, owner, identity.Read); err != nil {
				return err
			}
			d := receipt.Details()
			u, found, err := loadUpload(ctx, e, d.UploadID)
			if err != nil {
				return err
			}
			if !found {
				return failure(foundation.NotFound, nil)
			}
			if err = checkOriginal(u, actor, owner); err != nil {
				return err
			}
			if u.receipt != d.ID || u.object != id || !d.Owner.Equal(owner) || u.creation != d.CreationCause || grant.Details().CreationCause != u.creation {
				return failure(foundation.Forbidden, nil)
			}
			if u.disposition != "reserved" || u.state != "committed" {
				return failure(foundation.InvalidState, nil)
			}
			obj, found, err = loadObject(ctx, e, id)
			if err != nil {
				return err
			}
			if !found || !objectPartition(obj, owner) {
				return failure(foundation.Forbidden, nil)
			}
			if obj.cleaning || obj.meta.State != oc.Available {
				return deleted(false)
			}
			var reserved bool
			err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_references WHERE object_id=$1 AND upload_id=$2 AND owner_kind=$3 AND owner_id=$4 AND kind='reserved')`, id.String(), u.id.String(), string(owner.Details().Kind), owner.Details().ID).Scan(&reserved)
			if err != nil {
				return unavailable(err)
			}
			if !reserved {
				return failure(foundation.Forbidden, nil)
			}
		} else {
			obj, err = s.readRow(ctx, tx, actor, owner, id)
			if err != nil {
				return err
			}
		}
		if requested != nil {
			r, err := requested.Resolve(int64(obj.meta.ByteSize))
			if err != nil {
				return err
			}
			resolved = &r
		}
		_, err = e.Exec(ctx, `INSERT INTO agenteam_object.object_leases(id,object_id,owner_kind,owner_id,process_id,state) VALUES($1,$2,$3,$1,$4,'active')`, leaseID.String(), id.String(), string(kind), s.state().process.String())
		return unavailableIf(err)
	})
	if err = commitError(result); err != nil {
		if result.State() == foundation.Unknown {
			_ = s.releaseInternal(leaseID, id)
		}
		return nil, err
	}
	body, err := s.state().backend.get(op.ctx, obj.key, int64(obj.meta.ByteSize), resolved)
	if err != nil {
		_ = s.releaseInternal(leaseID, id)
		return nil, err
	}
	size, digest := int64(obj.meta.ByteSize), obj.meta.SHA256
	if resolved != nil {
		size = int64(resolved.Length)
		digest = ""
	}
	reader, err := newIntegrityReader(op.ctx, body, size, digest, func() error { err := s.releaseInternal(leaseID, id); finish(); return err })
	if err != nil {
		return nil, err
	}
	resultReader, err := oc.NewObjectReader(obj.meta, resolved, reader)
	if err != nil {
		_ = reader.Close()
		return nil, err
	}
	keep = true
	return resultReader, nil
}
func (s *Service) releaseInternal(id oc.LeaseID, object oc.ObjectID) error {
	ctx, cancel := s.cleanupContext()
	defer cancel()
	return s.releaseInternalContext(ctx, id, object)
}

// A caller which starts an asynchronous release registers cleanup before
// launching it, and keeps that registration until its real join completes.
// The supplied context is the one shared cleanup/force budget, not a fresh one.
func (s *Service) releaseInternalContext(ctx context.Context, id oc.LeaseID, object oc.ObjectID) error {
	r := s.state()
	r.mu.Lock()
	r.closedLeases[id] = object
	r.mu.Unlock()
	result := s.withinAccess(ctx, recoveryCause(), s.maintenanceRequest(oc.ReleaseReaderAccess, object, oc.AccessRequestDetails{LeaseID: id}), func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		_, err = e.Exec(ctx, `UPDATE agenteam_object.object_leases SET state='released',released_at=clock_timestamp() WHERE id=$1 AND object_id=$2 AND process_id=$3 AND owner_kind IN ('reader','source') AND state='active'`, id.String(), object.String(), r.process.String())
		return unavailableIf(err)
	})
	err := commitError(result)
	if err == nil {
		r.mu.Lock()
		delete(r.closedLeases, id)
		r.mu.Unlock()
	}
	return err
}

func (s *Service) AcquireLeaseInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, id oc.ObjectID, owner oc.LeaseOwner, plan oc.AccessLockPlan, locked oc.LockedAccess) (oc.ObjectLease, error) {
	if err := s.metadataAdmission(ctx); err != nil {
		return oc.ObjectLease{}, err
	}
	if actor.Validate() != nil || id.Validate() != nil || owner.Validate() != nil {
		return oc.ObjectLease{}, invalid()
	}
	kind := owner.Details().Kind
	if kind != oc.ExecutionOwner && kind != oc.HistoryOwner && kind != oc.TransferOwner {
		return oc.ObjectLease{}, failure(foundation.Forbidden, nil)
	}
	if nilPort(s.state().auth.Leases) {
		return oc.ObjectLease{}, failure(foundation.DependencyUnbound, nil)
	}
	request, _ := oc.NewLeaseAccess(oc.AccessRequestDetails{Operation: oc.AcquireUseAccess, Actor: actor, ObjectID: id, LeaseOwner: owner})
	if err := s.ValidateAccessPlanInTx(ctx, tx, request, plan, locked); err != nil {
		return oc.ObjectLease{}, err
	}
	if err := s.state().auth.Leases.AuthorizeLeaseInTx(ctx, tx, actor, id, owner, oc.AcquireLease); err != nil {
		return oc.ObjectLease{}, portError(err)
	}
	e, err := executor(s, tx)
	if err != nil {
		return oc.ObjectLease{}, err
	}
	obj, found, err := loadObject(ctx, e, id)
	if err != nil {
		return oc.ObjectLease{}, err
	}
	if !found {
		return oc.ObjectLease{}, failure(foundation.NotFound, nil)
	}
	if obj.cleaning || obj.meta.State == oc.Deleted {
		return oc.ObjectLease{}, deleted(false)
	}
	if obj.meta.State != oc.Available {
		return oc.ObjectLease{}, failure(foundation.InvalidState, nil)
	}
	leaseID, err := foundation.NewID[oc.Lease]()
	if err != nil {
		return oc.ObjectLease{}, unavailable(err)
	}
	var stored, state string
	err = e.QueryRow(ctx, `SELECT id::text,state FROM agenteam_object.object_leases WHERE object_id=$1 AND owner_kind=$2 AND owner_id=$3`, id.String(), string(kind), owner.Details().ID).Scan(&stored, &state)
	if err == nil {
		if state != "active" {
			return oc.ObjectLease{}, failure(foundation.InvalidState, nil)
		}
		leaseID, err = foundation.ParseID[oc.Lease](stored)
		if err != nil {
			return oc.ObjectLease{}, unavailable(err)
		}
		return oc.ObjectLease{ID: leaseID, ObjectID: id, Owner: owner}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return oc.ObjectLease{}, unavailable(err)
	}
	_, err = e.Exec(ctx, `INSERT INTO agenteam_object.object_leases(id,object_id,owner_kind,owner_id,state) VALUES($1,$2,$3,$4,'active')`, leaseID.String(), id.String(), string(kind), owner.Details().ID)
	if err != nil {
		return oc.ObjectLease{}, unavailable(err)
	}
	return oc.ObjectLease{ID: leaseID, ObjectID: id, Owner: owner}, nil
}
func (s *Service) ReleaseLeaseInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, id oc.ObjectID, owner oc.LeaseOwner, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
	if actor.Validate() != nil || id.Validate() != nil || owner.Validate() != nil {
		return invalid()
	}
	kind := owner.Details().Kind
	if kind != oc.ExecutionOwner && kind != oc.HistoryOwner && kind != oc.TransferOwner {
		return failure(foundation.Forbidden, nil)
	}
	if nilPort(s.state().auth.Leases) {
		return failure(foundation.DependencyUnbound, nil)
	}
	request, _ := oc.NewLeaseAccess(oc.AccessRequestDetails{Operation: oc.ReleaseUseAccess, Actor: actor, ObjectID: id, LeaseOwner: owner})
	if err := s.ValidateAccessPlanInTx(ctx, tx, request, plan, locked); err != nil {
		return err
	}
	if err := s.state().auth.Leases.AuthorizeLeaseInTx(ctx, tx, actor, id, owner, oc.ReleaseLease); err != nil {
		return portError(err)
	}
	e, err := executor(s, tx)
	if err != nil {
		return err
	}
	_, err = e.Exec(ctx, `UPDATE agenteam_object.object_leases SET state='released',released_at=clock_timestamp() WHERE object_id=$1 AND owner_kind=$2 AND owner_id=$3 AND state='active'`, id.String(), string(kind), owner.Details().ID)
	return unavailableIf(err)
}
