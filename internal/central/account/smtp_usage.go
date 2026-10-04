package account

import (
	"context"
	"errors"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

func smtpReference(owner, ref string, retain bool) (sc.UsageRequest, error) {
	q, e := linkUsage(owner, sc.CredentialRef{}, retain)
	if e != nil {
		return q, e
	}
	q.Ref, e = linkRef(ref)
	q.Purpose = sc.SMTP
	return q, e
}
func (a *Authority) smtpReferenceMapping(ctx context.Context, x postgres.SQLExecutor, r sc.UsageRequest) (foundation.Digest, []foundation.LockRequest, error) {
	if r.Purpose != sc.SMTP {
		return "", nil, fault(foundation.Forbidden, nil)
	}
	locks := []foundation.LockRequest{configLock("account-directory", foundation.Shared), configLock("account-mail", foundation.Exclusive), recordLock(r.ReferenceOwner)}
	ref := r.Ref.Details().ID.String()
	if r.Retain {
		var id string
		e := x.QueryRow(ctx, `SELECT id::text FROM agenteam_account.commands WHERE resource_id=$1 AND planned_secret_ref=$2 AND command_name='smtp-settings-update' AND phase IN ('planned','committed')`, r.ReferenceOwner, ref).Scan(&id)
		if e != nil {
			return "", nil, fault(foundation.Forbidden, e)
		}
		cmd, e := loadCommand(ctx, x, id, false)
		if e != nil {
			return "", nil, e
		}
		locks = append(locks, commandLocks(cmd)...)
		return digest([]byte(string(commandMapping(cmd)) + "\x00" + ref)), locks, nil
	}
	var present bool
	e := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.smtp_settings WHERE id=$1 AND password_ref=$2 UNION ALL SELECT 1 FROM agenteam_account.material_cleanup WHERE owner_kind='smtp_settings' AND owner_id=$1 AND credential_id=$2 AND phase<>'completed')`, r.ReferenceOwner, ref).Scan(&present)
	if e != nil {
		return "", nil, unavailable(e)
	}
	if !present {
		return "", nil, fault(foundation.Forbidden, nil)
	}
	return digest([]byte(r.ReferenceOwner + "\x00" + ref)), locks, nil
}
func (a *Authority) validateSMTPReference(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor, r sc.UsageRequest) error {
	var valid bool
	if r.Retain {
		var id string
		e := x.QueryRow(ctx, `SELECT c.id::text FROM agenteam_account.commands c JOIN agenteam_account.smtp_settings s ON s.id=c.resource_id AND s.password_ref=c.planned_secret_ref WHERE c.command_name='smtp-settings-update' AND c.phase='committed' AND c.resource_id=$1 AND c.planned_secret_ref=$2`, r.ReferenceOwner, r.Ref.Details().ID.String()).Scan(&id)
		if e != nil {
			return fault(foundation.Forbidden, e)
		}
		cmd, e := loadCommand(ctx, x, id, false)
		if e != nil {
			return e
		}
		u, e := parseID[identity.User](cmd.user)
		if e != nil {
			return e
		}
		sid, e := parseID[identity.Session](cmd.session)
		if e != nil {
			return e
		}
		actor, e := identity.NewHuman(u, sid)
		if e != nil {
			return e
		}
		_, e = a.AuthorizeSystem(ctx, tx, actor, identity.Mutate)
		return e
	}
	e := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.material_cleanup WHERE owner_kind='smtp_settings' AND owner_id=$1 AND credential_id=$2 AND phase IN ('reference','leases','delete','completed'))`, r.ReferenceOwner, r.Ref.Details().ID.String()).Scan(&valid)
	if e != nil {
		return unavailable(e)
	}
	if !valid {
		return fault(foundation.Forbidden, nil)
	}
	return nil
}
func (s *Service) queueSMTPMaterialCleanup(ctx context.Context, x postgres.SQLExecutor, id c.CleanupID, before smtpRecord) error {
	key, e := cleanupIdentity(before.ID, id.String())
	if e != nil {
		return e
	}
	dg, e := canonicalCommandDigest(key)
	if e != nil {
		return e
	}
	_, e = x.Exec(ctx, `INSERT INTO agenteam_account.material_cleanup(id,owner_kind,owner_id,credential_id,purpose,phase,version,secret_command_digest) VALUES($1,'smtp_settings',$2,$3,'smtp','delete',1,$4) ON CONFLICT(owner_kind,owner_id,credential_id) DO NOTHING`, id.String(), before.ID, before.Ref, string(dg))
	return portError(e)
}
func (s *Service) cleanSMTPMaterial(ctx context.Context, id string) error {
	var owner, refRaw, phase string
	e := s.state().store.QueryRow(ctx, `SELECT owner_id::text,credential_id::text,phase FROM agenteam_account.material_cleanup WHERE id=$1 AND owner_kind='smtp_settings'`, id).Scan(&owner, &refRaw, &phase)
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
	r, e := sc.NewServiceWriteRequest(sc.ServiceWriteFields{Actor: actor, Scope: identity.SystemScope(), Identity: key, OwnerKind: sc.SMTPSettingsMaterial, OwnerID: owner, Kind: sc.Delete, Ref: ref, ExpectedVersion: 1, Purpose: sc.SMTP})
	if e != nil {
		return e
	}
	prepared, e := s.state().deps.Secrets.PrepareServiceWrite(ctx, r)
	if e != nil {
		return portError(e)
	}
	cause, e := foundation.NewCommandsCause(key)
	if e != nil {
		return e
	}
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		locks := append(prepared.RequiredLocks(), configLock("account-mail", foundation.Exclusive), recordLock(owner))
		if e := s.state().store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		if _, e := s.state().deps.Secrets.ApplyServiceWriteInTx(ctx, tx, prepared); e != nil {
			return portError(e)
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_account.material_cleanup SET phase='completed',completed_at=coalesce(completed_at,clock_timestamp()) WHERE id=$1`, id)
		return portError(e)
	})
	return resultError(result)
}
