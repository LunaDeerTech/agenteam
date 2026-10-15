package mcp

import (
	"context"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func (s *Service) recoverOAuthRequests(ctx context.Context) error {
	if s == nil || s.owner == nil || s.options.OAuth == nil || nilPort(s.options.OAuth.Writes) {
		return fail(f.DependencyUnbound)
	}
	after := ""
	for {
		rows, err := s.store.Query(ctx, `SELECT r.id FROM agenteam_mcp.oauth_requests r
 WHERE r.operation IN ('begin','exchange') AND r.process_required AND r.process_id<>$1 AND r.id>$2
 AND NOT EXISTS(SELECT 1 FROM agenteam_mcp.oauth_recovery_claims c WHERE c.request_id=r.id AND c.phase='complete')
 AND (NOT r.io_retired OR NOT r.lease_released OR r.result_credential_id IS NOT NULL OR r.operation='exchange')
 ORDER BY r.id LIMIT 64`, s.owner.process.String(), after)
		if err != nil {
			return safeError(err)
		}
		var batch []string
		for rows.Next() {
			var request string
			if err = rows.Scan(&request); err != nil {
				break
			}
			if _, err = f.ParseID[f.Request](request); err != nil || len(batch) == 64 {
				err = fail(f.InvalidState)
				break
			}
			batch = append(batch, request)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return safeError(err)
		}
		if len(batch) == 0 {
			return ctx.Err()
		}
		for _, request := range batch {
			if err = ctx.Err(); err != nil {
				return err
			}
			// One unverifiable original owner cannot authorize work on itself
			// and cannot prevent independent connections from being reconciled.
			_ = s.recoverOAuthRequest(ctx, request)
			after = request
		}
	}
}

func (s *Service) recoverOAuthRequest(parent context.Context, request string) error {
	ctx, call, err := s.owner.begin(parent)
	if err != nil {
		return err
	}
	retained := false
	defer func() {
		if !retained {
			call.returnedWith(nil)
		}
	}()
	original, err := loadOAuthRecoveryFact(ctx, s.store, request)
	if err != nil {
		return err
	}
	prior, exists, err := loadOAuthRecoveryClaim(ctx, s.store, request)
	if err != nil || exists && prior.phase == "complete" {
		return err
	}
	if err = s.confirmOAuthRecoveryProcesses(ctx, original, prior); err != nil {
		return err
	}
	claim, err := f.NewID[f.Request]()
	if err != nil {
		return safeError(err)
	}
	w := &oauthRecoveryUse{service: s, call: call, original: original, claim: claim.String(), previous: prior.id, previousProcess: prior.process, stopped: true}
	w.locks, err = oauthRecoveryLocks(original)
	if err != nil {
		return err
	}
	w.active.Store(true)
	ctx = context.WithValue(ctx, oauthRecoveryKey{}, w)
	done, err := s.advanceOAuthRecovery(ctx, w)
	if err == nil && done {
		w.active.Store(false)
		return nil
	}
	retained = true
	call.returnedWith(func(observe context.Context) (bool, error) {
		done, err := s.advanceOAuthRecovery(context.WithValue(observe, oauthRecoveryKey{}, w), w)
		if err == nil && done {
			w.active.Store(false)
		}
		return done, err
	})
	return err
}

// A still-valid unclaimed flow survives process restart. The later callback
// must retain its original Session, state digest, current Owner and revision.
// If an exchange exists, that original request alone owns flow retirement.
func oauthBeginRecoveryDeferred(ctx context.Context, x postgres.SQLExecutor, v oauthRecoveryFact) (bool, error) {
	if v.operation != "begin" || v.resultCredential == "" {
		return false, nil
	}
	var deferCleanup bool
	err := x.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM agenteam_mcp.oauth_requests e WHERE e.flow_id=$1 AND e.operation='exchange'
   AND e.project_id=$2 AND e.config_id=$3 AND e.config_revision=$4 AND e.connection_id=$5
   AND e.connection_version=$6 AND e.credential_id=$7 AND e.credential_version=$8)
 OR EXISTS(SELECT 1 FROM agenteam_mcp.oauth_flows o JOIN agenteam_mcp.connections c ON c.id=o.connection_id
   JOIN agenteam_mcp.configs f ON f.id=o.config_id
   WHERE o.id=$1 AND o.begin_request_id=$9 AND o.project_id=$2 AND o.config_id=$3 AND o.config_revision=$4
   AND o.connection_id=$5 AND o.connection_version=$6 AND o.credential_id=$7 AND o.credential_version=$8
   AND o.phase='ready' AND o.consumed_at IS NULL AND o.expires_at>clock_timestamp()
   AND c.project_id=o.project_id AND c.config_id=o.config_id AND c.config_revision=o.config_revision
   AND c.version=o.connection_version AND c.state='auth_required' AND f.runtime_revision=o.config_revision AND f.auth_scheme='oauth')`,
		v.flow, v.project, v.config, int64(v.revision), v.connection, int64(v.version), v.resultCredential, int64(v.resultVersion), v.request).Scan(&deferCleanup)
	return deferCleanup, safeError(err)
}

func (s *Service) claimOAuthRecovery(ctx context.Context, w *oauthRecoveryUse) (bool, error) {
	cause, _ := f.NewRecoveryCause("mcp.oauth.recovery_claim", w.claim, "")
	done := false
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if actual, err := s.oauthRecoveryUse(ctx); err != nil || actual != w {
			return fail(f.Forbidden)
		}
		if err := s.store.AcquireAll(ctx, tx, w.locks); err != nil {
			return err
		}
		if err := s.owner.requireInTx(ctx, tx, w.call); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		current, err := loadOAuthRecoveryFact(ctx, x, w.original.request)
		if err != nil {
			return err
		}
		if !w.original.sameOrigin(current) {
			return fail(f.Forbidden)
		}
		prior, found, err := loadOAuthRecoveryClaim(ctx, x, current.request)
		if err != nil {
			return err
		}
		if found && prior.id == w.claim && prior.process == s.owner.process.String() && prior.originalProcess == current.process && prior.previous == w.previous {
			if prior.phase == "complete" {
				done = true
				return nil
			}
			if prior.phase != "claimed" {
				return fail(f.Forbidden)
			}
			return nil
		}
		if found != (w.previous != "") || found && (prior.id != w.previous || prior.process != w.previousProcess || prior.originalProcess != current.process || prior.phase == "complete") {
			return fail(f.Forbidden)
		}
		deferred, err := oauthBeginRecoveryDeferred(ctx, x, current)
		if err != nil {
			return err
		}
		if deferred {
			done = true
			return nil
		}
		if found && prior.phase == "claimed" {
			if _, err = x.Exec(ctx, `UPDATE agenteam_mcp.oauth_recovery_claims SET phase='superseded',completed_at=clock_timestamp() WHERE id=$1 AND phase='claimed'`, prior.id); err != nil {
				return err
			}
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.oauth_recovery_claims(id,request_id,original_process_id,process_id,previous_claim_id)
 VALUES($1,$2,$3,$4,NULLIF($5,''))`, w.claim, current.request, current.process, s.owner.process.String(), w.previous)
		return err
	})
	return done, commit(result)
}

