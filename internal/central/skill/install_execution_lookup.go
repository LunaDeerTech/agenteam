package skill

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type installLookupKey struct{}
type installLookupCall struct {
	call     *installExecutionCall
	provider sc.InstallLookupAuthority
	request  sc.InstallLookupRequest
}

// This is a distinct read binding. It never supplies a mutation plan or makes
// the original synchronous installation handoff live again.
func (a *Authority) bindInstallLookup(ctx context.Context, actor id.Actor, meta f.CommandMeta, project id.ProjectID, request InstallRequest) (context.Context, error) {
	if ctx == nil || actor.Validate() != nil || meta.Validate() != nil || meta.ExpectedVersion != nil || project.Validate() != nil || request.Validate() != nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, portError(err)
	}
	if a.state() == nil {
		return nil, fault(f.DependencyUnbound)
	}
	provider, ok := a.state().installExecution.(sc.InstallLookupAuthority)
	if actor.Details().Kind != id.AgentRun || !ok {
		return a.bindInstallExecution(ctx, actor, meta, project, request, false)
	}
	if nilPort(provider) {
		return nil, fault(f.DependencyUnbound)
	}
	command, err := installIdentity(project, meta.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	input := request.data()
	r := sc.InstallExecutionRequest{Actor: actor, ProjectID: project, Command: command, RequestID: meta.RequestID, Intent: id.Read, SkillID: input.skill, NormalizedName: input.normalized, PackageSHA256: input.packageDigest, ManifestSHA256: input.manifestDigest, ByteSize: input.size}
	lookup := sc.InstallLookupRequest{Original: r}
	if lookup.Validate() != nil {
		return nil, invalid()
	}
	plan, err := provider.DiscoverInstallLookup(ctx, lookup)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(plan) || !plan.Binding().Matches(r) {
		return nil, fault(f.Forbidden)
	}
	locks, err := oc.NormalizeAccessLocks(plan.RequiredLocks())
	if err != nil || len(locks) == 0 {
		return nil, unavailable(err)
	}
	call := &installExecutionCall{authority: a, read: r, readPlan: plan, readLocks: locks, binding: plan.Binding()}
	ctx = context.WithValue(ctx, installExecutionKey{}, call)
	return context.WithValue(ctx, installLookupKey{}, &installLookupCall{call: call, provider: provider, request: lookup}), nil
}

// The existing installation requirement will call this only for the private
// lookup context. The ordinary live branch retains its original requirements.
func (a *Authority) requireInstallLookup(ctx context.Context, tx f.Tx, actor id.Actor, project id.ProjectID, intent id.AccessIntent) (bool, error) {
	lookup, _ := ctx.Value(installLookupKey{}).(*installLookupCall)
	if lookup == nil {
		return false, nil
	}
	if intent != id.Read || lookup.call == nil || lookup.call.authority != a || lookup.request.Validate() != nil {
		return true, fault(f.Forbidden)
	}
	call, err := a.installCall(ctx, actor, project)
	if err != nil || call != lookup.call || !call.read.Equal(lookup.request.Original) || nilPort(lookup.provider) {
		return true, fault(f.Forbidden)
	}
	state := a.state()
	if _, err = state.store.InTx(tx); err != nil {
		return true, portError(err)
	}
	if err = state.store.RequireHeldLocks(ctx, tx, call.readLocks); err != nil {
		return true, portError(err)
	}
	return true, portError(lookup.provider.RequireInstallLookupInTx(ctx, tx, lookup.request, call.readPlan))
}
