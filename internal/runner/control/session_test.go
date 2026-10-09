package control

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func sessionID(t *testing.T) p.ID {
	t.Helper()
	id, err := p.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func hello(t *testing.T) p.Hello {
	return p.Hello{RunnerID: sessionID(t), RunnerVersion: "test", ProtocolVersion: p.CurrentVersion(), OS: "linux", Arch: "amd64", Headless: true, Capabilities: []string{}, FeatureFlags: []string{}}
}
func helloAck(t *testing.T) p.Message {
	return message(t, p.HelloAck{Accepted: true, NegotiatedProtocolVersion: p.CurrentVersion(), HeartbeatIntervalMS: 1000, HeartbeatTimeoutMS: 3000, EnabledFeatures: []string{}})
}
func request(t *testing.T) p.Request {
	return p.Request{RequestID: sessionID(t), OperationID: sessionID(t), ExecutionID: sessionID(t), ProjectID: sessionID(t), AgentID: sessionID(t), Mount: p.Mount{MountID: sessionID(t), WorkspaceID: sessionID(t)}, OperationName: "fixture.execute", OperationRevision: "1", Payload: json.RawMessage(`{"input":"CANARY"}`)}
}
func correlated(t *testing.T, payload p.Payload, request, operation p.ID) p.Message {
	t.Helper()
	m, err := newMessage(payload, request, operation)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func TestSessionClockAndCorrelation(t *testing.T) {
	now := time.Now()
	s := newSessionState(now)
	if err := s.observe(message(t, p.HeartbeatAck{Sequence: "1"}), now); err == nil {
		t.Fatal("pre-hello heartbeat accepted")
	}
	s = newSessionState(now)
	if err := s.observe(helloAck(t), now.Add(5*time.Second)); err != errHelloTimeout {
		t.Fatal("late hello revived original deadline", err)
	}
	s = newSessionState(now)
	ack := helloAck(t)
	if err := s.observe(ack, now); err != nil {
		t.Fatal(err)
	}
	if err := s.observe(ack, now); err == nil {
		t.Fatal("duplicate message id accepted")
	}
	beat, err := s.heartbeat(now.Add(time.Second))
	if err != nil || beat == nil || beat.Sequence != "1" {
		t.Fatal("heartbeat did not start at 1", err)
	}
	if err := s.observe(message(t, p.HeartbeatAck{Sequence: "2"}), now.Add(time.Second)); err == nil || s.lastAck != now {
		t.Fatal("unsent sequence renewed heartbeat")
	}
	if err := s.observe(message(t, p.HeartbeatAck{Sequence: "1"}), now.Add(1500*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	ackTime := s.lastAck
	if err := s.observe(message(t, p.HeartbeatAck{Sequence: "1"}), now.Add(2*time.Second)); err == nil || s.lastAck != ackTime {
		t.Fatal("duplicate ack renewed heartbeat")
	}
	info := message(t, p.ProtocolError{Code: p.UnsupportedMessage, SafeMessage: p.UnsupportedMessage.SafeMessage()})
	if err := s.observe(info, ackTime.Add(2999*time.Millisecond)); err != nil || s.lastAck != ackTime {
		t.Fatal("other message renewed heartbeat", err)
	}
	if _, err := s.heartbeat(ackTime.Add(3 * time.Second)); err != errHeartbeatTimeout {
		t.Fatal("heartbeat expiry was extended", err)
	}
}
func TestSessionBoundedDedupeAndRequests(t *testing.T) {
	now := time.Now()
	s := newSessionState(now)
	if err := s.observe(helloAck(t), now); err != nil {
		t.Fatal(err)
	}
	r := request(t)
	for range p.MaxPendingRequests {
		r.RequestID = sessionID(t)
		if err := s.admit(r); err != nil {
			t.Fatal(err)
		}
	}
	over := r
	over.RequestID = sessionID(t)
	if s.admit(over) == nil {
		t.Fatal("pending cap exceeded")
	}
	if s.seal(r) != nil || s.admit(r) == nil || s.admit(over) != nil {
		t.Fatal("terminal either lost dedupe or failed to release active slot")
	}
	if pending, err := s.cancel(p.Cancel{RequestID: r.RequestID, OperationID: r.OperationID}); err != nil || pending {
		t.Fatal("late correlated cancel changed known terminal", err)
	}
	if _, err := s.cancel(p.Cancel{RequestID: r.RequestID, OperationID: sessionID(t)}); err == nil {
		t.Fatal("wrong operation cancel accepted")
	}
	for len(s.seen) < p.MaxSeenMessages {
		m := message(t, p.ProtocolError{Code: p.UnsupportedMessage, SafeMessage: p.UnsupportedMessage.SafeMessage()})
		if err := s.observe(m, now); err != nil {
			t.Fatal(err)
		}
	}
	if s.observe(message(t, p.ProtocolError{Code: p.UnsupportedMessage, SafeMessage: p.UnsupportedMessage.SafeMessage()}), now) == nil || len(s.seen) != p.MaxSeenMessages {
		t.Fatal("dedupe overflow silently evicted or grew")
	}
}

type sessionSocket struct {
	in, out chan p.Message
	closed  chan struct{}
	once    sync.Once
}

func (s *sessionSocket) read() ([]byte, error) {
	select {
	case m := <-s.in:
		return p.Encode(m)
	case <-s.closed:
		return nil, ErrTransport
	}
}
func (s *sessionSocket) write(raw []byte) error {
	m, err := p.Decode(raw)
	if err != nil {
		return err
	}
	select {
	case s.out <- m:
		return nil
	case <-s.closed:
		return ErrTransport
	}
}
func (s *sessionSocket) close() error { s.once.Do(func() { close(s.closed) }); return nil }

type testExecutor struct {
	run   func(context.Context, p.Request, func(string, string) error) (p.Response, error)
	ready bool
}

func (e testExecutor) execute(ctx context.Context, r p.Request, sink func(string, string) error) (p.Response, error) {
	return e.run(ctx, r, sink)
}
func (testExecutor) accepts(p.Request) bool { return true }
func (e testExecutor) maskingReady() bool   { return e.ready }
func (testExecutor) mask(data string) (string, error) {
	return strings.ReplaceAll(data, "CANARY", "[masked]"), nil
}
func sessionFixture(t *testing.T, executor operationExecutor) (*sessionSocket, *session, context.CancelFunc, <-chan error) {
	t.Helper()
	socket := &sessionSocket{in: make(chan p.Message, 16), out: make(chan p.Message, 64), closed: make(chan struct{})}
	s, err := newSession(wireWithSocket(socket), hello(t), executor)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.run(ctx) }()
	t.Cleanup(cancel)
	if m := nextMessage(t, socket); m.Type() != p.HelloType {
		t.Fatal("hello was not first")
	}
	return socket, s, cancel, done
}
func nextMessage(t *testing.T, s *sessionSocket) p.Message {
	t.Helper()
	select {
	case m := <-s.out:
		return m
	case <-time.After(time.Second):
		t.Fatal("message not actually written")
		return p.Message{}
	}
}
func finishSession(t *testing.T, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("session owner did not actually return")
	}
}
func TestSessionProductionRegistryFailsClosed(t *testing.T) {
	socket, _, cancel, done := sessionFixture(t, nil)
	socket.in <- helloAck(t)
	r := request(t)
	socket.in <- correlated(t, r, r.RequestID, r.OperationID)
	result, ok := nextMessage(t, socket).Payload().(p.Response)
	if !ok || result.Outcome != p.Failure || result.Code == nil || *result.Code != p.UnsupportedOperation || result.RequestID != r.RequestID {
		t.Fatal("unbound production operation did not fail closed")
	}
	socket.in <- correlated(t, p.Cancel{RequestID: r.RequestID, OperationID: r.OperationID, Reason: "caller_cancelled"}, r.RequestID, r.OperationID)
	finishSession(t, cancel, done)
}
func TestSessionStreamsPrecedeOriginalTerminal(t *testing.T) {
	socket, _, cancel, done := sessionFixture(t, testExecutor{ready: true, run: func(ctx context.Context, r p.Request, sink func(string, string) error) (p.Response, error) {
		if err := sink("stdout", "CANARY first"); err != nil {
			return p.Response{}, err
		}
		if err := sink("stdout", "second"); err != nil {
			return p.Response{}, err
		}
		return p.Response{RequestID: r.RequestID, OperationID: r.OperationID, Outcome: p.Success}, nil
	}})
	socket.in <- helloAck(t)
	r := request(t)
	socket.in <- correlated(t, r, r.RequestID, r.OperationID)
	for i, want := range []string{"[masked] first", "second"} {
		stream, ok := nextMessage(t, socket).Payload().(p.Stream)
		if !ok || string(stream.Sequence) != []string{"1", "2"}[i] || stream.Data != want {
			t.Fatal("stream order, masking or sequence failed")
		}
	}
	terminal, ok := nextMessage(t, socket).Payload().(p.Response)
	if !ok || terminal.Outcome != p.Success || terminal.OperationID != r.OperationID {
		t.Fatal("original terminal did not follow streams")
	}
	finishSession(t, cancel, done)
}
func TestSessionCancelDoesNotInventTerminalOrJoin(t *testing.T) {
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	socket, s, cancel, done := sessionFixture(t, testExecutor{ready: true, run: func(ctx context.Context, _ p.Request, _ func(string, string) error) (p.Response, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return p.Response{}, ctx.Err()
	}})
	socket.in <- helloAck(t)
	r := request(t)
	socket.in <- correlated(t, r, r.RequestID, r.OperationID)
	await(t, entered)
	socket.in <- correlated(t, p.Cancel{RequestID: r.RequestID, OperationID: r.OperationID, Reason: "caller_cancelled"}, r.RequestID, r.OperationID)
	await(t, cancelled)
	select {
	case <-socket.out:
		t.Fatal("cancel manufactured a terminal")
	default:
	}
	cancel()
	await(t, socket.closed)
	if !s.wire.joined(context.Background()) {
		t.Fatal("native workers should have returned")
	}
	select {
	case <-done:
		t.Fatal("socket join substituted for execution join")
	default:
	}
	close(release)
	finishSession(t, cancel, done)
}
func TestSessionRuntimeErrorIsUnknownAndEnvironmentNeedsMasker(t *testing.T) {
	for _, ready := range []bool{false, true} {
		t.Run(map[bool]string{false: "masker-unbound", true: "runtime-cancel-not-proof"}[ready], func(t *testing.T) {
			calls := make(chan struct{}, 1)
			socket, _, cancel, done := sessionFixture(t, testExecutor{ready: ready, run: func(context.Context, p.Request, func(string, string) error) (p.Response, error) {
				calls <- struct{}{}
				return p.Response{}, context.Canceled
			}})
			socket.in <- helloAck(t)
			r := request(t)
			r.Environment = map[string]string{"TOKEN": "CANARY"}
			socket.in <- correlated(t, r, r.RequestID, r.OperationID)
			response, ok := nextMessage(t, socket).Payload().(p.Response)
			if !ok {
				t.Fatal("missing terminal")
			}
			if ready {
				if response.Outcome != p.Unknown || response.Code != nil || response.SafeMessage == nil || *response.SafeMessage != p.UnknownMessage || len(calls) != 1 {
					t.Fatal("runtime cancel was treated as known no effect")
				}
			} else if response.Outcome != p.Failure || response.Code == nil || *response.Code != p.CapabilityUnavailable || len(calls) != 0 {
				t.Fatal("environment admitted without bound masker")
			}
			finishSession(t, cancel, done)
		})
	}
}
func TestSessionDeadlineIsOriginalAndLateStreamCannotSucceed(t *testing.T) {
	t.Run("deadline-is-original", func(t *testing.T) {
		observed := make(chan time.Time, 1)
		socket, _, cancel, done := sessionFixture(t, testExecutor{ready: true, run: func(ctx context.Context, r p.Request, _ func(string, string) error) (p.Response, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				return p.Response{}, ErrProtocol
			}
			observed <- deadline
			return p.Response{RequestID: r.RequestID, OperationID: r.OperationID, Outcome: p.Success}, nil
		}})
		socket.in <- helloAck(t)
		r := request(t)
		original, _ := p.NewInstant(time.Now().Add(2 * time.Second))
		r.Deadline = &original
		socket.in <- correlated(t, r, r.RequestID, r.OperationID)
		if nextMessage(t, socket).Type() != p.ResponseType {
			t.Fatal("missing original terminal")
		}
		want, _ := original.Time()
		if got := <-observed; !got.Equal(want) {
			t.Fatal("operation deadline was refreshed")
		}
		finishSession(t, cancel, done)
	})
	t.Run("late-stream-after-known-terminal", func(t *testing.T) {
		sinkReady := make(chan func(string, string) error, 1)
		socket, _, cancel, done := sessionFixture(t, testExecutor{ready: true, run: func(ctx context.Context, r p.Request, sink func(string, string) error) (p.Response, error) {
			sinkReady <- sink
			return p.Response{RequestID: r.RequestID, OperationID: r.OperationID, Outcome: p.Success}, nil
		}})
		socket.in <- helloAck(t)
		r := request(t)
		socket.in <- correlated(t, r, r.RequestID, r.OperationID)
		if nextMessage(t, socket).Type() != p.ResponseType {
			t.Fatal("original terminal was not actually written")
		}
		if sink := <-sinkReady; sink("stdout", "late CANARY") == nil {
			t.Fatal("late stream was accepted")
		}
		finishSession(t, cancel, done)
		if len(socket.out) != 0 {
			t.Fatal("late stream escaped terminal seal")
		}
	})
}
func TestSessionNaturalHelloAndHeartbeatDeadlines(t *testing.T) {
	for _, handshake := range []bool{false, true} {
		t.Run(map[bool]string{false: "hello", true: "heartbeat"}[handshake], func(t *testing.T) {
			socket, _, cancel, done := sessionFixture(t, nil)
			defer cancel()
			want, limit := errHelloTimeout, 6*time.Second
			if handshake {
				socket.in <- helloAck(t)
				want, limit = errHeartbeatTimeout, 4*time.Second
			}
			select {
			case err := <-done:
				if !errors.Is(err, want) {
					t.Fatal("wrong original timeout", err)
				}
			case <-time.After(limit):
				t.Fatal("native timer failed to retire connection")
			}
			await(t, socket.closed)
		})
	}
}
