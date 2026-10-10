package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"reflect"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// ProjectAuditAuthority can be constructed before Project's immutable producer
// map. It has no setter, cached grant or public witness input. The original
// Agent writer identifies its Authority only through this package's private
// same-Tx witness; this wrapper additionally pins that writer to the one Store.
type ProjectAuditAuthority struct{ store Store }

func NewProjectAuditAuthority(store Store) (*ProjectAuditAuthority, error) {
	if nilPort(store) || !reflect.TypeOf(store).Comparable() {
		return nil, fault(f.DependencyUnbound)
	}
	return &ProjectAuditAuthority{store: store}, nil
}

func (a *ProjectAuditAuthority) CheckProjectAuditInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) error {
	if a == nil || nilPort(a.store) {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	w, ok := ctx.Value(mutationWitnessKey{}).(mutationWitness)
	if !ok || w.authority == nil || w.authority.store != a.store || w.tx != tx {
		return fault(f.Forbidden)
	}
	if _, err := a.store.InTx(tx); err != nil {
		return portError(err)
	}
	// Project has already performed current Owner Read/Mutate. Agent repeats
	// its original current gate and complete command/preimage/postimage proof;
	// same Store or a nonnil private value alone never becomes permission.
	return (&Authority{state: w.authority}).CheckProjectAuditInTx(ctx, tx, entry, key)
}

func (ProjectAuditAuthority) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "agent_project_audit_authority")
}
func (ProjectAuditAuthority) LogValue() slog.Value {
	return slog.StringValue("agent_project_audit_authority")
}

var _ ac.ProjectFactAuthority = (*ProjectAuditAuthority)(nil)
