package knowledge

import (
	"context"
	"encoding/json"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func dbNow(ctx context.Context, x postgres.SQLExecutor) (f.Instant, error) {
	var at time.Time
	if err := x.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
		return f.Instant{}, unavailable(err)
	}
	now, err := f.NewInstant(at)
	if err != nil {
		return f.Instant{}, internal(err)
	}
	return now, nil
}

func scopeNode(d kc.DocumentRef) kc.ScopeNode {
	return kc.ScopeNode{ID: d.ID, ProjectID: d.ProjectID, ParentID: d.ParentDocumentID, ContentVersion: d.ContentVersion, Status: d.Status}
}
func loadSubtree(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID, root kc.DocumentID) ([]documentRow, []kc.ScopeNode, error) {
	rows, err := x.Query(ctx, `WITH RECURSIVE subtree(id) AS (
 SELECT id FROM agenteam_knowledge.documents WHERE project_id=$1 AND id=$2 AND status='active'
 UNION SELECT child.id FROM agenteam_knowledge.documents child JOIN subtree parent ON child.parent_document_id=parent.id
 WHERE child.project_id=$1 AND child.status='active')
 SELECT `+documentColumns+` FROM agenteam_knowledge.documents
 WHERE project_id=$1 AND id IN (SELECT id FROM subtree) ORDER BY id`, project.String(), root.String())
	if err != nil {
		return nil, nil, unavailable(err)
	}
	defer rows.Close()
	values := make([]documentRow, 0)
	nodes := make([]kc.ScopeNode, 0)
	for rows.Next() {
		row, err := scanDocument(rows)
		if err != nil {
			return nil, nil, err
		}
		if row.head.Active == nil || row.head.Active.ProjectID != project {
			return nil, nil, internal(nil)
		}
		values = append(values, *row)
		nodes = append(nodes, scopeNode(*row.head.Active))
	}
	if err = rows.Err(); err != nil {
		return nil, nil, unavailable(err)
	}
	if len(nodes) == 0 {
		return nil, nil, fault(f.NotFound)
	}
	if _, err = kc.SubtreeDigest(project, root, nodes); err != nil {
		return nil, nil, internal(err)
	}
	return values, nodes, nil
}

func (s *Service) PrepareDeleteSubtree(ctx context.Context, actor id.Actor, project id.ProjectID, root kc.DocumentID) (kc.DeletePreview, error) {
	if err := readInput(ctx, actor, project); err != nil {
		return kc.DeletePreview{}, err
	}
	if root.Validate() != nil {
		return kc.DeletePreview{}, fault(f.InvalidArgument)
	}
	var out kc.DeletePreview
	err := s.read(ctx, actor, project, func(ctx context.Context, x postgres.SQLExecutor) error {
		rows, nodes, err := loadSubtree(ctx, x, project, root)
		if err != nil {
			return err
		}
		digest, err := kc.SubtreeDigest(project, root, nodes)
		if err != nil {
			return internal(err)
		}
		now, err := dbNow(ctx, x)
		if err != nil {
			return err
		}
		expires, err := f.NewInstant(now.Time().Add(10 * time.Minute))
		if err != nil {
			return internal(err)
		}
		user, err := f.ParseID[id.User](actor.Details().UserID)
		if err != nil {
			return internal(err)
		}
		token, err := s.state().deps.Confirmations.Sign(kc.DeleteConfirmationClaims{UserID: user, ProjectID: project, RootID: root, ScopeDigest: digest, ExpiresAt: expires})
		if err != nil {
			return portError(err)
		}
		out = kc.DeletePreview{Root: root, Nodes: make([]kc.DocumentRef, len(rows)), ScopeDigest: digest, Confirmation: token, ExpiresAt: expires}
		for i, row := range rows {
			out.Nodes[i] = *row.head.Active
		}
		return out.Validate()
	})
	if err != nil {
		return kc.DeletePreview{}, err
	}
	return out, nil
}

func moveFacts(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID, target kc.DocumentID, request kc.MoveRequest) (kc.DocumentRef, kc.MoveFacts, error) {
	row, err := loadDocument(ctx, x, project, target)
	if err != nil {
		return kc.DocumentRef{}, kc.MoveFacts{}, err
	}
	if row.head.Active == nil {
		return kc.DocumentRef{}, kc.MoveFacts{}, fault(f.NotFound)
	}
	facts := kc.MoveFacts{Current: scopeNode(*row.head.Active), TargetAncestors: make([]kc.ScopeNode, 0)}
	parent := request.TargetParentID
	seen := map[kc.DocumentID]bool{target: true}
	for parent != nil {
		if seen[*parent] {
			return kc.DocumentRef{}, kc.MoveFacts{}, fault(f.InvalidArgument)
		}
		seen[*parent] = true
		node, err := loadDocument(ctx, x, project, *parent)
		if err != nil {
			return kc.DocumentRef{}, kc.MoveFacts{}, err
		}
		if node.head.Active == nil {
			return kc.DocumentRef{}, kc.MoveFacts{}, fault(f.NotFound)
		}
		facts.TargetAncestors = append(facts.TargetAncestors, scopeNode(*node.head.Active))
		parent = node.head.Active.ParentDocumentID
	}
	return *row.head.Active, facts, nil
}

func moveMapping(facts kc.MoveFacts) (f.Digest, error) {
	raw, err := json.Marshal(struct {
		Domain string
		Facts  kc.MoveFacts
	}{"knowledge.move.v1", facts})
	if err != nil {
		return "", internal(err)
	}
	return ob.DigestBytes(raw), nil
}

