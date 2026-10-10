package commandhttp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func testID[T any](n int) f.ID[T] {
	v, e := f.ParseID[T](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if e != nil {
		panic(e)
	}
	return v
}
func must[T any](v T, e error) T {
	if e != nil {
		panic(e)
	}
	return v
}
func testAt() f.Instant   { return must(f.ParseInstant("2026-10-09T01:02:03.123456Z")) }
func testActor() id.Actor { return must(id.NewHuman(testID[id.User](1), testID[id.Session](2))) }
func testDocument() kc.DocumentRef {
	return kc.DocumentRef{ID: testID[kc.Document](3), ProjectID: testID[id.Project](4), Title: "original", ContentVersion: 1, SourceKind: kc.Text, MediaType: kc.Markdown, ObjectID: testID[oc.StoredObject](5), Status: kc.Active, IndexingStatus: kc.IndexPending, CreatedBy: must(kc.NewCreatorRef(kc.CreatorDetails{Kind: id.Human, UserID: testID[id.User](1)})), CreatedAt: testAt(), UpdatedAt: testAt()}
}
func testPreview() kc.DeletePreview {
	doc := testDocument()
	digest := must(kc.SubtreeDigest(doc.ProjectID, doc.ID, []kc.ScopeNode{{ID: doc.ID, ProjectID: doc.ProjectID, ContentVersion: 1, Status: kc.Active}}))
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))
	keys := must(kc.LoadConfirmationKeys(fmt.Sprintf(`{"format":1,"current_kid":"unit","keys":[{"kid":"unit","key_b64":"%s"}]}`, key)))
	token := must(keys.Sign(kc.DeleteConfirmationClaims{UserID: testID[id.User](1), ProjectID: doc.ProjectID, RootID: doc.ID, ScopeDigest: digest, ExpiresAt: testAt()}))
	return kc.DeletePreview{Root: doc.ID, Nodes: []kc.DocumentRef{doc}, ScopeDigest: digest, Confirmation: token, ExpiresAt: testAt()}
}

type testBoundary struct {
	check         func(*http.Request) error
	actor         id.Actor
	checks, auths atomic.Int32
}

func (b *testBoundary) CheckRequest(_ http.ResponseWriter, r *http.Request) error {
	b.checks.Add(1)
	if b.check != nil {
		return b.check(r)
	}
	return nil
}
func (b *testBoundary) RequireHuman(*http.Request) (id.Actor, error) {
	b.auths.Add(1)
	return b.actor, nil
}
func (*testBoundary) WriteProblem(w http.ResponseWriter, r *http.Request, e error) {
	(&account.HTTPBoundary{}).WriteProblem(w, r, e)
}

type testService struct {
	calls     int
	err       error
	before    func(context.Context)
	doc       kc.DocumentRef
	move      kc.MoveResult
	preview   kc.DeletePreview
	deleted   kc.DeleteResult
	lookup    kc.CommandLookup
	gotMeta   f.CommandMeta
	gotLookup kc.LookupRequest
	gotTarget kc.DocumentID
	gotUpdate kc.UpdateRequest
	gotSource *kc.SourceInput
	gotMove   kc.MoveRequest
	gotToken  kc.ConfirmationToken
}

