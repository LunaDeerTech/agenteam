package account

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type OperatorAuthorization struct {
	ID            string             `json:"id"`
	ExpiresAt     foundation.Instant `json:"expires_at"`
	Authorization sc.SecretMaterial  `json:"-"`
}

// IssueEmailAuthorization is an explicit deployment operator entry point. It
// is not an HTTP authority and grants no account access by itself: redemption
// still requires the same account's current session, password and SMTP code.
func (a *Authority) IssueEmailAuthorization(ctx context.Context, auditor ac.Appender, user, purpose, email string) (OperatorAuthorization, error) {
	var out OperatorAuthorization
	if a == nil || a.data == nil || nilPort(auditor) || !passkeyID(user) || (purpose != "initialize_email" && purpose != "recover_account") {
		return out, invalid()
	}
	email, e := NormalizeEmail(email)
	if e != nil {
		return out, e
	}
	id, e := foundation.NewID[struct{}]()
	if e != nil {
		return out, unavailable(e)
	}
	raw := make([]byte, 32)
	if _, e = rand.Read(raw); e != nil {
		return out, unavailable(e)
	}
	defer clear(raw)
	proof := operatorTokenVerifier(raw)
	defer clear(proof)
	material, e := sc.NewSecretMaterial([]byte(id.String() + "." + base64.RawURLEncoding.EncodeToString(raw)))
	if e != nil {
		return out, portError(e)
	}
	keep := false
	defer func() {
		if !keep {
			material.Destroy()
		}
	}()
	cause, e := recoveryCause("email-operator-authorization")
	if e != nil {
		return out, e
	}
	actor, e := serviceActor(identity.AccountMaintenance, id.String())
	if e != nil {
		return out, e
	}
	var expires time.Time
	result := a.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := a.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{configLock("account-directory", foundation.Exclusive), userLock(user, foundation.Exclusive), recordLock(id.String())}); e != nil {
			return unavailable(e)
		}
		x, e := a.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		u, e := loadUser(ctx, x, user)
		if e != nil {
			return e
		}
		var verified bool
		if e = x.QueryRow(ctx, `SELECT email_verified_at IS NOT NULL FROM agenteam_account.users WHERE id=$1`, user).Scan(&verified); e != nil {
			return unavailable(e)
		}
		if purpose == "initialize_email" && verified {
			return fault(foundation.InvalidState, nil)
		}
		var taken bool
		if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.users WHERE email=$1 AND id<>$2)`, email, user).Scan(&taken); e != nil {
			return unavailable(e)
		}
		if taken {
			return field("/email", "ALREADY_EXISTS")
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_account.operator_authorizations SET revoked_at=clock_timestamp() WHERE user_id=$1 AND purpose=$2 AND revoked_at IS NULL AND expires_at>clock_timestamp()`, user, purpose); e != nil {
			return unavailable(e)
		}
		e = x.QueryRow(ctx, `INSERT INTO agenteam_account.operator_authorizations(id,user_id,purpose,target_email,token_verifier,created_at,expires_at) SELECT $1,$2,$3,$4,$5,n,n+interval '30 minutes' FROM (SELECT clock_timestamp() n) z RETURNING expires_at`, id.String(), user, purpose, email, proof).Scan(&expires)
		if e != nil {
			return unavailable(e)
		}
		metadata, e := ac.AccountMetadata(ac.AccountEmailAuthorize, ac.AccountMetadataFields{UserID: user, Version: u.user.Version, Phase: ac.AccountUpdated})
		if e != nil {
			return e
		}
		resource, e := ac.NewResource(ac.UserResource, user)
		if e != nil {
			return e
		}
		entry, e := ac.NewEntry(ac.EntryFields{Actor: actor, Scope: identity.SystemScope(), Action: ac.AccountEmailAuthorize, Outcome: ac.Success, Resource: resource, Metadata: metadata})
		if e != nil {
			return e
		}
		key, e := ac.NewAppendKey(ac.AccountProducer, id.String(), 0)
		if e != nil {
			return e
		}
		_, e = auditor.AppendInTx(ctx, tx, entry, key)
		return portError(e)
	})
	if e = resultError(result); e != nil {
		return out, e
	}
	keep = true
	return OperatorAuthorization{ID: id.String(), ExpiresAt: instant(expires), Authorization: material}, nil
}
