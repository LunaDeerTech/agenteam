package knowledge

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func (s *Service) read(ctx context.Context, actor id.Actor, project id.ProjectID, fn func(context.Context, postgres.SQLExecutor) error) error {
	if err := readInput(ctx, actor, project); err != nil {
		return err
	}
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	st := s.state()
	cause, err := readCause()
	if err != nil {
		return err
	}
	locks, err := scopeLocks(actor, project, false)
	if err != nil {
		return err
	}
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := s.readScope(ctx, tx, actor, project)
		if err != nil {
			return err
		}
		return fn(ctx, x)
	})
	return txError(result)
}

// readScope rejects foreign/ended transactions and incomplete lock unions
// before authorization or a Knowledge query. It never acquires missing locks.
func (s *Service) readScope(ctx context.Context, tx f.Tx, actor id.Actor, project id.ProjectID) (postgres.SQLExecutor, error) {
	if err := readInput(ctx, actor, project); err != nil {
		return nil, err
	}
	st := s.state()
	if st == nil {
		return nil, fault(f.DependencyUnbound)
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	locks, err := scopeLocks(actor, project, false)
	if err != nil {
		return nil, err
	}
	if err = st.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, portError(err)
	}
	access, err := st.deps.Projects.RequireOwnerInTx(ctx, tx, actor, project, id.Read)
	if err != nil {
		return nil, portError(err)
	}
	if access.Project().ID != project {
		return nil, internal(nil)
	}
	return x, nil
}

func (s *Service) GetDocument(ctx context.Context, actor id.Actor, project id.ProjectID, document kc.DocumentID) (kc.DocumentHead, error) {
	if err := readInput(ctx, actor, project); err != nil {
		return kc.DocumentHead{}, err
	}
	if document.Validate() != nil {
		return kc.DocumentHead{}, fault(f.InvalidArgument)
	}
	var out kc.DocumentHead
	err := s.read(ctx, actor, project, func(ctx context.Context, x postgres.SQLExecutor) error {
		row, err := loadDocument(ctx, x, project, document)
		if err != nil {
			return err
		}
		out = row.head
		return nil
	})
	if err != nil {
		return kc.DocumentHead{}, err
	}
	return out, nil
}

func (s *Service) ReadCurrentInTx(ctx context.Context, tx f.Tx, actor id.Actor, project id.ProjectID, document kc.DocumentID) (kc.CurrentDocumentFact, error) {
	if err := readInput(ctx, actor, project); err != nil {
		return kc.CurrentDocumentFact{}, err
	}
	if document.Validate() != nil {
		return kc.CurrentDocumentFact{}, fault(f.InvalidArgument)
	}
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return kc.CurrentDocumentFact{}, err
	}
	defer done()
	x, err := s.readScope(ctx, tx, actor, project)
	if err != nil {
		return kc.CurrentDocumentFact{}, err
	}
	row, err := loadDocument(ctx, x, project, document)
	if err != nil {
		return kc.CurrentDocumentFact{}, err
	}
	if d := row.head.Active; d != nil {
		object := d.ObjectID
		return kc.CurrentDocumentFact{ProjectID: project, DocumentID: document, ContentVersion: d.ContentVersion, Status: kc.Active, ObjectID: &object}, nil
	}
	return kc.CurrentDocumentFact{ProjectID: project, DocumentID: document, ContentVersion: row.head.Deleted.ContentVersion, Status: kc.Deleted}, nil
}

func readAncestors(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID, document kc.DocumentID) ([]kc.DocumentRef, error) {
	row, err := loadDocument(ctx, x, project, document)
	if err != nil {
		return nil, err
	}
	if row.head.Active == nil {
		return nil, fault(f.NotFound)
	}
	parent := row.head.Active.ParentDocumentID
	seen := map[kc.DocumentID]bool{document: true}
	out := make([]kc.DocumentRef, 0)
	for parent != nil {
		if err := ctx.Err(); err != nil {
			return nil, unavailable(err)
		}
		if seen[*parent] {
			return nil, internal(nil)
		}
		seen[*parent] = true
		row, err = loadDocument(ctx, x, project, *parent)
		if err != nil {
			return nil, internal(err)
		}
		if row.head.Active == nil {
			return nil, internal(nil)
		}
		out = append(out, *row.head.Active)
		parent = row.head.Active.ParentDocumentID
	}
	for l, r := 0, len(out)-1; l < r; l, r = l+1, r-1 {
		out[l], out[r] = out[r], out[l]
	}
	return out, nil
}

func (s *Service) ReadAncestors(ctx context.Context, actor id.Actor, project id.ProjectID, document kc.DocumentID) ([]kc.DocumentRef, error) {
	if err := readInput(ctx, actor, project); err != nil {
		return nil, err
	}
	if document.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	var out []kc.DocumentRef
	err := s.read(ctx, actor, project, func(ctx context.Context, x postgres.SQLExecutor) error {
		var err error
		out, err = readAncestors(ctx, x, project, document)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

var _ kc.CanonicalFacts = (*Service)(nil)
