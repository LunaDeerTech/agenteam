package runnerhttp

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// ReadFileDispatcher is explicitly constructed by a trusted owner. It adds no
// public HTTP route or default operation registry. Only a ticket issued by the
// exact bound resolver can be consumed; a Request DTO is not an input port.
type ReadFileDispatcher struct {
	device *DeviceHandler
	mounts *mount.ReadFileResolver
}

type ReadFileDispatchResult struct {
	Attempted bool
	Terminal  *p.Response
}

func (ReadFileDispatchResult) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "read_file_dispatch_result")
}
func (ReadFileDispatchResult) MarshalJSON() ([]byte, error) {
	return []byte(`"read_file_dispatch_result"`), nil
}
func (ReadFileDispatchResult) LogValue() slog.Value {
	return slog.StringValue("read_file_dispatch_result")
}

func NewReadFileDispatcher(device *DeviceHandler, mounts *mount.ReadFileResolver) (*ReadFileDispatcher, error) {
	if device == nil || device.registry == nil || device.control == nil || mounts == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &ReadFileDispatcher{device: device, mounts: mounts}, nil
}

func (d *ReadFileDispatcher) Dispatch(ctx context.Context, ticket mount.ReadFileTicket) (ReadFileDispatchResult, error) {
	if d == nil || d.device == nil || d.mounts == nil {
		return ReadFileDispatchResult{}, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if ctx == nil || ticket.RunnerID().Validate() != nil {
		return ReadFileDispatchResult{}, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	d.device.registry.mu.Lock()
	s := d.device.registry.sessions[p.ID(ticket.RunnerID().String())]
	d.device.registry.mu.Unlock()
	if s == nil {
		return ReadFileDispatchResult{}, f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	s.mu.Lock()
	available := s.active && !s.closed && s.readFile
	s.mu.Unlock()
	if !available {
		return ReadFileDispatchResult{}, f.NewFault(f.CapabilityUnsupported, f.NotStarted)
	}
	// The original current-fact transaction fully returns before enqueue/socket
	// work. A committed claim is already in flight; clear affects future claims.
	request, err := d.mounts.Claim(ctx, ticket)
	if err != nil {
		return ReadFileDispatchResult{}, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		at, e := p.NewInstant(deadline)
		if e != nil {
			return ReadFileDispatchResult{}, f.NewFault(f.InvalidArgument, f.NotStarted)
		}
		request.Deadline = &at
	}
	result, err := s.dispatchReadFile(ctx, request)
	return ReadFileDispatchResult{Attempted: result.attempted, Terminal: result.terminal}, err
}

// A read-file caller owns its original call until terminal or actual connection
// retirement. Cancellation does not detach it after merely enqueueing Cancel.
// The ordinary private dispatch behavior is unchanged.
func (s *controlSession) dispatchReadFile(ctx context.Context, input p.Request) (controlResult, error) {
	if ctx == nil || ctx.Err() != nil {
		return controlResult{}, errControlClosed
	}
	requestID, err := p.NewID()
	if err != nil {
		return controlResult{}, errControlTransport
	}
	input.RequestID = requestID
	message, err := controlMessage(input, input.RequestID, input.OperationID)
	if err != nil {
		return controlResult{}, errControlProtocol
	}
	input = message.Payload().(p.Request)
	call := &controlCall{request: input, ctx: ctx, sequences: make(map[string]uint64), done: make(chan struct{})}
	s.mu.Lock()
	if !s.active || s.closed || !s.readFile {
		s.mu.Unlock()
		return controlResult{}, errControlClosed
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
		if call.cancelled || call.closed || ctx.Err() != nil || !s.readFile {
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
			value, e := p.DecodeReadFileResult(result.terminal.Payload)
			args, e2 := p.DecodeReadFilePayload(input.Payload)
			if e != nil || e2 != nil || value.Path != args.Arguments.Path || value.Offset != args.Arguments.Offset || value.Encoding != args.Arguments.Encoding || value.BytesRead > args.Arguments.Length || !value.EOF && value.BytesRead != args.Arguments.Length {
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
