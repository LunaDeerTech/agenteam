package adapter

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// This helper completes only local work. Actual D04 writer/connection facts
// belong to the later native fixture; mixed admission uses the real registry.
func embeddingLocalHandle(b *Budget, project id.ProjectID) (*EmbeddingExchange, error) {
	var x *EmbeddingExchange
	err := b.admit(context.Background(), project, func() budgetWork {
		x = newEmbeddingExchange(context.Background(), time.Second, b, sc.SecretMaterial{})
		return x
	})
	return x, err
}
func embeddingFinishLocal(x *EmbeddingExchange) {
	x.mu.Lock()
	select {
	case <-x.ioDone:
	default:
		close(x.ioDone)
		close(x.workerDone)
	}
	x.mu.Unlock()
}
func embeddingRetireLocal(t *testing.T, b *Budget, handles []budgetWork) {
	t.Helper()
	for _, handle := range handles {
		handle.cancelWork()
		switch x := handle.(type) {
		case *Exchange:
			select {
			case <-x.ioDone:
			default:
				finishLocal(x, Result{}, nil)
			}
		case *EmbeddingExchange:
			embeddingFinishLocal(x)
		}
	}
	b.StopAdmission()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := b.Drain(ctx); err != nil || !b.Joined() {
		t.Error("mixed local handles not actually joined", err)
	}
}

func TestOpenAIEmbeddingsMixedBudget(t *testing.T) {
	b := NewBudget()
	var handles []budgetWork
	t.Cleanup(func() { embeddingRetireLocal(t, b, handles) })
	var first id.ProjectID
	for projectIndex := range 8 {
		project := fresh[id.Project](t)
		if projectIndex == 0 {
			first = project
		}
		for n := range 8 {
			if n%3 == 2 {
				x, err := embeddingLocalHandle(b, project)
				if err != nil {
					t.Fatal(err)
				}
				handles = append(handles, x)
			} else {
				// Both accepted Chat modes (including structured's same
				// Exchange handle) share the exact old accept path.
				mode := JSONResponse
				if n%3 == 1 {
					mode = SSEResponse
				}
				x, err := b.accept(context.Background(), project, mode, time.Second)
				if err != nil {
					t.Fatal(err)
				}
				handles = append(handles, x)
			}
		}
		x, err := embeddingLocalHandle(b, project)
		if x != nil {
			t.Fatal("ninth mixed Project handle admitted")
		}
		requireFault(t, err, f.ResourceBusy)
		if x, err := b.accept(context.Background(), project, JSONResponse, time.Second); x != nil || err == nil {
			t.Fatal("Chat bypassed mixed Project cap")
		}
	}
	x, err := embeddingLocalHandle(b, fresh[id.Project](t))
	if x != nil {
		t.Fatal("65th global handle admitted")
	}
	requireFault(t, err, f.ResourceBusy)
	active, _ := b.snapshot()
	if len(active) != 64 {
		t.Fatal("mixed global registry split")
	}
	// Cancellation alone does not free either protocol's slot.
	for _, handle := range handles {
		handle.cancelWork()
	}
	x, err = embeddingLocalHandle(b, first)
	if x != nil {
		t.Fatal("cancelled but unfinished handle released capacity")
	}
	requireFault(t, err, f.ResourceBusy)
	// Input validation still precedes full admission and per-call Client.
	r, o := embeddingUnitInput(t)
	o.ProjectID = first
	a := &OpenAIEmbeddings{budget: b}
	var creates atomic.Int32
	create := func() (embeddingClient, error) { creates.Add(1); return nil, nil }
	r.ExpectedDimensions = 0
	if x, err := a.start(context.Background(), r, o, create); x != nil || err == nil {
		t.Fatal("invalid shape admitted")
	} else {
		requireFault(t, err, f.InvalidArgument)
	}
	r.ExpectedDimensions = 2
	if x, err := a.start(context.Background(), r, o, create); x != nil || err == nil {
		t.Fatal("full budget admitted Start")
	} else {
		requireFault(t, err, f.ResourceBusy)
	}
	if creates.Load() != 0 {
		t.Fatal("full/invalid Start created Client")
	}
	b.StopAdmission()
	if x, err := embeddingLocalHandle(b, first); x != nil || err == nil {
		t.Fatal("sealed mixed admission")
	} else {
		requireFault(t, err, f.ShuttingDown)
	}
}

