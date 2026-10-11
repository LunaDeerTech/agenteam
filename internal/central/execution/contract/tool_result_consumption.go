package contract

import (
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// ToolResultConsumptionReceipt describes an append-only, nonterminal boundary.
// It grants neither current access to the result nor permission to start a new
// Model turn. The original version-2 tool-pending Round remains immutable.
type ToolResultConsumptionReceipt struct {
	ExecutionID       i.ExecutionID  `json:"execution_id"`
	RoundID           f.ID[Round]    `json:"round_id"`
	CallID            string         `json:"call_id"`
	OperationID       tc.OperationID `json:"operation_id"`
	AttemptID         tc.AttemptID   `json:"attempt_id"`
	Version           f.Version      `json:"version"`
	TranscriptThrough f.Sequence     `json:"transcript_through"`
	MessageDigest     f.Digest       `json:"message_digest"`
	ResultDigest      f.Digest       `json:"result_digest"`
	CreatedAt         f.Instant      `json:"created_at"`
}

func (v ToolResultConsumptionReceipt) Validate() error {
	if v.ExecutionID.Validate() != nil || v.RoundID.Validate() != nil ||
		(mc.ToolResultPart{CallID: v.CallID}).Validate() != nil ||
		v.OperationID.Validate() != nil || v.AttemptID.Validate() != nil ||
		v.Version < 4 || v.TranscriptThrough != 3 ||
		v.MessageDigest.Validate() != nil || v.ResultDigest.Validate() != nil ||
		v.CreatedAt.Validate() != nil {
		return invalid()
	}
	return nil
}

func (ToolResultConsumptionReceipt) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_tool_result_consumption")
}
func (ToolResultConsumptionReceipt) LogValue() slog.Value {
	return slog.StringValue("execution_tool_result_consumption")
}
