//go:build integration

package knowledge_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ev "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// These nil embedded interfaces deliberately panic if a metadata-only path
// starts doing external work. They provide no authorization, storage success,
// event receipt, or join proof. This top does not test Object/publication.
type treeObjects struct{ oc.Objects }
type treeUploads struct{ oc.Uploads }
type treeSources struct{ oc.SourceResolver }
type treeReads struct{ oc.SourceReads }
type treeReferences struct{ oc.ReferenceCleanup }
type treeCleaner struct{ oc.Cleaner }
type treeAudit struct{ ac.Appender }
type treeOutbox struct{ ob.Appender }
type treeProcess struct{ process ob.ProcessID }

func (p treeProcess) CurrentProcess() ob.ProcessID { return p.process }
func (treeProcess) ConfirmStopped(context.Context, ob.ProcessID) error {
	return f.NewFault(f.DependencyUnbound, f.NotStarted)
}

type ownerTreeFixture struct {
	raw     *postgres.Store
	service *knowledge.Service
}

func treeID[T any](t *testing.T) f.ID[T] {
	t.Helper()
	v, e := f.NewID[T]()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func treeCode(t *testing.T, err error, code f.Code) {
	t.Helper()
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != code {
		t.Fatalf("expected %s; got %v", code, err)
	}
}
func treeMeta(t *testing.T) f.CommandMeta {
	return f.CommandMeta{RequestID: treeID[f.Request](t), IdempotencyKey: f.IdempotencyKey(knowledgeID(t))}
}
func newOwnerTreeFixture(t *testing.T) *ownerTreeFixture {
	t.Helper()
	db := pgfixture.NewDatabase(t)
	knowledgeMigrate(t, db, knowledgeMigrationSource(t, knowledgeMigrationFiles(t, "00025")))
	raw, e := postgres.Open(knowledgeContext(t), db.Config(t, nil))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if e := raw.ForceClose(ctx); e != nil {
			t.Error(e)
		}
	})
	key := func(k string, b byte) string {
		return fmt.Sprintf(`{"format":1,"current_kid":%q,"keys":[{"kid":%q,"key_b64":%q}]}`, k, k, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)))
	}
	cursors, e := cursor.LoadKeyring(key("c", 1))
	if e != nil {
		t.Fatal(e)
	}
	secrets, e := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))), cursors)
	if e != nil {
		t.Fatal(e)
	}
	downloads, e := object.LoadDownloadKeyring(key("d", 3), cursors, secrets)
	if e != nil {
		t.Fatal(e)
	}
	keys, e := account.LoadKeyring(key("a", 4), cursors, secrets, downloads)
	if e != nil {
		t.Fatal(e)
	}
	accounts, e := account.NewAuthority(raw, keys)
	if e != nil {
		t.Fatal(e)
	}
	if e = accounts.Initialize(knowledgeContext(t)); e != nil {
		t.Fatal(e)
	}
	projects, e := project.NewAuthority(raw, project.AuthorityDependencies{Sessions: accounts, Routes: accounts})
	if e != nil {
		t.Fatal(e)
	}
	confirmations, e := kc.LoadConfirmationKeys(key("k", 5))
	if e != nil {
		t.Fatal(e)
	}
	events, e := kc.RegisterKnowledgeEvents(ev.NewCatalog())
	if e != nil {
		t.Fatal(e)
	}
	service, e := knowledge.New(raw, knowledge.Dependencies{Projects: projects, Activity: accounts, Objects: treeObjects{}, Uploads: treeUploads{}, Sources: treeSources{}, SourceReads: treeReads{}, ReferenceCleanup: treeReferences{}, ObjectCleanup: treeCleaner{}, Audit: treeAudit{}, Outbox: treeOutbox{}, Events: events, Processes: treeProcess{treeID[ob.Process](t)}, Cursors: cursors, Confirmations: confirmations})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		service.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if e := service.Drain(ctx); e != nil {
			t.Error(e)
		}
	})
	return &ownerTreeFixture{raw: raw, service: service}
}
func (x *ownerTreeFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, e := x.raw.Exec(knowledgeContext(t), sql, args...); e != nil {
		t.Fatal(e)
	}
}
func (x *ownerTreeFixture) session(t *testing.T, user id.UserID) id.Actor {
	t.Helper()
	session := treeID[id.Session](t)
	verifier := sha256.Sum256([]byte(session.String()))
	x.exec(t, `INSERT INTO agenteam_account.sessions(id,user_id,token_verifier,csrf_kid,issued_at,last_activity_at,idle_seconds,absolute_expires_at) VALUES($1,$2,$3,'a',clock_timestamp()-interval '2 minutes',clock_timestamp()-interval '2 minutes',3600,clock_timestamp()+interval '1 hour')`, session.String(), user.String(), verifier[:])
	actor, e := id.NewHuman(user, session)
	if e != nil {
		t.Fatal(e)
	}
	return actor
}
func (x *ownerTreeFixture) human(t *testing.T) id.Actor {
	t.Helper()
	user := treeID[id.User](t)
	name := strings.ReplaceAll(user.String(), "-", "")
	x.exec(t, `INSERT INTO agenteam_account.users(id,email,username,display_name,role,password_phc,password_version,version,auth_sequence,initial_password_suggestion,theme) VALUES($1,$2,$3,'fixture','user','fixture-not-login',1,1,1,false,'system')`, user.String(), name+"@example.test", name)
	return x.session(t, user)
}

