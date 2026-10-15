// Package memory owns Agent-scoped long term memory, current revisions and
// explicit retain/recall/reflect operations. Execution completion is not a
// Memory input and cannot cause an implicit write.
package memory

import (
	"context"
	"sync"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// ToolAuthority is implemented by the original BuiltinAuthority. It requires
// that exact executor's private live witness, not merely a valid Actor DTO.
type ToolAuthority interface {
	DiscoverMemoryTool(context.Context, i.Actor, i.ProjectID, i.AgentID, tc.OperationID, string) ([]f.LockRequest, error)
	RequireMemoryToolInTx(context.Context, f.Tx, i.Actor, i.ProjectID, i.AgentID, tc.OperationID, string) error
	RequireAgentRunBindingInTx(context.Context, f.Tx, any) error
}

// AuditEvent intentionally excludes Memory text and raw input. App binds this
// port to the shared Audit domain; mutation and Audit must commit together.
type AuditEvent struct {
	Actor                         i.Actor
	Namespace                     c.Namespace
	Action                        string
	OperationID                   string
	CauseRef                      string
	MemoryID                      string
	Gate                          string
	Added, Updated, Deleted, Noop int
}
type AuditWriter interface {
	AppendMemoryInTx(context.Context, f.Tx, AuditEvent) error
}
type Dependencies struct {
	Projects pc.ProjectAuthority
	Agents   ac.WorkReferences
	Tools    ToolAuthority
	Audit    AuditWriter
	Gate     SensitiveGate
}
type Service struct {
	store   Store
	deps    Dependencies
	mu      sync.Mutex
	stopped bool
	calls   map[*call]struct{}
	changed chan struct{}
}
type call struct {
	service   *Service
	actor     i.Actor
	namespace c.Namespace
	operation tc.OperationID
	intent    string
	locks     []f.LockRequest
	cancel    context.CancelFunc
}
type callKey struct{}

func New(store Store, deps Dependencies) (*Service, error) {
	for _, p := range []any{store, deps.Projects, deps.Agents, deps.Tools, deps.Audit, deps.Gate} {
		if nilPort(p) {
			return nil, fault(f.DependencyUnbound)
		}
	}
	return &Service{store: store, deps: deps, calls: map[*call]struct{}{}, changed: make(chan struct{})}, nil
}
func (s *Service) begin(ctx context.Context, actor i.Actor, n c.Namespace, operation tc.OperationID, intent string) (context.Context, *call, error) {
	if err := checkContext(ctx); err != nil {
		return nil, nil, err
	}
	if s == nil {
		return nil, nil, fault(f.DependencyUnbound)
	}
	if actor.Validate() != nil {
		return nil, nil, fault(f.Unauthenticated)
	}
	if n.Validate() != nil {
		return nil, nil, fault(f.InvalidArgument)
	}
	d := actor.Details()
	var locks []f.LockRequest
	if d.Kind == i.AgentRun {
		if d.ProjectID != n.ProjectID.String() || d.AgentID != n.AgentID.String() || operation.Validate() != nil {
			return nil, nil, fault(f.Forbidden)
		}
		var err error
		locks, err = s.deps.Tools.DiscoverMemoryTool(ctx, actor, n.ProjectID, n.AgentID, operation, intent)
		if err != nil {
			return nil, nil, portError(err)
		}
	} else if d.Kind == i.Human {
		if intent != "list" && intent != "history" && intent != "delete" && intent != "lookup-delete" {
			return nil, nil, fault(f.Forbidden)
		}
		user, _ := f.UserLock(d.UserID)
		project, _ := f.ProjectLock(n.ProjectID.String())
		schedule, _ := f.ProjectScheduleLock(n.ProjectID.String())
		agent, _ := f.AgentLock(n.AgentID.String())
		locks = []f.LockRequest{{Key: user, Mode: f.Shared}, {Key: project, Mode: f.Shared}, {Key: schedule, Mode: f.Exclusive}, {Key: agent, Mode: f.Shared}}
	} else {
		return nil, nil, fault(f.Forbidden)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil, nil, fault(f.ShuttingDown)
	}
	run, cancel := context.WithCancel(ctx)
	entry := &call{s, actor, n, operation, intent, locks, cancel}
	s.calls[entry] = struct{}{}
	return context.WithValue(run, callKey{}, entry), entry, nil
}
func (s *Service) end(entry *call) {
	if entry == nil {
		return
	}
	entry.cancel()
	s.mu.Lock()
	delete(s.calls, entry)
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
}
func (s *Service) StopAdmission() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.stopped = true
	for call := range s.calls {
		call.cancel()
	}
	s.mu.Unlock()
}
func (s *Service) Drain(ctx context.Context) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if s == nil {
		return fault(f.DependencyUnbound)
	}
	for {
		s.mu.Lock()
		empty := len(s.calls) == 0
		changed := s.changed
		s.mu.Unlock()
		if empty {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return portError(ctx.Err())
		}
	}
}

// Joined reports actual registered call ownership; it never requests cleanup.
func (s *Service) Joined() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls) == 0
}
func (s *Service) RequireBuiltinBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	actual, ok := store.(Store)
	if s == nil || !ok || !sameStore(s.store, actual) {
		return fault(f.DependencyUnbound)
	}
	if _, err := s.store.InTx(tx); err != nil {
		return portError(err)
	}
	s.mu.Lock()
	stopped := s.stopped
	s.mu.Unlock()
	if stopped {
		return fault(f.ShuttingDown)
	}
	return s.deps.Tools.RequireAgentRunBindingInTx(ctx, tx, store)
}
