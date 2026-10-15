package knowledge

import (
	"context"
	"encoding/json"
	"testing"

	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestIndexWorkerReadAuthorityRequiresOriginalLiveAdmission(t *testing.T) {
	s, _, p := retentionFixture(t)
	run, done, err := s.admit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	job := indexJob{id: newID[struct{}](t).String(), document: newID[kc.Document](t), version: 3}
	actor := bindingActor(t, id.KnowledgeRuntime, job.id, p.Project)
	admission, _ := run.Value(indexAdmissionKey{}).(*indexAdmission)
	w := &indexWork{service: s, actor: actor, profile: p, job: job, admission: admission}
	ctx := context.WithValue(run, indexWorkKey{}, w)
	if err := readInput(ctx, actor, p.Project); err != nil {
		t.Fatal("original worker read denied", err)
	}
	if err := readInput(context.Background(), actor, p.Project); err == nil {
		t.Fatal("service registration alone granted document access")
	}
	if _, err := currentIndexWork(ctx, actor, newID[id.Project](t)); err == nil {
		t.Fatal("worker crossed project")
	}
	other := bindingActor(t, id.KnowledgeRuntime, newID[struct{}](t).String(), p.Project)
	if _, err := currentIndexWork(ctx, other, p.Project); err == nil {
		t.Fatal("worker replaced original operation actor")
	}
	forged := *w
	forged.admission = &indexAdmission{}
	if _, err := currentIndexWork(context.WithValue(run, indexWorkKey{}, &forged), actor, p.Project); err == nil {
		t.Fatal("unregistered admission granted authority")
	}
	done()
	if _, err := currentIndexWork(ctx, actor, p.Project); err == nil {
		t.Fatal("returned original worker retained read authority")
	}
}

func TestIndexWorkerObjectTargetCannotAdvanceOrBorrowAnotherBody(t *testing.T) {
	s, _, p := retentionFixture(t)
	run, done, err := s.admit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	object := newID[oc.StoredObject](t)
	job := indexJob{id: newID[struct{}](t).String(), document: newID[kc.Document](t), version: 3, object: object.String()}
	actor := bindingActor(t, id.KnowledgeRuntime, job.id, p.Project)
	admission, _ := run.Value(indexAdmissionKey{}).(*indexAdmission)
	w := &indexWork{service: s, actor: actor, profile: p, job: job, admission: admission}
	ctx := context.WithValue(run, indexWorkKey{}, w)
	doc := kc.DocumentRef{ProjectID: p.Project, ID: job.document, ContentVersion: job.version, ObjectID: object}
	if got, err := indexObjectReadTarget(ctx, actor, doc); err != nil || got != object {
		t.Fatal("original job's body target rejected", err)
	}
	for _, mutate := range []func(*kc.DocumentRef){
		func(d *kc.DocumentRef) { d.ProjectID = newID[id.Project](t) },
		func(d *kc.DocumentRef) { d.ID = newID[kc.Document](t) },
		func(d *kc.DocumentRef) { d.ContentVersion++ },
		func(d *kc.DocumentRef) { d.ObjectID = newID[oc.StoredObject](t) },
		func(d *kc.DocumentRef) { d.ObjectID = oc.ObjectID{} },
	} {
		other := doc
		mutate(&other)
		if _, err := indexObjectReadTarget(ctx, actor, other); err == nil {
			t.Fatal("original job's object target was substituted")
		}
	}
	if _, err := indexObjectReadTarget(context.Background(), actor, doc); err == nil {
		t.Fatal("technical actor alone bound an object body")
	}
	done()
	if _, err := indexObjectReadTarget(ctx, actor, doc); err == nil {
		t.Fatal("returned original admission retained object authority")
	}
}

func TestIndexWorkerLedgerIdentityCannotMasqueradeAsAgentOrHuman(t *testing.T) {
	job := newID[struct{}](t).String()
	document := newID[kc.Document](t).String()
	identity := projectCallIdentity{Format: 1, Method: "index-job", IndexJob: job, Document: document}
	raw, _ := json.Marshal(identity)
	if _, err := canonicalProjectIdentity(raw); err != nil {
		t.Fatal("closed worker identity rejected", err)
	}
	for _, mutate := range []func(*projectCallIdentity){
		func(v *projectCallIdentity) { v.User = newID[id.User](t).String() },
		func(v *projectCallIdentity) { v.Agent = newID[id.Agent](t).String() },
		func(v *projectCallIdentity) { v.Operation = job },
		func(v *projectCallIdentity) { v.Document = "" },
		func(v *projectCallIdentity) { v.Method = "read" },
		func(v *projectCallIdentity) { v.IndexJob = "" },
	} {
		forged := identity
		mutate(&forged)
		raw, _ = json.Marshal(forged)
		if _, err := canonicalProjectIdentity(raw); err == nil {
			t.Fatal("worker ledger accepted mixed or incomplete identity")
		}
	}
}
