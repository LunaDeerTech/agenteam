package account

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type localCheckpoint struct {
	key, id   string
	identity  foundation.CommandIdentity
	login     *loginOperation
	response  *responseState
	bootstrap *operation
}

// A COMMIT may remain unknown after the caller has joined while its eventual
// rollback leaves no durable row for the normal recovery scan. Retire only the
// local bookkeeping, after serializing an absence/terminal observation with the
// original writer. This never releases a lease or proves a process stopped.
func (s *Service) recoverLocal(ctx context.Context, status *RecoveryStatus) error {
	st := s.state()
	st.mu.Lock()
	var candidates []localCheckpoint
	bootstrapKey, _ := foundation.NewCommandIdentity("account.bootstrap", nil, "initialize", "initial-administrator")
	for id, op := range st.bootstraps {
		if operationJoined(op) {
			candidates = append(candidates, localCheckpoint{key: "bootstrap:" + id, id: id, identity: bootstrapKey, bootstrap: op})
		}
	}
	for id, v := range st.logins {
		if operationJoined(v.op) {
			candidates = append(candidates, localCheckpoint{key: "login:" + id, id: id, identity: v.identity, login: v})
		}
	}
	for id, v := range st.responses {
		if operationJoined(v.op) {
			candidates = append(candidates, localCheckpoint{key: "response:" + id, id: id, identity: v.identity, response: v})
		}
	}
	slices.SortFunc(candidates, func(a, b localCheckpoint) int {
		return cmp.Compare(a.key, b.key)
	})
	start := sort.Search(len(candidates), func(i int) bool { return candidates[i].key > st.localCursor })
	batch := make([]localCheckpoint, 0, min(100, len(candidates)))
	for i := 0; i < min(100, len(candidates)); i++ {
		batch = append(batch, candidates[(start+i)%len(candidates)])
	}
	st.mu.Unlock()
	var first error
	for _, v := range batch {
		if ctx.Err() != nil {
			return unavailable(ctx.Err())
		}
		st.mu.Lock()
		st.localCursor = v.key
		st.mu.Unlock()
		item, cancel := context.WithTimeout(ctx, 2*time.Second)
		removed, err := s.retireLocal(item, v)
		if ctx.Err() == nil && item.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			err = fault(foundation.ResourceBusy, err)
		}
		cancel()
		status.Examined++
		if removed {
			status.Advanced++
		} else {
			status.Pending++
		}
		if err != nil && !hasFaultCode(err, foundation.ResourceBusy) && first == nil {
			first = err
		}
	}
	return first
}

func (s *Service) retireLocal(ctx context.Context, v localCheckpoint) (bool, error) {
	st := s.state()
	cause, err := recoveryCause("local-checkpoint")
	if err != nil {
		return false, err
	}
	var required bool
	r := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, []foundation.LockRequest{commandLock(v.identity), recordLock(v.id)}); err != nil {
			return unavailable(err)
		}
		x, err := st.store.InTx(tx)
		if err != nil {
			return unavailable(err)
		}
		if v.bootstrap != nil {
			err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.bootstrap WHERE operation_id=$1 AND log_state IN ('eligible','attempted'))`, v.id).Scan(&required)
		} else if v.login != nil {
			err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.commands WHERE id=$1 AND phase='planned')`, v.id).Scan(&required)
		} else {
			err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.response_plans WHERE id=$1)`, v.id).Scan(&required)
		}
		return portError(err)
	})
	if err = resultError(r); err != nil {
		return false, err
	}
	if required {
		return false, nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if v.bootstrap != nil && st.bootstraps[v.id] == v.bootstrap {
		delete(st.bootstraps, v.id)
	}
	if v.login != nil && st.logins[v.id] == v.login {
		delete(st.logins, v.id)
	}
	if v.response != nil && st.responses[v.id] == v.response {
		delete(st.responses, v.id)
	}
	return true, nil
}
