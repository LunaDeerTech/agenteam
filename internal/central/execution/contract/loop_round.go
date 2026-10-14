package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// LoopRoundSchemaVersion versions the stored round, independently of Model's
// input schema (4 for the initial request, 3 for its successors).
const LoopRoundSchemaVersion f.Version = 4

type LoopRoundFields struct {
	Round           DirectTextRoundFields `json:"round"`
	Tools           []mc.Tool             `json:"tools"`
	Choice          mc.ToolChoice         `json:"tool_choice"`
	Ordinal         f.Sequence            `json:"ordinal"`
	TranscriptStart f.Sequence            `json:"transcript_start"`
	Predecessor     *RoundID              `json:"predecessor"`
}

func (v LoopRoundFields) Clone() LoopRoundFields {
	v.Round = v.Round.Clone()
	v.Tools = append([]mc.Tool(nil), v.Tools...)
	for n := range v.Tools {
		v.Tools[n] = v.Tools[n].Clone()
	}
	if v.Predecessor != nil {
		p := *v.Predecessor
		v.Predecessor = &p
	}
	return v
}

func (v LoopRoundFields) Validate() error {
	r := v.Round
	if r.ID.Validate() != nil || r.InputBindingID.Validate() != nil || r.ExecutionID.Validate() != nil || r.SnapshotID.Validate() != nil || r.StartID.Validate() != nil || r.CallID.Validate() != nil || r.ContextDigest.Validate() != nil || r.Input.Validate() != nil || r.Input.ExecutionID == nil || *r.Input.ExecutionID != r.ExecutionID || r.Input.RoundID != r.ID.String() || r.CreatedAt.Validate() != nil || v.Ordinal < 1 || v.TranscriptStart < 1 || len(r.Messages) < 2 || len(r.Messages) > 256 || len(v.Tools) < 1 || len(v.Tools) > 128 {
		return invalid()
	}
	if v.Ordinal == 1 {
		if v.Predecessor != nil || v.TranscriptStart != 1 || r.Input.SchemaVersion != 4 || v.Choice != (mc.ToolChoice{Kind: "required"}) {
			return invalid()
		}
	} else if v.Predecessor == nil || v.Predecessor.Validate() != nil || *v.Predecessor == r.ID || r.Input.SchemaVersion != mc.SuccessorInputSchemaVersion || v.Choice != (mc.ToolChoice{Kind: "auto"}) {
		return invalid()
	}
	names := make(map[string]bool, len(v.Tools))
	for _, tool := range v.Tools {
		if tool.Validate() != nil || names[tool.Name] {
			return invalid()
		}
		names[tool.Name] = true
	}
	for _, message := range r.Messages {
		if message.Validate() != nil {
			return invalid()
		}
	}
	return nil
}

type LoopRound struct {
	data func() (LoopRoundFields, []byte, f.Digest)
}

func NewLoopRound(v LoopRoundFields) (LoopRound, error) {
	if v.Validate() != nil {
		return LoopRound{}, invalid()
	}
	v = v.Clone()
	raw, err := json.Marshal(struct {
		Schema f.Version       `json:"schema_version"`
		Fields LoopRoundFields `json:"fields"`
	}{LoopRoundSchemaVersion, v})
	if err != nil || len(raw) > MaxDirectTextRoundBytes {
		return LoopRound{}, invalid()
	}
	raw, err = cursor.CanonicalJSON(raw)
	if err != nil || len(raw) > MaxDirectTextRoundBytes {
		return LoopRound{}, invalid()
	}
	digest := TriggerInputDigest(raw)
	return LoopRound{func() (LoopRoundFields, []byte, f.Digest) { return v.Clone(), bytes.Clone(raw), digest }}, nil
}
func (v LoopRound) Validate() error {
	if v.data == nil {
		return invalid()
	}
	return nil
}
func (v LoopRound) Fields() LoopRoundFields {
	if v.data == nil {
		return LoopRoundFields{}
	}
	x, _, _ := v.data()
	return x
}
func (v LoopRound) CanonicalBytes() []byte {
	if v.data == nil {
		return nil
	}
	_, x, _ := v.data()
	return x
}
func (v LoopRound) Digest() f.Digest {
	if v.data == nil {
		return ""
	}
	_, _, x := v.data()
	return x
}
func DecodeLoopRound(raw []byte) (LoopRound, error) {
	var wire struct {
		Schema f.Version       `json:"schema_version"`
		Fields LoopRoundFields `json:"fields"`
	}
	if len(raw) > MaxDirectTextRoundBytes || directTextDecode(raw, &wire) != nil || wire.Schema != LoopRoundSchemaVersion {
		return LoopRound{}, invalid()
	}
	v, err := NewLoopRound(wire.Fields)
	if err != nil || !bytes.Equal(v.CanonicalBytes(), raw) {
		return LoopRound{}, invalid()
	}
	return v, nil
}
func (LoopRound) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "execution_loop_round") }
func (LoopRound) LogValue() slog.Value         { return slog.StringValue("execution_loop_round") }
func (LoopRound) MarshalJSON() ([]byte, error) { return []byte(`"execution_loop_round"`), nil }

func (*LoopRound) UnmarshalJSON([]byte) error { return invalid() }
