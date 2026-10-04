package account

import (
	"context"
	"sort"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type mutationWork struct {
	identity   foundation.CommandIdentity
	operations map[*operation]bool
}

// Register before a command's first transaction. A joined request is evidence
// about its local work only; the original writer must still serialize recovery
// with a delayed COMMIT before absence or a terminal receipt is trusted.
func (s *Service) trackMutation(key foundation.CommandIdentity, op *operation) error {
	st := s.state()
	st.mu.Lock()
	defer st.mu.Unlock()
	k := key.Canonical()
	v := st.mutations[k]
	if v == nil {
		if len(st.mutations) >= 10000 {
			return fault(foundation.RateLimited, nil)
		}
		v = &mutationWork{key, map[*operation]bool{}}
		st.mutations[k] = v
	}
	v.operations[op] = true
	return nil
}
func (v *mutationWork) joined() bool {
	if v == nil || len(v.operations) == 0 {
		return false
	}
	for op := range v.operations {
		if !operationJoined(op) {
			return false
		}
	}
	return true
}
func (s *Service) mutationStopped(ctx context.Context, cmd commandRecord) error {
	st := s.state()
	if cmd.process != st.process.String() {
		return s.stopped(ctx, cmd.process, nil)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.mutations[cmd.identity.Canonical()].joined() {
		return nil
	}
	return fault(foundation.ResourceBusy, nil)
}
func (s *Service) recoverMutation(ctx context.Context, id string) error {
	st := s.state()
	cmd, e := loadCommand(ctx, st.store, id, false)
	if e != nil {
		return e
	}
	if e = s.mutationStopped(ctx, cmd); e != nil {
		return e
	}
	cause, e := recoveryCause("mutation-cancel")
	if e != nil {
		return e
	}
	r := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, commandLocks(cmd)); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		current, e := loadCommand(ctx, x, id, false)
		if e != nil {
			return e
		}
		if current.phase != "planned" {
			return nil
		}
		if commandMapping(current) != commandMapping(cmd) {
			return fault(foundation.ResourceBusy, nil)
		}
		if e = s.mutationStopped(ctx, current); e != nil {
			return e
		}
		// No account Secret Apply is committed before the business transaction.
		// A planned sealed envelope is process-local; nonce reservations remain the
		// Secret service's own durable recovery concern, never an active lease here.
		_, e = x.Exec(ctx, `UPDATE agenteam_account.commands SET phase='cancelled',result_code='INVALID_STATE',completed_at=clock_timestamp(),planned_secret_ref=NULL,secret_write_binding=NULL WHERE id=$1 AND phase='planned'`, id)
		return portError(e)
	})
	return resultError(r)
}
func (s *Service) retireMutations(ctx context.Context, status *RecoveryStatus) error {
	st := s.state()
	st.mu.Lock()
	keys := make([]string, 0, len(st.mutations))
	for key, v := range st.mutations {
		if v.joined() {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	start := sort.SearchStrings(keys, st.mutationCursor)
	if start < len(keys) && keys[start] == st.mutationCursor {
		start++
	}
	batch := make([]*mutationWork, 0, min(100, len(keys)))
	for n := 0; n < min(100, len(keys)); n++ {
		batch = append(batch, st.mutations[keys[(start+n)%len(keys)]])
	}
	st.mu.Unlock()
	var first error
	for _, v := range batch {
		if ctx.Err() != nil {
			return unavailable(ctx.Err())
		}
		st.mu.Lock()
		st.mutationCursor = v.identity.Canonical()
		st.mu.Unlock()
		item, cancel := context.WithTimeout(ctx, 2*time.Second)
		cause, e := recoveryCause("mutation-retire")
		if e != nil {
			cancel()
			return e
		}
		required := false
		r := st.store.WithinTx(item, cause, func(ctx context.Context, tx foundation.Tx) error {
			if e := st.store.AcquireAll(ctx, tx, []foundation.LockRequest{commandLock(v.identity)}); e != nil {
				return unavailable(e)
			}
			x, e := st.store.InTx(tx)
			if e != nil {
				return unavailable(e)
			}
			return portError(x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.commands WHERE identity_digest=$1 AND phase='planned')`, string(digest([]byte(v.identity.Canonical())))).Scan(&required))
		})
		cancel()
		e = resultError(r)
		status.Examined++
		if e == nil && !required {
			st.mu.Lock()
			if st.mutations[v.identity.Canonical()] == v && v.joined() {
				delete(st.mutations, v.identity.Canonical())
			}
			st.mu.Unlock()
			status.Advanced++
		} else {
			status.Pending++
			if e != nil && first == nil {
				first = e
			}
		}
	}
	return first
}
