package registry

import (
	"bytes"
	"context"
	"errors"
	"math"
	"slices"
	"sync/atomic"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
)

func (r *Registry) mcpAuthority(ctx context.Context, tx f.Tx) (tc.MCPRegistryAuthority, error) {
	s := r.data()
	if s == nil || nilPort(s.mcp) {
		return nil, fail(f.DependencyUnbound)
	}
	if err := s.mcp.RequireBindingInTx(ctx, tx, s.store); err != nil {
		return nil, portError(err)
	}
	return s.mcp, nil
}

// Reconcile publishes one complete, prevalidated discovery. Invalid remote
// entries included in PresentNames retain their previous exact serving head.
func (r *Registry) ReconcileMCPInTx(ctx context.Context, tx f.Tx, d tc.MCPDiscovery) ([]tc.SpecRef, error) {
	probe := tc.MCPBinding{ProjectID: d.ProjectID, ConfigID: d.ConfigID, ConnectionID: d.ConnectionID, RuntimeBindingID: d.RuntimeBindingID, ConfigRevision: d.ConfigRevision, RemoteToolName: "probe"}
	if probe.Validate() != nil || len(d.Tools) > 10000 || len(d.PresentNames) > 10000 {
		return nil, fail(f.InvalidArgument)
	}
	present := map[string]bool{}
	for _, name := range d.PresentNames {
		b := probe
		b.RemoteToolName = name
		if b.Validate() != nil || present[name] {
			return nil, fail(f.InvalidArgument)
		}
		present[name] = true
	}
	definitions := make([][]byte, len(d.Tools))
	seen := map[string]bool{}
	for n, v := range d.Tools {
		b := probe
		b.RemoteToolName = v.Binding.RemoteToolName
		if v.Validate(ctx) != nil || b != v.Binding || !present[b.RemoteToolName] || seen[b.RemoteToolName] {
			return nil, fail(f.InvalidArgument)
		}
		seen[b.RemoteToolName] = true
		var err error
		definitions[n], err = tc.CanonicalDefinition(ctx, v.Definition)
		if err != nil {
			return nil, err
		}
	}
	x, err := r.executor(ctx, tx, []f.LockRequest{RegistryLock(f.Exclusive)})
	if err != nil {
		return nil, err
	}
	authority, err := r.mcpAuthority(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err = authority.RequireMCPDiscoveryInTx(ctx, tx, d); err != nil {
		return nil, portError(err)
	}
	refs := make([]tc.SpecRef, 0, len(d.Tools))
	for n, v := range d.Tools {
		ref, err := ensureMCPDefinition(ctx, x, v.Definition.StableKey, definitions[n])
		if err != nil {
			return nil, err
		}
		b := v.Binding
		_, err = x.Exec(ctx, `INSERT INTO agenteam_tool.mcp_registrations(project_id,tool_id,spec_revision,config_id,connection_id,runtime_binding_id,config_revision,remote_tool_name,default_enabled,active) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(project_id,tool_id) DO UPDATE SET spec_revision=EXCLUDED.spec_revision,config_id=EXCLUDED.config_id,connection_id=EXCLUDED.connection_id,runtime_binding_id=EXCLUDED.runtime_binding_id,config_revision=EXCLUDED.config_revision,remote_tool_name=EXCLUDED.remote_tool_name,default_enabled=EXCLUDED.default_enabled,active=EXCLUDED.active`, d.ProjectID.String(), ref.ToolID.String(), int64(ref.SpecRevision), b.ConfigID, b.ConnectionID, b.RuntimeBindingID, int64(b.ConfigRevision), b.RemoteToolName, v.DefaultEnabled, v.Active)
		if err != nil {
			return nil, portError(err)
		}
		refs = append(refs, ref)
	}
	names := slices.Clone(d.PresentNames)
	if names == nil {
		names = []string{}
	}
	if _, err = x.Exec(ctx, `DELETE FROM agenteam_tool.mcp_registrations WHERE project_id=$1 AND connection_id=$2 AND NOT(remote_tool_name=ANY($3::text[]))`, d.ProjectID.String(), d.ConnectionID, names); err != nil {
		return nil, portError(err)
	}
	return refs, captureContext(ctx)
}
func ensureMCPDefinition(ctx context.Context, x postgres.SQLExecutor, key string, definition []byte) (tc.SpecRef, error) {
	old, err := loadIdentity(ctx, x, key)
	if err != nil {
		return tc.SpecRef{}, err
	}
	var tool id.ToolID
	revision := f.Version(1)
	if old == nil {
		tool, err = f.NewID[id.Tool]()
		if err != nil {
			return tc.SpecRef{}, portError(err)
		}
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_tool.identities(tool_id,stable_key,latest_revision) VALUES($1,$2,1)`, tool.String(), key); err != nil {
			return tc.SpecRef{}, portError(err)
		}
	} else {
		tool, revision = old.ID, old.Revision
		if bytes.Equal(old.Definition, definition) {
			return tc.SpecRef{ToolID: tool, SpecRevision: revision}, nil
		}
		var existing int64
		err = x.QueryRow(ctx, `SELECT spec_revision FROM agenteam_tool.spec_revisions WHERE tool_id=$1 AND definition=$2 ORDER BY spec_revision DESC LIMIT 1`, tool.String(), definition).Scan(&existing)
		if err == nil {
			if f.Version(existing).Validate() != nil {
				return tc.SpecRef{}, fail(f.InvalidState)
			}
			return tc.SpecRef{ToolID: tool, SpecRevision: f.Version(existing)}, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return tc.SpecRef{}, portError(err)
		}
		if revision == f.Version(math.MaxInt64) {
			return tc.SpecRef{}, fail(f.InvalidState)
		}
		revision++
	}
	if _, err = x.Exec(ctx, `INSERT INTO agenteam_tool.spec_revisions(tool_id,spec_revision,definition) VALUES($1,$2,$3)`, tool.String(), int64(revision), definition); err != nil {
		return tc.SpecRef{}, portError(err)
	}
	if old != nil {
		if _, err = x.Exec(ctx, `UPDATE agenteam_tool.identities SET latest_revision=$2 WHERE tool_id=$1`, tool.String(), int64(revision)); err != nil {
			return tc.SpecRef{}, portError(err)
		}
	}
	return tc.SpecRef{ToolID: tool, SpecRevision: revision}, nil
}
func (r *Registry) RemoveMCPConnectionInTx(ctx context.Context, tx f.Tx, project id.ProjectID, connection string) error {
	if project.Validate() != nil {
		return fail(f.InvalidArgument)
	}
	if _, err := f.ParseID[struct{}](connection); err != nil {
		return fail(f.InvalidArgument)
	}
	x, err := r.executor(ctx, tx, []f.LockRequest{RegistryLock(f.Exclusive)})
	if err != nil {
		return err
	}
	authority, err := r.mcpAuthority(ctx, tx)
	if err != nil {
		return err
	}
	if err = authority.RequireMCPRemovalInTx(ctx, tx, project, connection); err != nil {
		return portError(err)
	}
	_, err = x.Exec(ctx, `DELETE FROM agenteam_tool.mcp_registrations WHERE project_id=$1 AND connection_id=$2`, project.String(), connection)
	if err != nil {
		return portError(err)
	}
	return captureContext(ctx)
}
func (r *Registry) currentMCP(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, project id.ProjectID, tool id.ToolID) (currentTool, bool, error) {
	var v currentTool
	var revision, configRevision int64
	var raw []byte
	err := x.QueryRow(ctx, `SELECT i.stable_key,r.spec_revision,s.definition,r.config_id,r.connection_id,r.runtime_binding_id,r.config_revision,r.remote_tool_name FROM agenteam_tool.mcp_registrations r JOIN agenteam_tool.identities i ON i.tool_id=r.tool_id JOIN agenteam_tool.spec_revisions s ON s.tool_id=r.tool_id AND s.spec_revision=r.spec_revision WHERE r.project_id=$1 AND r.tool_id=$2 AND r.active`, project.String(), tool.String()).Scan(&v.Key, &revision, &raw, &v.MCPBinding.ConfigID, &v.MCPBinding.ConnectionID, &v.MCPBinding.RuntimeBindingID, &configRevision, &v.MCPBinding.RemoteToolName)
	if errors.Is(err, pgx.ErrNoRows) {
		return currentTool{}, false, nil
	}
	if err != nil {
		return currentTool{}, false, portError(err)
	}
	v.Ref = tc.SpecRef{ToolID: tool, SpecRevision: f.Version(revision)}
	v.MCPBinding.ProjectID = project
	v.MCPBinding.ConfigRevision = f.Version(configRevision)
	v.Scope, v.Risk, v.Class = "mcp.project", "mcp.external", tc.OrdinaryTool
	if v.Ref.Validate() != nil || v.MCPBinding.Validate() != nil || v.Key != "mcp:"+v.MCPBinding.ConfigID+":"+v.MCPBinding.RemoteToolName {
		return currentTool{}, false, fail(f.InvalidState)
	}
	v.Definition, err = decodeDefinition(ctx, raw, v.Key)
	if err != nil {
		return currentTool{}, false, err
	}
	authority, err := r.mcpAuthority(ctx, tx)
	if err != nil {
		return currentTool{}, false, err
	}
	if err = authority.RequireCurrentMCPBindingInTx(ctx, tx, v.MCPBinding); err != nil {
		return currentTool{}, false, portError(err)
	}
	return v, true, captureContext(ctx)
}

func (r *Registry) currentProjectTool(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, project id.ProjectID, tool id.ToolID) (currentTool, error) {
	if s := r.data(); s != nil && !nilPort(s.mcp) {
		v, exists, err := r.currentMCP(ctx, tx, x, project, tool)
		if err != nil {
			return currentTool{}, err
		}
		if exists {
			return v, nil
		}
	}
	return r.currentTool(ctx, tx, x, tool)
}

type mcpCaptureKey struct{}
type mcpCaptureWitness struct {
	capture *executionCaptureState
	tx      f.Tx
	request tc.ExecutionToolCaptureRequest
	binding tc.MCPBinding
	live    atomic.Bool
}

func (c *ExecutionCapture) RequireMCPCaptureInTx(ctx context.Context, tx f.Tx, execution id.ExecutionID, binding tc.MCPBinding) error {
	if ctx == nil {
		return fail(f.InvalidArgument)
	}
	s := c.state()
	w, ok := ctx.Value(mcpCaptureKey{}).(*mcpCaptureWitness)
	if s == nil || !ok || w == nil || !w.live.Load() || w.capture != s || w.tx != tx || w.request.ExecutionID != execution || w.request.ProjectID != binding.ProjectID || w.binding != binding {
		return fail(f.Forbidden)
	}
	if _, err := s.registry.executor(ctx, tx, append(w.request.RequiredLocks(), RegistryLock(f.Shared))); err != nil {
		return err
	}
	facts, err := s.owner.RequireToolCaptureInTx(ctx, tx, w.request)
	if err != nil {
		return portError(err)
	}
	return facts.Validate(w.request)
}
func (c *ExecutionCapture) retainMCP(ctx context.Context, tx f.Tx, request tc.ExecutionToolCaptureRequest, binding tc.MCPBinding, plan tc.MCPRetentionPlan) error {
	s := c.state()
	authority, err := s.registry.mcpAuthority(ctx, tx)
	if err != nil {
		return err
	}
	witness := &mcpCaptureWitness{capture: s, tx: tx, request: request, binding: binding}
	witness.live.Store(true)
	defer func() { witness.live.Store(false) }()
	return portError(authority.RetainMCPRuntimeInTx(context.WithValue(ctx, mcpCaptureKey{}, witness), tx, request.ExecutionID, binding, plan))
}
func writeMCPExecutionReference(ctx context.Context, x postgres.SQLExecutor, r tc.ExecutionToolCaptureRequest, v tc.ExecutionTool) error {
	b := v.BindingSnapshot.MCPBinding
	if b.Validate() != nil || b.ProjectID != r.ProjectID {
		return fail(f.InvalidState)
	}
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_tool.execution_references(execution_id,tool_id,spec_revision,model_visible_name,binding_kind,scope_resolver_id,risk_classifier_id,class,mcp_project_id,mcp_config_id,mcp_connection_id,mcp_runtime_binding_id,mcp_config_revision,mcp_remote_tool_name) VALUES($1,$2,$3,$4,'mcp','mcp.project','mcp.external','ordinary',$5,$6,$7,$8,$9,$10)`, r.ExecutionID.String(), v.ToolID.String(), int64(v.SpecRevision), v.ModelVisibleName, b.ProjectID.String(), b.ConfigID, b.ConnectionID, b.RuntimeBindingID, int64(b.ConfigRevision), b.RemoteToolName)
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fail(f.InvalidState)
	}
	return captureContext(ctx)
}
func checkMCPExecutionReference(ctx context.Context, x postgres.SQLExecutor, r tc.ExecutionToolCaptureRequest, v tc.ExecutionTool) error {
	var revision, configRevision int64
	var name, project, config, connection, runtime, remote, kind, scope, risk, class string
	err := x.QueryRow(ctx, `SELECT spec_revision,model_visible_name,binding_kind,scope_resolver_id,risk_classifier_id,class,mcp_project_id,mcp_config_id,mcp_connection_id,mcp_runtime_binding_id,mcp_config_revision,mcp_remote_tool_name FROM agenteam_tool.execution_references WHERE execution_id=$1 AND tool_id=$2`, r.ExecutionID.String(), v.ToolID.String()).Scan(&revision, &name, &kind, &scope, &risk, &class, &project, &config, &connection, &runtime, &configRevision, &remote)
	if err != nil {
		return portError(err)
	}
	b := v.BindingSnapshot.MCPBinding
	if revision != int64(v.SpecRevision) || name != v.ModelVisibleName || kind != "mcp" || scope != "mcp.project" || risk != "mcp.external" || class != "ordinary" || project != b.ProjectID.String() || config != b.ConfigID || connection != b.ConnectionID || runtime != b.RuntimeBindingID || configRevision != int64(b.ConfigRevision) || remote != b.RemoteToolName {
		return fail(f.InvalidState)
	}
	return captureContext(ctx)
}

