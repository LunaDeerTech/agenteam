package account

import (
	"context"
	"encoding/json"
	"math"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func retrySemantic(key foundation.CommandIdentity, actor, job string, version int64) ([]byte, error) {
	return json.Marshal(struct {
		Command, Actor, Job string
		Version             int64
	}{key.Canonical(), actor, job, version})
}
func retryLocks(cmd commandRecord, o deliveryOrigin) []foundation.LockRequest {
	l := append(commandLocks(cmd), o.locks()...)
	return append(l, configLock("account-mail", foundation.Exclusive), recordLock(cmd.resource), recordLock(cmd.attempt))
}
func retryMapping(cmd commandRecord, o deliveryOrigin) foundation.Digest {
	b, _ := json.Marshal(struct {
		Command, Origin foundation.Digest
		Job             string
		Expected        int64
	}{commandMapping(cmd), o.binding(), cmd.attempt, cmd.expectedVersion})
	return digest(b)
}
func retryEligible(ctx context.Context, x postgres.SQLExecutor, job string, version int64, o deliveryOrigin) error {
	var current int64
	var phase string
	var hasAttempt, terminal, joined bool
	e := x.QueryRow(ctx, `SELECT j.version,j.phase,a.id IS NOT NULL,coalesce(a.terminal,false),coalesce(a.io_joined,false) FROM agenteam_account.mail_jobs j LEFT JOIN agenteam_account.mail_attempts a ON a.id=j.current_attempt_id WHERE j.id=$1`, job).Scan(&current, &phase, &hasAttempt, &terminal, &joined)
	if e != nil {
		return portError(e)
	}
	if current != version {
		return fault(foundation.VersionConflict, nil)
	}
	if version == math.MaxInt64 {
		return fault(foundation.InvalidState, nil)
	}
	if phase != "sent" && phase != "failed" && phase != "cancelled" && phase != "unknown" || hasAttempt && (!terminal || !joined) || phase == "unknown" && !hasAttempt {
		return fault(foundation.ResourceBusy, nil)
	}
	busy, e := originHasOtherCycle(ctx, x, o.root.ID, job)
	if e != nil {
		return e
	}
	if busy {
		return fault(foundation.ResourceBusy, nil)
	}
	return originLive(ctx, x, o)
}
func retryReceipt(ctx context.Context, x postgres.SQLExecutor, cmd commandRecord, o deliveryOrigin) error {
	if cmd.phase != "committed" || cmd.resultVersion != 1 {
		return fault(foundation.ResourceBusy, nil)
	}
	created, e := loadDeliveryOrigin(ctx, x, cmd.id.String())
	if e != nil {
		return e
	}
	if created.root.ID != o.root.ID || created.source.Job != cmd.attempt || created.sourceCommand.resource != cmd.resource {
		return fault(foundation.Forbidden, nil)
	}
	var version int64
	if e = x.QueryRow(ctx, `SELECT version FROM agenteam_account.mail_jobs WHERE id=$1`, cmd.resource).Scan(&version); e != nil {
		return unavailable(e)
	}
	if version <= cmd.expectedVersion {
		return fault(foundation.ResourceBusy, nil)
	}
	return nil
}

func (s *Service) RetryMailJob(ctx context.Context, q c.MailJobRetry) (c.MailJobStatus, error) {
	empty := c.MailJobStatus{}
	if q.Actor.Validate() != nil || q.Actor.Details().Kind != identity.Human || q.Key.Validate() != nil || q.JobID.Validate() != nil || q.ExpectedVersion.Validate() != nil {
		return empty, invalid()
	}
	op, e := s.begin(ctx, false)
	if e != nil {
		return empty, e
	}
	defer s.finish(op)
	ctx = op.ctx
	st := s.state()
	if _, e = st.deps.Authority.AuthorizeSystem(ctx, foundation.Tx{}, q.Actor, identity.Mutate); e != nil {
		return empty, e
	}
	key, e := foundation.NewCommandIdentity("account.mail-retry", []string{q.Actor.Details().UserID}, "mail-retry", q.Key)
	if e != nil {
		return empty, e
	}
	semantic, e := retrySemantic(key, q.Actor.Details().UserID, q.JobID.String(), int64(q.ExpectedVersion))
	if e != nil {
		return empty, unavailable(e)
	}
	macFor := func(kid string) ([]byte, error) { return st.keys.mac(kid, "command-v1", semantic) }
	old, e := s.lookupMutation(ctx, key, macFor, q.Actor, c.BrowserIdentity{}, true)
	if e == nil && old.phase == "committed" {
		job, _ := parseID[c.MailJob](old.attempt)
		return s.GetMailJob(ctx, q.Actor, job)
	}
	if e != nil && !hasFaultCode(e, foundation.NotFound) {
		return empty, e
	}
	origin, e := loadJobOrigin(ctx, st.store, q.JobID.String())
	if e != nil {
		return empty, e
	}
	id, e := foundation.NewID[c.Command]()
	if e != nil {
		return empty, unavailable(e)
	}
	job, e := foundation.NewID[c.MailJob]()
	if e != nil {
		return empty, unavailable(e)
	}
	mac, e := macFor(st.keys.current())
	if e != nil {
		return empty, e
	}
	defer clear(mac)
	cmd := commandRecord{id: id, identity: key, name: "mail-retry", user: q.Actor.Details().UserID, session: q.Actor.Details().SessionID, resource: q.JobID.String(), attempt: job.String(), expectedVersion: int64(q.ExpectedVersion)}
	locks := retryLocks(cmd, origin)
	if old.id.Validate() == nil {
		locks = append(locks, commandLocks(old)...)
	}
	cause, e := foundation.NewCommandsCause(key)
	if e != nil {
		return empty, e
	}
	confirm := func(original foundation.CommitResult) (commandRecord, error) {
		var saved commandRecord
		r := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
			if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
				return unavailable(e)
			}
			if _, e := st.deps.Authority.AuthorizeSystem(ctx, tx, q.Actor, identity.Mutate); e != nil {
				return e
			}
			x, e := st.store.InTx(tx)
			if e != nil {
				return unavailable(e)
			}
			saved, e = loadCommand(ctx, x, string(digest([]byte(key.Canonical()))), true)
			if e != nil {
				return e
			}
			if e = s.checkCommand(saved, key, macFor); e != nil {
				return e
			}
			now, e := loadJobOrigin(ctx, x, q.JobID.String())
			if e != nil {
				return e
			}
			if now.binding() != origin.binding() {
				return fault(foundation.ResourceBusy, nil)
			}
			if saved.phase == "committed" {
				return retryReceipt(ctx, x, saved, now)
			}
			return nil
		})
		if e := resultError(r); e != nil {
			return saved, confirmationError(original, e)
		}
		return saved, nil
	}
	r := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		if _, e := st.deps.Authority.AuthorizeSystem(ctx, tx, q.Actor, identity.Mutate); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		existing, e := loadCommand(ctx, x, string(digest([]byte(key.Canonical()))), true)
		if e == nil {
			if e = s.checkCommand(existing, key, macFor); e != nil {
				return e
			}
			if existing.id != id && existing.id != old.id {
				return fault(foundation.ResourceBusy, nil)
			}
			cmd = existing
			if cmd.phase == "planned" && cmd.session != q.Actor.Details().SessionID {
				if _, e = x.Exec(ctx, `UPDATE agenteam_account.commands SET session_id=$2 WHERE id=$1`, cmd.id.String(), q.Actor.Details().SessionID); e != nil {
					return unavailable(e)
				}
				cmd, e = loadCommand(ctx, x, cmd.id.String(), false)
			}
			return e
		}
		if !hasFaultCode(e, foundation.NotFound) {
			return e
		}
		now, e := loadJobOrigin(ctx, x, q.JobID.String())
		if e != nil {
			return e
		}
		if now.binding() != origin.binding() {
			return fault(foundation.ResourceBusy, nil)
		}
		if e = retryEligible(ctx, x, q.JobID.String(), int64(q.ExpectedVersion), now); e != nil {
			return e
		}
		_, e = x.Exec(ctx, `INSERT INTO agenteam_account.commands(id,namespace,owner_id,command_key,command_name,identity_digest,semantic_kid,semantic_mac,actor_kind,user_id,session_id,origin_process_id,attempt_id,resource_id,expected_version,phase) VALUES($1,'account.mail-retry',$2,$3,'mail-retry',$4,$5,$6,'human',$2,$7,$8,$9,$10,$11,'planned')`, id.String(), q.Actor.Details().UserID, string(q.Key), string(digest([]byte(key.Canonical()))), st.keys.current(), mac, q.Actor.Details().SessionID, st.process.String(), job.String(), q.JobID.String(), int64(q.ExpectedVersion))
		if e != nil {
			return unavailable(e)
		}
		cmd, e = loadCommand(ctx, x, id.String(), false)
		return e
	})
	if r.State() == foundation.Unknown {
		cmd, e = confirm(r)
		if e != nil {
			return empty, e
		}
	} else if e = resultError(r); e != nil {
		return empty, e
	}
	if cmd.phase == "committed" {
		job, _ = parseID[c.MailJob](cmd.attempt)
		return s.GetMailJob(ctx, q.Actor, job)
	}
	if cmd.phase != "planned" {
		return empty, fault(foundation.InvalidState, nil)
	}
	actor, evt, plan, e := s.prepareRetryDelivery(ctx, cmd, origin)
	if e != nil {
		return empty, e
	}
	locks = append(retryLocks(cmd, origin), plan.Locks()...)
	r = st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		if _, e := st.deps.Authority.AuthorizeSystem(ctx, tx, q.Actor, identity.Mutate); e != nil {
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
			return retryReceipt(ctx, x, now, origin)
		}
		o, e := loadJobOrigin(ctx, x, q.JobID.String())
		if e != nil {
			return e
		}
		if now.phase != "planned" || retryMapping(now, o) != retryMapping(cmd, origin) {
			return fault(foundation.ResourceBusy, nil)
		}
		if e = retryEligible(ctx, x, q.JobID.String(), int64(q.ExpectedVersion), o); e != nil {
			return e
		}
		if e = checkDeliveryCapacity(ctx, x); e != nil {
			return e
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_account.mail_jobs SET version=version+1 WHERE id=$1`, q.JobID.String()); e != nil {
			return unavailable(e)
		}
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_account.delivery_intents(id,origin_intent_id,job_id,kind,link_id,initiator_id,recipient,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, cmd.id.String(), o.root.ID, cmd.attempt, o.root.Kind, null(o.root.Link), cmd.user, null(o.root.Recipient), cmd.created); e != nil {
			return unavailable(e)
		}
		if e = completeCommand(ctx, x, cmd.id.String(), 1); e != nil {
			return e
		}
		if _, e = st.deps.Events.AppendEventInTx(ctx, tx, actor, evt, plan); e != nil {
			return portError(e)
		}
		return s.mutationAudit(ctx, tx, q.Actor, ac.SMTPDeliveryRetry, ac.MailJobResource, q.JobID.String(), cmd.id.String(), ac.AccountMetadataFields{Version: q.ExpectedVersion, JobID: q.JobID.String(), InitiatorID: cmd.user, Phase: ac.AccountAccepted})
	})
	if r.State() == foundation.Unknown {
		saved, e := confirm(r)
		if e != nil {
			return empty, e
		}
		if saved.phase != "committed" {
			return empty, confirmationError(r, nil)
		}
	} else if e = resultError(r); e != nil {
		return empty, e
	}
	job, _ = parseID[c.MailJob](cmd.attempt)
	return s.GetMailJob(ctx, q.Actor, job)
}
