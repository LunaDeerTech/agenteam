package audit

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const independentAuditProject = "01900000-0000-7000-8000-000000000009"

type independentAuditProjectPort struct {
	c.ProjectAuthority
	call func(context.Context, f.Tx, id.Actor, id.ProjectID, id.AccessIntent) (id.AccessGrant, error)
}

func (p independentAuditProjectPort) AuthorizeProject(ctx context.Context, tx f.Tx, actor id.Actor, project id.ProjectID, intent id.AccessIntent) (id.AccessGrant, error) {
	return p.call(ctx, tx, actor, project, intent)
}

type independentAuditQueryStore struct {
	*auditReadStore
	locks    int
	original f.TransactionCause
	returned f.CommitResult
	unknown  bool
}

func (s *independentAuditQueryStore) Acquire(_ context.Context, tx f.Tx, key f.LockKey, mode f.LockMode) error {
	user, _ := f.UserLock("01900000-0000-7000-8000-000000000001")
	project, _ := f.ProjectLock(independentAuditProject)
	if tx != s.tx || mode != f.Shared || s.locks >= 2 {
		s.t.Error("wrong shared lock/transaction")
		return errors.New("lock-shape")
	}
	want := []f.LockKey{user, project}[s.locks]
	if f.CompareLockKeys(key, want) != 0 {
		s.t.Error("lock order or owner changed")
		return errors.New("lock-owner")
	}
	s.locks++
	s.steps = append(s.steps, []string{"user-shared", "project-shared"}[s.locks-1])
	s.held = s.locks == 2
	return nil
}
func (s *independentAuditQueryStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.started.Add(1)
	s.active.Add(1)
	defer s.active.Add(-1)
	s.original = cause
	if cause.Validate() != nil || cause.Kind() != f.RecoveryCause || cause.Details().Owner != "audit.project-read" {
		s.t.Error("original read recovery cause lost")
	}
	s.tx = f.NewTx()
	s.steps = append(s.steps, "begin")
	err := fn(ctx, s.tx)
	s.steps = append(s.steps, "callback-ended")
	if s.tail != nil {
		s.tail(ctx)
	}
	s.steps = append(s.steps, "transaction-ended")
	if s.unknown {
		attempt, _ := f.ParseID[f.TransactionAttempt]("01900000-0000-7000-8000-000000000099")
		s.returned = f.UnknownResult(attempt, cause)
		return s.returned
	}
	if err != nil {
		var fault *f.Fault
		if !errors.As(err, &fault) {
			fault = f.NewFault(f.InternalError, f.NotCommitted).WithCause(err)
		}
		return f.NotCommittedResult(fault)
	}
	return f.CommittedResult()
}
func independentAuditProjectFixture(t *testing.T) (*Service, *independentAuditQueryStore, id.Actor, id.ProjectID, c.ID) {
	t.Helper()
	old, base, actor, recordID := systemTestService(t)
	store := &independentAuditQueryStore{auditReadStore: base}
	row := systemTestRow()
	row.values[2] = "project"
	row.values[3] = independentAuditProject
	base.row = row
	project, _ := f.ParseID[id.Project](independentAuditProject)
	scope, _ := id.InProject(project)
	check := func(tx f.Tx, name string) {
		if tx != store.tx || !tx.Valid() || store.locks != 2 || !store.held {
			t.Error("authorization outside both actual callback locks")
		}
		store.steps = append(store.steps, name)
	}
	auth := Authorizations{
		Sessions: sessionPort(func(_ context.Context, tx f.Tx, a id.Actor) error {
			check(tx, "session")
			if a.Details() != actor.Details() {
				t.Error("principal changed")
			}
			return nil
		}),
		Projects: independentAuditProjectPort{call: func(_ context.Context, tx f.Tx, a id.Actor, p id.ProjectID, intent id.AccessIntent) (id.AccessGrant, error) {
			check(tx, "owner-read")
			if p != project || intent != id.Read {
				t.Error("scope/intent changed")
			}
			now, _ := f.NewInstant(time.Now())
			return id.NewAccessGrant(a, scope, intent, now, 1)
		}},
	}
	service, err := New(store, old.keys, auth)
	if err != nil {
		t.Fatal(err)
	}
	return service, store, actor, project, recordID
}
func independentAuditZeroRecord(t *testing.T, record c.SafeRecord) {
	t.Helper()
	if !reflect.DeepEqual(record, c.SafeRecord{}) {
		t.Fatal("candidate escaped failed read")
	}
}

