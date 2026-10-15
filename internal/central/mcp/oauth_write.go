package mcp

import (
	"context"
	"sync/atomic"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type OAuthOptions struct {
	Writes sc.MCPOAuthWrites
	// Fixed by the composition root from its validated public origin. Neither
	// a callback parameter nor remote metadata may override this redirect.
	RedirectURI string
}

// oauthWriteOwner is implemented only by original domain requests. Request
// constructors and Secret service actors cannot implement this private grant.
type oauthWriteOwner interface {
	checkOAuthWriteOwnerInTx(context.Context, f.Tx) error
}

type oauthWriteKey struct{}
type oauthWriteUse struct {
	service   *Service
	request   sc.MCPOAuthWriteRequest
	requestID string
	mapping   f.Digest
	locks     []f.LockRequest
	owner     oauthWriteOwner
	active    atomic.Bool
}

func (a *Authority) DiscoverMCPOAuthWrite(ctx context.Context, r sc.MCPOAuthWriteRequest) (sc.WriteDependencies, error) {
	s, err := a.service()
	if err != nil {
		return sc.WriteDependencies{}, err
	}
	return s.DiscoverMCPOAuthWrite(ctx, r)
}
func (a *Authority) ValidateMCPOAuthWriteInTx(ctx context.Context, tx f.Tx, r sc.MCPOAuthWriteRequest, p sc.WriteDependencies) error {
	s, err := a.service()
	if err != nil {
		return err
	}
	return s.ValidateMCPOAuthWriteInTx(ctx, tx, r, p)
}
func (a *Authority) ValidateMCPOAuthWriteObservationInTx(ctx context.Context, tx f.Tx, r sc.MCPOAuthWriteRequest, p sc.WriteDependencies) error {
	s, err := a.service()
	if err != nil {
		return err
	}
	return s.ValidateMCPOAuthWriteObservationInTx(ctx, tx, r, p)
}

func (s *Service) oauthWriteUse(ctx context.Context, r sc.MCPOAuthWriteRequest) (*oauthWriteUse, error) {
	if s == nil || ctx == nil || r.Validate() != nil {
		return nil, fail(f.Forbidden)
	}
	w, ok := ctx.Value(oauthWriteKey{}).(*oauthWriteUse)
	if !ok || w == nil || w.service != s || w.owner == nil || !w.active.Load() {
		return nil, fail(f.Forbidden)
	}
	got, err := sc.MCPOAuthWriteBinding(r)
	want, e := sc.MCPOAuthWriteBinding(w.request)
	if err != nil || e != nil || got != want || r.Fields().Actor.Details().CauseRef != w.requestID {
		return nil, fail(f.Forbidden)
	}
	return w, nil
}
func (s *Service) DiscoverMCPOAuthWrite(ctx context.Context, r sc.MCPOAuthWriteRequest) (sc.WriteDependencies, error) {
	w, err := s.oauthWriteUse(ctx, r)
	if err != nil {
		return sc.WriteDependencies{}, err
	}
	binding, err := sc.MCPOAuthWriteBinding(r)
	if err != nil {
		return sc.WriteDependencies{}, err
	}
	return sc.NewWriteDependencies(s.secretIssuer, binding, w.mapping, w.locks)
}
func (s *Service) ValidateMCPOAuthWriteInTx(ctx context.Context, tx f.Tx, r sc.MCPOAuthWriteRequest, p sc.WriteDependencies) error {
	w, err := s.validateOAuthWritePlan(ctx, tx, r, p, false)
	if err != nil {
		return err
	}
	return w.owner.checkOAuthWriteOwnerInTx(ctx, tx)
}
func (s *Service) ValidateMCPOAuthWriteObservationInTx(ctx context.Context, tx f.Tx, r sc.MCPOAuthWriteRequest, p sc.WriteDependencies) error {
	_, err := s.validateOAuthWritePlan(ctx, tx, r, p, true)
	return err
}
func (s *Service) validateOAuthWritePlan(ctx context.Context, tx f.Tx, r sc.MCPOAuthWriteRequest, p sc.WriteDependencies, observe bool) (*oauthWriteUse, error) {
	if ctx != nil && ctx.Value(oauthRecoveryKey{}) != nil {
		return s.validateRecoveryOAuthWritePlan(ctx, tx, r, p)
	}
	w, err := s.oauthWriteUse(ctx, r)
	if err != nil || !tx.Valid() {
		return nil, fail(f.Forbidden)
	}
	if err = s.requireOAuthProcessInTx(ctx, tx); err != nil {
		return nil, err
	}
	binding, _ := sc.MCPOAuthWriteBinding(r)
	if !p.Matches(s.secretIssuer, binding, w.mapping) {
		return nil, fail(f.Forbidden)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return nil, safeError(err)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return nil, safeError(err)
	}
	v := r.Fields()
	ref := ""
	if v.Ref.Validate() == nil {
		ref = v.Ref.Details().ID.String()
	}
	var present bool
	// The exact write binding includes the original material digest, command,
	// expected Secret version and flow/token owner. Observation uses this same
	// durable request after cancellation, without granting a fresh mutation.
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.oauth_requests
 WHERE id=$1 AND project_id=$2 AND connection_id=$3 AND config_revision=$4
 AND secret_write_binding=$5 AND ($6 OR phase='claimed')
 AND (($7='create' AND (($10='oauth_flow' AND operation='begin') OR ($10='oauth_token' AND operation='exchange'))) OR
      ($7<>'create' AND credential_id=$8 AND credential_version=$9))
 AND (($10='oauth_flow' AND flow_id=$11) OR ($10='oauth_token' AND connection_id=$11)))`, w.requestID, v.ProjectID.String(), v.ConnectionID, int64(v.ConfigRevision), string(binding), observe, string(v.Kind), ref, int64(v.ExpectedVersion), string(v.OwnerKind), v.OwnerID).Scan(&present)
	if err != nil {
		return nil, safeError(err)
	}
	if !present {
		return nil, fail(f.Forbidden)
	}
	return w, nil
}

// The control owner rechecks both current Account/Project permission and the
// exact live configuration/connection postimage before every Secret mutation.
type oauthControlOwner struct {
	service    *Service
	command    Command
	config     Config
	connection Connection
	requestID  string
}

func (o *oauthControlOwner) checkOAuthWriteOwnerInTx(ctx context.Context, tx f.Tx) error {
	if o == nil || o.service == nil || !tx.Valid() {
		return fail(f.Forbidden)
	}
	if err := o.service.requireOAuthProcessInTx(ctx, tx); err != nil {
		return err
	}
	if err := o.service.authorize(ctx, tx, o.command.Actor, o.command.Scope, id.Mutate); err != nil {
		return err
	}
	x, err := o.service.store.InTx(tx)
	if err != nil {
		return safeError(err)
	}
	var present bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.oauth_requests r
 JOIN agenteam_mcp.connections c ON c.id=r.connection_id AND c.project_id=r.project_id
 JOIN agenteam_mcp.configs f ON f.id=r.config_id
 WHERE r.id=$1 AND r.command_digest=$2 AND r.user_id=$3 AND r.session_id=$4
 AND r.config_id=$5 AND r.config_revision=$6 AND r.connection_id=$7 AND r.connection_version=$8
 AND c.config_id=r.config_id AND c.config_revision=r.config_revision AND c.version=r.connection_version
 AND f.runtime_revision=r.config_revision AND f.auth_scheme='oauth' AND r.phase='claimed')`, o.requestID, digest([]byte(o.command.Identity.Canonical())), o.command.Actor.Details().UserID, o.command.Actor.Details().SessionID, o.config.ID.String(), int64(o.config.RuntimeRevision), o.connection.ID.String(), int64(o.connection.Version)).Scan(&present)
	if err != nil {
		return safeError(err)
	}
	if !present {
		return fail(f.ConfirmationStale)
	}
	return nil
}

var _ sc.MCPOAuthWriteAuthority = (*Authority)(nil)
