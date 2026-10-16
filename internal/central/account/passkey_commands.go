package account

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/account/passkeycrypto"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type PasskeyBegin struct {
	Purpose         string             `json:"purpose"`
	Action          string             `json:"action,omitempty"`
	PasskeyID       string             `json:"passkey_id,omitempty"`
	GrantID         string             `json:"grant_id,omitempty"`
	Label           string             `json:"label,omitempty"`
	ExpectedVersion foundation.Version `json:"expected_version,omitempty"`
}

func (s *Service) BeginPasskeyOperation(ctx context.Context, v *pc.Verifier, actor identity.Actor, r PasskeyBegin) (PasskeyOptions, error) {
	var out PasskeyOptions
	if e := passkeyUnavailable(v); e != nil {
		return out, e
	}
	if r.Purpose == "register" {
		var e error
		r.Label, e = passkeyLabel(r.Label)
		if e != nil {
			return out, e
		}
		if !passkeyID(r.GrantID) || r.ExpectedVersion.Validate() != nil {
			return out, invalid()
		}
	} else if !validReauthPurpose(r.Purpose, r.Action, r.PasskeyID) {
		return out, invalid()
	}
	op, e := s.begin(ctx, false)
	if e != nil {
		return out, e
	}
	defer s.finish(op)
	id, e := foundation.NewID[struct{}]()
	if e != nil {
		return out, unavailable(e)
	}
	cause, e := recoveryCause("passkey-options")
	if e != nil {
		return out, e
	}
	st := s.state()
	result := st.store.WithinTx(op.ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, []foundation.LockRequest{userLock(actor.Details().UserID, foundation.Exclusive)}); e != nil {
			return unavailable(e)
		}
		if e := st.deps.Authority.RequireCurrentSession(ctx, tx, actor); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		var count int
		if e = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_account.passkey_operations WHERE user_id=$1 AND expires_at>clock_timestamp()`, actor.Details().UserID).Scan(&count); e != nil {
			return unavailable(e)
		}
		if count >= 20 {
			return fault(foundation.RateLimited, nil)
		}
		user, e := loadPasskeyUser(ctx, x, actor.Details().UserID)
		if e != nil {
			return e
		}
		var options, data json.RawMessage
		if r.Purpose == "register" {
			var version int64
			if e = x.QueryRow(ctx, `SELECT passkey_version FROM agenteam_account.users WHERE id=$1`, actor.Details().UserID).Scan(&version); e != nil {
				return unavailable(e)
			}
			if version != int64(r.ExpectedVersion) {
				return fault(foundation.VersionConflict, nil)
			}
			method, e := s.consumeReauthGrant(ctx, x, actor, r.GrantID, "manage_passkeys", "register", "", id.String())
			if e != nil {
				return e
			}
			if len(user.Credentials) >= 20 && method != "mail_recovery" {
				return fault(foundation.InvalidState, nil)
			}
			options, data, e = v.BeginRegistration(user)
		} else {
			if len(user.Credentials) == 0 {
				return fault(foundation.InvalidState, nil)
			}
			if r.PasskeyID != "" {
				var target bool
				if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.passkeys WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL)`, r.PasskeyID, actor.Details().UserID).Scan(&target); e != nil {
					return unavailable(e)
				}
				if !target {
					return fault(foundation.NotFound, nil)
				}
			}
			options, data, e = v.BeginAssertion(user)
		}
		if e != nil {
			return unavailable(nil)
		}
		var expires time.Time
		e = x.QueryRow(ctx, `INSERT INTO agenteam_account.passkey_operations(id,purpose,user_id,session_id,grant_id,action,target_id,label,expected_version,session_data,created_at,expires_at) SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,n,n+interval '5 minutes' FROM (SELECT clock_timestamp() n) z RETURNING expires_at`, id.String(), r.Purpose, actor.Details().UserID, actor.Details().SessionID, null(r.GrantID), null(r.Action), null(r.PasskeyID), null(r.Label), nullableVersion(r.Purpose == "register", int64(r.ExpectedVersion)), []byte(data)).Scan(&expires)
		if e != nil {
			return unavailable(e)
		}
		out = PasskeyOptions{id.String(), instant(expires), options}
		return nil
	})
	return out, resultError(result)
}

