package account

import (
	"context"
	"errors"
	"sort"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func (p *deliveryPort) RecoverDeliveries(ctx context.Context) (c.DeliveryRecoveryStatus, error) {
	status := c.DeliveryRecoveryStatus{}
	op, e := p.service.begin(ctx, true)
	if e != nil {
		return status, e
	}
	defer p.service.finish(op)
	ctx = op.ctx
	var first error
	keep := func(e error) {
		if e != nil && (first == nil || mailPending(first) && !mailPending(e)) {
			first = e
		}
	}
	// A never-handed operation carries positive local evidence. Its DB call
	// must have actually returned; a missing registry record alone proves none.
	p.mu.Lock()
	ids := make([]string, 0, len(p.claims))
	for id, v := range p.claims {
		if v.joined && v.closed && !v.handed {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := p.claims[ids[i]], p.claims[ids[j]]
		if a.pass != b.pass {
			return a.pass < b.pass
		}
		return ids[i] < ids[j]
	})
	if len(ids) > 50 {
		ids = ids[:50]
	}
	for _, id := range ids {
		p.claims[id].pass++
	}
	p.mu.Unlock()
	for _, id := range ids {
		if status.Examined >= 50 {
			break
		}
		if ctx.Err() != nil {
			return status, unavailable(ctx.Err())
		}
		status.Examined++
		item, cancel := context.WithTimeout(ctx, 2*time.Second)
		e = p.recoverUnhanded(item, id)
		cancel()
		if e == nil {
			status.Advanced++
		} else {
			status.Pending++
			keep(e)
		}
	}
	remaining := 100 - int(status.Examined)
	if remaining == 0 {
		return status, first
	}
	cause, e := recoveryCause("mail-recovery-scan")
	if e != nil {
		return status, e
	}
	type recoveryItem struct{ kind, id string }
	var jobs []recoveryItem
	r := p.service.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := p.service.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{configLock("account-mail-scan", foundation.Exclusive)}); e != nil {
			return unavailable(e)
		}
		x, e := p.service.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		rows, e := x.Query(ctx, `WITH candidates AS (
 SELECT 'job'::text AS kind,j.id,j.pass FROM agenteam_account.mail_jobs j
 WHERE EXISTS(SELECT 1 FROM agenteam_account.mail_attempts a WHERE a.id=j.current_attempt_id AND NOT a.terminal)
    OR (j.phase IN ('claimed','sending','processing','unknown') AND j.current_attempt_id IS NULL)
 UNION ALL SELECT 'material',id,pass FROM agenteam_account.material_cleanup WHERE owner_kind='smtp_settings' AND phase<>'completed'
), batch AS (SELECT kind,id FROM candidates ORDER BY pass,id LIMIT $1),
 jobs AS (UPDATE agenteam_account.mail_jobs j SET pass=j.pass+1 FROM batch b WHERE b.kind='job' AND j.id=b.id RETURNING j.id),
 materials AS (UPDATE agenteam_account.material_cleanup m SET pass=m.pass+1 FROM batch b WHERE b.kind='material' AND m.id=b.id RETURNING m.id)
 SELECT 'job',id::text FROM jobs UNION ALL SELECT 'material',id::text FROM materials`, remaining)
		if e != nil {
			return unavailable(e)
		}
		defer rows.Close()
		for rows.Next() {
			var item recoveryItem
			if e = rows.Scan(&item.kind, &item.id); e != nil {
				return unavailable(e)
			}
			jobs = append(jobs, item)
		}
		return portError(rows.Err())
	})
	if e = resultError(r); e != nil {
		return status, e
	}
	for _, job := range jobs {
		if ctx.Err() != nil {
			return status, unavailable(ctx.Err())
		}
		status.Examined++
		item, cancel := context.WithTimeout(ctx, 2*time.Second)
		if job.kind == "material" {
			e = p.service.cleanSMTPMaterial(item, job.id)
		} else {
			e = p.recoverMail(item, job.id)
		}
		cancel()
		if e == nil {
			status.Advanced++
		} else {
			status.Pending++
			keep(e)
		}
	}
	return status, first
}
func (p *deliveryPort) recoverUnhanded(ctx context.Context, id string) error {
	p.mu.Lock()
	work := p.claims[id]
	if work == nil || !work.joined || !work.closed || work.handed {
		p.mu.Unlock()
		return fault(foundation.ResourceBusy, nil)
	}
	copy := *work
	p.mu.Unlock()
	cause, e := recoveryCause("mail-unhanded")
	if e != nil {
		return e
	}
	var found mailRecord
	var absent bool
	r := p.service.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := p.service.state().store.AcquireAll(ctx, tx, copy.locks); e != nil {
			return unavailable(e)
		}
		x, e := p.service.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		var exists bool
		if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.mail_attempts WHERE id=$1 AND job_id=$2)`, copy.attempt, copy.job).Scan(&exists); e != nil {
			return unavailable(e)
		}
		if !exists {
			absent = true
			return nil
		}
		found, e = loadMail(ctx, x, copy.job, copy.attempt)
		if e != nil {
			return e
		}
		if copy.record.Attempt == "" || mailBinding(found) != mailBinding(copy.record) || found.Process != p.runtime.CurrentProcess().String() {
			return fault(foundation.ResourceBusy, nil)
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_account.mail_attempts SET io_joined=true WHERE id=$1`, copy.attempt)
		return portError(e)
	})
	if e = resultError(r); e != nil {
		return confirmationError(copy.origin, e)
	}
	if !absent {
		e = p.finishMail(ctx, found, c.DeliveryOutcome{Result: c.DeliveryFailed, Reason: c.ReasonNetwork, Retryable: true})
		if e != nil {
			return e
		}
	}
	p.mu.Lock()
	if p.claims[id] == work {
		delete(p.claims, id)
	}
	p.mu.Unlock()
	return nil
}
func (p *deliveryPort) recoverMail(ctx context.Context, job string) error {
	m, e := loadMail(ctx, p.service.state().store, job, "")
	if e != nil {
		return e
	}
	if m.Terminal {
		return nil
	}
	if m.Attempt == "" {
		return unavailable(nil)
	}
	p.mu.Lock()
	finish, hasFinish := p.finishes[m.Attempt]
	p.mu.Unlock()
	if hasFinish {
		return p.FinishDelivery(ctx, finish.attempt, finish.completion)
	}
	if !m.Joined {
		if m.Process == p.runtime.CurrentProcess().String() {
			return fault(foundation.ResourceBusy, nil)
		}
		pid, e := parseID[c.Process](m.Process)
		if e != nil {
			return e
		}
		if e = p.service.state().deps.Processes.ConfirmStopped(ctx, pid); e != nil {
			return portError(e)
		}
	}
	out := c.DeliveryOutcome{Result: c.DeliveryUnknown, Reason: c.ReasonUnknown, Retryable: true}
	if m.Protocol == "negotiation" || m.Protocol == "auth" {
		out = c.DeliveryOutcome{Result: c.DeliveryFailed, Reason: c.ReasonNetwork, Retryable: true}
	}
	return p.finishMail(ctx, m, out)
}

// Protected live work is a pending result for the scheduler, never death.
func mailPending(e error) bool {
	var f *foundation.Fault
	return errors.As(e, &f) && f.Code == foundation.ResourceBusy
}
