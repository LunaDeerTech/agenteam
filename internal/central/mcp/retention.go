package mcp

import (
	"context"
	"errors"
	"slices"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
)

type retentionPlan struct {
	owner    *Service
	request  tc.ExecutionToolCaptureRequest
	bindings []tc.MCPBinding
	entries  map[string]*retentionEntry
	locks    []f.LockRequest
}
type retentionEntry struct {
	binding    tc.MCPBinding
	connection tc.MCPResourceConnection
	credential string
	lease      sc.LeaseID
	usage      sc.UsageRequest
	secret     sc.UsageDependencies
	mapping    f.Digest
}

func (p *retentionPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}

type retentionUseKey struct{}
type retentionUse struct {
	mu        sync.Mutex
	plan      *retentionPlan
	entry     *retentionEntry
	binding   tc.MCPBinding
	resources *resourceRetentionPlan
	active    bool
	validated f.Tx
}

func (w *retentionUse) close() { w.mu.Lock(); w.active = false; w.mu.Unlock() }
func (s *Service) DiscoverMCPRetention(ctx context.Context, request tc.ExecutionToolCaptureRequest, bindings []tc.MCPBinding) (tc.MCPRetentionPlan, error) {
	if s == nil || ctx == nil || request.Validate() != nil || len(bindings) == 0 || len(bindings) > 128 || nilPort(s.options.Capture) {
		return nil, fail(f.DependencyUnbound)
	}
	for _, b := range bindings {
		if b.Validate() != nil || b.ProjectID != request.ProjectID {
			return nil, fail(f.Forbidden)
		}
	}
	locks, err := mergeLocks(request.RequiredLocks(), []f.LockRequest{registryLock(f.Shared)})
	if err != nil {
		return nil, err
	}
	p := &retentionPlan{owner: s, request: request, bindings: slices.Clone(bindings), entries: map[string]*retentionEntry{}, locks: locks}
	cause, _ := f.NewRecoveryCause("mcp.capture.discover", request.ExecutionID.String(), "")
	err = commit(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		for _, b := range bindings {
			if err = s.options.Capture.RequireMCPRetentionDiscoveryInTx(ctx, tx, request, b); err != nil {
				return safeError(err)
			}
			if err = s.RequireCurrentMCPBindingInTx(ctx, tx, b); err != nil {
				return err
			}
			if p.entries[b.RuntimeBindingID] != nil {
				continue
			}
			e := &retentionEntry{binding: b}
			if err = x.QueryRow(ctx, `SELECT COALESCE(credential_id,'') FROM agenteam_mcp.runtime_bindings WHERE id=$1`, b.RuntimeBindingID).Scan(&e.credential); err != nil {
				return safeError(err)
			}
			if e.credential != "" {
				var prior string
				err = x.QueryRow(ctx, `SELECT l.credential_lease_id FROM agenteam_mcp.runtime_leases l JOIN agenteam_mcp.runtime_bindings r ON r.id=l.runtime_binding_id WHERE l.execution_id=$1 AND r.project_id=$2 AND r.credential_id=$3 AND NOT l.retired LIMIT 1`, request.ExecutionID.String(), request.ProjectID.String(), e.credential).Scan(&prior)
				if err == nil {
					e.lease, err = f.ParseID[sc.Lease](prior)
				} else if errors.Is(err, pgx.ErrNoRows) {
					err = nil
				}
				if err != nil {
					return safeError(err)
				}
			}
			p.entries[b.RuntimeBindingID] = e
		}
		return nil
	}))
	if err != nil {
		return nil, err
	}
	leases := map[string]sc.LeaseID{}
	for _, e := range p.entries {
		if e.credential != "" && e.lease.Validate() == nil {
			leases[e.credential] = e.lease
		}
	}
	for _, e := range p.entries {
		if e.credential == "" {
			continue
		}
		if nilPort(s.options.Secret) {
			return nil, fail(f.DependencyUnbound)
		}
		if e.lease.Validate() != nil {
			e.lease = leases[e.credential]
			if e.lease.Validate() != nil {
				e.lease, err = f.NewID[sc.Lease]()
				if err != nil {
					return nil, fail(f.DependencyUnavailable)
				}
				leases[e.credential] = e.lease
			}
		}
		credential, err := f.ParseID[sc.Credential](e.credential)
		if err != nil {
			return nil, fail(f.InvalidState)
		}
		scope, _ := id.InProject(request.ProjectID)
		ref, _ := sc.NewCredentialRef(credential, scope)
		owner, _ := sc.NewCredentialLeaseOwner(sc.ExecutionOwner, request.ExecutionID.String())
		registration, _ := id.RegisterService(id.SecretService)
		actor, err := registration.Actor(request.ExecutionID.String(), scope)
		if err != nil {
			return nil, err
		}
		e.usage = sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.MCP, LeaseOwner: owner, LeaseID: e.lease, Action: sc.AcquireLeaseUsage}
		binding, err := sc.UsageBinding(e.usage)
		if err != nil {
			return nil, err
		}
		raw, err := encode(struct {
			Request tc.ExecutionToolCaptureRequest
			Binding tc.MCPBinding
			Usage   f.Digest
		}{request, e.binding, binding})
		if err != nil {
			return nil, err
		}
		e.mapping = f.Digest(digest(raw))
		w := &retentionUse{plan: p, entry: e, binding: e.binding, active: true}
		e.secret, err = s.options.Secret.DiscoverUsage(context.WithValue(ctx, retentionUseKey{}, w), e.usage)
		w.close()
		if err != nil {
			return nil, safeError(err)
		}
		p.locks, err = mergeLocks(p.locks, e.secret.RequiredLocks())
		if err != nil {
			return nil, err
		}
	}
	return p, nil
}
func (s *Service) RetainMCPRuntimeInTx(ctx context.Context, tx f.Tx, execution id.ExecutionID, b tc.MCPBinding, plan tc.MCPRetentionPlan) error {
	if s == nil || nilPort(s.options.Capture) {
		return fail(f.DependencyUnbound)
	}
	p, ok := plan.(*retentionPlan)
	if !ok || p == nil || p.owner != s || p.request.ExecutionID != execution || !slices.Contains(p.bindings, b) {
		return fail(f.Forbidden)
	}
	if err := s.store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return safeError(err)
	}
	if err := s.options.Capture.RequireMCPCaptureInTx(ctx, tx, execution, b); err != nil {
		return safeError(err)
	}
	if err := s.RequireCurrentMCPBindingInTx(ctx, tx, b); err != nil {
		return err
	}
	e := p.entries[b.RuntimeBindingID]
	if e == nil {
		return fail(f.Forbidden)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return safeError(err)
	}
	var credential string
	if err = x.QueryRow(ctx, `SELECT COALESCE(credential_id,'') FROM agenteam_mcp.runtime_bindings WHERE id=$1`, b.RuntimeBindingID).Scan(&credential); err != nil {
		return safeError(err)
	}
	if credential != e.credential {
		return fail(f.Forbidden)
	}
	leaseValue, err := credentialLeaseValue(credential, e.lease)
	if err != nil {
		return err
	}
	if credential != "" {
		w := &retentionUse{plan: p, entry: e, binding: b, active: true}
		defer w.close()
		if _, err = s.options.Secret.ApplyUsageInTx(context.WithValue(ctx, retentionUseKey{}, w), tx, e.usage, e.secret); err != nil {
			return safeError(err)
		}
	}
	_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.runtime_leases(execution_id,runtime_binding_id,project_id,connection_id,credential_lease_id) VALUES($1,$2,$3,$4,$5) ON CONFLICT(execution_id,runtime_binding_id) DO NOTHING`, execution.String(), b.RuntimeBindingID, b.ProjectID.String(), b.ConnectionID, null(leaseValue))
	if err != nil {
		return safeError(err)
	}
	var valid bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.runtime_leases WHERE execution_id=$1 AND runtime_binding_id=$2 AND project_id=$3 AND connection_id=$4 AND COALESCE(credential_lease_id,'')=$5 AND NOT retired)`, execution.String(), b.RuntimeBindingID, b.ProjectID.String(), b.ConnectionID, leaseValue).Scan(&valid)
	if err != nil {
		return safeError(err)
	}
	if !valid {
		return fail(f.InvalidState)
	}
	return ctx.Err()
}
func (s *Service) retentionUse(ctx context.Context, r sc.UsageRequest) (*retentionUse, error) {
	if ctx == nil {
		return nil, fail(f.Forbidden)
	}
	w, ok := ctx.Value(retentionUseKey{}).(*retentionUse)
	if !ok || w == nil || w.plan == nil || w.plan.owner != s || w.entry == nil {
		return nil, fail(f.Forbidden)
	}
	w.mu.Lock()
	active := w.active
	w.mu.Unlock()
	got, err := sc.UsageBinding(r)
	want, e := sc.UsageBinding(w.entry.usage)
	if !active || err != nil || e != nil || got != want {
		return nil, fail(f.Forbidden)
	}
	return w, nil
}
func (s *Service) discoverRetentionSecret(ctx context.Context, r sc.UsageRequest) (sc.UsageDependencies, error) {
	w, err := s.retentionUse(ctx, r)
	if err != nil {
		return sc.UsageDependencies{}, err
	}
	binding, _ := sc.UsageBinding(r)
	locks, err := mergeLocks(w.plan.request.RequiredLocks(), []f.LockRequest{registryLock(f.Shared)})
	if err != nil {
		return sc.UsageDependencies{}, err
	}
	return sc.NewUsageDependencies(s.secretIssuer, binding, w.entry.mapping, locks)
}
func (s *Service) validateRetentionSecret(ctx context.Context, tx f.Tx, r sc.UsageRequest, d sc.UsageDependencies) error {
	if s == nil || nilPort(s.options.Capture) {
		return fail(f.DependencyUnbound)
	}
	w, err := s.retentionUse(ctx, r)
	if err != nil {
		return err
	}
	binding, _ := sc.UsageBinding(r)
	if !d.Matches(s.secretIssuer, binding, w.entry.mapping) {
		return fail(f.Forbidden)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, w.plan.locks); err != nil {
		return safeError(err)
	}
	runtimeID := w.binding.RuntimeBindingID
	if w.resources != nil {
		if err = s.requireResourceRetentionInTx(ctx, tx, w.resources); err != nil {
			return err
		}
		runtimeID = w.entry.connection.RuntimeBindingID
	} else {
		if err = s.options.Capture.RequireMCPCaptureInTx(ctx, tx, w.plan.request.ExecutionID, w.binding); err != nil {
			return safeError(err)
		}
		if err = s.RequireCurrentMCPBindingInTx(ctx, tx, w.binding); err != nil {
			return err
		}
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return safeError(err)
	}
	var current string
	if err = x.QueryRow(ctx, `SELECT COALESCE(credential_id,'') FROM agenteam_mcp.runtime_bindings WHERE id=$1`, runtimeID).Scan(&current); err != nil {
		return safeError(err)
	}
	if current != w.entry.credential {
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
func (s *Service) authorizeRetentionSecret(ctx context.Context, tx f.Tx, actor id.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	w, ok := ctx.Value(retentionUseKey{}).(*retentionUse)
	if !ok || w == nil {
		return sc.UseGrant{}, fail(f.Forbidden)
	}
	if _, err := s.retentionUse(ctx, w.entry.usage); err != nil {
		return sc.UseGrant{}, err
	}
	w.mu.Lock()
	valid := tx.Valid() && w.active && w.validated == tx
	w.mu.Unlock()
	if !valid || action != sc.AcquireLease || !actor.Equal(w.entry.usage.Actor) || !ref.Equal(w.entry.usage.Ref) || !owner.Equal(w.entry.usage.LeaseOwner) {
		return sc.UseGrant{}, fail(f.Forbidden)
	}
	return sc.UseGrant{Subject: actor, Consumer: sc.MCP}, nil
}

var _ tc.MCPRegistryAuthority = (*Service)(nil)
