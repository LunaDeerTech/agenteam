package mount

import (
	"context"
	"errors"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

func verifyExecutionUseMaterial(ctx context.Context, x postgres.SQLExecutor, use mc.ExecutionMountUse) (f.Digest, error) {
	raw, digest, err := executionMountMaterial(use.Mounts)
	if err != nil {
		return "", err
	}
	found, err := matchExecutionMountMaterial(ctx, x, use.Mounts, raw, digest)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fault(f.DependencyUnavailable)
	}
	return digest, nil
}

// Comparing every stored identity also rejects malformed rows. This does not
// inspect mutable Agent, Mount, Runner or Execution configuration.
func matchExecutionMountUse(ctx context.Context, x postgres.SQLExecutor, use mc.ExecutionMountUse, capture f.Digest) (bool, error) {
	var project, agent, attempt, storedCapture, input, snapshot string
	var version int64
	err := x.QueryRow(ctx, `SELECT project_id,agent_id,agent_version,attempt_binding,capture_digest,input_digest,snapshot_digest FROM agenteam_mount.execution_uses WHERE execution_id=$1`, use.Mounts.Request.ExecutionID.String()).Scan(&project, &agent, &version, &attempt, &storedCapture, &input, &snapshot)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, portError(err)
	}
	if project != use.Mounts.Request.ProjectID.String() || agent != use.Mounts.Request.AgentID.String() || version != int64(use.Mounts.AgentVersion) || attempt != string(use.Mounts.AttemptBinding) || storedCapture != string(capture) || input != string(use.InputDigest) || snapshot != string(use.SnapshotDigest) {
		return true, fault(f.InvalidState)
	}
	return true, ctx.Err()
}

func readExecutionMountRetirement(ctx context.Context, x postgres.SQLExecutor, execution i.ExecutionID) (*mc.ExecutionMountRetirement, error) {
	var version int64
	var id string
	err := x.QueryRow(ctx, `SELECT terminal_version,terminal_event_id::text FROM agenteam_mount.execution_use_retirements WHERE execution_id=$1`, execution.String()).Scan(&version, &id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, portError(err)
	}
	eventID, err := f.ParseID[event.EventIdentity](id)
	retired := mc.ExecutionMountRetirement{Version: f.Version(version), EventID: eventID}
	if err != nil || retired.Validate() != nil {
		return nil, fault(f.InvalidState)
	}
	return &retired, ctx.Err()
}

func acquireExecutionMountUse(ctx context.Context, x postgres.SQLExecutor, use mc.ExecutionMountUse, capture f.Digest) error {
	found, err := matchExecutionMountUse(ctx, x, use, capture)
	if err != nil {
		return err
	}
	retired, err := readExecutionMountRetirement(ctx, x, use.Mounts.Request.ExecutionID)
	if err != nil {
		return err
	}
	if retired != nil {
		return fault(f.InvalidState)
	}
	if found {
		return ctx.Err()
	}
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_mount.execution_uses(execution_id,project_id,agent_id,agent_version,attempt_binding,capture_digest,input_digest,snapshot_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, use.Mounts.Request.ExecutionID.String(), use.Mounts.Request.ProjectID.String(), use.Mounts.Request.AgentID.String(), int64(use.Mounts.AgentVersion), string(use.Mounts.AttemptBinding), string(capture), string(use.InputDigest), string(use.SnapshotDigest))
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.InvalidState)
	}
	return ctx.Err()
}

func verifyExecutionMountUse(ctx context.Context, x postgres.SQLExecutor, use mc.ExecutionMountUse, capture f.Digest, expected *mc.ExecutionMountRetirement) error {
	found, err := matchExecutionMountUse(ctx, x, use, capture)
	if err != nil {
		return err
	}
	if !found {
		return fault(f.DependencyUnavailable)
	}
	actual, err := readExecutionMountRetirement(ctx, x, use.Mounts.Request.ExecutionID)
	if err != nil {
		return err
	}
	if (actual == nil) != (expected == nil) || actual != nil && *actual != *expected {
		return fault(f.InvalidState)
	}
	return ctx.Err()
}

func retireExecutionMountUse(ctx context.Context, x postgres.SQLExecutor, use mc.ExecutionMountUse, capture f.Digest, expected mc.ExecutionMountRetirement) error {
	found, err := matchExecutionMountUse(ctx, x, use, capture)
	if err != nil {
		return err
	}
	if !found {
		return fault(f.DependencyUnavailable)
	}
	actual, err := readExecutionMountRetirement(ctx, x, use.Mounts.Request.ExecutionID)
	if err != nil {
		return err
	}
	if actual != nil {
		if *actual != expected {
			return fault(f.InvalidState)
		}
		return ctx.Err()
	}
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_mount.execution_use_retirements(execution_id,terminal_version,terminal_event_id) VALUES($1,$2,$3)`, use.Mounts.Request.ExecutionID.String(), int64(expected.Version), expected.EventID.String())
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.InvalidState)
	}
	return ctx.Err()
}
