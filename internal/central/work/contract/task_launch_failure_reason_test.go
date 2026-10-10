package contract

import (
	"encoding/json"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestTaskLaunchFailureExhaustionReasonKeepsProofBoundary(t *testing.T) {
	for _, reason := range []TaskLaunchFailureReason{TaskLaunchFailureUnsupportedResourceConstraints, TaskLaunchFailureRetryExhausted} {
		if reason.Validate() != nil {
			t.Fatal("known failure reason rejected")
		}
	}
	for _, reason := range []TaskLaunchFailureReason{"", "retry_exhausted", "launch_retry_exhausted_v2", "launch_lock_timeout_v1", "COMMIT_UNKNOWN", "agent_busy"} {
		if reason.Validate() == nil {
			t.Fatal("unknown or non-final cause entered the reason closure")
		}
	}
	task := taskFixture(t)
	dispatch := testID[SchedulerClaim](t, 219).String()
	r := ec.LaunchRequest{ProjectID: task.ProjectID, AgentID: testID[i.Agent](t, 220), Trigger: ec.Trigger{Kind: "task", TaskID: task.ID.String()}, Purpose: "task/work", Policy: ec.Policy{SchemaVersion: 1, DeniedToolIDs: []i.ToolID{}, AllowedResourceConstraints: []json.RawMessage{json.RawMessage(`{"limit":1}`)}}, Lineage: ec.Lineage{DispatchID: dispatch}, Meta: f.CommandMeta{RequestID: testID[f.Request](t, 221), IdempotencyKey: f.IdempotencyKey("scheduler_dispatch:" + dispatch)}}
	if r.Validate() != nil {
		t.Fatal("invalid original rejection fixture")
	}
	// This public producer still classifies only the original unsupported
	// policy. Adding a final reason does not let it issue exhaustion evidence.
	err := RejectTaskResourceConstraints(r)
	if reason, ok := MatchTaskLaunchFailure(err, r); !ok || reason != TaskLaunchFailureUnsupportedResourceConstraints {
		t.Fatal("existing rejection changed kind")
	}
	for _, code := range []f.Code{f.InternalError, f.DependencyUnavailable, f.CommitUnknown, f.AgentBusy} {
		fault := f.NewFault(code, f.NotCommitted)
		fault.RetryHint = "retry"
		if _, ok := MatchTaskLaunchFailure(fault, r); ok {
			t.Fatal("code or hint became final-failure proof")
		}
	}
	if string(TaskLaunchFailureRetryExhausted) != "launch_retry_exhausted_v1" || string(TaskLaunchFailureUnsupportedResourceConstraints) != "unsupported_resource_constraints_v1" {
		t.Fatal("persistent reason changed")
	}
}
