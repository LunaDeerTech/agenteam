package skill

import (
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// Skill's private context carries only already-discovered plans. It is not the
// Runtime issuer: every use still calls that provider with its original Tx.
type installExecutionKey struct{}
type installExecutionCall struct {
	authority              *Authority
	read, mutate           sc.InstallExecutionRequest
	readPlan, mutatePlan   sc.InstallExecutionPlan
	readLocks, mutateLocks []f.LockRequest
	binding                sc.InstallExecutionBinding
}

type installationExecutionOrigin struct {
	agent     id.AgentID
	execution id.ExecutionID
	binding   sc.InstallExecutionBinding
	request   f.ID[f.Request]
}

func (a *Authority) bindInstallExecution(ctx context.Context, actor id.Actor, meta f.CommandMeta, project id.ProjectID, request InstallRequest, mutate bool) (context.Context, error) {
	if ctx == nil || actor.Validate() != nil || meta.Validate() != nil || meta.ExpectedVersion != nil || project.Validate() != nil || request.Validate() != nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, portError(err)
	}
	if actor.Details().Kind != id.AgentRun {
		if _, err := installationActor(actor); err != nil {
			return nil, err
		}
		return ctx, nil
	}
	state := a.state()
	if state == nil || nilPort(state.installExecution) {
		return nil, fault(f.DependencyUnbound)
	}
	command, err := installIdentity(project, meta.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	input := request.data()
	r := sc.InstallExecutionRequest{Actor: actor, ProjectID: project, Command: command, RequestID: meta.RequestID, Intent: id.Read, SkillID: input.skill, NormalizedName: input.normalized, PackageSHA256: input.packageDigest, ManifestSHA256: input.manifestDigest, ByteSize: input.size}
	if r.Validate() != nil {
		return nil, invalid()
	}
	call := &installExecutionCall{authority: a, read: r}
	call.readPlan, err = state.installExecution.DiscoverInstallExecution(ctx, r)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(call.readPlan) {
		return nil, fault(f.DependencyUnbound)
	}
	call.binding = call.readPlan.Binding()
	if !call.binding.Matches(r) {
		return nil, fault(f.Forbidden)
	}
	call.readLocks, err = oc.NormalizeAccessLocks(call.readPlan.RequiredLocks())
	if err != nil || len(call.readLocks) == 0 {
		return nil, unavailable(err)
	}
	if mutate {
		r.Intent = id.Mutate
		call.mutate = r
		call.mutatePlan, err = state.installExecution.DiscoverInstallExecution(ctx, r)
		if err != nil {
			return nil, portError(err)
		}
		if nilPort(call.mutatePlan) || call.mutatePlan.Binding() != call.binding {
			return nil, fault(f.Forbidden)
		}
		call.mutateLocks, err = oc.NormalizeAccessLocks(call.mutatePlan.RequiredLocks())
		if err != nil || len(call.mutateLocks) == 0 {
			return nil, unavailable(err)
		}
	}
	if err = ctx.Err(); err != nil {
		return nil, portError(err)
	}
	return context.WithValue(ctx, installExecutionKey{}, call), nil
}

func (a *Authority) installCall(ctx context.Context, actor id.Actor, project id.ProjectID) (*installExecutionCall, error) {
	if ctx == nil || a.state() == nil || nilPort(a.state().installExecution) {
		return nil, fault(f.DependencyUnbound)
	}
	call, _ := ctx.Value(installExecutionKey{}).(*installExecutionCall)
	if call == nil || call.authority != a || !call.read.Actor.Equal(actor) || call.read.ProjectID != project || !call.binding.Matches(call.read) {
		return nil, fault(f.Forbidden)
	}
	return call, nil
}

func (a *Authority) installExecutionLocks(ctx context.Context, actor id.Actor, project id.ProjectID, locks []f.LockRequest) ([]f.LockRequest, error) {
	if actor.Details().Kind != id.AgentRun {
		return locks, nil
	}
	call, err := a.installCall(ctx, actor, project)
	if err != nil {
		return nil, err
	}
	all := append(append([]f.LockRequest(nil), locks...), call.readLocks...)
	all = append(all, call.mutateLocks...)
	return oc.NormalizeAccessLocks(all)
}

func (a *Authority) requireInstallExecution(ctx context.Context, tx f.Tx, actor id.Actor, project id.ProjectID, intent id.AccessIntent) error {
	call, err := a.installCall(ctx, actor, project)
	if err != nil {
		return err
	}
	r, plan, locks := call.read, call.readPlan, call.readLocks
	if intent == id.Mutate {
		r, plan, locks = call.mutate, call.mutatePlan, call.mutateLocks
	} else if intent != id.Read {
		return fault(f.Forbidden)
	}
	if nilPort(plan) || r.Validate() != nil || plan.Binding() != call.binding {
		return fault(f.Forbidden)
	}
	state := a.state()
	if _, err = state.store.InTx(tx); err != nil {
		return portError(err)
	}
	if err = state.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	return portError(state.installExecution.RequireInstallExecutionInTx(ctx, tx, r, plan))
}

func (a *Authority) installOrigin(ctx context.Context, actor id.Actor, project id.ProjectID) (*installationExecutionOrigin, error) {
	if actor.Details().Kind != id.AgentRun {
		return nil, nil
	}
	call, err := a.installCall(ctx, actor, project)
	if err != nil {
		return nil, err
	}
	agent, err := f.ParseID[id.Agent](actor.Details().AgentID)
	if err != nil {
		return nil, invalid()
	}
	execution, err := f.ParseID[id.Execution](actor.Details().ExecutionID)
	if err != nil {
		return nil, invalid()
	}
	return &installationExecutionOrigin{agent, execution, call.binding, call.read.RequestID}, nil
}

func (r installationRow) actorKind() id.ActorKind {
	if r.execution != nil {
		return id.AgentRun
	}
	return id.Human
}
func (r installationRow) matchesActor(actor id.Actor) bool {
	if actor.Validate() != nil {
		return false
	}
	d := actor.Details()
	if r.execution == nil {
		return d.Kind == id.Human && d.UserID == r.user.String()
	}
	return d.Kind == id.AgentRun && d.ProjectID == r.project.String() && d.AgentID == r.execution.agent.String() && d.ExecutionID == r.execution.execution.String()
}
func (r installationRow) matchesExecution(call *installExecutionCall) bool {
	if r.execution == nil || call == nil {
		return false
	}
	b, current := r.execution.binding, call.binding
	return r.matchesActor(call.read.Actor) && b.OperationID == current.OperationID && b.ToolID == current.ToolID && b.SpecRevision == current.SpecRevision && b.HandlerID == current.HandlerID && b.ContractRevision == current.ContractRevision && b.Fingerprint == current.Fingerprint &&
		call.read.Command.Key() == r.key && call.read.SkillID == r.skill && call.read.NormalizedName == r.pkg.normalized && call.read.PackageSHA256 == r.pkg.packageDigest && call.read.ManifestSHA256 == r.pkg.manifestDigest && call.read.ByteSize == r.pkg.size
}

func (r installationRow) semanticDigest() (f.Digest, error) {
	if r.execution == nil {
		return installationSemantic(r.project, r.user, r.skill, r.pkg)
	}
	e := r.execution
	if r.user != (id.UserID{}) || e.agent.Validate() != nil || e.execution.Validate() != nil || e.binding.Validate() != nil || e.request.Validate() != nil || r.project.Validate() != nil || r.skill.Validate() != nil || r.pkg.validate(r.project, r.skill) != nil || r.key.String() != "tool.skill.install:"+e.binding.OperationID {
		return "", invalid()
	}
	raw, err := json.Marshal(struct {
		Format, Project, Agent, Execution, Operation, Tool, Handler string
		Spec, Contract                                              f.Version
		Fingerprint                                                 f.Digest
		Skill, Name, Normalized, Description                        string
		Package, Manifest                                           f.Digest
		Size                                                        f.Progress
	}{"skill.install.agent.v1", r.project.String(), e.agent.String(), e.execution.String(), e.binding.OperationID, e.binding.ToolID.String(), e.binding.HandlerID, e.binding.SpecRevision, e.binding.ContractRevision, e.binding.Fingerprint, r.skill.String(), r.pkg.name, r.pkg.normalized, r.pkg.description, r.pkg.packageDigest, r.pkg.manifestDigest, r.pkg.size})
	if err != nil {
		return "", unavailable(err)
	}
	return sum(raw), nil
}

func installPrincipalLocks(actor id.Actor) ([]f.LockRequest, error) {
	user, err := installationActor(actor)
	if err != nil {
		return nil, err
	}
	if actor.Details().Kind == id.Human {
		return []f.LockRequest{userLock(user.String(), f.Exclusive)}, nil
	}
	d := actor.Details()
	agent, err := f.AgentLock(d.AgentID)
	if err != nil {
		return nil, invalid()
	}
	execution, err := f.AggregateLock(f.ExecutionAggregate, d.ExecutionID)
	if err != nil {
		return nil, invalid()
	}
	return []f.LockRequest{{Key: agent, Mode: f.Shared}, {Key: execution, Mode: f.Exclusive}}, nil
}

func sameInstallationOrigin(a, b *installationExecutionOrigin) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (a *Authority) installationInput(ctx context.Context, actor id.Actor, project id.ProjectID, key f.IdempotencyKey, input installInput) (installationRow, error) {
	user, err := installationActor(actor)
	if err != nil {
		return installationRow{}, err
	}
	origin, err := a.installOrigin(ctx, actor, project)
	if err != nil {
		return installationRow{}, err
	}
	row := installationRow{project: project, user: user, execution: origin, key: key, skill: input.skill, pkg: freezeInstallation(input)}
	if origin != nil {
		call, e := a.installCall(ctx, actor, project)
		if e != nil {
			return installationRow{}, e
		}
		if !row.matchesExecution(call) {
			return installationRow{}, fault(f.Forbidden)
		}
	}
	return row, nil
}

func (a *Authority) installationInputLocks(ctx context.Context, actor id.Actor, project id.ProjectID, command f.CommandIdentity, skill sc.SkillID) ([]f.LockRequest, error) {
	locks, err := installPrincipalLocks(actor)
	if err != nil {
		return nil, err
	}
	locks = append(locks, commandLock(command), projectLock(project, f.Exclusive), skillLock(skill, f.Exclusive))
	locks, err = oc.NormalizeAccessLocks(locks)
	if err != nil {
		return nil, err
	}
	return a.installExecutionLocks(ctx, actor, project, locks)
}

func (a *Authority) executionInstallationDependencies(ctx context.Context, row installationRow, actor id.Actor, intent id.AccessIntent) (oc.AccessDependencies, error) {
	base, err := installationDependencies(row, actor, intent)
	if err != nil || actor.Details().Kind != id.AgentRun {
		return base, err
	}
	call, err := a.installCall(ctx, actor, row.project)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	if !row.matchesExecution(call) {
		return oc.AccessDependencies{}, fault(f.Forbidden)
	}
	locks, err := a.installExecutionLocks(ctx, actor, row.project, base.Locks())
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	// Bind this discovered Object access to the exact physical Runtime call,
	// while the installation command's semantic digest remains retry-stable.
	raw, err := json.Marshal([]string{base.Mapping().String(), call.binding.OperationID, call.binding.AttemptID, call.read.RequestID.String(), string(intent)})
	if err != nil {
		return oc.AccessDependencies{}, unavailable(err)
	}
	return oc.NewAccessDependencies(sum(raw), locks)
}

func (a *Authority) installationExecutionAccess(ctx context.Context, row installationRow, request oc.AccessRequest) error {
	d := request.Details()
	if d.Actor.Details().Kind != id.AgentRun {
		return nil
	}
	call, err := a.installCall(ctx, d.Actor, row.project)
	if err != nil {
		return err
	}
	if !row.matchesExecution(call) {
		return fault(f.Forbidden)
	}
	if d.Operation == oc.ReserveAccess && (d.Command == nil || d.Command.RequestID != call.read.RequestID) {
		return fault(f.Forbidden)
	}
	return nil
}
