package knowledge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	objectimpl "github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type publicationChunkBody struct {
	data                    []byte
	stride                  int
	position, reads, closes int
	closeErr                error
}

func (b *publicationChunkBody) Read(p []byte) (int, error) {
	b.reads++
	if b.position == len(b.data) {
		return 0, io.EOF
	}
	n := min(len(p), b.stride, len(b.data)-b.position)
	copy(p, b.data[b.position:b.position+n])
	b.position += n
	return n, nil
}
func (b *publicationChunkBody) Close() error { b.closes++; return b.closeErr }

func TestPublicationReaderUTF8AndExactMeasuredInput(t *testing.T) {
	for _, body := range []string{"", "plain\r\ntext", "\x00\ufeffé中🙂\ufffd", "e\u0301"} {
		for stride := 1; stride <= 5; stride++ {
			raw := &publicationChunkBody{data: []byte(body), stride: stride}
			reader := newPublicationReader(raw, true, int64(len(body)))
			got, err := io.ReadAll(reader)
			if err != nil || !bytes.Equal(got, []byte(body)) {
				t.Fatalf("valid text changed at stride %d: %v", stride, err)
			}
			source, err := kc.NewTextSource(kc.PlainText, body)
			if err != nil {
				t.Fatal(err)
			}
			descriptor, err := describePublicationSource(source)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := oc.NewPreparedPayload(oc.PreparedDetails{ID: newID[oc.Payload](t), MediaType: kc.PlainText, Length: int64(len(body)), SHA256: *descriptor.SHA})
			if err != nil || !reader.measured(prepared, descriptor) {
				t.Fatal("actual byte measurement did not match", err)
			}
			if reader.Close() != nil || reader.Close() != nil || raw.closes != 1 {
				t.Fatal("source was not closed exactly once")
			}
		}
	}
	for _, body := range [][]byte{{0x80}, {0xc0, 0x80}, {0xed, 0xa0, 0x80}, {0xf4, 0x90, 0x80, 0x80}, {0xe4, 0xb8}, {'a', 0xff, 'b'}} {
		for stride := 1; stride <= len(body); stride++ {
			reader := newPublicationReader(&publicationChunkBody{data: body, stride: stride}, true, int64(len(body)))
			var known *f.Fault
			if _, err := io.ReadAll(reader); !errors.As(err, &known) || known.Code != f.InvalidArgument {
				t.Fatalf("invalid UTF-8 accepted at stride %d: %v", stride, err)
			}
		}
		reader := newPublicationReader(&publicationChunkBody{data: body, stride: 1}, false, int64(len(body)))
		if got, err := io.ReadAll(reader); err != nil || !bytes.Equal(got, body) {
			t.Fatal("binary source was decoded as text", err)
		}
	}
	for _, length := range []int64{2, 4} {
		reader := newPublicationReader(&publicationChunkBody{data: []byte("abc"), stride: 1}, true, length)
		var known *f.Fault
		if _, err := io.ReadAll(reader); !errors.As(err, &known) || known.Code != f.InvalidArgument {
			t.Fatal("incorrect declared length accepted", err)
		}
	}
}

type publicationBlockingBody struct {
	started, stopped chan struct{}
	once             sync.Once
	failure          error
}

func (b *publicationBlockingBody) Read([]byte) (int, error) {
	close(b.started)
	<-b.stopped
	return 0, b.failure
}
func (b *publicationBlockingBody) Close() error {
	b.once.Do(func() { close(b.stopped) })
	return b.failure
}

