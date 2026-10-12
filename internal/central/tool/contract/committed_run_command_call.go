package contract

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// CommittedRunCommandCallPlan belongs to the actual Execution historical reader.
// Its safe binding is immutable discovery data, never permission to dispatch.
type CommittedRunCommandCallPlan interface {
	Binding() ToolCallBinding
	RequiredLocks() []f.LockRequest
}

// CommittedRunCommandCallAuthority proves the original persisted run-command call,
// captured Snapshot/Round/Tool metadata, Preparation identity and complete
// protected arguments. It is deliberately not RunCommandExecutionAuthority:
// later current Tool/Mount/allow/environment changes cannot erase a durable receipt.
// It lends a cloned argument slice synchronously once in the caller's same-Store
// Tx after the complete lock union is held; the slice is cleared on return.
// It neither lends a current-Agent witness nor authorizes any new invocation.
// consume may read or persist through the same Tx. It may not retain the loan,
// acquire locks, begin another Tx, or perform Model/Runner or other external I/O.
// A safe caller binding, args DTO or stopped dispatch owner is no substitute.
type CommittedRunCommandCallAuthority interface {
	DiscoverCommittedRunCommandCall(context.Context, i.ExecutionID) (CommittedRunCommandCallPlan, error)
	WithCommittedRunCommandCallInTx(context.Context, f.Tx, CommittedRunCommandCallPlan, func(context.Context, ToolCallBinding, []byte) error) error
}
