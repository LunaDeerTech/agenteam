package agentloop

import (
	"bytes"
	"context"

	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// ProjectRunCommandToolResult preserves the complete original report and both
// streams. Wire completion is not Tool success: the shared Tool classifier
// must agree with the persisted binding before a model-visible result exists.
// This pure projection grants no authority and starts no Model or Runner call.
func ProjectRunCommandToolResult(ctx context.Context, binding tc.RunCommandResultBinding, raw []byte) (ToolResultProjection, error) {
	var zero ToolResultProjection
	if err := contextError(ctx); err != nil {
		return zero, err
	}
	if binding.Validate() != nil || int64(len(raw)) != int64(binding.ResultBytes) || toolResultDigest(raw) != binding.ResultDigest {
		return zero, invalid()
	}
	bundle, err := tc.DecodeRunCommandResultBundle(ctx, raw)
	if err != nil {
		return zero, invalid()
	}
	defer clear(bundle.Stdout)
	defer clear(bundle.Stderr)
	outcome, code := tc.ClassifyRunCommandResult(bundle.Report)
	if outcome != binding.Outcome || code != binding.Code {
		return zero, invalid()
	}
	encoded, err := tc.EncodeRunCommandResultBundle(bundle)
	if err != nil {
		return zero, invalid()
	}
	defer clear(encoded)
	if !bytes.Equal(encoded, raw) {
		return zero, invalid()
	}
	// The explicit canonical envelope, not the bundle's redacted default JSON,
	// is the sole text. Known cancellation or incomplete output retains all its
	// facts with IsError true; it is never replaced with a success or summary.
	message := mc.Message{Role: "tool", Parts: []mc.MessagePart{{ToolResult: &mc.ToolResultPart{
		CallID: binding.Call.CallID, IsError: outcome != "success",
		Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: string(raw)}}},
	}}}}
	if message.Validate() != nil {
		return zero, invalid()
	}
	canonical, err := projectionJSON(message)
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
