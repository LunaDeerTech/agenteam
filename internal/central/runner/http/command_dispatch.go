package runnerhttp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// CommandDispatcher is explicitly constructed by a trusted owner. It adds no
// public HTTP route or default operation registry. Only a ticket issued by the
// exact bound resolver can be consumed; a Request DTO is not an input port.
type CommandDispatcher struct {
	device *DeviceHandler
	mounts *mount.CommandResolver
}

// A success wire envelope contains a complete invocation report. It does not
// imply exit zero, business success, rollback or permission for another call.
// Streams are returned only with a verified report and are owned by the caller.
type CommandDispatchResult struct {
	Attempted bool
	Terminal  *p.Response
	Stdout    []byte
	Stderr    []byte
}

// CommandCall fixes the future wire RequestID before the Operation owner
// commits its Attempt. Preparing a call does not claim its ticket, enqueue a
// frame or start background work. Copies share one private consumption state.
type CommandCall struct{ state *commandCallState }

type commandCallState struct {
	mu         sync.Mutex
	used       bool
	dispatcher *CommandDispatcher
	ticket     mount.CommandTicket
	requestID  p.ID
}

func (CommandCall) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "command_call") }
func (CommandCall) MarshalJSON() ([]byte, error) { return []byte(`"command_call"`), nil }
func (CommandCall) LogValue() slog.Value         { return slog.StringValue("command_call") }

func (c *CommandCall) RequestID() p.ID {
	if c == nil || c.state == nil {
		return ""
	}
	return c.state.requestID
}

func (CommandDispatchResult) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "command_dispatch_result")
}
func (CommandDispatchResult) MarshalJSON() ([]byte, error) {
	return []byte(`"command_dispatch_result"`), nil
}
func (CommandDispatchResult) LogValue() slog.Value {
	return slog.StringValue("command_dispatch_result")
}

func NewCommandDispatcher(device *DeviceHandler, mounts *mount.CommandResolver) (*CommandDispatcher, error) {
	if device == nil || device.registry == nil || device.control == nil || mounts == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &CommandDispatcher{device: device, mounts: mounts}, nil
}

