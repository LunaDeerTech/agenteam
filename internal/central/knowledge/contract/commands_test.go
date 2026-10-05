package contract_test

import (
	"encoding/json"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	k "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func createRequest() k.CreateRequest {
	return k.CreateRequest{ProjectID: keyID[id.Project](4), DocumentID: keyID[k.Document](10), Title: "same title"}
}
func TestCommandIdentityUsesRealNamespaceAndOwner(t *testing.T) {
	for _, name := range []k.CommandName{k.Create, k.Update, k.Move, k.DeleteSubtree} {
		got := must(k.CommandIdentity(keyID[id.Project](4), name, "intent"))
		if got.Namespace() != "knowledge" || len(got.OwnerIDs()) != 1 || got.OwnerIDs()[0] != keyID[id.Project](4).String() || got.Command() != string(name) || got.Key() != "intent" {
			t.Fatal("wrong formal identity")
		}
		if _, e := f.CommandLock(got); e != nil {
			t.Fatal(e)
		}
	}
	_, e := k.CommandIdentity(id.ProjectID{}, k.Create, "intent")
	requireError(t, e)
	_, e = k.CommandIdentity(keyID[id.Project](4), "other", "intent")
	requireError(t, e)
}
func TestBusinessDigestExcludesAttemptAndIncludesAllContent(t *testing.T) {
	request := createRequest()
	source := must(k.NewTextSource(k.Markdown, " body\r\n"))
	a := actor()
	m := meta()
	original := must(k.CreateDigest(a, m, request, source))
	m2 := m
	m2.RequestID = keyID[f.Request](99)
	a2 := must(id.NewHuman(keyID[id.User](1), keyID[id.Session](99)))
	if got := must(k.CreateDigest(a2, m2, request, source)); got != original {
		t.Fatal("attempt fields changed semantic digest")
	}
	otherUser := must(id.NewHuman(keyID[id.User](99), keyID[id.Session](99)))
	if must(k.CreateDigest(otherUser, m, request, source)) == original {
		t.Fatal("different stable user collided")
	}
	for _, change := range []func(*k.CreateRequest){func(x *k.CreateRequest) { x.DocumentID = keyID[k.Document](11) }, func(x *k.CreateRequest) { x.Title += " " }, func(x *k.CreateRequest) { x.ParentDocumentID = ptr(keyID[k.Document](12)) }, func(x *k.CreateRequest) { x.ProjectID = keyID[id.Project](11) }} {
		changed := request
		change(&changed)
		if must(k.CreateDigest(a, m, changed, source)) == original {
			t.Fatal("semantic field omitted")
		}
	}
	for _, s := range []k.SourceInput{must(k.NewTextSource(k.Markdown, "body\r\n")), must(k.NewTextSource(k.Markdown, " body\n")), must(k.NewTextSource(k.PlainText, " body\r\n"))} {
		if must(k.CreateDigest(a, m, request, s)) == original {
			t.Fatal("source bytes/media omitted")
		}
	}
	b := body("not-read-by-digest")
	upload := must(k.NewUploadSource(k.PDF, 18, sha(), b))
	first := must(k.CreateDigest(a, m, request, upload))
	r := must(upload.TakeUploadBody())
	buf := make([]byte, 1)
	n, e := r.Read(buf)
	requireOK(t, e)
	if n != 1 || buf[0] != 'n' {
		t.Fatal("digest consumed body")
	}
	requireOK(t, r.Close())
	if must(k.CreateDigest(a, m, request, upload)) != first {
		t.Fatal("close changed semantic meaning")
	}
}
func uploaded(cause int) oc.BusinessFileRef {
	owner := must(oc.NewObjectOwner(oc.Knowledge, keyID[k.Document](18).String(), keyID[id.Project](4).String()))
	receipt := must(oc.NewUploadReceipt(oc.ReceiptDetails{ID: keyID[oc.Receipt](20), UploadID: keyID[oc.Upload](21), ObjectID: keyID[oc.StoredObject](22), Owner: owner, CreationCause: keyID[f.Request](cause).String()}))
	return must(oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.UploadedObject, Receipt: receipt}))
}
func TestBusinessFileDigestUsesTypedDetailsNotRedactedLabel(t *testing.T) {
	a := must(k.NewBusinessSource(uploaded(31)))
	b := must(k.NewBusinessSource(uploaded(32)))
	if string(must(json.Marshal(a))) != string(must(json.Marshal(b))) {
		t.Fatal("unexpected generic source disclosure")
	}
	if must(k.CreateDigest(actor(), meta(), createRequest(), a)) == must(k.CreateDigest(actor(), meta(), createRequest(), b)) {
		t.Fatal("receipt cause omitted")
	}
	reference := func(version f.Version) k.SourceInput {
		return must(k.NewBusinessSource(must(oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.KnowledgeFile, ProjectID: keyID[id.Project](4), DocumentID: keyID[k.Document](100).String(), Revision: version}))))
	}
	if must(k.CreateDigest(actor(), meta(), createRequest(), reference(1))) == must(k.CreateDigest(actor(), meta(), createRequest(), reference(2))) {
		t.Fatal("source version omitted")
	}
}
func TestCommandExpectedVersionAndHumanBoundary(t *testing.T) {
	r := createRequest()
	source := must(k.NewTextSource(k.PlainText, "x"))
	m := meta()
	m.ExpectedVersion = ptr(f.Version(1))
	_, e := k.CreateDigest(actor(), m, r, source)
	requireError(t, e)
	_, e = k.MoveDigest(actor(), m, r.ProjectID, r.DocumentID, k.MoveRequest{})
	requireError(t, e)
	_, e = k.UpdateDigest(actor(), meta(), r.ProjectID, r.DocumentID, k.UpdateRequest{Title: ptr("x")}, nil)
	requireError(t, e)
	update := k.UpdateRequest{ReplaceSource: true}
	good := must(k.UpdateDigest(actor(), m, r.ProjectID, r.DocumentID, update, &source))
	_, e = k.UpdateDigest(actor(), m, r.ProjectID, r.DocumentID, update, nil)
	requireError(t, e)
	_, e = k.UpdateDigest(actor(), m, r.ProjectID, r.DocumentID, k.UpdateRequest{Title: ptr("x")}, &source)
	requireError(t, e)
	m.ExpectedVersion = ptr(f.Version(2))
	if must(k.UpdateDigest(actor(), m, r.ProjectID, r.DocumentID, update, &source)) == good {
		t.Fatal("version omitted")
	}
	_, e = k.CreateDigest(must(id.NewAgentRun(r.ProjectID, keyID[id.Agent](1), keyID[id.Execution](1))), meta(), r, source)
	requireError(t, e)
}
func TestNullableParentAndPatchWire(t *testing.T) {
	var move k.MoveRequest
	requireOK(t, json.Unmarshal([]byte(`{"expected_parent_id":null,"target_parent_id":null}`), &move))
	for _, raw := range []string{`{}`, `{"expected_parent_id":null}`, `{"expected_parent_id":null,"target_parent_id":""}`, `{"expected_parent_id":null,"target_parent_id":null,"target_parent_id":null}`} {
		requireError(t, json.Unmarshal([]byte(raw), &move))
	}
	var update k.UpdateRequest
	requireOK(t, json.Unmarshal([]byte(`{"title":"new","replace_source":false}`), &update))
	for _, raw := range []string{`{"title":null,"replace_source":true}`, `{"replace_source":false}`, `{"replace_source":null}`, `{"replace_source":true,"parent_document_id":null}`} {
		requireError(t, json.Unmarshal([]byte(raw), &update))
	}
	var create k.CreateRequest
	roundTrip(t, createRequest(), &create)
}
func TestReceiptAndLookupClosedShapes(t *testing.T) {
	d := doc(10)
	for _, name := range []k.CommandName{k.Create, k.Update, k.Move} {
		r := k.MutationReceipt{Command: name, Document: &d, Changed: true}
		var decoded k.MutationReceipt
		roundTrip(t, r, &decoded)
		wire := string(must(json.Marshal(r)))
		requireError(t, json.Unmarshal([]byte(strings.TrimSuffix(wire, "}")+`,"deleted_ids":[]}`), &decoded))
		var result k.CommandLookup
		roundTrip(t, k.CommandLookup{State: k.Committed, Receipt: &r}, &result)
	}
	deletion := k.MutationReceipt{Command: k.DeleteSubtree, RootID: &d.ID, DeletedIDs: []k.DocumentID{d.ID}, Changed: true, CleanupPending: false}
	var decoded k.MutationReceipt
	roundTrip(t, deletion, &decoded)
	wire := string(must(json.Marshal(deletion)))
	if strings.Contains(wire, "document") || strings.Contains(wire, "object_id") || strings.Contains(wire, "title") {
		t.Fatal("deletion receipt carried payload")
	}
	requireError(t, (k.CommandLookup{State: k.NotObserved, Receipt: &deletion}).Validate())
	requireError(t, (k.CommandLookup{State: k.Committed}).Validate())
	var result k.CommandLookup
	roundTrip(t, k.CommandLookup{State: k.InProgress}, &result)
}
