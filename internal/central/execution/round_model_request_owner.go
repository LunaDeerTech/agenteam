package execution

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"sync"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

var _ c.RoundModelRequests = (*RoundModelRequestBinder)(nil)

// RoundModelRequestBinder owns synchronous request persistence and observation.
// There is deliberately no Model, credential reader, Runner or worker here.
type RoundModelRequestBinder struct {
	store     Store
	authority *Authority
	projects  c.PreparationProjectGate
	mu        sync.Mutex
	stopped   bool
	active    map[i.ExecutionID]context.CancelFunc
	pending   map[i.ExecutionID]*roundModelRequestUnknown
	changed   chan struct{}
}

type roundModelRequestUnknown struct {
	round     c.RoundID
	call      mc.CallID
	candidate c.NextRoundModelRequest
	err       error
}

func NewRoundModelRequestBinder(store Store, authority *Authority, projects c.PreparationProjectGate) (*RoundModelRequestBinder, error) {
	if nilPort(store) || !reflect.TypeOf(store).Comparable() || authority == nil || authority.state == nil || authority.state.store != store || nilPort(projects) {
		return nil, fault(f.DependencyUnbound)
	}
	return &RoundModelRequestBinder{store: store, authority: authority, projects: projects,
		active: make(map[i.ExecutionID]context.CancelFunc), pending: make(map[i.ExecutionID]*roundModelRequestUnknown), changed: make(chan struct{})}, nil
}

func (b *RoundModelRequestBinder) enter(ctx context.Context, e i.ExecutionID, next c.RoundID, observe bool) (context.Context, func(), error) {
	if b == nil || nilPort(b.store) || ctx == nil || e.Validate() != nil || next.Validate() != nil {
		return nil, nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	u := b.pending[e]
	if u != nil && u.round != next {
		return nil, nil, fault(f.ConfirmationStale)
	}
	// Stop cannot turn a retained Unknown into a successful join. Only the
	// exact original observation may enter after Stop; it cannot write.
	if b.stopped && !(observe && u != nil) {
		return nil, nil, fault(f.InvalidState)
	}
	if b.active[e] != nil {
		return nil, nil, fault(f.ResourceBusy)
	}
	if !observe && u != nil {
		return nil, nil, u.err
	}
	owned, cancel := context.WithCancel(ctx)
	b.active[e] = cancel
	return owned, func() {
		cancel()
		b.mu.Lock()
		delete(b.active, e)
		close(b.changed)
		b.changed = make(chan struct{})
		b.mu.Unlock()
	}, nil
}

func (b *RoundModelRequestBinder) Stop() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.stopped = true
	for _, cancel := range b.active {
		cancel()
	}
	b.mu.Unlock()
}

func (b *RoundModelRequestBinder) Joined() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stopped && len(b.active) == 0 && len(b.pending) == 0
}

func (b *RoundModelRequestBinder) Drain(ctx context.Context) error {
	if b == nil || ctx == nil {
		return invalid()
	}
	b.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		b.mu.Lock()
		active, changed := len(b.active), b.changed
		var unresolved error
		if active == 0 {
			for _, u := range b.pending {
				unresolved = u.err
				break
			}
		}
		b.mu.Unlock()
		if active == 0 {
			return unresolved
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (*RoundModelRequestBinder) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_round_model_request_binder")
}
func (*RoundModelRequestBinder) LogValue() slog.Value {
	return slog.StringValue("execution_round_model_request_binder")
}
func (*RoundModelRequestBinder) MarshalJSON() ([]byte, error) {
	return []byte(`"execution_round_model_request_binder"`), nil
}
