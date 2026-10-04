package account

import (
	"context"
	"crypto/subtle"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func (a *Authority) checkMailRetryAuditInTx(ctx context.Context, tx foundation.Tx, entry ac.Entry, key ac.AppendKey) error {
	f := entry.Fields()
	m, e := f.Metadata.AccountFields()
	if e != nil {
		return e
	}
	if key.Details().Ordinal != 0 || key.Details().Producer != ac.AccountProducer {
		return fault(foundation.Forbidden, nil)
	}
	x, e := a.state().store.InTx(tx)
	if e != nil {
		return unavailable(e)
	}
	cmd, e := loadCommand(ctx, x, key.Details().CauseRef, false)
	if e != nil {
		return e
	}
	if cmd.name != "mail-retry" || cmd.phase != "committed" || cmd.actorKind != "human" || cmd.user != f.Actor.Details().UserID || cmd.session != f.Actor.Details().SessionID || cmd.resource != f.Resource.Details().ID || m.JobID != cmd.resource || m.InitiatorID != cmd.user || int64(m.Version) != cmd.expectedVersion {
		return fault(foundation.Forbidden, nil)
	}
	o, e := loadJobOrigin(ctx, x, cmd.resource)
	if e != nil {
		return e
	}
	if e = a.state().store.RequireHeldLocks(ctx, tx, retryLocks(cmd, o)); e != nil {
		return unavailable(e)
	}
	semantic, e := retrySemantic(cmd.identity, cmd.user, cmd.resource, cmd.expectedVersion)
	if e != nil {
		return unavailable(e)
	}
	want, e := a.state().keys.mac(cmd.kid, "command-v1", semantic)
	if e != nil {
		return e
	}
	defer clear(want)
	if subtle.ConstantTimeCompare(want, cmd.mac) != 1 {
		return fault(foundation.Forbidden, nil)
	}
	if e = retryReceipt(ctx, x, cmd, o); e != nil {
		return e
	}
	var version int64
	if e = x.QueryRow(ctx, `SELECT version FROM agenteam_account.mail_jobs WHERE id=$1`, cmd.resource).Scan(&version); e != nil {
		return unavailable(e)
	}
	if version != cmd.expectedVersion+1 {
		return fault(foundation.Forbidden, nil)
	}
	return nil
}
