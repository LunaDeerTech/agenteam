package app

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

type profileRouteProbe struct {
	mc.ConsumerAuthority
	ctx     context.Context
	binding f.Digest
	calls   int
	err     error
}

func (p *profileRouteProbe) check(ctx context.Context, r mc.EmbeddingProfileRequest) error {
	binding, err := mc.EmbeddingProfileBinding(r)
	if err != nil || binding != p.binding || ctx != p.ctx {
		return errors.New("embedding profile changed original caller")
	}
	p.calls++
	return p.err
}
func (p *profileRouteProbe) DiscoverEmbeddingProfile(ctx context.Context, r mc.EmbeddingProfileRequest) (mc.ConsumerDependencies, error) {
	return mc.ConsumerDependencies{}, p.check(ctx, r)
}
func (p *profileRouteProbe) RequireEmbeddingProfileInTx(ctx context.Context, _ f.Tx, r mc.EmbeddingProfileRequest, _ mc.ConsumerDependencies) error {
	return p.check(ctx, r)
}

func TestM3EmbeddingProfileRoutesOriginalOwner(t *testing.T) {
	for _, kind := range []mc.ConsumerKind{mc.MemoryConsumer, mc.KnowledgeConsumer} {
		t.Run(string(kind), func(t *testing.T) {
			project, _ := f.NewID[id.Project]()
			operation, _ := f.NewID[struct{}]()
			scope, _ := id.InProject(project)
			name, purpose := id.KnowledgeRuntime, mc.KnowledgeEmbedding
			consumer := mc.Consumer{Kind: kind, ProjectID: project, OperationID: operation.String()}
			if kind == mc.MemoryConsumer {
				name, purpose = id.MemoryRuntime, mc.MemoryEmbedding
				agent, _ := f.NewID[id.Agent]()
				consumer.AgentID = &agent
			}
			consumer.Purpose = purpose
			registration, err := id.RegisterService(name)
			if err != nil {
				t.Fatal(err)
			}
			actor, err := registration.Actor(operation.String(), scope)
			if err != nil {
				t.Fatal(err)
			}
			request := mc.EmbeddingProfileRequest{Actor: actor, Consumer: consumer}
			binding, err := mc.EmbeddingProfileBinding(request)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			denied := errors.New("original domain rejected")
			selected := &profileRouteProbe{ctx: ctx, binding: binding, err: denied}
			other := &profileRouteProbe{}
			router := m3ConsumerRouter{knowledge: selected, memory: other, agents: other, meeting: other}
			if kind == mc.MemoryConsumer {
				router.knowledge, router.memory = other, selected
			}
			if _, err = router.DiscoverEmbeddingProfile(ctx, request); !errors.Is(err, denied) {
				t.Fatal("discovery bypassed domain", err)
			}
			if err = router.RequireEmbeddingProfileInTx(ctx, f.Tx{}, request, mc.ConsumerDependencies{}); !errors.Is(err, denied) {
				t.Fatal("transaction bypassed domain", err)
			}
			if selected.calls != 2 || other.calls != 0 {
				t.Fatal("domain rejection fell through")
			}
			if _, err = (m3ConsumerRouter{}).DiscoverEmbeddingProfile(ctx, request); err == nil {
				t.Fatal("unbound domain admitted profile read")
			}
			request.Consumer.Purpose = mc.MemoryReflection
			if _, err = router.DiscoverEmbeddingProfile(ctx, request); err == nil || selected.calls != 2 {
				t.Fatal("non-embedding consumer was routed")
			}
		})
	}
}
