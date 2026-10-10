package contract

import (
	"errors"
	"fmt"
	"io"
	"log/slog"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type taskLaunchFailureMarker struct {
	digest  f.Digest
	request f.ID[f.Request]
	key     f.IdempotencyKey
}

func (*taskLaunchFailureMarker) Error() string { return "task_launch_policy_unsupported" }
func (*taskLaunchFailureMarker) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "task_launch_policy_unsupported")
}
func (*taskLaunchFailureMarker) LogValue() slog.Value {
	return slog.StringValue("task_launch_policy_unsupported")
}

func resourceConstraintRejectionRequest(r ec.LaunchRequest) bool {
	return r.Validate() == nil && r.Trigger.Kind == "task" && r.Purpose == "task/work" && r.Lineage.DispatchID != "" &&
		r.Meta.IdempotencyKey == f.IdempotencyKey("scheduler_dispatch:"+r.Lineage.DispatchID) &&
		r.Lineage.RetryOf == nil && r.Lineage.RegenerateOf == nil && r.Lineage.ContributionGeneration == nil && r.Lineage.ContributionAttempt == nil &&
		len(r.Policy.AllowedResourceConstraints) > 0
}

// RejectTaskResourceConstraints describes only the v1 Task provider's exact
// unsupported original policy. It performs no authorization or write. The
// Scheduler may persist a marker only from its original synchronous Execution
// call, a definite not-created result and its own known-committed send attempt.
func RejectTaskResourceConstraints(r ec.LaunchRequest) error {
	if !resourceConstraintRejectionRequest(r) {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	digest, err := r.Digest()
	if err != nil {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	return f.NewFault(f.DependencyUnbound, f.NotStarted).WithCause(&taskLaunchFailureMarker{digest, r.Meta.RequestID, r.Meta.IdempotencyKey})
}

// MatchTaskLaunchFailure recognizes the private typed cause and complete saved
// request binding. A match is a safe classification, never a finality grant:
// unknown physical outcomes and unrelated calls must still be rejected by the
// Scheduler owner before it issues any mutation authority.
func MatchTaskLaunchFailure(err error, original ec.LaunchRequest) (TaskLaunchFailureReason, bool) {
	if err == nil || !resourceConstraintRejectionRequest(original) {
		return "", false
	}
	var marker *taskLaunchFailureMarker
	if !errors.As(err, &marker) || marker == nil {
		return "", false
	}
	digest, e := original.Digest()
	if e != nil || marker.digest != digest || marker.request != original.Meta.RequestID || marker.key != original.Meta.IdempotencyKey {
		return "", false
	}
	return TaskLaunchFailureUnsupportedResourceConstraints, true
}
