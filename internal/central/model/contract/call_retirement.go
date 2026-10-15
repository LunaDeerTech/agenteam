package contract

import (
	"context"
	"fmt"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// Retirement concerns the Model-call lease, including resolution that was never admitted.
// Its fields do not prove terminal state or that borrowers have returned.
type ModelCallRetirementRequest struct {
	Actor           id.Actor
	Model           ResolvedModel
	TerminalVersion f.Version
}

func (r ModelCallRetirementRequest) Validate() error {
	if r.Model.Validate() != nil || r.TerminalVersion.Validate() != nil || r.Model.CredentialLease == nil || r.Model.Consumer.Kind == AgentConsumer || r.Model.LeaseOwner.Details().Kind != sc.ModelCallOwner {
		return bad()
	}
	return r.ConsumerRequest().Validate()
}
func (r ModelCallRetirementRequest) Clone() ModelCallRetirementRequest {
	r.Model = r.Model.Clone()
	return r
}
func (r ModelCallRetirementRequest) ConsumerRequest() ConsumerRequest {
	request := ConsumerRequest{Action: RetireConsumer, Actor: r.Actor, Consumer: r.Model.Consumer.Clone(), SnapshotID: r.Model.Snapshot.ID, LeaseOwner: r.Model.LeaseOwner, TerminalVersion: copyPtr(&r.TerminalVersion)}
	if r.Model.CredentialLease != nil {
		request.LeaseID = copyPtr(&r.Model.CredentialLease.LeaseID)
	}
	call, err := f.ParseID[Call](r.Model.LeaseOwner.Details().ID)
	if err == nil {
		request.CallID = &call
	}
	return request
}

// Discovery freezes the complete Secret/consumer/call lock union. Release runs in
// the original terminal transaction after its business owner proves the exact
// terminal version; Model additionally requires no live or unretired call. The Secret-owned
// UsageOperations release participates in that same transaction. Neither a
// plan nor this DTO is a public terminal grant; Unknown remains caller-owned.
type ModelCallRetirementPlan interface{ RequiredLocks() []f.LockRequest }
type ModelCallRetirement interface {
	DiscoverModelCallRetirement(context.Context, ModelCallRetirementRequest) (ModelCallRetirementPlan, error)
	ReleaseModelCallInTx(context.Context, f.Tx, ModelCallRetirementRequest, ModelCallRetirementPlan) error
	ModelCallRetiredInTx(context.Context, f.Tx, ModelCallRetirementRequest, ModelCallRetirementPlan) (bool, error)
}

func (ModelCallRetirementRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_call_retirement"))
}
func (ModelCallRetirementRequest) LogValue() slog.Value {
	return slog.StringValue("model_call_retirement")
}
func (ModelCallRetirementRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"model_call_retirement"`), nil
}
