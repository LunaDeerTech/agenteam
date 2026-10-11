package execution

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"sync"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type RoundInputDependencies struct {
	Projects      c.PreparationProjectGate
	Agents        ac.ExecutionConfiguration
	InitialSkills sc.InitialRoundBindingsReader
	Assignments   sc.RoundAssignmentValidation
}

// RoundInputDeterminer owns synchronous persistence only. No Model/Tool runtime
// port, new worker, default binding, lease retirement or waiting resume exists.
type RoundInputDeterminer struct {
	store     Store
	authority *Authority
	deps      RoundInputDependencies
	slots     *DispatchObservation
	mu        sync.Mutex
	stopped   bool
	active    map[i.ExecutionID]context.CancelFunc
	unknown   map[i.ExecutionID]*roundInputUnknown
	changed   chan struct{}
}
type roundInputUnknown struct {
	round     c.RoundID
	candidate c.RoundInput
	err       error
}

func NewRoundInputDeterminer(store Store, authority *Authority, deps RoundInputDependencies) (*RoundInputDeterminer, error) {
	if nilPort(store) || !reflect.TypeOf(store).Comparable() || authority == nil || authority.state == nil || authority.state.store != store || nilPort(deps.Projects) || nilPort(deps.Agents) || nilPort(deps.InitialSkills) || nilPort(deps.Assignments) {
		return nil, fault(f.DependencyUnbound)
	}
	slots, err := NewDispatchObservation(store)
	if err != nil {
		return nil, err
	}
	return &RoundInputDeterminer{store: store, authority: authority, deps: deps, slots: slots, active: make(map[i.ExecutionID]context.CancelFunc), unknown: make(map[i.ExecutionID]*roundInputUnknown), changed: make(chan struct{})}, nil
}
func (d *RoundInputDeterminer) enter(ctx context.Context, e i.ExecutionID, next c.RoundID) (context.Context, func(), error) {
	if d == nil || nilPort(d.store) || ctx == nil || e.Validate() != nil || next.Validate() != nil {
		return nil, nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return nil, nil, fault(f.InvalidState)
	}
	if d.active[e] != nil {
		return nil, nil, fault(f.ResourceBusy)
	}
	owned, cancel := context.WithCancel(ctx)
	d.active[e] = cancel
	return owned, func() {
		cancel()
		d.mu.Lock()
		delete(d.active, e)
		close(d.changed)
		d.changed = make(chan struct{})
		d.mu.Unlock()
	}, nil
}
func (d *RoundInputDeterminer) Stop() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.stopped = true
	for _, cancel := range d.active {
		cancel()
	}
	d.mu.Unlock()
}
func (d *RoundInputDeterminer) Joined() bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stopped && len(d.active) == 0
}
func (d *RoundInputDeterminer) Drain(ctx context.Context) error {
	if d == nil || ctx == nil {
		return invalid()
	}
	d.Stop()
	for {
		d.mu.Lock()
		done, changed := len(d.active) == 0, d.changed
		d.mu.Unlock()
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

func (d *RoundInputDeterminer) Determine(ctx context.Context, e i.ExecutionID, next c.RoundID) (c.RoundInput, error) {
	var zero c.RoundInput
	ctx, leave, err := d.enter(ctx, e, next)
	if err != nil {
		return zero, err
	}
	defer leave()
	d.mu.Lock()
	uncertain := d.unknown[e]
	d.mu.Unlock()
	if uncertain != nil {
		return zero, uncertain.err
	}
	plan, err := d.discoverRoundInput(ctx, e, next)
	if err != nil {
		return zero, err
	}
	cause, err := f.NewJobCause("execution.round-input.determine", e.String(), next.String())
	if err != nil {
		return zero, err
	}
	var candidate c.RoundInput
	calls := 0
	completed := false
	result := d.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		calls++
		if calls != 1 {
			return fault(f.InvalidState)
		}
		if err := d.store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return portError(err)
		}
		var err error
		candidate, err = d.determineRoundInputInTx(ctx, tx, e, next, plan)
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		completed = true
		return nil
	})
	if result.State() == f.Unknown {
		err = commitError(result)
		d.mu.Lock()
		d.unknown[e] = &roundInputUnknown{round: next, candidate: candidate, err: err}
		d.mu.Unlock()
		return zero, err
	}
	if err = commitError(result); err != nil {
		return zero, err
	}
	if calls != 1 || !completed || candidate.Validate() != nil {
		return zero, fault(f.InvalidState)
	}
	return candidate, nil
}

