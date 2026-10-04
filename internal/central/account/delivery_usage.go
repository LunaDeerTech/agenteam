package account

import (
	"context"
	"encoding/json"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func loadMailAttempt(ctx context.Context, x postgres.SQLExecutor, id string) (mailRecord, error) {
	var job string
	if e := x.QueryRow(ctx, `SELECT job_id::text FROM agenteam_account.mail_attempts WHERE id=$1`, id).Scan(&job); e != nil {
		return mailRecord{}, fault(foundation.Forbidden, e)
	}
	return loadMail(ctx, x, job, id)
}
func mailSlot(r mailRecord, p sc.Purpose) (ref, lease string) {
	if p == sc.SMTP {
		return r.CredentialRef, r.CredentialLease
	}
	return r.TokenRef, r.TokenLease
}
func (a *Authority) deliveryUsageMapping(ctx context.Context, x postgres.SQLExecutor, r sc.UsageRequest) (foundation.Digest, []foundation.LockRequest, error) {
	m, e := loadMailAttempt(ctx, x, r.LeaseOwner.Details().ID)
	if e != nil {
		return "", nil, e
	}
	ref, lease := mailSlot(m, r.Purpose)
	if lease == "" || lease != r.LeaseID.String() || ref != "" && ref != r.Ref.Details().ID.String() {
		return "", nil, fault(foundation.Forbidden, nil)
	}
	if ref == "" {
		// Legacy recovery may only release an exact pre-existing lease, after
		// the acquisition fence is durable. Secret itself checks its stored ref.
		if r.Action != sc.ReleaseLeaseUsage || !m.Joined {
			return "", nil, fault(foundation.Forbidden, nil)
		}
		if e = a.legacyMailCandidate(ctx, x, m, r.Ref.Details().ID.String(), r.Purpose); e != nil {
			return "", nil, e
		}
	}
	b, _ := json.Marshal(struct {
		Mapping foundation.Digest
		Ref     string
	}{mailMapping(m), r.Ref.Details().ID.String()})
	return digest(b), mailLocks(m), nil
}
func (a *Authority) legacyMailCandidate(ctx context.Context, x postgres.SQLExecutor, m mailRecord, ref string, p sc.Purpose) error {
	var valid bool
	var e error
	if p == sc.SMTP {
		e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.smtp_settings WHERE password_ref=$1 UNION ALL SELECT 1 FROM agenteam_account.material_cleanup WHERE owner_kind='smtp_settings' AND credential_id=$1 AND phase<>'completed')`, ref).Scan(&valid)
	} else {
		e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.invitations WHERE id=$1 AND material_ref=$2 UNION ALL SELECT 1 FROM agenteam_account.password_resets WHERE id=$1 AND material_ref=$2 UNION ALL SELECT 1 FROM agenteam_account.material_cleanup WHERE owner_id=$1 AND credential_id=$2 AND owner_kind IN ('invitation','password_reset') AND phase<>'completed')`, m.Link, ref).Scan(&valid)
	}
	if e != nil {
		return unavailable(e)
	}
	if !valid {
		return fault(foundation.Forbidden, nil)
	}
	return nil
}
func (a *Authority) authorizeDeliveryLease(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor, actor identity.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	empty := sc.UseGrant{}
	m, e := loadMailAttempt(ctx, x, owner.Details().ID)
	if e != nil {
		return empty, e
	}
	if e = a.state().store.RequireHeldLocks(ctx, tx, mailLocks(m)); e != nil {
		return empty, unavailable(e)
	}
	purpose := sc.System
	if m.CredentialRef == ref.Details().ID.String() {
		purpose = sc.SMTP
	} else if m.TokenRef != ref.Details().ID.String() {
		if action != sc.ReleaseLease || !m.Joined {
			return empty, fault(foundation.Forbidden, nil)
		}
		// This branch is reachable only through a planned release. The exact
		// lease/ref/purpose was checked by the Secret outer operation.
		matchedSMTP := false
		if m.CredentialRef == "" && m.CredentialLease != "" {
			e = a.legacyMailCandidate(ctx, x, m, ref.Details().ID.String(), sc.SMTP)
			if e != nil && !hasFaultCode(e, foundation.Forbidden) {
				return empty, e
			}
			matchedSMTP = e == nil
		}
		if matchedSMTP {
			purpose = sc.SMTP
		} else if m.TokenRef == "" && m.TokenLease != "" {
			if e = a.legacyMailCandidate(ctx, x, m, ref.Details().ID.String(), sc.System); e != nil {
				return empty, e
			}
		} else {
			return empty, fault(foundation.Forbidden, nil)
		}
	}
	if action == sc.ReleaseLease {
		if !m.Joined {
			return empty, fault(foundation.ResourceBusy, nil)
		}
	} else {
		if e = mailCurrent(m); e != nil {
			return empty, e
		}
		cfg, e := loadSMTP(ctx, x)
		if e != nil {
			return empty, e
		}
		if _, _, e = mailLive(ctx, x, m, cfg); e != nil {
			return empty, e
		}
	}
	return sc.UseGrant{Subject: actor, Consumer: purpose}, nil
}
func deliveryUsage(m mailRecord, p sc.Purpose, action sc.UsageAction) (sc.UsageRequest, error) {
	refRaw, leaseRaw := mailSlot(m, p)
	ref, e := linkRef(refRaw)
	if e != nil {
		return sc.UsageRequest{}, e
	}
	lease, e := foundation.ParseID[sc.Lease](leaseRaw)
	if e != nil {
		return sc.UsageRequest{}, invalid()
	}
	a, e := serviceActor(identity.SecretService, m.Attempt)
	if e != nil {
		return sc.UsageRequest{}, e
	}
	owner, e := sc.NewCredentialLeaseOwner(sc.AccountDeliveryOwner, m.Attempt)
	if e != nil {
		return sc.UsageRequest{}, e
	}
	return sc.UsageRequest{Actor: a, Ref: ref, Purpose: p, LeaseOwner: owner, LeaseID: lease, Action: action}, nil
}
