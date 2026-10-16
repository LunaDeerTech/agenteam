package account

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/account/passkeycrypto"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type PasskeyItem struct {
	ID         string              `json:"id"`
	Label      string              `json:"label"`
	CreatedAt  foundation.Instant  `json:"created_at"`
	LastUsedAt *foundation.Instant `json:"last_used_at"`
}
type PasskeyList struct {
	Version                     foundation.Version `json:"version"`
	EmailVerified               bool               `json:"email_verified"`
	EmailInitializationRequired bool               `json:"email_initialization_required"`
	Items                       []PasskeyItem      `json:"items"`
}
type PasskeyOptions struct {
	OperationID string             `json:"operation_id"`
	ExpiresAt   foundation.Instant `json:"expires_at"`
	PublicKey   json.RawMessage    `json:"public_key"`
}
type ReauthGrant struct {
	GrantID   string             `json:"grant_id"`
	Purpose   string             `json:"purpose"`
	ExpiresAt foundation.Instant `json:"expires_at"`
}
type PasskeyReceipt struct {
	Version   foundation.Version `json:"version"`
	ID        string             `json:"id"`
	CreatedAt foundation.Instant `json:"created_at"`
}
type passkeyOperation struct {
	id, purpose, browser, user, session, grant, action, target, label, command string
	expected                                                                   int64
	data                                                                       json.RawMessage
	expires                                                                    time.Time
	failures                                                                   int
}

func passkeyID(raw string) bool { _, e := foundation.ParseID[struct{}](raw); return e == nil }
func passkeyLabel(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if !utf8.ValidString(v) || len(v) > 256 || utf8.RuneCountInString(v) < 1 || utf8.RuneCountInString(v) > 64 {
		return "", field("/label", "INVALID")
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return "", field("/label", "INVALID")
		}
	}
	return v, nil
}
func passkeyUnavailable(v *pc.Verifier) error {
	if v == nil || !v.Capabilities().Available {
		return fault(foundation.InvalidState, nil)
	}
	return nil
}
func loadPasskeyUser(ctx context.Context, x postgres.SQLExecutor, id string) (pc.User, error) {
	u, e := loadUser(ctx, x, id)
	if e != nil {
		return pc.User{}, e
	}
	out := pc.User{ID: []byte(id), Name: u.user.Email, DisplayName: u.user.DisplayName, Credentials: []pc.Credential{}}
	rows, e := x.Query(ctx, `SELECT credential_id,public_data FROM agenteam_account.passkeys WHERE user_id=$1 AND revoked_at IS NULL ORDER BY id`, id)
	if e != nil {
		return out, unavailable(e)
	}
	defer rows.Close()
	for rows.Next() {
		var cred pc.Credential
		if e = rows.Scan(&cred.ID, &cred.PublicData); e != nil {
			return out, unavailable(e)
		}
		out.Credentials = append(out.Credentials, cred)
	}
	return out, portError(rows.Err())
}
func loadPasskeyOperation(ctx context.Context, x postgres.SQLExecutor, id string) (passkeyOperation, error) {
	var o passkeyOperation
	e := x.QueryRow(ctx, `SELECT id::text,purpose,coalesce(browser_id::text,''),coalesce(user_id::text,''),coalesce(session_id::text,''),coalesce(grant_id::text,''),coalesce(action,''),coalesce(target_id::text,''),coalesce(label,''),coalesce(expected_version,0),session_data,expires_at,coalesce(consumed_command_id::text,''),failures FROM agenteam_account.passkey_operations WHERE id=$1`, id).Scan(&o.id, &o.purpose, &o.browser, &o.user, &o.session, &o.grant, &o.action, &o.target, &o.label, &o.expected, &o.data, &o.expires, &o.command, &o.failures)
	if errors.Is(e, pgx.ErrNoRows) {
		return o, fault(foundation.Unauthenticated, nil)
	}
	return o, portError(e)
}
func livePasskeyOperation(ctx context.Context, x postgres.SQLExecutor, o passkeyOperation) error {
	var live bool
	if e := x.QueryRow(ctx, `SELECT clock_timestamp()<$1`, o.expires).Scan(&live); e != nil {
		return unavailable(e)
	}
	if !live || o.failures >= 5 || o.command != "" {
		return fault(foundation.Unauthenticated, nil)
	}
	return nil
}
func (s *Service) Passkeys(ctx context.Context, actor identity.Actor) (PasskeyList, error) {
	out := PasskeyList{Items: []PasskeyItem{}}
	op, e := s.begin(ctx, false)
	if e != nil {
		return out, e
	}
	defer s.finish(op)
	cause, e := recoveryCause("passkey-list")
	if e != nil {
		return out, e
	}
	st := s.state()
	result := st.store.WithinTx(op.ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, []foundation.LockRequest{userLock(actor.Details().UserID, foundation.Shared)}); e != nil {
			return unavailable(e)
		}
		if e := st.deps.Authority.RequireCurrentSession(ctx, tx, actor); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		var version int64
		if e = x.QueryRow(ctx, `SELECT passkey_version,email_verified_at IS NOT NULL,email_verified_at IS NULL AND EXISTS(SELECT 1 FROM agenteam_account.bootstrap b WHERE b.user_id=users.id) FROM agenteam_account.users WHERE id=$1`, actor.Details().UserID).Scan(&version, &out.EmailVerified, &out.EmailInitializationRequired); e != nil {
			return unavailable(e)
		}
		out.Version = foundation.Version(version)
		rows, e := x.Query(ctx, `SELECT id::text,label,created_at,last_used_at FROM agenteam_account.passkeys WHERE user_id=$1 AND revoked_at IS NULL ORDER BY created_at,id`, actor.Details().UserID)
		if e != nil {
			return unavailable(e)
		}
		defer rows.Close()
		for rows.Next() {
			var item PasskeyItem
			var created time.Time
			var used *time.Time
			if e = rows.Scan(&item.ID, &item.Label, &created, &used); e != nil {
				return unavailable(e)
			}
			item.CreatedAt = instant(created)
			if used != nil {
				v := instant(*used)
				item.LastUsedAt = &v
			}
			out.Items = append(out.Items, item)
		}
		return portError(rows.Err())
	})
	return out, resultError(result)
}

