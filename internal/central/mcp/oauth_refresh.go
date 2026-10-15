package mcp

import (
	"context"
	"errors"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type oauthRefreshKey struct{}
type oauthRefresh struct {
	service           *Service
	source            *oauthCredentialSource
	before            oauthTokenSnapshot
	connectionVersion f.Version
	request           string
	call              *processCall
	locks             []f.LockRequest
	write             *oauthWriteUse
	prepared          sc.PreparedMCPOAuthWrite
	applyUnknown      bool
	observedResult    bool
}

func (s *Service) refreshOAuthCredential(ctx context.Context, source *oauthCredentialSource, before oauthTokenSnapshot, material sc.SecretMaterial) (sc.SecretMaterial, error) {
	if s.owner == nil || s.options.OAuth == nil || nilPort(s.options.OAuth.Writes) || nilPort(s.options.Protocol) {
		return sc.SecretMaterial{}, fail(f.DependencyUnbound)
	}
	child, call, err := s.owner.begin(ctx)
	if err != nil {
		return sc.SecretMaterial{}, err
	}
	request, err := f.NewID[f.Request]()
	if err != nil {
		call.returnedWith(nil)
		return sc.SecretMaterial{}, fail(f.DependencyUnavailable)
	}
	locks, err := mergeLocks(source.locks, []f.LockRequest{catalogLock(f.Exclusive)})
	if err != nil {
		call.returnedWith(nil)
		return sc.SecretMaterial{}, err
	}
	w := &oauthRefresh{service: s, source: source, before: before, request: request.String(), call: call, locks: locks}
	w.connectionVersion = source.connectionVersion
	if w.connectionVersion == 0 {
		w.connectionVersion = before.connectionVersion
	}
	child = context.WithValue(child, oauthRefreshKey{}, w)
	defer w.returned(child)
	if err = w.prepare(child); err != nil {
		return sc.SecretMaterial{}, errors.Join(err, w.failReturned(child, "mcp_cancelled", false))
	}
	// This is the only network attempt. Neither a failed CAS nor an ambiguous
	// response installs an SDK TokenSource or repeats the original Tool.
	rotated, info, err := s.options.Protocol.RefreshOAuth(child, material, source.call)
	defer rotated.Destroy()
	if err != nil {
		var outcome *protocol.Error
		unknown := errors.As(err, &outcome) && outcome.MayHaveExecuted
		return sc.SecretMaterial{}, errors.Join(err, w.failReturned(child, safeProtocolCode(err), unknown))
	}
	if info.Issuer != before.issuer || info.Resource != before.resource {
		return sc.SecretMaterial{}, errors.Join(fail(f.Forbidden), w.failReturned(child, "mcp_invalid_response", true))
	}
	if err = w.publish(child, rotated, info); err != nil {
		if !w.applyUnknown {
			// The issuer already rotated its token. A known local CAS failure
			// cannot make the previous refresh token safe to send again.
			err = errors.Join(err, w.failReturned(child, safeProtocolCode(err), true))
		}
		return sc.SecretMaterial{}, err
	}
	access, _, err := protocol.OAuthAccess(rotated, before.resource, time.Now())
	return access, err
}

func (w *oauthRefresh) requireProcess(ctx context.Context, tx f.Tx) error {
	if w == nil || w.service == nil || w.source == nil || w.source.service != w.service || w.request == "" || w.call == nil || ctx == nil || ctx.Value(oauthRefreshKey{}) != w {
		return fail(f.Forbidden)
	}
	return w.service.owner.requireInTx(ctx, tx, w.call)
}

func (w *oauthRefresh) prepare(ctx context.Context) error {
	s, o := w.service, w.source
	cause, _ := f.NewRecoveryCause("mcp.oauth.refresh", w.request, "")
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, w.locks); err != nil {
			return err
		}
		if err := w.requireProcess(ctx, tx); err != nil {
			return err
		}
		if err := o.require(ctx, tx); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		current, err := o.tokenInTx(ctx, x)
		if err != nil {
			return err
		}
		if !w.before.equal(current) {
			return fail(f.ConfirmationStale)
		}
		c := o.caller
		_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.oauth_requests(id,operation,phase,project_id,config_id,config_revision,connection_id,connection_version,
 command_digest,user_id,session_id,execution_id,operation_id,attempt_id,backend_request_id,runtime_binding_id,startup_request_id,
 credential_id,credential_version,process_id,process_required)
 VALUES($1,'refresh','claimed',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,true)`, w.request, o.binding.ProjectID.String(), o.binding.ConfigID, int64(o.binding.ConfigRevision), o.binding.ConnectionID, int64(w.connectionVersion), null(c.command), null(c.user), null(c.session), null(c.execution), null(c.operation), null(c.attempt), null(c.request), null(c.runtime), null(c.startup), o.credential, int64(current.version), s.owner.process.String())
		return err
	})
	return commit(result)
}

// All operations use this exact persisted origin; the original private source
// is also rechecked for a new write. Cleanup/observation grants no fresh use.
const oauthRefreshTuple = `id=$1 AND operation='refresh' AND process_id=$2 AND process_required
 AND project_id=$3 AND config_id=$4 AND config_revision=$5 AND connection_id=$6
 AND credential_id=$7 AND credential_version=$8 AND credential_lease_id IS NULL
 AND command_digest IS NOT DISTINCT FROM $9 AND user_id IS NOT DISTINCT FROM $10 AND session_id IS NOT DISTINCT FROM $11
 AND execution_id IS NOT DISTINCT FROM $12 AND operation_id IS NOT DISTINCT FROM $13 AND attempt_id IS NOT DISTINCT FROM $14
 AND backend_request_id IS NOT DISTINCT FROM $15 AND runtime_binding_id IS NOT DISTINCT FROM $16 AND startup_request_id IS NOT DISTINCT FROM $17
 AND connection_version=$18 AND flow_id IS NULL`

func (w *oauthRefresh) tuple() []any {
	o, c := w.source, w.source.caller
	return []any{w.request, w.service.owner.process.String(), o.binding.ProjectID.String(), o.binding.ConfigID, int64(o.binding.ConfigRevision), o.binding.ConnectionID, o.credential, int64(w.before.version), null(c.command), null(c.user), null(c.session), null(c.execution), null(c.operation), null(c.attempt), null(c.request), null(c.runtime), null(c.startup), int64(w.connectionVersion)}
}

func (w *oauthRefresh) checkOAuthWriteOwnerInTx(ctx context.Context, tx f.Tx) error {
	if err := w.requireProcess(ctx, tx); err != nil {
		return err
	}
	if err := w.source.require(ctx, tx); err != nil {
		return err
	}
	x, err := w.service.store.InTx(tx)
	if err != nil {
		return err
	}
	current, err := w.source.tokenInTx(ctx, x)
	if err != nil {
		return err
	}
	if !w.before.equal(current) {
		return fail(f.ConfirmationStale)
	}
	var exists bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.oauth_requests WHERE `+oauthRefreshTuple+` AND phase='claimed')`, w.tuple()...).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return fail(f.Forbidden)
	}
	return nil
}

