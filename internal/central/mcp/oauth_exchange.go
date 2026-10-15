package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// CompleteOAuth claims one original session-bound flow before the only code
// exchange. Repeated callbacks observe the original command; they never replay
// a registration, authorization code or partially completed token mutation.
func (s *Service) CompleteOAuth(ctx context.Context, actor id.Actor, key f.IdempotencyKey, callback OAuthCallback) (CommandResult, error) {
	if s == nil || s.options.OAuth == nil || nilPort(s.options.OAuth.Writes) {
		return CommandResult{}, fail(f.DependencyUnbound)
	}
	ctx, owned, err := s.beginOAuthProcess(ctx)
	if err != nil {
		return CommandResult{}, err
	}
	defer owned.returned(ctx)
	p, replay, err := s.prepareOAuthExchange(ctx, actor, key, callback)
	if err != nil {
		return CommandResult{}, err
	}
	if replay != nil {
		return *replay, nil
	}
	flow, err := s.readOAuthFlow(ctx, p)
	if err != nil {
		return s.failOAuthExchange(ctx, p, err)
	}
	accessKey, err := ac.NewAppendKey(ac.AccessProducer, p.owner.requestID, 0)
	if err != nil {
		flow.Destroy()
		return s.failOAuthExchange(ctx, p, err)
	}
	call, err := outbound.NewCallContext(actor, p.owner.command.Scope, accessKey, ac.Associations{RequestID: p.owner.requestID})
	if err != nil {
		flow.Destroy()
		return s.failOAuthExchange(ctx, p, err)
	}
	token, info, exchangeErr := s.options.Protocol.ExchangeOAuth(ctx, flow, callback.code, callback.issuer, call)
	defer token.Destroy()
	// ExchangeOAuth has joined the response and destroyed its flow read copy.
	// Technical retirement uses a fresh bounded tail even after caller cancel.
	retireCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	retireErr := s.retireOAuthRead(retireCtx, p)
	if retireErr == nil {
		retireErr = s.retireOAuthFlow(retireCtx, p)
	}
	cancel()
	if retireErr != nil {
		return CommandResult{}, errors.Join(exchangeErr, retireErr)
	}
	if exchangeErr != nil {
		return s.failOAuthExchange(ctx, p, exchangeErr)
	}
	pending, err := s.publishOAuthToken(ctx, p, token, info)
	if err != nil {
		return CommandResult{}, err
	}
	// The token and original connection request are already durable. Only the
	// ordinary SDK initialization/discovery follows, under that request's lease.
	return s.synchronizePrepared(ctx, pending)
}

