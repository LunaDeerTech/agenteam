package account

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func changedResponseForTest(t *testing.T) (PasswordChangeResponse, *operation) {
	t.Helper()
	state := &serviceState{operations: map[*operation]bool{}, changed: make(chan struct{})}
	s := &Service{data: func() *serviceState { return state }}
	op, err := s.begin(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	m, err := sc.NewSecretMaterial([]byte("test-cookie-material"))
	if err != nil {
		t.Fatal(err)
	}
	v := &changedResponse{service: s, op: op, material: m, done: make(chan struct{})}
	v.stop = context.AfterFunc(op.ctx, v.close)
	r := PasswordChangeResponse{data: func() *changedResponse { return v }}
	t.Cleanup(func() { _ = r.Close(context.Background()) })
	return r, op
}

func TestChangedResponseConcurrentCloseWaitsEveryActualUse(t *testing.T) {
	r, op := changedResponseForTest(t)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var users sync.WaitGroup
	users.Add(2)
	for range 2 {
		go func() {
			defer users.Done()
			if err := r.UseCookie(func(value []byte) error {
				if len(value) == 0 {
					return errors.New("empty test material")
				}
				entered <- struct{}{}
				<-release
				return nil
			}); err != nil {
				t.Error(err)
			}
		}()
	}
	<-entered
	<-entered
	defer users.Wait()
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	closed := make(chan error, 2)
	for range 2 {
		go func() { closed <- r.Close(ctx) }()
	}
	for range 2 {
		if err := <-closed; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("close reported joined callback: %v", err)
		}
	}
	if operationJoined(op) {
		t.Fatal("actual callbacks still active")
	}
	called := false
	if err := r.UseCookie(func([]byte) error { called = true; return nil }); err == nil || called {
		t.Fatal("closing response admitted a new use")
	}
}

func TestChangedResponsePanickingUseStillReleasesAfterClose(t *testing.T) {
	r, op := changedResponseForTest(t)
	panicked := false
	func() {
		defer func() { panicked = recover() == "test-panic" }()
		_ = r.UseCookie(func([]byte) error {
			r.data().close()
			if operationJoined(op) {
				t.Fatal("close inside callback claimed it had joined")
			}
			panic("test-panic")
		})
	}()
	if !panicked || !operationJoined(op) {
		t.Fatal("panicking callback did not join exactly once")
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
