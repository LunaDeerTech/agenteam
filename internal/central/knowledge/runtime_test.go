package knowledge

import (
	"context"
	"errors"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

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
