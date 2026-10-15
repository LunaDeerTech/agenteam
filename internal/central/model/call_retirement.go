package model

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// Matches Secret's ModelCallLeaseRetirementObservation without granting raw
// access to its ledger or to any credential material.
type modelCallLeaseRetirementObservation interface {
	ModelCallLeaseReleasedInTx(context.Context, f.Tx, sc.UsageRequest, sc.UsageDependencies) (bool, error)
}

type ModelCallLeaseRetirement struct {
	authority *RuntimeAuthority
	secrets   sc.UsageOperations
	observer  modelCallLeaseRetirementObservation
}

func NewModelCallLeaseRetirement(authority *RuntimeAuthority, secrets sc.UsageOperations) (*ModelCallLeaseRetirement, error) {
	observer, ok := secrets.(modelCallLeaseRetirementObservation)
	if authority.state() == nil || nilPort(secrets) || !ok || nilPort(observer) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ModelCallLeaseRetirement{authority, secrets, observer}, nil
}

type modelCallRetirementPlan struct {
	owner    *ModelCallLeaseRetirement
	binding  f.Digest
	consumer mc.ConsumerDependencies
	secret   sc.UsageDependencies
	locks    []f.LockRequest
}

func (p *modelCallRetirementPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return append([]f.LockRequest(nil), p.locks...)
}
func (*modelCallRetirementPlan) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "model_call_retirement_plan")
}
func (*modelCallRetirementPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"model_call_retirement_plan"`), nil
}
func (*modelCallRetirementPlan) LogValue() slog.Value {
	return slog.StringValue("model_call_retirement_plan")
}

type modelCallRetirementKey struct{}
type modelCallRetirementWitness struct {
	authority *RuntimeAuthority
	request   mc.ModelCallRetirementRequest
	usage     sc.UsageRequest
	consumer  mc.ConsumerDependencies
	mapping   f.Digest
	locks     []f.LockRequest
	mu        sync.Mutex
	active    bool
	validated f.Tx
}

func modelCallRetirementBinding(r mc.ModelCallRetirementRequest) (f.Digest, error) {
	if r.Validate() != nil {
		return "", fault(f.InvalidArgument)
	}
	bytes, err := encoded(struct {
		Actor   i.ActorDetails
		Model   mc.ResolvedModel
		Version f.Version
	}{r.Actor.Details(), r.Model.Clone(), r.TerminalVersion})
	if err != nil {
		return "", err
	}
	return hash(bytes), nil
}

func (p *ModelCallLeaseRetirement) witness(request mc.ModelCallRetirementRequest, consumer mc.ConsumerDependencies) (*modelCallRetirementWitness, error) {
	binding, err := modelCallRetirementBinding(request)
	if err != nil || consumer.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	ref := request.Model.CredentialLease.CredentialRef
	actor, err := p.authority.state().auth.SecretService.Actor(request.Model.LeaseOwner.Details().ID, ref.Details().Scope)
	if err != nil {
		return nil, runtimePortError(err)
	}
	usage := sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.Model, LeaseOwner: request.Model.LeaseOwner, LeaseID: request.Model.CredentialLease.LeaseID, Action: sc.ReleaseLeaseUsage}
	raw, err := encoded(struct{ Request, Consumer f.Digest }{binding, consumer.Details().Mapping})
	if err != nil {
		return nil, err
	}
	call := request.ConsumerRequest().CallID
	cause, err := runtimeCallCause(request.Model.Consumer.ProjectID, *call)
	if err != nil {
		return nil, err
	}
	identity, err := resolutionUnitIdentity(request.Model.Consumer, request.Model.Consumer.Purpose, request.Model.LeaseOwner.Details())
	if err != nil {
		return nil, err
	}
	locks, err := resolutionUnion(consumer.RequiredLocks(), []f.LockRequest{commandLock(cause.Details().Primary), commandLock(identity), projectLock(request.Model.Consumer.ProjectID.String()), recordLock(f.ReferenceRecordLock, "model-snapshot:"+request.Model.Snapshot.ID.String())})
	if err != nil {
		return nil, err
	}
	return &modelCallRetirementWitness{authority: p.authority, request: request.Clone(), usage: usage, consumer: consumer, mapping: hash(raw), locks: locks, active: true}, nil
}
func (w *modelCallRetirementWitness) retire() {
	w.mu.Lock()
	w.active = false
	w.mu.Unlock()
}

func (p *ModelCallLeaseRetirement) DiscoverModelCallRetirement(ctx context.Context, request mc.ModelCallRetirementRequest) (mc.ModelCallRetirementPlan, error) {
	if err := runtimeContextError(ctx); err != nil {
		return nil, err
	}
	if p == nil || p.authority.state() == nil {
		return nil, fault(f.DependencyUnbound)
	}
	binding, err := modelCallRetirementBinding(request)
	if err != nil {
		return nil, err
	}
	consumer, err := p.authority.discoverConsumer(ctx, request.ConsumerRequest())
	if err != nil {
		return nil, err
	}
	w, err := p.witness(request, consumer)
	if err != nil {
		return nil, err
	}
	defer w.retire()
	secret, err := p.secrets.DiscoverUsage(context.WithValue(ctx, modelCallRetirementKey{}, w), w.usage)
	if err != nil {
		return nil, runtimePortError(err)
	}
	usageBinding, _ := sc.UsageBinding(w.usage)
	if secret.Validate() != nil || !secret.ProviderPlan().Matches(p.authority.state().secretIssuer, usageBinding, w.mapping) {
		return nil, fault(f.Forbidden)
	}
	locks, err := resolutionUnion(w.locks, secret.RequiredLocks())
	if err != nil {
		return nil, err
	}
	if err = runtimeContextError(ctx); err != nil {
		return nil, err
	}
	return &modelCallRetirementPlan{p, binding, consumer, secret, locks}, nil
}

func (p *ModelCallLeaseRetirement) prepare(ctx context.Context, tx f.Tx, request mc.ModelCallRetirementRequest, supplied mc.ModelCallRetirementPlan) (*modelCallRetirementPlan, *modelCallRetirementWitness, error) {
	if err := runtimeContextError(ctx); err != nil {
		return nil, nil, err
	}
	if p == nil || p.authority.state() == nil {
		return nil, nil, fault(f.DependencyUnbound)
	}
	plan, ok := supplied.(*modelCallRetirementPlan)
	binding, err := modelCallRetirementBinding(request)
	if !ok || plan == nil || plan.owner != p || err != nil || plan.binding != binding {
		return nil, nil, fault(f.Forbidden)
	}
	if _, err = p.authority.state().store.InTx(tx); err != nil {
		return nil, nil, runtimePortError(err)
	}
	if err = p.authority.state().store.RequireHeldLocks(ctx, tx, plan.locks); err != nil {
		return nil, nil, runtimePortError(err)
	}
	w, err := p.witness(request, plan.consumer)
	return plan, w, err
}

func (p *ModelCallLeaseRetirement) ReleaseModelCallInTx(ctx context.Context, tx f.Tx, request mc.ModelCallRetirementRequest, supplied mc.ModelCallRetirementPlan) error {
	plan, w, err := p.prepare(ctx, tx, request, supplied)
	if err != nil {
		return err
	}
	defer w.retire()
	result, err := p.secrets.ApplyUsageInTx(context.WithValue(ctx, modelCallRetirementKey{}, w), tx, w.usage, plan.secret)
	if err != nil {
		return runtimePortError(err)
	}
	if result.Action() != sc.ReleaseLeaseUsage {
		return fault(f.Forbidden)
	}
	return runtimeContextError(ctx)
}

func (p *ModelCallLeaseRetirement) ModelCallRetiredInTx(ctx context.Context, tx f.Tx, request mc.ModelCallRetirementRequest, supplied mc.ModelCallRetirementPlan) (bool, error) {
	plan, w, err := p.prepare(ctx, tx, request, supplied)
	if err != nil {
		return false, err
	}
	defer w.retire()
	released, err := p.observer.ModelCallLeaseReleasedInTx(context.WithValue(ctx, modelCallRetirementKey{}, w), tx, w.usage, plan.secret)
	if err != nil {
		return false, runtimePortError(err)
	}
	if err = runtimeContextError(ctx); err != nil {
		return false, err
	}
	return released, nil
}

func (a *RuntimeAuthority) modelCallRetirementWitness(ctx context.Context, request sc.UsageRequest) (*modelCallRetirementWitness, error) {
	if err := runtimeContextError(ctx); err != nil {
		return nil, err
	}
	w, ok := ctx.Value(modelCallRetirementKey{}).(*modelCallRetirementWitness)
	if !ok || w == nil || w.authority != a || a.state() == nil {
		return nil, fault(f.DependencyUnbound)
	}
	w.mu.Lock()
	active := w.active
	w.mu.Unlock()
	actual, err := sc.UsageBinding(request)
	want, e := sc.UsageBinding(w.usage)
	if !active || err != nil || e != nil || actual != want {
		return nil, fault(f.Forbidden)
	}
	return w, nil
}

func (a *RuntimeAuthority) discoverModelCallRetirement(ctx context.Context, request sc.UsageRequest) (sc.UsageDependencies, error) {
	w, err := a.modelCallRetirementWitness(ctx, request)
	if err != nil {
		return sc.UsageDependencies{}, err
	}
	binding, _ := sc.UsageBinding(request)
	return sc.NewUsageDependencies(a.state().secretIssuer, binding, w.mapping, w.locks)
}

func (a *RuntimeAuthority) validateModelCallRetirement(ctx context.Context, tx f.Tx, request sc.UsageRequest, plan sc.UsageDependencies) error {
	w, err := a.modelCallRetirementWitness(ctx, request)
	if err != nil {
		return err
	}
	binding, _ := sc.UsageBinding(request)
	if !plan.Matches(a.state().secretIssuer, binding, w.mapping) {
		return fault(f.Forbidden)
	}
	if err = a.validateConsumer(ctx, tx, w.request.ConsumerRequest(), w.consumer); err != nil {
		return err
	}
	// No admission or original Runtime owner may still hold this call. The
	// command lock also orders a preflight racing with this release.
	a.state().mu.Lock()
	runtime := a.state().runtime
	a.state().mu.Unlock()
	if runtime == nil {
		return fault(f.DependencyUnbound)
	}
	runtime.mu.Lock()
	busy := false
	for _, call := range runtime.calls {
		if call.request.Model.LeaseOwner.Equal(w.request.Model.LeaseOwner) {
			busy = true
			break
		}
	}
	runtime.mu.Unlock()
	if busy {
		return fault(f.ResourceBusy)
	}
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return runtimePortError(err)
	}
	if err = a.state().store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return runtimePortError(err)
	}
	record, err := loadRuntimeRecord(ctx, x, *w.request.ConsumerRequest().CallID)
	if err != nil {
		return err
	}
	if record != nil && !record.retired {
		return fault(f.ResourceBusy)
	}
	if _, err = loadRuntimeSnapshot(ctx, x, mc.ModelRequest{Consumer: w.request.Model.Consumer, Model: w.request.Model}); err != nil {
		return err
	}
	if err = runtimeContextError(ctx); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.active {
		return fault(f.Forbidden)
	}
	w.validated = tx
	return nil
}

func (a *RuntimeAuthority) authorizeModelCallRetirement(ctx context.Context, tx f.Tx, actor i.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	w, ok := ctx.Value(modelCallRetirementKey{}).(*modelCallRetirementWitness)
	if !ok || w == nil {
		return sc.UseGrant{}, fault(f.DependencyUnbound)
	}
	if _, err := a.modelCallRetirementWitness(ctx, w.usage); err != nil {
		return sc.UseGrant{}, err
	}
	w.mu.Lock()
	valid := w.active && w.validated == tx
	w.mu.Unlock()
	if !valid || action != sc.ReleaseLease || !actor.Equal(w.usage.Actor) || !ref.Equal(w.usage.Ref) || !owner.Equal(w.usage.LeaseOwner) {
		return sc.UseGrant{}, fault(f.Forbidden)
	}
	return sc.UseGrant{Subject: actor, Consumer: sc.Model}, nil
}

var _ mc.ModelCallRetirement = (*ModelCallLeaseRetirement)(nil)
