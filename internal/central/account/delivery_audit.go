package account

import (
	"context"
	"errors"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/jackc/pgx/v5"
)

// mailAuditProjection is shared by the writer and current-authority check.
// The latter reads the actual result/reason again from the same locked Tx;
// using this projection when constructing an entry is not authorization.
func mailAuditProjection(result, reason string) (ac.Outcome, ac.AccountPhase, ac.AccountReason, error) {
	switch c.DeliveryResult(result) {
	case c.DeliverySent:
		return ac.Success, ac.AccountSent, "", nil
	case c.DeliveryUnknown:
		return ac.Unknown, ac.AccountUnknown, ac.DeliveryUnknown, nil
	case c.DeliveryFailed, c.DeliveryCancelled:
		projected := ac.DeliveryRejected
		switch c.DeliveryReason(reason) {
		case c.ReasonTimeout:
			projected = ac.DeliveryTimeout
		case c.ReasonCancelled, c.ReasonTokenInvalid:
			projected = ac.DeliveryCancelled
		}
		return ac.Failed, ac.AccountFailed, projected, nil
	default:
		return "", "", "", fault(foundation.Forbidden, nil)
	}
}

func (a *Authority) checkMailDeliveryAuditInTx(ctx context.Context, tx foundation.Tx, entry ac.Entry, key ac.AppendKey) error {
	f, k := entry.Fields(), key.Details()
	m, e := f.Metadata.AccountFields()
	if e != nil {
		return e
	}
	if f.Action != ac.SMTPDelivery || k.Producer != ac.AccountMailProducer || k.Ordinal != 0 || k.CauseRef != m.AttemptID || f.Actor.Details().CauseRef != m.AttemptID {
		return fault(foundation.Forbidden, nil)
	}
	if e = a.state().store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{recordLock(m.JobID), recordLock(m.AttemptID)}); e != nil {
		return unavailable(e)
	}
	x, e := a.state().store.InTx(tx)
	if e != nil {
		return unavailable(e)
	}
	var result, reason string
	e = x.QueryRow(ctx, `SELECT a.result,coalesce(j.reason,'')
	 FROM agenteam_account.mail_attempts a
	 JOIN agenteam_account.mail_jobs j ON j.id=a.job_id
	 JOIN agenteam_account.delivery_intents i ON i.id=j.intent_id
	 WHERE a.id=$1 AND a.job_id=$2 AND i.initiator_id=$3 AND a.channel=$4
	 AND j.current_attempt_id=a.id AND a.fence=j.fence AND a.fence=$5
	 AND a.phase='closed' AND a.terminal AND a.io_joined`, m.AttemptID, m.JobID, m.InitiatorID, string(m.Channel), int64(m.Version)).Scan(&result, &reason)
	if errors.Is(e, pgx.ErrNoRows) {
		return fault(foundation.Forbidden, nil)
	}
	if e != nil {
		return unavailable(e)
	}
	outcome, phase, safeReason, e := mailAuditProjection(result, reason)
	if e != nil {
		return e
	}
	if f.Outcome != outcome || m.Phase != phase || m.Reason != safeReason {
		return fault(foundation.Forbidden, nil)
	}
	return nil
}
