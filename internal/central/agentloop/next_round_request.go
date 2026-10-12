package agentloop

import (
	"context"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// BuildNextRoundModelRequest is pure computation inside the Execution owner's
// verified historical source loan. Its return is a candidate, not evidence of
// commitment or permission to Begin/Invoke. The binder must wait for its real
// Store return before publishing it. It must not call the public Project reader
// here: that reader owns a separate transaction.
func BuildNextRoundModelRequest(ctx context.Context, source ec.RoundProjectionSource, callID mc.CallID) (ec.NextRoundModelRequest, error) {
	var zero ec.NextRoundModelRequest
	if err := contextError(ctx); err != nil {
		return zero, err
	}
	if source.Input.Validate() != nil || source.Snapshot.Validate() != nil || source.Predecessor.Validate() != nil || callID.Validate() != nil || callID == source.Predecessor.Fields().Round.CallID {
		return zero, invalid()
	}
	input := source.Input.Fields()
	projection, err := projectRoundSource(ctx, input.ExecutionID, input.RoundID, source)
	if err != nil {
		return zero, err
	}
	captured := source.Snapshot.Fields().Context.Input().Fields()
	actor, err := i.NewAgentRun(captured.Request.Launch.ProjectID, captured.Request.Launch.AgentID, captured.Request.ExecutionID)
	if err != nil {
		return zero, invalid()
	}
	return ec.NewNextRoundModelRequest(ctx, ec.NextRoundModelRequestInput{
		ProjectID: captured.Request.Launch.ProjectID, AgentID: captured.Request.Launch.AgentID, ExecutionID: input.ExecutionID,
		RoundInputID: input.RoundID, RoundInputDigest: source.Input.Digest(), SnapshotID: input.SnapshotID, SnapshotDigest: input.SnapshotDigest,
		ProjectionDigest: projection.Digest(), CallID: callID, Actor: actor, Model: captured.Model,
		Messages: projection.Messages(), Tools: projection.Tools(),
	})
}
