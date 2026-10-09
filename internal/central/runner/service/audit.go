package service

import (
	"context"
	"encoding/json"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

// The private proof identifies this exact producer invocation and Tx. Public
// DTO/Actor constructors cannot mint it. The checker additionally reads the
// actual command, identity event and durable postimage through its own Store.
type auditProofKey struct{}
type auditProof struct {
	authority  *Authority
	tx         f.Tx
	entry      ac.Entry
	key        ac.AppendKey
	record     *commandRecord
	commandKey f.IdempotencyKey
	eventKind  string
}

func (s *Service) appendManagement(ctx context.Context, tx f.Tx, actor id.Actor, key f.IdempotencyKey, record *commandRecord, before c.Snapshot, changed []string) error {
	target := record.Intent.Target()
	resource, e := ac.NewResource(ac.RunnerResource, target.String())
	if e != nil {
		return unavailable(e)
	}
	actions := []ac.Action{ac.Action(record.Intent.Command())}
	if record.Intent.Command() == c.IssueEnrollment && before.PublicKeyFingerprint != nil {
		actions = []ac.Action{ac.RunnerRevoke, ac.RunnerEnrollmentIssue}
	}
	for ordinal, action := range actions {
		fields := ac.RunnerMetadataFields{RunnerID: target.String(), Version: record.Receipt.Runner.Version, CredentialGeneration: record.Receipt.Runner.CredentialGeneration, ChangedFields: append([]string(nil), changed...)}
		eventKind := ""
		if action == ac.RunnerRevoke {
			fields.PublicKeyFingerprint = before.PublicKeyFingerprint
			eventKind = "revoked"
		} else if action == ac.RunnerEnrollmentIssue {
			eventKind = "enrollment_issued"
		}
		metadata, e := ac.RunnerMetadata(action, fields)
		if e != nil {
			return unavailable(e)
		}
		entry, e := ac.NewEntry(ac.EntryFields{Scope: id.SystemScope(), Actor: actor, Action: action, Outcome: ac.Success, Resource: resource, Metadata: metadata, Associations: ac.Associations{RunnerID: target.String()}})
		if e != nil {
			return unavailable(e)
		}
		appendKey, e := ac.NewAppendKey(ac.RunnerProducer, record.ID.String(), int64(ordinal))
		if e != nil {
			return unavailable(e)
		}
		proof := &auditProof{authority: s.state().authority, tx: tx, entry: entry, key: appendKey, record: record, commandKey: key, eventKind: eventKind}
		if _, e = s.state().audit.AppendInTx(context.WithValue(ctx, auditProofKey{}, proof), tx, entry, appendKey); e != nil {
			return portError(e)
		}
	}
	return nil
}
func (a *Authority) CheckAppendInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) error {
	if a.state() == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() || entry.Validate() != nil || key.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if entry.Fields().Actor.Details().Kind == id.Service {
		return a.checkEnrollment(ctx, tx, entry, key)
	}
	proof, ok := ctx.Value(auditProofKey{}).(*auditProof)
	if !ok || proof == nil || proof.authority != a || proof.tx != tx || proof.record == nil || proof.key.Details() != key.Details() || !sameEntry(proof.entry, entry) {
		return fault(f.Forbidden)
	}
	ef := entry.Fields()
	record := proof.record
	if ef.Actor.Details().Kind != id.Human {
		return fault(f.Forbidden)
	}
	identity, e := record.Intent.Identity(ef.Actor, proof.commandKey)
	if e != nil {
		return fault(f.Forbidden)
	}
	target := record.Intent.Target()
	held, e := locks(ef.Actor, &target, &identity, true)
	if e != nil {
		return e
	}
	if e = a.state().store.RequireHeldLocks(ctx, tx, held); e != nil {
		return portError(e)
	}
	if e = a.authorize(ctx, tx, ef.Actor, id.Mutate); e != nil {
		return e
	}
	x, e := a.state().store.InTx(tx)
	if e != nil {
		return unavailable(e)
	}
	saved, e := loadCommand(ctx, x, ef.Actor, proof.commandKey, record.Intent)
	if e != nil {
		return e
	}
	if saved == nil || saved.ID != record.ID || saved.User != ef.Actor.Details().UserID || saved.Session != ef.Actor.Details().SessionID || !saved.Receipt.Changed {
		return fault(f.Forbidden)
	}
	left, e := json.Marshal(saved.Receipt)
	if e != nil {
		return unavailable(e)
	}
	right, e := json.Marshal(record.Receipt)
	if e != nil || !sameJSON(left, right) {
		return fault(f.Forbidden)
	}
	current, e := loadRunner(ctx, x, target)
	if e != nil {
		return e
	}
	if !samePostimage(current, record.Receipt.Runner) {
		return fault(f.Forbidden)
	}
	if proof.eventKind != "" {
		m, e := ef.Metadata.RunnerFields()
		if e != nil {
			return e
		}
		var fp any
		if m.PublicKeyFingerprint != nil {
			fp = string(*m.PublicKeyFingerprint)
		}
		var exists bool
		e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_runner.identity_events WHERE command_id=$1 AND runner_id=$2 AND kind=$3 AND version=$4 AND credential_generation=$5 AND public_key_fingerprint IS NOT DISTINCT FROM $6::text)`, record.ID.String(), target.String(), proof.eventKind, int64(m.Version), int64(m.CredentialGeneration), fp).Scan(&exists)
		if e != nil {
			return unavailable(e)
		}
		if !exists {
			return fault(f.Forbidden)
		}
	}
	return nil
}
func sameEntry(a, b ac.Entry) bool {
	x, y := a.Fields(), b.Fields()
	return x.Scope.Equal(y.Scope) && x.Actor.Equal(y.Actor) && x.Action == y.Action && x.Outcome == y.Outcome && x.Resource.Details() == y.Resource.Details() && x.Associations == y.Associations && sameJSON(x.Metadata.JSON(), y.Metadata.JSON())
}
func samePostimage(a, b c.Snapshot) bool {
	// Lease time can pass inside a transaction. Online is a derived read, not a
	// mutable business field or part of Audit's durable version proof.
	a.Status = c.Offline
	b.Status = c.Offline
	left, e := json.Marshal(a)
	if e != nil {
		return false
	}
	right, e := json.Marshal(b)
	return e == nil && sameJSON(left, right)
}

var _ ac.RunnerAuthority = (*Authority)(nil)
