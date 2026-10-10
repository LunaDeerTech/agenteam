package runnerhttp

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/runner/service"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type pureSessionSocket struct {
	in, out chan []byte
	closed  chan struct{}
	once    sync.Once
	writeFn func(context.Context, []byte) error
}

func newPureSessionSocket() *pureSessionSocket {
	return &pureSessionSocket{in: make(chan []byte, 8), out: make(chan []byte, 8), closed: make(chan struct{})}
}
func (s *pureSessionSocket) read() ([]byte, error) {
	select {
	case raw := <-s.in:
		return raw, nil
	case <-s.closed:
		return nil, errControlTransport
	}
}
func (s *pureSessionSocket) write(ctx context.Context, raw []byte) error {
	if s.writeFn != nil {
		if e := s.writeFn(ctx, raw); e != nil {
			return e
		}
	}
	select {
	case s.out <- append([]byte(nil), raw...):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.closed:
		return errControlTransport
	}
}
func (s *pureSessionSocket) close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

type pureSessionAuthority struct {
	hellos, beats, statuses atomic.Int32
	current                 func(context.Context) error
	gate                    controlWriteGate
}

func (a *pureSessionAuthority) CurrentConnection(ctx context.Context, _ service.Connection) error {
	if a.current != nil {
		return a.current(ctx)
	}
	return ctx.Err()
}
func (a *pureSessionAuthority) WithCurrentConnection(ctx context.Context, _ service.Connection, fn func(context.Context) error) error {
	if a.gate != nil {
		return a.gate(ctx, fn)
	}
	return pureControlGate(ctx, fn)
}
func (a *pureSessionAuthority) AcceptHello(ctx context.Context, _ service.Connection, _ p.Hello) (p.HelloAck, error) {
	a.hellos.Add(1)
	return p.HelloAck{Accepted: true, NegotiatedProtocolVersion: p.CurrentVersion(), HeartbeatIntervalMS: 10000, HeartbeatTimeoutMS: 30000, EnabledFeatures: []string{}}, ctx.Err()
}
func (a *pureSessionAuthority) Heartbeat(ctx context.Context, _ service.Connection, _ p.Heartbeat) error {
	a.beats.Add(1)
	return ctx.Err()
}
func (a *pureSessionAuthority) UpdateStatus(ctx context.Context, _ service.Connection, _ p.RunnerStatus) error {
	a.statuses.Add(1)
	return ctx.Err()
}

func sessionInput(t *testing.T, socket *pureSessionSocket, value p.Payload) {
	t.Helper()
	var request, operation p.ID
	switch v := value.(type) {
	case p.Response:
		request, operation = v.RequestID, v.OperationID
	case p.Stream:
		request, operation = v.RequestID, v.OperationID
	}
	m, e := controlMessage(value, request, operation)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := p.Encode(m)
	if e != nil {
		t.Fatal(e)
	}
	socket.in <- raw
}
func sessionOutput(t *testing.T, socket *pureSessionSocket) p.Message {
	t.Helper()
	select {
	case raw := <-socket.out:
		message, e := p.Decode(raw)
		if e != nil {
			t.Fatal(e)
		}
		return message
	case <-time.After(2 * time.Second):
		t.Fatal("expected original writer output missing")
		return p.Message{}
	}
}

