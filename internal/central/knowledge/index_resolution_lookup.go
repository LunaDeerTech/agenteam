package knowledge

import (
	"context"

	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

type indexResolutionLookupKey struct{}

// Only retireOperation lends this proof, while holding the original operation
// mutex, to Model.LookupResolution. That port only observes the original
// command under its lock; it neither creates a resolution nor acquires a lease.
// The proof is never supplied to ResolveModel or an embedding invocation.
func indexResolutionLookup(ctx context.Context, op *indexOperation, request mc.ConsumerRequest) bool {
	original, ok := ctx.Value(indexResolutionLookupKey{}).(*indexOperation)
	return ok && original == op && op.resolveUnknown && op.terminal == "failed" && request.Action == mc.ResolveConsumer && request.Resolve != nil
}