// Seed only task-owned upstream facts. Account/Project authorization still
// executes its real SQL and lock checks; no claim is made about creation APIs.
func (x *ownerTreeFixture) project(t *testing.T, actor id.Actor, initialized bool) id.ProjectID {
	t.Helper()
	project, creation := treeID[id.Project](t), knowledgeID(t)
	cause, e := f.NewRecoveryCause("knowledge.fixture", creation, "")
	if e != nil {
		t.Fatal(e)
	}
	result := x.raw.WithinTx(knowledgeContext(t), cause, func(ctx context.Context, tx f.Tx) error {
		q, e := x.raw.InTx(tx)
		if e != nil {
			return e
		}
		_, e = q.Exec(ctx, `INSERT INTO agenteam_project.projects(id,owner_user_id,name,normalized_name,description,lifecycle,version,created_at,updated_at,creation_id,initialized_at) VALUES($1,$2,$3,$3,'','active',1,clock_timestamp(),clock_timestamp(),$4,CASE WHEN $5::boolean THEN clock_timestamp() END)`, project.String(), actor.Details().UserID, "p"+project.String(), creation, initialized)
		if e != nil {
			return e
		}
		if !initialized {
			_, e = q.Exec(ctx, `INSERT INTO agenteam_project.creations(id,project_id,owner_user_id,command_key,semantic_digest,request_name,request_description,state,initialization_key,version,created_at,updated_at,event_id) VALUES($1,$2,$3,$6,'sha256:'||repeat('1',64),$4,'','accepted',$6,1,clock_timestamp(),clock_timestamp(),$5)`, creation, project.String(), actor.Details().UserID, "p"+project.String(), knowledgeID(t), creation)
			return e
		}
		_, e = q.Exec(ctx, `INSERT INTO agenteam_project.creations(id,project_id,owner_user_id,command_key,semantic_digest,state,initialization_key,protected_skill_id,protected_revision,version,created_at,updated_at,event_id,event_header,event_payload,safe_result) VALUES($1,$2,$3,$6,'sha256:'||repeat('1',64),'completed',$6,$4,1,1,clock_timestamp(),clock_timestamp(),$5,'{}','{}','{}')`, creation, project.String(), actor.Details().UserID, knowledgeID(t), knowledgeID(t), creation)
		return e
	})
	if result.State() != f.Committed {
		t.Fatal("upstream fixture transaction", result.Fault())
	}
	return project
}
func (x *ownerTreeFixture) document(t *testing.T, actor id.Actor, project id.ProjectID, parent *kc.DocumentID, title string) kc.DocumentRef {
	t.Helper()
	doc := treeID[kc.Document](t)
	var p any
	if parent != nil {
		p = parent.String()
	}
	x.exec(t, `INSERT INTO agenteam_knowledge.documents(id,project_id,parent_document_id,title,content_version,source_kind,media_type,current_object_id,current_upload_id,status,indexing_status,creator_user_id,created_at,updated_at) VALUES($1,$2,$3,$4,1,'text','text/plain',$5,$6,'active','pending',$7,clock_timestamp(),clock_timestamp())`, doc.String(), project.String(), p, title, knowledgeID(t), knowledgeID(t), actor.Details().UserID)
	head, e := x.service.GetDocument(knowledgeContext(t), actor, project, doc)
	if e != nil || head.Active == nil {
		t.Fatal("seeded metadata read", e)
	}
	return *head.Active
}
func (x *ownerTreeFixture) activity(t *testing.T, actor id.Actor) time.Time {
	t.Helper()
	var at time.Time
	if e := x.raw.QueryRow(knowledgeContext(t), `SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1`, actor.Details().SessionID).Scan(&at); e != nil {
		t.Fatal(e)
	}
	return at
}
func (x *ownerTreeFixture) count(t *testing.T, project id.ProjectID) (int, int) {
	t.Helper()
	var commands, events int
	if e := x.raw.QueryRow(knowledgeContext(t), `SELECT (SELECT count(*) FROM agenteam_knowledge.commands WHERE project_id=$1),(SELECT count(*) FROM agenteam_knowledge.command_events WHERE project_id=$1)`, project.String()).Scan(&commands, &events); e != nil {
		t.Fatal(e)
	}
	return commands, events
}
func (x *ownerTreeFixture) archive(t *testing.T, project id.ProjectID) {
	x.exec(t, `UPDATE agenteam_project.projects SET lifecycle='archived',archived_at=clock_timestamp() WHERE id=$1`, project.String())
}

