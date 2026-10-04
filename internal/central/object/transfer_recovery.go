package object

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func (t *TransferService) CancelTransfer(ctx context.Context, actor identity.Actor, id oc.TransferID, command foundation.CommandMeta) (oc.TransferStatusView, error) {
	if actor.Validate() != nil || id.Validate() != nil || command.Validate() != nil {
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
	request, err := r.request(actor, oc.TransferCancel, nil, &command, oc.PreparedPayload{})
	if err != nil {
		return r.status(), err
	}
	expected := ""
	if command.ExpectedVersion != nil {
		expected = command.ExpectedVersion.String()
	}
	raw, err := json.Marshal([]string{stableActor(actor), id.String(), expected})
	if err != nil {
		return r.status(), invalid()
	}
	semantic := sha256.Sum256(raw)
	result := t.within(ctx, request, nil, func(ctx context.Context, tx foundation.Tx, _ []oc.AccessLockPlan, _ oc.LockedAccess, _ oc.TransferAuthorization) error {
		if stableActor(actor) != r.stable {
			return failure(foundation.Forbidden, nil)
		}
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		var priorKey *string
		var prior []byte
		if err = e.QueryRow(ctx, `SELECT cancel_key,cancel_digest FROM agenteam_object.object_transfers WHERE id=$1`, id.String()).Scan(&priorKey, &prior); err != nil {
			return unavailable(err)
		}
		if priorKey != nil {
			if *priorKey != string(command.IdempotencyKey) || !bytes.Equal(prior, semantic[:]) {
				return failure(foundation.IdempotencyKeyReused, nil)
			}
			return nil
		}
		if command.ExpectedVersion != nil && *command.ExpectedVersion != r.version {
			return failure(foundation.VersionConflict, nil)
		}
		if _, err = s.authorize(ctx, tx, actor, r.spec.Details().Owner, identity.Converge); err != nil {
			return err
		}
		if err = s.gate(ctx, tx, actor, r.spec.Details().Owner, identity.Converge); err != nil {
			return err
		}
		if err = t.gateTransfer(ctx, e, r); err != nil {
			return err
		}
		_, err = e.Exec(ctx, `UPDATE agenteam_object.object_transfers SET revoked_at=coalesce(revoked_at,clock_timestamp()),phase=CASE WHEN phase='complete' THEN phase ELSE 'failed' END,version=version+1,cancel_key=$2,cancel_digest=$3,cleanup_gate=true WHERE id=$1`, id.String(), string(command.IdempotencyKey), semantic[:])
		if err != nil {
			return unavailable(err)
		}
		return t.appendAudit(ctx, tx, r, ac.ObjectTransferRevoke)
	})
	if err = commitError(result); err != nil {
		return r.status(), err
	}
	if r.spec.Details().Direction == oc.TransferPUT {
		if err = s.stopWriters(ctx, r.object); err != nil {
			return r.status(), err
		}
		if _, err = s.cleanObject(ctx, r.object); err != nil {
			return r.status(), err
		}
	}
	r, ok, err = loadTransfer(ctx, s.state().store, id)
	if err != nil || !ok {
		return oc.TransferStatusView{}, unavailable(err)
	}
	return r.status(), nil
}
func (t *TransferService) gateTransfer(ctx context.Context, e postgres.SQLExecutor, r transferRow) error {
	if r.spec.Details().Direction != oc.TransferPUT {
		return nil
	}
	u, ok, err := loadUpload(ctx, e, r.upload)
	if err != nil {
		return err
	}
	if !ok {
		return unavailable(nil)
	}
	stage, ok, err := loadAttempt(ctx, e, r.staging)
	if err != nil {
		return err
	}
	if !ok || stage.kind != "runner_staging" || stage.transfer != r.id {
		return unavailable(nil)
	}
	if err = t.state().objects.gateAttempt(ctx, e, stage, oc.CancelledUpload, r.id.String()); err != nil {
		return err
	}
	if u.disposition == "attached" {
		return nil
	}
	_, err = e.Exec(ctx, `UPDATE agenteam_object.uploads SET disposition='revoked' WHERE id=$1`, u.id.String())
	if err != nil {
		return unavailable(err)
	}
	_, err = e.Exec(ctx, `DELETE FROM agenteam_object.object_references WHERE object_id=$1 AND upload_id=$2 AND kind='reserved'`, r.object.String(), u.id.String())
	if err != nil {
		return unavailable(err)
	}
	return t.state().objects.gateObject(ctx, e, r.object, oc.CancelledUpload, r.id.String())
}

func (t *TransferService) ConfirmStopped(ctx context.Context, actor identity.Actor, id oc.TransferID, evidence oc.TransferEvidenceRef) (oc.TransferStatusView, error) {
	if actor.Validate() != nil || id.Validate() != nil || evidence.Validate() != nil {
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
	request, err := r.request(actor, oc.TransferConfirmTerminal, &evidence, nil, oc.PreparedPayload{})
	if err != nil {
		return r.status(), err
	}
	owner, _ := oc.NewLeaseOwner(oc.TransferOwner, id.String())
	leaseRequest, _ := oc.NewLeaseAccess(oc.AccessRequestDetails{Operation: oc.ReleaseUseAccess, Actor: actor, ObjectID: r.object, LeaseOwner: owner})
	result := t.within(ctx, request, []oc.AccessRequest{leaseRequest}, func(ctx context.Context, tx foundation.Tx, plans []oc.AccessLockPlan, locked oc.LockedAccess, a oc.TransferAuthorization) error {
		if r.stable != stableActor(actor) {
			return failure(foundation.Forbidden, nil)
		}
		proof := a.Details().Retirement
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		// The reference is a validated checkpoint, not a terminal proof.
		if r.retirement.Validate() != nil {
			_, err = e.Exec(ctx, `UPDATE agenteam_object.object_transfers SET retirement_pending=$2,retirement_pending_kind=$3,version=version+1 WHERE id=$1`, id.String(), evidence.ID.String(), string(evidence.Kind))
			if err != nil {
				return unavailable(err)
			}
		}
		if proof == nil {
			return nil
		}
		if err = checkpointRetirement(ctx, e, &r, proof); err != nil {
			return err
		}
		return t.retireInTx(ctx, tx, actor, r, plans[1], locked)

	})
	if err = commitError(result); err != nil {
		return r.status(), err
	}
	if r.spec.Details().Direction == oc.TransferPUT {
		if _, err = s.cleanObject(ctx, r.object); err != nil {
			return r.status(), err
		}
	}
	r, ok, err = loadTransfer(ctx, s.state().store, id)
	if err != nil || !ok {
		return oc.TransferStatusView{}, unavailable(err)
	}
	return r.status(), nil
}

func checkpointRetirement(ctx context.Context, e postgres.SQLExecutor, r *transferRow, proof *oc.TransferRetirement) error {
	if proof == nil {
		return failure(foundation.Forbidden, nil)
	}
	if r.retirement.Validate() == nil {
		if r.retirement != proof.Evidence.ID || r.retirementKind != proof.Evidence.Kind || !bytes.Equal(r.retirementDigest, digestBytes(proof.Digest)) {
			return failure(foundation.IdempotencyKeyReused, nil)
		}
		return nil
	}
	_, err := e.Exec(ctx, `UPDATE agenteam_object.object_transfers SET retirement_evidence=$2,retirement_kind=$3,retirement_digest=$4,retirement_pending=$2,retirement_pending_kind=$3,version=version+1 WHERE id=$1`, r.id.String(), proof.Evidence.ID.String(), string(proof.Evidence.Kind), digestBytes(proof.Digest))
	if err != nil {
		return unavailable(err)
	}
	r.retirement = proof.Evidence.ID
	r.retirementKind = proof.Evidence.Kind
	r.retirementDigest = digestBytes(proof.Digest)
	return nil
}

// retireInTx consumes a durable, previously validated independent retirement
// proof. The caller has just revalidated the current full transfer authority.
func (t *TransferService) retireInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, r transferRow, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
	s := t.state().objects
	if r.retirement.Validate() != nil || len(r.retirementDigest) != 32 {
		return failure(foundation.Forbidden, nil)
	}
	e, err := executor(s, tx)
	if err != nil {
		return err
	}
	if !r.leaseActive {
		return nil
	}
	var local bool
	if err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_leases WHERE object_id=$1 AND state='active' AND owner_kind IN ('source','reader','writer'))`, r.object.String()).Scan(&local); err != nil {
		return unavailable(err)
	}
	if local {
		return nil
	}
	if r.spec.Details().Direction == oc.TransferPUT {
		stage, exists, err := loadAttempt(ctx, e, r.staging)
		if err != nil {
			return err
		}
		if !exists {
			return unavailable(nil)
		}
		if stage.phase != "cleaned" {
			if !stage.cleaning && r.phase != "complete" && !r.revoked {
				if r.retirementKind != oc.TransferStoppedEvidence {
					// A completed-but-not-published PUT still needs its staging
					// source. Only exact stopped retirement abandons this grant.
					return nil
				}
				_, err = e.Exec(ctx, `UPDATE agenteam_object.object_transfers SET phase='failed',version=version+1 WHERE id=$1 AND phase<>'complete'`, r.id.String())
				if err != nil {
					return unavailable(err)
				}
			}
			if err = s.gateAttempt(ctx, e, stage, oc.AbandonedAttempt, r.id.String()); err != nil {
				return err
			}
			_, err = e.Exec(ctx, `UPDATE agenteam_object.object_transfers SET cleanup_gate=true,version=version+1 WHERE id=$1 AND NOT cleanup_gate`, r.id.String())
			return unavailableIf(err)
		}
	}
	owner, _ := oc.NewLeaseOwner(oc.TransferOwner, r.id.String())
	return s.ReleaseLeaseInTx(ctx, tx, actor, r.object, owner, plan, locked)
}

// Recover advances only already gated technical work. It never resubmits a
// Runner operation, reissues material, claims completion or releases a remote
// lease on the strength of elapsed time or a Central process death.
func (t *TransferService) Recover(ctx context.Context) error {
	return t.recoverProgress(ctx, &recoveryFailures{})
}
func (t *TransferService) recoverProgress(ctx context.Context, failures *recoveryFailures) (out error) {
	defer func() { failures.remember(out) }()
	s := t.state().objects
	op, done, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	ctx = op.ctx
	s.state().mu.Lock()
	joins := make([]oc.UploadAttempt, 0, len(t.state().pendingJoins))
	for _, attempt := range t.state().pendingJoins {
		joins = append(joins, attempt)
	}
	s.state().mu.Unlock()
	for _, attempt := range joins {
		if err = ctx.Err(); err != nil {
			return failures.remember(unavailable(err))
		}
		failures.remember(t.joinUnsentCandidate(ctx, attempt))
	}
	rows, err := s.state().store.Query(ctx, `SELECT t.id::text FROM agenteam_object.object_transfers t WHERE EXISTS(SELECT 1 FROM agenteam_object.object_leases l WHERE l.id=t.lease_id AND l.state='active') OR t.direction='put' AND EXISTS(SELECT 1 FROM agenteam_object.upload_attempts a WHERE a.id=t.staging_id AND a.phase<>'cleaned') ORDER BY t.recovery_pass,t.id LIMIT 100`)
	if err != nil {
		return unavailable(err)
	}
	var ids []oc.TransferID
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return unavailable(err)
		}
		id, err := foundation.ParseID[oc.Transfer](raw)
		if err != nil {
			rows.Close()
			return unavailable(err)
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return unavailable(err)
	}
	for _, id := range ids {
		if err = ctx.Err(); err != nil {
			return failures.remember(unavailable(err))
		}
		r, ok, err := loadTransfer(ctx, s.state().store, id)
		if err != nil {
			failures.remember(err)
			continue
		}
		if !ok {
			continue
		}
		registration, _ := identity.RegisterService(identity.ObjectMaintenance)
		actor, _ := registration.Actor(id.String(), r.spec.Details().Owner.Scope())
		request, err := r.request(actor, oc.TransferCleanup, nil, nil, oc.PreparedPayload{})
		if err != nil {
			failures.remember(err)
			continue
		}
		result := t.within(ctx, request, nil, func(ctx context.Context, tx foundation.Tx, _ []oc.AccessLockPlan, _ oc.LockedAccess, _ oc.TransferAuthorization) error {
			e, err := executor(s, tx)
			if err != nil {
				return err
			}
			_, err = e.Exec(ctx, `UPDATE agenteam_object.object_transfers SET recovery_pass=recovery_pass+1 WHERE id=$1`, id.String())
			return unavailableIf(err)
		})
		if err = commitError(result); err != nil {
			failures.remember(err)
			continue
		}
		if r.cleanup && r.spec.Details().Direction == oc.TransferPUT {
			_, err = s.cleanObject(ctx, r.object)
			failures.remember(err)
		}
		if ctx.Err() != nil {
			return failures.remember(unavailable(ctx.Err()))
		}
		failures.remember(t.recoverRetirement(ctx, actor, r.id))
	}
	return failures.first
}

func (t *TransferService) recoverRetirement(ctx context.Context, actor identity.Actor, id oc.TransferID) error {
	s := t.state().objects
	current, found, err := loadTransfer(ctx, s.state().store, id)
	if err != nil {
		return err
	}
	if !found || !current.leaseActive {
		return nil
	}
	evidence := oc.TransferEvidenceRef{ID: current.retirement, Kind: current.retirementKind}
	if evidence.Validate() != nil {
		evidence = oc.TransferEvidenceRef{ID: current.pendingRetirement, Kind: current.pendingKind}
	}
	if evidence.Validate() != nil {
		return nil
	}
	request, err := current.request(actor, oc.TransferCleanup, nil, nil, oc.PreparedPayload{})
	if err != nil {
		return err
	}
	terminal, err := current.request(actor, oc.TransferConfirmTerminal, &evidence, nil, oc.PreparedPayload{})
	if err != nil {
		return err
	}
	owner, _ := oc.NewLeaseOwner(oc.TransferOwner, current.id.String())
	leaseRequest, err := oc.NewLeaseAccess(oc.AccessRequestDetails{Operation: oc.ReleaseUseAccess, Actor: actor, ObjectID: current.object, LeaseOwner: owner})
	if err != nil {
		return err
	}
	result := t.within(ctx, request, []oc.AccessRequest{terminal, leaseRequest}, func(ctx context.Context, tx foundation.Tx, plans []oc.AccessLockPlan, locked oc.LockedAccess, _ oc.TransferAuthorization) error {
		authorization, err := t.authorize(ctx, tx, terminal, plans[1], locked)
		if err != nil {
			return err
		}
		proof := authorization.Details().Retirement
		if proof == nil {
			return nil
		}
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		if err = checkpointRetirement(ctx, e, &current, proof); err != nil {
			return err
		}
		return t.retireInTx(ctx, tx, actor, current, plans[2], locked)
	})
	return commitError(result)
}

// Project destruction has already acquired Project EX and every Object in the
// batch. No transfer field or payload survives the domain's final deletion.
func gateProjectTransfers(ctx context.Context, s *Service, tx foundation.Tx, id oc.ObjectID) error {
	e, err := executor(s, tx)
	if err != nil {
		return err
	}
	rows, err := e.Query(ctx, `SELECT id::text FROM agenteam_object.object_transfers WHERE object_id=$1 AND revoked_at IS NULL ORDER BY id`, id.String())
	if err != nil {
		return unavailable(err)
	}
	var ids []oc.TransferID
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return unavailable(err)
		}
		transfer, err := foundation.ParseID[oc.Transfer](raw)
		if err != nil {
			rows.Close()
			return unavailable(err)
		}
		ids = append(ids, transfer)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return unavailable(err)
	}
	for _, id := range ids {
		r, found, err := loadTransfer(ctx, e, id)
		if err != nil {
			return err
		}
		if !found {
			return unavailable(nil)
		}
		if err = appendTransferAudit(ctx, s, tx, r, ac.ObjectTransferRevoke); err != nil {
			return err
		}
	}
	_, err = e.Exec(ctx, `UPDATE agenteam_object.object_transfers SET revoked_at=coalesce(revoked_at,clock_timestamp()),cleanup_gate=true,version=version+1 WHERE object_id=$1 AND revoked_at IS NULL`, id.String())
	return unavailableIf(err)
}
func purgeObjectTransfers(ctx context.Context, e postgres.SQLExecutor, id oc.ObjectID) error {
	var remaining bool
	if err := e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_transfers t JOIN agenteam_object.object_leases l ON l.id=t.lease_id WHERE t.object_id=$1 AND (l.state='active' OR t.retirement_evidence IS NULL))`, id.String()).Scan(&remaining); err != nil {
		return unavailable(err)
	}
	if remaining {
		return failure(foundation.ResourceBusy, nil)
	}
	_, err := e.Exec(ctx, `DELETE FROM agenteam_object.object_transfers WHERE object_id=$1`, id.String())
	return unavailableIf(err)
}

var _ oc.Transfers = (*TransferService)(nil)
