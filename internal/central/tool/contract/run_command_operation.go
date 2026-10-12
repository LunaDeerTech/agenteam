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
// With proves the original committed call/input and exact run-command profile,
// same Store/Tx/full held union and current Execution/Agent/Tool/Mount facts.
// It does not apply the separate command admission gate to historical Lookup.
// consume runs synchronously once inside the original private Agent witness;
// borrowed args and witness expire on return. No caller DTO, nested transaction,
// late lock, environment value resolution, default binding or I/O is implied.
type RunCommandExecutionAuthority interface {
	DiscoverRunCommandCall(context.Context, i.ExecutionID) (RunCommandCallPlan, error)
	WithRunCommandCallInTx(context.Context, f.Tx, RunCommandCallPlan, func(context.Context, ToolCallBinding, []byte) error) error
	// Admission is valid only inside the matching original With callback and
	// same live Tx/full held union. It requires current approval_policy=allow
	// and the real ProjectVariable owner's current empty injection set. Resolve
	// and final Claim invoke it; Claim rechecks before its actual commit return.
	// Historical Lookup does not invoke it: later policy/environment changes
	// cannot erase the original durable report. It returns no reusable grant.
	RequireRunCommandAdmissionInTx(context.Context, f.Tx, RunCommandCallPlan) error
}
