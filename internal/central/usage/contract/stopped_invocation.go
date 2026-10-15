package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"reflect"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// StoppedInvocationRequest describes only an existing Invocation. It cannot
// reserve one, reconstruct its Initiator, or prove that its process stopped.
// Model's private recovery facts and Usage's original stored header supply
// those authorities in the caller's original transaction.
type StoppedInvocationRequest struct {
	Identity       InvocationIdentity
	BeforeSequence f.Sequence
	Before         Invocation
	AfterSequence  f.Sequence
	After          Invocation
}

func (r StoppedInvocationRequest) Validate() error {
	if r.Identity.Validate() != nil || r.BeforeSequence.Validate() != nil || r.AfterSequence.Validate() != nil || r.Before.Validate() != nil || r.After.Validate() != nil || r.After.Final == nil {
		return bad()
	}
	a, before := r.Identity.Attempt, r.Before
	if before.ID != a.InvocationID || before.CallID != a.CallID || before.AttemptIndex != a.AttemptIndex || before.ProcessID != a.ProcessID || before.Fence != a.Fence || before.SnapshotID != r.Identity.SnapshotID || !before.Consumer.Equal(r.Identity.Consumer) {
		return bad()
	}
	if before.Final != nil {
		if r.BeforeSequence != r.AfterSequence || !reflect.DeepEqual(before, r.After) {
			return bad()
		}
		return nil
	}
	steps := f.Sequence(1)
	dispatch, terminal := before.Dispatch, Unknown
	switch before.Dispatch {
	case Reserved:
		steps, dispatch, terminal = 2, NotSent, Cancelled
	case Authorized:
		steps, dispatch = 2, DispatchUnknown
	case NotSent:
		terminal = Cancelled
	}
	if r.BeforeSequence > f.Sequence(math.MaxInt64)-steps || r.AfterSequence != r.BeforeSequence+steps || r.After.Dispatch != dispatch || r.After.Final.Status != terminal || r.After.Final.Version != 1 {
		return bad()
	}
	// No missing usage, response or dispatch evidence may be invented by
	// recovery. Only the honest dispatch observation and final are added.
	after := r.After.Clone()
	after.Dispatch, after.Final = before.Dispatch, nil
	if !reflect.DeepEqual(before, after) {
		return bad()
	}
	return nil
}
func (r StoppedInvocationRequest) Clone() StoppedInvocationRequest {
	r.Identity, r.Before, r.After = r.Identity.Clone(), r.Before.Clone(), r.After.Clone()
	return r
}
func (StoppedInvocationRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"usage_stopped_invocation"`), nil
}
func (*StoppedInvocationRequest) UnmarshalJSON([]byte) error { return bad() }
func (StoppedInvocationRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("usage_stopped_invocation"))
}
func (StoppedInvocationRequest) LogValue() slog.Value {
	return slog.StringValue("usage_stopped_invocation")
}
func StoppedInvocationBinding(r StoppedInvocationRequest) (f.Digest, error) {
	if r.Validate() != nil {
		return "", bad()
	}
	// A distinct projection avoids the public request's redacted encoding.
	type binding StoppedInvocationRequest
	raw, err := json.Marshal(binding(r))
	if err != nil {
		return "", bad()
	}
	return digestBytes(raw), nil
}

type StoppedInvocationFacts interface {
	DiscoverStoppedInvocationFacts(context.Context, StoppedInvocationRequest) (InvocationDependencies, error)
	ValidateStoppedInvocationFactsInTx(context.Context, f.Tx, StoppedInvocationRequest, InvocationDependencies) error
}

// The existing issued-plan envelope supplies immutable dependencies and a
// transaction cause, but never turns a recovery request into a live Actor.
type StoppedInvocationPlan InvocationPlan

func NewStoppedInvocationPlan(i PlanIssuer, d InvocationPlanDetails) (StoppedInvocationPlan, error) {
	p, err := NewInvocationPlan(i, d)
	return StoppedInvocationPlan(p), err
}
func (p StoppedInvocationPlan) Validate() error { return InvocationPlan(p).Validate() }
func (p StoppedInvocationPlan) Details() InvocationPlanDetails {
	return InvocationPlan(p).Details()
}
func (p StoppedInvocationPlan) RequiredLocks() []f.LockRequest {
	return InvocationPlan(p).RequiredLocks()
}
func (p StoppedInvocationPlan) Cause() f.TransactionCause { return InvocationPlan(p).Cause() }
func (p StoppedInvocationPlan) Matches(i PlanIssuer, binding, mapping f.Digest) bool {
	return InvocationPlan(p).Matches(i, binding, mapping)
}
func (StoppedInvocationPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"usage_stopped_invocation_plan"`), nil
}
func (*StoppedInvocationPlan) UnmarshalJSON([]byte) error { return bad() }
func (StoppedInvocationPlan) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("usage_stopped_invocation_plan"))
}
func (StoppedInvocationPlan) LogValue() slog.Value {
	return slog.StringValue("usage_stopped_invocation_plan")
}

type StoppedInvocationRecovery interface {
	DiscoverStoppedInvocation(context.Context, StoppedInvocationRequest) (StoppedInvocationPlan, error)
	RecoverStoppedInvocationInTx(context.Context, f.Tx, StoppedInvocationRequest, StoppedInvocationPlan) error
	StoppedInvocationAppliedInTx(context.Context, f.Tx, StoppedInvocationRequest, StoppedInvocationPlan) (bool, error)
}
