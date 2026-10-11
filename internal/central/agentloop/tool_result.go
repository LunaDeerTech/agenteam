package agentloop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// ToolResultProjection owns a validated model-visible message and its canonical
// encoding. It proves neither durable Tool facts nor current authorization;
// those remain the real Tool reader's responsibility in the caller's transaction.
// Explicit access includes file content. Default formatting and JSON do not.
type ToolResultProjection struct {
	data *toolResultProjectionData
}

type toolResultProjectionData struct {
	message   mc.Message
	canonical []byte
	digest    f.Digest
}

func (v ToolResultProjection) Validate() error {
	if v.data == nil {
		return invalid()
	}
	return nil
}

func (v ToolResultProjection) Message() mc.Message {
	if v.data == nil {
		return mc.Message{}
	}
	return v.data.message.Clone()
}

func (v ToolResultProjection) CanonicalBytes() []byte {
	if v.data == nil {
		return nil
	}
	return bytes.Clone(v.data.canonical)
}

func (v ToolResultProjection) Digest() f.Digest {
	if v.data == nil {
		return ""
	}
	return v.data.digest
}

func (ToolResultProjection) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "agent_tool_result_projection")
}
func (ToolResultProjection) LogValue() slog.Value {
	return slog.StringValue("agent_tool_result_projection")
}
func (ToolResultProjection) MarshalJSON() ([]byte, error) {
	return []byte(`"agent_tool_result_projection"`), nil
}
func (*ToolResultProjection) UnmarshalJSON([]byte) error { return invalid() }

// ProjectReadFileToolResult accepts only the original successful read-file
// result lent by the Tool reader. It retains no borrowed slice. The reader must
// establish the binding and bytes from current, protected facts; a caller-made
// binding is not an authority grant. This pure projection performs no Model
// invocation, next-round assembly, transcript write or reference retirement.
func ProjectReadFileToolResult(ctx context.Context, binding tc.ReadFileResultBinding, raw []byte) (ToolResultProjection, error) {
	var zero ToolResultProjection
	if err := contextError(ctx); err != nil {
		return zero, err
	}
	if binding.Validate() != nil || int64(len(raw)) != int64(binding.ResultBytes) || toolResultDigest(raw) != binding.ResultDigest {
		return zero, invalid()
	}
	result, err := p.DecodeReadFileResult(raw)
	if err != nil {
		return zero, invalid()
	}
	encoded, err := p.EncodeReadFileResult(result)
	if err != nil {
		return zero, invalid()
	}
	defer clear(encoded)
	if !bytes.Equal(encoded, raw) {
		return zero, invalid()
	}
	// Do not MarshalJSON(result): the protocol's default representation is
	// intentionally redacted. Its original six-field JSON is the sole text part.
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
	if err := contextError(ctx); err != nil {
		clear(canonical)
		return zero, err
	}
	return ToolResultProjection{data: &toolResultProjectionData{
		message: message, canonical: canonical, digest: toolResultDigest(canonical),
	}}, nil
}

func toolResultDigest(raw []byte) f.Digest {
	sum := sha256.Sum256(raw)
	return f.Digest(hex.EncodeToString(sum[:]))
}
