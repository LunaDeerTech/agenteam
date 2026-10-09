package knowledge

import (
	"context"
	"errors"
	"io"
	"strings"
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
	"github.com/jackc/pgx/v5"
)

// This intentionally expects the product defect, so a passing reproducer is
// not acceptance. Object's real cancellation/Close code runs over an in-memory
// body and a controlled release callback; no SQL, lease or socket is claimed.
func TestIndependentB02CancelledReaderRemainsRegistered(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		name := "ordinary_close_control"
		if cancelled {
			name = "cancelled_close_defect"
		}
		t.Run(name, func(t *testing.T) {
			st := &serviceState{calls: make(map[*call]struct{}), changed: make(chan struct{})}
			s := &Service{data: func() *serviceState { return st }}
			run, done, err := s.begin(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer done() // release the review's registry even for the reproduced defect
			payload := strings.Repeat("r", 2*oc.StreamBufferSize+1)
			released := make(chan struct{})
			var releases atomic.Int32
			raw, err := object.ReviewB02IntegrityReader(run, io.NopCloser(strings.NewReader(payload)), int64(len(payload)), ob.DigestBytes([]byte(payload)), func() error {
				if releases.Add(1) == 1 {
					close(released)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			defer raw.Close()
			scope, _ := id.InProject(newID[id.Project](t))
			now, _ := f.NewInstant(time.Now())
			reader, err := oc.NewObjectReader(oc.ObjectMeta{ID: newID[oc.StoredObject](t), Scope: scope, MediaType: kc.PlainText, ByteSize: f.Progress(len(payload)), SHA256: ob.DigestBytes([]byte(payload)), State: oc.Available, Version: 1, CreatedAt: now}, nil, raw)
			if err != nil {
				t.Fatal(err)
			}
			tracked := &trackedRead{body: reader, done: done}
			if cancelled {
				s.Stop()
				select {
				case <-released:
				case <-time.After(time.Second):
					t.Fatal("actual cancellation callback did not return")
				}
			}
			closeErr := tracked.Close()
			if releases.Load() != 1 {
				t.Fatal("actual source release not once")
			}
			budget, cancel := context.WithCancel(context.Background())
			cancel()
			drainErr := s.Drain(budget)
			if cancelled {
				if !errors.Is(closeErr, context.Canceled) || !errors.Is(drainErr, context.Canceled) || len(st.calls) != 1 {
					t.Fatal("expected current defect after actual Object.Close joined", closeErr, drainErr, len(st.calls))
				}
				if !errors.Is(tracked.Close(), context.Canceled) || len(st.calls) != 1 {
					t.Fatal("original cancellation or stuck registry changed on retry")
				}
			} else if closeErr != nil || drainErr != nil || len(st.calls) != 0 {
				t.Fatal("ordinary Close control failed", closeErr, drainErr)
			}
		})
	}
}

type reviewCleanupRows struct {
	pgx.Rows
	keys  []oc.CleanupID
	index int
}

func (r *reviewCleanupRows) Next() bool { r.index++; return r.index <= len(r.keys) }
func (r *reviewCleanupRows) Scan(v ...any) error {
	*v[0].(*string) = r.keys[r.index-1].String()
	return nil
}
func (*reviewCleanupRows) Close()     {}
func (*reviewCleanupRows) Err() error { return nil }

type reviewCleanupStore struct {
	Store
	keys []oc.CleanupID
	rows map[string]sourceRow
}

func (s *reviewCleanupStore) Query(_ context.Context, query string, args ...any) (*postgres.Rows, error) {
	if !strings.Contains(query, "ORDER BY id LIMIT 32") || len(args) != 1 || args[0] != nil {
		return nil, errors.New("unexpected recovery page")
	}
	return postgres.ReviewB02Rows(&reviewCleanupRows{keys: s.keys}), nil
}
func (s *reviewCleanupStore) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	if !strings.Contains(query, "FROM agenteam_knowledge.object_cleanup WHERE id=$1") || len(args) != 1 {
		panic("unexpected recovery row query")
	}
	return s.rows[args[0].(string)]
}

type reviewPendingCleaner struct {
	oc.Cleaner
	seen []oc.CleanupID
}

func (c *reviewPendingCleaner) DeleteUnreferenced(_ context.Context, cause oc.ObjectCleanupCause, _ oc.ObjectID) (oc.CleanupResult, error) {
	key := cause.Details().OperationID
	c.seen = append(c.seen, key)
	return oc.CleanupResult{OperationID: key, State: oc.CleanupPending}, nil
}

// Executes the real public scheduler with controlled durable rows and Cleaner
// responses. Both exact per-item attempts are valid; the first pending result
// prevents the second project's attempt on every new public invocation.
func TestIndependentB02PendingCleanupStarvesFollowingProject(t *testing.T) {
	first, second := newID[oc.CleanupOperation](t), newID[oc.CleanupOperation](t)
	if first.String() > second.String() {
		first, second = second, first
	}
	store := &reviewCleanupStore{keys: []oc.CleanupID{first, second}, rows: make(map[string]sourceRow)}
	for _, key := range store.keys {
		store.rows[key.String()] = sourceRow{values: []any{key.String(), newID[id.Project](t).String(), newID[kc.Document](t).String(), newID[command](t).String(), newID[oc.StoredObject](t).String(), newID[oc.Upload](t).String(), "owner_deleted", "object"}}
	}
	cleaner := &reviewPendingCleaner{}
	st := &serviceState{store: store, deps: Dependencies{ObjectCleanup: cleaner}, calls: make(map[*call]struct{}), changed: make(chan struct{})}
	s := &Service{data: func() *serviceState { return st }}
	for range 3 {
		var known *f.Fault
		if err := s.RecoverCleanup(context.Background()); !errors.As(err, &known) || known.Code != f.ResourceBusy {
			t.Fatal("expected pending", err)
		}
	}
	if len(cleaner.seen) != 3 {
		t.Fatal("unexpected cleanup attempts", cleaner.seen)
	}
	for _, key := range cleaner.seen {
		if key != first {
			t.Fatal("current starvation defect not reproduced", cleaner.seen)
		}
	}
	// The later task itself is well formed and reaches the real per-item method.
	var known *f.Fault
	if err := s.recoverCleanup(context.Background(), second); !errors.As(err, &known) || known.Code != f.ResourceBusy {
		t.Fatal("later-item control", err)
	}
	if len(cleaner.seen) != 4 || cleaner.seen[3] != second {
		t.Fatal("later-item control never reached Cleaner")
	}
	if err := s.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
}
