package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// This read uses the original ReleaseLeaseUsage plan and its current provider
// authority in the caller's transaction. It exposes no credential material and
// does not infer release from absence or repeat a release write.
type ModelLeaseRetirementObservation interface {
	ModelExecutionLeaseReleasedInTx(context.Context, f.Tx, UsageRequest, UsageDependencies) (bool, error)
}

// ModelCallLeaseRetirementObservation observes the original auxiliary model
// call's lease after its release writer returned. It never performs a release.
type ModelCallLeaseRetirementObservation interface {
	ModelCallLeaseReleasedInTx(context.Context, f.Tx, UsageRequest, UsageDependencies) (bool, error)
}
