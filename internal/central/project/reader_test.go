package project

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func readerKeys(t *testing.T) cursor.Keyring {
	t.Helper()
	k, e := cursor.LoadKeyring(`{"format":1,"current_kid":"read","keys":[{"kid":"read","key_b64":"` + base64.StdEncoding.EncodeToString(make([]byte, 32)) + `"}]}`)
	if e != nil {
		t.Fatal(e)
	}
	return k
}

type readerStore struct {
	Store
	tx             f.Tx
	locks          []f.LockRequest
	calls, queries int
	row            postgres.Row
	after          func(f.TransactionCause) f.CommitResult
	query          func(string, []any)
}

func (s *readerStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.calls++
	s.locks = nil
	if err := fn(ctx, s.tx); err != nil {
		var fault *f.Fault
		if !errors.As(err, &fault) {
			fault = f.NewFault(f.DependencyUnavailable, f.NotCommitted)
		}
		return f.NotCommittedResult(fault)
	}
	if s.after != nil {
		return s.after(cause)
	}
	return f.CommittedResult()
}
func (s *readerStore) AcquireAll(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if tx != s.tx {
		return errors.New("foreign tx")
	}
	s.locks = append([]f.LockRequest{}, locks...)
	return nil
}
func (s *readerStore) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if tx != s.tx || len(locks) != len(s.locks) {
		return errors.New("missing held locks")
	}
	for i, lock := range locks {
		if f.CompareLockKeys(lock.Key, s.locks[i].Key) != 0 || lock.Mode != s.locks[i].Mode {
			return errors.New("missing held locks")
		}
	}
	return nil
}
func (s *readerStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, errors.New("foreign tx")
	}
	return s, nil
}
func (s *readerStore) QueryRow(_ context.Context, q string, _ ...any) postgres.Row {
	s.queries++
	if q == "SELECT clock_timestamp()" {
		return valuesRow(time.Now().UTC().Truncate(time.Microsecond))
	}
	return s.row
}
func (s *readerStore) Query(_ context.Context, q string, args ...any) (*postgres.Rows, error) {
	s.queries++
	if s.query != nil {
		s.query(q, args)
	}
	return nil, errors.New("controlled query terminal")
}

func readerFixture(t *testing.T) (*Reader, *readerStore, id.Actor, c.ProjectRef) {
	t.Helper()
	actor := testActor(t)
	ref := testProject(t)
	ref.OwnerUserID, _ = f.ParseID[id.User](actor.Details().UserID)
	st := &readerStore{tx: f.NewTx()}
	st.row = valuesRow(readerValues(t, ref)...)
	auth, e := NewAuthority(st, AuthorityDependencies{Sessions: sessionFunc(func(_ context.Context, tx f.Tx, a id.Actor) error {
		if tx != st.tx || !a.Equal(actor) || len(st.locks) == 0 {
			return errors.New("unbound current session")
		}
		return nil
	})})
	if e != nil {
		t.Fatal(e)
	}
	r, e := NewReader(st, auth, readerKeys(t))
	if e != nil {
		t.Fatal(e)
	}
	return r, st, actor, ref
}
func readerValues(t *testing.T, ref c.ProjectRef) []any {
	var sprint, archived, operation any
	if ref.CurrentSprintID != nil {
		sprint = ptrString(ref.CurrentSprintID.String())
	}
	if ref.ArchivedAt != nil {
		v := ref.ArchivedAt.Time()
		archived = &v
	}
	if ref.Lifecycle == c.Archiving || ref.Lifecycle == c.Deleting {
		operation = ptrString(testID[c.Operation](t).String())
	}
	return []any{ref.ID.String(), ref.OwnerUserID.String(), ref.Name, ref.NormalizedName, ref.Description, string(ref.Lifecycle), int64(ref.Version), sprint, ref.CreatedAt.Time(), ref.UpdatedAt.Time(), archived, testID[c.Creation](t).String(), true, operation}
}
func ptrString(s string) *string { return &s }

