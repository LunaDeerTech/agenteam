package work

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// The persisted preimage retains the independent creation source. Resolution
// belongs to the original Human transition, never to the Scheduler failure.
type transitionBlockerResolution struct {
	Before           c.TaskBlocker           `json:"before"`
	After            c.TaskBlocker           `json:"after"`
	CreatedOperation *c.TaskBlockerCommandID `json:"created_operation_id"`
	FailureOperation *c.SchedulerClaimID     `json:"failure_operation_id"`
}

func (v *transitionBlockerResolution) UnmarshalJSON(raw []byte) error {
	if _, err := taskPrivateObject(raw, 2*c.MaxTaskBlockerRecordBytes+1024, []string{"before", "after", "created_operation_id", "failure_operation_id"}, []string{"created_operation_id", "failure_operation_id"}); err != nil {
		return err
	}
	type wire transitionBlockerResolution
	var next wire
	if err := json.Unmarshal(raw, &next); err != nil {
		return internal(err)
	}
	n := transitionBlockerResolution(next)
	if n.Before.Validate() != nil || n.After.Validate() != nil || n.validateOrigin() != nil {
		return internal(nil)
	}
	*v = n
	return nil
}
func (v transitionBlockerResolution) validateOrigin() error {
	if v.Before.Type == c.TaskBlockerTechnical {
		if v.CreatedOperation != nil || v.FailureOperation == nil || v.FailureOperation.Validate() != nil || v.Before.Technical == nil || v.Before.Technical.ReferenceID != v.FailureOperation.String() {
			return internal(nil)
		}
	} else if v.FailureOperation != nil || v.CreatedOperation == nil || v.CreatedOperation.Validate() != nil {
		return internal(nil)
	}
	return nil
}
func transitionAgent(before c.Task, request c.TaskTransfer) *i.AgentID {
	if request.AssigneeAgentID != nil {
		return request.AssigneeAgentID
	}
	return before.AssigneeAgentID
}
func transitionEdge(before c.Task, request c.TaskTransfer) error {
	if request.TargetState != c.TaskStateTodo || (before.State != c.TaskStateBacklog && before.State != c.TaskStateBlocked) || len(request.AddBlockers) != 0 {
		return fault(f.DependencyUnbound)
	}
	// Keep the existing backlog writer's supported intent unchanged.
	if before.State == c.TaskStateBacklog && (request.AssigneeAgentID == nil || len(request.ResolveBlockerIDs) != 0) {
		if request.AssigneeAgentID == nil {
			return fault(f.TaskAssigneeRequired)
		}
		return fault(f.DependencyUnbound)
	}
	if transitionAgent(before, request) == nil {
		return fault(f.TaskAssigneeRequired)
	}
	return nil
}
func transitionResolveIDs(in transitionInput) []c.TaskBlockerID {
	ids := slices.Clone(in.Request.ResolveBlockerIDs)
	slices.SortFunc(ids, func(a, b c.TaskBlockerID) int { return strings.Compare(a.String(), b.String()) })
	return ids
}
func prepareTransitionResolutions(ctx context.Context, x postgres.SQLExecutor, in transitionInput, at f.Instant) ([]transitionBlockerResolution, f.Instant, error) {
	var out []transitionBlockerResolution
	for _, id := range transitionResolveIDs(in) {
		row, err := loadBlocker(ctx, &blockerScope{x, in.Project, in.Task}, id)
		if err != nil {
			return nil, at, err
		}
		if row == nil {
			return nil, at, fault(f.BlockerNotFound)
		}
		b := row.Value
		if b.ID != id || b.ProjectID != in.Project || b.TaskID != in.Task {
			return nil, at, internal(nil)
		}
		if b.ResolvedAt != nil {
			return nil, at, fault(f.BlockerAlreadyResolved)
		}
		v := transitionBlockerResolution{Before: b.Clone(), FailureOperation: row.FailureOperation}
		if row.FailureOperation == nil {
			id := row.CreatedOperation
			v.CreatedOperation = &id
		}
		if v.validateOrigin() != nil {
			return nil, at, internal(nil)
		}
		if at.Time().Before(b.CreatedAt.Time()) {
			at = b.CreatedAt
		}
		out = append(out, v)
	}
	var remaining int64
	if err := x.QueryRow(ctx, `SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1 AND task_id=$2 AND resolved_at IS NULL`, in.Project.String(), in.Task.String()).Scan(&remaining); err != nil {
		return nil, at, taskSQL(err)
	}
	if remaining < int64(len(out)) {
		return nil, at, internal(nil)
	}
	if remaining != int64(len(out)) {
		return nil, at, field(f.InvalidState, "/task_id", "UNRESOLVED_BLOCKERS")
	}
	for n := range out {
		b := out[n].Before.Clone()
		when := at
		who := c.TaskEventActor{Type: i.Human, UserID: in.User, Source: "task_domain"}
		b.ResolvedAt, b.ResolvedBy, b.ResolutionComment = &when, &who, nil
		if b.Validate() != nil {
			return nil, at, internal(nil)
		}
		out[n].After = b
	}
	if err := ctx.Err(); err != nil {
		return nil, at, canceled(err)
	}
	return out, at, nil
}
func validateTransitionResolutions(r *transitionRecord) error {
	ids := transitionResolveIDs(r.Input)
	if len(ids) != len(r.Plan.Resolutions) {
		return internal(nil)
	}
	for n, v := range r.Plan.Resolutions {
		b := v.Before
		if b.Validate() != nil || v.validateOrigin() != nil || b.ID != ids[n] || b.ProjectID != r.Input.Project || b.TaskID != r.Input.Task || b.ResolvedAt != nil || (n > 0 && ids[n-1] == ids[n]) {
			return internal(nil)
		}
		expected := b.Clone()
		at := r.Plan.After.Task.UpdatedAt
		by := c.TaskEventActor{Type: i.Human, UserID: r.Input.User, Source: "task_domain"}
		expected.ResolvedAt, expected.ResolvedBy = &at, &by
		if expected.Validate() != nil || !sameValue(expected, v.After) {
			return internal(nil)
		}
	}
	return nil
}
func applyTransitionResolutions(ctx context.Context, x postgres.SQLExecutor, r *transitionRecord) error {
	for _, v := range r.Plan.Resolutions {
		by, err := json.Marshal(v.After.ResolvedBy)
		if err != nil {
			return internal(err)
		}
		var created, failure any
		if v.CreatedOperation != nil {
			created = v.CreatedOperation.String()
		}
		if v.FailureOperation != nil {
			failure = v.FailureOperation.String()
		}
		if err = taskAffected(x.Exec(ctx, `UPDATE agenteam_work.task_blockers SET resolved_at=$4,resolved_by=$5,resolution_comment=NULL,resolved_transition_operation_id=$6 WHERE project_id=$1 AND task_id=$2 AND id=$3 AND resolved_at IS NULL AND resolved_operation_id IS NULL AND resolved_transition_operation_id IS NULL AND created_operation_id IS NOT DISTINCT FROM $7::agenteam_work.safe_id AND failure_operation_id IS NOT DISTINCT FROM $8::agenteam_work.safe_id`, r.Input.Project.String(), r.Input.Task.String(), v.Before.ID.String(), v.After.ResolvedAt.Time(), by, r.ID.String(), created, failure)); err != nil {
			return err
		}
	}
	return nil
}
func verifyTransitionResolutions(ctx context.Context, x postgres.SQLExecutor, r *transitionRecord) error {
	for _, v := range r.Plan.Resolutions {
		row, err := loadBlocker(ctx, &blockerScope{x, r.Input.Project, r.Input.Task}, v.Before.ID)
		if err != nil {
			return err
		}
		if row == nil || !sameValue(row.Value, v.After) || row.ResolvedOperation != nil || row.ResolvedTransition == nil || *row.ResolvedTransition != r.ID || !sameValue(row.FailureOperation, v.FailureOperation) {
			return fault(f.Forbidden)
		}
		if v.CreatedOperation != nil && row.CreatedOperation != *v.CreatedOperation {
			return fault(f.Forbidden)
		}
	}
	if len(r.Plan.Resolutions) == 0 {
		return nil
	}
	var linked, unresolved int64
	if err := x.QueryRow(ctx, `SELECT count(*) FILTER(WHERE resolved_transition_operation_id=$3),count(*) FILTER(WHERE resolved_at IS NULL) FROM agenteam_work.task_blockers WHERE project_id=$1 AND task_id=$2`, r.Input.Project.String(), r.Input.Task.String(), r.ID.String()).Scan(&linked, &unresolved); err != nil {
		return taskSQL(err)
	}
	if linked != int64(len(r.Plan.Resolutions)) || unresolved != 0 {
		return fault(f.Forbidden)
	}
	return nil
}
