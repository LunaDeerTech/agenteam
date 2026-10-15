package mcp

import (
	"context"
	"errors"
	"sync/atomic"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// Facts are parsing data, never an admission or a stopped-process proof. The
// original request's immutable caller tuple is retained across every claim.
type oauthRecoveryFact struct {
	request, operation, process, project, config, connection, flow string
	command, user, session, credential, lease                      string
	revision, version, credentialVersion                           f.Version
	phase, writeBinding, resultCredential                          string
	resultVersion                                                  f.Version
	retired, released                                              bool
}

func loadOAuthRecoveryFact(ctx context.Context, x postgres.SQLExecutor, request string) (oauthRecoveryFact, error) {
	v := oauthRecoveryFact{request: request}
	err := x.QueryRow(ctx, `SELECT operation,process_id::text,project_id,config_id,config_revision,connection_id,connection_version,
 flow_id,command_digest,user_id,session_id,COALESCE(credential_id,''),COALESCE(credential_version,0),
 COALESCE(credential_lease_id,''),phase,COALESCE(secret_write_binding,''),COALESCE(result_credential_id,''),
 COALESCE(result_credential_version,0),io_retired,lease_released
 FROM agenteam_mcp.oauth_requests WHERE id=$1 AND operation IN ('begin','exchange','retire')
 AND process_required AND process_id IS NOT NULL AND user_id IS NOT NULL AND session_id IS NOT NULL
 AND command_digest IS NOT NULL AND flow_id IS NOT NULL AND execution_id IS NULL AND operation_id IS NULL
 AND attempt_id IS NULL AND backend_request_id IS NULL AND runtime_binding_id IS NULL AND startup_request_id IS NULL`, request).Scan(
		&v.operation, &v.process, &v.project, &v.config, &v.revision, &v.connection, &v.version, &v.flow,
		&v.command, &v.user, &v.session, &v.credential, &v.credentialVersion, &v.lease, &v.phase, &v.writeBinding,
		&v.resultCredential, &v.resultVersion, &v.retired, &v.released)
	if err != nil {
		return v, safeError(err)
	}
	return v, v.validate()
}

func (v oauthRecoveryFact) validate() error {
	for _, raw := range []string{v.request, v.process, v.project, v.config, v.connection, v.flow, v.user, v.session} {
		if _, err := f.ParseID[f.Request](raw); err != nil {
			return fail(f.InvalidState)
		}
	}
	if v.revision.Validate() != nil || v.version.Validate() != nil || f.Digest(v.command).Validate() != nil ||
		v.writeBinding != "" && f.Digest(v.writeBinding).Validate() != nil || v.released && !v.retired {
		return fail(f.InvalidState)
	}
	for _, pair := range []struct {
		id      string
		version f.Version
	}{{v.credential, v.credentialVersion}, {v.resultCredential, v.resultVersion}} {
		if pair.id == "" {
			if pair.version != 0 {
				return fail(f.InvalidState)
			}
		} else if _, err := f.ParseID[f.Request](pair.id); err != nil || pair.version.Validate() != nil {
			return fail(f.InvalidState)
		}
	}
	if v.lease != "" {
		if _, err := f.ParseID[f.Request](v.lease); err != nil {
			return fail(f.InvalidState)
		}
	}
	switch v.operation {
	case "begin":
		if v.credential != "" || v.lease != "" {
			return fail(f.InvalidState)
		}
	case "exchange":
		if v.credential == "" || v.lease == "" {
			return fail(f.InvalidState)
		}
	case "retire":
		if v.credential == "" || v.lease != "" || v.writeBinding == "" || !v.retired || !v.released {
			return fail(f.InvalidState)
		}
	default:
		return fail(f.InvalidState)
	}
	switch v.phase {
	case "pending", "claimed", "complete", "failed", "unknown":
	default:
		return fail(f.InvalidState)
	}
	return nil
}

func (v oauthRecoveryFact) sameOrigin(other oauthRecoveryFact) bool {
	return v.request == other.request && v.operation == other.operation && v.process == other.process &&
		v.project == other.project && v.config == other.config && v.revision == other.revision && v.connection == other.connection &&
		v.version == other.version && v.flow == other.flow && v.command == other.command && v.user == other.user && v.session == other.session &&
		v.credential == other.credential && v.credentialVersion == other.credentialVersion && v.lease == other.lease
}

type oauthRecoveryClaim struct {
	id, request, originalProcess, process, previous, phase string
}

func loadOAuthRecoveryClaim(ctx context.Context, x postgres.SQLExecutor, request string) (oauthRecoveryClaim, bool, error) {
	var v oauthRecoveryClaim
	err := x.QueryRow(ctx, `SELECT id,request_id,original_process_id::text,process_id::text,COALESCE(previous_claim_id,''),phase
	FROM agenteam_mcp.oauth_recovery_claims c WHERE request_id=$1
 AND NOT EXISTS(SELECT 1 FROM agenteam_mcp.oauth_recovery_claims next WHERE next.previous_claim_id=c.id)`, request).Scan(
		&v.id, &v.request, &v.originalProcess, &v.process, &v.previous, &v.phase)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, false, nil
	}
	if err != nil {
		return v, false, safeError(err)
	}
	for _, raw := range []string{v.id, v.request, v.originalProcess, v.process} {
		if _, err := f.ParseID[f.Request](raw); err != nil {
			return v, false, fail(f.InvalidState)
		}
	}
	if v.previous != "" {
		if _, err := f.ParseID[f.Request](v.previous); err != nil {
			return v, false, fail(f.InvalidState)
		}
	}
	if v.phase != "claimed" && v.phase != "complete" && v.phase != "superseded" {
		return v, false, fail(f.InvalidState)
	}
	return v, true, nil
}

