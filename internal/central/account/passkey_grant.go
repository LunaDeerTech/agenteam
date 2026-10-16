package account

import (
	"context"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func validReauthPurpose(purpose, action, target string) bool {
	if purpose == "change_email" {
		return action == "" && target == ""
	}
	if purpose != "manage_passkeys" {
		return false
	}
	if action == "register" {
		return target == ""
	}
	return (action == "rename" || action == "delete") && passkeyID(target)
}
func (s *Service) issueReauthGrant(ctx context.Context, x postgres.SQLExecutor, actor identity.Actor, purpose, action, target, method, credentialID string) (ReauthGrant, error) {
	var out ReauthGrant
	if !validReauthPurpose(purpose, action, target) || method != "passkey" && method != "mail_recovery" || method == "mail_recovery" && (purpose != "manage_passkeys" || action != "register" || credentialID != "") || method == "passkey" && !passkeyID(credentialID) {
		return out, invalid()
	}
	id, e := foundation.NewID[struct{}]()
	if e != nil {
		return out, unavailable(e)
	}
	var expires time.Time
	e = x.QueryRow(ctx, `INSERT INTO agenteam_account.reauth_grants(id,user_id,session_id,purpose,action,target_id,method,credential_id,password_version,passkey_version,created_at,expires_at) SELECT $1,id,$3,$4,$5,$6,$7,$8,password_version,passkey_version,n,n+interval '5 minutes' FROM agenteam_account.users CROSS JOIN (SELECT clock_timestamp() n) z WHERE id=$2 RETURNING expires_at`, id.String(), actor.Details().UserID, actor.Details().SessionID, purpose, null(action), null(target), method, null(credentialID)).Scan(&expires)
	if e != nil {
		return out, unavailable(e)
	}
	return ReauthGrant{id.String(), purpose, instant(expires)}, nil
}

func (s *Service) consumeReauthGrant(ctx context.Context, x postgres.SQLExecutor, actor identity.Actor, id, purpose, action, target, consumer string) (string, error) {
	if !passkeyID(id) || !passkeyID(consumer) || !validReauthPurpose(purpose, action, target) {
		return "", invalid()
	}
	var method, consumed string
	var valid bool
	e := x.QueryRow(ctx, `SELECT g.method,coalesce(g.consumed_by::text,''),g.user_id=$2 AND g.session_id=$3 AND g.purpose=$4 AND coalesce(g.action,'')=$5 AND coalesce(g.target_id::text,'')=$6 AND g.expires_at>clock_timestamp() AND g.password_version=u.password_version AND g.passkey_version=u.passkey_version AND (g.method='mail_recovery' OR EXISTS(SELECT 1 FROM agenteam_account.passkeys p WHERE p.id=g.credential_id AND p.user_id=g.user_id AND p.revoked_at IS NULL)) FROM agenteam_account.reauth_grants g JOIN agenteam_account.users u ON u.id=g.user_id WHERE g.id=$1`, id, actor.Details().UserID, actor.Details().SessionID, purpose, action, target).Scan(&method, &consumed, &valid)
	if e != nil || !valid || consumed != "" && consumed != consumer {
		return "", fault(foundation.InvalidState, nil)
	}
	if consumed == "" {
		if _, e = x.Exec(ctx, `UPDATE agenteam_account.reauth_grants SET consumed_by=$2 WHERE id=$1`, id, consumer); e != nil {
			return "", unavailable(e)
		}
	}
	return method, nil
}

func (s *Service) securityAudit(ctx context.Context, tx foundation.Tx, actor identity.Actor, name, user, command string, version foundation.Version) error {
	var action ac.Action
	switch name {
	case "passkey-register":
		action = ac.AccountPasskeyRegister
	case "passkey-rename":
		action = ac.AccountPasskeyRename
	case "passkey-delete":
		action = ac.AccountPasskeyRevoke
	case "passkey-reauth":
		action = ac.AccountPasskeyReauthenticate
	case "passkey-recovery-request", "email-change-request", "email-code-resend", "email-initialize-request", "operator-recovery-request":
		action = ac.AccountEmailProofRequest
	case "passkey-recovery-confirm":
		action = ac.AccountEmailProofConfirm
	case "email-change-confirm", "email-initialize-confirm", "operator-recovery-confirm":
		action = ac.AccountEmailChange
	case "email-authorize":
		action = ac.AccountEmailAuthorize
	default:
		return invalid()
	}
	return s.mutationAudit(ctx, tx, actor, action, ac.UserResource, user, command, ac.AccountMetadataFields{UserID: user, Version: version, Phase: ac.AccountUpdated})
}
