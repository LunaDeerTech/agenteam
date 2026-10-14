package artifact

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// RequireBuiltinBindingInTx checks the actual Service, original Store and live
// process ownership at Registry exposure. It grants no Agent or tool authority.
func (s *Service) RequireBuiltinBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if s == nil || s.data == nil || ctx == nil || !tx.Valid() {
		return failure(f.DependencyUnbound, nil)
	}
	actual, ok := store.(Store)
	r := s.state()
	if !ok || !sameProjectAuditStore(r.store, actual) || r.owners == nil || r.owners.bound() != nil || !sameProjectAuditStore(r.owners.state().store, actual) || r.ownership == nil {
		return failure(f.DependencyUnbound, nil)
	}
	binding, ok := r.owners.state().authority.(interface {
		RequireAgentRunBindingInTx(context.Context, f.Tx, any) error
	})
	if !ok {
		return failure(f.DependencyUnbound, nil)
	}
	if err := binding.RequireAgentRunBindingInTx(ctx, tx, store); err != nil {
		return portError(err)
	}
	if _, err := r.store.InTx(tx); err != nil {
		return portError(err)
	}
	if err := r.ownership.requireProcess(ctx, tx); err != nil {
		return portError(err)
	}
	r.ownership.mu.Lock()
	stopped := r.ownership.stopped
	r.ownership.mu.Unlock()
	if stopped {
		return failure(f.InvalidState, nil)
	}
	return portOrNil(ctx.Err())
}