func startPureCentralSession(t *testing.T, a *pureSessionAuthority, socket *pureSessionSocket, gate controlWriteGate, handshake bool) (*controlSession, context.CancelFunc, <-chan error) {
	t.Helper()
	id, _ := p.NewID()
	s := newControlSession(newControlWire(socket, gate), a, service.Connection{}, id)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	returned := make(chan struct{})
	go func() { defer close(returned); done <- s.run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-returned:
		case <-time.After(3 * time.Second):
			t.Error("session owner did not actually return")
		}
	})
	if handshake {
		sessionInput(t, socket, p.Hello{RunnerID: id, RunnerVersion: "unit", ProtocolVersion: p.CurrentVersion(), OS: "linux", Arch: "amd64", Capabilities: []string{}, FeatureFlags: []string{}})
		if ack := sessionOutput(t, socket); ack.Type() != p.HelloAckType || !ack.Payload().(p.HelloAck).Accepted {
			t.Fatal("hello acknowledgement missing")
		}
		sessionInput(t, socket, p.Heartbeat{Sequence: "1", RunnerTime: "2026-10-09T00:00:00Z"})
		if ack := sessionOutput(t, socket); ack.Type() != p.HeartbeatAckType || ack.Payload().(p.HeartbeatAck).Sequence != "1" {
			t.Fatal("current heartbeat acknowledgement missing")
		}
	}
	return s, cancel, done
}

func TestRunnerCentralSessionPureHelloHeartbeatAndStatus(t *testing.T) {
	t.Run("first frame", func(t *testing.T) {
		a, socket := &pureSessionAuthority{}, newPureSessionSocket()
		_, _, done := startPureCentralSession(t, a, socket, pureControlGate, false)
		sessionInput(t, socket, p.Heartbeat{Sequence: "1", RunnerTime: "2026-10-09T00:00:00Z"})
		message := sessionOutput(t, socket)
		if message.Type() != p.ProtocolErrorType || message.Payload().(p.ProtocolError).Code != p.HelloOrder {
			t.Fatal("non-hello first frame accepted")
		}
		<-done
		if a.hellos.Load() != 0 || a.beats.Load() != 0 {
			t.Fatal("first-frame error published state")
		}
	})
	t.Run("only new heartbeat extends deadline", func(t *testing.T) {
		a, socket := &pureSessionAuthority{}, newPureSessionSocket()
		s, _, done := startPureCentralSession(t, a, socket, pureControlGate, true)
		before := s.nextDeadline()
		// Directly consume a status through the same public-message callback;
		// the socket reader is idle, so no second reader can race this stimulus.
		if e := s.receive(context.Background(), pureControlMessage(t, p.RunnerStatus{Capabilities: []string{}})); e != nil {
			t.Fatal(e)
		}
		if !s.nextDeadline().Equal(before) || a.statuses.Load() != 1 {
			t.Fatal("status renewed heartbeat lease")
		}
		sessionInput(t, socket, p.Heartbeat{Sequence: "1", RunnerTime: "2026-10-09T00:00:00Z"})
		message := sessionOutput(t, socket)
		if message.Payload().(p.ProtocolError).Code != p.CorrelationInvalid {
			t.Fatal("replayed heartbeat accepted")
		}
		<-done
		if a.beats.Load() != 1 || !s.nextDeadline().Equal(before) {
			t.Fatal("replayed heartbeat reached persistence or renewed timer")
		}
	})
}

func TestRunnerCentralSessionPureTerminalAndCorrelation(t *testing.T) {
	a, socket := &pureSessionAuthority{}, newPureSessionSocket()
	s, stop, done := startPureCentralSession(t, a, socket, pureControlGate, true)
	result := make(chan controlResult, 1)
	original := pureControlRequest(t, 0).Payload().(p.Request)
	var streams atomic.Int32
	go func() {
		value, _ := s.dispatch(context.Background(), original, func(context.Context, p.Stream) error { streams.Add(1); return nil })
		result <- value
	}()
	message := sessionOutput(t, socket)
	request := message.Payload().(p.Request)
	if request.RequestID == original.RequestID || request.OperationID != original.OperationID {
		t.Fatal("technical attempt did not replace only request identity")
	}
	sessionInput(t, socket, p.Stream{RequestID: request.RequestID, OperationID: request.OperationID, Stream: "stdout", Sequence: "1", Data: "unit", Timestamp: "2026-10-09T00:00:00Z"})
	terminal := p.Response{RequestID: request.RequestID, OperationID: request.OperationID, Outcome: p.Success, Payload: []byte(`{}`)}
	sessionInput(t, socket, terminal)
	got := <-result
	if !got.attempted || got.terminal == nil || got.terminal.Outcome != p.Success || streams.Load() != 1 {
		t.Fatal("legal terminal was not retained")
	}
	sessionInput(t, socket, terminal)
	fatal := sessionOutput(t, socket)
	if fatal.Type() != p.ProtocolErrorType || fatal.Payload().(p.ProtocolError).Code != p.CorrelationInvalid {
		t.Fatal("duplicate terminal accepted")
	}
	<-done
	stop()
	if got.terminal.Outcome != p.Success {
		t.Fatal("disconnect rewrote known terminal")
	}
}

