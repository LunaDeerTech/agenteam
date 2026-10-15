package mcp

import (
	"context"
	"encoding/json"
	"math"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// Each adapter publishes a complete validated diff. A failed refresh keeps its
// previous snapshot and revision; an error is never treated as an empty list.
func (s *Service) finishStartup(ctx context.Context, w *startupUse, result synchronization) error {
	cause, _ := f.NewRecoveryCause("mcp.startup.publish", w.request, "")
	return commit(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, w.locks); err != nil {
			return err
		}
		_, fact, err := s.requireStartupInTx(ctx, tx, true)
		if err != nil {
			return err
		}
		if fact.retired {
			return fail(f.Forbidden)
		}
		if err = s.requireStartupProject(ctx, tx, w); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		c := w.connection
		if c.ToolsRevision == math.MaxInt64 || c.ResourcesRevision == math.MaxInt64 {
			return fail(f.InvalidState)
		}
		validTools, invalidTools := 0, 0
		code := result.connectError
		if code != "" {
			_, err = x.Exec(ctx, `UPDATE agenteam_mcp.connections SET refresh_id=NULL,last_error_code=$2,updated_at=clock_timestamp() WHERE id=$1`, c.ID.String(), code)
		} else {
			binding := c.RuntimeBindingID
			if binding == "" {
				fresh, err := f.NewID[struct{}]()
				if err != nil {
					return fail(f.DependencyUnavailable)
				}
				binding = fresh.String()
				_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.runtime_bindings(id,project_id,config_id,connection_id,config_revision,endpoint,auth_scheme,header_name,credential_id,protocol_revision) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, binding, c.ProjectID.String(), c.ConfigID.String(), c.ID.String(), int64(w.config.RuntimeRevision), w.config.Endpoint, w.config.Authentication.Scheme, w.config.Authentication.HeaderName, null(c.CredentialID), result.revision)
				if err != nil {
					return err
				}
			}
			if result.toolsError == "" {
				discovery, invalid, err := normalizeTools(ctx, tc.MCPBinding{ProjectID: c.ProjectID, ConfigID: c.ConfigID.String(), ConnectionID: c.ID.String(), RuntimeBindingID: binding, ConfigRevision: c.ConfigRevision}, result.tools, c.Enabled)
				if err != nil {
					return err
				}
				validTools, invalidTools = len(discovery.Tools), invalid
				if _, err = s.publishTools(ctx, tx, discovery); err != nil {
					return err
				}
				raw, err := json.Marshal(result.tools)
				if err != nil {
					return fail(f.InvalidState)
				}
				_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.tool_snapshots(connection_id,revision,protocol_revision,tool_count,invalid_count,content_sha256) VALUES($1,$2,$3,$4,$5,$6)`, c.ID.String(), int64(c.ToolsRevision+1), result.revision, validTools, invalid, digest(raw))
				if err != nil {
					return err
				}
			}
			if result.resourcesError == "" {
				if err = s.publishResources(ctx, x, c, result.resources, result.templates); err != nil {
					return err
				}
			}
			code = result.toolsError
			if code == "" {
				code = result.resourcesError
			}
			_, err = x.Exec(ctx, `UPDATE agenteam_mcp.connections SET state='connected',runtime_binding_id=$2,protocol_revision=$3,refresh_id=NULL,last_error_code=$4,last_connected_at=clock_timestamp(),updated_at=clock_timestamp(),tools_state=CASE WHEN $5='' THEN 'ready' ELSE 'error' END,resources_state=CASE WHEN $6='' THEN 'ready' ELSE 'error' END,tools_revision=tools_revision+CASE WHEN $5='' THEN 1 ELSE 0 END,resources_revision=resources_revision+CASE WHEN $6='' THEN 1 ELSE 0 END WHERE id=$1`, c.ID.String(), binding, result.revision, code, result.toolsError, result.resourcesError)
		}
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.startup_requests SET phase='complete',io_retired=true,retirement_process_id=$4,retired_at=clock_timestamp(),result_version=$2,error_code=$3,completed_at=clock_timestamp() WHERE id=$1`, w.request, int64(c.Version), code, s.owner.process.String())
		if err != nil {
			return err
		}
		auditCode := ""
		if code != "" {
			auditCode = auditFailure(code)
		}
		scope, _ := id.InProject(c.ProjectID)
		return s.emit(ctx, tx, AuditEvent{Actor: w.actor, Scope: scope, Action: string(ac.MCPDiscoveryRefresh), ConfigID: c.ConfigID.String(), ConnectionID: c.ID.String(), Version: c.Version, ToolCount: validTools, ResourceCount: len(result.resources) + len(result.templates), InvalidCount: invalidTools, ErrorCode: auditCode, CauseID: w.request})
	}))
}
