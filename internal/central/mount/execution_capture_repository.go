package mount

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/jackc/pgx/v5"
)

// The limit includes JSON escaping of 128 complete Mount and Runner metadata
// entries. It is independent of the caller's complete preparation input limit.
const maxExecutionMountMaterialBytes = 8 << 20

type executionMountSource struct {
	head   referenceState
	mounts []mc.ExecutionMountMetadata
}

func cloneExecutionMounts(v []mc.ExecutionMountMetadata) []mc.ExecutionMountMetadata {
	return (mc.ExecutionMountSet{Mounts: v}).Clone().Mounts
}
func sameExecutionMountSource(a, b executionMountSource) bool {
	if a.head.exists != b.head.exists || a.head.version != b.head.version || !slices.Equal(a.head.ids, b.head.ids) {
		return false
	}
	return slices.EqualFunc(a.mounts, b.mounts, func(a, b mc.ExecutionMountMetadata) bool {
		if (a.Description == nil) != (b.Description == nil) {
			return false
		}
		if a.Description != nil && *a.Description != *b.Description {
			return false
		}
		a.Description = nil
		b.Description = nil
		return a == b
	})
}
func readExecutionMountSource(ctx context.Context, x postgres.SQLExecutor, r mc.ExecutionMountCaptureRequest) (executionMountSource, error) {
	zero := executionMountSource{}
	head, err := loadReferences(ctx, x, r.ProjectID, r.AgentID)
	if err != nil {
		return zero, err
	}
	if !head.exists {
		return zero, fault(f.DependencyUnbound)
	}
	source := executionMountSource{head: head, mounts: make([]mc.ExecutionMountMetadata, 0, len(head.ids))}
	for _, id := range head.ids {
		d, err := loadDefinition(ctx, x, r.ProjectID, r.AgentID, id)
		if err != nil {
			return zero, err
		}
		unique, err := definitionForTuple(ctx, x, r.ProjectID, r.AgentID, d.Runner, d.Workspace)
		if err != nil {
			return zero, err
		}
		if unique == nil || unique.ID != d.ID {
			return zero, fault(f.InvalidState)
		}
		mapping, err := loadWorkspaceMapping(ctx, x, id)
		if err != nil {
			return zero, err
		}
		if !mappingMatches(mapping, d) {
			return zero, fault(f.DependencyUnbound)
		}
		source.mounts = append(source.mounts, mc.ExecutionMountMetadata{ID: id, WorkspaceID: mapping.binding.WorkspaceID, RunnerID: d.Runner, Name: d.Name, Description: d.Description, Workspace: d.Workspace, Version: d.Version})
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	source.mounts = cloneExecutionMounts(source.mounts)
	return source, nil
}
func executionMountMaterial(value mc.ExecutionMountSet) ([]byte, f.Digest, error) {
	if value.Validate() != nil || len(value.Mounts) == 0 {
		return nil, "", fault(f.InvalidArgument)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, "", fault(f.InvalidState)
	}
	raw, err = cursor.CanonicalJSON(raw)
	if err != nil {
		return nil, "", fault(f.InvalidState)
	}
	if len(raw) > maxExecutionMountMaterialBytes {
		return nil, "", fault(f.PayloadTooLarge)
	}
	sum := sha256.Sum256(raw)
	return raw, f.Digest("sha256:" + hex.EncodeToString(sum[:])), nil
}

type executionMountReference struct {
	MountID       i.MountID      `json:"mount_id"`
	WorkspaceID   mc.WorkspaceID `json:"workspace_id"`
	RunnerID      rc.RunnerID    `json:"runner_id"`
	MountVersion  f.Version      `json:"mount_version"`
	RunnerVersion f.Version      `json:"runner_version"`
}

func executionMountReferences(value mc.ExecutionMountSet) []executionMountReference {
	refs := make([]executionMountReference, len(value.Mounts))
	for n, m := range value.Mounts {
		refs[n] = executionMountReference{m.ID, m.WorkspaceID, m.RunnerID, m.Version, m.RunnerVersion}
	}
	return refs
}
func readExecutionMountReferences(ctx context.Context, x postgres.SQLExecutor, execution i.ExecutionID) ([]executionMountReference, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('mount_id',mount_id,'workspace_id',workspace_id,'runner_id',runner_id,'mount_version',mount_version::text,'runner_version',runner_version::text) ORDER BY mount_id),'[]'::jsonb) FROM (SELECT mount_id,workspace_id,runner_id,mount_version,runner_version FROM agenteam_mount.execution_references WHERE execution_id=$1 ORDER BY mount_id LIMIT 129) refs`, execution.String()).Scan(&raw)
	if err != nil {
		return nil, portError(err)
	}
	if len(raw) > 65536 {
		return nil, fault(f.InvalidState)
	}
	var refs []executionMountReference
	if json.Unmarshal(raw, &refs) != nil || refs == nil || len(refs) > mc.MaxExecutionMounts {
		return nil, fault(f.InvalidState)
	}
	for n, r := range refs {
		if r.MountID.Validate() != nil || r.WorkspaceID.Validate() != nil || r.RunnerID.Validate() != nil || r.MountVersion.Validate() != nil || r.RunnerVersion.Validate() != nil || n > 0 && refs[n-1].MountID.String() >= r.MountID.String() {
			return nil, fault(f.InvalidState)
		}
	}
	return refs, ctx.Err()
}

// A missing row is distinct from a corrupt row. Callers choose whether a
// missing original material may be inserted (capture) or must fail (recovery).
func matchExecutionMountMaterial(ctx context.Context, x postgres.SQLExecutor, value mc.ExecutionMountSet, raw []byte, digest f.Digest) (bool, error) {
	var project, agent, attempt, storedDigest string
	var version int64
	var count int
	var stored []byte
	err := x.QueryRow(ctx, `SELECT project_id,agent_id,agent_version,attempt_binding,capture_digest,mount_count,material FROM agenteam_mount.execution_captures WHERE execution_id=$1`, value.Request.ExecutionID.String()).Scan(&project, &agent, &version, &attempt, &storedDigest, &count, &stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, portError(err)
	}
	if project != value.Request.ProjectID.String() || agent != value.Request.AgentID.String() || version != int64(value.AgentVersion) || attempt != string(value.AttemptBinding) || storedDigest != string(digest) || count != len(value.Mounts) || !bytes.Equal(stored, raw) {
		return true, fault(f.InvalidState)
	}
	refs, err := readExecutionMountReferences(ctx, x, value.Request.ExecutionID)
	if err != nil {
		return true, err
	}
	if !slices.Equal(refs, executionMountReferences(value)) {
		return true, fault(f.InvalidState)
	}
	return true, ctx.Err()
}
func verifyExecutionMountMaterial(ctx context.Context, x postgres.SQLExecutor, value mc.ExecutionMountSet) error {
	raw, digest, err := executionMountMaterial(value)
	if err != nil {
		return err
	}
	found, err := matchExecutionMountMaterial(ctx, x, value, raw, digest)
	if err != nil {
		return err
	}
	if !found {
		return fault(f.DependencyUnavailable)
	}
	return ctx.Err()
}
func persistExecutionMountMaterial(ctx context.Context, x postgres.SQLExecutor, value mc.ExecutionMountSet) error {
	raw, digest, err := executionMountMaterial(value)
	if err != nil {
		return err
	}
	found, err := matchExecutionMountMaterial(ctx, x, value, raw, digest)
	if err != nil || found {
		return err
	}
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_mount.execution_captures(execution_id,project_id,agent_id,agent_version,attempt_binding,capture_digest,mount_count,material) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, value.Request.ExecutionID.String(), value.Request.ProjectID.String(), value.Request.AgentID.String(), int64(value.AgentVersion), string(value.AttemptBinding), string(digest), len(value.Mounts), raw)
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.InvalidState)
	}
	for _, r := range executionMountReferences(value) {
		tag, err = x.Exec(ctx, `INSERT INTO agenteam_mount.execution_references(execution_id,mount_id,workspace_id,runner_id,mount_version,runner_version) VALUES($1,$2,$3,$4,$5,$6)`, value.Request.ExecutionID.String(), r.MountID.String(), r.WorkspaceID.String(), r.RunnerID.String(), int64(r.MountVersion), int64(r.RunnerVersion))
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.InvalidState)
		}
	}
	return ctx.Err()
}
