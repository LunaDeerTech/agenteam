package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
)

const RunnerReadFileKey = "runner:read-file"

// RunnerBinding names a protocol contract, not an online Runner or dispatch grant.
type RunnerBinding struct {
	OperationName     string `json:"operation_name"`
	OperationRevision string `json:"operation_revision"`
	ProtocolMajor     uint16 `json:"protocol_major"`
}

func (v RunnerBinding) Validate() error {
	if v.OperationName != "read-file" || v.OperationRevision != "1" || v.ProtocolMajor != 1 {
		return invalidRegistry()
	}
	return nil
}

func validBindingChoice(b BuiltinBinding, r RunnerBinding) bool {
	if r == (RunnerBinding{}) {
		return stablePart(b.HandlerID) && b.ContractRevision.Validate() == nil
	}
	return b == (BuiltinBinding{}) && r.Validate() == nil
}

type RunnerRegistration struct {
	Definition       Definition
	Binding          RunnerBinding
	ScopeResolverID  ScopeResolverID
	RiskClassifierID RiskClassifierID
	Class            ToolClass
	Active           bool
}

// ReadFileDefinition is fixed metadata. The path validator in runnerprotocol
// remains authoritative for relative paths and the exact int64 offset bound.
func ReadFileDefinition() Definition {
	return Definition{
		StableKey: RunnerReadFileKey, Name: "read-file",
		Description:  "Read a bounded range from a file in an allowed mount.",
		InputSchema:  json.RawMessage(`{"type":"object","additionalProperties":false,"required":["mount_id","path","offset","length","encoding"],"properties":{"mount_id":{"type":"string","pattern":"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"},"path":{"type":"string","minLength":1,"maxLength":4096},"offset":{"type":"string","pattern":"^(0|[1-9][0-9]{0,18})$"},"length":{"type":"integer","minimum":1,"maximum":65536},"encoding":{"type":"string","enum":["utf8","base64"]}}}`),
		OutputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["path","offset","bytes_read","eof","encoding","data"],"properties":{"path":{"type":"string"},"offset":{"type":"string","pattern":"^(0|[1-9][0-9]{0,18})$"},"bytes_read":{"type":"integer","minimum":0,"maximum":65536},"eof":{"type":"boolean"},"encoding":{"type":"string","enum":["utf8","base64"]},"data":{"type":"string"}}}`),
		Annotations:  &Annotations{ReadOnly: true, Idempotency: Inherent},
	}
}

func ReadFileRegistration() RunnerRegistration {
	return RunnerRegistration{Definition: ReadFileDefinition(), Binding: RunnerBinding{"read-file", "1", 1}, ScopeResolverID: "runner.read-file", RiskClassifierID: "runner.read-file", Class: OrdinaryTool, Active: true}
}

func (v RunnerRegistration) Validate(ctx context.Context) error {
	if ctx == nil || v.Binding.Validate() != nil || v.ScopeResolverID != "runner.read-file" || v.RiskClassifierID != "runner.read-file" || v.Class != OrdinaryTool {
		return invalidRegistry()
	}
	a, err := CanonicalDefinition(ctx, v.Definition)
	if err != nil {
		return err
	}
	b, err := CanonicalDefinition(ctx, ReadFileDefinition())
	if err != nil {
		return err
	}
	if !bytes.Equal(a, b) {
		return invalidRegistry()
	}
	return ctx.Err()
}

func (RunnerRegistration) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "runner_tool_registration")
}
func (RunnerRegistration) LogValue() slog.Value { return slog.StringValue("runner_tool_registration") }
func (RunnerRegistration) MarshalJSON() ([]byte, error) {
	return []byte(`"runner_tool_registration"`), nil
}
