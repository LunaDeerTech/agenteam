package contract

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// InitializationConvergenceAuthority checks the Project-owned context for
// observing original initialization metadata and converging existing failed
// work. The caller must already hold Project EX in the same Store's live Tx.
// Only the registered project-initialization actor for the exact original
// Creation, Project and initialization key may pass this check.
//
// A nil error applies only inside that Tx. It is not an Owner grant, a Skills
// fact, a publication permission, a completion receipt or proof of joined work.
// Consumers must separately prove their own exact facts and permissions. New
// writes and successful confirmation retain ValidateInitializationInTx and all
// existing domain gates. Lifecycle cleanup retains its own current cause gate.
// This optional interface deliberately does not extend ProjectAuthority.
type InitializationConvergenceAuthority interface {
	ValidateInitializationConvergenceInTx(context.Context, foundation.Tx, identity.Actor, InitializationRequest) error
}
