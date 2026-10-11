package runnerhttp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// DirectoryDispatcher is explicitly constructed by a trusted owner. It adds no
// public HTTP route or default operation registry. Only a ticket issued by the
// exact bound resolver can be consumed; a Request DTO is not an input port.
type DirectoryDispatcher struct {
	device *DeviceHandler
	mounts *mount.DirectoryResolver
}

type DirectoryDispatchResult struct {
	Attempted bool
	Terminal  *p.Response
}

func (DirectoryDispatchResult) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "directory_dispatch_result")
}
func (DirectoryDispatchResult) MarshalJSON() ([]byte, error) {
	return []byte(`"directory_dispatch_result"`), nil
}
func (DirectoryDispatchResult) LogValue() slog.Value {
	return slog.StringValue("directory_dispatch_result")
}

func NewDirectoryDispatcher(device *DeviceHandler, mounts *mount.DirectoryResolver) (*DirectoryDispatcher, error) {
	if device == nil || device.registry == nil || device.control == nil || mounts == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &DirectoryDispatcher{device: device, mounts: mounts}, nil
}

func (d *DirectoryDispatcher) Dispatch(ctx context.Context, ticket mount.DirectoryTicket) (DirectoryDispatchResult, error) {
	if d == nil || d.device == nil || d.mounts == nil {
		return DirectoryDispatchResult{}, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if ctx == nil || ticket.RunnerID().Validate() != nil {
		return DirectoryDispatchResult{}, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	d.device.registry.mu.Lock()
	s := d.device.registry.sessions[p.ID(ticket.RunnerID().String())]
	d.device.registry.mu.Unlock()
	if s == nil {
		return DirectoryDispatchResult{}, f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	s.mu.Lock()
	available := s.active && !s.closed && s.directoryAvailable(ticket.Operation())
	s.mu.Unlock()
	if !available {
		return DirectoryDispatchResult{}, f.NewFault(f.CapabilityUnsupported, f.NotStarted)
	}
	// The original current-fact transaction fully returns before enqueue/socket
	// work. A committed claim is already in flight; clear affects future claims.
	request, err := d.mounts.Claim(ctx, ticket)
	if err != nil {
		return DirectoryDispatchResult{}, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		at, e := p.NewInstant(deadline)
		if e != nil {
			return DirectoryDispatchResult{}, f.NewFault(f.InvalidArgument, f.NotStarted)
		}
		request.Deadline = &at
	}
	result, err := s.dispatchDirectory(ctx, request)
	return DirectoryDispatchResult{Attempted: result.attempted, Terminal: result.terminal}, err
}

// A directory caller owns its original call until terminal or actual connection
// retirement. Cancellation does not detach it after merely enqueueing Cancel.
// The ordinary private dispatch behavior is unchanged.
func (s *controlSession) dispatchDirectory(ctx context.Context, input p.Request) (controlResult, error) {
	if ctx == nil || ctx.Err() != nil {
		return controlResult{}, errControlClosed
	}
	if input.OperationRevision != p.DirectoryRevision {
		return controlResult{}, errControlProtocol
	}
	if _, err := p.DecodeDirectoryPayload(input.OperationName, input.Payload); err != nil {
		return controlResult{}, errControlProtocol
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
	if !s.active || s.closed || !s.directoryAvailable(input.OperationName) {
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
		if call.cancelled || call.closed || ctx.Err() != nil || !s.directoryAvailable(input.OperationName) {
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
			if !validDirectoryResponse(input, result.terminal.Payload) {
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

// directoryAvailable is called only while the original session lock is held.
func (s *controlSession) directoryAvailable(operation string) bool {
	switch operation {
	case p.ListDirectoryOperation:
		return s.listDirectory
	case p.FindFilesOperation:
		return s.findFiles
	default:
		return false
	}
}

func validDirectoryResponse(request p.Request, raw []byte) bool {
	args, err := p.DecodeDirectoryPayload(request.OperationName, request.Payload)
	if err != nil {
		return false
	}
	switch request.OperationName {
	case p.ListDirectoryOperation:
		value, err := p.DecodeListDirectoryResult(raw)
		return err == nil && value.Path == args.Arguments.Path && len(value.Entries) <= args.Arguments.Limit && (!value.Truncated || len(value.Entries) == args.Arguments.Limit)
	case p.FindFilesOperation:
		value, err := p.DecodeFindFilesResult(raw)
		if err != nil || value.Path != args.Arguments.Path || len(value.Paths) > args.Arguments.Limit || value.Truncated && len(value.Paths) != args.Arguments.Limit {
			return false
		}
		for _, candidate := range value.Paths {
			if args.Arguments.Path != "." && !strings.HasPrefix(candidate, args.Arguments.Path+"/") {
				return false
			}
			subject := candidate
			if args.Arguments.Match == "name" {
				subject = path.Base(candidate)
			}
			matched, err := path.Match(args.Arguments.Pattern, subject)
			if err != nil || !matched {
				return false
			}
		}
		return true
	default:
		return false
	}
}
