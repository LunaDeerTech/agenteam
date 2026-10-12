package runnerhttp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// WriteFileDispatcher is explicitly constructed by a trusted owner. It adds no
// public HTTP route or default operation registry. Only a ticket issued by the
// exact bound resolver can be consumed; a Request DTO is not an input port.
type WriteFileDispatcher struct {
	device *DeviceHandler
	mounts *mount.WriteFileResolver
}

type WriteFileDispatchResult struct {
	Attempted bool
	Terminal  *p.Response
}

// WriteFileCall fixes the future wire RequestID before the Operation owner
// commits its Attempt. Preparing a call does not claim its ticket, enqueue a
// frame or start background work. Copies share one private consumption state.
type WriteFileCall struct{ state *writeFileCallState }

type writeFileCallState struct {
	mu         sync.Mutex
	used       bool
	dispatcher *WriteFileDispatcher
	ticket     mount.WriteFileTicket
	requestID  p.ID
}

func (WriteFileCall) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "write_file_call") }
func (WriteFileCall) MarshalJSON() ([]byte, error) { return []byte(`"write_file_call"`), nil }
func (WriteFileCall) LogValue() slog.Value         { return slog.StringValue("write_file_call") }

func (c *WriteFileCall) RequestID() p.ID {
	if c == nil || c.state == nil {
		return ""
	}
	return c.state.requestID
}

func (WriteFileDispatchResult) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "write_file_dispatch_result")
}
func (WriteFileDispatchResult) MarshalJSON() ([]byte, error) {
	return []byte(`"write_file_dispatch_result"`), nil
}
func (WriteFileDispatchResult) LogValue() slog.Value {
	return slog.StringValue("write_file_dispatch_result")
}

func NewWriteFileDispatcher(device *DeviceHandler, mounts *mount.WriteFileResolver) (*WriteFileDispatcher, error) {
	if device == nil || device.registry == nil || device.control == nil || mounts == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &WriteFileDispatcher{device: device, mounts: mounts}, nil
}

func (d *WriteFileDispatcher) Prepare(ticket mount.WriteFileTicket) (*WriteFileCall, error) {
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
	return &WriteFileCall{state: &writeFileCallState{dispatcher: d, ticket: ticket, requestID: requestID}}, nil
}