func TestPublicationReaderCloseInterruptsReadAndRetainsFailure(t *testing.T) {
	wanted := errors.New("private source failure")
	body := &publicationBlockingBody{started: make(chan struct{}), stopped: make(chan struct{}), failure: wanted}
	reader := newPublicationReader(body, true, 1)
	readDone := make(chan error, 1)
	go func() { var p [1]byte; _, err := reader.Read(p[:]); readDone <- err }()
	<-body.started
	if reader.Close() != wanted || reader.Close() != wanted {
		t.Fatal("original Close error changed")
	}
	select {
	case err := <-readDone:
		if err != wanted {
			t.Fatal("original Read error changed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close waited on the blocked Read lock")
	}
}

type publicationPreparePort struct {
	oc.Uploads
	prepare         func(context.Context, id.Actor, oc.ObjectOwner, string, int64, *f.Digest, io.ReadCloser) (oc.PreparedPayload, error)
	calls, discards int
	discarded       oc.PayloadID
	discard         func(oc.PreparedPayload) error
}

func (p *publicationPreparePort) PreparePayload(ctx context.Context, actor id.Actor, owner oc.ObjectOwner, media string, length int64, digest *f.Digest, body io.ReadCloser) (oc.PreparedPayload, error) {
	p.calls++
	return p.prepare(ctx, actor, owner, media, length, digest, body)
}
func (p *publicationPreparePort) DiscardPrepared(prepared oc.PreparedPayload) error {
	p.discards++
	p.discarded = prepared.Details().ID
	if p.discard != nil {
		return p.discard(prepared)
	}
	return nil
}

func TestDirectPublicationUsesActualD05MeasuredSpool(t *testing.T) {
	for _, data := range [][]byte{[]byte("\ufeffé中🙂\r\n"), {0xe4, 0xb8}} {
		t.Run(fmt.Sprintf("bytes-%d", len(data)), func(t *testing.T) {
			sum := sha256.Sum256(data)
			source, err := kc.NewUploadSource(kc.Markdown, f.Progress(len(data)), f.Digest("sha256:"+hex.EncodeToString(sum[:])), &publicationChunkBody{data: data, stride: 1})
			if err != nil {
				t.Fatal(err)
			}
			s, input, intent, work, retirement, port := directPublicationFixture(t, source)
			path := filepath.Join(t.TempDir(), "owned-spool")
			process := newID[oc.Process](t)
			spool, err := objectimpl.OpenSpool(path, process)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := spool.Close(); err != nil {
					t.Error(err)
				}
			})
			port.prepare = func(ctx context.Context, _ id.Actor, _ oc.ObjectOwner, media string, length int64, digest *f.Digest, body io.ReadCloser) (oc.PreparedPayload, error) {
				return spool.Prepare(ctx, media, length, digest, body)
			}
			port.discard = spool.Discard
			prepared, err := s.prepareDirectPublication(context.Background(), input, intent, source, work, retirement)
			if len(data) == 2 {
				if err == nil || prepared.Validate() == nil {
					t.Fatal("actual spool accepted truncated UTF-8")
				}
			} else if err != nil || prepared.Validate() != nil {
				t.Fatal("actual measured preparation failed", err)
			}
			if err := retirement.join(); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(path)
			if err != nil || len(entries) != 1 || entries[0].Name() != ".object.lock" {
				t.Fatal("actual owned spool files remain after retirement", err)
			}
		})
	}
}

func directPublicationFixture(t *testing.T, source kc.SourceInput) (*Service, contentInput, contentIntent, publicationWork, *publicationRetirement, *publicationPreparePort) {
	t.Helper()
	s, record, work, done := retirementFixture(t)
	_, actor, _ := queryFixture(t)
	meta := f.CommandMeta{RequestID: newID[f.Request](t), IdempotencyKey: "source-command"}
	input, err := createContentInput(actor, meta, kc.CreateRequest{ProjectID: work.project, DocumentID: record.document, Title: "Original"}, source)
	if err != nil {
		t.Fatal(err)
	}
	user, err := f.ParseID[id.User](actor.Details().UserID)
	if err != nil {
		t.Fatal(err)
	}
	record.key, record.digest, record.user, record.name = meta.IdempotencyKey, input.digest, user, kc.Create
	retirement, err := s.publicationRetirement(work, done)
	if err != nil {
		t.Fatal(err)
	}
	port := &publicationPreparePort{}
	s.state().deps.Uploads = port
	return s, input, contentIntent{record: record, request: input.request}, work, retirement, port
}

