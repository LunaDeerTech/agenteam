package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

type InvocationAction string

const (
	ReserveAction  InvocationAction = "reserve"
	ObserveAction  InvocationAction = "observe"
	FinalizeAction InvocationAction = "finalize"
)

func (a InvocationAction) Valid() bool {
	return a == ReserveAction || a == ObserveAction || a == FinalizeAction
}

type InvocationAccess string

const (
	ApplyInvocation   InvocationAccess = "apply"
	ConfirmInvocation InvocationAccess = "confirm"
)

type InvocationIdentity struct {
	Attempt    mc.AttemptIdentity `json:"attempt"`
	Consumer   mc.Consumer        `json:"consumer"`
	SnapshotID mc.SnapshotID      `json:"snapshot_id"`
	Input      mc.InputIdentity   `json:"input"`
}

func (i InvocationIdentity) Validate() error {
	if i.Attempt.Validate() != nil || i.Consumer.Validate() != nil || i.SnapshotID.Validate() != nil || i.Input.ValidateFor(i.Consumer) != nil {
		return bad()
	}
	return nil
}
func (i InvocationIdentity) Clone() InvocationIdentity {
	i.Consumer = i.Consumer.Clone()
	i.Input = i.Input.Clone()
	return i
}
func (i InvocationIdentity) Equal(other InvocationIdentity) bool {
	return i.Validate() == nil && other.Validate() == nil && reflect.DeepEqual(i, other)
}

type InvocationRequest struct {
	Actor    id.Actor           `json:"-"`
	Access   InvocationAccess   `json:"access"`
	Action   InvocationAction   `json:"action"`
	Identity InvocationIdentity `json:"identity"`
	Sequence f.Sequence         `json:"sequence"`
}

func (r InvocationRequest) Validate() error {
	if r.Actor.Validate() != nil || r.Identity.Validate() != nil || !r.Action.Valid() || r.Access != ApplyInvocation && r.Access != ConfirmInvocation || r.Sequence.Validate() != nil || r.Action == ReserveAction && r.Sequence != 1 {
		return bad()
	}
	return nil
}
func (r InvocationRequest) Clone() InvocationRequest { r.Identity = r.Identity.Clone(); return r }

