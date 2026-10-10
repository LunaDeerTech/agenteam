package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

// DirectTextStart is the identity of one Execution-owned startup candidate.
// It is neither a database transaction-attempt ID nor a runtime permission.
type DirectTextStart struct{}
type DirectTextStartID = f.ID[DirectTextStart]

const DirectTextSchemaVersion f.Version = 1
const MaxDirectTextRoundBytes = 16 << 20

type DirectTextSnapshotFields struct {
	ID        SnapshotID
	StartID   DirectTextStartID
	ProcessID oc.ProcessID
	Context   ExecutionContext
	CreatedAt f.Instant
}

// Construction encodes a candidate. Only the original Execution writer can
// seal it together with running and Started in one transaction.
type DirectTextSnapshot struct {
	data func() (DirectTextSnapshotFields, []byte, f.Digest)
}

type directTextSnapshotWire struct {
	Schema    f.Version         `json:"schema_version"`
	ID        SnapshotID        `json:"snapshot_id"`
	StartID   DirectTextStartID `json:"start_id"`
	ProcessID oc.ProcessID      `json:"process_id"`
	Context   json.RawMessage   `json:"context"`
	CreatedAt f.Instant         `json:"created_at"`
}

func NewDirectTextSnapshot(v DirectTextSnapshotFields) (DirectTextSnapshot, error) {
	if v.ID.Validate() != nil || v.StartID.Validate() != nil || v.ProcessID.Validate() != nil || v.Context.Validate() != nil || v.CreatedAt.Validate() != nil || v.CreatedAt.Time().Before(v.Context.Input().Fields().CapturedAt.Time()) {
		return DirectTextSnapshot{}, invalid()
	}
	raw, err := json.Marshal(directTextSnapshotWire{DirectTextSchemaVersion, v.ID, v.StartID, v.ProcessID, v.Context.CanonicalBytes(), v.CreatedAt})
	if err != nil || len(raw) > MaxExecutionContextBytes+4096 {
		return DirectTextSnapshot{}, invalid()
	}
	raw, err = cursor.CanonicalJSON(raw)
	if err != nil {
		return DirectTextSnapshot{}, invalid()
	}
	digest := TriggerInputDigest(raw)
	return DirectTextSnapshot{func() (DirectTextSnapshotFields, []byte, f.Digest) { return v, bytes.Clone(raw), digest }}, nil
}
func (v DirectTextSnapshot) Validate() error {
	if v.data == nil {
		return invalid()
	}
	return nil
}
func (v DirectTextSnapshot) Fields() DirectTextSnapshotFields {
	if v.data == nil {
		return DirectTextSnapshotFields{}
	}
	f, _, _ := v.data()
	return f
}
func (v DirectTextSnapshot) CanonicalBytes() []byte {
	if v.data == nil {
		return nil
	}
	_, raw, _ := v.data()
	return raw
}
func (v DirectTextSnapshot) Digest() f.Digest {
	if v.data == nil {
		return ""
	}
	_, _, digest := v.data()
	return digest
}
func DecodeDirectTextSnapshot(raw []byte) (DirectTextSnapshot, error) {
	var w directTextSnapshotWire
	if len(raw) > MaxExecutionContextBytes+4096 || directTextDecode(raw, &w) != nil || w.Schema != DirectTextSchemaVersion {
		return DirectTextSnapshot{}, invalid()
	}
	context, err := DecodeExecutionContext(w.Context)
	if err != nil {
		return DirectTextSnapshot{}, invalid()
	}
	v, err := NewDirectTextSnapshot(DirectTextSnapshotFields{w.ID, w.StartID, w.ProcessID, context, w.CreatedAt})
	if err != nil || !bytes.Equal(v.CanonicalBytes(), raw) {
		return DirectTextSnapshot{}, invalid()
	}
	return v, nil
}
func (DirectTextSnapshot) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "execution_snapshot") }
func (DirectTextSnapshot) LogValue() slog.Value         { return slog.StringValue("execution_snapshot") }
func (DirectTextSnapshot) MarshalJSON() ([]byte, error) { return []byte(`"execution_snapshot"`), nil }
func (*DirectTextSnapshot) UnmarshalJSON([]byte) error  { return invalid() }

