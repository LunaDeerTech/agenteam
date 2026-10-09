package knowledge

import (
	"context"
	"slices"

	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

// ProjectAuditAuthority proves only Knowledge facts. Its caller must separately
// authorize the current Project/Session in the same live transaction.
type ProjectAuditAuthority struct{ data func() Store }

func NewProjectAuditAuthority(store Store) (*ProjectAuditAuthority, error) {
	if nilPort(store) || !sameStore(store, store) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ProjectAuditAuthority{data: func() Store { return store }}, nil
}

type deleteAuditWitnessKey struct{}
type deleteAuditWitness struct {
	store     Store
	tx        f.Tx
	command   f.ID[command]
	key       f.IdempotencyKey
	entry     au.Entry
	appendKey au.AppendKey
	nodes     []kc.ScopeNode
}

func deleteAuditKey(project id.ProjectID, key f.IdempotencyKey) (au.AppendKey, error) {
	identity, err := kc.CommandIdentity(project, kc.DeleteSubtree, key)
	if err != nil {
		return au.AppendKey{}, portError(err)
	}
	// The same canonical SHA-256 binding as audit.CommandAppendKey, without
	// depending on the Audit implementation or exposing the original key.
	digest, err := cursor.Digest([]byte(identity.Canonical()))
	if err != nil {
		return au.AppendKey{}, internal(err)
	}
	return au.NewAppendKey(au.KnowledgeProducer, digest.String(), 0)
}

func deleteAuditEntry(actor id.Actor, project id.ProjectID, root kc.DocumentID, nodes []kc.ScopeNode) (au.Entry, error) {
	digest, err := kc.SubtreeDigest(project, root, nodes)
	if err != nil {
		return au.Entry{}, portError(err)
	}
	meta, err := au.KnowledgeMetadata(au.KnowledgeDeleteSubtree, au.KnowledgeMetadataFields{
		ProjectID: project.String(), RootID: root.String(), InitiatorID: actor.Details().UserID,
		ScopeDigest: digest, DeletedCount: f.Progress(len(nodes)),
	})
	if err != nil {
		return au.Entry{}, portError(err)
	}
	scope, err := id.InProject(project)
	if err != nil {
		return au.Entry{}, portError(err)
	}
	resource, err := au.NewResource(au.KnowledgeDocumentResource, root.String())
	if err != nil {
		return au.Entry{}, portError(err)
	}
	return au.NewEntry(au.EntryFields{Scope: scope, Actor: actor, Action: au.KnowledgeDeleteSubtree, Outcome: au.Success, Resource: resource, Metadata: meta})
}

func (s *Service) appendDeleteAudit(ctx context.Context, tx f.Tx, actor id.Actor, record *commandRecord, nodes []kc.ScopeNode) error {
	if record == nil || record.name != kc.DeleteSubtree || record.state != kc.Committed || record.receipt == nil || record.user.String() != actor.Details().UserID {
		return internal(nil)
	}
	entry, err := deleteAuditEntry(actor, record.project, record.document, nodes)
	if err != nil {
		return err
	}
	key, err := deleteAuditKey(record.project, record.key)
	if err != nil {
		return err
	}
	cloned := slices.Clone(nodes)
	for n := range cloned {
		if cloned[n].ParentID != nil {
			p := *cloned[n].ParentID
			cloned[n].ParentID = &p
		}
	}
	witness := deleteAuditWitness{s.state().store, tx, record.id, record.key, entry, key, cloned}
	_, err = s.state().deps.Audit.AppendInTx(context.WithValue(ctx, deleteAuditWitnessKey{}, witness), tx, entry, key)
	return portError(err)
}

func (a *ProjectAuditAuthority) CheckProjectAuditInTx(ctx context.Context, tx f.Tx, entry au.Entry, key au.AppendKey) error {
	if a == nil || a.data == nil || nilPort(a.data()) {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() || entry.Validate() != nil || key.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return unavailable(err)
	}
	e, k := entry.Fields(), key.Details()
	if e.Action != au.KnowledgeDeleteSubtree || e.Outcome != au.Success || e.Actor.Details().Kind != id.Human || e.Scope.Details().Kind != id.ProjectScope || e.Resource.Details().Kind != au.KnowledgeDocumentResource || k.Producer != au.KnowledgeProducer || k.Ordinal != 0 {
		return fault(f.Forbidden)
	}
	w, ok := ctx.Value(deleteAuditWitnessKey{}).(deleteAuditWitness)
	if !ok || w.tx != tx || !sameStore(a.data(), w.store) || w.command.Validate() != nil || w.key.Validate() != nil || w.appendKey.Validate() != nil || w.appendKey.Details() != k {
		return fault(f.Forbidden)
	}
	// Preserve actual Actor/Session and every field, not Audit's replay digest,
	// which deliberately projects a stable user across renewed Sessions.
	actualMeta, err := e.Metadata.KnowledgeFields()
	if err != nil {
		return portError(err)
	}
	wf := w.entry.Fields()
	expectedMeta, err := wf.Metadata.KnowledgeFields()
	if err != nil || !e.Actor.Equal(wf.Actor) || !e.Scope.Equal(wf.Scope) || e.Action != wf.Action || e.Outcome != wf.Outcome || e.Resource.Details() != wf.Resource.Details() || e.Associations != wf.Associations || actualMeta != expectedMeta {
		return fault(f.Forbidden)
	}
	project, err := f.ParseID[id.Project](actualMeta.ProjectID)
	if err != nil {
		return fault(f.Forbidden)
	}
	root, err := f.ParseID[kc.Document](actualMeta.RootID)
	if err != nil {
		return fault(f.Forbidden)
	}
	digest, err := kc.SubtreeDigest(project, root, w.nodes)
	if err != nil || digest != actualMeta.ScopeDigest || actualMeta.DeletedCount != f.Progress(len(w.nodes)) {
		return fault(f.Forbidden)
	}
	expectedKey, err := deleteAuditKey(project, w.key)
	if err != nil || expectedKey.Details() != k {
		return fault(f.Forbidden)
	}
	store := a.data()
	x, err := store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	locks, err := scopeLocks(e.Actor, project, true)
	if err != nil {
		return err
	}
	identity, err := kc.CommandIdentity(project, kc.DeleteSubtree, w.key)
	if err != nil {
		return portError(err)
	}
	commandLock, err := f.CommandLock(identity)
	if err != nil {
		return portError(err)
	}
	locks, err = ob.NormalizeLocks(append(locks, f.LockRequest{Key: commandLock, Mode: f.Exclusive}))
	if err != nil {
		return portError(err)
	}
	if err = store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	record, err := loadCommand(ctx, x, project, kc.DeleteSubtree, w.key)
	if err != nil {
		return err
	}
	if record == nil || record.id != w.command || record.document != root || record.user.String() != e.Actor.Details().UserID || record.state != kc.Committed || record.receipt == nil || record.receipt.RootID == nil || *record.receipt.RootID != root || len(record.receipt.DeletedIDs) != len(w.nodes) {
		return fault(f.Forbidden)
	}
	ids := make(map[kc.DocumentID]bool, len(record.receipt.DeletedIDs))
	for _, key := range record.receipt.DeletedIDs {
		ids[key] = true
	}
	for _, node := range w.nodes {
		if !ids[node.ID] || node.Status != kc.Active {
			return fault(f.Forbidden)
		}
		row, err := loadDocument(ctx, x, project, node.ID)
		if err != nil {
			return err
		}
		if row.head.Deleted == nil || row.head.Deleted.ContentVersion != node.ContentVersion {
			return fault(f.Forbidden)
		}
	}
	return nil
}

var _ au.ProjectFactAuthority = (*ProjectAuditAuthority)(nil)
