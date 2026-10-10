package object

import (
	"bytes"
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// These helpers consume an already validated full plan. They do not acquire
// locks, open transactions or turn declared metadata into a sealed payload.
func (s *Service) reserveObjectCommand(ctx context.Context, e postgres.SQLExecutor, actor identity.Actor, owner oc.ObjectOwner, meta foundation.CommandMeta, d oc.PreparedDetails, object oc.ObjectID, upload oc.UploadID, receipt oc.ReceiptID, semantic []byte, grant oc.OwnerAuthorization) (uploadRow, error) {
	od := owner.Details()
	a := actor.Details()
	initiator := a.UserID
	if a.Kind == identity.AgentRun {
		initiator = a.AgentID
	}
	if a.Kind == identity.Service {
		// The CreationID is the durable initiator, not an empty UserID or an
		// Object service cause. The current domain gate was checked by the
		// supplied full plan; recheck the narrow grant before any SQL here.
		_, creationErr := foundation.ParseID[struct{}](a.CauseRef)
		if a.ServiceName != identity.ProjectInitialization || od.Kind != oc.SkillRevision ||
			a.ProjectID != od.ProjectID || creationErr != nil ||
			!grant.Matches(actor, owner, identity.Mutate) || grant.Details().CreationCause != a.CauseRef {
			return uploadRow{}, failure(foundation.Forbidden, nil)
		}
		initiator = a.CauseRef
	}
	command, err := commandIdentity(owner, meta.IdempotencyKey)
	if err != nil {
		return uploadRow{}, err
	}
	_, err = e.Exec(ctx, `INSERT INTO agenteam_object.objects(id,scope,partition_id,project_id,media_type,byte_size,sha256,state) VALUES($1,$2,$3,$4,$5,$6,$7,'pending')`, object.String(), string(owner.Scope().Details().Kind), owner.Partition(), null(od.ProjectID), d.MediaType, d.Length, digestBytes(d.SHA256))
	if err != nil {
		return uploadRow{}, unavailable(err)
	}
	_, err = e.Exec(ctx, `INSERT INTO agenteam_object.uploads(id,object_id,command_hash,command_key,semantic_digest,expected_version,owner_kind,owner_id,project_id,stable_actor,initiator_kind,initiator_id,initiator_execution_id,existence,creation_cause,state,disposition,receipt_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,'pending','reserved',$16)`, upload.String(), object.String(), commandHash(command), string(meta.IdempotencyKey), semantic, optionalVersion(meta.ExpectedVersion), string(od.Kind), od.ID, null(od.ProjectID), stableActor(actor), string(a.Kind), initiator, null(a.ExecutionID), string(grant.Details().Existence), null(grant.Details().CreationCause), receipt.String())
	if err != nil {
		return uploadRow{}, unavailable(err)
	}
	_, err = e.Exec(ctx, `INSERT INTO agenteam_object.object_references(object_id,owner_kind,owner_id,partition_id,kind,upload_id) VALUES($1,$2,$3,$4,'reserved',$5)`, object.String(), string(od.Kind), od.ID, owner.Partition(), upload.String())
	if err != nil {
		return uploadRow{}, unavailable(err)
	}
	u, found, err := loadCommand(ctx, e, command)
	if err != nil {
		return u, err
	}
	if !found {
		return u, unavailable(nil)
	}
	return u, nil
}
func (s *Service) newPrivateCandidate(ctx context.Context, tx foundation.Tx, e postgres.SQLExecutor, u uploadRow, p oc.PreparedPayload, plan oc.AccessLockPlan, locked oc.LockedAccess) (oc.UploadAttempt, error) {
	raw, err := s.plannedWorkID(plan)
	if err != nil {
		return oc.UploadAttempt{}, err
	}
	id, err := foundation.ParseID[oc.Attempt](raw)
	if err != nil {
		return oc.UploadAttempt{}, unavailable(err)
	}
	lease, err := foundation.NewID[oc.Lease]()
	if err != nil {
		return oc.UploadAttempt{}, unavailable(err)
	}
	var ordinal int64
	if err = e.QueryRow(ctx, `SELECT coalesce(max(ordinal),0)+1 FROM agenteam_object.upload_attempts WHERE upload_id=$1`, u.id.String()).Scan(&ordinal); err != nil {
		return oc.UploadAttempt{}, unavailable(err)
	}
	d := p.Details()
	_, err = e.Exec(ctx, `INSERT INTO agenteam_object.upload_attempts(id,upload_id,object_id,ordinal,candidate_key,phase,process_id,byte_size,sha256,spool_payload_id) VALUES($1,$2,$3,$4,$5,'reserved',$6,$7,$8,$9)`, id.String(), u.id.String(), u.object.String(), ordinal, "candidate/"+id.String(), s.state().process.String(), d.Length, digestBytes(d.SHA256), d.ID.String())
	if err != nil {
		return oc.UploadAttempt{}, unavailable(err)
	}
	_, err = e.Exec(ctx, `INSERT INTO agenteam_object.object_leases(id,object_id,attempt_id,owner_kind,owner_id,process_id,state) VALUES($1,$2,$3,'writer',$3,$4,'active')`, lease.String(), u.object.String(), id.String(), s.state().process.String())
	if err != nil {
		return oc.UploadAttempt{}, unavailable(err)
	}
	_, err = e.Exec(ctx, `UPDATE agenteam_object.uploads SET current_attempt_id=$1,state='pending' WHERE id=$2`, id.String(), u.id.String())
	if err != nil {
		return oc.UploadAttempt{}, unavailable(err)
	}

	if project := workProject(u.owner); project.Validate() == nil {
		r := s.state()
		r.mu.Lock()
		entry := r.prepared[p.Details().ID]
		r.mu.Unlock()
		workCtx := ctx
		if entry != nil {
			workCtx = entry.operation.ctx
		}
		h := s.projectWorkHandle(workCtx, projectWork{id: raw, project: project, process: r.process, kind: "preparation", resource: raw, object: u.object})
		if err = s.registerProjectWork(ctx, tx, h, locked.Locks()); err != nil {
			return oc.UploadAttempt{}, err
		}
	}
	return oc.NewUploadAttempt(oc.AttemptDetails{ID: id, UploadID: u.id, ObjectID: u.object})
}

type transferBinding struct {
	row   transferRow
	actor identity.Actor
}

func (t *TransferService) reserveTransferPUTInTx(ctx context.Context, tx foundation.Tx, b transferBinding, manifest oc.TransferManifest, plan oc.AccessLockPlan, locked oc.LockedAccess) (oc.UploadAttempt, error) {
	r := b.row
	s := t.state().objects
	request, err := r.request(b.actor, oc.TransferIssue, nil, nil, oc.PreparedPayload{})
	if err != nil {
		return oc.UploadAttempt{}, err
	}
	if _, err = t.authorize(ctx, tx, request, plan, locked); err != nil {
		return oc.UploadAttempt{}, err
	}
	d := r.spec.Details()
	if d.Direction != oc.TransferPUT || !manifest.Equal(d.Manifest) {
		return oc.UploadAttempt{}, invalid()
	}
	grant, err := s.authorize(ctx, tx, b.actor, d.Owner, identity.Mutate)
	if err != nil {
		return oc.UploadAttempt{}, err
	}
	if err = s.gate(ctx, tx, b.actor, d.Owner, identity.Mutate); err != nil {
		return oc.UploadAttempt{}, err
	}
	if d.UploadCommand.ExpectedVersion != nil && *d.UploadCommand.ExpectedVersion != grant.Details().Version {
		return oc.UploadAttempt{}, failure(foundation.VersionConflict, nil)
	}
	e, err := executor(s, tx)
	if err != nil {
		return oc.UploadAttempt{}, err
	}
	c, err := commandIdentity(d.Owner, d.UploadCommand.IdempotencyKey)
	if err != nil {
		return oc.UploadAttempt{}, err
	}
	u, exists, err := loadCommand(ctx, e, c)
	if err != nil {
		return oc.UploadAttempt{}, err
	}
	m := manifest.Details()
	semantic, err := contentSemanticDigest(b.actor, d.Owner, *d.UploadCommand, m.MediaType, m.Length, m.SHA256)
	if err != nil {
		return oc.UploadAttempt{}, err
	}
	if exists {
		if u.object != r.object || u.id != r.upload {
			return oc.UploadAttempt{}, accessChanged()
		}
		if err = checkOriginal(u, b.actor, d.Owner); err != nil {
			return oc.UploadAttempt{}, err
		}
		if !bytes.Equal(u.digest, semantic) {
			return oc.UploadAttempt{}, failure(foundation.IdempotencyKeyReused, nil)
		}
		if u.state == "committed" || u.disposition == "revoked" {
			return oc.UploadAttempt{}, failure(foundation.InvalidState, nil)
		}
		obj, ok, err := loadObject(ctx, e, u.object)
		if err != nil {
			return oc.UploadAttempt{}, err
		}
		if !ok || obj.cleaning {
			return oc.UploadAttempt{}, deleted(false)
		}
		if u.existence == oc.ProspectiveOwner && u.creation != grant.Details().CreationCause {
			return oc.UploadAttempt{}, failure(foundation.Forbidden, nil)
		}
		var live bool
		if err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_transfers t JOIN agenteam_object.object_leases l ON l.id=t.lease_id WHERE t.upload_id=$1 AND l.state='active')`, u.id.String()).Scan(&live); err != nil {
			return oc.UploadAttempt{}, unavailable(err)
		}
		if live {
			return oc.UploadAttempt{}, failure(foundation.ResourceBusy, nil)
		}
	} else {
		receipt, err := foundation.NewID[oc.Receipt]()
		if err != nil {
			return oc.UploadAttempt{}, unavailable(err)
		}
		u, err = s.reserveObjectCommand(ctx, e, b.actor, d.Owner, *d.UploadCommand, oc.PreparedDetails{MediaType: m.MediaType, Length: m.Length, SHA256: m.SHA256}, r.object, r.upload, receipt, semantic, grant)
		if err != nil {
			return oc.UploadAttempt{}, err
		}
	}
	if err = checkAttemptQuota(ctx, e, u.id); err != nil {
		return oc.UploadAttempt{}, err
	}
	if nilPort(s.state().auth.Leases) {
		return oc.UploadAttempt{}, failure(foundation.DependencyUnbound, nil)
	}
	leaseOwner, _ := oc.NewLeaseOwner(oc.TransferOwner, r.id.String())
	if err = s.state().auth.Leases.AuthorizeLeaseInTx(ctx, tx, b.actor, r.object, leaseOwner, oc.AcquireLease); err != nil {
		return oc.UploadAttempt{}, portError(err)
	}
	var ordinal int64
	if err = e.QueryRow(ctx, `SELECT coalesce(max(ordinal),0)+1 FROM agenteam_object.upload_attempts WHERE upload_id=$1`, u.id.String()).Scan(&ordinal); err != nil {
		return oc.UploadAttempt{}, unavailable(err)
	}
	_, err = e.Exec(ctx, `INSERT INTO agenteam_object.upload_attempts(id,upload_id,object_id,ordinal,candidate_key,phase,process_id,spool_payload_id,byte_size,sha256,kind,transfer_id,maybe_late) VALUES($1,$2,$3,$4,$5,'reserved',NULL,NULL,$6,$7,'runner_staging',$8,true)`, r.staging.String(), u.id.String(), u.object.String(), ordinal, "staging/"+r.staging.String(), m.Length, digestBytes(m.SHA256), r.id.String())
	if err != nil {
		return oc.UploadAttempt{}, unavailable(err)
	}
	_, err = e.Exec(ctx, `INSERT INTO agenteam_object.object_leases(id,object_id,owner_kind,owner_id,state) VALUES($1,$2,'transfer',$3,'active')`, r.lease.String(), r.object.String(), r.id.String())
	if err != nil {
		return oc.UploadAttempt{}, unavailable(err)
	}
	_, err = e.Exec(ctx, `UPDATE agenteam_object.uploads SET current_attempt_id=$1,state='pending' WHERE id=$2`, r.staging.String(), u.id.String())
	if err != nil {
		return oc.UploadAttempt{}, unavailable(err)
	}
	return oc.NewUploadAttempt(oc.AttemptDetails{ID: r.staging, UploadID: u.id, ObjectID: u.object})
}
func checkAttemptQuota(ctx context.Context, e postgres.SQLExecutor, upload oc.UploadID) error {
	var global, local int64
	if err := e.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE upload_id=$1) FROM agenteam_object.upload_attempts WHERE phase NOT IN ('published','cleaned')`, upload.String()).Scan(&global, &local); err != nil {
		return unavailable(err)
	}
	if global >= 64 || local >= 2 {
		return failure(foundation.ResourceBusy, nil)
	}
	return nil
}
func (t *TransferService) reserveTransferCandidateInTx(ctx context.Context, tx foundation.Tx, b transferBinding, staging oc.UploadAttempt, p oc.PreparedPayload, plan oc.AccessLockPlan, locked oc.LockedAccess, evidence oc.TransferEvidenceRef) (oc.UploadAttempt, error) {
	s := t.state().objects
	r := b.row
	request, err := r.request(b.actor, oc.TransferReserveCandidate, &evidence, nil, p)
	if err != nil {
		return oc.UploadAttempt{}, err
	}
	auth, err := t.authorize(ctx, tx, request, plan, locked)
	if err != nil {
		return oc.UploadAttempt{}, err
	}
	if auth.Details().Completed == nil {
		return oc.UploadAttempt{}, failure(foundation.Forbidden, nil)
	}
	if _, err = s.prepared(p, b.actor, r.spec.Details().Owner); err != nil {
		return oc.UploadAttempt{}, err
	}
	m := r.manifest.Details()
	pd := p.Details()
	if pd.MediaType != m.MediaType || pd.Length != m.Length || pd.SHA256 != m.SHA256 || staging.Details().ID != r.staging {
		return oc.UploadAttempt{}, failure(foundation.ObjectIntegrityMismatch, nil)
	}
	e, err := executor(s, tx)
	if err != nil {
		return oc.UploadAttempt{}, err
	}
	u, ok, err := loadUpload(ctx, e, r.upload)
	if err != nil {
		return oc.UploadAttempt{}, err
	}
	if !ok || u.object != r.object || u.disposition == "revoked" {
		return oc.UploadAttempt{}, failure(foundation.InvalidState, nil)
	}
	if r.candidate.Validate() == nil {
		prior, ok, err := loadAttempt(ctx, e, r.candidate)
		if err != nil {
			return oc.UploadAttempt{}, err
		}
		if ok && prior.kind == "private_candidate" && (prior.phase == "verified" || prior.phase == "published") {
			return attemptOf(prior), nil
		}
		if ok && prior.phase != "cleaned" {
			return oc.UploadAttempt{}, failure(foundation.ResourceBusy, nil)
		}
	}
	if _, err = s.authorize(ctx, tx, b.actor, r.spec.Details().Owner, identity.Mutate); err != nil {
		return oc.UploadAttempt{}, err
	}
	if err = s.gate(ctx, tx, b.actor, r.spec.Details().Owner, identity.Mutate); err != nil {
		return oc.UploadAttempt{}, err
	}
	if err = checkAttemptQuota(ctx, e, u.id); err != nil {
		return oc.UploadAttempt{}, err
	}
	return s.newPrivateCandidate(ctx, tx, e, u, p, plan, locked)
}