// Input identities and actors are private association data, never log DTOs.
func (InvocationRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"usage_invocation_request"`), nil
}
func (*InvocationRequest) UnmarshalJSON([]byte) error { return bad() }

type InvocationFact struct {
	Identity  InvocationIdentity
	Initiator id.Actor
	Sequence  f.Sequence
	Value     Invocation
}

func (r InvocationFact) Validate() error {
	if r.Identity.Validate() != nil || r.Initiator.Validate() != nil || r.Sequence.Validate() != nil || r.Value.Validate() != nil {
		return bad()
	}
	a, v := r.Identity.Attempt, r.Value
	if v.ID != a.InvocationID || v.CallID != a.CallID || v.AttemptIndex != a.AttemptIndex || v.ProcessID != a.ProcessID || v.Fence != a.Fence || v.SnapshotID != r.Identity.SnapshotID || !v.Consumer.Equal(r.Identity.Consumer) {
		return bad()
	}
	return nil
}
func (r InvocationFact) Clone() InvocationFact {
	r.Identity = r.Identity.Clone()
	r.Value = r.Value.Clone()
	return r
}
func (InvocationFact) MarshalJSON() ([]byte, error) { return []byte(`"usage_invocation_fact"`), nil }
func (*InvocationFact) UnmarshalJSON([]byte) error  { return bad() }

type InvocationReceipt struct {
	Action   InvocationAction `json:"action"`
	Sequence f.Sequence       `json:"sequence"`
	Digest   f.Digest         `json:"digest"`
	Value    Invocation       `json:"value"`
}

func (r InvocationReceipt) Validate() error {
	if !r.Action.Valid() || r.Sequence.Validate() != nil || r.Digest.Validate() != nil || r.Value.Validate() != nil {
		return bad()
	}
	if r.Action == ReserveAction && (r.Sequence != 1 || r.Value.Dispatch != Reserved || r.Value.Final != nil) || r.Action == ObserveAction && r.Value.Final != nil || r.Action == FinalizeAction && r.Value.Final == nil {
		return bad()
	}
	return nil
}
func (r InvocationReceipt) Clone() InvocationReceipt { r.Value = r.Value.Clone(); return r }

type InvocationLookup struct {
	Observed bool               `json:"observed"`
	Receipt  *InvocationReceipt `json:"receipt,omitempty"`
}

func (r InvocationLookup) Validate() error {
	if r.Observed != (r.Receipt != nil) || r.Receipt != nil && r.Receipt.Validate() != nil {
		return bad()
	}
	return nil
}
func (r InvocationLookup) Clone() InvocationLookup {
	if r.Receipt != nil {
		v := r.Receipt.Clone()
		r.Receipt = &v
	}
	return r
}

type ExecutionSummary struct {
	ProjectID   id.ProjectID   `json:"project_id"`
	ExecutionID id.ExecutionID `json:"execution_id"`
	Version     f.Version      `json:"version"`
	Summary     Summary        `json:"summary"`
}

func (r ExecutionSummary) Validate() error {
	if r.ProjectID.Validate() != nil || r.ExecutionID.Validate() != nil || r.Version.Validate() != nil || r.Summary.Validate() != nil {
		return bad()
	}
	return nil
}
func (r ExecutionSummary) Clone() ExecutionSummary { r.Summary = r.Summary.Clone(); return r }

type Writer interface {
	DiscoverInvocation(context.Context, InvocationRequest) (InvocationPlan, error)
	ReserveInvocationInTx(context.Context, f.Tx, InvocationRequest, InvocationPlan) (InvocationReceipt, error)
	ObserveInvocationInTx(context.Context, f.Tx, InvocationRequest, InvocationPlan) (InvocationReceipt, error)
	FinalizeInvocationInTx(context.Context, f.Tx, InvocationRequest, InvocationPlan) (InvocationReceipt, error)
	LookupInvocation(context.Context, InvocationRequest) (InvocationLookup, error)
}

// Binding encodes explicit projections; InvocationRequest's public encoding is redacted.
func InvocationBinding(r InvocationRequest) (f.Digest, error) {
	if r.Validate() != nil {
		return "", bad()
	}
	b, e := json.Marshal(struct {
		Actor    id.ActorDetails
		Access   InvocationAccess
		Action   InvocationAction
		Identity InvocationIdentity
		Sequence f.Sequence
	}{r.Actor.Details(), r.Access, r.Action, r.Identity, r.Sequence})
	if e != nil {
		return "", bad()
	}
	return digestBytes(b), nil
}

func (InvocationIdentity) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("usage_invocation_identity"))
}
func (InvocationIdentity) LogValue() slog.Value { return slog.StringValue("usage_invocation_identity") }
func (InvocationRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("usage_invocation_request"))
}
func (InvocationRequest) LogValue() slog.Value    { return slog.StringValue("usage_invocation_request") }
func (InvocationFact) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("usage_invocation_fact")) }
func (InvocationFact) LogValue() slog.Value       { return slog.StringValue("usage_invocation_fact") }
func (InvocationReceipt) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("usage_invocation_receipt"))
}
func (InvocationReceipt) LogValue() slog.Value { return slog.StringValue("usage_invocation_receipt") }
func (InvocationLookup) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("usage_invocation_lookup"))
}
func (InvocationLookup) LogValue() slog.Value { return slog.StringValue("usage_invocation_lookup") }
func (ExecutionSummary) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("usage_execution_summary"))
}
func (ExecutionSummary) LogValue() slog.Value { return slog.StringValue("usage_execution_summary") }
