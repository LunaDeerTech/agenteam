package project

import (
	"context"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// NewSecretStopParticipant binds the Project gate barrier for the Secret
// library. It does not stop users of Secret leases or provide Cleanup.
func NewSecretStopParticipant(authority *LifecycleAuthority) (c.ProjectLifecycleParticipant, error) {
	return newDomainBarrierParticipant(authority, c.SecretParticipant)
}

// NewAuditStopParticipant binds the Project gate barrier for the Audit library.
// Other participants still own their technical and permitted read Audit work.
func NewAuditStopParticipant(authority *LifecycleAuthority) (c.ProjectLifecycleParticipant, error) {
	return newDomainBarrierParticipant(authority, c.AuditParticipant)
}

type domainBarrierParticipant struct {
	authority *LifecycleAuthority
	name      c.ParticipantName
}

func newDomainBarrierParticipant(authority *LifecycleAuthority, name c.ParticipantName) (c.ProjectLifecycleParticipant, error) {
	if !authority.bound() {
		return nil, fault(f.DependencyUnbound)
	}
	return &domainBarrierParticipant{authority: authority, name: name}, nil
}

func (p *domainBarrierParticipant) Name() c.ParticipantName { return p.name }

func (p *domainBarrierParticipant) RequestStop(ctx context.Context, actor identity.Actor, cause c.LifecycleCause, scope c.ScopeRef) (c.StopReport, error) {
	return p.observe(ctx, actor, cause, scope, false)
}

func (p *domainBarrierParticipant) InspectStop(ctx context.Context, actor identity.Actor, cause c.LifecycleCause, scope c.ScopeRef) (c.StopReport, error) {
	return p.observe(ctx, actor, cause, scope, true)
}

func (*domainBarrierParticipant) Cleanup(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef, *c.CleanupCheckpoint) (c.CleanupReport, error) {
	return c.CleanupReport{}, fault(f.DependencyUnbound)
}

func (p *domainBarrierParticipant) observe(ctx context.Context, actor identity.Actor, cause c.LifecycleCause, scope c.ScopeRef, inspect bool) (c.StopReport, error) {
	if ctx == nil || scope.Validate() != nil {
		return c.StopReport{}, invalid()
	}
	if p == nil || !p.authority.bound() || p.name != c.SecretParticipant && p.name != c.AuditParticipant {
		return c.StopReport{}, fault(f.DependencyUnbound)
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
	// The synchronous call owns its transaction until WithinTx actually
	// returns. A deadline never substitutes for transaction completion.
	ctx, end := context.WithTimeout(ctx, 2*time.Second)
	defer end()
	if err = ctx.Err(); err != nil {
		return c.StopReport{}, portError(err)
	}
	txCause, err := readCause("barrier-stop-" + string(p.name))
	if err != nil {
		return c.StopReport{}, err
	}
	var candidate c.StopReport
	result := p.authority.store.WithinTx(ctx, txCause, func(ctx context.Context, tx f.Tx) error {
		if err := p.authority.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(project, f.Shared)}); err != nil {
			return portError(err)
		}
		x, err := p.authority.lifecycleExecutor(ctx, tx, project)
		if err != nil {
			return err
		}
		fact, err := p.authority.lifecycleFact(ctx, x, project, cause, p.name)
		if err != nil {
			return err
		}
		if err = fact.authorize(inspect); err != nil {
			return err
		}
		// R3 also recognizes minimal deletion receipts. These adapters have
		// no independent terminal receipt proving its version or progress.
		record, err := loadLifecycleOperation(ctx, x, project, cause.OperationID)
		if err != nil {
			return err
		}
		if record == nil {
			return fault(f.DependencyUnbound)
		}
		for _, row := range record.participants {
			if row.name != p.name {
				continue
			}
			details := c.StopDetails{State: c.Stopped}
			if fact.state != c.OperationStopping && row.stop != "stopped" {
				// Stored refs do not distinguish active from unknown. Preserve
				// them conservatively; never invent an active resource.
				details = c.StopDetails{State: c.StopPending, UnknownRefs: row.refs, SafeReason: row.reason}
				if row.stop == "failed" {
					details.State = c.StopFailed
				}
			}
			candidate, err = c.NewStopReport(p.name, cause, scope, details)
			if err != nil {
				return unavailable(err)
			}
			return nil
		}
		return fault(f.DependencyUnbound)
	})
	if err = commitError(result); err != nil {
		return c.StopReport{}, err
	}
	if err = ctx.Err(); err != nil {
		return c.StopReport{}, portError(err)
	}
	if !candidate.Matches(p.name, cause, scope) {
		return c.StopReport{}, unavailable(nil)
	}
	return candidate, nil
}

var _ c.ProjectLifecycleParticipant = (*domainBarrierParticipant)(nil)
