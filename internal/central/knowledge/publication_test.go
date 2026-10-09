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

func TestPublicationMeasurementRequiresExactMeasuredWire(t *testing.T) {
	m := publicationMeasurement{Media: kc.PlainText, Length: 0, SHA: ob.DigestBytes(nil)}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodePublicationMeasurement(raw)
	if err != nil || got != m {
		t.Fatal("empty body measurement rejected", err)
	}
	for _, invalid := range [][]byte{
		bytes.Replace(raw, []byte(`"length":"0",`), nil, 1),
		bytes.Replace(raw, []byte(`"length":"0"`), []byte(`"length":null`), 1),
		bytes.Replace(raw, []byte(`"length":"0"`), []byte(`"length":"0","length":"0"`), 1),
		bytes.Replace(raw, []byte(`"length":"0"`), []byte(`"length":"1073741825"`), 1),
		bytes.Replace(raw, []byte(kc.PlainText), []byte("application/octet-stream"), 1),
		append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"payload":"forbidden"}`)...),
		append(append([]byte{}, raw...), []byte(`{}`)...),
		[]byte(`null`),
	} {
		if bytes.Equal(raw, invalid) {
			t.Fatal("negative stimulus did not change input")
		}
		if _, err := decodePublicationMeasurement(invalid); err == nil {
			t.Fatal("missing, ambiguous or unmeasured wire accepted")
		}
	}
}

type publicationReservationExecutor struct {
	postgres.SQLExecutor
	record                   *commandRecord
	source, measurement      []byte
	object, upload, attempt  *string
	phase, project, document string
}

type publicationTransferStore struct {
	publicationClaimStore
	reservation        publicationReservationExecutor
	updates, unknownAt int
}

func (s *publicationTransferStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	result := s.publicationClaimStore.WithinTx(ctx, cause, fn)
	if result.State() == f.Committed && s.calls == s.unknownAt {
		return f.UnknownResult(s.attempt, cause)
	}
	return result
}
func (s *publicationTransferStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.publicationClaimStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *publicationTransferStore) QueryRow(ctx context.Context, sql string, args ...any) postgres.Row {
	if strings.Contains(sql, "FROM agenteam_knowledge.publications") {
		return s.reservation.QueryRow(ctx, sql, args...)
	}
	return s.publicationClaimStore.QueryRow(ctx, sql, args...)
}
func (s *publicationTransferStore) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if !s.active || s.acquires != s.calls || len(args) == 0 || args[0] != s.record.id.String() {
		return pgconn.CommandTag{}, errors.New("write outside original lock/command")
	}
	if strings.Contains(sql, "SET object_id=$2,upload_id=$3,attempt_id=$4") && len(args) == 5 {
		object, upload, attempt := args[1].(string), args[2].(string), args[3].(string)
		s.reservation.object, s.reservation.upload, s.reservation.attempt = &object, &upload, &attempt
		s.reservation.measurement, s.reservation.phase = args[4].([]byte), "reserved"
	} else if strings.Contains(sql, "SET phase='uploaded'") && len(args) == 1 {
		s.reservation.phase = "uploaded"
	} else {
		return pgconn.CommandTag{}, errors.New("unexpected transfer checkpoint")
	}
	s.updates++
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type publicationTransferPort struct {
	oc.Uploads
	t               *testing.T
	store           *publicationTransferStore
	input           contentInput
	issuer          oc.AccessIssuer
	request         oc.AccessRequest
	attempt         oc.UploadAttempt
	reserveErr      error
	sendErr         error
	wrongResult     bool
	reserves, sends int
}

func (p *publicationTransferPort) DiscoverAccess(_ context.Context, request oc.AccessRequest) (oc.AccessLockPlan, error) {
	if p.store.active {
		p.t.Fatal("discovery ran inside transaction")
	}
	d := request.Details()
	if d.Operation != oc.ReserveAccess || !d.Actor.Equal(p.input.actor) || d.Owner.Details().Kind != oc.Knowledge || d.Owner.Details().ID != p.input.document.String() || d.Owner.Details().ProjectID != p.input.project.String() || d.Command == nil || d.Command.IdempotencyKey != f.IdempotencyKey(p.store.record.id.String()) || d.Command.RequestID != p.input.meta.RequestID || d.Command.ExpectedVersion != nil {
		p.t.Fatal("reserve discovery lost original owner/private command mapping")
	}
	p.request = request
	key, _ := f.AggregateLock(f.ObjectAggregate, p.attempt.Details().ObjectID.String())
	locks := []f.LockRequest{{Key: key, Mode: f.Exclusive}}
	dependencies, err := oc.NewAccessDependencies(ob.DigestBytes([]byte("controlled exact object mapping")), locks)
	if err != nil {
		return oc.AccessLockPlan{}, err
	}
	return oc.NewAccessLockPlan(p.issuer, oc.AccessPlanDetails{Request: request, DependencyRequest: request, Dependencies: dependencies, DomainBinding: ob.DigestBytes([]byte("controlled original upload")), Objects: []oc.ObjectID{p.attempt.Details().ObjectID}, Locks: locks})
}
func (p *publicationTransferPort) AcquireAccessPlansInTx(ctx context.Context, tx f.Tx, plans []oc.AccessLockPlan, extra []f.LockRequest) (oc.LockedAccess, error) {
	if len(plans) != 1 || !plans[0].Details().Request.Equal(p.request) {
		p.t.Fatal("plan identity changed before acquire")
	}
	locked, err := oc.NewLockedAccess(p.issuer, tx, plans, extra)
	if err != nil {
		return locked, err
	}
	return locked, p.store.AcquireAll(ctx, tx, locked.Locks())
}
func (p *publicationTransferPort) ReserveUploadInTx(_ context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, meta f.CommandMeta, prepared oc.PreparedPayload, plan oc.AccessLockPlan, locked oc.LockedAccess) (oc.UploadAttempt, error) {
	p.reserves++
	if !p.store.active || !locked.Matches(p.issuer, tx, plan, p.request) || !actor.Equal(p.input.actor) || !owner.Equal(p.request.Details().Owner) || meta.IdempotencyKey != f.IdempotencyKey(p.store.record.id.String()) || meta.ExpectedVersion != nil || prepared.Details() != p.request.Details().Prepared.Details() {
		p.t.Fatal("reserve lost original Tx/plan/body/owner")
	}
	return p.attempt, p.reserveErr
}
func (p *publicationTransferPort) UploadPrepared(_ context.Context, actor id.Actor, owner oc.ObjectOwner, prepared oc.PreparedPayload, attempt oc.UploadAttempt) (oc.UploadAttempt, error) {
	p.sends++
	if p.store.active || !actor.Equal(p.input.actor) || !owner.Equal(p.request.Details().Owner) || prepared.Details() != p.request.Details().Prepared.Details() || attempt.Details() != p.attempt.Details() {
		p.t.Fatal("send lost exact reservation or ran inside Tx")
	}
	if p.wrongResult {
		d := p.attempt.Details()
		d.ID = newID[oc.Attempt](p.t)
		return oc.NewUploadAttempt(d)
	}
	return attempt, p.sendErr
}

func TestPublicationReserveAndSendKeepPhysicalCommitAndAttempt(t *testing.T) {
	for _, mode := range []string{"complete", "reserve unknown", "reserve error", "send error", "send wrong attempt", "checkpoint unknown", "stale owner", "work drift", "measured drift"} {
		t.Run(mode, func(t *testing.T) {
			source, _ := kc.NewTextSource(kc.PlainText, "original")
			s, input, intent, work, retirement, _ := directPublicationFixture(t, source)
			defer retirement.done()
			now, _ := f.NewInstant(time.Now())
			intent.record.created = now
			intent.header, _ = newContentHeader(input.project, input.document, 1, now)
			request, _ := json.Marshal(input.request)
			header, _ := json.Marshal(intent.header)
			store := &publicationTransferStore{publicationClaimStore: publicationClaimStore{publicationCheckpointStore: publicationCheckpointStore{record: intent.record, work: work, attempt: newID[f.TransactionAttempt](t)}, request: request, header: header}}
			rawSource, _ := json.Marshal(input.request.Source)
			store.reservation = publicationReservationExecutor{record: intent.record, source: rawSource, project: input.project.String(), document: input.document.String(), phase: "planned"}
			user, _ := f.ParseID[id.User](input.actor.Details().UserID)
			grant, err := pc.NewProjectAccess(input.actor, pc.ProjectRef{ID: input.project, OwnerUserID: user, Name: "Current", NormalizedName: "current", Lifecycle: pc.Active, Version: 1, CreatedAt: now, UpdatedAt: now}, now)
			if err != nil {
				t.Fatal(err)
			}
			gate := &ownerGate{grant: grant}
			s.state().store, s.state().deps.Projects = store, gate
			attempt, _ := oc.NewUploadAttempt(oc.AttemptDetails{ID: newID[oc.Attempt](t), ObjectID: newID[oc.StoredObject](t), UploadID: newID[oc.Upload](t)})
			port := &publicationTransferPort{t: t, store: store, input: input, issuer: oc.NewAccessIssuer(), attempt: attempt}
			s.state().deps.Uploads = port
			d := input.request.Source
			prepared, _ := oc.NewPreparedPayload(oc.PreparedDetails{ID: newID[oc.Payload](t), MediaType: d.Media, Length: int64(*d.Length), SHA256: *d.SHA})
			originalCause := errors.New("private transfer cause")
			originalError := fault(f.DependencyUnavailable).WithCause(originalCause)
			switch mode {
			case "reserve unknown":
				store.unknownAt = 1
			case "reserve error":
				port.reserveErr = originalError
			case "stale owner":
				gate.err = fault(f.NotFound)
			case "work drift":
				store.work.fence++
			case "measured drift":
				m := publicationMeasurement{Media: d.Media, Length: *d.Length, SHA: ob.DigestBytes([]byte("changed"))}
				store.reservation.measurement, _ = json.Marshal(m)
				a := attempt.Details()
				object, upload, physical := a.ObjectID.String(), a.UploadID.String(), a.ID.String()
				store.reservation.object, store.reservation.upload, store.reservation.attempt, store.reservation.phase = &object, &upload, &physical, "reserved"
			}
			got, replay, err := s.reserveContentPublication(context.Background(), input, intent, work, prepared)
			if store.active || replay != nil {
				t.Fatal("reserve leaked transaction/replay")
			}
			if mode == "reserve unknown" {
				var unknown commitFailure
				if !errors.As(err, &unknown) || unknown.result.AttemptID() != store.attempt || unknown.result.Cause().Details().Primary.Canonical() != store.cause.Details().Primary.Canonical() || got.Details() != attempt.Details() || port.sends != 0 {
					t.Fatal("unknown reservation lost physical cause or sent payload", err)
				}
				return
			}
			if mode == "reserve error" || mode == "stale owner" || mode == "work drift" || mode == "measured drift" {
				if err == nil || store.updates != 0 || mode != "reserve error" && port.reserves != 0 {
					t.Fatal("denied reservation wrote later facts", err)
				}
				var actualFault *f.Fault
				if mode == "reserve error" && (!errors.As(err, &actualFault) || actualFault.Code != originalError.Code || !errors.Is(err, originalCause)) {
					t.Fatal("reserve transaction lost original Fault/cause")
				}
				return
			}
			if err != nil || got.Details() != attempt.Details() || store.updates != 1 || store.acquires != 1 {
				t.Fatal("confirmed reservation failed", err)
			}
			if mode == "send error" {
				port.sendErr = originalError
			}
			if mode == "send wrong attempt" {
				port.wrongResult = true
			}
			if mode == "checkpoint unknown" {
				store.unknownAt = 2
			}
			err = s.sendContentPublication(context.Background(), input, intent, work, prepared, got)
			if mode == "send error" || mode == "send wrong attempt" {
				if err == nil || store.calls != 1 || store.updates != 1 {
					t.Fatal("failed/wrong upload reached SQL checkpoint", err)
				}
				if mode == "send error" && err != originalError {
					t.Fatal("send Fault identity changed")
				}
			} else if mode == "checkpoint unknown" {
				var unknown commitFailure
				if !errors.As(err, &unknown) || unknown.result.AttemptID() != store.attempt || unknown.result.Cause().Details().Primary.Canonical() != store.cause.Details().Primary.Canonical() {
					t.Fatal("checkpoint Unknown lost original physical cause", err)
				}
			} else if err != nil || store.reservation.phase != "uploaded" {
				t.Fatal("confirmed send did not checkpoint", err)
			}
			if port.sends != 1 || store.active || store.calls != store.acquires {
				t.Fatal("send repeated or transaction/lock lifecycle changed")
			}
		})
	}
}

func (x *publicationReservationExecutor) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	return publicationCheckpointRow(func(v ...any) error {
		if !strings.Contains(query, "FROM agenteam_knowledge.publications WHERE command_id=$1") || len(args) != 1 || args[0] != x.record.id.String() {
			return errors.New("unexpected publication selection")
		}
		*v[0].(*string), *v[1].(*string) = x.project, x.document
		*v[2].(*[]byte), *v[3].(*[]byte) = x.source, x.measurement
		*v[4].(**string), *v[5].(**string), *v[6].(**string), *v[7].(*string) = x.object, x.upload, x.attempt, x.phase
		return nil
	})
}

func TestPublicationReservationRejectsPartialOrForeignDurableFacts(t *testing.T) {
	record := &commandRecord{id: newID[command](t), project: newID[id.Project](t), document: newID[kc.Document](t)}
	source, _ := kc.NewTextSource(kc.PlainText, "original")
	descriptor, _ := describePublicationSource(source)
	rawSource, _ := json.Marshal(descriptor)
	measurement := publicationMeasurement{Media: descriptor.Media, Length: *descriptor.Length, SHA: *descriptor.SHA}
	rawMeasured, _ := json.Marshal(measurement)
	objectID, uploadID, attemptID := newID[oc.StoredObject](t).String(), newID[oc.Upload](t).String(), newID[oc.Attempt](t).String()
	x := publicationReservationExecutor{record: record, project: record.project.String(), document: record.document.String(), source: rawSource, phase: "planned"}
	if got, err := loadPublicationReservation(context.Background(), &x, record); err != nil || got.measurement != nil || got.attempt.Validate() == nil {
		t.Fatal("planned source fabricated measurement/reservation", err)
	}
	x.measurement, x.object, x.upload, x.attempt = rawMeasured, &objectID, &uploadID, &attemptID
	for _, phase := range []string{"reserved", "uploaded"} {
		x.phase = phase
		got, err := loadPublicationReservation(context.Background(), &x, record)
		if err != nil || got.measurement == nil || *got.measurement != measurement || got.attempt.Details().ID.String() != attemptID || got.attempt.Details().UploadID.String() != uploadID || got.attempt.Details().ObjectID.String() != objectID {
			t.Fatal("exact reservation identity lost", err)
		}
	}
	for name, change := range map[string]func(*publicationReservationExecutor){
		"foreign project":          func(x *publicationReservationExecutor) { x.project = newID[id.Project](t).String() },
		"wrong document":           func(x *publicationReservationExecutor) { x.document = newID[kc.Document](t).String() },
		"missing object":           func(x *publicationReservationExecutor) { x.object = nil },
		"missing upload":           func(x *publicationReservationExecutor) { x.upload = nil },
		"missing attempt":          func(x *publicationReservationExecutor) { x.attempt = nil },
		"missing measurement":      func(x *publicationReservationExecutor) { x.measurement = nil },
		"missing source":           func(x *publicationReservationExecutor) { x.source = nil },
		"planned with reservation": func(x *publicationReservationExecutor) { x.phase = "planned" },
		"already published":        func(x *publicationReservationExecutor) { x.phase = "published" },
	} {
		t.Run(name, func(t *testing.T) {
			other := x
			change(&other)
			if _, err := loadPublicationReservation(context.Background(), &other, record); err == nil {
				t.Fatal("partial/foreign durable reservation accepted")
			}
		})
	}
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
			if err := retirement.join(context.Background()); err != nil {
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
			joinErr := retirement.join(context.Background())
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
				if err := retirement.join(context.Background()); err != nil {
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
			if err := retirement.join(context.Background()); err != nil {
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
	go func() { result <- retirement.join(context.Background()) }()
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
	if err = retirement.join(context.Background()); err != nil {
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
	if err = retirement.join(context.Background()); err == nil || strings.Join(order, ",") != "spool,source" {
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
	if err = retirement.join(context.Background()); err != nil || strings.Join(order, ",") != "spool,source,spool" {
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
	if err = newRetirement.join(context.Background()); err != nil {
		t.Fatal(err)
	}
	origin = newID[id.Project](t)
	if err = old.join(context.Background()); err != nil {
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
	if err = retirement.join(context.Background()); err != nil {
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
	if _, exists := s.state().joinedPublications[work.attempt]; exists {
		t.Fatal("confirmed durable join retained unnecessary memory proof")
	}
	if _, err = nextPublicationWork(record, nil, work.process, &store.work, nil); err != nil {
		t.Fatal("durable joined attempt could not be retried", err)
	}
}

func TestPublicationUnknownAbsentSameFenceControl(t *testing.T) {
	for _, absent := range []bool{false, true} {
		name := "confirmed_prior_row"
		if absent {
			name = "unknown_prior_claim_absent"
		}
		t.Run(name, func(t *testing.T) {
			s, record, first, done := retirementFixture(t)
			prior, err := s.publicationRetirement(first, done)
			if err != nil {
				t.Fatal(err)
			}
			if err = prior.join(context.Background()); err != nil {
				t.Fatal(err)
			}
			previous := first.clone()
			previous.phase = "joined"
			old := &previous
			if absent {
				old = nil
			}
			// This is the distinct in-memory result of a next claim after SQL has
			// confirmed no prior durable row; it is not a PG Unknown simulation.
			second, err := nextPublicationWork(record, nil, first.process, old, nil)
			if err != nil || second.attempt == first.attempt {
				t.Fatal("fresh attempt", err)
			}
			if absent && second.fence != first.fence {
				t.Fatal("absent claim should use initial fence")
			}
			retirement, err := s.publicationRetirement(second, func() {})
			if err != nil {
				t.Fatal(err)
			}
			if err = retirement.join(context.Background()); err != nil {
				t.Fatal(err)
			}
			proof, err := s.stoppedPublication(context.Background(), &second)
			if err != nil || proof == nil || !proof.original.equal(second) {
				t.Fatal("actual second retirement lost its exact proof", err)
			}
		})
	}
}

func TestPublicationSameFenceCheckpointCannotDeleteOtherProof(t *testing.T) {
	s, record, first, done := retirementFixture(t)
	_, actor, _ := queryFixture(t)
	record.user, _ = f.ParseID[id.User](actor.Details().UserID)
	record.name, record.key, record.digest = kc.Create, "same-fence-checkpoint", ob.DigestBytes([]byte("intent"))
	record.created, _ = f.NewInstant(time.Now())
	a, err := s.publicationRetirement(first, done)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.join(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, err := nextPublicationWork(record, nil, first.process, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.publicationRetirement(second, func() {})
	if err != nil {
		t.Fatal(err)
	}
	if err = b.join(context.Background()); err != nil {
		t.Fatal(err)
	}
	store := &publicationCheckpointStore{record: record, work: second}
	s.state().store = store
	if err = s.checkpointPublicationJoin(context.Background(), actor, record, first); err == nil || store.updates != 0 {
		t.Fatal("old attempt checkpoint altered newer durable work", err)
	}
	if proof, err := s.stoppedPublication(context.Background(), &second); err != nil || proof == nil || !proof.original.equal(second) {
		t.Fatal("old checkpoint erased new local proof", err)
	}
	if err = s.checkpointPublicationJoin(context.Background(), actor, record, second); err != nil || store.updates != 1 {
		t.Fatal("new proof cannot project its exact durable work", err)
	}
	if proof, err := s.stoppedPublication(context.Background(), &second); err == nil || proof != nil {
		t.Fatal("confirmed projection retained unnecessary new local proof")
	}
	if proof, err := s.stoppedPublication(context.Background(), &first); err != nil || proof == nil || !proof.original.equal(first) {
		t.Fatal("new checkpoint incorrectly erased different attempt", err)
	}
}
func TestPublicationAttemptCollisionRejectsDifferentIdentity(t *testing.T) {
	s, _, first, done := retirementFixture(t)
	a, err := s.publicationRetirement(first, done)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.join(context.Background()); err != nil {
		t.Fatal(err)
	}
	wrong := first.clone()
	wrong.command = newID[command](t)
	called := false
	b, err := s.publicationRetirement(wrong, func() { called = true })
	if err != nil {
		t.Fatal(err)
	}
	if err = b.join(context.Background()); err == nil || called {
		t.Fatal("same attempt different command replaced proof or retired call", err)
	}
	if proof, err := s.stoppedPublication(context.Background(), &first); err != nil || proof == nil || !proof.original.equal(first) {
		t.Fatal("collision destroyed original proof", err)
	}
	if proof, err := s.stoppedPublication(context.Background(), &wrong); err == nil || proof != nil {
		t.Fatal("wrong command consumed original proof")
	}
}

type businessOpenPort struct {
	oc.SourceReads
	t      *testing.T
	actor  id.Actor
	lease  oc.SourceLease
	reader *oc.ObjectReader
	err    error
	calls  int
}

func (p *businessOpenPort) OpenLeasedSource(_ context.Context, actor id.Actor, lease oc.SourceLease) (*oc.ObjectReader, error) {
	p.calls++
	if !actor.Equal(p.actor) || lease.ID() != p.lease.ID() {
		p.t.Fatal("opening changed original actor/lease")
	}
	return p.reader, p.err
}

func TestBusinessPublicationPreparesExactLeasedBytesAndJoinsActualReader(t *testing.T) {
	for _, mode := range []string{"valid", "pdf", "invalid utf8", "open error with reader", "wrong object", "wrong scope", "wrong version", "wrong digest", "range", "early prepare", "prepare error with handle", "close error", "wrong command", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			data := []byte("é中🙂\r\n")
			media := kc.Markdown
			if mode == "pdf" {
				data, media = []byte{0xff, 0x00, 0x80}, kc.PDF
			}
			if mode == "invalid utf8" {
				data = []byte{0xe4, 0xb8}
			}
			origin, document := newID[id.Project](t), newID[kc.Document](t)
			ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.KnowledgeFile, ProjectID: origin, DocumentID: document.String(), Revision: 3})
			if err != nil {
				t.Fatal(err)
			}
			inputSource, err := kc.NewBusinessSource(ref)
			if err != nil {
				t.Fatal(err)
			}
			s, input, intent, work, retirement, upload := directPublicationFixture(t, inputSource)
			defer retirement.done()
			work.source = &origin
			retirement.work = work.clone()
			now, _ := f.NewInstant(time.Now())
			scope, _ := id.InProject(origin)
			meta := oc.ObjectMeta{ID: newID[oc.StoredObject](t), Scope: scope, MediaType: media, ByteSize: f.Progress(len(data)), SHA256: ob.DigestBytes(data), State: oc.Available, Version: 2, CreatedAt: now}
			owner, _ := oc.NewObjectOwner(oc.Knowledge, document.String(), origin.String())
			resolved, err := oc.NewResolvedSource(oc.ResolvedSourceDetails{Reference: ref, Owner: owner, Meta: meta, Revision: 3})
			if err != nil {
				t.Fatal(err)
			}
			bound := businessPublicationSource{resolved: resolved, description: *input.request.Source, measurement: publicationMeasurement{Media: media, Length: meta.ByteSize, SHA: meta.SHA256}}
			actual := meta
			switch mode {
			case "wrong object":
				actual.ID = newID[oc.StoredObject](t)
			case "wrong scope":
				actual.Scope, _ = id.InProject(newID[id.Project](t))
			case "wrong version":
				actual.Version++
			case "wrong digest":
				actual.SHA256 = ob.DigestBytes([]byte("different"))
			}
			body := &publicationChunkBody{data: data, stride: 1}
			physical := errors.New("leased source physical failure")
			if mode == "close error" {
				body.closeErr = physical
			}
			var span *oc.ResolvedRange
			if mode == "range" {
				span = &oc.ResolvedRange{Offset: 0, Length: 1, Total: meta.ByteSize}
			}
			reader, err := oc.NewObjectReader(actual, span, body)
			if err != nil {
				t.Fatal(err)
			}
			lease, _ := oc.NewSourceLease(oc.NewSourceIssuer(), newID[oc.Lease](t))
			port := &businessOpenPort{t: t, actor: input.actor, lease: lease, reader: reader}
			if mode == "open error with reader" {
				port.err = fault(f.DependencyUnavailable).WithCause(physical)
			}
			s.state().deps.SourceReads = port
			path := filepath.Join(t.TempDir(), "business-spool")
			spool, err := objectimpl.OpenSpool(path, newID[oc.Process](t))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := spool.Close(); err != nil {
					t.Error(err)
				}
			})
			fakeHandle := mode == "early prepare" || mode == "prepare error with handle"
			upload.prepare = func(ctx context.Context, actor id.Actor, target oc.ObjectOwner, m string, n int64, digest *f.Digest, r io.ReadCloser) (oc.PreparedPayload, error) {
				if !actor.Equal(input.actor) || target.Details().ID != input.document.String() || target.Details().ProjectID != input.project.String() || m != media || n != int64(len(data)) || digest == nil || *digest != meta.SHA256 {
					t.Fatal("target measured arguments changed")
				}
				if fakeHandle {
					if mode != "early prepare" {
						if _, err := io.ReadAll(r); err != nil {
							t.Fatal(err)
						}
					}
					prepared, _ := oc.NewPreparedPayload(oc.PreparedDetails{ID: newID[oc.Payload](t), MediaType: media, Length: int64(len(data)), SHA256: meta.SHA256})
					if mode == "prepare error with handle" {
						return prepared, fault(f.DependencyUnavailable).WithCause(physical)
					}
					return prepared, nil
				}
				return spool.Prepare(ctx, m, n, digest, r)
			}
			if !fakeHandle {
				upload.discard = spool.Discard
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			if mode == "wrong command" {
				intent.record.id = newID[command](t)
			}
			prepared, err := s.prepareBusinessPublication(ctx, input, intent, bound, work, lease, retirement)
			positive := mode == "valid" || mode == "pdf"
			if positive {
				if err != nil || prepared.Validate() != nil {
					t.Fatal("valid business preparation failed", err)
				}
			} else if err == nil {
				t.Fatal("unsafe business preparation accepted")
			}
			if mode == "open error with reader" || mode == "prepare error with handle" || mode == "close error" {
				if !errors.Is(err, physical) {
					t.Fatal("original source error cause lost", err)
				}
			}
			beforeOpen := mode == "wrong command" || mode == "cancelled"
			beforePrepare := beforeOpen || mode == "open error with reader" || mode == "wrong object" || mode == "wrong scope" || mode == "wrong version" || mode == "wrong digest" || mode == "range"
			if beforeOpen {
				if port.calls != 0 {
					t.Fatal("invalid operation opened source")
				}
			} else if port.calls != 1 {
				t.Fatal("lease not opened exactly once")
			}
			if beforePrepare && (upload.calls != 0 || body.reads != 0) {
				t.Fatal("invalid source reached bytes/preparation")
			}
			joinErr := retirement.join(context.Background())
			if mode == "close error" {
				if !errors.Is(joinErr, physical) || len(s.state().calls) != 1 {
					t.Fatal("failed reader Close became join", joinErr)
				}
			} else if joinErr != nil || len(s.state().calls) != 0 {
				t.Fatal("actual reader did not join", joinErr)
			}
			wantClose := 1
			if beforeOpen {
				wantClose = 0
			}
			if body.closes != wantClose {
				t.Fatal("reader Close count", body.closes, wantClose)
			}
			if (positive || fakeHandle) && upload.discards != 1 {
				t.Fatal("prepared handle not discarded exactly once")
			}
			entries, err := os.ReadDir(path)
			if err != nil || len(entries) != 1 || entries[0].Name() != ".object.lock" {
				t.Fatal("owned source preparation remains", err)
			}
		})
	}
}
