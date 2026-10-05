package model

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type resolutionNoIOConsumer struct{ mc.ConsumerAuthority }
type resolutionConsumerChan chan int

func (resolutionConsumerChan) Discover(context.Context, mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
	panic("nil channel called")
}
func (resolutionConsumerChan) ValidateInTx(context.Context, f.Tx, mc.ConsumerRequest, mc.ConsumerDependencies) error {
	panic("nil channel called")
}

func resolutionTestRequest(t *testing.T) mc.ResolveRequest {
	t.Helper()
	project, agent, execution := mustID[id.Project](t), mustID[id.Agent](t), mustID[id.Execution](t)
	actor, e := id.NewAgentRun(project, agent, execution)
	if e != nil {
		t.Fatal(e)
	}
	owner, e := sc.NewCredentialLeaseOwner(sc.ExecutionOwner, execution.String())
	if e != nil {
		t.Fatal(e)
	}
	model := mustID[mc.Model](t)
	selection := mc.SelectionRef{Kind: "direct"}
	r := mc.ResolveRequest{Actor: actor, Consumer: mc.Consumer{Kind: mc.AgentConsumer, ProjectID: project, Purpose: mc.AgentGeneration, AgentID: &agent, ExecutionID: &execution}, Purpose: mc.AgentGeneration, Source: mc.CurrentSelectionSource, ModelRef: &model, Selection: &selection, LeaseOwner: owner}
	if e = r.Validate(); e != nil {
		t.Fatal(e)
	}
	return r
}
func resolutionTestAuthority(t *testing.T, store Store) *Authority {
	t.Helper()
	registration, e := id.RegisterService(id.SecretService)
	if e != nil {
		t.Fatal(e)
	}
	a, e := NewAuthority(store, Authorizations{Sessions: sessionFunc(func(context.Context, f.Tx, id.Actor) error { panic("Session I/O") }), System: systemFunc(func(context.Context, f.Tx, id.Actor, id.AccessIntent) (id.AccessGrant, error) { panic("System I/O") }), Resolution: &ResolutionAuthorizations{Consumers: &resolutionNoIOConsumer{}, SecretService: registration}})
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func TestModelCurrentResolutionConstructionAndClosedVariants(t *testing.T) {
	store := &noIOStore{}
	a := resolutionTestAuthority(t, store)
	svc, e := New(store, a, testDependencies(t))
	if e != nil {
		t.Fatal(e)
	}
	request := resolutionTestRequest(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if p, e := svc.DiscoverResolve(ctx, request); !errors.Is(e, context.Canceled) || p.Validate() == nil {
		t.Fatal("cancellation did not return zero plan")
	}
	if v, e := svc.ResolveModel(ctx, request); !errors.Is(e, context.Canceled) || v.Validate() == nil {
		t.Fatal("cancellation did not return zero result")
	}
	_, e = svc.DiscoverResolve(nil, request)
	requireCode(t, e, f.InvalidArgument)
	old, e := New(store, pureAuthority(t, store), testDependencies(t))
	if e != nil {
		t.Fatal(e)
	}
	_, e = old.DiscoverResolve(context.Background(), request)
	requireCode(t, e, f.DependencyUnbound)
	request.ReasoningEffort = "high"
	_, e = svc.DiscoverResolve(context.Background(), request)
	requireCode(t, e, f.CapabilityUnsupported)
	request = resolutionTestRequest(t)
	_, e = svc.SelectModel(context.Background(), request.Actor, mc.SelectionRequest{Consumer: request.Consumer, ModelRef: request.ModelRef, Selection: *request.Selection})
	requireCode(t, e, f.DependencyUnbound)
}
func TestModelCurrentResolutionRegistrationAndTypedNil(t *testing.T) {
	store := &noIOStore{}
	a := resolutionTestAuthority(t, store)
	base := a.state().auth
	for _, consumer := range []mc.ConsumerAuthority{nil, (*resolutionNoIOConsumer)(nil), resolutionConsumerChan(nil)} {
		copy := base
		options := *base.Resolution
		options.Consumers = consumer
		copy.Resolution = &options
		_, e := NewAuthority(store, copy)
		requireCode(t, e, f.DependencyUnbound)
	}
	for _, name := range []id.ServiceName{id.AccountAuth, id.ObjectService} {
		copy := base
		options := *base.Resolution
		options.SecretService, _ = id.RegisterService(name)
		copy.Resolution = &options
		_, e := NewAuthority(store, copy)
		requireCode(t, e, f.DependencyUnbound)
	}
	options := *base.Resolution
	copy := base
	copy.Resolution = &options
	next, e := NewAuthority(store, copy)
	if e != nil {
		t.Fatal(e)
	}
	options.Consumers = nil
	options.SecretService = id.ServiceRegistration{}
	if next.state().auth.Resolution.Consumers == nil {
		t.Fatal("caller changed copied resolution configuration")
	}
	if _, e = next.state().auth.Resolution.SecretService.Actor("018f0000-0000-7000-8000-000000000001", id.SystemScope()); e != nil {
		t.Fatal(e)
	}
}
