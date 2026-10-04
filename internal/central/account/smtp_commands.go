package account

import (
	"context"
	"errors"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/jackc/pgx/v5"
)

func (s *Service) TestSMTP(ctx context.Context, f c.SMTPTest) (c.MailJobStatus, error) {
	if f.Actor.Validate() != nil || f.Actor.Details().Kind != identity.Human || f.Key.Validate() != nil {
		return c.MailJobStatus{}, invalid()
	}
	recipient, e := NormalizeEmail(f.Recipient)
	if e != nil {
		return c.MailJobStatus{}, e
	}
	op, e := s.begin(ctx, false)
	if e != nil {
		return c.MailJobStatus{}, e
	}
	defer s.finish(op)
	ctx = op.ctx
	st := s.state()
	if _, e = st.deps.Authority.AuthorizeSystem(ctx, foundation.Tx{}, f.Actor, identity.Mutate); e != nil {
		return c.MailJobStatus{}, e
	}
	key, e := foundation.NewCommandIdentity("account.smtp", []string{f.Actor.Details().UserID}, "smtp-test", f.Key)
	if e != nil {
		return c.MailJobStatus{}, e
	}
	macFor := func(kid string) ([]byte, error) {
		return st.keys.mac(kid, "command-v1", []byte(key.Canonical()), []byte(recipient))
	}
	cmd, e := s.lookupMutation(ctx, key, macFor, f.Actor, c.BrowserIdentity{}, true)
	if e == nil && cmd.phase == "committed" {
		job, _ := parseID[c.MailJob](cmd.attempt)
		return s.GetMailJob(ctx, f.Actor, job)
	}
	if e != nil && !hasFaultCode(e, foundation.NotFound) {
		return c.MailJobStatus{}, e
	}
	id, e := foundation.NewID[c.Command]()
	if e != nil {
		return c.MailJobStatus{}, unavailable(e)
	}
	job, e := foundation.NewID[c.MailJob]()
	if e != nil {
		return c.MailJobStatus{}, unavailable(e)
	}
	mac, e := macFor(st.keys.current())
	if e != nil {
		return c.MailJobStatus{}, e
	}
	defer clear(mac)
	cause, e := foundation.NewCommandsCause(key)
	if e != nil {
		return c.MailJobStatus{}, e
	}
	locks := []foundation.LockRequest{commandLock(key), configLock("account-directory", foundation.Shared), configLock("account-mail", foundation.Exclusive), configLock("account-security", foundation.Shared), userLock(f.Actor.Details().UserID, foundation.Shared), recordLock(id.String()), recordLock(job.String())}
	r := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		if _, e := st.deps.Authority.AuthorizeSystem(ctx, tx, f.Actor, identity.Mutate); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		cfg, e := loadSMTP(ctx, x)
		if e != nil {
			return e
		}
		if !cfg.Configured {
			return fault(foundation.InvalidState, nil)
		}
		old, e := loadCommand(ctx, x, string(digest([]byte(key.Canonical()))), true)
		if e == nil {
			if e = s.checkCommand(old, key, macFor); e != nil {
				return e
			}
			cmd = old
			if cmd.phase == "planned" {
				_, e = x.Exec(ctx, `UPDATE agenteam_account.commands SET expected_version=$2,session_id=$3 WHERE id=$1`, cmd.id.String(), cfg.Version, f.Actor.Details().SessionID)
				if e != nil {
					return unavailable(e)
				}
				cmd, e = loadCommand(ctx, x, cmd.id.String(), false)
			}
			return e
		}
		if !hasFaultCode(e, foundation.NotFound) {
			return e
		}
		_, e = x.Exec(ctx, `INSERT INTO agenteam_account.commands(id,namespace,owner_id,command_key,command_name,identity_digest,semantic_kid,semantic_mac,actor_kind,user_id,session_id,origin_process_id,attempt_id,resource_id,expected_version,phase) VALUES($1,'account.smtp',$2,$3,'smtp-test',$4,$5,$6,'human',$2,$7,$8,$9,$10,$11,'planned')`, id.String(), f.Actor.Details().UserID, string(f.Key), string(digest([]byte(key.Canonical()))), st.keys.current(), mac, f.Actor.Details().SessionID, st.process.String(), job.String(), cfg.ID, cfg.Version)
		if e != nil {
			return unavailable(e)
		}
		cmd, e = loadCommand(ctx, x, id.String(), false)
		return e
	})
	if r.State() == foundation.Unknown {
		cmd, e = s.lookupMutation(ctx, key, macFor, f.Actor, c.BrowserIdentity{}, true)
		if e != nil {
			return c.MailJobStatus{}, confirmationError(r, e)
		}
	} else if e = resultError(r); e != nil {
		return c.MailJobStatus{}, e
	}
	if cmd.phase == "committed" {
		job, _ := parseID[c.MailJob](cmd.attempt)
		return s.GetMailJob(ctx, f.Actor, job)
	}
	if cmd.phase != "planned" {
		return c.MailJobStatus{}, fault(foundation.InvalidState, nil)
	}
	actor, evt, plan, e := s.prepareDelivery(ctx, cmd)
	if e != nil {
		return c.MailJobStatus{}, e
	}
	locks = append(locks, commandLocks(cmd)...)
	locks = append(locks, plan.Locks()...)
	r = st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		if _, e := st.deps.Authority.AuthorizeSystem(ctx, tx, f.Actor, identity.Mutate); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
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
		cfg, e := loadSMTP(ctx, x)
		if e != nil {
			return e
		}
		if !cfg.Configured || cfg.Version != now.expectedVersion {
			return fault(foundation.ResourceBusy, nil)
		}
		if e = completeCommand(ctx, x, cmd.id.String(), 1); e != nil {
			return e
		}
		if e = s.insertDelivery(ctx, x, cmd, recipient); e != nil {
			return e
		}
		if _, e = st.deps.Events.AppendEventInTx(ctx, tx, actor, evt, plan); e != nil {
			return portError(e)
		}
		return s.mutationAudit(ctx, tx, f.Actor, ac.SMTPTestRequest, ac.MailJobResource, cmd.attempt, cmd.id.String(), ac.AccountMetadataFields{Version: 1, JobID: cmd.attempt, InitiatorID: cmd.user, Phase: ac.AccountAccepted, Channel: ac.SMTPChannel})
	})
	if r.State() == foundation.Unknown {
		saved, e := s.lookupMutation(ctx, key, macFor, f.Actor, c.BrowserIdentity{}, true)
		if e != nil || saved.phase != "committed" {
			return c.MailJobStatus{}, confirmationError(r, e)
		}
	} else if e = resultError(r); e != nil {
		return c.MailJobStatus{}, e
	}
	job, _ = parseID[c.MailJob](cmd.attempt)
	return s.GetMailJob(ctx, f.Actor, job)
}
func (s *Service) GetMailJob(ctx context.Context, actor identity.Actor, id c.JobID) (c.MailJobStatus, error) {
	if actor.Validate() != nil || actor.Details().Kind != identity.Human || id.Validate() != nil {
		return c.MailJobStatus{}, invalid()
	}
	op, e := s.begin(ctx, false)
	if e != nil {
		return c.MailJobStatus{}, e
	}
	defer s.finish(op)
	ctx = op.ctx
	cause, e := recoveryCause("mail-query")
	if e != nil {
		return c.MailJobStatus{}, e
	}
	out := c.MailJobStatus{JobID: id}
	r := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{userLock(actor.Details().UserID, foundation.Shared), recordLock(id.String())}); e != nil {
			return unavailable(e)
		}
		if _, e := s.state().deps.Authority.AuthorizeSystem(ctx, tx, actor, identity.Read); e != nil {
			return e
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		var attempts, version int64
		e = x.QueryRow(ctx, `SELECT coalesce(j.phase,'enqueue_pending'),coalesce(j.attempts,0),coalesce(j.version,1),coalesce(j.reason,'') FROM agenteam_account.delivery_intents i LEFT JOIN agenteam_account.mail_jobs j ON j.intent_id=i.id WHERE i.job_id=$1`, id.String()).Scan(&out.Phase, &attempts, &version, &out.Reason)
		if errors.Is(e, pgx.ErrNoRows) {
			return fault(foundation.NotFound, nil)
		}
		if e != nil {
			return unavailable(e)
		}
		if out.Phase == "processing" {
			out.Phase = "unknown"
		}
		out.Attempts = foundation.Progress(attempts)
		out.Version = foundation.Version(version)
		return nil
	})
	if e = resultError(r); e != nil {
		return c.MailJobStatus{}, e
	}
	return out, nil
}
