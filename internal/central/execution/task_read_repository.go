package execution

import (
	"context"
	"encoding/json"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// One statement observes the canonical row and terminal projection together.
// No snapshot, round input, transcript, response or private fault is loaded.
const taskExecutionSelect = `SELECT e.id::text,e.project_id::text,e.agent_id::text,e.status,e.version,e.cancel_requested_at,e.snapshot_id::text,e.created_at,e.started_at,e.completed_at,e.launch_request,e.idempotency_key,e.request_id::text,e.request_digest,r.terminal_reason,r.terminal_status,r.terminal_version,r.finished_at,e.waiting_reason,e.waiting_reference::text,e.waiting_at FROM agenteam_execution.executions e LEFT JOIN LATERAL (SELECT terminal_reason,terminal_status,terminal_version,finished_at FROM agenteam_execution.rounds WHERE execution_id=e.id ORDER BY round_ordinal DESC LIMIT 1) r ON true`

type taskExecutionPosition struct {
	at f.Instant
	id i.ExecutionID
}

func taskExecutionBinding(project i.ProjectID, task, user string) (cursor.Binding, error) {
	raw, err := json.Marshal(struct{ Kind, Project, Task, User string }{"execution.task-history", project.String(), task, user})
	if err != nil {
		return cursor.Binding{}, invalid()
	}
	digest, err := cursor.Digest(raw)
	if err != nil {
		return cursor.Binding{}, portError(err)
	}
	scope, err := i.InProject(project)
	if err != nil {
		return cursor.Binding{}, err
	}
	return cursor.Binding{Scope: scope, QueryDigest: digest, Order: taskExecutionOrder}, nil
}
func (r *TaskExecutionReader) taskPosition(binding cursor.Binding, token string) (*taskExecutionPosition, error) {
	if token == "" {
		return nil, nil
	}
	p, err := r.deps.Cursors.Verify(token, binding)
	if err != nil {
		return nil, portError(err)
	}
	if p.OrderGeneration != nil || len(p.Scalars) != 2 || p.Scalars[0].Kind() != "instant" || p.Scalars[1].Kind() != "uuid" {
		return nil, fault(f.CursorInvalid)
	}
	at, err := f.ParseInstant(p.Scalars[0].Value())
	if err != nil {
		return nil, fault(f.CursorInvalid)
	}
	id, err := f.ParseID[i.Execution](p.Scalars[1].Value())
	if err != nil {
		return nil, fault(f.CursorInvalid)
	}
	return &taskExecutionPosition{at, id}, nil
}

type terminalProjection struct {
	reason, status                  *string
	version                         *int64
	at                              *time.Time
	waitingReason, waitingReference *string
	waitingAt                       *time.Time
}
type taskExecutionRow struct {
	row      postgres.Row
	terminal *terminalProjection
}

func (r taskExecutionRow) Scan(dest ...any) error {
	t := r.terminal
	return r.row.Scan(append(dest, &t.reason, &t.status, &t.version, &t.at, &t.waitingReason, &t.waitingReference, &t.waitingAt)...)
}
func scanTaskExecution(row postgres.Row, project i.ProjectID, task string) (*ec.TaskExecution, error) {
	terminal := terminalProjection{}
	record, err := scanExecution(taskExecutionRow{row, &terminal})
	if err != nil || record == nil {
		return nil, err
	}
	s := record.summary.Clone()
	if s.Status == ec.Succeeded && (s.SnapshotID == nil || s.StartedAt == nil) {
		return nil, unavailable(nil)
	}
	if s.ProjectID != project || s.Trigger.Kind != "task" || s.Trigger.TaskID != task || s.Trigger.Validate(s.Purpose) != nil {
		return nil, unavailable(nil)
	}
	if terminal.reason == nil {
		if terminal.status != nil || terminal.version != nil || terminal.at != nil {
			return nil, unavailable(nil)
		}
	} else {
		if terminal.status == nil || terminal.version == nil || terminal.at == nil || s.CompletedAt == nil || *terminal.status != string(s.Status) || *terminal.version != int64(s.Version) || !terminal.at.Equal(s.CompletedAt.Time()) {
			return nil, unavailable(nil)
		}
		reason := *terminal.reason
		if !(s.Status == ec.Succeeded && reason == "completed" || s.Status == ec.Failed && (reason == "model_failed" || reason == "incomplete_response" || reason == "runtime_failed") || s.Status == ec.Cancelled && reason == "cancelled") {
			return nil, unavailable(nil)
		}
	}
	out := &ec.TaskExecution{ID: s.ID, ProjectID: s.ProjectID, TaskID: task, AgentID: s.AgentID, Purpose: s.Purpose, Status: s.Status, Version: s.Version, CreatedAt: s.CreatedAt, StartedAt: s.StartedAt, CompletedAt: s.CompletedAt, CancelRequestedAt: s.CancelRequestedAt, SnapshotID: s.SnapshotID, Reason: terminal.reason}
	if err = projectTaskWaiting(out, terminal.waitingReason, terminal.waitingReference, terminal.waitingAt); err != nil {
		return nil, err
	}
	return out, nil
}
func listTaskExecutions(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, task string, limit int, position *taskExecutionPosition) ([]ec.TaskExecution, error) {
	query := taskExecutionSelect + ` WHERE e.project_id=$1 AND e.launch_request->'trigger'->>'kind'='task' AND e.launch_request->'trigger'->>'task_id'=$2`
	args := []any{project.String(), task, limit + 1}
	if position != nil {
		query += ` AND (e.created_at,e.id)<($4,$5)`
		args = append(args, position.at.Time(), position.id.String())
	}
	query += ` ORDER BY e.created_at DESC,e.id DESC LIMIT $3`
	rows, err := x.Query(ctx, query, args...)
	if err != nil {
		return nil, portError(err)
	}
	if rows == nil {
		return nil, fault(f.DependencyUnbound)
	}
	defer rows.Close()
	out := make([]ec.TaskExecution, 0, limit+1)
	for rows.Next() {
		row, err := scanTaskExecution(rows, project, task)
		if err != nil {
			return nil, err
		}
		if row == nil || len(out) >= limit+1 {
			return nil, unavailable(nil)
		}
		previous := position
		if len(out) > 0 {
			last := out[len(out)-1]
			previous = &taskExecutionPosition{last.CreatedAt, last.ID}
		}
		if previous != nil && (row.CreatedAt.Time().After(previous.at.Time()) || row.CreatedAt == previous.at && row.ID.String() >= previous.id.String()) {
			return nil, unavailable(nil)
		}
		out = append(out, *row)
	}
	if err = rows.Err(); err != nil {
		return nil, portError(err)
	}
	return out, ctx.Err()
}