var _ tc.MCPCaptureAuthority = (*ExecutionCapture)(nil)

type mcpDiscoveryKey struct{}
type mcpDiscoveryWitness struct {
	capture  *executionCaptureState
	request  tc.ExecutionToolCaptureRequest
	bindings []tc.MCPBinding
	live     atomic.Bool
}

func (c *ExecutionCapture) RequireMCPRetentionDiscoveryInTx(ctx context.Context, tx f.Tx, request tc.ExecutionToolCaptureRequest, binding tc.MCPBinding) error {
	if ctx == nil {
		return fail(f.InvalidArgument)
	}
	s := c.state()
	w, ok := ctx.Value(mcpDiscoveryKey{}).(*mcpDiscoveryWitness)
	if s == nil || !ok || w == nil || !w.live.Load() || w.capture != s || w.request != request || !slices.Contains(w.bindings, binding) || binding.ProjectID != request.ProjectID {
		return fail(f.Forbidden)
	}
	if _, err := s.registry.executor(ctx, tx, append(request.RequiredLocks(), RegistryLock(f.Shared))); err != nil {
		return err
	}
	facts, err := s.owner.RequireToolDiscoveryInTx(ctx, tx, request)
	if err != nil {
		return portError(err)
	}
	return facts.Validate(request)
}
