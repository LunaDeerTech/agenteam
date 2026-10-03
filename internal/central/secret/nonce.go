package secret

import (
	"context"

	"errors"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/jackc/pgx/v5"
)

const nonceReservationSize = uint64(1024)

type nonceRange struct{ next, end uint64 }

// nextNonce is used exclusively by transaction-external preparation. Nothing
// in an ...InTx path can reserve or refill a range. A range is published into
// this process only after the dedicated reservation transaction commits.
func (s *Service) nextNonce(ctx context.Context, version foundation.Version) ([]byte, error) {
	state := s.state()
	state.nonceMu.Lock()
	defer state.nonceMu.Unlock()
	if r := state.nonces[version]; r != nil && r.next <= r.end {
		counter := r.next
		r.next++
		return masterNonce(counter)
	}
	cause, err := recoveryCause("secret-nonce")
	if err != nil {
		return nil, err
	}
	var low, high int64
	result := state.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		e, err := state.store.InTx(tx)
		if err != nil {
			return unavailable(err)
		}
		err = e.QueryRow(ctx, `WITH previous AS (SELECT version,nonce_high_water FROM agenteam_secret.secret_master_registry WHERE version=$1 AND nonce_high_water<$3 FOR UPDATE) UPDATE agenteam_secret.secret_master_registry r SET nonce_high_water=LEAST(previous.nonce_high_water+$2,$3) FROM previous WHERE r.version=previous.version RETURNING previous.nonce_high_water+1,r.nonce_high_water`, int64(version), int64(nonceReservationSize), int64(maxMasterCounter)).Scan(&low, &high)
		if errors.Is(err, pgx.ErrNoRows) {
			return failure(NonceExhausted, foundation.DependencyUnavailable, nil)
		}
		if err != nil {
			return unavailable(err)
		}
		return nil
	})
	if result.State() == foundation.Unknown {
		return nil, failure(NonceReservationUnknown, foundation.CommitUnknown, nil)
	}
	if err = commitError(result); err != nil {
		return nil, err
	}
	// Read the old boundary atomically too: even a final partial range must
	// never infer a starting counter below the actual persisted high-water.
	start := uint64(low)
	state.nonces[version] = &nonceRange{next: start + 1, end: uint64(high)}
	return masterNonce(start)
}
