package account

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// RecoverAvatars advances bounded persistent passes. Actual completion of a
// local operation or trusted death of its exact origin precedes cancellation.
func (p *ProfileService) RecoverAvatars(ctx context.Context) (RecoveryStatus, error) {
	core := p.state().core
	op, e := core.begin(ctx, true)
	if e != nil {
		return RecoveryStatus{}, e
	}
	defer core.finish(op)
	ctx = op.ctx
	var status RecoveryStatus
	var first error
	accept := func(err error) {
		status.Examined++
		if err == nil {
			status.Advanced++
		} else {
			status.Pending++
			if first == nil && !hasFaultCode(err, foundation.ResourceBusy) {
				first = err
			}
		}
	}
	// Local COMMIT unknown may eventually roll back without leaving a row. Only
	// a writer-serialized observation can retire that joined local checkpoint.
	p.state().mu.Lock()
	ids := make([]string, 0, len(p.state().local))
	for id, v := range p.state().local {
		if operationJoined(v.op) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	start := sort.SearchStrings(ids, p.state().cursor)
	for start < len(ids) && ids[start] <= p.state().cursor {
		start++
	}
	batch := make([]string, 0, min(100, len(ids)))
	for i := 0; i < min(100, len(ids)); i++ {
		batch = append(batch, ids[(start+i)%len(ids)])
	}
	p.state().mu.Unlock()
	for _, id := range batch {
		if ctx.Err() != nil {
			return status, unavailable(ctx.Err())
		}
		p.state().mu.Lock()
		p.state().cursor = id
		local := p.state().local[id]
		p.state().mu.Unlock()
		if local == nil {
			continue
		}
		item, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := p.retireAvatarLocal(item, id, local)
		cancel()
		accept(err)
	}
	for _, stage := range []struct {
		table, predicate string
		run              func(context.Context, string) error
	}{
		{"avatar_changes", "phase IN ('preparing','published','cancelled','cleanup_pending')", p.recoverAvatarChange},
		{"avatar_cleanup", "phase='pending'", p.cleanReplacedAvatar},
	} {
		if ctx.Err() != nil {
			return status, unavailable(ctx.Err())
		}
		ids, e := core.recoveryBatch(ctx, stage.table, "pass", stage.predicate)
		if e != nil {
			return status, e
		}
		for _, id := range ids {
			if ctx.Err() != nil {
				return status, unavailable(ctx.Err())
			}
			item, cancel := context.WithTimeout(ctx, 2*time.Second)
			err := stage.run(item, id)
			if ctx.Err() == nil && item.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
				err = fault(foundation.ResourceBusy, err)
			}
			cancel()
			accept(err)
		}
	}
	if ctx.Err() != nil {
		return status, unavailable(ctx.Err())
	}
	return status, first
}
func (p *ProfileService) retireAvatarLocal(ctx context.Context, id string, local *avatarLocal) error {
	st := p.state().core.state()
	cause, _ := foundation.NewCommandsCause(local.identity)
	var pending bool
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, []foundation.LockRequest{commandLock(local.identity)}); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.avatar_changes WHERE id=$1 AND phase IN ('preparing','published','cancelled','cleanup_pending'))`, id).Scan(&pending)
		return portError(e)
	})
	if e := resultError(result); e != nil {
		return e
	}
	if !pending {
		p.state().mu.Lock()
		if p.state().local[id] == local {
			delete(p.state().local, id)
		}
		p.state().mu.Unlock()
	}
	return nil
}
func (p *ProfileService) recoverAvatarChange(ctx context.Context, id string) error {
	core := p.state().core
	st := core.state()
	change, e := loadAvatarChange(ctx, st.store, id)
	if e != nil {
		return e
	}
	if change.phase == "applied" || change.phase == "completed" {
		return nil
	}
	if change.phase == "preparing" || change.phase == "published" {
		p.state().mu.Lock()
		local := p.state().local[id]
		p.state().mu.Unlock()
		var op *operation
		if local != nil {
			op = local.op
		}
		if e = core.stopped(ctx, change.process, op); e != nil {
			return e
		}
		cmd, e := loadCommand(ctx, st.store, change.command, false)
		if e != nil {
			return e
		}
		cause, _ := foundation.NewCommandsCause(cmd.identity)
		result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
			locks := append(commandLocks(cmd), userLock(change.user, foundation.Exclusive))
			if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
				return unavailable(e)
			}
			x, e := st.store.InTx(tx)
			if e != nil {
				return unavailable(e)
			}
			now, e := loadAvatarChange(ctx, x, id)
			if e != nil {
				return e
			}
			current, e := loadCommand(ctx, x, change.command, false)
			if e != nil {
				return e
			}
			if now.process != change.process || now.command != change.command {
				return fault(foundation.ResourceBusy, nil)
			}
			if now.phase == "applied" || now.phase == "completed" {
				change = now
				return nil
			}
			if current.phase != "planned" {
				return fault(foundation.InvalidState, nil)
			}
			// Re-read after the writer lock: a reservation may have committed after the
			// initial discovery. Never infer its absence from the earlier projection.
			if _, e = x.Exec(ctx, `UPDATE agenteam_account.commands SET phase='cancelled',result_code='INVALID_STATE',completed_at=clock_timestamp() WHERE id=$1`, current.id.String()); e != nil {
				return portError(e)
			}
			if now.object == "" {
				_, e = x.Exec(ctx, `UPDATE agenteam_account.avatar_changes SET phase='completed',completed_at=clock_timestamp() WHERE id=$1`, id)
			} else {
				_, e = x.Exec(ctx, `UPDATE agenteam_account.avatar_changes SET phase='cancelled',cleanup_cause=id WHERE id=$1`, id)
			}
			if e != nil {
				return portError(e)
			}
			change, e = loadAvatarChange(ctx, x, id)
			return e
		})
		if e = resultError(result); e != nil {
			return e
		}
	}
	if change.phase == "completed" || change.phase == "applied" {
		return nil
	}
	return p.cleanCancelledAvatar(ctx, change)
}
