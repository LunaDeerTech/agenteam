package model

import (
	"context"
	"encoding/json"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"time"
)

func (m *Management) finish(parent context.Context, o managementOperation, result ManagementResult) (ManagementResult, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	var req sc.UsageRequest
	var plan sc.UsageDependencies
	var e error
	locks := []f.LockRequest{managementLock(o.ID, f.Exclusive)}
	if o.Credential != "" {
		ctx, req, e = m.authority.context(ctx, o)
		if e != nil {
			return ManagementResult{}, managementUnknown()
		}
		req.Action = sc.ReleaseLeaseUsage
		plan, e = m.secrets.DiscoverUsage(ctx, req)
		if e != nil {
			return ManagementResult{}, managementUnknown()
		}
		locks = append(locks, plan.RequiredLocks()...)
	}
	cause, e := readCause("management-finish")
	if e != nil {
		return ManagementResult{}, e
	}
	raw, e := json.Marshal(result)
	if e != nil {
		return ManagementResult{}, unavailable(e)
	}
	st := m.service.state()
	commit := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return e
		}
		current, e := readManagement(ctx, x, "id", o.ID)
		if e != nil {
			return e
		}
		if current == nil {
			return fault(f.NotFound)
		}
		if len(current.Result) != 0 {
			return json.Unmarshal(current.Result, &result)
		}
		if current.Process != o.Process || current.Lease != o.Lease {
			return fault(f.Forbidden)
		}
		if o.Credential != "" {
			if _, e = m.secrets.ApplyUsageInTx(ctx, tx, req, plan); e != nil {
				return e
			}
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_model.management_operations SET result=$2 WHERE id=$1`, o.ID, raw)
		return e
	})
	if commitError(commit) != nil {
		return ManagementResult{}, managementUnknown()
	}
	return result, nil
}
func (m *Management) recoverOne(ctx context.Context, o managementOperation) error {
	m.mu.Lock()
	active := m.active[o.ID]
	m.mu.Unlock()
	if active {
		return managementUnknown()
	}
	current, e := m.process.CurrentManagementProcess()
	if e != nil {
		return e
	}
	if o.Process != current {
		if e = m.process.ConfirmManagementProcessStopped(ctx, o.Process); e != nil {
			return e
		}
	}
	// An inactive operation in this process has completed all synchronous I/O.
	// Foreign operations require the original ProcessGuard to be actually gone.
	_, e = m.finish(ctx, o, o.result("interrupted", 0))
	return e
}
func (m *Management) Recover(ctx context.Context) error {
	if m == nil {
		return fault(f.DependencyUnbound)
	}
	rows, e := m.service.state().store.Query(ctx, `SELECT id::text FROM agenteam_model.management_operations WHERE result IS NULL ORDER BY created_at,id`)
	if e != nil {
		return unavailable(e)
	}
	var keys []string
	for rows.Next() {
		var key string
		if e = rows.Scan(&key); e != nil {
			rows.Close()
			return unavailable(e)
		}
		keys = append(keys, key)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return unavailable(e)
	}
	for _, key := range keys {
		o, e := readManagement(ctx, m.service.state().store, "id", key)
		if e != nil {
			return e
		}
		if o == nil || len(o.Result) != 0 {
			continue
		}
		if e = m.recoverOne(ctx, *o); e != nil {
			return e
		}
	}
	return nil
}
