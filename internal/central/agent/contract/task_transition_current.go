package contract

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// TaskTransitionAuthority is supplied by the actual Tool operation owner.
type TaskTransitionAuthority interface {
	RequireTaskToolInTx(context.Context, f.Tx, i.Actor, i.ProjectID, string, string) error
}

type TaskTransitionReferences interface {
	RequireTaskTransitionCurrentInTx(context.Context, f.Tx, i.Actor, i.ProjectID, string, i.AgentID) (AgentRef, error)
}
