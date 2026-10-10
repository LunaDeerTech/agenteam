//go:build integration

package knowledge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	ev "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
)

func titleService(t *testing.T, x *ownerTreeFixture, change func(*knowledge.Dependencies)) *knowledge.Service {
	t.Helper()
	deps := x.deps
	projects := deps.Projects.(*project.Authority)
	accounts := deps.Activity.(*account.Authority)
	authority, err := knowledge.NewAuthority(x.raw, projects)
	if err != nil {
		t.Fatal(err)
	}
	catalog := ev.NewCatalog()
	deps.Events, err = kc.RegisterKnowledgeEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	deps.Outbox, err = outbox.New(x.raw, catalog, outbox.Authorizations{
		Projects: projects, Sessions: accounts, System: accounts, Processes: deps.Processes,
		Audit: deps.Audit, Cursors: deps.Cursors, Producers: map[ev.StableName]ob.ProducerAuthority{kc.KnowledgeProducer: authority},
	})
	if err != nil {
		t.Fatal(err)
	}
	if change != nil {
		change(&deps)
	}
	s, err := knowledge.New(x.raw, deps)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.Drain(ctx); err != nil {
			t.Error(err)
		}
	})
	return s
}

func titleMeta(t *testing.T, version f.Version) f.CommandMeta {
	m := treeMeta(t)
	m.ExpectedVersion = &version
	return m
}

func titleSameDocument(t *testing.T, a, b kc.DocumentRef) {
	t.Helper()
	x, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	y, err := json.Marshal(b)
	if err != nil || !bytes.Equal(x, y) {
		t.Fatal("document does not match confirmed public result", err)
	}
}

