package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

type descriptorBody struct{ reads, closes int }

func (b *descriptorBody) Read([]byte) (int, error) { b.reads++; return 0, io.EOF }
func (b *descriptorBody) Close() error             { b.closes++; return nil }

func TestPublicationDescriptorNeverPersistsOrConsumesContent(t *testing.T) {
	text := "private-document-body-canary"
	source, err := kc.NewTextSource("text/plain", text)
	if err != nil {
		t.Fatal(err)
	}
	p, err := describePublicationSource(source)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(p)
	if err != nil || bytes.Contains(raw, []byte(text)) || strings.Contains(fmt.Sprintf("%+v", p), p.SHA.String()) {
		t.Fatal("descriptor exposed source content", err)
	}
	if p.Length == nil || *p.Length != f.Progress(len(text)) || p.SHA == nil {
		t.Fatal("exact original text metadata missing")
	}
	for _, changed := range []string{text + "!", text[:len(text)-1]} {
		s, err := kc.NewTextSource("text/plain", changed)
		if err != nil {
			t.Fatal(err)
		}
		v, err := describePublicationSource(s)
		if err != nil || *v.SHA == *p.SHA {
			t.Fatal("different body lost semantic identity", err)
		}
	}
	body := &descriptorBody{}
	upload, err := kc.NewUploadSource("text/plain", *p.Length, *p.SHA, body)
	if err != nil {
		t.Fatal(err)
	}
	u, err := describePublicationSource(upload)
	if err != nil || body.reads != 0 || body.closes != 0 || u.Kind != kc.InputUpload || *u.Length != *p.Length || *u.SHA != *p.SHA {
		t.Fatal("descriptor performed I/O or changed declared semantics", err)
	}
	if err := upload.Close(); err != nil || body.closes != 1 {
		t.Fatal("caller retained source ownership", err)
	}
	if _, err := decodePublicationSource(raw); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]byte{
		bytes.Replace(raw, []byte(`"format":1`), []byte(`"format":2`), 1),
		bytes.Replace(raw, []byte(`"format":1`), []byte(`"format":1,"format":1`), 1),
		append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"text":"forbidden"}`)...),
		append(append([]byte{}, raw...), []byte(`{}`)...),
	} {
		if _, err := decodePublicationSource(invalid); err == nil {
			t.Fatal("noncanonical or body-bearing descriptor accepted")
		}
	}
}

func TestPublicationBusinessDescriptorPreservesExactTypedReference(t *testing.T) {
	project := newID[id.Project](t)
	owner, err := oc.NewObjectOwner(oc.Knowledge, newID[kc.Document](t).String(), project.String())
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := oc.NewUploadReceipt(oc.ReceiptDetails{ID: newID[oc.Receipt](t), UploadID: newID[oc.Upload](t), ObjectID: newID[oc.StoredObject](t), Owner: owner, CreationCause: newID[struct{}](t).String()})
	if err != nil {
		t.Fatal(err)
	}
	refs := []oc.BusinessFileDetails{
		{Kind: oc.UploadedObject, Receipt: receipt},
		{Kind: oc.KnowledgeFile, ProjectID: project, DocumentID: owner.Details().ID, Revision: 9},
		{Kind: oc.ArtifactFile, ProjectID: project, ArtifactID: newID[struct{}](t).String(), FileID: newID[struct{}](t).String()},
		{Kind: oc.ExecutionFile, ProjectID: project, ExecutionID: newID[struct{}](t).String(), PayloadID: newID[struct{}](t).String()},
	}
	for _, d := range refs {
		ref, err := oc.NewBusinessFileRef(d)
		if err != nil {
			t.Fatal(err)
		}
		source, err := kc.NewBusinessSource(ref)
		if err != nil {
			t.Fatal(err)
		}
		p, err := describePublicationSource(source)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decodePublicationSource(raw)
		if err != nil {
			t.Fatal(err)
		}
		back, err := decoded.Business.reference()
		if err != nil {
			t.Fatal(err)
		}
		actual := back.Details()
		if actual.Kind != d.Kind || actual.ProjectID != d.ProjectID || actual.Revision != d.Revision || actual.DocumentID != d.DocumentID || actual.ArtifactID != d.ArtifactID || actual.FileID != d.FileID || actual.ExecutionID != d.ExecutionID || actual.PayloadID != d.PayloadID {
			t.Fatal("business reference drifted")
		}
		if d.Kind == oc.UploadedObject {
			got, want := actual.Receipt.Details(), d.Receipt.Details()
			if got.ID != want.ID || got.UploadID != want.UploadID || got.ObjectID != want.ObjectID || got.CreationCause != want.CreationCause || !got.Owner.Equal(want.Owner) {
				t.Fatal("original upload receipt identity drifted")
			}
		}
		origin, err := decoded.sourceProject()
		if err != nil || origin == nil || *origin != project {
			t.Fatal("durable source-project registration omitted", err)
		}
		decoded.Business.Execution = newID[struct{}](t).String()
		if d.Kind != oc.ExecutionFile && decoded.validate() == nil {
			t.Fatal("foreign variant field accepted")
		}
	}
}

type publicationProcesses struct {
	current ob.ProcessID
	calls   []ob.ProcessID
	err     error
}

func (p *publicationProcesses) CurrentProcess() ob.ProcessID { return p.current }
func (p *publicationProcesses) ConfirmStopped(_ context.Context, process ob.ProcessID) error {
	p.calls = append(p.calls, process)
	return p.err
}

func TestPublicationTakeoverRequiresActualExactStoppedProof(t *testing.T) {
	_, actor, input := queryFixture(t)
	user, err := f.ParseID[id.User](actor.Details().UserID)
	if err != nil {
		t.Fatal(err)
	}
	record := &commandRecord{id: newID[command](t), project: input.project, document: newID[kc.Document](t), user: user, name: kc.Create, key: "publication-fence", state: kc.InProgress}
	processes := &publicationProcesses{current: newID[ob.Process](t)}
	st := &serviceState{deps: Dependencies{Processes: processes}}
	s := &Service{data: func() *serviceState { return st }}
	first, err := nextPublicationWork(record, nil, processes.current, nil, nil)
	if err != nil || first.fence != 1 || first.phase != "active" {
		t.Fatal("first registration failed", err)
	}
	if _, err = s.stoppedPublication(context.Background(), &first); err == nil || len(processes.calls) != 0 {
		t.Fatal("own live process inferred stopped", err)
	}
	previous := first
	previous.process = newID[ob.Process](t)
	processes.err = errors.New("actual guard remains held")
	if proof, err := s.stoppedPublication(context.Background(), &previous); err == nil || proof != nil || len(processes.calls) != 1 || processes.calls[0] != previous.process {
		t.Fatal("held exact guard accepted", err)
	}
	if _, err = nextPublicationWork(record, nil, processes.current, &previous, nil); err == nil {
		t.Fatal("unproven old active worker replaced")
	}
	processes.err = nil
	proof, err := s.stoppedPublication(context.Background(), &previous)
	if err != nil || proof == nil || len(processes.calls) != 2 || processes.calls[1] != previous.process {
		t.Fatal("real stopped check not consumed", err)
	}
	next, err := nextPublicationWork(record, nil, processes.current, &previous, proof)
	if err != nil || next.fence != previous.fence+1 || next.attempt == previous.attempt || next.process != processes.current {
		t.Fatal("takeover did not fence original attempt", err)
	}
	for _, change := range []func(*publicationWork){
		func(w *publicationWork) { w.fence++ },
		func(w *publicationWork) { w.attempt = newID[publicationAttempt](t) },
		func(w *publicationWork) { w.process = newID[ob.Process](t) },
		func(w *publicationWork) { w.project = newID[id.Project](t) },
		func(w *publicationWork) { w.command = newID[command](t) },
	} {
		changed := previous
		change(&changed)
		if _, err := nextPublicationWork(record, nil, processes.current, &changed, proof); err == nil {
			t.Fatal("stale or unrelated stopped proof accepted")
		}
	}
	joined := first
	joined.phase = "joined"
	if proof, err := s.stoppedPublication(context.Background(), &joined); err != nil || proof != nil || len(processes.calls) != 2 {
		t.Fatal("joined original work queried process guard", err)
	}
	retry, err := nextPublicationWork(record, nil, processes.current, &joined, nil)
	if err != nil || retry.fence != 2 || retry.attempt == joined.attempt {
		t.Fatal("joined same-process work could not be safely retried", err)
	}
	origin := newID[id.Project](t)
	if _, err := nextPublicationWork(record, &origin, processes.current, &joined, nil); err == nil {
		t.Fatal("recovery silently changed source-project registration")
	}
	locks, err := publicationLocks(actor, record, &origin)
	if err != nil {
		t.Fatal(err)
	}
	global, err := f.RecordLock(f.ReferenceRecordLock, "knowledge:document:"+record.document.String())
	if err != nil {
		t.Fatal(err)
	}
	foundGlobal := false
	for _, lock := range locks {
		if lock.Key.Canonical() == global.Canonical() && lock.Mode == f.Exclusive {
			foundGlobal = true
		}
	}
	if !foundGlobal {
		t.Fatal("create claim omitted global document collision lock")
	}
	wanted, _ := scopeLocks(actor, origin, false)
	for _, want := range wanted {
		found := false
		for _, got := range locks {
			if got.Key.Canonical() == want.Key.Canonical() && (got.Mode == want.Mode || got.Mode == f.Exclusive) {
				found = true
			}
		}
		if !found {
			t.Fatal("cross-project source missing from initial full union")
		}
	}
}
