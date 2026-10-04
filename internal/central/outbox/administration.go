package outbox

import (
	"context"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

type humanPlan struct {
	actor        identity.Actor
	scope        identity.Scope
	request      oc.ProjectRequest
	dependencies oc.Dependencies
	locks        []foundation.LockRequest
}

func eventScope(scope identity.Scope) (event.Scope, error) {
	if scope.Validate() != nil {
		return event.Scope{}, invalid()
	}
	switch d := scope.Details(); d.Kind {
	case identity.System:
		return event.Scope{Kind: event.SystemScope}, nil
	case identity.ProjectScope:
		id, err := foundation.ParseID[event.Project](d.ProjectID)
		if err != nil {
			return event.Scope{}, invalid()
		}
		return event.Scope{Kind: event.ProjectScope, ProjectID: id}, nil
	default:
		return event.Scope{}, invalid()
	}
}
func identityScope(scope event.Scope) (identity.Scope, error) {
	if scope.Validate() != nil {
		return identity.Scope{}, invalid()
	}
	if scope.Kind == event.SystemScope {
		return identity.SystemScope(), nil
	}
	id, err := foundation.ParseID[identity.Project](scope.ProjectID.String())
	if err != nil {
		return identity.Scope{}, invalid()
	}
	return identity.InProject(id)
}
func (s *Service) requireHuman(ctx context.Context, tx foundation.Tx, actor identity.Actor) error {
	if actor.Validate() != nil || actor.Details().Kind != identity.Human {
		return invalid()
	}
	if nilPort(s.state().auth.Sessions) {
		return failure(foundation.DependencyUnbound, nil)
	}
	if err := s.state().auth.Sessions.RequireCurrentSession(ctx, tx, actor); err != nil {
		return portError(err)
	}
	return nil
}
func (s *Service) planHuman(ctx context.Context, actor identity.Actor, scope identity.Scope, action oc.ProjectAction, d record, stage oc.Stage) (humanPlan, error) {
	p := humanPlan{actor: actor, scope: scope}
	if actor.Validate() != nil || actor.Details().Kind != identity.Human {
		return p, invalid()
	}
	sc, err := eventScope(scope)
	if err != nil {
		return p, err
	}
	p.locks, err = actorLocks(actor)
	if err != nil {
		return p, err
	}
	if sc.Kind == event.SystemScope {
		if nilPort(s.state().auth.System) {
			return p, failure(foundation.DependencyUnbound, nil)
		}
		return p, nil
	}
	if nilPort(s.state().auth.Projects) {
		return p, failure(foundation.DependencyUnbound, nil)
	}
	project, _ := foundation.ParseID[identity.Project](sc.ProjectID.String())
	details := oc.ProjectRequestDetails{Kind: action, ProjectID: project, Actor: actor, Stage: stage}
	if action == oc.RequeueProject {
		details.Requeue = oc.RequeueTarget{EventID: d.eventID, DeliveryID: d.id, HandlerID: d.handler, Scope: d.scope, Effect: d.effect}
	}
	p.request, err = oc.NewProjectRequest(details)
	if err != nil {
		return p, err
	}
	p.dependencies, err = s.state().auth.Projects.Discover(ctx, p.request)
	if err != nil {
		return p, portError(err)
	}
	if p.dependencies.Validate() != nil {
		return p, unavailable(nil)
	}
	k, _ := foundation.ProjectLock(project.String())
	p.locks = append(p.locks, foundation.LockRequest{Key: k, Mode: foundation.Shared})
	p.locks = append(p.locks, p.dependencies.Locks()...)
	p.locks, err = oc.NormalizeLocks(p.locks)
	return p, err
}
func (s *Service) authorizeHuman(ctx context.Context, tx foundation.Tx, p humanPlan, intent identity.AccessIntent) error {
	if err := s.requireHuman(ctx, tx, p.actor); err != nil {
		return err
	}
	if p.scope.Details().Kind == identity.System {
		grant, err := s.state().auth.System.AuthorizeSystem(ctx, tx, p.actor, intent)
		if err != nil {
			return portError(err)
		}
		if !grant.Matches(p.actor, p.scope, intent) {
			return failure(foundation.Forbidden, nil)
		}
		return nil
	}
	if err := s.state().auth.Projects.ValidateInTx(ctx, tx, p.request, p.dependencies); err != nil {
		return portError(err)
	}
	return nil
}
func (s *Service) beginAdministration(ctx context.Context) (context.Context, func(), error) {
	s.state().mu.RLock()
	r := s.state().runtime
	s.state().mu.RUnlock()
	if r == nil {
		return ctx, func() {}, nil
	}
	return r.data().beginControl(ctx)
}
