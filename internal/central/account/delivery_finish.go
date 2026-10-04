package account

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type mailRelease struct {
	request sc.UsageRequest
	deps    sc.UsageDependencies
}

func (p *deliveryPort) FinishDelivery(ctx context.Context, a c.DeliveryAttempt, completion c.DeliveryCompletion) error {
	if !a.IssuedBy(p.issuer) || completion.Outcome().Validate() != nil {
		return invalid()
	}
	if e := p.runtime.RequireJoined(a, completion); e != nil {
		return portError(e)
	}
	p.mu.Lock()
	if p.finishes == nil {
		p.finishes = map[string]pendingMailFinish{}
	}
	p.finishes[a.Details().AttemptID.String()] = pendingMailFinish{a, completion}
	p.mu.Unlock()
	op, e := p.service.begin(ctx, true)
	if e != nil {
		return e
	}
	defer p.service.finish(op)
	m, e := loadMail(op.ctx, p.service.state().store, a.Details().JobID.String(), a.Details().AttemptID.String())
	if e != nil {
		return e
	}
	if e = p.match(a, m); e != nil {
		return e
	}
	e = p.finishMail(op.ctx, m, completion.Outcome())
	if e == nil {
		p.mu.Lock()
		delete(p.finishes, m.Attempt)
		p.mu.Unlock()
	}
	return e
}

type pendingMailFinish struct {
	attempt    c.DeliveryAttempt
	completion c.DeliveryCompletion
}

