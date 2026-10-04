package object

import (
	"context"
	"errors"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/jackc/pgx/v5"
)

// Initialize verifies the pre-created private bucket and binds it to this DB.
// No external request runs with a transaction or advisory lock held. A reserved
// DB identity survives either side's unknown commit and is never regenerated.
func (s *Service) Initialize(ctx context.Context) error {
	op, finish, err := s.admit(ctx, true)
	if err != nil {
		return err
	}
	defer finish()
	ctx = op.ctx
	if err = s.state().backend.CheckBucket(ctx); err != nil {
		return err
	}
	key, _ := foundation.SystemConfigLock("object-store-identity")
	var id oc.ObjectID
	var phase string
	result := s.state().store.WithinTx(ctx, recoveryCause(), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}); err != nil {
			return unavailable(err)
		}
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		var raw string
		err = e.QueryRow(ctx, `SELECT instance_id::text,phase FROM agenteam_object.store_identity WHERE singleton`).Scan(&raw, &phase)
		if errors.Is(err, pgx.ErrNoRows) {
			var n int64
			if err = e.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.objects`).Scan(&n); err != nil {
				return unavailable(err)
			}
			if n != 0 {
				return unavailable(nil)
			}
			id, err = foundation.NewID[oc.StoredObject]()
			if err != nil {
				return unavailable(err)
			}
			phase = "reserved"
			_, err = e.Exec(ctx, `INSERT INTO agenteam_object.store_identity(singleton,instance_id,phase) VALUES(true,$1,'reserved')`, id.String())
			return unavailableIf(err)
		}
		if err != nil {
			return unavailable(err)
		}
		id, err = foundation.ParseID[oc.StoredObject](raw)
		return unavailableIf(err)
	})
	if err = commitError(result); err != nil {
		return err
	}
	marker, err := s.state().backend.readControl(ctx)
	exists := !hasCode(err, foundation.ObjectPayloadMissing)
	if err != nil && exists {
		return err
	}
	if !exists {
		if phase != "reserved" {
			return unavailable(nil)
		}
		empty, err := s.state().backend.empty(ctx)
		if err != nil {
			return err
		}
		if !empty {
			return unavailable(nil)
		}
		// Conditional publication never overwrites another DB's marker. If another
		// initializer won, reread that precise key and require our stable identity.
		putErr := s.state().backend.writeControl(ctx, id.String())
		marker, err = s.state().backend.readControl(ctx)
		exists = !hasCode(err, foundation.ObjectPayloadMissing)
		if err != nil {
			return err
		}
		if !exists {
			return unavailable(putErr)
		}
	}
	if marker != id.String() {
		return unavailable(nil)
	}
	result = s.state().store.WithinTx(ctx, recoveryCause(), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}); err != nil {
			return unavailable(err)
		}
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		tag, err := e.Exec(ctx, `UPDATE agenteam_object.store_identity SET phase='confirmed' WHERE singleton AND instance_id=$1`, id.String())
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return unavailable(nil)
		}
		return nil
	})
	if err = commitError(result); err != nil {
		return err
	}
	r := s.state()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped || r.forced {
		return failure(foundation.ShuttingDown, nil)
	}
	r.initialized = true
	return nil
}
func unavailableIf(err error) error {
	if err != nil {
		return unavailable(err)
	}
	return nil
}

// Check does not initialize, repair or silently accept missing bucket controls.
func (s *Service) Check(ctx context.Context) error {
	op, finish, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer finish()
	ctx = op.ctx
	if err = s.state().backend.CheckBucket(ctx); err != nil {
		return err
	}
	var id, phase string
	if err = s.state().store.QueryRow(ctx, `SELECT instance_id::text,phase FROM agenteam_object.store_identity WHERE singleton`).Scan(&id, &phase); err != nil {
		return unavailable(err)
	}
	if phase != "confirmed" {
		return unavailable(nil)
	}
	marker, err := s.state().backend.readControl(ctx)
	exists := !hasCode(err, foundation.ObjectPayloadMissing)
	if err != nil {
		return err
	}
	if !exists || marker != id {
		return unavailable(nil)
	}
	return nil
}
