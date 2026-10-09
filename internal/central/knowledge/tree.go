package knowledge

import (
	"context"
	"encoding/json"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
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

func (s *Service) DeleteSubtree(ctx context.Context, actor id.Actor, meta f.CommandMeta, project id.ProjectID, root kc.DocumentID, token kc.ConfirmationToken) (kc.DeleteResult, error) {
	if err := readInput(ctx, actor, project); err != nil {
		return kc.DeleteResult{}, err
	}
	request, err := kc.NewDeleteMutation(actor, meta, project, root, token)
	if err != nil {
		return kc.DeleteResult{}, err
	}
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return kc.DeleteResult{}, err
	}
	defer done()
	plan, err := s.DiscoverMutation(ctx, request)
	if err != nil {
		return kc.DeleteResult{}, err
	}
	identity, err := kc.CommandIdentity(project, kc.DeleteSubtree, meta.IdempotencyKey)
	if err != nil {
		return kc.DeleteResult{}, err
	}
	cause, err := f.NewCommandsCause(identity)
	if err != nil {
		return kc.DeleteResult{}, portError(err)
	}
	var out kc.DeleteResult
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		locked, err := s.AcquireMutationInTx(ctx, tx, plan, nil)
		if err != nil {
			return err
		}
		out, err = s.DeleteSubtreeInTx(ctx, tx, actor, meta, project, root, token, locked)
		return err
	})
	if err = txError(result); err != nil {
		return kc.DeleteResult{}, err
	}
	return out, nil
}

