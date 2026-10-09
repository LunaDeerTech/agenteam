package control

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/gorilla/websocket"
)

// Only the actual gorilla adapter is used by production. The private interface
// permits tests to hold a native call's return after cancellation, without
// substituting a successful operation or fabricating a joined connection.
type wireSocket interface {
	read() ([]byte, error)
	write([]byte) error
	close() error
}
type nativeSocket struct{ conn *websocket.Conn }

func (s nativeSocket) read() ([]byte, error) {
	kind, reader, err := s.conn.NextReader()
	if err != nil {
		return nil, ErrTransport
	}
	if kind != websocket.TextMessage {
		_ = s.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseUnsupportedData, "text required"), time.Now().Add(time.Second))
		return nil, ErrProtocol
	}
	// SetReadLimit bounds fragmented wire messages. This additional bound also
	// protects the application read if an incorrectly negotiated extension ever
	// reached this adapter; the dial/upgrade boundary must reject extensions.
	raw, err := io.ReadAll(io.LimitReader(reader, p.MaxMessageBytes+1))
	if err != nil {
		clear(raw)
		return nil, ErrTransport
	}
	if len(raw) > p.MaxMessageBytes {
		clear(raw)
		_ = s.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseMessageTooBig, "message limit"), time.Now().Add(time.Second))
		return nil, p.ErrTooLarge
	}
	return raw, nil
}
func (s nativeSocket) write(raw []byte) error {
	if s.conn.SetWriteDeadline(time.Now().Add(5*time.Second)) != nil {
		return ErrTransport
	}
	if s.conn.WriteMessage(websocket.TextMessage, raw) != nil {
		return ErrTransport
	}
	return nil
}
func (s nativeSocket) close() error {
	// Interrupt a TLS write before Close tries to send close_notify. This is an
	// abort path, not evidence that either goroutine or its callback has joined.
	_ = s.conn.UnderlyingConn().SetDeadline(time.Now())
	if s.conn.Close() != nil {
		return ErrTransport
	}
	return nil
}

type wire struct {
	socket   wireSocket
	queue    *outbox
	stopOnce sync.Once
	stopCh   chan struct{}
	done     chan struct{}
	mu       sync.Mutex
	started  bool
}

func newWire(conn *websocket.Conn) (*wire, error) {
	if conn == nil {
		return nil, ErrTransport
	}
	if conn.Subprotocol() != p.Subprotocol {
		_ = (nativeSocket{conn}).close()
		return nil, ErrProtocol
	}
	conn.EnableWriteCompression(false)
	conn.SetReadLimit(p.MaxMessageBytes)
	return wireWithSocket(nativeSocket{conn}), nil
}
func wireWithSocket(socket wireSocket) *wire {
	return &wire{socket: socket, queue: newOutbox(), stopCh: make(chan struct{}), done: make(chan struct{})}
}
func (w *wire) send(message p.Message) error {
	err := w.queue.push(message)
	if err == ErrBackpressure {
		w.stop()
	}
	return err
}
func (w *wire) stop() {
	w.stopOnce.Do(func() {
		close(w.stopCh)
		w.queue.close()
		_ = w.socket.close()
	})
}
func (w *wire) joined(ctx context.Context) bool {
	select {
	case <-w.done:
		return true
	default:
	}
	select {
	case <-w.done:
		return true
	case <-ctx.Done():
		return false
	}
}

// run has one application data writer and one reader/callback owner. Gorilla's
// bounded native ping/close replies use its documented concurrent WriteControl.
// Cancellation closes the socket, but this method returns only after both
// workers, including the original receive callback, actually return.
func (w *wire) run(ctx context.Context, receive func(context.Context, p.Message) error) error {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return ErrProtocol
	}
	w.started = true
	w.mu.Unlock()
	defer close(w.done)
	if receive == nil {
		w.stop()
		return ErrProtocol
	}
	owned, cancel := context.WithCancel(ctx)
	defer cancel()
	readDone, writeDone := make(chan error, 1), make(chan error, 1)
	go func() { readDone <- w.read(owned, receive) }()
	go func() { writeDone <- w.write(owned) }()
	var result error
	readReturned, writeReturned := false, false
	select {
	case result = <-readDone:
		readReturned = true
	case result = <-writeDone:
		writeReturned = true
	case <-ctx.Done():
		result = ctx.Err()
	case <-w.stopCh:
		result = ErrClosed
	}
	cancel()
	w.stop()
	if !readReturned {
		<-readDone
	}
	if !writeReturned {
		<-writeDone
	}
	return result
}
func (w *wire) read(ctx context.Context, receive func(context.Context, p.Message) error) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, err := w.socket.read()
		if err != nil {
			return err
		}
		message, err := p.Decode(raw)
		clear(raw)
		if err != nil {
			if errors.Is(err, p.ErrIncompatibleVersion) {
				return p.ErrIncompatibleVersion
			}
			return ErrProtocol
		}
		if message.AllowedFrom(p.Central) != nil {
			return ErrProtocol
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = receive(ctx, message); err != nil {
			return err
		}
	}
}
func (w *wire) write(ctx context.Context) error {
	for {
		frame, err := w.queue.acquire(ctx)
		if err != nil {
			return err
		}
		err = w.socket.write(frame.wire)
		w.queue.release(frame)
		if err != nil {
			return err
		}
	}
}
