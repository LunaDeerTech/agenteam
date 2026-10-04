package contract

import (
	"context"

	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// LifecycleActorResolver is an optional capability of the explicitly bound
// ProjectAuthority. It resolves an exact durable operation to its current
// registered actor. Resolution itself grants no mutation permission: Discover,
// the complete lock union and ValidateInTx still run for every recovery step.
// Implementations must never derive an actor merely from the supplied IDs.
type LifecycleActorResolver interface {
	ResolveLifecycleActor(context.Context, LifecycleCause) (identity.Actor, error)
}
