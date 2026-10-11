package execution

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// ToolResultConsumer appends one already durable successful result. It does
// not own an executor, invoke Model, change the immutable pending Round, or
// retire its references. Every public call remains owned until its actual
// transaction/reader return; Stop requests cancellation and Drain joins it.
type ToolResultConsumer struct {
	driver  *ToolCallDriver
	results tc.ReadFileResultAuthority
	store   Store
	mu      sync.Mutex
	stopped bool
	active  map[i.ExecutionID]context.CancelFunc
	pending map[i.ExecutionID]*toolResultUnknown
	changed chan struct{}
}

type toolResultUnknown struct {
	candidate *toolResultConsumption
	err       error
}

func NewToolResultConsumer(driver *ToolCallDriver, results tc.ReadFileResultAuthority) (*ToolResultConsumer, error) {
	if driver == nil || driver.driver == nil || driver.driver.state == nil || nilPort(results) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ToolResultConsumer{driver: driver, results: results, store: driver.driver.state.store,
		active: make(map[i.ExecutionID]context.CancelFunc), pending: make(map[i.ExecutionID]*toolResultUnknown), changed: make(chan struct{})}, nil
}

func (s *ToolResultConsumer) enter(ctx context.Context, e i.ExecutionID) (context.Context, func(), error) {
	if s == nil || s.driver == nil || ctx == nil || e.Validate() != nil {
		return nil, nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil, nil, fault(f.InvalidState)
	}
	if s.active[e] != nil {
		return nil, nil, fault(f.ResourceBusy)
	}
	owned, cancel := context.WithCancel(ctx)
	s.active[e] = cancel
	return owned, func() {
		cancel()
		s.mu.Lock()
		delete(s.active, e)
		close(s.changed)
		s.changed = make(chan struct{})
		s.mu.Unlock()
	}, nil
}

func (s *ToolResultConsumer) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.stopped = true
	for _, cancel := range s.active {
		cancel()
	}
	s.mu.Unlock()
}

func (s *ToolResultConsumer) Drain(ctx context.Context) error {
	if s == nil || ctx == nil {
		return invalid()
	}
	s.Stop()
	for {
		s.mu.Lock()
		done, changed := len(s.active) == 0, s.changed
		s.mu.Unlock()
		if done {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (s *ToolResultConsumer) Joined() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped && len(s.active) == 0
}

// A retained Unknown cannot turn another Consume call into a physical retry.
// ResolveUnknown and Lookup use only read transactions and the original
// complete candidate. A new instance can independently read durable facts.
func (s *ToolResultConsumer) Consume(ctx context.Context, e i.ExecutionID) (c.ToolResultConsumptionReceipt, error) {
	var zero c.ToolResultConsumptionReceipt
	ctx, leave, err := s.enter(ctx, e)
	if err != nil {
		return zero, err
	}
	defer leave()
	s.mu.Lock()
	unknown := s.pending[e]
	s.mu.Unlock()
	if unknown != nil {
		return zero, unknown.err
	}
	candidate, found, result, err := s.transactToolResult(ctx, e, true)
	if err != nil {
		return zero, err
	}
	if err = commitError(result); err != nil {
		if result.State() == f.Unknown && candidate != nil {
			s.mu.Lock()
			s.pending[e] = &toolResultUnknown{candidate: candidate, err: err}
			s.mu.Unlock()
		}
		return zero, err
	}
	if !found || candidate == nil {
		return zero, fault(f.InvalidState)
	}
	return candidate.receipt, nil
}

func (s *ToolResultConsumer) Lookup(ctx context.Context, e i.ExecutionID) (c.ToolResultConsumptionReceipt, bool, error) {
	var zero c.ToolResultConsumptionReceipt
	ctx, leave, err := s.enter(ctx, e)
	if err != nil {
		return zero, false, err
	}
	defer leave()
	return s.lookupToolResult(ctx, e)
}

func (s *ToolResultConsumer) ResolveUnknown(ctx context.Context, e i.ExecutionID) (c.ToolResultConsumptionReceipt, error) {
	var zero c.ToolResultConsumptionReceipt
	ctx, leave, err := s.enter(ctx, e)
	if err != nil {
		return zero, err
	}
	defer leave()
	s.mu.Lock()
	unknown := s.pending[e]
	s.mu.Unlock()
	if unknown == nil {
		return zero, fault(f.NotFound)
	}
	receipt, found, err := s.lookupToolResult(ctx, e)
	if err != nil {
		return zero, err
	}
	if !found {
		return zero, unknown.err
	}
	return receipt, nil
}

func (s *ToolResultConsumer) lookupToolResult(ctx context.Context, e i.ExecutionID) (c.ToolResultConsumptionReceipt, bool, error) {
	var zero c.ToolResultConsumptionReceipt
	candidate, found, result, err := s.transactToolResult(ctx, e, false)
	if err != nil {
		return zero, false, err
	}
	if err = commitError(result); err != nil {
		return zero, false, err
	}
	if !found {
		return zero, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if original := s.pending[e]; original != nil {
		if !sameToolResultConsumption(candidate, original.candidate) {
			return zero, false, fault(f.ConfirmationStale)
		}
		delete(s.pending, e)
	}
	return candidate.receipt, true, nil
}

func (*ToolResultConsumer) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_tool_result_consumer")
}
func (*ToolResultConsumer) LogValue() slog.Value {
	return slog.StringValue("execution_tool_result_consumer")
}
func (*ToolResultConsumer) MarshalJSON() ([]byte, error) {
	return []byte(`"execution_tool_result_consumer"`), nil
}
