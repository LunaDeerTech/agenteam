package contract

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// WriteFileCallPlan is private to its actual Execution producer and binds the
// original committed write-file call, protected payload and full lock union.
// Binding is a safe copy, never an authority grant or borrowed argument bytes.
type WriteFileCallPlan interface {
	RequiredLocks() []f.LockRequest
	Binding() ToolCallBinding
}

// WriteFileExecutionAuthority is distinct from read-file. It rejects foreign
// plans and non-write bindings, checks the same Store/Tx and all held locks, and
// revalidates current Execution/cancellation/Agent/Tool rights against the
// original call/input. No caller payload or target DTO can replace that proof.
// The callback runs synchronously once within the actual Agent witness lifetime;
// neither it nor the caller may retain that witness or the borrowed bytes. A
// validated private typed intent may be retained by the Tool owner. No nested
// transaction, late lock, Model/Runner I/O or default composition is implied.
type WriteFileExecutionAuthority interface {
	DiscoverWriteFileCall(context.Context, i.ExecutionID) (WriteFileCallPlan, error)
	WithWriteFileCallInTx(context.Context, f.Tx, WriteFileCallPlan, func(context.Context, ToolCallBinding, []byte) error) error
}
