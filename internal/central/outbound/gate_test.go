package outbound

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGateWriterPriorityCancellationAndRelease(t *testing.T) {
	var g sendGate
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, err := g.acquire(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	wctx, wcancel := context.WithCancel(ctx)
	waiting := make(chan struct{})
	gotWriter := make(chan func(), 1)
	writerErr := make(chan error, 1)
	go func() {
		close(waiting)
		w, e := g.acquire(wctx, true)
		if e != nil {
			writerErr <- e
			return
		}
		gotWriter <- w
	}()
	<-waiting
	// Observe the real queue state; no timing sleep is needed to create priority.
	for {
		g.mu.Lock()
		n := g.writersWaiting
		g.mu.Unlock()
		if n == 1 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("writer did not queue")
		default:
		}
	}
	rctx, rcancel := context.WithCancel(ctx)
	readerResult := make(chan error, 1)
	go func() {
		release, e := g.acquire(rctx, false)
		if release != nil {
			release()
		}
		readerResult <- e
	}()
	rcancel()
	if e := <-readerResult; !errors.Is(e, context.Canceled) {
		t.Fatal("reader barged past queued writer")
	}
	wcancel()
	if e := <-writerErr; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	r2, e := g.acquire(ctx, false)
	if e != nil {
		t.Fatal("cancelled writer blocked readers")
	}
	r2()
	r()
	r()
	w, e := g.acquire(ctx, true)
	if e != nil {
		t.Fatal(e)
	}
	cancelled, c := context.WithCancel(ctx)
	c()
	if _, e = g.acquire(cancelled, false); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	w()
	r3, e := g.acquire(ctx, false)
	if e != nil {
		t.Fatal(e)
	}
	r3()
}
