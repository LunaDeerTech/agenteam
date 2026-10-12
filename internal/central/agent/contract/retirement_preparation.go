package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// RetirementPreparation reads a complete initialized configuration under the
// original Project Delete stop-round issuer. It owns no transaction, writes no
// lifecycle state or references, and supplies no grant for later retirement.
// Any failure returns a zero configuration. The expected version is rechecked
// in the original live transaction; a captured configuration cannot be reused
// as authority after the callback or in another transaction.
type RetirementPreparation interface {
	ReadRetirementPreparationInTx(context.Context, f.Tx, pc.AgentRetirementPreparationRequest) (AgentConfig, error)
}
