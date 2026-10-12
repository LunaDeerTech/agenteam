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
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

var _ c.RoundModelCalls = (*RoundModelCallOwner)(nil)

// Only the public constructor supplies a concrete ProcessGuard. This private
// seam allows package-local controls; it is not a configurable runtime port.
type roundModelCallProcess interface {
	CurrentProcess() (oc.ProcessID, error)
	RequireCurrentProcessInTx(context.Context, f.Tx, oc.ProcessID) error
}
type roundModelCallKey struct {
	execution i.ExecutionID
	next      c.RoundID
}
type roundModelCallWork struct {
	key     roundModelCallKey
	ctx     context.Context
	cancel  context.CancelFunc
	observe bool
	owned   *roundModelClaimRecord
}
type roundModelCallUnknown struct {
	key       roundModelCallKey
	candidate roundModelClaimRecord
	result    f.CommitResult
	err       error
}

// RoundModelCallOwner reserves an existing request. It has no Model caller,
// credential reader, worker, process-death authority or executable lease.
type RoundModelCallOwner struct {
	store    Store
	history  *RoundModelRequestBinder
	projects c.PreparationProjectGate
	guard    roundModelCallProcess
	process  oc.ProcessID
	mu       sync.Mutex
	stopped  bool
	active   map[i.ExecutionID]*roundModelCallWork
	pending  map[i.ExecutionID]*roundModelCallUnknown
	owned    map[roundModelCallKey]roundModelClaimRecord
	changed  chan struct{}
}

// The root must retain this actual guard until all original borrowers join.
// CurrentProcess observes identity; only the later original InTx check proves
// the guard recognizes the same live transaction. Neither check pins it.
func NewRoundModelCallOwner(store Store, authority *Authority, projects c.PreparationProjectGate, guard *object.ProcessGuard) (*RoundModelCallOwner, error) {
	return newRoundModelCallOwner(store, authority, projects, guard)
}

func newRoundModelCallOwner(store Store, authority *Authority, projects c.PreparationProjectGate, guard roundModelCallProcess) (*RoundModelCallOwner, error) {
	history, err := NewRoundModelRequestBinder(store, authority, projects)
	if err != nil {
		return nil, err
	}
	if nilPort(guard) {
		return nil, fault(f.DependencyUnbound)
	}
	process, err := guard.CurrentProcess()
	if err != nil {
		return nil, portError(err)
	}
	if process.Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	return &RoundModelCallOwner{store: store, history: history, projects: projects, guard: guard, process: process,
		active: make(map[i.ExecutionID]*roundModelCallWork), pending: make(map[i.ExecutionID]*roundModelCallUnknown), owned: make(map[roundModelCallKey]roundModelClaimRecord), changed: make(chan struct{})}, nil
}

func (o *RoundModelCallOwner) enter(ctx context.Context, e i.ExecutionID, next c.RoundID, observe bool) (*roundModelCallWork, error) {
	if o == nil || nilPort(o.store) || ctx == nil || e.Validate() != nil || next.Validate() != nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	key := roundModelCallKey{e, next}
	u := o.pending[e]
	if u != nil && u.key != key {
		return nil, fault(f.ConfirmationStale)
	}
	if o.stopped && !(observe && u != nil) {
		return nil, fault(f.ShuttingDown)
	}
	if o.active[e] != nil {
		return nil, fault(f.ResourceBusy)
	}
	if !observe && u != nil {
		return nil, u.err
	}
	owned, cancel := context.WithCancel(ctx)
	work := &roundModelCallWork{key: key, ctx: owned, cancel: cancel, observe: observe}
	if prior, ok := o.owned[key]; ok {
		work.owned = &prior
	}
	o.active[e] = work
	return work, nil
}

func (o *RoundModelCallOwner) requireWork(ctx context.Context, work *roundModelCallWork) error {
	if o == nil || ctx == nil || work == nil || work.ctx == nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.active[work.key.execution] != work {
		return fault(f.Forbidden)
	}
	return work.ctx.Err()
}

func (o *RoundModelCallOwner) leave(work *roundModelCallWork) {
	work.cancel()
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.active[work.key.execution] == work {
		delete(o.active, work.key.execution)
		close(o.changed)
		o.changed = make(chan struct{})
	}
}

func (o *RoundModelCallOwner) Stop() {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stopped = true
	for _, work := range o.active {
		work.cancel()
	}
}
func (o *RoundModelCallOwner) Joined() bool {
	if o == nil {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.stopped && len(o.active) == 0 && len(o.pending) == 0
}
func (o *RoundModelCallOwner) Drain(ctx context.Context) error {
	if o == nil || ctx == nil {
		return invalid()
	}
	o.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		o.mu.Lock()
		active, changed := len(o.active), o.changed
		var unresolved error
		if active == 0 {
			for _, u := range o.pending {
				unresolved = u.err
				break
			}
		}
		o.mu.Unlock()
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
func (*RoundModelCallOwner) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_round_model_call_owner")
}
func (*RoundModelCallOwner) MarshalJSON() ([]byte, error) {
	return []byte(`"execution_round_model_call_owner"`), nil
}
func (*RoundModelCallOwner) LogValue() slog.Value {
	return slog.StringValue("execution_round_model_call_owner")
}
