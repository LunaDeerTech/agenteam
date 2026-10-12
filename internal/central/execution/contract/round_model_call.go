package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// RoundModelClaim is an immutable reservation identity, not an Invocation,
// executable lease or proof that the original process has stopped.
type RoundModelClaim struct{}
type RoundModelClaimID = f.ID[RoundModelClaim]

type RoundModelClaimFields struct {
	SchemaVersion    f.Version
	ClaimID          RoundModelClaimID
	ProjectID        i.ProjectID
	AgentID          i.AgentID
	ExecutionID      i.ExecutionID
	RoundInputID     RoundID
	RoundInputDigest f.Digest
	SnapshotID       SnapshotID
	SnapshotDigest   f.Digest
	ProjectionDigest f.Digest
	CallID           mc.CallID
	ProcessID        oc.ProcessID
	InputDigest      f.Digest
	RequestDigest    f.Digest
	CreatedAt        f.Instant
}

func (v RoundModelClaimFields) Validate() error {
	if v.SchemaVersion != 1 || v.ClaimID.Validate() != nil || v.ProjectID.Validate() != nil || v.AgentID.Validate() != nil || v.ExecutionID.Validate() != nil || v.RoundInputID.Validate() != nil || v.RoundInputDigest.Validate() != nil || v.SnapshotID.Validate() != nil || v.SnapshotDigest.Validate() != nil || v.ProjectionDigest.Validate() != nil || v.CallID.Validate() != nil || v.ProcessID.Validate() != nil || v.InputDigest.Validate() != nil || v.RequestDigest.Validate() != nil || v.CreatedAt.Validate() != nil {
		return invalid()
	}
	return nil
}
func (RoundModelClaimFields) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_round_model_claim_fields")
}
func (RoundModelClaimFields) MarshalJSON() ([]byte, error) {
	return []byte(`"execution_round_model_claim_fields"`), nil
}
func (RoundModelClaimFields) LogValue() slog.Value {
	return slog.StringValue("execution_round_model_claim_fields")
}

// Receipt is output only. Implementations own their scalar fields; no API
// consumes a receipt as authority, and even a genuine receipt grants no Invoke.
// The Execution implementation constructs its private value after the actual
// transaction return. There is no public receipt constructor or decoder.
type RoundModelClaimReceipt interface {
	Fields() RoundModelClaimFields
	Validate() error
	fmt.Formatter
	MarshalJSON() ([]byte, error)
	LogValue() slog.Value
}

type RoundModelCalls interface {
	Claim(context.Context, i.ExecutionID, RoundID) (RoundModelClaimReceipt, error)
	Lookup(context.Context, i.ExecutionID, RoundID) (RoundModelClaimReceipt, bool, error)
	Stop()
	Drain(context.Context) error
	Joined() bool
}
