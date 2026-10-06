package audit

import (
	"context"
	"time"

	contract "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

const systemReadBudget = 3 * time.Second

type systemReadAttempt struct{}

// ListSystem rechecks the current Session and administrator while holding the
// same User shared lock and transaction as the complete, bounded observation.
func (s *Service) ListSystem(ctx context.Context, actor identity.Actor, filter contract.Filter, page foundation.PageRequest) (foundation.Page[contract.SafeRecord], error) {
	var candidate foundation.Page[contract.SafeRecord]
	err := s.systemRead(ctx, actor, func(ctx context.Context, sql postgres.SQLExecutor) (err error) {
		candidate, err = s.listRecords(ctx, sql, identity.SystemScope(), filter, page, systemRecord)
		return err
	})
	if err != nil {
		return foundation.Page[contract.SafeRecord]{}, err
	}
	return candidate, nil
}

// GetSystem has the same transaction and publication boundary as ListSystem.
func (s *Service) GetSystem(ctx context.Context, actor identity.Actor, id contract.ID) (contract.SafeRecord, error) {
	var candidate contract.SafeRecord
	err := s.systemRead(ctx, actor, func(ctx context.Context, sql postgres.SQLExecutor) (err error) {
		candidate, err = getRecord(ctx, sql, identity.SystemScope(), id, systemRecord)
		return err
	})
	if err != nil {
		return contract.SafeRecord{}, err
	}
	return candidate, nil
}

func systemRecord(record contract.SafeRecord) error {
	if !record.Scope.Equal(identity.SystemScope()) {
		return unavailable(nil)
	}
	return nil
}

func readContextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

// The callback owns candidates; a successful callback is not a successful read.
// WithinTx must actually return Committed before any caller can receive them.
func (s *Service) systemRead(parent context.Context, actor identity.Actor, read func(context.Context, postgres.SQLExecutor) error) error {
	ctx, cancel := context.WithTimeout(parent, systemReadBudget)
	defer cancel()
	if actor.Validate() != nil || actor.Details().Kind != identity.Human {
		return failure(foundation.Forbidden, "human_required", nil)
	}
	if err := readContextError(ctx); err != nil {
		return unavailable(err)
	}
	if s == nil || nilPort(s.store) {
		return failure(foundation.DependencyUnbound, "audit_unbound", nil)
	}
	run, err := foundation.NewID[systemReadAttempt]()
	if err != nil {
		return unavailable(err)
	}
	cause, err := foundation.NewRecoveryCause("audit.system-read", run.String(), "")
	if err != nil {
		return unavailable(err)
	}
	lock, err := foundation.UserLock(actor.Details().UserID)
	if err != nil {
		return unavailable(err)
	}
	complete := false
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := readContextError(ctx); err != nil {
			return unavailable(err)
		}
		if err := s.store.Acquire(ctx, tx, lock, foundation.Shared); err != nil {
			return unavailable(err)
		}
		if err := s.authorizeHuman(ctx, tx, actor, identity.SystemScope(), identity.Read); err != nil {
			return err
		}
		sql, err := s.store.InTx(tx)
		if err != nil {
			return unavailable(err)
		}
		if err = read(ctx, sql); err != nil {
			return err
		}
		if err = readContextError(ctx); err != nil {
			return unavailable(err)
		}
		complete = true
		return nil
	})
	switch result.State() {
	case foundation.NotCommitted:
		if fault := result.Fault(); fault != nil {
			return fault
		}
		return foundation.NewFault(foundation.InternalError, foundation.NotCommitted)
	case foundation.Committed:
		if err := readContextError(ctx); err != nil {
			return unavailable(err)
		}
		if !complete {
			return unavailable(nil)
		}
		return nil
	default:
		// portError deliberately does not know this new read-transaction
		// boundary. Using it here would lose the real Unknown classification.
		return failure(foundation.CommitUnknown, "system_read_unknown", nil)
	}
}
