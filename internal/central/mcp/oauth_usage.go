package mcp

import (
	"context"
	"sync"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type oauthUsageKey struct{}
type oauthUsage struct {
	mu        sync.Mutex
	service   *Service
	request   sc.UsageRequest
	requestID string
	version   f.Version
	locks     []f.LockRequest
	mapping   f.Digest
	subject   id.Actor
	check     func(context.Context, f.Tx) error
	active    bool
	validated f.Tx
}

func (s *Service) newOAuthUsage(p *oauthExchange, action sc.UsageAction) (*oauthUsage, error) {
	if p == nil || p.owner.service != s || p.lease.Validate() != nil || p.version.Validate() != nil {
		return nil, fail(f.Forbidden)
	}
	credential, err := f.ParseID[sc.Credential](p.credential)
	if err != nil {
		return nil, fail(f.InvalidState)
	}
	ref, err := sc.NewCredentialRef(credential, p.owner.command.Scope)
	if err != nil {
		return nil, err
	}
	registration, _ := id.RegisterService(id.SecretService)
	actor, err := registration.Actor(p.owner.requestID, p.owner.command.Scope)
	if err != nil {
		return nil, err
	}
	owner, _ := sc.NewCredentialLeaseOwner(sc.MCPConnectionRequestOwner, p.owner.requestID)
	r := sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.MCP, LeaseOwner: owner, LeaseID: p.lease, Action: action}
	if action == sc.ReadLeaseUsage {
		r.RequestID = p.owner.requestID
	}
	if r.Validate() != nil || (action != sc.AcquireLeaseUsage && action != sc.ReadLeaseUsage && action != sc.ReleaseLeaseUsage) {
		return nil, fail(f.InvalidArgument)
	}
	locks, err := s.locks(p.owner.command.Actor, p.owner.command.Scope, nil, action != sc.ReadLeaseUsage)
	if err != nil {
		return nil, err
	}
	binding, err := sc.UsageBinding(r)
	if err != nil {
		return nil, err
	}
	raw, err := encode(struct {
		Binding       f.Digest
		Request, Flow string
		Version       f.Version
	}{binding, p.owner.requestID, p.flowID, p.version})
	if err != nil {
		return nil, err
	}
	check := func(ctx context.Context, tx f.Tx) error {
		if action == sc.ReleaseLeaseUsage {
			return nil
		}
		if err := p.owner.checkOAuthWriteOwnerInTx(ctx, tx); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		var exact bool
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.oauth_flows WHERE id=$1 AND exchange_request_id=$2 AND phase='exchanging' AND credential_id=$3 AND credential_version=$4 AND project_id=$5 AND connection_id=$6 AND user_id=$7 AND session_id=$8)`, p.flowID, p.owner.requestID, p.credential, int64(p.version), p.owner.connection.ProjectID.String(), p.owner.connection.ID.String(), p.owner.command.Actor.Details().UserID, p.owner.command.Actor.Details().SessionID).Scan(&exact)
		if err != nil {
			return err
		}
		if !exact {
			return fail(f.Forbidden)
		}
		return nil
	}
	return &oauthUsage{service: s, request: r, requestID: p.owner.requestID, version: p.version, locks: locks, mapping: f.Digest(digest(raw)), subject: p.owner.command.Actor, check: check, active: true}, nil
}
func (w *oauthUsage) close() { w.mu.Lock(); w.active = false; w.mu.Unlock() }
func (s *Service) oauthUsage(ctx context.Context, r sc.UsageRequest) (*oauthUsage, error) {
	if s == nil || ctx == nil || r.Validate() != nil || r.Purpose != sc.MCP {
		return nil, fail(f.Forbidden)
	}
	w, ok := ctx.Value(oauthUsageKey{}).(*oauthUsage)
	if !ok || w == nil || w.service != s || w.check == nil {
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
func (s *Service) discoverOAuthUsage(ctx context.Context, r sc.UsageRequest) (sc.UsageDependencies, error) {
	w, err := s.oauthUsage(ctx, r)
	if err != nil {
		return sc.UsageDependencies{}, err
	}
	binding, _ := sc.UsageBinding(r)
	return sc.NewUsageDependencies(s.secretIssuer, binding, w.mapping, w.locks)
}
func (s *Service) validateOAuthUsage(ctx context.Context, tx f.Tx, r sc.UsageRequest, plan sc.UsageDependencies) error {
	w, err := s.oauthUsage(ctx, r)
	if err != nil || !tx.Valid() {
		return fail(f.Forbidden)
	}
	if err = s.requireOAuthProcessInTx(ctx, tx); err != nil {
		return err
	}
	binding, _ := sc.UsageBinding(r)
	if !plan.Matches(s.secretIssuer, binding, w.mapping) {
		return fail(f.Forbidden)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return safeError(err)
	}
	if err = w.check(ctx, tx); err != nil {
		return err
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return err
	}
	var exact bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.oauth_requests WHERE id=$1 AND project_id=$2 AND credential_id=$3 AND credential_version=$4 AND credential_lease_id=$5 AND operation='exchange' AND io_retired=$6 AND ($6 OR NOT lease_released))`, w.requestID, r.Ref.Details().Scope.Details().ProjectID, r.Ref.Details().ID.String(), int64(w.version), r.LeaseID.String(), r.Action == sc.ReleaseLeaseUsage).Scan(&exact)
	if err != nil {
		return safeError(err)
	}
	if !exact {
		return fail(f.Forbidden)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.active {
		return fail(f.Forbidden)
	}
	w.validated = tx
	return ctx.Err()
}
func (s *Service) authorizeOAuthUsage(ctx context.Context, tx f.Tx, actor id.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	w, ok := ctx.Value(oauthUsageKey{}).(*oauthUsage)
	if !ok || w == nil {
		return sc.UseGrant{}, fail(f.Forbidden)
	}
	if _, err := s.oauthUsage(ctx, w.request); err != nil {
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
	if !valid || want != action || !actor.Equal(w.request.Actor) || !ref.Equal(w.request.Ref) || !owner.Equal(w.request.LeaseOwner) {
		return sc.UseGrant{}, fail(f.Forbidden)
	}
	subject := w.subject
	if action == sc.ReleaseLease {
		subject = actor
	}
	return sc.UseGrant{Subject: subject, Consumer: sc.MCP}, nil
}
func (s *Service) applyOAuthUsage(ctx context.Context, p *oauthExchange, action sc.UsageAction) error {
	if nilPort(s.options.Secret) {
		return fail(f.DependencyUnbound)
	}
	w, err := s.newOAuthUsage(p, action)
	if err != nil {
		return err
	}
	defer w.close()
	ctx = context.WithValue(ctx, oauthUsageKey{}, w)
	plan, err := s.options.Secret.DiscoverUsage(ctx, w.request)
	if err != nil {
		return safeError(err)
	}
	cause, _ := f.NewRecoveryCause("mcp.oauth.usage", p.owner.requestID, "")
	if action == sc.AcquireLeaseUsage {
		p.leaseAbsent = false
	}
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return err
		}
		if _, err := s.options.Secret.ApplyUsageInTx(ctx, tx, w.request, plan); err != nil {
			return err
		}
		if action == sc.ReleaseLeaseUsage {
			x, err := s.store.InTx(tx)
			if err != nil {
				return err
			}
			tag, err := x.Exec(ctx, `UPDATE agenteam_mcp.oauth_requests SET lease_released=true WHERE id=$1 AND io_retired AND credential_lease_id=$2`, p.owner.requestID, p.lease.String())
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return fail(f.InvalidState)
			}
		}
		return nil
	})
	err = commit(result)
	if result.State() == f.Committed {
		if action == sc.AcquireLeaseUsage {
			p.leaseAcquired = true
		}
		if action == sc.ReleaseLeaseUsage {
			p.leaseReleased = true
		}
		return nil
	}
	if result.State() == f.NotCommitted && action == sc.AcquireLeaseUsage {
		p.leaseAbsent = true
	}
	if result.State() != f.Unknown {
		return err
	}
	observer, ok := s.options.Secret.(sc.MCPRequestLeaseObservation)
	if !ok || nilPort(observer) {
		return err
	}
	tail, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var state sc.MCPRequestLeaseState
	observed := s.store.WithinTx(tail, cause, func(ctx context.Context, tx f.Tx) error {
		if e := s.store.AcquireAll(ctx, tx, plan.RequiredLocks()); e != nil {
			return e
		}
		var e error
		state, e = observer.ObserveMCPRequestLeaseInTx(ctx, tx, w.request, plan)
		return e
	})
	if observed.State() != f.Committed {
		return err
	}
	if action == sc.AcquireLeaseUsage {
		p.leaseAbsent = !state.Exists
		p.leaseAcquired = state.Exists && !state.Released
		if p.leaseAcquired {
			return nil
		}
	}
	if action == sc.ReleaseLeaseUsage && state.Exists && state.Released {
		p.leaseReleased = true
		return nil
	}
	return err
}
func (s *Service) readOAuthFlow(ctx context.Context, p *oauthExchange) (sc.SecretMaterial, error) {
	if nilPort(s.options.SecretReader) {
		return sc.SecretMaterial{}, fail(f.DependencyUnbound)
	}
	if err := s.applyOAuthUsage(ctx, p, sc.AcquireLeaseUsage); err != nil {
		return sc.SecretMaterial{}, err
	}
	w, err := s.newOAuthUsage(p, sc.ReadLeaseUsage)
	if err != nil {
		return sc.SecretMaterial{}, err
	}
	defer w.close()
	material, err := s.options.SecretReader.ReadMCPCredentialForUsage(context.WithValue(ctx, oauthUsageKey{}, w), w.request)
	if err != nil {
		material.Destroy()
		return sc.SecretMaterial{}, safeError(err)
	}
	return material, nil
}
