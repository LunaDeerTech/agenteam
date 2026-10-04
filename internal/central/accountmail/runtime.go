package accountmail

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// Runtime owns mail scheduling, not the process identity. The composition root
// must keep its ProcessGuard until Joined also confirms the whole Sink closed.
// No SMTP configuration is required for the invitation/reset recovery channel.
type Runtime struct{ data func() *runtimeState }
type runtimeState struct {
	mu                         sync.Mutex
	account                    *account.Service
	worker                     *Worker
	starting, started, stopped bool
	startDone, loopsDone       chan struct{}
	scanCancel                 context.CancelFunc
	stop                       chan struct{}
	jobs                       chan c.JobID
	pending                    map[c.JobID]bool
	last                       error
	force                      context.Context
}

func NewRuntime(service *account.Service, worker *Worker) (*Runtime, error) {
	if service == nil || worker == nil || worker.data == nil {
		return nil, invalid()
	}
	s := &runtimeState{account: service, worker: worker, stop: make(chan struct{}), jobs: make(chan c.JobID, 100), pending: make(map[c.JobID]bool)}
	return &Runtime{func() *runtimeState { return s }}, nil
}

// Start uses the caller's remaining startup budget. Initialization does not
// start a second recovery loop or a detached timeout. Serving work receives an
// independent context only after this initialization has actually completed.
func (r *Runtime) Start(ctx context.Context) error {
	s := r.data()
	s.mu.Lock()
	if s.starting || s.started || s.stopped {
		s.mu.Unlock()
		return fail(foundation.InvalidState, nil)
	}
	s.starting = true
	s.startDone = make(chan struct{})
	s.mu.Unlock()
	err := r.maintain(ctx)
	s.mu.Lock()
	s.starting = false
	defer close(s.startDone)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil || s.stopped {
		s.last = err
		s.mu.Unlock()
		if err != nil {
			return fail(foundation.DependencyUnavailable, err)
		}
		return fail(foundation.ShuttingDown, nil)
	}
	s.started = true
	s.loopsDone = make(chan struct{})
	scan, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.scanCancel = cancel
	s.mu.Unlock()
	go r.run(scan)
	return nil
}

func pendingRecovery(err error) bool {
	var f *foundation.Fault
	return errors.As(err, &f) && f.Code == foundation.ResourceBusy
}
func (r *Runtime) maintain(ctx context.Context) error {
	s := r.data()
	_, err := s.account.ReconcileDeliveryIntents(ctx)
	if err != nil && !pendingRecovery(err) {
		return err
	}
	_, err = s.worker.data().Port.RecoverDeliveries(ctx)
	if err != nil && !pendingRecovery(err) {
		return err
	}
	return ctx.Err()
}
func (r *Runtime) run(ctx context.Context) {
	s := r.data()
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-s.stop:
					return
				case id, ok := <-s.jobs:
					if !ok {
						return
					}
					// The registry rejects new work after Stop. Already registered
					// work remains independent of scheduler cancellation.
					_ = s.worker.RunJob(context.WithoutCancel(ctx), id)
					s.mu.Lock()
					delete(s.pending, id)
					s.mu.Unlock()
				}
			}
		}()
	}
	defer func() { close(s.jobs); workers.Wait(); close(s.loopsDone) }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		budget, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := r.maintain(budget)
		cancel()
		s.mu.Lock()
		s.last = err
		stopped := s.stopped
		s.mu.Unlock()
		if stopped {
			return
		}
		// A failed recovery item is protected in the store; it must not starve
		// separate due jobs. NextDeliveries advances a full bounded fair batch.
		budget, cancel = context.WithTimeout(ctx, 2*time.Second)
		ids, scanErr := s.worker.data().Port.NextDeliveries(budget)
		cancel()
		if scanErr == nil {
			for _, id := range ids {
				s.mu.Lock()
				if s.stopped {
					s.mu.Unlock()
					return
				}
				if !s.pending[id] {
					select {
					case s.jobs <- id:
						s.pending[id] = true
					default:
					}
				}
				s.mu.Unlock()
			}
		} else {
			s.mu.Lock()
			s.last = scanErr
			s.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Runtime) StopAdmission() {
	s := r.data()
	s.mu.Lock()
	if !s.stopped {
		s.stopped = true
		close(s.stop)
	}
	if s.scanCancel != nil {
		s.scanCancel()
	}
	s.mu.Unlock()
	s.worker.data().Registry.StopAdmission()
	s.worker.data().RecoveryLog.StopAdmission()
}
func (r *Runtime) waitLoops(ctx context.Context) error {
	s := r.data()
	s.mu.Lock()
	start, loops := s.startDone, s.loopsDone
	s.mu.Unlock()
	for _, done := range []chan struct{}{start, loops} {
		if done == nil {
			continue
		}
		select {
		case <-done:
		case <-ctx.Done():
			return fail(foundation.DependencyUnavailable, ctx.Err())
		}
	}
	return nil
}
func (r *Runtime) Drain(ctx context.Context) error {
	r.StopAdmission()
	if err := r.waitLoops(ctx); err != nil {
		return err
	}
	if err := r.data().worker.data().Registry.Drain(ctx); err != nil {
		return err
	}
	return r.data().worker.data().RecoveryLog.Drain(ctx)
}
func (r *Runtime) Force(ctx context.Context) error {
	r.StopAdmission()
	s := r.data()
	s.mu.Lock()
	if s.force == nil {
		s.force = ctx
	}
	original := s.force
	s.mu.Unlock()
	d := s.worker.data()
	// These concrete implementations initiate cancellation/close even with
	// an exhausted parent. Each wait uses this same original force budget.
	d.Registry.requestForce(original)
	a := d.RecoveryLog.Force(original)
	b := d.Outbound.ForceClose(original)
	c := d.Registry.Drain(original)
	e := r.waitLoops(original)
	return errors.Join(a, b, c, e)
}
func (r *Runtime) Joined() bool {
	s := r.data()
	s.mu.Lock()
	start, loops, stopped := s.startDone, s.loopsDone, s.stopped
	s.mu.Unlock()
	if !stopped {
		return false
	}
	for _, done := range []chan struct{}{start, loops} {
		if done != nil {
			select {
			case <-done:
			default:
				return false
			}
		}
	}
	return s.worker.data().Registry.Joined() && s.worker.data().RecoveryLog.Joined()
}

// Check reports only the scheduler's technical status. It says nothing about
// recipient delivery, identity HTTP binding or overall product readiness.
func (r *Runtime) Check(ctx context.Context) error {
	if ctx.Err() != nil {
		return fail(foundation.DependencyUnavailable, ctx.Err())
	}
	s := r.data()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.stopped {
		return fail(foundation.DependencyUnavailable, nil)
	}
	if s.last != nil {
		return fail(foundation.DependencyUnavailable, s.last)
	}
	return nil
}
