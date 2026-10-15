package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// StoppedInvocationOwner identifies a technical borrower, including the two
// crash windows before Model admission and after Model retirement. It carries
// neither row identities nor authority to reconstruct an Invocation or Actor.
type StoppedInvocationOwner struct {
	ProjectID id.ProjectID
	CallID    mc.CallID
	ProcessID oc.ProcessID
}

func (r StoppedInvocationOwner) Validate() error {
	if r.ProjectID.Validate() != nil || r.CallID.Validate() != nil || r.ProcessID.Validate() != nil {
		return bad()
	}
	return nil
}
func (StoppedInvocationOwner) MarshalJSON() ([]byte, error) {
	return []byte(`"usage_stopped_invocation_owner"`), nil
}
func (*StoppedInvocationOwner) UnmarshalJSON([]byte) error { return bad() }
func (StoppedInvocationOwner) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("usage_stopped_invocation_owner"))
}
func (StoppedInvocationOwner) LogValue() slog.Value {
	return slog.StringValue("usage_stopped_invocation_owner")
}
func StoppedInvocationOwnerBinding(r StoppedInvocationOwner) (f.Digest, error) {
	if r.Validate() != nil {
		return "", bad()
	}
	type binding StoppedInvocationOwner
	raw, err := json.Marshal(binding(r))
	if err != nil {
		return "", bad()
	}
	return digestBytes(raw), nil
}

// The provider must bind a private current Runtime startup witness. Candidate
// discovery authorizes no write. Per-owner facts prove, under the original
// Model call lock, either no admission or that the exact old call is retired.
// Usage additionally proves that the old ProcessGuard has actually stopped.
type StoppedInvocationOwnerFacts interface {
	RequireStoppedInvocationStartupInTx(context.Context, f.Tx) error
	DiscoverStoppedInvocationOwnerFacts(context.Context, StoppedInvocationOwner) (InvocationDependencies, error)
	ValidateStoppedInvocationOwnerFactsInTx(context.Context, f.Tx, StoppedInvocationOwner, InvocationDependencies) error
}

type StoppedInvocationOwnerPlan InvocationPlan

func NewStoppedInvocationOwnerPlan(i PlanIssuer, d InvocationPlanDetails) (StoppedInvocationOwnerPlan, error) {
	p, err := NewInvocationPlan(i, d)
	return StoppedInvocationOwnerPlan(p), err
}
func (p StoppedInvocationOwnerPlan) Validate() error { return InvocationPlan(p).Validate() }
func (p StoppedInvocationOwnerPlan) Details() InvocationPlanDetails {
	return InvocationPlan(p).Details()
}
func (p StoppedInvocationOwnerPlan) RequiredLocks() []f.LockRequest {
	return InvocationPlan(p).RequiredLocks()
}
func (p StoppedInvocationOwnerPlan) Cause() f.TransactionCause { return InvocationPlan(p).Cause() }
func (p StoppedInvocationOwnerPlan) Matches(i PlanIssuer, b, m f.Digest) bool {
	return InvocationPlan(p).Matches(i, b, m)
}
func (StoppedInvocationOwnerPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"usage_stopped_invocation_owner_plan"`), nil
}
func (*StoppedInvocationOwnerPlan) UnmarshalJSON([]byte) error { return bad() }
func (StoppedInvocationOwnerPlan) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("usage_stopped_invocation_owner_plan"))
}
func (StoppedInvocationOwnerPlan) LogValue() slog.Value {
	return slog.StringValue("usage_stopped_invocation_owner_plan")
}

type StoppedInvocationOwnerRecovery interface {
	// Each bounded batch contains at most 64 original running invocation-lane
	// borrowers. Closing them removes them from the next batch; no history scan
	// or caller-selected project/row filter is exposed.
	DiscoverStoppedInvocationOwners(context.Context) ([]StoppedInvocationOwner, error)
	DiscoverStoppedInvocationOwner(context.Context, StoppedInvocationOwner) (StoppedInvocationOwnerPlan, error)
	CloseStoppedInvocationOwnerInTx(context.Context, f.Tx, StoppedInvocationOwner, StoppedInvocationOwnerPlan) error
	StoppedInvocationOwnerClosedInTx(context.Context, f.Tx, StoppedInvocationOwner, StoppedInvocationOwnerPlan) (bool, error)
}