func TestDirectPublicationValidatesProviderAndRetiresOwnedResources(t *testing.T) {
	for _, mode := range []string{"valid", "early-return", "wrong-digest", "wrong-media", "error-with-handle", "close-error"} {
		t.Run(mode, func(t *testing.T) {
			text := []byte("é🙂\r\n")
			sum := sha256.Sum256(text)
			digest := f.Digest("sha256:" + hex.EncodeToString(sum[:]))
			body := &publicationChunkBody{data: text, stride: 1}
			closeFailure := errors.New("private close failure")
			if mode == "close-error" {
				body.closeErr = closeFailure
			}
			source, err := kc.NewUploadSource(kc.Markdown, f.Progress(len(text)), digest, body)
			if err != nil {
				t.Fatal(err)
			}
			s, input, intent, work, retirement, port := directPublicationFixture(t, source)
			preparedID := newID[oc.Payload](t)
			failure := fault(f.CommitUnknown)
			port.prepare = func(_ context.Context, actor id.Actor, owner oc.ObjectOwner, media string, length int64, expected *f.Digest, reader io.ReadCloser) (oc.PreparedPayload, error) {
				if !actor.Equal(input.actor) || owner.Details().ID != input.document.String() || owner.Details().ProjectID != input.project.String() || media != kc.Markdown || length != int64(len(text)) || expected == nil || *expected != digest {
					t.Fatal("preparation arguments changed")
				}
				if mode != "early-return" {
					if _, err := io.ReadAll(reader); err != nil {
						t.Fatal(err)
					}
				}
				_ = reader.Close()
				d := oc.PreparedDetails{ID: preparedID, MediaType: media, Length: length, SHA256: digest}
				if mode == "wrong-media" {
					d.MediaType = kc.PDF
				}
				if mode == "wrong-digest" {
					d.SHA256 = f.Digest("sha256:" + strings.Repeat("0", 64))
				}
				out, err := oc.NewPreparedPayload(d)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "error-with-handle" {
					return out, failure
				}
				return out, nil
			}
			prepared, err := s.prepareDirectPublication(context.Background(), input, intent, source, work, retirement)
			if mode == "valid" {
				if err != nil || prepared.Details().ID != preparedID {
					t.Fatal("valid preparation failed", err)
				}
			} else if err == nil {
				t.Fatal("invalid provider preparation accepted")
			}
			if mode == "error-with-handle" && err != failure {
				t.Fatal("original Fault identity lost")
			}
			if mode == "close-error" && !errors.Is(err, closeFailure) {
				t.Fatal("ignored provider Close failure lost")
			}
			joinErr := retirement.join()
			if (joinErr != nil) != (mode == "close-error") {
				t.Fatal("incorrect resource retirement", joinErr)
			}
			if port.calls != 1 || port.discards != 1 || port.discarded != preparedID || body.closes != 1 {
				t.Fatal("owned handle/body not retired exactly once")
			}
			if mode == "close-error" && len(s.state().calls) != 1 {
				t.Fatal("failed close faked runtime join")
			}
		})
	}
}