// Round fields contain the exact model-visible input, never a mutable source
// pointer or credential material. Model/lease identity comes from Snapshot.
// The writer separately recomputes Model's format-1 TextInputDigest and binds
// these bytes to the actual Loop projection before committing the first round.
type DirectTextRoundFields struct {
	ID             RoundID           `json:"round_id"`
	InputBindingID InputBindingID    `json:"input_binding_id"`
	ExecutionID    i.ExecutionID     `json:"execution_id"`
	SnapshotID     SnapshotID        `json:"snapshot_id"`
	StartID        DirectTextStartID `json:"start_id"`
	CallID         mc.CallID         `json:"call_id"`
	ContextDigest  f.Digest          `json:"context_digest"`
	Input          mc.InputIdentity  `json:"input"`
	Messages       []mc.Message      `json:"messages"`
	CreatedAt      f.Instant         `json:"created_at"`
}

func (v DirectTextRoundFields) Clone() DirectTextRoundFields {
	v.Input = v.Input.Clone()
	v.Messages = append([]mc.Message(nil), v.Messages...)
	for n := range v.Messages {
		v.Messages[n] = v.Messages[n].Clone()
	}
	return v
}
func (v DirectTextRoundFields) Validate() error {
	if v.ID.Validate() != nil || v.InputBindingID.Validate() != nil || v.ExecutionID.Validate() != nil || v.SnapshotID.Validate() != nil || v.StartID.Validate() != nil || v.CallID.Validate() != nil || v.ContextDigest.Validate() != nil || v.Input.Validate() != nil || v.Input.ExecutionID == nil || *v.Input.ExecutionID != v.ExecutionID || v.Input.RoundID != v.ID.String() || v.Input.SchemaVersion != 1 || v.CreatedAt.Validate() != nil || len(v.Messages) != 2 {
		return invalid()
	}
	for n, m := range v.Messages {
		role := "system"
		if n == 1 {
			role = "user"
		}
		if m.Validate() != nil || m.Role != role || len(m.Parts) != 1 || m.Parts[0].Text == nil {
			return invalid()
		}
	}
	return nil
}

type DirectTextRound struct {
	data func() (DirectTextRoundFields, []byte, f.Digest)
}

