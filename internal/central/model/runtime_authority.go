package model

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

type RuntimeAuthorizations struct {
	Consumers                                    mc.ConsumerAuthority
	Process                                      *object.ProcessGuard
	ModelRuntime, SecretService, OutboundService id.ServiceRegistration
}

type RuntimeAuthority struct{ data func() *runtimeAuthorityState }
type runtimeAuthorityState struct {
	store        Store
	auth         RuntimeAuthorizations
	usageIssuer  uc.PlanIssuer
	secretIssuer sc.PlanIssuer
	mu           sync.Mutex
	runtime      *runtimeState
}

func NewRuntimeAuthority(store Store, auth RuntimeAuthorizations) (*RuntimeAuthority, error) {
	if nilPort(store) || nilPort(auth.Consumers) || auth.Process == nil {
		return nil, fault(f.DependencyUnbound)
	}
	for _, pair := range []struct {
		registration id.ServiceRegistration
		name         id.ServiceName
	}{{auth.ModelRuntime, id.ModelRuntime}, {auth.SecretService, id.SecretService}, {auth.OutboundService, id.OutboundService}} {
		actor, err := pair.registration.Actor("018f0000-0000-7000-8000-000000000001", id.SystemScope())
		if err != nil || actor.Details().ServiceName != pair.name {
			return nil, fault(f.DependencyUnbound)
		}
	}
	a := &runtimeAuthorityState{store: store, auth: auth, usageIssuer: uc.NewPlanIssuer(), secretIssuer: sc.NewPlanIssuer()}
	return &RuntimeAuthority{data: func() *runtimeAuthorityState { return a }}, nil
}

func (a *RuntimeAuthority) state() *runtimeAuthorityState {
	if a == nil || a.data == nil {
		return nil
	}
	return a.data()
}

func (a *RuntimeAuthority) discoverConsumer(ctx context.Context, request mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
	if err := runtimeContextError(ctx); err != nil {
		return mc.ConsumerDependencies{}, err
	}
	if a.state() == nil {
		return mc.ConsumerDependencies{}, fault(f.DependencyUnbound)
	}
	if request.Validate() != nil {
		return mc.ConsumerDependencies{}, fault(f.InvalidArgument)
	}
	plan, err := a.state().auth.Consumers.Discover(ctx, request.Clone())
	if err != nil {
		return mc.ConsumerDependencies{}, runtimePortError(err)
	}
	binding, err := mc.ConsumerBinding(request)
	if err != nil || plan.Validate() != nil || plan.Details().Binding != binding {
		return mc.ConsumerDependencies{}, fault(f.Forbidden)
	}
	if err = runtimeContextError(ctx); err != nil {
		return mc.ConsumerDependencies{}, err
	}
	return plan, nil
}

func (a *RuntimeAuthority) validateConsumer(ctx context.Context, tx f.Tx, request mc.ConsumerRequest, plan mc.ConsumerDependencies) error {
	if err := runtimeContextError(ctx); err != nil {
		return err
	}
	if a.state() == nil {
		return fault(f.DependencyUnbound)
	}
	if _, err := a.state().store.InTx(tx); err != nil {
		return runtimePortError(err)
	}
	binding, err := mc.ConsumerBinding(request)
	if err != nil || plan.Validate() != nil || plan.Details().Binding != binding {
		return fault(f.Forbidden)
	}
	if err = a.state().store.RequireHeldLocks(ctx, tx, plan.RequiredLocks()); err != nil {
		return runtimePortError(err)
	}
	if err = a.state().auth.Consumers.ValidateInTx(ctx, tx, request.Clone(), plan); err != nil {
		return runtimePortError(err)
	}
	return runtimeContextError(ctx)
}

func runtimeRetryPolicy(request mc.ModelRequest, plan mc.ConsumerDependencies) (mc.RetryPolicy, error) {
	if plan.Validate() != nil {
		return mc.RetryPolicy{}, fault(f.Forbidden)
	}
	p := plan.Details().RetryPolicy
	if p == nil || p.ValidateFor(request.Consumer) != nil {
		return mc.RetryPolicy{}, fault(f.Forbidden)
	}
	if request.RetryClass == mc.AgentRetry && p.Class == mc.AgentRetry {
		return p.Clone(), nil
	}
	if request.RetryClass != mc.BoundedRetry || p.Class != mc.BoundedRetry || p.MaxAttempts == nil || *p.MaxAttempts != 1 || len(p.Categories) != 0 {
		return mc.RetryPolicy{}, fault(f.CapabilityUnsupported)
	}
	return p.Clone(), nil
}

