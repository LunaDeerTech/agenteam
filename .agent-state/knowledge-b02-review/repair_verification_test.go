package knowledge

import (
	"context"
	"errors"
	"fmt"
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
	object "github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func repairService(store Store, cleaner oc.Cleaner) *Service {
	st := &serviceState{store: store, deps: Dependencies{ObjectCleanup: cleaner}, calls: make(map[*call]struct{}), changed: make(chan struct{})}
	return &Service{data: func() *serviceState { return st }}
}

func repairCallCount(s *Service) int {
	s.state().mu.Lock()
	defer s.state().mu.Unlock()
	return len(s.state().calls)
}

func repairWait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("controlled operation did not reach/leave actual boundary")
	}
}

// The real D05 constructor, cancellation callback and Close monitor execute.
// The memory source and controlled release are not durable lease evidence.
func repairRealReader(t *testing.T, s *Service, release func() error) *trackedRead {
	t.Helper()
	run, done, err := s.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	payload := strings.Repeat("r", 2*oc.StreamBufferSize+1)
	body, err := object.ReviewB02IntegrityReader(run, io.NopCloser(strings.NewReader(payload)), int64(len(payload)), ob.DigestBytes([]byte(payload)), release)
	if err != nil {
		done()
		t.Fatal(err)
	}
	scope, _ := id.InProject(newID[id.Project](t))
	now, _ := f.NewInstant(time.Now())
	reader, err := oc.NewObjectReader(oc.ObjectMeta{ID: newID[oc.StoredObject](t), Scope: scope, MediaType: kc.PlainText, ByteSize: f.Progress(len(payload)), SHA256: ob.DigestBytes([]byte(payload)), State: oc.Available, Version: 1, CreatedAt: now}, nil, body)
	if err != nil {
		_ = body.Close()
		done()
		t.Fatal(err)
	}
	return &trackedRead{body: reader, done: done}
}