func (s *Service) CompletePasskeyOperation(ctx context.Context, v *pc.Verifier, actor identity.Actor, key foundation.IdempotencyKey, purpose, operationID string, response json.RawMessage) (json.RawMessage, error) {
	if e := passkeyUnavailable(v); e != nil {
		return nil, e
	}
	if !passkeyID(operationID) || len(response) == 0 {
		return nil, invalid()
	}
	if purpose == "register" {
		guard, e := s.state().deps.Authority.mailExclusive(ctx)
		if e != nil {
			return nil, e
		}
		defer guard.release()
	}
	name := "passkey-reauth"
	if purpose == "register" {
		name = "passkey-register"
	}
	semantic, e := json.Marshal([]any{purpose, operationID, response})
	if e != nil {
		return nil, invalid()
	}
	var plan securityEventPlanner
	if purpose == "register" {
		plan = func(ctx context.Context, x postgres.SQLExecutor, cmd commandRecord, u userRecord) (*event.Event, error) {
			operation, e := loadPasskeyOperation(ctx, x, operationID)
			if e != nil {
				return nil, e
			}
			if operation.purpose != "register" || operation.user != actor.Details().UserID || operation.session != actor.Details().SessionID {
				return nil, fault(foundation.InvalidState, nil)
			}
			var method string
			if e = x.QueryRow(ctx, `SELECT method FROM agenteam_account.reauth_grants WHERE id=$1 AND user_id=$2 AND session_id=$3`, operation.grant, cmd.user, cmd.session).Scan(&method); e != nil {
				return nil, field("/grant_id", "INVALID_OR_EXPIRED")
			}
			if method != "mail_recovery" {
				return nil, nil
			}
			value, e := s.planOtherSessionsEvent(ctx, x, cmd, u, c.PasskeysRecovered)
			return &value, e
		}
	}
	out, e := s.securityCommandWithEvent(ctx, actor, key, name, semantic, plan, func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor, cmd c.CommandID) (any, error) {
		operation, e := loadPasskeyOperation(ctx, x, operationID)
		if e != nil {
			return nil, e
		}
		if operation.user != actor.Details().UserID || operation.session != actor.Details().SessionID || purpose == "register" && operation.purpose != "register" || purpose != "register" && operation.purpose != "manage_passkeys" && operation.purpose != "change_email" {
			return nil, fault(foundation.InvalidState, nil)
		}
		if e = livePasskeyOperation(ctx, x, operation); e != nil {
			return nil, fault(foundation.InvalidState, nil)
		}
		user, e := loadPasskeyUser(ctx, x, actor.Details().UserID)
		if e != nil {
			return nil, e
		}
		var receipt any
		var version int64
		if e = x.QueryRow(ctx, `SELECT passkey_version FROM agenteam_account.users WHERE id=$1`, actor.Details().UserID).Scan(&version); e != nil {
			return nil, unavailable(e)
		}
		if purpose == "register" {
			if version != operation.expected {
				return nil, fault(foundation.VersionConflict, nil)
			}
			method, e := s.consumeReauthGrant(ctx, x, actor, operation.grant, "manage_passkeys", "register", "", operation.id)
			if e != nil {
				return nil, e
			}
			if len(user.Credentials) >= 20 && method != "mail_recovery" {
				return nil, fault(foundation.InvalidState, nil)
			}
			cred, e := v.FinishRegistration(user, operation.data, response)
			if e != nil {
				return passkeyCredentialRejection(ctx, x, operation.id)
			}
			id, e := foundation.NewID[struct{}]()
			if e != nil {
				return nil, unavailable(e)
			}
			if method == "mail_recovery" {
				if e = revokeOtherSessionsAndResets(ctx, x, actor.Details().UserID, actor.Details().SessionID); e != nil {
					return nil, e
				}
				if _, e = x.Exec(ctx, `UPDATE agenteam_account.passkeys SET revoked_at=clock_timestamp() WHERE user_id=$1 AND revoked_at IS NULL`, actor.Details().UserID); e != nil {
					return nil, unavailable(e)
				}
			}
			var created time.Time
			if e = x.QueryRow(ctx, `INSERT INTO agenteam_account.passkeys(id,user_id,credential_id,public_data,label) VALUES($1,$2,$3,$4,$5) RETURNING created_at`, id.String(), actor.Details().UserID, cred.ID, []byte(cred.PublicData), operation.label).Scan(&created); e != nil {
				return nil, accountConflict(e)
			}
			if e = x.QueryRow(ctx, `UPDATE agenteam_account.users SET passkey_version=passkey_version+1 WHERE id=$1 RETURNING passkey_version`, actor.Details().UserID).Scan(&version); e != nil {
				return nil, unavailable(e)
			}
			receipt = PasskeyReceipt{foundation.Version(version), id.String(), instant(created)}
		} else {
			var usedID string
			lookup := func(rawID, handle []byte) (pc.User, error) {
				if len(handle) > 0 && !bytes.Equal(handle, user.ID) {
					return pc.User{}, invalid()
				}
				if e := x.QueryRow(ctx, `SELECT id::text FROM agenteam_account.passkeys WHERE credential_id=$1 AND user_id=$2 AND revoked_at IS NULL`, rawID, actor.Details().UserID).Scan(&usedID); e != nil {
					return pc.User{}, invalid()
				}
				return user, nil
			}
			_, updated, e := v.FinishAssertion(lookup, operation.data, response)
			if e != nil {
				return passkeyCredentialRejection(ctx, x, operation.id)
			}
			if _, e = x.Exec(ctx, `UPDATE agenteam_account.passkeys SET public_data=$2,version=version+1,last_used_at=clock_timestamp() WHERE id=$1`, usedID, []byte(updated.PublicData)); e != nil {
				return nil, unavailable(e)
			}
			receipt, e = s.issueReauthGrant(ctx, x, actor, operation.purpose, operation.action, operation.target, "passkey", usedID)
			if e != nil {
				return nil, e
			}
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_account.passkey_operations SET consumed_command_id=$2 WHERE id=$1`, operation.id, cmd.String()); e != nil {
			return nil, unavailable(e)
		}
		if e = s.securityAudit(ctx, tx, actor, name, actor.Details().UserID, cmd.String(), foundation.Version(version)); e != nil {
			return nil, e
		}
		return receipt, nil
	})
	if e != nil {
		return nil, e
	}
	var rejection struct {
		Rejected string `json:"rejected"`
	}
	if json.Unmarshal(out, &rejection) == nil && rejection.Rejected == "invalid_credential" {
		return nil, field("/credential", "INVALID")
	}
	return out, nil
}

func (s *Service) ChangePasskey(ctx context.Context, actor identity.Actor, key foundation.IdempotencyKey, id, grant, label string, expected foundation.Version, remove bool) (json.RawMessage, error) {
	if !passkeyID(id) || !passkeyID(grant) || expected.Validate() != nil {
		return nil, invalid()
	}
	guard, e := s.state().deps.Authority.mailExclusive(ctx)
	if e != nil {
		return nil, e
	}
	defer guard.release()
	name, action := "passkey-rename", "rename"
	if remove {
		name, action = "passkey-delete", "delete"
	} else {
		var e error
		label, e = passkeyLabel(label)
		if e != nil {
			return nil, e
		}
	}
	semantic, e := json.Marshal([]any{id, grant, label, expected, remove})
	if e != nil {
		return nil, invalid()
	}
	return s.securityCommand(ctx, actor, key, name, semantic, func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor, cmd c.CommandID) (any, error) {
		var version int64
		if e := x.QueryRow(ctx, `SELECT passkey_version FROM agenteam_account.users WHERE id=$1`, actor.Details().UserID).Scan(&version); e != nil {
			return nil, unavailable(e)
		}
		if version != int64(expected) {
			return nil, fault(foundation.VersionConflict, nil)
		}
		if _, e := s.consumeReauthGrant(ctx, x, actor, grant, "manage_passkeys", action, id, cmd.String()); e != nil {
			return nil, e
		}
		var found bool
		if e := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.passkeys WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL)`, id, actor.Details().UserID).Scan(&found); e != nil {
			return nil, unavailable(e)
		}
		if !found {
			return nil, fault(foundation.NotFound, nil)
		}
		var created time.Time
		if remove {
			_, e = x.Exec(ctx, `UPDATE agenteam_account.passkeys SET revoked_at=clock_timestamp() WHERE id=$1`, id)
		} else {
			_, e = x.Exec(ctx, `UPDATE agenteam_account.passkeys SET label=$2 WHERE id=$1`, id, label)
		}
		if e != nil {
			return nil, unavailable(e)
		}
		if e = x.QueryRow(ctx, `UPDATE agenteam_account.users SET passkey_version=passkey_version+1 WHERE id=$1 RETURNING passkey_version,clock_timestamp()`, actor.Details().UserID).Scan(&version, &created); e != nil {
			return nil, unavailable(e)
		}
		if e = s.securityAudit(ctx, tx, actor, name, actor.Details().UserID, cmd.String(), foundation.Version(version)); e != nil {
			return nil, e
		}
		return PasskeyReceipt{foundation.Version(version), id, instant(created)}, nil
	})
}
