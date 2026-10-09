package control

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func TestOutboxControlReserveAndOrder(t *testing.T) {
	q := newOutbox()
	for i := range p.MaxQueuedMessages - p.ReservedControlMessages {
		if err := q.pushEncoded([]byte{byte(i)}, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.pushEncoded([]byte{1}, true); err != ErrBackpressure {
		t.Fatal("stream invaded reserve", err)
	}
	for range p.ReservedControlMessages {
		if err := q.pushEncoded([]byte("terminal"), false); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.pushEncoded([]byte{1}, false); err != ErrBackpressure {
		t.Fatal("queue overflow", err)
	}
	for i := range p.MaxQueuedMessages {
		frame, err := q.acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if i < p.MaxQueuedMessages-p.ReservedControlMessages && (!frame.data || frame.wire[0] != byte(i)) {
			t.Fatal("terminal overtook stream")
		}
		if i >= p.MaxQueuedMessages-p.ReservedControlMessages && string(frame.wire) != "terminal" {
			t.Fatal("terminal dropped")
		}
		if i == 0 {
			if err := q.pushEncoded([]byte{1}, false); err != ErrBackpressure {
				t.Fatal("leased write no longer counted")
			}
			if _, err := q.acquire(context.Background()); err != ErrProtocol {
				t.Fatal("second writer admitted")
			}
		}
		q.release(frame)
	}
	if q.bytes != 0 || q.dataCount != 0 || q.dataBytes != 0 {
		t.Fatal("accounting did not retire frames")
	}
}

func TestOutboxByteCapacityAndWriterOwnership(t *testing.T) {
	q := newOutbox()
	for range 3 {
		if err := q.pushEncoded(bytes.Repeat([]byte{1}, p.MaxMessageBytes), true); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.pushEncoded(make([]byte, p.MaxMessageBytes-p.ReservedControlBytes), true); err != nil {
		t.Fatal(err)
	}
	if err := q.pushEncoded([]byte{1}, true); err != ErrBackpressure {
		t.Fatal("byte reserve invaded")
	}
	if err := q.pushEncoded(make([]byte, p.ReservedControlBytes), false); err != nil {
		t.Fatal(err)
	}
	if err := q.pushEncoded([]byte{1}, false); err != ErrBackpressure {
		t.Fatal("total byte cap exceeded")
	}
	frame, err := q.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	q.close()
	if len(frame.wire) != p.MaxMessageBytes || frame.wire[0] != 1 || q.bytes != p.MaxMessageBytes {
		t.Fatal("close pretended native writer released buffer")
	}
	q.release(frame)
	if q.bytes != 0 || len(q.frames) != 0 {
		t.Fatal("leased buffer not retired")
	}
	if _, err := q.acquire(context.Background()); err != ErrClosed {
		t.Fatal(err)
	}
	if err := q.pushEncoded([]byte{1}, false); err != ErrClosed {
		t.Fatal(err)
	}
}

func TestOutboxCancellationAndConcurrentClose(t *testing.T) {
	q := newOutbox()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { _, err := q.acquire(ctx); result <- err }()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled writer did not join")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				err := q.pushEncoded([]byte{1}, false)
				if err != nil && err != ErrClosed && err != ErrBackpressure {
					t.Error(err)
				}
			}
		})
	}
	q.close()
	wg.Wait()
	if q.bytes != 0 || len(q.frames) != 0 {
		t.Fatal("closed queue retained frames")
	}
}