func (s *Service) retireOAuthRead(ctx context.Context, p *oauthExchange) error {
	if p.leaseReleased {
		return nil
	}
	if !p.leaseAcquired && !p.leaseAbsent {
		return fail(f.CommitUnknown)
	}
	locks, err := s.locks(p.owner.command.Actor, p.owner.command.Scope, nil, true)
	if err != nil {
		return err
	}
	cause, _ := f.NewRecoveryCause("mcp.oauth.returned", p.owner.requestID, "")
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_mcp.oauth_requests SET io_retired=true,lease_released=lease_released OR $6,retired_at=COALESCE(retired_at,clock_timestamp()) WHERE id=$1 AND operation='exchange' AND flow_id=$2 AND credential_id=$3 AND credential_version=$4 AND credential_lease_id=$5`, p.owner.requestID, p.flowID, p.credential, int64(p.version), p.lease.String(), p.leaseAbsent)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fail(f.InvalidState)
		}
		return nil
	})
	if err = commit(result); err != nil {
		return err
	}
	if p.leaseAbsent {
		p.leaseReleased = true
		return nil
	}
	return s.applyOAuthUsage(ctx, p, sc.ReleaseLeaseUsage)
}

func (s *Service) publishOAuthToken(ctx context.Context, p *oauthExchange, material sc.SecretMaterial, info protocol.OAuthTokenInfo) (*pendingConnection, error) {
	if info.Issuer != p.issuer || info.Resource == "" {
		return nil, fail(f.Forbidden)
	}
	c := p.owner.command
	registration, _ := id.RegisterService(id.SecretService)
	actor, err := registration.Actor(p.owner.requestID, c.Scope)
	if err != nil {
		return nil, err
	}
	key, err := f.ParseIdempotencyKey(p.owner.requestID)
	if err != nil {
		return nil, err
	}
	identity, err := f.NewCommandIdentity("secret.mcp", []string{p.owner.connection.ProjectID.String(), p.owner.connection.ID.String(), p.owner.connection.ID.String()}, string(sc.Create), key)
	if err != nil {
		return nil, err
	}
	request, err := sc.NewMCPOAuthWriteRequest(sc.MCPOAuthWriteFields{Actor: actor, Identity: identity, ProjectID: p.owner.connection.ProjectID, ConnectionID: p.owner.connection.ID.String(), ConfigRevision: p.owner.config.RuntimeRevision, Kind: sc.Create, OwnerKind: sc.MCPOAuthToken, OwnerID: p.owner.connection.ID.String(), Value: material})
	if err != nil {
		return nil, err
	}
	binding, err := sc.MCPOAuthWriteBinding(request)
	if err != nil {
		return nil, err
	}
	locks, err := s.locks(c.Actor, c.Scope, &c.Identity, true)
	if err != nil {
		return nil, err
	}
	cause, _ := f.NewCommandsCause(c.Identity)
	err = commit(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		if err := p.owner.checkOAuthWriteOwnerInTx(ctx, tx); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_mcp.oauth_requests SET secret_write_binding=$2 WHERE id=$1 AND phase='claimed' AND secret_write_binding IS NULL AND io_retired AND lease_released`, p.owner.requestID, string(binding))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fail(f.ConfirmationStale)
		}
		return nil
	}))
	if err != nil {
		return nil, err
	}
	w := &oauthWriteUse{service: s, request: request.WithoutMaterial(), requestID: p.owner.requestID, mapping: binding, locks: locks, owner: &p.owner}
	w.active.Store(true)
	defer w.active.Store(false)
	writeCtx := context.WithValue(ctx, oauthWriteKey{}, w)
	plan, err := s.options.OAuth.Writes.DiscoverMCPOAuthWrite(writeCtx, request.WithoutMaterial())
	if err != nil {
		return nil, safeError(err)
	}
	prepared, err := s.options.OAuth.Writes.PrepareMCPOAuthWrite(writeCtx, request, plan)
	if err != nil {
		return nil, safeError(err)
	}
	material.Destroy()
	connectionRequest, err := f.NewID[struct{}]()
	if err != nil {
		return nil, fail(f.DependencyUnavailable)
	}
	lease, err := f.NewID[sc.Lease]()
	if err != nil {
		return nil, fail(f.DependencyUnavailable)
	}
	pending := &pendingConnection{command: c, name: "connection.connect", config: p.owner.config, requestID: connectionRequest.String(), commandDigest: digest([]byte(c.Identity.Canonical())), semanticDigest: p.semantic, credentialLease: lease}
	result := s.store.WithinTx(writeCtx, cause, func(ctx context.Context, tx f.Tx) error {
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
		ref := prepared.Ref().Details().ID.String()
		version := receipt.Result.Metadata.Version
		var expiry any
		if !info.Expiry.IsZero() {
			expiry = info.Expiry
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.oauth_tokens(credential_id,project_id,connection_id,config_id,config_revision,secret_version,issuer,resource,expires_at,refreshable,last_request_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, ref, p.owner.connection.ProjectID.String(), p.owner.connection.ID.String(), p.owner.config.ID.String(), int64(p.owner.config.RuntimeRevision), int64(version), info.Issuer, info.Resource, expiry, info.Refreshable, p.owner.requestID)
		if err != nil {
			return err
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_mcp.connections SET version=version+1,credential_id=$2,state='connecting',refresh_id=$3,updated_at=clock_timestamp() WHERE id=$1 AND version=$4 AND config_revision=$5`, p.owner.connection.ID.String(), ref, pending.requestID, int64(p.owner.connection.Version), int64(p.owner.config.RuntimeRevision))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fail(f.ConfirmationStale)
		}
		pending.connection, err = scanConnection(x.QueryRow(ctx, `SELECT `+connectionColumns+` FROM agenteam_mcp.connections WHERE id=$1`, p.owner.connection.ID.String()))
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.connection_requests(id,command_digest,connection_id,project_id,config_id,config_revision,connection_version,user_id,session_id,credential_id,credential_lease_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, pending.requestID, pending.commandDigest, pending.connection.ID.String(), pending.connection.ProjectID.String(), pending.config.ID.String(), int64(pending.config.RuntimeRevision), int64(pending.connection.Version), c.Actor.Details().UserID, c.Actor.Details().SessionID, ref, pending.credentialLease.String())
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.oauth_requests SET phase='complete',result_credential_id=$2,result_credential_version=$3 WHERE id=$1 AND phase='claimed'`, p.owner.requestID, ref, int64(version))
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.oauth_flows SET phase='complete' WHERE id=$1 AND exchange_request_id=$2 AND phase='exchanging'`, p.flowID, p.owner.requestID)
		return err
	})
	if err = commit(result); err == nil {
		return pending, nil
	}
	if result.State() != f.Unknown {
		return nil, err
	}
	observeCtx, cancel := context.WithTimeout(context.WithoutCancel(writeCtx), 5*time.Second)
	defer cancel()
	observed := false
	recovery, _ := f.NewRecoveryCause("mcp.oauth.observe", p.owner.requestID, "")
	observation := s.store.WithinTx(observeCtx, recovery, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, prepared.RequiredLocks()); err != nil {
			return err
		}
		receipt, e := s.options.OAuth.Writes.ObserveMCPOAuthWriteInTx(ctx, tx, prepared)
		if e != nil || !receipt.Found {
			return e
		}
		x, e := s.store.InTx(tx)
		if e != nil {
			return e
		}
		var exact bool
		e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.oauth_requests o JOIN agenteam_mcp.connection_requests c ON c.command_digest=o.command_digest WHERE o.id=$1 AND o.phase='complete' AND o.result_credential_id=$2 AND o.result_credential_version=$3 AND c.id=$4 AND c.credential_id=o.result_credential_id AND c.credential_lease_id=$5)`, p.owner.requestID, prepared.Ref().Details().ID.String(), int64(receipt.Result.Metadata.Version), pending.requestID, pending.credentialLease.String()).Scan(&exact)
		if e != nil {
			return e
		}
		if !exact {
			return fail(f.InvalidState)
		}
		pending.connection, e = scanConnection(x.QueryRow(ctx, `SELECT `+connectionColumns+` FROM agenteam_mcp.connections WHERE id=$1 AND refresh_id=$2`, p.owner.connection.ID.String(), pending.requestID))
		if e != nil {
			return e
		}
		observed = true
		return nil
	})
	if observation.State() == f.Committed && observed {
		return pending, nil
	}
	return nil, err
}

