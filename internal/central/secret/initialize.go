package secret

import (
	"context"
	"crypto/subtle"
	"errors"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/jackc/pgx/v5"
)

// Initialize validates the configured history, canaries and every required key
// before publishing availability. It performs only finite startup work; bulk
// rewrapping belongs to the separately supervised maintenance worker.
func (s *Service) Initialize(ctx context.Context) error {
	state := s.state()
	for _, version := range state.keys.Versions() {
		if err := s.registerAndVerify(ctx, version); err != nil {
			return err
		}
	}
	if err := s.verifyRequiredKeys(ctx); err != nil {
		return err
	}
	if err := s.establishWriteFence(ctx); err != nil {
		return err
	}
	version, epoch, err := s.control(ctx, state.store)
	if err != nil {
		return err
	}
	var rotation string
	var remaining int64
	if err = state.store.QueryRow(ctx, `SELECT state FROM agenteam_secret.secret_rotation_runs WHERE target_version=$1`, int64(version)).Scan(&rotation); err != nil {
		return unavailable(err)
	}
	if err = state.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_secret.secret_payloads WHERE master_version<>$1`, int64(version)).Scan(&remaining); err != nil {
		return unavailable(err)
	}
	state.mu.Lock()
	state.initialized = true
	state.unavailable = false
	state.writeVersion = version
	state.epoch = epoch
	state.rotationState = rotation
	state.remaining = foundation.Progress(remaining)
	state.mu.Unlock()
	return nil
}
func (s *Service) registerAndVerify(ctx context.Context, version foundation.Version) error {
	state := s.state()
	key, ok := state.keys.key(version)
	if !ok {
		return failure(KeyUnavailable, foundation.DependencyUnavailable, nil)
	}
	fingerprint := keyFingerprint(key)
	clear(key[:])
	registration, err := foundation.NewID[struct{}]()
	if err != nil {
		return unavailable(err)
	}
	cause, err := recoveryCause("secret-registry")
	if err != nil {
		return err
	}
	var rawRegistration string
	var saved, nonce, encrypted []byte
	result := state.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := state.store.Acquire(ctx, tx, writeLock(), foundation.Exclusive); err != nil {
			return unavailable(err)
		}
		e, err := state.store.InTx(tx)
		if err != nil {
			return unavailable(err)
		}
		if _, err = e.Exec(ctx, `INSERT INTO agenteam_secret.secret_master_registry(version,fingerprint,registration_id) VALUES($1,$2,$3) ON CONFLICT(version) DO NOTHING`, int64(version), fingerprint[:], registration.String()); err != nil {
			return failure(KeyReused, foundation.DependencyUnavailable, err)
		}
		if err = e.QueryRow(ctx, `SELECT registration_id::text,fingerprint,canary_nonce,canary_ciphertext FROM agenteam_secret.secret_master_registry WHERE version=$1`, int64(version)).Scan(&rawRegistration, &saved, &nonce, &encrypted); err != nil {
			return unavailable(err)
		}
		if subtle.ConstantTimeCompare(saved, fingerprint[:]) != 1 {
			return failure(KeyReused, foundation.DependencyUnavailable, nil)
		}
		return nil
	})
	if err = commitError(result); err != nil {
		return err
	}
	if len(nonce) != 0 || len(encrypted) != 0 {
		return verifyCanary(state.keys, version, nonce, encrypted)
	}
	// A registered-but-pending key always consumes a new confirmed nonce. A
	// concurrent initializer may win the conditional update; both nonces burn.
	nonce, err = s.nextNonce(ctx, version)
	if err != nil {
		return err
	}
	encrypted, err = sealCanary(state.keys, version, nonce)
	if err != nil {
		return err
	}
	cause, err = recoveryCause("secret-canary")
	if err != nil {
		return err
	}
	result = state.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := state.store.Acquire(ctx, tx, writeLock(), foundation.Exclusive); err != nil {
			return unavailable(err)
		}
		e, err := state.store.InTx(tx)
		if err != nil {
			return unavailable(err)
		}
		tag, err := e.Exec(ctx, `UPDATE agenteam_secret.secret_master_registry SET canary_nonce=$2,canary_ciphertext=$3 WHERE version=$1 AND canary_nonce IS NULL AND canary_ciphertext IS NULL`, int64(version), nonce, encrypted)
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() == 1 {
			return s.maintenanceAudit(ctx, tx, ac.MasterRegister, version, rawRegistration, 0, 0, "")
		}
		return nil
	})
	if err = commitError(result); err != nil {
		return err
	}
	if err = state.store.QueryRow(ctx, `SELECT canary_nonce,canary_ciphertext FROM agenteam_secret.secret_master_registry WHERE version=$1`, int64(version)).Scan(&nonce, &encrypted); err != nil {
		return unavailable(err)
	}
	return verifyCanary(state.keys, version, nonce, encrypted)
}
func (s *Service) verifyRequiredKeys(ctx context.Context) error {
	state := s.state()
	rows, err := state.store.Query(ctx, `SELECT version,retireable FROM agenteam_secret.secret_master_registry WHERE version<=coalesce((SELECT current_write_version FROM agenteam_secret.secret_control WHERE singleton),0)`)
	if err != nil {
		return unavailable(err)
	}
	for rows.Next() {
		var version int64
		var retireable bool
		if err = rows.Scan(&version, &retireable); err != nil {
			rows.Close()
			return unavailable(err)
		}
		if _, ok := state.keys.key(foundation.Version(version)); !ok && !retireable {
			rows.Close()
			return failure(KeyUnavailable, foundation.DependencyUnavailable, nil)
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return unavailable(err)
	}
	// One actual authenticated DEK per required master version, including
	// encrypted command receipt digests. No raw key/DEK enters a generic DTO.
	rows, err = state.store.Query(ctx, `SELECT DISTINCT ON(master_version) `+payloadColumns+` FROM agenteam_secret.secret_payloads ORDER BY master_version,payload_id`)
	if err != nil {
		return unavailable(err)
	}
	for rows.Next() {
		p, err := scanPayload(rows)
		if err != nil {
			rows.Close()
			return err
		}
		dek, err := unwrap(state.keys, p)
		clear(dek)
		if err != nil {
			rows.Close()
			return err
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return unavailable(err)
	}
	return nil
}
func (s *Service) establishWriteFence(ctx context.Context) error {
	state := s.state()
	target := state.keys.CurrentVersion()
	cause, err := recoveryCause("secret-write-fence")
	if err != nil {
		return err
	}
	runID, err := foundation.NewID[struct{}]()
	if err != nil {
		return unavailable(err)
	}
	result := state.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := state.store.Acquire(ctx, tx, writeLock(), foundation.Exclusive); err != nil {
			return unavailable(err)
		}
		e, err := state.store.InTx(tx)
		if err != nil {
			return unavailable(err)
		}
		var retired bool
		if err = e.QueryRow(ctx, `SELECT retireable FROM agenteam_secret.secret_master_registry WHERE version=$1`, int64(target)).Scan(&retired); err != nil {
			return unavailable(err)
		}
		if retired {
			return failure(KeyReused, foundation.InvalidState, nil)
		}
		var current, epoch int64
		err = e.QueryRow(ctx, `SELECT current_write_version,write_epoch FROM agenteam_secret.secret_control WHERE singleton`).Scan(&current, &epoch)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return unavailable(err)
		}
		if err == nil && current > int64(target) {
			return failure(EpochChanged, foundation.InvalidState, nil)
		}
		var unfinished bool
		if err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secret_rotation_runs WHERE state<>'completed' AND target_version<>$1)`, int64(target)).Scan(&unfinished); err != nil {
			return unavailable(err)
		}
		if unfinished {
			return failure(Busy, foundation.ResourceBusy, nil)
		}
		if current == 0 {
			if _, err = e.Exec(ctx, `INSERT INTO agenteam_secret.secret_control(singleton,current_write_version,write_epoch) VALUES(true,$1,1)`, int64(target)); err != nil {
				return unavailable(err)
			}
		} else if current != int64(target) {
			if _, err = e.Exec(ctx, `UPDATE agenteam_secret.secret_control SET current_write_version=$1,write_epoch=write_epoch+1 WHERE singleton`, int64(target)); err != nil {
				return unavailable(err)
			}
		}
		tag, err := e.Exec(ctx, `INSERT INTO agenteam_secret.secret_rotation_runs(run_id,target_version,state) VALUES($1,$2,'running') ON CONFLICT(target_version) DO NOTHING`, runID.String(), int64(target))
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() == 1 {
			if err = s.maintenanceAudit(ctx, tx, ac.RotationStart, target, runID.String(), 0, 0, ""); err != nil {
				return err
			}
		}
		_, err = e.Exec(ctx, `UPDATE agenteam_secret.secret_rotation_runs SET state='running',safe_error=NULL,updated_at=clock_timestamp() WHERE target_version=$1 AND state='failed'`, int64(target))
		if err != nil {
			return unavailable(err)
		}
		return nil
	})
	return commitError(result)
}
func (s *Service) maintenanceAudit(ctx context.Context, tx foundation.Tx, action ac.Action, version foundation.Version, cause string, ordinal int64, count foundation.Progress, reason ac.Reason) error {
	registration, err := identity.RegisterService(identity.SecretMaintenance)
	if err != nil {
		return unavailable(err)
	}
	actor, err := registration.Actor(cause, identity.SystemScope())
	if err != nil {
		return unavailable(err)
	}
	var resource ac.Resource
	var metadata ac.Metadata
	if action == ac.MasterRegister {
		resource, err = ac.NewResource(ac.MasterResource, "")
		metadata, err = ac.MasterMetadata(action, version, "", 0, "")
	} else {
		resource, err = ac.NewResource(ac.RotationResource, cause)
		metadata, err = ac.MasterMetadata(action, version, cause, count, reason)
	}
	if err != nil {
		return unavailable(err)
	}
	outcome := ac.Success
	if action == ac.RotationFailed {
		outcome = ac.Failed
	}
	entry, err := ac.NewEntry(ac.EntryFields{Scope: identity.SystemScope(), Actor: actor, Action: action, Outcome: outcome, Resource: resource, Metadata: metadata})
	if err != nil {
		return unavailable(err)
	}
	key, err := ac.NewAppendKey(ac.MasterProducer, cause, ordinal)
	if err != nil {
		return unavailable(err)
	}
	_, err = s.state().audit.AppendInTx(ctx, tx, entry, key)
	if err != nil {
		return unavailable(err)
	}
	return nil
}
