package execution

import (
	"context"
	"reflect"
	"slices"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type Authority struct{ state *authorityState }
type authorityState struct {
	store       Store
	projects    pc.ProjectAuthority
	services    c.ServiceProjectAccess
	modelIssuer mc.PlanIssuer
}

// The Authority is constructed before Agent's execution-configuration adapter;
// it does not call that adapter and therefore creates no dependency cycle.
func NewAuthority(store Store, projects pc.ProjectAuthority, services c.ServiceProjectAccess) (*Authority, error) {
	if nilPort(store) || nilPort(projects) {
		return nil, fault(f.DependencyUnbound)
	}
	if !reflect.TypeOf(store).Comparable() {
		return nil, invalid()
	}
	return &Authority{&authorityState{store: store, projects: projects, services: services, modelIssuer: mc.NewPlanIssuer()}}, nil
}
func projectLock(project i.ProjectID) f.LockRequest {
	key, _ := f.ProjectLock(project.String())
	return f.LockRequest{Key: key, Mode: f.Shared}
}
func agentLock(agent i.AgentID, mode f.LockMode) f.LockRequest {
	key, _ := f.AgentLock(agent.String())
	return f.LockRequest{Key: key, Mode: mode}
}
func executionLock(execution i.ExecutionID) f.LockRequest {
	key, _ := f.AggregateLock(f.ExecutionAggregate, execution.String())
	return f.LockRequest{Key: key, Mode: f.Exclusive}
}
func commandLock(command f.CommandIdentity) f.LockRequest {
	key, _ := f.CommandLock(command)
	return f.LockRequest{Key: key, Mode: f.Exclusive}
}
func scopeLocks(actor i.Actor, project i.ProjectID, agent i.AgentID, mode f.LockMode) []f.LockRequest {
	locks := []f.LockRequest{projectLock(project), agentLock(agent, mode)}
	if actor.Details().Kind == i.Human {
		key, _ := f.UserLock(actor.Details().UserID)
		locks = append(locks, f.LockRequest{Key: key, Mode: f.Shared})
	}
	return locks
}
func launchActor(actor i.Actor) error {
	if actor.Validate() != nil {
		return fault(f.Unauthenticated)
	}
	if kind := actor.Details().Kind; kind != i.Human && kind != i.Service {
		return fault(f.Forbidden)
	}
	return nil
}
func (a *Authority) requireScope(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, agent i.AgentID, intent i.AccessIntent) error {
	if err := launchActor(actor); err != nil {
		return err
	}
	if actor.Details().Kind == i.Human {
		grant, err := a.state.projects.RequireOwnerInTx(ctx, tx, actor, project, intent)
		if err != nil {
			return portError(err)
		}
		if !grant.Matches(actor, project) {
			return fault(f.Forbidden)
		}
		return nil
	}
	if nilPort(a.state.services) {
		return fault(f.DependencyUnbound)
	}
	return portError(a.state.services.RequireExecutionProjectInTx(ctx, tx, actor, project, agent, intent))
}

type configurationWitnessKey struct{}
type configurationWitness struct {
	owner   *authorityState
	tx      f.Tx
	request ac.ExecutionConfigurationRequest
	launch  c.LaunchRequest
	permit  c.LaunchPermit
	locks   []f.LockRequest
}

// Only the successful original source validation in Launch issues this private
// witness. Exported request constructors and valid Tx handles cannot mint it.
func (a *Authority) launchContext(ctx context.Context, tx f.Tx, request ac.ExecutionConfigurationRequest, launch c.LaunchRequest, permit c.LaunchPermit, locks []f.LockRequest) context.Context {
	return context.WithValue(ctx, configurationWitnessKey{}, configurationWitness{a.state, tx, request.Clone(), launch.Clone(), permit, slices.Clone(locks)})
}
func (a *Authority) RequireExecutionConfigurationInTx(ctx context.Context, tx f.Tx, request ac.ExecutionConfigurationRequest) error {
	if a == nil || a.state == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := request.Validate(); err != nil {
		return err
	}
	x, err := a.state.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = a.state.store.RequireHeldLocks(ctx, tx, request.RequiredLocks()); err != nil {
		return portError(err)
	}
	if request.Stage == ac.ExecutionConfigurationCapture {
		return a.requireCaptureConfiguration(ctx, tx, request)
	}
	if request.Stage == ac.ExecutionConfigurationCurrent {
		return a.requireModelCurrentConfiguration(ctx, tx, request)
	}
	if request.Stage != ac.ExecutionConfigurationLaunch {
		// Preparing capture and sealed Snapshot/current proofs are separately
		// installed by their actual writer. A valid Actor or created row is not
		// a substitute and cannot prematurely authorize running Tool calls.
		return fault(f.DependencyUnbound)
	}
	w, ok := ctx.Value(configurationWitnessKey{}).(configurationWitness)
	if !ok || w.owner != a.state || w.tx != tx || !w.request.Actor.Equal(request.Actor) || w.request.ProjectID != request.ProjectID || w.request.AgentID != request.AgentID || w.request.ExecutionID != request.ExecutionID || w.request.Stage != request.Stage || !w.permit.Matches(w.launch) || w.launch.ProjectID != request.ProjectID || w.launch.AgentID != request.AgentID {
		return fault(f.Forbidden)
	}
	if err = a.state.store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return portError(err)
	}
	if err = a.requireScope(ctx, tx, request.Actor, request.ProjectID, request.AgentID, i.Launch); err != nil {
		return err
	}
	// Launch proof precedes the only canonical insert in this same transaction.
	existing, err := loadExecution(ctx, x, request.ExecutionID)
	if err != nil {
		return err
	}
	if existing != nil {
		return fault(f.InvalidState)
	}
	return nil
}

var _ ac.ExecutionIdentityAuthority = (*Authority)(nil)
