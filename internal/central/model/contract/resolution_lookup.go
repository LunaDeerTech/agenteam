package contract

import "context"

// ResolutionLookup observes the original immutable resolution after Unknown.
// False means no committed resolution was observed under the original command
// lock. It never acquires a replacement lease or invokes a provider.
type ResolutionLookup interface {
	LookupResolution(context.Context, ResolveRequest) (ResolvedModel, bool, error)
}
