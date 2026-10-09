package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	auditimpl "github.com/LunaDeerTech/agenteam/internal/central/audit"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	ev "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

func TestContentIntentCopiesExactRequestWithoutRetainingBody(t *testing.T) {
	_, actor, q := queryFixture(t)
	parent := newID[kc.Document](t)
	originalParent := parent
	meta := f.CommandMeta{RequestID: newID[f.Request](t), IdempotencyKey: "original-content-command"}
	source, err := kc.NewTextSource("text/plain", "private-content-intent-canary")
	if err != nil {
		t.Fatal(err)
	}
	request := kc.CreateRequest{ProjectID: q.project, DocumentID: newID[kc.Document](t), ParentDocumentID: &parent, Title: "Name"}
	input, err := createContentInput(actor, meta, request, source)
	if err != nil {
		t.Fatal(err)
	}
	parent = newID[kc.Document](t)
	if *input.request.Create.ParentDocumentID != originalParent {
		t.Fatal("caller changed captured parent after digest")
	}
	raw, err := json.Marshal(input.request)
	if err != nil || bytes.Contains(raw, []byte("private-content-intent-canary")) {
		t.Fatal("body entered persistent command request", err)
	}
	if _, err = decodeContentRequest(raw); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{
		bytes.Replace(raw, []byte(`"format":1`), []byte(`"format":1,"format":1`), 1),
		append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"expected_version":"2"}`)...),
		append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"body":"forbidden"}`)...),
	} {
		if _, err = decodeContentRequest(bad); err == nil {
			t.Fatal("corrupt intent shape accepted")
		}
	}
	title := "Updated"
	expected := f.Version(4)
	meta.ExpectedVersion = &expected
	update, err := updateContentInput(actor, meta, q.project, request.DocumentID, kc.UpdateRequest{Title: &title}, nil)
	if err != nil {
		t.Fatal(err)
	}
	title = "Caller changed title"
	expected = 5
	if *update.request.Update.Title != "Updated" || *update.request.Expected != 4 || *update.meta.ExpectedVersion != 4 {
		t.Fatal("caller mutated captured expected version or title")
	}
	if _, err = updateContentInput(actor, meta, q.project, request.DocumentID, kc.UpdateRequest{ReplaceSource: true}, nil); err == nil {
		t.Fatal("replacement without source accepted")
	}
	if _, err = updateContentInput(actor, meta, q.project, request.DocumentID, kc.UpdateRequest{Title: &title}, &source); err == nil {
		t.Fatal("unrequested source replacement accepted")
	}
}

func TestDeleteAuditMatchesFormalKeyAndRequiresExactPrivateWitness(t *testing.T) {
	_, actor, q := queryFixture(t)
	root := newID[kc.Document](t)
	nodes := []kc.ScopeNode{{ID: root, ProjectID: q.project, ContentVersion: 3, Status: kc.Active}}
	entry, err := deleteAuditEntry(actor, q.project, root, nodes)
	if err != nil {
		t.Fatal(err)
	}
	key, err := deleteAuditKey(q.project, "private-original-key")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := kc.CommandIdentity(q.project, kc.DeleteSubtree, "private-original-key")
	if err != nil {
		t.Fatal(err)
	}
	formal, err := auditimpl.CommandAppendKey(au.KnowledgeProducer, identity, 0)
	if err != nil || formal.Details() != key.Details() {
		t.Fatal("changed Audit canonical key", err)
	}
	store := &authorityStore{tx: f.NewTx()}
	checker, err := NewProjectAuditAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	if err = checker.CheckProjectAuditInTx(context.Background(), store.tx, entry, key); err == nil || store.queries != 0 || store.held != 0 {
		t.Fatal("public entry authorized without domain witness")
	}
	w := deleteAuditWitness{store: store, tx: store.tx, command: newID[command](t), key: "private-original-key", entry: entry, appendKey: key, nodes: nodes}
	for name, alter := range map[string]func(*deleteAuditWitness){
		"foreign Store": func(v *deleteAuditWitness) { v.store = &authorityStore{} },
		"foreign Tx":    func(v *deleteAuditWitness) { v.tx = f.NewTx() },
		"different key": func(v *deleteAuditWitness) { v.key = "different-key" },
		"different scope": func(v *deleteAuditWitness) {
			v.nodes = []kc.ScopeNode{{ID: root, ProjectID: q.project, ContentVersion: 4, Status: kc.Active}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := w
			alter(&changed)
			ctx := context.WithValue(context.Background(), deleteAuditWitnessKey{}, changed)
			if err := checker.CheckProjectAuditInTx(ctx, store.tx, entry, key); err == nil || store.queries != 0 {
				t.Fatal("altered witness reached SQL", err)
			}
		})
	}
	// A matching witness still does not grant access: actual locks and SQL facts
	// must succeed. The fixture has no command row, so this remains a rejection.
	ctx := context.WithValue(context.Background(), deleteAuditWitnessKey{}, w)
	if err = checker.CheckProjectAuditInTx(ctx, store.tx, entry, key); err == nil || store.held != 1 || store.queries != 1 {
		t.Fatal("witness replaced durable facts", err)
	}
}

