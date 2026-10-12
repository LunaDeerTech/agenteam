package execution

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
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
	default:
		return agentloop.ToolResultProjection{}, invalid()
	}
}
