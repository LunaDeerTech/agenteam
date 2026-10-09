package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strings"
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
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type contentReuseExecutor struct {
	publicationReservationExecutor
	work publicationWork
}

func (x *contentReuseExecutor) QueryRow(ctx context.Context, sql string, args ...any) postgres.Row {
	if strings.Contains(sql, "FROM agenteam_knowledge.work_claims") {
		store := publicationCheckpointStore{work: x.work}
		return store.QueryRow(ctx, sql, args...)
	}
	return x.publicationReservationExecutor.QueryRow(ctx, sql, args...)
}

func TestMeasuredSourceReuseRequiresExactFinalCurrentState(t *testing.T) {
	source, _ := kc.NewTextSource(kc.PlainText, "same")
	_, input, intent, work, retirement, _ := directPublicationFixture(t, source)
	defer retirement.done()
	now, _ := f.NewInstant(time.Now())
	intent.record.created = now
	user, _ := f.ParseID[id.User](input.actor.Details().UserID)
	creator, _ := kc.NewCreatorRef(kc.CreatorDetails{Kind: id.Human, UserID: user})
	version := f.Version(4)
	sameTitle := "Current title"
	input, err := updateContentInput(input.actor, f.CommandMeta{RequestID: newID[f.Request](t), IdempotencyKey: "source-noop", ExpectedVersion: &version}, input.project, input.document, kc.UpdateRequest{Title: &sameTitle, ReplaceSource: true}, &source)
	if err != nil {
		t.Fatal(err)
	}
	intent.record.name, intent.record.digest = kc.Update, input.digest
	doc := kc.DocumentRef{ID: input.document, ProjectID: input.project, Title: sameTitle, ContentVersion: 4, SourceKind: kc.Text, MediaType: kc.PlainText, ObjectID: newID[oc.StoredObject](t), Status: kc.Active, IndexingStatus: kc.IndexReady, CreatedBy: creator, CreatedAt: now, UpdatedAt: now}
	row := &documentRow{head: kc.DocumentHead{Active: &doc}, upload: newID[oc.Upload](t)}
	scope, _ := id.InProject(input.project)
	meta := oc.ObjectMeta{ID: doc.ObjectID, Scope: scope, MediaType: kc.PlainText, ByteSize: 4, SHA256: ob.DigestBytes([]byte("same")), State: oc.Available, Version: 1, CreatedAt: now}
	reuse := contentReuse{work: work, digest: input.digest, object: meta, upload: row.upload, version: 4, title: sameTitle, measured: publicationMeasurement{Media: kc.PlainText, Length: 4, SHA: meta.SHA256}}
	raw, _ := json.Marshal(input.request.Source)
	x := &contentReuseExecutor{publicationReservationExecutor: publicationReservationExecutor{record: intent.record, project: input.project.String(), document: input.document.String(), source: raw, phase: "planned"}, work: work}
	if err = validateContentReuse(context.Background(), x, input, intent.record, row, &reuse); err != nil {
		t.Fatal("exact measured current source rejected", err)
	}
	for name, change := range map[string]func(*contentReuse){
		"different actual digest":  func(p *contentReuse) { p.object.SHA256 = ob.DigestBytes([]byte("other")) },
		"different actual length":  func(p *contentReuse) { p.object.ByteSize++ },
		"different actual media":   func(p *contentReuse) { p.object.MediaType = kc.Markdown },
		"different current object": func(p *contentReuse) { p.object.ID = newID[oc.StoredObject](t) },
		"different upload":         func(p *contentReuse) { p.upload = newID[oc.Upload](t) },
		"stale content":            func(p *contentReuse) { p.version++ },
		"stale title":              func(p *contentReuse) { p.title = "Before title update" },
		"different command input":  func(p *contentReuse) { p.digest = ob.DigestBytes([]byte("other input")) },
		"stale work":               func(p *contentReuse) { p.work.fence++ },
	} {
		t.Run(name, func(t *testing.T) {
			other := reuse
			change(&other)
			if err := validateContentReuse(context.Background(), x, input, intent.record, row, &other); err == nil {
				t.Fatal("stale comparison witness accepted")
			}
		})
	}
	// A concurrent Move at the same content version remains valid and its
	// current parent is retained; it does not manufacture a source change.
	parent := newID[kc.Document](t)
	doc.ParentDocumentID = &parent
	if err = validateContentReuse(context.Background(), x, input, intent.record, row, &reuse); err != nil {
		t.Fatal("Move invalidated unchanged content", err)
	}
	x.phase = "cancelled"
	if err = validateContentReuse(context.Background(), x, input, intent.record, row, &reuse); err == nil {
		t.Fatal("final reuse ignored durable publication state")
	}
}

