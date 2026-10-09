package knowledge

import (
	"context"
	"errors"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type cleanupProbe struct {
	oc.Cleaner
	result oc.CleanupResult
	err    error
	cause  oc.ObjectCleanupCause
	object oc.ObjectID
	calls  int
}

func (p *cleanupProbe) DeleteUnreferenced(_ context.Context, cause oc.ObjectCleanupCause, object oc.ObjectID) (oc.CleanupResult, error) {
	p.cause, p.object = cause, object
	p.calls++
	return p.result, p.err
}

type cleanupCommitStore struct {
	*authorityStore
	commit  f.CommitResult
	commits int
}

func (s *cleanupCommitStore) WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
	s.commits++
	return s.commit
}

func TestCleanupCompletionRequiresRealCleanerAndCheckpointOutcome(t *testing.T) {
	operation, project, document, commandID := newID[oc.CleanupOperation](t), newID[id.Project](t), newID[kc.Document](t), newID[command](t)
	object, upload := newID[oc.StoredObject](t), newID[oc.Upload](t)
	sentinel := f.NewFault(f.CommitUnknown, f.Unknown)
	cause, err := f.NewRecoveryCause("knowledge.cleanup", operation.String(), "")
	if err != nil {
		t.Fatal(err)
	}
	attempt := newID[f.TransactionAttempt](t)
	for _, kind := range []string{"pending", "provider_unknown", "wrong_operation", "live_reader", "checkpoint_unknown"} {
		t.Run(kind, func(t *testing.T) {
			store := &cleanupCommitStore{authorityStore: &authorityStore{row: sourceRow{values: []any{operation.String(), project.String(), document.String(), commandID.String(), object.String(), upload.String(), "owner_deleted", "object"}}}, commit: f.UnknownResult(attempt, cause)}
			cleaner := &cleanupProbe{result: oc.CleanupResult{State: oc.CleanupCompleted, OperationID: operation}}
			switch kind {
			case "pending":
				cleaner.result.State = oc.CleanupPending
			case "provider_unknown":
				cleaner.err = sentinel
			case "wrong_operation":
				cleaner.result.OperationID = newID[oc.CleanupOperation](t)
			case "live_reader":
				cleaner.result.Remaining.ActiveLeases = []oc.ObjectLease{{}}
			}
			st := &serviceState{store: store, deps: Dependencies{ObjectCleanup: cleaner}}
			s := &Service{data: func() *serviceState { return st }}
			err := s.recoverCleanup(context.Background(), operation)
			if err == nil || cleaner.calls != 1 || cleaner.object != object || cleaner.cause.Details().OperationID != operation || cleaner.cause.Details().Owner.Details().ID != document.String() || cleaner.cause.Details().Reason != oc.OwnerDeleted {
				t.Fatal("cleanup lost exact original cause or completed early", err)
			}
			if kind == "checkpoint_unknown" {
				var original commitFailure
				if store.commits != 1 || !errors.As(err, &original) || original.result.AttemptID() != attempt {
					t.Fatal("physical completion erased checkpoint Unknown", err)
				}
			} else if store.commits != 0 {
				t.Fatal("incomplete cleaner advanced durable phase")
			}
			if kind == "provider_unknown" && err != sentinel {
				t.Fatal("provider Unknown identity lost")
			}
		})
	}
}

func TestStopRequiresActualCallReturnBeforeDrain(t *testing.T) {
	st := &serviceState{calls: make(map[*call]struct{}), changed: make(chan struct{})}
	s := &Service{data: func() *serviceState { return st }}
	ctx, done, err := s.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s.Stop()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("stop did not cancel")
	}
	_, _, err = s.begin(context.Background())
	var faultValue *f.Fault
	if !errors.As(err, &faultValue) || faultValue.Code != f.ShuttingDown {
		t.Fatal("new admission after stop", err)
	}
	budget, cancel := context.WithCancel(context.Background())
	cancel()
	if err = s.Drain(budget); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel request was treated as join", err)
	}
	done()
	done()
	if err = s.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDrainWaitsForAllRegisteredCalls(t *testing.T) {
	st := &serviceState{calls: make(map[*call]struct{}), changed: make(chan struct{})}
	s := &Service{data: func() *serviceState { return st }}
	_, first, err := s.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := s.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s.Stop()
	first()
	st.mu.Lock()
	remaining := len(st.calls)
	st.mu.Unlock()
	if remaining != 1 {
		t.Fatalf("remaining=%d", remaining)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- s.Drain(ctx) }()
	second()
	if err = <-result; err != nil {
		t.Fatal(err)
	}
}
