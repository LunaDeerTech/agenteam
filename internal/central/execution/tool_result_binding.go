package execution

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// Shared SQL columns, never a public authority or a read-as-write conversion.
// The original captured profile selects its own closed validator/projector.
type sealedToolResultBinding struct {
	Call           tc.ToolCallBinding
	OperationID    tc.OperationID
	AttemptID      tc.AttemptID
	RequestID      p.ID
	InputDigest    f.Digest
	Fingerprint    f.Digest
	TerminalDigest f.Digest
	Outcome        string
	Code           string
	Attempted      bool
	ResultDigest   f.Digest
	ResultBytes    f.Progress
}

func (b sealedToolResultBinding) validate() error {
	switch b.Call.RunnerBinding {
	case tc.ReadFileRegistration().Binding:
		return tc.ReadFileResultBinding(b).Validate()
	case tc.WriteFileRegistration().Binding:
		return tc.WriteFileResultBinding(b).Validate()
	case tc.RunCommandRegistration().Binding:
		return tc.RunCommandResultBinding(b).Validate()
	default:
		return invalid()
	}
}

func (b sealedToolResultBinding) project(ctx context.Context, raw []byte) (agentloop.ToolResultProjection, error) {
	switch b.Call.RunnerBinding {
	case tc.ReadFileRegistration().Binding:
		return agentloop.ProjectReadFileToolResult(ctx, tc.ReadFileResultBinding(b), raw)
	case tc.WriteFileRegistration().Binding:
		return agentloop.ProjectWriteFileToolResult(ctx, tc.WriteFileResultBinding(b), raw)
	case tc.RunCommandRegistration().Binding:
		return agentloop.ProjectRunCommandToolResult(ctx, tc.RunCommandResultBinding(b), raw)
	default:
		return agentloop.ToolResultProjection{}, invalid()
	}
}

// Command outcomes are sealed facts, never inferred as success by the 68
// reader. Reconstruct the complete original binding using the independently
// loaded columns and captured Call, then require byte equality with the sealed
// canonical object. This rejects missing/extra keys and substituted identities
// as well as an independently valid but mismatched outcome/code.
func commandSealedBinding(expected sealedToolResultBinding, raw []byte) (sealedToolResultBinding, error) {
	if expected.Call.RunnerBinding != tc.RunCommandRegistration().Binding || len(raw) < 2 || len(raw) > 32768 {
		return sealedToolResultBinding{}, unavailable(nil)
	}
	var state struct {
		Outcome   string `json:"outcome"`
		Code      string `json:"code"`
		Attempted bool   `json:"attempted"`
	}
	if json.Unmarshal(raw, &state) != nil {
		return sealedToolResultBinding{}, unavailable(nil)
	}
	expected.Outcome, expected.Code, expected.Attempted = state.Outcome, state.Code, state.Attempted
	canonical, err := sealedToolResultBindingJSON(expected)
	if err != nil || !bytes.Equal(canonical, raw) {
		return sealedToolResultBinding{}, unavailable(nil)
	}
	return expected, nil
}

func loadCommandSealedBinding(ctx context.Context, x postgres.SQLExecutor, expected sealedToolResultBinding) (sealedToolResultBinding, error) {
	var raw []byte
	var digest string
	if err := x.QueryRow(ctx, `SELECT binding,binding_digest FROM agenteam_execution.tool_result_consumptions WHERE execution_id=$1`, expected.Call.ExecutionID.String()).Scan(&raw, &digest); err != nil {
		return sealedToolResultBinding{}, unavailable(err)
	}
	if string(c.TriggerInputDigest(raw)) != digest {
		return sealedToolResultBinding{}, unavailable(nil)
	}
	return commandSealedBinding(expected, raw)
}
