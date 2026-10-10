package contract

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// PreparationRequest is copied from the original committed Execution. It is
// an identity and lock-planning input, never a preparing permission or a new
// Launch request. Its original physical RequestID and command key are retained.
type PreparationRequest struct {
	ExecutionID i.ExecutionID
	Launch      LaunchRequest
}

func (r PreparationRequest) Validate() error {
	if r.ExecutionID.Validate() != nil || r.Launch.Validate() != nil {
		return invalid()
	}
	return nil
}
func (r PreparationRequest) Clone() PreparationRequest {
	r.Launch = r.Launch.Clone()
	return r
}
func (r PreparationRequest) Equal(other PreparationRequest) bool {
	if r.Validate() != nil || other.Validate() != nil || r.ExecutionID != other.ExecutionID || r.Launch.Meta.RequestID != other.Launch.Meta.RequestID || r.Launch.Meta.IdempotencyKey != other.Launch.Meta.IdempotencyKey {
		return false
	}
	a, err := r.Launch.Digest()
	b, otherErr := other.Launch.Digest()
	return err == nil && otherErr == nil && a == b
}

// PreparationProjectGate reads only Project-owned facts: original same-Store
// Tx, held Project SH, current initialized active Project. It does not grant an
// Execution identity and does not recurse into Execution or borrow a Human
// Session. The Execution owner checks its original private claim before this
// call and retains the returned Project view only in that same transaction.
type PreparationProjectGate interface {
	RequirePreparingProjectInTx(context.Context, f.Tx, i.ProjectID) (pc.ProjectRef, error)
}

// TriggerCaptureAuthority is implemented by the real Execution owner. Before
// any protected source read, the provider calls it with the original request.
// It requires the original private preparing call, live same-Store Tx, exact
// claim/attempt/fence, complete held locks and no cancellation. The returned
// Project view came from PreparationProjectGate in this same transaction.
// Neither an AgentRun constructor nor this public request can create the proof.
type TriggerCaptureAuthority interface {
	RequireTriggerCaptureInTx(context.Context, f.Tx, i.ExecutionID, LaunchRequest) (pc.ProjectRef, error)
}

// CapturePlan only plans locks. A provider recognizes its private issuer and
// the complete original request, then rechecks source versions/relationships
// under that union in CaptureInputInTx. Changed lock sets require a new outer
// discovery; providers must not acquire missing locks in the callback.
type CapturePlan interface{ RequiredLocks() []f.LockRequest }

// TriggerCaptureProvider is separate from the existing launch provider. The
// capture method is synchronous and SQL-only, validates the original Execution
// proof and source gates, and returns an immutable versioned input. Any owner
// references it writes are in the caller's original transaction. Build/remote
// work is outside this interface and never runs in this transaction.
type TriggerCaptureProvider interface {
	DiscoverCapture(context.Context, i.ExecutionID, LaunchRequest) (CapturePlan, error)
	CaptureInputInTx(context.Context, f.Tx, i.ExecutionID, LaunchRequest, CapturePlan) (CapturedTriggerInput, error)
}

type TriggerInput struct{}
type TriggerInputID = f.ID[TriggerInput]

type TriggerInputRef struct {
	ProviderType  string         `json:"provider_type"`
	SchemaVersion f.Version      `json:"schema_version"`
	InputID       TriggerInputID `json:"input_id"`
	Digest        f.Digest       `json:"digest"`
}

func (r TriggerInputRef) Validate() error {
	if r.ProviderType != "task" && r.ProviderType != "meeting" || r.SchemaVersion.Validate() != nil || r.InputID.Validate() != nil || r.Digest.Validate() != nil {
		return invalid()
	}
	return nil
}

// MaxTriggerInputBytes bounds this internal, versioned source projection. The
// provider must reject an over-limit complete input, never truncate required
// source fields. Its own typed decoder owns the data schema and field rules.
const MaxTriggerInputBytes = 1 << 20

type CapturedTriggerInput struct {
	data func() (TriggerInputRef, []byte)
}

// NewCapturedTriggerInput preserves the exact typed source encoding. Digest is
// SHA-256 of these exact bytes, not a digest of mutable current source pointers.
// Construction validates a value, not its source authority or transaction.
func NewCapturedTriggerInput(ref TriggerInputRef, raw []byte) (CapturedTriggerInput, error) {
	if len(raw) > MaxTriggerInputBytes {
		return CapturedTriggerInput{}, f.NewFault(f.PayloadTooLarge, f.NotStarted)
	}
	trimmed := bytes.TrimSpace(raw)
	if ref.Validate() != nil || len(trimmed) < 2 || trimmed[0] != '{' || !utf8.Valid(raw) || !json.Valid(raw) || TriggerInputDigest(raw) != ref.Digest {
		return CapturedTriggerInput{}, invalid()
	}
	owned := bytes.Clone(raw)
	return CapturedTriggerInput{func() (TriggerInputRef, []byte) { return ref, bytes.Clone(owned) }}, nil
}
func TriggerInputDigest(raw []byte) f.Digest {
	sum := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(sum[:]))
}
func (v CapturedTriggerInput) Validate() error {
	if v.data == nil {
		return invalid()
	}
	return nil
}
func (v CapturedTriggerInput) Ref() TriggerInputRef {
	if v.data == nil {
		return TriggerInputRef{}
	}
	r, _ := v.data()
	return r
}
func (v CapturedTriggerInput) Data() []byte {
	if v.data == nil {
		return nil
	}
	_, raw := v.data()
	return raw
}

func (PreparationRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "preparation_request")
}
func (PreparationRequest) LogValue() slog.Value         { return slog.StringValue("preparation_request") }
func (PreparationRequest) MarshalJSON() ([]byte, error) { return []byte(`"preparation_request"`), nil }
func (*PreparationRequest) UnmarshalJSON([]byte) error  { return invalid() }
func (CapturedTriggerInput) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "captured_trigger_input")
}
func (CapturedTriggerInput) LogValue() slog.Value { return slog.StringValue("captured_trigger_input") }
func (CapturedTriggerInput) MarshalJSON() ([]byte, error) {
	return []byte(`"captured_trigger_input"`), nil
}
func (*CapturedTriggerInput) UnmarshalJSON([]byte) error { return invalid() }
