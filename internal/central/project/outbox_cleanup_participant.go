package project

import (
	"context"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// NewOutboxLifecycleParticipant adds the actual D06 cleanup call to the fixed
// stop adapter. It does not change NewOutboxStopParticipant or manufacture a
// complete Project registry. Cleanup requires the live cleanup driver.
func NewOutboxLifecycleParticipant(authority *LifecycleAuthority, outbox oc.ProjectLifecycleParticipant) (c.ProjectLifecycleParticipant, error) {
	stop, err := NewOutboxStopParticipant(authority, outbox)
	if err != nil {
		return nil, err
	}
	return &outboxCleanupParticipant{ProjectLifecycleParticipant: stop, authority: authority, outbox: outbox}, nil
}

type outboxCleanupParticipant struct {
	c.ProjectLifecycleParticipant
	authority *LifecycleAuthority
	outbox    oc.ProjectLifecycleParticipant
}

func (p *outboxCleanupParticipant) Cleanup(ctx context.Context, actor identity.Actor, cause c.LifecycleCause, scope c.ScopeRef, checkpoint *c.CleanupCheckpoint) (c.CleanupReport, error) {
	if ctx == nil || cause.Validate() != nil || scope.Validate() != nil {
		return c.CleanupReport{}, invalid()
	}
	if p == nil || !p.authority.bound() || nilPort(p.outbox) {
		return c.CleanupReport{}, fault(f.DependencyUnbound)
	}
	// D06 persists its own scan position. There is no external cursor to
	// replay, substitute or treat as permission to skip canonical inspection.
	if checkpoint != nil {
		return c.CleanupReport{}, invalid()
	}
	project, err := lifecycleActorProject(actor, cause)
	if err != nil {
		return c.CleanupReport{}, err
	}
	if project != scope.ProjectID || cause.Action != c.Delete {
		return c.CleanupReport{}, fault(f.Forbidden)
	}
	if scope.Kind != c.ProjectScope {
		return c.CleanupReport{}, fault(f.DependencyUnbound)
	}
	name, err := outboxStopName(p.outbox)
	if err != nil {
		return c.CleanupReport{}, err
	}
	if name != string(c.OutboxParticipant) {
		return c.CleanupReport{}, unavailable(nil)
	}
	ctx, end := context.WithTimeout(ctx, 2*time.Second)
	defer end()
	if err = ctx.Err(); err != nil {
		return c.CleanupReport{}, portError(err)
	}
	txCause, err := readCause("outbox-cleanup-preflight")
	if err != nil {
		return c.CleanupReport{}, err
	}
	validated := false
	result := p.authority.store.WithinTx(ctx, txCause, func(ctx context.Context, tx f.Tx) error {
		if err := p.authority.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(project, f.Shared)}); err != nil {
			return portError(err)
		}
		if err := p.authority.validateOutboxCleanupInTx(ctx, tx, project, cause); err != nil {
			return err
		}
		validated = true
		return nil
	})
	if err = commitError(result); err != nil {
		return c.CleanupReport{}, err
	}
	if err = ctx.Err(); err != nil {
		return c.CleanupReport{}, portError(err)
	}
	if !validated {
		return c.CleanupReport{}, unavailable(nil)
	}
	operation, err := f.ParseID[oc.LifecycleOperation](cause.OperationID.String())
	if err != nil {
		return c.CleanupReport{}, invalid()
	}
	downstream, err := oc.NewLifecycleCause(oc.LifecycleDetails{ProjectID: project, OperationID: operation, Action: oc.DeleteProject, ProjectVersion: cause.ProjectVersion})
	if err != nil {
		return c.CleanupReport{}, invalid()
	}
	// No preflight transaction or Project lock survives delegation. The
	// original D06 call owns and reauthorizes each of its real transactions.
	observed, err := callOutboxCleanup(p.outbox, ctx, actor, downstream)
	if err != nil {
		return c.CleanupReport{}, err
	}
	if err = ctx.Err(); err != nil {
		return c.CleanupReport{}, portError(err)
	}
	if observed.Removed.Validate() != nil || observed.Remaining.Validate() != nil || observed.Completed && observed.Remaining != 0 {
		return c.CleanupReport{}, unavailable(nil)
	}
	details := c.CleanupDetails{State: c.CleanupPending, SafeReason: c.ReasonWorkPending}
	if observed.Completed {
		details = c.CleanupDetails{State: c.CleanupCompleted}
	}
	// Counts do not identify resources. Never invent RemainingRefs or a
	// Project cursor from the internal D06 scan or its aggregate counts.
	return c.NewCleanupReport(c.OutboxParticipant, cause, scope, details)
}

func callOutboxCleanup(port oc.ProjectLifecycleParticipant, ctx context.Context, actor identity.Actor, cause oc.LifecycleCause) (report oc.CleanupReport, err error) {
	defer func() {
		if recover() != nil {
			report, err = oc.CleanupReport{}, unavailable(nil)
		}
	}()
	return port.Cleanup(ctx, actor, cause)
}

var _ c.ProjectLifecycleParticipant = (*outboxCleanupParticipant)(nil)