// securityCommand keeps current authorization, one-use proof consumption,
// the business mutation and its recoverable receipt in the same transaction.
// The semantic MAC includes input secrets without storing an offline verifier.
func (s *Service) securityCommand(ctx context.Context, actor identity.Actor, key foundation.IdempotencyKey, name string, semantic []byte, change func(context.Context, foundation.Tx, postgres.SQLExecutor, c.CommandID) (any, error)) (json.RawMessage, error) {
	if actor.Validate() != nil || actor.Details().Kind != identity.Human || key.Validate() != nil {
		return nil, invalid()
	}
	op, e := s.begin(ctx, false)
	if e != nil {
		return nil, e
	}
	defer s.finish(op)
	identityKey, e := foundation.NewCommandIdentity("account.security", []string{actor.Details().UserID}, name, key)
	if e != nil {
		return nil, invalid()
	}
	if e = s.trackMutation(identityKey, op); e != nil {
		return nil, e
	}
	cause, e := foundation.NewCommandsCause(identityKey)
	if e != nil {
		return nil, e
	}
	id, e := foundation.NewID[c.Command]()
	if e != nil {
		return nil, unavailable(e)
	}
	st := s.state()
	var out json.RawMessage
	mac := func(kid string) ([]byte, error) {
		return st.keys.mac(kid, "command-v1", []byte(name), []byte(actor.Details().UserID), []byte(actor.Details().SessionID), semantic)
	}
	result := st.store.WithinTx(op.ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		locks := []foundation.LockRequest{commandLock(identityKey), configLock("account-directory", foundation.Exclusive), configLock("account-security", foundation.Shared), configLock("account-mail", foundation.Exclusive), userLock(actor.Details().UserID, foundation.Exclusive)}
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		if e := st.deps.Authority.RequireCurrentSession(ctx, tx, actor); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		old, e := loadCommand(ctx, x, string(digest([]byte(identityKey.Canonical()))), true)
		if e == nil {
			if e = s.checkCommand(old, identityKey, mac); e != nil {
				return e
			}
			if old.phase != "committed" {
				return fault(foundation.InvalidState, nil)
			}
			return portError(x.QueryRow(ctx, `SELECT receipt FROM agenteam_account.security_receipts WHERE command_id=$1`, old.id.String()).Scan(&out))
		}
		if !hasFaultCode(e, foundation.NotFound) {
			return e
		}
		sig, e := mac(st.keys.current())
		if e != nil {
			return e
		}
		defer clear(sig)
		_, e = x.Exec(ctx, `INSERT INTO agenteam_account.commands(id,namespace,owner_id,command_key,command_name,identity_digest,semantic_kid,semantic_mac,actor_kind,user_id,session_id,origin_process_id,resource_id,phase) VALUES($1,'account.security',$2,$3,$4,$5,$6,$7,'human',$2,$8,$9,$2,'planned')`, id.String(), actor.Details().UserID, string(key), name, string(digest([]byte(identityKey.Canonical()))), st.keys.current(), sig, actor.Details().SessionID, st.process.String())
		if e != nil {
			return unavailable(e)
		}
		receipt, e := change(ctx, tx, x, id)
		if e != nil {
			return e
		}
		out, e = json.Marshal(receipt)
		if e != nil {
			return unavailable(e)
		}
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_account.security_receipts(command_id,receipt) VALUES($1,$2)`, id.String(), out); e != nil {
			return unavailable(e)
		}
		return completeCommand(ctx, x, id.String(), 1)
	})
	return out, resultError(result)
}