func TestRunnerCentralSessionPureNotSentAndUnknown(t *testing.T) {
	t.Run("cancel before actual gate admission", func(t *testing.T) {
		a, socket := &pureSessionAuthority{}, newPureSessionSocket()
		var hold atomic.Bool
		entered, release := make(chan struct{}), make(chan struct{})
		gate := func(ctx context.Context, fn func(context.Context) error) error {
			if hold.CompareAndSwap(true, false) {
				close(entered)
				<-release
			}
			return pureControlGate(ctx, fn)
		}
		s, _, _ := startPureCentralSession(t, a, socket, gate, true)
		t.Cleanup(func() {
			select {
			case <-release:
			default:
				close(release)
			}
		})
		hold.Store(true)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		result := make(chan controlResult, 1)
		go func() {
			value, _ := s.dispatch(ctx, pureControlRequest(t, 0).Payload().(p.Request), nil)
			result <- value
		}()
		<-entered
		cancel()
		if value := <-result; value.attempted || value.terminal != nil {
			t.Fatal("pre-write cancel claimed an attempt or terminal")
		}
		close(release)
		sessionInput(t, socket, p.Heartbeat{Sequence: "2", RunnerTime: "2026-10-09T00:00:00Z"})
		if output := sessionOutput(t, socket); output.Type() != p.HeartbeatAckType {
			t.Fatal("cancelled queued request reached native writer")
		}
	})
	t.Run("attempted write with original callback held", func(t *testing.T) {
		a, socket := &pureSessionAuthority{}, newPureSessionSocket()
		entered, release := make(chan struct{}), make(chan struct{})
		socket.writeFn = func(ctx context.Context, raw []byte) error {
			m, _ := p.Decode(raw)
			if m.Type() == p.RequestType {
				close(entered)
				<-release
				return ctx.Err()
			}
			return nil
		}
		s, stop, done := startPureCentralSession(t, a, socket, pureControlGate, true)
		t.Cleanup(func() {
			select {
			case <-release:
			default:
				close(release)
			}
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		result := make(chan controlResult, 1)
		go func() {
			value, _ := s.dispatch(ctx, pureControlRequest(t, 0).Payload().(p.Request), nil)
			result <- value
		}()
		<-entered
		cancel()
		if value := <-result; !value.attempted || value.terminal != nil {
			t.Fatal("attempted write became not_sent or fabricated cancelled")
		}
		stop()
		select {
		case <-done:
			t.Fatal("session returned before actual writer")
		case <-time.After(15 * time.Millisecond):
		}
		close(release)
		<-done
	})
}

func TestRunnerCentralSessionPureGenerationAndObserverJoin(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	denied := errors.New("different generation")
	a := &pureSessionAuthority{current: func(context.Context) error { close(entered); <-release; return denied }}
	socket := newPureSessionSocket()
	_, stop, done := startPureCentralSession(t, a, socket, pureControlGate, true)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	select {
	case <-entered:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("generation observer did not start within its fixed cadence")
	}
	stop()
	<-socket.closed
	select {
	case <-done:
		t.Fatal("cancel skipped actual generation observer return")
	case <-time.After(15 * time.Millisecond):
	}
	close(release)
	<-done
}