func (s *Service) retireStoppedOAuthIO(ctx context.Context, w *oauthRecoveryUse) (bool, error) {
	cause, _ := f.NewRecoveryCause("mcp.oauth.stopped_io", w.original.request, "")
	var current oauthRecoveryFact
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, w.locks); err != nil {
			return err
		}
		var err error
		current, err = w.require(ctx, tx)
		if err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		// Physical process death retires I/O, never proves a remote outcome.
		// Existing completion remains immutable and unfinished outcomes stay
		// Unknown; neither this transaction nor its retry performs networking.
		tag, err := x.Exec(ctx, `UPDATE agenteam_mcp.oauth_requests SET
 phase=CASE WHEN phase IN ('pending','claimed') THEN 'unknown' ELSE phase END,
 error_code=CASE WHEN phase IN ('pending','claimed') THEN 'mcp_cancelled' ELSE error_code END,
 io_retired=true,retired_at=COALESCE(retired_at,clock_timestamp()),lease_released=lease_released OR credential_lease_id IS NULL
 WHERE id=$1 AND process_id=$2`, current.request, current.process)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fail(f.InvalidState)
		}
		return nil
	})
	return oauthRecoveryLeaseReleased(w.original, current, result)
}

// Only the canonical snapshot obtained by require under the original locks,
// followed by a committed stopped-I/O transaction, can skip lease release.
// A normal flow Delete may already have removed Secret's released lease row;
// its absence is not evidence. This helper checks data, never grants authority.
func oauthRecoveryLeaseReleased(original, current oauthRecoveryFact, result f.CommitResult) (bool, error) {
	if err := commit(result); err != nil {
		return false, err
	}
	if original.validate() != nil || current.validate() != nil || !original.sameOrigin(current) {
		return false, fail(f.Forbidden)
	}
	return current.released || current.lease == "", nil
}

