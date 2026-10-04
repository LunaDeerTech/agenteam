package contract

import (
	"context"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type ordinaryProjectAuthority struct{}

func (ordinaryProjectAuthority) Discover(context.Context, ProjectRequest) (Dependencies, error) {
	return Dependencies{}, invalid()
}
func (ordinaryProjectAuthority) ValidateInTx(context.Context, foundation.Tx, ProjectRequest, Dependencies) error {
	return invalid()
}

type resolvingProjectAuthority struct{ ordinaryProjectAuthority }

func (resolvingProjectAuthority) ResolveLifecycleActor(context.Context, LifecycleCause) (identity.Actor, error) {
	return identity.Actor{}, invalid()
}

func TestLifecycleRecoveryCapabilityIsExplicitAndOptional(t *testing.T) {
	var ordinary ProjectAuthority = ordinaryProjectAuthority{}
	if _, ok := ordinary.(LifecycleActorResolver); ok {
		t.Fatal("ordinary authority acquired implicit recovery identity")
	}
	var resolving ProjectAuthority = resolvingProjectAuthority{}
	if _, ok := resolving.(LifecycleActorResolver); !ok {
		t.Fatal("independent resolver capability cannot compose")
	}
}
