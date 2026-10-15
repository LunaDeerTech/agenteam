package contract

import (
	"context"

	e "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	o "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

// AssignmentProjectEventAuthority is implemented by the fixed Skill facts
// provider. A summary alone grants nothing: both methods require the original
// live assignment invocation; NewFact additionally requires its applied writer.
type AssignmentProjectEventAuthority interface {
	DiscoverAssignmentProjectEvent(context.Context, i.Actor, e.Summary) (o.Dependencies, error)
	ValidateAssignmentProjectEventInTx(context.Context, f.Tx, i.Actor, e.Summary, o.Dependencies, o.Stage) error
}
