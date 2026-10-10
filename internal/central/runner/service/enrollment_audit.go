package service

import (
	"context"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

type enrollmentProofKey struct{}
type enrollmentProof struct {
	authority *Authority
	tx        f.Tx
	entry     ac.Entry
	key       ac.AppendKey
	event     c.IdentityEventID
	current   c.Snapshot
	tokenHash [32]byte
	consumed  time.Time
}

func (s *Service) appendEnrollment(ctx context.Context, tx f.Tx, event c.IdentityEventID, current c.Snapshot, hash [32]byte, consumed time.Time) error {
	registration, e := id.RegisterService(id.RunnerIdentity)
	if e != nil {
		return unavailable(e)
	}
	actor, e := registration.Actor(event.String(), id.SystemScope())
	if e != nil {
		return unavailable(e)
	}
	resource, e := ac.NewResource(ac.RunnerResource, current.ID.String())
	if e != nil {
		return unavailable(e)
	}
	metadata, e := ac.RunnerMetadata(ac.RunnerEnroll, ac.RunnerMetadataFields{RunnerID: current.ID.String(), Version: current.Version, CredentialGeneration: current.CredentialGeneration, ChangedFields: []string{"credential"}, PublicKeyFingerprint: current.PublicKeyFingerprint})
	if e != nil {
		return unavailable(e)
	}
	entry, e := ac.NewEntry(ac.EntryFields{Scope: id.SystemScope(), Actor: actor, Action: ac.RunnerEnroll, Outcome: ac.Success, Resource: resource, Metadata: metadata, Associations: ac.Associations{RunnerID: current.ID.String()}})
	if e != nil {
		return unavailable(e)
	}
	key, e := ac.NewAppendKey(ac.RunnerProducer, event.String(), 0)
	if e != nil {
		return unavailable(e)
	}
	proof := &enrollmentProof{s.state().authority, tx, entry, key, event, current.Clone(), hash, consumed}
	_, e = s.state().audit.AppendInTx(context.WithValue(ctx, enrollmentProofKey{}, proof), tx, entry, key)
	return portError(e)
}

func (a *Authority) checkEnrollment(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) error {
	proof, ok := ctx.Value(enrollmentProofKey{}).(*enrollmentProof)
	if !ok || proof == nil || proof.authority != a || proof.tx != tx || proof.event.Validate() != nil || proof.current.Validate() != nil || proof.current.EnrolledAt == nil || proof.current.PublicKeyFingerprint == nil || !sameEntry(proof.entry, entry) || proof.key.Details() != key.Details() {
		return fault(f.Forbidden)
	}
	ef := entry.Fields()
	actor := ef.Actor.Details()
	if actor.Kind != id.Service || actor.ServiceName != id.RunnerIdentity || actor.CauseRef != proof.event.String() || ef.Action != ac.RunnerEnroll {
		return fault(f.Forbidden)
	}
	held, e := deviceLocks(proof.current.ID)
	if e != nil {
		return e
	}
	if e = a.state().store.RequireHeldLocks(ctx, tx, held); e != nil {
		return portError(e)
	}
	x, e := a.state().store.InTx(tx)
	if e != nil {
		return unavailable(e)
	}
	current, e := loadRunner(ctx, x, proof.current.ID)
	if e != nil {
		return e
	}
	if !samePostimage(current, proof.current) {
		return fault(f.Forbidden)
	}
	var exists bool
	e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_runner.identity_events e JOIN agenteam_runner.enrollment_tokens t ON t.runner_id=e.runner_id AND t.credential_generation=e.credential_generation WHERE e.id=$1 AND e.runner_id=$2 AND e.kind='enrolled' AND e.command_id IS NULL AND e.version=$3 AND e.credential_generation=$4 AND e.public_key_fingerprint=$5 AND e.occurred_at=$6 AND t.token_hash=$7 AND t.consumed_at=$8 AND t.revoked_at IS NULL)`, proof.event.String(), current.ID.String(), int64(current.Version), int64(current.CredentialGeneration), string(*current.PublicKeyFingerprint), current.EnrolledAt.Time(), proof.tokenHash[:], proof.consumed).Scan(&exists)
	if e != nil {
		return unavailable(e)
	}
	if !exists {
		return fault(f.Forbidden)
	}
	return nil
}
