package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type stoppedResolutionRouteProbe struct {
	mc.ConsumerAuthority
	ctx     context.Context
	binding f.Digest
	calls   int
	err     error
}

func (p *stoppedResolutionRouteProbe) check(ctx context.Context, request mc.StoppedResolutionRequest) error {
	binding, err := mc.StoppedResolutionBinding(request)
	if err != nil || binding != p.binding || ctx != p.ctx {
		return errors.New("original stopped resolution changed")
	}
	p.calls++
	return p.err
}
func (p *stoppedResolutionRouteProbe) DiscoverStoppedResolution(ctx context.Context, request mc.StoppedResolutionRequest) (mc.ConsumerDependencies, error) {
	return mc.ConsumerDependencies{}, p.check(ctx, request)
}
func (p *stoppedResolutionRouteProbe) ValidateStoppedResolutionInTx(ctx context.Context, _ f.Tx, request mc.StoppedResolutionRequest, _ mc.ConsumerDependencies) error {
	return p.check(ctx, request)
}

func TestM3StoppedResolutionRoutesOriginalDomainWithoutFallback(t *testing.T) {
	for _, kind := range []mc.ConsumerKind{mc.KnowledgeConsumer, mc.MemoryConsumer, mc.MeetingConsumer} {
		t.Run(string(kind), func(t *testing.T) {
			project, _ := f.NewID[id.Project]()
			agent, _ := f.NewID[id.Agent]()
			operation, _ := f.NewID[struct{}]()
			call, _ := f.NewID[mc.Call]()
			snapshot, _ := f.NewID[mc.Snapshot]()
			process, _ := f.NewID[oc.Process]()
			user, _ := f.NewID[id.User]()
			session, _ := f.NewID[id.Session]()
			actor, _ := id.NewHuman(user, session)
			consumer := mc.Consumer{Kind: kind, ProjectID: project, OperationID: operation.String(), Purpose: mc.KnowledgeEmbedding}
			switch kind {
			case mc.MemoryConsumer:
				consumer.Purpose, consumer.AgentID = mc.MemoryEmbedding, &agent
			case mc.MeetingConsumer:
				meeting, _ := f.NewID[struct{}]()
				consumer.Purpose, consumer.MeetingID = mc.MeetingSummaryInitial, meeting.String()
			}
			lease, _ := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, call.String())
			request := mc.StoppedResolutionRequest{
				Resolve: mc.ResolveRequest{Actor: actor, Consumer: consumer, Purpose: consumer.Purpose, Source: mc.ServingSnapshotSource, ServingSnapshotID: &snapshot, ServingGenerationID: operation.String(), LeaseOwner: lease},
				CallID:  call, PreparedProcessID: process,
				Input: mc.InputIdentity{OperationID: operation.String(), Digest: f.Digest("sha256:" + strings.Repeat("a", 64)), SchemaVersion: 1},
			}
			binding, err := mc.StoppedResolutionBinding(request)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			denied := errors.New("original resolution owner rejected")
			selected := &stoppedResolutionRouteProbe{ctx: ctx, binding: binding, err: denied}
			other := &stoppedResolutionRouteProbe{}
			router := m3ConsumerRouter{knowledge: other, memory: other, meeting: other, agents: other}
			var target *mc.ConsumerAuthority
			switch kind {
			case mc.KnowledgeConsumer:
				target = &router.knowledge
			case mc.MemoryConsumer:
				target = &router.memory
			case mc.MeetingConsumer:
				target = &router.meeting
			}
			*target = selected
			if _, err = router.DiscoverStoppedResolution(ctx, request); !errors.Is(err, denied) {
				t.Fatal("discovery bypassed original owner", err)
			}
			if err = router.ValidateStoppedResolutionInTx(ctx, f.Tx{}, request, mc.ConsumerDependencies{}); !errors.Is(err, denied) {
				t.Fatal("transaction bypassed original owner", err)
			}
			for _, missing := range []mc.ConsumerAuthority{nil, &stoppedRouteProbe{}, (*stoppedResolutionRouteProbe)(nil)} {
				*target = missing
				_, err = router.DiscoverStoppedResolution(ctx, request)
				lifecycleAssemblyFault(t, err, f.DependencyUnbound)
			}
			request.PreparedProcessID = oc.ProcessID{}
			_, err = router.DiscoverStoppedResolution(ctx, request)
			lifecycleAssemblyFault(t, err, f.InvalidArgument)
			if selected.calls != 2 || other.calls != 0 {
				t.Fatal("domain rejection or missing port fell through")
			}
		})
	}
}
