package object

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// RequireCurrentProcessInTx checks this guard's current identity and asks its
// original Store to recognize the caller's live transaction. It issues no SQL
// or grant, starts no transaction, and neither pins the guard nor proves death.
// The composition root must retain the guard until every borrower has joined.
func (g *ProcessGuard) RequireCurrentProcessInTx(ctx context.Context, tx f.Tx, expected oc.ProcessID) error {
	if ctx == nil || !tx.Valid() || expected.Validate() != nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return unavailable(err)
	}
	if g == nil || g.data == nil {
		return failure(f.DependencyUnbound, nil)
	}
	r := g.state()
	if r == nil {
		return failure(f.DependencyUnbound, nil)
	}
	r.mu.Lock()
	if !r.bound || r.closed || r.finishDone != nil || r.process != expected || r.service == nil || r.service.data == nil {
		r.mu.Unlock()
		return unavailable(nil)
	}
	service := r.service
	state := service.state()
	if state == nil || nilPort(state.store) {
		r.mu.Unlock()
		return failure(f.DependencyUnbound, nil)
	}
	store := state.store
	r.mu.Unlock()

	// Store is an external port. Never hold the guard mutex across its actual
	// return; cancellation must not detach that call or skip the final check.
	x, err := store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if nilPort(x) {
		return unavailable(nil)
	}
	r.mu.Lock()
	valid := r.bound && !r.closed && r.finishDone == nil && r.process == expected && r.service == service && service.state() == state
	r.mu.Unlock()
	if !valid {
		return unavailable(nil)
	}
	return unavailableIf(ctx.Err())
}
