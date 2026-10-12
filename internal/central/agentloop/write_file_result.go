package agentloop

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// ProjectWriteFileToolResult projects the original known publication receipt,
// not the protected file bytes. Only the real historical Tool reader can prove
// its durable binding; this pure codec neither grants dispatch nor calls Model.
// A late completion code or unconfirmed durability does not undo publication:
// the original complete receipt is preserved with IsError false.
func ProjectWriteFileToolResult(ctx context.Context, binding tc.WriteFileResultBinding, raw []byte) (ToolResultProjection, error) {
	var zero ToolResultProjection
	if err := contextError(ctx); err != nil {
		return zero, err
	}
	if binding.Validate() != nil || int64(len(raw)) != int64(binding.ResultBytes) || toolResultDigest(raw) != binding.ResultDigest {
		return zero, invalid()
	}
	result, err := p.DecodeWriteFileResult(raw)
	if err != nil {
		return zero, invalid()
	}
	encoded, err := p.EncodeWriteFileResult(result)
	if err != nil {
		return zero, invalid()
	}
	defer clear(encoded)
	if !bytes.Equal(encoded, raw) {
		return zero, invalid()
	}
	// The protocol's default JSON is redacted. Use the exact, explicitly encoded
	// receipt; never reconstruct a success from durability or completion flags.
	message := mc.Message{Role: "tool", Parts: []mc.MessagePart{{ToolResult: &mc.ToolResultPart{
		CallID: binding.Call.CallID, IsError: false,
		Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: string(raw)}}},
	}}}}
	if message.Validate() != nil {
		return zero, invalid()
	}
	messageJSON, err := json.Marshal(message)
	if err != nil {
		return zero, invalid()
	}
	defer clear(messageJSON)
	canonical, err := cursor.CanonicalJSON(messageJSON)
	if err != nil {
		return zero, invalid()
	}
	if err = contextError(ctx); err != nil {
		clear(canonical)
		return zero, err
	}
	return ToolResultProjection{data: &toolResultProjectionData{
		message: message, canonical: canonical, digest: toolResultDigest(canonical),
	}}, nil
}
