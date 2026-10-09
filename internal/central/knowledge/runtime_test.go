package knowledge

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

type contentCloseBody struct {
	started, release chan struct{}
	closes, reads    int
	err              error
}

func (b *contentCloseBody) Read([]byte) (int, error) { b.reads++; return 0, io.EOF }
func (b *contentCloseBody) Close() error {
	b.closes++
	if b.started != nil {
		close(b.started)
		<-b.release
	}
	return b.err
}

func TestPublicContentRejectsWithoutReadingAndOwnsActualClose(t *testing.T) {
	_, actor, query := queryFixture(t)
	request := kc.CreateRequest{ProjectID: query.project, DocumentID: newID[kc.Document](t), Title: "Original"}
	meta := f.CommandMeta{RequestID: newID[f.Request](t), IdempotencyKey: "original-content"}
	for _, mode := range []string{"invalid", "stopped", "plan rejected", "close failed"} {
		t.Run(mode, func(t *testing.T) {
			body := &contentCloseBody{}
			if mode == "close failed" {
				body.err = errors.New("actual close failed")
			}
			source, err := kc.NewUploadSource(kc.PlainText, 0, ob.DigestBytes(nil), body)
			if err != nil {
				t.Fatal(err)
			}
			store := &cleanupCommitStore{commit: f.NotCommittedResult(fault(f.Forbidden))}
			st := &serviceState{store: store, calls: make(map[*call]struct{}), changed: make(chan struct{}), stopped: mode == "stopped"}
			service := &Service{data: func() *serviceState { return st }}
			actual := request
			if mode == "invalid" {
				actual.Title = ""
			}
			_, err = service.CreateDocument(context.Background(), actor, meta, actual, source)
			if err == nil || body.reads != 0 || body.closes != 1 {
				t.Fatal("rejection read input or omitted actual close", err)
			}
			if (mode == "invalid" || mode == "stopped") && store.commits != 0 {
				t.Fatal("invalid/new stopped admission opened SQL")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err = service.Drain(ctx)
			if mode == "close failed" {
				if !errors.Is(err, context.Canceled) || len(st.calls) != 1 {
					t.Fatal("failed owned close was declared joined", err)
				}
			} else if err != nil || len(st.calls) != 0 {
				t.Fatal("fully closed rejected operation was retained", err)
			}
		})
	}
}

func TestPublicContentDrainWaitsForActualSourceClose(t *testing.T) {
	_, actor, query := queryFixture(t)
	body := &contentCloseBody{started: make(chan struct{}), release: make(chan struct{})}
	source, _ := kc.NewUploadSource(kc.PlainText, 0, ob.DigestBytes(nil), body)
	store := &cleanupCommitStore{commit: f.NotCommittedResult(fault(f.Forbidden))}
	st := &serviceState{store: store, calls: make(map[*call]struct{}), changed: make(chan struct{})}
	service := &Service{data: func() *serviceState { return st }}
	finished := make(chan error, 1)
	joined := make(chan struct{})
	var release sync.Once
	meta := f.CommandMeta{RequestID: newID[f.Request](t), IdempotencyKey: "blocked-close"}
	request := kc.CreateRequest{ProjectID: query.project, DocumentID: newID[kc.Document](t), Title: "Original"}
	go func() {
		defer close(joined)
		_, err := service.CreateDocument(context.Background(), actor, meta, request, source)
		finished <- err
	}()
	defer func() { release.Do(func() { close(body.release) }); <-joined }()
	select {
	case <-body.started:
	case <-time.After(time.Second):
		t.Fatal("source close was not reached")
	}
	service.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.Drain(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation replaced actual close join", err)
	}
	release.Do(func() { close(body.release) })
	if err := <-finished; err == nil {
		t.Fatal("lost original plan rejection")
	}
	if err := service.Drain(context.Background()); err != nil || body.closes != 1 || body.reads != 0 {
		t.Fatal("actual return did not release admission", err)
	}
}

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
