package contract

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// RunCommandCallPlan is issued by the actual Execution owner. Its safe Binding
// copy is not authority and never exposes protected argv or a current witness.
type RunCommandCallPlan interface {
	RequiredLocks() []f.LockRequest
	Binding() ToolCallBinding
}

// RunCommandExecutionAuthority is deliberately separate from read/write-file.
// It proves the original committed call/input and exact run-command profile,
// same Store/Tx/full held union, current running Execution/Agent/Tool/Mount,
// current approval_policy=allow and the real ProjectVariable owner's current
// empty injection set. The final Claim repeats these checks in its own Tx;
// historical completed reads do not invoke this current mutation gate.
// consume runs synchronously once inside the original private Agent witness;
// borrowed args and witness expire on return. No caller DTO, nested transaction,
// late lock, environment value resolution, default binding or I/O is implied.
type RunCommandExecutionAuthority interface {
	DiscoverRunCommandCall(context.Context, i.ExecutionID) (RunCommandCallPlan, error)
	WithRunCommandCallInTx(context.Context, f.Tx, RunCommandCallPlan, func(context.Context, ToolCallBinding, []byte) error) error
}
