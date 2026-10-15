package mcp

import (
	"context"
	"errors"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
)

type resourceRetentionPlan struct {
	base        *retentionPlan
	connections []tc.MCPResourceConnection
}

func (p *resourceRetentionPlan) RequiredLocks() []f.LockRequest {
	if p == nil || p.base == nil {
		return nil
	}
	return slices.Clone(p.base.locks)
}
func (p *resourceRetentionPlan) RuntimeConnections() []tc.MCPResourceConnection {
	if p == nil {
		return nil
	}
	return slices.Clone(p.connections)
}

func (s *Service) resourceCaptureAuthority() (tc.MCPResourceCaptureAuthority, error) {
	if s == nil {
		return nil, fail(f.DependencyUnbound)
	}
	a, ok := s.options.Capture.(tc.MCPResourceCaptureAuthority)
	if !ok || nilPort(a) {
		return nil, fail(f.DependencyUnbound)
	}
	return a, nil
}
func currentResourceConnections(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID) ([]tc.MCPResourceConnection, error) {
	rows, err := x.Query(ctx, `SELECT c.config_id,c.id,c.runtime_binding_id,c.config_revision FROM agenteam_mcp.connections c JOIN agenteam_mcp.configs f ON f.id=c.config_id AND f.runtime_revision=c.config_revision JOIN agenteam_mcp.runtime_bindings r ON r.id=c.runtime_binding_id AND r.project_id=c.project_id AND r.connection_id=c.id AND r.config_id=c.config_id AND r.config_revision=c.config_revision WHERE c.project_id=$1 AND c.state='connected' AND c.enabled AND c.resources_revision>0 ORDER BY c.id COLLATE "C" LIMIT 65`, project.String())
	if err != nil {
		return nil, safeError(err)
	}
	defer rows.Close()
	connections := []tc.MCPResourceConnection{}
	for rows.Next() {
		b := tc.MCPResourceConnection{ProjectID: project}
		var revision int64
		if err = rows.Scan(&b.ConfigID, &b.ConnectionID, &b.RuntimeBindingID, &revision); err != nil {
			return nil, safeError(err)
		}
		b.ConfigRevision = f.Version(revision)
		if b.Validate() != nil || len(connections) >= 64 {
			return nil, fail(f.PayloadTooLarge)
		}
		connections = append(connections, b)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError(err)
	}
	return connections, nil
}

func (s *Service) DiscoverMCPResources(ctx context.Context, request tc.ExecutionToolCaptureRequest, tools tc.MCPRetentionPlan) (tc.MCPResourceCapturePlan, error) {
	if ctx == nil || request.Validate() != nil {
		return nil, fail(f.InvalidArgument)
	}
	authority, err := s.resourceCaptureAuthority()
	if err != nil {
		return nil, err
	}
	leases := map[string]sc.LeaseID{}
	if !nilPort(tools) {
		p, ok := tools.(*retentionPlan)
		if !ok || p == nil || p.owner != s || p.request != request {
			return nil, fail(f.Forbidden)
		}
		for _, e := range p.entries {
			if e.credential != "" && e.lease.Validate() == nil {
				leases[e.credential] = e.lease
			}
		}
	}
	locks, err := mergeLocks(request.RequiredLocks(), []f.LockRequest{registryLock(f.Shared)})
	if err != nil {
		return nil, err
	}
	plan := &resourceRetentionPlan{base: &retentionPlan{owner: s, request: request, entries: map[string]*retentionEntry{}, locks: locks}}
	cause, _ := f.NewRecoveryCause("mcp.resource.capture", request.ExecutionID.String(), "")
	err = commit(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return safeError(err)
		}
		if err := authority.RequireMCPResourceDiscoveryInTx(ctx, tx, request); err != nil {
			return safeError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return safeError(err)
		}
		plan.connections, err = currentResourceConnections(ctx, x, request.ProjectID)
		if err != nil {
			return err
		}
		for _, b := range plan.connections {
			e := &retentionEntry{connection: b}
			if err = x.QueryRow(ctx, `SELECT COALESCE(credential_id,'') FROM agenteam_mcp.runtime_bindings WHERE id=$1`, b.RuntimeBindingID).Scan(&e.credential); err != nil {
				return safeError(err)
			}
			if e.credential != "" {
				e.lease = leases[e.credential]
				var prior string
				err = x.QueryRow(ctx, `SELECT l.credential_lease_id FROM agenteam_mcp.runtime_leases l JOIN agenteam_mcp.runtime_bindings r ON r.id=l.runtime_binding_id WHERE l.execution_id=$1 AND r.project_id=$2 AND r.credential_id=$3 AND NOT l.retired LIMIT 1`, request.ExecutionID.String(), request.ProjectID.String(), e.credential).Scan(&prior)
				if err == nil {
					parsed, e2 := f.ParseID[sc.Lease](prior)
					if e2 != nil || e.lease.Validate() == nil && e.lease != parsed {
						return fail(f.InvalidState)
					}
					e.lease = parsed
					leases[e.credential] = parsed
				} else if !errors.Is(err, pgx.ErrNoRows) {
					return safeError(err)
				}
			}
			plan.base.entries[b.RuntimeBindingID] = e
		}
		return nil
	}))
	if err != nil {
		return nil, err
	}
	for _, connection := range plan.connections {
		e := plan.base.entries[connection.RuntimeBindingID]
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
					return nil, safeError(err)
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
			Request    tc.ExecutionToolCaptureRequest
			Connection tc.MCPResourceConnection
			Usage      f.Digest
		}{request, connection, binding})
		if err != nil {
			return nil, err
		}
		e.mapping = f.Digest(digest(raw))
		w := &retentionUse{plan: plan.base, entry: e, resources: plan, active: true}
		e.secret, err = s.options.Secret.DiscoverUsage(context.WithValue(ctx, retentionUseKey{}, w), e.usage)
		w.close()
		if err != nil {
			return nil, safeError(err)
		}
		plan.base.locks, err = mergeLocks(plan.base.locks, e.secret.RequiredLocks())
		if err != nil {
			return nil, err
		}
	}
	return plan, nil
}

