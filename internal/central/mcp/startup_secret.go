package mcp

import (
	"context"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type startupSecretKey struct{}
type startupSecretUse struct {
	mu        sync.Mutex
	startup   *startupUse
	request   sc.UsageRequest
	mapping   f.Digest
	active    bool
	validated f.Tx
}

func (w *startupSecretUse) close() { w.mu.Lock(); w.active = false; w.mu.Unlock() }

func (s *Service) newStartupSecret(w *startupUse, action sc.UsageAction) (*startupSecretUse, error) {
	if w == nil || w.service != s || !w.active.Load() || w.lease.Validate() != nil || action != sc.AcquireLeaseUsage && action != sc.ReadLeaseUsage && action != sc.ReleaseLeaseUsage {
		return nil, fail(f.Forbidden)
	}
	credential, err := f.ParseID[sc.Credential](w.connection.CredentialID)
	if err != nil {
		return nil, fail(f.InvalidState)
	}
	scope, _ := id.InProject(w.connection.ProjectID)
	ref, err := sc.NewCredentialRef(credential, scope)
	if err != nil {
		return nil, err
	}
	owner, err := sc.NewCredentialLeaseOwner(sc.MCPConnectionRequestOwner, w.request)
	if err != nil {
		return nil, err
	}
	registration, _ := id.RegisterService(id.SecretService)
	actor, err := registration.Actor(w.request, scope)
	if err != nil {
		return nil, err
	}
	r := sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.MCP, LeaseOwner: owner, LeaseID: w.lease, Action: action}
	if action == sc.ReadLeaseUsage {
		r.RequestID = w.request
	}
	binding, err := sc.UsageBinding(r)
	if err != nil {
		return nil, err
	}
	raw, err := encode(struct {
		Binding                                                f.Digest
		Process, Request, Project, Connection, Config, Runtime string
		Revision, Version                                      f.Version
	}{binding, w.process.String(), w.request, w.connection.ProjectID.String(), w.connection.ID.String(), w.config.ID.String(), w.connection.RuntimeBindingID, w.config.RuntimeRevision, w.connection.Version})
	if err != nil {
		return nil, err
	}
	return &startupSecretUse{startup: w, request: r, mapping: f.Digest(digest(raw)), active: true}, nil
}

func (s *Service) startupSecretUse(ctx context.Context, r sc.UsageRequest) (*startupSecretUse, error) {
	if ctx == nil || r.Validate() != nil || r.Purpose != sc.MCP {
		return nil, fail(f.Forbidden)
	}
	w, ok := ctx.Value(startupSecretKey{}).(*startupSecretUse)
	if !ok || w == nil || w.startup == nil || w.startup.service != s {
		return nil, fail(f.Forbidden)
	}
	original, err := s.startupUse(ctx)
	if err != nil || original != w.startup {
		return nil, fail(f.Forbidden)
	}
	w.mu.Lock()
	active := w.active
	w.mu.Unlock()
	a, err := sc.UsageBinding(r)
	b, other := sc.UsageBinding(w.request)
	if !active || err != nil || other != nil || a != b {
		return nil, fail(f.Forbidden)
	}
	return w, nil
}

func (s *Service) discoverStartupSecret(ctx context.Context, r sc.UsageRequest) (sc.UsageDependencies, error) {
	w, err := s.startupSecretUse(ctx, r)
	if err != nil {
		return sc.UsageDependencies{}, err
	}
	binding, err := sc.UsageBinding(r)
	if err != nil {
		return sc.UsageDependencies{}, err
	}
	return sc.NewUsageDependencies(s.secretIssuer, binding, w.mapping, w.startup.locks)
}

func (s *Service) validateStartupSecret(ctx context.Context, tx f.Tx, r sc.UsageRequest, plan sc.UsageDependencies) error {
	w, err := s.startupSecretUse(ctx, r)
	if err != nil {
		return err
	}
	binding, err := sc.UsageBinding(r)
	if err != nil || !plan.Matches(s.secretIssuer, binding, w.mapping) {
		return fail(f.Forbidden)
	}
	current := r.Action != sc.ReleaseLeaseUsage
	original, fact, err := s.requireStartupInTx(ctx, tx, current)
	if err != nil {
		return err
	}
	if original != w.startup || fact.released || current && fact.retired || !current && !fact.retired {
		return fail(f.Forbidden)
	}
	if current {
		if err = s.requireStartupProject(ctx, tx, original); err != nil {
			return err
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

func (s *Service) authorizeStartupSecret(ctx context.Context, tx f.Tx, actor id.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	if ctx == nil {
		return sc.UseGrant{}, fail(f.Forbidden)
	}
	w, ok := ctx.Value(startupSecretKey{}).(*startupSecretUse)
	if !ok || w == nil {
		return sc.UseGrant{}, fail(f.Forbidden)
	}
	if _, err := s.startupSecretUse(ctx, w.request); err != nil {
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
	// The explicit usage reader takes the request association from its opaque
	// UsageRequest proof. A grant RequestID belongs to the legacy read port.
	return sc.UseGrant{Subject: actor, Consumer: sc.MCP}, nil
}

func (s *Service) applyStartupSecret(ctx context.Context, original *startupUse, action sc.UsageAction) error {
	if nilPort(s.options.Secret) {
		return fail(f.DependencyUnbound)
	}
	w, err := s.newStartupSecret(original, action)
	if err != nil {
		return err
	}
	defer w.close()
	ctx = context.WithValue(ctx, startupSecretKey{}, w)
	plan, err := s.options.Secret.DiscoverUsage(ctx, w.request)
	if err != nil {
		return safeError(err)
	}
	cause, _ := f.NewRecoveryCause("mcp.startup.secret", original.request, "")
	return commit(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return err
		}
		_, err := s.options.Secret.ApplyUsageInTx(ctx, tx, w.request, plan)
		return err
	}))
}

func (s *Service) readStartupCredential(ctx context.Context, original *startupUse) (sc.SecretMaterial, error) {
	if nilPort(s.options.SecretReader) {
		return sc.SecretMaterial{}, fail(f.DependencyUnbound)
	}
	if err := s.applyStartupSecret(ctx, original, sc.AcquireLeaseUsage); err != nil {
		return sc.SecretMaterial{}, err
	}
	w, err := s.newStartupSecret(original, sc.ReadLeaseUsage)
	if err != nil {
		return sc.SecretMaterial{}, err
	}
	defer w.close()
	material, err := s.options.SecretReader.ReadMCPCredentialForUsage(context.WithValue(ctx, startupSecretKey{}, w), w.request)
	if err != nil {
		material.Destroy()
		return sc.SecretMaterial{}, safeError(err)
	}
	return material, nil
}
