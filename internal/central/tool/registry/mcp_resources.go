package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"sync/atomic"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type resourceCaptureKey struct{}
type resourceCaptureWitness struct {
	owner       *executionCaptureState
	request     tc.ExecutionToolCaptureRequest
	tx          f.Tx
	connections []tc.MCPResourceConnection
	discovery   bool
	live        atomic.Bool
}

func (c *ExecutionCapture) discoverMCPResources(ctx context.Context, request tc.ExecutionToolCaptureRequest, entries []currentTool, tools tc.MCPRetentionPlan) (tc.MCPResourceCapturePlan, error) {
	s := c.state()
	if s == nil || ctx == nil || request.Validate() != nil {
		return nil, fail(f.InvalidArgument)
	}
	found := false
	for _, e := range entries {
		if e.Binding.ContractRevision == 1 && (e.Binding.HandlerID == "mcp.list-resources" || e.Binding.HandlerID == "mcp.read-resource") {
			found = true
		}
	}
	if !found {
		return nil, nil
	}
	provider, ok := s.registry.data().mcp.(tc.MCPResourceCaptureProvider)
	if !ok || nilPort(provider) {
		return nil, fail(f.DependencyUnbound)
	}
	w := &resourceCaptureWitness{owner: s, request: request, discovery: true}
	w.live.Store(true)
	defer w.live.Store(false)
	plan, err := provider.DiscoverMCPResources(context.WithValue(ctx, resourceCaptureKey{}, w), request, tools)
	if err != nil {
		return nil, captureError(err)
	}
	if nilPort(plan) {
		return nil, fail(f.DependencyUnbound)
	}
	if _, err = resourceCaptureDigest(request, plan.RuntimeConnections()); err != nil {
		return nil, err
	}
	return plan, nil
}
func (c *ExecutionCapture) RequireMCPResourceDiscoveryInTx(ctx context.Context, tx f.Tx, request tc.ExecutionToolCaptureRequest) error {
	s := c.state()
	if s == nil || ctx == nil {
		return fail(f.Forbidden)
	}
	w, ok := ctx.Value(resourceCaptureKey{}).(*resourceCaptureWitness)
	if !ok || w == nil || w.owner != s || !w.discovery || !w.live.Load() || w.request != request {
		return fail(f.Forbidden)
	}
	if _, err := s.registry.executor(ctx, tx, append(request.RequiredLocks(), RegistryLock(f.Shared))); err != nil {
		return err
	}
	facts, err := s.owner.RequireToolDiscoveryInTx(ctx, tx, request)
	if err != nil {
		return captureError(err)
	}
	return facts.Validate(request)
}
func (c *ExecutionCapture) RequireMCPResourceCaptureInTx(ctx context.Context, tx f.Tx, request tc.ExecutionToolCaptureRequest, connections []tc.MCPResourceConnection) error {
	s := c.state()
	if s == nil || ctx == nil {
		return fail(f.Forbidden)
	}
	w, ok := ctx.Value(resourceCaptureKey{}).(*resourceCaptureWitness)
	if !ok || w == nil || w.owner != s || w.discovery || !w.live.Load() || w.tx != tx || w.request != request || !slices.Equal(w.connections, connections) {
		return fail(f.Forbidden)
	}
	if _, err := s.registry.executor(ctx, tx, append(request.RequiredLocks(), RegistryLock(f.Shared))); err != nil {
		return err
	}
	facts, err := s.owner.RequireToolCaptureInTx(ctx, tx, request)
	if err != nil {
		return captureError(err)
	}
	return facts.Validate(request)
}
func (c *ExecutionCapture) retainMCPResourcesInTx(ctx context.Context, tx f.Tx, request tc.ExecutionToolCaptureRequest, plan tc.MCPResourceCapturePlan) error {
	if nilPort(plan) {
		return nil
	}
	s := c.state()
	if s == nil {
		return fail(f.DependencyUnbound)
	}
	provider, ok := s.registry.data().mcp.(tc.MCPResourceCaptureProvider)
	if !ok || nilPort(provider) {
		return fail(f.DependencyUnbound)
	}
	connections := slices.Clone(plan.RuntimeConnections())
	digest, err := resourceCaptureDigest(request, connections)
	if err != nil {
		return err
	}
	locks, err := normalizedLocks(append(plan.RequiredLocks(), append(request.RequiredLocks(), RegistryLock(f.Shared))...))
	if err != nil {
		return err
	}
	x, err := s.registry.executor(ctx, tx, locks)
	if err != nil {
		return err
	}
	w := &resourceCaptureWitness{owner: s, request: request, tx: tx, connections: slices.Clone(connections)}
	w.live.Store(true)
	defer w.live.Store(false)
	if err = provider.RetainMCPResourcesInTx(context.WithValue(ctx, resourceCaptureKey{}, w), tx, request, plan); err != nil {
		return captureError(err)
	}
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_tool.execution_mcp_resource_captures(execution_id,project_id,agent_id,connection_count,bindings_digest) VALUES($1,$2,$3,$4,$5) ON CONFLICT(execution_id) DO NOTHING`, request.ExecutionID.String(), request.ProjectID.String(), request.AgentID.String(), len(connections), string(digest))
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() == 1 {
		for _, b := range connections {
			_, err = x.Exec(ctx, `INSERT INTO agenteam_tool.execution_mcp_resources(execution_id,project_id,config_id,connection_id,runtime_binding_id,config_revision) VALUES($1,$2,$3,$4,$5,$6)`, request.ExecutionID.String(), b.ProjectID.String(), b.ConfigID, b.ConnectionID, b.RuntimeBindingID, int64(b.ConfigRevision))
			if err != nil {
				return portError(err)
			}
		}
	}
	current, err := readMCPResourceConnections(ctx, x, request)
	if err != nil {
		return err
	}
	if !slices.Equal(current, connections) {
		return fail(f.ConfirmationStale)
	}
	return ctx.Err()
}

func resourceCaptureDigest(request tc.ExecutionToolCaptureRequest, connections []tc.MCPResourceConnection) (f.Digest, error) {
	if request.Validate() != nil || len(connections) > 64 {
		return "", fail(f.InvalidArgument)
	}
	for n, b := range connections {
		if b.Validate() != nil || b.ProjectID != request.ProjectID || n > 0 && connections[n-1].ConnectionID >= b.ConnectionID {
			return "", fail(f.InvalidState)
		}
	}
	// The empty set has an explicit canonical head, distinguishable from a
	// missing capture. Ordering is a part of its immutable persisted identity.
	if connections == nil {
		connections = []tc.MCPResourceConnection{}
	}
	raw, err := json.Marshal(struct {
		Format      int
		Request     tc.ExecutionToolCaptureRequest
		Connections []tc.MCPResourceConnection
	}{1, request, connections})
	if err != nil {
		return "", fail(f.InvalidState)
	}
	sum := sha256.Sum256(raw)
	return f.ParseDigest("sha256:" + hex.EncodeToString(sum[:]))
}

func readMCPResourceConnections(ctx context.Context, x postgres.SQLExecutor, request tc.ExecutionToolCaptureRequest) ([]tc.MCPResourceConnection, error) {
	var project, agent, digest string
	var count int
	err := x.QueryRow(ctx, `SELECT project_id,agent_id,connection_count,bindings_digest FROM agenteam_tool.execution_mcp_resource_captures WHERE execution_id=$1`, request.ExecutionID.String()).Scan(&project, &agent, &count, &digest)
	if err != nil {
		return nil, portError(err)
	}
	if project != request.ProjectID.String() || agent != request.AgentID.String() || count < 0 || count > 64 {
		return nil, fail(f.Forbidden)
	}
	rows, err := x.Query(ctx, `SELECT project_id,config_id,connection_id,runtime_binding_id,config_revision FROM agenteam_tool.execution_mcp_resources WHERE execution_id=$1 ORDER BY connection_id COLLATE "C" LIMIT 65`, request.ExecutionID.String())
	if err != nil {
		return nil, portError(err)
	}
	defer rows.Close()
	connections := make([]tc.MCPResourceConnection, 0, count)
	for rows.Next() {
		var b tc.MCPResourceConnection
		var p string
		var revision int64
		if err = rows.Scan(&p, &b.ConfigID, &b.ConnectionID, &b.RuntimeBindingID, &revision); err != nil {
			return nil, portError(err)
		}
		if p != project {
			return nil, fail(f.Forbidden)
		}
		b.ProjectID = request.ProjectID
		b.ConfigRevision = f.Version(revision)
		connections = append(connections, b)
	}
	if err = rows.Err(); err != nil {
		return nil, portError(err)
	}
	actual, err := resourceCaptureDigest(request, connections)
	if err != nil {
		return nil, err
	}
	if len(connections) != count || string(actual) != digest {
		return nil, fail(f.InvalidState)
	}
	return slices.Clone(connections), nil
}

// This returns immutable Registry metadata, not a dispatch grant. The runtime
// pairs it with its original active Builtin witness in this same transaction.
func (r *Registry) ReadMCPResourceConnectionsInTx(ctx context.Context, tx f.Tx, call tc.ToolCallBinding) ([]tc.MCPResourceConnection, error) {
	if call.Validate() != nil || call.Binding.ContractRevision != 1 || (call.Binding.HandlerID != "mcp.list-resources" && call.Binding.HandlerID != "mcp.read-resource") {
		return nil, fail(f.InvalidArgument)
	}
	key, err := f.AggregateLock(f.ExecutionAggregate, call.ExecutionID.String())
	if err != nil {
		return nil, err
	}
	x, err := r.executor(ctx, tx, []f.LockRequest{RegistryLock(f.Shared), {Key: key, Mode: f.Shared}})
	if err != nil {
		return nil, err
	}
	return readMCPResourceConnections(ctx, x, tc.ExecutionToolCaptureRequest{ProjectID: call.ProjectID, AgentID: call.AgentID, ExecutionID: call.ExecutionID})
}

var _ tc.MCPResourceCaptureAuthority = (*ExecutionCapture)(nil)
