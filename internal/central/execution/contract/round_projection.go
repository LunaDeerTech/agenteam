package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// RoundProjectionReader lends verified, already committed Execution facts.
// consume runs synchronously inside the original read transaction and may only
// perform pure computation: it must not call a Model, Runner, registry or other
// domain, or retain the borrowed byte slices. Its candidate is tentative until
// this method actually returns nil. Cancellation, rollback and Unknown must not
// publish a candidate, even when consume has already returned successfully.
// The input IDs select historical facts; this interface grants no execution,
// current resource or Tool dispatch authority.
type RoundProjectionReader interface {
	WithCommittedRoundProjection(context.Context, i.ExecutionID, RoundID, func(RoundProjectionSource) error) error
}

// RoundProjectionSource is a callback-only loan, not caller-supplied evidence of
// commitment. Arguments preserves the original protected Model argument bytes;
// canonical Transcript messages alone cannot prove their original spelling.
// Result.Call binds those bytes and their independently checked canonical form.
// Input contains the fixed final Skill directory and optional typed control;
// ControlCanonical is nil exactly when there is no committed control.
type RoundProjectionSource struct {
	SchemaVersion    f.Version
	Input            RoundInput
	Snapshot         DirectTextSnapshot
	Predecessor      ToolCallRound
	Result           tc.ReadFileResultBinding
	Arguments        []byte
	Transcript       []RoundProjectionEntry
	ControlCanonical []byte
}

type RoundProjectionEntry struct {
	Sequence         f.Sequence
	RoundID          RoundID
	Kind             string
	CanonicalMessage []byte
	Digest           f.Digest
}

func (RoundProjectionSource) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_round_projection_source")
}
func (RoundProjectionSource) LogValue() slog.Value {
	return slog.StringValue("execution_round_projection_source")
}
func (RoundProjectionSource) MarshalJSON() ([]byte, error) {
	return []byte(`"execution_round_projection_source"`), nil
}
func (RoundProjectionEntry) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_round_projection_entry")
}
func (RoundProjectionEntry) LogValue() slog.Value {
	return slog.StringValue("execution_round_projection_entry")
}
func (RoundProjectionEntry) MarshalJSON() ([]byte, error) {
	return []byte(`"execution_round_projection_entry"`), nil
}
