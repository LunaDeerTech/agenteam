package execution

import (
	"context"
	"encoding/json"
	"log/slog"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// Private sum type: existing v1 values keep their exact codec and bytes.
type executionRound interface {
	Validate() error
	Fields() c.DirectTextRoundFields
	CanonicalBytes() []byte
	Digest() f.Digest
	LogValue() slog.Value
}
type toolExecutionRound struct{ c.ToolCallRound }

func (v toolExecutionRound) Fields() c.DirectTextRoundFields { return v.ToolCallRound.Fields().Round }
func (v toolExecutionRound) tool() mc.Tool                   { return v.ToolCallRound.Fields().Tool }
func decodeExecutionRound(raw []byte) (executionRound, error) {
	var tag struct {
		Schema f.Version `json:"schema_version"`
	}
	if json.Unmarshal(raw, &tag) != nil {
		return nil, invalid()
	}
	switch tag.Schema {
	case c.DirectTextSchemaVersion:
		return c.DecodeDirectTextRound(raw)
	case c.ToolCallRoundSchemaVersion:
		v, err := c.DecodeToolCallRound(raw)
		if err != nil {
			return nil, err
		}
		return toolExecutionRound{v}, nil
	default:
		return nil, invalid()
	}
}
func executionRoundDigest(round executionRound) (f.Digest, error) {
	if tool, ok := round.(toolExecutionRound); ok {
		return c.ToolCallInputDigest(context.Background(), tool.Fields().Messages, tool.tool())
	}
	return model.TextInputDigest(round.Fields().Messages)
}
