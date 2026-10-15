package runtime

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"slices"
)

func sameMeetingToolCall(call *builtinCall, in c.AgentToolCall, lookup bool) bool {
	return call != nil && in.Validate() == nil && call.lookup == lookup && sameRunnerCallBinding(call.binding, in.Call) && call.record.ID == in.OperationID && call.record.Attempt == in.AttemptID && call.record.Request == in.RequestID
}
func (a *BuiltinAuthority) meetingToolWitness(ctx context.Context, in c.AgentToolCall, lookup bool) (*builtinWitness, error) {
	if in.Validate() != nil {
		return nil, fail(f.Forbidden)
	}
	w, err := a.witness(ctx, in.Call.Actor, in.Call.ProjectID)
	if err != nil {
		return nil, err
	}
	if w.cleanupOnly.Load() || !sameMeetingToolCall(w.call, in, lookup) {
		return nil, fail(f.Forbidden)
	}
	return w, nil
}
func (a *BuiltinAuthority) RequireMeetingToolBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	return a.RequireAgentRunBindingInTx(ctx, tx, store)
}
func (a *BuiltinAuthority) DiscoverMeetingTool(ctx context.Context, in c.AgentToolCall, lookup bool) ([]f.LockRequest, error) {
	w, err := a.meetingToolWitness(ctx, in, lookup)
	if err != nil {
		return nil, err
	}
	return slices.Clone(w.call.locks), nil
}
func (a *BuiltinAuthority) WithMeetingToolInputInTx(ctx context.Context, tx f.Tx, in c.AgentToolCall, lookup bool, consume func(context.Context, []byte, c.AgentSource) error) error {
	if consume == nil {
		return fail(f.InvalidArgument)
	}
	w, err := a.meetingToolWitness(ctx, in, lookup)
	if err != nil {
		return err
	}
	source, ok := w.owner.executions.(c.AgentSourceReader)
	if !ok || nilPort(source) {
		return fail(f.DependencyUnbound)
	}
	return a.withCurrent(ctx, tx, w, func(current context.Context, raw []byte) error {
		origin, err := source.ReadMeetingToolSourceInTx(current, tx, in.Call, w.call.plan, lookup)
		if err != nil {
			return portError(err)
		}
		if origin.Validate() != nil || origin.ExecutionID != in.Call.ExecutionID {
			return fail(f.Forbidden)
		}
		return consume(current, raw, origin)
	})
}

var _ c.AgentInvocationAuthority = (*BuiltinAuthority)(nil)
