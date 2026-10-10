package skill

import (
	"context"
	"reflect"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// RequireInstallBindingInTx checks immutable composition, not an Actor grant.
// The concrete Service must consume the exact Runtime producer and process
// claimed by its registry source. Both domains validate this same caller Tx;
// this method neither begins a transaction nor calls the producer or Install.
func (s *Service) RequireInstallBindingInTx(ctx context.Context, tx f.Tx, expected sc.InstallExecutionAuthority, expectedProcess oc.ProcessID) error {
	if ctx == nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return portError(err)
	}
	d := s.state()
	if d == nil || d.authority.state() == nil || nilPort(expected) || expectedProcess.Validate() != nil {
		return fault(f.DependencyUnbound)
	}
	a := d.authority.state()
	if nilPort(a.installExecution) || nilPort(a.store) {
		return fault(f.DependencyUnbound)
	}
	actual, want := reflect.ValueOf(a.installExecution), reflect.ValueOf(expected)
	if actual.Type() != want.Type() || !actual.Comparable() || !want.Comparable() || actual.Interface() != want.Interface() || d.process != expectedProcess {
		return fault(f.InvalidState)
	}
	if !tx.Valid() {
		return invalid()
	}
	if _, err := a.store.InTx(tx); err != nil {
		return portError(err)
	}
	d.mu.Lock()
	stopped := d.stopped
	d.mu.Unlock()
	if stopped {
		return fault(f.ShuttingDown)
	}
	return portError(ctx.Err())
}
