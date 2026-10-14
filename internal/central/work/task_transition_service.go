package work

import (
	"context"
	"sync"

	agentc "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type TaskTransitionPending interface {
	ec.PendingDispatchReader
	ec.PendingClaimGroupGuard
}

type TaskTransitionDependencies struct {
	AgentTools          c.AgentTaskAuthority
	AgentRunAgents      agentc.TaskTransitionReferences
	Cancellation        ec.CancellationInTx
	PendingCancellation c.TaskPendingCanceller
	Agents              agentc.WorkReferences
	Occupancy           ec.WorkOccupancyReader
	Pending             TaskTransitionPending
	Structure           *Reader
	TaskEvents          c.TaskTransitionEvents
	Authority           *Authority
	Events              oc.Appender
	Activity            ActivityAuthority
}
type TaskTransitionService struct {
	data func() *taskTransitionServiceState
}
type taskTransitionServiceState struct {
	ownership *projectCallOwnershipState
	store     Store
	deps      TaskTransitionDependencies
	mu        sync.Mutex
	stopped   bool
	calls     map[*call]struct{}
	changed   chan struct{}
}

func NewTaskTransition(store Store, deps TaskTransitionDependencies) (*TaskTransitionService, error) {
	if nilPort(deps.Agents) || nilPort(deps.Occupancy) || nilPort(deps.Pending) || nilPort(store) || deps.Authority.state() == nil || !sameStore(store, deps.Authority.state().store) || nilPort(deps.Events) || nilPort(deps.Activity) || !deps.TaskEvents.Valid() || deps.Structure.state() == nil || !sameStore(store, deps.Structure.state().store) || deps.Structure.state().authority != deps.Authority {
		return nil, fault(f.DependencyUnbound)
	}
	st := &taskTransitionServiceState{store: store, deps: deps, calls: map[*call]struct{}{}, changed: make(chan struct{})}
	deps.Authority.state().transitions = st
	return &TaskTransitionService{data: func() *taskTransitionServiceState { return st }}, nil
}
func (s *TaskTransitionService) state() *taskTransitionServiceState {
	if s == nil || s.data == nil {
		return nil
	}
	return s.data()
}
func (s *TaskTransitionService) begin(ctx context.Context) (context.Context, *call, func(), error) {
	st := s.state()
	if st == nil {
		return nil, nil, nil, fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return nil, nil, nil, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, canceled(err)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.stopped {
		return nil, nil, nil, fault(f.ShuttingDown)
	}
	run, cancel := context.WithCancel(ctx)
	entry := &call{cancel: cancel, confirmations: map[*confirmation]struct{}{}}
	st.calls[entry] = struct{}{}
	var once sync.Once
	done := func() {
		once.Do(func() {
			if st.ownership != nil {
				st.ownership.finish(entry)
			}
			cancel()
			st.mu.Lock()
			delete(st.calls, entry)
			close(st.changed)
			st.changed = make(chan struct{})
			st.mu.Unlock()
			if st.ownership != nil {
				st.ownership.returned(entry)
			}
		})
	}
	return run, entry, done, nil
}
func (s *TaskTransitionService) Stop() {
	st := s.state()
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.stopped = true
	for entry := range st.calls {
		entry.cancel()
		for c := range entry.confirmations {
			c.cancel()
		}
	}
}
func (s *TaskTransitionService) Drain(ctx context.Context) error {
	st := s.state()
	if st == nil {
		return nil
	}
	for {
		st.mu.Lock()
		if len(st.calls) == 0 {
			st.mu.Unlock()
			if st.ownership != nil && !st.ownership.laneJoined(projectCallTransition) {
				return st.ownership.drainReturned(ctx, projectCallTransition)
			}
			return nil
		}
		changed := st.changed
		st.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

var _ c.TaskTransitions = (*TaskTransitionService)(nil)
