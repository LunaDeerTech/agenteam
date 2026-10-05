package object

import (
	"bytes"
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func (t *TransferService) CompleteTransfer(ctx context.Context, actor identity.Actor, id oc.TransferID, evidence oc.TransferEvidenceRef) (oc.TransferStatusView, error) {
	if actor.Validate() != nil || id.Validate() != nil || evidence.Validate() != nil || evidence.Kind != oc.TransferCompletedEvidence {
		return oc.TransferStatusView{}, invalid()
	}
	if nilPort(t.state().authority) {
		return oc.TransferStatusView{}, failure(foundation.DependencyUnbound, nil)
	}
	s := t.state().objects
	op, done, err := s.begin(ctx)
	if err != nil {
		return oc.TransferStatusView{}, err
	}
	defer done()
	ctx = op.ctx
	r, ok, err := loadTransfer(ctx, s.state().store, id)
	if err != nil {
		return oc.TransferStatusView{}, err
	}
	if !ok {
		return oc.TransferStatusView{}, failure(foundation.NotFound, nil)
	}
	kind := "transfer_get"
	if r.spec.Details().Direction == oc.TransferPUT {
		kind = "transfer_put"
	}
	work, err := s.newProjectWork(ctx, workProject(r.spec.Details().Owner), kind, r.id.String(), r.object)
	if err != nil {
		return oc.TransferStatusView{}, err
	}
	ctx = context.WithValue(ctx, projectWorkContextKey{}, work)
	request, err := r.request(actor, oc.TransferCapture, &evidence, nil, oc.PreparedPayload{})
	if err != nil {
		return oc.TransferStatusView{}, err
	}
	var already, needRead bool
	result := t.within(ctx, request, nil, func(ctx context.Context, tx foundation.Tx, plans []oc.AccessLockPlan, locked oc.LockedAccess, a oc.TransferAuthorization) error {
		if r.stable != stableActor(actor) {
			return failure(foundation.Forbidden, nil)
		}
		proof := a.Details().Completed
		if proof == nil {
			return failure(foundation.Forbidden, nil)
		}
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		current, exists, err := loadTransfer(ctx, e, id)
		if err != nil {
			return err
		}
		if !exists {
			return failure(foundation.NotFound, nil)
		}
		r = current
		if r.completed.Validate() == nil && (r.completed != proof.Evidence.ID || !bytes.Equal(r.completedDigest, digestBytes(proof.Digest))) {
			return failure(foundation.IdempotencyKeyReused, nil)
		}
		if r.phase == "complete" {
			already = true
			return nil
		}
		if r.revoked || r.phase == "failed" {
			return failure(foundation.InvalidState, nil)
		}
		// within has checked current owner/Project authority before any replay.
		// GET terminal bookkeeping uses Converge; only a new PUT uses Mutate.
		if r.spec.Details().Direction == oc.TransferGET {
			_, err = e.Exec(ctx, `UPDATE agenteam_object.object_transfers SET phase='complete',version=version+1,completed_evidence=$2,completed_digest=$3 WHERE id=$1`, id.String(), evidence.ID.String(), digestBytes(proof.Digest))
			if err != nil {
				return unavailable(err)
			}
			if err = t.appendAudit(ctx, tx, r, ac.ObjectTransferComplete); err != nil {
				return err
			}
			already = true
			return nil
		}
		if r.candidate.Validate() == nil {
			candidate, exists, err := loadAttempt(ctx, e, r.candidate)
			if err != nil {
				return err
			}
			if exists && candidate.kind == "private_candidate" && (candidate.phase == "verified" || candidate.phase == "published") {
				return nil
			}
			if exists && candidate.phase != "cleaned" {
				return failure(foundation.ResourceBusy, nil)
			}
		}
		if r.sourceLease.Validate() == nil {
			var active bool
			if err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_leases WHERE id=$1 AND state='active')`, r.sourceLease.String()).Scan(&active); err != nil {
				return unavailable(err)
			}
			if active {
				return failure(foundation.ResourceBusy, nil)
			}
		}
		stage, exists, err := loadAttempt(ctx, e, r.staging)
		if err != nil {
			return err
		}
		if !exists || stage.kind != "runner_staging" || stage.transfer != r.id || stage.cleaning {
			return failure(foundation.InvalidState, nil)
		}
		raw, err := s.plannedWorkID(plans[0])
		if err != nil {
			return err
		}
		lease, err := foundation.ParseID[oc.Lease](raw)
		if err != nil {
			return unavailable(err)
		}
		_, err = e.Exec(ctx, `INSERT INTO agenteam_object.object_leases(id,object_id,owner_kind,owner_id,process_id,state) VALUES($1,$2,'source',$1,$3,'active')`, lease.String(), r.object.String(), s.state().process.String())
		if err != nil {
			return unavailable(err)
		}
		_, err = e.Exec(ctx, `UPDATE agenteam_object.object_transfers SET phase='completing',version=version+1,completed_evidence=$2,completed_digest=$3,source_lease_id=$4,source_process_id=$5 WHERE id=$1`, id.String(), evidence.ID.String(), digestBytes(proof.Digest), lease.String(), s.state().process.String())
		if err != nil {
			return unavailable(err)
		}
		h := s.projectWorkHandle(ctx, projectWork{id: raw, project: workProject(r.spec.Details().Owner), process: s.state().process, kind: "source", resource: raw, object: r.object})
		if err = s.registerProjectWork(ctx, tx, h, locked.Locks()); err != nil {
			return err
		}
		r.sourceLease = lease
		r.sourceProcess = s.state().process
		needRead = true
		return nil
	})
	if err = commitError(result); err != nil {
		// No GET occurred. Preserve an exact local join checkpoint even when the
		// source-lease reservation COMMIT response was lost.
		if needRead {
			_ = s.releaseInternal(r.sourceLease, r.object)
		}
		return r.status(), err
	}
	r, ok, err = loadTransfer(ctx, s.state().store, id)
	if err != nil || !ok {
		return oc.TransferStatusView{}, unavailable(err)
	}
	if already {
		return r.status(), nil
	}
	var p oc.PreparedPayload
	if needRead {
		stage, _ := oc.NewUploadAttempt(oc.AttemptDetails{ID: r.staging, UploadID: r.upload, ObjectID: r.object})
		p, err = t.prepareTransferPUT(ctx, transferBinding{row: r, actor: actor}, stage)
		if err != nil {
			return r.status(), err
		}
		defer s.DiscardPrepared(p)
		// Recollect after the confirmed capture mutation; the operation holds no
		// database transaction while the staging stream is read or spooled.
		r, ok, err = loadTransfer(ctx, s.state().store, id)
		if err != nil || !ok {
			return r.status(), unavailable(err)
		}
		request, err = r.request(actor, oc.TransferReserveCandidate, &evidence, nil, p)
		if err != nil {
			return r.status(), err
		}
		var candidate oc.UploadAttempt
		result = t.within(ctx, request, nil, func(ctx context.Context, tx foundation.Tx, plans []oc.AccessLockPlan, locked oc.LockedAccess, _ oc.TransferAuthorization) error {
			e, err := executor(s, tx)
			if err != nil {
				return err
			}
			current, exists, err := loadTransfer(ctx, e, id)
			if err != nil {
				return err
			}
			if !exists || current.revoked || current.phase == "failed" {
				return failure(foundation.InvalidState, nil)
			}
			candidate, err = t.reserveTransferCandidateInTx(ctx, tx, transferBinding{row: r, actor: actor}, stage, p, plans[0], locked, evidence)
			if err != nil {
				return err
			}
			_, err = e.Exec(ctx, `UPDATE agenteam_object.object_transfers SET candidate_id=$2,version=version+1 WHERE id=$1`, id.String(), candidate.Details().ID.String())
			return unavailableIf(err)
		})
		if err = commitError(result); err != nil {
			if result.State() == foundation.Unknown && candidate.Validate() == nil {
				state := s.state()
				state.mu.Lock()
				t.state().pendingJoins[candidate.Details().ID] = candidate
				state.mu.Unlock()
				cleanup, finished := s.cleanupContext()
				_ = t.joinUnsentCandidate(cleanup, candidate)
				finished()
			}
			return r.status(), err
		}
		if _, err = s.UploadPrepared(ctx, actor, r.spec.Details().Owner, p, candidate); err != nil {
			return r.status(), err
		}
		r, ok, err = loadTransfer(ctx, s.state().store, id)
		if err != nil || !ok {
			return r.status(), unavailable(err)
		}
	}
	request, err = r.request(actor, oc.TransferPublish, &evidence, nil, oc.PreparedPayload{})
	if err != nil {
		return r.status(), err
	}
	candidate, _ := oc.NewUploadAttempt(oc.AttemptDetails{ID: r.candidate, UploadID: r.upload, ObjectID: r.object})
	publish := ownerRequest(actor, r.spec.Details().Owner, oc.PublishAccess, oc.AccessRequestDetails{Attempt: candidate})
	result = t.within(ctx, request, []oc.AccessRequest{publish}, func(ctx context.Context, tx foundation.Tx, plans []oc.AccessLockPlan, locked oc.LockedAccess, a oc.TransferAuthorization) error {
		proof := a.Details().Completed
		if proof == nil {
			return failure(foundation.Forbidden, nil)
		}
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		current, exists, err := loadTransfer(ctx, e, id)
		if err != nil {
			return err
		}
		if !exists || current.revoked || current.phase == "failed" {
			return failure(foundation.InvalidState, nil)
		}
		if current.completed != evidence.ID || !bytes.Equal(current.completedDigest, digestBytes(proof.Digest)) {
			return failure(foundation.IdempotencyKeyReused, nil)
		}
		if _, err = s.PublishVerifiedInTx(ctx, tx, actor, r.spec.Details().Owner, candidate, plans[1], locked); err != nil {
			return err
		}
		_, err = e.Exec(ctx, `UPDATE agenteam_object.object_transfers SET phase='complete',version=version+1,cleanup_gate=true WHERE id=$1`, id.String())
		if err != nil {
			return unavailable(err)
		}
		stage, exists, err := loadAttempt(ctx, e, r.staging)
		if err != nil {
			return err
		}
		if !exists {
			return unavailable(nil)
		}
		if err = s.gateAttempt(ctx, e, stage, oc.AbandonedAttempt, r.id.String()); err != nil {
			return err
		}
		return t.appendAudit(ctx, tx, r, ac.ObjectTransferComplete)
	})
	if err = commitError(result); err != nil {
		return r.status(), err
	}
	// Marker convergence is independent of the successful business publication.
	// Failure keeps the durable checkpoint and the external lease intact.
	_, _ = s.cleanObject(ctx, r.object)
	r, ok, err = loadTransfer(ctx, s.state().store, id)
	if err != nil || !ok {
		return oc.TransferStatusView{}, unavailable(err)
	}
	return r.status(), nil
}

// A candidate reservation may have committed even though no PUT was called.
// Lock its original upload command before deciding an absent row is terminal;
// an unlocked miss must never discard the local join checkpoint.
func (t *TransferService) joinUnsentCandidate(ctx context.Context, attempt oc.UploadAttempt) error {
	s := t.state().objects
	d := attempt.Details()
	request := s.maintenanceRequest(oc.JoinAttemptAccess, d.ObjectID, oc.AccessRequestDetails{AttemptID: d.ID, ProcessID: s.state().process})
	result := s.withinAccess(ctx, recoveryCause(), request, func(ctx context.Context, tx foundation.Tx, _ oc.AccessLockPlan, _ oc.LockedAccess) error {
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		current, found, err := loadAttempt(ctx, e, d.ID)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		if current.kind != "private_candidate" || current.process != s.state().process || current.object != d.ObjectID || current.upload != d.UploadID {
			return failure(foundation.Forbidden, nil)
		}
		if current.phase != "reserved" && current.phase != "unknown" {
			return failure(foundation.InvalidState, nil)
		}
		_, err = e.Exec(ctx, `UPDATE agenteam_object.upload_attempts SET io_closed=true,phase='unknown' WHERE id=$1`, d.ID.String())
		if err != nil {
			return unavailable(err)
		}
		_, err = e.Exec(ctx, `UPDATE agenteam_object.object_leases SET state='released',released_at=clock_timestamp() WHERE attempt_id=$1 AND process_id=$2 AND owner_kind='writer' AND state='active'`, d.ID.String(), s.state().process.String())
		return unavailableIf(err)
	})
	err := commitError(result)
	if err == nil {
		s.state().mu.Lock()
		delete(t.state().pendingJoins, d.ID)
		s.state().mu.Unlock()
	}
	return err
}

func (t *TransferService) prepareTransferPUT(ctx context.Context, b transferBinding, staging oc.UploadAttempt) (oc.PreparedPayload, error) {
	r := b.row
	s := t.state().objects
	m := r.manifest.Details()
	if staging.Details().ID != r.staging || r.sourceProcess != s.state().process || r.sourceLease.Validate() != nil {
		return oc.PreparedPayload{}, invalid()
	}
	// The source lease protects this exact staging body through Close/join. A
	// second GET is never used to create the candidate after validation.
	body, err := s.state().backend.get(ctx, "staging/"+r.staging.String(), m.Length, nil)
	if err != nil {
		_ = s.releaseInternal(r.sourceLease, r.object)
		return oc.PreparedPayload{}, err
	}
	p, prepareErr := s.PreparePayload(ctx, b.actor, r.spec.Details().Owner, m.MediaType, m.Length, &m.SHA256, body)
	closeErr := body.Close()
	releaseErr := s.releaseInternal(r.sourceLease, r.object)
	if prepareErr != nil {
		// Here the declaration came from the trusted, persisted Runner output
		// manifest. A short/long body or different SHA is storage corruption,
		// unlike a caller's invalid declaration at the ordinary upload boundary.
		if hasCode(prepareErr, foundation.InvalidArgument) {
			return oc.PreparedPayload{}, failure(foundation.ObjectIntegrityMismatch, prepareErr)
		}
		return oc.PreparedPayload{}, prepareErr
	}
	if closeErr != nil || releaseErr != nil {
		_ = s.DiscardPrepared(p)
		if releaseErr != nil {
			return oc.PreparedPayload{}, releaseErr
		}
		return oc.PreparedPayload{}, closeErr
	}
	return p, nil
}
