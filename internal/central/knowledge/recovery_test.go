package knowledge

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type cleanupScanRowError struct{ err error }

func (r cleanupScanRowError) Scan(...any) error { return r.err }

// These rows control scheduling, not SQL execution or physical deletion. The
// public scheduler and exact per-item cause/phase validation both execute.
type cleanupScanStore struct {
	Store
	keys      []string
	records   map[string]sourceRow
	frontiers int
	queries   int
	pageErr   error
}

func (s *cleanupScanStore) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	s.queries++
	switch {
	case strings.Contains(query, "ORDER BY id DESC LIMIT 1"):
		s.frontiers++
		if len(s.keys) == 0 {
			return cleanupScanRowError{pgx.ErrNoRows}
		}
		return sourceRow{values: []any{s.keys[len(s.keys)-1]}}
	case strings.Contains(query, "array_agg"):
		if s.pageErr != nil {
			return cleanupScanRowError{s.pageErr}
		}
		if len(args) != 2 || !strings.Contains(query, "LIMIT 33") {
			panic("unbounded or malformed recovery page")
		}
		after := ""
		if args[0] != nil {
			after = args[0].(string)
		}
		through := args[1].(string)
		keys := []string{}
		for _, key := range s.keys {
			if key > after && key <= through {
				keys = append(keys, key)
			}
			if len(keys) == 33 {
				break
			}
		}
		return sourceRow{values: []any{keys}}
	case strings.Contains(query, "SELECT EXISTS"):
		return sourceRow{values: []any{len(s.keys) != 0}}
	case strings.Contains(query, "FROM agenteam_knowledge.object_cleanup WHERE id=$1"):
		return s.records[args[0].(string)]
	default:
		panic("unexpected cleanup SQL")
	}
}

type cleanupScanCleaner struct {
	oc.Cleaner
	seen []oc.CleanupID
	hook func(oc.CleanupID) error
}

func (c *cleanupScanCleaner) DeleteUnreferenced(_ context.Context, cause oc.ObjectCleanupCause, _ oc.ObjectID) (oc.CleanupResult, error) {
	key := cause.Details().OperationID
	c.seen = append(c.seen, key)
	if c.hook != nil {
		if err := c.hook(key); err != nil {
			return oc.CleanupResult{}, err
		}
	}
	return oc.CleanupResult{OperationID: key, State: oc.CleanupPending}, nil
}

func cleanupScanFixture(t *testing.T, count int) (*Service, *cleanupScanStore, *cleanupScanCleaner) {
	t.Helper()
	store := &cleanupScanStore{records: make(map[string]sourceRow)}
	for n := range count {
		key, err := f.ParseID[oc.CleanupOperation](fmt.Sprintf("01900000-0000-7000-8000-%012x", n+1))
		if err != nil {
			t.Fatal(err)
		}
		store.keys = append(store.keys, key.String())
		// Distinct projects establish that no shared business cause is required.
		store.records[key.String()] = sourceRow{values: []any{key.String(), newID[id.Project](t).String(), newID[kc.Document](t).String(), newID[command](t).String(), newID[oc.StoredObject](t).String(), newID[oc.Upload](t).String(), "owner_deleted", "object"}}
	}
	sort.Strings(store.keys)
	cleaner := &cleanupScanCleaner{}
	st := &serviceState{store: store, deps: Dependencies{ObjectCleanup: cleaner}, calls: make(map[*call]struct{}), changed: make(chan struct{})}
	return &Service{data: func() *serviceState { return st }}, store, cleaner
}

func requireCleanupBusy(t *testing.T, err error) {
	t.Helper()
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.ResourceBusy || fault.CommitState == f.Unknown {
		t.Fatal("pending work became success or lost its outcome", err)
	}
}

