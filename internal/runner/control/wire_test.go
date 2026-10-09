package control

import (
	"context"
	"sync"
	"testing"
	"time"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type heldSocket struct {
	readInput    chan []byte
	closed       chan struct{}
	closeOnce    sync.Once
	writeEntered chan struct{}
	writeRelease chan struct{}
	writeOnce    sync.Once
	mu           sync.Mutex
	writes       [][]byte
}

func testSocket() *heldSocket {
	return &heldSocket{readInput: make(chan []byte, 1), closed: make(chan struct{}), writeEntered: make(chan struct{}), writeRelease: make(chan struct{})}
}
func (s *heldSocket) read() ([]byte, error) {
	select {
	case raw := <-s.readInput:
		return raw, nil
	case <-s.closed:
		return nil, ErrTransport
	}
}
func (s *heldSocket) write(raw []byte) error {
	s.writeOnce.Do(func() { close(s.writeEntered) })
	<-s.writeRelease
	s.mu.Lock()
	s.writes = append(s.writes, append([]byte(nil), raw...))
	s.mu.Unlock()
	return nil
}
func (s *heldSocket) close() error { s.closeOnce.Do(func() { close(s.closed) }); return nil }
func message(t *testing.T, payload p.Payload) p.Message {
	t.Helper()
	id, err := p.NewID()
	if err != nil {
		t.Fatal(err)
	}
	at, err := p.NewInstant(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	m, err := p.NewMessage(p.Header{ProtocolVersion: p.CurrentVersion(), MessageID: id, Timestamp: at}, payload)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("owned barrier not reached")
	}
}
func notJoined(t *testing.T, w *wire) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if w.joined(ctx) {
		t.Fatal("close/cancel was mistaken for actual join")
	}
}
func TestWireActualWriterReturnOwnsBuffer(t *testing.T) {
	s := testSocket()
	w := wireWithSocket(s)
	m := message(t, p.Heartbeat{Sequence: "1", RunnerTime: "2026-10-09T00:00:00Z"})
	if err := w.send(m); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.run(ctx, func(context.Context, p.Message) error { return nil }) }()
	await(t, s.writeEntered)
	cancel()
	await(t, s.closed)
	notJoined(t, w)
	close(s.writeRelease)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("writer did not actually join")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.writes) != 1 {
		t.Fatal("wrong number of native writes")
	}
	got, err := p.Decode(s.writes[0])
	if err != nil || got.Header() != m.Header() {
		t.Fatal("active writer buffer was cleared before return", err)
	}
	if w.queue.bytes != 0 {
		t.Fatal("native write buffer retained after actual return")
	}
}
func TestWireActualCallbackReturnIsRequired(t *testing.T) {
	s := testSocket()
	w := wireWithSocket(s)
	m := message(t, p.HeartbeatAck{Sequence: "1"})
	raw, err := p.Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	s.readInput <- raw
	entered, release := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- w.run(ctx, func(ctx context.Context, got p.Message) error {
			if got.Header() != m.Header() {
				t.Error("wrong original message")
			}
			close(entered)
			<-release
			return ctx.Err()
		})
	}()
	await(t, entered)
	cancel()
	await(t, s.closed)
	notJoined(t, w)
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("receive callback did not actually join")
	}
	if !w.joined(context.Background()) {
		t.Fatal("completed owner not joined")
	}
}
func TestWireRejectsInvalidInboundAndWrongDirection(t *testing.T) {
	wrong := message(t, p.Heartbeat{Sequence: "1", RunnerTime: "2026-10-09T00:00:00Z"})
	wireWrong, err := p.Encode(wrong)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{[]byte(`{"secret":"CANARY"}`), wireWrong} {
		s := testSocket()
		close(s.writeRelease)
		w := wireWithSocket(s)
		s.readInput <- raw
		done := make(chan error, 1)
		go func() {
			done <- w.run(context.Background(), func(context.Context, p.Message) error { t.Error("invalid message published"); return nil })
		}()
		select {
		case err := <-done:
			if err != ErrProtocol {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("invalid wire owner leaked")
		}
		await(t, s.closed)
		s.mu.Lock()
		if len(s.writes) != 1 {
			t.Fatal("fatal diagnostic was not actually written")
		}
		diagnostic, err := p.Decode(s.writes[0])
		s.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		failure, ok := diagnostic.Payload().(p.ProtocolError)
		if !ok || !failure.Fatal {
			t.Fatal("wrong fatal protocol diagnostic")
		}
	}
}

func TestWireFatalDiagnosticTimeoutStillWaitsForWriter(t *testing.T) {
	s := testSocket()
	w := wireWithSocket(s)
	s.readInput <- []byte(`{"invalid":true}`)
	done := make(chan error, 1)
	go func() { done <- w.run(context.Background(), func(context.Context, p.Message) error { return nil }) }()
	await(t, s.writeEntered)
	select {
	case <-s.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("fatal diagnostic exceeded close budget")
	}
	notJoined(t, w)
	close(s.writeRelease)
	select {
	case err := <-done:
		if err != ErrProtocol {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("original diagnostic writer did not join")
	}
}
