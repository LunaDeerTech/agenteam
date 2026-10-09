package runnerhttp

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/runner/service"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type controlAuthority interface {
	CurrentConnection(context.Context, service.Connection) error
	WithCurrentConnection(context.Context, service.Connection, func(context.Context) error) error
	AcceptHello(context.Context, service.Connection, p.Hello) (p.HelloAck, error)
	Heartbeat(context.Context, service.Connection, p.Heartbeat) error
	UpdateStatus(context.Context, service.Connection, p.RunnerStatus) error
}

type controlSession struct {
	wire       *controlWire
	authority  controlAuthority
	connection service.Connection
	runner     p.ID
	mu         sync.Mutex
	started    bool
	active     bool
	closed     bool
	deadline   time.Time
	sequence   uint64
	seen       map[p.ID]struct{}
	pending    map[p.ID]*controlCall
	completed  map[p.ID]struct{}
	wake       chan struct{}
}

type controlCall struct {
	request   p.Request
	ctx       context.Context
	stream    func(context.Context, p.Stream) error
	attempted bool
	cancelled bool
	terminal  *p.Response
	sequences map[string]uint64
	done      chan struct{}
	closed    bool
}

// Dispatch remains a private endpoint seam until the declared Mount/Execution
// authority issues a verified ticket. No HTTP endpoint accepts an RPC DTO.
type controlResult struct {
	attempted bool
	terminal  *p.Response
}

func newControlSession(w *controlWire, a controlAuthority, c service.Connection, runner p.ID) *controlSession {
	return &controlSession{wire: w, authority: a, connection: c, runner: runner, deadline: time.Now().Add(5 * time.Second), seen: make(map[p.ID]struct{}), pending: make(map[p.ID]*controlCall), completed: make(map[p.ID]struct{}), wake: make(chan struct{}, 1)}
}

func (s *controlSession) nextDeadline() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deadline
}

func (s *controlSession) run(ctx context.Context) error {
	s.mu.Lock()
	if s.started || s.wire == nil || s.authority == nil || !s.runner.Valid() {
		s.mu.Unlock()
		return errControlProtocol
	}
	s.started = true
	s.mu.Unlock()
	owned, cancel := context.WithCancel(ctx)
	defer cancel()
	defer s.finishCalls()
	wireDone, observerDone := make(chan error, 1), make(chan error, 1)
	go func() { wireDone <- s.wire.run(owned, s.receive) }()
	go func() { observerDone <- s.observeGeneration(owned) }()
	timer := time.NewTimer(time.Until(s.nextDeadline()))
	defer timer.Stop()
	wireReturned, observerReturned := false, false
	var e error
loop:
	for {
		select {
		case e = <-wireDone:
			wireReturned = true
			break loop
		case e = <-observerDone:
			observerReturned = true
			break loop
		case <-ctx.Done():
			e = ctx.Err()
			break loop
		case <-s.wake:
		case <-timer.C:
			if !time.Now().Before(s.nextDeadline()) {
				e = errControlTransport
				break loop
			}
		}
		timer.Reset(time.Until(s.nextDeadline()))
	}
	cancel()
	s.wire.stop()
	if !wireReturned {
		<-wireDone
	}
	if !observerReturned {
		<-observerDone
	}
	return e
}

func (s *controlSession) observeGeneration(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if e := s.authority.CurrentConnection(ctx, s.connection); e != nil {
				return e
			}
		}
	}
}

func (s *controlSession) observeMessage(message p.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errControlClosed
	}
	header := message.Header()
	if _, exists := s.seen[header.MessageID]; exists {
		return controlProtocolFailure{p.DuplicateMessage}
	}
	if len(s.seen) >= p.MaxSeenMessages {
		return controlProtocolFailure{p.ResourceExhausted}
	}
	if !s.active && message.Type() != p.HelloType || s.active && message.Type() == p.HelloType {
		return controlProtocolFailure{p.HelloOrder}
	}
	if s.active && header.ProtocolVersion != p.CurrentVersion() {
		return controlProtocolFailure{p.IncompatibleVersion}
	}
	s.seen[header.MessageID] = struct{}{}
	return nil
}

func (s *controlSession) send(payload p.Payload, request, operation p.ID) (*controlFrame, error) {
	message, e := controlMessage(payload, request, operation)
	if e != nil {
		return nil, e
	}
	return s.wire.send(message, nil)
}

