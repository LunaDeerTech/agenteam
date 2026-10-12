package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// ProjectStopAuthority lends only the original Project stop invocation to the
// fixed Work call owner. The callback starts after the authorization Store call
// actually returns Committed. Each subsequent Work transaction must consume the
// same active loan again; public actor/cause values are not permission.
type ProjectStopAuthority interface {
	WithProjectStop(context.Context, i.Actor, pc.LifecycleCause, pc.ScopeRef, func(context.Context) error) error
	RequireProjectStopInTx(context.Context, f.Tx, i.Actor, pc.LifecycleCause, pc.ScopeRef) error
}
