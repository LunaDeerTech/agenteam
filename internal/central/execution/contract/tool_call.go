package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// ToolCallRoundSchemaVersion is deliberately distinct from the original
// no-tools round. Constructing a candidate grants no Model or Tool authority.
const ToolCallRoundSchemaVersion f.Version = 2

type ToolCallRoundFields struct {
	Round DirectTextRoundFields `json:"round"`
	Tool  mc.Tool               `json:"tool"`
}

func (v ToolCallRoundFields) Clone() ToolCallRoundFields {
	v.Round = v.Round.Clone()
	v.Tool = v.Tool.Clone()
	return v
}
func (v ToolCallRoundFields) Validate() error {
	if v.Round.Input.SchemaVersion != ToolCallRoundSchemaVersion || v.Tool.Validate() != nil {
		return invalid()
	}
	// Reuse the canonical identity and two-message shape checks on a local
	// copy, never admitting the version-2 input as a version-1 request.
	base := v.Round.Clone()
	base.Input.SchemaVersion = DirectTextSchemaVersion
	if base.Validate() != nil {
		return invalid()
	}
	digest, err := ToolCallInputDigest(context.Background(), v.Round.Messages, v.Tool)
	if err != nil || digest != v.Round.Input.Digest {
		return invalid()
	}
	return nil
}

// ToolCallInputDigest binds the complete first-round input, including the
// exact schema and required choice. It is not Model's format-1 text digest.
func ToolCallInputDigest(ctx context.Context, messages []mc.Message, tool mc.Tool) (f.Digest, error) {
	return mc.ToolCallInputDigest(ctx, messages, tool)
}

type ToolCallRound struct {
	data func() (ToolCallRoundFields, []byte, f.Digest)
}

func NewToolCallRound(v ToolCallRoundFields) (ToolCallRound, error) {
	if v.Validate() != nil {
		return ToolCallRound{}, invalid()
	}
	v = v.Clone()
	raw, err := json.Marshal(struct {
		Schema f.Version           `json:"schema_version"`
		Fields ToolCallRoundFields `json:"fields"`
	}{ToolCallRoundSchemaVersion, v})
	if err != nil || len(raw) > MaxDirectTextRoundBytes {
		return ToolCallRound{}, invalid()
	}
	raw, err = cursor.CanonicalJSON(raw)
	if err != nil || len(raw) > MaxDirectTextRoundBytes {
		return ToolCallRound{}, invalid()
	}
	digest := TriggerInputDigest(raw)
	return ToolCallRound{func() (ToolCallRoundFields, []byte, f.Digest) { return v.Clone(), bytes.Clone(raw), digest }}, nil
}
func (v ToolCallRound) Validate() error {
	if v.data == nil {
		return invalid()
	}
	return nil
}
func (v ToolCallRound) Fields() ToolCallRoundFields {
	if v.data == nil {
		return ToolCallRoundFields{}
	}
	f, _, _ := v.data()
	return f
}
func (v ToolCallRound) CanonicalBytes() []byte {
	if v.data == nil {
		return nil
	}
	_, raw, _ := v.data()
	return raw
}
func (v ToolCallRound) Digest() f.Digest {
	if v.data == nil {
		return ""
	}
	_, _, digest := v.data()
	return digest
}
func DecodeToolCallRound(raw []byte) (ToolCallRound, error) {
	var w struct {
		Schema f.Version           `json:"schema_version"`
		Fields ToolCallRoundFields `json:"fields"`
	}
	if len(raw) > MaxDirectTextRoundBytes || directTextDecode(raw, &w) != nil || w.Schema != ToolCallRoundSchemaVersion {
		return ToolCallRound{}, invalid()
	}
	v, err := NewToolCallRound(w.Fields)
	if err != nil || !bytes.Equal(v.CanonicalBytes(), raw) {
		return ToolCallRound{}, invalid()
	}
	return v, nil
}
func (ToolCallRound) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_tool_call_round")
}
func (ToolCallRound) LogValue() slog.Value         { return slog.StringValue("execution_tool_call_round") }
func (ToolCallRound) MarshalJSON() ([]byte, error) { return []byte(`"execution_tool_call_round"`), nil }
func (*ToolCallRound) UnmarshalJSON([]byte) error  { return invalid() }
