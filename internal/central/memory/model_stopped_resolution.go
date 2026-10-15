package memory

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"sync/atomic"
)

type stoppedResolutionKey struct{}
type stoppedResolutionWitness struct {
	authority  *ModelAuthority
	row        stoppedModelRow
	request    mc.StoppedResolutionRequest
	mapping    f.Digest
	current    oc.ProcessID
	retirement *mc.ModelCallRetirementRequest
	active     atomic.Bool
}

func (row stoppedModelRow) resolutionRequest(a *ModelAuthority) (mc.StoppedResolutionRequest, error) {
	if row.Process == nil || row.ResolveBinding == nil {
		return mc.StoppedResolutionRequest{}, fault(f.ResourceBusy)
	}
	scope, err := i.InProject(row.Project)
	if err != nil {
		return mc.StoppedResolutionRequest{}, err
	}
	actor, err := a.registration.Actor(row.Operation.String(), scope)
	if err != nil {
		return mc.StoppedResolutionRequest{}, err
	}
	owner, err := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, row.Call.String())
	if err != nil {
		return mc.StoppedResolutionRequest{}, err
	}
	agent := row.Agent
	consumer := mc.Consumer{Kind: mc.MemoryConsumer, ProjectID: row.Project, AgentID: &agent, OperationID: row.Operation.String(), Purpose: row.Purpose}
	base := mc.ResolveRequest{Actor: actor, Consumer: consumer, Purpose: row.Purpose, LeaseOwner: owner}
	resolve, err := restoreModelResolution(row.ResolveSource, *row.ResolveBinding, base)
	if err != nil {
		return mc.StoppedResolutionRequest{}, err
	}
	r := mc.StoppedResolutionRequest{Resolve: resolve, CallID: row.Call, Input: mc.InputIdentity{OperationID: row.Operation.String(), Digest: row.Input, SchemaVersion: 1}, PreparedProcessID: *row.Process}
	return r, r.Validate()
}
func (a *ModelAuthority) resolutionWitness(ctx context.Context) (*stoppedResolutionWitness, error) {
	if a == nil || ctx == nil {
		return nil, fault(f.Forbidden)
	}
	w, _ := ctx.Value(stoppedResolutionKey{}).(*stoppedResolutionWitness)
	if w == nil || w.authority != a || !w.active.Load() || w.current != a.process || w.row.Process == nil || *w.row.Process == a.process || w.request.Validate() != nil {
		return nil, fault(f.Forbidden)
	}
	return w, checkContext(ctx)
}
func (w *stoppedResolutionWitness) locks() ([]f.LockRequest, error) {
	key, _ := f.RecordLock(f.CommandRecordLock, "memory.model:"+w.row.Call.String())
	project, _ := f.ProjectLock(w.row.Project.String())
	return normalizeLocks([]f.LockRequest{{Key: key, Mode: f.Exclusive}, {Key: project, Mode: f.Shared}, namespaceLock(c.Namespace{ProjectID: w.row.Project, AgentID: w.row.Agent}, f.Exclusive)})
}
func (a *ModelAuthority) DiscoverStoppedResolution(ctx context.Context, r mc.StoppedResolutionRequest) (mc.ConsumerDependencies, error) {
	w, err := a.resolutionWitness(ctx)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	binding, err := mc.StoppedResolutionBinding(r)
	expected, e := mc.StoppedResolutionBinding(w.request)
	if err != nil || e != nil || binding != expected {
		return mc.ConsumerDependencies{}, fault(f.Forbidden)
	}
	locks, err := w.locks()
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	return mc.NewConsumerDependencies(a.issuer, mc.ConsumerDependencyDetails{Binding: binding, Mapping: w.mapping, Locks: locks})
}
func (a *ModelAuthority) ValidateStoppedResolutionInTx(ctx context.Context, tx f.Tx, r mc.StoppedResolutionRequest, plan mc.ConsumerDependencies) error {
	w, err := a.resolutionWitness(ctx)
	if err != nil {
		return err
	}
	expected, err := a.DiscoverStoppedResolution(ctx, r)
	if err != nil {
		return err
	}
	if !plan.Matches(a.issuer, expected.Details().Binding, w.mapping) || plan.Details().RetryPolicy != nil {
		return fault(f.Forbidden)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, plan.RequiredLocks()); err != nil {
		return portError(err)
	}
	return a.validateStoppedResolutionRow(ctx, tx, w, false)
}
func (a *ModelAuthority) validateStoppedResolutionRow(ctx context.Context, tx f.Tx, w *stoppedResolutionWitness, allowRetired bool) error {
	if a.guard == nil {
		return fault(f.DependencyUnbound)
	}
	if err := a.guard.RequireCurrentProcessInTx(ctx, tx, a.process); err != nil {
		return portError(err)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	row, err := loadStoppedModelRow(ctx, x, w.row.Call)
	if err != nil {
		return err
	}
	actual, err := row.mapping()
	if err != nil {
		return err
	}
	if actual == w.mapping {
		return nil
	}
	if allowRetired {
		after := w.row
		after.Retired = true
		mapping, err := after.mapping()
		if err == nil && actual == mapping {
			return nil
		}
	}
	return fault(f.ConfirmationStale)
}

// A stopped resolution grants only the exact terminal lease retirement. It
// cannot be used as the normal invocation witness for Resolve or Begin.
func (a *ModelAuthority) discoverStoppedRetirement(ctx context.Context, r mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
	w, err := a.resolutionWitness(ctx)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	if w.retirement == nil || w.retirement.Validate() != nil || w.row.TerminalVersion == nil || *w.row.TerminalVersion != w.retirement.TerminalVersion {
		return mc.ConsumerDependencies{}, fault(f.Forbidden)
	}
	expected, err := mc.ConsumerBinding(w.retirement.ConsumerRequest())
	binding, e := mc.ConsumerBinding(r)
	if err != nil || e != nil || binding != expected {
		return mc.ConsumerDependencies{}, fault(f.Forbidden)
	}
	locks, err := w.locks()
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	return mc.NewConsumerDependencies(a.issuer, mc.ConsumerDependencyDetails{Binding: binding, Mapping: w.mapping, Locks: locks})
}
func (a *ModelAuthority) validateStoppedRetirement(ctx context.Context, tx f.Tx, r mc.ConsumerRequest, plan mc.ConsumerDependencies) error {
	w, err := a.resolutionWitness(ctx)
	if err != nil {
		return err
	}
	expected, err := a.discoverStoppedRetirement(ctx, r)
	if err != nil {
		return err
	}
	if !plan.Matches(a.issuer, expected.Details().Binding, w.mapping) || plan.Details().RetryPolicy != nil {
		return fault(f.Forbidden)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, plan.RequiredLocks()); err != nil {
		return portError(err)
	}
	return a.validateStoppedResolutionRow(ctx, tx, w, true)
}

var _ mc.StoppedResolutionAuthority = (*ModelAuthority)(nil)
