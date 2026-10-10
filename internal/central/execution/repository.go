package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type executionRecord struct {
	summary c.Summary
	launch  c.LaunchRequest
	digest  f.Digest
}

const executionColumns = `id::text,project_id::text,agent_id::text,status,version,cancel_requested_at,snapshot_id::text,created_at,started_at,completed_at,launch_request,idempotency_key,request_id::text,request_digest`

func loadExecution(ctx context.Context, x postgres.SQLExecutor, execution i.ExecutionID) (*executionRecord, error) {
	return scanExecution(x.QueryRow(ctx, `SELECT `+executionColumns+` FROM agenteam_execution.executions WHERE id=$1`, execution.String()))
}
func loadLaunch(ctx context.Context, x postgres.SQLExecutor, key c.LaunchLookupKey) (*executionRecord, error) {
	return scanExecution(x.QueryRow(ctx, `SELECT `+executionColumns+` FROM agenteam_execution.executions WHERE project_id=$1 AND agent_id=$2 AND idempotency_key=$3`, key.ProjectID.String(), key.AgentID.String(), key.IdempotencyKey.String()))
}
func scanExecution(row postgres.Row) (*executionRecord, error) {
	var execution, project, agent, state, key, requestID, digest string
	var snapshot *string
	var version int64
	var created time.Time
	var cancelled, started, completed *time.Time
	var raw []byte
	err := row.Scan(&execution, &project, &agent, &state, &version, &cancelled, &snapshot, &created, &started, &completed, &raw, &key, &requestID, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	r := &executionRecord{}
	s := &r.summary
	if s.ID, err = f.ParseID[i.Execution](execution); err != nil {
		return nil, unavailable(nil)
	}
	if s.ProjectID, err = f.ParseID[i.Project](project); err != nil {
		return nil, unavailable(nil)
	}
	if s.AgentID, err = f.ParseID[i.Agent](agent); err != nil {
		return nil, unavailable(nil)
	}
	s.Status, s.Version = c.Status(state), f.Version(version)
	if !s.Status.Valid() || s.Version.Validate() != nil {
		return nil, unavailable(nil)
	}
	if s.CreatedAt, err = f.NewInstant(created); err != nil {
		return nil, unavailable(nil)
	}
	for _, pair := range []struct {
		raw    *time.Time
		target **f.Instant
	}{{cancelled, &s.CancelRequestedAt}, {started, &s.StartedAt}, {completed, &s.CompletedAt}} {
		if pair.raw == nil {
			continue
		}
		v, err := f.NewInstant(*pair.raw)
		if err != nil || v.Time().Before(s.CreatedAt.Time()) {
			return nil, unavailable(nil)
		}
		*pair.target = &v
	}
	if snapshot != nil {
		v, err := f.ParseID[c.Snapshot](*snapshot)
		if err != nil {
			return nil, unavailable(nil)
		}
		s.SnapshotID = &v
	}
	if s.Status.Terminal() != (s.CompletedAt != nil) || (s.Status == c.Running || s.Status == c.Waiting) && (s.SnapshotID == nil || s.StartedAt == nil) || (s.Status == c.Created || s.Status == c.Preparing) && (s.SnapshotID != nil || s.StartedAt != nil) {
		return nil, unavailable(nil)
	}
	if len(raw) > 262144 {
		return nil, unavailable(nil)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&r.launch); err != nil {
		return nil, unavailable(nil)
	}
	var extra any
	if !errors.Is(decoder.Decode(&extra), io.EOF) {
		return nil, unavailable(nil)
	}
	r.launch.Meta.IdempotencyKey = f.IdempotencyKey(key)
	if r.launch.Meta.RequestID, err = f.ParseID[f.Request](requestID); err != nil {
		return nil, unavailable(nil)
	}
	if r.launch.Validate() != nil || r.launch.ProjectID != s.ProjectID || r.launch.AgentID != s.AgentID {
		return nil, unavailable(nil)
	}
	r.digest = f.Digest(digest)
	actual, err := r.launch.Digest()
	if err != nil || actual != r.digest {
		return nil, unavailable(nil)
	}
	s.Trigger, s.Purpose = r.launch.Trigger, r.launch.Purpose
	return r, nil
}
func occupied(ctx context.Context, x postgres.SQLExecutor, agent i.AgentID) (bool, error) {
	var exists bool
	err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE agent_id=$1 AND status IN ('created','preparing','running','waiting'))`, agent.String()).Scan(&exists)
	if err != nil {
		return false, unavailable(err)
	}
	return exists, nil
}
func insertCreated(ctx context.Context, x postgres.SQLExecutor, execution i.ExecutionID, actor i.Actor, request c.LaunchRequest, permit c.LaunchPermit) (*executionRecord, error) {
	raw, err := json.Marshal(request)
	if err != nil || len(raw) > 262144 {
		return nil, invalid()
	}
	origin, err := json.Marshal(actor.Details())
	if err != nil {
		return nil, invalid()
	}
	digest, err := request.Digest()
	if err != nil {
		return nil, err
	}
	tag, err := x.Exec(ctx, `WITH instant AS MATERIALIZED (SELECT clock_timestamp() AS at) INSERT INTO agenteam_execution.executions(id,project_id,agent_id,status,version,launch_request,idempotency_key,request_id,request_digest,initiator,trigger_reference_digest,created_at,updated_at) SELECT $1,$2,$3,'created',1,$4,$5,$6,$7,$8,$9,at,at FROM instant`, execution.String(), request.ProjectID.String(), request.AgentID.String(), raw, request.Meta.IdempotencyKey.String(), request.Meta.RequestID.String(), string(digest), origin, string(permit.ReferenceDigest))
	if err != nil {
		return nil, unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return nil, unavailable(nil)
	}
	r, err := loadExecution(ctx, x, execution)
	if err != nil {
		return nil, err
	}
	if r == nil || r.digest != digest || r.summary.Status != c.Created || r.summary.Version != 1 {
		return nil, unavailable(nil)
	}
	return r, nil
}