func TestProjectReaderPureConstructionAndServiceAdmission(t *testing.T) {
	r, st, actor, ref := readerFixture(t)
	var typedNil *readerStore
	for _, store := range []Store{nil, typedNil, &readerStore{tx: st.tx}} {
		if got, e := NewReader(store, r.authority, r.cursors); e == nil || got != nil {
			t.Fatal("foreign/unbound store accepted")
		}
	}
	for _, a := range []*Authority{nil, {}} {
		if got, e := NewReader(st, a, r.cursors); e == nil || got != nil {
			t.Fatal("unbound authority accepted")
		}
	}
	if _, e := NewReader(st, r.authority, cursor.Keyring{}); e == nil {
		t.Fatal("invalid cursors accepted")
	}
	if st.calls != 0 || st.queries != 0 {
		t.Fatal("constructor performed I/O")
	}
	for _, zero := range []*Reader{nil, {}} {
		_, e := zero.GetProject(context.Background(), actor, ref.ID)
		hasCode(t, e, f.DependencyUnbound)
	}
	state := &serviceState{store: st, deps: Dependencies{Authority: r.authority, Cursors: r.cursors}, calls: map[*call]struct{}{}, changed: make(chan struct{})}
	service := &Service{data: func() *serviceState { return state }}
	got, e := service.GetProject(context.Background(), actor, ref.ID)
	if e != nil || !reflect.DeepEqual(got, ref) || len(state.calls) != 0 {
		t.Fatal("legacy wrapper diverged", e)
	}
	service.Stop()
	_, e = service.GetProject(context.Background(), actor, ref.ID)
	hasCode(t, e, f.ShuttingDown)
	got, e = r.GetProject(context.Background(), actor, ref.ID)
	if e != nil || got.ID != ref.ID {
		t.Fatal("read-only reader borrowed command admission", e)
	}
}

func TestProjectReaderPureAuthorityAndCommitTerminal(t *testing.T) {
	r, st, actor, ref := readerFixture(t)
	for _, kind := range []string{"foreign-owner", "pending", "deleting", "revoked", "unknown", "rollback", "committed-cancel"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			raw := readerValues(t, ref)
			st.after = nil
			oldSessions := r.authority.state().sessions
			defer func() { r.authority.state().sessions = oldSessions }()
			want := f.ProjectNotActive
			switch kind {
			case "foreign-owner":
				raw[1] = testID[id.User](t).String()
				want = f.NotFound
			case "pending":
				raw[12] = false
			case "deleting":
				raw[5] = string(c.Deleting)
				raw[13] = ptrString(testID[c.Operation](t).String())
			case "revoked":
				r.authority.state().sessions = sessionFunc(func(context.Context, f.Tx, id.Actor) error { return fault(f.SessionRevoked) })
				want = f.SessionRevoked
			case "unknown":
				want = f.CommitUnknown
				st.after = func(cause f.TransactionCause) f.CommitResult {
					return f.UnknownResult(testID[f.TransactionAttempt](t), cause)
				}
			case "rollback":
				want = f.DependencyUnavailable
				st.after = func(f.TransactionCause) f.CommitResult { return f.NotCommittedResult(fault(f.DependencyUnavailable)) }
			case "committed-cancel":
				st.after = func(f.TransactionCause) f.CommitResult { cancel(); return f.CommittedResult() }
			}
			st.row = valuesRow(raw...)
			got, e := r.GetProject(ctx, actor, ref.ID)
			if got.ID != (c.ProjectID{}) || e == nil {
				t.Fatal("candidate escaped rejected/uncertain read")
			}
			if kind == "committed-cancel" {
				if !errors.Is(e, context.Canceled) {
					t.Fatal(e)
				}
			} else {
				hasCode(t, e, want)
			}
			if kind == "unknown" {
				original, ok := UnknownAttempt(e)
				var fault *f.Fault
				if !ok || original.State() != f.Unknown || !errors.As(e, &fault) || fault.CauseID != original.AttemptID().String() || original.Cause().Kind() != f.RecoveryCause {
					t.Fatal("original physical attempt erased")
				}
			}
		})
	}
}

type readerRows struct {
	values        [][]any
	index, closed int
	err           error
	closeFn       func()
}

