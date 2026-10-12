package contract

import "encoding/json"

// WriteFileDefinition describes the bounded wire operation. It grants no
// permission, implies no durable filesystem commit and promises no retry safety.
func WriteFileDefinition() Definition {
	return Definition{
		StableKey: RunnerWriteFileKey, Name: "write-file",
		Description:  "Atomically publish a bounded file in an allowed mount.",
		InputSchema:  json.RawMessage(`{"type":"object","additionalProperties":false,"required":["mount_id","path","data_base64","overwrite"],"properties":{"mount_id":{"type":"string","pattern":"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"},"path":{"type":"string","minLength":1,"maxLength":4096},"data_base64":{"type":"string","maxLength":699052},"overwrite":{"type":"boolean"}}}`),
		OutputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["path","state","bytes_written","durable"],"properties":{"path":{"type":"string"},"state":{"type":"string","enum":["committed"]},"bytes_written":{"type":"integer","minimum":0,"maximum":524288},"durable":{"type":"boolean"},"completion_code":{"type":"string","enum":["INTERNAL_ERROR","CANCELLED","TIMEOUT"]}}}`),
		Annotations:  &Annotations{ReadOnly: false, Destructive: true, Idempotency: None},
	}
}

func WriteFileRegistration() RunnerRegistration {
	return RunnerRegistration{Definition: WriteFileDefinition(), Binding: RunnerBinding{"write-file", "1", 1}, ScopeResolverID: "runner.write-file", RiskClassifierID: "runner.write-file", Class: OrdinaryTool, Active: true}
}
