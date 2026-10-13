package mount

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

const projectMountRows = 1024
const projectMountBytes = 8 << 20

// Each batch is one complete Agent index. Historical definitions are included,
// including those no longer in the current allowlist. No system Runner row or
// physical directory is part of this representation.
type projectMountSnapshot struct {
	Head        json.RawMessage   `json:"head"`
	References  []json.RawMessage `json:"references"`
	Workspaces  []json.RawMessage `json:"workspaces"`
	Definitions []json.RawMessage `json:"definitions"`
}
type projectMountBatch struct {
	agent    i.AgentID
	before   ac.AgentConfig
	snapshot projectMountSnapshot
	raw      []byte
	proof    *projectAgentRetirementProof
}

func canonicalProjectJSON(raw []byte) ([]byte, error) {
	var value any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&value); err != nil {
		return nil, fault(f.InvalidState)
	}
	var out bytes.Buffer
	e := json.NewEncoder(&out)
	e.SetEscapeHTML(false)
	if err := e.Encode(value); err != nil {
		return nil, portError(err)
	}
	return bytes.TrimSpace(out.Bytes()), nil
}
func projectJSONDigest(raw []byte) f.Digest {
	return f.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(raw)))
}

func nextProjectMountAgent(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID) (i.AgentID, error) {
	var raw string
	err := x.QueryRow(ctx, `SELECT h.agent_id::text FROM agenteam_mount.agent_mount_heads h WHERE h.project_id=$1::uuid AND NOT EXISTS(SELECT 1 FROM agenteam_mount.project_agent_retirements r WHERE r.project_id=h.project_id AND r.agent_id=h.agent_id) ORDER BY h.agent_id LIMIT 1`, p.String()).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return i.AgentID{}, nil
	}
	if err != nil {
		return i.AgentID{}, portError(err)
	}
	a, err := f.ParseID[i.Agent](raw)
	if err != nil {
		return i.AgentID{}, fault(f.InvalidState)
	}
	return a, nil
}