func (w *oauthRefresh) failReturned(ctx context.Context, code string, unknown bool) error {
	tail, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	cause, _ := f.NewRecoveryCause("mcp.oauth.refresh_returned", w.request, "")
	return commit(w.service.store.WithinTx(tail, cause, func(ctx context.Context, tx f.Tx) error {
		if err := w.service.store.AcquireAll(ctx, tx, []f.LockRequest{catalogLock(f.Exclusive)}); err != nil {
			return err
		}
		if err := w.requireProcess(ctx, tx); err != nil {
			return err
		}
		x, err := w.service.store.InTx(tx)
		if err != nil {
			return err
		}
		phase := "failed"
		if unknown {
			// The returned I/O can join, but this original ambiguous refresh
			// continues excluding another refresh of the same credential.
			phase = "unknown"
		}
		args := append(w.tuple(), code, phase)
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.oauth_requests SET phase=$20,error_code=$19,io_retired=true,lease_released=true,retired_at=COALESCE(retired_at,clock_timestamp()) WHERE `+oauthRefreshTuple+` AND phase='claimed'`, args...)
		return err
	}))
}

func (w *oauthRefresh) publish(ctx context.Context, material sc.SecretMaterial, info protocol.OAuthTokenInfo) error {
	s, o := w.service, w.source
	scope, _ := id.InProject(o.binding.ProjectID)
	registration, _ := id.RegisterService(id.SecretService)
	actor, err := registration.Actor(w.request, scope)
	if err != nil {
		return err
	}
	credential, err := f.ParseID[sc.Credential](o.credential)
	if err != nil {
		return err
	}
	ref, err := sc.NewCredentialRef(credential, scope)
	if err != nil {
		return err
	}
	key, _ := f.ParseIdempotencyKey(w.request)
	identity, err := f.NewCommandIdentity("secret.mcp", []string{o.binding.ProjectID.String(), o.binding.ConnectionID, o.binding.ConnectionID}, string(sc.Update), key)
	if err != nil {
		return err
	}
	request, err := sc.NewMCPOAuthWriteRequest(sc.MCPOAuthWriteFields{Actor: actor, Identity: identity, ProjectID: o.binding.ProjectID, ConnectionID: o.binding.ConnectionID, ConfigRevision: o.binding.ConfigRevision, Kind: sc.Update, Ref: ref, ExpectedVersion: w.before.version, OwnerKind: sc.MCPOAuthToken, OwnerID: o.binding.ConnectionID, Value: material})
	if err != nil {
		return err
	}
	binding, err := sc.MCPOAuthWriteBinding(request)
	if err != nil {
		return err
	}
	cause, _ := f.NewRecoveryCause("mcp.oauth.refresh_publish", w.request, "")
	bound := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, w.locks); err != nil {
			return err
		}
		if err := w.checkOAuthWriteOwnerInTx(ctx, tx); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		args := append(w.tuple(), string(binding))
		tag, err := x.Exec(ctx, `UPDATE agenteam_mcp.oauth_requests SET secret_write_binding=$19 WHERE `+oauthRefreshTuple+` AND phase='claimed' AND secret_write_binding IS NULL`, args...)
		if err == nil && tag.RowsAffected() != 1 {
			return fail(f.ConfirmationStale)
		}
		return err
	})
	if err = commit(bound); err != nil {
		return err
	}
	w.write = &oauthWriteUse{service: s, request: request.WithoutMaterial(), requestID: w.request, mapping: binding, locks: w.locks, owner: w}
	w.write.active.Store(true)
	writeCtx := context.WithValue(ctx, oauthWriteKey{}, w.write)
	plan, err := s.options.OAuth.Writes.DiscoverMCPOAuthWrite(writeCtx, request.WithoutMaterial())
	if err != nil {
		return err
	}
	w.prepared, err = s.options.OAuth.Writes.PrepareMCPOAuthWrite(writeCtx, request, plan)
	if err != nil {
		return err
	}
	result := s.store.WithinTx(writeCtx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, w.prepared.RequiredLocks()); err != nil {
			return err
		}
		receipt, err := s.options.OAuth.Writes.ApplyMCPOAuthWriteInTx(ctx, tx, w.prepared)
		if err != nil {
			return err
		}
		if !receipt.Found || receipt.Result.Metadata.Version <= w.before.version {
			return fail(f.InvalidState)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		var expiry any
		if !info.Expiry.IsZero() {
			expiry = info.Expiry
		}
		version := int64(receipt.Result.Metadata.Version)
		tag, err := x.Exec(ctx, `UPDATE agenteam_mcp.oauth_tokens SET secret_version=$2,expires_at=$3,refreshable=$4,last_request_id=$5,updated_at=clock_timestamp() WHERE credential_id=$1 AND secret_version=$6 AND last_request_id=$7`, o.credential, version, expiry, info.Refreshable, w.request, int64(w.before.version), w.before.request)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fail(f.ConfirmationStale)
		}
		args := append(w.tuple(), version)
		tag, err = x.Exec(ctx, `UPDATE agenteam_mcp.oauth_requests SET phase='complete',result_credential_id=credential_id,result_credential_version=$19,io_retired=true,lease_released=true,retired_at=clock_timestamp() WHERE `+oauthRefreshTuple+` AND phase='claimed'`, args...)
		if err == nil && tag.RowsAffected() != 1 {
			return fail(f.ConfirmationStale)
		}
		return err
	})
	err = commit(result)
	if result.State() != f.Unknown {
		return err
	}
	w.applyUnknown = true
	tail, cancel := context.WithTimeout(context.WithoutCancel(writeCtx), 5*time.Second)
	defer cancel()
	if confirmed, observedErr := w.observe(tail); observedErr == nil && confirmed && w.observedResult {
		return nil
	}
	// An unavailable/cancelled observer cannot replace the original cause or
	// turn an unknown Secret mutation into another refresh attempt.
	return err
}

func (w *oauthRefresh) observe(ctx context.Context) (bool, error) {
	ctx = context.WithValue(ctx, oauthRefreshKey{}, w)
	locks := []f.LockRequest{catalogLock(f.Shared)}
	if w.prepared != nil {
		locks = w.prepared.RequiredLocks()
		ctx = context.WithValue(ctx, oauthWriteKey{}, w.write)
	}
	cause, _ := f.NewRecoveryCause("mcp.oauth.refresh_observe", w.request, "")
	confirmed, success := false, false
	result := w.service.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := w.service.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		if err := w.requireProcess(ctx, tx); err != nil {
			return err
		}
		x, err := w.service.store.InTx(tx)
		if err != nil {
			return err
		}
		var phase string
		var retired, released bool
		var version int64
		err = x.QueryRow(ctx, `SELECT phase,io_retired,lease_released,COALESCE(result_credential_version,0) FROM agenteam_mcp.oauth_requests WHERE `+oauthRefreshTuple, w.tuple()...).Scan(&phase, &retired, &released, &version)
		if errors.Is(err, pgx.ErrNoRows) {
			var exists bool
			if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.oauth_requests WHERE id=$1)`, w.request).Scan(&exists); err != nil {
				return err
			}
			confirmed = !exists
			return nil
		}
		if err != nil || !retired || !released {
			return err
		}
		if phase == "failed" || phase == "unknown" {
			confirmed = true
			return nil
		}
		if phase != "complete" || w.prepared == nil {
			return nil
		}
		receipt, err := w.service.options.OAuth.Writes.ObserveMCPOAuthWriteInTx(ctx, tx, w.prepared)
		if err != nil {
			return err
		}
		confirmed = receipt.Found && int64(receipt.Result.Metadata.Version) == version && version > int64(w.before.version)
		success = confirmed
		return nil
	})
	if err := commit(result); err != nil {
		return false, err
	}
	w.observedResult = success
	return confirmed, nil
}

func (w *oauthRefresh) returned(ctx context.Context) {
	tail, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	confirmed, err := w.observe(tail)
	cancel()
	if err == nil && confirmed {
		if w.write != nil {
			w.write.active.Store(false)
		}
		w.call.returnedWith(nil)
		return
	}
	w.call.returnedWith(func(ctx context.Context) (bool, error) {
		confirmed, err := w.observe(ctx)
		if err == nil && confirmed && w.write != nil {
			w.write.active.Store(false)
		}
		return confirmed, err
	})
}