func TestIndependentProjectAuditQueryControls(t *testing.T) {
	t.Run("same-tx-current-gates-before-query", func(t *testing.T) {
		service, store, actor, project, recordID := independentAuditProjectFixture(t)
		got, err := service.GetProject(context.Background(), actor, project, recordID)
		if err != nil || got.AuditID != recordID {
			t.Fatal("valid authorized record rejected", err)
		}
		want := []string{"begin", "user-shared", "project-shared", "session", "owner-read", "executor", "query-row", "callback-ended", "transaction-ended"}
		if !reflect.DeepEqual(store.steps, want) || store.started.Load() != 1 || store.active.Load() != 0 {
			t.Fatal("same-Tx gate/terminal sequence changed")
		}
	})
	for _, gate := range []string{"session", "owner"} {
		t.Run("reject-current-"+gate+"-before-query", func(t *testing.T) {
			service, store, actor, project, recordID := independentAuditProjectFixture(t)
			code := f.SessionRevoked
			if gate == "session" {
				service.auth.Sessions = sessionPort(func(_ context.Context, tx f.Tx, _ id.Actor) error {
					if tx != store.tx || store.locks != 2 {
						t.Error("current session outside transaction locks")
					}
					return f.NewFault(code, f.NotStarted)
				})
			} else {
				code = f.NotFound
				service.auth.Projects = independentAuditProjectPort{call: func(_ context.Context, tx f.Tx, _ id.Actor, _ id.ProjectID, intent id.AccessIntent) (id.AccessGrant, error) {
					if tx != store.tx || store.locks != 2 || intent != id.Read {
						t.Error("current owner outside transaction locks")
					}
					return id.AccessGrant{}, f.NewFault(code, f.NotStarted)
				}}
			}
			got, err := service.GetProject(context.Background(), actor, project, recordID)
			independentAuditZeroRecord(t, got)
			wantSystemFault(t, err, code, f.NotCommitted)
			if store.query != "" || store.started.Load() != 1 || store.active.Load() != 0 {
				t.Fatal("rejection queried or lost actual terminal")
			}
		})
	}
	t.Run("original-unknown-attempt-through-wrapping", func(t *testing.T) {
		service, store, actor, project, recordID := independentAuditProjectFixture(t)
		store.unknown = true
		got, err := service.GetProject(context.Background(), actor, project, recordID)
		independentAuditZeroRecord(t, got)
		wantSystemFault(t, err, f.CommitUnknown, f.Unknown)
		saved, ok := ProjectReadUnknownAttempt(fmt.Errorf("outer: %w", err))
		if !ok || saved.State() != store.returned.State() || saved.AttemptID() != store.returned.AttemptID() || !reflect.DeepEqual(saved.Cause().Details(), store.original.Details()) {
			t.Fatal("original physical identity not retained")
		}
		var fault *f.Fault
		if !errors.As(err, &fault) || fault.CauseID != saved.AttemptID().String() || fault.RetryHint != "lookup" || store.started.Load() != 1 || store.active.Load() != 0 {
			t.Fatal("Unknown classification or zero-confirmation boundary changed")
		}
		if _, ok := ProjectReadUnknownAttempt(f.NewFault(f.CommitUnknown, f.Unknown)); ok {
			t.Fatal("unrelated Unknown accepted")
		}
	})
	t.Run("cancel-does-not-publish-before-actual-tail", func(t *testing.T) {
		service, store, actor, project, recordID := independentAuditProjectFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		reached, release, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var once sync.Once
		store.tail = func(context.Context) { close(reached); <-release }
		result := make(chan error, 1)
		go func() {
			defer close(joined)
			got, err := service.GetProject(ctx, actor, project, recordID)
			if !reflect.DeepEqual(got, c.SafeRecord{}) {
				result <- errors.New("candidate escaped")
				return
			}
			result <- err
		}()
		t.Cleanup(func() { cancel(); once.Do(func() { close(release) }); <-joined })
		select {
		case <-reached:
		case <-time.After(time.Second):
			t.Fatal("tail not reached")
		}
		cancel()
		select {
		case <-joined:
			t.Fatal("returned before actual tail")
		default:
		}
		if store.active.Load() != 1 {
			t.Fatal("owner released early")
		}
		once.Do(func() { close(release) })
		<-joined
		err := <-result
		wantSystemFault(t, err, f.DependencyUnavailable, f.NotStarted)
		if !errors.Is(err, context.Canceled) || store.active.Load() != 0 || store.started.Load() != 1 {
			t.Fatal("cancel/real terminal lost")
		}
	})
	for _, mode := range []string{"valid201", "bad201", "extra202", "close-error"} {
		t.Run("complete-page-"+mode, func(t *testing.T) {
			service, _, _, project, _ := independentAuditProjectFixture(t)
			scope, _ := id.InProject(project)
			filter := c.Filter{Action: c.SecretCreate}
			check, err := service.projectPageCheck(scope, filter, f.PageRequest{Limit: 200})
			if err != nil {
				t.Fatal(err)
			}
			digest, _ := filterDigest(filter)
			binding := cursor.Binding{Scope: scope, QueryDigest: digest, Order: cursor.AuditOrder}
			count := 201
			if mode == "extra202" {
				count = 202
			}
			rows := &auditTestRows{}
			for i := 0; i < count; i++ {
				row := systemTestRow()
				row.values[0] = fmt.Sprintf("01900000-0000-7000-8000-%012x", 1000-i)
				row.values[2] = "project"
				row.values[3] = independentAuditProject
				rows.rows = append(rows.rows, row)
			}
			if mode == "bad201" {
				rows.rows[200].values[12] = "secret.update"
			}
			if mode == "close-error" {
				rows.closeErr = errors.New("only-after-close")
			}
			scans := 0
			got, err := service.recordPage(rows, binding, 200, func(record c.SafeRecord) error { scans++; return check(record) })
			if rows.closes != 1 || scans != count {
				t.Fatal("sentinel/202 not fully validated or Close not once")
			}
			if mode != "valid201" {
				if err == nil || len(got.Items) != 0 || got.NextCursor != "" {
					t.Fatal("bad page published")
				}
				if mode == "close-error" && !errors.Is(err, rows.closeErr) {
					t.Fatal("Close-only Err cause lost")
				}
				return
			}
			if err != nil || len(got.Items) != 200 || got.NextCursor == "" {
				t.Fatal("valid full page rejected", err)
			}
			pos, err := service.keys.Verify(got.NextCursor, binding)
			if err != nil || pos.Scalars[1].Value() != got.Items[199].AuditID.String() {
				t.Fatal("cursor includes sentinel")
			}
			next, err := service.projectPageCheck(scope, filter, f.PageRequest{Limit: 1, Cursor: got.NextCursor})
			if err != nil {
				t.Fatal("limit incorrectly bound", err)
			}
			last, err := scanRecord(rows.rows[200])
			if err != nil || next(last) != nil {
				t.Fatal("same-time next-ID boundary rejected")
			}
		})
	}
}
