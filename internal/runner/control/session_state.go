package control

import (
	"errors"
	"math"
	"strconv"
	"sync"
	"time"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

var (
	errHelloTimeout     = errors.New("runner hello timed out")
	errHeartbeatTimeout = errors.New("runner heartbeat timed out")
)

type protocolFailure struct{ code p.ProtocolCode }

func (e protocolFailure) Error() string { return e.code.SafeMessage() }

// All time arguments originate in the owner's local monotonic clock. Wire UTC
// timestamps never extend a handshake, heartbeat or operation deadline.
type sessionState struct {
	mu                    sync.Mutex
	started, lastAck, due time.Time
	active                bool
	interval, timeout     time.Duration
	sent, acknowledged    uint64
	seen                  map[p.ID]struct{}
	requests              map[p.ID]p.ID
	pending               map[p.ID]struct{}
}

func newSessionState(now time.Time) *sessionState {
	return &sessionState{started: now, seen: make(map[p.ID]struct{}), requests: make(map[p.ID]p.ID), pending: make(map[p.ID]struct{})}
}
func (s *sessionState) observe(message p.Message, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	header := message.Header()
	if message.AllowedFrom(p.Central) != nil {
		return protocolFailure{p.UnsupportedMessage}
	}
	if _, exists := s.seen[header.MessageID]; exists {
		return protocolFailure{p.DuplicateMessage}
	}
	if len(s.seen) >= p.MaxSeenMessages {
		return protocolFailure{p.ResourceExhausted}
	}
	s.seen[header.MessageID] = struct{}{}
	if !s.active {
		if !now.Before(s.started.Add(5 * time.Second)) {
			return errHelloTimeout
		}
		ack, ok := message.Payload().(p.HelloAck)
		if !ok {
			return protocolFailure{p.HelloOrder}
		}
		if !ack.Accepted || ack.NegotiatedProtocolVersion != p.CurrentVersion() || len(ack.EnabledFeatures) != 0 {
			return protocolFailure{p.IncompatibleVersion}
		}
		// NewMessage/Decode already checked interval and timeout bounds. The
		// state is private and only consumes validated immutable messages.
		s.interval, s.timeout = time.Duration(ack.HeartbeatIntervalMS)*time.Millisecond, time.Duration(ack.HeartbeatTimeoutMS)*time.Millisecond
		s.active, s.lastAck, s.due = true, now, now.Add(s.interval)
		return nil
	}
	if header.ProtocolVersion != p.CurrentVersion() {
		return protocolFailure{p.IncompatibleVersion}
	}
	if !now.Before(s.lastAck.Add(s.timeout)) {
		return errHeartbeatTimeout
	}
	if message.Type() == p.HelloAckType {
		return protocolFailure{p.HelloOrder}
	}
	if ack, ok := message.Payload().(p.HeartbeatAck); ok {
		sequence, err := strconv.ParseUint(string(ack.Sequence), 10, 64)
		if err != nil || sequence <= s.acknowledged || sequence > s.sent {
			return protocolFailure{p.CorrelationInvalid}
		}
		s.acknowledged, s.lastAck = sequence, now
	}
	return nil
}
func (s *sessionState) nextDeadline() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return s.started.Add(5 * time.Second)
	}
	expiry := s.lastAck.Add(s.timeout)
	if s.due.Before(expiry) {
		return s.due
	}
	return expiry
}
func (s *sessionState) heartbeat(now time.Time) (*p.Heartbeat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		if !now.Before(s.started.Add(5 * time.Second)) {
			return nil, errHelloTimeout
		}
		return nil, nil
	}
	if !now.Before(s.lastAck.Add(s.timeout)) {
		return nil, errHeartbeatTimeout
	}
	if now.Before(s.due) {
		return nil, nil
	}
	if s.sent == math.MaxUint64 {
		return nil, protocolFailure{p.ResourceExhausted}
	}
	at, err := p.NewInstant(now)
	if err != nil {
		return nil, ErrProtocol
	}
	s.sent++
	s.due = now.Add(s.interval)
	return &p.Heartbeat{Sequence: p.Decimal(strconv.FormatUint(s.sent, 10)), RunnerTime: at}, nil
}
func (s *sessionState) admit(request p.Request) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return protocolFailure{p.HelloOrder}
	}
	if _, exists := s.requests[request.RequestID]; exists {
		return protocolFailure{p.DuplicateMessage}
	}
	if len(s.pending) >= p.MaxPendingRequests || len(s.requests) >= p.MaxSeenMessages {
		return protocolFailure{p.ResourceExhausted}
	}
	s.requests[request.RequestID] = request.OperationID
	s.pending[request.RequestID] = struct{}{}
	return nil
}
func (s *sessionState) seal(request p.Request) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, active := s.pending[request.RequestID]; !active || s.requests[request.RequestID] != request.OperationID {
		return protocolFailure{p.CorrelationInvalid}
	}
	delete(s.pending, request.RequestID)
	return nil
}
func (s *sessionState) cancel(request p.Cancel) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	operation, exists := s.requests[request.RequestID]
	if !exists || operation != request.OperationID {
		return false, protocolFailure{p.CorrelationInvalid}
	}
	_, active := s.pending[request.RequestID]
	// A correctly correlated cancel may cross the original terminal in flight.
	// Retain that known terminal; never execute the operation or send it twice.
	return active, nil
}
