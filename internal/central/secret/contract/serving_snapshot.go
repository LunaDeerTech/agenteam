package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// ModelServingSnapshotReferenceObservation observes one exact Model reference
// under its original retain/release plan and current provider authority. It
// does not expose credential material or change the reference ledger.
type ModelServingSnapshotReferenceObservation interface {
	ModelServingSnapshotReferenceInTx(context.Context, f.Tx, UsageRequest, UsageDependencies) (bool, error)
}