func TestKnowledgeB02OwnerTree(t *testing.T) {
	x := newOwnerTreeFixture(t)
	t.Run("current_owner", func(t *testing.T) {
		owner, stranger := x.human(t), x.human(t)
		p := x.project(t, owner, true)
		doc := x.document(t, owner, p, nil, "visible")
		if _, e := x.service.GetDocument(knowledgeContext(t), stranger, p, doc.ID); e == nil {
			t.Fatal("foreign Owner admitted")
		} else {
			treeCode(t, e, f.NotFound)
		}
		foreign := x.project(t, owner, true)
		if _, e := x.service.GetDocument(knowledgeContext(t), owner, foreign, doc.ID); e == nil {
			t.Fatal("foreign document admitted")
		} else {
			treeCode(t, e, f.NotFound)
		}
		user, _ := f.ParseID[id.User](owner.Details().UserID)
		revoked := x.session(t, user)
		x.exec(t, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, revoked.Details().SessionID)
		_, e := x.service.GetDocument(knowledgeContext(t), revoked, p, doc.ID)
		treeCode(t, e, f.SessionRevoked)
		pending := x.project(t, owner, false)
		_, e = x.service.GetDocument(knowledgeContext(t), owner, pending, doc.ID)
		treeCode(t, e, f.ProjectNotActive)
		x.archive(t, p)
		if _, e = x.service.GetDocument(knowledgeContext(t), owner, p, doc.ID); e != nil {
			t.Fatal("archived current read", e)
		}
		_, e = x.service.MoveDocument(knowledgeContext(t), owner, treeMeta(t), p, doc.ID, kc.MoveRequest{})
		treeCode(t, e, f.ProjectNotActive)
		if a, b := x.count(t, p); a != 0 || b != 0 {
			t.Fatal("rejected mutation left a fact", a, b)
		}
	})
	t.Run("move_replay_and_cycle", func(t *testing.T) {
		owner := x.human(t)
		p := x.project(t, owner, true)
		a, b := x.document(t, owner, p, nil, "a"), x.document(t, owner, p, nil, "b")
		child := x.document(t, owner, p, &a.ID, "child")
		meta := treeMeta(t)
		request := kc.MoveRequest{ExpectedParentID: &a.ID, TargetParentID: &b.ID}
		before := x.activity(t, owner)
		out, e := x.service.MoveDocument(knowledgeContext(t), owner, meta, p, child.ID, request)
		if e != nil || !out.Changed || out.Document.ParentDocumentID == nil || *out.Document.ParentDocumentID != b.ID || out.Document.ContentVersion != child.ContentVersion || out.Document.ObjectID != child.ObjectID {
			t.Fatal("Move current metadata", e)
		}
		if !x.activity(t, owner).After(before) {
			t.Fatal("changed Move did not touch actual Session")
		}
		ancestors, e := x.service.ReadAncestors(knowledgeContext(t), owner, p, child.ID)
		if e != nil || len(ancestors) != 1 || ancestors[0].ID != b.ID {
			t.Fatal("current ancestor path", e)
		}
		user, _ := f.ParseID[id.User](owner.Details().UserID)
		renewed := x.session(t, user)
		untouched := x.activity(t, renewed)
		replay, e := x.service.MoveDocument(knowledgeContext(t), renewed, meta, p, child.ID, request)
		if e != nil || !sameMoveReceipt(t, out, replay) || !x.activity(t, renewed).Equal(untouched) {
			t.Fatal("stable user replay changed receipt or touched Activity", e)
		}
		noop, e := x.service.MoveDocument(knowledgeContext(t), renewed, treeMeta(t), p, child.ID, kc.MoveRequest{ExpectedParentID: &b.ID, TargetParentID: &b.ID})
		if e != nil || noop.Changed || !x.activity(t, renewed).Equal(untouched) {
			t.Fatal("Move no-op generated activity", e)
		}
		_, e = x.service.MoveDocument(knowledgeContext(t), renewed, treeMeta(t), p, child.ID, request)
		treeCode(t, e, f.VersionConflict)
		_, e = x.service.MoveDocument(knowledgeContext(t), renewed, treeMeta(t), p, b.ID, kc.MoveRequest{TargetParentID: &child.ID})
		treeCode(t, e, f.InvalidArgument)
		wrong := request
		wrong.TargetParentID = nil
		_, e = x.service.MoveDocument(knowledgeContext(t), renewed, meta, p, child.ID, wrong)
		treeCode(t, e, f.IdempotencyKeyReused)
		digest, e := kc.MoveDigest(owner, meta, p, child.ID, request)
		if e != nil {
			t.Fatal(e)
		}
		x.archive(t, p)
		lookup, e := x.service.LookupCommand(knowledgeContext(t), renewed, kc.LookupRequest{ProjectID: p, Command: kc.Move, Key: meta.IdempotencyKey, SemanticDigest: digest})
		if e != nil || lookup.State != kc.Committed || lookup.Receipt == nil || lookup.Receipt.Document.ID != child.ID {
			t.Fatal("archived safe lookup", e)
		}
		replay, e = x.service.MoveDocument(knowledgeContext(t), renewed, meta, p, child.ID, request)
		if e != nil || !sameMoveReceipt(t, replay, out) {
			t.Fatal("archived completed replay gated as new work", e)
		}
		x.exec(t, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, renewed.Details().SessionID)
		_, e = x.service.LookupCommand(knowledgeContext(t), renewed, kc.LookupRequest{ProjectID: p, Command: kc.Move, Key: meta.IdempotencyKey, SemanticDigest: digest})
		treeCode(t, e, f.SessionRevoked)
		if a, b := x.count(t, p); a != 2 || b != 0 {
			t.Fatal("Move receipt/no-op/event cardinality", a, b)
		}
	})
	t.Run("title_pagination_and_paths", func(t *testing.T) {
		owner := x.human(t)
		p := x.project(t, owner, true)
		titles := []string{"é", "e\u0301", "same", "same", "100%_literal", "100ZZliteral", "文"}
		expected := make([]kc.DocumentRef, 0, len(titles))
		for _, title := range titles {
			expected = append(expected, x.document(t, owner, p, nil, title))
		}
		sort.Slice(expected, func(i, j int) bool {
			if expected[i].Title == expected[j].Title {
				return expected[i].ID.String() < expected[j].ID.String()
			}
			return expected[i].Title < expected[j].Title
		})
		var got []kc.DocumentRef
		token := ""
		firstToken := ""
		for i := 0; i < len(titles); i++ {
			page, e := x.service.ListChildren(knowledgeContext(t), owner, p, nil, kc.ListFilter{}, f.PageRequest{Limit: 2, Cursor: token})
			if e != nil {
				t.Fatal(e)
			}
			got = append(got, page.Items...)
			token = page.NextCursor
			if firstToken == "" {
				firstToken = token
			}
			if token == "" {
				break
			}
		}
		if len(got) != len(expected) {
			t.Fatal("pagination lost rows", len(got))
		}
		for i := range got {
			if got[i].ID != expected[i].ID {
				t.Fatal("C/UUID ordering drift", i)
			}
		}
		_, e := x.service.ListDocuments(knowledgeContext(t), owner, p, kc.ListFilter{}, f.PageRequest{Limit: 2, Cursor: firstToken})
		treeCode(t, e, f.CursorInvalid)
		filtered, e := x.service.ListDocuments(knowledgeContext(t), owner, p, kc.ListFilter{TitleQuery: "%_"}, f.PageRequest{Limit: 20})
		if e != nil || len(filtered.Items) != 1 || filtered.Items[0].Title != "100%_literal" {
			t.Fatal("literal title search", e)
		}
		parent := expected[0]
		child := x.document(t, owner, p, &parent.ID, "nested_%")
		hits, e := x.service.SearchTitles(knowledgeContext(t), owner, p, "nested_%", f.PageRequest{Limit: 20})
		if e != nil || len(hits.Items) != 1 || hits.Items[0].Document.ID != child.ID || len(hits.Items[0].Ancestors) != 1 || hits.Items[0].Ancestors[0].ID != parent.ID {
			t.Fatal("search exact current path", e)
		}
		preview, e := x.service.PrepareDeleteSubtree(knowledgeContext(t), owner, p, parent.ID)
		if e != nil || len(preview.Nodes) != 2 || preview.Confirmation.Validate() != nil {
			t.Fatal("complete subtree preview", e)
		}
		if a, b := x.count(t, p); a != 0 || b != 0 {
			t.Fatal("read paths created command facts", a, b)
		}
	})
	t.Run("caller_tx_rollback_and_ended", func(t *testing.T) {
		owner := x.human(t)
		p := x.project(t, owner, true)
		a, b := x.document(t, owner, p, nil, "a"), x.document(t, owner, p, nil, "b")
		child := x.document(t, owner, p, &a.ID, "child")
		meta := treeMeta(t)
		request := kc.MoveRequest{ExpectedParentID: &a.ID, TargetParentID: &b.ID}
		mutation, e := kc.NewMoveMutation(owner, meta, p, child.ID, request)
		if e != nil {
			t.Fatal(e)
		}
		plan, e := x.service.DiscoverMutation(knowledgeContext(t), mutation)
		if e != nil {
			t.Fatal(e)
		}
		identity, e := kc.CommandIdentity(p, kc.Move, meta.IdempotencyKey)
		if e != nil {
			t.Fatal(e)
		}
		cause, e := f.NewCommandsCause(identity)
		if e != nil {
			t.Fatal(e)
		}
		before := x.activity(t, owner)
		var ended f.Tx
		var entered bool
		abort := f.NewFault(f.ResourceBusy, f.NotStarted)
		result := x.raw.WithinTx(knowledgeContext(t), cause, func(ctx context.Context, tx f.Tx) error {
			ended = tx
			locked, e := x.service.AcquireMutationInTx(ctx, tx, plan, nil)
			if e != nil {
				return e
			}
			out, e := x.service.MoveDocumentInTx(ctx, tx, owner, meta, p, child.ID, request, locked)
			if e != nil {
				return e
			}
			if !out.Changed {
				return errors.New("planned Move did not change")
			}
			fact, e := x.service.ReadCurrentInTx(ctx, tx, owner, p, child.ID)
			if e != nil {
				return e
			}
			if fact.ContentVersion != child.ContentVersion {
				return errors.New("Move advanced content version")
			}
			entered = true
			return abort
		})
		if !entered || result.State() != f.NotCommitted || result.Fault().Code != f.ResourceBusy {
			t.Fatal("injected caller rollback not reached", result.Fault())
		}
		current, e := x.service.GetDocument(knowledgeContext(t), owner, p, child.ID)
		if e != nil || *current.Active.ParentDocumentID != a.ID || !x.activity(t, owner).Equal(before) {
			t.Fatal("caller rollback leaked tree/activity", e)
		}
		if a, b := x.count(t, p); a != 0 || b != 0 {
			t.Fatal("caller rollback leaked receipt/event", a, b)
		}
		if _, e = x.service.ReadCurrentInTx(knowledgeContext(t), ended, owner, p, child.ID); e == nil {
			t.Fatal("ended transaction reused")
		}
		out, e := x.service.MoveDocument(knowledgeContext(t), owner, meta, p, child.ID, request)
		if e != nil || !out.Changed {
			t.Fatal("rolled-back original key could not commit", e)
		}
	})
}

func sameMoveReceipt(t *testing.T, a, b kc.MoveResult) bool {
	t.Helper()
	x, e := json.Marshal(a)
	if e != nil {
		t.Fatal(e)
	}
	y, e := json.Marshal(b)
	if e != nil {
		t.Fatal(e)
	}
	return bytes.Equal(x, y)
}