func (s *Service) MoveDocument(ctx context.Context, actor id.Actor, meta f.CommandMeta, project id.ProjectID, document kc.DocumentID, request kc.MoveRequest) (kc.MoveResult, error) {
	if err := readInput(ctx, actor, project); err != nil {
		return kc.MoveResult{}, err
	}
	mutation, err := kc.NewMoveMutation(actor, meta, project, document, request)
	if err != nil {
		return kc.MoveResult{}, err
	}
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return kc.MoveResult{}, err
	}
	defer done()
	plan, err := s.discoverMove(ctx, mutation)
	if err != nil {
		return kc.MoveResult{}, err
	}
	identity, err := kc.CommandIdentity(project, kc.Move, meta.IdempotencyKey)
	if err != nil {
		return kc.MoveResult{}, err
	}
	cause, err := f.NewCommandsCause(identity)
	if err != nil {
		return kc.MoveResult{}, portError(err)
	}
	var out kc.MoveResult
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		locked, err := s.AcquireMutationInTx(ctx, tx, plan, nil)
		if err != nil {
			return err
		}
		out, err = s.MoveDocumentInTx(ctx, tx, actor, meta, project, document, request, locked)
		return err
	})
	if err = txError(result); err != nil {
		return kc.MoveResult{}, err
	}
	return out, nil
}

func (s *Service) MoveDocumentInTx(ctx context.Context, tx f.Tx, actor id.Actor, meta f.CommandMeta, project id.ProjectID, document kc.DocumentID, request kc.MoveRequest, locked kc.LockedMutation) (kc.MoveResult, error) {
	if err := readInput(ctx, actor, project); err != nil {
		return kc.MoveResult{}, err
	}
	mutation, err := kc.NewMoveMutation(actor, meta, project, document, request)
	if err != nil {
		return kc.MoveResult{}, err
	}
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return kc.MoveResult{}, err
	}
	defer done()
	st := s.state()
	plan := locked.Details().Plan
	if !locked.Matches(st.issuer, tx, plan, mutation) || len(plan.Details().ObjectPlans) != 0 || len(plan.Details().EventPlans) != 0 {
		return kc.MoveResult{}, fault(f.InvalidArgument)
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return kc.MoveResult{}, portError(err)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, locked.Locks()); err != nil {
		return kc.MoveResult{}, portError(err)
	}
	if _, err = s.readScope(ctx, tx, actor, project); err != nil {
		return kc.MoveResult{}, err
	}
	semantic, err := kc.MoveDigest(actor, meta, project, document, request)
	if err != nil {
		return kc.MoveResult{}, err
	}
	previous, err := loadCommand(ctx, x, project, kc.Move, meta.IdempotencyKey)
	if err != nil {
		return kc.MoveResult{}, err
	}
	if previous != nil {
		if previous.project != project || previous.name != kc.Move || previous.key != meta.IdempotencyKey {
			return kc.MoveResult{}, internal(nil)
		}
		if previous.digest != semantic || previous.user.String() != actor.Details().UserID {
			return kc.MoveResult{}, fault(f.IdempotencyKeyReused)
		}
		if previous.state != kc.Committed {
			return kc.MoveResult{}, fault(f.ResourceBusy)
		}
		return kc.MoveResult{Document: *previous.receipt.Document, Changed: previous.receipt.Changed}, nil
	}
	if _, err = st.deps.Projects.RequireOwnerInTx(ctx, tx, actor, project, id.Mutate); err != nil {
		return kc.MoveResult{}, portError(err)
	}
	current, facts, err := moveFacts(ctx, x, project, document, request)
	if err != nil {
		return kc.MoveResult{}, err
	}
	mapping, err := moveMapping(facts)
	if err != nil {
		return kc.MoveResult{}, err
	}
	if mapping != plan.Details().DomainMapping {
		return kc.MoveResult{}, fault(f.VersionConflict)
	}
	decision, err := kc.CheckMove(project, document, request, facts)
	if err != nil {
		return kc.MoveResult{}, err
	}
	now, err := dbNow(ctx, x)
	if err != nil {
		return kc.MoveResult{}, err
	}
	if decision.Changed {
		var parent any
		if decision.To != nil {
			parent = decision.To.String()
		}
		if now.Time().Before(current.UpdatedAt.Time()) {
			now = current.UpdatedAt
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.documents SET parent_document_id=$3,updated_at=$4 WHERE project_id=$1 AND id=$2 AND status='active'`, project.String(), document.String(), parent, now.Time())
		if err != nil {
			return kc.MoveResult{}, unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return kc.MoveResult{}, internal(nil)
		}
		current.ParentDocumentID = decision.To
		current.UpdatedAt = now
		if err = st.deps.Activity.TouchActivityInTx(ctx, tx, actor); err != nil {
			return kc.MoveResult{}, portError(err)
		}
	}
	out := kc.MoveResult{Document: current, Changed: decision.Changed}
	if err = out.Validate(); err != nil {
		return kc.MoveResult{}, internal(err)
	}
	receipt := kc.MutationReceipt{Command: kc.Move, Document: &current, Changed: decision.Changed}
	if err = insertCompletedCommand(ctx, x, actor, project, document, kc.Move, meta.IdempotencyKey, semantic, receipt, now); err != nil {
		return kc.MoveResult{}, err
	}
	return out, nil
}