func (r *readerRows) Next() bool             { r.index++; return r.index <= len(r.values) }
func (r *readerRows) Scan(dest ...any) error { return valuesRow(r.values[r.index-1]...).Scan(dest...) }
func (r *readerRows) Err() error             { return r.err }
func (r *readerRows) Close() {
	r.closed++
	if r.closeFn != nil {
		r.closeFn()
	}
}
func readerBinding(t *testing.T, owner id.UserID, request c.ListOwnedProjectsRequest) cursor.Binding {
	t.Helper()
	d, e := c.OwnedProjectsQueryDigest(owner, request)
	if e != nil {
		t.Fatal(e)
	}
	return cursor.Binding{Scope: id.SystemScope(), QueryDigest: d, Order: "created_at-desc,id-desc"}
}

func TestProjectReaderPureCompleteRowsAndSentinel(t *testing.T) {
	r, _, _, ref := readerFixture(t)
	filter, _ := (c.ListOwnedProjectsRequest{}).NormalizedFilter()
	binding := readerBinding(t, ref.OwnerUserID, c.ListOwnedProjectsRequest{})
	// Fix a genuine descending UUID tie-break at one timestamp.
	ref.ID, _ = f.ParseID[id.Project]("01900000-0000-7000-8000-000000000003")
	next := ref
	next.ID, _ = f.ParseID[id.Project]("01900000-0000-7000-8000-000000000002")
	for _, at := range []int{0, 1} {
		for _, damage := range []string{"owner", "pending", "creation", "name", "description", "version", "operation", "lifecycle", "time", "duplicate", "order", "late-error", "close-error", "extra"} {
			t.Run(string(rune('0'+at))+"/"+damage, func(t *testing.T) {
				rows := &readerRows{values: [][]any{readerValues(t, ref), readerValues(t, next)}}
				bad := rows.values[at]
				switch damage {
				case "owner":
					bad[1] = testID[id.User](t).String()
				case "pending":
					bad[12] = false
				case "creation":
					bad[11] = "bad"
				case "name":
					bad[3] = "wrong-normalized"
				case "description":
					bad[4] = "\x00"
				case "version":
					bad[6] = int64(0)
				case "operation":
					bad[5] = string(c.Archiving)
					bad[13] = nil
				case "lifecycle":
					bad[5] = "unknown"
				case "time":
					bad[9] = ref.CreatedAt.Time().Add(-time.Second)
				case "duplicate":
					rows.values[1][0] = rows.values[0][0]
				case "order":
					rows.values[0], rows.values[1] = rows.values[1], rows.values[0]
				case "late-error":
					rows.err = errors.New("late rows failure")
				case "close-error":
					rows.closeFn = func() { rows.err = errors.New("close terminal") }
				case "extra":
					rows.values = append(rows.values, readerValues(t, next))
				}
				got, e := readProjectRows(rows, ref.OwnerUserID, filter, f.PageRequest{Limit: 1}, r.cursors, binding)
				if e == nil || got.Items != nil || got.NextCursor != "" || rows.closed != 1 {
					t.Fatal("bad full/sentinel row leaked", damage, e, rows.closed)
				}
			})
		}
	}
	rows := &readerRows{values: [][]any{readerValues(t, ref), readerValues(t, next)}}
	got, e := readProjectRows(rows, ref.OwnerUserID, filter, f.PageRequest{Limit: 1}, r.cursors, binding)
	if e != nil || len(got.Items) != 1 || got.Items[0].ID != ref.ID || got.NextCursor == "" || rows.closed != 1 {
		t.Fatal("valid page", e)
	}
	position, e := r.cursors.Verify(got.NextCursor, binding)
	if e != nil || len(position.Scalars) != 2 || position.Scalars[1].Value() != ref.ID.String() {
		t.Fatal("cursor used sentinel instead of last visible", e)
	}
	empty, e := readProjectRows(&readerRows{}, ref.OwnerUserID, filter, f.PageRequest{Limit: 1}, r.cursors, binding)
	if e != nil || empty.Items == nil || len(empty.Items) != 0 || empty.NextCursor != "" {
		t.Fatal("empty page", e)
	}
}

