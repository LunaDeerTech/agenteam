package texthttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
func testActor() id.Actor { return must(id.NewHuman(testID[id.User](1), testID[id.Session](2))) }
func testDocument() kc.DocumentRef {
	at := must(f.ParseInstant("2026-10-11T01:02:03.123456Z"))
	return kc.DocumentRef{ID: testID[kc.Document](3), ProjectID: testID[id.Project](4), Title: " exact title ", ContentVersion: 1, SourceKind: kc.Text, MediaType: kc.PlainText, ObjectID: testID[oc.StoredObject](5), Status: kc.Active, IndexingStatus: kc.IndexPending, CreatedBy: must(kc.NewCreatorRef(kc.CreatorDetails{Kind: id.Human, UserID: testID[id.User](1)})), CreatedAt: at, UpdatedAt: at}
}

type testBoundary struct {
	reject        error
	checks, auths int
}

func (b *testBoundary) CheckRequest(http.ResponseWriter, *http.Request) error {
	b.checks++
	return b.reject
}
func (b *testBoundary) RequireHuman(*http.Request) (id.Actor, error) {
	b.auths++
	return testActor(), nil
}
func (*testBoundary) WriteProblem(w http.ResponseWriter, r *http.Request, e error) {
	(&account.HTTPBoundary{}).WriteProblem(w, r, e)
}

type testService struct {
	doc                              kc.DocumentRef
	lookup                           kc.CommandLookup
	err                              error
	calls, creates, updates, lookups int
	meta                             f.CommandMeta
	create                           kc.CreateRequest
	update                           kc.UpdateRequest
	source                           kc.SourceInput
	lookupRequest                    kc.LookupRequest
	before                           func(context.Context)
}

func (s *testService) enter(ctx context.Context) {
	s.calls++
	if s.before != nil {
		s.before(ctx)
	}
}
func (s *testService) CreateDocument(ctx context.Context, _ id.Actor, m f.CommandMeta, r kc.CreateRequest, src kc.SourceInput) (kc.DocumentRef, error) {
	s.enter(ctx)
	s.creates++
	s.meta = m
	s.create = r
	s.source = src
	return s.doc, s.err
}
func (s *testService) UpdateDocument(ctx context.Context, _ id.Actor, m f.CommandMeta, _ id.ProjectID, _ kc.DocumentID, r kc.UpdateRequest, src *kc.SourceInput) (kc.DocumentRef, error) {
	s.enter(ctx)
	s.updates++
	s.meta = m
	s.update = r
	s.source = *src
	return s.doc, s.err
}
func (s *testService) LookupCommand(ctx context.Context, _ id.Actor, r kc.LookupRequest) (kc.CommandLookup, error) {
	s.enter(ctx)
	s.lookups++
	s.lookupRequest = r
	return s.lookup, s.err
}
func fixture() (*handler, *testService, *testBoundary) {
	s := &testService{doc: testDocument(), lookup: kc.CommandLookup{State: kc.NotObserved}}
	b := &testBoundary{}
	return &handler{s, b}, s, b
}
func testPath(action string) string {
	base := projectPrefix + testID[id.Project](4).String() + "/knowledge/documents/"
	if action == "lookup" {
		return base + "text-commands/lookup"
	}
	return base + testID[kc.Document](3).String() + "/" + action
}

const createBody = `{"parent_document_id":null,"title":" exact title ","media_type":"text/plain","text":" original 中文\r\n "}`
const replaceBody = `{"expected_version":"1","media_type":"text/plain","text":" original 中文\r\n "}`

func request(action, body string) *http.Request {
	r := httptest.NewRequest("POST", testPath(action), strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "original-key")
	return r
}

type testWriter struct {
	*httptest.ResponseRecorder
	read, write func(time.Time) error
	flush       func() error
}