func (s *testService) enter(ctx context.Context) {
	s.calls++
	if s.before != nil {
		s.before(ctx)
	}
}
func (s *testService) UpdateDocument(ctx context.Context, _ id.Actor, m f.CommandMeta, _ id.ProjectID, d kc.DocumentID, r kc.UpdateRequest, src *kc.SourceInput) (kc.DocumentRef, error) {
	s.enter(ctx)
	s.gotMeta = m
	s.gotTarget = d
	s.gotUpdate = r
	s.gotSource = src
	return s.doc, s.err
}
func (s *testService) MoveDocument(ctx context.Context, _ id.Actor, m f.CommandMeta, _ id.ProjectID, d kc.DocumentID, r kc.MoveRequest) (kc.MoveResult, error) {
	s.enter(ctx)
	s.gotMeta = m
	s.gotTarget = d
	s.gotMove = r
	return s.move, s.err
}
func (s *testService) PrepareDeleteSubtree(ctx context.Context, _ id.Actor, _ id.ProjectID, d kc.DocumentID) (kc.DeletePreview, error) {
	s.enter(ctx)
	s.gotTarget = d
	return s.preview, s.err
}
func (s *testService) DeleteSubtree(ctx context.Context, _ id.Actor, m f.CommandMeta, _ id.ProjectID, d kc.DocumentID, r kc.ConfirmationToken) (kc.DeleteResult, error) {
	s.enter(ctx)
	s.gotMeta = m
	s.gotTarget = d
	s.gotToken = r
	return s.deleted, s.err
}
func (s *testService) LookupCommand(ctx context.Context, _ id.Actor, r kc.LookupRequest) (kc.CommandLookup, error) {
	s.enter(ctx)
	s.gotLookup = r
	return s.lookup, s.err
}
func fixture() (*handler, *testService, *testBoundary) {
	doc := testDocument()
	s := &testService{doc: doc, move: kc.MoveResult{Document: doc}, preview: testPreview(), deleted: kc.DeleteResult{Root: doc.ID, DeletedIDs: []kc.DocumentID{doc.ID}, CleanupPending: true}, lookup: kc.CommandLookup{State: kc.NotObserved}}
	b := &testBoundary{actor: testActor()}
	return &handler{s, b}, s, b
}
func testPath(action string) string {
	p := projectPrefix + testID[id.Project](4).String() + "/knowledge/documents/"
	if action == "lookup" {
		return p + "commands/lookup"
	}
	return p + testID[kc.Document](3).String() + "/" + action
}
func request(action, body string) *http.Request {
	r := httptest.NewRequest("POST", testPath(action), strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if action != "delete-preview" {
		r.Header.Set("Idempotency-Key", "original-key")
	}
	return r
}

type testWriter struct {
	*httptest.ResponseRecorder
	mu                    sync.Mutex
	readTimes, writeTimes []time.Time
	read, write           func(time.Time) error
	flush                 func() error
	output                func([]byte) (int, error)
}

func writer() *testWriter { return &testWriter{ResponseRecorder: httptest.NewRecorder()} }
func (w *testWriter) SetReadDeadline(t time.Time) error {
	w.mu.Lock()
	w.readTimes = append(w.readTimes, t)
	w.mu.Unlock()
	if w.read != nil {
		return w.read(t)
	}
	return nil
}
func (w *testWriter) SetWriteDeadline(t time.Time) error {
	w.mu.Lock()
	w.writeTimes = append(w.writeTimes, t)
	w.mu.Unlock()
	if w.write != nil {
		return w.write(t)
	}
	return nil
}
func (w *testWriter) Write(b []byte) (int, error) {
	if w.output != nil {
		return w.output(b)
	}
	return w.ResponseRecorder.Write(b)
}
func (w *testWriter) FlushError() error {
	if w.flush != nil {
		return w.flush()
	}
	return nil
}
func (w *testWriter) cleared() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.readTimes) > 0 && len(w.writeTimes) > 0 && w.readTimes[len(w.readTimes)-1].IsZero() && w.writeTimes[len(w.writeTimes)-1].IsZero()
}
func serve(h http.Handler, r *http.Request, w http.ResponseWriter) (aborted bool) {
	defer func() {
		if v := recover(); v != nil {
			if v != http.ErrAbortHandler {
				panic(v)
			}
			aborted = true
		}
	}()
	httpapi.WithRequestID(nil, h).ServeHTTP(w, r)
	return false
}
func requireSuccess(t *testing.T, h http.Handler, r *http.Request) *testWriter {
	t.Helper()
	w := writer()
	if serve(h, r, w) || w.Code != 200 || !w.cleared() {
		t.Fatalf("success code=%d body=%s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Length") != fmt.Sprint(w.Body.Len()) {
		t.Fatal("bounded headers")
	}
	for _, private := range []string{"object_id", testID[oc.StoredObject](5).String(), "upload_id", "original-key", "user-private-canary"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatal("private material in response")
		}
	}
	return w
}

