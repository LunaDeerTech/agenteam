package account

import (
	"context"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func (s *Service) InspectPasswordReset(ctx context.Context, browser c.BrowserIdentity, token c.LinkToken) (c.ResetInspection, error) {
	kind, id, _ := token.Fields()
	if kind != c.PasswordResetToken {
		return c.ResetInspection{}, fault(foundation.ResourceDeleted, nil)
	}
	op, e := s.begin(ctx, false)
	if e != nil {
		return c.ResetInspection{}, e
	}
	defer s.finish(op)
	ctx = op.ctx
	if e = s.requireBrowser(ctx, browser); e != nil {
		return c.ResetInspection{}, e
	}
	link, e := loadLink(ctx, s.state().store, kind, id)
	if e != nil {
		return c.ResetInspection{}, e
	}
	cause, e := recoveryCause("reset-inspect")
	if e != nil {
		return c.ResetInspection{}, e
	}
	var out c.ResetInspection
	r := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{configLock("account-directory", foundation.Shared), userLock(link.user, foundation.Shared), recordLock(id)}); e != nil {
			return unavailable(e)
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		if e = s.validateBrowserInTx(ctx, x, browser); e != nil {
			return e
		}
		now, e := verifyLink(ctx, x, token)
		if e != nil {
			return e
		}
		if now.user != link.user {
			return fault(foundation.ResourceBusy, nil)
		}
		out = c.ResetInspection{Valid: true, ExpiresAt: instant(now.expires)}
		return nil
	})
	if e = resultError(r); e != nil {
		return c.ResetInspection{}, e
	}
	return out, nil
}
func (s *Service) CompletePasswordReset(ctx context.Context, r c.ResetComplete) (c.RedemptionReceipt, error) {
	if r.Validate() != nil {
		return c.RedemptionReceipt{}, invalid()
	}
	op, e := s.begin(ctx, false)
	if e != nil {
		return c.RedemptionReceipt{}, e
	}
	defer s.finish(op)
	ctx = op.ctx
	st := s.state()
	f := r.Fields()
	if nilPort(st.deps.Events) || st.deps.SessionsRevoked.Schema().EventType != c.SessionsRevokedType {
		return c.RedemptionReceipt{}, fault(foundation.DependencyUnbound, nil)
	}
	if e = s.requireBrowser(ctx, f.Browser); e != nil {
		return c.RedemptionReceipt{}, e
	}
	if e = passwordPair(f.Password, f.Confirmation); e != nil {
		return c.RedemptionReceipt{}, e
	}
	_, linkID, token := f.Token.Fields()
	key, e := foundation.NewCommandIdentity("account.reset", []string{f.Browser.ID().String()}, "reset-complete", f.Key)
	if e != nil {
		return c.RedemptionReceipt{}, e
	}
	if e = s.trackMutation(key, op); e != nil {
		return c.RedemptionReceipt{}, e
	}
	macFor := func(kid string) (out []byte, e error) {
		e = token.Use(func(raw []byte) error {
			return f.Password.Use(func(p []byte) error {
				out, e = st.keys.mac(kid, "command-v1", []byte("reset-complete"), []byte(f.Browser.ID().String()), []byte(linkID), raw, p)
				return e
			})
		})
		return out, portError(e)
	}
	old, e := s.lookupMutation(ctx, key, macFor, identity.Actor{}, f.Browser, false)
	if e == nil && old.phase == "committed" {
		return c.RedemptionReceipt{Completed: true}, nil
	}
	if e != nil && !hasFaultCode(e, foundation.NotFound) {
		return c.RedemptionReceipt{}, e
	}
	if e == nil && old.phase != "planned" {
		return c.RedemptionReceipt{}, fault(foundation.InvalidState, nil)
	}
	link, e := verifyLink(ctx, st.store, f.Token)
	if e != nil {
		return c.RedemptionReceipt{}, e
	}
	removal, e := s.planLinkRemoval(ctx, link)
	if e != nil {
		return c.RedemptionReceipt{}, e
	}
	hash, e := st.hasher.Hash(ctx, f.Password)
	if e != nil {
		return c.RedemptionReceipt{}, e
	}
	id, e := foundation.NewID[c.Command]()
	if e != nil {
		return c.RedemptionReceipt{}, unavailable(e)
	}
	mac, e := macFor(st.keys.current())
	if e != nil {
		return c.RedemptionReceipt{}, e
	}
	defer clear(mac)
	cause, e := foundation.NewCommandsCause(key)
	if e != nil {
		return c.RedemptionReceipt{}, e
	}
	locks := append(removal.locks(), commandLock(key), configLock("account-security", foundation.Shared), userLock(link.user, foundation.Exclusive), recordLock(id.String()))
	var cmd commandRecord
	var evt event.Event
	done := false
	planned := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		if e = s.validateBrowserInTx(ctx, x, f.Browser); e != nil {
			return e
		}
		old, e := loadCommand(ctx, x, string(digest([]byte(key.Canonical()))), true)
		if e == nil {
			if e = s.checkCommand(old, key, macFor); e != nil {
				return e
			}
			if old.phase == "committed" {
				done = true
				return nil
			}
			if old.phase != "planned" {
				return fault(foundation.InvalidState, nil)
			}
			cmd = old
		} else if !hasFaultCode(e, foundation.NotFound) {
			return e
		}
		now, e := verifyLink(ctx, x, f.Token)
		if e != nil {
			return e
		}
		if now.ref != link.ref || now.user != link.user {
			return fault(foundation.ResourceBusy, nil)
		}
		u, e := loadUser(ctx, x, link.user)
		if e != nil {
			return e
		}
		if cmd.id.Validate() != nil {
			if _, e = x.Exec(ctx, `INSERT INTO agenteam_account.commands(id,namespace,owner_id,command_key,command_name,identity_digest,semantic_kid,semantic_mac,actor_kind,user_id,browser_id,browser_expires_at,origin_process_id,resource_id,password_version,phase) VALUES($1,'account.reset',$2,$3,'reset-complete',$4,$5,$6,'browser',$7,$2,$8,$9,$10,$11,'planned')`, id.String(), f.Browser.ID().String(), string(f.Key), string(digest([]byte(key.Canonical()))), st.keys.current(), mac, link.user, f.Browser.ExpiresAt().Time(), st.process.String(), linkID, link.passwordVersion); e != nil {
				return unavailable(e)
			}
			cmd, e = loadCommand(ctx, x, id.String(), false)
			if e != nil {
				return e
			}
		}
		if cmd.user != now.user || cmd.resource != now.id || cmd.passwordVersion != now.passwordVersion {
			return fault(foundation.ResourceBusy, nil)
		}
		evt, e = s.planPasswordEvent(ctx, x, cmd, u, c.PasswordWasReset)
		return e
	})
	if e = resultError(planned); e != nil {
		return c.RedemptionReceipt{}, e
	}
	if done {
		return c.RedemptionReceipt{Completed: true}, nil
	}
	actor, e := serviceActor(identity.AccountAuth, cmd.id.String())
	if e != nil {
		return c.RedemptionReceipt{}, e
	}
	plan, e := st.deps.Events.PrepareAppend(ctx, actor, evt)
	if e != nil {
		return c.RedemptionReceipt{}, portError(e)
	}
	locks = append(locks, commandLocks(cmd)...)
	locks = append(locks, plan.Locks()...)
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		if e = s.validateBrowserInTx(ctx, x, f.Browser); e != nil {
			return e
		}
		now, e := loadCommand(ctx, x, cmd.id.String(), false)
		if e != nil {
			return e
		}
		if e = s.checkCommand(now, key, macFor); e != nil {
			return e
		}
		if now.phase == "committed" {
			return nil
		}
		if now.phase != "planned" || commandMapping(now) != commandMapping(cmd) {
			return fault(foundation.ResourceBusy, nil)
		}
		current, e := verifyLink(ctx, x, f.Token)
		if e != nil {
			return e
		}
		if current.ref != link.ref || current.user != link.user || current.passwordVersion != cmd.passwordVersion {
			return fault(foundation.ResourceBusy, nil)
		}
		u, e := loadUser(ctx, x, cmd.user)
		if e != nil {
			return e
		}
		seq := *evt.Header().AggregateSequence
		if u.sequence+1 != seq {
			return fault(foundation.ResourceBusy, nil)
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_account.users SET auth_sequence=$2 WHERE id=$1`, cmd.user, int64(seq)); e != nil {
			return unavailable(e)
		}
		if _, e = st.deps.Events.AppendEventInTx(ctx, tx, actor, evt, plan); e != nil {
			return portError(e)
		}
		if e = completeCommand(ctx, x, cmd.id.String(), int64(u.user.Version+1)); e != nil {
			return e
		}
		if e = s.mutationAudit(ctx, tx, actor, ac.AccountPasswordResetComplete, ac.PasswordResetResource, linkID, cmd.id.String(), ac.AccountMetadataFields{UserID: cmd.user, ResetID: linkID, Version: u.user.Version + 1, Phase: ac.AccountRedeemed}); e != nil {
			return e
		}
		if e = s.removeLinkInTx(ctx, tx, removal); e != nil {
			return e
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='password_reset' WHERE user_id=$1 AND revoked_at IS NULL`, cmd.user); e != nil {
			return unavailable(e)
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_account.users SET password_phc=$2,password_version=password_version+1,version=version+1,initial_password_suggestion=false,updated_at=clock_timestamp() WHERE id=$1`, cmd.user, hash.encoded())
		return portError(e)
	})
	if result.State() == foundation.Unknown {
		now, e := s.lookupMutation(ctx, key, macFor, identity.Actor{}, f.Browser, false)
		if e != nil || now.phase != "committed" {
			return c.RedemptionReceipt{}, confirmationError(result, e)
		}
	} else if e = resultError(result); e != nil {
		return c.RedemptionReceipt{}, e
	}
	return c.RedemptionReceipt{Completed: true}, nil
}
