package knowledge

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

// These are explicit transaction/authority controls, not a PostgreSQL proof.
type contentReadStore struct {
	Store
	tx             f.Tx
	active         bool
	held, expected []f.LockRequest
	project        id.ProjectID
	document       kc.DocumentID
	rows           []postgres.Row
	queries        int
	unknown        bool
}

func (s *contentReadStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.tx, s.active, s.held = f.NewTx(), true, nil
	defer func() { s.active = false }()
	if err := fn(ctx, s.tx); err != nil {
		var fault *f.Fault
		if !errors.As(err, &fault) {
			panic("controlled callback must return a classified failure")
		}
		return f.NotCommittedResult(fault)
	}
	if s.unknown {
		attempt, err := f.NewID[f.TransactionAttempt]()
		if err != nil {
			panic(err)
		}
		return f.UnknownResult(attempt, cause)
	}
	return f.CommittedResult()
}
func (s *contentReadStore) AcquireAll(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if !s.active || tx != s.tx || !reflect.DeepEqual(locks, s.expected) {
		return errors.New("wrong live transaction or read locks")
	}
	s.held = append([]f.LockRequest(nil), locks...)
	return nil
}
func (s *contentReadStore) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if !s.active || tx != s.tx || !reflect.DeepEqual(locks, s.held) {
		return errors.New("read was not preceded by the original lock union")
	}
	return nil
}
func (s *contentReadStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if !s.active || tx != s.tx {
		return nil, errors.New("foreign or retired transaction")
	}
	return s, nil
}
func (s *contentReadStore) QueryRow(_ context.Context, sql string, args ...any) postgres.Row {
	if !s.active || !strings.Contains(sql, "FROM agenteam_knowledge.documents") || len(args) != 2 || args[0] != s.project.String() || args[1] != s.document.String() {
		return rejectedRow{}
	}
	s.queries++
	if s.queries > len(s.rows) {
		return rejectedRow{}
	}
	return s.rows[s.queries-1]
}

type contentReadObjects struct {
	oc.Objects
	calls int
	read  func(context.Context, id.Actor, oc.ObjectOwner, oc.ObjectID, *oc.ByteRange) (*oc.ObjectReader, error)
}

func (o *contentReadObjects) ReadObject(ctx context.Context, actor id.Actor, owner oc.ObjectOwner, object oc.ObjectID, span *oc.ByteRange) (*oc.ObjectReader, error) {
	o.calls++
	if o.read != nil {
		return o.read(ctx, actor, owner, object, span)
	}
	return nil, errors.New("refused content must not open an Object reader")
}

func TestContentDeletedClassificationAfterCurrentAuthority(t *testing.T) {
	for _, method := range []string{"document", "canonical"} {
		for _, mode := range []string{"deleted", "missing", "foreign", "revoked", "unknown"} {
			t.Run(method+"/"+mode, func(t *testing.T) {
				service, store, gate, objects, actor := contentReadFixture(t)
				want, queries := f.ResourceDeleted, 1
				switch mode {
				case "missing":
					store.rows = []postgres.Row{publicationCheckpointRow(func(...any) error { return pgx.ErrNoRows })}
					want = f.NotFound
				case "foreign":
					gate.err, want, queries = fault(f.NotFound), f.NotFound, 0
				case "revoked":
					gate.err, want, queries = fault(f.SessionRevoked), f.SessionRevoked, 0
				case "unknown":
					store.unknown, want = true, f.CommitUnknown
				}
				var err error
				if method == "document" {
					_, err = service.ReadDocument(context.Background(), actor, store.project, store.document, kc.DefaultReadRequest())
				} else {
					_, err = service.OpenCanonical(context.Background(), actor, store.project, store.document, nil)
				}
				var actual *f.Fault
				if !errors.As(err, &actual) || actual.Code != want || store.queries != queries || objects.calls != 0 || store.active || len(service.state().calls) != 0 {
					t.Fatal("wrong classification, protected read, or retained original call", err)
				}
				if gate.err != nil && err != gate.err {
					t.Fatal("original authority error was replaced")
				}
			})
		}
	}
}

