package project

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

// The controlled Store exercises the real directory's transaction boundary;
// positive multi-row decoding below uses the existing private Rows seam. It
// supplies no Human or Scheduler grant and does not stand in for actual PG.
type schedulerProjectsStore struct {
	readerStore
	queriesSeen []string
	queryArgs   []any
	queryErr    error
}

func (s *schedulerProjectsStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, errors.New("foreign discovery transaction")
	}
	return s, nil
}
func (s *schedulerProjectsStore) QueryRow(_ context.Context, query string, _ ...any) postgres.Row {
	s.queriesSeen = append(s.queriesSeen, query)
	return s.row
}
func (s *schedulerProjectsStore) Query(_ context.Context, query string, args ...any) (*postgres.Rows, error) {
	s.queriesSeen = append(s.queriesSeen, query)
	s.queryArgs = append([]any(nil), args...)
	return nil, s.queryErr
}

func schedulerProjectsFixture(t *testing.T) (*SchedulerProjects, *schedulerProjectsStore) {
	t.Helper()
	s := &schedulerProjectsStore{readerStore: readerStore{tx: f.NewTx(), row: rowFunc(func(...any) error { return pgx.ErrNoRows })}}
	a, err := NewAuthority(s, AuthorityDependencies{Sessions: sessionFunc(func(context.Context, f.Tx, i.Actor) error {
		t.Fatal("internal identity discovery tried to borrow a Human Session")
		return nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewSchedulerProjects(a)
	if err != nil {
		t.Fatal(err)
	}
	return r, s
}

func TestSchedulerProjectDiscoveryTransactionBoundaries(t *testing.T) {
	for _, authority := range []*Authority{nil, {}} {
		if got, err := NewSchedulerProjects(authority); got != nil || err == nil {
			t.Fatal("unbound directory constructed")
		}
	}
	r, s := schedulerProjectsFixture(t)
	if s.calls != 0 || len(s.queriesSeen) != 0 {
		t.Fatal("constructor performed I/O")
	}
	request := c.SchedulerProjectPageRequest{Limit: 2}
	page, err := r.ListSchedulerProjects(context.Background(), request)
	if err != nil || page.ValidateFor(request) != nil || !page.Complete || page.Through != nil || s.calls != 1 || len(s.queriesSeen) != 1 || len(s.locks) != 0 {
		t.Fatalf("empty original read: %v", err)
	}
	if !strings.Contains(s.queriesSeen[0], "initialized_at IS NOT NULL AND lifecycle='active'") || !strings.HasSuffix(s.queriesSeen[0], "ORDER BY id DESC LIMIT 1") {
		t.Fatal("first page did not fix the initialized active identity highwater")
	}
	_, err = r.ListSchedulerProjects(context.Background(), c.SchedulerProjectPageRequest{})
	hasCode(t, err, f.InvalidArgument)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = r.ListSchedulerProjects(ctx, request); !errors.Is(err, context.Canceled) || s.calls != 1 {
		t.Fatal("invalid/canceled admission touched Store")
	}
	through := testID[i.Project](t)
	s.row = valuesRow(through.String())
	s.queryErr = errors.New("controlled actual query error")
	page, err = r.ListSchedulerProjects(context.Background(), request)
	if !errors.Is(err, s.queryErr) || page.ProjectIDs != nil || s.calls != 2 || len(s.queriesSeen) != 3 {
		t.Fatalf("query failure returned a partial page: %v", err)
	}
	query := s.queriesSeen[2]
	if !strings.Contains(query, "initialized_at IS NOT NULL AND lifecycle='active'") ||
		!strings.Contains(query, "id>$1::uuid) AND id<=$2::uuid ORDER BY id ASC LIMIT $3") ||
		strings.Contains(query, "scheduler_enabled") || strings.Contains(query, "current_sprint") ||
		!reflect.DeepEqual(s.queryArgs, []any{nil, through.String(), 3}) {
		t.Fatal("discovery must remain bounded and include paused/no-sprint candidates")
	}
	request.Through = &through
	before := len(s.queriesSeen)
	_, _ = r.ListSchedulerProjects(context.Background(), request)
	if len(s.queriesSeen) != before+1 || s.queriesSeen[before] != query {
		t.Fatal("later page changed/requeried the fixed highwater")
	}
	s.queryErr = nil
	page, err = r.ListSchedulerProjects(context.Background(), request)
	if err == nil || page.ProjectIDs != nil {
		t.Fatal("nil Rows treated as an empty successful page")
	}
	for _, mode := range []string{"committed-cancel", "unknown-cancel", "rollback"} {
		r, s := schedulerProjectsFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		attempt := testID[f.TransactionAttempt](t)
		s.after = func(cause f.TransactionCause) f.CommitResult {
			cancel()
			switch mode {
			case "unknown-cancel":
				return f.UnknownResult(attempt, cause)
			case "rollback":
				return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
			default:
				return f.CommittedResult()
			}
		}
		page, err := r.ListSchedulerProjects(ctx, c.SchedulerProjectPageRequest{Limit: 1})
		cancel()
		if err == nil || page.ProjectIDs != nil || page.Through != nil || page.Complete {
			t.Fatalf("%s returned candidate facts", mode)
		}
		switch mode {
		case "committed-cancel":
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		case "unknown-cancel":
			hasCode(t, err, f.CommitUnknown)
			original, ok := UnknownAttempt(err)
			if !ok || original.AttemptID() != attempt || original.Cause().Kind() != f.RecoveryCause {
				t.Fatal("original Unknown attempt/cause was discarded")
			}
		default:
			hasCode(t, err, f.DependencyUnavailable)
		}
	}
}

func TestSchedulerProjectDiscoveryCompleteRowsAndClose(t *testing.T) {
	one, err := f.ParseID[i.Project]("01960000-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	two, _ := f.ParseID[i.Project]("01960000-0000-7000-8000-000000000002")
	three, _ := f.ParseID[i.Project]("01960000-0000-7000-8000-000000000003")
	position := c.SchedulerProjectPageRequest{Through: &three, Limit: 1}
	values := func(ids ...c.ProjectID) [][]any {
		out := make([][]any, len(ids))
		for n, id := range ids {
			out[n] = []any{id.String(), string(c.Active), true}
		}
		return out
	}
	rows := &readerRows{values: values(one, two)}
	first, err := readSchedulerProjectRows(context.Background(), rows, position)
	if err != nil || first.ValidateFor(position) != nil || first.Complete || !reflect.DeepEqual(first.ProjectIDs, []c.ProjectID{one}) || rows.closed != 1 {
		t.Fatalf("complete bounded first page: %v", err)
	}
	position.After = &one
	rows = &readerRows{values: values(three)}
	last, err := readSchedulerProjectRows(context.Background(), rows, position)
	if err != nil || !last.Complete || !reflect.DeepEqual(last.ProjectIDs, []c.ProjectID{three}) || *last.Through != three || rows.closed != 1 {
		t.Fatalf("fixed highwater continuation: %v", err)
	}
	rows = &readerRows{}
	last, err = readSchedulerProjectRows(context.Background(), rows, position)
	if err != nil || !last.Complete || last.ProjectIDs == nil || len(last.ProjectIDs) != 0 || *last.Through != three {
		t.Fatalf("concurrent membership removal must be a genuine empty final page: %v", err)
	}
	position.After = nil
	for _, damage := range []string{"sentinel-uninitialized", "sentinel-archiving", "sentinel-id", "duplicate", "out-of-range", "over-limit", "late-close", "cancel-close"} {
		ctx, cancel := context.WithCancel(context.Background())
		rows := &readerRows{values: values(one, two)}
		switch damage {
		case "sentinel-uninitialized":
			rows.values[1][2] = false
		case "sentinel-archiving":
			rows.values[1][1] = string(c.Archiving)
		case "sentinel-id":
			rows.values[1][0] = "invalid"
		case "duplicate":
			rows.values[1][0] = one.String()
		case "out-of-range":
			rows.values[1][0] = "01960000-0000-7000-8000-000000000004"
		case "over-limit":
			rows.values = values(one, two, three)
		case "late-close":
			rows.closeFn = func() { rows.err = errors.New("controlled close error") }
		case "cancel-close":
			rows.closeFn = cancel
		}
		got, err := readSchedulerProjectRows(ctx, rows, position)
		cancel()
		if err == nil || got.ProjectIDs != nil || got.Through != nil || got.Complete || rows.closed != 1 {
			t.Fatalf("%s: partial page or unclosed Rows: %v", damage, err)
		}
		if damage == "cancel-close" && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
}
