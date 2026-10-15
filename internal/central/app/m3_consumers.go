package app

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// The App copies a fixed selection before Model is constructed. Routing never
// manufactures a Consumer plan or substitutes Project membership for the
// original domain authority's live operation and private context.
type m3ConsumerRouter struct{ agents, knowledge, memory, meeting mc.ConsumerAuthority }

func (r m3ConsumerRouter) owner(request mc.ConsumerRequest) (mc.ConsumerAuthority, error) {
	if request.Validate() != nil {
		return nil, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	var authority mc.ConsumerAuthority
	switch request.Consumer.Kind {
	case mc.AgentConsumer:
		authority = r.agents
	case mc.KnowledgeConsumer:
		authority = r.knowledge
	case mc.MemoryConsumer:
		authority = r.memory
	case mc.MeetingConsumer:
		authority = r.meeting
	default:
		return nil, f.NewFault(f.Forbidden, f.NotStarted)
	}
	if runtimeInformationNil(authority) {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return authority, nil
}
func (r m3ConsumerRouter) Discover(ctx context.Context, request mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
	authority, err := r.owner(request)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	return authority.Discover(ctx, request)
}
func (r m3ConsumerRouter) ValidateInTx(ctx context.Context, tx f.Tx, request mc.ConsumerRequest, plan mc.ConsumerDependencies) error {
	authority, err := r.owner(request)
	if err != nil {
		return err
	}
	return authority.ValidateInTx(ctx, tx, request, plan)
}

var _ mc.ConsumerAuthority = m3ConsumerRouter{}

func (r m3ConsumerRouter) servingOwner(request mc.ServingSnapshotRetentionRequest) (mc.ServingSnapshotAuthority, error) {
	if request.Validate() != nil {
		return nil, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	var consumer mc.ConsumerAuthority
	switch request.Owner.Kind {
	case mc.KnowledgeConsumer:
		consumer = r.knowledge
	case mc.MemoryConsumer:
		consumer = r.memory
	case mc.MeetingConsumer:
		consumer = r.meeting
	default:
		return nil, f.NewFault(f.Forbidden, f.NotStarted)
	}
	owner, ok := consumer.(mc.ServingSnapshotAuthority)
	if !ok || runtimeInformationNil(owner) {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return owner, nil
}

func (r m3ConsumerRouter) DiscoverServingSnapshot(ctx context.Context, request mc.ServingSnapshotRetentionRequest) (mc.ConsumerDependencies, error) {
	owner, err := r.servingOwner(request)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	return owner.DiscoverServingSnapshot(ctx, request)
}

func (r m3ConsumerRouter) ValidateServingSnapshotInTx(ctx context.Context, tx f.Tx, request mc.ServingSnapshotRetentionRequest, plan mc.ConsumerDependencies) error {
	owner, err := r.servingOwner(request)
	if err != nil {
		return err
	}
	return owner.ValidateServingSnapshotInTx(ctx, tx, request, plan)
}

func (r m3ConsumerRouter) ValidateServingSnapshotObservationInTx(ctx context.Context, tx f.Tx, request mc.ServingSnapshotRetentionRequest, plan mc.ConsumerDependencies) error {
	owner, err := r.servingOwner(request)
	if err != nil {
		return err
	}
	return owner.ValidateServingSnapshotObservationInTx(ctx, tx, request, plan)
}

var _ mc.ServingSnapshotAuthority = m3ConsumerRouter{}

func (r m3ConsumerRouter) embeddingProfileOwner(request mc.EmbeddingProfileRequest) (mc.EmbeddingProfileAuthority, error) {
	if request.Validate() != nil {
		return nil, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	var consumer mc.ConsumerAuthority
	switch request.Consumer.Kind {
	case mc.KnowledgeConsumer:
		consumer = r.knowledge
	case mc.MemoryConsumer:
		consumer = r.memory
	default:
		return nil, f.NewFault(f.Forbidden, f.NotStarted)
	}
	owner, ok := consumer.(mc.EmbeddingProfileAuthority)
	if !ok || runtimeInformationNil(owner) {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return owner, nil
}

func (r m3ConsumerRouter) DiscoverEmbeddingProfile(ctx context.Context, request mc.EmbeddingProfileRequest) (mc.ConsumerDependencies, error) {
	owner, err := r.embeddingProfileOwner(request)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	return owner.DiscoverEmbeddingProfile(ctx, request)
}

func (r m3ConsumerRouter) RequireEmbeddingProfileInTx(ctx context.Context, tx f.Tx, request mc.EmbeddingProfileRequest, plan mc.ConsumerDependencies) error {
	owner, err := r.embeddingProfileOwner(request)
	if err != nil {
		return err
	}
	return owner.RequireEmbeddingProfileInTx(ctx, tx, request, plan)
}

var _ mc.EmbeddingProfileAuthority = m3ConsumerRouter{}

func (r m3ConsumerRouter) stoppedOwner(request mc.StoppedCallObservationRequest) (mc.StoppedCallAuthority, error) {
	if request.Validate() != nil {
		return nil, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	var consumer mc.ConsumerAuthority
	switch request.Consumer.Kind {
	case mc.AgentConsumer:
		consumer = r.agents
	case mc.KnowledgeConsumer:
		consumer = r.knowledge
	case mc.MemoryConsumer:
		consumer = r.memory
	case mc.MeetingConsumer:
		consumer = r.meeting
	default:
		return nil, f.NewFault(f.Forbidden, f.NotStarted)
	}
	owner, ok := consumer.(mc.StoppedCallAuthority)
	if !ok || runtimeInformationNil(owner) {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return owner, nil
}
func (r m3ConsumerRouter) DiscoverStoppedCall(ctx context.Context, request mc.StoppedCallObservationRequest) (mc.ConsumerDependencies, error) {
	owner, err := r.stoppedOwner(request)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	return owner.DiscoverStoppedCall(ctx, request)
}
func (r m3ConsumerRouter) ValidateStoppedCallInTx(ctx context.Context, tx f.Tx, request mc.StoppedCallObservationRequest, plan mc.ConsumerDependencies) error {
	owner, err := r.stoppedOwner(request)
	if err != nil {
		return err
	}
	return owner.ValidateStoppedCallInTx(ctx, tx, request, plan)
}

var _ mc.StoppedCallAuthority = m3ConsumerRouter{}
