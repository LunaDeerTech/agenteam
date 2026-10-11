package mount

import (
	"context"
	"encoding/json"
	"errors"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/jackc/pgx/v5"
)

type workspaceMapping struct {
	binding ac.RunnerMountBinding
	project i.ProjectID
	agent   i.AgentID
}

// Bound before allocation/aggregation: more than one historical definition for
// a tuple is ambiguous, even if only one remains currently active.
func definitionForTuple(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, agent i.AgentID, runner rc.RunnerID, workspace string) (*definition, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(id ORDER BY id),'[]'::jsonb) FROM (SELECT id FROM agenteam_mount.mounts WHERE project_id=$1 AND agent_id=$2 AND runner_id=$3 AND workspace=$4 ORDER BY id LIMIT 2) matching`, project.String(), agent.String(), runner.String(), workspace).Scan(&raw)
	if err != nil {
		return nil, portError(err)
	}
	var ids []string
	if len(raw) > 128 || json.Unmarshal(raw, &ids) != nil || ids == nil || len(ids) > 1 {
		return nil, fault(f.InvalidState)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	id, err := f.ParseID[i.Mount](ids[0])
	if err != nil {
		return nil, fault(f.InvalidState)
	}
	d, err := loadDefinition(ctx, x, project, agent, id)
	if err != nil {
		return nil, err
	}
	if d.Runner != runner || d.Workspace != workspace {
		return nil, fault(f.InvalidState)
	}
	return &d, nil
}

func loadWorkspaceMapping(ctx context.Context, x postgres.SQLExecutor, mount i.MountID) (*workspaceMapping, error) {
	var workspaceID, mountID, projectID, agentID, runnerID, workspace string
	err := x.QueryRow(ctx, `SELECT workspace_id,mount_id,project_id,agent_id,runner_id,workspace FROM agenteam_mount.runner_workspaces WHERE mount_id=$1`, mount.String()).Scan(&workspaceID, &mountID, &projectID, &agentID, &runnerID, &workspace)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, portError(err)
	}
	wid, e1 := f.ParseID[mc.Workspace](workspaceID)
	mid, e2 := f.ParseID[i.Mount](mountID)
	p, e3 := f.ParseID[i.Project](projectID)
	a, e4 := f.ParseID[i.Agent](agentID)
	r, e5 := f.ParseID[rc.Runner](runnerID)
	b := ac.RunnerMountBinding{MountID: mid, WorkspaceID: wid, RunnerID: r, Workspace: workspace}
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || b.Validate() != nil || mid != mount {
		return nil, fault(f.InvalidState)
	}
	return &workspaceMapping{b, p, a}, portError(ctx.Err())
}

func mappingMatches(m *workspaceMapping, d definition) bool {
	return m != nil && m.project == d.Project && m.agent == d.Agent && m.binding.MountID == d.ID && m.binding.RunnerID == d.Runner && m.binding.Workspace == d.Workspace
}

type runnerBindingState struct {
	binding    ac.RunnerMountBinding
	definition *definition
	mapping    *workspaceMapping
}

func sameBindingState(a, b runnerBindingState) bool {
	if a.binding != b.binding || (a.definition == nil) != (b.definition == nil) || (a.mapping == nil) != (b.mapping == nil) {
		return false
	}
	return (a.definition == nil || sameDefinition(*a.definition, *b.definition)) && (a.mapping == nil || *a.mapping == *b.mapping)
}

// Discover and final Require both validate the complete frozen allocation.
// Newly allocated identities must still be absent; existing identities must
// still designate exactly the same active definition and physical mapping.
func loadBindingState(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID, a i.AgentID, b ac.RunnerMountBinding) (runnerBindingState, error) {
	zero := runnerBindingState{}
	if b.Validate() != nil {
		return zero, fault(f.InvalidArgument)
	}
	d, err := definitionForTuple(ctx, x, p, a, b.RunnerID, b.Workspace)
	if err != nil {
		return zero, err
	}
	if d == nil {
		var occupied bool
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mount.mounts WHERE id=$1)`, b.MountID.String()).Scan(&occupied)
		if err != nil {
			return zero, portError(err)
		}
		if occupied {
			return zero, fault(f.VersionConflict)
		}
	} else if d.ID != b.MountID {
		return zero, fault(f.VersionConflict)
	}
	m, err := loadWorkspaceMapping(ctx, x, b.MountID)
	if err != nil {
		return zero, err
	}
	if m != nil {
		if d == nil || !mappingMatches(m, *d) || m.binding != b {
			return zero, fault(f.VersionConflict)
		}
	} else {
		var occupied bool
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mount.runner_workspaces WHERE workspace_id=$1 OR (runner_id=$2 AND project_id=$3 AND agent_id=$4 AND workspace=$5))`, b.WorkspaceID.String(), b.RunnerID.String(), p.String(), a.String(), b.Workspace).Scan(&occupied)
		if err != nil {
			return zero, portError(err)
		}
		if occupied {
			return zero, fault(f.VersionConflict)
		}
	}
	return runnerBindingState{b, d, m}, portError(ctx.Err())
}

// Only called after the original Agent same-Tx canonical writer witness.
func insertBinding(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID, a i.AgentID, s runnerBindingState) error {
	b := s.binding
	if s.definition == nil {
		tag, err := x.Exec(ctx, `INSERT INTO agenteam_mount.mounts(id,project_id,agent_id,runner_id,name,description,workspace,lifecycle,version) VALUES($1::text::uuid,$2,$3,$4,$1::text,NULL,$5,'active',1)`, b.MountID.String(), p.String(), a.String(), b.RunnerID.String(), b.Workspace)
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.InvalidState)
		}
	}
	if s.mapping == nil {
		tag, err := x.Exec(ctx, `INSERT INTO agenteam_mount.runner_workspaces(workspace_id,mount_id,project_id,agent_id,runner_id,workspace) VALUES($1,$2,$3,$4,$5,$6)`, b.WorkspaceID.String(), b.MountID.String(), p.String(), a.String(), b.RunnerID.String(), b.Workspace)
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.InvalidState)
		}
	}
	return portError(ctx.Err())
}
