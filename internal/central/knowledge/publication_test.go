package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5/pgconn"
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

func retirementFixture(t *testing.T) (*Service, *commandRecord, publicationWork, func()) {
	t.Helper()
	processes := &publicationProcesses{current: newID[ob.Process](t)}
	st := &serviceState{deps: Dependencies{Processes: processes}, calls: map[*call]struct{}{}, changed: make(chan struct{})}
	s := &Service{data: func() *serviceState { return st }}
	_, done, err := s.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	record := &commandRecord{id: newID[command](t), project: newID[id.Project](t), document: newID[kc.Document](t), state: kc.InProgress}
	work, err := nextPublicationWork(record, nil, processes.current, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s, record, work, done
}

func TestPublicationRetirementWaitsActualCloseBeforeLocalTakeover(t *testing.T) {
	s, record, work, done := retirementFixture(t)
	retirement, err := s.publicationRetirement(work, done)
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	if err = retirement.own(func() error { close(started); <-release; return nil }); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- retirement.join() }()
	<-started
	s.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err = s.Drain(ctx); err != context.DeadlineExceeded {
		t.Fatal("cancel or close entry admitted premature Drain", err)
	}
	if proof, err := s.stoppedPublication(context.Background(), &work); err == nil || proof != nil {
		t.Fatal("in-flight close admitted local takeover")
	}
	once.Do(func() { close(release) })
	if err = <-result; err != nil {
		t.Fatal(err)
	}
	if err = s.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	proof, err := s.stoppedPublication(context.Background(), &work)
	if err != nil || proof == nil || !proof.joinedLocal {
		t.Fatal("actual joined same-process proof missing", err)
	}
	next, err := nextPublicationWork(record, nil, work.process, &work, proof)
	if err != nil || next.fence != work.fence+1 || next.attempt == work.attempt {
		t.Fatal("same-process retry failed after actual resource join", err)
	}
	if err = retirement.join(); err != nil {
		t.Fatal("retirement repeated a successful close", err)
	}
	if err = retirement.own(func() error { return nil }); err == nil {
		t.Fatal("resource admitted after retirement")
	}
	for _, changed := range []publicationWork{
		func() publicationWork { v := work; v.fence++; return v }(),
		func() publicationWork { v := work; v.attempt = newID[publicationAttempt](t); return v }(),
		func() publicationWork { v := work; v.process = newID[ob.Process](t); return v }(),
	} {
		if _, err = nextPublicationWork(record, nil, work.process, &changed, proof); err == nil {
			t.Fatal("joined proof admitted another attempt/process/fence")
		}
	}
}

