package mcp

import (
	"context"
	"errors"
	"sync"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type secretUseKey struct{}
type secretUse struct {
	mu        sync.Mutex
	service   *Service
	pending   *pendingConnection
	request   sc.UsageRequest
	locks     []f.LockRequest
	mapping   f.Digest
	active    bool
	validated f.Tx
}

func (s *Service) newConnectionSecretUse(p *pendingConnection, action sc.UsageAction) (*secretUse, error) {
	if p == nil || p.credentialLease.Validate() != nil {
		return nil, fail(f.InvalidArgument)
	}
	credential, err := f.ParseID[sc.Credential](p.connection.CredentialID)
	if err != nil {
		return nil, fail(f.InvalidState)
	}
	ref, err := sc.NewCredentialRef(credential, p.command.Scope)
	if err != nil {
		return nil, err
	}
	owner, err := sc.NewCredentialLeaseOwner(sc.MCPConnectionRequestOwner, p.requestID)
	if err != nil {
		return nil, err
	}
	registration, err := id.RegisterService(id.SecretService)
	if err != nil {
		return nil, err
	}
	actor, err := registration.Actor(p.requestID, p.command.Scope)
	if err != nil {
		return nil, err
	}
	r := sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.MCP, LeaseOwner: owner, LeaseID: p.credentialLease, Action: action}
	if action == sc.ReadLeaseUsage {
		r.RequestID = p.requestID
	}
	if action == sc.RetainReferenceUsage {
		r.Actor = p.command.Actor
		r.ReferenceOwner = p.connection.ID.String()
		r.LeaseOwner = sc.CredentialLeaseOwner{}
		r.LeaseID = sc.LeaseID{}
		r.Retain = true
	}
	if r.Validate() != nil {
		return nil, fail(f.InvalidArgument)
	}
	locks, err := s.locks(p.command.Actor, p.command.Scope, nil, action != sc.ReadLeaseUsage)
	if err != nil {
		return nil, err
	}
	binding, err := sc.UsageBinding(r)
	if err != nil {
		return nil, err
	}
	raw, err := encode(struct {
		Binding                                                      f.Digest
		Request, Command, Connection, Project, Config, User, Session string
		Revision, Version                                            f.Version
	}{binding, p.requestID, p.commandDigest, p.connection.ID.String(), p.connection.ProjectID.String(), p.config.ID.String(), p.command.Actor.Details().UserID, p.command.Actor.Details().SessionID, p.config.RuntimeRevision, p.connection.Version})
	if err != nil {
		return nil, err
	}
	return &secretUse{service: s, pending: p, request: r, locks: locks, mapping: f.Digest(digest(raw)), active: true}, nil
}
func (w *secretUse) close() { w.mu.Lock(); w.active = false; w.mu.Unlock() }
func (s *Service) secretUse(ctx context.Context, r sc.UsageRequest) (*secretUse, error) {
	if ctx == nil || s == nil || r.Validate() != nil || r.Purpose != sc.MCP {
		return nil, fail(f.Forbidden)
	}
	w, ok := ctx.Value(secretUseKey{}).(*secretUse)
	if !ok || w == nil || w.service != s {
		return nil, fail(f.Forbidden)
	}
	w.mu.Lock()
	active := w.active
	w.mu.Unlock()
	got, err := sc.UsageBinding(r)
	want, e := sc.UsageBinding(w.request)
	if !active || err != nil || e != nil || got != want {
		return nil, fail(f.Forbidden)
	}
	return w, nil
}
func (s *Service) DiscoverUsage(ctx context.Context, r sc.UsageRequest) (sc.UsageDependencies, error) {
	if ctx != nil && ctx.Value(humanRefreshRecoveryKey{}) != nil {
		return s.discoverHumanRefreshRecovery(ctx, r)
	}
	if ctx != nil && ctx.Value(oauthConnectionRecoveryKey{}) != nil {
		return s.discoverOAuthConnectionRecovery(ctx, r)
	}
	if ctx != nil && ctx.Value(startupSecretKey{}) != nil {
		return s.discoverStartupSecret(ctx, r)
	}
	if ctx != nil && ctx.Value(oauthUsageKey{}) != nil {
		return s.discoverOAuthUsage(ctx, r)
	}
	if ctx != nil && ctx.Value(executionRetirementKey{}) != nil {
		return s.discoverExecutionRetirementSecret(ctx, r)
	}
	if ctx != nil && ctx.Value(resourceSecretKey{}) != nil {
		return s.discoverResourceSecret(ctx, r)
	}
	if ctx != nil && ctx.Value(retentionUseKey{}) != nil {
		return s.discoverRetentionSecret(ctx, r)
	}
	if ctx != nil && ctx.Value(runtimeSecretKey{}) != nil {
		return s.discoverRuntimeSecret(ctx, r)
	}
	w, err := s.secretUse(ctx, r)
	if err != nil {
		return sc.UsageDependencies{}, err
	}
	binding, err := sc.UsageBinding(r)
	if err != nil {
		return sc.UsageDependencies{}, err
	}
	return sc.NewUsageDependencies(s.secretIssuer, binding, w.mapping, w.locks)
}
func (s *Service) ValidateUsageInTx(ctx context.Context, tx f.Tx, r sc.UsageRequest, plan sc.UsageDependencies) error {
	if ctx != nil && ctx.Value(humanRefreshRecoveryKey{}) != nil {
		return s.validateHumanRefreshRecovery(ctx, tx, r, plan)
	}
	if ctx != nil && ctx.Value(oauthConnectionRecoveryKey{}) != nil {
		return s.validateOAuthConnectionRecovery(ctx, tx, r, plan)
	}
	if ctx != nil && ctx.Value(startupSecretKey{}) != nil {
		return s.validateStartupSecret(ctx, tx, r, plan)
	}
	if ctx != nil && ctx.Value(oauthUsageKey{}) != nil {
		return s.validateOAuthUsage(ctx, tx, r, plan)
	}
	if ctx != nil && ctx.Value(executionRetirementKey{}) != nil {
		return s.validateExecutionRetirementSecret(ctx, tx, r, plan)
	}
	if ctx != nil && ctx.Value(resourceSecretKey{}) != nil {
		return s.validateResourceSecret(ctx, tx, r, plan)
	}
	if ctx != nil && ctx.Value(retentionUseKey{}) != nil {
		return s.validateRetentionSecret(ctx, tx, r, plan)
	}
	if ctx != nil && ctx.Value(runtimeSecretKey{}) != nil {
		return s.validateRuntimeSecret(ctx, tx, r, plan)
	}
	w, err := s.secretUse(ctx, r)
	if err != nil {
		return err
	}
	binding, err := sc.UsageBinding(r)
	if err != nil || !plan.Matches(s.secretIssuer, binding, w.mapping) {
		return fail(f.Forbidden)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return safeError(err)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return safeError(err)
	}
	p := w.pending
	var present bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.connection_requests WHERE id=$1 AND command_digest=$2 AND connection_id=$3 AND project_id=$4 AND config_id=$5 AND config_revision=$6 AND connection_version=$7 AND user_id=$8 AND session_id=$9 AND credential_id=$10 AND credential_lease_id=$11 AND io_retired=$12 AND NOT lease_released)`, p.requestID, p.commandDigest, p.connection.ID.String(), p.connection.ProjectID.String(), p.config.ID.String(), int64(p.config.RuntimeRevision), int64(p.connection.Version), p.command.Actor.Details().UserID, p.command.Actor.Details().SessionID, p.connection.CredentialID, p.credentialLease.String(), r.Action == sc.ReleaseLeaseUsage).Scan(&present)
	if err != nil {
		return safeError(err)
	}
	if !present {
		return fail(f.Forbidden)
	}
	if r.Action != sc.ReleaseLeaseUsage {
		if err = s.authorize(ctx, tx, p.command.Actor, p.command.Scope, id.Mutate); err != nil {
			return err
		}
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.connections c JOIN agenteam_mcp.configs f ON f.id=c.config_id WHERE c.id=$1 AND c.project_id=$2 AND c.config_id=$3 AND c.config_revision=$4 AND f.runtime_revision=$4 AND c.version=$5 AND c.refresh_id=$6 AND c.credential_id=$7)`, p.connection.ID.String(), p.connection.ProjectID.String(), p.config.ID.String(), int64(p.config.RuntimeRevision), int64(p.connection.Version), p.requestID, p.connection.CredentialID).Scan(&present)
		if err != nil {
			return safeError(err)
		}
		if !present {
			return fail(f.ConfirmationStale)
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.active {
		return fail(f.Forbidden)
	}
	w.validated = tx
	return ctx.Err()
}
func (s *Service) AuthorizeLeaseInTx(ctx context.Context, tx f.Tx, actor id.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	if ctx != nil && ctx.Value(humanRefreshRecoveryKey{}) != nil {
		return s.authorizeHumanRefreshRecovery(ctx, tx, actor, ref, owner, action)
	}
	if ctx != nil && ctx.Value(oauthConnectionRecoveryKey{}) != nil {
		return s.authorizeOAuthConnectionRecovery(ctx, tx, actor, ref, owner, action)
	}
	if ctx != nil && ctx.Value(startupSecretKey{}) != nil {
		return s.authorizeStartupSecret(ctx, tx, actor, ref, owner, action)
	}
	if ctx != nil && ctx.Value(oauthUsageKey{}) != nil {
		return s.authorizeOAuthUsage(ctx, tx, actor, ref, owner, action)
	}
	if ctx != nil && ctx.Value(executionRetirementKey{}) != nil {
		return s.authorizeExecutionRetirementSecret(ctx, tx, actor, ref, owner, action)
	}
	if ctx != nil && ctx.Value(resourceSecretKey{}) != nil {
		return s.authorizeResourceSecret(ctx, tx, actor, ref, owner, action)
	}
	if ctx != nil && ctx.Value(retentionUseKey{}) != nil {
		return s.authorizeRetentionSecret(ctx, tx, actor, ref, owner, action)
	}
	if ctx != nil && ctx.Value(runtimeSecretKey{}) != nil {
		return s.authorizeRuntimeSecret(ctx, tx, actor, ref, owner, action)
	}
	if ctx == nil {
		return sc.UseGrant{}, fail(f.Forbidden)
	}
	w, ok := ctx.Value(secretUseKey{}).(*secretUse)
	if !ok || w == nil {
		return sc.UseGrant{}, fail(f.Forbidden)
	}
	if _, err := s.secretUse(ctx, w.request); err != nil {
		return sc.UseGrant{}, err
	}
	w.mu.Lock()
	valid := tx.Valid() && w.active && w.validated == tx
	w.mu.Unlock()
	want := sc.AcquireLease
	switch w.request.Action {
	case sc.ReadLeaseUsage:
		want = sc.ReadLease
	case sc.ReleaseLeaseUsage:
		want = sc.ReleaseLease
	case sc.AcquireLeaseUsage:
	default:
		return sc.UseGrant{}, fail(f.Forbidden)
	}
	if !valid || action != want || !actor.Equal(w.request.Actor) || !ref.Equal(w.request.Ref) || !owner.Equal(w.request.LeaseOwner) {
		return sc.UseGrant{}, fail(f.Forbidden)
	}
	subject := w.pending.command.Actor
	if action == sc.ReleaseLease {
		subject = actor
	}
	return sc.UseGrant{Subject: subject, Consumer: sc.MCP}, nil
}
func (s *Service) CheckReferenceInTx(context.Context, f.Tx, id.Actor, sc.CredentialRef, sc.Purpose, string, bool) error {
	// All MCP reference writes must enter Secret's planned usage path.
	return fail(f.ResourceBusy)
}
func (s *Service) applyConnectionSecret(ctx context.Context, p *pendingConnection, action sc.UsageAction) error {
	if nilPort(s.options.Secret) {
		return fail(f.DependencyUnbound)
	}
	w, err := s.newConnectionSecretUse(p, action)
	if err != nil {
		return err
	}
	defer w.close()
	ctx = context.WithValue(ctx, secretUseKey{}, w)
	plan, err := s.options.Secret.DiscoverUsage(ctx, w.request)
	if err != nil {
		return safeError(err)
	}
	cause, _ := f.NewRecoveryCause("mcp.secret", p.requestID, "")
	return commit(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return err
		}
		_, err := s.options.Secret.ApplyUsageInTx(ctx, tx, w.request, plan)
		return err
	}))
}
func (s *Service) readConnectionCredential(ctx context.Context, p *pendingConnection) (sc.SecretMaterial, error) {
	if nilPort(s.options.SecretReader) || nilPort(s.options.Secret) {
		return sc.SecretMaterial{}, fail(f.DependencyUnbound)
	}
	if err := s.applyConnectionSecret(ctx, p, sc.RetainReferenceUsage); err != nil {
		return sc.SecretMaterial{}, err
	}
	if err := s.applyConnectionSecret(ctx, p, sc.AcquireLeaseUsage); err != nil {
		return sc.SecretMaterial{}, err
	}
	w, err := s.newConnectionSecretUse(p, sc.ReadLeaseUsage)
	if err != nil {
		return sc.SecretMaterial{}, err
	}
	defer w.close()
	material, err := s.options.SecretReader.ReadMCPCredentialForUsage(context.WithValue(ctx, secretUseKey{}, w), w.request)
	if err != nil {
		material.Destroy()
		return sc.SecretMaterial{}, safeError(err)
	}
	return material, nil
}
func (s *Service) retireConnectionRequest(p *pendingConnection) {
	// Called only after Open failed or Session.Close joined the owned I/O. A
	// cancelled caller cannot erase the durable original release obligation.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	locks, err := s.locks(p.command.Actor, p.command.Scope, nil, true)
	if err != nil {
		return
	}
	cause, _ := f.NewRecoveryCause("mcp.retire", p.requestID, "")
	err = commit(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.connection_requests SET io_retired=true,retired_at=COALESCE(retired_at,clock_timestamp()),phase=CASE WHEN phase='pending' THEN 'abandoned' ELSE phase END,lease_released=credential_lease_id IS NULL WHERE id=$1 AND command_digest=$2`, p.requestID, p.commandDigest)
		return err
	}))
	if err != nil || p.credentialLease.Validate() != nil {
		return
	}
	err = s.applyConnectionSecret(ctx, p, sc.ReleaseLeaseUsage)
	var fault *f.Fault
	if err != nil && (!errors.As(err, &fault) || fault.Code != f.NotFound) {
		return
	}
	_ = commit(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.connection_requests SET lease_released=true WHERE id=$1 AND command_digest=$2 AND io_retired`, p.requestID, p.commandDigest)
		return err
	}))
}

var _ sc.UsagePlanner = (*Service)(nil)
var _ sc.UsageAuthority = (*Service)(nil)
