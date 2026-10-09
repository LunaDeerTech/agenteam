//go:build integration

package knowledge_test

import (
	"context"
	"slices"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func independentCreate(t *testing.T, x *ownerTreeFixture, actor id.Actor, p id.ProjectID, parent *kc.DocumentID, title, body string) kc.DocumentRef {
	t.Helper()
	doc, err := x.service.CreateDocument(knowledgeContext(t), actor, treeMeta(t), kc.CreateRequest{ProjectID: p, DocumentID: treeID[kc.Document](t), ParentDocumentID: parent, Title: title}, publicationText(t, body))
	if err != nil {
		t.Fatal("real canonical tree creation", err)
	}
	publicationFacts(t, x, doc, []byte(body))
	return doc
}

// Capture only the real public result. The original private context, Tx,
// plan, and locked access are passed unchanged to the real D05 service.
type independentPutCapture struct {
	oc.Uploads
	puts []oc.PutResult
}

func (p *independentPutCapture) PublishVerifiedInTx(ctx context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, attempt oc.UploadAttempt, plan oc.AccessLockPlan, locked oc.LockedAccess) (oc.PutResult, error) {
	put, err := p.Uploads.PublishVerifiedInTx(ctx, tx, actor, owner, attempt, plan, locked)
	if err == nil {
		p.puts = append(p.puts, put)
	}
	return put, err
}

func independentObjectTx(t *testing.T, x *ownerTreeFixture, request oc.AccessRequest, apply func(context.Context, f.Tx, oc.AccessLockPlan, oc.LockedAccess) error, want f.Code) {
	t.Helper()
	plan, err := x.deps.Objects.DiscoverAccess(knowledgeContext(t), request)
	if err != nil {
		t.Fatal("must reach the actual locked Object method", err)
	}
	calls := 0
	result := x.raw.WithinTx(knowledgeContext(t), independentCause(t), func(ctx context.Context, tx f.Tx) error {
		locked, err := x.deps.Objects.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, nil)
		if err != nil {
			return err
		}
		calls++
		return apply(ctx, tx, plan, locked)
	})
	if calls != 1 {
		t.Fatal("locked Object method not invoked exactly once", calls, result.Fault())
	}
	if want == "" {
		if result.State() != f.Committed {
			t.Fatal("real Object positive control", result.Fault())
		}
	} else {
		if result.State() != f.NotCommitted {
			t.Fatal("rejected Object operation committed", result.State())
		}
		treeCode(t, result.Fault(), want)
	}
}

type independentReleasedFacts struct {
	references, operations, records int
	disposition                     string
	cleaning                        bool
}