func TestIndependentB02RepairActualObjectClose(t *testing.T) {
	for _, mode := range []string{"ordinary", "release_error", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			s := repairService(nil, nil)
			var releases atomic.Int32
			released := make(chan struct{})
			failure := errors.New("controlled release failure")
			r := repairRealReader(t, s, func() error {
				if releases.Add(1) == 1 {
					close(released)
				}
				if mode == "release_error" {
					return failure
				}
				return nil
			})
			defer r.Close()
			if mode == "cancelled" {
				s.Stop()
				repairWait(t, released)
			}
			for range 2 {
				err := r.Close()
				if mode == "ordinary" && err != nil || mode == "release_error" && err != failure || mode == "cancelled" && !errors.Is(err, context.Canceled) {
					t.Fatal("original typed Close outcome changed", mode, err)
				}
				if repairCallCount(s) != 0 || releases.Load() != 1 {
					t.Fatal("actual Close left a call or repeated release")
				}
				if err := s.Drain(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
	t.Run("cancelled_release_still_blocked", func(t *testing.T) {
		s := repairService(nil, nil)
		entered, release, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		r := repairRealReader(t, s, func() error { close(entered); <-release; return nil })
		defer func() { unblock(); _ = r.Close() }()
		s.Stop()
		repairWait(t, entered)
		result := make(chan error, 1)
		go func() { defer close(joined); result <- r.Close() }()
		defer func() { unblock(); repairWait(t, joined) }()
		budget, cancel := context.WithCancel(context.Background())
		cancel()
		if err := s.Drain(budget); !errors.Is(err, context.Canceled) || repairCallCount(s) != 1 {
			t.Fatal("cancellation retired the blocked actual Close", err)
		}
		select {
		case <-joined:
			t.Fatal("Close escaped a blocked release")
		default:
		}
		unblock()
		repairWait(t, joined)
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatal("Close lost original cancellation", err)
		}
		if err := s.Drain(context.Background()); err != nil || repairCallCount(s) != 0 {
			t.Fatal("actual Close return failed to retire", err)
		}
	})
}

type repairPage struct {
	after, through string
	keys           []string
}
type repairRow struct {
	values []any
	err    error
}

func (r repairRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	return (sourceRow{values: r.values}).Scan(dest...)
}

// Scripted SQL responses assert the next public invocation's exact cursor.
// They deliberately do not implement a second copy of the scheduling loop.
type repairStore struct {
	Store
	frontiers []string
	pages     []repairPage
	records   map[string]sourceRow
	queries   int
}

func (s *repairStore) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	s.queries++
	switch {
	case strings.Contains(query, "ORDER BY id DESC LIMIT 1"):
		if len(s.frontiers) == 0 || len(args) != 0 {
			return repairRow{err: errors.New("unexpected new pass")}
		}
		v := s.frontiers[0]
		s.frontiers = s.frontiers[1:]
		return repairRow{values: []any{v}}
	case strings.Contains(query, "array_agg"):
		if len(s.pages) == 0 || len(args) != 2 || !strings.Contains(query, "LIMIT 33") {
			return repairRow{err: errors.New("unexpected/unbounded page")}
		}
		p := s.pages[0]
		s.pages = s.pages[1:]
		var after any
		if p.after != "" {
			after = p.after
		}
		if !reflect.DeepEqual(args, []any{after, p.through}) {
			return repairRow{err: fmt.Errorf("wrong page arguments: %v", args)}
		}
		return repairRow{values: []any{p.keys}}
	case strings.Contains(query, "FROM agenteam_knowledge.object_cleanup WHERE id=$1"):
		if len(args) != 1 {
			return repairRow{err: errors.New("invalid exact item query")}
		}
		row, ok := s.records[args[0].(string)]
		if !ok {
			return repairRow{err: errors.New("unknown exact item")}
		}
		return row
	default:
		return repairRow{err: errors.New("unexpected recovery SQL")}
	}
}

type repairCleaner struct {
	oc.Cleaner
	seen []oc.CleanupDetails
	hook func(context.Context, oc.CleanupDetails) error
}

func (c *repairCleaner) DeleteUnreferenced(ctx context.Context, cause oc.ObjectCleanupCause, _ oc.ObjectID) (oc.CleanupResult, error) {
	d := cause.Details()
	c.seen = append(c.seen, d)
	if c.hook != nil {
		if err := c.hook(ctx, d); err != nil {
			return oc.CleanupResult{}, err
		}
	}
	return oc.CleanupResult{OperationID: d.OperationID, State: oc.CleanupPending}, nil
}
func repairKeys(t *testing.T, count int) ([]string, *repairStore, *repairCleaner) {
	t.Helper()
	keys := make([]string, count)
	store := &repairStore{records: make(map[string]sourceRow)}
	for n := range count {
		key, err := f.ParseID[oc.CleanupOperation](fmt.Sprintf("01900000-0000-7000-8000-%012x", n+1))
		if err != nil {
			t.Fatal(err)
		}
		keys[n] = key.String()
		store.records[key.String()] = sourceRow{values: []any{key.String(), newID[id.Project](t).String(), newID[kc.Document](t).String(), newID[command](t).String(), newID[oc.StoredObject](t).String(), newID[oc.Upload](t).String(), "owner_deleted", "object"}}
	}
	return keys, store, &repairCleaner{}
}
func repairBusy(t *testing.T, err error) {
	t.Helper()
	var known *f.Fault
	if !errors.As(err, &known) || known.Code != f.ResourceBusy || known.CommitState == f.Unknown {
		t.Fatal("pending became another outcome", err)
	}
}
func repairSeen(t *testing.T, c *repairCleaner, want []string) {
	t.Helper()
	got := make([]string, len(c.seen))
	for n, d := range c.seen {
		got[n] = d.OperationID.String()
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wrong attempted identities: got %v want %v", got, want)
	}
}

func TestIndependentB02RepairFairBoundedCleanup(t *testing.T) {
	keys, store, cleaner := repairKeys(t, 67)
	// First pass spans three public calls (65 old items). New lower and upper
	// IDs enter only the next finite pass; all rows remain Pending throughout.
	old := keys[1:66]
	store.frontiers = []string{old[64], keys[66]}
	store.pages = []repairPage{
		{"", old[64], old[:33]}, {old[31], old[64], old[32:65]}, {old[63], old[64], old[64:]},
		{"", keys[66], keys[:33]}, {keys[31], keys[66], keys[32:65]}, {keys[63], keys[66], keys[64:]},
	}
	s := repairService(store, cleaner)
	counts := []int{32, 32, 1, 32, 32, 3}
	for _, count := range counts {
		before := len(cleaner.seen)
		repairBusy(t, s.RecoverCleanup(context.Background()))
		if len(cleaner.seen)-before != count {
			t.Fatal("attempt budget/fair pass failed", len(cleaner.seen)-before, count)
		}
	}
	want := append(append([]string{}, old...), keys...)
	repairSeen(t, cleaner, want)
	if len(store.frontiers) != 0 || len(store.pages) != 0 || s.state().cleanupCursor != (cleanupCursor{}) {
		t.Fatal("script/cursor did not finish two finite passes")
	}
	if err := s.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestIndependentB02RepairFailureRetainsExactCause(t *testing.T) {
	for _, mode := range []string{"hard", "unknown", "busy_unknown"} {
		t.Run(mode, func(t *testing.T) {
			keys, store, cleaner := repairKeys(t, 3)
			store.frontiers = []string{keys[2]}
			store.pages = []repairPage{{"", keys[2], keys}, {keys[0], keys[2], keys[1:]}}
			underlying := errors.New("original dependency cause")
			var original error = f.NewFault(f.DependencyUnavailable, f.NotStarted).WithCause(underlying)
			attempt := newID[f.TransactionAttempt](t)
			cause, err := f.NewRecoveryCause("knowledge.cleanup", keys[1], "")
			if err != nil {
				t.Fatal(err)
			}
			if mode != "hard" {
				original = txError(f.UnknownResult(attempt, cause))
				if mode == "busy_unknown" {
					original.(*f.Fault).Code = f.ResourceBusy
				}
			}
			cleaner.hook = func(_ context.Context, d oc.CleanupDetails) error {
				if d.OperationID.String() == keys[1] {
					return original
				}
				return nil
			}
			s := repairService(store, cleaner)
			for range 2 {
				if err := s.RecoverCleanup(context.Background()); err != original {
					t.Fatal("pending masked/replaced original failure", err)
				}
			}
			repairSeen(t, cleaner, []string{keys[0], keys[1], keys[1]})
			first, retry := cleaner.seen[1], cleaner.seen[2]
			if first.OperationID != retry.OperationID || first.Reason != retry.Reason || !first.Owner.Equal(retry.Owner) {
				t.Fatal("retry changed original cleanup cause")
			}
			if mode == "hard" {
				if !errors.Is(original, underlying) {
					t.Fatal("private cause lost")
				}
			} else {
				var failure commitFailure
				if !errors.As(original, &failure) || failure.result.AttemptID() != attempt || !reflect.DeepEqual(failure.result.Cause().Details(), cause.Details()) || original.(*f.Fault).CauseID != attempt.String() {
					t.Fatal("original Unknown identity lost")
				}
			}
		})
	}
}

func TestIndependentB02RepairConcurrentScanActualJoin(t *testing.T) {
	keys, store, cleaner := repairKeys(t, 2)
	store.frontiers = []string{keys[1]}
	store.pages = []repairPage{{"", keys[1], keys}, {keys[0], keys[1], keys[1:]}}
	entered, release, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	cleaner.hook = func(_ context.Context, d oc.CleanupDetails) error {
		if d.OperationID.String() == keys[0] {
			close(entered)
			<-release
		}
		return nil
	}
	s := repairService(store, cleaner)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { defer close(joined); result <- s.RecoverCleanup(ctx) }()
	defer func() { unblock(); repairWait(t, joined) }()
	repairWait(t, entered)
	queries := store.queries
	repairBusy(t, s.RecoverCleanup(context.Background()))
	if store.queries != queries || repairCallCount(s) != 1 {
		t.Fatal("concurrent admission queried or stole live call")
	}
	cancel()
	budget, stop := context.WithCancel(context.Background())
	stop()
	if err := s.Drain(budget); !errors.Is(err, context.Canceled) {
		t.Fatal("blocked cleaner declared joined", err)
	}
	unblock()
	repairWait(t, joined)
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal("original cancellation lost", err)
	}
	cleaner.hook = nil
	repairBusy(t, s.RecoverCleanup(context.Background()))
	repairSeen(t, cleaner, keys)
	if err := s.Drain(context.Background()); err != nil || repairCallCount(s) != 0 {
		t.Fatal("returned cleanup did not join", err)
	}
}
