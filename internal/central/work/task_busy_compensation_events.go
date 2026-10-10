package work

import (
	"context"
	"slices"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

const taskBusyEventPurpose = "work.task-busy-compensation.append-v1"

func taskBusyEventTriple(s event.Summary) bool {
	return s.Producer == c.WorkProducer && s.Header.EventType == c.TaskTransitionedName && s.Header.AggregateType == c.TaskAggregate && s.Header.SchemaVersion == c.TaskBusyCompensationSchemaVersion
}
func (a *Authority) busyEventContext(ctx context.Context, actor i.Actor, summary event.Summary) (taskBusyContext, error) {
	var zero taskBusyContext
	if ctx == nil {
		return zero, fault(f.InvalidArgument)
	}
	if ctx.Err() != nil {
		return zero, ctx.Err()
	}
	v, ok := ctx.Value(taskBusyContextKey{}).(taskBusyContext)
	if !ok || v.plan == nil || v.plan.owner == nil || v.plan.owner.deps.Authority != a || a.state() == nil || !sameStore(a.state().store, v.plan.owner.store) || !v.plan.actor.Equal(actor) || claimActor(actor, v.plan.request.Claim) != nil || !taskBusyEventTriple(summary) {
		return zero, fault(f.Forbidden)
	}
	p := v.plan
	if err := validateTaskBusyRecord(&p.record); err != nil {
		return zero, err
	}
	if !p.record.Restored || p.record.Header == nil || p.record.Event == nil {
		return zero, fault(f.Forbidden)
	}
	e, err := p.owner.deps.CompensationEvents.NewTaskCompensated(*p.record.Header, *p.record.Event)
	if err != nil || !sameValue(e.Summary(), summary) {
		return zero, fault(f.Forbidden)
	}
	return v, nil
}
func busyEventDependencies(p *taskBusyPlan, summary event.Summary) (f.Digest, []f.LockRequest, []byte, error) {
	type held struct {
		Key  string     `json:"key"`
		Mode f.LockMode `json:"mode"`
	}
	locks := make([]held, len(p.baseLocks))
	for n, v := range p.baseLocks {
		locks[n] = held{v.Key.Canonical(), v.Mode}
	}
	raw, err := canonical(struct {
		Purpose string
		Actor   i.ActorDetails
		Request c.TaskBusyCompensationRequest
		Summary event.Summary
		Record  taskBusyRecord
		Locks   []held
		Stages  [2]oc.Stage
	}{taskBusyEventPurpose, p.actor.Details(), p.request, summary, p.record, locks, [2]oc.Stage{oc.CurrentAccess, oc.NewFact}})
	if err != nil {
		return "", nil, nil, internal(err)
	}
	opaque, err := canonical(struct {
		Kind    string `json:"kind"`
		ClaimID string `json:"claim_id"`
		EventID string `json:"event_id"`
	}{taskBusyEventPurpose, p.request.Claim.DispatchID, p.record.Header.EventID.String()})
	if err != nil {
		return "", nil, nil, internal(err)
	}
	return digest(raw), slices.Clone(p.baseLocks), opaque, nil
}
func (a *Authority) discoverTaskBusyAppend(ctx context.Context, actor i.Actor, summary event.Summary) (oc.Dependencies, error) {
	v, err := a.busyEventContext(ctx, actor, summary)
	if err != nil {
		return oc.Dependencies{}, err
	}
	binding, locks, opaque, err := busyEventDependencies(v.plan, summary)
	if err != nil {
		return oc.Dependencies{}, err
	}
	return oc.NewDependencies(a.state().issuer, binding, locks, opaque)
}
func (a *Authority) validateTaskBusyAppendInTx(ctx context.Context, tx f.Tx, actor i.Actor, summary event.Summary, deps oc.Dependencies, stage oc.Stage) error {
	if !stage.Valid() {
		return fault(f.Forbidden)
	}
	v, err := a.busyEventContext(ctx, actor, summary)
	if err != nil {
		return err
	}
	p := v.plan
	// The private applied value exists only after this writer's original SQL
	// succeeded. A public shape, dependency plan or a later row cannot replace it.
	if v.applied == nil || v.applied.owner != p.owner || v.applied.plan != p || v.applied.tx != tx {
		return fault(f.Forbidden)
	}
	binding, locks, opaque, err := busyEventDependencies(p, summary)
	if err != nil {
		return err
	}
	if !deps.Matches(a.state().issuer, binding) || !sameTaskLocks(deps.Locks(), locks) || !slices.Equal(deps.Opaque(), opaque) {
		return fault(f.Forbidden)
	}
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = a.state().store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return portError(err)
	}
	guard, err := p.owner.deps.Scheduler.RequireTaskBusyCompensationInTx(ctx, tx, actor, p.request, p)
	if err != nil {
		return portError(err)
	}
	if !sameValue(guard, p.record.Claim.Guard) {
		return fault(f.Forbidden)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if _, err = p.owner.currentProject(ctx, tx, p.request); err != nil {
		return err
	}
	if err = verifyTaskBusyPostimage(ctx, x, &p.record); err != nil {
		return err
	}
	return ctx.Err()
}
