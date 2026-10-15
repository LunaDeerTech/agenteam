package mcp

import (
	"context"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// An original synchronous exchange owns this cleanup. It does not reconstruct
// a live owner from a DTO, require a fresh Human mutation after cancellation,
// or use the existence of a stale flow as permission to read it again.
type oauthFlowRetirement struct {
	original  *oauthExchange
	requestID string
}

func (o *oauthFlowRetirement) checkOAuthWriteOwnerInTx(ctx context.Context, tx f.Tx) error {
	if o == nil || o.original == nil || o.original.owner.service == nil || !o.original.leaseReleased || !tx.Valid() {
		return fail(f.Forbidden)
	}
	p := o.original
	s := p.owner.service
	if err := s.oauthProcessInTx(ctx, tx, p.owner.requestID, digest([]byte(p.owner.command.Identity.Canonical()))); err != nil {
		return err
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return err
	}
	var exact bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.oauth_requests original JOIN agenteam_mcp.oauth_requests retirement ON retirement.project_id=original.project_id AND retirement.connection_id=original.connection_id AND retirement.flow_id=original.flow_id AND retirement.credential_id=original.credential_id AND retirement.credential_version=original.credential_version AND retirement.process_id=original.process_id WHERE original.id=$1 AND original.operation='exchange' AND original.io_retired AND original.lease_released AND original.config_id=$2 AND original.config_revision=$3 AND original.credential_id=$4 AND original.credential_version=$5 AND retirement.id=$6 AND retirement.operation='retire' AND retirement.phase='claimed' AND retirement.io_retired AND retirement.lease_released AND original.process_required AND retirement.process_required AND original.process_id=$7)`, p.owner.requestID, p.owner.config.ID.String(), int64(p.owner.config.RuntimeRevision), p.credential, int64(p.version), o.requestID, s.owner.process.String()).Scan(&exact)
	if err != nil {
		return safeError(err)
	}
	if !exact {
		return fail(f.Forbidden)
	}
	return ctx.Err()
}
func (s *Service) retireOAuthFlow(ctx context.Context, p *oauthExchange) error {
	if p.flowRetired {
		return nil
	}
	if !p.leaseReleased {
		return fail(f.ResourceBusy)
	}
	requestID, err := f.NewID[struct{}]()
	if err != nil {
		return fail(f.DependencyUnavailable)
	}
	registration, _ := id.RegisterService(id.SecretService)
	actor, err := registration.Actor(requestID.String(), p.owner.command.Scope)
	if err != nil {
		return err
	}
	key, _ := f.ParseIdempotencyKey(requestID.String())
	identity, err := f.NewCommandIdentity("secret.mcp", []string{p.owner.connection.ProjectID.String(), p.owner.connection.ID.String(), p.flowID}, string(sc.Delete), key)
	if err != nil {
		return err
	}
	credential, err := f.ParseID[sc.Credential](p.credential)
	if err != nil {
		return fail(f.InvalidState)
	}
	ref, err := sc.NewCredentialRef(credential, p.owner.command.Scope)
	if err != nil {
		return err
	}
	request, err := sc.NewMCPOAuthWriteRequest(sc.MCPOAuthWriteFields{Actor: actor, Identity: identity, ProjectID: p.owner.connection.ProjectID, ConnectionID: p.owner.connection.ID.String(), ConfigRevision: p.owner.config.RuntimeRevision, Kind: sc.Delete, Ref: ref, ExpectedVersion: p.version, OwnerKind: sc.MCPOAuthFlow, OwnerID: p.flowID})
	if err != nil {
		return err
	}
	binding, err := sc.MCPOAuthWriteBinding(request)
	if err != nil {
		return err
	}
	locks, err := s.locks(p.owner.command.Actor, p.owner.command.Scope, nil, true)
	if err != nil {
		return err
	}
	cause, _ := f.NewRecoveryCause("mcp.oauth.retire", requestID.String(), "")
	owner := &oauthFlowRetirement{p, requestID.String()}
	err = commit(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		if err := s.oauthProcessInTx(ctx, tx, p.owner.requestID, digest([]byte(p.owner.command.Identity.Canonical()))); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.oauth_requests(id,operation,phase,project_id,config_id,config_revision,connection_id,connection_version,flow_id,command_digest,user_id,session_id,credential_id,credential_version,secret_write_binding,io_retired,lease_released,retired_at,process_id) SELECT $1,'retire','claimed',project_id,config_id,config_revision,connection_id,connection_version,flow_id,command_digest,user_id,session_id,credential_id,credential_version,$2,true,true,clock_timestamp(),process_id FROM agenteam_mcp.oauth_requests WHERE id=$3 AND operation='exchange' AND io_retired AND lease_released AND process_required AND process_id=$4`, requestID.String(), string(binding), p.owner.requestID, s.owner.process.String())
		if err != nil {
			return err
		}
		return owner.checkOAuthWriteOwnerInTx(ctx, tx)
	}))
	if err != nil {
		return err
	}
	w := &oauthWriteUse{service: s, request: request.WithoutMaterial(), requestID: requestID.String(), mapping: binding, locks: locks, owner: owner}
	w.active.Store(true)
	defer w.active.Store(false)
	ctx = context.WithValue(ctx, oauthWriteKey{}, w)
	plan, err := s.options.OAuth.Writes.DiscoverMCPOAuthWrite(ctx, request)
	if err != nil {
		return safeError(err)
	}
	prepared, err := s.options.OAuth.Writes.PrepareMCPOAuthWrite(ctx, request, plan)
	if err != nil {
		return safeError(err)
	}
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, prepared.RequiredLocks()); err != nil {
			return err
		}
		receipt, err := s.options.OAuth.Writes.ApplyMCPOAuthWriteInTx(ctx, tx, prepared)
		if err != nil {
			return err
		}
		if !receipt.Found {
			return fail(f.InvalidState)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.oauth_requests SET phase='complete',result_credential_id=$2,result_credential_version=$3 WHERE id=$1 AND phase='claimed'`, requestID.String(), p.credential, int64(receipt.Result.Metadata.Version))
		return err
	})
	if err = commit(result); err == nil {
		p.flowRetired = true
		return nil
	}
	if result.State() != f.Unknown {
		return err
	}
	tail, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	found := false
	observed := s.store.WithinTx(tail, cause, func(ctx context.Context, tx f.Tx) error {
		if e := s.store.AcquireAll(ctx, tx, prepared.RequiredLocks()); e != nil {
			return e
		}
		receipt, e := s.options.OAuth.Writes.ObserveMCPOAuthWriteInTx(ctx, tx, prepared)
		if e != nil || !receipt.Found {
			return e
		}
		x, e := s.store.InTx(tx)
		if e != nil {
			return e
		}
		return x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.oauth_requests WHERE id=$1 AND phase='complete' AND result_credential_id=$2 AND result_credential_version=$3)`, requestID.String(), p.credential, int64(receipt.Result.Metadata.Version)).Scan(&found)
	})
	if observed.State() == f.Committed && found {
		p.flowRetired = true
		return nil
	}
	return err
}
