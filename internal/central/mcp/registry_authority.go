package mcp

import (
	"context"
	"encoding/json"
	"sync/atomic"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type registryWitnessKey struct{}
type registryWitness struct {
	owner      *Service
	tx         f.Tx
	project    id.ProjectID
	connection string
	discovery  string
	removing   bool
	active     atomic.Bool
}

func discoveryDigest(d tc.MCPDiscovery) (string, error) {
	raw, err := json.Marshal(d)
	if err != nil || len(raw) > 8<<20 {
		return "", fail(f.InvalidArgument)
	}
	return digest(raw), nil
}
func (s *Service) publishTools(ctx context.Context, tx f.Tx, d tc.MCPDiscovery) ([]tc.SpecRef, error) {
	if nilPort(s.options.Registry) {
		return nil, fail(f.DependencyUnbound)
	}
	hash, err := discoveryDigest(d)
	if err != nil {
		return nil, err
	}
	w := &registryWitness{owner: s, tx: tx, project: d.ProjectID, connection: d.ConnectionID, discovery: hash}
	w.active.Store(true)
	defer w.active.Store(false)
	return s.options.Registry.ReconcileMCPInTx(context.WithValue(ctx, registryWitnessKey{}, w), tx, d)
}
func (s *Service) removeTools(ctx context.Context, tx f.Tx, project id.ProjectID, connection string) error {
	if nilPort(s.options.Registry) {
		return fail(f.DependencyUnbound)
	}
	w := &registryWitness{owner: s, tx: tx, project: project, connection: connection, removing: true}
	w.active.Store(true)
	defer w.active.Store(false)
	return s.options.Registry.RemoveMCPConnectionInTx(context.WithValue(ctx, registryWitnessKey{}, w), tx, project, connection)
}
func (s *Service) registryWitness(ctx context.Context, tx f.Tx, project id.ProjectID, connection string) (*registryWitness, error) {
	if ctx == nil || s == nil {
		return nil, fail(f.Forbidden)
	}
	w, ok := ctx.Value(registryWitnessKey{}).(*registryWitness)
	if !ok || w == nil || !w.active.Load() || w.owner != s || w.tx != tx || w.project != project || w.connection != connection {
		return nil, fail(f.Forbidden)
	}
	if _, err := s.store.InTx(tx); err != nil {
		return nil, safeError(err)
	}
	if err := s.store.RequireHeldLocks(ctx, tx, []f.LockRequest{catalogLock(f.Exclusive), registryLock(f.Exclusive)}); err != nil {
		return nil, safeError(err)
	}
	return w, nil
}
func (s *Service) RequireBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if ctx == nil || s == nil || !sameStore(s.store, store) {
		return fail(f.Forbidden)
	}
	_, err := s.store.InTx(tx)
	return safeError(err)
}
func (s *Service) RequireMCPDiscoveryInTx(ctx context.Context, tx f.Tx, d tc.MCPDiscovery) error {
	w, err := s.registryWitness(ctx, tx, d.ProjectID, d.ConnectionID)
	if err != nil {
		return err
	}
	hash, err := discoveryDigest(d)
	if err != nil || w.removing || hash != w.discovery {
		return fail(f.Forbidden)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return safeError(err)
	}
	var present bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.connections c JOIN agenteam_mcp.configs f ON f.id=c.config_id JOIN agenteam_mcp.runtime_bindings r ON r.id=$4 AND r.connection_id=c.id AND r.project_id=c.project_id WHERE c.id=$1 AND c.project_id=$2 AND c.config_id=$3 AND c.config_revision=$5 AND f.runtime_revision=$5 AND r.config_revision=$5 AND r.config_id=f.id AND c.refresh_id IS NOT NULL)`, d.ConnectionID, d.ProjectID.String(), d.ConfigID, d.RuntimeBindingID, int64(d.ConfigRevision)).Scan(&present)
	if err != nil {
		return safeError(err)
	}
	if !present {
		return fail(f.Forbidden)
	}
	return ctx.Err()
}
func (s *Service) RequireMCPRemovalInTx(ctx context.Context, tx f.Tx, project id.ProjectID, connection string) error {
	w, err := s.registryWitness(ctx, tx, project, connection)
	if err != nil {
		return err
	}
	if !w.removing {
		return fail(f.Forbidden)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return safeError(err)
	}
	var present bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.connections WHERE id=$1 AND project_id=$2)`, connection, project.String()).Scan(&present)
	if err != nil {
		return safeError(err)
	}
	if !present {
		return fail(f.Forbidden)
	}
	return ctx.Err()
}
func (s *Service) RequireCurrentMCPBindingInTx(ctx context.Context, tx f.Tx, b tc.MCPBinding) error {
	if s == nil || ctx == nil || b.Validate() != nil {
		return fail(f.Forbidden)
	}
	if err := s.store.RequireHeldLocks(ctx, tx, []f.LockRequest{registryLock(f.Shared)}); err != nil {
		return safeError(err)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return safeError(err)
	}
	var present bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.connections c JOIN agenteam_mcp.configs f ON f.id=c.config_id JOIN agenteam_mcp.runtime_bindings r ON r.id=c.runtime_binding_id AND r.connection_id=c.id AND r.project_id=c.project_id WHERE c.id=$1 AND c.project_id=$2 AND c.config_id=$3 AND c.runtime_binding_id=$4 AND c.config_revision=$5 AND f.runtime_revision=$5 AND r.config_revision=$5 AND r.config_id=f.id AND c.state='connected' AND c.enabled)`, b.ConnectionID, b.ProjectID.String(), b.ConfigID, b.RuntimeBindingID, int64(b.ConfigRevision)).Scan(&present)
	if err != nil {
		return safeError(err)
	}
	if !present {
		return fail(f.CapabilityUnsupported)
	}
	return ctx.Err()
}
