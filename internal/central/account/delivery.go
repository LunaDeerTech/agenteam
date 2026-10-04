package account

import (
	"context"
	"errors"
	"sync"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type deliveryPort struct {
	service  *Service
	runtime  c.DeliveryRuntime
	issuer   c.DeliveryIssuer
	mu       sync.Mutex
	claims   map[string]*claimOperation
	finishes map[string]pendingMailFinish
}
type claimOperation struct {
	job, attempt           string
	locks                  []foundation.LockRequest
	record                 mailRecord
	origin                 foundation.CommitResult
	joined, closed, handed bool
	pass                   int64
}

func NewDeliveryPort(s *Service, r c.DeliveryRuntime) (c.DeliveryPort, error) {
	if s == nil || s.data == nil || nilPort(r) || r.CurrentProcess() != s.state().process || r.CurrentProcess().Validate() != nil {
		return nil, invalid()
	}
	return &deliveryPort{service: s, runtime: r, issuer: c.NewDeliveryIssuer(), claims: map[string]*claimOperation{}}, nil
}
func (p *deliveryPort) handle(m mailRecord) (c.DeliveryAttempt, error) {
	j, e := parseID[c.MailJob](m.Job)
	if e != nil {
		return c.DeliveryAttempt{}, e
	}
	i, e := parseID[c.DeliveryIntent](m.Intent)
	if e != nil {
		return c.DeliveryAttempt{}, e
	}
	a, e := parseID[c.Attempt](m.Attempt)
	if e != nil {
		return c.DeliveryAttempt{}, e
	}
	process, e := parseID[c.Process](m.Process)
	if e != nil {
		return c.DeliveryAttempt{}, e
	}
	return c.NewDeliveryAttempt(p.issuer, c.DeliveryAttemptDetails{JobID: j, IntentID: i, AttemptID: a, Kind: c.DeliveryKind(m.Kind), ProcessID: process, Fence: foundation.Sequence(m.Fence), ConfigVersion: foundation.Version(m.ConfigVersion), Channel: c.DeliveryChannel(m.Channel)}, mailBinding(m))
}
func (p *deliveryPort) match(a c.DeliveryAttempt, m mailRecord) error {
	if !a.IssuedBy(p.issuer) || a.Details().ProcessID != p.runtime.CurrentProcess() || a.Binding() != mailBinding(m) {
		return fault(foundation.Forbidden, nil)
	}
	return nil
}
func (p *deliveryPort) withRecord(ctx context.Context, before mailRecord, extra []foundation.LockRequest, fn func(context.Context, foundation.Tx, postgres.SQLExecutor, mailRecord) error) foundation.CommitResult {
	s := p.service.state()
	cause, e := recoveryCause("mail")
	if e != nil {
		return foundation.NotCommittedResult(foundation.NewFault(foundation.DependencyUnavailable, foundation.NotCommitted).WithCause(e))
	}
	locks := append(mailLocks(before), extra...)
	return s.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := s.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		x, e := s.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		m, e := loadMail(ctx, x, before.Job, before.Attempt)
		if e != nil {
			return e
		}
		if m.Intent != before.Intent || m.Link != before.Link || m.User != before.User || m.RootBinding != before.RootBinding {
			return fault(foundation.ResourceBusy, nil)
		}
		return fn(ctx, tx, x, m)
	})
}
func (p *deliveryPort) NextDeliveries(ctx context.Context) ([]c.JobID, error) {
	op, e := p.service.begin(ctx, false)
	if e != nil {
		return nil, e
	}
	defer p.service.finish(op)
	ctx = op.ctx
	cause, e := recoveryCause("mail-scan")
	if e != nil {
		return nil, e
	}
	var ids []c.JobID
	r := p.service.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := p.service.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{configLock("account-mail-scan", foundation.Exclusive)}); e != nil {
			return unavailable(e)
		}
		x, e := p.service.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		rows, e := x.Query(ctx, `WITH batch AS (SELECT id FROM agenteam_account.mail_jobs WHERE phase IN ('pending','retry_wait') AND next_at<=clock_timestamp() ORDER BY pass,next_at,id LIMIT 100 FOR UPDATE) UPDATE agenteam_account.mail_jobs j SET pass=j.pass+1 FROM batch b WHERE j.id=b.id RETURNING j.id::text`)
		if e != nil {
			return unavailable(e)
		}
		defer rows.Close()
		for rows.Next() {
			var raw string
			if e = rows.Scan(&raw); e != nil {
				return unavailable(e)
			}
			id, e := parseID[c.MailJob](raw)
			if e != nil {
				return e
			}
			ids = append(ids, id)
		}
		return portError(rows.Err())
	})
	if e = resultError(r); e != nil {
		return nil, e
	}
	return ids, nil
}
func (p *deliveryPort) ClaimDelivery(ctx context.Context, id c.JobID) (c.DeliveryAttempt, error) {
	empty := c.DeliveryAttempt{}
	if id.Validate() != nil {
		return empty, invalid()
	}
	op, e := p.service.begin(ctx, false)
	if e != nil {
		return empty, e
	}
	defer p.service.finish(op)
	ctx = op.ctx
	before, e := loadMail(ctx, p.service.state().store, id.String(), "")
	if e != nil {
		return empty, e
	}
	aid, e := foundation.NewID[c.Attempt]()
	if e != nil {
		return empty, unavailable(e)
	}
	token, e := foundation.NewID[sc.Lease]()
	if e != nil {
		return empty, unavailable(e)
	}
	credential, e := foundation.NewID[sc.Lease]()
	if e != nil {
		return empty, unavailable(e)
	}
	cop := &claimOperation{job: id.String(), attempt: aid.String(), locks: append(mailLocks(before), recordLock(aid.String()))}
	p.mu.Lock()
	p.claims[cop.attempt] = cop
	p.mu.Unlock()
	// Registration precedes the DB call. Actual return, not context selection,
	// establishes joined; the single handoff decision can never be reopened.
	defer func() { p.mu.Lock(); cop.joined = true; cop.closed = true; p.mu.Unlock() }()
	var claimed mailRecord
	var refused error
	r := p.withRecord(ctx, before, []foundation.LockRequest{recordLock(aid.String())}, func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor, m mailRecord) error {
		busy, e := originHasOtherCycle(ctx, x, m.Origin, m.Job)
		if e != nil {
			return e
		}
		if busy {
			return fault(foundation.ResourceBusy, nil)
		}
		if m.JobPhase != "pending" && m.JobPhase != "retry_wait" {
			return fault(foundation.ResourceBusy, nil)
		}
		var due bool
		if e := x.QueryRow(ctx, `SELECT next_at<=clock_timestamp() FROM agenteam_account.mail_jobs WHERE id=$1`, m.Job).Scan(&due); e != nil {
			return unavailable(e)
		}
		if !due {
			return fault(foundation.ResourceBusy, nil)
		}
		cfg, e := loadSMTP(ctx, x)
		if e != nil {
			return e
		}
		if m.Attempts >= cfg.Retries+1 || m.Attempts >= 6 {
			refused = fault(foundation.InvalidState, nil)
		}
		m.Attempt = aid.String()
		m.Process = p.runtime.CurrentProcess().String()
		m.Fence++
		m.JobFence = m.Fence
		m.ConfigVersion = cfg.Version
		m.Protocol = "negotiation"
		m.Joined = false
		m.Terminal = false
		m.Result = ""
		m.TokenRef = ""
		m.CredentialRef = ""
		m.TokenLease = ""
		m.CredentialLease = ""
		m.Channel = string(c.RecoveryLogChannel)
		if cfg.Configured {
			m.Channel = string(c.SMTPChannel)
			m.CredentialRef = cfg.Ref
			if cfg.Ref != "" {
				m.CredentialLease = credential.String()
			}
		}
		if m.Kind != string(c.TestDelivery) {
			link, e := loadLink(ctx, x, c.TokenKind(m.Kind), m.Link)
			if e != nil {
				var f *foundation.Fault
				if !errors.As(e, &f) || f.Code != foundation.ResourceDeleted {
					return e
				}
				refused = e
			} else {
				m.TokenRef = link.ref
				m.TokenLease = token.String()
			}
		}
		if _, _, e = mailLive(ctx, x, m, cfg); e != nil {
			refused = e
		}
		if refused != nil {
			_, e = x.Exec(ctx, `UPDATE agenteam_account.mail_jobs SET phase='cancelled',reason='token_invalid',version=version+1,completed_at=clock_timestamp() WHERE id=$1`, m.Job)
			return portError(e)
		}
		_, e = x.Exec(ctx, `INSERT INTO agenteam_account.mail_attempts(id,job_id,fence,process_id,config_version,token_lease_id,credential_lease_id,token_ref,credential_ref,phase,channel) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'negotiation',$10)`, m.Attempt, m.Job, m.Fence, m.Process, m.ConfigVersion, null(m.TokenLease), null(m.CredentialLease), null(m.TokenRef), null(m.CredentialRef), m.Channel)
		if e != nil {
			return unavailable(e)
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_account.mail_jobs SET phase='claimed',attempts=attempts+1,fence=$2,current_attempt_id=$3,version=version+1,completed_at=NULL,reason=NULL WHERE id=$1`, m.Job, m.Fence, m.Attempt)
		if e != nil {
			return unavailable(e)
		}
		m.CurrentAttempt = m.Attempt
		m.JobPhase = "claimed"
		m.Attempts++
		m.JobVersion++
		claimed = m
		cop.record = m
		return nil
	})
	p.mu.Lock()
	cop.origin = r
	p.mu.Unlock()
	if r.State() == foundation.Unknown {
		if claimed.Attempt == "" {
			return empty, confirmationError(r, nil)
		}
		var found mailRecord
		confirmation := p.withRecord(ctx, claimed, nil, func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor, m mailRecord) error {
			if mailBinding(m) != mailBinding(claimed) {
				return fault(foundation.ResourceBusy, nil)
			}
			found = m
			return nil
		})
		if e = resultError(confirmation); e != nil {
			return empty, confirmationError(r, e)
		}
		claimed = found
	} else if e = resultError(r); e != nil {
		p.mu.Lock()
		delete(p.claims, cop.attempt)
		p.mu.Unlock()
		return empty, e
	}
	if refused != nil {
		p.mu.Lock()
		delete(p.claims, cop.attempt)
		p.mu.Unlock()
		return empty, refused
	}
	p.mu.Lock()
	if ctx.Err() != nil {
		cop.closed = true
		cop.joined = true
		p.mu.Unlock()
		return empty, confirmationError(r, unavailable(ctx.Err()))
	}
	h, e := p.handle(claimed)
	if e == nil {
		cop.handed = true
		cop.closed = true
		delete(p.claims, cop.attempt)
	}
	p.mu.Unlock()
	// No ctx-select after this irrevocable handoff. Worker installs its finally
	// before Claim and registers this handle even if Stop has already arrived.
	return h, e
}
func (p *deliveryPort) PrepareDelivery(ctx context.Context, a c.DeliveryAttempt) (c.DeliveryMaterials, error) {
	empty := c.DeliveryMaterials{}
	if a.Validate() != nil || !a.IssuedBy(p.issuer) {
		return empty, invalid()
	}
	if e := p.runtime.RequireActive(a); e != nil {
		return empty, portError(e)
	}
	op, e := p.service.begin(ctx, false)
	if e != nil {
		return empty, e
	}
	defer p.service.finish(op)
	ctx = op.ctx
	m, e := loadMail(ctx, p.service.state().store, a.Details().JobID.String(), a.Details().AttemptID.String())
	if e != nil {
		return empty, e
	}
	if e = p.match(a, m); e != nil {
		return empty, e
	}
	var requests []sc.UsageRequest
	var deps []sc.UsageDependencies
	var locks []foundation.LockRequest
	for _, purpose := range []sc.Purpose{sc.System, sc.SMTP} {
		ref, _ := mailSlot(m, purpose)
		if ref == "" {
			continue
		}
		q, e := deliveryUsage(m, purpose, sc.AcquireLeaseUsage)
		if e != nil {
			return empty, e
		}
		d, e := p.service.state().deps.Secrets.DiscoverUsage(ctx, q)
		if e != nil {
			return empty, portError(e)
		}
		requests = append(requests, q)
		deps = append(deps, d)
		locks = append(locks, d.RequiredLocks()...)
	}
	var fields c.DeliveryMaterialFields
	r := p.withRecord(ctx, m, locks, func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor, now mailRecord) error {
		if e := p.match(a, now); e != nil {
			return e
		}
		if e := mailCurrent(now); e != nil {
			return e
		}
		cfg, e := loadSMTP(ctx, x)
		if e != nil {
			return e
		}
		recipient, expires, e := mailLive(ctx, x, now, cfg)
		if e != nil {
			return e
		}
		for i, q := range requests {
			if _, e = p.service.state().deps.Secrets.ApplyUsageInTx(ctx, tx, q, deps[i]); e != nil {
				return portError(e)
			}
		}
		fields = c.DeliveryMaterialFields{Attempt: a, Recipient: recipient, Host: cfg.Host, Port: cfg.Port, TLSMode: cfg.Mode, Username: cfg.Username, SenderEmail: cfg.From, SenderName: cfg.Name, LinkID: now.Link, ExpiresAt: expires}
		return nil
	})
	if r.State() == foundation.Unknown {
		// This is only a writer barrier. No lease is created during confirmation.
		confirm := p.withRecord(ctx, m, nil, func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor, now mailRecord) error {
			if e := p.match(a, now); e != nil {
				return e
			}
			return mailCurrent(now)
		})
		if e = resultError(confirm); e != nil {
			return empty, confirmationError(r, e)
		}
	} else if e = resultError(r); e != nil {
		return empty, e
	}
	ok := false
	defer func() {
		if !ok {
			fields.Token.Destroy()
			fields.Password.Destroy()
		}
	}()
	for _, q := range requests {
		value, e := p.service.state().deps.Secrets.ReadCredentialForRequest(ctx, q.Actor, q.LeaseID)
		if e != nil {
			return empty, confirmationError(r, portError(e))
		}
		if q.Purpose == sc.System {
			fields.Token = value
		} else {
			fields.Password = value
		}
	}
	materials, e := c.NewDeliveryMaterials(fields)
	if e != nil {
		return empty, e
	}
	ok = true
	return materials, nil
}
func (p *deliveryPort) BeginDelivery(ctx context.Context, a c.DeliveryAttempt, phase c.DeliveryPhase) (c.SendPermit, error) {
	if a.Validate() != nil || !phase.Valid() || !a.IssuedBy(p.issuer) {
		return c.SendPermit{}, invalid()
	}
	if e := p.runtime.RequireActive(a); e != nil {
		return c.SendPermit{}, portError(e)
	}
	op, e := p.service.begin(ctx, false)
	if e != nil {
		return c.SendPermit{}, e
	}
	// This operation belongs to the permit until the real first-write/grant
	// callback has returned, so Service force/join also sees it.
	g, e := p.service.state().deps.Authority.state().mail.acquire(op.ctx, false)
	if e != nil {
		p.service.finish(op)
		return c.SendPermit{}, e
	}
	deadline := time.Now().Add(time.Second)
	check, cancel := context.WithDeadline(op.ctx, deadline)
	cleanup := func() { cancel(); g.release(); p.service.finish(op) }
	m, e := loadMail(check, p.service.state().store, a.Details().JobID.String(), a.Details().AttemptID.String())
	if e != nil {
		cleanup()
		return c.SendPermit{}, e
	}
	if (phase == c.RecoveryLog) != (m.Channel == string(c.RecoveryLogChannel)) {
		cleanup()
		return c.SendPermit{}, invalid()
	}
	r := p.withRecord(check, m, nil, func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor, now mailRecord) error {
		if e := p.match(a, now); e != nil {
			return e
		}
		if e := mailCurrent(now); e != nil {
			return e
		}
		cfg, e := loadSMTP(ctx, x)
		if e != nil {
			return e
		}
		_, expires, e := mailLive(ctx, x, now, cfg)
		if e != nil {
			return e
		}
		if !expires.IsZero() && expires.Before(deadline) {
			deadline = expires
		}
		if phase == c.SMTPAuth {
			if now.Protocol != "negotiation" || now.CredentialRef == "" {
				return fault(foundation.InvalidState, nil)
			}
			_, e = x.Exec(ctx, `UPDATE agenteam_account.mail_attempts SET phase='auth' WHERE id=$1`, now.Attempt)
		} else {
			if now.Protocol != "negotiation" && now.Protocol != "auth" {
				return fault(foundation.InvalidState, nil)
			}
			_, e = x.Exec(ctx, `UPDATE agenteam_account.mail_attempts SET phase='envelope' WHERE id=$1`, now.Attempt)
			if e == nil {
				_, e = x.Exec(ctx, `UPDATE agenteam_account.mail_jobs SET phase='sending',version=version+1 WHERE id=$1`, now.Job)
			}
		}
		return portError(e)
	})
	if e = resultError(r); e != nil {
		cleanup()
		return c.SendPermit{}, e
	}
	permit, e := c.NewSendPermit(check, phase, deadline, cleanup)
	if e != nil {
		cleanup()
		return c.SendPermit{}, e
	}
	return permit, nil
}
func (p *deliveryPort) CheckpointDelivery(ctx context.Context, a c.DeliveryAttempt, phase c.DeliveryProtocolPhase) error {
	if !a.IssuedBy(p.issuer) || phase != c.Data && phase != c.AwaitingAcceptance {
		return invalid()
	}
	if e := p.runtime.RequireActive(a); e != nil {
		return portError(e)
	}
	op, e := p.service.begin(ctx, false)
	if e != nil {
		return e
	}
	defer p.service.finish(op)
	ctx = op.ctx
	m, e := loadMail(ctx, p.service.state().store, a.Details().JobID.String(), a.Details().AttemptID.String())
	if e != nil {
		return e
	}
	r := p.withRecord(ctx, m, nil, func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor, now mailRecord) error {
		if e := p.match(a, now); e != nil {
			return e
		}
		if e := mailCurrent(now); e != nil {
			return e
		}
		want := "envelope"
		if phase == c.AwaitingAcceptance {
			want = "data"
		}
		if now.Protocol != want {
			return fault(foundation.InvalidState, nil)
		}
		_, e := x.Exec(ctx, `UPDATE agenteam_account.mail_attempts SET phase=$2 WHERE id=$1`, now.Attempt, string(phase))
		return portError(e)
	})
	return resultError(r)
}

var _ c.DeliveryPort = (*deliveryPort)(nil)
