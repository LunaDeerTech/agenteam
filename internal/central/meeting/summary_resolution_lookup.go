package meeting

import (
	"context"

	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

type summaryResolutionLookupKey struct{}

// The original call mutex is held while lending this proof exclusively to
// LookupResolution. A cancellation may retire an existing lease; it must not
// authorize resolving another selection or admitting a new provider request.
func summaryResolutionLookup(ctx context.Context, owner *summaryOwner, request mc.ConsumerRequest) bool {
	call, _ := ctx.Value(summaryResolutionLookupKey{}).(*summaryModelCall)
	if call == nil || owner == nil || call.owner != owner || !owner.active || call.done || !call.resolutionUnknown || call.resolve == nil || request.Action != mc.ResolveConsumer || request.Resolve == nil {
		return false
	}
	want, err := mc.ResolveBinding(*call.resolve)
	got, actual := mc.ResolveBinding(*request.Resolve)
	return err == nil && actual == nil && want == got
}
