package contract

import (
	"context"
	"fmt"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// Retirement concerns the Execution-owned shared lease, not one Model Call.
// Its fields do not prove terminal state or that borrowers have returned.
type ExecutionModelRetirementRequest struct {
	Actor           id.Actor
	Model           ResolvedModel
	TerminalVersion f.Version
}

func (r ExecutionModelRetirementRequest) Validate() error {
	if r.Model.Validate() != nil || r.TerminalVersion.Validate() != nil || r.Model.CredentialLease == nil || r.Model.Consumer.Kind != AgentConsumer || r.Model.Consumer.Purpose != AgentGeneration || r.Model.Consumer.ExecutionID == nil || r.Model.LeaseOwner.Details().Kind != sc.ExecutionOwner {
		return bad()
	}
	return r.ConsumerRequest().Validate()
}
func (r ExecutionModelRetirementRequest) Clone() ExecutionModelRetirementRequest {
	r.Model = r.Model.Clone()
	return r
}
func (r ExecutionModelRetirementRequest) ConsumerRequest() ConsumerRequest {
	request := ConsumerRequest{Action: RetireConsumer, Actor: r.Actor, Consumer: r.Model.Consumer.Clone(), SnapshotID: r.Model.Snapshot.ID, LeaseOwner: r.Model.LeaseOwner, TerminalVersion: copyPtr(&r.TerminalVersion)}
	if r.Model.CredentialLease != nil {
		request.LeaseID = copyPtr(&r.Model.CredentialLease.LeaseID)
	}
	return request
}

// Discovery freezes the complete Secret/consumer lock union. Release runs in
// the original terminal transaction after its real Execution owner proves all
// calls joined and the exact terminal version was written. The Secret-owned
// UsageOperations release participates in that same transaction. Neither a
// plan nor this DTO is a public terminal grant; Unknown remains caller-owned.
type ExecutionModelRetirementPlan interface{ RequiredLocks() []f.LockRequest }
type ExecutionModelRetirement interface {
	DiscoverExecutionModelRetirement(context.Context, ExecutionModelRetirementRequest) (ExecutionModelRetirementPlan, error)
	ReleaseExecutionModelInTx(context.Context, f.Tx, ExecutionModelRetirementRequest, ExecutionModelRetirementPlan) error
	ExecutionModelRetiredInTx(context.Context, f.Tx, ExecutionModelRetirementRequest, ExecutionModelRetirementPlan) (bool, error)
}

func (ExecutionModelRetirementRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("execution_model_retirement"))
}
func (ExecutionModelRetirementRequest) LogValue() slog.Value {
	return slog.StringValue("execution_model_retirement")
}
func (ExecutionModelRetirementRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"execution_model_retirement"`), nil
}
