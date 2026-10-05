package account

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// This tests the actual semaphore used around the synchronous decoder. Its
// deliberately blocked function models a decoder that cannot be interrupted;
// no claim is made that cancellation interrupts the image package itself.
func TestB04AvatarCapacityCancellationWaitsActualWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	finished := make(chan error, 2)
	for range 2 {
		go func() { finished <- withAvatarSlot(ctx, func() error { entered <- struct{}{}; <-release; return nil }) }()
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("decoder slots not entered")
		}
	}
	if len(avatarSlots) != 2 || len(avatarCapacity) != 2 {
		t.Fatal("slot accounting")
	}
	cancel()
	select {
	case <-finished:
		t.Fatal("cancellation falsely joined decoder")
	default:
	}
	if len(avatarSlots) != 2 || len(avatarCapacity) != 2 {
		t.Fatal("active decode permits released on cancel")
	}
	// All sixteen queue reservations remain bounded. Fill them directly while
	// both actual workers are held, then exercise the production rejection path.
	for range 16 {
		avatarCapacity <- struct{}{}
	}
	err := withAvatarSlot(context.Background(), func() error { t.Fatal("excess caller entered"); return nil })
	var f *foundation.Fault
	if !errors.As(err, &f) || f.Code != foundation.RateLimited {
		t.Fatal("overflow was admitted", err)
	}
	for range 16 {
		<-avatarCapacity
	}
	unblock()
	for range 2 {
		if e := <-finished; e != nil {
			t.Fatal(e)
		}
	}
	if len(avatarSlots) != 0 || len(avatarCapacity) != 0 {
		t.Fatal("finished worker leaked permit")
	}
}

func TestB04AvatarSixteenRealWaitersUseTwoSecondAdmissionBudget(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	workers := make(chan error, 2)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	for range 2 {
		go func() {
			workers <- withAvatarSlot(context.Background(), func() error { entered <- struct{}{}; <-release; return nil })
		}()
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("actual decode slots not held")
		}
	}
	type outcome struct {
		err     error
		elapsed time.Duration
	}
	queued := make(chan outcome, 16)
	for range 16 {
		go func() {
			start := time.Now()
			e := withAvatarSlot(context.Background(), func() error { t.Error("queued body entered while two workers held"); return nil })
			queued <- outcome{e, time.Since(start)}
		}()
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for len(avatarCapacity) != 18 {
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("sixteen real reservations not present")
		}
	}
	e := withAvatarSlot(context.Background(), func() error { t.Fatal("nineteenth request entered"); return nil })
	var f *foundation.Fault
	if !errors.As(e, &f) || f.Code != foundation.RateLimited {
		t.Fatal("real queue overflow", e)
	}
	for range 16 {
		select {
		case result := <-queued:
			if !errors.As(result.err, &f) || f.Code != foundation.RateLimited || result.elapsed < 1900*time.Millisecond || result.elapsed > 3*time.Second {
				t.Fatal("queue admission budget", result.err, result.elapsed)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("queued work did not expire")
		}
	}
	if len(avatarSlots) != 2 || len(avatarCapacity) != 2 {
		t.Fatal("expired waiters disturbed live workers")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e = withAvatarSlot(ctx, func() error { t.Fatal("cancelled waiter executed"); return nil })
	if !errors.Is(e, context.Canceled) {
		t.Fatal("caller cancellation not retained", e)
	}
	unblock()
	for range 2 {
		if e = <-workers; e != nil {
			t.Fatal(e)
		}
	}
	if len(avatarSlots) != 0 || len(avatarCapacity) != 0 {
		t.Fatal("real queue leaked reservations")
	}
}
