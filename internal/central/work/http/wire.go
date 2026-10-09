package workhttp

import (
	"context"
	"encoding/json"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"strings"
	"unicode/utf8"
)

const bodyLimit = 1 << 20
const listLimit = 5 << 20
const maxQueryBytes = 32 << 10
const maxCursorBytes = 8192

func invalidInput() error  { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func badProjection() error { return f.NewFault(f.DependencyUnavailable, f.NotStarted) }
func encodeValue(ctx context.Context, v any, limit int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > limit {
		return nil, badProjection()
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}
func validPage(request f.PageRequest, n int, next string) bool {
	return request.Validate() == nil && n <= request.Limit && len(next) <= maxCursorBytes && utf8.ValidString(next) && !strings.ContainsRune(next, 0) && (next == "" || n == request.Limit)
}

type milestoneSummary struct {
	ID         c.MilestoneID `json:"id"`
	ProjectID  c.ProjectID   `json:"project_id"`
	Title      string        `json:"title"`
	ManualRank string        `json:"manual_rank"`
	Version    f.Version     `json:"version"`
	CreatedAt  f.Instant     `json:"created_at"`
	UpdatedAt  f.Instant     `json:"updated_at"`
}

func summarizeMilestone(v c.Milestone) milestoneSummary {
	return milestoneSummary{v.ID, v.ProjectID, v.Title, v.ManualRank, v.Version, v.CreatedAt, v.UpdatedAt}
}

type sprintSummary struct {
	ID          c.SprintID    `json:"id"`
	ProjectID   c.ProjectID   `json:"project_id"`
	MilestoneID c.MilestoneID `json:"milestone_id"`
	Title       string        `json:"title"`
	State       c.SprintState `json:"state"`
	ManualRank  string        `json:"manual_rank"`
	Version     f.Version     `json:"version"`
	CreatedAt   f.Instant     `json:"created_at"`
	UpdatedAt   f.Instant     `json:"updated_at"`
}

func summarizeSprint(v c.Sprint) sprintSummary {
	return sprintSummary{v.ID, v.ProjectID, v.MilestoneID, v.Title, v.State, v.ManualRank, v.Version, v.CreatedAt, v.UpdatedAt}
}

type taskSummary struct {
	ID              c.TaskID       `json:"id"`
	ProjectID       c.ProjectID    `json:"project_id"`
	MilestoneID     c.MilestoneID  `json:"milestone_id"`
	SprintID        c.SprintID     `json:"sprint_id"`
	Title           string         `json:"title"`
	Type            c.TaskType     `json:"type"`
	Priority        c.TaskPriority `json:"priority"`
	State           c.TaskState    `json:"state"`
	AssigneeAgentID *id.AgentID    `json:"assignee_agent_id"`
	ManualRank      string         `json:"manual_rank"`
	Version         f.Version      `json:"version"`
	CreatedAt       f.Instant      `json:"created_at"`
	UpdatedAt       f.Instant      `json:"updated_at"`
}

func summarizeTask(v c.Task) taskSummary {
	return taskSummary{v.ID, v.ProjectID, v.MilestoneID, v.SprintID, v.Title, v.Type, v.Priority, v.State, v.AssigneeAgentID, v.ManualRank, v.Version, v.CreatedAt, v.UpdatedAt}
}
