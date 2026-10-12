package contract

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// CommittedWriteFileCallPlan belongs to the actual Execution historical reader.
// Its safe binding is immutable discovery data, never permission to dispatch.
type CommittedWriteFileCallPlan interface {
	Binding() ToolCallBinding
	RequiredLocks() []f.LockRequest
}

// CommittedWriteFileCallAuthority proves the original persisted write-file call,
// captured Snapshot/Round/Tool metadata, Preparation identity and complete
// protected arguments. It is deliberately not WriteFileExecutionAuthority:
// later current Tool/Mount/configuration changes cannot erase a durable receipt.
// It lends a cloned argument slice synchronously once in the caller's same-Store
// Tx after the complete lock union is held; the slice is cleared on return.
// It neither lends a current-Agent witness nor authorizes any new invocation.
// consume may not retain the loan, acquire locks, begin a Tx or perform I/O.
// A safe caller binding, args DTO or stopped dispatch owner is no substitute.
type CommittedWriteFileCallAuthority interface {
	DiscoverCommittedWriteFileCall(context.Context, i.ExecutionID) (CommittedWriteFileCallPlan, error)
	WithCommittedWriteFileCallInTx(context.Context, f.Tx, CommittedWriteFileCallPlan, func(context.Context, ToolCallBinding, []byte) error) error
}
