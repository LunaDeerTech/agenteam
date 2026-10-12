package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// ProjectStopAuthority lends the original Delete stop invocation to the fixed
// Execution call owner. Its authorization Store call must actually return
// Committed before the callback starts. Every subsequent transaction consumes
// the same active loan with Project EX and ProjectSchedule EX; public values do
// not confer permission. Archive is outside this binding.
type ProjectStopAuthority interface {
	WithProjectStop(context.Context, i.Actor, pc.LifecycleCause, pc.ScopeRef, func(context.Context) error) error
	RequireProjectStopInTx(context.Context, f.Tx, i.Actor, pc.LifecycleCause, pc.ScopeRef) error
}
