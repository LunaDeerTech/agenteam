package secret

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

var _ sc.ProjectVariableWrites = (*Service)(nil)

// Apply uses the caller's one transaction and already-held complete lock union.
// A completed original intent replays before NewWrite. No callback, commit,
// confirmation transaction or lock acquisition is hidden in this method.
func (s *Service) ApplyProjectVariableWriteInTx(ctx context.Context, tx f.Tx, raw sc.PreparedProjectVariableWrite) (sc.ProjectVariableWriteObservation, error) {
	empty := sc.ProjectVariableWriteObservation{}
	saved, err := s.MatchProjectVariableIntentInTx(ctx, tx, raw)
	if err != nil {
		return empty, err
	}
	if saved.Observed() {
		return saved, nil
	}
	p, err := s.lockProjectVariablePrepared(raw)
	if err != nil {
		return empty, err
	}
	defer p.mu.Unlock()
	meta, err := p.preparation.Fields()
	if err != nil {
		return empty, invalid()
	}
	r := meta.Request.Fields()
	basis, err := p.plan.Basis()
	if err != nil || basis.Receipt.Observed() || !basis.Ref.Equal(meta.Ref) {
		return empty, invalid()
	}
	e, err := s.projectVariableInTx(ctx, tx, meta.Request, p.plan, sc.ProjectVariableNewWrite)
	if err != nil {
		return empty, err
	}
	if err = s.writable(); err != nil {
		return empty, err
	}
	version, epoch, err := s.control(ctx, e)
	if err != nil {
		return empty, err
	}
	if version != p.version || epoch != p.epoch {
		return empty, failure(EpochChanged, f.InvalidState, nil)
	}
	if p.receipt.ownerKind != projectVariableReceiptOwner || p.receipt.ownerID != meta.ReceiptID.String() || !p.receipt.scope.Equal(meta.Ref.Details().Scope) || p.receipt.id.Validate() != nil {
		return empty, invalid()
	}
	valuePresent := p.value.id.Validate() == nil
	if valuePresent && (p.value.ownerKind != valueOwner || p.value.ownerID != meta.Ref.Details().ID.String() || !p.value.scope.Equal(meta.Ref.Details().Scope)) {
		return empty, invalid()
	}
	user, err := f.ParseID[i.User](r.Actor.Details().UserID)
	if err != nil {
		return empty, invalid()
	}
	result := sc.ProjectVariableWriteResultFields{ReceiptID: meta.ReceiptID, ProjectID: r.ProjectID, VariableID: r.VariableID, UserID: user, Identity: r.Identity, Kind: r.Kind, ExpectedVersion: r.ExpectedVersion, Ref: meta.Ref, Effect: sc.ProjectVariableCreated, Version: 1}
	var prior sc.Metadata
	var oldPayload payloadID
	refID := meta.Ref.Details().ID.String()
	if r.Kind == sc.Create {
		if !valuePresent {
			return empty, invalid()
		}
		var exists bool
		if err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secrets WHERE id=$1)`, refID).Scan(&exists); err != nil {
			return empty, unavailable(err)
		}
		if exists {
			return empty, failure(PreparationRequired, f.ResourceBusy, nil)
		}
	} else {
		prior, oldPayload, err = loadProjectVariableMetadata(ctx, e, meta.Ref)
		if err != nil {
			return empty, err
		}
		if prior.Version != basis.CredentialVersion {
			return empty, failure(Conflict, f.VersionConflict, nil)
		}
		result.Version, result.Effect = prior.Version, sc.ProjectVariableUnchanged
		if r.Kind == sc.Delete || valuePresent {
			result.Version++
			result.Effect = sc.ProjectVariableReplaced
		}
		if r.Kind == sc.Delete {
			if valuePresent {
				return empty, invalid()
			}
			result.Effect, result.Deleted = sc.ProjectVariableDeleted, true
			var busy bool
			if err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secret_references WHERE credential_id=$1) OR EXISTS(SELECT 1 FROM agenteam_secret.secret_leases WHERE credential_id=$1 AND NOT released) OR EXISTS(SELECT 1 FROM agenteam_secret.project_variable_execution_leases WHERE credential_id=$1)`, refID).Scan(&busy); err != nil {
				return empty, unavailable(err)
			}
			if busy {
				return empty, failure(Busy, f.ResourceBusy, nil)
			}
		}
	}
	observation, err := sc.NewProjectVariableWriteObservation(result)
	if err != nil {
		return empty, failure(Conflict, f.VersionConflict, nil)
	}
	if valuePresent {
		if err = insertPayload(ctx, e, p.value); err != nil {
			return empty, err
		}
	}
	var tag pgconn.CommandTag
	switch result.Effect {
	case sc.ProjectVariableCreated:
		tag, err = e.Exec(ctx, `INSERT INTO agenteam_secret.secrets(id,scope,project_id,purpose,version,current_payload_id) VALUES($1,'project',$2,'project_variable',1,$3)`, refID, r.ProjectID.String(), p.value.id.String())
	case sc.ProjectVariableReplaced:
		tag, err = e.Exec(ctx, `UPDATE agenteam_secret.secrets SET version=$4,current_payload_id=$5 WHERE id=$1 AND scope='project' AND project_id=$2 AND purpose='project_variable' AND version=$3`, refID, r.ProjectID.String(), int64(prior.Version), int64(result.Version), p.value.id.String())
	case sc.ProjectVariableDeleted:
		if _, err = e.Exec(ctx, `DELETE FROM agenteam_secret.secret_leases WHERE credential_id=$1 AND released`, refID); err != nil {
			return empty, unavailable(err)
		}
		tag, err = e.Exec(ctx, `DELETE FROM agenteam_secret.secrets WHERE id=$1 AND scope='project' AND project_id=$2 AND purpose='project_variable' AND version=$3`, refID, r.ProjectID.String(), int64(prior.Version))
	}
	if err != nil {
		return empty, unavailable(err)
	}
	if result.Effect != sc.ProjectVariableUnchanged && tag.RowsAffected() != 1 {
		return empty, failure(Conflict, f.VersionConflict, nil)
	}
	if oldPayload.Validate() == nil && result.Effect != sc.ProjectVariableUnchanged {
		if _, err = e.Exec(ctx, `DELETE FROM agenteam_secret.secret_payloads WHERE payload_id=$1`, oldPayload.String()); err != nil {
			return empty, unavailable(err)
		}
	}
	if err = insertPayload(ctx, e, p.receipt); err != nil {
		return empty, err
	}
	digest, err := cursor.Digest([]byte(r.Identity.Canonical()))
	if err != nil {
		return empty, invalid()
	}
	var expected any
	if r.ExpectedVersion != nil {
		expected = int64(*r.ExpectedVersion)
	}
	if _, err = e.Exec(ctx, `INSERT INTO agenteam_secret.project_variable_receipts(id,project_id,variable_id,user_id,command_digest,command_kind,external_expected_version,credential_id,effect,result_version,deleted,digest_payload_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, meta.ReceiptID.String(), r.ProjectID.String(), r.VariableID.String(), user.String(), string(digest), r.Identity.Command(), expected, refID, string(result.Effect), int64(result.Version), result.Deleted, p.receipt.id.String()); err != nil {
		return empty, unavailable(err)
	}
	if result.Effect != sc.ProjectVariableUnchanged {
		if err = s.appendProjectVariableAudit(ctx, tx, p, prior, observation); err != nil {
			return empty, err
		}
	}
	if err = ctx.Err(); err != nil {
		return empty, unavailable(err)
	}
	return observation, nil
}
