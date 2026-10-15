package registry

import (
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

const maxMCPDirectoryBytes = 8 << 20

// ListMCPTools reads the fixed registration revisions under the same current
// Owner/Session gate used by Agent configuration. Listing neither refreshes a
// connection nor authorizes use of the returned bindings.
func (r *Registry) ListMCPTools(ctx context.Context, actor id.Actor, project id.ProjectID, connection string) ([]tc.MCPRegistrationView, error) {
	if ctx == nil || actor.Validate() != nil {
		return nil, fail(f.Unauthenticated)
	}
	if actor.Details().Kind != id.Human {
		return nil, fail(f.Forbidden)
	}
	if project.Validate() != nil {
		return nil, fail(f.InvalidArgument)
	}
	if _, err := f.ParseID[struct{}](connection); err != nil {
		return nil, fail(f.InvalidArgument)
	}
	user, _ := f.UserLock(actor.Details().UserID)
	projectLock, _ := f.ProjectLock(project.String())
	locks, err := normalizedLocks([]f.LockRequest{{Key: user, Mode: f.Shared}, {Key: projectLock, Mode: f.Shared}, RegistryLock(f.Shared)})
	if err != nil {
		return nil, err
	}
	out := []tc.MCPRegistrationView{}
	err = r.read(ctx, locks, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if err := r.currentProject(ctx, tx, actor, project); err != nil {
			return err
		}
		rows, err := x.Query(ctx, `SELECT r.tool_id,r.spec_revision,s.definition,r.config_id,r.runtime_binding_id,r.config_revision,r.remote_tool_name,r.default_enabled,r.active FROM agenteam_tool.mcp_registrations r JOIN agenteam_tool.spec_revisions s ON s.tool_id=r.tool_id AND s.spec_revision=r.spec_revision WHERE r.project_id=$1 AND r.connection_id=$2 ORDER BY r.remote_tool_name,r.tool_id LIMIT 10001`, project.String(), connection)
		if err != nil {
			return portError(err)
		}
		defer rows.Close()
		bytes := 2
		for rows.Next() {
			if len(out) >= 10000 {
				return fail(f.PayloadTooLarge)
			}
			var v tc.MCPRegistrationView
			var tool string
			var raw []byte
			var revision, configRevision int64
			v.Binding.ProjectID, v.Binding.ConnectionID = project, connection
			if err = rows.Scan(&tool, &revision, &raw, &v.Binding.ConfigID, &v.Binding.RuntimeBindingID, &configRevision, &v.Binding.RemoteToolName, &v.DefaultEnabled, &v.Active); err != nil {
				return portError(err)
			}
			v.SpecRef.ToolID, err = f.ParseID[id.Tool](tool)
			v.SpecRef.SpecRevision, v.Binding.ConfigRevision = f.Version(revision), f.Version(configRevision)
			if err != nil || v.SpecRef.Validate() != nil || v.Binding.Validate() != nil {
				return fail(f.InvalidState)
			}
			canonical, err := decodeDefinition(ctx, raw, "mcp:"+v.Binding.ConfigID+":"+v.Binding.RemoteToolName)
			if err != nil {
				return err
			}
			if json.Unmarshal(canonical, &v.Definition) != nil || v.Definition.Name != v.Binding.RemoteToolName {
				return fail(f.InvalidState)
			}
			encoded, err := json.Marshal(v)
			if err != nil {
				return fail(f.InvalidState)
			}
			bytes += len(encoded) + 1
			if bytes > maxMCPDirectoryBytes {
				return fail(f.PayloadTooLarge)
			}
			out = append(out, v)
		}
		if err = rows.Err(); err != nil {
			return portError(err)
		}
		return captureContext(ctx)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

var _ tc.MCPToolDirectory = (*Registry)(nil)
