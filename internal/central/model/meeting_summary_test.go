package model

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type summaryReadStore struct {
	Store
	t               *testing.T
	tx              f.Tx
	actor           id.Actor
	selection, refs []byte
	locks           []f.LockRequest
	sessions        int
	state           f.CommitState
	sqlErr          error
	after           func()
	tail            func(context.Context)
}

func (s *summaryReadStore) WithinTx(ctx context.Context, c f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	if c.Details().Owner != "model.meeting-summary-query" {
		s.t.Fatal("wrong read cause")
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > managementReadBudget {
		s.t.Fatal("read budget missing")
	}
	s.tx = f.NewTx()
	if e := fn(ctx, s.tx); e != nil {
		var ff *f.Fault
		if !errors.As(e, &ff) {
			s.t.Fatal(e)
		}
		return f.NotCommittedResult(ff)
	}
	if s.after != nil {
		s.after()
	}
	switch s.state {
	case f.Unknown:
		return f.UnknownResult(mustID[f.TransactionAttempt](s.t), c)
	case f.NotCommitted:
		return f.NotCommittedResult(fault(f.ResourceBusy))
	}
	return f.CommittedResult()
}
func (s *summaryReadStore) AcquireAll(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	want := []f.LockRequest{userLock(s.actor.Details().UserID), systemLock("model-meeting-summary-selection", f.Shared), systemLock("model-references", f.Shared)}
	if tx != s.tx || len(locks) != len(want) {
		s.t.Fatal("incomplete summary read lock union")
	}
	for i, w := range want {
		if locks[i].Key.Canonical() != w.Key.Canonical() || locks[i].Mode != w.Mode {
			s.t.Fatal("wrong summary read lock")
		}
	}
	s.locks = locks
	return nil
}
func (s *summaryReadStore) RequireHeldLocks(_ context.Context, tx f.Tx, want []f.LockRequest) error {
	if tx != s.tx || len(s.locks) != 3 {
		s.t.Fatal("no actual read locks")
	}
	for _, w := range want {
		found := false
		for _, h := range s.locks {
			if w.Key.Canonical() == h.Key.Canonical() && w.Mode == h.Mode {
				found = true
			}
		}
		if !found {
			s.t.Fatal("missing held lock")
		}
	}
	return nil
}
func (s *summaryReadStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		s.t.Fatal("foreign Tx")
	}
	return s, nil
}
func (s *summaryReadStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	if s.sessions != 1 {
		s.t.Fatal("SQL before current session")
	}
	return scanFunc(func(dst ...any) error {
		if s.sqlErr != nil {
			return s.sqlErr
		}
		if strings.Contains(q, "FROM agenteam_model.meeting_summary_selection") {
			if !strings.Contains(q, "LIMIT 2") {
				s.t.Fatal("missing singleton sentinel")
			}
			*dst[0].(*[]byte) = append([]byte(nil), s.selection...)
		} else if strings.Contains(q, "summary_references") {
			if !strings.Contains(q, "role='meeting_summary' OR owner_id::text=$1") || !strings.Contains(q, "LIMIT 2") {
				s.t.Fatal("incomplete summary reference closure")
			}
			if s.tail != nil {
				s.tail(ctx)
			}
			*dst[0].(*[]byte) = append([]byte(nil), s.refs...)
		} else {
			s.t.Fatal("unexpected summary SQL")
		}
		return nil
	})
}
func summaryReadFixture(t *testing.T) (*Service, *summaryReadStore, meetingSummaryRecord) {
	t.Helper()
	actor := testActor(t)
	at, _ := f.NewInstant(time.Now())
	r := meetingSummaryRecord{ID: mustID[struct{}](t).String(), Version: 9007199254740993, UpdatedAt: at}
	raw, _ := json.Marshal([]any{struct {
		meetingSummaryRecord
		Singleton bool
		Platform  string
	}{r, true, mustID[struct{}](t).String()}})
	store := &summaryReadStore{t: t, actor: actor, selection: raw, refs: []byte(`[]`)}
	auth, e := NewAuthority(store, Authorizations{Sessions: sessionFunc(func(_ context.Context, tx f.Tx, a id.Actor) error {
		if tx != store.tx || !a.Equal(actor) {
			t.Fatal("wrong current session")
		}
		store.sessions++
		return nil
	}), System: systemFunc(func(_ context.Context, tx f.Tx, a id.Actor, intent id.AccessIntent) (id.AccessGrant, error) {
		if tx != store.tx || intent != id.Read {
			t.Fatal("wrong read authority")
		}
		return validGrant(a, intent)
	})})
	if e != nil {
		t.Fatal(e)
	}
	s, e := New(store, auth, testDependencies(t))
	if e != nil {
		t.Fatal(e)
	}
	return s, store, r
}
func TestModelMeetingSummaryReadStateAndTerminal(t *testing.T) {
	for _, scenario := range []string{"unconfigured", "configured", "uninitialized", "absent-with-edge", "extra-owner", "extra-role", "wrong-version", "unknown", "rollback", "canceled", "missing-schema", "bad-json", "two-singletons", "collision"} {
		t.Run(scenario, func(t *testing.T) {
			s, store, r := summaryReadFixture(t)
			want := f.Code("")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "configured":
				r.Model = mustID[mc.Model](t).String()
				store.selection, _ = json.Marshal([]any{struct {
					meetingSummaryRecord
					Singleton bool
					Platform  string
				}{r, true, mustID[struct{}](t).String()}})
				store.refs, _ = json.Marshal(meetingSummaryReferences(&r))
			case "uninitialized":
				store.selection = []byte(`[]`)
				want = f.DependencyUnbound
			case "absent-with-edge":
				store.selection = []byte(`[]`)
				store.refs = []byte(`[{}]`)
				want = f.DependencyUnavailable
			case "extra-owner", "extra-role", "wrong-version":
				r.Model = mustID[mc.Model](t).String()
				store.selection, _ = json.Marshal([]any{struct {
					meetingSummaryRecord
					Singleton bool
					Platform  string
				}{r, true, mustID[struct{}](t).String()}})
				refs := meetingSummaryReferences(&r)
				if scenario == "extra-owner" {
					refs[0].Owner = mustID[struct{}](t).String()
				} else if scenario == "extra-role" {
					refs[0].Role = "memory"
				} else {
					refs[0].Version++
				}
				store.refs, _ = json.Marshal(refs)
				want = f.DependencyUnavailable
			case "unknown":
				store.state = f.Unknown
				want = f.CommitUnknown
			case "rollback":
				store.state = f.NotCommitted
				want = f.ResourceBusy
			case "canceled":
				store.after = cancel
				want = f.DependencyUnavailable
			case "missing-schema":
				store.sqlErr = errors.New("missing relation")
				want = f.DependencyUnavailable
			case "bad-json":
				store.selection = []byte(`null`)
				want = f.DependencyUnavailable
			case "two-singletons":
				store.selection = []byte(`[{},{}]`)
				want = f.DependencyUnavailable
			case "collision":
				store.selection, _ = json.Marshal([]any{struct {
					meetingSummaryRecord
					Singleton bool
					Platform  string
				}{r, true, r.ID}})
				want = f.DependencyUnavailable
			}
			out, e := s.GetMeetingSummarySelection(ctx, store.actor)
			if want != "" {
				requireCode(t, e, want)
				if !reflect.DeepEqual(out, mc.MeetingSummarySelection{}) {
					t.Fatal("candidate escaped unsuccessful query")
				}
				return
			}
			if e != nil || out.ID != r.ID || out.Version != r.Version || (out.Model != nil) != (r.Model != "") {
				t.Fatal("wrong safe state", e)
			}
			raw, _ := json.Marshal(out)
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(raw, &fields)
			if len(fields) != 3 || string(fields["version"]) != `"9007199254740993"` {
				t.Fatal("unsafe or lossy projection")
			}
		})
	}
}
func TestModelMeetingSummaryReadWaitsForActualTail(t *testing.T) {
	s, store, _ := summaryReadFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	store.tail = func(context.Context) { close(entered); <-release }
	go func() { _, e := s.GetMeetingSummarySelection(ctx, store.actor); done <- e }()
	<-entered
	cancel()
	select {
	case <-done:
		t.Fatal("query returned before actual SQL completion")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	requireCode(t, <-done, f.DependencyUnavailable)
}
func TestModelMeetingSummaryInvalidRequestsPerformNoIO(t *testing.T) {
	store := &noIOStore{}
	s, e := New(store, pureAuthority(t, store), testDependencies(t))
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.GetMeetingSummarySelection(context.Background(), id.Actor{})
	requireCode(t, e, f.Unauthenticated)
	_, e = s.UpdateMeetingSummarySelection(context.Background(), mc.UpdateMeetingSummarySelectionRequest{})
	requireCode(t, e, f.InvalidArgument)
	var missing *Service
	if e = missing.InitializeMeetingSummarySelection(context.Background()); e == nil {
		t.Fatal("missing service initialized")
	}
}