func TestPublicationRetirementCloseFailureRetainsCallAndExactEvidence(t *testing.T) {
	s, _, work, done := retirementFixture(t)
	retirement, err := s.publicationRetirement(work, done)
	if err != nil {
		t.Fatal(err)
	}
	order := []string{}
	fail := true
	if err = retirement.own(func() error { order = append(order, "source"); return nil }); err != nil {
		t.Fatal(err)
	}
	if err = retirement.own(func() error {
		order = append(order, "spool")
		if fail {
			return fault(f.ResourceBusy)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err = retirement.join(); err == nil || strings.Join(order, ",") != "spool,source" {
		t.Fatal("failed close skipped other resources or retired", err, order)
	}
	if proof, err := s.stoppedPublication(context.Background(), &work); err == nil || proof != nil {
		t.Fatal("failed close produced joined proof")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = s.Drain(ctx); err != context.Canceled {
		t.Fatal("failed close removed actual call", err)
	}
	fail = false
	if err = retirement.join(); err != nil || strings.Join(order, ",") != "spool,source,spool" {
		t.Fatal("close retry repeated successful resources", err, order)
	}
	if err = s.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPublicationRetirementOldFinalizerCannotReplaceNewJoinedFence(t *testing.T) {
	s, _, work, done := retirementFixture(t)
	origin := newID[id.Project](t)
	work.source = &origin
	old, err := s.publicationRetirement(work, done)
	if err != nil {
		t.Fatal(err)
	}
	newWork := work.clone()
	newWork.fence++
	newWork.attempt = newID[publicationAttempt](t)
	newRetirement, err := s.publicationRetirement(newWork, func() {})
	if err != nil {
		t.Fatal(err)
	}
	if err = newRetirement.join(); err != nil {
		t.Fatal(err)
	}
	origin = newID[id.Project](t)
	if err = old.join(); err != nil {
		t.Fatal(err)
	}
	proof, err := s.stoppedPublication(context.Background(), &newWork)
	if err != nil || proof == nil || !proof.original.equal(newWork) {
		t.Fatal("late finalizer replaced newer proof or source pointer aliased", err)
	}
	if proof, err := s.stoppedPublication(context.Background(), &work); err == nil || proof != nil {
		t.Fatal("mutated/older work obtained newer proof")
	}
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

type publicationCheckpointRow func(...any) error

func (r publicationCheckpointRow) Scan(values ...any) error { return r(values...) }

type publicationCheckpointStore struct {
	Store
	tx                       f.Tx
	record                   *commandRecord
	work                     publicationWork
	unknown                  bool
	attempt                  f.ID[f.TransactionAttempt]
	cause                    f.TransactionCause
	calls, acquires, updates int
}

func (s *publicationCheckpointStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.calls++
	s.cause = cause
	s.tx = f.NewTx()
	if err := fn(ctx, s.tx); err != nil {
		var fault *f.Fault
		if !errors.As(err, &fault) {
			panic("controlled callback returned non-Fault")
		}
		return f.NotCommittedResult(fault)
	}
	if s.unknown {
		return f.UnknownResult(s.attempt, cause)
	}
	return f.CommittedResult()
}
func (s *publicationCheckpointStore) AcquireAll(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if tx != s.tx || len(locks) < 4 {
		return errors.New("foreign transaction or incomplete union")
	}
	s.acquires++
	return nil
}
func (s *publicationCheckpointStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, errors.New("foreign transaction")
	}
	return s, nil
}
func (s *publicationCheckpointStore) QueryRow(_ context.Context, sql string, _ ...any) postgres.Row {
	return publicationCheckpointRow(func(v ...any) error {
		if strings.Contains(sql, "FROM agenteam_knowledge.commands") {
			r := s.record
			*v[0].(*string), *v[1].(*string), *v[2].(*string), *v[3].(*string) = r.id.String(), r.project.String(), r.document.String(), r.user.String()
			*v[4].(*string), *v[5].(*string), *v[6].(*string), *v[7].(*string) = string(r.name), string(r.key), r.digest.String(), "planned"
			*v[8].(*[]byte), *v[9].(*time.Time), *v[10].(**time.Time) = nil, r.created.Time(), nil
			return nil
		}
		if strings.Contains(sql, "FROM agenteam_knowledge.work_claims") {
			w := s.work
			*v[0].(*string), *v[1].(*string), *v[2].(**string) = w.command.String(), w.project.String(), nil
			*v[3].(*string), *v[4].(*string), *v[5].(*int64), *v[6].(*string) = w.process.String(), w.attempt.String(), w.fence, w.phase
			return nil
		}
		return errors.New("unexpected SQL")
	})
}
func (s *publicationCheckpointStore) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if !strings.Contains(sql, "SET phase='joined'") || len(args) != 4 || args[0] != s.work.command.String() || args[1] != s.work.process.String() || args[2] != s.work.attempt.String() || args[3] != s.work.fence {
		return pgconn.CommandTag{}, errors.New("unbound checkpoint")
	}
	s.updates++
	s.work.phase = "joined"
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func TestPublicationJoinCheckpointKeepsProofAcrossUnknownAndChecksOriginalCommand(t *testing.T) {
	s, record, work, done := retirementFixture(t)
	_, actor, _ := queryFixture(t)
	record.user, _ = f.ParseID[id.User](actor.Details().UserID)
	record.name, record.key, record.digest = kc.Create, "original-publication-key", ob.DigestBytes([]byte("original intent"))
	record.created, _ = f.NewInstant(time.Now())
	store := &publicationCheckpointStore{record: record, work: work, unknown: true, attempt: newID[f.TransactionAttempt](t)}
	s.state().store = store
	if err := s.checkpointPublicationJoin(context.Background(), actor, record, work); err == nil || store.calls != 0 {
		t.Fatal("absence of local join proof opened SQL")
	}
	retirement, err := s.publicationRetirement(work, done)
	if err != nil {
		t.Fatal(err)
	}
	if err = retirement.join(); err != nil {
		t.Fatal(err)
	}
	wrong := *record
	wrong.id = newID[command](t)
	store.record = &wrong
	if err = s.checkpointPublicationJoin(context.Background(), actor, record, work); err == nil || store.updates != 0 {
		t.Fatal("foreign original command checkpointed work")
	}
	store.record = record
	err = s.checkpointPublicationJoin(context.Background(), actor, record, work)
	var original commitFailure
	if !errors.As(err, &original) || original.result.State() != f.Unknown || original.result.AttemptID() != store.attempt || original.result.Cause().Details().Primary.Canonical() != store.cause.Details().Primary.Canonical() {
		t.Fatal("checkpoint Unknown lost exact physical attempt/cause", err)
	}
	if store.updates != 1 || store.calls != 2 || store.acquires != 2 {
		t.Fatal("checkpoint retried callback or took locks more than once")
	}
	proof, err := s.stoppedPublication(context.Background(), &work)
	if err != nil || proof == nil {
		t.Fatal("Unknown removed actual join proof", err)
	}
	store.unknown = false
	if err = s.checkpointPublicationJoin(context.Background(), actor, record, work); err != nil || store.updates != 1 {
		t.Fatal("committed checkpoint could not be safely confirmed", err)
	}
	if _, exists := s.state().joinedPublications[work.command]; exists {
		t.Fatal("confirmed durable join retained unnecessary memory proof")
	}
	if _, err = nextPublicationWork(record, nil, work.process, &store.work, nil); err != nil {
		t.Fatal("durable joined attempt could not be retried", err)
	}
}