func readProjectMountSnapshot(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID, a i.AgentID) (projectMountSnapshot, []byte, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT jsonb_build_object('head',(SELECT to_jsonb(h) FROM agenteam_mount.agent_mount_heads h WHERE h.project_id=$1::uuid AND h.agent_id=$2::uuid),'references',COALESCE((SELECT jsonb_agg(to_jsonb(t) ORDER BY t.mount_id) FROM (SELECT * FROM agenteam_mount.agent_mount_refs WHERE project_id=$1::uuid AND agent_id=$2::uuid ORDER BY mount_id LIMIT 1025) t),'[]'::jsonb),'workspaces',COALESCE((SELECT jsonb_agg(to_jsonb(t) ORDER BY t.workspace_id) FROM (SELECT * FROM agenteam_mount.runner_workspaces WHERE project_id=$1::uuid AND agent_id=$2::uuid ORDER BY workspace_id LIMIT 1025) t),'[]'::jsonb),'definitions',COALESCE((SELECT jsonb_agg(to_jsonb(t) ORDER BY t.id) FROM (SELECT * FROM agenteam_mount.mounts WHERE project_id=$1::uuid AND agent_id=$2::uuid ORDER BY id LIMIT 1025) t),'[]'::jsonb))`, p.String(), a.String()).Scan(&raw)
	if err != nil {
		return projectMountSnapshot{}, nil, portError(err)
	}
	if len(raw) > projectMountBytes {
		return projectMountSnapshot{}, nil, fault(f.ResourceBusy)
	}
	var s projectMountSnapshot
	if json.Unmarshal(raw, &s) != nil || s.References == nil || s.Workspaces == nil || s.Definitions == nil {
		return s, nil, fault(f.InvalidState)
	}
	if len(s.References) > ac.MaxCapabilityReferences || len(s.Workspaces) > projectMountRows || len(s.Definitions) > projectMountRows {
		return s, nil, fault(f.ResourceBusy)
	}
	raw, err = canonicalProjectJSON(raw)
	if err != nil {
		return s, nil, err
	}
	if len(raw) > projectMountBytes {
		return s, nil, fault(f.ResourceBusy)
	}
	return s, raw, ctx.Err()
}

func validateProjectMountSnapshot(s projectMountSnapshot, before ac.AgentConfig) error {
	if before.Validate() != nil || before.Fields().Core.Lifecycle != ac.AgentActive || s.References == nil || s.Workspaces == nil || s.Definitions == nil || len(s.References) > ac.MaxCapabilityReferences || len(s.Workspaces) > projectMountRows || len(s.Definitions) > projectMountRows {
		return fault(f.InvalidState)
	}
	c := before.Fields()
	p, a := c.Core.ProjectID.String(), c.Core.ID.String()
	var head struct {
		Agent   string `json:"agent_id"`
		Project string `json:"project_id"`
		Version int64  `json:"owner_version"`
	}
	if json.Unmarshal(s.Head, &head) != nil || head.Agent != a || head.Project != p || head.Version != int64(c.Core.Version) {
		return fault(f.ConfirmationStale)
	}
	ids := make([]i.MountID, 0, len(s.References))
	for _, raw := range s.References {
		var r struct {
			Project string `json:"project_id"`
			Agent   string `json:"agent_id"`
			Mount   string `json:"mount_id"`
		}
		if json.Unmarshal(raw, &r) != nil || r.Project != p || r.Agent != a {
			return fault(f.InvalidState)
		}
		id, err := f.ParseID[i.Mount](r.Mount)
		if err != nil || len(ids) > 0 && ids[len(ids)-1].String() >= id.String() {
			return fault(f.InvalidState)
		}
		ids = append(ids, id)
	}
	if !slices.Equal(ids, c.AllowedMountIDs) {
		return fault(f.ConfirmationStale)
	}
	definitions := map[string]map[string]json.RawMessage{}
	previous := ""
	for _, raw := range s.Definitions {
		var row map[string]json.RawMessage
		if json.Unmarshal(raw, &row) != nil {
			return fault(f.InvalidState)
		}
		var id, project, agent string
		if json.Unmarshal(row["id"], &id) != nil || json.Unmarshal(row["project_id"], &project) != nil || json.Unmarshal(row["agent_id"], &agent) != nil || project != p || agent != a || id <= previous {
			return fault(f.InvalidState)
		}
		if _, err := f.ParseID[i.Mount](id); err != nil {
			return fault(f.InvalidState)
		}
		previous = id
		definitions[id] = row
	}
	for _, id := range ids {
		if definitions[id.String()] == nil {
			return fault(f.InvalidState)
		}
	}
	previous = ""
	for _, raw := range s.Workspaces {
		var row map[string]json.RawMessage
		if json.Unmarshal(raw, &row) != nil {
			return fault(f.InvalidState)
		}
		var id, mount, project, agent string
		if json.Unmarshal(row["workspace_id"], &id) != nil || json.Unmarshal(row["mount_id"], &mount) != nil || json.Unmarshal(row["project_id"], &project) != nil || json.Unmarshal(row["agent_id"], &agent) != nil || project != p || agent != a || id <= previous {
			return fault(f.InvalidState)
		}
		if _, err := f.ParseID[struct{}](id); err != nil {
			return fault(f.InvalidState)
		}
		previous = id
		d := definitions[mount]
		if d == nil || !bytes.Equal(row["runner_id"], d["runner_id"]) || !bytes.Equal(row["workspace"], d["workspace"]) {
			return fault(f.InvalidState)
		}
	}
	return nil
}

func deleteProjectMountSnapshot(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID, a i.AgentID, s projectMountSnapshot) error {
	for _, part := range []struct {
		table string
		rows  []json.RawMessage
	}{{"agent_mount_refs", s.References}, {"runner_workspaces", s.Workspaces}, {"mounts", s.Definitions}} {
		if len(part.rows) == 0 {
			continue
		}
		raw, err := json.Marshal(part.rows)
		if err != nil {
			return portError(err)
		}
		raw, err = canonicalProjectJSON(raw)
		if err != nil {
			return err
		}
		if len(raw) > projectMountBytes {
			return fault(f.ResourceBusy)
		}
		tag, err := x.Exec(ctx, `DELETE FROM agenteam_mount.`+part.table+` t USING jsonb_array_elements($3::jsonb) b WHERE t.project_id=$1::uuid AND t.agent_id=$2::uuid AND to_jsonb(t)=b.value`, p.String(), a.String(), raw)
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != int64(len(part.rows)) {
			return fault(f.ConfirmationStale)
		}
	}
	return ctx.Err()
}

func noProjectExecutionMounts(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID) error {
	unsupported, err := projectRunnerOperationsPresent(ctx, x, p)
	if err != nil {
		return err
	}
	if unsupported {
		return fault(f.ResourceBusy)
	}
	var busy bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mount.execution_captures WHERE project_id=$1::text) OR EXISTS(SELECT 1 FROM agenteam_mount.execution_uses WHERE project_id=$1::text) OR EXISTS(SELECT 1 FROM agenteam_mount.execution_references r JOIN agenteam_mount.mounts m ON m.id=r.mount_id WHERE m.project_id=$1::uuid)`, p.String()).Scan(&busy)
	if err != nil {
		return portError(err)
	}
	if busy {
		return fault(f.ResourceBusy)
	}
	return ctx.Err()
}

// The bound graph contains Agent configuration and four Execution owners.
// Persisted Tool/Runner operations have a different original owner, including
// terminal rows whose actual callback return has not been bound to this graph.
func projectRunnerOperationsPresent(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID) (bool, error) {
	var present bool
	err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_tool.operations WHERE project_id=$1::text) OR EXISTS(SELECT 1 FROM agenteam_tool.attempts WHERE project_id=$1::text)`, p.String()).Scan(&present)
	if err != nil {
		return false, portError(err)
	}
	return present, ctx.Err()
}