func (s *Service) requireResourceRetentionInTx(ctx context.Context, tx f.Tx, p *resourceRetentionPlan) error {
	if p == nil || p.base == nil || p.base.owner != s {
		return fail(f.Forbidden)
	}
	if err := s.store.RequireHeldLocks(ctx, tx, p.base.locks); err != nil {
		return safeError(err)
	}
	authority, err := s.resourceCaptureAuthority()
	if err != nil {
		return err
	}
	if err = authority.RequireMCPResourceCaptureInTx(ctx, tx, p.base.request, p.connections); err != nil {
		return safeError(err)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return safeError(err)
	}
	current, err := currentResourceConnections(ctx, x, p.base.request.ProjectID)
	if err != nil {
		return err
	}
	if !slices.Equal(current, p.connections) {
		return fail(f.ConfirmationStale)
	}
	return nil
}
func (s *Service) RetainMCPResourcesInTx(ctx context.Context, tx f.Tx, request tc.ExecutionToolCaptureRequest, plan tc.MCPResourceCapturePlan) error {
	p, ok := plan.(*resourceRetentionPlan)
	if !ok || p == nil || p.base == nil || p.base.owner != s || p.base.request != request {
		return fail(f.Forbidden)
	}
	if err := s.requireResourceRetentionInTx(ctx, tx, p); err != nil {
		return err
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return safeError(err)
	}
	for _, b := range p.connections {
		e := p.base.entries[b.RuntimeBindingID]
		if e == nil || e.connection != b {
			return fail(f.Forbidden)
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
			w := &retentionUse{plan: p.base, entry: e, resources: p, active: true}
			_, err = s.options.Secret.ApplyUsageInTx(context.WithValue(ctx, retentionUseKey{}, w), tx, e.usage, e.secret)
			w.close()
			if err != nil {
				return safeError(err)
			}
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.runtime_leases(execution_id,runtime_binding_id,project_id,connection_id,credential_lease_id) VALUES($1,$2,$3,$4,$5) ON CONFLICT(execution_id,runtime_binding_id) DO NOTHING`, request.ExecutionID.String(), b.RuntimeBindingID, b.ProjectID.String(), b.ConnectionID, null(leaseValue))
		if err != nil {
			return safeError(err)
		}
		var valid bool
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.runtime_leases WHERE execution_id=$1 AND runtime_binding_id=$2 AND project_id=$3 AND connection_id=$4 AND COALESCE(credential_lease_id,'')=$5 AND NOT retired)`, request.ExecutionID.String(), b.RuntimeBindingID, b.ProjectID.String(), b.ConnectionID, leaseValue).Scan(&valid)
		if err != nil {
			return safeError(err)
		}
		if !valid {
			return fail(f.InvalidState)
		}
	}
	return ctx.Err()
}

var _ tc.MCPResourceCaptureProvider = (*Service)(nil)
