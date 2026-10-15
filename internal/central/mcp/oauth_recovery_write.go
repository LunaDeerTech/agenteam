package mcp

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type oauthRecoveryDeleteOwner struct {
	recovery *oauthRecoveryUse
	deletion oauthRecoveryFact
	// A pre-existing Delete process is independently confirmed stopped. A
	// newly created Delete instead belongs to this actual admitted claim.
	deleteStopped bool
}

func (o *oauthRecoveryDeleteOwner) checkOAuthWriteOwnerInTx(ctx context.Context, tx f.Tx) error {
	if o == nil || o.recovery == nil || o.deletion.validate() != nil || o.deletion.operation != "retire" {
		return fail(f.Forbidden)
	}
	w := o.recovery
	s := w.service
	original, err := w.require(ctx, tx)
	if err != nil {
		return err
	}
	if !original.retired || !original.released || o.deletion.process != s.owner.process.String() && !o.deleteStopped {
		return fail(f.Forbidden)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return safeError(err)
	}
	current, err := loadOAuthRecoveryFact(ctx, x, o.deletion.request)
	if err != nil {
		return err
	}
	if !o.deletion.sameOrigin(current) || current.writeBinding != o.deletion.writeBinding || !current.retired || !current.released {
		return fail(f.Forbidden)
	}
	// The immutable canonical cleanup row is independent of the currently
	// claiming process. Takeover consumes the same original Delete identity.
	var exact bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.oauth_flow_cleanups c
 JOIN agenteam_mcp.oauth_recovery_claims first_claim ON first_claim.id=c.created_by_claim_id
 WHERE c.flow_id=$1 AND c.source_request_id=$2 AND c.delete_request_id=$3
 AND c.source_process_id=$4::uuid AND c.delete_process_id=$5::uuid
 AND c.project_id=$6 AND c.config_id=$7 AND c.config_revision=$8 AND c.connection_id=$9 AND c.connection_version=$10
 AND c.command_digest=$11 AND c.user_id=$12 AND c.session_id=$13 AND c.credential_id=$14 AND c.credential_version=$15
 AND c.secret_write_binding=$16 AND first_claim.request_id=c.source_request_id
 AND first_claim.original_process_id=c.source_process_id)`, original.flow, original.request, current.request,
		original.process, current.process, current.project, current.config, int64(current.revision), current.connection, int64(current.version),
		current.command, current.user, current.session, current.credential, int64(current.credentialVersion), current.writeBinding).Scan(&exact)
	if err != nil {
		return safeError(err)
	}
	if !exact {
		return fail(f.Forbidden)
	}
	return ctx.Err()
}

func oauthRecoveryDeleteRequest(v oauthRecoveryFact) (sc.MCPOAuthWriteRequest, error) {
	if v.validate() != nil || v.operation != "retire" {
		return sc.MCPOAuthWriteRequest{}, fail(f.InvalidState)
	}
	request, err := makeOAuthRecoveryDeleteRequest(v)
	if err != nil {
		return sc.MCPOAuthWriteRequest{}, err
	}
	binding, err := sc.MCPOAuthWriteBinding(request)
	if err != nil || string(binding) != v.writeBinding {
		return sc.MCPOAuthWriteRequest{}, fail(f.Forbidden)
	}
	return request, nil
}

// Builds construction data only. Before selecting the one canonical Delete,
// its binding is not yet present; every authority path instead calls the
// strict reconstruction above and rechecks the persisted cleanup tuple.
func makeOAuthRecoveryDeleteRequest(v oauthRecoveryFact) (sc.MCPOAuthWriteRequest, error) {
	shape := v
	if shape.writeBinding == "" {
		shape.writeBinding = digest(nil)
	}
	if shape.validate() != nil || shape.operation != "retire" {
		return sc.MCPOAuthWriteRequest{}, fail(f.InvalidState)
	}
	project, _ := f.ParseID[id.Project](v.project)
	scope, _ := id.InProject(project)
	registration, _ := id.RegisterService(id.SecretService)
	actor, err := registration.Actor(v.request, scope)
	if err != nil {
		return sc.MCPOAuthWriteRequest{}, err
	}
	key, err := f.ParseIdempotencyKey(v.request)
	if err != nil {
		return sc.MCPOAuthWriteRequest{}, err
	}
	command, err := f.NewCommandIdentity("secret.mcp", []string{v.project, v.connection, v.flow}, string(sc.Delete), key)
	if err != nil {
		return sc.MCPOAuthWriteRequest{}, err
	}
	credential, _ := f.ParseID[sc.Credential](v.credential)
	ref, _ := sc.NewCredentialRef(credential, scope)
	request, err := sc.NewMCPOAuthWriteRequest(sc.MCPOAuthWriteFields{Actor: actor, Identity: command, ProjectID: project,
		ConnectionID: v.connection, ConfigRevision: v.revision, Kind: sc.Delete, Ref: ref, ExpectedVersion: v.credentialVersion,
		OwnerKind: sc.MCPOAuthFlow, OwnerID: v.flow})
	if err != nil {
		return sc.MCPOAuthWriteRequest{}, err
	}
	return request, nil
}

func (s *Service) validateRecoveryOAuthWritePlan(ctx context.Context, tx f.Tx, r sc.MCPOAuthWriteRequest, p sc.WriteDependencies) (*oauthWriteUse, error) {
	if sc.ValidateMCPOAuthDeleteRecoveryRequest(r) != nil {
		return nil, fail(f.Forbidden)
	}
	w, err := s.oauthWriteUse(ctx, r)
	if err != nil {
		return nil, err
	}
	owner, ok := w.owner.(*oauthRecoveryDeleteOwner)
	if !ok || owner == nil || owner.recovery == nil || owner.recovery.service != s || owner.deletion.request != w.requestID {
		return nil, fail(f.Forbidden)
	}
	original, err := oauthRecoveryDeleteRequest(owner.deletion)
	if err != nil {
		return nil, err
	}
	binding, _ := sc.MCPOAuthWriteBinding(r)
	expected, _ := sc.MCPOAuthWriteBinding(original)
	if binding != expected || !p.Matches(s.secretIssuer, binding, w.mapping) {
		return nil, fail(f.Forbidden)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return nil, safeError(err)
	}
	if err = owner.checkOAuthWriteOwnerInTx(ctx, tx); err != nil {
		return nil, err
	}
	return w, nil
}

func (a *Authority) ValidateMCPOAuthDeleteRecoveryInTx(ctx context.Context, tx f.Tx, r sc.MCPOAuthWriteRequest, p sc.WriteDependencies) error {
	s, err := a.service()
	if err != nil {
		return err
	}
	_, err = s.validateRecoveryOAuthWritePlan(ctx, tx, r, p)
	return err
}

var _ sc.MCPOAuthDeleteRecoveryAuthority = (*Authority)(nil)
