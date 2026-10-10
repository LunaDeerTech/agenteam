package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const MaxDispatchCapacityBatch = 128

// AssociatedDispatch names an already persisted Scheduler association. It is
// not an authorization or evidence that Execution accepted a caller DTO. The
// reader verifies every field against the original canonical Execution row.
type AssociatedDispatch struct {
	ExecutionID i.ExecutionID
	AgentID     i.AgentID
	Key         f.IdempotencyKey
	Digest      f.Digest
	DispatchID  string
}

// DispatchCapacityReader reads trusted Execution facts in the caller's live
// same-Store transaction under Project SH and ProjectSchedule EX. It neither
// invokes authorization callbacks nor adds locks, opens a transaction or writes.
// The Scheduler caller owns and authorizes its complete durable association
// set; it must process every batch in the SAME transaction without truncation,
// omitting history or releasing Schedule EX between batches.
//
// Each nonnil batch has at most MaxDispatchCapacityBatch distinct Execution and
// Dispatch identities. Even terminal/waiting rows must match the complete tuple
// and Task lineage. Only created/preparing/running count; waiting still occupies
// an Agent slot. Missing rows, mismatches, SQL errors and cancellation return a
// zero count and an error, never a partial count or permission to resend Launch.
// Task Execution status writers must use the same Schedule EX gate.
type DispatchCapacityReader interface {
	CountAssociatedInTx(context.Context, f.Tx, i.ProjectID, []AssociatedDispatch) (int64, error)
}
