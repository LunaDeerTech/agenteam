package runnerhttp

import (
	"context"
	"fmt"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	"github.com/LunaDeerTech/agenteam/internal/central/runner/service"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"io"
	"log/slog"
	"sync"
)

type SkillPrepareDispatcher struct {
	device  *DeviceHandler
	mounts  *mount.SkillPrepareResolver
	service *service.Service
}
type SkillPrepareDispatchResult struct {
	Attempted bool
	Terminal  *p.Response
}

// The call captures an actual authenticated session and the exact already
// committed Builtin RequestID. It cannot be minted from DTOs or dispatched twice.
type SkillPrepareCall struct{ state *skillPrepareCallState }
type skillPrepareCallState struct {
	mu                             sync.Mutex
	dispatcher                     *SkillPrepareDispatcher
	session                        *controlSession
	ticket                         mount.SkillPrepareTicket
	used, returned, requestsJoined bool
}

func NewSkillPrepareDispatcher(device *DeviceHandler, mounts *mount.SkillPrepareResolver) (*SkillPrepareDispatcher, error) {
	if device == nil || device.registry == nil || mounts == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	core, ok := device.control.(*service.Service)
	if !ok || core == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &SkillPrepareDispatcher{device: device, mounts: mounts, service: core}, nil
}
func (d *SkillPrepareDispatcher) Prepare(ticket mount.SkillPrepareTicket) (*SkillPrepareCall, error) {
	if d == nil || d.device == nil || d.service == nil || ticket.RunnerID().Validate() != nil || !ticket.RequestID().Valid() {
		return nil, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	d.device.registry.mu.Lock()
	s := d.device.registry.sessions[p.ID(ticket.RunnerID().String())]
	d.device.registry.mu.Unlock()
	if s == nil {
		return nil, f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	s.mu.Lock()
	available := s.active && !s.closed && s.skillPrepare
	s.mu.Unlock()
	if !available {
		return nil, f.NewFault(f.CapabilityUnsupported, f.NotStarted)
	}
	return &SkillPrepareCall{state: &skillPrepareCallState{dispatcher: d, session: s, ticket: ticket}}, nil
}
func (*SkillPrepareCall) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "skill_prepare_call") }
func (*SkillPrepareCall) MarshalJSON() ([]byte, error) { return []byte(`"skill_prepare_call"`), nil }
func (*SkillPrepareCall) LogValue() slog.Value         { return slog.StringValue("skill_prepare_call") }
func (c *SkillPrepareCall) RequiredLocks() ([]f.LockRequest, error) {
	if c == nil || c.state == nil || c.state.dispatcher == nil || c.state.session == nil {
		return nil, f.NewFault(f.Forbidden, f.NotStarted)
	}
	return c.state.dispatcher.service.SkillPrepareConnectionLocks(c.state.session.connection)
}
func (c *SkillPrepareCall) RequireCurrentInTx(ctx context.Context, tx f.Tx) (f.Version, error) {
	if c == nil || c.state == nil || c.state.dispatcher == nil || c.state.session == nil {
		return 0, f.NewFault(f.Forbidden, f.NotStarted)
	}
	c.state.session.mu.Lock()
	available := c.state.session.active && !c.state.session.closed && c.state.session.skillPrepare
	c.state.session.mu.Unlock()
	if !available {
		return 0, f.NewFault(f.CapabilityUnsupported, f.NotStarted)
	}
	return c.state.dispatcher.service.RequireSkillPrepareConnectionInTx(ctx, tx, c.state.session.connection)
}

// Retirement proves only this exact call's handler returned (or no frame was
// attempted). Connection loss by itself is never proof that remote HTTP joined.
func (c *SkillPrepareCall) Retirement() bool {
	if c == nil || c.state == nil {
		return false
	}
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	return c.state.used && c.state.returned && c.state.requestsJoined
}
func (c *SkillPrepareCall) Dispatch(ctx context.Context, payload p.SkillPreparePayload) (out SkillPrepareDispatchResult, err error) {
	if c == nil || c.state == nil || c.state.dispatcher == nil || c.state.session == nil || ctx == nil {
		return out, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	c.state.mu.Lock()
	if c.state.used {
		c.state.mu.Unlock()
		return out, f.NewFault(f.InvalidState, f.NotStarted)
	}
	c.state.used = true
	c.state.mu.Unlock()
	defer func() {
		c.state.mu.Lock()
		c.state.returned = true
		c.state.requestsJoined = !out.Attempted || out.Terminal != nil
		c.state.mu.Unlock()
	}()
	request, err := c.state.dispatcher.mounts.Claim(ctx, c.state.ticket, payload)
	if err != nil {
		return out, err
	}
	defer clear(request.Payload)
	if request.RequestID != c.state.ticket.RequestID() {
		return out, f.NewFault(f.InvalidState, f.NotStarted)
	}
	if deadline, ok := ctx.Deadline(); ok {
		at, e := p.NewInstant(deadline)
		if e != nil {
			return out, f.NewFault(f.InvalidArgument, f.NotStarted)
		}
		request.Deadline = &at
	}
	result, err := c.state.session.dispatchSkillPrepare(ctx, request)
	return SkillPrepareDispatchResult{Attempted: result.attempted, Terminal: result.terminal}, err
}

func (s *controlSession) dispatchSkillPrepare(ctx context.Context, input p.Request) (controlResult, error) {
	if ctx == nil || ctx.Err() != nil {
		return controlResult{}, errControlClosed
	}
	if !input.RequestID.Valid() || input.OperationName != p.SkillPrepareOperation || input.OperationRevision != p.SkillPrepareRevision || input.Environment != nil || input.IdempotencyKey != nil {
		return controlResult{}, errControlProtocol
	}
	arguments, err := p.DecodeSkillPreparePayload(input.Payload)
	if err != nil {
		return controlResult{}, errControlProtocol
	}
	defer func() { arguments.Transfer = p.SkillTransfer{} }()
	defer clear(input.Payload)
	message, err := controlMessage(input, input.RequestID, input.OperationID)
	if err != nil {
		return controlResult{}, errControlProtocol
	}
	input = message.Payload().(p.Request)
	defer clear(input.Payload)
	call := &controlCall{request: input, ctx: ctx, sequences: make(map[string]uint64), done: make(chan struct{})}
	s.mu.Lock()
	if !s.active || s.closed || !s.skillPrepare {
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
		if call.cancelled || call.closed || ctx.Err() != nil || !s.skillPrepare {
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
			value, e := p.DecodeSkillPrepareResult(result.terminal.Payload)
			if e != nil || value.SkillID != arguments.SkillID || value.RevisionID != arguments.RevisionID || value.Revision != arguments.Revision || value.TransferID != arguments.Transfer.ID || value.ArchiveSHA256 != arguments.ArchiveSHA256 || !value.Durable {
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

// CloseUnsent closes the one-shot admission before any Dispatch. A running or
// already attempted call cannot be converted into an unsent retirement proof.
func (c *SkillPrepareCall) CloseUnsent() error {
	if c == nil || c.state == nil {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	if c.state.used {
		if c.state.returned && c.state.requestsJoined {
			return nil
		}
		if c.state.returned {
			return f.NewFault(f.CommitUnknown, f.Unknown)
		}
		return f.NewFault(f.ResourceBusy, f.NotStarted)
	}
	c.state.used, c.state.returned, c.state.requestsJoined = true, true, true
	return nil
}
