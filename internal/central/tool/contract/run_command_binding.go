package contract

import "encoding/json"

// RunCommandRisk is the fixed governance classification. The classifier ID
// runner.run-command names this classification, not a safe-command whitelist.
// This metadata does not implement or bypass default/auto Approval handling.
const RunCommandRisk = "arbitrary_execution"

// RunCommandDefinition describes direct argv invocation. Nonzero exit, signals,
// cancellation and local I/O completion remain explicit report facts. Schema
// limits do not replace the stricter byte and cross-field protocol checks.
func RunCommandDefinition() Definition {
	return Definition{
		StableKey: RunnerRunCommandKey, Name: "run-command",
		Description:  "Run explicit argv in an allowed mount and retain its completed invocation report and separate output streams.",
		InputSchema:  json.RawMessage(`{"type":"object","additionalProperties":false,"required":["mount_id","mode","program","args","cwd"],"properties":{"mount_id":{"type":"string","pattern":"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"},"mode":{"type":"string","enum":["argv"]},"program":{"type":"string","minLength":1,"maxLength":4096},"args":{"type":"array","maxItems":256,"items":{"type":"string","maxLength":65536}},"cwd":{"type":"string","minLength":1,"maxLength":4096}}}`),
		OutputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["report","stdout","stderr"],"properties":{"report":{"type":"object","additionalProperties":false,"required":["state","exit_code","signal","cancelled","timed_out","duration_ms","stdout_bytes","stderr_bytes","stdout_truncated","stderr_truncated","output_complete","process_group_termination_confirmed"],"properties":{"state":{"type":"string","enum":["completed"]},"exit_code":{"type":"integer","minimum":-1,"maximum":255},"signal":{"type":"integer","minimum":0},"cancelled":{"type":"boolean"},"timed_out":{"type":"boolean"},"duration_ms":{"type":"string","pattern":"^(0|[1-9][0-9]{0,18})$"},"stdout_bytes":{"type":"string","pattern":"^(0|[1-9][0-9]{0,4})$"},"stderr_bytes":{"type":"string","pattern":"^(0|[1-9][0-9]{0,4})$"},"stdout_truncated":{"type":"boolean"},"stderr_truncated":{"type":"boolean"},"output_complete":{"type":"boolean"},"process_group_termination_confirmed":{"type":"boolean"},"completion_code":{"type":"string","enum":["INTERNAL_ERROR","CANCELLED","TIMEOUT"]}}},"stdout":{"type":"string","maxLength":65536},"stderr":{"type":"string","maxLength":65536}}}`),
		Annotations:  &Annotations{ReadOnly: false, Destructive: true, Idempotency: None},
	}
}
func RunCommandRegistration() RunnerRegistration {
	return RunnerRegistration{Definition: RunCommandDefinition(), Binding: RunnerBinding{"run-command", "1", 1}, ScopeResolverID: "runner.run-command", RiskClassifierID: "runner.run-command", Class: OrdinaryTool, Active: true}
}