func TestContentDeletedBetweenOriginalAndCanonicalReads(t *testing.T) {
	service, store, _, objects, actor := contentReadFixture(t)
	deleted := store.rows[0]
	user := actor.Details().UserID
	title, kind, media, indexing := "current text", "text", kc.PlainText, "pending"
	object, upload, at := newID[oc.StoredObject](t).String(), newID[oc.Upload](t).String(), time.Now().UTC()
	active := sourceRow{values: []any{store.document.String(), store.project.String(), (*string)(nil), &title, int64(1), &kind, &media, &object, &upload, "active", &indexing, &user, &at, &at, (*time.Time)(nil)}}
	store.rows = []postgres.Row{active, deleted}
	_, err := service.ReadDocument(context.Background(), actor, store.project, store.document, kc.DefaultReadRequest())
	var actual *f.Fault
	if !errors.As(err, &actual) || actual.Code != f.ResourceDeleted || store.queries != 2 || objects.calls != 0 || len(service.state().calls) != 0 {
		t.Fatal("second authorized Get lost its deleted classification or retired before original call", err)
	}
	store.queries, store.rows = 0, []postgres.Row{deleted}
	head, err := service.GetDocument(context.Background(), actor, store.project, store.document)
	if err != nil || head.Deleted == nil || head.Active != nil {
		t.Fatal("metadata tombstone behavior was changed", err)
	}
}

func contentReadFixture(t *testing.T) (*Service, *contentReadStore, *ownerGate, *contentReadObjects, id.Actor) {
	t.Helper()
	_, actor, query := queryFixture(t)
	user, _ := f.ParseID[id.User](actor.Details().UserID)
	now, _ := f.NewInstant(time.Now())
	project := pc.ProjectRef{ID: query.project, OwnerUserID: user, Name: "Content", NormalizedName: "content", Lifecycle: pc.Active, Version: 1, CreatedAt: now, UpdatedAt: now}
	grant, err := pc.NewProjectAccess(actor, project, now)
	if err != nil {
		t.Fatal(err)
	}
	locks, err := scopeLocks(actor, query.project, false)
	if err != nil {
		t.Fatal(err)
	}
	store := &contentReadStore{project: query.project, document: newID[kc.Document](t), expected: locks}
	at := now.Time()
	store.rows = []postgres.Row{sourceRow{values: []any{store.document.String(), store.project.String(), (*string)(nil), (*string)(nil), int64(2), (*string)(nil), (*string)(nil), (*string)(nil), (*string)(nil), "deleted", (*string)(nil), (*string)(nil), (*time.Time)(nil), (*time.Time)(nil), &at}}}
	gate, objects := &ownerGate{grant: grant}, &contentReadObjects{}
	state := &serviceState{store: store, deps: Dependencies{Projects: gate, Objects: objects}, calls: make(map[*call]struct{}), changed: make(chan struct{})}
	return &Service{data: func() *serviceState { return state }}, store, gate, objects, actor
}

// This is the actual Service -> typed ObjectReader -> Read/Close path with an
// explicit controlled body. It proves local ownership, not D05 lease release
// or native storage I/O. Every launched call is joined in the same test.
type contentReaderBody struct {
	ctx                        context.Context
	text                       *strings.Reader
	readStarted, readRelease   chan struct{}
	closeStarted, closeRelease chan struct{}
	readOnce, closeOnce        sync.Once
	reads, closes              atomic.Int32
	holdRead                   bool
	readErr, closeErr          error
}

func (b *contentReaderBody) Read(p []byte) (int, error) {
	b.reads.Add(1)
	b.readOnce.Do(func() { close(b.readStarted) })
	if b.holdRead {
		<-b.readRelease
		return 0, b.ctx.Err()
	}
	if b.readErr != nil {
		return 0, b.readErr
	}
	return b.text.Read(p)
}
func (b *contentReaderBody) Close() error {
	b.closes.Add(1)
	b.closeOnce.Do(func() { close(b.closeStarted) })
	<-b.closeRelease
	return b.closeErr
}
func contentReaderAwait(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal(label)
	}
}
func contentReaderRelease(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}

