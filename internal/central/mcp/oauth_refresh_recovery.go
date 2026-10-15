package mcp

import (
	"context"
	"errors"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// A refresh borrows its caller's Secret read lease and never owns a second
// lease. Its retirement records only this request's I/O; it does not close the
// outer Execution, connection or startup lease or assert that they joined.
type oauthRefreshFact struct {
	request, process, project, config, connection, credential string
	revision, connectionVersion, credentialVersion            f.Version
	caller                                                    oauthRefreshCaller
	writeBinding, phase                                       string
	retired, released                                         bool
}

func loadOAuthRefreshFact(ctx context.Context, x postgres.SQLExecutor, request string) (oauthRefreshFact, error) {
	v := oauthRefreshFact{request: request}
	err := x.QueryRow(ctx, `SELECT process_id::text,project_id,config_id,config_revision,connection_id,connection_version,
 credential_id,credential_version,COALESCE(command_digest,''),COALESCE(user_id,''),COALESCE(session_id,''),
 COALESCE(execution_id,''),COALESCE(operation_id,''),COALESCE(attempt_id,''),COALESCE(backend_request_id,''),
 COALESCE(runtime_binding_id,''),COALESCE(startup_request_id,''),COALESCE(secret_write_binding,''),phase,io_retired,lease_released
 FROM agenteam_mcp.oauth_requests WHERE id=$1 AND operation='refresh' AND process_required AND process_id IS NOT NULL
 AND flow_id IS NULL AND credential_lease_id IS NULL`, request).Scan(&v.process, &v.project, &v.config, &v.revision, &v.connection, &v.connectionVersion, &v.credential, &v.credentialVersion, &v.caller.command, &v.caller.user, &v.caller.session, &v.caller.execution, &v.caller.operation, &v.caller.attempt, &v.caller.request, &v.caller.runtime, &v.caller.startup, &v.writeBinding, &v.phase, &v.retired, &v.released)
	if err != nil {
		return v, err
	}
	if err = v.validate(); err != nil {
		return v, err
	}
	return v, nil
}

func (v oauthRefreshFact) validate() error {
	for _, raw := range []string{v.request, v.process, v.project, v.config, v.connection, v.credential} {
		if _, err := f.ParseID[f.Request](raw); err != nil {
			return fail(f.InvalidState)
		}
	}
	if v.revision.Validate() != nil || v.connectionVersion.Validate() != nil || v.credentialVersion.Validate() != nil || !v.caller.valid() || v.writeBinding != "" && f.Digest(v.writeBinding).Validate() != nil {
		return fail(f.InvalidState)
	}
	switch v.phase {
	case "claimed", "complete", "failed", "unknown":
	default:
		return fail(f.InvalidState)
	}
	return nil
}

func (v oauthRefreshFact) sameOrigin(other oauthRefreshFact) bool {
	return v.request == other.request && v.process == other.process && v.project == other.project && v.config == other.config && v.connection == other.connection && v.credential == other.credential && v.revision == other.revision && v.connectionVersion == other.connectionVersion && v.credentialVersion == other.credentialVersion && v.caller == other.caller
}

type oauthRefreshRecovery struct {
	service  *Service
	call     *processCall
	original oauthRefreshFact
	stopped  bool
}

func (w *oauthRefreshRecovery) require(ctx context.Context, tx f.Tx) error {
	if w == nil || w.service == nil || w.service.owner == nil || w.call == nil || !w.stopped || w.original.validate() != nil || w.original.process == w.service.owner.process.String() {
		return fail(f.Forbidden)
	}
	return w.service.owner.requireInTx(ctx, tx, w.call)
}

func (s *Service) recoverOAuthRefreshes(ctx context.Context) error {
	if s == nil || s.owner == nil {
		return fail(f.DependencyUnbound)
	}
	after := ""
	for {
		rows, err := s.store.Query(ctx, `SELECT id FROM agenteam_mcp.oauth_requests WHERE operation='refresh'
 AND process_required AND process_id<>$1 AND credential_lease_id IS NULL AND id>$2
 AND (NOT io_retired OR EXISTS(SELECT 1 FROM agenteam_mcp.connection_requests c JOIN agenteam_mcp.commands cmd ON cmd.command_digest=c.command_digest
 WHERE c.command_digest=oauth_requests.command_digest AND cmd.command_name='connection.refresh'
 AND (NOT c.io_retired OR NOT c.lease_released OR EXISTS(SELECT 1 FROM agenteam_mcp.connections live WHERE live.id=c.connection_id AND live.refresh_id=c.id))))
 ORDER BY id LIMIT 64`, s.owner.process.String(), after)
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
			// An unverifiable original owner remains unavailable. Its active
			// refresh uniqueness constraint prevents another token request.
			_ = s.recoverOAuthRefresh(ctx, request)
			after = request
		}
	}
}