// D04 owns the original Project SH transaction and the live Exchange. This
// callback reads canonical accepted facts in that Tx; it must not acquire call
// or consumer locks while holding the already acquired Project lock.
func (a *RuntimeAuthority) CheckProjectAuditInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) error {
	if err := runtimeContextError(ctx); err != nil {
		return err
	}
	if a.state() == nil {
		return fault(f.DependencyUnbound)
	}
	c, ok := ctx.Value(runtimeHandoffKey{}).(*runtimeCall)
	if !ok || c == nil || c.runtime.authority != a || entry.Validate() != nil || key.Validate() != nil {
		return fault(f.Forbidden)
	}
	e, k := entry.Fields(), key.Details()
	v := c.copyRecord()
	actor := e.Actor.Details()
	var metadata struct {
		Consumer ac.Consumer `json:"consumer"`
	}
	if e.Action != ac.AccessDeny || e.Outcome != ac.Denied || e.Resource.Details().Kind != ac.PolicyResource ||
		k.Producer != ac.AccessProducer || k.Ordinal != 0 || k.CauseRef != v.value.ID.String() ||
		actor.Kind != id.Service || actor.ServiceName != id.OutboundService || actor.CauseRef != k.CauseRef ||
		e.Scope.Details().Kind != id.ProjectScope || actor.ProjectID != v.binding.Consumer.ProjectID.String() || e.Scope.Details().ProjectID != actor.ProjectID ||
		e.Associations != c.outboundAssociations() || json.Unmarshal(e.Metadata.JSON(), &metadata) != nil || metadata.Consumer != ac.Model {
		return fault(f.Forbidden)
	}
	s := c.runtime
	s.mu.Lock()
	current := s.calls[v.value.CallID] == c
	s.mu.Unlock()
	c.mu.Lock()
	owned := c.accepted && c.handoff && !c.ioRetired && !c.joined
	c.mu.Unlock()
	if !current || !owned {
		return fault(f.Forbidden)
	}
	if err := a.state().store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(actor.ProjectID)}); err != nil {
		return runtimePortError(err)
	}
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return runtimePortError(err)
	}
	actual, err := loadRuntimeRecord(ctx, x, v.value.CallID)
	if err != nil {
		return err
	}
	if actual == nil || actual.digest != v.digest || actual.value.ID != v.value.ID || actual.value.ProcessID != v.value.ProcessID || actual.value.Fence != v.value.Fence || actual.retired || actual.value.Final != nil || actual.value.Dispatch != uc.Authorized && actual.value.Dispatch != uc.Sent {
		return fault(f.Forbidden)
	}
	process, err := a.state().auth.Process.CurrentProcess()
	if err != nil {
		return runtimePortError(err)
	}
	if process != actual.value.ProcessID {
		return fault(f.Forbidden)
	}
	return runtimeContextError(ctx)
}

func (c *runtimeCall) outboundAssociations() ac.Associations {
	return ac.Associations{OperationID: c.request.Consumer.OperationID}
}
func (c *runtimeCall) outboundContext() (outbound.CallContext, error) {
	v := c.copyRecord().value
	scope, err := id.InProject(c.request.Consumer.ProjectID)
	if err != nil {
		return outbound.CallContext{}, runtimePortError(err)
	}
	actor, err := c.runtime.authority.state().auth.OutboundService.Actor(v.ID.String(), scope)
	if err != nil {
		return outbound.CallContext{}, runtimePortError(err)
	}
	key, err := ac.NewAppendKey(ac.AccessProducer, v.ID.String(), 0)
	if err != nil {
		return outbound.CallContext{}, runtimePortError(err)
	}
	return outbound.NewCallContext(actor, scope, key, c.outboundAssociations())
}
func (c *runtimeCall) callOptions(call outbound.CallContext, material sc.SecretMaterial) wire.CallOptions {
	deadline, _ := c.wireContext().Deadline()
	remaining := time.Until(deadline)
	if remaining <= 0 {
		remaining = time.Nanosecond
	}
	return wire.CallOptions{ProjectID: c.request.Consumer.ProjectID, Context: call, Credential: material, AllowHTTP: c.runtime.deps.AllowHTTP,
		Limits: outbound.Limits{Overall: remaining, ReadIdle: min(remaining, 60*time.Second), RequestBodyBytes: 16 << 20, ResponseBodyBytes: 16 << 20}}
}

var _ ac.ProjectFactAuthority = (*RuntimeAuthority)(nil)

func (*RuntimeAuthority) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "model_runtime_authority")
}
func (*RuntimeAuthority) MarshalJSON() ([]byte, error) {
	return []byte(`"model_runtime_authority"`), nil
}
func (*RuntimeAuthority) LogValue() slog.Value { return slog.StringValue("model_runtime_authority") }
