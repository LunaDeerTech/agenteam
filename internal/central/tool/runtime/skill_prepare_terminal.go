package runtime

import (
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func skillPrepareResultBinding(call tc.ToolCallBinding) bool {
	return call.Binding == (tc.BuiltinBinding{HandlerID: "skill.prepare", ContractRevision: 1}) && call.RunnerBinding == (tc.RunnerBinding{}) && call.MCPBinding == (tc.MCPBinding{})
}

// These envelopes describe an already returned, joined preparation attempt.
// Missing Runner replies and uncertain mutations remain errors/pending calls;
// they cannot acquire a terminal merely by selecting an outcome string.
// Ordinary success continues through the registered output schema.
func skillPrepareReturnedTerminal(body []byte) (outcome, code string, special bool, err error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		return "", "", false, fail(f.InvalidState)
	}
	if _, ok := fields["outcome"]; !ok {
		return "success", "", false, nil
	}
	if _, err := sc.DecodePrepareUnknownResult(body); err == nil {
		return "unknown", string(f.DependencyUnavailable), true, nil
	}
	if _, err := sc.DecodePrepareUnissuedResult(body); err == nil {
		return "failure", string(f.DependencyUnavailable), true, nil
	}
	return "", "", false, fail(f.InvalidState)
}