func TestContentPublicationFinalUsesCurrentTreeAndMeasuredReservation(t *testing.T) {
	source, _ := kc.NewTextSource(kc.Markdown, "original")
	_, input, intent, _, retirement, _ := directPublicationFixture(t, source)
	defer retirement.done()
	now, _ := f.NewInstant(time.Now())
	before, _ := f.NewInstant(now.Time().Add(-time.Hour))
	intent.record.created = before
	intent.header, _ = newContentHeader(input.project, input.document, 1, before)
	attempt, _ := oc.NewUploadAttempt(oc.AttemptDetails{ID: newID[oc.Attempt](t), ObjectID: newID[oc.StoredObject](t), UploadID: newID[oc.Upload](t)})
	measured := publicationMeasurement{Media: kc.Markdown, Length: 8, SHA: ob.DigestBytes([]byte("original"))}
	parent := newID[kc.Document](t)
	input.request.Create.ParentDocumentID = &parent
	created, changes, err := contentPublicationResult(input, intent, nil, measured, attempt, now)
	if err != nil || created.ObjectID != attempt.Details().ObjectID || created.ContentVersion != 1 || created.SourceKind != kc.Text || created.MediaType != kc.Markdown || created.CreatedBy.Details().UserID.String() != input.actor.Details().UserID || created.CreatedAt != now || created.UpdatedAt != now || !reflect.DeepEqual(changes, []kc.ContentChange{kc.ContentCreated}) {
		t.Fatal("create lost original actor/measured object or event", err)
	}
	if err := (kc.ContentChangedPayload{DocumentID: created.ID, ContentVersion: created.ContentVersion, ObjectID: created.ObjectID, Changes: changes}).Validate(); err != nil {
		t.Fatal(err)
	}
	*created.ParentDocumentID = newID[kc.Document](t)
	if *input.request.Create.ParentDocumentID != parent {
		t.Fatal("final document aliases caller parent")
	}

	// A newer Move changes only the parent and updated_at. Publication must
	// keep that current position, creator and creation instant.
	current := created
	current.ContentVersion = 4
	current.CreatedAt = before
	future, _ := f.NewInstant(now.Time().Add(time.Minute))
	current.UpdatedAt = future
	title := "Replacement"
	expected := current.ContentVersion
	input, err = updateContentInput(input.actor, f.CommandMeta{RequestID: newID[f.Request](t), IdempotencyKey: "replace-content", ExpectedVersion: &expected}, current.ProjectID, current.ID, kc.UpdateRequest{Title: &title, ReplaceSource: true}, &source)
	if err != nil {
		t.Fatal(err)
	}
	intent.header, _ = newContentHeader(input.project, input.document, 5, now)
	intent.record.name, intent.record.project, intent.record.document = kc.Update, input.project, input.document
	old := &documentRow{head: kc.DocumentHead{Active: &current}, upload: newID[oc.Upload](t)}
	newAttempt, _ := oc.NewUploadAttempt(oc.AttemptDetails{ID: newID[oc.Attempt](t), ObjectID: newID[oc.StoredObject](t), UploadID: newID[oc.Upload](t)})
	updated, changes, err := contentPublicationResult(input, intent, old, measured, newAttempt, now)
	if err != nil || updated.Title != title || updated.ContentVersion != 5 || *updated.ParentDocumentID != *current.ParentDocumentID || updated.CreatedAt != before || updated.CreatedBy.Details() != current.CreatedBy.Details() || updated.UpdatedAt != future || updated.ObjectID != newAttempt.Details().ObjectID || !reflect.DeepEqual(changes, []kc.ContentChange{kc.TitleChanged, kc.SourceChanged}) {
		t.Fatal("update lost current tree/creator/clock or exact changes", err)
	}
	cleanup, err := replacementCleanup(intent.record, old)
	if err != nil || cleanup.id.String() != intent.record.id.String() || cleanup.object != current.ObjectID || cleanup.upload != old.upload || cleanup.reason != oc.ReplacedObject {
		t.Fatal("replacement cleanup lost original identity", err)
	}
	second, err := replacementCleanup(intent.record, old)
	if err != nil || *cleanup != *second {
		t.Fatal("retry minted another cleanup identity", err)
	}
	for name, change := range map[string]func(*kc.DocumentRef){
		"stale content":         func(d *kc.DocumentRef) { d.ContentVersion++ },
		"foreign Project":       func(d *kc.DocumentRef) { d.ProjectID = newID[id.Project](t) },
		"wrong document":        func(d *kc.DocumentRef) { d.ID = newID[kc.Document](t) },
		"same canonical object": func(d *kc.DocumentRef) { d.ObjectID = newAttempt.Details().ObjectID },
	} {
		t.Run(name, func(t *testing.T) {
			d := current
			change(&d)
			if _, _, err := contentPublicationResult(input, intent, &documentRow{head: kc.DocumentHead{Active: &d}, upload: old.upload}, measured, newAttempt, now); err == nil {
				t.Fatal("invalid final current state accepted")
			}
		})
	}
	unchangedTitle := current.Title
	input.request.Update.Title = &unchangedTitle
	if _, changes, err = contentPublicationResult(input, intent, old, measured, newAttempt, now); err != nil || !reflect.DeepEqual(changes, []kc.ContentChange{kc.SourceChanged}) {
		t.Fatal("same title manufactured title event", err)
	}
}

