package work

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type reviewRow []any

func (r reviewRow) Scan(dst ...any) error {
	for n, v := range r {
		d := reflect.ValueOf(dst[n]).Elem()
		if v == nil {
			d.SetZero()
		} else {
			d.Set(reflect.ValueOf(v))
		}
	}
	return nil
}

type reviewRows struct {
	pgx.Rows
	items                                []reviewRow
	index                                int
	ctx                                  context.Context
	err                                  error
	waitNext, closeEntered, closeRelease chan struct{}
	closed                               atomic.Bool
}

func (r *reviewRows) Next() bool {
	if r.waitNext != nil {
		close(r.waitNext)
		<-r.ctx.Done()
		r.err = r.ctx.Err()
		return false
	}
	r.index++
	return r.index <= len(r.items)
}
func (r *reviewRows) Scan(dst ...any) error { return r.items[r.index-1].Scan(dst...) }
func (r *reviewRows) Err() error            { return r.err }
func (r *reviewRows) Close() {
	if r.closed.Swap(true) {
		return
	}
	if r.closeEntered != nil {
		close(r.closeEntered)
		<-r.closeRelease
	}
}

type reviewStore struct {
	Store
	t                       *testing.T
	tx                      f.Tx
	task                    reviewRow
	rows                    *reviewRows
	query                   string
	args                    []any
	locks                   []f.LockRequest
	reads, queries          int
	released                atomic.Bool
	callbackDone, txRelease chan struct{}
	returned                atomic.Bool
}

func (s *reviewStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	if cause.Validate() != nil {
		s.t.Fatal("invalid read cause")
	}
	s.tx = f.NewTx()
	s.returned.Store(false)
	err := fn(ctx, s.tx)
	if s.callbackDone != nil {
		close(s.callbackDone)
		<-s.txRelease
	}
	s.returned.Store(true)
	if err != nil {
		return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err))
	}
	return f.CommittedResult()
}
func (s *reviewStore) AcquireAll(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if tx != s.tx {
		s.t.Fatal("wrong lock Tx")
	}
	s.locks = append([]f.LockRequest{}, locks...)
	return nil
}
func (s *reviewStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, errors.New("wrong Tx")
	}
	return s, nil
}
func (s *reviewStore) QueryRow(_ context.Context, q string, args ...any) postgres.Row {
	s.reads++
	if !strings.Contains(q, "FROM agenteam_work.tasks WHERE project_id=$1 AND id=$2") {
		s.t.Fatal("unexpected canonical read")
	}
	return s.task
}
func (s *reviewStore) Query(ctx context.Context, q string, args ...any) (*postgres.Rows, error) {
	s.queries++
	s.query = q
	s.args = append([]any{}, args...)
	s.rows.ctx = ctx
	return postgres.TimelineReviewRows(ctx, s.rows, func() { s.released.Store(true) }), nil
}
func (s *reviewStore) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	s.t.Fatal("reader wrote facts")
	return pgconn.CommandTag{}, nil
}

type reviewAuthority struct {
	pc.ProjectAuthority
	store   *reviewStore
	project pc.ProjectRef
	calls   int
	deny    error
}

