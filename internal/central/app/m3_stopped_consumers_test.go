package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

type stoppedRouteProbe struct {
	mc.ConsumerAuthority
	ctx     context.Context
	binding f.Digest
	calls   int
	err     error
}

func (p *stoppedRouteProbe) check(ctx context.Context, r mc.StoppedCallObservationRequest) error {
	binding, err := mc.StoppedCallObservationBinding(r)
	if err != nil || binding != p.binding || ctx != p.ctx {
		return errors.New("original stopped caller changed")
	}
	p.calls++
	return p.err
}
func (p *stoppedRouteProbe) DiscoverStoppedCall(ctx context.Context, r mc.StoppedCallObservationRequest) (mc.ConsumerDependencies, error) {
	return mc.ConsumerDependencies{}, p.check(ctx, r)
}
func (p *stoppedRouteProbe) ValidateStoppedCallInTx(ctx context.Context, _ f.Tx, r mc.StoppedCallObservationRequest, _ mc.ConsumerDependencies) error {
	return p.check(ctx, r)
}

func TestM3StoppedCallRoutesOriginalDomainWithoutFallback(t *testing.T) {
	for _, kind := range []mc.ConsumerKind{mc.AgentConsumer, mc.KnowledgeConsumer, mc.MemoryConsumer, mc.MeetingConsumer} {
		t.Run(string(kind), func(t *testing.T) {
			project, _ := f.NewID[id.Project]()
			agent, _ := f.NewID[id.Agent]()
			execution, _ := f.NewID[id.Execution]()
			operation, _ := f.NewID[struct{}]()
			call, _ := f.NewID[mc.Call]()
			snapshot, _ := f.NewID[mc.Snapshot]()
			scope, _ := id.InProject(project)
			consumer := mc.Consumer{Kind: kind, ProjectID: project, OperationID: operation.String(), Purpose: mc.KnowledgeEmbedding}
			input := mc.InputIdentity{OperationID: operation.String(), Digest: f.Digest("sha256:" + strings.Repeat("a", 64)), SchemaVersion: 1}
			name := id.KnowledgeRuntime
			var actor id.Actor
			switch kind {
			case mc.AgentConsumer:
				consumer.Purpose, consumer.OperationID, consumer.AgentID, consumer.ExecutionID = mc.AgentGeneration, "", &agent, &execution
				input.OperationID, input.ExecutionID, input.RoundID = "", &execution, operation.String()
				actor, _ = id.NewAgentRun(project, agent, execution)
			case mc.MemoryConsumer:
				consumer.Purpose, consumer.AgentID, name = mc.MemoryEmbedding, &agent, id.MemoryRuntime
			case mc.MeetingConsumer:
				meeting, _ := f.NewID[struct{}]()
				consumer.Purpose, consumer.MeetingID, name = mc.MeetingSummaryInitial, meeting.String(), id.MeetingRuntime
			}
			if kind != mc.AgentConsumer {
				registration, err := id.RegisterService(name)
				if err != nil {
					t.Fatal(err)
				}
				actor, err = registration.Actor(operation.String(), scope)
				if err != nil {
					t.Fatal(err)
				}
			}
			request := mc.StoppedCallObservationRequest{Actor: actor, Consumer: consumer, CallID: call, Input: input, SnapshotID: snapshot}
			binding, err := mc.StoppedCallObservationBinding(request)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			denied := errors.New("original owner rejected")
			selected := &stoppedRouteProbe{ctx: ctx, binding: binding, err: denied}
			other := &stoppedRouteProbe{}
			router := m3ConsumerRouter{agents: other, knowledge: other, memory: other, meeting: other}
			var target *mc.ConsumerAuthority
			switch kind {
			case mc.AgentConsumer:
				target = &router.agents
			case mc.KnowledgeConsumer:
				target = &router.knowledge
			case mc.MemoryConsumer:
				target = &router.memory
			case mc.MeetingConsumer:
				target = &router.meeting
			}
			*target = selected
			if _, err = router.DiscoverStoppedCall(ctx, request); !errors.Is(err, denied) {
				t.Fatal("discovery bypassed original owner", err)
			}
			if err = router.ValidateStoppedCallInTx(ctx, f.Tx{}, request, mc.ConsumerDependencies{}); !errors.Is(err, denied) {
				t.Fatal("transaction bypassed original owner", err)
			}
			*target = nil
			_, err = router.DiscoverStoppedCall(ctx, request)
			lifecycleAssemblyFault(t, err, f.DependencyUnbound)
			if selected.calls != 2 || other.calls != 0 {
				t.Fatal("domain rejection or missing authority fell through")
			}
		})
	}
}
