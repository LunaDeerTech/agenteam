package account

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

func queuedMailWaiters(t *testing.T, g *mailAdmission, n int) {
	t.Helper()
	end := time.Now().Add(time.Second)
	for {
		g.mu.Lock()
		got := len(g.queue)
		g.mu.Unlock()
		if got == n {
			return
		}
		if time.Now().After(end) {
			t.Fatal("gate waiter never queued")
		}
		runtime.Gosched()
	}
}
func TestMailAdmissionWriterPriorityCancellationAndGuardIdentity(t *testing.T) {
	gate := &mailAdmission{}
	first, e := gate.acquire(context.Background(), false)
	if e != nil {
		t.Fatal(e)
	}
	writer := make(chan *mailGuard, 1)
	go func() {
		g, e := gate.acquire(context.Background(), true)
		if e != nil {
			t.Error(e)
		}
		writer <- g
	}()
	queuedMailWaiters(t, gate, 1)
	cancelled, cancel := context.WithCancel(context.Background())
	cancelResult := make(chan error, 1)
	go func() {
		g, e := gate.acquire(cancelled, false)
		if g != nil {
			g.release()
		}
		cancelResult <- e
	}()
	queuedMailWaiters(t, gate, 2)
	cancel()
	if e := <-cancelResult; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	queuedMailWaiters(t, gate, 1)
	reader := make(chan *mailGuard, 1)
	go func() {
		g, e := gate.acquire(context.Background(), false)
		if e != nil {
			t.Error(e)
		}
		reader <- g
	}()
	queuedMailWaiters(t, gate, 2)
	first.release()
	ex := <-writer
	select {
	case g := <-reader:
		g.release()
		t.Fatal("new reader bypassed queued writer")
	default:
	}
	ex.release()
	ex.release()
	sh := <-reader
	sh.release()
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.writer || gate.readers != 0 || len(gate.queue) != 0 {
		t.Fatal("gate leaked")
	}
}
func TestMailAdmissionCancelledHeadAllowsFollowingGroup(t *testing.T) {
	gate := &mailAdmission{}
	hold, _ := gate.acquire(context.Background(), false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		g, e := gate.acquire(ctx, true)
		if g != nil {
			g.release()
		}
		done <- e
	}()
	queuedMailWaiters(t, gate, 1)
	sh := make(chan *mailGuard, 1)
	go func() { g, _ := gate.acquire(context.Background(), false); sh <- g }()
	queuedMailWaiters(t, gate, 2)
	cancel()
	if !errors.Is(<-done, context.Canceled) {
		t.Fatal("cancel lost")
	}
	joined := <-sh
	joined.release()
	hold.release()
	if _, e := gate.acquire(ctx, true); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
