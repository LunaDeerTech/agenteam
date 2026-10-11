package work

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5"
)

type taskRelaunchRecord struct {
	Request   c.TaskRelaunchRequest `json:"request"`
	Task      c.Task                `json:"task"`
	Sprint    c.Sprint              `json:"sprint"`
	Milestone c.Milestone           `json:"milestone"`
	CreatedAt f.Instant             `json:"created_at"`
}

func validateTaskRelaunchRecord(r *taskRelaunchRecord) error {
	if r == nil || r.Request.Validate() != nil || validateRelaunchTask(r.Task, r.Request) != nil || r.Sprint.Validate() != nil || r.Milestone.Validate() != nil || r.CreatedAt.Validate() != nil || r.CreatedAt.Time().Before(r.Task.UpdatedAt.Time()) || r.Sprint.ProjectID != r.Request.ProjectID || r.Sprint.ID != r.Request.CurrentSprintID || r.Sprint.State != c.Current || r.Sprint.MilestoneID != r.Task.MilestoneID || r.Milestone.ID != r.Task.MilestoneID || r.Milestone.ProjectID != r.Request.ProjectID {
		return internal(nil)
	}
	return nil
}
func (r taskRelaunchRecord) source() c.TaskRelaunchSource {
	raw, err := canonical(r)
	if err != nil || validateTaskRelaunchRecord(&r) != nil {
		return c.TaskRelaunchSource{}
	}
	return c.TaskRelaunchSource{Request: r.Request, MilestoneID: r.Milestone.ID, ReferenceDigest: digest(raw)}
}
func (r *taskRelaunchRecord) UnmarshalJSON(raw []byte) error {
	if r == nil {
		return internal(nil)
	}
	if _, err := taskPrivateObject(raw, taskPlanCap, []string{"request", "task", "sprint", "milestone", "created_at"}, nil); err != nil {
		return internal(err)
	}
	type wire taskRelaunchRecord
	var w wire
	if json.Unmarshal(raw, &w) != nil {
		return internal(nil)
	}
	n := taskRelaunchRecord(w)
	if err := validateTaskRelaunchRecord(&n); err != nil {
		return err
	}
	a, err := cursorPayload(raw)
	b, e2 := canonical(n)
	if err != nil || e2 != nil || !slices.Equal(a, b) {
		return internal(nil)
	}
	*r = n
	return nil
}
func insertTaskRelaunch(ctx context.Context, x postgres.SQLExecutor, r *taskRelaunchRecord) error {
	if err := validateTaskRelaunchRecord(r); err != nil {
		return err
	}
	raw, err := canonical(r)
	if err != nil || len(raw) > taskPlanCap {
		return internal(err)
	}
	ref := r.source()
	if ref.Validate() != nil {
		return internal(nil)
	}
	err = taskAffected(x.Exec(ctx, `INSERT INTO agenteam_work.task_scheduler_relaunches(id,project_id,task_id,agent_id,sprint_id,milestone_id,request_id,task_version,purpose,source_digest,record,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'task/work',$9,$10,$11)`, r.Request.DispatchID, r.Request.ProjectID.String(), r.Request.TaskID.String(), r.Request.AgentID.String(), r.Request.CurrentSprintID.String(), r.Task.MilestoneID.String(), r.Request.RequestID.String(), int64(r.Task.Version), string(ref.ReferenceDigest), raw, r.CreatedAt.Time()))
	if err != nil {
		return err
	}
	return ctx.Err()
}
func loadTaskRelaunch(ctx context.Context, x postgres.SQLExecutor, project c.ProjectID, id string) (*taskRelaunchRecord, error) {
	var task, agent, sprint, milestone, request, purpose, hash string
	var version int64
	var raw []byte
	var at time.Time
	err := x.QueryRow(ctx, `SELECT task_id::text,agent_id::text,sprint_id::text,milestone_id::text,request_id::text,task_version,purpose,source_digest,record,created_at FROM agenteam_work.task_scheduler_relaunches WHERE project_id=$1 AND id=$2`, project.String(), id).Scan(&task, &agent, &sprint, &milestone, &request, &version, &purpose, &hash, &raw, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, taskSQL(err)
	}
	var r taskRelaunchRecord
	if json.Unmarshal(raw, &r) != nil || r.Request.ProjectID != project || r.Request.DispatchID != id || r.Request.TaskID.String() != task || r.Request.AgentID.String() != agent || r.Request.CurrentSprintID.String() != sprint || r.Task.MilestoneID.String() != milestone || r.Request.RequestID.String() != request || int64(r.Task.Version) != version || r.Request.Purpose != purpose || string(r.source().ReferenceDigest) != hash || !r.CreatedAt.Time().Equal(at) {
		return nil, internal(nil)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return &r, nil
}
func verifyTaskRelaunch(ctx context.Context, x postgres.SQLExecutor, r *taskRelaunchRecord) error {
	actual, err := loadTaskRelaunch(ctx, x, r.Request.ProjectID, r.Request.DispatchID)
	if err != nil {
		return err
	}
	if actual == nil || !sameValue(*actual, *r) {
		return fault(f.Forbidden)
	}
	task, err := loadTask(ctx, x, r.Request.ProjectID, r.Request.TaskID)
	if err != nil {
		return err
	}
	if !sameValue(task, r.Task) {
		return fault(f.Forbidden)
	}
	return ctx.Err()
}
