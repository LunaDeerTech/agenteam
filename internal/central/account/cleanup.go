package account

import (
	"context"
	"errors"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type linkRemoval struct {
	link    linkRecord
	id      string
	key     foundation.CommandIdentity
	digest  foundation.Digest
	request sc.UsageRequest
	deps    sc.UsageDependencies
}

func (s *Service) planLinkRemoval(ctx context.Context, link linkRecord) (linkRemoval, error) {
	p := linkRemoval{link: link}
	id, e := foundation.NewID[c.Cleanup]()
	if e != nil {
		return p, unavailable(e)
	}
	p.id = id.String()
	p.key, e = cleanupIdentity(link.id, p.id)
	if e != nil {
		return p, e
	}
	p.digest, e = canonicalCommandDigest(p.key)
	if e != nil {
		return p, e
	}
	ref, e := linkRef(link.ref)
	if e != nil {
		return p, e
	}
	p.request, e = linkUsage(link.id, ref, false)
	if e != nil {
		return p, e
	}
	p.deps, e = s.state().deps.Secrets.DiscoverUsage(ctx, p.request)
	return p, portError(e)
}
func (p linkRemoval) locks() []foundation.LockRequest {
	locks := append(p.deps.RequiredLocks(), recordLock(p.id))
	if p.link.user != "" {
		locks = append(locks, userLock(p.link.user, foundation.Exclusive))
	}
	return locks
}
func (s *Service) removeLinkInTx(ctx context.Context, tx foundation.Tx, p linkRemoval) error {
	st := s.state()
	if e := st.store.RequireHeldLocks(ctx, tx, p.locks()); e != nil {
		return unavailable(e)
	}
	x, e := st.store.InTx(tx)
	if e != nil {
		return unavailable(e)
	}
	current, e := loadLink(ctx, x, c.TokenKind(p.link.kind), p.link.id)
	if e != nil {
		return e
	}
	if current.ref != p.link.ref || current.version != p.link.version {
		return fault(foundation.ResourceBusy, nil)
	}
	_, e = x.Exec(ctx, `INSERT INTO agenteam_account.material_cleanup(id,owner_kind,owner_id,credential_id,purpose,phase,version,secret_command_digest) VALUES($1,$2,$3,$4,'system','reference',1,$5) ON CONFLICT(owner_kind,owner_id,credential_id) DO NOTHING`, p.id, p.link.kind, p.link.id, p.link.ref, string(p.digest))
	if e != nil {
		return unavailable(e)
	}
	if _, e = st.deps.Secrets.ApplyUsageInTx(ctx, tx, p.request, p.deps); e != nil {
		return portError(e)
	}
	// Pending work loses its admission in the same transaction. An actual future
	// mail attempt is not guessed joined: its lease remains protected separately.
	if _, e = x.Exec(ctx, `UPDATE agenteam_account.mail_jobs SET phase='cancelled',reason='token_invalid',version=version+1,completed_at=clock_timestamp() WHERE intent_id IN (SELECT id FROM agenteam_account.delivery_intents WHERE kind=$1 AND link_id=$2) AND phase='pending'`, p.link.kind, p.link.id); e != nil {
		return unavailable(e)
	}
	table := "invitations"
	if p.link.kind == string(c.PasswordResetToken) {
		table = "password_resets"
	}
	if _, e = x.Exec(ctx, `DELETE FROM agenteam_account.`+table+` WHERE id=$1`, p.link.id); e != nil {
		return unavailable(e)
	}
	_, e = x.Exec(ctx, `UPDATE agenteam_account.material_cleanup SET phase='delete' WHERE owner_kind=$1 AND owner_id=$2 AND credential_id=$3 AND phase='reference'`, p.link.kind, p.link.id, p.link.ref)
	return portError(e)
}
func (s *Service) expireLink(ctx context.Context, kind c.TokenKind, id string) error {
	link, e := loadLink(ctx, s.state().store, kind, id)
	if hasFaultCode(e, foundation.ResourceDeleted) {
		return nil
	}
	if e != nil {
		return e
	}
	if link.live {
		return fault(foundation.ResourceBusy, nil)
	}
	p, e := s.planLinkRemoval(ctx, link)
	if e != nil {
		return e
	}
	cause, e := recoveryCause("expire-link")
	if e != nil {
		return e
	}
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, p.locks()); e != nil {
			return unavailable(e)
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		current, e := loadLink(ctx, x, kind, id)
		if hasFaultCode(e, foundation.ResourceDeleted) {
			return nil
		}
		if e != nil {
			return e
		}
		if current.live {
			return fault(foundation.ResourceBusy, nil)
		}
		return s.removeLinkInTx(ctx, tx, p)
	})
	return resultError(result)
}
func (s *Service) RevokeInvitation(ctx context.Context, r c.InvitationRevoke) error {
	if r.Actor.Validate() != nil || r.Actor.Details().Kind != identity.Human || r.Key.Validate() != nil || r.ID.Validate() != nil || r.ExpectedVersion.Validate() != nil {
		return invalid()
	}
	op, e := s.begin(ctx, false)
	if e != nil {
		return e
	}
	defer s.finish(op)
	ctx = op.ctx
	st := s.state()
	if _, e = st.deps.Authority.AuthorizeSystem(ctx, foundation.Tx{}, r.Actor, identity.Mutate); e != nil {
		return e
	}
	key, e := foundation.NewCommandIdentity("account.invitation", []string{r.Actor.Details().UserID}, "invite-revoke", r.Key)
	if e != nil {
		return e
	}
	macFor := func(kid string) ([]byte, error) {
		return st.keys.mac(kid, "command-v1", []byte("invite-revoke"), []byte(r.Actor.Details().UserID), []byte(r.ID.String()), []byte(r.ExpectedVersion.String()))
	}
	old, e := s.lookupMutation(ctx, key, macFor, r.Actor, c.BrowserIdentity{}, true)
	if e == nil {
		if old.phase == "committed" {
			return nil
		}
		return fault(foundation.InvalidState, nil)
	}
	if !hasFaultCode(e, foundation.NotFound) {
		return e
	}
	link, e := loadLink(ctx, st.store, c.InvitationToken, r.ID.String())
	if e != nil {
		return e
	}
	p, e := s.planLinkRemoval(ctx, link)
	if e != nil {
		return e
	}
	id, e := foundation.NewID[c.Command]()
	if e != nil {
		return unavailable(e)
	}
	mac, e := macFor(st.keys.current())
	if e != nil {
		return e
	}
	defer clear(mac)
	cause, e := foundation.NewCommandsCause(key)
	if e != nil {
		return e
	}
	locks := append(p.locks(), commandLock(key), userLock(r.Actor.Details().UserID, foundation.Shared), configLock("account-security", foundation.Shared), recordLock(id.String()))
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		if _, e := st.deps.Authority.AuthorizeSystem(ctx, tx, r.Actor, identity.Mutate); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		old, e := loadCommand(ctx, x, string(digest([]byte(key.Canonical()))), true)
		if e == nil {
			if e = s.checkCommand(old, key, macFor); e != nil {
				return e
			}
			if old.phase == "committed" {
				return nil
			}
			return fault(foundation.InvalidState, nil)
		}
		if !hasFaultCode(e, foundation.NotFound) {
			return e
		}
		current, e := loadLink(ctx, x, c.InvitationToken, r.ID.String())
		if e != nil {
			return e
		}
		if current.version != int64(r.ExpectedVersion) {
			return fault(foundation.VersionConflict, nil)
		}
		_, e = x.Exec(ctx, `INSERT INTO agenteam_account.commands(id,namespace,owner_id,command_key,command_name,identity_digest,semantic_kid,semantic_mac,actor_kind,user_id,session_id,origin_process_id,resource_id,expected_version,phase,result_code,result_version,completed_at) VALUES($1,'account.invitation',$2,$3,'invite-revoke',$4,$5,$6,'human',$2,$7,$8,$9,$10,'committed','COMPLETED',$10,clock_timestamp())`, id.String(), r.Actor.Details().UserID, string(r.Key), string(digest([]byte(key.Canonical()))), st.keys.current(), mac, r.Actor.Details().SessionID, st.process.String(), r.ID.String(), int64(r.ExpectedVersion))
		if e != nil {
			return unavailable(e)
		}
		if e = s.removeLinkInTx(ctx, tx, p); e != nil {
			return e
		}
		return s.mutationAudit(ctx, tx, r.Actor, ac.AccountInviteRevoke, ac.InvitationResource, r.ID.String(), id.String(), ac.AccountMetadataFields{InvitationID: r.ID.String(), InitiatorID: r.Actor.Details().UserID, Version: r.ExpectedVersion, Phase: ac.AccountRevoked})
	})
	if result.State() == foundation.Unknown {
		cmd, e := s.lookupMutation(ctx, key, macFor, r.Actor, c.BrowserIdentity{}, true)
		if e != nil {
			return confirmationError(result, e)
		}
		if cmd.phase != "committed" {
			return confirmationError(result, nil)
		}
		return nil
	}
	return resultError(result)
}
func (s *Service) cleanLinkMaterial(ctx context.Context, id string) error {
	st := s.state()
	var owner, kind, refRaw, phase string
	e := st.store.QueryRow(ctx, `SELECT owner_id::text,owner_kind,credential_id::text,phase FROM agenteam_account.material_cleanup WHERE id=$1 AND owner_kind IN ('invitation','password_reset')`, id).Scan(&owner, &kind, &refRaw, &phase)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return unavailable(e)
	}
	if phase == "completed" {
		return nil
	}
	if phase != "delete" {
		return fault(foundation.ResourceBusy, nil)
	}
	ref, e := linkRef(refRaw)
	if e != nil {
		return e
	}
	actor, e := serviceActor(identity.AccountMaintenance, id)
	if e != nil {
		return e
	}
	key, e := cleanupIdentity(owner, id)
	if e != nil {
		return e
	}
	request, e := sc.NewServiceWriteRequest(sc.ServiceWriteFields{Actor: actor, Scope: identity.SystemScope(), Identity: key, OwnerKind: sc.AccountOwnerKind(kind), OwnerID: owner, Kind: sc.Delete, Ref: ref, ExpectedVersion: 1, Purpose: sc.System})
	if e != nil {
		return e
	}
	prepared, e := st.deps.Secrets.PrepareServiceWrite(ctx, request)
	if e != nil {
		return portError(e)
	}
	cause, e := foundation.NewCommandsCause(key)
	if e != nil {
		return e
	}
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		locks := append(prepared.RequiredLocks(), configLock("account-directory", foundation.Exclusive), configLock("account-mail", foundation.Exclusive), recordLock(owner))
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		if _, e := st.deps.Secrets.ApplyServiceWriteInTx(ctx, tx, prepared); e != nil {
			return portError(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_account.material_cleanup SET phase='completed',completed_at=coalesce(completed_at,clock_timestamp()) WHERE id=$1`, id)
		return portError(e)
	})
	return resultError(result)
}

func (s *Service) expireCommandLink(ctx context.Context, id string, kind c.TokenKind) error {
	cmd, e := loadCommand(ctx, s.state().store, id, false)
	if e != nil {
		return e
	}
	return s.expireLink(ctx, kind, cmd.resource)
}