func (s *Service) DeleteSubtreeInTx(ctx context.Context, tx f.Tx, actor id.Actor, meta f.CommandMeta, project id.ProjectID, root kc.DocumentID, token kc.ConfirmationToken, locked kc.LockedMutation) (kc.DeleteResult, error) {
	if err := readInput(ctx, actor, project); err != nil {
		return kc.DeleteResult{}, err
	}
	request, err := kc.NewDeleteMutation(actor, meta, project, root, token)
	if err != nil {
		return kc.DeleteResult{}, err
	}
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return kc.DeleteResult{}, err
	}
	defer done()
	st := s.state()
	plan := locked.Details().Plan
	if !locked.Matches(st.issuer, tx, plan, request) {
		return kc.DeleteResult{}, fault(f.InvalidArgument)
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return kc.DeleteResult{}, portError(err)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, locked.Locks()); err != nil {
		return kc.DeleteResult{}, portError(err)
	}
	if _, err = s.readScope(ctx, tx, actor, project); err != nil {
		return kc.DeleteResult{}, err
	}
	semantic, err := kc.DeleteDigest(actor, meta, project, root, token)
	if err != nil {
		return kc.DeleteResult{}, err
	}
	record, err := loadCommand(ctx, x, project, kc.DeleteSubtree, meta.IdempotencyKey)
	if err != nil {
		return kc.DeleteResult{}, err
	}
	if record == nil {
		return kc.DeleteResult{}, fault(f.ResourceBusy)
	}
	if record.digest != semantic || record.user.String() != actor.Details().UserID {
		return kc.DeleteResult{}, fault(f.IdempotencyKeyReused)
	}
	if record.state == kc.Committed {
		// Completed replay precedes old token-key, scope and cleanup checks.
		return kc.DeleteResult{Root: *record.receipt.RootID, DeletedIDs: append([]kc.DocumentID(nil), record.receipt.DeletedIDs...), CleanupPending: record.receipt.CleanupPending}, nil
	}
	grant, err := st.deps.Projects.RequireOwnerInTx(ctx, tx, actor, project, id.Mutate)
	if err != nil {
		return kc.DeleteResult{}, portError(err)
	}
	if !grant.Matches(actor, project) {
		return kc.DeleteResult{}, internal(nil)
	}
	planned, err := loadDeletePlan(ctx, x, record)
	if err != nil {
		return kc.DeleteResult{}, err
	}
	mapping, err := planned.mapping()
	if err != nil {
		return kc.DeleteResult{}, err
	}
	if mapping != plan.Details().DomainMapping {
		return kc.DeleteResult{}, fault(f.VersionConflict)
	}
	rows, err := s.validateDeleteScope(ctx, x, request)
	if err != nil {
		return kc.DeleteResult{}, err
	}
	if !deleteRowsMatch(planned, rows) {
		return kc.DeleteResult{}, fault(f.VersionConflict)
	}
	objects, events := plan.Details().ObjectPlans, plan.Details().EventPlans
	if len(objects) != len(planned.Nodes) || len(events) != len(planned.Nodes) {
		return kc.DeleteResult{}, fault(f.InvalidArgument)
	}
	now, err := dbNow(ctx, x)
	if err != nil {
		return kc.DeleteResult{}, err
	}
	if now.Time().Before(record.created.Time()) {
		now = record.created
	}
	out := kc.DeleteResult{Root: root, DeletedIDs: make([]kc.DocumentID, len(planned.Nodes)), CleanupPending: true}
	// Revoke all current Knowledge pointers before asking D05 to close exact
	// canonical/reserved/upload gates. Every statement stays in this same Tx.
	for j, node := range planned.Nodes {
		tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.documents SET status='deleted',deleted_at=$3,
 parent_document_id=NULL,title=NULL,source_kind=NULL,media_type=NULL,current_object_id=NULL,current_upload_id=NULL,
 indexing_status=NULL,creator_user_id=NULL,created_at=NULL,updated_at=NULL
 WHERE project_id=$1 AND id=$2 AND status='active' AND content_version=$4 AND current_object_id=$5 AND current_upload_id=$6`, project.String(), node.Node.ID.String(), now.Time(), int64(node.Node.ContentVersion), node.Object.String(), node.Upload.String())
		if err != nil {
			return kc.DeleteResult{}, unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return kc.DeleteResult{}, internal(nil)
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_knowledge.object_cleanup(id,project_id,document_id,command_id,object_id,upload_id,reason,phase)
 VALUES($1,$2,$3,$4,$5,$6,'owner_deleted','reference')`, node.Cleanup.String(), project.String(), node.Node.ID.String(), record.id.String(), node.Object.String(), node.Upload.String())
		if err != nil {
			return kc.DeleteResult{}, unavailable(err)
		}
		out.DeletedIDs[j] = node.Node.ID
	}
	for j, node := range planned.Nodes {
		owner, err := oc.NewObjectOwner(oc.Knowledge, node.Node.ID.String(), project.String())
		if err != nil {
			return kc.DeleteResult{}, internal(err)
		}
		cause, err := oc.NewObjectCleanupCause(oc.CleanupDetails{OperationID: node.Cleanup, Owner: owner, Reason: oc.OwnerDeleted})
		if err != nil {
			return kc.DeleteResult{}, internal(err)
		}
		if err = st.deps.ReferenceCleanup.ReleaseForCleanupInTx(ctx, tx, cause, node.Object, objects[j], locked.Details().ObjectAccess); err != nil {
			return kc.DeleteResult{}, portError(err)
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.object_cleanup SET phase='object' WHERE id=$1 AND phase='reference'`, node.Cleanup.String())
		if err != nil {
			return kc.DeleteResult{}, unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return kc.DeleteResult{}, internal(nil)
		}
		event, err := st.deps.Events.Deleted(node.Event, kc.DeletedPayload{DocumentID: node.Node.ID, ContentVersion: node.Node.ContentVersion})
		if err != nil {
			return kc.DeleteResult{}, portError(err)
		}
		if _, err = st.deps.Outbox.AppendEventInTx(ctx, tx, actor, event, events[j]); err != nil {
			return kc.DeleteResult{}, portError(err)
		}
	}
	if err = out.Validate(); err != nil {
		return kc.DeleteResult{}, internal(err)
	}
	receipt := kc.MutationReceipt{Command: kc.DeleteSubtree, RootID: &root, Changed: true, DeletedIDs: out.DeletedIDs, CleanupPending: true}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return kc.DeleteResult{}, internal(err)
	}
	if len(raw) > 4<<20 {
		return kc.DeleteResult{}, fault(f.ResourceBusy)
	}
	tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.commands SET state='completed',receipt=$3::jsonb,request=NULL,plan=NULL,committed_at=$4
 WHERE project_id=$1 AND id=$2 AND state='planned'`, project.String(), record.id.String(), raw, now.Time())
	if err != nil {
		return kc.DeleteResult{}, unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return kc.DeleteResult{}, internal(nil)
	}
	record.state, record.receipt, record.committed = kc.Committed, &receipt, &now
	if err = s.appendDeleteAudit(ctx, tx, actor, record, planned.scope()); err != nil {
		return kc.DeleteResult{}, err
	}
	if err = st.deps.Activity.TouchActivityInTx(ctx, tx, actor); err != nil {
		return kc.DeleteResult{}, portError(err)
	}
	return out, nil
}

var _ kc.AtomicTreeMutations = (*Service)(nil)