func TestTreeCommandsHTTPDispatchAndSafeProjection(t *testing.T) {
	if h, e := NewHTTPHandler(nil, nil); h != nil || e == nil {
		t.Fatal("unbound constructor")
	}
	t.Run("rename", func(t *testing.T) {
		h, s, _ := fixture()
		s.doc.Title = " exact title "
		s.doc.ContentVersion = 2
		w := requireSuccess(t, h, request("rename", `{"expected_version":"1","title":" exact title "}`))
		var v map[string]map[string]json.RawMessage
		if json.Unmarshal(w.Body.Bytes(), &v) != nil || len(v["document"]) != 12 {
			t.Fatal("document field shape")
		}
		if s.calls != 1 || s.gotMeta.IdempotencyKey != "original-key" || s.gotMeta.ExpectedVersion == nil || *s.gotMeta.ExpectedVersion != 1 || s.gotUpdate.Title == nil || *s.gotUpdate.Title != " exact title " || s.gotUpdate.ReplaceSource || s.gotSource != nil {
			t.Fatal("title-only service contract")
		}
	})
	t.Run("move", func(t *testing.T) {
		h, s, _ := fixture()
		parent := testID[kc.Document](7)
		s.move.Document.ParentDocumentID = &parent
		s.move.Changed = true
		requireSuccess(t, h, request("move", fmt.Sprintf(`{"expected_parent_id":null,"target_parent_id":%q}`, parent.String())))
		if s.gotMeta.ExpectedVersion != nil || s.gotMove.ExpectedParentID != nil || s.gotMove.TargetParentID == nil || *s.gotMove.TargetParentID != parent {
			t.Fatal("move original fields")
		}
	})
	t.Run("preview_and_delete", func(t *testing.T) {
		h, s, _ := fixture()
		w := requireSuccess(t, h, request("delete-preview", `{}`))
		if !strings.Contains(w.Body.String(), s.preview.Confirmation.ForHumanResponse()) {
			t.Fatal("missing explicit confirmation")
		}
		original := s.preview.Confirmation.ForHumanResponse()
		requireSuccess(t, h, request("delete-subtree", fmt.Sprintf(`{"confirmation_token":%q}`, original)))
		if s.gotToken.ForHumanResponse() != original || s.gotMeta.ExpectedVersion != nil {
			t.Fatal("token changed")
		}
	})
}

func TestTreeCommandsHTTPLookupRecomputesOriginalDigest(t *testing.T) {
	doc := testDocument()
	token := testPreview().Confirmation
	for _, name := range []kc.CommandName{kc.Update, kc.Move, kc.DeleteSubtree} {
		t.Run(string(name), func(t *testing.T) {
			h, s, b := fixture()
			original := `{"expected_parent_id":null,"target_parent_id":null}`
			meta := f.CommandMeta{RequestID: testID[f.Request](17), IdempotencyKey: "original-key"}
			var digest f.Digest
			receipt := kc.MutationReceipt{Command: name, Document: &doc}
			switch name {
			case kc.Update:
				expected := f.Version(1)
				meta.ExpectedVersion = &expected
				title := doc.Title
				original = `{"expected_version":"1","title":"original"}`
				digest = must(kc.UpdateDigest(b.actor, meta, doc.ProjectID, doc.ID, kc.UpdateRequest{Title: &title}, nil))
			case kc.Move:
				digest = must(kc.MoveDigest(b.actor, meta, doc.ProjectID, doc.ID, kc.MoveRequest{}))
			case kc.DeleteSubtree:
				original = fmt.Sprintf(`{"confirmation_token":%q}`, token.ForHumanResponse())
				digest = must(kc.DeleteDigest(b.actor, meta, doc.ProjectID, doc.ID, token))
				receipt = kc.MutationReceipt{Command: name, RootID: &doc.ID, Changed: true, DeletedIDs: []kc.DocumentID{doc.ID}, CleanupPending: true}
			}
			for _, state := range []kc.LookupState{kc.NotObserved, kc.InProgress, kc.Committed} {
				s.lookup = kc.CommandLookup{State: state}
				if state == kc.Committed {
					s.lookup.Receipt = &receipt
				}
				w := requireSuccess(t, h, request("lookup", fmt.Sprintf(`{"command":%q,"document_id":%q,"request":%s}`, name, doc.ID.String(), original)))
				if s.gotLookup.SemanticDigest != digest || s.gotLookup.Key != "original-key" || s.gotLookup.Command != name || s.gotLookup.ProjectID != doc.ProjectID {
					t.Fatal("digest or identity replaced")
				}
				var result struct {
					State   string          `json:"state"`
					Receipt json.RawMessage `json:"receipt"`
				}
				if json.Unmarshal(w.Body.Bytes(), &result) != nil || (string(result.Receipt) == "null") != (state != kc.Committed) {
					t.Fatal("lookup union")
				}
			}
			if s.calls != 3 {
				t.Fatal("lookup automatically executed mutation")
			}
		})
	}
}

