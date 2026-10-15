package memory

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// Initialize runs after Model.Initialize and before Tool/Execution recovery.
// It observes original stopped calls only. No Resolve, Begin or output replay
// is permitted through this private cleanup owner.
func (r *Runtime) Initialize(ctx context.Context) error {
	if r == nil || r.models == nil {
		return fault(f.DependencyUnbound)
	}
	return r.models.initializeStopped(ctx)
}
func (m *Models) initializeStopped(ctx context.Context) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if m == nil || m.authority == nil || m.authority.guard == nil || nilPort(m.deps.Stopped) {
		return fault(f.DependencyUnbound)
	}
	m.mu.Lock()
	if m.initializing {
		m.mu.Unlock()
		return fault(f.ResourceBusy)
	}
	if m.initialized {
		m.mu.Unlock()
		return nil
	}
	m.initializing = true
	m.initializeDone = make(chan struct{})
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.initializing = false; close(m.initializeDone); m.mu.Unlock() }()
	cause, err := readCause("model-stopped-candidates")
	if err != nil {
		return err
	}
	var ids []mc.CallID
	result := m.domain.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := m.authority.guard.RequireCurrentProcessInTx(ctx, tx, m.authority.process); err != nil {
			return portError(err)
		}
		x, err := m.domain.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		rows, err := x.Query(ctx, `SELECT call_id::text FROM agenteam_memory.model_calls WHERE NOT retired AND (process_id IS NULL OR process_id<>$1) ORDER BY created_at,call_id LIMIT 129`, m.authority.process.String())
		if err != nil {
			return portError(err)
		}
		defer rows.Close()
		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				return portError(err)
			}
			call, err := f.ParseID[mc.Call](raw)
			if err != nil {
				return portError(err)
			}
			ids = append(ids, call)
		}
		return portError(rows.Err())
	})
	if err = transactionError(result); err != nil {
		return err
	}
	if len(ids) > 128 {
		return fault(f.ResourceBusy)
	}
	for _, id := range ids {
		if err = m.recoverStoppedModel(ctx, id); err != nil {
			return err
		}
	}
	m.mu.Lock()
	m.initialized = true
	m.mu.Unlock()
	return nil
}
func (m *Models) recoverStoppedModel(ctx context.Context, id mc.CallID) error {
	row, err := loadStoppedModelRow(ctx, m.domain.store, id)
	if err != nil {
		return err
	}
	if row.Retired {
		return nil
	}
	if row.Process == nil || *row.Process == m.authority.process {
		return fault(f.Forbidden)
	}
	if err = m.authority.guard.ConfirmStopped(ctx, *row.Process); err != nil {
		return portError(err)
	}
	return m.observeStoppedModel(ctx, row, true)
}
func (m *Models) observeStoppedModel(ctx context.Context, row stoppedModelRow, allowResolution bool) error {
	if row.Snapshot == nil {
		if allowResolution {
			return m.recoverStoppedResolution(ctx, row)
		}
		return fault(f.ResourceBusy)
	}
	request, err := row.request(m.authority)
	if err != nil {
		return err
	} // A missing snapshot needs the separate resolution-settled proof.
	mapping, err := row.mapping()
	if err != nil {
		return err
	}
	w := &stoppedModelWitness{authority: m.authority, row: row, current: m.authority.process, mapping: mapping, request: request}
	w.active.Store(true)
	defer w.active.Store(false)
	run := context.WithValue(ctx, stoppedModelKey{}, w)
	observation, err := m.deps.Stopped.LookupStoppedCall(run, request)
	if err != nil {
		return portError(err)
	}
	if observation.Validate() != nil {
		return fault(f.ResourceBusy)
	}
	if !observation.Found && allowResolution {
		return m.recoverStoppedResolution(ctx, row)
	}
	if !observation.Found {
		return fault(f.ResourceBusy)
	}
	attempt := observation.Attempt
	if attempt.CallID != row.Call || attempt.ProcessID != *row.Process || row.Invocation != nil && *row.Invocation != attempt.InvocationID || row.AttemptIndex != nil && *row.AttemptIndex != attempt.AttemptIndex || row.AttemptFence != nil && *row.AttemptFence != attempt.Fence {
		return fault(f.Forbidden)
	}
	return m.markStoppedModelRetired(run, w, *attempt)
}
func (m *Models) markStoppedModelRetired(ctx context.Context, w *stoppedModelWitness, attempt mc.AttemptIdentity) error {
	plan, err := m.authority.DiscoverStoppedCall(ctx, w.request)
	if err != nil {
		return err
	}
	cause, err := readCause("model-stopped-retire")
	if err != nil {
		return err
	}
	after := w.row
	after.Retired = true
	after.Invocation = &attempt.InvocationID
	after.AttemptIndex = &attempt.AttemptIndex
	after.AttemptProcess = &attempt.ProcessID
	after.AttemptFence = &attempt.Fence
	expected, err := after.mapping()
	if err != nil {
		return err
	}
	transact := func(check func(context.Context, f.Tx, postgres.SQLExecutor) error) f.CommitResult {
		return m.domain.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
			if err := m.domain.store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
				return portError(err)
			}
			if err := m.authority.guard.RequireCurrentProcessInTx(ctx, tx, m.authority.process); err != nil {
				return portError(err)
			}
			x, err := m.domain.store.InTx(tx)
			if err != nil {
				return portError(err)
			}
			return check(ctx, tx, x)
		})
	}
	result := transact(func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if err := m.authority.ValidateStoppedCallInTx(ctx, tx, w.request, plan); err != nil {
			return err
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_memory.model_calls SET retired=true,invocation_id=$2,attempt_index=$3,attempt_process_id=$4,attempt_fence=$5 WHERE call_id=$1 AND NOT retired`, w.row.Call.String(), attempt.InvocationID.String(), int64(attempt.AttemptIndex), attempt.ProcessID.String(), int64(attempt.Fence))
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.VersionConflict)
		}
		return nil
	})
	err = transactionError(result)
	if !isUnknown(err) {
		return err
	}
	// A transport-unknown domain mark gets only one same-lock observation; it
	// never repeats a Model call, lease release or the original write callback.
	confirmed := false
	observed := transact(func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		if _, err := m.authority.stoppedWitness(ctx, w.request); err != nil {
			return err
		}
		actual, err := loadStoppedModelRow(ctx, x, w.row.Call)
		if err != nil {
			return err
		}
		actualMapping, err := actual.mapping()
		if err != nil {
			return err
		}
		confirmed = actualMapping == expected
		if !confirmed && actualMapping != w.mapping {
			return fault(f.ConfirmationStale)
		}
		return nil
	})
	if observationErr := transactionError(observed); observationErr != nil {
		// The observation did not determine the original write outcome.
		return err
	}
	if confirmed {
		return nil
	}
	return err
}