func TestCleanupPendingDoesNotStarveOtherProjects(t *testing.T) {
	s, store, cleaner := cleanupScanFixture(t, 2)
	for round := range 3 {
		requireCleanupBusy(t, s.RecoverCleanup(context.Background()))
		if len(cleaner.seen) != (round+1)*2 || cleaner.seen[round*2].String() != store.keys[0] || cleaner.seen[round*2+1].String() != store.keys[1] {
			t.Fatal("first pending item starved another project", cleaner.seen)
		}
	}
	if err := s.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupBatchBoundAndFixedPassFrontier(t *testing.T) {
	s, store, cleaner := cleanupScanFixture(t, 35)
	requireCleanupBusy(t, s.RecoverCleanup(context.Background()))
	if len(cleaner.seen) != 32 || store.frontiers != 1 {
		t.Fatal("batch was not bounded", len(cleaner.seen), store.frontiers)
	}
	frontier := store.keys[34]
	arrival, err := f.ParseID[oc.CleanupOperation]("01900000-0000-7000-8000-000000000100")
	if err != nil {
		t.Fatal(err)
	}
	if arrival.String() <= frontier {
		t.Fatal("test arrival must be after captured frontier")
	}
	store.keys = append(store.keys, arrival.String())
	store.records[arrival.String()] = sourceRow{values: []any{arrival.String(), newID[id.Project](t).String(), newID[kc.Document](t).String(), newID[command](t).String(), newID[oc.StoredObject](t).String(), newID[oc.Upload](t).String(), "owner_deleted", "object"}}
	requireCleanupBusy(t, s.RecoverCleanup(context.Background()))
	if len(cleaner.seen) != 35 || cleaner.seen[34].String() != frontier || store.frontiers != 1 || s.state().cleanupCursor != (cleanupCursor{}) {
		t.Fatal("new arrival extended old pass or later batch did not run")
	}
	requireCleanupBusy(t, s.RecoverCleanup(context.Background()))
	if len(cleaner.seen) != 67 || cleaner.seen[35].String() != store.keys[0] || store.frontiers != 2 {
		t.Fatal("old pending work was not revisited in a fresh finite pass")
	}
	requireCleanupBusy(t, s.RecoverCleanup(context.Background()))
	if len(cleaner.seen) != 71 || cleaner.seen[70] != arrival {
		t.Fatal("new work never entered the next bounded pass")
	}
}

func TestCleanupPendingCannotHideUnknownOrHardFailure(t *testing.T) {
	for _, code := range []f.Code{f.CommitUnknown, f.DependencyUnavailable} {
		t.Run(string(code), func(t *testing.T) {
			s, store, cleaner := cleanupScanFixture(t, 3)
			state := f.NotStarted
			if code == f.CommitUnknown {
				state = f.Unknown
			}
			original := f.NewFault(code, state)
			cleaner.hook = func(key oc.CleanupID) error {
				if key.String() == store.keys[1] {
					return original
				}
				return nil
			}
			for round := range 2 {
				if err := s.RecoverCleanup(context.Background()); err != original {
					t.Fatal("pending masked original hard/Unknown result", err)
				}
				if len(cleaner.seen) != round+2 || cleaner.seen[len(cleaner.seen)-1].String() != store.keys[1] {
					t.Fatal("failed item skipped or earlier pending retried instead")
				}
			}
		})
	}
}

func TestCleanupCancellationKeepsProgressAndScanAdmissionJoins(t *testing.T) {
	s, store, cleaner := cleanupScanFixture(t, 3)
	entered, release, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	cleaner.hook = func(key oc.CleanupID) error {
		if key.String() == store.keys[0] {
			close(entered)
			<-release
		}
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { defer close(joined); result <- s.RecoverCleanup(ctx) }()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
		<-joined
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not enter actual callback")
	}
	requireCleanupBusy(t, s.RecoverCleanup(context.Background()))
	cancel()
	budget, stop := context.WithCancel(context.Background())
	stop()
	if err := s.Drain(budget); !errors.Is(err, context.Canceled) {
		t.Fatal("blocked cleaner was declared joined", err)
	}
	close(release)
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("cleaner did not return")
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal("caller cancellation disappeared", err)
	}
	cleaner.hook = nil
	requireCleanupBusy(t, s.RecoverCleanup(context.Background()))
	if len(cleaner.seen) != 3 || cleaner.seen[1].String() != store.keys[1] || cleaner.seen[2].String() != store.keys[2] {
		t.Fatal("cancelled caller lost completed scheduling progress")
	}
	if err := s.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
}
