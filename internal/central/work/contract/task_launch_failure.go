package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// TaskLaunchFailureRequest identifies exactly one original dispatch origin and send attempt.
// Neither these fields nor a known-not-created outcome prove final failure.
type TaskLaunchFailureRequest struct {
	Claim           TaskClaimRequest
	DispatchVersion f.Version
	LaunchAttempt   int64
	Relaunch        *TaskRelaunchRequest
}

func (v TaskLaunchFailureRequest) Validate() error {
	if v.DispatchVersion.Validate() != nil || v.LaunchAttempt <= 0 {
		return invalid("", "INVALID_TASK_LAUNCH_FAILURE")
	}
	if v.Relaunch == nil {
		if v.Claim.Validate() != nil {
			return invalid("", "INVALID_TASK_LAUNCH_FAILURE")
		}
	} else if v.Claim != (TaskClaimRequest{}) || v.Relaunch.Validate() != nil {
		return invalid("", "INVALID_TASK_LAUNCH_FAILURE")
	}
	return nil
}
func (v TaskLaunchFailureRequest) Clone() TaskLaunchFailureRequest {
	v.Relaunch = taskClonePtr(v.Relaunch)
	return v
}

// The closed reason distinguishes a deterministic policy rejection from an
// exhausted bound retry policy. A Fault code, retryable flag, attempt count or
// absent Lookup result cannot establish either fact.
type TaskLaunchFailureReason string

const (
	TaskLaunchFailureUnsupportedResourceConstraints TaskLaunchFailureReason = "unsupported_resource_constraints_v1"
	TaskLaunchFailureRetryExhausted                 TaskLaunchFailureReason = "launch_retry_exhausted_v1"
)

func (v TaskLaunchFailureReason) Validate() error {
	if v != TaskLaunchFailureUnsupportedResourceConstraints && v != TaskLaunchFailureRetryExhausted {
		return invalid("", "INVALID_TASK_LAUNCH_FAILURE_REASON")
	}
	return nil
}

// Facts are a callback-local projection of Scheduler's durable final marker,
// not a bearer grant. Work compares Guard with its own immutable claim and
// freezes the complete facts into its private plan before any write.
type TaskLaunchFailureFacts struct {
	Guard      TaskClaimGuard
	Reason     TaskLaunchFailureReason
	OccurredAt f.Instant
	Relaunch   *TaskRelaunchSource
}

func (v TaskLaunchFailureFacts) Clone() TaskLaunchFailureFacts {
	v.Guard = v.Guard.Clone()
	v.Relaunch = taskClonePtr(v.Relaunch)
	return v
}

type TaskLaunchFailurePlan interface{ RequiredLocks() []f.LockRequest }

// Changed means the new technical Blocker was actually written with the Work
// result in the caller's still-tentative transaction. It does not imply a
// state change if the current Task was blocked, or a known physical commit.
// Private concrete issuer/plan/Tx evidence, not this projection, authorizes
// the Scheduler's final compare-and-swap in the same transaction.
type AppliedTaskLaunchFailure interface{ Changed() bool }

type SchedulerTaskLaunchFailures interface {
	DiscoverTaskLaunchFailure(context.Context, i.Actor, TaskLaunchFailureRequest) (TaskLaunchFailurePlan, error)
	ApplyTaskLaunchFailureInTx(context.Context, f.Tx, i.Actor, TaskLaunchFailureRequest, TaskLaunchFailurePlan) (AppliedTaskLaunchFailure, error)
	CheckTaskLaunchFailureAppliedInTx(context.Context, f.Tx, i.Actor, TaskLaunchFailureRequest, TaskLaunchFailurePlan, AppliedTaskLaunchFailure) error
}

// Scheduler must verify its live private discovery/applying call, the original
// same-Store transaction and complete held union, exact pending Dispatch and
// same-attempt durable final rejection and exact claim or relaunch origin. Unknown, AgentBusy and historical
// known-not-created without the final marker never pass. The final callback
// also binds the original Work plan and excludes only this Dispatch from its
// own protected source group; other pending claims remain protected.
//
// RetryExhausted additionally requires the Scheduler's immutable stored policy
// and its durable, request-bound temporary rejection for the same canonical
// attempt, with that policy's attempt budget exhausted. A missing policy,
// unrelated temporary observation or numeric counter alone must not issue
// facts. Work does not calculate retries or reinterpret Execution errors.
//
// Paused scheduling preserves confirmed-final pending without Task mutation or
// settlement. This is not a new Launch: no free-slot, quota or current-Sprint
// admission is inferred. Work independently rechecks the current Task's full
// preimage and applicability, preserving user changes and rejecting a stale
// discovery plan. These callbacks must not recurse into Work or Execution.
// A relaunch never supplies a ClaimGuard. Its source is compared with Work's
// immutable relaunch row; failure handling does not manufacture a todo claim.
type SchedulerTaskLaunchFailureAuthority interface {
	RequireTaskLaunchFailureDiscoveryInTx(context.Context, f.Tx, i.Actor, TaskLaunchFailureRequest) (TaskLaunchFailureFacts, error)
	RequireTaskLaunchFailureInTx(context.Context, f.Tx, i.Actor, TaskLaunchFailureRequest, TaskLaunchFailurePlan) (TaskLaunchFailureFacts, error)
}

func (TaskLaunchFailureRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "task_launch_failure")
}
func (TaskLaunchFailureRequest) LogValue() slog.Value { return slog.StringValue("task_launch_failure") }
func (TaskLaunchFailureFacts) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "task_launch_failure")
}
func (TaskLaunchFailureFacts) LogValue() slog.Value { return slog.StringValue("task_launch_failure") }
