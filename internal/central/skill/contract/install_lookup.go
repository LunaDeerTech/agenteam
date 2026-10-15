package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// InstallLookupRequest identifies only the original installation. It never
// authorizes Install, upload, publication, or a new physical Attempt.
type InstallLookupRequest struct{ Original InstallExecutionRequest }

func (r InstallLookupRequest) Validate() error {
	if r.Original.Validate() != nil || r.Original.Intent != id.Read {
		return invalid()
	}
	return nil
}

func (r InstallLookupRequest) Equal(other InstallLookupRequest) bool {
	return r.Validate() == nil && other.Validate() == nil && r.Original.Equal(other.Original)
}

// InstallLookupAuthority supports the original live read handoff, or a
// separately issued, bounded read borrow after that same backend returned an
// uncertain publication. The Runtime owns both private proofs. A returned
// call, valid request DTO, receipt, or cancelled context alone grants nothing.
// The final requirement checks current authority, the original running Tool
// tuple and ProcessGuard in the same Store/Tx under the complete lock union.
type InstallLookupAuthority interface {
	DiscoverInstallLookup(context.Context, InstallLookupRequest) (InstallExecutionPlan, error)
	RequireInstallLookupInTx(context.Context, f.Tx, InstallLookupRequest, InstallExecutionPlan) error
}