func TestTitleContentUsesCurrentTreeAndPreservesCanonicalSource(t *testing.T) {
	_, actor, q := queryFixture(t)
	user, _ := f.ParseID[id.User](actor.Details().UserID)
	creator, err := kc.NewCreatorRef(kc.CreatorDetails{Kind: id.Human, UserID: user})
	if err != nil {
		t.Fatal(err)
	}
	created, _ := f.NewInstant(time.Now().Add(-time.Hour))
	updated, _ := f.NewInstant(created.Time().Add(time.Minute))
	parent, oldParent := newID[kc.Document](t), newID[kc.Document](t)
	current := kc.DocumentRef{ID: newID[kc.Document](t), ProjectID: q.project, ParentDocumentID: &parent,
		Title: "Original", ContentVersion: 4, SourceKind: kc.Text, MediaType: kc.Markdown,
		ObjectID: newID[oc.StoredObject](t), Status: kc.Active, IndexingStatus: kc.IndexPending,
		CreatedBy: creator, CreatedAt: created, UpdatedAt: updated}
	if err = current.Validate(); err != nil {
		t.Fatal(err)
	}
	title, expected := "New title", current.ContentVersion
	input, err := updateContentInput(actor, f.CommandMeta{RequestID: newID[f.Request](t), IdempotencyKey: "title", ExpectedVersion: &expected}, q.project, current.ID, kc.UpdateRequest{Title: &title}, nil)
	if err != nil {
		t.Fatal(err)
	}
	header, err := newContentHeader(q.project, current.ID, 5, created)
	if err != nil {
		t.Fatal(err)
	}
	stale := current
	stale.ParentDocumentID = &oldParent
	intent := contentIntent{header: header, current: &documentRow{head: kc.DocumentHead{Active: &stale}}}
	out, err := titleContentResult(input, intent, current, created)
	if err != nil || out.Title != title || out.ContentVersion != 5 || *out.ParentDocumentID != parent || out.UpdatedAt != updated {
		t.Fatal("title finalization lost newer Move/current timestamp", err)
	}
	if out.ObjectID != current.ObjectID || out.MediaType != current.MediaType || out.SourceKind != current.SourceKind || out.CreatedBy.Details() != current.CreatedBy.Details() || out.CreatedAt != current.CreatedAt {
		t.Fatal("title-only update replaced canonical source or creator")
	}
	*out.ParentDocumentID = oldParent
	if *current.ParentDocumentID != parent || current.Title != "Original" || current.ContentVersion != 4 {
		t.Fatal("result aliases or mutates current row")
	}
	for name, alter := range map[string]func(*kc.DocumentRef){
		"stale version":      func(d *kc.DocumentRef) { d.ContentVersion++ },
		"version overflow":   func(d *kc.DocumentRef) { d.ContentVersion = f.Version(math.MaxInt64) },
		"foreign project":    func(d *kc.DocumentRef) { d.ProjectID = newID[id.Project](t) },
		"wrong document":     func(d *kc.DocumentRef) { d.ID = newID[kc.Document](t) },
		"already same title": func(d *kc.DocumentRef) { d.Title = title },
	} {
		t.Run(name, func(t *testing.T) {
			changed := current
			alter(&changed)
			if _, err := titleContentResult(input, intent, changed, updated); err == nil {
				t.Fatal("stale final input accepted")
			}
		})
	}
}

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