// Discovery grants no write and never substitutes a caller-supplied receipt.
func (d *RoundInputDeterminer) DiscoverRoundInput(ctx context.Context, e i.ExecutionID, next c.RoundID) (c.RoundInputPlan, error) {
	ctx, leave, err := d.enter(ctx, e, next)
	if err != nil {
		return nil, err
	}
	defer leave()
	d.mu.Lock()
	uncertain := d.unknown[e]
	d.mu.Unlock()
	if uncertain != nil {
		return nil, uncertain.err
	}
	return d.discoverRoundInput(ctx, e, next)
}

// The caller owns this original transaction and its commit outcome. A returned
// value is tentative until that commit is known; no borrowed context survives.
func (d *RoundInputDeterminer) DetermineRoundInputInTx(ctx context.Context, tx f.Tx, e i.ExecutionID, next c.RoundID, plan c.RoundInputPlan) (c.RoundInput, error) {
	ctx, leave, err := d.enter(ctx, e, next)
	if err != nil {
		return c.RoundInput{}, err
	}
	defer leave()
	d.mu.Lock()
	uncertain := d.unknown[e]
	d.mu.Unlock()
	if uncertain != nil {
		return c.RoundInput{}, uncertain.err
	}
	return d.determineRoundInputInTx(ctx, tx, e, next, plan)
}

// Lookup observes immutable input without rerunning current Skill validity or
// obtaining a fresh runtime grant. Absence cannot resolve an original Unknown.
func (d *RoundInputDeterminer) Lookup(ctx context.Context, e i.ExecutionID, next c.RoundID) (c.RoundInput, bool, error) {
	var zero c.RoundInput
	ctx, leave, err := d.enter(ctx, e, next)
	if err != nil {
		return zero, false, err
	}
	defer leave()
	d.mu.Lock()
	original := d.unknown[e]
	d.mu.Unlock()
	if original != nil && original.round != next {
		return zero, false, fault(f.ConfirmationStale)
	}
	scope, err := d.roundScope(ctx, e, next)
	if err != nil {
		return zero, false, err
	}
	var stored c.RoundInput
	found := false
	err = d.roundReadTx(ctx, "lookup", e, next, roundInputLocks(scope.summary.ProjectID, scope.summary.AgentID, e), func(ctx context.Context, tx f.Tx) error {
		x, err := d.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		stored, found, err = loadRoundInput(ctx, x, e)
		if err != nil {
			return err
		}
		if found && stored.Fields().RoundID != next {
			return fault(f.ConfirmationStale)
		}
		return ctx.Err()
	})
	if err != nil || !found {
		return zero, false, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if original := d.unknown[e]; original != nil {
		if original.round != next || original.candidate.Validate() == nil && !bytes.Equal(original.candidate.CanonicalBytes(), stored.CanonicalBytes()) {
			return zero, false, fault(f.ConfirmationStale)
		}
		delete(d.unknown, e)
	}
	return stored, true, nil
}
func (*RoundInputDeterminer) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_round_input_determiner")
}
func (*RoundInputDeterminer) LogValue() slog.Value {
	return slog.StringValue("execution_round_input_determiner")
}
func (*RoundInputDeterminer) MarshalJSON() ([]byte, error) {
	return []byte(`"execution_round_input_determiner"`), nil
}