func (s *Service) failOAuthExchange(ctx context.Context, p *oauthExchange, original error) (CommandResult, error) {
	// No path repeats the consumed callback. Retiring an unread/failed lease is
	// idempotent and remains bound to this exact durable exchange request.
	tail, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.retireOAuthRead(tail, p); err != nil {
		return CommandResult{}, errors.Join(original, err)
	}
	if err := s.retireOAuthFlow(tail, p); err != nil {
		return CommandResult{}, errors.Join(original, err)
	}
	locks, err := s.locks(p.owner.command.Actor, p.owner.command.Scope, nil, true)
	if err != nil {
		return CommandResult{}, errors.Join(original, err)
	}
	cause, _ := f.NewRecoveryCause("mcp.oauth.failed", p.owner.requestID, "")
	var output CommandResult
	result := s.store.WithinTx(tail, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		code := safeProtocolCode(original)
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.oauth_requests SET phase='failed',error_code=$2 WHERE id=$1 AND phase='claimed' AND io_retired AND lease_released`, p.owner.requestID, code)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.oauth_flows SET phase='failed' WHERE id=$1 AND exchange_request_id=$2 AND phase='exchanging'`, p.flowID, p.owner.requestID)
		if err != nil {
			return err
		}
		connection := p.owner.connection
		connection.LastErrorCode = code
		output = CommandResult{Connection: &connection}
		body, err := json.Marshal(output)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.commands SET phase='complete',response=$2 WHERE command_digest=$1 AND phase='pending'`, digest([]byte(p.owner.command.Identity.Canonical())), body)
		return err
	})
	if err = commit(result); err != nil {
		return CommandResult{}, errors.Join(original, err)
	}
	return output, nil
}