func TestEventAuthorityBindsOriginalFactAndPrivateIssuer(t *testing.T) {
	_, actor, q := queryFixture(t)
	document, object := newID[kc.Document](t), newID[oc.StoredObject](t)
	project, _ := f.ParseID[ev.Project](q.project.String())
	aggregate, _ := f.ParseID[ev.Aggregate](document.String())
	now, _ := f.NewInstant(time.Now())
	version := f.Version(2)
	header := ev.Header{EventID: newID[ev.EventIdentity](t), EventType: kc.ContentChangedEvent, SchemaVersion: 1, OccurredAt: now, Scope: ev.Scope{Kind: ev.ProjectScope, ProjectID: project}, AggregateType: kc.KnowledgeAggregate, AggregateID: aggregate, AggregateVersion: &version}
	types, err := kc.RegisterKnowledgeEvents(ev.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	event, err := types.ContentChanged(header, kc.ContentChangedPayload{DocumentID: document, ContentVersion: version, Changes: []kc.ContentChange{kc.TitleChanged}, ObjectID: object})
	if err != nil {
		t.Fatal(err)
	}
	user, _ := f.ParseID[id.User](actor.Details().UserID)
	record := &commandRecord{id: newID[command](t), project: q.project, document: document, user: user, name: kc.Update, key: "update", state: kc.InProgress}
	row := &commandEvent{record, document, event.Header(), event.PayloadBytes()}
	binding, locks, opaque, err := eventBinding(actor, event.Summary(), row)
	if err != nil {
		t.Fatal(err)
	}
	renewed, _ := id.NewHuman(user, newID[id.Session](t))
	other, _, _, err := eventBinding(renewed, event.Summary(), row)
	if err != nil || other == binding {
		t.Fatal("event plan did not bind actual Session", err)
	}
	for name, alter := range map[string]func(*commandEvent){
		"wrong command":   func(v *commandEvent) { c := *v.command; c.name = kc.Move; v.command = &c },
		"wrong user":      func(v *commandEvent) { c := *v.command; c.user = newID[id.User](t); v.command = &c },
		"payload changed": func(v *commandEvent) { v.payload = []byte(`{}`) },
		"header changed":  func(v *commandEvent) { v.header.EventID = newID[ev.EventIdentity](t) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := *row
			alter(&changed)
			if _, _, _, err := eventBinding(actor, event.Summary(), &changed); err == nil {
				t.Fatal("altered event fact accepted")
			}
		})
	}
	store := &authorityStore{tx: f.NewTx()}
	a, _ := NewAuthority(store, &ownerGate{err: fault(f.Forbidden)})
	foreign, err := ob.NewDependencies(ob.NewPlanIssuer(), binding, locks, opaque)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.ValidateAppendInTx(context.Background(), store.tx, actor, event.Summary(), foreign, ob.NewFact); err == nil || store.held != 0 || store.queries != 0 {
		t.Fatal("foreign producer issuer reached Store")
	}
	var decoded string
	if json.Unmarshal(opaque, &decoded) != nil || decoded != record.id.String() {
		t.Fatal("opaque contains more than safe command identity")
	}
}
