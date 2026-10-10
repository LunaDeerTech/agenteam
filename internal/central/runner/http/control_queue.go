package runnerhttp

import (
	"context"
	"errors"
	"sync"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

var (
	errControlClosed    = errors.New("runner connection closed")
	errControlProtocol  = errors.New("invalid runner protocol")
	errControlTransport = errors.New("runner transport failed")
	errControlCapacity  = errors.New("runner connection capacity exceeded")
)

// The writer retains the first frame in the queue until its original native
// call returns. Control reserve is admission capacity, never priority ordering.
type controlQueue struct {
	mu                    sync.Mutex
	frames                []*controlFrame
	bytes, data, dataSize int
	active                *controlFrame
	closed                bool
	wake                  chan struct{}
}

type controlFrame struct {
	raw     []byte
	data    bool
	attempt func() bool // Runs inside the current-generation write transaction.
	done    chan error
}

func newControlQueue() *controlQueue { return &controlQueue{wake: make(chan struct{}, 1)} }

func (q *controlQueue) enqueue(message p.Message, attempt func() bool) (*controlFrame, error) {
	if message.AllowedFrom(p.Central) != nil {
		return nil, errControlProtocol
	}
	raw, e := p.Encode(message)
	if e != nil {
		return nil, errControlProtocol
	}
	frame := &controlFrame{raw: raw, data: message.Type() == p.RequestType, attempt: attempt, done: make(chan error, 1)}
	if e := q.admit(frame); e != nil {
		return nil, e
	}
	return frame, nil
}

func (q *controlQueue) admit(frame *controlFrame) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		clear(frame.raw)
		return errControlClosed
	}
	if len(frame.raw) == 0 || len(frame.raw) > p.MaxMessageBytes {
		clear(frame.raw)
		return errControlProtocol
	}
	if len(q.frames) >= p.MaxQueuedMessages || q.bytes+len(frame.raw) > p.MaxQueuedBytes || frame.data && (q.data >= p.MaxQueuedMessages-p.ReservedControlMessages || q.dataSize+len(frame.raw) > p.MaxQueuedBytes-p.ReservedControlBytes) {
		clear(frame.raw)
		return errControlCapacity
	}
	q.frames = append(q.frames, frame)
	q.bytes += len(frame.raw)
	if frame.data {
		q.data++
		q.dataSize += len(frame.raw)
	}
	q.signal()
	return nil
}

func (q *controlQueue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *controlQueue) acquire(ctx context.Context) (*controlFrame, error) {
	for {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		q.mu.Lock()
		if q.closed || q.active != nil {
			q.mu.Unlock()
			return nil, errControlClosed
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

func (q *controlQueue) retire(frame *controlFrame, e error) {
	q.bytes -= len(frame.raw)
	if frame.data {
		q.data--
		q.dataSize -= len(frame.raw)
	}
	clear(frame.raw)
	frame.raw = nil
	frame.done <- e
	close(frame.done)
}

func (q *controlQueue) release(frame *controlFrame, e error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if frame == nil || q.active != frame {
		return
	}
	q.active = nil
	q.retire(frame, e)
	q.frames[0] = nil
	q.frames = q.frames[1:]
	q.signal()
}

func (q *controlQueue) stop() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.closed = true
	first := 0
	if q.active != nil {
		first = 1
	}
	for _, frame := range q.frames[first:] {
		q.retire(frame, errControlClosed)
	}
	clear(q.frames[first:])
	q.frames = q.frames[:first]
	q.signal()
}
