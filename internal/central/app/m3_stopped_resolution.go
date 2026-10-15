package app

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// Selecting the original domain is not a resolution grant. Only its private
// stopped owner can prove the persisted request, input and prepared process.
func (r m3ConsumerRouter) stoppedResolutionOwner(request mc.StoppedResolutionRequest) (mc.StoppedResolutionAuthority, error) {
	if request.Validate() != nil {
		return nil, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	var consumer mc.ConsumerAuthority
	switch request.Resolve.Consumer.Kind {
	case mc.KnowledgeConsumer:
		consumer = r.knowledge
	case mc.MemoryConsumer:
		consumer = r.memory
	case mc.MeetingConsumer:
		consumer = r.meeting
	default:
		return nil, f.NewFault(f.Forbidden, f.NotStarted)
	}
	owner, ok := consumer.(mc.StoppedResolutionAuthority)
	if !ok || runtimeInformationNil(owner) {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return owner, nil
}

func (r m3ConsumerRouter) DiscoverStoppedResolution(ctx context.Context, request mc.StoppedResolutionRequest) (mc.ConsumerDependencies, error) {
	owner, err := r.stoppedResolutionOwner(request)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	return owner.DiscoverStoppedResolution(ctx, request)
}

func (r m3ConsumerRouter) ValidateStoppedResolutionInTx(ctx context.Context, tx f.Tx, request mc.StoppedResolutionRequest, plan mc.ConsumerDependencies) error {
	owner, err := r.stoppedResolutionOwner(request)
	if err != nil {
		return err
	}
	return owner.ValidateStoppedResolutionInTx(ctx, tx, request, plan)
}

var _ mc.StoppedResolutionAuthority = m3ConsumerRouter{}
