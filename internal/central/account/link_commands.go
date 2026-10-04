package account

import (
	"context"
	"crypto/subtle"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func verifyLink(ctx context.Context, x postgres.SQLExecutor, t c.LinkToken) (linkRecord, error) {
	kind, id, value := t.Fields()
	if t.Validate() != nil {
		return linkRecord{}, fault(foundation.ResourceDeleted, nil)
	}
	var proof []byte
	e := value.Use(func(b []byte) error { var e error; proof, e = tokenVerifier(kind, b); return e })
	if e != nil {
		return linkRecord{}, fault(foundation.ResourceDeleted, nil)
	}
	defer clear(proof)
	r, e := loadLink(ctx, x, kind, id)
	if e != nil {
		return r, e
	}
	if !r.live || subtle.ConstantTimeCompare(proof, r.verifier) != 1 {
		return r, fault(foundation.ResourceDeleted, nil)
	}
	return r, nil
}

// InspectInvitation needs the anonymous browser proof as well as the link.
// When a current Human is supplied, only that same email may inspect it; the
// link never associates an invitation with another signed-in identity.
func (s *Service) InspectInvitation(ctx context.Context, browser c.BrowserIdentity, token c.LinkToken, current *identity.Actor) (c.InvitationInspection, error) {
	kind, id, _ := token.Fields()
	if kind != c.InvitationToken {
		return c.InvitationInspection{}, fault(foundation.ResourceDeleted, nil)
	}
	op, e := s.begin(ctx, false)
	if e != nil {
		return c.InvitationInspection{}, e
	}
	defer s.finish(op)
	ctx = op.ctx
	if e = s.requireBrowser(ctx, browser); e != nil {
		return c.InvitationInspection{}, e
	}
	locks := []foundation.LockRequest{configLock("account-directory", foundation.Shared), recordLock(id)}
	if current != nil {
		if current.Validate() != nil || current.Details().Kind != identity.Human {
			return c.InvitationInspection{}, invalid()
		}
		locks = append(locks, userLock(current.Details().UserID, foundation.Shared))
	}
	cause, e := recoveryCause("invite-inspect")
	if e != nil {
		return c.InvitationInspection{}, e
	}
	var out c.InvitationInspection
	r := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		if e = s.validateBrowserInTx(ctx, x, browser); e != nil {
			return e
		}
		link, e := verifyLink(ctx, x, token)
		if e != nil {
			return e
		}
		if current != nil {
			if e = s.state().deps.Authority.RequireCurrentSession(ctx, tx, *current); e != nil {
				return e
			}
			u, e := loadUser(ctx, x, current.Details().UserID)
			if e != nil {
				return e
			}
			if u.user.Email != link.email {
				return fault(foundation.Forbidden, nil)
			}
		}
		out = c.InvitationInspection{Email: link.email, ExpiresAt: instant(link.expires)}
		return nil
	})
	if e = resultError(r); e != nil {
		return c.InvitationInspection{}, e
	}
	return out, nil
}
func (s *Service) RedeemInvitation(ctx context.Context, r c.InvitationRedeem) (c.RedemptionReceipt, error) {
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
	if e = s.requireBrowser(ctx, f.Browser); e != nil {
		return c.RedemptionReceipt{}, e
	}
	username, e := NormalizeUsername(f.Username)
	if e != nil {
		return c.RedemptionReceipt{}, e
	}
	if e = ValidateDisplayName(f.DisplayName); e != nil {
		return c.RedemptionReceipt{}, e
	}
	if e = passwordPair(f.Password, f.Confirmation); e != nil {
		return c.RedemptionReceipt{}, e
	}
	_, linkID, token := f.Token.Fields()
	key, e := foundation.NewCommandIdentity("account.invitation", []string{f.Browser.ID().String()}, "invite-redeem", f.Key)
	if e != nil {
		return c.RedemptionReceipt{}, e
	}
	macFor := func(kid string) (out []byte, e error) {
		e = token.Use(func(token []byte) error {
			return f.Password.Use(func(p []byte) error {
				out, e = st.keys.mac(kid, "command-v1", []byte("invite-redeem"), []byte(f.Browser.ID().String()), []byte(linkID), token, []byte(username), []byte(f.DisplayName), p)
				return e
			})
		})
		return out, portError(e)
	}
	old, e := s.lookupMutation(ctx, key, macFor, identity.Actor{}, f.Browser, false)
	if e == nil {
		if old.phase == "committed" {
			return c.RedemptionReceipt{Completed: true}, nil
		}
		return c.RedemptionReceipt{}, fault(foundation.InvalidState, nil)
	}
	if !hasFaultCode(e, foundation.NotFound) {
		return c.RedemptionReceipt{}, e
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
	uid, e := foundation.NewID[identity.User]()
	if e != nil {
		return c.RedemptionReceipt{}, unavailable(e)
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
	locks := append(removal.locks(), commandLock(key), configLock("account-security", foundation.Shared), userLock(uid.String(), foundation.Exclusive), recordLock(id.String()))
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
		now, e := verifyLink(ctx, x, f.Token)
		if e != nil {
			return e
		}
		if now.ref != link.ref {
			return fault(foundation.ResourceBusy, nil)
		}
		var emailTaken, nameTaken bool
		if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.users WHERE email=$1),EXISTS(SELECT 1 FROM agenteam_account.users WHERE username=$2)`, now.email, username).Scan(&emailTaken, &nameTaken); e != nil {
			return unavailable(e)
		}
		if emailTaken {
			return field("/email", "ALREADY_EXISTS")
		}
		if nameTaken {
			return field("/username", "ALREADY_EXISTS")
		}
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_account.users(id,email,username,display_name,role,password_phc,password_version,version,auth_sequence,initial_password_suggestion,theme) VALUES($1,$2,$3,$4,'user',$5,1,1,1,false,'system')`, uid.String(), now.email, username, f.DisplayName, hash.encoded()); e != nil {
			return accountConflict(e)
		}
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_account.commands(id,namespace,owner_id,command_key,command_name,identity_digest,semantic_kid,semantic_mac,actor_kind,user_id,browser_id,browser_expires_at,origin_process_id,resource_id,phase,result_code,result_version,completed_at) VALUES($1,'account.invitation',$2,$3,'invite-redeem',$4,$5,$6,'browser',$7,$2,$8,$9,$10,'committed','COMPLETED',1,clock_timestamp())`, id.String(), f.Browser.ID().String(), string(f.Key), string(digest([]byte(key.Canonical()))), st.keys.current(), mac, uid.String(), f.Browser.ExpiresAt().Time(), st.process.String(), linkID); e != nil {
			return unavailable(e)
		}
		if e = s.removeLinkInTx(ctx, tx, removal); e != nil {
			return e
		}
		actor, e := serviceActor(identity.AccountAuth, id.String())
		if e != nil {
			return e
		}
		return s.mutationAudit(ctx, tx, actor, ac.AccountInviteRedeem, ac.UserResource, uid.String(), id.String(), ac.AccountMetadataFields{UserID: uid.String(), InvitationID: linkID, Version: 1, Phase: ac.AccountRedeemed})
	})
	if result.State() == foundation.Unknown {
		old, e = s.lookupMutation(ctx, key, macFor, identity.Actor{}, f.Browser, false)
		if e != nil || old.phase != "committed" {
			return c.RedemptionReceipt{}, confirmationError(result, e)
		}
	} else if e = resultError(result); e != nil {
		return c.RedemptionReceipt{}, e
	}
	return c.RedemptionReceipt{Completed: true}, nil
}