// Dispatch consumes this call even if current admission fails. The trusted
// Operation owner may invoke it only after the Attempt containing RequestID()
// has a known committed outcome and its original transaction has returned.
// An unknown outcome must retain that Attempt without dispatching or retrying.
func (c *WriteFileCall) Dispatch(ctx context.Context) (WriteFileDispatchResult, error) {
	if c == nil || c.state == nil {
		return WriteFileDispatchResult{}, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	s := c.state
	s.mu.Lock()
	if s.used {
		s.mu.Unlock()
		return WriteFileDispatchResult{}, f.NewFault(f.InvalidState, f.NotStarted)
	}
	s.used = true
	s.mu.Unlock()
	return s.dispatcher.dispatch(ctx, s.ticket, s.requestID)
}

func (d *WriteFileDispatcher) dispatch(ctx context.Context, ticket mount.WriteFileTicket, requestID p.ID) (WriteFileDispatchResult, error) {
	if d == nil || d.device == nil || d.mounts == nil {
		return WriteFileDispatchResult{}, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if ctx == nil || ticket.RunnerID().Validate() != nil || !requestID.Valid() {
		return WriteFileDispatchResult{}, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	d.device.registry.mu.Lock()
	s := d.device.registry.sessions[p.ID(ticket.RunnerID().String())]
	d.device.registry.mu.Unlock()
	if s == nil {
		return WriteFileDispatchResult{}, f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	s.mu.Lock()
	available := s.active && !s.closed && s.writeFile
	s.mu.Unlock()
	if !available {
		return WriteFileDispatchResult{}, f.NewFault(f.CapabilityUnsupported, f.NotStarted)
	}
	// The original current-fact transaction fully returns before enqueue/socket
	// work. A committed claim is already in flight; clear affects future claims.
	request, err := d.mounts.Claim(ctx, ticket, requestID)
	if err != nil {
		return WriteFileDispatchResult{}, err
	}
	if request.RequestID != requestID {
		return WriteFileDispatchResult{}, f.NewFault(f.InvalidState, f.NotStarted)
	}
	if deadline, ok := ctx.Deadline(); ok {
		at, e := p.NewInstant(deadline)
		if e != nil {
			return WriteFileDispatchResult{}, f.NewFault(f.InvalidArgument, f.NotStarted)
		}
		request.Deadline = &at
	}
	result, err := s.dispatchWriteFile(ctx, request)
	return WriteFileDispatchResult{Attempted: result.attempted, Terminal: result.terminal}, err
}

// A write-file caller owns its original call until terminal or actual connection
// retirement. Cancellation does not detach it after merely enqueueing Cancel.
// The ordinary private dispatch behavior is unchanged.
func (s *controlSession) dispatchWriteFile(ctx context.Context, input p.Request) (controlResult, error) {
	if ctx == nil || ctx.Err() != nil {
		return controlResult{}, errControlClosed
	}
	if !input.RequestID.Valid() || input.OperationName != p.WriteFileOperation || input.OperationRevision != p.WriteFileRevision || input.Environment != nil || input.IdempotencyKey != nil {
		return controlResult{}, errControlProtocol
	}
	arguments, err := p.DecodeWriteFilePayload(input.Payload)
	if err != nil {
		return controlResult{}, errControlProtocol
	}
	defer clear(arguments.Arguments.Data)
	message, err := controlMessage(input, input.RequestID, input.OperationID)
	if err != nil {
		return controlResult{}, errControlProtocol
	}
	input = message.Payload().(p.Request)
	call := &controlCall{request: input, ctx: ctx, sequences: make(map[string]uint64), done: make(chan struct{})}
	s.mu.Lock()
	if !s.active || s.closed || !s.writeFile {
		s.mu.Unlock()
		return controlResult{}, errControlClosed
	}
	if _, exists := s.pending[input.RequestID]; exists {
		s.mu.Unlock()
		return controlResult{}, errControlProtocol
	}
	if _, exists := s.completed[input.RequestID]; exists {
		s.mu.Unlock()
		return controlResult{}, errControlProtocol
	}
	if len(s.pending) >= p.MaxPendingRequests || len(s.pending)+len(s.completed) >= p.MaxSeenMessages {
		s.mu.Unlock()
		return controlResult{}, errControlCapacity
	}
	s.pending[input.RequestID] = call
	s.mu.Unlock()
	frame, err := s.wire.send(message, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		if call.cancelled || call.closed || ctx.Err() != nil || !s.writeFile {
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
		return controlResult{}, err
	}
	select {
	case <-call.done:
	case <-ctx.Done():
		s.mu.Lock()
		call.cancelled = true
		attempted := call.attempted
		if !attempted {
			delete(s.pending, input.RequestID)
			s.completed[input.RequestID] = struct{}{}
			call.finish()
		}
		s.mu.Unlock()
		if attempted {
			_, _ = s.send(p.Cancel{RequestID: input.RequestID, OperationID: input.OperationID, Reason: "caller_cancelled"}, input.RequestID, input.OperationID)
		}
		<-call.done
	}
	// The original frame remains owned until its actual write or skipped write
	// returns, even if a very fast terminal arrived concurrently.
	writeErr := <-frame.done
	s.mu.Lock()
	result := call.result()
	s.mu.Unlock()
	if result.terminal != nil {
		if result.terminal.Outcome == p.Success {
			value, e := p.DecodeWriteFileResult(result.terminal.Payload)
			if e != nil || value.Path != arguments.Arguments.Path || value.BytesWritten != len(arguments.Arguments.Data) {
				return controlResult{attempted: true}, errControlProtocol
			}
		}
		return result, nil
	}
	if result.attempted {
		return result, f.NewFault(f.CommitUnknown, f.Unknown)
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if writeErr != nil {
		return result, writeErr
	}
	return result, errControlClosed
}