func TestContentActualTypedReaderRequiresOriginalClose(t *testing.T) {
	for _, mode := range []string{"eof_close_held", "read_error_close_held", "close_error", "current_changed_after_open", "cancel_during_original_read"} {
		t.Run(mode, func(t *testing.T) {
			service, store, _, objects, actor := contentReadFixture(t)
			deleted := store.rows[0]
			object := newID[oc.StoredObject](t)
			user, title, kind, media, indexing := actor.Details().UserID, "reader", "text", kc.PlainText, "pending"
			objectText, upload, at := object.String(), newID[oc.Upload](t).String(), time.Now().UTC()
			active := sourceRow{values: []any{store.document.String(), store.project.String(), (*string)(nil), &title, int64(1), &kind, &media, &objectText, &upload, "active", &indexing, &user, &at, &at, (*time.Time)(nil)}}
			store.rows = []postgres.Row{active, active, active}
			if mode == "current_changed_after_open" {
				store.rows[2] = deleted
			}
			body := &contentReaderBody{text: strings.NewReader("文a"), readStarted: make(chan struct{}), readRelease: make(chan struct{}), closeStarted: make(chan struct{}), closeRelease: make(chan struct{})}
			readErr, closeErr := errors.New("original content read failure"), errors.New("original content close failure")
			switch mode {
			case "read_error_close_held":
				body.readErr, body.closeErr = readErr, closeErr
			case "close_error":
				body.closeErr = closeErr
			case "cancel_during_original_read":
				body.holdRead = true
			}
			expectedOwner, err := oc.NewObjectOwner(oc.Knowledge, store.document.String(), store.project.String())
			if err != nil {
				t.Fatal(err)
			}
			scope, _ := id.InProject(store.project)
			now, _ := f.NewInstant(at)
			objects.read = func(ctx context.Context, actual id.Actor, owner oc.ObjectOwner, target oc.ObjectID, span *oc.ByteRange) (*oc.ObjectReader, error) {
				if !reflect.DeepEqual(actual.Details(), actor.Details()) || !owner.Equal(expectedOwner) || target != object || span != nil {
					return nil, errors.New("wrong original actor, owner, object or range")
				}
				body.ctx = ctx
				return oc.NewObjectReader(oc.ObjectMeta{ID: object, Scope: scope, MediaType: media, ByteSize: 4, SHA256: f.Digest("sha256:" + strings.Repeat("0", 64)), State: oc.Available, Version: 1, CreatedAt: now}, nil, body)
			}
			type result struct {
				content kc.DocumentContent
				err     error
			}
			finished, joined := make(chan result, 1), make(chan struct{})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				defer close(joined)
				out, err := service.ReadDocument(ctx, actor, store.project, store.document, kc.DefaultReadRequest())
				finished <- result{out, err}
			}()
			defer func() {
				cancel()
				contentReaderRelease(body.readRelease)
				contentReaderRelease(body.closeRelease)
				contentReaderAwait(t, joined, "original ReadDocument goroutine did not join")
			}()
			assertOwned := func() {
				t.Helper()
				select {
				case <-finished:
					t.Fatal("content published before original reader actually returned")
				default:
				}
				ended, stop := context.WithCancel(context.Background())
				stop()
				if err := service.Drain(ended); !errors.Is(err, context.Canceled) {
					t.Fatal("held original reader mistaken for local join", err)
				}
			}
			if body.holdRead {
				contentReaderAwait(t, body.readStarted, "original read not entered")
				cancel()
				service.Stop()
				assertOwned()
				if body.closes.Load() != 0 {
					t.Fatal("Close raced the still-running synchronous read")
				}
				contentReaderRelease(body.readRelease)
			}
			contentReaderAwait(t, body.closeStarted, "original typed Close not entered")
			assertOwned()
			contentReaderRelease(body.closeRelease)
			contentReaderAwait(t, joined, "original Close did not retire call")
			got := <-finished
			if body.closes.Load() != 1 || store.queries != 3 || objects.calls != 1 || store.active {
				t.Fatal("wrong original reader/transaction lifetime")
			}
			if err := service.Drain(context.Background()); err != nil {
				t.Fatal("returned reader did not join", err)
			}
			switch mode {
			case "eof_close_held":
				if got.err != nil || got.content.Validate() != nil || got.content.Text == nil || got.content.Text.Text != "文a" || got.content.Text.NextByteOffset != 4 || got.content.Text.Truncated {
					t.Fatal("joined EOF did not publish exact content", got.err)
				}
			case "read_error_close_held":
				if !errors.Is(got.err, readErr) || !errors.Is(got.err, closeErr) {
					t.Fatal("original read/Close causes were replaced", got.err)
				}
			case "close_error":
				if !errors.Is(got.err, closeErr) {
					t.Fatal("original returned Close failure lost", got.err)
				}
			case "current_changed_after_open":
				var classified *f.Fault
				if !errors.As(got.err, &classified) || classified.Code != f.VersionConflict || body.reads.Load() != 0 {
					t.Fatal("post-open change lost conflict or exposed bytes", got.err)
				}
			case "cancel_during_original_read":
				if !errors.Is(got.err, context.Canceled) {
					t.Fatal("original cancellation was replaced", got.err)
				}
			}
			if mode != "eof_close_held" && !reflect.DeepEqual(got.content, kc.DocumentContent{}) {
				t.Fatal("failed reader published a candidate")
			}
		})
	}
}

var _ io.ReadCloser = (*contentReaderBody)(nil)
