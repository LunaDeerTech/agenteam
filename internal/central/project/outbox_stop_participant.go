package project

import (
	"context"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// NewOutboxStopParticipant adapts the explicitly supplied Outbox lifecycle
// service. It grants neither ordinary delivery nor Project cleanup authority.
func NewOutboxStopParticipant(authority *LifecycleAuthority, outbox oc.ProjectLifecycleParticipant) (c.ProjectLifecycleParticipant, error) {
	if !authority.bound() || nilPort(outbox) {
		return nil, fault(f.DependencyUnbound)
	}
	name, err := outboxStopName(outbox)
	if err != nil {
		return nil, err
	}
	if name != string(c.OutboxParticipant) {
		return nil, invalid()
	}
	return &outboxStopParticipant{authority: authority, outbox: outbox}, nil
}

type outboxStopParticipant struct {
	authority *LifecycleAuthority
	outbox    oc.ProjectLifecycleParticipant
}

func (*outboxStopParticipant) Name() c.ParticipantName { return c.OutboxParticipant }

func (p *outboxStopParticipant) RequestStop(ctx context.Context, actor identity.Actor, cause c.LifecycleCause, scope c.ScopeRef) (c.StopReport, error) {
	return p.stop(ctx, actor, cause, scope, false)
}

func (p *outboxStopParticipant) InspectStop(ctx context.Context, actor identity.Actor, cause c.LifecycleCause, scope c.ScopeRef) (c.StopReport, error) {
	return p.stop(ctx, actor, cause, scope, true)
}

func (*outboxStopParticipant) Cleanup(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef, *c.CleanupCheckpoint) (c.CleanupReport, error) {
	return c.CleanupReport{}, fault(f.DependencyUnbound)
}

func (p *outboxStopParticipant) stop(ctx context.Context, actor identity.Actor, cause c.LifecycleCause, scope c.ScopeRef, inspect bool) (c.StopReport, error) {
	if ctx == nil || scope.Validate() != nil {
		return c.StopReport{}, invalid()
	}
	if p == nil || !p.authority.bound() || nilPort(p.outbox) {
		return c.StopReport{}, fault(f.DependencyUnbound)
	}
	name, err := outboxStopName(p.outbox)
	if err != nil {
		return c.StopReport{}, err
	}
	if name != string(c.OutboxParticipant) {
		return c.StopReport{}, fault(f.DependencyUnavailable)
	}
	project, err := lifecycleActorProject(actor, cause)
	if err != nil {
		return c.StopReport{}, err
	}
	if project != scope.ProjectID {
		return c.StopReport{}, fault(f.Forbidden)
	}
	if scope.Kind != c.ProjectScope {
		return c.StopReport{}, fault(f.DependencyUnbound)
	}
	// One caller-derived budget covers preflight and the real downstream call.
	// Neither cancellation nor its deadline substitutes for either actual return.
	ctx, end := context.WithTimeout(ctx, 2*time.Second)
	defer end()
	if err = ctx.Err(); err != nil {
		return c.StopReport{}, portError(err)
	}
	txCause, err := readCause("outbox-stop-preflight")
	if err != nil {
		return c.StopReport{}, err
	}
	validated := false
	result := p.authority.store.WithinTx(ctx, txCause, func(ctx context.Context, tx f.Tx) error {
		if err := p.authority.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(project, f.Shared)}); err != nil {
			return portError(err)
		}
		x, err := p.authority.lifecycleExecutor(ctx, tx, project)
		if err != nil {
			return err
		}
		fact, err := p.authority.lifecycleFact(ctx, x, project, cause, c.OutboxParticipant)
		if err != nil {
			return err
		}
		if err = fact.authorize(inspect); err != nil {
			return err
		}
		validated = true
		return nil
	})
	if err = commitError(result); err != nil {
		return c.StopReport{}, err
	}
	if err = ctx.Err(); err != nil {
		return c.StopReport{}, portError(err)
	}
	if !validated {
		return c.StopReport{}, fault(f.DependencyUnavailable)
	}
	operation, err := f.ParseID[oc.LifecycleOperation](cause.OperationID.String())
	if err != nil {
		return c.StopReport{}, invalid()
	}
	downstreamCause, err := oc.NewLifecycleCause(oc.LifecycleDetails{ProjectID: project, OperationID: operation, Action: oc.LifecycleAction(cause.Action), ProjectVersion: cause.ProjectVersion})
	if err != nil {
		return c.StopReport{}, invalid()
	}
	// The preflight transaction and its Project SH are gone. D06 must still
	// validate the current facts in each of its own original transactions.
	observed, err := callOutboxStop(p.outbox, ctx, actor, downstreamCause, inspect)
	if err != nil {
		return c.StopReport{}, err
	}
	if err = ctx.Err(); err != nil {
		return c.StopReport{}, portError(err)
	}
	if observed.Pending.Validate() != nil || observed.Stopped && observed.Pending != 0 {
		return c.StopReport{}, fault(f.DependencyUnavailable)
	}
	details := c.StopDetails{State: c.StopPending, SafeReason: c.ReasonWorkPending}
	if observed.Stopped {
		details = c.StopDetails{State: c.Stopped}
	}
	// D06's count supplies no resource identities; do not invent PendingRefs.
	return c.NewStopReport(c.OutboxParticipant, cause, scope, details)
}

func outboxStopName(port oc.ProjectLifecycleParticipant) (name string, err error) {
	defer func() {
		if recover() != nil {
			name, err = "", fault(f.DependencyUnavailable)
		}
	}()
	return port.Name(), nil
}

// Only the opaque call is contained here, after preflight has returned. A
// panic supplies no rollback or join proof. It cannot publish a stop report,
// and this boundary never intercepts the original preflight transaction unwind.
func callOutboxStop(port oc.ProjectLifecycleParticipant, ctx context.Context, actor identity.Actor, cause oc.LifecycleCause, inspect bool) (report oc.StopReport, err error) {
	defer func() {
		if recover() != nil {
			report, err = oc.StopReport{}, fault(f.DependencyUnavailable)
		}
	}()
	if inspect {
		return port.InspectStop(ctx, actor, cause)
	}
	return port.RequestStop(ctx, actor, cause)
}

var _ c.ProjectLifecycleParticipant = (*outboxStopParticipant)(nil)
