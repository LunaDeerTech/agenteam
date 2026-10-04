package account

import (
	"context"
	"errors"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

func (a *Authority) CheckAppendInTx(ctx context.Context, tx foundation.Tx, entry ac.Entry, key ac.AppendKey) error {
	if entry.Validate() != nil || key.Validate() != nil {
		return invalid()
	}
	f := entry.Fields()
	actor := f.Actor.Details()
	if f.Scope.Details().Kind != identity.System || key.Details().Producer != ac.ProducerFor(f.Action) {
		return fault(foundation.Forbidden, nil)
	}
	x, e := a.state().store.InTx(tx)
	if e != nil {
		return unavailable(e)
	}
	if actor.Kind == identity.Human {
		if e = a.RequireCurrentSession(ctx, tx, f.Actor); e != nil {
			return e
		}
		m, e := f.Metadata.AccountFields()
		if e != nil {
			return fault(foundation.Forbidden, e)
		}
		if m.InitiatorID != "" && m.InitiatorID != actor.UserID {
			return fault(foundation.Forbidden, nil)
		}
		switch f.Action {
		case ac.SMTPDeliveryRetry:
			if _, e = a.AuthorizeSystem(ctx, tx, f.Actor, identity.Mutate); e != nil {
				return e
			}
			return a.checkMailRetryAuditInTx(ctx, tx, entry, key)
		case ac.AccountLogout:
			if m.UserID != actor.UserID || m.SessionID != actor.SessionID {
				return fault(foundation.Forbidden, nil)
			}
		case ac.AccountPasswordChange, ac.AccountProfileUpdate, ac.AccountAvatarUpdate:
			if m.UserID != actor.UserID {
				return fault(foundation.Forbidden, nil)
			}
		case ac.AccountInviteCreate, ac.AccountInviteRevoke, ac.AccountSettingsUpdate, ac.SMTPSettingsUpdate, ac.SMTPTestRequest:
			if _, e = a.AuthorizeSystem(ctx, tx, f.Actor, identity.Mutate); e != nil {
				return e
			}
			return a.checkHumanAuditResource(ctx, x, f)
		default:
			return fault(foundation.Forbidden, nil)
		}
		return nil
	}
	if actor.Kind != identity.Service {
		return fault(foundation.Forbidden, nil)
	}
	if f.Action == ac.SecretCreate || f.Action == ac.SecretDelete {
		return a.checkSecretAudit(ctx, tx, x, f, key)
	}
	if !ac.AccountAction(f.Action) || actor.CauseRef != key.Details().CauseRef {
		return fault(foundation.Forbidden, nil)
	}
	m, e := f.Metadata.AccountFields()
	if e != nil {
		return e
	}
	switch actor.ServiceName {
	case identity.AccountBootstrap:
		if f.Action != ac.AccountBootstrap || f.Outcome != ac.Success {
			return fault(foundation.Forbidden, nil)
		}
		if e = a.state().store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{configLock("account-directory", foundation.Exclusive), userLock(m.UserID, foundation.Exclusive)}); e != nil {
			return unavailable(e)
		}
		var exists bool
		e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.bootstrap WHERE operation_id=$1 AND user_id=$2)`, actor.CauseRef, m.UserID).Scan(&exists)
		if e != nil {
			return unavailable(e)
		}
		if !exists {
			return fault(foundation.Forbidden, nil)
		}
		return nil
	case identity.AccountAuth:
		if f.Action == ac.AccountLogin || f.Action == ac.AccountPasswordResetRequest {
			var commandID, kind, outcome, user string
			e = x.QueryRow(ctx, `SELECT command_id::text,kind,outcome,coalesce(user_id::text,'') FROM agenteam_account.auth_attempts WHERE id=$1`, actor.CauseRef).Scan(&commandID, &kind, &outcome, &user)
			if errors.Is(e, pgx.ErrNoRows) {
				return fault(foundation.Forbidden, nil)
			}
			if e != nil {
				return unavailable(e)
			}
			if actor.CauseRef != m.AttemptID || user != m.UserID || f.Action == ac.AccountLogin && kind != "login" || f.Action == ac.AccountPasswordResetRequest && kind != "reset_request" || outcome != string(f.Outcome) {
				return fault(foundation.Forbidden, nil)
			}
			r, e := loadCommand(ctx, x, commandID, false)
			if e != nil {
				return e
			}
			if e = a.state().store.RequireHeldLocks(ctx, tx, commandLocks(r)); e != nil {
				return unavailable(e)
			}
			if r.attempt != m.AttemptID || f.Action == ac.AccountLogin && r.user != m.UserID || f.Action == ac.AccountLogin && f.Outcome == ac.Success && r.session != m.SessionID {
				return fault(foundation.Forbidden, nil)
			}
			return nil
		}
		if f.Action != ac.AccountInviteRedeem && f.Action != ac.AccountPasswordResetComplete {
			return fault(foundation.Forbidden, nil)
		}
		r, e := loadCommand(ctx, x, actor.CauseRef, false)
		if e != nil {
			return e
		}
		if e = a.state().store.RequireHeldLocks(ctx, tx, commandLocks(r)); e != nil {
			return unavailable(e)
		}
		if r.phase != "committed" || r.user != m.UserID || f.Action == ac.AccountInviteRedeem && (r.name != "invite-redeem" || r.resource != m.InvitationID) || f.Action == ac.AccountPasswordResetComplete && (r.name != "reset-complete" || r.resource != m.ResetID) {
			return fault(foundation.Forbidden, nil)
		}
		return nil
	case identity.AccountMail:
		return a.checkMailDeliveryAuditInTx(ctx, tx, entry, key)
	default:
		return fault(foundation.Forbidden, nil)
	}
}
func (a *Authority) checkHumanAuditResource(ctx context.Context, x postgres.SQLExecutor, f ac.EntryFields) error {
	m, _ := f.Metadata.AccountFields()
	var exists bool
	var e error
	switch f.Action {
	case ac.AccountInviteCreate:
		e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.invitations i JOIN agenteam_account.commands c ON c.resource_id=i.id WHERE i.id=$1 AND c.user_id=$2 AND c.command_name='invite-create' AND c.phase='committed')`, m.InvitationID, f.Actor.Details().UserID).Scan(&exists)
	case ac.AccountInviteRevoke:
		e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.commands WHERE command_name='invite-revoke' AND resource_id=$1 AND user_id=$2 AND phase='committed')`, m.InvitationID, f.Actor.Details().UserID).Scan(&exists)
	case ac.AccountSettingsUpdate:
		e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.account_settings WHERE id=$1 AND version=$2)`, f.Resource.Details().ID, int64(m.Version)).Scan(&exists)
	case ac.SMTPSettingsUpdate:
		e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.smtp_settings WHERE id=$1 AND version=$2)`, f.Resource.Details().ID, int64(m.Version)).Scan(&exists)
	case ac.SMTPTestRequest:
		e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.delivery_intents WHERE job_id=$1 AND kind='test' AND initiator_id=$2)`, m.JobID, f.Actor.Details().UserID).Scan(&exists)
	}
	if e != nil {
		return unavailable(e)
	}
	if !exists {
		return fault(foundation.Forbidden, nil)
	}
	return nil
}
func (a *Authority) checkSecretAudit(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor, f ac.EntryFields, key ac.AppendKey) error {
	actor := f.Actor.Details()
	if f.Outcome != ac.Success {
		return fault(foundation.Forbidden, nil)
	}
	if actor.ServiceName == identity.AccountAuth && f.Action == ac.SecretCreate {
		r, e := loadCommand(ctx, x, actor.CauseRef, false)
		if e != nil {
			return e
		}
		if e = a.state().store.RequireHeldLocks(ctx, tx, commandLocks(r)); e != nil {
			return unavailable(e)
		}
		if r.secretDigest != key.Details().CauseRef || key.Details().Ordinal != 0 {
			return fault(foundation.Forbidden, nil)
		}
		if r.name == "login" && r.phase == "committed" && r.responseRef == f.Resource.Details().ID {
			return nil
		}
		var matches bool
		switch r.name {
		case "invite-create":
			e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.invitations WHERE id=$1 AND material_ref=$2)`, r.resource, f.Resource.Details().ID).Scan(&matches)
		case "reset-request":
			e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.password_resets WHERE id=$1 AND material_ref=$2)`, r.resource, f.Resource.Details().ID).Scan(&matches)
		default:
			return fault(foundation.Forbidden, nil)
		}
		if e != nil {
			return unavailable(e)
		}
		if matches {
			return nil
		}
	}
	if actor.ServiceName == identity.AccountMaintenance && f.Action == ac.SecretDelete {
		if e := a.state().store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{recordLock(actor.CauseRef)}); e != nil {
			return unavailable(e)
		}
		var exists bool
		e := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.material_cleanup WHERE id=$1 AND credential_id=$2 AND secret_command_digest=$3 AND phase IN ('delete','completed'))`, actor.CauseRef, f.Resource.Details().ID, key.Details().CauseRef).Scan(&exists)
		if e != nil {
			return unavailable(e)
		}
		if exists && key.Details().Ordinal == 0 {
			return nil
		}
	}
	return fault(foundation.Forbidden, nil)
}
func (a *Authority) CheckServiceLookup(ctx context.Context, actor identity.Actor, key ac.AppendKey) error {
	if actor.Validate() != nil || actor.Details().Kind != identity.Service || actor.Details().ProjectID != "" || key.Validate() != nil {
		return invalid()
	}
	d := actor.Details()
	var exists bool
	var e error
	s := a.state().store
	switch d.ServiceName {
	case identity.AccountBootstrap:
		if key.Details().Producer != ac.AccountProducer || key.Details().CauseRef != d.CauseRef {
			return fault(foundation.Forbidden, nil)
		}
		e = s.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.bootstrap WHERE operation_id=$1)`, d.CauseRef).Scan(&exists)
	case identity.AccountAuth:
		if key.Details().Producer == ac.SecretProducer {
			e = s.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.commands WHERE id=$1 AND secret_command_digest=$2)`, d.CauseRef, key.Details().CauseRef).Scan(&exists)
		} else if key.Details().Producer == ac.AccountProducer && key.Details().CauseRef == d.CauseRef {
			e = s.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.auth_attempts WHERE id=$1) OR EXISTS(SELECT 1 FROM agenteam_account.commands WHERE id=$1)`, d.CauseRef).Scan(&exists)
		}
	case identity.AccountMaintenance:
		if key.Details().Producer == ac.SecretProducer {
			e = s.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.material_cleanup WHERE id=$1 AND secret_command_digest=$2)`, d.CauseRef, key.Details().CauseRef).Scan(&exists)
		}
	case identity.AccountMail:
		if key.Details().Producer == ac.AccountMailProducer && key.Details().CauseRef == d.CauseRef {
			e = s.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.mail_attempts WHERE id=$1)`, d.CauseRef).Scan(&exists)
		}
	}
	if e != nil {
		return unavailable(e)
	}
	if !exists {
		return fault(foundation.Forbidden, nil)
	}
	return nil
}

var _ ac.AccountAuthority = (*Authority)(nil)
