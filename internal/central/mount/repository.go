package mount

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/jackc/pgx/v5"
)

type referenceState struct {
	exists  bool
	version f.Version
	ids     []i.MountID
}

func loadReferences(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, agent i.AgentID) (referenceState, error) {
	var out referenceState
	var ownerProject string
	err := x.QueryRow(ctx, `SELECT project_id,owner_version FROM agenteam_mount.agent_mount_heads WHERE agent_id=$1`, agent.String()).Scan(&ownerProject, &out.version)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, portError(err)
	}
	if ownerProject != project.String() || out.version.Validate() != nil {
		return out, fault(f.InvalidState)
	}
	out.exists = true
	// Limit before aggregation bounds both driver allocation and corruption
	// detection. The single row owns and closes its original SQL reader.
	var raw []byte
	err = x.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(mount_id ORDER BY mount_id),'[]'::jsonb) FROM (SELECT mount_id FROM agenteam_mount.agent_mount_refs WHERE agent_id=$1 ORDER BY mount_id LIMIT 129) refs`, agent.String()).Scan(&raw)
	if err != nil {
		return out, portError(err)
	}
	var ids []string
	if len(raw) > 8192 || json.Unmarshal(raw, &ids) != nil || ids == nil || len(ids) > ac.MaxCapabilityReferences {
		return out, fault(f.InvalidState)
	}
	out.ids = make([]i.MountID, 0, len(ids))
	for n, rawID := range ids {
		id, err := f.ParseID[i.Mount](rawID)
		if err != nil || n > 0 && ids[n-1] >= rawID {
			return out, fault(f.InvalidState)
		}
		out.ids = append(out.ids, id)
	}
	return out, portError(ctx.Err())
}
func checkBefore(r ac.MountConfigurationChange, s referenceState) error {
	if r.ExpectedOwnerVersion == nil {
		if s.exists {
			return fault(f.VersionConflict)
		}
	} else if !s.exists || s.version != *r.ExpectedOwnerVersion || !slices.Equal(s.ids, r.Before) {
		return fault(f.VersionConflict)
	}
	return nil
}
func loadDefinition(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, agent i.AgentID, mount i.MountID) (definition, error) {
	var out definition
	var id, p, a, runner string
	err := x.QueryRow(ctx, `SELECT id,project_id,agent_id,runner_id,name,description,workspace,lifecycle,version FROM agenteam_mount.mounts WHERE id=$1`, mount.String()).Scan(&id, &p, &a, &runner, &out.Name, &out.Description, &out.Workspace, &out.Lifecycle, &out.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, fault(f.NotFound)
	}
	if err != nil {
		return out, portError(err)
	}
	out.ID, err = f.ParseID[i.Mount](id)
	if err != nil {
		return out, fault(f.InvalidState)
	}
	out.Project, err = f.ParseID[i.Project](p)
	if err != nil {
		return out, fault(f.InvalidState)
	}
	out.Agent, err = f.ParseID[i.Agent](a)
	if err != nil {
		return out, fault(f.InvalidState)
	}
	out.Runner, err = f.ParseID[rc.Runner](runner)
	if err != nil || !out.valid() || out.ID != mount {
		return out, fault(f.InvalidState)
	}
	if out.Project != project || out.Agent != agent || out.Lifecycle != "active" {
		return out, fault(f.NotFound)
	}
	return out, portError(ctx.Err())
}
func writeReferences(ctx context.Context, x postgres.SQLExecutor, r ac.MountConfigurationChange, old referenceState) error {
	if old.exists && old.version == r.ResultOwnerVersion {
		return portError(ctx.Err())
	}
	if !old.exists {
		tag, err := x.Exec(ctx, `INSERT INTO agenteam_mount.agent_mount_heads(agent_id,project_id,owner_version) VALUES($1,$2,$3)`, r.AgentID.String(), r.ProjectID.String(), int64(r.ResultOwnerVersion))
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.InvalidState)
		}
	} else {
		tag, err := x.Exec(ctx, `UPDATE agenteam_mount.agent_mount_heads SET owner_version=$3 WHERE agent_id=$1 AND project_id=$2 AND owner_version=$4`, r.AgentID.String(), r.ProjectID.String(), int64(r.ResultOwnerVersion), int64(old.version))
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.VersionConflict)
		}
	}
	tag, err := x.Exec(ctx, `DELETE FROM agenteam_mount.agent_mount_refs WHERE agent_id=$1`, r.AgentID.String())
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != int64(len(old.ids)) {
		return fault(f.InvalidState)
	}
	for _, id := range r.After {
		tag, err = x.Exec(ctx, `INSERT INTO agenteam_mount.agent_mount_refs(project_id,agent_id,mount_id) VALUES($1,$2,$3)`, r.ProjectID.String(), r.AgentID.String(), id.String())
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.InvalidState)
		}
	}
	return portError(ctx.Err())
}