func TestProjectReaderPureStateProjectionAndActualClose(t *testing.T) {
	r, _, _, ref := readerFixture(t)
	filter, _ := (c.ListOwnedProjectsRequest{}).NormalizedFilter()
	binding := readerBinding(t, ref.OwnerUserID, c.ListOwnedProjectsRequest{})
	for _, state := range filter {
		t.Run(string(state), func(t *testing.T) {
			p := ref
			p.Lifecycle = state
			if state == c.Archived {
				at := p.UpdatedAt
				p.ArchivedAt = &at
			}
			values := readerValues(t, p)
			values[13] = ptrString(testID[c.Operation](t).String())
			got, e := readProjectRows(&readerRows{values: [][]any{values}}, ref.OwnerUserID, filter, f.PageRequest{Limit: 1}, r.cursors, binding)
			if e != nil {
				t.Fatal(e)
			}
			item := got.Items[0]
			if (item.Description == nil) != (state == c.Deleting) || (item.OperationID != nil) != (state == c.Archiving || state == c.Deleting) {
				t.Fatal("state projection leaked hidden content")
			}
		})
	}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	rows := &readerRows{values: [][]any{readerValues(t, ref)}, closeFn: func() { close(entered); <-release }}
	go func() {
		defer close(done)
		_, _ = readProjectRows(rows, ref.OwnerUserID, filter, f.PageRequest{Limit: 1}, r.cursors, binding)
	}()
	<-entered
	select {
	case <-done:
		t.Fatal("Close not joined")
	default:
	}
	close(release)
	<-done
	if rows.closed != 1 {
		t.Fatal("Close ownership duplicated")
	}
}

func TestProjectReaderPureCursorBindingAndCurrentSession(t *testing.T) {
	r, st, actor, ref := readerFixture(t)
	request := c.ListOwnedProjectsRequest{Lifecycle: []c.Lifecycle{c.Archived, c.Active}}
	binding := readerBinding(t, ref.OwnerUserID, request)
	ti, _ := cursor.Instant(ref.CreatedAt)
	uuid, _ := cursor.UUID(ref.ID.String())
	token, e := r.cursors.Sign(binding, cursor.Position{Scalars: []cursor.Scalar{ti, uuid}})
	if e != nil {
		t.Fatal(e)
	}
	st.query = func(q string, args []any) {
		if !strings.Contains(q, "initialized_at IS NOT NULL") || !strings.Contains(q, "ORDER BY created_at DESC,id DESC LIMIT $5") || args[0] != actor.Details().UserID || args[4] != 8 || args[3] != ref.ID.String() || len(st.locks) != 1 {
			t.Fatal("owner/cursor/query boundary changed")
		}
	}
	_, e = r.ListOwnedProjects(context.Background(), actor, c.ListOwnedProjectsRequest{Lifecycle: []c.Lifecycle{c.Active, c.Archived}}, f.PageRequest{Limit: 7, Cursor: token})
	hasCode(t, e, f.DependencyUnavailable)
	if st.queries != 1 {
		t.Fatal("equivalent filter and changed page limit rejected before query")
	}
	st.query = nil
	for _, tc := range []struct {
		name    string
		actor   id.Actor
		request c.ListOwnedProjectsRequest
		token   string
	}{{"owner", testActor(t), request, token}, {"filter", actor, c.ListOwnedProjectsRequest{}, token}, {"tamper", actor, request, token + "x"}} {
		t.Run(tc.name, func(t *testing.T) {
			before := st.calls
			_, e := r.ListOwnedProjects(context.Background(), tc.actor, tc.request, f.PageRequest{Limit: 7, Cursor: tc.token})
			hasCode(t, e, f.CursorInvalid)
			if st.calls != before {
				t.Fatal("invalid cursor entered read transaction")
			}
		})
	}
	before := st.queries
	r.authority.state().sessions = sessionFunc(func(context.Context, f.Tx, id.Actor) error { return fault(f.SessionRevoked) })
	_, e = r.ListOwnedProjects(context.Background(), actor, request, f.PageRequest{Limit: 7, Cursor: token})
	hasCode(t, e, f.SessionRevoked)
	if st.queries != before {
		t.Fatal("valid cursor bypassed current Session")
	}
}
