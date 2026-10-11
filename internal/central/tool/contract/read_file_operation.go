package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// ReadFileCallPlan belongs to the actual Execution producer. Its private
// identity binds the committed call, immutable input and complete lock union.
// A plan is discovery material, never permission to dispatch a Runner request.
type ReadFileCallPlan interface {
	RequiredLocks() []f.LockRequest
	// Binding returns a safe value copy for stable command/Operation discovery.
	// The producer rechecks every field; this value carries no protected bytes.
	Binding() ToolCallBinding
}

// ReadFileExecutionAuthority lends the original protected arguments only inside
// the caller's same-Store transaction. The producer rechecks current Execution,
// cancellation, Agent permissions, Tool binding and original payload identity.
// It invokes consume synchronously exactly once with its live current-Agent
// witness; that witness expires before this method returns. No transaction,
// lock acquisition, Model call or Runner I/O may be opened by consume.
//
// Tool may retain a validated typed read intent in its private operation owner,
// but must not retain the borrowed byte slice. Metadata stores only the original
// payload reference/digests, not another copy of the protected arguments.
// A public Actor, binding or digest does not implement this authority. There is
// deliberately no fallback producer or default application registration.
type ReadFileExecutionAuthority interface {
	DiscoverReadFileCall(context.Context, i.ExecutionID) (ReadFileCallPlan, error)
	WithReadFileCallInTx(context.Context, f.Tx, ReadFileCallPlan, func(context.Context, ToolCallBinding, []byte) error) error
}
