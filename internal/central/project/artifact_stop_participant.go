package project

import (
	"context"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/artifact"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// NewArtifactObjectLifecycleParticipant explicitly binds the original owned
// Artifact service and that service's same Object instance. The original
// cleanup-only constructor remains unchanged. This does not bind a registry.
func NewArtifactObjectLifecycleParticipant(authority *LifecycleAuthority, artifacts *artifact.Service, objects *object.Service) (c.ProjectLifecycleParticipant, error) {
	if !authority.bound() || nilPort(artifacts) || nilPort(objects) {
		return nil, fault(f.DependencyUnbound)
	}
	if err := artifacts.RequireProjectStopBinding(objects); err != nil {
		return nil, portError(err)
	}
	cleanup, err := NewArtifactLifecycleParticipant(authority, artifacts)
	if err != nil {
		return nil, err
	}
	return &artifactStopParticipant{ProjectLifecycleParticipant: cleanup, authority: authority, artifacts: artifacts, objects: objects}, nil
}

type artifactStopParticipant struct {
	c.ProjectLifecycleParticipant
	authority          *LifecycleAuthority
	artifacts, objects oc.ObjectProjectStop
}

func (*artifactStopParticipant) Name() c.ParticipantName { return c.ArtifactObjectParticipant }
func (p *artifactStopParticipant) RequestStop(ctx context.Context, actor identity.Actor, cause c.LifecycleCause, scope c.ScopeRef) (c.StopReport, error) {
	return p.stop(ctx, actor, cause, scope, false)
}
func (p *artifactStopParticipant) InspectStop(ctx context.Context, actor identity.Actor, cause c.LifecycleCause, scope c.ScopeRef) (c.StopReport, error) {
	return p.stop(ctx, actor, cause, scope, true)
}
func (p *artifactStopParticipant) stop(ctx context.Context, actor identity.Actor, cause c.LifecycleCause, scope c.ScopeRef, inspect bool) (c.StopReport, error) {
	if ctx == nil || cause.Validate() != nil || scope.Validate() != nil {
		return c.StopReport{}, invalid()
	}
	if p == nil || !p.authority.bound() || nilPort(p.artifacts) || nilPort(p.objects) {
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
	ctx, end := context.WithTimeout(ctx, 2*time.Second)
	defer end()
	if err = ctx.Err(); err != nil {
		return c.StopReport{}, portError(err)
	}
	txCause, err := readCause("artifact-object-stop-preflight")
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
		fact, err := p.authority.lifecycleFact(ctx, x, project, cause, c.ArtifactObjectParticipant)
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
		return c.StopReport{}, unavailable(nil)
	}
	operation, err := f.ParseID[oc.ProjectStopOperation](cause.OperationID.String())
	if err != nil {
		return c.StopReport{}, invalid()
	}
	downstream, err := oc.NewProjectStopCause(oc.ProjectStopCauseDetails{ProjectID: project, OperationID: operation, Action: oc.ProjectStopAction(cause.Action), ProjectVersion: cause.ProjectVersion})
	if err != nil {
		return c.StopReport{}, invalid()
	}
	// No preflight lock survives either original call. Each owner independently
	// checks current facts in its own transactions; both share the caller budget.
	reports, err := callArtifactObjectStop(ctx, actor, downstream, inspect, p.artifacts, p.objects)
	if err != nil {
		return c.StopReport{}, err
	}
	return artifactObjectStopReport(cause, scope, downstream, reports)
}

func callArtifactObjectStop(ctx context.Context, actor identity.Actor, cause oc.ProjectStopCause, inspect bool, artifacts, objects oc.ObjectProjectStop) (reports [2]oc.ObjectStopReport, err error) {
	defer func() {
		if recover() != nil {
			reports = [2]oc.ObjectStopReport{}
			err = unavailable(nil)
		}
	}()
	for n, port := range []oc.ObjectProjectStop{artifacts, objects} {
		if err = ctx.Err(); err != nil {
			return [2]oc.ObjectStopReport{}, portError(err)
		}
		if inspect {
			reports[n], err = port.InspectProjectStop(ctx, actor, cause)
		} else {
			reports[n], err = port.RequestProjectStop(ctx, actor, cause)
		}
		if err != nil {
			return [2]oc.ObjectStopReport{}, portError(err)
		}
		if err = ctx.Err(); err != nil {
			return [2]oc.ObjectStopReport{}, portError(err)
		}
		component := oc.ArtifactStopComponent
		if n == 1 {
			component = oc.ObjectStopComponent
		}
		if !reports[n].Matches(component, cause) {
			return [2]oc.ObjectStopReport{}, unavailable(nil)
		}
	}
	return reports, nil
}

func artifactObjectStopReport(cause c.LifecycleCause, scope c.ScopeRef, downstream oc.ProjectStopCause, reports [2]oc.ObjectStopReport) (c.StopReport, error) {
	if !reports[0].Matches(oc.ArtifactStopComponent, downstream) || !reports[1].Matches(oc.ObjectStopComponent, downstream) {
		return c.StopReport{}, unavailable(nil)
	}
	d := downstream.Details()
	if d.ProjectID != scope.ProjectID || d.OperationID.String() != cause.OperationID.String() || string(d.Action) != string(cause.Action) || d.ProjectVersion != cause.ProjectVersion {
		return c.StopReport{}, invalid()
	}
	details := c.StopDetails{State: c.Stopped}
	unknown := false
	for _, report := range reports {
		observed := report.Details()
		unknown = unknown || len(observed.UnknownRefs) > 0 || observed.SafeReason == oc.ProjectStopOutcomeUnknown
		if observed.State == oc.ProjectStopFailed {
			details.State = c.StopFailed
			details.SafeReason = c.SafeReason(observed.SafeReason)
		} else if observed.State == oc.ProjectStopPending && details.State != c.StopFailed {
			if details.State == c.Stopped {
				details.State, details.SafeReason = c.StopPending, c.ReasonWorkPending
			}
			if observed.SafeReason != "" && details.SafeReason == c.ReasonWorkPending {
				details.SafeReason = c.SafeReason(observed.SafeReason)
			}
		}
	}
	if unknown {
		details.State, details.SafeReason = c.StopPending, c.ReasonOutcomeUnknown
	}
	// Child references name uploads, attempts and leases rather than canonical
	// Object/Artifact identities. Preserve the stop state without inventing refs.
	return c.NewStopReport(c.ArtifactObjectParticipant, cause, scope, details)
}

var _ c.ProjectLifecycleParticipant = (*artifactStopParticipant)(nil)