func TestTreeCommandsHTTPBadCommittedProjectionAndFault(t *testing.T) {
	for _, kind := range []string{"object", "target", "project", "title", "version", "overflow", "move_changed", "delete_ids"} {
		t.Run(kind, func(t *testing.T) {
			h, s, _ := fixture()
			action, body := "rename", `{"expected_version":"1","title":"original"}`
			switch kind {
			case "object":
				s.doc.ObjectID = oc.ObjectID{}
			case "target":
				s.doc.ID = testID[kc.Document](8)
			case "project":
				s.doc.ProjectID = testID[id.Project](8)
			case "title":
				s.doc.Title = "wrong"
			case "version":
				s.doc.ContentVersion = 3
			case "overflow":
				body = fmt.Sprintf(`{"expected_version":%q,"title":"original"}`, fmt.Sprint(int64(math.MaxInt64)))
				s.doc.ContentVersion = 1
			case "move_changed":
				action = "move"
				body = `{"expected_parent_id":null,"target_parent_id":null}`
				s.move.Changed = true
			case "delete_ids":
				action = "delete-subtree"
				body = fmt.Sprintf(`{"confirmation_token":%q}`, s.preview.Confirmation.ForHumanResponse())
				s.deleted.DeletedIDs = append(s.deleted.DeletedIDs, s.deleted.Root)
			}
			w := writer()
			if !serve(h, request(action, body), w) || w.Body.Len() != 0 || w.cleared() {
				t.Fatal("bad committed result published or connection reused")
			}
		})
	}
	t.Run("unknown", func(t *testing.T) {
		h, s, _ := fixture()
		s.err = f.NewFault(f.CommitUnknown, f.Unknown).WithCause(errors.New("user-private-canary"))
		w := writer()
		if serve(h, request("rename", `{"expected_version":"1","title":"original"}`), w) || !strings.Contains(w.Body.String(), `"commit_state":"unknown"`) || strings.Contains(w.Body.String(), "user-private-canary") {
			t.Fatal("Unknown fault was lost or leaked")
		}
	})
	t.Run("lookup_wrong_receipt", func(t *testing.T) {
		h, s, _ := fixture()
		s.doc.ContentVersion = 2
		s.lookup = kc.CommandLookup{State: kc.Committed, Receipt: &kc.MutationReceipt{Command: kc.Update, Document: &s.doc, Changed: false}}
		w := writer()
		if serve(h, request("lookup", fmt.Sprintf(`{"command":"update","document_id":%q,"request":{"expected_version":"1","title":"original"}}`, s.doc.ID.String())), w) || w.Code != 503 {
			t.Fatal("lookup trusted bad historical receipt")
		}
	})
}

// IO controls use a native-capable in-memory writer; no listener or socket.
type heldBody struct {
	io.Reader
	closes atomic.Int32
	close  func() error
}

func (b *heldBody) Close() error {
	b.closes.Add(1)
	if b.close != nil {
		return b.close()
	}
	return nil
}