func TestDirectPublicationRejectsIdentityBeforeTakingSource(t *testing.T) {
	for _, mode := range []string{"different-command", "different-document", "different-digest", "different-source", "different-origin", "joined", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			body := &publicationChunkBody{data: []byte("body"), stride: 1}
			sum := sha256.Sum256(body.data)
			source, err := kc.NewUploadSource(kc.PlainText, 4, f.Digest("sha256:"+hex.EncodeToString(sum[:])), body)
			if err != nil {
				t.Fatal(err)
			}
			s, input, intent, work, retirement, port := directPublicationFixture(t, source)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "different-command":
				work.command = newID[command](t)
			case "different-document":
				input.document = newID[kc.Document](t)
			case "different-digest":
				input.digest = f.Digest("sha256:" + strings.Repeat("0", 64))
			case "different-source":
				length := f.Progress(5)
				input.request.Source.Length = &length
			case "different-origin":
				origin := newID[id.Project](t)
				work.source = &origin
				retirement.work = work.clone()
			case "joined":
				if err := retirement.join(); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				cancel()
			}
			if _, err := s.prepareDirectPublication(ctx, input, intent, source, work, retirement); err == nil {
				t.Fatal("invalid work/source admitted")
			}
			if port.calls != 0 || body.reads != 0 || body.closes != 0 {
				t.Fatal("rejected input performed I/O")
			}
			if _, err := source.TakeUploadBody(); err != nil {
				t.Fatal("rejected input consumed upload ownership", err)
			}
			_ = source.Close()
			if err := retirement.join(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

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

type publicationClaimStore struct {
	publicationCheckpointStore
	request, header []byte
	active          bool
	inserts         int
	drift           bool
}

func (s *publicationClaimStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.calls++
	s.cause = cause
	s.tx = f.NewTx()
	s.active = true
	defer func() { s.active = false }()
	if s.calls == 2 && s.drift {
		s.work.fence++
	}
	if err := fn(ctx, s.tx); err != nil {
		var known *f.Fault
		if !errors.As(err, &known) {
			panic("controlled non-Fault")
		}
		return f.NotCommittedResult(known)
	}
	if s.calls == 2 && s.unknown {
		return f.UnknownResult(s.attempt, cause)
	}
	return f.CommittedResult()
}
func (s *publicationClaimStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx || !s.active {
		return nil, errors.New("foreign/ended Tx")
	}
	return s, nil
}
func (s *publicationClaimStore) RequireHeldLocks(_ context.Context, tx f.Tx, _ []f.LockRequest) error {
	if tx != s.tx || !s.active || s.acquires != s.calls {
		return errors.New("not already held")
	}
	return nil
}
func (s *publicationClaimStore) QueryRow(ctx context.Context, sql string, args ...any) postgres.Row {
	if strings.HasPrefix(sql, "SELECT request,plan") {
		return publicationCheckpointRow(func(v ...any) error { *v[0].(*[]byte), *v[1].(*[]byte) = s.request, s.header; return nil })
	}
	if strings.Contains(sql, "FROM agenteam_knowledge.work_claims") && s.work.command.Validate() != nil {
		return publicationCheckpointRow(func(...any) error { return pgx.ErrNoRows })
	}
	return s.publicationCheckpointStore.QueryRow(ctx, sql, args...)
}
func (s *publicationClaimStore) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if !s.active || s.acquires != s.calls || !strings.HasPrefix(sql, "INSERT INTO agenteam_knowledge.work_claims") || len(args) != 6 {
		return pgconn.CommandTag{}, errors.New("unexpected claim write")
	}
	var err error
	if s.work.command, err = f.ParseID[command](args[0].(string)); err != nil {
		return pgconn.CommandTag{}, err
	}
	if s.work.project, err = f.ParseID[id.Project](args[1].(string)); err != nil {
		return pgconn.CommandTag{}, err
	}
	if args[2] != nil {
		return pgconn.CommandTag{}, errors.New("unexpected source Project")
	}
	if s.work.process, err = f.ParseID[ob.Process](args[3].(string)); err != nil {
		return pgconn.CommandTag{}, err
	}
	if s.work.attempt, err = f.ParseID[publicationAttempt](args[4].(string)); err != nil {
		return pgconn.CommandTag{}, err
	}
	s.work.fence, s.work.phase = args[5].(int64), "active"
	s.inserts++
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

type publicationOutsideProcesses struct {
	publicationProcesses
	store *publicationClaimStore
}

func (p *publicationOutsideProcesses) ConfirmStopped(ctx context.Context, process ob.ProcessID) error {
	if p.store.active {
		return errors.New("process proof requested inside Tx")
	}
	return p.publicationProcesses.ConfirmStopped(ctx, process)
}

func TestPublicationClaimRechecksStoppedProofAndRetainsUnknownIdentity(t *testing.T) {
	for _, mode := range []string{"fresh", "stopped", "live", "proof-drift", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			source, err := kc.NewTextSource(kc.PlainText, "content")
			if err != nil {
				t.Fatal(err)
			}
			s, input, intent, old, retirement, _ := directPublicationFixture(t, source)
			defer retirement.done()
			now, _ := f.NewInstant(time.Now())
			intent.record.created = now
			intent.header, err = newContentHeader(input.project, input.document, 1, now)
			if err != nil {
				t.Fatal(err)
			}
			request, _ := json.Marshal(input.request)
			header, _ := json.Marshal(intent.header)
			store := &publicationClaimStore{publicationCheckpointStore: publicationCheckpointStore{record: intent.record, attempt: newID[f.TransactionAttempt](t), unknown: mode == "unknown"}, request: request, header: header, drift: mode == "proof-drift"}
			processes := &publicationOutsideProcesses{publicationProcesses: publicationProcesses{current: old.process}, store: store}
			if mode == "stopped" || mode == "live" || mode == "proof-drift" {
				store.work = old
				store.work.process = newID[ob.Process](t)
			}
			if mode == "live" {
				processes.err = fault(f.ResourceBusy)
			}
			previous := store.work
			s.state().store, s.state().deps.Processes = store, processes
			user, _ := f.ParseID[id.User](input.actor.Details().UserID)
			grant, err := pc.NewProjectAccess(input.actor, pc.ProjectRef{ID: input.project, OwnerUserID: user, Name: "Current", NormalizedName: "current", Lifecycle: pc.Active, Version: 1, CreatedAt: now, UpdatedAt: now}, now)
			if err != nil {
				t.Fatal(err)
			}
			s.state().deps.Projects = &ownerGate{grant: grant}
			work, replay, err := s.claimContentPublication(context.Background(), input, intent)
			if replay != nil || store.active {
				t.Fatal("claim escaped with a live transaction or unexpected replay")
			}
			if mode == "live" || mode == "proof-drift" {
				if err == nil || store.inserts != 0 {
					t.Fatal("live or changed original worker was replaced", err)
				}
				return
			}
			if mode == "unknown" {
				var original commitFailure
				if !errors.As(err, &original) || original.result.AttemptID() != store.attempt || original.result.Cause().Details().Primary.Canonical() != store.cause.Details().Primary.Canonical() {
					t.Fatal("Unknown lost original physical cause", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if work.command != intent.record.id || work.project != input.project || work.process != old.process || work.phase != "active" || work.attempt.Validate() != nil || store.inserts != 1 || store.calls != 2 || store.acquires != 2 {
				t.Fatal("claim omitted exact binding or repeated the lock/commit")
			}
			if mode == "stopped" && (len(processes.calls) != 1 || processes.calls[0] != previous.process || work.fence != previous.fence+1 || work.attempt == previous.attempt) {
				t.Fatal("stopped proof did not fence the original attempt")
			}
		})
	}
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
