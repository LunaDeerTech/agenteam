package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// ProjectStopAuthority lends the original Project Delete stop invocation.
// Its authorization transaction must actually return before the callback;
// every subsequent transaction consumes the same live private loan again.
// Public cause values and stopped rows are not permission.
type ProjectStopAuthority interface {
	WithProjectStop(context.Context, i.Actor, pc.LifecycleCause, pc.ScopeRef, func(context.Context) error) error
	RequireProjectStopInTx(context.Context, f.Tx, i.Actor, pc.LifecycleCause, pc.ScopeRef) error
}