func TestOpenAIEmbeddingsConcurrentBudget(t *testing.T) {
	b := NewBudget()
	project := fresh[id.Project](t)
	var handles []budgetWork
	t.Cleanup(func() { embeddingRetireLocal(t, b, handles) })
	start := make(chan struct{})
	results := make(chan budgetWork, 96)
	failures := make(chan error, 96)
	var wg sync.WaitGroup
	for n := range 96 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if n%2 == 0 {
				x, err := embeddingLocalHandle(b, project)
				if err != nil {
					failures <- err
				} else {
					results <- x
				}
			} else {
				x, err := b.accept(context.Background(), project, SSEResponse, time.Second)
				if err != nil {
					failures <- err
				} else {
					results <- x
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(failures)
	for x := range results {
		handles = append(handles, x)
	}
	if len(handles) != 8 || len(failures) != 88 {
		t.Fatal("mixed concurrent owner count", len(handles), len(failures))
	}
	for err := range failures {
		requireFault(t, err, f.ResourceBusy)
	}
}

func TestOpenAIEmbeddingsForceSharesCallerBudget(t *testing.T) {
	b := NewBudget()
	project := fresh[id.Project](t)
	chat, err := b.accept(context.Background(), project, JSONResponse, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	embedding, err := embeddingLocalHandle(b, project)
	if err != nil {
		finishLocal(chat, Result{}, nil)
		chat.cancel()
		t.Fatal(err)
	}
	handles := []budgetWork{chat, embedding}
	t.Cleanup(func() { embeddingRetireLocal(t, b, handles) })
	// A cancelled caller still cancels every exact type before the first wait.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.Force(cancelled); err == nil || chat.ctx.Err() == nil || embedding.ctx.Err() == nil {
		t.Fatal("Force did not cancel all handles before waiting")
	}
	if b.Joined() || chat.Joined() || embedding.Joined() {
		t.Fatal("Force manufactured actual completion")
	}
	active, stopped := b.snapshot()
	if len(active) != 2 || !stopped {
		t.Fatal("Force lost exact work")
	}
	// Attach two controlled D04 tails to real Embedding handles. Each Drain
	// must receive the very same caller deadline, not a new per-handle budget.
	embeddingRetireLocal(t, b, handles)
	handles = nil
	b = NewBudget()
	gates := []*embeddingTestGate{embeddingGate(), embeddingGate()}
	var deadlinesMu sync.Mutex
	var deadlines []time.Time
	for _, gate := range gates {
		x, err := embeddingLocalHandle(b, project)
		if err != nil {
			t.Fatal(err)
		}
		client := &embeddingTestClient{}
		client.onDrain = func(ctx context.Context) error {
			deadline, ok := ctx.Deadline()
			if !ok {
				return context.DeadlineExceeded
			}
			deadlinesMu.Lock()
			deadlines = append(deadlines, deadline)
			deadlinesMu.Unlock()
			gate.enter.Do(func() { close(gate.entered) })
			select {
			case <-gate.release:
				return nil
			default:
			}
			select {
			case <-gate.release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		x.client = client
		embeddingFinishLocal(x)
		handles = append(handles, x)
	}
	t.Cleanup(func() {
		for _, gate := range gates {
			gate.open()
		}
	})
	caller, stop := context.WithTimeout(context.Background(), 60*time.Millisecond)
	want, _ := caller.Deadline()
	started := time.Now()
	err = b.Force(caller)
	elapsed := time.Since(started)
	stop()
	if err == nil || elapsed < 40*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Fatal("Force reset caller budget", elapsed)
	}
	for _, x := range handles {
		if x.(*EmbeddingExchange).ctx.Err() == nil {
			t.Fatal("Force waited before cancelling a handle")
		}
	}
	deadlinesMu.Lock()
	observed := append([]time.Time(nil), deadlines...)
	deadlinesMu.Unlock()
	for _, got := range observed {
		if !got.Equal(want) {
			t.Fatal("per-handle Drain got a renewed deadline")
		}
	}
	if len(observed) == 0 {
		t.Fatal("no actual Drain")
	}
	active, _ = b.snapshot()
	if len(active) != 2 {
		t.Fatal("timeout freed controlled tails")
	}
	// Release both and use one explicit caller cleanup window. Both exact
	// tails must be visited with that same deadline before actual retirement.
	for _, gate := range gates {
		gate.open()
	}
	cleanup, done := context.WithTimeout(context.Background(), time.Second)
	cleanupDeadline, _ := cleanup.Deadline()
	deadlinesMu.Lock()
	deadlines = nil
	deadlinesMu.Unlock()
	err = b.Drain(cleanup)
	done()
	deadlinesMu.Lock()
	observed = append([]time.Time(nil), deadlines...)
	deadlinesMu.Unlock()
	if err != nil || len(observed) != 2 || !observed[0].Equal(cleanupDeadline) || !observed[1].Equal(cleanupDeadline) || !b.Joined() {
		t.Fatal("shared cleanup window did not join both exact tails", err)
	}
}
