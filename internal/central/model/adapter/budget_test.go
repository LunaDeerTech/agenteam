package adapter

import (
	"context"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// Only local-work completion is synthesized here. The integration group holds
// a real D04 request writer and verifies the additional Client.Drain condition.
func finishLocal(x *Exchange, result Result, err error) {
	x.mu.Lock()
	x.result, x.err, x.done = result, err, true
	close(x.ioDone)
	x.notify()
	x.mu.Unlock()
}
func TestWireBudgetGlobalProjectAndSealedAdmission(t *testing.T) {
	b := NewBudget()
	ctx := context.Background()
	var handles []*Exchange
	for p := 0; p < 8; p++ {
		project := fresh[id.Project](t)
		for n := 0; n < 8; n++ {
			x, err := b.accept(ctx, project, JSONResponse, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			handles = append(handles, x)
		}
		if _, err := b.accept(ctx, project, JSONResponse, time.Minute); err == nil {
			t.Fatal("project limit exceeded")
		} else {
			requireFault(t, err, f.ResourceBusy)
		}
	}
	if _, err := b.accept(ctx, fresh[id.Project](t), JSONResponse, time.Minute); err == nil {
		t.Fatal("global limit exceeded")
	} else {
		requireFault(t, err, f.ResourceBusy)
	}
	b.StopAdmission()
	if b.Joined() {
		t.Fatal("active work claimed joined")
	}
	if _, err := b.accept(ctx, fresh[id.Project](t), JSONResponse, time.Minute); err == nil {
		t.Fatal("accepted after stop")
	} else {
		requireFault(t, err, f.ShuttingDown)
	}
	for _, x := range handles {
		finishLocal(x, Result{}, nil)
		x.cancel()
	}
	if err := b.Drain(ctx); err != nil || !b.Joined() {
		t.Fatal("drain", err)
	}
}
func TestWireBudgetCancellationDoesNotRetireLocalWork(t *testing.T) {
	b := NewBudget()
	project := fresh[id.Project](t)
	x, err := b.accept(context.Background(), project, JSONResponse, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() { close(entered); <-release; finishLocal(x, Result{}, nil); close(returned) }()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.Force(ctx); err == nil || x.Joined() || b.Joined() {
		t.Fatal("cancellation forged actual completion")
	}
	list, _ := b.snapshot()
	if len(list) != 1 {
		t.Fatal("live slot lost")
	}
	close(release)
	<-returned
	if err := x.Close(ctx); err != nil || !x.Joined() || !b.Joined() {
		t.Fatal("already-cancelled probe must observe real completion", err)
	}
}
func TestWireBudgetConcurrentAdmissionHasOneOwner(t *testing.T) {
	b := NewBudget()
	project := fresh[id.Project](t)
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan *Exchange, 80)
	failures := make(chan error, 80)
	for i := 0; i < 80; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			x, e := b.accept(context.Background(), project, SSEResponse, time.Minute)
			if e != nil {
				failures <- e
			} else {
				results <- x
			}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(failures)
	if len(results) != 8 || len(failures) != 72 {
		t.Fatalf("admission count %d/%d", len(results), len(failures))
	}
	for err := range failures {
		requireFault(t, err, f.ResourceBusy)
	}
	b.StopAdmission()
	for x := range results {
		finishLocal(x, Result{}, nil)
		x.cancel()
	}
	if !b.Joined() {
		t.Fatal("joined slots not retired")
	}
}
