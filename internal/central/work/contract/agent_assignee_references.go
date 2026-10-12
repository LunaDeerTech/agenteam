package contract

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// AgentAssigneeReferences checks Work's complete current assignee predicate in
// the caller's live transaction. It includes terminal Tasks and does not filter
// by Sprint or page. Any current assignment returns RESOURCE_BUSY.
//
// The caller must already hold User SH, Project SH, ProjectSchedule EX and Agent
// EX. The implementation verifies those locks and current Human Owner Read in
// the same Store/Tx before Task SQL; it never adds locks or owns a transaction.
// Invalid Actor is UNAUTHENTICATED, AgentRun is DEPENDENCY_UNBOUND and Service is
// FORBIDDEN. Project/Session/Owner failures precede the Work fact query.
//
// Nil means only that this Project has no current Task assigned to this AgentID
// in that transaction. It does not prove Agent existence, Project membership,
// absence of references elsewhere, execution completion or retirement rights.
// A Human caller needing a current same-Project Agent must first use Agent's
// RequireCurrentInTx in the same transaction. Nil is not a reusable grant;
// future Project deletion needs its own authorized lifecycle issuer.
type AgentAssigneeReferences interface {
	RequireAgentUnassignedInTx(context.Context, foundation.Tx, identity.Actor,
		identity.ProjectID, identity.AgentID) error
}
