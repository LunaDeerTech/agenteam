package adapter

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
)

func localExchange(t *testing.T, mode ResponseMode) *Exchange {
	t.Helper()
	b := NewBudget()
	x, err := b.accept(context.Background(), fresh[id.Project](t), mode, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { x.cancel(); b.StopAdmission() })
	return x
}
func TestWireConsumerModesCopiesAndActualPrefix(t *testing.T) {
	x := localExchange(t, SSEResponse)
	version := f.Version(7)
	n := mc.TokenCount(9)
	x.observation.Decision = &outbound.Decision{Sent: true, PolicyVersion: &version}
	x.observation.Usage = mc.Usage{TotalTokens: &n, Source: mc.ProviderUsage}
	copy := x.Observe()
	*copy.Decision.PolicyVersion = 100
	*copy.Usage.TotalTokens = 100
	if *x.Observe().Decision.PolicyVersion != 7 || *x.Observe().Usage.TotalTokens != 9 {
		t.Fatal("observation alias")
	}
	if _, err := x.Result(context.Background()); err == nil {
		t.Fatal("wrong mode accepted")
	} else {
		requireFault(t, err, f.InvalidState)
	}
	if err := x.emit(Event{Kind: TextDelta, Text: "prefix"}); err != nil {
		t.Fatal(err)
	}
	finishLocal(x, Result{}, failure("content_filter", ""))
	e, err := x.Next(context.Background())
	if err != nil || e.Kind != TextDelta || e.Text != "prefix" {
		t.Fatal(e, err)
	}
	_, err = x.Next(context.Background())
	m := requireModel(t, err, "content_filter", "")
	if !m.Dispatched || !m.PartialOutput || m.Retryable {
		t.Fatal("wrong actual prefix")
	}
	if _, err = x.Next(context.Background()); err != io.EOF {
		t.Fatal("more than one error")
	}
	if !x.Joined() {
		t.Fatal("local work not joined")
	}
	y := localExchange(t, JSONResponse)
	finishLocal(y, Result{Text: "answer", End: End{Usage: mc.Usage{Source: mc.UnknownUsage}, FinishReason: "stop"}}, nil)
	result, err := y.Result(context.Background())
	if err != nil || result.Text != "answer" || !y.Joined() {
		t.Fatal(result, err)
	}
	if _, err := y.Result(context.Background()); err == nil {
		t.Fatal("wire result silently replayed")
	} else {
		requireFault(t, err, f.InvalidState)
	}
}
func TestWireBackpressureCancellationAndConcurrentReader(t *testing.T) {
	x := localExchange(t, SSEResponse)
	x.observation.Decision = &outbound.Decision{Sent: true}
	for n := 0; n < 3; n++ {
		if err := x.emit(Event{Kind: TextDelta, Text: strings.Repeat("x", 64<<10)}); err != nil {
			t.Fatal(err)
		}
	}
	blocked, done := make(chan struct{}), make(chan error, 1)
	go func() { close(blocked); done <- x.emit(Event{Kind: TextDelta, Text: strings.Repeat("y", 64<<10)}) }()
	<-blocked
	select {
	case err := <-done:
		t.Fatal("payload limit did not backpressure", err)
	default:
	}
	x.mu.Lock()
	if x.queueBytes > 256<<10 || len(x.queue) > 32 {
		t.Fatal("unbounded queue")
	}
	x.reading = true
	x.mu.Unlock()
	if _, err := x.Next(context.Background()); err == nil {
		t.Fatal("concurrent reader accepted")
	} else {
		requireFault(t, err, f.InvalidState)
	}
	x.mu.Lock()
	x.reading = false
	x.mu.Unlock()
	x.cancel()
	select {
	case err := <-done:
		requireModel(t, err, "cancelled", "wire_cancelled")
	case <-time.After(time.Second):
		t.Fatal("backpressured producer did not stop")
	}
	finishLocal(x, Result{}, contextFailure(context.Canceled))
	_, err := x.Next(context.Background())
	m := requireModel(t, err, "cancelled", "wire_cancelled")
	if m.PartialOutput {
		t.Fatal("queued text is not delivered prefix")
	}
	if _, err = x.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatal("cancelled reader did not end")
	}
}
func TestWireCancelledStartDoesNotAdmitOrCreateClient(t *testing.T) {
	r, o := unitInput(t)
	a := &OpenAIChat{budget: NewBudget()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	x, err := a.Start(ctx, r, o)
	if x != nil {
		t.Fatal("cancelled admission handle")
	}
	requireModel(t, err, "cancelled", "wire_cancelled")
	active, _ := a.budget.snapshot()
	if len(active) != 0 {
		t.Fatal("cancelled request owns slot")
	}
}
