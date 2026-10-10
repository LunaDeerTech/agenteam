package runnerhttp

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/gorilla/websocket"
)

// Production always uses gorilla. The private socket permits an offline test
// to hold an original call after cancellation, not to manufacture a joined I/O.
type controlSocket interface {
	read() ([]byte, error)
	write(context.Context, []byte) error
	close() error
}

type controlNativeSocket struct{ conn *websocket.Conn }

func (s controlNativeSocket) read() ([]byte, error) {
	typeID, reader, e := s.conn.NextReader()
	if e != nil {
		return nil, errControlTransport
	}
	if typeID != websocket.TextMessage {
		_ = s.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseUnsupportedData, "text required"), time.Now().Add(time.Second))
		return nil, errControlProtocol
	}
	raw, e := io.ReadAll(io.LimitReader(reader, p.MaxMessageBytes+1))
	if e != nil {
		clear(raw)
		return nil, errControlTransport
	}
	if len(raw) > p.MaxMessageBytes {
		clear(raw)
		_ = s.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseMessageTooBig, "message limit"), time.Now().Add(time.Second))
		return nil, p.ErrTooLarge
	}
	return raw, nil
}

func (s controlNativeSocket) write(ctx context.Context, raw []byte) error {
	deadline, ok := ctx.Deadline()
	if !ok || ctx.Err() != nil || time.Until(deadline) > 5*time.Second || s.conn.SetWriteDeadline(deadline) != nil {
		return errControlTransport
	}
	if s.conn.WriteMessage(websocket.TextMessage, raw) != nil {
		return errControlTransport
	}
	return nil
}

func (s controlNativeSocket) close() error {
	_ = s.conn.UnderlyingConn().SetDeadline(time.Now())
	if s.conn.Close() != nil {
		return errControlTransport
	}
	return nil
}

type controlWriteGate func(context.Context, func(context.Context) error) error

type controlWire struct {
	socket       controlSocket
	queue        *controlQueue
	gate         controlWriteGate
	incompatible func(context.Context) error
	once         sync.Once
	stopCh       chan struct{}
	done         chan struct{}
	mu           sync.Mutex
	active       bool
}

func newControlWire(socket controlSocket, gate controlWriteGate) *controlWire {
	return &controlWire{socket: socket, queue: newControlQueue(), gate: gate, stopCh: make(chan struct{}), done: make(chan struct{})}
}

func (w *controlWire) send(message p.Message, attempt func() bool) (*controlFrame, error) {
	frame, e := w.queue.enqueue(message, attempt)
	if e == errControlCapacity {
		w.stop()
	}
	return frame, e
}

func (w *controlWire) stop() {
	w.once.Do(func() {
		close(w.stopCh)
		w.queue.stop()
		if w.socket != nil {
			_ = w.socket.close()
		}
	})
}

func (w *controlWire) run(ctx context.Context, receive func(context.Context, p.Message) error) error {
	w.mu.Lock()
	if w.active {
		w.mu.Unlock()
		return errControlProtocol
	}
	w.active = true
	w.mu.Unlock()
	defer close(w.done)
	if ctx == nil || w.socket == nil || w.gate == nil || receive == nil {
		w.stop()
		return errControlProtocol
	}
	owned, cancel := context.WithCancel(ctx)
	defer cancel()
	readDone, writeDone := make(chan error, 1), make(chan error, 1)
	go func() { readDone <- w.read(owned, receive) }()
	go func() { writeDone <- w.write(owned) }()
	var e error
	reader, writer := false, false
	select {
	case e = <-readDone:
		reader = true
	case e = <-writeDone:
		writer = true
	case <-ctx.Done():
		e = ctx.Err()
	case <-w.stopCh:
		e = errControlClosed
	}
	cancel()
	w.stop()
	if !reader {
		<-readDone
	}
	if !writer {
		<-writeDone
	}
	return e
}

func (w *controlWire) write(ctx context.Context) error {
	for {
		frame, e := w.queue.acquire(ctx)
		if e != nil {
			return e
		}
		e = w.gate(ctx, func(gated context.Context) error {
			if e := gated.Err(); e != nil {
				return e
			}
			if frame.attempt != nil && !frame.attempt() {
				return nil // Caller cancelled before any write attempt.
			}
			return w.socket.write(gated, frame.raw)
		})
		w.queue.release(frame, e)
		if e != nil {
			return e
		}
	}
}

func (w *controlWire) read(ctx context.Context, receive func(context.Context, p.Message) error) error {
	for {
		if e := ctx.Err(); e != nil {
			return e
		}
		raw, e := w.socket.read()
		if e != nil {
			return e
		}
		message, e := p.Decode(raw)
		clear(raw)
		if e != nil {
			code := p.InvalidEnvelope
			if errors.Is(e, p.ErrIncompatibleVersion) {
				code = p.IncompatibleVersion
				if w.incompatible != nil {
					if rejected := w.incompatible(ctx); rejected != nil {
						return rejected
					}
				}
			}
			w.report(ctx, code, "")
			return errControlProtocol
		}
		if message.AllowedFrom(p.Runner) != nil {
			w.report(ctx, p.UnsupportedMessage, message.Header().MessageID)
			return errControlProtocol
		}
		if e := ctx.Err(); e != nil {
			return e
		}
		if e := receive(ctx, message); e != nil {
			var failure controlProtocolFailure
			if errors.As(e, &failure) {
				w.report(ctx, failure.code, message.Header().MessageID)
			}
			return e
		}
	}
}

type controlProtocolFailure struct{ code p.ProtocolCode }

func (e controlProtocolFailure) Error() string { return "runner protocol rejected" }

func controlMessage(payload p.Payload, request, operation p.ID) (p.Message, error) {
	id, e := p.NewID()
	if e != nil {
		return p.Message{}, errControlTransport
	}
	at, e := p.NewInstant(time.Now())
	if e != nil {
		return p.Message{}, errControlProtocol
	}
	return p.NewMessage(p.Header{ProtocolVersion: p.CurrentVersion(), MessageID: id, RequestID: request, OperationID: operation, Timestamp: at}, payload)
}

func (w *controlWire) report(ctx context.Context, code p.ProtocolCode, offending p.ID) {
	message, e := controlMessage(p.ProtocolError{Code: code, SafeMessage: code.SafeMessage(), OffendingMessageID: offending, Fatal: true}, "", "")
	if e != nil {
		return
	}
	frame, e := w.send(message, nil)
	if e != nil {
		return
	}
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-frame.done:
	case <-ctx.Done():
	case <-timer.C:
	}
}
