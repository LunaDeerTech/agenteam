package runtime

import (
	"bytes"
	"context"
	"slices"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func sameMeetingDecisionCall(call *builtinCall, in ec.MeetingDecisionCall, lookup bool) bool {
	return call != nil && in.Validate() == nil && call.lookup == lookup &&
		sameRunnerCallBinding(call.binding, in.Call) && call.record.ID == in.OperationID &&
		call.record.Attempt == in.AttemptID && call.record.Request == in.RequestID
}

func (a *BuiltinAuthority) meetingDecisionWitness(ctx context.Context, in ec.MeetingDecisionCall, lookup bool) (*builtinWitness, error) {
	if in.Validate() != nil {
		return nil, fail(f.Forbidden)
	}
	w, err := a.witness(ctx, in.Call.Actor, in.Call.ProjectID)
	if err != nil {
		return nil, err
	}
	if w.cleanupOnly.Load() || !sameMeetingDecisionCall(w.call, in, lookup) {
		return nil, fail(f.Forbidden)
	}
	return w, nil
}

func (a *BuiltinAuthority) RequireMeetingDecisionToolBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if a == nil || a.guard == nil || nilPort(a.store) {
		return fail(f.DependencyUnbound)
	}
	return a.RequireAgentRunBindingInTx(ctx, tx, store)
}

func (a *BuiltinAuthority) DiscoverMeetingDecisionTool(ctx context.Context, in ec.MeetingDecisionCall, lookup bool) ([]f.LockRequest, error) {
	w, err := a.meetingDecisionWitness(ctx, in, lookup)
	if err != nil {
		return nil, err
	}
	return slices.Clone(w.call.locks), nil
}

// Decision waiting has a source-specific Execution loan. The generic live
// Builtin input loan intentionally requires a running Execution and cannot be
// used to make every waiting reason callable.
func (a *BuiltinAuthority) meetingDecisionExecution(ctx context.Context, tx f.Tx, w *builtinWitness) (ec.MeetingDecisionExecution, error) {
	if w == nil || !w.active.Load() || w.cleanupOnly.Load() || w.owner == nil || w.owner.authority != a || w.call == nil || w.call.lookup {
		return nil, fail(f.Forbidden)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, w.call.locks); err != nil {
		return nil, portError(err)
	}
	if err = a.guard.RequireCurrentProcessInTx(ctx, tx, a.process); err != nil {
		return nil, portError(err)
	}
	if err = requireBuiltinAttempt(ctx, x, a.process, w.call.record); err != nil {
		return nil, err
	}
	owner, ok := w.owner.executions.(ec.MeetingDecisionExecution)
	if !ok || nilPort(owner) {
		return nil, fail(f.DependencyUnbound)
	}
	return owner, nil
}

func (a *BuiltinAuthority) WithMeetingDecisionToolInTx(ctx context.Context, tx f.Tx, in ec.MeetingDecisionCall, decision string, waiting bool, consume func(context.Context, []byte) error) error {
	if consume == nil {
		return fail(f.InvalidArgument)
	}
	w, err := a.meetingDecisionWitness(ctx, in, false)
	if err != nil {
		return err
	}
	owner, err := a.meetingDecisionExecution(ctx, tx, w)
	if err != nil {
		return err
	}
	calls := 0
	err = owner.WithMeetingDecisionToolCallInTx(ctx, tx, in, w.call.plan, decision, waiting, func(current context.Context, raw []byte) error {
		calls++
		if calls != 1 || !w.active.Load() || w.cleanupOnly.Load() || !bytes.Equal(raw, w.call.arguments) {
			return fail(f.Forbidden)
		}
		return consume(current, raw)
	})
	if err != nil {
		return err
	}
	if calls != 1 {
		return fail(f.Forbidden)
	}
	return contextError(ctx)
}

func (a *BuiltinAuthority) SetMeetingDecisionToolWaitingInTx(ctx context.Context, tx f.Tx, in ec.MeetingDecisionCall, decision string, enter bool) error {
	w, err := a.meetingDecisionWitness(ctx, in, false)
	if err != nil {
		return err
	}
	owner, err := a.meetingDecisionExecution(ctx, tx, w)
	if err != nil {
		return err
	}
	return owner.SetMeetingDecisionWaitingInTx(ctx, tx, in, w.call.plan, decision, enter)
}

func (a *BuiltinAuthority) WithCommittedMeetingDecisionToolInTx(ctx context.Context, tx f.Tx, in ec.MeetingDecisionCall, consume func(context.Context, []byte) error) error {
	w, err := a.meetingDecisionWitness(ctx, in, true)
	if err != nil {
		return err
	}
	return a.withCurrent(ctx, tx, w, consume)
}

var _ ec.MeetingDecisionToolAuthority = (*BuiltinAuthority)(nil)