func (d *CommandDispatcher) Prepare(ticket mount.CommandTicket) (*CommandCall, error) {
	if d == nil || d.device == nil || d.mounts == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if ticket.RunnerID().Validate() != nil {
		return nil, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	requestID, err := p.NewID()
	if err != nil {
		return nil, f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	return &CommandCall{state: &commandCallState{dispatcher: d, ticket: ticket, requestID: requestID}}, nil
}

// Dispatch consumes this call even if current admission fails. The trusted
// Operation owner may invoke it only after the Attempt containing RequestID()
// has a known committed outcome and its original transaction has returned.
// An unknown outcome must retain that Attempt without dispatching or retrying.
func (c *CommandCall) Dispatch(ctx context.Context) (CommandDispatchResult, error) {
	if c == nil || c.state == nil {
		return CommandDispatchResult{}, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	s := c.state
	s.mu.Lock()
	if s.used {
		s.mu.Unlock()
		return CommandDispatchResult{}, f.NewFault(f.InvalidState, f.NotStarted)
	}
	s.used = true
	s.mu.Unlock()
	return s.dispatcher.dispatch(ctx, s.ticket, s.requestID)
}

func (d *CommandDispatcher) dispatch(ctx context.Context, ticket mount.CommandTicket, requestID p.ID) (CommandDispatchResult, error) {
	if d == nil || d.device == nil || d.mounts == nil {
		return CommandDispatchResult{}, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if ctx == nil || ticket.RunnerID().Validate() != nil || !requestID.Valid() {
		return CommandDispatchResult{}, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	d.device.registry.mu.Lock()
	s := d.device.registry.sessions[p.ID(ticket.RunnerID().String())]
	d.device.registry.mu.Unlock()
	if s == nil {
		return CommandDispatchResult{}, f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	s.mu.Lock()
	available := s.active && !s.closed && s.command
	s.mu.Unlock()
	if !available {
		return CommandDispatchResult{}, f.NewFault(f.CapabilityUnsupported, f.NotStarted)
	}
	// The original current-fact transaction fully returns before enqueue/socket
	// work. A committed claim is already in flight; clear affects future claims.
	request, err := d.mounts.Claim(ctx, ticket, requestID)
	if err != nil {
		return CommandDispatchResult{}, err
	}
	if request.RequestID != requestID {
		return CommandDispatchResult{}, f.NewFault(f.InvalidState, f.NotStarted)
	}
	if deadline, ok := ctx.Deadline(); ok {
		at, e := p.NewInstant(deadline)
		if e != nil {
			return CommandDispatchResult{}, f.NewFault(f.InvalidArgument, f.NotStarted)
		}
		request.Deadline = &at
	}
	return s.dispatchCommand(ctx, request)
}

// commandOutput is private to one authenticated session/call. The session
// mutex protects both streams and the terminal check. It accepts no caller
// callback, has no durable side effects, and never exposes an unverified prefix.
type commandOutput struct {
	stdout, stderr []byte
}

func (o *commandOutput) stream(value p.Stream) error {
	if !utf8.ValidString(value.Data) {
		return controlProtocolFailure{p.CorrelationInvalid}
	}
	var target *[]byte
	switch value.Stream {
	case "stdout":
		target = &o.stdout
	case "stderr":
		target = &o.stderr
	default:
		return controlProtocolFailure{p.CorrelationInvalid}
	}
	if len(value.Data) > p.MaxRunCommandStreamBytes-len(*target) {
		return controlProtocolFailure{p.ResourceExhausted}
	}
	*target = append(*target, value.Data...)
	return nil
}

func (o *commandOutput) response(value p.Response) error {
	if value.Outcome != p.Success {
		// An explicit unknown cannot certify its prefix. A pre-start refusal
		// may contain no report or stream; a started invocation must report it.
		if value.Outcome != p.Unknown && (len(o.stdout) != 0 || len(o.stderr) != 0) {
			return controlProtocolFailure{p.CorrelationInvalid}
		}
		return nil
	}
	if len(value.Payload) > 32<<10 {
		return controlProtocolFailure{p.ResourceExhausted}
	}
	report, err := p.DecodeRunCommandResult(value.Payload)
	if err != nil || report.StdoutBytes != p.Decimal(strconv.Itoa(len(o.stdout))) || report.StderrBytes != p.Decimal(strconv.Itoa(len(o.stderr))) {
		return controlProtocolFailure{p.CorrelationInvalid}
	}
	return nil
}

// Cancellation requests a stop but never discards the remaining original
// streams. This call remains owned through its actual terminal or connection
// retirement and every outgoing frame it enqueued, including Cancel.
func (s *controlSession) dispatchCommand(ctx context.Context, input p.Request) (CommandDispatchResult, error) {
	if ctx == nil || ctx.Err() != nil {
		return CommandDispatchResult{}, errControlClosed
	}
	if !input.RequestID.Valid() || input.OperationName != p.RunCommandOperation || input.OperationRevision != p.RunCommandRevision || input.Environment != nil || input.IdempotencyKey != nil {
		return CommandDispatchResult{}, errControlProtocol
	}
	if _, err := p.DecodeRunCommandPayload(input.Payload); err != nil {
		return CommandDispatchResult{}, errControlProtocol
	}
	message, err := controlMessage(input, input.RequestID, input.OperationID)
	if err != nil {
		return CommandDispatchResult{}, errControlProtocol
	}
	input = message.Payload().(p.Request)
	output := &commandOutput{}
	defer func() { clear(output.stdout); clear(output.stderr) }()
	call := &controlCall{request: input, ctx: ctx, command: output, sequences: make(map[string]uint64), done: make(chan struct{})}
	s.mu.Lock()
	if !s.active || s.closed || !s.command {
		s.mu.Unlock()
		return CommandDispatchResult{}, errControlClosed
	}
	if _, exists := s.pending[input.RequestID]; exists {
		s.mu.Unlock()
		return CommandDispatchResult{}, errControlProtocol
	}
	if _, exists := s.completed[input.RequestID]; exists {
		s.mu.Unlock()
		return CommandDispatchResult{}, errControlProtocol
	}
	if len(s.pending) >= p.MaxPendingRequests || len(s.pending)+len(s.completed) >= p.MaxSeenMessages {
		s.mu.Unlock()
		return CommandDispatchResult{}, errControlCapacity
	}
	s.pending[input.RequestID] = call
	s.mu.Unlock()
	frame, err := s.wire.send(message, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		if call.cancelled || call.closed || ctx.Err() != nil || !s.command {
			delete(s.pending, input.RequestID)
			s.completed[input.RequestID] = struct{}{}
			call.finish()
			return false
		}
		call.attempted = true
		return true
	})
	if err != nil {
		s.mu.Lock()
		delete(s.pending, input.RequestID)
		s.completed[input.RequestID] = struct{}{}
		call.finish()
		s.mu.Unlock()
		return CommandDispatchResult{}, err
	}
	var cancelFrame *controlFrame
	var cancelErr error
	select {
	case <-call.done:
	case <-ctx.Done():
		s.mu.Lock()
		call.cancelled = true
		attempted, terminal := call.attempted, call.terminal != nil
		if !attempted {
			delete(s.pending, input.RequestID)
			s.completed[input.RequestID] = struct{}{}
			call.finish()
		}
		s.mu.Unlock()
		if attempted && !terminal {
			cancelFrame, cancelErr = s.send(p.Cancel{RequestID: input.RequestID, OperationID: input.OperationID, Reason: "caller_cancelled"}, input.RequestID, input.OperationID)
		}
		<-call.done
	}
	writeErr := <-frame.done
	if cancelFrame != nil {
		cancelErr = <-cancelFrame.done
	}
	if writeErr != nil || cancelErr != nil {
		// A concurrently received report cannot erase an original native I/O
		// failure. Wait for the same session owner and both wire workers.
		s.wire.stop()
		<-s.retired
	}
	s.mu.Lock()
	result := call.result()
	var stdout, stderr []byte
	if result.terminal != nil && result.terminal.Outcome == p.Success && writeErr == nil && cancelErr == nil {
		stdout, stderr = bytes.Clone(output.stdout), bytes.Clone(output.stderr)
	}
	s.mu.Unlock()
	if result.terminal != nil && writeErr == nil && cancelErr == nil {
		return CommandDispatchResult{Attempted: result.attempted, Terminal: result.terminal, Stdout: stdout, Stderr: stderr}, nil
	}
	if result.attempted {
		return CommandDispatchResult{Attempted: true}, f.NewFault(f.CommitUnknown, f.Unknown)
	}
	if ctx.Err() != nil {
		return CommandDispatchResult{}, ctx.Err()
	}
	if writeErr != nil {
		return CommandDispatchResult{}, writeErr
	}
	return CommandDispatchResult{}, errControlClosed
}
