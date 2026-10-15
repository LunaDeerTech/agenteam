package service

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// SkillPrepareConnectionLocks describes only the original authenticated
// connection. The opaque reservation and current database row are checked again
// in the caller's existing transaction; these public locks are not a grant.
func (s *Service) SkillPrepareConnectionLocks(connection Connection) ([]f.LockRequest, error) {
	r, err := s.reservation(connection)
	if err != nil {
		return nil, err
	}
	key, err := f.SystemConfigLock("runner-control-" + r.runner.String())
	if err != nil {
		return nil, fault(f.InvalidArgument)
	}
	return []f.LockRequest{{Key: key, Mode: f.Shared}}, nil
}

// RequireSkillPrepareConnectionInTx opens no transaction, acquires no late
// locks and reconstructs no Connection. It returns the actual original
// authenticated generation only while that same hello/live/device row remains
// current in the original Service and Store.
func (s *Service) RequireSkillPrepareConnectionInTx(ctx context.Context, tx f.Tx, connection Connection) (f.Version, error) {
	r, err := s.reservation(connection)
	if err != nil {
		return 0, err
	}
	locks, err := s.SkillPrepareConnectionLocks(connection)
	if err != nil {
		return 0, err
	}
	owned, done, err := s.begin(ctx)
	if err != nil {
		return 0, err
	}
	defer done()
	store := s.state().authority.state().store
	if err = store.RequireHeldLocks(owned, tx, locks); err != nil {
		return 0, portError(err)
	}
	x, err := store.InTx(tx)
	if err != nil {
		return 0, portError(err)
	}
	var current bool
	err = x.QueryRow(owned, `SELECT EXISTS(SELECT 1 FROM agenteam_runner.connections c JOIN agenteam_runner.runners r ON r.id=c.runner_id WHERE c.runner_id=$1 AND c.id=$2 AND c.owner_id=$3 AND c.credential_generation=$4 AND c.generation=$5 AND r.credential_generation=c.credential_generation AND r.connection_generation=c.generation AND r.device_public_key IS NOT NULL AND c.hello_at IS NOT NULL AND c.lease_expires_at>clock_timestamp())`, r.runner.String(), r.id.String(), s.state().owner.String(), r.credential, r.generation).Scan(&current)
	if err != nil {
		return 0, unavailable(err)
	}
	if err = owned.Err(); err != nil {
		return 0, canceled(err)
	}
	if !current {
		return 0, fault(f.Unauthenticated)
	}
	return f.Version(r.generation), nil
}
