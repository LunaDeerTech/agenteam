package memory

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// AuditAuthority is safe to register before Audit and Memory Services exist.
// It delegates only a private live mutation witness from the same Store.
type AuditAuthority struct{ store Store }

func NewAuditAuthority(store Store) (*AuditAuthority, error) {
	if nilPort(store) {
		return nil, fault(f.DependencyUnbound)
	}
	return &AuditAuthority{store: store}, nil
}
func (a *AuditAuthority) CheckProjectAuditInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	w, _ := ctx.Value(auditKey{}).(*auditWitness)
	if a == nil || w == nil || !w.active || w.service == nil || !sameStore(a.store, w.service.store) {
		return fault(f.Forbidden)
	}
	return w.service.CheckProjectAuditInTx(ctx, tx, entry, key)
}

var _ ac.ProjectFactAuthority = (*AuditAuthority)(nil)
