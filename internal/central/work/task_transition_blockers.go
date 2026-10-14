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
	Before            c.TaskBlocker              `json:"before"`
	After             c.TaskBlocker              `json:"after"`
	CreatedOperation  *c.TaskBlockerCommandID    `json:"created_operation_id"`
	CreatedTransition *c.TaskTransitionCommandID `json:"created_transition_operation_id,omitempty"`
	FailureOperation  *c.SchedulerClaimID        `json:"failure_operation_id"`
}

func (v *transitionBlockerResolution) UnmarshalJSON(raw []byte) error {
	var shape map[string]json.RawMessage
	if json.Unmarshal(raw, &shape) != nil {
		return internal(nil)
	}
	fields := []string{"before", "after", "created_operation_id", "failure_operation_id"}
	if _, ok := shape["created_transition_operation_id"]; ok {
		fields = append(fields, "created_transition_operation_id")
	}
	if _, err := taskPrivateObject(raw, 2*c.MaxTaskBlockerRecordBytes+1024, fields, []string{"created_operation_id", "failure_operation_id"}); err != nil {
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
	if v.Before.Type == c.TaskBlockerUserCancelledExecution {
		if v.CreatedTransition == nil || v.CreatedTransition.Validate() != nil || v.CreatedOperation != nil || v.FailureOperation != nil {
			return internal(nil)
		}
		return nil
	}
	if v.CreatedTransition != nil {
		return internal(nil)
	}
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
	if len(request.AddBlockers) != 0 {
		return fault(f.DependencyUnbound)
	}
	switch {
	case transitionCancelEdge(before, request), transitionBlockEdge(before, request):
		if before.State == c.TaskStateInReview && request.TargetState == c.TaskStateBlocked && request.Comment == nil {
			return field(f.CommentRequired, "/comment", "COMMENT_REQUIRED")
		}
		// This capability changes state only. Retain the historical assignee
		// (including nil) and every Blocker; no new assignment or resolution.
		if request.AssigneeAgentID != nil || len(request.ResolveBlockerIDs) != 0 {
			return fault(f.DependencyUnbound)
		}
		return nil
	case before.State == c.TaskStateBacklog && request.TargetState == c.TaskStateTodo:
		// Keep the existing backlog writer's supported intent unchanged.
		if request.AssigneeAgentID == nil {
			return fault(f.TaskAssigneeRequired)
		}
		if len(request.ResolveBlockerIDs) != 0 {
			return fault(f.DependencyUnbound)
		}
	case before.State == c.TaskStateBlocked && request.TargetState == c.TaskStateTodo:
	case before.State == c.TaskStateInProgress && request.TargetState == c.TaskStateInReview,
		before.State == c.TaskStateInReview && request.TargetState == c.TaskStateTodo:
		if request.AssigneeAgentID == nil {
			return fault(f.TaskAssigneeRequired)
		}
	case before.State == c.TaskStateInReview && request.TargetState == c.TaskStateDone:
	default:
		return fault(f.DependencyUnbound)
	}
	if transitionAgent(before, request) == nil {
		return fault(f.TaskAssigneeRequired)
	}
	if transitionReviewEdge(before, request) && request.Comment == nil {
		return field(f.CommentRequired, "/comment", "COMMENT_REQUIRED")
	}
	return nil
}

func transitionCancelEdge(before c.Task, request c.TaskTransfer) bool {
	return request.TargetState == c.TaskStateCancelled && before.State.Validate() == nil && !before.State.Terminal()
}

func transitionBlockEdge(before c.Task, request c.TaskTransfer) bool {
	return (before.State == c.TaskStateTodo || before.State == c.TaskStateInProgress || before.State == c.TaskStateInReview) && request.TargetState == c.TaskStateBlocked
}

// Cancelling or blocking does not select an Agent for new work. Retaining an
// old assignment requires no current-Agent fact, like an omitted done reviewer.
func transitionRetainsAssignee(before c.Task, request c.TaskTransfer) bool {
	return transitionRetainsReviewer(before, request) || (transitionCancelEdge(before, request) || transitionBlockEdge(before, request)) && request.AssigneeAgentID == nil
}

func transitionReviewEdge(before c.Task, request c.TaskTransfer) bool {
	return before.State == c.TaskStateInProgress && request.TargetState == c.TaskStateInReview ||
		before.State == c.TaskStateInReview && (request.TargetState == c.TaskStateDone || request.TargetState == c.TaskStateTodo)
}

// An incoming done transition retains the current reviewer when omitted. It
// does not select that Agent for new work or require a new current-Agent fact.
// An explicit selection, including the same ID, still requires that fact.
func transitionRetainsReviewer(before c.Task, request c.TaskTransfer) bool {
	return before.State == c.TaskStateInReview && request.TargetState == c.TaskStateDone && request.AssigneeAgentID == nil
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
		v := transitionBlockerResolution{Before: b.Clone(), FailureOperation: row.FailureOperation, CreatedTransition: row.CreatedTransition}
		if row.FailureOperation == nil && row.CreatedTransition == nil {
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
	if in.Request.TargetState == c.TaskStateTodo && remaining != int64(len(out)) {
		return nil, at, field(f.InvalidState, "/task_id", "UNRESOLVED_BLOCKERS")
	}
	if in.Request.TargetState == c.TaskStateBlocked && in.Cancellation == nil && remaining == int64(len(out)) {
		return nil, at, fault(f.TaskBlockedWithoutBlocker)
	}
	for n := range out {
		b := out[n].Before.Clone()
		when := at
		who := c.TaskEventActor{Type: i.Human, UserID: in.User, Source: "task_domain"}
		b.ResolvedAt, b.ResolutionComment = &when, nil
		if in.Agent.Validate() == nil {
			a := in.historyActor()
			b.TransitionResolvedBy = &a
		} else {
			b.ResolvedBy = &who
		}
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
		expected.ResolvedAt = &at
		if r.Input.Agent.Validate() == nil {
			a := r.Input.historyActor()
			expected.TransitionResolvedBy = &a
		} else {
			expected.ResolvedBy = &by
		}
		if expected.Validate() != nil || !sameValue(expected, v.After) {
			return internal(nil)
		}
	}
	return nil
}
func applyTransitionResolutions(ctx context.Context, x postgres.SQLExecutor, r *transitionRecord) error {
	for _, v := range r.Plan.Resolutions {
		var resolver any = v.After.ResolvedBy
		if v.After.TransitionResolvedBy != nil {
			resolver = v.After.TransitionResolvedBy
		}
		by, err := json.Marshal(resolver)
		if err != nil {
			return internal(err)
		}
		var created, failure, transition any
		if v.CreatedTransition != nil {
			transition = v.CreatedTransition.String()
		}
		if v.CreatedOperation != nil {
			created = v.CreatedOperation.String()
		}
		if v.FailureOperation != nil {
			failure = v.FailureOperation.String()
		}
		if err = taskAffected(x.Exec(ctx, `UPDATE agenteam_work.task_blockers SET resolved_at=$4,resolved_by=$5,resolution_comment=NULL,resolved_transition_operation_id=$6 WHERE project_id=$1 AND task_id=$2 AND id=$3 AND resolved_at IS NULL AND resolved_operation_id IS NULL AND resolved_transition_operation_id IS NULL AND created_operation_id IS NOT DISTINCT FROM $7::agenteam_work.safe_id AND failure_operation_id IS NOT DISTINCT FROM $8::agenteam_work.safe_id AND created_transition_operation_id IS NOT DISTINCT FROM $9::agenteam_work.safe_id`, r.Input.Project.String(), r.Input.Task.String(), v.Before.ID.String(), v.After.ResolvedAt.Time(), by, r.ID.String(), created, failure, transition)); err != nil {
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
		if row == nil || !sameValue(row.Value, v.After) || row.ResolvedOperation != nil || row.ResolvedTransition == nil || *row.ResolvedTransition != r.ID || !sameValue(row.FailureOperation, v.FailureOperation) || !sameValue(row.CreatedTransition, v.CreatedTransition) {
			return fault(f.Forbidden)
		}
		if v.CreatedOperation != nil && row.CreatedOperation != *v.CreatedOperation {
			return fault(f.Forbidden)
		}
	}
	if len(r.Plan.Resolutions) == 0 && r.Input.Request.TargetState != c.TaskStateBlocked {
		return nil
	}
	var linked, unresolved int64
	if err := x.QueryRow(ctx, `SELECT count(*) FILTER(WHERE resolved_transition_operation_id=$3),count(*) FILTER(WHERE resolved_at IS NULL) FROM agenteam_work.task_blockers WHERE project_id=$1 AND task_id=$2`, r.Input.Project.String(), r.Input.Task.String(), r.ID.String()).Scan(&linked, &unresolved); err != nil {
		return taskSQL(err)
	}
	if linked != int64(len(r.Plan.Resolutions)) || unresolved < 0 || r.Input.Request.TargetState == c.TaskStateTodo && unresolved != 0 || r.Input.Request.TargetState == c.TaskStateBlocked && unresolved == 0 {
		return fault(f.Forbidden)
	}
	return nil
}
