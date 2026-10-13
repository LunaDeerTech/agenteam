package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// These are Execution-owned tables only. Tool-driven and later-round owners
// are not part of the four-owner factory, so their facts must remain a barrier.
var retirementUnsupportedTables = [...]string{
	"tool_call_arguments", "tool_calls", "tool_result_consumptions",
	"round_inputs", "skill_assignment_dispositions", "control_inputs",
	"round_model_requests", "round_model_claims",
}

// The Execution -> Snapshot edge is initially deferred. All other edges are
// removed child first; no state or receipt is rewritten to make deletion pass.
var retirementHistoryTables = [...]string{
	"transcript_entries", "rounds", "snapshots", "preparation_inputs",
	"preparation_claims", "preparation_attempts", "skill_assignment_ingress",
}

type executionRetirementBefore struct {
	record  *executionRecord
	raw     []byte
	history [len(retirementHistoryTables)][]byte
	counts  [len(retirementHistoryTables)]int64
}

func nextRetirementExecution(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID) (i.ExecutionID, error) {
	var raw string
	err := x.QueryRow(ctx, `SELECT id::text FROM agenteam_execution.executions WHERE project_id=$1::text ORDER BY id LIMIT 1`, project.String()).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return i.ExecutionID{}, nil
	}
	if err != nil {
		return i.ExecutionID{}, portError(err)
	}
	id, err := f.ParseID[i.Execution](raw)
	if err != nil {
		return i.ExecutionID{}, unavailable(nil)
	}
	return id, ctx.Err()
}

func readRetirementBefore(ctx context.Context, x postgres.SQLExecutor, id i.ExecutionID) (*executionRetirementBefore, error) {
	r, err := loadExecution(ctx, x, id)
	if err != nil || r == nil {
		return nil, err
	}
	b := &executionRetirementBefore{record: r}
	if err = x.QueryRow(ctx, `SELECT to_jsonb(e) FROM agenteam_execution.executions e WHERE id=$1::text`, id.String()).Scan(&b.raw); err != nil {
		return nil, portError(err)
	}
	// loadExecution validates the business shape, but its historical projection
	// omits these immutable/current fields. Freeze the complete row, not Summary.
	var full map[string]json.RawMessage
	if json.Unmarshal(b.raw, &full) != nil || len(full) != 17 {
		return nil, unavailable(nil)
	}
	for _, key := range []string{"id", "project_id", "agent_id", "status", "version", "idempotency_key", "request_id", "request_digest", "trigger_reference_digest", "launch_request", "initiator", "cancel_requested_at", "snapshot_id", "created_at", "updated_at", "started_at", "completed_at"} {
		if _, ok := full[key]; !ok {
			return nil, unavailable(nil)
		}
	}
	var trigger f.Digest
	var updated time.Time
	var origin i.ActorDetails
	if json.Unmarshal(full["trigger_reference_digest"], &trigger) != nil || trigger.Validate() != nil || json.Unmarshal(full["updated_at"], &updated) != nil || updated.Before(r.summary.CreatedAt.Time()) || json.Unmarshal(full["initiator"], &origin) != nil || origin.Kind != i.Human && origin.Kind != i.Service {
		return nil, unavailable(nil)
	}
	if err = requireNoUnsupportedRetirementHistory(ctx, x, id); err != nil {
		return nil, err
	}
	for n, table := range retirementHistoryTables {
		var raw []byte
		// table is selected only from the private, fixed list above.
		if err = x.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb) FROM agenteam_execution.`+table+` t WHERE execution_id=$1::text`, id.String()).Scan(&raw); err != nil {
			return nil, portError(err)
		}
		var rows []json.RawMessage
		if json.Unmarshal(raw, &rows) != nil || rows == nil {
			return nil, unavailable(nil)
		}
		b.history[n], b.counts[n] = bytes.Clone(raw), int64(len(rows))
		if table == "rounds" {
			var rounds []struct {
				Schema      int64 `json:"schema_version"`
				ToolPending bool  `json:"tool_pending"`
				Through     int64 `json:"transcript_through"`
			}
			if json.Unmarshal(raw, &rounds) != nil {
				return nil, unavailable(nil)
			}
			for _, round := range rounds {
				if round.Schema != 1 || round.ToolPending || round.Through < 1 || round.Through > 2 {
					return nil, fault(f.ResourceBusy)
				}
			}
		}
		if table == "preparation_inputs" && len(rows) > 0 {
			input, inputErr := loadPreparationInput(ctx, x, id)
			if inputErr != nil {
				return nil, inputErr
			}
			if input == nil || input.claim.project != r.summary.ProjectID || input.claim.agent != r.summary.AgentID {
				return nil, unavailable(nil)
			}
			digest, digestErr := input.input.Fields().Request.Launch.Digest()
			if digestErr != nil || digest != r.digest {
				return nil, unavailable(nil)
			}
		}
	}
	return b, ctx.Err()
}

func requireNoUnsupportedRetirementHistory(ctx context.Context, x postgres.SQLExecutor, id i.ExecutionID) error {
	for _, table := range retirementUnsupportedTables {
		var exists bool
		if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_execution.`+table+` WHERE execution_id=$1::text)`, id.String()).Scan(&exists); err != nil {
			return portError(err)
		}
		if exists {
			return fault(f.ResourceBusy)
		}
	}
	return ctx.Err()
}

func (b *executionRetirementBefore) binding() ec.ProjectExecutionRetirementBinding {
	s := b.record.summary
	return ec.ProjectExecutionRetirementBinding{ProjectID: s.ProjectID, AgentID: s.AgentID, ExecutionID: s.ID, Version: s.Version, CanonicalDigest: projectInvocationDigest(string(b.raw))}
}

func sameRetirementBefore(a, b *executionRetirementBefore) bool {
	if a == nil || b == nil {
		return a == b
	}
	if !bytes.Equal(a.raw, b.raw) || a.counts != b.counts {
		return false
	}
	for n := range a.history {
		if !bytes.Equal(a.history[n], b.history[n]) {
			return false
		}
	}
	return true
}

func deleteRetirementBefore(ctx context.Context, x postgres.SQLExecutor, b *executionRetirementBefore) error {
	id := b.record.summary.ID
	for n, table := range retirementHistoryTables {
		if b.counts[n] == 0 {
			continue
		}
		tag, err := x.Exec(ctx, `DELETE FROM agenteam_execution.`+table+` t USING jsonb_array_elements($2::jsonb) original WHERE t.execution_id=$1::text AND to_jsonb(t)=original.value`, id.String(), b.history[n])
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != b.counts[n] {
			return fault(f.VersionConflict)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
	}
	tag, err := x.Exec(ctx, `DELETE FROM agenteam_execution.executions e WHERE id=$1::text AND to_jsonb(e)=$2::jsonb`, id.String(), b.raw)
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.VersionConflict)
	}
	return requireRetirementAbsent(ctx, x, id)
}

func requireRetirementAbsent(ctx context.Context, x postgres.SQLExecutor, id i.ExecutionID) error {
	var exists bool
	if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE id=$1::text)`, id.String()).Scan(&exists); err != nil {
		return portError(err)
	}
	if exists {
		return fault(f.VersionConflict)
	}
	for _, table := range retirementHistoryTables {
		if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_execution.`+table+` WHERE execution_id=$1::text)`, id.String()).Scan(&exists); err != nil {
			return portError(err)
		}
		if exists {
			return fault(f.VersionConflict)
		}
	}
	return requireNoUnsupportedRetirementHistory(ctx, x, id)
}
