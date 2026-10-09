// Package control owns the Runner's outbound control connection and its work.
// It has no Central business dependencies and never authorizes a Mount itself.
package control

import (
	"context"
	"errors"
	"sync"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

var (
	ErrClosed       = errors.New("runner control is closed")
	ErrBackpressure = errors.New("runner control queue is full")
	ErrProtocol     = errors.New("runner control protocol violation")
	ErrTransport    = errors.New("runner control transport failed")
)

// FIFO order is intentional: a terminal can never overtake its previously
// admitted streams. Reserved capacity limits admission, not write ordering.
// Capacity includes the one leased frame until the writer actually returns.
type outbox struct {
	mu                          sync.Mutex
	frames                      []*outbound
	bytes, dataCount, dataBytes int
	active                      *outbound
	closed                      bool
	wake                        chan struct{}
}
type outbound struct {
	wire []byte
	data bool
	done chan error
}

func newOutbox() *outbox { return &outbox{wake: make(chan struct{}, 1)} }
func (q *outbox) push(message p.Message) error {
	_, err := q.enqueue(message, false)
	return err
}
func (q *outbox) enqueue(message p.Message, observe bool) (<-chan error, error) {
	if message.AllowedFrom(p.Runner) != nil {
		return nil, ErrProtocol
	}
	raw, err := p.Encode(message)
	if err != nil {
		return nil, ErrProtocol
	}
	var done chan error
	if observe {
		done = make(chan error, 1)
	}
	return done, q.admit(&outbound{wire: raw, data: message.Type() == p.StreamType, done: done})
}

// raw ownership is transferred even on rejection. No caller may retain it.
func (q *outbox) pushEncoded(raw []byte, data bool) error {
	return q.admit(&outbound{wire: raw, data: data})
}
func (q *outbox) admit(frame *outbound) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	raw, data := frame.wire, frame.data
	if q.closed {
		clear(raw)
		return ErrClosed
	}
	if len(raw) == 0 || len(raw) > p.MaxMessageBytes {
		clear(raw)
		return ErrProtocol
	}
	if len(q.frames) >= p.MaxQueuedMessages || q.bytes+len(raw) > p.MaxQueuedBytes || data && (q.dataCount >= p.MaxQueuedMessages-p.ReservedControlMessages || q.dataBytes+len(raw) > p.MaxQueuedBytes-p.ReservedControlBytes) {
		clear(raw)
		return ErrBackpressure
	}
	q.frames = append(q.frames, frame)
	q.bytes += len(raw)
	if data {
		q.dataCount++
		q.dataBytes += len(raw)
	}
	q.signal()
	return nil
}
func (q *outbox) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *outbox) acquire(ctx context.Context) (*outbound, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		q.mu.Lock()
		if q.closed {
			q.mu.Unlock()
			return nil, ErrClosed
		}
		if q.active != nil {
			q.mu.Unlock()
			return nil, ErrProtocol
		}
		if len(q.frames) != 0 {
			frame := q.frames[0]
			q.active = frame
			q.mu.Unlock()
			return frame, nil
		}
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-q.wake:
		}
	}
}
func (q *outbox) release(frame *outbound, result error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if frame == nil || q.active != frame {
		return
	}
	q.active = nil
	q.bytes -= len(frame.wire)
	if frame.data {
		q.dataCount--
		q.dataBytes -= len(frame.wire)
	}
	clear(frame.wire)
	frame.wire = nil
	frame.finish(result)
	q.frames[0] = nil
	q.frames = q.frames[1:]
	q.signal()
}
func (q *outbox) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.closed = true
	// A leased buffer belongs to the writer. Clearing it here would race a
	// still-running native write and falsely imply that write has joined.
	start := 0
	if q.active != nil {
		start = 1
	}
	for _, frame := range q.frames[start:] {
		q.bytes -= len(frame.wire)
		if frame.data {
			q.dataCount--
			q.dataBytes -= len(frame.wire)
		}
		clear(frame.wire)
		frame.wire = nil
		frame.finish(ErrClosed)
	}
	clear(q.frames[start:])
	q.frames = q.frames[:start]
	q.signal()
}
func (frame *outbound) finish(result error) {
	if frame.done != nil {
		frame.done <- result
		close(frame.done)
		frame.done = nil
	}
}
