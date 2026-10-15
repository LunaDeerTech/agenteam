package memory

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"slices"
)

func (m *Models) recoverStoppedResolution(ctx context.Context, row stoppedModelRow) error {
	observer, ok := m.deps.Stopped.(mc.StoppedResolutionObserver)
	if !ok || nilPort(observer) {
		return fault(f.DependencyUnbound)
	}
	request, err := row.resolutionRequest(m.authority)
	if err != nil {
		return err
	}
	mapping, err := row.mapping()
	if err != nil {
		return err
	}
	w := &stoppedResolutionWitness{authority: m.authority, row: row, request: request, mapping: mapping, current: m.authority.process}
	w.active.Store(true)
	defer w.active.Store(false)
	run := context.WithValue(ctx, stoppedResolutionKey{}, w)
	observation, err := observer.LookupStoppedResolution(run, request)
	if err != nil {
		return portError(err)
	}
	if observation.Validate() != nil {
		return fault(f.ResourceBusy)
	}
	if observation.Model == nil {
		// Only the formal original-process/resolution observer may prove that
		// neither Resolve nor credential acquisition committed.
		// Resolve discovery already binds the draft snapshot ID in Memory's
		// original transaction. That ID alone is not a committed resolution.
		if row.Lease != nil || row.Invocation != nil {
			return fault(f.ConfirmationStale)
		}
		after := row
		after.Retired = true
		version := f.Version(1)
		after.TerminalVersion = &version
		return m.changeStoppedResolution(run, w, after, nil, nil, nil)
	}
	resolved := observation.Model.Clone()
	if !resolved.Consumer.Equal(request.Resolve.Consumer) || !resolved.LeaseOwner.Equal(request.Resolve.LeaseOwner) || row.Snapshot != nil && *row.Snapshot != resolved.Snapshot.ID {
		return fault(f.Forbidden)
	}
	if row.Lease != nil && (resolved.CredentialLease == nil || *row.Lease != resolved.CredentialLease.LeaseID) {
		return fault(f.Forbidden)
	}
	after := row
	after.Snapshot = &resolved.Snapshot.ID
	if resolved.CredentialLease != nil {
		after.Lease = &resolved.CredentialLease.LeaseID
	}
	if !observation.CallFound {
		version := f.Version(1)
		after.TerminalVersion = &version
	}
	if err = m.changeStoppedResolution(run, w, after, nil, nil, nil); err != nil {
		return err
	}
	if observation.CallFound {
		// A found Model row still requires its own stopped invocation/Usage
		// proof. Do not infer retirement from resolution or recurse on absence.
		return m.observeStoppedModel(ctx, w.row, false)
	}
	if resolved.CredentialLease == nil {
		after = w.row
		after.Retired = true
		return m.changeStoppedResolution(run, w, after, nil, nil, nil)
	}
	retirement := mc.ModelCallRetirementRequest{Actor: request.Resolve.Actor, Model: resolved, TerminalVersion: 1}
	if retirement.Validate() != nil {
		return fault(f.DependencyUnavailable)
	}
	w.retirement = &retirement
	plan, err := m.deps.Retirement.DiscoverModelCallRetirement(run, retirement)
	if err != nil {
		return portError(err)
	}
	if nilPort(plan) {
		return fault(f.DependencyUnbound)
	}
	after = w.row
	after.Retired = true
	return m.changeStoppedResolution(run, w, after, plan.RequiredLocks(), func(ctx context.Context, tx f.Tx) error {
		return m.deps.Retirement.ReleaseModelCallInTx(ctx, tx, retirement, plan)
	}, func(ctx context.Context, tx f.Tx) (bool, error) {
		return m.deps.Retirement.ModelCallRetiredInTx(ctx, tx, retirement, plan)
	})
}

// The caller owns the original active cleanup witness. Unknown is observed
// under the same lock union and never causes the write callback to be replayed.
func (m *Models) changeStoppedResolution(ctx context.Context, w *stoppedResolutionWitness, after stoppedModelRow, extra []f.LockRequest, apply func(context.Context, f.Tx) error, observe func(context.Context, f.Tx) (bool, error)) error {
	locks, err := w.locks()
	if err != nil {
		return err
	}
	locks, err = normalizeLocks(append(locks, slices.Clone(extra)...))
	if err != nil {
		return err
	}
	expected, err := after.mapping()
	if err != nil {
		return err
	}
	if expected == w.mapping && apply == nil {
		return nil
	}
	cause, err := readCause("stopped-resolution-retire")
	if err != nil {
		return err
	}
	transact := func(fn func(context.Context, f.Tx, postgres.SQLExecutor) error) f.CommitResult {
		return m.domain.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
			if _, err := m.authority.resolutionWitness(ctx); err != nil {
				return err
			}
			if err := m.domain.store.AcquireAll(ctx, tx, locks); err != nil {
				return portError(err)
			}
			if err := m.authority.guard.RequireCurrentProcessInTx(ctx, tx, m.authority.process); err != nil {
				return portError(err)
			}
			x, err := m.domain.store.InTx(tx)
			if err != nil {
				return portError(err)
			}
			return fn(ctx, tx, x)
		})
	}
	result := transact(func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if err := m.authority.validateStoppedResolutionRow(ctx, tx, w, false); err != nil {
			return err
		}
		if apply != nil {
			if err := apply(ctx, tx); err != nil {
				return portError(err)
			}
		}
		var snapshot, lease, terminal any
		if after.Snapshot != nil {
			snapshot = after.Snapshot.String()
		}
		if after.Lease != nil {
			lease = after.Lease.String()
		}
		if after.TerminalVersion != nil {
			terminal = int64(*after.TerminalVersion)
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_memory.model_calls SET snapshot_id=$2,lease_id=$3,terminal_version=$4,retired=$5 WHERE call_id=$1 AND NOT retired`, w.row.Call.String(), snapshot, lease, terminal, after.Retired)
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.VersionConflict)
		}
		return nil
	})
	err = transactionError(result)
	if isUnknown(err) {
		confirmed := false
		observed := transact(func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
			actual, err := loadStoppedModelRow(ctx, x, w.row.Call)
			if err != nil {
				return err
			}
			mapping, err := actual.mapping()
			if err != nil {
				return err
			}
			if mapping != expected && mapping != w.mapping {
				return fault(f.ConfirmationStale)
			}
			confirmed = mapping == expected
			if confirmed && observe != nil {
				confirmed, err = observe(ctx, tx)
				if err != nil {
					return portError(err)
				}
			}
			return nil
		})
		if observedErr := transactionError(observed); observedErr != nil {
			// Preserve the original mutation attempt and cause on failed observation.
			return err
		}
		if !confirmed {
			return err
		}
		err = nil
	}
	if err != nil {
		return err
	}
	w.row, w.mapping = after, expected
	return nil
}