func NewDirectTextRound(v DirectTextRoundFields) (DirectTextRound, error) {
	if v.Validate() != nil {
		return DirectTextRound{}, invalid()
	}
	v = v.Clone()
	raw, err := json.Marshal(struct {
		Schema f.Version             `json:"schema_version"`
		Fields DirectTextRoundFields `json:"fields"`
	}{DirectTextSchemaVersion, v})
	if err != nil || len(raw) > MaxDirectTextRoundBytes {
		return DirectTextRound{}, invalid()
	}
	raw, err = cursor.CanonicalJSON(raw)
	if err != nil {
		return DirectTextRound{}, invalid()
	}
	digest := TriggerInputDigest(raw)
	return DirectTextRound{func() (DirectTextRoundFields, []byte, f.Digest) { return v.Clone(), bytes.Clone(raw), digest }}, nil
}
func (v DirectTextRound) Validate() error {
	if v.data == nil {
		return invalid()
	}
	return nil
}
func (v DirectTextRound) Fields() DirectTextRoundFields {
	if v.data == nil {
		return DirectTextRoundFields{}
	}
	f, _, _ := v.data()
	return f
}
func (v DirectTextRound) CanonicalBytes() []byte {
	if v.data == nil {
		return nil
	}
	_, b, _ := v.data()
	return b
}
func (v DirectTextRound) Digest() f.Digest {
	if v.data == nil {
		return ""
	}
	_, _, d := v.data()
	return d
}
func DecodeDirectTextRound(raw []byte) (DirectTextRound, error) {
	var w struct {
		Schema f.Version             `json:"schema_version"`
		Fields DirectTextRoundFields `json:"fields"`
	}
	if len(raw) > MaxDirectTextRoundBytes || directTextDecode(raw, &w) != nil || w.Schema != DirectTextSchemaVersion {
		return DirectTextRound{}, invalid()
	}
	v, err := NewDirectTextRound(w.Fields)
	if err != nil || !bytes.Equal(v.CanonicalBytes(), raw) {
		return DirectTextRound{}, invalid()
	}
	return v, nil
}
func (DirectTextRound) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "execution_round_input") }
func (DirectTextRound) LogValue() slog.Value         { return slog.StringValue("execution_round_input") }
func (DirectTextRound) MarshalJSON() ([]byte, error) { return []byte(`"execution_round_input"`), nil }
func (*DirectTextRound) UnmarshalJSON([]byte) error  { return invalid() }
func directTextDecode(raw []byte, target any) error {
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil || !bytes.Equal(canonical, raw) {
		return invalid()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil {
		return invalid()
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return invalid()
	}
	return nil
}

type DirectTextReceipt struct {
	ExecutionID       i.ExecutionID
	StartID           DirectTextStartID
	SnapshotID        SnapshotID
	RoundID           RoundID
	CallID            mc.CallID
	Status            Status
	Version           f.Version
	TranscriptThrough f.Sequence
}

const ExecutionProducer event.StableName = "execution"
const ExecutionAggregate event.StableName = "execution.execution"
const ExecutionStartedName event.StableName = "execution.started"
const ExecutionSucceededName event.StableName = "execution.succeeded"
const ExecutionFailedName event.StableName = "execution.failed"
const ExecutionCancelledName event.StableName = "execution.cancelled"

// Lifecycle payloads contain identities and a closed safe reason, not input,
// output, provider errors or credentials. The producer proves the actual write.
type DirectTextLifecycle struct {
	ExecutionID       i.ExecutionID     `json:"execution_id"`
	AgentID           i.AgentID         `json:"agent_id"`
	StartID           DirectTextStartID `json:"start_id"`
	SnapshotID        SnapshotID        `json:"snapshot_id"`
	RoundID           RoundID           `json:"round_id"`
	CallID            mc.CallID         `json:"call_id"`
	Status            Status            `json:"status"`
	Reason            string            `json:"reason"`
	TranscriptThrough f.Sequence        `json:"transcript_through"`
}

func (v DirectTextLifecycle) Validate() error {
	if v.ExecutionID.Validate() != nil || v.AgentID.Validate() != nil || v.StartID.Validate() != nil || v.SnapshotID.Validate() != nil || v.RoundID.Validate() != nil || v.CallID.Validate() != nil || v.TranscriptThrough.Validate() != nil {
		return invalid()
	}
	if !(v.Status == Running && v.Reason == "started" || v.Status == Succeeded && v.Reason == "completed" || v.Status == Cancelled && v.Reason == "cancelled" || v.Status == Failed && (v.Reason == "model_failed" || v.Reason == "incomplete_response" || v.Reason == "runtime_failed")) {
		return invalid()
	}
	return nil
}

type DirectTextEvents struct {
	catalog *event.Catalog
	types   map[Status]event.EventType[DirectTextLifecycle]
}

func RegisterDirectTextEvents(catalog *event.Catalog) (DirectTextEvents, error) {
	if !catalog.Valid() {
		return DirectTextEvents{}, invalid()
	}
	result := DirectTextEvents{catalog: catalog, types: make(map[Status]event.EventType[DirectTextLifecycle])}
	for _, v := range []struct {
		status Status
		name   event.StableName
	}{{Running, ExecutionStartedName}, {Succeeded, ExecutionSucceededName}, {Failed, ExecutionFailedName}, {Cancelled, ExecutionCancelledName}} {
		t, err := event.DefineEvent(catalog, event.Definition[DirectTextLifecycle]{Schema: event.Schema{Producer: ExecutionProducer, EventType: v.name, AggregateType: ExecutionAggregate, Version: 1}, Codec: event.JSONCodec[DirectTextLifecycle]{}, Validate: DirectTextLifecycle.Validate})
		if err != nil {
			return DirectTextEvents{}, err
		}
		result.types[v.status] = t
	}
	return result, nil
}
func (v DirectTextEvents) Valid() bool { return v.catalog.Valid() && len(v.types) == 4 }
func (v DirectTextEvents) New(h event.Header, p DirectTextLifecycle) (event.Event, error) {
	t, ok := v.types[p.Status]
	if !v.Valid() || !ok || p.Validate() != nil || h.Validate() != nil || h.SchemaVersion != 1 || h.EventType != t.Schema().EventType || h.AggregateType != ExecutionAggregate || h.AggregateID.String() != p.ExecutionID.String() || h.AggregateVersion == nil || h.AggregateSequence != nil || h.Scope.Kind != event.ProjectScope {
		return event.Event{}, invalid()
	}
	return event.NewEvent(t, h, p)
}
