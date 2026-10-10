package knowledge

import (
	"context"
	"errors"
	"reflect"
	"strings"
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
}

func (o *contentReadObjects) ReadObject(context.Context, id.Actor, oc.ObjectOwner, oc.ObjectID, *oc.ByteRange) (*oc.ObjectReader, error) {
	o.calls++
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
