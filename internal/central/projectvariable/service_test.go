package projectvariable

import (
	"context"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestVariableCallOwnerStopCancelsButDrainWaitsActualCompletion(t *testing.T) {
	st := &serviceState{calls: map[*call]struct{}{}, changed: make(chan struct{})}
	s := &Service{data: func() *serviceState { return st }}
	ctx, entry, done, e := s.begin(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	tail, cancel := context.WithCancel(context.Background())
	token := &confirmation{cancel: cancel}
	st.mu.Lock()
	entry.confirmations[token] = struct{}{}
	st.mu.Unlock()
	s.Stop()
	if ctx.Err() != context.Canceled || tail.Err() != context.Canceled {
		t.Fatal("stop did not cancel both")
	}
	bounded, cancelBounded := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancelBounded()
	if e = s.Drain(bounded); e != context.DeadlineExceeded {
		t.Fatal("reported joined before callback return")
	}
	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() { defer wg.Done(); done() }()
	}
	wg.Wait()
	if e = s.Drain(context.Background()); e != nil {
		t.Fatal(e)
	}
	_, _, _, e = s.begin(context.Background())
	code(t, e, f.ShuttingDown)
	code(t, s.Drain(nil), f.InvalidArgument)
	var nilService *Service
	if _, _, _, e = nilService.begin(context.Background()); e == nil {
		t.Fatal("nil service")
	}
}