// The caller has already established actual local join or exact process death.
// This durable fence is required even if no Secret lease was ever acquired.
func (p *deliveryPort) stopAcquisition(ctx context.Context, m mailRecord) error {
	r := p.withRecord(ctx, m, nil, func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor, now mailRecord) error {
		if mailBinding(now) != mailBinding(m) {
			return fault(foundation.ResourceBusy, nil)
		}
		if !now.Terminal && (now.CurrentAttempt != now.Attempt || now.JobFence != now.Fence) {
			return fault(foundation.ResourceBusy, nil)
		}
		if now.Joined {
			return nil
		}
		_, e := x.Exec(ctx, `UPDATE agenteam_account.mail_attempts SET io_joined=true WHERE id=$1 AND NOT terminal`, m.Attempt)
		return portError(e)
	})
	if r.State() != foundation.Unknown {
		return resultError(r)
	}
	confirmed := p.withRecord(ctx, m, nil, func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor, now mailRecord) error {
		if mailBinding(now) != mailBinding(m) || !now.Joined {
			return fault(foundation.ResourceBusy, nil)
		}
		return nil
	})
	if e := resultError(confirmed); e != nil {
		return confirmationError(r, e)
	}
	return nil
}
func (p *deliveryPort) releasePlan(ctx context.Context, m mailRecord, purpose sc.Purpose) (*mailRelease, string, error) {
	ref, lease := mailSlot(m, purpose)
	if lease == "" {
		if ref != "" {
			return nil, "", unavailable(nil)
		}
		return nil, "", nil
	}
	var candidates []string
	if ref != "" {
		candidates = []string{ref}
	} else {
		var e error
		candidates, e = p.legacyCandidates(ctx, m, purpose)
		if e != nil {
			return nil, "", e
		}
		if len(candidates) == 0 {
			return nil, "", fault(foundation.ResourceBusy, nil)
		}
	}

	var found *mailRelease
	var chosen string
	var first error
	for _, candidate := range candidates {
		copy := m
		if purpose == sc.SMTP {
			copy.CredentialRef = candidate
		} else {
			copy.TokenRef = candidate
		}
		q, e := deliveryUsage(copy, purpose, sc.ReleaseLeaseUsage)
		if e != nil {
			return nil, "", e
		}
		d, e := p.service.state().deps.Secrets.DiscoverUsage(ctx, q)
		if e != nil {
			// Only the Secret service's top-level missing lease is absence. A
			// wrapped provider error or Apply failure is never consumed here.
			if se, ok := e.(*secret.Error); ok && se.Code() == secret.NotFound && ref != "" {
				return nil, "", nil
			}
			if ref != "" {
				return nil, "", portError(e)
			}
			if first == nil {
				first = portError(e)
			}
			continue
		}
		if found != nil {
			return nil, "", fault(foundation.ResourceBusy, nil)
		}
		found = &mailRelease{q, d}
		chosen = candidate
	}
	if found == nil {
		if first != nil {
			return nil, "", first
		}
		return nil, "", fault(foundation.ResourceBusy, nil)
	}
	return found, chosen, nil
}
func (p *deliveryPort) finishMail(ctx context.Context, m mailRecord, out c.DeliveryOutcome) error {
	if out.Validate() != nil {
		return invalid()
	}
	if e := p.stopAcquisition(ctx, m); e != nil {
		return e
	}
	now, e := loadMail(ctx, p.service.state().store, m.Job, m.Attempt)
	if e != nil {
		return e
	}
	if now.Terminal {
		return nil
	}
	if !now.Joined || mailBinding(now) != mailBinding(m) {
		return fault(foundation.ResourceBusy, nil)
	}
	m = now
	var releases []mailRelease
	var locks []foundation.LockRequest
	refs := map[sc.Purpose]string{}
	for _, purpose := range []sc.Purpose{sc.System, sc.SMTP} {
		plan, ref, e := p.releasePlan(ctx, m, purpose)
		if e != nil {
			return e
		}
		refs[purpose] = ref
		if plan != nil {
			releases = append(releases, *plan)
			locks = append(locks, plan.deps.RequiredLocks()...)
		}
	}
	r := p.withRecord(ctx, m, locks, func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor, now mailRecord) error {
		if mailBinding(now) != mailBinding(m) || !now.Joined {
			return fault(foundation.ResourceBusy, nil)
		}
		if now.Terminal {
			return nil
		}
		for _, plan := range releases {
			if _, e := p.service.state().deps.Secrets.ApplyUsageInTx(ctx, tx, plan.request, plan.deps); e != nil {
				return portError(e)
			}
		}
		// Legacy facts are filled only with the original exact lease's proven
		// ref, in this same final transaction. New claim refs never change.
		_, e := x.Exec(ctx, `UPDATE agenteam_account.mail_attempts SET token_ref=coalesce(token_ref,$2),credential_ref=coalesce(credential_ref,$3),phase='closed',result=$4,terminal=true,completed_at=clock_timestamp() WHERE id=$1 AND io_joined`, m.Attempt, null(refs[sc.System]), null(refs[sc.SMTP]), string(out.Result))
		if e != nil {
			return unavailable(e)
		}
		phase := string(out.Result)
		var next any
		cfg, e := loadSMTP(ctx, x)
		if e != nil {
			return e
		}
		if out.Retryable && m.Attempts < cfg.Retries+1 && m.Attempts < 6 {
			var databaseNow time.Time
			if e = x.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&databaseNow); e != nil {
				return unavailable(e)
			}
			at := databaseNow.Add(mailRetryDelay(m.Job, m.Attempts, cfg.Interval))
			valid := m.Kind == string(c.TestDelivery) && cfg.Configured && cfg.Enabled
			if m.Kind != string(c.TestDelivery) {
				link, err := loadLink(ctx, x, c.TokenKind(m.Kind), m.Link)
				if err == nil {
					valid = link.live && at.Before(link.expires)
				} else {
					var f *foundation.Fault
					if !errors.As(err, &f) || f.Code != foundation.ResourceDeleted {
						return err
					}
				}
			}
			if valid {
				phase = "retry_wait"
				next = at
			}
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_account.mail_jobs SET phase=$2,reason=$3,version=version+1,next_at=coalesce($4::timestamptz,next_at),completed_at=CASE WHEN $2='retry_wait' THEN NULL ELSE clock_timestamp() END WHERE id=$1 AND current_attempt_id=$5 AND fence=$6`, m.Job, phase, string(out.Reason), next, m.Attempt, m.Fence)
		if e != nil {
			return unavailable(e)
		}
		return p.deliveryAudit(ctx, tx, m, out)
	})
	if r.State() != foundation.Unknown {
		return resultError(r)
	}
	confirmed := p.withRecord(ctx, m, nil, func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor, now mailRecord) error {
		if !now.Joined || !now.Terminal || now.Result != string(out.Result) {
			return fault(foundation.ResourceBusy, nil)
		}
		return nil
	})
	if e = resultError(confirmed); e != nil {
		return confirmationError(r, e)
	}
	return nil
}
func mailRetryDelay(job string, attempts, base int64) time.Duration {
	seconds := base
	for i := int64(1); i < attempts && seconds < 3600; i++ {
		seconds *= 2
	}
	if seconds > 3600 {
		seconds = 3600
	}
	h := sha256.Sum256([]byte(job))
	jitter := int64(binary.BigEndian.Uint64(h[:8]) % uint64(seconds*100+1))
	return time.Duration(seconds)*time.Second + time.Duration(jitter)*time.Millisecond
}
func (p *deliveryPort) deliveryAudit(ctx context.Context, tx foundation.Tx, m mailRecord, out c.DeliveryOutcome) error {
	outcome, phase, reason, e := mailAuditProjection(string(out.Result), string(out.Reason))
	if e != nil {
		return e
	}
	metadata, e := ac.AccountMetadata(ac.SMTPDelivery, ac.AccountMetadataFields{Version: foundation.Version(m.Fence), JobID: m.Job, AttemptID: m.Attempt, InitiatorID: m.Initiator, Channel: ac.DeliveryChannel(m.Channel), Phase: phase, Reason: reason})
	if e != nil {
		return e
	}
	a, e := serviceActor(identity.AccountMail, m.Attempt)
	if e != nil {
		return e
	}
	resource, e := ac.NewResource(ac.MailJobResource, m.Job)
	if e != nil {
		return e
	}
	entry, e := ac.NewEntry(ac.EntryFields{Scope: identity.SystemScope(), Actor: a, Action: ac.SMTPDelivery, Outcome: outcome, Resource: resource, Metadata: metadata})
	if e != nil {
		return e
	}
	key, e := ac.NewAppendKey(ac.AccountMailProducer, m.Attempt, 0)
	if e != nil {
		return e
	}
	_, e = p.service.state().deps.Audit.AppendInTx(ctx, tx, entry, key)
	return portError(e)
}

// Rotate candidate discovery with the domain's existing persistent pass. A
// missing original Ref never authorizes a guessed release: each candidate is
// still checked against the exact stored lease by Secret.DiscoverUsage.
func (p *deliveryPort) legacyCandidates(ctx context.Context, m mailRecord, purpose sc.Purpose) ([]string, error) {
	cause, e := recoveryCause("mail-legacy-candidates")
	if e != nil {
		return nil, e
	}
	var candidates []string
	r := p.service.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := p.service.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{configLock("account-mail-scan", foundation.Exclusive)}); e != nil {
			return unavailable(e)
		}
		x, e := p.service.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		query := `WITH batch AS (SELECT id FROM agenteam_account.material_cleanup WHERE owner_id=$1 AND owner_kind IN ('invitation','password_reset') AND phase<>'completed' ORDER BY pass,id LIMIT 98 FOR UPDATE), moved AS (UPDATE agenteam_account.material_cleanup m SET pass=m.pass+1 FROM batch b WHERE m.id=b.id RETURNING m.credential_id) SELECT credential_id::text FROM moved UNION SELECT material_ref::text FROM agenteam_account.invitations WHERE id=$1 UNION SELECT material_ref::text FROM agenteam_account.password_resets WHERE id=$1`
		args := []any{m.Link}
		if purpose == sc.SMTP {
			query = `WITH batch AS (SELECT id FROM agenteam_account.material_cleanup WHERE owner_kind='smtp_settings' AND phase<>'completed' ORDER BY pass,id LIMIT 99 FOR UPDATE), moved AS (UPDATE agenteam_account.material_cleanup m SET pass=m.pass+1 FROM batch b WHERE m.id=b.id RETURNING m.credential_id) SELECT credential_id::text FROM moved UNION SELECT password_ref::text FROM agenteam_account.smtp_settings WHERE password_ref IS NOT NULL`
			args = nil
		}
		rows, e := x.Query(ctx, query, args...)
		if e != nil {
			return unavailable(e)
		}
		defer rows.Close()
		for rows.Next() {
			var ref string
			if e = rows.Scan(&ref); e != nil {
				return unavailable(e)
			}
			candidates = append(candidates, ref)
		}
		return portError(rows.Err())
	})
	if e = resultError(r); e != nil {
		return nil, e
	}
	return candidates, nil
}
