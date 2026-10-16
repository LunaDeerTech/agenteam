package account

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type emailAuthorization struct {
	live                                bool
	id, user, purpose, target, consumed string
	verifier                            []byte
}

func operatorTokenVerifier(raw []byte) []byte {
	h := sha256.New()
	_, _ = h.Write([]byte("agenteam.account.operator-authorization.v1\x00"))
	_, _ = h.Write(raw)
	return h.Sum(nil)
}
func readEmailAuthorization(ctx context.Context, x postgres.SQLExecutor, material sc.SecretMaterial, user string) (emailAuthorization, error) {
	var a emailAuthorization
	var id string
	var proof []byte
	e := material.Use(func(b []byte) error {
		parts := strings.Split(string(b), ".")
		if len(parts) != 2 || !passkeyID(parts[0]) {
			return invalid()
		}
		raw, e := base64.RawURLEncoding.Strict().DecodeString(parts[1])
		if e != nil || len(raw) != 32 {
			return invalid()
		}
		defer clear(raw)
		id = parts[0]
		proof = operatorTokenVerifier(raw)
		return nil
	})
	if e != nil {
		return a, field("/authorization", "INVALID_OR_EXPIRED")
	}
	defer clear(proof)
	e = x.QueryRow(ctx, `SELECT id::text,user_id::text,purpose,target_email,token_verifier,coalesce(consumed_request_id::text,''),revoked_at IS NULL AND expires_at>clock_timestamp() FROM agenteam_account.operator_authorizations WHERE id=$1 AND user_id=$2`, id, user).Scan(&a.id, &a.user, &a.purpose, &a.target, &a.verifier, &a.consumed, &a.live)
	if errors.Is(e, pgx.ErrNoRows) || e == nil && subtle.ConstantTimeCompare(a.verifier, proof) != 1 {
		return a, field("/authorization", "INVALID_OR_EXPIRED")
	}
	return a, portError(e)
}

