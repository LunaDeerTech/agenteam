package outbox

import (
	"context"
	"errors"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

// A local inspection budget is protection, not evidence of transaction or
// process death. Preserve an independently observed hard error even if its
// return races that deadline; only explicit cancellation evidence is mapped.
func recoveryBudgetError(item, parent context.Context, err error) error {
	if item.Err() == nil || parent.Err() != nil || safeFaultCode(err) == foundation.CommitUnknown {
		return err
	}
	var state interface{ SQLState() string }
	if err == nil || errors.Is(err, item.Err()) || errors.As(err, &state) && state.SQLState() == "57014" {
		return failure(foundation.ResourceBusy, item.Err())
	}
	return err
}

// Recover is an admitted, cancellable operation, not an untracked background
// goroutine. It never invokes a handler. Safe protected items do not prevent
// independent items from progressing or turn an empty marker into retry proof.
func (r *runtimeState) Recover(ctx context.Context) error {
	ctx, done, err := r.beginControl(ctx)
	if err != nil {
		return err
	}
	defer done()
	return r.recoverAll(ctx)
}
func (r *runtimeState) recoverAll(ctx context.Context) error {
	select {
	case r.recoverSlot <- struct{}{}:
	case <-ctx.Done():
		return portError(ctx.Err())
	}
	defer func() { <-r.recoverSlot }()
	var first error
	after := ""
	for {
		if ctx.Err() != nil {
			if first != nil {
				return first
			}
			return portError(ctx.Err())
		}
		rows, err := r.svc.state().store.Query(ctx, `SELECT `+recordColumns+recordFrom+` WHERE d.current_attempt_id IS NOT NULL AND (d.phase='processing' OR (a.joined_at IS NULL AND a.checkpoint NOT IN ('not_committed','stopped'))) AND ($1::uuid IS NULL OR d.id>$1) ORDER BY d.id LIMIT $2`, nullableUUID(after), r.options.Batch)
		if err != nil {
			return unavailable(err)
		}
		var batch []record
		for rows.Next() {
			d, e := scanRecord(rows)
			if e != nil {
				rows.Close()
				return e
			}
			batch = append(batch, d)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return unavailable(err)
		}
		if len(batch) == 0 {
			break
		}
		for _, d := range batch {
			after = d.id.String()
			if ctx.Err() != nil {
				if first != nil {
					return first
				}
				return portError(ctx.Err())
			}
			item, cancel := context.WithTimeout(ctx, r.options.ItemTimeout)
			err = r.recoverOne(item, d)
			err = recoveryBudgetError(item, ctx, err)
			cancel()
			if err != nil && !protectedError(err) && first == nil {
				first = err
			}
		}
	}
	if err := r.recoverLifecycles(ctx, true); err != nil && first == nil {
		first = err
	}
	return first
}
func (r *runtimeState) recoverBatch(ctx context.Context) error {
	select {
	case r.recoverSlot <- struct{}{}:
	case <-ctx.Done():
		return portError(ctx.Err())
	}
	defer func() { <-r.recoverSlot }()
	p := r.recoveryAfter
	rows, err := r.svc.state().store.Query(ctx, `SELECT `+recordColumns+recordFrom+` WHERE d.current_attempt_id IS NOT NULL AND (d.phase='processing' OR (a.joined_at IS NULL AND a.checkpoint NOT IN ('not_committed','stopped'))) AND ($1::uuid IS NULL OR (d.recovery_pass,d.id)>($2,$1)) ORDER BY d.recovery_pass,d.id LIMIT $3`, nullableUUID(p.id), p.pass, r.options.Batch)
	if err != nil {
		return unavailable(err)
	}
	var batch []record
	for rows.Next() {
		d, e := scanRecord(rows)
		if e != nil {
			rows.Close()
			return e
		}
		batch = append(batch, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return unavailable(err)
	}
	if len(batch) == 0 {
		r.recoveryAfter = recoveryPosition{}
		return nil
	}
	var first error
	for _, d := range batch {
		r.recoveryAfter = recoveryPosition{d.pass, d.id.String()}
		if ctx.Err() != nil {
			if first != nil {
				return first
			}
			return portError(ctx.Err())
		}
		item, cancel := context.WithTimeout(ctx, r.options.ItemTimeout)
		err = r.recoverOne(item, d)
		err = recoveryBudgetError(item, ctx, err)
		cancel()
		if err != nil && !protectedError(err) && first == nil {
			first = err
		}
	}
	return first
}

func (r *runtimeState) localReturned(d record) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.active[d.attempt]
	if a == nil || a.record.id != d.id {
		return false
	}
	select {
	case <-a.done:
		return true
	default:
		return false
	}
}
func (r *runtimeState) recoverOne(ctx context.Context, d record) error {
	local := d.process == r.svc.state().auth.Processes.CurrentProcess()
	proved := false
	if local {
		r.mu.Lock()
		active := r.active[d.attempt]
		running := false
		if active != nil {
			select {
			case <-active.done:
			default:
				running = true
			}
		}
		r.mu.Unlock()
		if running {
			return failure(foundation.ResourceBusy, nil)
		}
		proved = r.localReturned(d)
	} else {
		err := r.svc.state().auth.Processes.ConfirmStopped(ctx, d.process)
		if err == nil {
			proved = true
		} else {
			switch safeFaultCode(err) {
			case foundation.ResourceBusy, foundation.NotFound, foundation.DependencyUnavailable, foundation.DependencyUnbound:
				// A refused death proof (including another host/deployment) is
				// protection, not permission to retry. The technical transaction
				// below must still validate the current DB claim/marker; a DB or
				// structural failure there remains a real component error.
			default:
				return portError(err)
			}
		}
	}
	if proved {
		var returned *time.Time
		if local {
			r.mu.Lock()
			if a := r.active[d.attempt]; a != nil {
				returned = a.returned
			}
			r.mu.Unlock()
		}
		err := r.checkpoint(ctx, d, oc.Retry(oc.ProcessUnconfirmed), oc.ProcessUnconfirmed, returned)
		if err != nil {
			return err
		}
		return r.markJoined(ctx, d, returned)
	}
	// A live or unconfirmed process remains protected even past its deadline.
	// Its marker can still be recognized under the exact writer lock. Advancing
	// this scan position is technical bookkeeping, not a death assertion.
	s := r.svc
	result := s.state().store.WithinTx(ctx, recoveryCause("outbox.recovery-inspect"), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, technicalLocks(d)); err != nil {
			return unavailable(err)
		}
		x, err := executor(s, tx)
		if err != nil {
			return err
		}
		current, err := s.deliveryRecord(ctx, x, d.id)
		if err != nil {
			return err
		}
		if current.attempt != d.attempt || current.fence != d.fence || current.process != d.process || current.scope != d.scope {
			return failure(foundation.ResourceBusy, nil)
		}
		_, found, err := readProcessed(ctx, x, current.identity())
		if err != nil {
			return err
		}
		if found {
			if err = finishDeliveryInTx(ctx, x, current.identity()); err != nil {
				return err
			}
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_outbox.deliveries SET recovery_pass=recovery_pass+1,safe_reason=CASE WHEN phase='processing' THEN 'process_unconfirmed' ELSE safe_reason END WHERE id=$1 AND current_attempt_id=$2 AND fence=$3`, d.id.String(), d.attempt.String(), d.fence)
		return portResult(err)
	})
	if err := commitError(result); err != nil {
		return err
	}
	return failure(foundation.ResourceBusy, nil)
}
