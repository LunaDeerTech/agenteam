package knowledge

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

// OwnerDocumentReferences lends only active document identity validation to
// another domain's original transaction. That caller owns the SQL lifetime and
// full lock union. This port opens no Knowledge read session or content reader.
type OwnerDocumentReferences struct{ original *Service }

func NewOwnerDocumentReferences(original *Service) (*OwnerDocumentReferences, error) {
	if original.state() == nil || original.state().ownership == nil {
		return nil, fault(f.DependencyUnbound)
	}
	return &OwnerDocumentReferences{original: original}, nil
}

func (r *OwnerDocumentReferences) RequireStore(value any) error {
	store, ok := value.(Store)
	if r == nil || r.original.state() == nil || !ok || !sameStore(store, r.original.state().store) {
		return fault(f.DependencyUnbound)
	}
	return nil
}

func (r *OwnerDocumentReferences) DocumentReferenceLocks(actor id.Actor, project id.ProjectID, document kc.DocumentID) ([]f.LockRequest, error) {
	if r == nil || r.original.state() == nil {
		return nil, fault(f.DependencyUnbound)
	}
	if actor.Validate() != nil || actor.Details().Kind != id.Human {
		return nil, fault(f.Forbidden)
	}
	if project.Validate() != nil || document.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	// Canonical document mutations take this same tree lock exclusively.
	return scopeLocks(actor, project, false)
}

func (r *OwnerDocumentReferences) RequireDocumentReferenceInTx(ctx context.Context, tx f.Tx, actor id.Actor, project id.ProjectID, document kc.DocumentID) error {
	locks, err := r.DocumentReferenceLocks(actor, project, document)
	if err != nil {
		return err
	}
	if err = readInput(ctx, actor, project); err != nil {
		return err
	}
	st := r.original.state()
	if err = st.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	grant, err := st.deps.Projects.RequireOwnerInTx(ctx, tx, actor, project, id.Read)
	if err != nil {
		return portError(err)
	}
	if !grant.Matches(actor, project) {
		return fault(f.Forbidden)
	}
	var exists bool
	if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.documents WHERE project_id=$1 AND id=$2 AND status='active')`, project.String(), document.String()).Scan(&exists); err != nil {
		return portError(err)
	}
	if !exists {
		return fault(f.NotFound)
	}
	return ctx.Err()
}
