package mcp

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func (s *Service) connectionOAuthAccess(ctx context.Context, p *pendingConnection, call outbound.CallContext) (sc.SecretMaterial, error) {
	locks, err := s.locks(p.command.Actor, p.command.Scope, nil, false)
	if err != nil {
		return sc.SecretMaterial{}, err
	}
	b := RuntimeConnectionBinding{ProjectID: p.connection.ProjectID, ConfigID: p.config.ID.String(), ConnectionID: p.connection.ID.String(), ConfigRevision: p.config.RuntimeRevision}
	source := &oauthCredentialSource{service: s, credential: p.connection.CredentialID, binding: b, connectionVersion: p.connection.Version, locks: locks, call: call,
		caller: oauthRefreshCaller{user: p.command.Actor.Details().UserID, session: p.command.Actor.Details().SessionID, command: p.commandDigest}}
	source.read = func(ctx context.Context) (sc.SecretMaterial, error) { return s.readConnectionCredential(ctx, p) }
	source.require = func(ctx context.Context, tx f.Tx) error {
		if err := s.authorize(ctx, tx, p.command.Actor, p.command.Scope, id.Mutate); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		var exact bool
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.connection_requests r JOIN agenteam_mcp.connections c ON c.id=r.connection_id JOIN agenteam_mcp.configs f ON f.id=r.config_id WHERE r.id=$1 AND r.command_digest=$2 AND r.connection_version=$3 AND r.credential_id=$4 AND NOT r.io_retired AND NOT r.lease_released AND c.version=r.connection_version AND c.refresh_id=r.id AND f.runtime_revision=r.config_revision)`, p.requestID, p.commandDigest, int64(p.connection.Version), p.connection.CredentialID).Scan(&exact)
		if err != nil {
			return err
		}
		if !exact {
			return fail(f.Forbidden)
		}
		return nil
	}
	return s.readOAuthCredential(ctx, source)
}