func writer() *testWriter { return &testWriter{ResponseRecorder: httptest.NewRecorder()} }
func (w *testWriter) SetReadDeadline(at time.Time) error {
	if w.read != nil {
		return w.read(at)
	}
	return nil
}
func (w *testWriter) SetWriteDeadline(at time.Time) error {
	if w.write != nil {
		return w.write(at)
	}
	return nil
}
func (w *testWriter) FlushError() error {
	if w.flush != nil {
		return w.flush()
	}
	return nil
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
func success(t *testing.T, h http.Handler, r *http.Request) *testWriter {
	t.Helper()
	w := writer()
	if serve(h, r, w) || w.Code != 200 {
		t.Fatalf("response status=%d body=%s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Length") != fmt.Sprint(w.Body.Len()) {
		t.Fatal("complete safe response headers")
	}
	for _, private := range []string{"object_id", testID[oc.StoredObject](5).String(), "original-key", " original 中文"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatal("private field in metadata")
		}
	}
	return w
}

func TestKnowledgeTextHTTPOriginalSourceAndIntent(t *testing.T) {
	if h, e := NewHTTPHandler(nil, nil); h != nil || e == nil {
		t.Fatal("unbound constructor")
	}
	for _, action := range []string{"create-text", "replace-text"} {
		t.Run(action, func(t *testing.T) {
			h, s, _ := fixture()
			body := createBody
			if action == "replace-text" {
				body = replaceBody
				s.doc.ContentVersion = 2
			}
			success(t, h, request(action, body))
			details, e := s.source.Details()
			if e != nil || details.Text == nil || *details.Text != " original 中文\r\n " || details.Kind != kc.InputText || details.MediaType != kc.PlainText {
				t.Fatal("original text not preserved")
			}
			if s.meta.RequestID.Validate() != nil || s.meta.IdempotencyKey != "original-key" {
				t.Fatal("command identity")
			}
			if action == "create-text" {
				if s.creates != 1 || s.updates != 0 || s.create.ProjectID != s.doc.ProjectID || s.create.DocumentID != s.doc.ID || s.create.ParentDocumentID != nil || s.create.Title != s.doc.Title || s.meta.ExpectedVersion != nil {
					t.Fatal("create contract")
				}
			} else if s.updates != 1 || s.creates != 0 || s.update.Title != nil || !s.update.ReplaceSource || s.meta.ExpectedVersion == nil || *s.meta.ExpectedVersion != 1 {
				t.Fatal("replacement contract")
			}
		})
	}
}

func TestKnowledgeTextHTTPLookupNeverExecutes(t *testing.T) {
	for _, command := range []kc.CommandName{kc.Create, kc.Update} {
		for _, state := range []kc.LookupState{kc.NotObserved, kc.InProgress, kc.Committed} {
			t.Run(string(command)+"/"+string(state), func(t *testing.T) {
				h, s, _ := fixture()
				body := createBody
				meta := f.CommandMeta{RequestID: testID[f.Request](12), IdempotencyKey: "original-key"}
				s.lookup = kc.CommandLookup{State: state}
				source := must(kc.NewTextSource(kc.PlainText, " original 中文\r\n "))
				var digest f.Digest
				if command == kc.Create {
					digest = must(kc.CreateDigest(testActor(), meta, kc.CreateRequest{ProjectID: s.doc.ProjectID, DocumentID: s.doc.ID, Title: s.doc.Title}, source))
				} else {
					body = replaceBody
					v := f.Version(1)
					meta.ExpectedVersion = &v
					s.doc.ContentVersion = 2
					digest = must(kc.UpdateDigest(testActor(), meta, s.doc.ProjectID, s.doc.ID, kc.UpdateRequest{ReplaceSource: true}, &source))
				}
				if state == kc.Committed {
					s.lookup.Receipt = &kc.MutationReceipt{Command: command, Document: &s.doc, Changed: true}
				}
				raw := fmt.Sprintf(`{"command":%q,"document_id":%q,"request":%s}`, command, s.doc.ID, body)
				w := success(t, h, request("lookup", raw))
				if s.lookups != 1 || s.creates != 0 || s.updates != 0 || s.lookupRequest.Command != command || s.lookupRequest.SemanticDigest != digest || s.lookupRequest.Key != "original-key" {
					t.Fatal("lookup mutated or lost original semantic binding")
				}
				var result map[string]json.RawMessage
				if json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result) != 2 {
					t.Fatal("lookup union")
				}
				if state != kc.Committed && string(result["receipt"]) != "null" {
					t.Fatal("uncommitted receipt")
				}
			})
		}
	}
}

