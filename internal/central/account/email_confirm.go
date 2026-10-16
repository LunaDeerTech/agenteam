package account

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"strings"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// revokeOtherSessionsAndResets runs under the current User/directory writers.
// The caller also owns mailExclusive until the enclosing transaction joins.
// Existing reset material remains available only to its cleanup lifecycle.
func revokeOtherSessionsAndResets(ctx context.Context, x postgres.SQLExecutor, user, keep string) error {
	if _, e := x.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE user_id=$1 AND id<>$2 AND revoked_at IS NULL`, user, keep); e != nil {
		return unavailable(e)
	}
	if _, e := x.Exec(ctx, `UPDATE agenteam_account.password_resets SET expires_at=clock_timestamp() WHERE user_id=$1 AND expires_at>clock_timestamp()`, user); e != nil {
		return unavailable(e)
	}
	_, e := x.Exec(ctx, `UPDATE agenteam_account.reset_requests SET phase='processed',completed_at=clock_timestamp() WHERE phase='accepted' AND (canonical_email IN (SELECT email FROM agenteam_account.users WHERE id=$1) OR command_id IN (SELECT id FROM agenteam_account.commands WHERE user_id=$1 AND command_name='reset-request'))`, user)
	return portError(e)
}

// ConfirmEmailProof commits the attempt budget even for an incorrect code. A
// receipt records that rejection so replaying one idempotency key never charges
// another attempt. Raw codes appear only inside the command HMAC input.
func (s *Service) ConfirmEmailProof(ctx context.Context, actor identity.Actor, key foundation.IdempotencyKey, kind, requestID string, code sc.SecretMaterial) (json.RawMessage, error) {
	if !passkeyID(requestID) || actor.Validate() != nil || key.Validate() != nil {
		return nil, field("/code", "INVALID_OR_EXPIRED")
	}
	if kind == "initialization" {
		prior, e := loadEmailRequest(ctx, s.state().store, requestID)
		if e != nil {
			return nil, e
		}
		if prior.kind == "operator_recovery" {
			kind = prior.kind
		}
	}
	name := strings.TrimSuffix(emailRequestName(kind), "request") + "confirm"
	if emailRequestName(kind) == "" {
		return nil, invalid()
	}
	var semantic []byte
	e := code.Use(func(b []byte) error {
		if !validEmailCode(b) {
			return field("/code", "INVALID_OR_EXPIRED")
		}
		var e error
		semantic, e = json.Marshal([]string{requestID, kind, string(b)})
		return e
	})
	if e != nil {
		return nil, e
	}
	defer clear(semantic)
	guard, e := s.state().deps.Authority.mailExclusive(ctx)
	if e != nil {
		return nil, e
	}
	defer guard.release()
	planEvent := func(ctx context.Context, x postgres.SQLExecutor, cmd commandRecord, u userRecord) (*event.Event, error) {
		if kind == "passkey_recovery" {
			return nil, nil
		}
		reason := c.EmailChanged
		if kind == "operator_recovery" {
			reason = c.PasskeysRecovered
		}
		evt, e := s.planOtherSessionsEvent(ctx, x, cmd, u, reason)
		if e != nil {
			return nil, e
		}
		return &evt, nil
	}
	out, e := s.securityCommandWithEvent(ctx, actor, key, name, semantic, planEvent, func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor, command c.CommandID) (any, error) {
		r, e := loadEmailRequest(ctx, x, requestID)
		if e != nil {
			return nil, e
		}
		if r.user != actor.Details().UserID || r.session != actor.Details().SessionID || r.kind != kind || r.phase != "pending" || !r.live || r.attempts >= 5 {
			return nil, field("/code", "INVALID_OR_EXPIRED")
		}
		u, e := loadUser(ctx, x, r.user)
		if e != nil {
			return nil, e
		}
		var pkv int64
		var oldVerified bool
		if e = x.QueryRow(ctx, `SELECT passkey_version,email_verified_at IS NOT NULL FROM agenteam_account.users WHERE id=$1`, r.user).Scan(&pkv, &oldVerified); e != nil {
			return nil, unavailable(e)
		}
		if int64(u.user.Version) != r.userVersion || int64(u.passwordVersion) != r.passwordVersion || pkv != r.passkeyVersion || u.user.Email != r.original {
			return nil, field("/code", "INVALID_OR_EXPIRED")
		}
		if r.kind == "initialization" && oldVerified {
			return nil, fault(foundation.InvalidState, nil)
		}
		if e = emailRate(ctx, x, r.user, r.kind, false); e != nil {
			return nil, e
		}
		var expected []byte
		e = code.Use(func(b []byte) error { var e error; expected, e = s.emailCodeMAC(r.kid, r, b); return e })
		if e != nil {
			return nil, e
		}
		defer clear(expected)
		if subtle.ConstantTimeCompare(expected, r.mac) != 1 {
			if _, e = x.Exec(ctx, `UPDATE agenteam_account.email_requests SET attempts=attempts+1,phase=CASE WHEN attempts+1>=5 THEN 'locked' ELSE phase END WHERE id=$1`, r.id); e != nil {
				return nil, unavailable(e)
			}
			if _, e = x.Exec(ctx, `UPDATE agenteam_account.email_rate_limits SET failures=failures+1 WHERE user_id=$1 AND kind=$2`, r.user, r.kind); e != nil {
				return nil, unavailable(e)
			}
			return map[string]string{"rejected": "invalid_code"}, nil
		}
		if r.kind == "passkey_recovery" {
			if _, e = x.Exec(ctx, `UPDATE agenteam_account.users SET email_verified_at=coalesce(email_verified_at,clock_timestamp()) WHERE id=$1`, r.user); e != nil {
				return nil, unavailable(e)
			}
			grant, e := s.issueReauthGrant(ctx, x, actor, "manage_passkeys", "register", "", "mail_recovery", "")
			if e != nil {
				return nil, e
			}
			if _, e = x.Exec(ctx, `UPDATE agenteam_account.email_requests SET phase='consumed',completed_at=clock_timestamp() WHERE id=$1`, r.id); e != nil {
				return nil, unavailable(e)
			}
			if e = s.securityAudit(ctx, tx, actor, name, r.user, command.String(), u.user.Version); e != nil {
				return nil, e
			}
			return grant, nil
		}
		if r.kind == "initialization" || r.kind == "operator_recovery" {
			var authorized bool
			e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.operator_authorizations WHERE id=$1 AND user_id=$2 AND target_email=$3 AND consumed_request_id=$4 AND revoked_at IS NULL AND expires_at>clock_timestamp() AND purpose=$5)`, r.authorization, r.user, r.target, r.id, map[string]string{"initialization": "initialize_email", "operator_recovery": "recover_account"}[r.kind]).Scan(&authorized)
			if e != nil {
				return nil, unavailable(e)
			}
			if !authorized {
				return nil, field("/code", "INVALID_OR_EXPIRED")
			}
		}
		if e = revokeOtherSessionsAndResets(ctx, x, r.user, r.session); e != nil {
			return nil, e
		}
		if r.kind == "operator_recovery" {
			if _, e = x.Exec(ctx, `UPDATE agenteam_account.passkeys SET revoked_at=clock_timestamp(),version=version+1 WHERE user_id=$1 AND revoked_at IS NULL`, r.user); e != nil {
				return nil, unavailable(e)
			}
			if _, e = x.Exec(ctx, `UPDATE agenteam_account.users SET passkey_version=passkey_version+1 WHERE id=$1`, r.user); e != nil {
				return nil, unavailable(e)
			}
		}
		var changed EmailChangeReceipt
		var at time.Time
		e = x.QueryRow(ctx, `UPDATE agenteam_account.users SET email=$2,email_verified_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp() WHERE id=$1 AND version=$3 RETURNING version,email,updated_at`, r.user, r.target, r.userVersion).Scan(&changed.Version, &changed.Email, &at)
		if e != nil {
			return nil, accountConflict(e)
		}
		changed.CreatedAt = instant(at)
		if _, e = x.Exec(ctx, `UPDATE agenteam_account.email_requests SET phase='consumed',completed_at=clock_timestamp() WHERE id=$1`, r.id); e != nil {
			return nil, unavailable(e)
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_account.email_requests SET phase='cancelled',completed_at=clock_timestamp() WHERE user_id=$1 AND id<>$2 AND phase='pending'`, r.user, r.id); e != nil {
			return nil, unavailable(e)
		}
		if oldVerified {
			if e = s.queueOldEmailNotice(ctx, x, command, r, changed.Version); e != nil {
				return nil, e
			}
		}
		if e = s.securityAudit(ctx, tx, actor, name, r.user, command.String(), changed.Version); e != nil {
			return nil, e
		}
		return changed, nil
	})
	if e != nil {
		return nil, e
	}
	var rejected struct {
		Rejected string `json:"rejected"`
	}
	if json.Unmarshal(out, &rejected) == nil && rejected.Rejected != "" {
		return nil, field("/code", "INVALID_OR_EXPIRED")
	}
	return out, nil
}
