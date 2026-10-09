package control

import (
	"context"
	"strconv"
	"sync"
	"time"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// This private operation port is also the package test seam. The production
// connection binds nil until D16 supplies the frozen, probed operation registry;
// nil always returns UNSUPPORTED_OPERATION, never a successful stub.
type operationExecutor interface {
	execute(context.Context, p.Request, func(string, string) error) (p.Response, error)
	mask(string) (string, error)
	maskingReady() bool
	accepts(p.Request) bool
}
type activeRequest struct {
	mu           sync.Mutex
	cancel       context.CancelFunc
	terminal     bool
	streamFailed bool
	sequences    map[string]uint64
}
type session struct {
	wire     *wire
	state    *sessionState
	hello    p.Hello
	executor operationExecutor
	wake     chan struct{}
	mu       sync.Mutex
	started  bool
	requests map[p.ID]*activeRequest
	workers  sync.WaitGroup
}

func newSession(w *wire, hello p.Hello, executor operationExecutor) (*session, error) {
	// No Data Channel features are implemented by this owner. Production's
	// nil registry must not advertise an operation or a capability.
	if w == nil || len(hello.FeatureFlags) != 0 || executor == nil && len(hello.Capabilities) != 0 {
		return nil, ErrProtocol
	}
	validated, err := newMessage(hello, "", "")
	if err != nil {
		return nil, ErrProtocol
	}
	hello = validated.Payload().(p.Hello)
	return &session{wire: w, hello: hello, executor: executor, wake: make(chan struct{}, 1), requests: make(map[p.ID]*activeRequest)}, nil
}
func newMessage(payload p.Payload, request, operation p.ID) (p.Message, error) {
	id, err := p.NewID()
	if err != nil {
		return p.Message{}, ErrTransport
	}
	at, err := p.NewInstant(time.Now())
	if err != nil {
		return p.Message{}, ErrProtocol
	}
	return p.NewMessage(p.Header{ProtocolVersion: p.CurrentVersion(), MessageID: id, Timestamp: at, RequestID: request, OperationID: operation}, payload)
}
func (s *session) send(payload p.Payload, request, operation p.ID) error {
	message, err := newMessage(payload, request, operation)
	if err != nil {
		return ErrProtocol
	}
	return s.wire.send(message)
}

// run owns the connection timer, native wire and every admitted execution. Stop
// signals cancel them, but the return waits for their actual original callbacks.
// A future process root must use its original Drain/Force context to bound its
// wait for this return; cancellation is never converted into a joined flag.
func (s *session) run(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return ErrProtocol
	}
	s.started = true
	s.mu.Unlock()
	s.state = newSessionState(time.Now())
	owned, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := s.send(s.hello, "", ""); err != nil {
		s.wire.stop()
		return err
	}
	wireDone := make(chan error, 1)
	go func() { wireDone <- s.wire.run(owned, s.receive) }()
	timer := time.NewTimer(time.Until(s.state.nextDeadline()))
	defer timer.Stop()
	var result error
	wireReturned := false
loop:
	for {
		select {
		case result = <-wireDone:
			wireReturned = true
			break loop
		case <-ctx.Done():
			result = ctx.Err()
			break loop
		case <-s.wake:
		case <-timer.C:
			heartbeat, err := s.state.heartbeat(time.Now())
			if err != nil {
				result = err
				break loop
			}
			if heartbeat != nil {
				if err = s.send(*heartbeat, "", ""); err != nil {
					result = err
					break loop
				}
			}
		}
		timer.Reset(time.Until(s.state.nextDeadline()))
	}
	cancel()
	s.wire.stop()
	if !wireReturned {
		<-wireDone
	}
	// The reader has returned, so it can no longer admit or Add workers.
	s.workers.Wait()
	return result
}
func (s *session) receive(ctx context.Context, message p.Message) error {
	if err := s.state.observe(message, time.Now()); err != nil {
		return err
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
	switch payload := message.Payload().(type) {
	case p.HelloAck, p.HeartbeatAck:
		return nil
	case p.ProtocolError:
		if payload.Fatal {
			return ErrProtocol
		}
		return nil
	case p.DataOpen, p.DataClose:
		return s.send(p.ProtocolError{Code: p.UnsupportedMessage, SafeMessage: p.UnsupportedMessage.SafeMessage(), OffendingMessageID: message.Header().MessageID}, "", "")
	case p.Request:
		return s.admit(ctx, payload)
	case p.Cancel:
		pending, err := s.state.cancel(payload)
		if err != nil || !pending {
			return err
		}
		s.mu.Lock()
		request := s.requests[payload.RequestID]
		s.mu.Unlock()
		if request != nil {
			request.cancel()
		}
		return nil
	default:
		return ErrProtocol
	}
}
func (s *session) admit(ctx context.Context, request p.Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.state.admit(request); err != nil {
		return err
	}
	if s.executor == nil || !s.executor.accepts(request) {
		return s.reject(request, p.UnsupportedOperation)
	}
	if len(request.Environment) != 0 && !s.executor.maskingReady() {
		return s.reject(request, p.CapabilityUnavailable)
	}
	owned, cancel := context.WithCancel(ctx)
	if request.Deadline != nil {
		deadline, err := request.Deadline.Time()
		if err != nil {
			cancel()
			return ErrProtocol
		}
		if !time.Now().Before(deadline) {
			cancel()
			return s.reject(request, p.Timeout)
		}
		cancel()
		owned, cancel = context.WithDeadline(ctx, deadline)
	}
	active := &activeRequest{cancel: cancel, sequences: make(map[string]uint64)}
	s.mu.Lock()
	s.requests[request.RequestID] = active
	s.mu.Unlock()
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		defer cancel()
		defer func() {
			s.mu.Lock()
			delete(s.requests, request.RequestID)
			s.mu.Unlock()
		}()
		var response p.Response
		var err error
		if stopped := owned.Err(); stopped != nil {
			// No operation callback was invoked, so this particular terminal
			// can truthfully establish cancellation without a side effect.
			code := p.CancelledCode
			if stopped == context.DeadlineExceeded {
				code = p.Timeout
			}
			safe := code.SafeMessage()
			response = p.Response{RequestID: request.RequestID, OperationID: request.OperationID, Outcome: p.Cancelled, Code: &code, SafeMessage: &safe}
		} else {
			response, err = s.executor.execute(owned, request, func(stream, data string) error {
				return s.stream(owned, active, request, stream, data)
			})
		}
		if err != nil {
			// Runtime error/cancel alone cannot prove whether a side effect
			// happened. Only the Runtime's valid terminal can establish it.
			message := p.UnknownMessage
			response = p.Response{RequestID: request.RequestID, OperationID: request.OperationID, Outcome: p.Unknown, SafeMessage: &message}
		}
		active.mu.Lock()
		defer active.mu.Unlock()
		active.terminal = true
		if active.streamFailed || response.RequestID != request.RequestID || response.OperationID != request.OperationID || s.state.seal(request) != nil {
			s.wire.stop()
			return
		}
		if s.send(response, request.RequestID, request.OperationID) != nil {
			s.wire.stop()
		}
	}()
	return nil
}
func (s *session) reject(request p.Request, code p.Code) error {
	if err := s.state.seal(request); err != nil {
		return err
	}
	safe := code.SafeMessage()
	return s.send(p.Response{RequestID: request.RequestID, OperationID: request.OperationID, Outcome: p.Failure, Code: &code, SafeMessage: &safe}, request.RequestID, request.OperationID)
}
func (s *session) stream(ctx context.Context, active *activeRequest, request p.Request, stream, data string) (result error) {
	active.mu.Lock()
	defer active.mu.Unlock()
	defer func() {
		if result != nil {
			active.streamFailed = true
			s.wire.stop()
		}
	}()
	if active.terminal || ctx.Err() != nil {
		return ErrClosed
	}
	if stream != "stdout" && stream != "stderr" && stream != "progress" {
		return ErrProtocol
	}
	if !s.executor.maskingReady() {
		return ErrProtocol
	}
	masked, err := s.executor.mask(data)
	if err != nil {
		return ErrProtocol
	}
	sequence := active.sequences[stream]
	if sequence == ^uint64(0) {
		return ErrBackpressure
	}
	sequence++
	at, err := p.NewInstant(time.Now())
	if err != nil {
		return ErrProtocol
	}
	// The immutable protocol constructor checks size/UTF-8 and clones data;
	// this lock orders every admitted stream before the original terminal.
	if err = s.send(p.Stream{RequestID: request.RequestID, OperationID: request.OperationID, Stream: stream, Sequence: p.Decimal(strconv.FormatUint(sequence, 10)), Data: masked, Timestamp: at}, request.RequestID, request.OperationID); err != nil {
		return err
	}
	active.sequences[stream] = sequence
	return nil
}