func (s *Service) RequestEmailProof(ctx context.Context, f EmailProofRequest) (EmailProofReceipt, error) {
	var out EmailProofReceipt
	if f.Actor.Validate() != nil || f.Actor.Details().Kind != identity.Human || f.Key.Validate() != nil {
		return out, invalid()
	}
	resend := f.RequestID != ""
	if resend && !passkeyID(f.RequestID) {
		return out, field("/code", "INVALID_OR_EXPIRED")
	}
	op, e := s.begin(ctx, false)
	if e != nil {
		return out, e
	}
	defer s.finish(op)
	ctx = op.ctx
	st := s.state()
	current, e := st.deps.Authority.current(ctx, foundation.Tx{}, f.Actor)
	if e != nil {
		return out, e
	}
	var a emailAuthorization
	var prior emailRequest
	if resend {
		prior, e = loadEmailRequest(ctx, st.store, f.RequestID)
		if e != nil {
			return out, e
		}
		if f.Kind == "initialization" && prior.kind == "operator_recovery" {
			f.Kind = prior.kind
		}
		if prior.user != f.Actor.Details().UserID || prior.session != f.Actor.Details().SessionID || prior.kind != f.Kind {
			return out, field("/code", "INVALID_OR_EXPIRED")
		}
		f.Email = prior.target
	} else {
		switch f.Kind {
		case "passkey_recovery":
			f.Email = current.user.user.Email
		case "email_change":
			f.Email, e = NormalizeEmail(f.Email)
			if e != nil {
				return out, e
			}
			if !passkeyID(f.GrantID) || f.ExpectedVersion.Validate() != nil {
				return out, invalid()
			}
		case "initialization":
			a, e = readEmailAuthorization(ctx, st.store, f.Authorization, f.Actor.Details().UserID)
			if e != nil {
				return out, e
			}
			f.Email = a.target
			if a.purpose == "recover_account" {
				f.Kind = "operator_recovery"
			}
		default:
			return out, invalid()
		}
	}
	name := emailRequestName(f.Kind)
	if resend {
		name = "email-code-resend"
	}
	if name == "" {
		return out, invalid()
	}
	key, e := foundation.NewCommandIdentity("account.email", []string{f.Actor.Details().UserID}, name, f.Key)
	if e != nil {
		return out, e
	}
	if e = s.trackMutation(key, op); e != nil {
		return out, e
	}
	semanticEmail := f.Email
	if f.Kind == "passkey_recovery" && !resend {
		semanticEmail = ""
	}
	semantic, _ := json.Marshal([]string{f.Kind, f.RequestID, f.GrantID, semanticEmail, f.ExpectedVersion.String(), a.id})
	macFor := func(kid string) ([]byte, error) {
		parts := [][]byte{[]byte(name), []byte(f.Actor.Details().UserID), []byte(f.Actor.Details().SessionID), semantic}
		var signature []byte
		if !resend && f.Kind != "email_change" {
			err := f.Password.Use(func(password []byte) error {
				var err error
				signature, err = st.keys.mac(kid, "command-v1", append(parts, password)...)
				return err
			})
			return signature, portError(err)
		}
		return st.keys.mac(kid, "command-v1", parts...)
	}
	// Replay receipts before expensive password work; the command MAC includes
	// its original session and input, and the session is checked above.
	old, e := s.lookupMutation(ctx, key, macFor, f.Actor, c.BrowserIdentity{}, false)
	if e == nil {
		if e = s.checkCommand(old, key, macFor); e != nil {
			return out, e
		}
		if old.phase == "committed" {
			var raw []byte
			if e = st.store.QueryRow(ctx, `SELECT receipt FROM agenteam_account.security_receipts WHERE command_id=$1`, old.id.String()).Scan(&raw); e != nil {
				return out, unavailable(e)
			}
			e = json.Unmarshal(raw, &out)
			return out, portError(e)
		}
	} else if !hasFaultCode(e, foundation.NotFound) {
		return out, e
	}
	if !resend && a.id != "" && (!a.live || a.consumed != "") {
		return out, field("/authorization", "INVALID_OR_EXPIRED")
	}
	if !resend && f.Kind != "email_change" {
		ok, e := st.hasher.Verify(ctx, f.Password, current.user.phc)
		if e != nil {
			return out, e
		}
		if !ok {
			return out, field("/password", "INVALID")
		}
	}
	requestID, e := foundation.NewID[struct{}]()
	if e != nil {
		return out, unavailable(e)
	}
	commandID, e := foundation.NewID[c.Command]()
	if e != nil {
		return out, unavailable(e)
	}
	jobID, e := foundation.NewID[c.MailJob]()
	if e != nil {
		return out, unavailable(e)
	}
	cause, e := foundation.NewCommandsCause(key)
	if e != nil {
		return out, e
	}
	locks := []foundation.LockRequest{commandLock(key), configLock("account-directory", foundation.Exclusive), configLock("account-security", foundation.Shared), configLock("account-mail", foundation.Exclusive), userLock(f.Actor.Details().UserID, foundation.Exclusive)}
	var cmd commandRecord
	var desired emailRequest
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		if e := st.deps.Authority.RequireCurrentSession(ctx, tx, f.Actor); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		previous, e := loadCommand(ctx, x, string(digest([]byte(key.Canonical()))), true)
		if e == nil {
			if e = s.checkCommand(previous, key, macFor); e != nil {
				return e
			}
			if previous.phase == "committed" {
				cmd = previous
				return nil
			}
			if previous.phase != "planned" {
				return fault(foundation.InvalidState, nil)
			}
		} else if !hasFaultCode(e, foundation.NotFound) {
			return e
		}
		u, e := loadUser(ctx, x, f.Actor.Details().UserID)
		if e != nil {
			return e
		}
		if u.passwordVersion != current.user.passwordVersion || u.user.Version != current.user.user.Version {
			return fault(foundation.ResourceBusy, nil)
		}
		var verified bool
		var pkv int64
		if e = x.QueryRow(ctx, `SELECT email_verified_at IS NOT NULL,passkey_version FROM agenteam_account.users WHERE id=$1`, f.Actor.Details().UserID).Scan(&verified, &pkv); e != nil {
			return unavailable(e)
		}
		cfg, e := loadSMTP(ctx, x)
		if e != nil {
			return e
		}
		if !cfg.Configured || !cfg.Enabled {
			return fault(foundation.InvalidState, nil)
		}
		if e = emailRate(ctx, x, f.Actor.Details().UserID, f.Kind, true); e != nil {
			return e
		}
		if resend {
			prior, e = loadEmailRequest(ctx, x, f.RequestID)
			if e != nil {
				return e
			}
			if !prior.live || prior.phase != "pending" || prior.userVersion != int64(u.user.Version) || prior.passwordVersion != int64(u.passwordVersion) || prior.passkeyVersion != pkv || prior.deliveries >= 5 || prior.attempts >= 5 {
				return field("/code", "INVALID_OR_EXPIRED")
			}
			desired = prior
			desired.version++
			desired.deliveries++
		} else {
			var bootstrap bool
			if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.bootstrap WHERE user_id=$1)`, f.Actor.Details().UserID).Scan(&bootstrap); e != nil {
				return unavailable(e)
			}
			if f.Kind == "passkey_recovery" && !verified && bootstrap || f.Kind == "initialization" && verified {
				return fault(foundation.InvalidState, nil)
			}
			if f.Kind == "email_change" && (u.user.Version != f.ExpectedVersion || f.Email == u.user.Email) {
				return fault(foundation.VersionConflict, nil)
			}
			desired = emailRequest{id: requestID.String(), user: f.Actor.Details().UserID, session: f.Actor.Details().SessionID, kind: f.Kind, original: u.user.Email, target: f.Email, userVersion: int64(u.user.Version), passwordVersion: int64(u.passwordVersion), passkeyVersion: pkv, grant: f.GrantID, authorization: a.id, phase: "pending", version: 1, deliveries: 1}
		}
		old, e := loadCommand(ctx, x, string(digest([]byte(key.Canonical()))), true)
		if e == nil {
			if e = s.checkCommand(old, key, macFor); e != nil {
				return e
			}
			if old.phase != "planned" && old.phase != "committed" {
				return fault(foundation.InvalidState, nil)
			}
			cmd = old
			desired.id = old.resource
			return nil
		}
		if !hasFaultCode(e, foundation.NotFound) {
			return e
		}
		sig, e := macFor(st.keys.current())
		if e != nil {
			return e
		}
		defer clear(sig)
		_, e = x.Exec(ctx, `INSERT INTO agenteam_account.commands(id,namespace,owner_id,command_key,command_name,identity_digest,semantic_kid,semantic_mac,actor_kind,user_id,session_id,origin_process_id,attempt_id,resource_id,password_version,expected_version,phase) VALUES($1,'account.email',$2,$3,$4,$5,$6,$7,'human',$2,$8,$9,$10,$11,$12,$13,'planned')`, commandID.String(), f.Actor.Details().UserID, string(f.Key), name, string(digest([]byte(key.Canonical()))), st.keys.current(), sig, f.Actor.Details().SessionID, st.process.String(), jobID.String(), desired.id, desired.passwordVersion, desired.userVersion)
		if e != nil {
			return unavailable(e)
		}
		cmd, e = loadCommand(ctx, x, commandID.String(), false)
		return e
	})
	if e = resultError(result); e != nil {
		return out, e
	}
	if cmd.phase == "committed" {
		var raw []byte
		if e = st.store.QueryRow(ctx, `SELECT receipt FROM agenteam_account.security_receipts WHERE command_id=$1`, cmd.id.String()).Scan(&raw); e != nil {
			return out, unavailable(e)
		}
		e = json.Unmarshal(raw, &out)
		return out, portError(e)
	}
	var remove *linkRemoval
	if !resend {
		var id string
		e = st.store.QueryRow(ctx, `SELECT id::text FROM agenteam_account.email_requests WHERE user_id=$1 AND kind=$2 AND phase='pending'`, desired.user, desired.kind).Scan(&id)
		if e == nil {
			prior, e = loadEmailRequest(ctx, st.store, id)
		} else if errors.Is(e, pgx.ErrNoRows) {
			e = nil
			prior = emailRequest{}
		}
		if e != nil {
			return out, portError(e)
		}
	}
	if prior.ref != "" {
		l, e := loadEmailLink(ctx, st.store, c.EmailCodeToken, prior.id)
		if e != nil {
			return out, e
		}
		p, e := s.planLinkRemoval(ctx, l)
		if e != nil {
			return out, e
		}
		remove = &p
	}
	material, code, e := newEmailCodeMaterial(f.Kind)
	if e != nil {
		return out, e
	}
	defer material.Destroy()
	defer clear(code)
	desired.kid = st.keys.current()
	desired.mac, e = s.emailCodeMAC(desired.kid, desired, code)
	if e != nil {
		return out, e
	}
	defer clear(desired.mac)
	prepared, usage, usagePlan, e := s.prepareEmailMaterial(ctx, cmd, f.Actor, material)
	if e != nil {
		return out, e
	}
	cmd, e = loadCommand(ctx, st.store, cmd.id.String(), false)
	if e != nil {
		return out, e
	}
	actor, evt, appendPlan, e := s.prepareDelivery(ctx, cmd)
	if e != nil {
		return out, e
	}
	all := append(append([]foundation.LockRequest{}, locks...), commandLocks(cmd)...)
	all = append(all, prepared.RequiredLocks()...)
	all = append(all, usagePlan.RequiredLocks()...)
	all = append(all, appendPlan.Locks()...)
	if remove != nil {
		all = append(all, remove.locks()...)
	}
	guard, e := st.deps.Authority.mailExclusive(ctx)
	if e != nil {
		return out, e
	}
	defer guard.release()
	result = st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, all); e != nil {
			return unavailable(e)
		}
		if e := st.deps.Authority.RequireCurrentSession(ctx, tx, f.Actor); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		current, e := loadCommand(ctx, x, cmd.id.String(), false)
		if e != nil {
			return e
		}
		if e = s.checkCommand(current, key, macFor); e != nil {
			return e
		}
		if current.phase == "committed" {
			var raw []byte
			if e = x.QueryRow(ctx, `SELECT receipt FROM agenteam_account.security_receipts WHERE command_id=$1`, current.id.String()).Scan(&raw); e != nil {
				return unavailable(e)
			}
			return portError(json.Unmarshal(raw, &out))
		}
		if current.phase != "planned" || current.plannedRef != prepared.Ref().Details().ID.String() || current.secretBinding != cmd.secretBinding {
			return fault(foundation.ResourceBusy, nil)
		}
		u, e := loadUser(ctx, x, desired.user)
		if e != nil {
			return e
		}
		var pkv int64
		if e = x.QueryRow(ctx, `SELECT passkey_version FROM agenteam_account.users WHERE id=$1`, desired.user).Scan(&pkv); e != nil {
			return unavailable(e)
		}
		if int64(u.user.Version) != desired.userVersion || int64(u.passwordVersion) != desired.passwordVersion || pkv != desired.passkeyVersion || u.user.Email != desired.original {
			return fault(foundation.ResourceBusy, nil)
		}
		cfg, e := loadSMTP(ctx, x)
		if e != nil {
			return e
		}
		if !cfg.Configured || !cfg.Enabled {
			return fault(foundation.InvalidState, nil)
		}
		if e = emailRate(ctx, x, desired.user, desired.kind, true); e != nil {
			return e
		}
		if !resend && desired.kind == "email_change" {
			if _, e = s.consumeReauthGrant(ctx, x, f.Actor, desired.grant, "change_email", "", "", cmd.id.String()); e != nil {
				return e
			}
		}
		if !resend && desired.authorization != "" {
			authorized, e := readEmailAuthorization(ctx, x, f.Authorization, desired.user)
			if e != nil {
				return e
			}
			if authorized.id != desired.authorization || authorized.target != desired.target || authorized.consumed != "" || !authorized.live {
				return field("/authorization", "INVALID_OR_EXPIRED")
			}
		}
		if !resend {
			var active string
			e = x.QueryRow(ctx, `SELECT id::text FROM agenteam_account.email_requests WHERE user_id=$1 AND kind=$2 AND phase='pending'`, desired.user, desired.kind).Scan(&active)
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				return unavailable(e)
			}
			if active != prior.id {
				return fault(foundation.ResourceBusy, nil)
			}
		}
		if remove != nil {
			if e = s.retireEmailMaterialInTx(ctx, tx, *remove, guard); e != nil {
				return e
			}
		}
		if resend {
			old, e := loadEmailRequest(ctx, x, desired.id)
			if e != nil {
				return e
			}
			if old.version+1 != desired.version || old.phase != "pending" || !old.live {
				return field("/code", "INVALID_OR_EXPIRED")
			}
			_, e = x.Exec(ctx, `UPDATE agenteam_account.email_requests SET version=$2,deliveries=deliveries+1,code_kid=$3,code_mac=$4,material_ref=$5,last_delivery_at=clock_timestamp() WHERE id=$1`, desired.id, desired.version, desired.kid, desired.mac, prepared.Ref().Details().ID.String())
		} else {
			_, e = x.Exec(ctx, `UPDATE agenteam_account.email_requests SET phase='cancelled',material_ref=NULL,completed_at=clock_timestamp() WHERE user_id=$1 AND kind=$2 AND phase='pending'`, desired.user, desired.kind)
			if e != nil {
				return unavailable(e)
			}
			_, e = x.Exec(ctx, `INSERT INTO agenteam_account.email_requests(id,user_id,session_id,kind,original_email,target_email,user_version,password_version,passkey_version,grant_id,authorization_id,phase,code_kid,code_mac,material_ref,created_at,expires_at,last_delivery_at) SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'pending',$12,$13,$14,n,n+interval '10 minutes',n FROM (SELECT clock_timestamp() n) z`, desired.id, desired.user, desired.session, desired.kind, desired.original, desired.target, desired.userVersion, desired.passwordVersion, desired.passkeyVersion, null(desired.grant), null(desired.authorization), desired.kid, desired.mac, prepared.Ref().Details().ID.String())
		}
		if e != nil {
			return accountConflict(e)
		}
		if desired.authorization != "" && !resend {
			if _, e = x.Exec(ctx, `UPDATE agenteam_account.operator_authorizations SET consumed_request_id=$2 WHERE id=$1 AND consumed_request_id IS NULL`, desired.authorization, desired.id); e != nil {
				return unavailable(e)
			}
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_account.email_rate_limits SET deliveries=deliveries+1,last_delivery_at=clock_timestamp() WHERE user_id=$1 AND kind=$2`, desired.user, desired.kind); e != nil {
			return unavailable(e)
		}
		if e = completeCommand(ctx, x, cmd.id.String(), 1); e != nil {
			return e
		}
		if _, e = st.deps.Secrets.ApplyServiceWriteInTx(ctx, tx, prepared); e != nil {
			return portError(e)
		}
		if _, e = st.deps.Secrets.ApplyUsageInTx(ctx, tx, usage, usagePlan); e != nil {
			return portError(e)
		}
		if e = s.insertDelivery(ctx, x, cmd); e != nil {
			return e
		}
		if _, e = st.deps.Events.AppendEventInTx(ctx, tx, actor, evt, appendPlan); e != nil {
			return portError(e)
		}
		r, e := loadEmailRequest(ctx, x, desired.id)
		if e != nil {
			return e
		}
		out = emailReceipt(r)
		raw, e := json.Marshal(out)
		if e != nil {
			return unavailable(e)
		}
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_account.security_receipts(command_id,receipt) VALUES($1,$2)`, cmd.id.String(), raw); e != nil {
			return unavailable(e)
		}
		return s.securityAudit(ctx, tx, f.Actor, name, desired.user, cmd.id.String(), u.user.Version)
	})
	return out, resultError(result)
}