func (s *Service) advanceOAuthRecovery(ctx context.Context, w *oauthRecoveryUse) (bool, error) {
	if done, err := s.claimOAuthRecovery(ctx, w); err != nil || done {
		return done, err
	}
	released, err := s.retireStoppedOAuthIO(ctx, w)
	if err != nil {
		return false, err
	}
	if !released {
		if err := s.releaseStoppedOAuthLease(ctx, w); err != nil {
			return false, err
		}
	}
	if err := s.cleanupStoppedOAuthFlow(ctx, w); err != nil {
		return false, err
	}
	if err := s.releaseStoppedOAuthConnection(ctx, w); err != nil {
		return false, err
	}
	return s.finishOAuthRecovery(ctx, w)
}

func (s *Service) finishOAuthRecovery(ctx context.Context, w *oauthRecoveryUse) (bool, error) {
	done := false
	cause, _ := f.NewRecoveryCause("mcp.oauth.recovery_complete", w.claim, "")
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, w.locks); err != nil {
			return err
		}
		current, err := w.require(ctx, tx)
		if err != nil {
			return err
		}
		if !current.retired || !current.released {
			return fail(f.ResourceBusy)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		credential := current.credential
		if current.operation == "begin" {
			credential = current.resultCredential
		}
		if credential != "" {
			var complete bool
			err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.oauth_flow_cleanups c
 JOIN agenteam_mcp.oauth_requests d ON d.id=c.delete_request_id WHERE c.flow_id=$1 AND c.source_request_id=$2
 AND c.credential_id=$3 AND d.phase='complete' AND d.result_credential_id=c.credential_id
 AND d.result_credential_version=c.credential_version+1 AND d.io_retired AND d.lease_released)`, current.flow, current.request, credential).Scan(&complete)
			if err != nil {
				return err
			}
			if !complete {
				return fail(f.ResourceBusy)
			}
		}
		if current.operation == "exchange" && current.phase == "complete" {
			// The token publication's original follow-on connection request
			// owns a separate lease. Our flow cleanup cannot declare it joined.
			connection, err := loadOAuthRecoveryConnection(ctx, x, current)
			if err != nil {
				return err
			}
			if !connection.retired || !connection.released {
				return fail(f.ResourceBusy)
			}
			if err = requireStoppedOAuthConnectionDetached(ctx, x, connection); err != nil {
				return err
			}
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.oauth_recovery_claims SET phase='complete',completed_at=clock_timestamp()
 WHERE id=$1 AND process_id=$2 AND phase='claimed'`, w.claim, s.owner.process.String())
		done = err == nil
		return err
	})
	if err := commit(result); err != nil {
		return false, err
	}
	return done, nil
}

// A bounded independent tail is used only for observations of the original
// attempt; it never retries an OAuth network request or reads token material.
func oauthRecoveryTail(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
}
