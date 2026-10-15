package app

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// These probes test only App dispatch: the selected domain must still decide
// authorization, and a rejection must never fall through to another domain.
type servingRouteProbe struct {
	mc.ConsumerAuthority
	ctx     context.Context
	binding f.Digest
	calls   []string
	err     error
}

func (p *servingRouteProbe) check(ctx context.Context, r mc.ServingSnapshotRetentionRequest, method string) error {
	binding, err := mc.ServingSnapshotRetentionBinding(r)
	if err != nil || binding != p.binding || ctx != p.ctx {
		return errors.New("original serving owner changed")
	}
	p.calls = append(p.calls, method)
	return p.err
}
func (p *servingRouteProbe) DiscoverServingSnapshot(ctx context.Context, r mc.ServingSnapshotRetentionRequest) (mc.ConsumerDependencies, error) {
	return mc.ConsumerDependencies{}, p.check(ctx, r, "discover")
}
func (p *servingRouteProbe) ValidateServingSnapshotInTx(ctx context.Context, _ f.Tx, r mc.ServingSnapshotRetentionRequest, _ mc.ConsumerDependencies) error {
	return p.check(ctx, r, "apply")
}
func (p *servingRouteProbe) ValidateServingSnapshotObservationInTx(ctx context.Context, _ f.Tx, r mc.ServingSnapshotRetentionRequest, _ mc.ConsumerDependencies) error {
	return p.check(ctx, r, "observe")
}

func TestM3ServingSnapshotRoutesExactOwnerAndObservation(t *testing.T) {
	for _, kind := range []mc.ConsumerKind{mc.KnowledgeConsumer, mc.MemoryConsumer, mc.MeetingConsumer} {
		t.Run(string(kind), func(t *testing.T) {
			project, err := f.NewID[id.Project]()
			if err != nil {
				t.Fatal(err)
			}
			generation, err := f.NewID[struct{}]()
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := f.NewID[mc.Snapshot]()
			if err != nil {
				t.Fatal(err)
			}
			scope, err := id.InProject(project)
			if err != nil {
				t.Fatal(err)
			}
			registration, err := id.RegisterService(id.ModelRuntime)
			if err != nil {
				t.Fatal(err)
			}
			actor, err := registration.Actor(generation.String(), scope)
			if err != nil {
				t.Fatal(err)
			}
			r := mc.ServingSnapshotRetentionRequest{Actor: actor, Owner: mc.ServingSnapshotOwner{Kind: kind, ProjectID: project, GenerationID: generation.String()}, SnapshotID: snapshot, OwnerVersion: 2, Action: mc.ReleaseServingSnapshot}
			if kind == mc.MemoryConsumer {
				agent, err := f.NewID[id.Agent]()
				if err != nil {
					t.Fatal(err)
				}
				r.Owner.AgentID = &agent
			}
			binding, err := mc.ServingSnapshotRetentionBinding(r)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			denied := errors.New("domain rejected original owner")
			selected := &servingRouteProbe{ctx: ctx, binding: binding, err: denied}
			other := &servingRouteProbe{}
			router := m3ConsumerRouter{knowledge: selected, memory: other, agents: other, meeting: other}
			if kind == mc.MemoryConsumer {
				router.knowledge, router.memory = other, selected
			} else if kind == mc.MeetingConsumer {
				router.knowledge, router.meeting = other, selected
			}
			if _, err := router.DiscoverServingSnapshot(ctx, r); !errors.Is(err, denied) {
				t.Fatalf("discovery changed domain result: %v", err)
			}
			if err := router.ValidateServingSnapshotInTx(ctx, f.Tx{}, r, mc.ConsumerDependencies{}); !errors.Is(err, denied) {
				t.Fatalf("apply changed domain result: %v", err)
			}
			if err := router.ValidateServingSnapshotObservationInTx(ctx, f.Tx{}, r, mc.ConsumerDependencies{}); !errors.Is(err, denied) {
				t.Fatalf("observation changed domain result: %v", err)
			}
			if len(selected.calls) != 3 || selected.calls[2] != "observe" || len(other.calls) != 0 {
				t.Fatal("observation authorized apply or rejection fell through")
			}
			if kind == mc.MemoryConsumer {
				router.memory = nil
			} else if kind == mc.MeetingConsumer {
				router.meeting = nil
			} else {
				router.knowledge = nil
			}
			_, err = router.DiscoverServingSnapshot(ctx, r)
			lifecycleAssemblyFault(t, err, f.DependencyUnbound)
			if len(other.calls) != 0 {
				t.Fatal("missing owner fell through")
			}
		})
	}
}
