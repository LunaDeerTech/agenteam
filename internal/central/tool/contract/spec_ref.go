// Package contract defines Tool references without resolving Registry facts.
package contract

import (
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// SpecRef identifies an immutable Tool definition. Validation establishes only
// scalar validity, not registration, revision existence, scope or authority.
type SpecRef struct {
	ToolID       identity.ToolID
	SpecRevision foundation.Version
}

func (r SpecRef) Validate() error {
	if r.ToolID.Validate() != nil || r.SpecRevision.Validate() != nil {
		return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
	}
	return nil
}
