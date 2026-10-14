package builtin

import (
	"encoding/json"

	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

const (
	TaskReadName          = "read-task"
	TaskTransferName      = "transfer-task"
	TaskReadHandlerID     = "task.read"
	TaskTransferHandlerID = "task.transfer"
	TaskScopeID           = tc.ScopeResolverID("task.project.v1")
	TaskReadRiskID        = tc.RiskClassifierID("task.read.v1")
	TaskTransferRiskID    = tc.RiskClassifierID("task.transfer.v1")
)

func TaskDefinitions() []tc.Definition {
	return []tc.Definition{
		{StableKey: "builtin:" + TaskReadName, Name: TaskReadName, Description: "Read the current canonical Task and its version before making a state transition. Reviewers must inspect the worker's actual Artifacts before accepting or returning the Task.", InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["task_id"],"properties":{"task_id":{"type":"string","format":"uuid"}}}`), OutputSchema: json.RawMessage(`{"type":"object"}`), Annotations: &tc.Annotations{ReadOnly: true, Idempotency: tc.Inherent}},
		{StableKey: "builtin:" + TaskTransferName, Name: TaskTransferName, Description: "Request a Task state transition using the current expected_version. Submit work to in_review with the reviewer assignee and a comment containing Artifact references. The current reviewer may accept to done, or return to todo with a worker assignee and actionable comments. Domain authorization and state rules decide whether the transition is allowed.", InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["task_id","expected_version","target_state"],"properties":{"task_id":{"type":"string","format":"uuid"},"expected_version":{"type":"string","pattern":"^[1-9][0-9]*$"},"target_state":{"type":"string","enum":["backlog","todo","in_progress","in_review","blocked","done","cancelled"]},"assignee_agent_id":{"type":"string","format":"uuid"},"comment":{"type":"string","minLength":1,"maxLength":32768},"add_blockers":{"type":"array","maxItems":16,"items":{"type":"object"}},"resolve_blocker_ids":{"type":"array","maxItems":16,"items":{"type":"string","format":"uuid"}}}}`), OutputSchema: json.RawMessage(`{"type":"object"}`), Annotations: &tc.Annotations{Idempotency: tc.Keyed}},
	}
}

func TaskDefinition(name string) (tc.Definition, error) {
	for _, definition := range TaskDefinitions() {
		if definition.Name == name {
			return definition, nil
		}
	}
	return tc.Definition{}, taskToolInvalid()
}
