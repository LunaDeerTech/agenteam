package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// PreparationColdInput is borrowed only inside the original committed-input
// callback. It cannot authorize Execute, signing, or a replacement Attempt.
type PreparationColdInput struct {
	Arguments        []byte
	Bindings         []SkillBinding
	OperationVersion f.Version
	OldProcess       oc.ProcessID
}

// PreparationColdAuthority is a separate read-only capability issued by the
// original Builtin Lookup after ConfirmStopped for its exact prior process.
// Each callback rechecks that registered private lookup, the complete original
// call/attempt/request/input, held locks and the current ProcessGuard.
type PreparationColdAuthority interface {
	DiscoverSkillPreparationCold(context.Context, PreparationInvocation) (InvocationPlan, error)
	WithSkillPreparationColdInTx(context.Context, f.Tx, PreparationInvocation, InvocationPlan, func(context.Context, PreparationColdInput) error) error
}