func (a *reviewAuthority) RequireOwnerInTx(_ context.Context, tx f.Tx, actor i.Actor, p c.ProjectID, intent i.AccessIntent) (pc.ProjectAccess, error) {
	a.calls++
	if tx != a.store.tx || len(a.store.locks) != 4 || intent != i.Read {
		a.store.t.Fatal("authority before same-Tx complete locks")
	}
	for _, l := range a.store.locks {
		if l.Mode != f.Shared {
			a.store.t.Fatal("read lock mode")
		}
	}
	if a.deny != nil {
		return pc.ProjectAccess{}, a.deny
	}
	return pc.NewProjectAccess(actor, a.project, a.project.UpdatedAt)
}
func reviewFixture(t *testing.T) (*TaskReader, *reviewStore, *reviewAuthority, i.Actor, c.ProjectID, c.TaskID) {
	t.Helper()
	actor := pureActor(t, 2)
	p := pureID[i.Project](t, 3)
	id := pureID[c.Task](t, 4)
	at, _ := f.ParseInstant("2026-10-09T10:00:00.000000Z")
	s := &reviewStore{t: t, rows: &reviewRows{}}
	s.task = reviewRow{id.String(), p.String(), pureID[c.Milestone](t, 5).String(), pureID[pc.Sprint](t, 6).String(), "task", "", c.TaskTypeTask, c.TaskPriorityMedium, c.TaskStateBacklog, nil, "", "7fffffffffffffffffffffffffffffff", f.Version(7), at.Time(), at.Time()}
	a := &reviewAuthority{store: s, project: pc.ProjectRef{ID: p, OwnerUserID: pureID[i.User](t, 1), Name: "Project", NormalizedName: "project", Lifecycle: pc.Active, Version: 1, CreatedAt: at, UpdatedAt: at}}
	authority, err := NewAuthority(s, a)
	if err != nil {
		t.Fatal(err)
	}
	structure, err := NewReader(s, authority, pureKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewTaskReader(s, authority, structure, pureKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	return reader, s, a, actor, p, id
}
func reviewEvent(t *testing.T, p c.ProjectID, id c.TaskID, n int) reviewRow {
	t.Helper()
	at := time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)
	operation := pureID[c.TaskCommand](t, 20+n).String()
	actor, _ := json.Marshal(c.TaskEventActor{Type: i.Human, UserID: pureID[i.User](t, 1), Source: "task_domain"})
	payload, _ := json.Marshal(c.TaskFieldsUpdatedPayload{ChangedFields: []c.TaskChangedField{c.TaskTitleChanged}})
	return reviewRow{pureID[c.TaskEvent](t, 40+n).String(), p.String(), id.String(), int64(2 + n), "fields_updated", actor, &operation, nil, operation, payload, at}
}
func TestTimelineIndependentReadBindingsAndReauthorization(t *testing.T) {
	for _, order := range []c.TaskTimelineOrder{c.TaskTimelineAscending, c.TaskTimelineDescending} {
		t.Run(string(order), func(t *testing.T) {
			r, s, a, actor, p, id := reviewFixture(t)
			s.rows.items = []reviewRow{reviewEvent(t, p, id, 1), reviewEvent(t, p, id, 2)}
			out, err := r.ListTaskEvents(context.Background(), actor, p, id, c.TaskTimelineFilter{Order: order}, f.PageRequest{Limit: 1})
			if err != nil || len(out.Items) != 1 || out.NextCursor == "" || !s.released.Load() || !s.returned.Load() {
				t.Fatal("first page", err)
			}
			if s.args[2] != int64(7) || s.args[len(s.args)-1] != 2 || strings.Contains(s.query, "type=ANY") {
				t.Fatal("W/lookahead/default unknown filtering")
			}
			s.task[12] = f.Version(8)
			s.rows = &reviewRows{}
			s.released.Store(false)
			_, err = r.ListTaskEvents(context.Background(), actor, p, id, c.TaskTimelineFilter{Order: order}, f.PageRequest{Limit: 5, Cursor: out.NextCursor})
			if err != nil {
				t.Fatal(err)
			}
			comparison, direction := ">", "ASC"
			if order == c.TaskTimelineDescending {
				comparison, direction = "<", "DESC"
			}
			if s.args[2] != int64(7) || s.args[len(s.args)-1] != 6 || !strings.Contains(s.query, "(created_at,id)"+comparison) || !strings.Contains(s.query, "ORDER BY created_at "+direction+",id "+direction) || a.calls != 2 {
				t.Fatal("continuation query/authority")
			}
			reads, queries := s.reads, s.queries
			a.deny = f.NewFault(f.SessionRevoked, f.NotStarted)
			out, err = r.ListTaskEvents(context.Background(), actor, p, id, c.TaskTimelineFilter{Order: order}, f.PageRequest{Limit: 5, Cursor: out.NextCursor})
			if err == nil || out.Items != nil || out.NextCursor != "" || a.calls != 3 || s.reads != reads || s.queries != queries {
				t.Fatal("revocation did not precede Work SQL")
			}
		})
	}
}
func TestTimelineIndependentBadTailAndActualJoin(t *testing.T) {
	for _, bad := range []string{"lookahead", "rows_error"} {
		t.Run(bad, func(t *testing.T) {
			r, s, _, actor, p, id := reviewFixture(t)
			s.rows.items = []reviewRow{reviewEvent(t, p, id, 1), reviewEvent(t, p, id, 2)}
			if bad == "lookahead" {
				s.rows.items[1][4] = "future_history"
			} else {
				s.rows.err = errors.New("controlled EOF failure")
			}
			out, err := r.ListTaskEvents(context.Background(), actor, p, id, c.TaskTimelineFilter{}, f.PageRequest{Limit: 1})
			if err == nil || out.Items != nil || out.NextCursor != "" || !s.rows.closed.Load() || !s.released.Load() || !s.returned.Load() {
				t.Fatal("bad tail leaked partial output or lifetime")
			}
		})
	}
	t.Run("cancel_close_and_tx", func(t *testing.T) {
		r, s, _, actor, p, id := reviewFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s.rows.waitNext = make(chan struct{})
		s.rows.closeEntered = make(chan struct{})
		s.rows.closeRelease = make(chan struct{})
		s.callbackDone = make(chan struct{})
		s.txRelease = make(chan struct{})
		done := make(chan error, 1)
		var closeRows, closeTx sync.Once
		defer func() {
			cancel()
			closeRows.Do(func() { close(s.rows.closeRelease) })
			closeTx.Do(func() { close(s.txRelease) })
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("controlled reader failed actual join")
			}
		}()
		go func() {
			defer close(done)
			out, err := r.ListTaskEvents(ctx, actor, p, id, c.TaskTimelineFilter{}, f.PageRequest{Limit: 1})
			if out.Items != nil || out.NextCursor != "" {
				err = errors.New("partial canceled page")
			}
			done <- err
		}()
		select {
		case <-s.rows.waitNext:
		case <-time.After(time.Second):
			t.Fatal("Next not entered")
		}
		cancel()
		select {
		case <-s.rows.closeEntered:
		case <-time.After(time.Second):
			t.Fatal("Close not entered")
		}
		select {
		case <-done:
			t.Fatal("returned before real Close")
		default:
		}
		if s.released.Load() {
			t.Fatal("released before raw Close")
		}
		closeRows.Do(func() { close(s.rows.closeRelease) })
		select {
		case <-s.callbackDone:
		case <-time.After(time.Second):
			t.Fatal("callback did not join")
		}
		select {
		case <-done:
			t.Fatal("returned before WithinTx")
		default:
		}
		closeTx.Do(func() { close(s.txRelease) })
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) || !s.released.Load() || !s.returned.Load() {
				t.Fatal("lost cancellation/join", err)
			}
		case <-time.After(time.Second):
			t.Fatal("reader did not return")
		}
	})
}
