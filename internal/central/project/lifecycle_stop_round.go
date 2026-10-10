package project

import (
	"context"
	"errors"
	"sync"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// LifecycleLocalStopStep is a trusted, fixed, synchronous local provider call.
// Returning nil proves neither a whole participant nor foreign work has joined.
// The assembly owns the provider's same-Store relationship and actual call tail.
type LifecycleLocalStopStep func(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef) error

// LifecycleStopDriver advances only accepted -> stopping and owns one local
// stop round. It never completes participants, cleanup, or the operation itself.
type LifecycleStopDriver struct{ state *lifecycleStopState }
type lifecycleStopState struct {
	store        Store
	authority    *LifecycleAuthority
	processes    oc.ProcessAuthority
	process      oc.ProcessID
	step         LifecycleLocalStopStep
	registration identity.ServiceRegistration
	mu           sync.Mutex
	stopped      bool
	calls        map[c.ProjectID]context.CancelFunc
	joined       map[c.OperationID]lifecycleStopClaim
	changed      chan struct{}
}

func NewLifecycleStopDriver(store Store, authority *LifecycleAuthority, processes oc.ProcessAuthority, step LifecycleLocalStopStep) (*LifecycleStopDriver, error) {
	if nilPort(store) || !authority.bound() || !sameStore(store, authority.store) || nilPort(processes) || step == nil {
		return nil, fault(f.DependencyUnbound)
	}
	process := processes.CurrentProcess()
	if process.Validate() != nil {
		return nil, invalid()
	}
	registration, err := identity.RegisterService(identity.ProjectLifecycle)
	if err != nil {
		return nil, portError(err)
	}
	return &LifecycleStopDriver{state: &lifecycleStopState{store: store, authority: authority, processes: processes, process: process, step: step, registration: registration, calls: map[c.ProjectID]context.CancelFunc{}, joined: map[c.OperationID]lifecycleStopClaim{}, changed: make(chan struct{})}}, nil
}

// Run accepts identity only; action/version are read from the canonical frozen
// operation. A returned error does not undo an already confirmed phase change.
func (d *LifecycleStopDriver) Run(ctx context.Context, project c.ProjectID, operation c.OperationID) error {
	if ctx == nil || project.Validate() != nil || operation.Validate() != nil {
		return invalid()
	}
	if d == nil || d.state == nil {
		return fault(f.DependencyUnbound)
	}
	if err := ctx.Err(); err != nil {
		return portError(err)
	}
	st := d.state
	st.mu.Lock()
	if st.stopped {
		st.mu.Unlock()
		return fault(f.ShuttingDown)
	}
	if _, exists := st.calls[project]; exists || len(st.calls) >= 4 {
		st.mu.Unlock()
		return fault(f.ResourceBusy)
	}
	run, cancel := context.WithCancel(ctx)
	st.calls[project] = cancel
	st.mu.Unlock()
	var claim *lifecycleStopClaim
	terminal := false
	defer func() {
		cancel()
		st.mu.Lock()
		if claim != nil && !terminal {
			st.joined[operation] = *claim
		}
		delete(st.calls, project)
		close(st.changed)
		st.changed = make(chan struct{})
		st.mu.Unlock()
	}()
	var cause c.LifecycleCause
	var err error
	claim, cause, err = st.start(run, project, operation)
	if err != nil {
		return err
	}
	// COMMIT confirmation is not permission to ignore a canceled original call.
	stepErr := st.invoke(run, project, operation, cause)

	// The original provider call has actually returned. The original Run owns
	// this bounded checkpoint too; Stop/Drain cannot pass it in the background.
	checkpoint, end := context.WithTimeout(context.WithoutCancel(run), 3*time.Second)
	finishErr := st.finish(checkpoint, *claim, cause)
	end()
	terminal = finishErr == nil
	if finishErr != nil {
		return errors.Join(finishErr, stepErr)
	}
	return portError(stepErr)
}

func (st *lifecycleStopState) invoke(ctx context.Context, project c.ProjectID, operation c.OperationID, cause c.LifecycleCause) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if cause.Validate() != nil || cause.OperationID != operation {
		return invalid()
	}
	scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: project}
	actorScope, err := identity.InProject(project)
	if err != nil {
		return invalid()
	}
	actor, err := st.registration.Actor(operation.String(), actorScope)
	if err != nil {
		return invalid()
	}
	stepCtx, end := context.WithTimeout(ctx, 2*time.Second)
	defer end()
	err = callLifecycleLocalStop(st.step, stepCtx, actor, cause, scope)
	if err == nil {
		err = stepCtx.Err()
	}
	return err
}

func callLifecycleLocalStop(step LifecycleLocalStopStep, ctx context.Context, actor identity.Actor, cause c.LifecycleCause, scope c.ScopeRef) (err error) {
	defer func() {
		if recover() != nil {
			err = unavailable(nil)
		}
	}()
	return step(ctx, actor, cause, scope)
}

func (d *LifecycleStopDriver) Stop() {
	if d == nil || d.state == nil {
		return
	}
	st := d.state
	st.mu.Lock()
	st.stopped = true
	for _, cancel := range st.calls {
		cancel()
	}
	st.mu.Unlock()
}

func (d *LifecycleStopDriver) Drain(ctx context.Context) error {
	if ctx == nil {
		return invalid()
	}
	if d == nil || d.state == nil {
		return nil
	}
	st := d.state
	for {
		st.mu.Lock()
		if len(st.calls) == 0 {
			st.mu.Unlock()
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
