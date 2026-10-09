//go:build integration

package knowledge_test

import (
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func recoveryChild(t *testing.T, x *ownerTreeFixture, actor id.Actor, parent kc.DocumentRef) kc.DocumentRef {
	t.Helper()
	d, err := x.service.CreateDocument(knowledgeContext(t), actor, treeMeta(t), kc.CreateRequest{ProjectID: parent.ProjectID, DocumentID: treeID[kc.Document](t), ParentDocumentID: &parent.ID, Title: "child"}, publicationText(t, "child bytes"))
	if err != nil {
		t.Fatal("real child creation", err)
	}
	return d
}

func recoveryDeleteFacts(t *testing.T, x *ownerTreeFixture, p id.ProjectID) (deletions, objectDeletes, pending int) {
	t.Helper()
	err := x.raw.QueryRow(knowledgeContext(t), `SELECT
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='knowledge.delete_subtree'),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='object.delete'),
 (SELECT count(*) FROM agenteam_knowledge.object_cleanup WHERE project_id=$1 AND phase<>'completed')`, p.String()).Scan(&deletions, &objectDeletes, &pending)
	if err != nil {
		t.Fatal(err)
	}
	return
}

// Real, same-process cleanup only. No missing process is treated as stopped,
// and no Object runtime or crash/COMMIT Unknown conclusion follows from this top.
func TestKnowledgeB02Cleanup(t *testing.T) {
	t.Run("deleted_subtree_waits_for_actual_reader", func(t *testing.T) {
		x := newPublicationFixture(t)
		actor := x.human(t)
		p := x.project(t, actor, true)
		// D05 eagerly verifies/releases a small payload during Open. Keep this
		// body beyond its initial integrity window, then assert a real lease;
		// merely retaining an ObjectReader value is not a live-reader proof.
		root := publicationSeedContent(t, x, actor, p, strings.Repeat("r", 2*oc.StreamBufferSize+1))
		child := recoveryChild(t, x, actor, root)
		outside := publicationSeedContent(t, x, actor, p, "outside bytes")
		stale, err := x.service.PrepareDeleteSubtree(knowledgeContext(t), actor, p, root.ID)
		if err != nil || len(stale.Nodes) != 2 {
			t.Fatal("initial exact subtree", err)
		}
		title := "changed child"
		_, err = x.service.UpdateDocument(knowledgeContext(t), actor, titleMeta(t, 1), p, child.ID, kc.UpdateRequest{Title: &title}, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = x.service.DeleteSubtree(knowledgeContext(t), actor, treeMeta(t), p, root.ID, stale.Confirmation)
		treeCode(t, err, f.VersionConflict)
		preview, err := x.service.PrepareDeleteSubtree(knowledgeContext(t), actor, p, root.ID)
		if err != nil || len(preview.Nodes) != 2 {
			t.Fatal("fresh exact subtree", err)
		}
		reader, err := x.service.OpenCanonical(knowledgeContext(t), actor, p, root.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		closed := false
		defer func() {
			if !closed {
				if err := reader.Close(); err != nil {
					t.Error("held reader actual Close", err)
				}
			}
		}()
		var activeReaders int
		if err = x.raw.QueryRow(knowledgeContext(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND owner_kind='reader' AND state='active'`, root.ObjectID.String()).Scan(&activeReaders); err != nil || activeReaders != 1 {
			t.Fatal("exact actual reader was not active before deletion", err)
		}
		meta := treeMeta(t)
		result, err := x.service.DeleteSubtree(knowledgeContext(t), actor, meta, p, root.ID, preview.Confirmation)
		if err != nil || !result.CleanupPending || len(result.DeletedIDs) != 2 {
			t.Fatal("public subtree deletion", err)
		}
		seen := map[kc.DocumentID]bool{}
		for _, key := range result.DeletedIDs {
			seen[key] = true
		}
		if !seen[root.ID] || !seen[child.ID] {
			t.Fatal("delete crossed exact subtree")
		}
		for _, key := range []kc.DocumentID{root.ID, child.ID} {
			head, err := x.service.GetDocument(knowledgeContext(t), actor, p, key)
			if err != nil || head.Deleted == nil || head.Active != nil {
				t.Fatal("missing public tombstone", err)
			}
		}
		publicationRead(t, x, actor, outside, []byte("outside bytes"))
		if d, _, pending := recoveryDeleteFacts(t, x, p); d != 1 || pending != 2 {
			t.Fatal("delete audit or durable cleanup set")
		}
		// Cleanup of an already-deleted exact scope may converge after archive;
		// this is not a grant for a new ordinary Owner mutation.
		x.archive(t, p)
		err = x.service.RecoverCleanup(knowledgeContext(t))
		treeCode(t, err, f.ResourceBusy)
		var phase, state string
		if err = x.raw.QueryRow(knowledgeContext(t), `SELECT c.phase,o.state FROM agenteam_knowledge.object_cleanup c JOIN agenteam_object.objects o ON o.id=c.object_id WHERE c.project_id=$1 AND c.object_id=$2`, p.String(), root.ObjectID.String()).Scan(&phase, &state); err != nil || phase == "completed" || state == "deleted" {
			t.Fatal("live real reader was called retired", err)
		}
		if err = reader.Close(); err != nil {
			t.Fatal("actual reader close before retry", err)
		}
		closed = true
		if err = x.service.RecoverCleanup(knowledgeContext(t)); err != nil {
			t.Fatal("real cleanup after Close", err)
		}
		if d, objects, pending := recoveryDeleteFacts(t, x, p); d != 1 || objects != 2 || pending != 0 {
			t.Fatal("cleanup completion and exact Object Audit facts")
		}
		var deleted int
		if err = x.raw.QueryRow(knowledgeContext(t), `SELECT count(*) FROM agenteam_object.objects WHERE id IN ($1,$2) AND state='deleted'`, root.ObjectID.String(), child.ObjectID.String()).Scan(&deleted); err != nil || deleted != 2 {
			t.Fatal("Object cleanup final states", err)
		}
		beforeEvents, beforeActivity := titleEventCount(t, x, p), x.activity(t, actor)
		replay, err := x.service.DeleteSubtree(knowledgeContext(t), actor, meta, p, root.ID, preview.Confirmation)
		if err != nil || replay.Root != result.Root || replay.CleanupPending != result.CleanupPending || len(replay.DeletedIDs) != 2 {
			t.Fatal("archived delete stable replay", err)
		}
		if err = x.service.RecoverCleanup(knowledgeContext(t)); err != nil {
			t.Fatal("idempotent cleanup replay", err)
		}
		if d, objects, pending := recoveryDeleteFacts(t, x, p); d != 1 || objects != 2 || pending != 0 || titleEventCount(t, x, p) != beforeEvents || !x.activity(t, actor).Equal(beforeActivity) {
			t.Fatal("cleanup/replay duplicated user facts")
		}
		publicationRead(t, x, actor, outside, []byte("outside bytes"))
	})
	t.Run("pending_reader_does_not_starve_another_project", func(t *testing.T) {
		x := newPublicationFixture(t)
		actor := x.human(t)
		firstProject, laterProject := x.project(t, actor, true), x.project(t, actor, true)
		first := publicationSeedContent(t, x, actor, firstProject, strings.Repeat("p", 2*oc.StreamBufferSize+1))
		later := publicationSeedContent(t, x, actor, laterProject, strings.Repeat("q", 2*oc.StreamBufferSize+1))
		reader, err := x.service.OpenCanonical(knowledgeContext(t), actor, firstProject, first.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		closed := false
		defer func() {
			if !closed {
				if err := reader.Close(); err != nil {
					t.Error("held reader Close", err)
				}
			}
		}()
		var lease string
		if err = x.raw.QueryRow(knowledgeContext(t), `SELECT id::text FROM agenteam_object.object_leases
 WHERE object_id=$1 AND owner_kind='reader' AND state='active'`, first.ObjectID.String()).Scan(&lease); err != nil {
			t.Fatal("first cleanup lacks an actual active reader", err)
		}
		otherReader, err := x.service.OpenCanonical(knowledgeContext(t), actor, laterProject, later.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		otherClosed := false
		defer func() {
			if !otherClosed {
				if err := otherReader.Close(); err != nil {
					t.Error("other reader Close", err)
				}
			}
		}()
		var otherLease string
		if err = x.raw.QueryRow(knowledgeContext(t), `SELECT id::text FROM agenteam_object.object_leases
 WHERE object_id=$1 AND owner_kind='reader' AND state='active'`, later.ObjectID.String()).Scan(&otherLease); err != nil {
			t.Fatal("other cleanup lacks an actual active reader", err)
		}
		for _, document := range []kc.DocumentRef{first, later} {
			preview, err := x.service.PrepareDeleteSubtree(knowledgeContext(t), actor, document.ProjectID, document.ID)
			if err != nil || len(preview.Nodes) != 1 {
				t.Fatal("exact single-document delete", err)
			}
			result, err := x.service.DeleteSubtree(knowledgeContext(t), actor, treeMeta(t), document.ProjectID, document.ID, preview.Confirmation)
			if err != nil || !result.CleanupPending || len(result.DeletedIDs) != 1 {
				t.Fatal("real independent pending cleanup", err)
			}
		}
		var ordered bool
		if err = x.raw.QueryRow(knowledgeContext(t), `SELECT a.id<b.id FROM agenteam_knowledge.object_cleanup a,
 agenteam_knowledge.object_cleanup b WHERE a.object_id=$1 AND b.object_id=$2`, first.ObjectID.String(), later.ObjectID.String()).Scan(&ordered); err != nil {
			t.Fatal("actual cleanup order", err)
		}
		// UUIDv7 does not promise ordering between allocations in one millisecond.
		// Hold both real readers until the persisted IDs establish the order;
		// close only the later one, never alter either durable cleanup identity.
		if !ordered {
			first, later = later, first
			firstProject, laterProject = laterProject, firstProject
			reader, otherReader = otherReader, reader
			lease, otherLease = otherLease, lease
		}
		if err = otherReader.Close(); err != nil {
			t.Fatal("actual later reader Close", err)
		}
		otherClosed = true
		beforeActivity := x.activity(t, actor)
		beforeFirstEvents, beforeLaterEvents := titleEventCount(t, x, firstProject), titleEventCount(t, x, laterProject)
		for range 2 {
			treeCode(t, x.service.RecoverCleanup(knowledgeContext(t)), f.ResourceBusy)
			if d, deleted, pending := recoveryDeleteFacts(t, x, firstProject); d != 1 || deleted != 0 || pending != 1 {
				t.Fatal("held first reader was falsely retired")
			}
			if d, deleted, pending := recoveryDeleteFacts(t, x, laterProject); d != 1 || deleted != 1 || pending != 0 {
				t.Fatal("first Pending starved another project or repeated deletion")
			}
			var active bool
			if err = x.raw.QueryRow(knowledgeContext(t), `SELECT state='active' FROM agenteam_object.object_leases
 WHERE id=$1 AND object_id=$2 AND owner_kind='reader'`, lease, first.ObjectID.String()).Scan(&active); err != nil || !active {
				t.Fatal("first reader stopped being an actual blocker", err)
			}
			var deleted bool
			if err = x.raw.QueryRow(knowledgeContext(t), `SELECT state='deleted' FROM agenteam_object.objects WHERE id=$1`, later.ObjectID.String()).Scan(&deleted); err != nil || !deleted {
				t.Fatal("later cleanup did not really delete its object", err)
			}
		}
		if err = reader.Close(); err != nil {
			t.Fatal("actual first reader Close", err)
		}
		closed = true
		if err = x.service.RecoverCleanup(knowledgeContext(t)); err != nil {
			t.Fatal("first cleanup did not converge after actual Close", err)
		}
		for _, project := range []id.ProjectID{firstProject, laterProject} {
			if d, deleted, pending := recoveryDeleteFacts(t, x, project); d != 1 || deleted != 1 || pending != 0 {
				t.Fatal("final exact cleanup facts are not once-only")
			}
		}
		if !x.activity(t, actor).Equal(beforeActivity) || titleEventCount(t, x, firstProject) != beforeFirstEvents || titleEventCount(t, x, laterProject) != beforeLaterEvents {
			t.Fatal("technical cleanup emitted user Activity or configuration events")
		}
	})
	t.Run("delete_final_transaction_rollback", func(t *testing.T) {
		x := newPublicationFixture(t)
		actor := x.human(t)
		p := x.project(t, actor, true)
		root := publicationSeedContent(t, x, actor, p, "root")
		child := recoveryChild(t, x, actor, root)
		preview, err := x.service.PrepareDeleteSubtree(knowledgeContext(t), actor, p, root.ID)
		if err != nil {
			t.Fatal(err)
		}
		injected := errors.New("delete final activity rollback")
		var activity *titleFailActivity
		s := publicationService(t, x, func(d *knowledge.Dependencies) {
			activity = &titleFailActivity{ActivityAuthority: d.Activity, err: injected}
			d.Activity = activity
		})
		meta := treeMeta(t)
		beforeEvents, beforeActivity := titleEventCount(t, x, p), x.activity(t, actor)
		_, err = s.DeleteSubtree(knowledgeContext(t), actor, meta, p, root.ID, preview.Confirmation)
		if !errors.Is(err, injected) || activity.calls != 1 {
			t.Fatal("actual delete final boundary not reached", err)
		}
		for _, original := range []kc.DocumentRef{root, child} {
			titleSameDocument(t, original, titleCurrent(t, x, actor, p, original.ID))
		}
		publicationRead(t, x, actor, root, []byte("root"))
		publicationRead(t, x, actor, child, []byte("child bytes"))
		if d, objects, pending := recoveryDeleteFacts(t, x, p); d != 0 || objects != 0 || pending != 0 || titleEventCount(t, x, p) != beforeEvents || !x.activity(t, actor).Equal(beforeActivity) {
			t.Fatal("delete rollback leaked Audit/cleanup/Outbox/Activity")
		}
		digest, err := kc.DeleteDigest(actor, meta, p, root.ID, preview.Confirmation)
		if err != nil {
			t.Fatal(err)
		}
		lookup, err := x.service.LookupCommand(knowledgeContext(t), actor, kc.LookupRequest{ProjectID: p, Command: kc.DeleteSubtree, Key: meta.IdempotencyKey, SemanticDigest: digest})
		// Discovery committed the original delete plan in its own transaction.
		// The failed final transaction must leave that plan resumable, with no
		// completed receipt; it must not erase the already-durable intent.
		if err != nil || lookup.State != kc.InProgress || lookup.Receipt != nil {
			t.Fatal("rolled-back delete did not retain an incomplete original plan", lookup.State, lookup.Receipt != nil, err)
		}
		var planned bool
		if err = x.raw.QueryRow(knowledgeContext(t), `SELECT state='planned' AND request IS NOT NULL AND plan IS NOT NULL AND receipt IS NULL
 FROM agenteam_knowledge.commands WHERE project_id=$1 AND command_name='delete-subtree' AND command_key=$2`, p.String(), string(meta.IdempotencyKey)).Scan(&planned); err != nil || !planned {
			t.Fatal("delete final rollback changed its durable preparation", err)
		}
		var published int
		if err = x.raw.QueryRow(knowledgeContext(t), `SELECT count(*) FROM agenteam_object.object_references WHERE object_id IN ($1,$2) AND kind='canonical'`, root.ObjectID.String(), child.ObjectID.String()).Scan(&published); err != nil || published != 2 {
			t.Fatal("rollback released real references", err)
		}
		resumed, err := x.service.DeleteSubtree(knowledgeContext(t), actor, meta, p, root.ID, preview.Confirmation)
		if err != nil || resumed.Root != root.ID || len(resumed.DeletedIDs) != 2 || !resumed.CleanupPending {
			t.Fatal("original delete key/token did not resume", err)
		}
		if d, objects, pending := recoveryDeleteFacts(t, x, p); d != 1 || objects != 0 || pending != 2 || titleEventCount(t, x, p) != beforeEvents+2 {
			t.Fatal("resumed delete lost or duplicated final facts")
		}
		lookup, err = x.service.LookupCommand(knowledgeContext(t), actor, kc.LookupRequest{ProjectID: p, Command: kc.DeleteSubtree, Key: meta.IdempotencyKey, SemanticDigest: digest})
		if err != nil || lookup.State != kc.Committed || lookup.Receipt == nil || len(lookup.Receipt.DeletedIDs) != 2 {
			t.Fatal("resumed delete did not expose the committed receipt", err)
		}
		afterActivity := x.activity(t, actor)
		replay, err := x.service.DeleteSubtree(knowledgeContext(t), actor, meta, p, root.ID, preview.Confirmation)
		if err != nil || replay.Root != resumed.Root || len(replay.DeletedIDs) != 2 || replay.CleanupPending != resumed.CleanupPending || !x.activity(t, actor).Equal(afterActivity) {
			t.Fatal("original completed delete replay changed receipt or activity", err)
		}
		if d, _, pending := recoveryDeleteFacts(t, x, p); d != 1 || pending != 2 || titleEventCount(t, x, p) != beforeEvents+2 {
			t.Fatal("original completed delete replay repeated side effects")
		}
	})
}