func independentReleased(t *testing.T, x *ownerTreeFixture, object oc.ObjectID) independentReleasedFacts {
	t.Helper()
	var out independentReleasedFacts
	err := x.raw.QueryRow(knowledgeContext(t), `SELECT
 (SELECT count(*) FROM agenteam_object.object_references WHERE object_id=$1),
 (SELECT count(*) FROM agenteam_object.cleanup_operations WHERE object_id=$1),
 (SELECT count(*) FROM agenteam_knowledge.object_cleanup WHERE object_id=$1),
 u.disposition,o.cleaning FROM agenteam_object.objects o JOIN agenteam_object.uploads u ON u.object_id=o.id WHERE o.id=$1`, object.String()).Scan(&out.references, &out.operations, &out.records, &out.disposition, &out.cleaning)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestKnowledgeB02IndependentTreeReference(t *testing.T) {
	x := newPublicationFixture(t)
	t.Run("body_replacement_preserves_interleaved_parent", func(t *testing.T) {
		actor := x.human(t)
		p := x.project(t, actor, true)
		left := independentCreate(t, x, actor, p, nil, "left", "left parent")
		right := independentCreate(t, x, actor, p, nil, "right", "right parent")
		original := independentCreate(t, x, actor, p, &left.ID, "document", "original body")
		before := independentSnapshot(t, x, p)
		var hook *independentAfterPlan
		s := publicationService(t, x, func(d *knowledge.Dependencies) {
			hook = &independentAfterPlan{Appender: d.Outbox, after: func() {
				moved, err := x.service.MoveDocument(knowledgeContext(t), actor, treeMeta(t), p, original.ID, kc.MoveRequest{ExpectedParentID: &left.ID, TargetParentID: &right.ID})
				if err != nil || !moved.Changed || moved.Document.ParentDocumentID == nil || *moved.Document.ParentDocumentID != right.ID || moved.Document.ContentVersion != original.ContentVersion {
					t.Fatal("real Move interleaving", err)
				}
			}}
			d.Outbox = hook
		})
		raw := []byte("replacement body 世界")
		input, body := independentUpload(t, "text/plain", raw, len(raw), independentDigest(raw))
		meta, request := titleMeta(t, original.ContentVersion), kc.UpdateRequest{ReplaceSource: true}
		updated, err := s.UpdateDocument(knowledgeContext(t), actor, meta, p, original.ID, request, &input)
		if err != nil || hook.calls != 1 || body.closes != 1 || body.eof != 1 || body.bytes != len(raw) {
			t.Fatal("real body replacement did not finish", err)
		}
		if updated.ParentDocumentID == nil || *updated.ParentDocumentID != right.ID || updated.ContentVersion != original.ContentVersion+1 || updated.ObjectID == original.ObjectID || updated.Title != original.Title || updated.CreatedAt != original.CreatedAt || updated.CreatedBy.Details() != original.CreatedBy.Details() {
			t.Fatal("body publication overwrote current parent or immutable creation facts")
		}
		titleSameDocument(t, updated, titleCurrent(t, x, actor, p, original.ID))
		publicationFacts(t, x, updated, raw)
		independentRead(t, x, actor, updated, raw)
		old := independentReleased(t, x, original.ObjectID)
		if old.references != 0 || old.disposition != "revoked" || !old.cleaning || old.records != 1 || old.operations < 1 {
			t.Fatal("old real publication not revoked", old)
		}
		want := before
		want.committedUploads++
		want.audits++
		want.events++
		want.completed += 2 // the real Move and body Update
		if independentSnapshot(t, x, p) != want {
			t.Fatal("body update and Move produced wrong canonical/Audit/Event/command facts")
		}
		digest, err := kc.UpdateDigest(actor, meta, p, original.ID, request, &input)
		if err != nil {
			t.Fatal(err)
		}
		lookup, err := s.LookupCommand(knowledgeContext(t), actor, kc.LookupRequest{ProjectID: p, Command: kc.Update, Key: meta.IdempotencyKey, SemanticDigest: digest})
		if err != nil || lookup.State != kc.Committed || lookup.Receipt == nil || lookup.Receipt.Document == nil || !lookup.Receipt.Changed {
			t.Fatal("body replacement public receipt", err)
		}
		titleSameDocument(t, updated, *lookup.Receipt.Document)
		at := x.activity(t, actor)
		replaySource, replayBody := independentUpload(t, "text/plain", raw, len(raw), independentDigest(raw))
		replayed, err := s.UpdateDocument(knowledgeContext(t), actor, meta, p, original.ID, request, &replaySource)
		if err != nil || replayBody.closes != 1 || replayBody.bytes != 0 || hook.calls != 1 || independentSnapshot(t, x, p) != want || !x.activity(t, actor).Equal(at) {
			t.Fatal("body replay repeated source consumption or business effects", err)
		}
		titleSameDocument(t, updated, replayed)
		independentJoined(t, x, s, p)
	})
	t.Run("preview_rejects_same_count_member_exchange", func(t *testing.T) {
		actor := x.human(t)
		p := x.project(t, actor, true)
		root := independentCreate(t, x, actor, p, nil, "root", "root body")
		leaving := independentCreate(t, x, actor, p, &root.ID, "leaving", "retained outside body")
		entering := independentCreate(t, x, actor, p, nil, "entering", "entering body")
		old, err := x.service.PrepareDeleteSubtree(knowledgeContext(t), actor, p, root.ID)
		if err != nil || len(old.Nodes) != 2 {
			t.Fatal("original actual preview", err)
		}
		for _, move := range []struct {
			doc kc.DocumentID
			req kc.MoveRequest
		}{{leaving.ID, kc.MoveRequest{ExpectedParentID: &root.ID}}, {entering.ID, kc.MoveRequest{TargetParentID: &root.ID}}} {
			result, err := x.service.MoveDocument(knowledgeContext(t), actor, treeMeta(t), p, move.doc, move.req)
			if err != nil || !result.Changed {
				t.Fatal("actual membership exchange", err)
			}
		}
		fresh, err := x.service.PrepareDeleteSubtree(knowledgeContext(t), actor, p, root.ID)
		if err != nil || len(fresh.Nodes) != len(old.Nodes) || fresh.ScopeDigest == old.ScopeDigest {
			t.Fatal("same count must retain distinct exact scope", err)
		}
		for _, node := range fresh.Nodes {
			if node.ID != root.ID && node.ID != entering.ID {
				t.Fatal("fresh scope retained departed member")
			}
		}
		before, at := independentSnapshot(t, x, p), x.activity(t, actor)
		_, err = x.service.DeleteSubtree(knowledgeContext(t), actor, treeMeta(t), p, root.ID, old.Confirmation)
		treeCode(t, err, f.VersionConflict)
		if independentSnapshot(t, x, p) != before || !x.activity(t, actor).Equal(at) {
			t.Fatal("stale same-count scope changed canonical/Audit/Event/receipt/Activity")
		}
		meta := treeMeta(t)
		deleted, err := x.service.DeleteSubtree(knowledgeContext(t), actor, meta, p, root.ID, fresh.Confirmation)
		if err != nil || !deleted.CleanupPending || len(deleted.DeletedIDs) != 2 || !slices.Contains(deleted.DeletedIDs, root.ID) || !slices.Contains(deleted.DeletedIDs, entering.ID) {
			t.Fatal("fresh exact scope did not delete only current members", err)
		}
		want := before
		want.canonical -= 2
		want.audits++
		want.events += 2
		want.completed++
		if independentSnapshot(t, x, p) != want {
			t.Fatal("fresh delete did not publish exact atomic facts")
		}
		var tombstones int
		if err := x.raw.QueryRow(knowledgeContext(t), `SELECT count(*) FROM agenteam_knowledge.documents WHERE project_id=$1 AND status='deleted'`, p.String()).Scan(&tombstones); err != nil || tombstones != 2 {
			t.Fatal("exact tombstone count", tombstones, err)
		}
		current := titleCurrent(t, x, actor, p, leaving.ID)
		if current.ParentDocumentID != nil || current.ContentVersion != leaving.ContentVersion {
			t.Fatal("departed child was changed by stale or fresh scope")
		}
		independentRead(t, x, actor, current, []byte("retained outside body"))
		digest, err := kc.DeleteDigest(actor, meta, p, root.ID, fresh.Confirmation)
		if err != nil {
			t.Fatal(err)
		}
		lookup, err := x.service.LookupCommand(knowledgeContext(t), actor, kc.LookupRequest{ProjectID: p, Command: kc.DeleteSubtree, Key: meta.IdempotencyKey, SemanticDigest: digest})
		if err != nil || lookup.State != kc.Committed || lookup.Receipt == nil || !slices.Equal(lookup.Receipt.DeletedIDs, deleted.DeletedIDs) {
			t.Fatal("fresh exact public delete receipt", err)
		}
		at = x.activity(t, actor)
		replayed, err := x.service.DeleteSubtree(knowledgeContext(t), actor, meta, p, root.ID, fresh.Confirmation)
		if err != nil || !slices.Equal(replayed.DeletedIDs, deleted.DeletedIDs) || independentSnapshot(t, x, p) != want || !x.activity(t, actor).Equal(at) {
			t.Fatal("exact delete replay changed facts", err)
		}
		independentJoined(t, x, x.service, p)
	})
	t.Run("revoked_original_receipt_cannot_reopen_reference", func(t *testing.T) {
		actor := x.human(t)
		p := x.project(t, actor, true)
		capture := &independentPutCapture{Uploads: x.deps.Uploads}
		s := publicationService(t, x, func(d *knowledge.Dependencies) { d.Uploads = capture })
		raw := []byte("original receipt bytes")
		input, body := independentUpload(t, "text/plain", raw, len(raw), independentDigest(raw))
		original, err := s.CreateDocument(knowledgeContext(t), actor, treeMeta(t), kc.CreateRequest{ProjectID: p, DocumentID: treeID[kc.Document](t), Title: "receipt"}, input)
		if err != nil || body.closes != 1 || len(capture.puts) != 1 {
			t.Fatal("real original opaque receipt", err)
		}
		publicationFacts(t, x, original, raw)
		put := capture.puts[0]
		if put.Meta.ID != original.ObjectID || put.Receipt.Validate() != nil {
			t.Fatal("capture was not exact committed original upload")
		}
		owner, err := oc.NewObjectOwner(oc.Knowledge, original.ID.String(), p.String())
		if err != nil {
			t.Fatal(err)
		}
		checkReference := func(operation oc.AccessOperation, want f.Code) {
			t.Helper()
			details := oc.AccessRequestDetails{Operation: operation, Actor: actor, Owner: owner, Intent: id.Mutate}
			if operation == oc.ConsumeAccess {
				details.Receipt = put.Receipt
			} else {
				details.ObjectID = original.ObjectID
			}
			request, err := oc.NewOwnerAccess(details)
			if err != nil {
				t.Fatal(err)
			}
			independentObjectTx(t, x, request, func(ctx context.Context, tx f.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
				if operation == oc.ConsumeAccess {
					_, err := x.deps.Objects.ConsumeUploadInTx(ctx, tx, actor, owner, put.Receipt, plan, locked)
					return err
				}
				_, err := x.deps.Objects.AttachObjectInTx(ctx, tx, actor, owner, original.ObjectID, plan, locked)
				return err
			}, want)
		}
		before, at := independentSnapshot(t, x, p), x.activity(t, actor)
		checkReference(oc.ConsumeAccess, "")
		// A prospective Create upload requires its original receipt even while
		// active; bare Attach is already forbidden for that original upload.
		checkReference(oc.AttachAccess, f.Forbidden)
		if independentSnapshot(t, x, p) != before || !x.activity(t, actor).Equal(at) {
			t.Fatal("active original receipt replay duplicated business facts")
		}
		next := []byte("new exact canonical bytes")
		replacement, replacementBody := independentUpload(t, "text/plain", next, len(next), independentDigest(next))
		updated, err := s.UpdateDocument(knowledgeContext(t), actor, titleMeta(t, original.ContentVersion), p, original.ID, kc.UpdateRequest{ReplaceSource: true}, &replacement)
		if err != nil || replacementBody.closes != 1 || len(capture.puts) != 2 || updated.ObjectID == original.ObjectID {
			t.Fatal("real replacement of original upload", err)
		}
		publicationFacts(t, x, updated, next)
		// The replacement was reserved for an existing owner. Its active bare
		// Attach is the positive control for the later old-object rejection.
		activeRequest, err := oc.NewOwnerAccess(oc.AccessRequestDetails{Operation: oc.AttachAccess, Actor: actor, Owner: owner, Intent: id.Mutate, ObjectID: updated.ObjectID})
		if err != nil {
			t.Fatal(err)
		}
		independentObjectTx(t, x, activeRequest, func(ctx context.Context, tx f.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
			_, err := x.deps.Objects.AttachObjectInTx(ctx, tx, actor, owner, updated.ObjectID, plan, locked)
			return err
		}, "")
		retired := independentReleased(t, x, original.ObjectID)
		if retired.references != 0 || retired.disposition != "revoked" || !retired.cleaning || retired.records != 1 || retired.operations < 1 {
			t.Fatal("original publication was not actually revoked", retired)
		}
		var cleanupID, uploadID, reason string
		if err := x.raw.QueryRow(knowledgeContext(t), `SELECT id,upload_id,reason FROM agenteam_knowledge.object_cleanup WHERE project_id=$1 AND document_id=$2 AND object_id=$3`, p.String(), original.ID.String(), original.ObjectID.String()).Scan(&cleanupID, &uploadID, &reason); err != nil || reason != string(oc.ReplacedObject) {
			t.Fatal("actual original cleanup provenance", err)
		}
		op, err := f.ParseID[oc.CleanupOperation](cleanupID)
		if err != nil {
			t.Fatal(err)
		}
		upload, err := f.ParseID[oc.Upload](uploadID)
		if err != nil {
			t.Fatal(err)
		}
		before, at = independentSnapshot(t, x, p), x.activity(t, actor)
		if before != (independentFacts{documents: 1, canonical: 1, committedUploads: 2, audits: 2, events: 2, completed: 2}) {
			t.Fatal("two actual publications required before negative controls")
		}
		checkReference(oc.ConsumeAccess, f.ResourceDeleted)
		checkReference(oc.AttachAccess, f.ResourceDeleted)
		for _, variant := range []string{"original", "wrong_operation", "wrong_reason"} {
			details := oc.CleanupDetails{OperationID: op, Owner: owner, Reason: oc.ReplacedObject}
			want := f.Code("")
			if variant == "wrong_operation" {
				details.OperationID, want = treeID[oc.CleanupOperation](t), f.Forbidden
			} else if variant == "wrong_reason" {
				details.Reason, want = oc.OwnerDeleted, f.Forbidden
			}
			cause, err := oc.NewObjectCleanupCause(details)
			if err != nil {
				t.Fatal(err)
			}
			request, err := oc.NewCleanupReleaseAccess(oc.AccessRequestDetails{Operation: oc.ReleaseForCleanupAccess, Cleanup: cause, ObjectID: original.ObjectID, UploadID: upload})
			if err != nil {
				t.Fatal(err)
			}
			independentObjectTx(t, x, request, func(ctx context.Context, tx f.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
				return x.deps.ReferenceCleanup.ReleaseForCleanupInTx(ctx, tx, cause, original.ObjectID, plan, locked)
			}, want)
			if independentReleased(t, x, original.ObjectID) != retired || independentSnapshot(t, x, p) != before || !x.activity(t, actor).Equal(at) {
				t.Fatal(variant, "changed original gate or current canonical facts")
			}
		}
		titleSameDocument(t, updated, titleCurrent(t, x, actor, p, original.ID))
		independentRead(t, x, actor, updated, next)
		independentJoined(t, x, s, p)
	})
}