func (s *Service) recoverOAuthRefresh(parent context.Context, request string) error {
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
	fact, err := loadOAuthRefreshFact(ctx, s.store, request)
	if err != nil {
		return safeError(err)
	}
	process, err := f.ParseID[oc.Process](fact.process)
	if err != nil || process == s.owner.process {
		return fail(f.Forbidden)
	}
	if err = s.owner.guard.ConfirmStopped(ctx, process); err != nil {
		return safeError(err)
	}
	w := &oauthRefreshRecovery{service: s, call: call, original: fact, stopped: true}
	// No credential is read, no Secret command is prepared/applied, and no
	// refresh or original Tool is retried after the stopped-process proof.
	result := w.retire(ctx)
	err = commit(result)
	if result.State() != f.Unknown && err != nil {
		return err
	}
	if err == nil {
		var done bool
		done, err = w.releaseHumanConnection(ctx)
		if err == nil && done {
			return nil
		}
	}
	tail, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	confirmed, observeErr := w.observeWithConnection(tail)
	cancel()
	if observeErr == nil && confirmed {
		return nil
	}
	retained = true
	call.returnedWith(w.observeWithConnection)
	return err // Original mutation attempt survives a failed/cancelled lookup.
}

func (w *oauthRefreshRecovery) retire(ctx context.Context) f.CommitResult {
	cause, _ := f.NewRecoveryCause("mcp.oauth.refresh_stopped", w.original.request, "")
	return w.service.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := w.service.store.AcquireAll(ctx, tx, []f.LockRequest{catalogLock(f.Exclusive)}); err != nil {
			return err
		}
		if err := w.require(ctx, tx); err != nil {
			return err
		}
		x, err := w.service.store.InTx(tx)
		if err != nil {
			return err
		}
		current, err := loadOAuthRefreshFact(ctx, x, w.original.request)
		if err != nil {
			return err
		}
		if !w.original.sameOrigin(current) {
			return fail(f.Forbidden)
		}
		if current.retired && current.released {
			return nil
		}
		// Completion and token CAS were one physical transaction. Preserve a
		// completed postimage; an unfinished request remains unknown, so the
		// old token cannot be automatically refreshed by a later caller.
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.oauth_requests SET
 phase=CASE WHEN phase='claimed' THEN 'unknown' ELSE phase END,
 error_code=CASE WHEN phase='claimed' THEN 'mcp_cancelled' ELSE error_code END,
 io_retired=true,lease_released=true,retired_at=COALESCE(retired_at,clock_timestamp())
 WHERE id=$1 AND process_id=$2 AND operation='refresh' AND credential_lease_id IS NULL`, current.request, current.process)
		return err
	})
}

func (w *oauthRefreshRecovery) observe(ctx context.Context) (bool, error) {
	confirmed := false
	cause, _ := f.NewRecoveryCause("mcp.oauth.refresh_stopped_observe", w.original.request, "")
	result := w.service.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := w.service.store.AcquireAll(ctx, tx, []f.LockRequest{catalogLock(f.Shared)}); err != nil {
			return err
		}
		if err := w.require(ctx, tx); err != nil {
			return err
		}
		x, err := w.service.store.InTx(tx)
		if err != nil {
			return err
		}
		current, err := loadOAuthRefreshFact(ctx, x, w.original.request)
		if errors.Is(err, pgx.ErrNoRows) {
			return fail(f.NotFound)
		}
		if err != nil {
			return err
		}
		confirmed = w.original.sameOrigin(current) && current.retired && current.released && current.phase != "claimed"
		return nil
	})
	return confirmed, commit(result)
}