func TestKnowledgeTextHTTPRejectsBeforeService(t *testing.T) {
	cases := []struct {
		name, action, body string
		modify             func(*http.Request)
	}{
		{"unknown", "create-text", strings.TrimSuffix(createBody, "}") + `,"extra":true}`, nil},
		{"decoded duplicate", "create-text", strings.TrimSuffix(createBody, "}") + `,"te\u0078t":"evil"}`, nil},
		{"null text", "create-text", `{"parent_document_id":null,"title":"x","media_type":"text/plain","text":null}`, nil},
		{"missing parent", "create-text", `{"title":"x","media_type":"text/plain","text":""}`, nil},
		{"self parent", "create-text", fmt.Sprintf(`{"parent_document_id":%q,"title":"x","media_type":"text/plain","text":""}`, testID[kc.Document](3)), nil},
		{"lone surrogate", "create-text", `{"parent_document_id":null,"title":"x","media_type":"text/plain","text":"\ud800"}`, nil},
		{"invalid bytes", "create-text", strings.Replace(createBody, "中文", string([]byte{0xff}), 1), nil},
		{"file media", "create-text", strings.Replace(createBody, "text/plain", "application/pdf", 1), nil},
		{"title control", "create-text", strings.Replace(createBody, " exact title ", `bad\n`, 1), nil},
		{"version zero", "replace-text", strings.Replace(replaceBody, `"1"`, `"0"`, 1), nil},
		{"version number", "replace-text", strings.Replace(replaceBody, `"1"`, `1`, 1), nil},
		{"extra replacement title", "replace-text", strings.TrimSuffix(replaceBody, "}") + `,"title":"hidden edit"}`, nil},
		{"wrong method", "create-text", createBody, func(r *http.Request) { r.Method = "GET" }},
		{"query", "create-text", createBody, func(r *http.Request) { r.URL.RawQuery = "a=b" }},
		{"empty query", "create-text", createBody, func(r *http.Request) { r.URL.ForceQuery = true }},
		{"no key", "create-text", createBody, func(r *http.Request) { r.Header.Del("Idempotency-Key") }},
		{"duplicate key header", "create-text", createBody, func(r *http.Request) { r.Header.Add("Idempotency-Key", "other") }},
		{"encoding header", "create-text", createBody, func(r *http.Request) { r.Header["Content-Encoding"] = []string{""} }},
		{"body limit", "create-text", strings.Replace(createBody, " original", strings.Repeat("x", requestLimit), 1), nil},
		{"nested extra", "lookup", fmt.Sprintf(`{"command":"create","document_id":%q,"request":%s}`, testID[kc.Document](3), strings.TrimSuffix(createBody, "}")+`,"extra":true}`), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, s, _ := fixture()
			r := request(tc.action, tc.body)
			if tc.modify != nil {
				tc.modify(r)
			}
			w := writer()
			if serve(h, r, w) || w.Code < 400 || s.calls != 0 {
				t.Fatalf("reject status=%d calls=%d", w.Code, s.calls)
			}
		})
	}
	for _, path := range []string{testPath("create-text") + "/", projectPrefix + "x/knowledge/documents/create-text", testPath("rename")} {
		if HandlesPath(path) {
			t.Fatal("route overlap")
		}
	}
	h, s, b := fixture()
	b.reject = f.NewFault(f.Forbidden, f.NotStarted)
	r := request("create-text", createBody)
	r.Method = "DELETE"
	w := writer()
	if serve(h, r, w) || w.Code != 403 || s.calls != 0 || b.auths != 0 || w.Header().Get("Allow") != "" {
		t.Fatal("browser precheck must precede method/identity")
	}
}

func TestKnowledgeTextHTTPUnknownAndSafeProjection(t *testing.T) {
	h, s, _ := fixture()
	s.err = f.NewFault(f.CommitUnknown, f.Unknown)
	w := writer()
	if serve(h, request("create-text", createBody), w) || s.calls != 1 || !strings.Contains(w.Body.String(), `"commit_state":"unknown"`) || !strings.Contains(w.Body.String(), `"retry_hint":"lookup"`) || strings.Contains(w.Body.String(), `"document"`) {
		t.Fatalf("unknown altered: %s", w.Body.String())
	}
	for _, corrupt := range []func(*kc.DocumentRef){func(d *kc.DocumentRef) { d.ID = testID[kc.Document](99) }, func(d *kc.DocumentRef) { d.ContentVersion = 9 }, func(d *kc.DocumentRef) { d.MediaType = kc.Markdown }, func(d *kc.DocumentRef) { d.Title = "another" }, func(d *kc.DocumentRef) { d.ObjectID = oc.ObjectID{} }} {
		h, s, _ := fixture()
		corrupt(&s.doc)
		w := writer()
		if !serve(h, request("create-text", createBody), w) || w.Body.Len() != 0 {
			t.Fatal("postcommit invalid projection must abort")
		}
	}
}

func TestKnowledgeTextHTTPExactRecoverableBodyBoundary(t *testing.T) {
	for _, action := range []string{"create-text", "replace-text"} {
		t.Run(action, func(t *testing.T) {
			prefix := `{"parent_document_id":null,"title":" exact title ","media_type":"text/plain","text":"`
			command := "create"
			if action == "replace-text" {
				prefix = `{"expected_version":"1","media_type":"text/plain","text":"`
				command = "update"
			}
			body := prefix + strings.Repeat("a", requestLimit-len(prefix)-2) + `"}`
			if len(body) != requestLimit {
				t.Fatal("incorrect exact write boundary")
			}
			h, s, _ := fixture()
			success(t, h, request(action, body))
			if s.creates+s.updates != 1 {
				t.Fatal("exact write not admitted")
			}
			envelope := fmt.Sprintf(`{"command":%q,"document_id":%q,"request":%s}`, command, s.doc.ID.String(), body)
			if len(envelope) != lookupRequestLimit {
				t.Fatal("minimal recovery overhead changed")
			}
			h, s, _ = fixture()
			success(t, h, request("lookup", envelope))
			if s.lookups != 1 || s.creates+s.updates != 0 {
				t.Fatal("exact original lookup was rejected or executed")
			}
			for _, input := range []struct{ action, body string }{{action, body + " "}, {"lookup", envelope + " "}, {"lookup", strings.TrimSuffix(envelope, "}") + " }"}} {
				h, s, _ := fixture()
				w := writer()
				if serve(h, request(input.action, input.body), w) || w.Code != http.StatusRequestEntityTooLarge || s.calls != 0 {
					t.Fatalf("oversize boundary status=%d calls=%d", w.Code, s.calls)
				}
			}
		})
	}
}