func titleLookup(t *testing.T, s *knowledge.Service, actor id.Actor, meta f.CommandMeta, p id.ProjectID, doc kc.DocumentID, req kc.UpdateRequest) kc.CommandLookup {
	t.Helper()
	digest, err := kc.UpdateDigest(actor, meta, p, doc, req, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.LookupCommand(knowledgeContext(t), actor, kc.LookupRequest{ProjectID: p, Command: kc.Update, Key: meta.IdempotencyKey, SemanticDigest: digest})
	if err != nil {
		t.Fatal("public command lookup", err)
	}
	return out
}

func titleEventCount(t *testing.T, x *ownerTreeFixture, p id.ProjectID) int {
	t.Helper()
	var n int
	if err := x.raw.QueryRow(knowledgeContext(t), `SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1`, p.String()).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func titleCurrent(t *testing.T, x *ownerTreeFixture, actor id.Actor, p id.ProjectID, d kc.DocumentID) kc.DocumentRef {
	t.Helper()
	h, err := x.service.GetDocument(knowledgeContext(t), actor, p, d)
	if err != nil || h.Active == nil {
		t.Fatal("current document", err)
	}
	return *h.Active
}

// This hook performs a real second command or revocation after the real Outbox
// plan is returned, before the Knowledge final transaction starts. It grants
// nothing, changes no plan, and does not bypass either current gate.
type titleAfterPrepare struct {
	ob.Appender
	after func()
	calls int
}

func (h *titleAfterPrepare) PrepareAppend(ctx context.Context, actor id.Actor, e ev.Event) (ob.AppendPlan, error) {
	p, err := h.Appender.PrepareAppend(ctx, actor, e)
	if err == nil {
		h.calls++
		h.after()
	}
	return p, err
}

type titleFailActivity struct {
	knowledge.ActivityAuthority
	err   error
	calls int
}

func (a *titleFailActivity) TouchActivityInTx(ctx context.Context, tx f.Tx, actor id.Actor) error {
	if err := a.ActivityAuthority.TouchActivityInTx(ctx, tx, actor); err != nil {
		return err
	}
	a.calls++
	return a.err
}

// Only public title-only content is exercised here. Account, Project,
// Knowledge producer and Outbox use the same real Store. No Object I/O,
// prepared source, Audit append or dispatcher is replaced with a success stub;
// unused external ports from the tree fixture still panic if called.
func TestKnowledgeB02TitleContent(t *testing.T) {
	x := newOwnerTreeFixture(t)
	t.Run("change_replay_noop_archive", func(t *testing.T) {
		s := titleService(t, x, nil)
		actor := x.human(t)
		p := x.project(t, actor, true)
		parent := x.document(t, actor, p, nil, "parent")
		before := x.document(t, actor, p, &parent.ID, "before")
		activity := x.activity(t, actor)
		var oldUpload string
		if err := x.raw.QueryRow(knowledgeContext(t), `SELECT current_upload_id::text FROM agenteam_knowledge.documents WHERE id=$1`, before.ID.String()).Scan(&oldUpload); err != nil {
			t.Fatal(err)
		}
		title := "new title"
		meta, req := titleMeta(t, 1), kc.UpdateRequest{Title: &title}
		out, err := s.UpdateDocument(knowledgeContext(t), actor, meta, p, before.ID, req, nil)
		if err != nil {
			t.Fatal("public title update", err)
		}
		if out.Title != title || out.ContentVersion != 2 || out.ObjectID != before.ObjectID || out.SourceKind != before.SourceKind || out.MediaType != before.MediaType || out.CreatedBy.Details() != before.CreatedBy.Details() || out.CreatedAt != before.CreatedAt || out.ParentDocumentID == nil || *out.ParentDocumentID != parent.ID || out.IndexingStatus != kc.IndexPending {
			t.Fatal("title update rewrote unrelated document facts")
		}
		titleSameDocument(t, out, titleCurrent(t, x, actor, p, before.ID))
		if !x.activity(t, actor).After(activity) {
			t.Fatal("confirmed title update did not touch real Session")
		}
		var upload, producer, kind, aggregate string
		var version, sequence int64
		var header, payload []byte
		if err = x.raw.QueryRow(knowledgeContext(t), `SELECT current_upload_id::text FROM agenteam_knowledge.documents WHERE id=$1`, before.ID.String()).Scan(&upload); err != nil || upload != oldUpload {
			t.Fatal("title changed upload pointer", err)
		}
		if titleEventCount(t, x, p) != 1 {
			t.Fatal("expected one actual Outbox fact")
		}
		if err = x.raw.QueryRow(knowledgeContext(t), `SELECT producer,event_type,aggregate_id::text,aggregate_version,sequence,header,payload FROM agenteam_outbox.events WHERE project_id=$1`, p.String()).Scan(&producer, &kind, &aggregate, &version, &sequence, &header, &payload); err != nil {
			t.Fatal(err)
		}
		h, err := ev.DecodeHeader(header)
		if err != nil || producer != "knowledge" || kind != "knowledge.content_changed" || aggregate != before.ID.String() || version != 2 || sequence < 1 || h.Scope.ProjectID.String() != p.String() {
			t.Fatal("invalid persisted event envelope", err)
		}
		var change kc.ContentChangedPayload
		if err = json.Unmarshal(payload, &change); err != nil || change.DocumentID != before.ID || change.ContentVersion != 2 || change.ObjectID != before.ObjectID || len(change.Changes) != 1 || change.Changes[0] != kc.TitleChanged {
			t.Fatal("invalid title event payload", err)
		}
		lookup := titleLookup(t, s, actor, meta, p, before.ID, req)
		if lookup.State != kc.Committed || lookup.Receipt == nil || !lookup.Receipt.Changed || lookup.Receipt.Document == nil {
			t.Fatal("missing confirmed mutation receipt")
		}
		titleSameDocument(t, out, *lookup.Receipt.Document)
		activity = x.activity(t, actor)
		noMeta := titleMeta(t, 2)
		noop, err := s.UpdateDocument(knowledgeContext(t), actor, noMeta, p, before.ID, req, nil)
		if err != nil {
			t.Fatal("same title", err)
		}
		titleSameDocument(t, out, noop)
		noLookup := titleLookup(t, s, actor, noMeta, p, before.ID, req)
		if noLookup.State != kc.Committed || noLookup.Receipt == nil || noLookup.Receipt.Changed {
			t.Fatal("no-op receipt did not distinguish unchanged")
		}
		x.archive(t, p)
		replay, err := s.UpdateDocument(knowledgeContext(t), actor, meta, p, before.ID, req, nil)
		if err != nil {
			t.Fatal("archived completed replay", err)
		}
		titleSameDocument(t, out, replay)
		other := "must reject"
		_, err = s.UpdateDocument(knowledgeContext(t), actor, titleMeta(t, 2), p, before.ID, kc.UpdateRequest{Title: &other}, nil)
		treeCode(t, err, f.ProjectNotActive)
		if titleEventCount(t, x, p) != 1 || !x.activity(t, actor).Equal(activity) {
			t.Fatal("no-op/replay/rejection produced new fact or Activity")
		}
	})
	t.Run("final_activity_rollback", func(t *testing.T) {
		actor := x.human(t)
		p := x.project(t, actor, true)
		before := x.document(t, actor, p, nil, "rollback")
		at := x.activity(t, actor)
		injected := errors.New("title final activity rollback")
		var activity *titleFailActivity
		s := titleService(t, x, func(d *knowledge.Dependencies) {
			activity = &titleFailActivity{ActivityAuthority: d.Activity, err: injected}
			d.Activity = activity
		})
		title := "changed after retry"
		meta, req := titleMeta(t, 1), kc.UpdateRequest{Title: &title}
		_, err := s.UpdateDocument(knowledgeContext(t), actor, meta, p, before.ID, req, nil)
		if !errors.Is(err, injected) || activity.calls != 1 {
			t.Fatal("did not reach real final Activity before injected rollback", err)
		}
		titleSameDocument(t, before, titleCurrent(t, x, actor, p, before.ID))
		if titleEventCount(t, x, p) != 0 || !x.activity(t, actor).Equal(at) {
			t.Fatal("final transaction leaked event or Activity")
		}
		pending := titleLookup(t, s, actor, meta, p, before.ID, req)
		if pending.State != kc.InProgress || pending.Receipt != nil {
			t.Fatal("rolled-back final has canonical receipt")
		}
		var fixed string
		if err = x.raw.QueryRow(knowledgeContext(t), `SELECT id::text FROM agenteam_knowledge.command_events WHERE project_id=$1`, p.String()).Scan(&fixed); err != nil {
			t.Fatal("original durable plan absent", err)
		}
		activity.err = nil
		out, err := s.UpdateDocument(knowledgeContext(t), actor, meta, p, before.ID, req, nil)
		if err != nil || out.Title != title || out.ContentVersion != 2 || activity.calls != 2 {
			t.Fatal("same command final retry", err)
		}
		var actual string
		if err = x.raw.QueryRow(knowledgeContext(t), `SELECT id::text FROM agenteam_outbox.events WHERE project_id=$1`, p.String()).Scan(&actual); err != nil || actual != fixed || titleEventCount(t, x, p) != 1 {
			t.Fatal("retry did not preserve original event identity", err)
		}
		if !x.activity(t, actor).After(at) {
			t.Fatal("confirmed retry did not touch Activity")
		}
	})
	t.Run("move_between_plan_and_final", func(t *testing.T) {
		actor := x.human(t)
		p := x.project(t, actor, true)
		a, b := x.document(t, actor, p, nil, "a"), x.document(t, actor, p, nil, "b")
		doc := x.document(t, actor, p, &a.ID, "child")
		var hook *titleAfterPrepare
		s := titleService(t, x, func(d *knowledge.Dependencies) {
			hook = &titleAfterPrepare{Appender: d.Outbox, after: func() {
				moved, err := x.service.MoveDocument(knowledgeContext(t), actor, treeMeta(t), p, doc.ID, kc.MoveRequest{ExpectedParentID: &a.ID, TargetParentID: &b.ID})
				if err != nil || !moved.Changed || moved.Document.ParentDocumentID == nil || *moved.Document.ParentDocumentID != b.ID {
					t.Fatal("actual interleaved Move", err)
				}
			}}
			d.Outbox = hook
		})
		title := "moved title"
		out, err := s.UpdateDocument(knowledgeContext(t), actor, titleMeta(t, 1), p, doc.ID, kc.UpdateRequest{Title: &title}, nil)
		if err != nil || hook.calls != 1 || out.ParentDocumentID == nil || *out.ParentDocumentID != b.ID || out.Title != title || out.ContentVersion != 2 || out.ObjectID != doc.ObjectID {
			t.Fatal("title overwrote newer parent", err)
		}
		titleSameDocument(t, out, titleCurrent(t, x, actor, p, doc.ID))
		if titleEventCount(t, x, p) != 1 {
			t.Fatal("interleaved Move changed content event count")
		}
	})
	t.Run("current_gate_and_version", func(t *testing.T) {
		actor, stranger := x.human(t), x.human(t)
		p := x.project(t, actor, true)
		doc := x.document(t, actor, p, nil, "protected")
		s := titleService(t, x, nil)
		title := "not accepted"
		req := kc.UpdateRequest{Title: &title}
		_, err := s.UpdateDocument(knowledgeContext(t), stranger, titleMeta(t, 1), p, doc.ID, req, nil)
		treeCode(t, err, f.NotFound)
		_, err = s.UpdateDocument(knowledgeContext(t), actor, titleMeta(t, 2), p, doc.ID, req, nil)
		treeCode(t, err, f.VersionConflict)
		if n, e := x.count(t, p); n != 0 || e != 0 {
			t.Fatal("initial gate rejection left a command/event plan", n, e)
		}
		at := x.activity(t, actor)
		var hook *titleAfterPrepare
		s = titleService(t, x, func(d *knowledge.Dependencies) {
			hook = &titleAfterPrepare{Appender: d.Outbox, after: func() {
				x.exec(t, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, actor.Details().SessionID)
			}}
			d.Outbox = hook
		})
		_, err = s.UpdateDocument(knowledgeContext(t), actor, titleMeta(t, 1), p, doc.ID, req, nil)
		treeCode(t, err, f.SessionRevoked)
		if hook.calls != 1 || titleEventCount(t, x, p) != 0 || !x.activity(t, actor).Equal(at) {
			t.Fatal("revoked final gate left event/Activity or was not reached")
		}
		user, err := f.ParseID[id.User](actor.Details().UserID)
		if err != nil {
			t.Fatal(err)
		}
		current := x.session(t, user)
		titleSameDocument(t, doc, titleCurrent(t, x, current, p, doc.ID))
		var committed int
		if err = x.raw.QueryRow(knowledgeContext(t), `SELECT count(*) FROM agenteam_knowledge.commands WHERE project_id=$1 AND state='completed'`, p.String()).Scan(&committed); err != nil || committed != 0 {
			t.Fatal("revoked final left committed receipt", err)
		}
	})
}