type oauthRecoveryKey struct{}

// Only recoverOAuthRequest creates this owner after the actual guard confirms
// both the original writer and any preceding recovery process have stopped.
// Parsing a canonical row cannot set stopped, admit a call, or activate it.
type oauthRecoveryUse struct {
	service         *Service
	call            *processCall
	original        oauthRecoveryFact
	claim, previous string
	previousProcess string
	// Chosen once by this actual claimant. A transaction with an Unknown
	// outcome is observed using the same candidate, never a replacement key.
	newDelete *oauthRecoveryFact
	locks     []f.LockRequest
	stopped   bool
	active    atomic.Bool
}

func (s *Service) oauthRecoveryUse(ctx context.Context) (*oauthRecoveryUse, error) {
	if s == nil || s.owner == nil || ctx == nil {
		return nil, fail(f.Forbidden)
	}
	w, ok := ctx.Value(oauthRecoveryKey{}).(*oauthRecoveryUse)
	if !ok || w == nil || w.service != s || w.call == nil || w.call.owner != s.owner || !w.active.Load() || !w.stopped ||
		w.original.validate() != nil || w.original.process == s.owner.process.String() || w.original.operation == "retire" {
		return nil, fail(f.Forbidden)
	}
	return w, nil
}

func (w *oauthRecoveryUse) require(ctx context.Context, tx f.Tx) (oauthRecoveryFact, error) {
	var empty oauthRecoveryFact
	if w == nil || w.service == nil {
		return empty, fail(f.Forbidden)
	}
	actual, err := w.service.oauthRecoveryUse(ctx)
	if err != nil || actual != w || !tx.Valid() {
		return empty, fail(f.Forbidden)
	}
	s := w.service
	if err = s.store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return empty, safeError(err)
	}
	if err = s.owner.requireInTx(ctx, tx, w.call); err != nil {
		return empty, err
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return empty, safeError(err)
	}
	current, err := loadOAuthRecoveryFact(ctx, x, w.original.request)
	if err != nil {
		return empty, err
	}
	claim, found, err := loadOAuthRecoveryClaim(ctx, x, w.original.request)
	if err != nil {
		return empty, err
	}
	if !found || !w.original.sameOrigin(current) || claim.id != w.claim || claim.process != s.owner.process.String() ||
		claim.originalProcess != w.original.process || claim.phase != "claimed" || claim.previous != w.previous {
		return empty, fail(f.Forbidden)
	}
	return current, nil
}

func oauthRecoveryLocks(v oauthRecoveryFact) ([]f.LockRequest, error) {
	project, err := f.ParseID[id.Project](v.project)
	if err != nil {
		return nil, fail(f.InvalidState)
	}
	connection, err := f.ParseID[ConnectionIdentity](v.connection)
	if err != nil {
		return nil, fail(f.InvalidState)
	}
	return startupLocks(project, connection)
}

func (s *Service) confirmOAuthRecoveryProcesses(ctx context.Context, original oauthRecoveryFact, previous oauthRecoveryClaim) error {
	process, err := f.ParseID[oc.Process](original.process)
	if err != nil || process == s.owner.process {
		return fail(f.Forbidden)
	}
	if err = s.owner.guard.ConfirmStopped(ctx, process); err != nil {
		return safeError(err)
	}
	if previous.id != "" {
		if previous.originalProcess != original.process || previous.request != original.request {
			return fail(f.Forbidden)
		}
		process, err = f.ParseID[oc.Process](previous.process)
		if err != nil || process == s.owner.process {
			return fail(f.ResourceBusy)
		}
		if err = s.owner.guard.ConfirmStopped(ctx, process); err != nil {
			return safeError(err)
		}
	}
	return nil
}
