package work

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// Caller holds Project SH and Schedule SH and Task SH for the complete read.
// Structure writers require Project EX; Task/blocker/history writers require
// Schedule EX and Task EX. Thus placement and child rows cannot move while
// resolving their IDs, and no lower-ranked lock is acquired after this read.
func loadTaskTriggerInput(ctx context.Context, x postgres.SQLExecutor, project pc.ProjectRef, id c.TaskID, purpose string) (TaskTriggerInput, error) {
	task, err := loadTask(ctx, x, project.ID, id)
	if err != nil {
		return TaskTriggerInput{}, err
	}
	sprint, err := loadSprint(ctx, x, project.ID, task.SprintID, project.CurrentSprintID)
	if err != nil {
		return TaskTriggerInput{}, err
	}
	milestone, err := loadMilestone(ctx, x, project.ID, task.MilestoneID)
	if err != nil {
		return TaskTriggerInput{}, err
	}
	blockers, err := loadTaskTriggerBlockers(ctx, x, project.ID, id)
	if err != nil {
		return TaskTriggerInput{}, err
	}
	events, err := loadTaskTriggerEvents(ctx, x, project.ID, id)
	if err != nil {
		return TaskTriggerInput{}, err
	}
	if err = ctx.Err(); err != nil {
		return TaskTriggerInput{}, err
	}
	input, err := newTaskTriggerInput(taskTriggerWire{1, task, sprint, milestone, purpose, blockers, events})
	if err != nil {
		return TaskTriggerInput{}, taskTriggerError(err)
	}
	return input, nil
}
func loadTaskTriggerBlockers(ctx context.Context, x postgres.SQLExecutor, project c.ProjectID, task c.TaskID) ([]c.TaskBlocker, error) {
	rows, err := x.Query(ctx, `SELECT `+blockerColumns+` FROM agenteam_work.task_blockers WHERE project_id=$1 AND task_id=$2 AND resolved_at IS NULL ORDER BY created_at,id LIMIT 257`, project.String(), task.String())
	if err != nil {
		return nil, taskTriggerError(err)
	}
	defer rows.Close()
	result := []c.TaskBlocker{}
	for rows.Next() {
		if len(result) == 256 {
			return nil, fault(f.ResourceBusy)
		}
		row, err := scanBlocker(rows)
		if err != nil {
			return nil, taskTriggerError(err)
		}
		if row == nil || row.Value.ProjectID != project || row.Value.TaskID != task || row.Value.ResolvedAt != nil {
			return nil, internal(nil)
		}
		result = append(result, row.Value.Clone())
	}
	if err = rows.Err(); err != nil {
		return nil, taskTriggerError(err)
	}
	return result, nil
}
func loadTaskTriggerEvents(ctx context.Context, x postgres.SQLExecutor, project c.ProjectID, task c.TaskID) ([]json.RawMessage, error) {
	rows, err := x.Query(ctx, `SELECT id::text,project_id::text,task_id::text,task_version,type,actor,operation_id::text,blocker_operation_id::text,transition_operation_id::text,claim_operation_id::text,correlation_id::text,payload,created_at FROM agenteam_work.task_events WHERE project_id=$1 AND task_id=$2 ORDER BY created_at DESC,id DESC LIMIT $3`, project.String(), task.String(), TaskTriggerRecentEvents)
	if err != nil {
		return nil, taskTriggerError(err)
	}
	defer rows.Close()
	result := []json.RawMessage{}
	for rows.Next() {
		if len(result) == TaskTriggerRecentEvents {
			return nil, internal(nil)
		}
		raw, err := scanTaskTriggerEvent(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, raw)
	}
	if err = rows.Err(); err != nil {
		return nil, taskTriggerError(err)
	}
	slices.Reverse(result)
	return result, nil
}
func scanTaskTriggerEvent(row interface{ Scan(...any) error }) (json.RawMessage, error) {
	var id, project, task, kind, correlation string
	var version f.Version
	var operation, blockerOperation, transitionOperation, claimOperation *string
	var actor, payload []byte
	var created time.Time
	if err := row.Scan(&id, &project, &task, &version, &kind, &actor, &operation, &blockerOperation, &transitionOperation, &claimOperation, &correlation, &payload, &created); err != nil {
		return nil, taskTriggerError(err)
	}
	var actorKind struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(actor, &actorKind) != nil {
		return nil, internal(nil)
	}
	if claimOperation != nil && actorKind.Type != "system" || transitionOperation != nil && actorKind.Type != "human" {
		return nil, internal(nil)
	}
	selected := operation
	switch kind {
	case string(c.TaskEventCreated), string(c.TaskEventFieldsUpdated):
		if operation == nil || blockerOperation != nil || transitionOperation != nil || claimOperation != nil {
			return nil, internal(nil)
		}
	case string(c.TaskBlockerEventAdded), string(c.TaskBlockerEventResolved):
		if blockerOperation == nil || operation != nil || transitionOperation != nil || claimOperation != nil {
			return nil, internal(nil)
		}
		selected = blockerOperation
	case string(c.TaskTransitionStateChanged), string(c.TaskTransitionAssigneeChanged), string(c.TaskTransitionComment):
		if claimOperation != nil {
			if kind != string(c.TaskTransitionStateChanged) || transitionOperation != nil || operation != nil || blockerOperation != nil {
				return nil, internal(nil)
			}
			selected = claimOperation
			break
		}
		if transitionOperation == nil || operation != nil || blockerOperation != nil {
			return nil, internal(nil)
		}
		selected = transitionOperation
	default:
		return nil, fault(f.SchemaUnsupported)
	}
	at, err := f.NewInstant(created)
	if err != nil {
		return nil, internal(nil)
	}
	raw, err := json.Marshal(struct {
		ID          string          `json:"id"`
		Project     string          `json:"project_id"`
		Task        string          `json:"task_id"`
		Version     f.Version       `json:"task_version"`
		Type        string          `json:"type"`
		Actor       json.RawMessage `json:"actor"`
		Operation   string          `json:"operation_id"`
		Correlation string          `json:"correlation_id"`
		Payload     json.RawMessage `json:"payload"`
		Created     f.Instant       `json:"created_at"`
	}{id, project, task, version, kind, actor, *selected, correlation, payload, at})
	if err != nil {
		return nil, internal(nil)
	}
	if _, err = decodeTaskTriggerEvent(raw); err != nil {
		return nil, internal(nil)
	}
	return raw, nil
}
