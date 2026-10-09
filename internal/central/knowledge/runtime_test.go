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

func TestDeferredPublicationRetirementKeepsAttemptAndActualCompletion(t *testing.T) {
	t.Run("attempt isolation", func(t *testing.T) {
		s, _, work, done := retirementFixture(t)
		defer done()
		old, err := s.publicationRetirement(work, done)
		if err != nil {
			t.Fatal(err)
		}
		_, secondDone, err := s.begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer secondDone()
		secondWork := work.clone()
		secondWork.attempt = newID[publicationAttempt](t)
		second, err := s.publicationRetirement(secondWork, secondDone)
		if err != nil {
			t.Fatal(err)
		}
		failure := errors.New("lease not yet released")
		allowOld := false
		oldCalls, newCalls := 0, 0
		if err = old.ownContext(func(context.Context) error {
			oldCalls++
			if !allowOld {
				return failure
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err = second.ownContext(func(context.Context) error { newCalls++; return nil }); err != nil {
			t.Fatal(err)
		}
		if err = old.join(context.Background()); !errors.Is(err, failure) {
			t.Fatal(err)
		}
		if err = s.deferPublicationRetirement(old, done); err != nil {
			t.Fatal(err)
		}
		if err = s.deferPublicationRetirement(second, secondDone); err != nil {
			t.Fatal(err)
		}
		collisionWork := work.clone()
		collisionWork.command = newID[command](t)
		collision, err := s.publicationRetirement(collisionWork, func() { t.Error("wrong attempt owner retired") })
		if err != nil {
			t.Fatal(err)
		}
		if err = s.deferPublicationRetirement(collision, func() {}); err == nil || s.state().retiringPublications[work.attempt] != old {
			t.Fatal("same attempt replaced an owned handle")
		}
		if err = second.join(context.Background()); err != nil || len(s.state().calls) != 1 || s.state().retiringPublications[work.attempt] != old || len(s.state().joinedPublications) != 1 {
			t.Fatal("new retirement consumed old resources", err)
		}
		allowOld = true
		if err = s.retryPublicationRetirements(context.Background(), work.command); err != nil {
			t.Fatal(err)
		}
		if oldCalls != 2 || newCalls != 1 || len(s.state().calls) != 0 || len(s.state().retiringPublications) != 0 || len(s.state().joinedPublications) != 2 {
			t.Fatal("exact retirement proof or resource count lost")
		}
	})
	t.Run("concurrent retry is bounded", func(t *testing.T) {
		s, _, work, done := retirementFixture(t)
		defer done()
		retirement, err := s.publicationRetirement(work, done)
		if err != nil {
			t.Fatal(err)
		}
		start, release := make(chan struct{}), make(chan struct{})
		var releaseOnce sync.Once
		result := make(chan error, 1)
		started := false
		t.Cleanup(func() {
			releaseOnce.Do(func() { close(release) })
			if started {
				<-result
			}
		})
		armed, calls := false, 0
		failure := errors.New("first close rejected")
		if err = retirement.ownContext(func(ctx context.Context) error {
			calls++
			if !armed {
				return failure
			}
			close(start)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}); err != nil {
			t.Fatal(err)
		}
		if err = retirement.join(context.Background()); !errors.Is(err, failure) {
			t.Fatal(err)
		}
		if err = s.deferPublicationRetirement(retirement, done); err != nil {
			t.Fatal(err)
		}
		armed = true
		live, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		go func() { result <- s.retryPublicationRetirements(live, work.command) }()
		started = true
		<-start
		bounded, stop := context.WithCancel(context.Background())
		stop()
		if err = s.Drain(bounded); err == nil {
			t.Fatal("concurrent Close became joined")
		}
		if len(s.state().calls) != 1 || len(s.state().retiringPublications) != 1 || len(s.state().joinedPublications) != 0 || calls != 2 {
			t.Fatal("another retry closed the same lease or lost ownership")
		}
		releaseOnce.Do(func() { close(release) })
		err = <-result
		started = false
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Drain(live); err != nil || calls != 2 || len(s.state().calls) != 0 {
			t.Fatal("actual closing callback did not retire", err)
		}
	})
}