func (s *controlSession) receive(ctx context.Context, message p.Message) error {
	if e := s.observeMessage(message); e != nil {
		return e
	}
	switch value := message.Payload().(type) {
	case p.Hello:
		if value.RunnerID != s.runner {
			return controlProtocolFailure{p.HelloOrder}
		}
		ack, e := s.authority.AcceptHello(ctx, s.connection, value)
		if e != nil {
			return e
		}
		frame, e := s.send(ack, "", "")
		if e != nil {
			return e
		}
		select {
		case e = <-frame.done:
			if e != nil {
				return e
			}
		case <-ctx.Done():
			return ctx.Err()
		}
		if !ack.Accepted {
			return errControlProtocol
		}
		s.mu.Lock()
		s.active = true
		s.deadline = time.Now().Add(time.Duration(ack.HeartbeatTimeoutMS) * time.Millisecond)
		s.mu.Unlock()
		select {
		case s.wake <- struct{}{}:
		default:
		}
		return nil
	case p.Heartbeat:
		sequence, e := strconv.ParseUint(string(value.Sequence), 10, 64)
		s.mu.Lock()
		valid := e == nil && sequence != 0 && s.sequence != ^uint64(0) && sequence == s.sequence+1
		s.mu.Unlock()
		if !valid {
			return controlProtocolFailure{p.CorrelationInvalid}
		}
		if e := s.authority.Heartbeat(ctx, s.connection, value); e != nil {
			return e
		}
		s.mu.Lock()
		s.sequence = sequence
		s.deadline = time.Now().Add(30 * time.Second)
		s.mu.Unlock()
		select {
		case s.wake <- struct{}{}:
		default:
		}
		_, e = s.send(p.HeartbeatAck{Sequence: value.Sequence}, "", "")
		return e
	case p.RunnerStatus:
		return s.authority.UpdateStatus(ctx, s.connection, value)
	case p.Response:
		return s.authority.WithCurrentConnection(ctx, s.connection, func(context.Context) error { return s.response(value) })
	case p.Stream:
		return s.authority.WithCurrentConnection(ctx, s.connection, func(gated context.Context) error { return s.stream(gated, value) })
	case p.DataReady, p.DataClose:
		if e := s.authority.CurrentConnection(ctx, s.connection); e != nil {
			return e
		}
		_, e := s.send(p.ProtocolError{Code: p.UnsupportedMessage, SafeMessage: p.UnsupportedMessage.SafeMessage(), OffendingMessageID: message.Header().MessageID}, "", "")
		return e
	case p.ProtocolError:
		if e := s.authority.CurrentConnection(ctx, s.connection); e != nil {
			return e
		}
		if value.Fatal {
			return errControlProtocol
		}
		return nil
	default:
		return controlProtocolFailure{p.UnsupportedMessage}
	}
}

func (s *controlSession) finishCalls() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for _, call := range s.pending {
		call.finish()
	}
	clear(s.pending)
}
func (c *controlCall) finish() {
	if !c.closed {
		c.closed = true
		close(c.done)
	}
}
func (c *controlCall) result() controlResult {
	return controlResult{attempted: c.attempted, terminal: c.terminal}
}

func (s *controlSession) dispatch(ctx context.Context, input p.Request, stream func(context.Context, p.Stream) error) (controlResult, error) {
	if ctx == nil || ctx.Err() != nil {
		return controlResult{}, errControlClosed
	}
	requestID, e := p.NewID()
	if e != nil {
		return controlResult{}, errControlTransport
	}
	input.RequestID = requestID
	message, e := controlMessage(input, input.RequestID, input.OperationID)
	if e != nil {
		return controlResult{}, errControlProtocol
	}
	input = message.Payload().(p.Request)
	call := &controlCall{request: input, ctx: ctx, stream: stream, sequences: make(map[string]uint64), done: make(chan struct{})}
	s.mu.Lock()
	if !s.active || s.closed {
		s.mu.Unlock()
		return controlResult{}, errControlClosed
	}
	if len(s.pending) >= p.MaxPendingRequests || len(s.pending)+len(s.completed) >= p.MaxSeenMessages {
		s.mu.Unlock()
		return controlResult{}, errControlCapacity
	}
	s.pending[input.RequestID] = call
	s.mu.Unlock()
	_, e = s.wire.send(message, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		if call.cancelled || call.closed || ctx.Err() != nil {
			return false
		}
		call.attempted = true
		return true
	})
	if e != nil {
		s.mu.Lock()
		delete(s.pending, input.RequestID)
		s.completed[input.RequestID] = struct{}{}
		call.finish()
		s.mu.Unlock()
		return controlResult{}, e
	}
	select {
	case <-call.done:
		s.mu.Lock()
		result := call.result()
		s.mu.Unlock()
		return result, nil
	case <-ctx.Done():
		s.mu.Lock()
		result := call.result()
		if result.terminal != nil {
			s.mu.Unlock()
			return result, nil
		}
		call.cancelled = true
		if !call.attempted {
			delete(s.pending, input.RequestID)
			s.completed[input.RequestID] = struct{}{}
			call.finish()
		}
		s.mu.Unlock()
		if result.attempted {
			_, _ = s.send(p.Cancel{RequestID: input.RequestID, OperationID: input.OperationID, Reason: "caller_cancelled"}, input.RequestID, input.OperationID)
		}
		return result, ctx.Err()
	}
}

func (s *controlSession) response(value p.Response) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	call := s.pending[value.RequestID]
	if call == nil || !call.attempted || call.terminal != nil || call.request.OperationID != value.OperationID {
		return controlProtocolFailure{p.CorrelationInvalid}
	}
	call.terminal = &value
	delete(s.pending, value.RequestID)
	s.completed[value.RequestID] = struct{}{}
	call.finish()
	return nil
}

func (s *controlSession) stream(ctx context.Context, value p.Stream) error {
	s.mu.Lock()
	call := s.pending[value.RequestID]
	sequence, e := strconv.ParseUint(string(value.Sequence), 10, 64)
	if call == nil || !call.attempted || call.terminal != nil || call.request.OperationID != value.OperationID || call.stream == nil || e != nil || sequence == 0 || call.sequences[value.Stream] == ^uint64(0) || sequence != call.sequences[value.Stream]+1 {
		s.mu.Unlock()
		return controlProtocolFailure{p.CorrelationInvalid}
	}
	call.sequences[value.Stream] = sequence
	s.mu.Unlock()
	if e := call.ctx.Err(); e != nil {
		return e
	}
	return call.stream(ctx, value)
}
